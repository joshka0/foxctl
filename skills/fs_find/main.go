// Package main implements the fs/find skill.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib"
	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

type input struct {
	Query         string `json:"query"`
	Path          string `json:"path"`
	Pattern       string `json:"pattern"`
	Type          string `json:"type"`
	MinSize       int64  `json:"min_size"`
	MaxSize       int64  `json:"max_size"`
	ModifiedSince string `json:"modified_since"`
	MaxDepth      int    `json:"max_depth"`
	Hidden        bool   `json:"hidden"`
	SortBy        string `json:"sort_by"`
	MaxResults    int    `json:"max_results"`
}

type fileResult struct {
	Path         string   `json:"path"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Size         int64    `json:"size"`
	Modified     string   `json:"modified"`
	ModifiedUnix int64    `json:"modified_unix"`
	Score        float64  `json:"score,omitempty"`
	Matches      []string `json:"matches,omitempty"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("fs/find", "ERUNTIME", err)
	}

	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("fs/find", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("fs/find", "EARG", err)
	}
	if err := run(ctx, rc, in); err != nil {
		fail("fs/find", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, in input) error {
	// Resolve workspace and search path
	workspace := rc.PathValidator.Workspace()
	searchPath := workspace
	if in.Path != "" {
		validated, err := rc.PathValidator.ValidatePath(in.Path)
		if err != nil {
			return fmt.Errorf("path validation failed: %w", err)
		}
		searchPath = validated
	}

	// Parse time filter
	var modifiedSince time.Time
	if in.ModifiedSince != "" {
		parsed, err := parseTimeFilter(in.ModifiedSince)
		if err != nil {
			return fmt.Errorf("invalid modified_since: %w", err)
		}
		modifiedSince = parsed
	}

	// Walk directory tree and collect matching files
	var results []fileResult
	err := filepath.WalkDir(searchPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip files/dirs we can't read
		}

		// Check depth limit
		if in.MaxDepth > 0 {
			depth := strings.Count(relativeTo(searchPath, path), "/")
			if depth > in.MaxDepth {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Skip hidden files/directories
		if !in.Hidden && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip common directories
		if d.IsDir() && isCommonExclude(d.Name()) {
			return filepath.SkipDir
		}

		// Type filter
		if in.Type == "file" && d.IsDir() {
			return nil
		}
		if in.Type == "directory" && !d.IsDir() {
			return nil
		}

		// Get file info
		info, err := d.Info()
		if err != nil {
			return nil
		}

		// Size filters
		if in.MinSize > 0 && info.Size() < in.MinSize {
			return nil
		}
		if in.MaxSize > 0 && info.Size() > in.MaxSize {
			return nil
		}

		// Modified time filter
		if !modifiedSince.IsZero() && info.ModTime().Before(modifiedSince) {
			return nil
		}

		// Pattern matching
		if in.Pattern != "" {
			matched, err := filepath.Match(in.Pattern, d.Name())
			if err != nil || !matched {
				return nil
			}
		}

		// Create result
		relPath := relativeTo(workspace, path)
		result := fileResult{
			Path:         relPath,
			Name:         d.Name(),
			Type:         getFileType(d),
			Size:         info.Size(),
			Modified:     info.ModTime().Format(time.RFC3339),
			ModifiedUnix: info.ModTime().Unix(),
		}

		// Fuzzy scoring if query provided
		if in.Query != "" {
			score, matches := fuzzyScore(in.Query, relPath, d.Name())
			if score > 0 {
				result.Score = score
				result.Matches = matches
				results = append(results, result)
			}
		} else {
			results = append(results, result)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("directory walk failed: %w", err)
	}

	// Sort results
	sortResults(results, in.SortBy)

	// Limit results
	if len(results) > in.MaxResults {
		results = results[:in.MaxResults]
	}

	// Prepare preview and artifact
	preview, truncated := preparePreview(results, rc.MaxPreview)
	artifact, err := persistResultsArtifact(ctx, rc, results, truncated)
	if err != nil {
		return err
	}

	// Build response
	data := map[string]any{
		"result_count": len(results),
		"preview":      preview,
		"sort_by":      in.SortBy,
		"max_results":  in.MaxResults,
	}
	if in.Query != "" {
		data["query"] = in.Query
	}
	if artifact.Digest != "" {
		data["artifact"] = artifact.Digest
	}

	return rc.Emit("fs/find", data, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func parseInput(r io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}
	if in.Type == "" {
		in.Type = "file"
	}
	if in.SortBy == "" {
		in.SortBy = "relevance"
	}
	if in.MaxResults <= 0 {
		in.MaxResults = 100
	}
	return in, nil
}

func parseTimeFilter(since string) (time.Time, error) {
	now := time.Now()

	if strings.HasSuffix(since, "d") {
		days := strings.TrimSuffix(since, "d")
		var d int
		if _, err := fmt.Sscanf(days, "%d", &d); err != nil {
			return time.Time{}, err
		}
		return now.AddDate(0, 0, -d), nil
	}
	if strings.HasSuffix(since, "w") {
		weeks := strings.TrimSuffix(since, "w")
		var w int
		if _, err := fmt.Sscanf(weeks, "%d", &w); err != nil {
			return time.Time{}, err
		}
		return now.AddDate(0, 0, -w*7), nil
	}
	if strings.HasSuffix(since, "m") {
		months := strings.TrimSuffix(since, "m")
		var m int
		if _, err := fmt.Sscanf(months, "%d", &m); err != nil {
			return time.Time{}, err
		}
		return now.AddDate(0, -m, 0), nil
	}
	if strings.HasSuffix(since, "y") {
		years := strings.TrimSuffix(since, "y")
		var y int
		if _, err := fmt.Sscanf(years, "%d", &y); err != nil {
			return time.Time{}, err
		}
		return now.AddDate(-y, 0, 0), nil
	}

	return time.Time{}, fmt.Errorf("invalid time format: use Nd, Nw, Nm, or Ny")
}

func fuzzyScore(query, path, name string) (float64, []string) {
	query = strings.ToLower(query)
	pathLower := strings.ToLower(path)
	nameLower := strings.ToLower(name)

	var score float64
	var matches []string

	// Exact name match (highest score)
	if nameLower == query {
		score += 100
		matches = append(matches, "exact_name")
	}

	// Name contains query
	if strings.Contains(nameLower, query) {
		score += 50
		matches = append(matches, "name_contains")
	}

	// Path contains query
	if strings.Contains(pathLower, query) {
		score += 30
		matches = append(matches, "path_contains")
	}

	// Name starts with query
	if strings.HasPrefix(nameLower, query) {
		score += 40
		matches = append(matches, "name_prefix")
	}

	// Fuzzy match on initials (e.g., "mp" matches "main.py")
	if fuzzyInitials(query, nameLower) {
		score += 20
		matches = append(matches, "fuzzy_initials")
	}

	// Word boundary matching
	words := strings.Fields(strings.ReplaceAll(pathLower, "/", " "))
	for _, word := range words {
		if strings.HasPrefix(word, query) {
			score += 15
			matches = append(matches, "word_prefix")
			break
		}
	}

	return score, matches
}

func fuzzyInitials(query, target string) bool {
	if len(query) == 0 {
		return false
	}

	queryIdx := 0
	for _, ch := range target {
		if queryIdx < len(query) && ch == rune(query[queryIdx]) {
			queryIdx++
		}
		if queryIdx == len(query) {
			return true
		}
	}

	return queryIdx == len(query)
}

func sortResults(results []fileResult, sortBy string) {
	switch sortBy {
	case "name":
		sort.Slice(results, func(i, j int) bool {
			return results[i].Name < results[j].Name
		})
	case "size":
		sort.Slice(results, func(i, j int) bool {
			return results[i].Size > results[j].Size
		})
	case "modified":
		sort.Slice(results, func(i, j int) bool {
			return results[i].ModifiedUnix > results[j].ModifiedUnix
		})
	case "relevance":
		sort.Slice(results, func(i, j int) bool {
			if results[i].Score == results[j].Score {
				return results[i].Name < results[j].Name
			}
			return results[i].Score > results[j].Score
		})
	}
}

func getFileType(d fs.DirEntry) string {
	if d.IsDir() {
		return "directory"
	}
	return "file"
}

func isCommonExclude(name string) bool {
	excludes := []string{
		".git", ".svn", ".hg",
		"node_modules", "vendor", "__pycache__",
		".venv", "venv", ".tox",
		"dist", "build", "target",
	}
	for _, exclude := range excludes {
		if name == exclude {
			return true
		}
	}
	return false
}

func relativeTo(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return filepath.ToSlash(target)
	}
	if strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(target)
	}
	return filepath.ToSlash(rel)
}

func preparePreview(results []fileResult, limit int) ([]fileResult, bool) {
	preview, truncated := skillslib.PreparePreview(results, limit)
	if truncated {
		dup := make([]fileResult, len(preview))
		copy(dup, preview)
		preview = dup
	}
	return preview, truncated
}

func persistResultsArtifact(ctx context.Context, rc *runner.RunnerContext, results []fileResult, truncated bool) (runner.Artifact, error) {
	if !truncated {
		return runner.Artifact{}, nil
	}
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	for _, r := range results {
		if err := enc.Encode(r); err != nil {
			return runner.Artifact{}, fmt.Errorf("encode result: %w", err)
		}
	}
	return runner.PersistBuffer(ctx, rc, buf, "application/x-ndjson", "fs_find")
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit fs/find failure")
	os.Exit(1)
}
