// Package main implements the codemap/check skill for staleness detection.
// It determines if a codemap needs updating by checking if referenced files
// have changed since the codemap was created.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/codemap"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

type input struct {
	CodemapID   string `json:"codemap_id"`
	Workspace   string `json:"workspace"`
	IncludeDiff bool   `json:"include_diff"`
	Regenerate  bool   `json:"regenerate"`
}

type changedFile struct {
	Path         string `json:"path"`
	CommitsSince int    `json:"commits_since"`
	LastChange   string `json:"last_change"`
}

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

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("codemap/check", "ERUNTIME", err)
	}

	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("codemap/check", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	var in input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fail("codemap/check", "EARG", fmt.Errorf("decode input: %w", err))
	}

	if in.CodemapID == "" {
		fail("codemap/check", "EARG", fmt.Errorf("codemap_id is required"))
	}

	workspace := in.Workspace
	if workspace == "" {
		if wd, err := os.Getwd(); err == nil && wd != "" {
			workspace = wd
		} else {
			fail("codemap/check", "ERUNTIME", fmt.Errorf("detect workspace: %w", err))
		}
	}

	result, err := checkStaleness(ctx, cfg, in.CodemapID, workspace, in.IncludeDiff, in.Regenerate)
	if err != nil {
		fail("codemap/check", "ERUNTIME", err)
	}

	if err := rc.Emit("codemap/check", result, "application/json", envelope.Meta{
		Source:    "run",
		Runner:    "exec",
		Workspace: workspace,
	}); err != nil {
		fail("codemap/check", "ERUNTIME", err)
	}
}

func checkStaleness(ctx context.Context, cfg config.Config, codemapID, workspace string, includeDiff bool, regenerate bool) (*output, error) {
	// Load codemap from memory store
	cm, err := loadCodemap(ctx, cfg, codemapID, workspace)
	if err != nil {
		return nil, fmt.Errorf("load codemap: %w", err)
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
			return nil, fmt.Errorf("check changes: %w", err)
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

func loadCodemap(ctx context.Context, cfg config.Config, codemapID, workspace string) (*codemap.Codemap, error) {
	storageRoot := cfg.Storage.Root
	casRoot := filepath.Join(filepath.Dir(storageRoot), "cas")

	memStore, err := memory.Open(ctx, storageRoot, casRoot)
	if err != nil {
		return nil, fmt.Errorf("open memory store: %w", err)
	}
	defer memStore.Close()

	// Try to find by ID prefix in the name
	name := fmt.Sprintf("codemap://%s", codemapID)
	entry, err := memStore.Get(ctx, name, workspace)
	if err != nil {
		// Try listing all entries and matching by ID
		entries, listErr := memStore.List(ctx, workspace, 100)
		if listErr != nil {
			return nil, fmt.Errorf("get codemap %s: %w", codemapID, err)
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
			return nil, fmt.Errorf("codemap not found: %s", codemapID)
		}
		entry = *found
	}

	// Parse the codemap from result
	var cm codemap.Codemap
	if err := json.Unmarshal(entry.Result, &cm); err != nil {
		return nil, fmt.Errorf("parse codemap: %w", err)
	}

	return &cm, nil
}

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

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workspace

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	if len(out) == 0 {
		return []changedFile{}, nil // No changes
	}

	// Parse output
	return parseGitLogOutput(string(out), files)
}

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

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit failure")
	os.Exit(1)
}

// regenerateCodemap calls the codemap/generate skill to create a new codemap
// with the same query. Returns the new codemap ID on success.
func regenerateCodemap(ctx context.Context, cfg config.Config, query, workspace string) (string, error) {
	// Build input for codemap/generate
	genInput := map[string]any{
		"query":     query,
		"workspace": workspace,
	}
	inputJSON, err := json.Marshal(genInput)
	if err != nil {
		return "", fmt.Errorf("marshal input: %w", err)
	}

	// Execute codemap/generate skill via exec
	// We use the agentctl binary to run the skill
	agentctlBin := "agentctl"
	if bin := os.Getenv("AGENTCTL_BIN"); bin != "" {
		agentctlBin = bin
	}

	cmd := exec.CommandContext(ctx, agentctlBin, "run", "codemap/generate", "--input-file", "-")
	cmd.Dir = workspace
	cmd.Stdin = strings.NewReader(string(inputJSON))

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("codemap/generate failed: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("exec codemap/generate: %w", err)
	}

	// Parse envelope response to get new codemap ID
	var env envelope.Envelope
	if err := json.Unmarshal(output, &env); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if env.Status != "ok" {
		var errMsg string
		if env.Error.Message != "" {
			errMsg = env.Error.Message
		} else {
			b, _ := json.Marshal(env)
			errMsg = string(b)
		}
		return "", fmt.Errorf("codemap/generate error: %s", errMsg)
	}

	// Extract ID from data
	dataBytes, err := json.Marshal(env.Data)
	if err != nil {
		return "", fmt.Errorf("marshal data: %w", err)
	}

	var cm codemap.Codemap
	if err := json.Unmarshal(dataBytes, &cm); err != nil {
		return "", fmt.Errorf("parse codemap: %w", err)
	}

	return cm.ID, nil
}
