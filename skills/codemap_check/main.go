// Package main implements the codemap/check skill for staleness detection.
// It determines if a codemap needs updating by checking if referenced files
// have changed since the codemap was created.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/workspaceutil"
	"github.com/joshka0/foxctl/internal/intelligence/codemap"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

const command = "codemap/check"

// input is the expected JSON input for codemap/check operations.
type input struct {
	CodemapID   string `json:"codemap_id"`
	Workspace   string `json:"workspace"`
	IncludeDiff bool   `json:"include_diff"`
	Regenerate  bool   `json:"regenerate"`
}

// changedFile represents a file that has changed since codemap creation.
type changedFile struct {
	Path         string `json:"path"`
	CommitsSince int    `json:"commits_since"`
	LastChange   string `json:"last_change"`
}

// output contains staleness analysis results for a codemap.
type output struct {
	CodemapID      string        `json:"codemap_id"`
	Title          string        `json:"title"`
	CreatedAt      string        `json:"created_at"`
	IsStale        bool          `json:"is_stale"`
	StalenessScore float64       `json:"staleness_score"`
	ChangedFiles   []changedFile `json:"changed_files"`
	TotalFiles     int           `json:"total_files"`
	Recommendation string        `json:"recommendation"`
	Summary        string        `json:"summary"`
	Regenerated    bool          `json:"regenerated,omitempty"`
	NewCodemapID   string        `json:"new_codemap_id,omitempty"`
	Query          string        `json:"query,omitempty"` // Original query for reference
}

// pathPattern extracts file path from annotation path like "@internal/foo.go:42"
var pathPattern = regexp.MustCompile(`^@?(.+?)(?::\d+)?$`)

// main is the skill entry point for codemap/check.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates codemap staleness detection with optional regeneration.
//
// Index:
// - Purpose: Determine if a codemap needs updating by checking if referenced files have changed since creation
// - Flow: validate input → load codemap → extract file paths → check git/mtime changes → calculate staleness → optionally regenerate
// - SideEffects: database queries; git log execution; file system stat calls; subprocess execution (regeneration)
// - FailureModes: invalid codemap ID, load failures, git errors, regeneration failures
// - Observability: emits comprehensive staleness analysis with changed files, recommendations, and regeneration status
// - Related: checkStaleness, loadCodemap, extractFilePaths, checkGitChanges, checkMtimeChanges, regenerateCodemap
// - Keywords: codemap/check, staleness, git, mtime, regeneration, recommendations
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	if in.CodemapID == "" {
		return skillerr.Arg("codemap_id is required", skillerr.WithHint("Provide a codemap_id from codemap/list."))
	}

	workspace := workspaceutil.Resolve(in.Workspace, "", rc.Workspace)

	result, err := checkStaleness(ctx, rc.Config, in.CodemapID, workspace, in.IncludeDiff, in.Regenerate)
	if err != nil {
		return err
	}

	return skillout.Emit(rc, command, result)
}

// checkStaleness performs the core staleness analysis for a codemap.
func checkStaleness(ctx context.Context, cfg config.Config, codemapID, workspace string, includeDiff bool, regenerate bool) (*output, error) {
	// Load codemap from memory store
	cm, err := loadCodemap(ctx, cfg, codemapID, workspace)
	if err != nil {
		return nil, err
	}

	// Extract file paths from annotations
	filePaths := extractFilePaths(cm)
	if len(filePaths) == 0 {
		return &output{
			CodemapID:      codemapID,
			Title:          cm.Title,
			CreatedAt:      cm.CreatedAt.Format(time.RFC3339),
			IsStale:        false,
			StalenessScore: 0,
			TotalFiles:     0,
			Recommendation: "none",
			Summary:        "No file references found in codemap",
			Query:          cm.Query,
			ChangedFiles:   []changedFile{},
		}, nil
	}

	// Check git for changes since codemap creation
	changedFiles, err := checkGitChanges(ctx, workspace, cm.CreatedAt, filePaths)
	if err != nil {
		// Git check failed - fall back to mtime check
		changedFiles, err = checkMtimeChanges(workspace, cm.CreatedAt, filePaths)
		if err != nil {
			return nil, err
		}
	}
	if changedFiles == nil {
		changedFiles = []changedFile{}
	}

	// Calculate staleness score (0.0 = fresh, 1.0 = completely stale)
	stalenessScore := float64(len(changedFiles)) / float64(len(filePaths))
	isStale := len(changedFiles) > 0

	// Determine recommendation
	recommendation := "none"
	switch {
	case stalenessScore >= 0.5:
		recommendation = "regenerate"
	case stalenessScore > 0:
		recommendation = "update"
	}

	// Build summary
	summary := buildSummary(cm, len(filePaths), changedFiles, stalenessScore)

	result := &output{
		CodemapID:      codemapID,
		Title:          cm.Title,
		CreatedAt:      cm.CreatedAt.Format(time.RFC3339),
		IsStale:        isStale,
		StalenessScore: stalenessScore,
		ChangedFiles:   changedFiles,
		TotalFiles:     len(filePaths),
		Recommendation: recommendation,
		Summary:        summary,
		Query:          cm.Query,
	}

	// If regenerate requested and codemap is stale, trigger regeneration
	if regenerate && isStale && cm.Query != "" {
		newID, err := regenerateCodemap(ctx, cfg, cm.Query, workspace)
		if err != nil {
			// Log error but don't fail - return staleness info with regeneration failure note
			result.Summary += fmt.Sprintf(" Regeneration failed: %v", err)
		} else {
			result.Regenerated = true
			result.NewCodemapID = newID
			result.Summary += fmt.Sprintf(" Regenerated as %s.", newID)
		}
	}

	return result, nil
}

// loadCodemap loads a codemap from memory store with format fallback support.
func loadCodemap(ctx context.Context, cfg config.Config, codemapID, workspace string) (*codemap.Codemap, error) {
	memStore, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		return nil, skillerr.WrapIO("open memory store", err)
	}
	defer memStore.Close()

	// Try to find by ID prefix in the name
	name := fmt.Sprintf("codemap://%s", codemapID)
	entry, err := memStore.Get(ctx, name, workspace)
	if err != nil {
		// Try listing all entries and matching by ID
		entries, listErr := memStore.List(ctx, workspace, 100)
		if listErr != nil {
			return nil, skillerr.WrapIO(fmt.Sprintf("get codemap %s", codemapID), err)
		}

		// Find matching codemap
		var found *memory.NamedEntry
		for _, e := range entries {
			if e.Type == "codemap" && strings.Contains(e.Name, codemapID) {
				entryCopy := e
				found = &entryCopy
				break
			}
		}
		if found == nil {
			return nil, skillerr.NotFound(
				fmt.Sprintf("codemap not found: %s", codemapID),
				skillerr.WithHint("Use codemap/list to find available codemaps."),
			)
		}
		entry = *found
	}

	// Parse the codemap from result (support Windsurf format)
	if ws, ok, err := codemap.ParseWindsurfCodemap(entry.Result); err != nil {
		return nil, skillerr.WrapParse("parse codemap", err)
	} else if ok {
		converted := ws.ToCodemap()
		if converted != nil {
			if converted.ID == "" {
				converted.ID = codemapIDFromName(entry.Name)
			}
			if converted.CreatedAt.IsZero() {
				converted.CreatedAt = entry.CreatedAt
			}
			return converted, nil
		}
	}

	var cm codemap.Codemap
	if err := json.Unmarshal(entry.Result, &cm); err != nil {
		return nil, skillerr.WrapParse("parse codemap", err)
	}
	if cm.ID == "" {
		cm.ID = codemapIDFromName(entry.Name)
	}
	if cm.CreatedAt.IsZero() {
		cm.CreatedAt = entry.CreatedAt
	}

	return &cm, nil
}

// extractFilePaths extracts unique file paths from codemap annotations.
func extractFilePaths(cm *codemap.Codemap) []string {
	pathSet := make(map[string]struct{})

	for _, trace := range cm.Traces {
		for _, ann := range trace.Annotations {
			if ann.Path == "" {
				continue
			}
			// Extract path from "@path:line" format
			matches := pathPattern.FindStringSubmatch(ann.Path)
			if len(matches) >= 2 {
				path := matches[1]
				// Normalize path
				path = strings.TrimPrefix(path, "./")
				pathSet[path] = struct{}{}
			}
		}
	}

	paths := make([]string, 0, len(pathSet))
	for p := range pathSet {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// codemapIDFromName extracts codemap ID from storage name.
func codemapIDFromName(name string) string {
	name = strings.TrimPrefix(name, "codemap://")
	name = strings.TrimPrefix(name, "codemap:")
	return name
}

// checkGitChanges uses git log to find files changed since codemap creation.
func checkGitChanges(ctx context.Context, workspace string, since time.Time, files []string) ([]changedFile, error) {
	if len(files) == 0 {
		return []changedFile{}, nil
	}

	// Build git log command
	// git log --since="2024-01-01" --name-only --pretty=format:"%H|%ad" -- file1 file2 ...
	sinceStr := since.Format("2006-01-02T15:04:05")
	args := []string{
		"log",
		"--since=" + sinceStr,
		"--name-only",
		"--pretty=format:%H|%ad",
		"--date=iso",
		"--",
	}
	args = append(args, files...)

	result := executil.Run(ctx, workspace, "git", args...)
	if result.Err != nil {
		return nil, skillerr.Runtimef("git log failed: %v\nstderr: %s", result.Err, string(result.Stderr))
	}

	if len(result.Stdout) == 0 {
		return []changedFile{}, nil // No changes
	}

	// Parse output
	return parseGitLogOutput(string(result.Stdout), files)
}

// parseGitLogOutput parses git log output to extract file change information.
func parseGitLogOutput(output string, watchedFiles []string) ([]changedFile, error) {
	// Build set of watched files for fast lookup
	watchedSet := make(map[string]struct{})
	for _, f := range watchedFiles {
		watchedSet[f] = struct{}{}
	}

	// Track changes per file
	fileChanges := make(map[string]*changedFile)
	var currentCommitDate string

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Check if this is a commit line (contains |)
		if strings.Contains(line, "|") {
			parts := strings.SplitN(line, "|", 2)
			if len(parts) == 2 {
				currentCommitDate = strings.TrimSpace(parts[1])
			}
			continue
		}

		// Check if this file is in our watched set
		if _, ok := watchedSet[line]; ok {
			if existing, ok := fileChanges[line]; ok {
				existing.CommitsSince++
			} else {
				fileChanges[line] = &changedFile{
					Path:         line,
					CommitsSince: 1,
					LastChange:   currentCommitDate,
				}
			}
		}
	}

	// Convert to slice
	result := make([]changedFile, 0, len(fileChanges))
	for _, fc := range fileChanges {
		result = append(result, *fc)
	}

	// Sort by commits (most changed first)
	sort.Slice(result, func(i, j int) bool {
		return result[i].CommitsSince > result[j].CommitsSince
	})

	return result, nil
}

// checkMtimeChanges falls back to file modification time when git is unavailable.
func checkMtimeChanges(workspace string, since time.Time, files []string) ([]changedFile, error) {
	var changed []changedFile

	for _, f := range files {
		fullPath := filepath.Join(workspace, f)
		info, err := os.Stat(fullPath)
		if err != nil {
			continue // File doesn't exist or can't be read
		}

		if info.ModTime().After(since) {
			changed = append(changed, changedFile{
				Path:         f,
				CommitsSince: 1, // Unknown, use 1 as indicator
				LastChange:   info.ModTime().Format(time.RFC3339),
			})
		}
	}

	if changed == nil {
		return []changedFile{}, nil
	}
	return changed, nil
}

// buildSummary creates a human-readable summary of staleness analysis.
func buildSummary(cm *codemap.Codemap, totalFiles int, changed []changedFile, score float64) string {
	if len(changed) == 0 {
		return fmt.Sprintf("Codemap '%s' is fresh. All %d referenced files unchanged since %s.",
			cm.Title, totalFiles, cm.CreatedAt.Format("2006-01-02"))
	}

	changedNames := make([]string, 0, len(changed))
	for _, c := range changed {
		changedNames = append(changedNames, filepath.Base(c.Path))
	}

	if score >= 0.5 {
		return fmt.Sprintf("Codemap '%s' is significantly stale (%.0f%% changed). %d/%d files modified: %s. Recommend regenerating.",
			cm.Title, score*100, len(changed), totalFiles, strings.Join(changedNames, ", "))
	}

	return fmt.Sprintf("Codemap '%s' has minor staleness (%.0f%% changed). %d/%d files modified: %s. Consider updating.",
		cm.Title, score*100, len(changed), totalFiles, strings.Join(changedNames, ", "))
}

// regenerateCodemap calls the codemap/generate skill to create a new codemap with the same query.
func regenerateCodemap(ctx context.Context, cfg config.Config, query, workspace string) (string, error) {
	// Build input for codemap/generate
	genInput := map[string]any{
		"query":     query,
		"workspace": workspace,
	}
	inputJSON, err := json.Marshal(genInput)
	if err != nil {
		return "", skillerr.WrapRuntime("marshal input", err)
	}

	var cm codemap.Codemap
	result, err := executil.RunFoxctlSkillDecode(ctx, workspace, "codemap/generate", inputJSON, &cm)
	if err != nil {
		var decodeErr executil.DecodeError
		if errors.As(err, &decodeErr) {
			return "", skillerr.WrapParse("parse codemap", decodeErr)
		}
		return "", skillerr.Runtimef("codemap/generate failed: %v\nstderr: %s", err, string(result.Stderr))
	}

	return cm.ID, nil
}
