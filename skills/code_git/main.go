// Package main implements the code/git skill.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib"
	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

// Security: Regex patterns for input validation to prevent command injection
var (
	// gitAuthorPattern allows alphanumeric, spaces, dots, hyphens, underscores, and apostrophes
	// Common in names but prevents special characters that could be used for injection
	gitAuthorPattern = regexp.MustCompile(`^[a-zA-Z0-9\s.\-_']+$`)

	// gitSincePattern matches valid time specifications like "7d", "2w", "3m", "1y"
	gitSincePattern = regexp.MustCompile(`^(\d+)([dwmy])$`)
)

// validateGitAuthor validates the author input to prevent command injection
func validateGitAuthor(author string) error {
	if author == "" {
		return nil // Empty is allowed, means no filter
	}
	if len(author) > 100 {
		return errors.New("author name too long (max 100 characters)")
	}
	if strings.ContainsAny(author, "\n\r\t") {
		return errors.New("author name contains invalid control characters")
	}
	if !gitAuthorPattern.MatchString(author) {
		return errors.New("author name contains invalid characters (only letters, numbers, spaces, dots, hyphens, underscores, and apostrophes allowed)")
	}
	return nil
}

// validateGitSince validates the since input to prevent command injection
func validateGitSince(since string) error {
	if since == "" {
		return errors.New("since parameter cannot be empty")
	}
	if len(since) > 20 {
		return errors.New("since parameter too long")
	}
	// Check if it matches our shorthand pattern (e.g., "7d", "2w")
	if gitSincePattern.MatchString(since) {
		return nil
	}
	// If not shorthand, check if it's a safe ISO date or git-compatible format
	// For now, we'll only allow the shorthand format to be safe
	return errors.New("since parameter must be in format like '7d', '2w', '3m', or '1y'")
}

type input struct {
	QueryType    string `json:"query_type"`
	Path         string `json:"path"`
	Since        string `json:"since"`
	Author       string `json:"author"`
	MaxResults   int    `json:"max_results"`
	ContextLines int    `json:"context_lines"`
}

type gitResult struct {
	Type     string         `json:"type"`
	File     string         `json:"file,omitempty"`
	Count    int            `json:"count,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	Message  string         `json:"message,omitempty"`
	Related  []string       `json:"related,omitempty"`
	Author   string         `json:"author,omitempty"`
	Email    string         `json:"email,omitempty"`
	Date     string         `json:"date,omitempty"`
	Commit   string         `json:"commit,omitempty"`
	Line     int            `json:"line,omitempty"`
	LineText string         `json:"line_text,omitempty"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("code/git", "ECONFIG", err)
	}

	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("code/git", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("code/git", "EARG", err)
	}
	if err := run(ctx, rc, in); err != nil {
		fail("code/git", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, in input) error {
	// Check if git is available
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git not found in PATH: %w", err)
	}

	// Resolve workspace
	workspace := rc.PathValidator.Workspace()
	searchPath := workspace
	if in.Path != "" {
		validated, err := rc.PathValidator.ValidatePath(in.Path)
		if err != nil {
			return fmt.Errorf("path validation failed: %w", err)
		}
		searchPath = validated
	}

	// Check if we're in a git repository
	if err := checkGitRepo(ctx, workspace); err != nil {
		return err
	}

	// Execute appropriate git query
	var results []gitResult
	var err error

	switch in.QueryType {
	case "recent":
		results, err = queryRecent(ctx, workspace, searchPath, in)
	case "hotspots":
		results, err = queryHotspots(ctx, workspace, searchPath, in)
	case "cochanged":
		results, err = queryCochanged(ctx, workspace, searchPath, in)
	case "blame":
		results, err = queryBlame(ctx, workspace, searchPath, in)
	case "authors":
		results, err = queryAuthors(ctx, workspace, searchPath, in)
	default:
		return fmt.Errorf("unknown query_type: %s (expected: recent, hotspots, cochanged, blame, authors)", in.QueryType)
	}

	if err != nil {
		return err
	}

	// Prepare preview and artifact
	preview, truncated := preparePreview(results, rc.MaxPreview)
	artifact, err := persistResultsArtifact(ctx, rc, results, truncated)
	if err != nil {
		return err
	}

	// Build response data
	data := map[string]any{
		"query_type":   in.QueryType,
		"result_count": len(results),
		"preview":      preview,
		"since":        in.Since,
	}
	if in.Path != "" {
		data["path"] = relativeTo(workspace, searchPath)
	}
	if artifact.Digest != "" {
		data["artifact"] = artifact.Digest
		data["artifact_kind"] = artifact.Kind
		data["artifact_size_bytes"] = artifact.Size
	}

	return rc.Emit("code/git", data, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func parseInput(r io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}
	if in.QueryType == "" {
		return input{}, fmt.Errorf("query_type is required")
	}
	if in.Since == "" {
		in.Since = "1m"
	}
	if in.MaxResults <= 0 {
		in.MaxResults = 100
	}
	return in, nil
}

func checkGitRepo(ctx context.Context, workspace string) error {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--git-dir")
	cmd.Dir = workspace
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("not a git repository")
	}
	return nil
}

func queryRecent(ctx context.Context, workspace, path string, in input) ([]gitResult, error) {
	// git log --since="..." --name-only --pretty=format:"%H|%an|%ae|%ad|%s"

	// Validate inputs to prevent command injection
	if err := validateGitSince(in.Since); err != nil {
		return nil, fmt.Errorf("invalid since parameter: %w", err)
	}
	if err := validateGitAuthor(in.Author); err != nil {
		return nil, fmt.Errorf("invalid author parameter: %w", err)
	}

	sinceArg := parseSinceArg(in.Since)

	args := []string{
		"log",
		"--since=" + sinceArg,
		"--name-only",
		"--pretty=format:%H|%an|%ae|%ad|%s",
	}

	if in.Author != "" {
		args = append(args, "--author="+in.Author)
	}

	if path != workspace {
		args = append(args, "--", path)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workspace

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	return parseRecentChanges(output, workspace, in.MaxResults), nil
}

func queryHotspots(ctx context.Context, workspace, path string, in input) ([]gitResult, error) {
	// git log --since="..." --pretty=format: --name-only | sort | uniq -c | sort -rn

	// Validate inputs to prevent command injection
	if err := validateGitSince(in.Since); err != nil {
		return nil, fmt.Errorf("invalid since parameter: %w", err)
	}
	if err := validateGitAuthor(in.Author); err != nil {
		return nil, fmt.Errorf("invalid author parameter: %w", err)
	}

	sinceArg := parseSinceArg(in.Since)

	args := []string{
		"log",
		"--since=" + sinceArg,
		"--pretty=format:",
		"--name-only",
	}

	if in.Author != "" {
		args = append(args, "--author="+in.Author)
	}

	if path != workspace {
		args = append(args, "--", path)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workspace

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	return parseHotspots(output, workspace, in.MaxResults), nil
}

func queryCochanged(ctx context.Context, workspace, path string, in input) ([]gitResult, error) {
	if path == workspace {
		return nil, fmt.Errorf("cochanged query requires a specific file path")
	}

	relPath := relativeTo(workspace, path)

	// Get commits that touched this file
	cmd := exec.CommandContext(ctx, "git", "log", "--all", "--format=%H", "--", relPath)
	cmd.Dir = workspace

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	commits := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(commits) == 0 || commits[0] == "" {
		return []gitResult{}, nil
	}

	// For each commit, get other files changed
	cochangeMap := make(map[string]int)
	for _, commit := range commits {
		cmd := exec.CommandContext(ctx, "git", "diff-tree", "--no-commit-id", "--name-only", "-r", commit)
		cmd.Dir = workspace

		output, err := cmd.Output()
		if err != nil {
			continue
		}

		files := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, file := range files {
			if file != "" && file != relPath {
				cochangeMap[file]++
			}
		}
	}

	// Convert to results
	var results []gitResult
	for file, count := range cochangeMap {
		results = append(results, gitResult{
			Type:  "cochanged",
			File:  file,
			Count: count,
		})
	}

	// Sort by count descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Count > results[j].Count
	})

	if len(results) > in.MaxResults {
		results = results[:in.MaxResults]
	}

	return results, nil
}

func queryBlame(ctx context.Context, workspace, path string, in input) ([]gitResult, error) {
	if path == workspace {
		return nil, fmt.Errorf("blame query requires a specific file path")
	}

	relPath := relativeTo(workspace, path)

	// git blame -e --line-porcelain
	cmd := exec.CommandContext(ctx, "git", "blame", "-e", "--line-porcelain", "--", relPath)
	cmd.Dir = workspace

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git blame failed: %w", err)
	}

	return parseBlame(output, relPath, in.MaxResults), nil
}

func queryAuthors(ctx context.Context, workspace, path string, in input) ([]gitResult, error) {
	// Validate inputs to prevent command injection
	if err := validateGitSince(in.Since); err != nil {
		return nil, fmt.Errorf("invalid since parameter: %w", err)
	}

	sinceArg := parseSinceArg(in.Since)

	args := []string{
		"log",
		"--since=" + sinceArg,
		"--format=%an|%ae",
	}

	if path != workspace {
		args = append(args, "--", path)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workspace

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	return parseAuthors(output, in.MaxResults), nil
}

func parseRecentChanges(output []byte, _ string, maxResults int) []gitResult {
	var results []gitResult
	lines := strings.Split(string(output), "\n")

	var currentCommit map[string]string
	filesSeen := make(map[string]bool)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.Contains(line, "|") {
			// Commit header: %H|%an|%ae|%ad|%s
			parts := strings.SplitN(line, "|", 5)
			if len(parts) == 5 {
				currentCommit = map[string]string{
					"hash":    parts[0],
					"author":  parts[1],
					"email":   parts[2],
					"date":    parts[3],
					"subject": parts[4],
				}
				filesSeen = make(map[string]bool)
			}
		} else if currentCommit != nil && line != "" {
			// File name
			if !filesSeen[line] {
				filesSeen[line] = true
				results = append(results, gitResult{
					Type:    "recent_change",
					File:    line,
					Commit:  currentCommit["hash"][:7],
					Author:  currentCommit["author"],
					Email:   currentCommit["email"],
					Date:    currentCommit["date"],
					Message: currentCommit["subject"],
				})

				if len(results) >= maxResults {
					break
				}
			}
		}
	}

	return results
}

func parseHotspots(output []byte, _ string, maxResults int) []gitResult {
	lines := strings.Split(string(output), "\n")
	fileCounts := make(map[string]int)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			fileCounts[line]++
		}
	}

	var results []gitResult
	for file, count := range fileCounts {
		results = append(results, gitResult{
			Type:  "hotspot",
			File:  file,
			Count: count,
		})
	}

	// Sort by count descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Count > results[j].Count
	})

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return results
}

func parseBlame(output []byte, file string, maxResults int) []gitResult {
	var results []gitResult
	lines := strings.Split(string(output), "\n")

	commitRe := regexp.MustCompile(`^([0-9a-f]{40}) (\d+) (\d+)`)
	authorRe := regexp.MustCompile(`^author (.+)`)
	emailRe := regexp.MustCompile(`^author-mail <(.+)>`)
	timeRe := regexp.MustCompile(`^author-time (\d+)`)

	var currentCommit, currentAuthor, currentEmail, currentTime string
	var currentLine int

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if match := commitRe.FindStringSubmatch(line); match != nil {
			currentCommit = match[1][:7]
			if lineNum, err := strconv.Atoi(match[3]); err == nil {
				currentLine = lineNum
			}
		} else if match := authorRe.FindStringSubmatch(line); match != nil {
			currentAuthor = match[1]
		} else if match := emailRe.FindStringSubmatch(line); match != nil {
			currentEmail = match[1]
		} else if match := timeRe.FindStringSubmatch(line); match != nil {
			currentTime = match[1]
		} else if strings.HasPrefix(line, "\t") {
			// Line content
			lineText := strings.TrimPrefix(line, "\t")
			results = append(results, gitResult{
				Type:     "blame",
				File:     file,
				Line:     currentLine,
				LineText: lineText,
				Commit:   currentCommit,
				Author:   currentAuthor,
				Email:    currentEmail,
				Data: map[string]any{
					"timestamp": currentTime,
				},
			})

			if len(results) >= maxResults {
				break
			}
		}
	}

	return results
}

func parseAuthors(output []byte, maxResults int) []gitResult {
	lines := strings.Split(string(output), "\n")
	authorCounts := make(map[string]map[string]any)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}

		name := parts[0]
		email := parts[1]
		key := email // Use email as unique key

		if _, exists := authorCounts[key]; !exists {
			authorCounts[key] = map[string]any{
				"name":  name,
				"email": email,
				"count": 0,
			}
		}
		authorCounts[key]["count"] = authorCounts[key]["count"].(int) + 1
	}

	var results []gitResult
	for _, data := range authorCounts {
		results = append(results, gitResult{
			Type:   "author",
			Author: data["name"].(string),
			Email:  data["email"].(string),
			Count:  data["count"].(int),
		})
	}

	// Sort by count descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Count > results[j].Count
	})

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return results
}

func parseSinceArg(since string) string {
	// Convert shorthand to git-compatible format
	// "7d" -> "7 days ago", "1w" -> "1 week ago", "1m" -> "1 month ago"
	if strings.HasSuffix(since, "d") {
		days := strings.TrimSuffix(since, "d")
		return days + " days ago"
	}
	if strings.HasSuffix(since, "w") {
		weeks := strings.TrimSuffix(since, "w")
		return weeks + " weeks ago"
	}
	if strings.HasSuffix(since, "m") {
		months := strings.TrimSuffix(since, "m")
		return months + " months ago"
	}
	if strings.HasSuffix(since, "y") {
		years := strings.TrimSuffix(since, "y")
		return years + " years ago"
	}
	return since
}

func preparePreview(results []gitResult, limit int) ([]gitResult, bool) {
	preview, truncated := skillslib.PreparePreview(results, limit)
	if truncated {
		dup := make([]gitResult, len(preview))
		copy(dup, preview)
		preview = dup
	}
	return preview, truncated
}

func persistResultsArtifact(ctx context.Context, rc *runner.RunnerContext, results []gitResult, truncated bool) (runner.Artifact, error) {
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
	return runner.PersistBuffer(ctx, rc, buf, "application/x-ndjson", "code_git")
}

func relativeTo(base, target string) string {
	if base == "" {
		if rel, err := filepath.Rel(".", target); err == nil {
			return filepath.ToSlash(rel)
		}
		return filepath.ToSlash(target)
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return filepath.ToSlash(target)
	}
	if strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(target)
	}
	return filepath.ToSlash(rel)
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit code/git failure")
	os.Exit(1)
}
