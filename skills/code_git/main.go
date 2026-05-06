// Package main implements the code/git skill.
package main

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/gitutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/pathutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
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
// validateGitAuthor validates the author input to prevent command injection.
func validateGitAuthor(author string) error {
	if author == "" {
		return nil // Empty is allowed, means no filter
	}
	if len(author) > 100 {
		return skillerr.Validation("author name too long (max 100 characters)")
	}
	if strings.ContainsAny(author, "\n\r\t") {
		return skillerr.Validation("author name contains invalid control characters")
	}
	if !gitAuthorPattern.MatchString(author) {
		return skillerr.Validation("author name contains invalid characters (only letters, numbers, spaces, dots, hyphens, underscores, and apostrophes allowed)")
	}
	return nil
}

// validateGitSince validates the since input to prevent command injection
// validateGitSince validates the since input to prevent command injection.
func validateGitSince(since string) error {
	if since == "" {
		return skillerr.Validation("since parameter cannot be empty")
	}
	if len(since) > 20 {
		return skillerr.Validation("since parameter too long")
	}
	// Check if it matches our shorthand pattern (e.g., "7d", "2w")
	if gitSincePattern.MatchString(since) {
		return nil
	}
	// If not shorthand, check if it's a safe ISO date or git-compatible format
	// For now, we'll only allow the shorthand format to be safe
	return skillerr.Validation("since parameter must be in format like '7d', '2w', '3m', or '1y'")
}

// input defines the input parameters for code/git operations.
type input struct {
	QueryType    string `json:"query_type" validate:"required"`
	Path         string `json:"path"`
	Since        string `json:"since"`
	Author       string `json:"author"`
	MaxResults   int    `json:"max_results"`
	ContextLines int    `json:"context_lines"`
}

// gitResult represents a git query result with metadata.
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

// main is the skill entry point for code/git.
func main() {
	skillmain.Main("code/git", run)
}

// run orchestrates git repository analysis with multiple query types and security validation.
//
// Index:
//
//	Purpose: Execute various git queries with security validation and result persistence
//	Flow: validate input → check git availability → resolve workspace → execute query → emit results
//	SideEffects: git command execution; file system access; CAS storage for large result sets
//	FailureModes: invalid git repo, command injection, git execution errors
//	Observability: emits query results, statistics, and artifact hints for large result sets
//	Related: queryRecent, queryHotspots, queryCochanged, queryBlame, queryAuthors, validateGitAuthor, validateGitSince, parseSinceArg
//	Keywords: code/git, git_analysis, repository_queries, security_validation, command_injection_prevention
//
// [[domain:git-repository-analysis]]
// [[invariant:git-input-validation]]
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Apply defaults
	if in.Since == "" {
		in.Since = "1m"
	}
	if in.MaxResults <= 0 {
		in.MaxResults = 100
	}

	// Check if git is available
	gitPath, err := gitutil.RequireGit()
	if err != nil {
		return err
	}

	// Resolve workspace
	workspace, searchPath, err := skillmain.ResolvePath(rc, in.Path)
	if err != nil {
		return err
	}

	// Check if we're in a git repository
	if err := gitutil.CheckRepo(ctx, gitPath, workspace); err != nil {
		return err
	}

	// Execute appropriate git query
	var results []gitResult

	switch in.QueryType {
	case "recent":
		results, err = queryRecent(ctx, gitPath, workspace, searchPath, in)
	case "hotspots":
		results, err = queryHotspots(ctx, gitPath, workspace, searchPath, in)
	case "cochanged":
		results, err = queryCochanged(ctx, gitPath, workspace, searchPath, in)
	case "blame":
		results, err = queryBlame(ctx, gitPath, workspace, searchPath, in)
	case "authors":
		results, err = queryAuthors(ctx, gitPath, workspace, searchPath, in)
	default:
		return skillerr.Validationf("unknown query_type: %s (expected: recent, hotspots, cochanged, blame, authors)", in.QueryType)
	}

	if err != nil {
		return err
	}

	previewResult, err := skillout.PreviewAndPersistNDJSON(ctx, rc, results, rc.MaxPreview, "code_git", true)
	if err != nil {
		return err
	}

	// Build response data
	data := map[string]any{
		"query_type":   in.QueryType,
		"result_count": len(results),
		"preview":      previewResult.Preview,
		"since":        in.Since,
	}
	if in.Path != "" {
		data["path"] = pathutil.RelTo(workspace, searchPath)
	}
	skillout.AddArtifact(data, previewResult.Artifact)

	return skillout.Emit(rc, "code/git", data)
}

// queryRecent executes git log to find recent changes with author and time filtering.
func queryRecent(ctx context.Context, gitPath, workspace, path string, in input) ([]gitResult, error) {
	// git log --since="..." --name-only --pretty=format:"%H|%an|%ae|%ad|%s"

	// Validate inputs to prevent command injection
	if err := validateGitSince(in.Since); err != nil {
		return nil, skillerr.WrapValidation("invalid since parameter", err)
	}
	if err := validateGitAuthor(in.Author); err != nil {
		return nil, skillerr.WrapValidation("invalid author parameter", err)
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

	result := executil.Run(ctx, workspace, gitPath, args...)
	if result.Err != nil {
		return nil, skillerr.Runtimef("git log failed: %v\nstderr: %s", result.Err, string(result.Stderr))
	}

	return parseRecentChanges(result.Stdout, workspace, in.MaxResults), nil
}

// queryHotspots finds frequently changed files by analyzing git commit history.
func queryHotspots(ctx context.Context, gitPath, workspace, path string, in input) ([]gitResult, error) {
	// git log --since="..." --pretty=format: --name-only | sort | uniq -c | sort -rn

	// Validate inputs to prevent command injection
	if err := validateGitSince(in.Since); err != nil {
		return nil, skillerr.WrapValidation("invalid since parameter", err)
	}
	if err := validateGitAuthor(in.Author); err != nil {
		return nil, skillerr.WrapValidation("invalid author parameter", err)
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

	result := executil.Run(ctx, workspace, gitPath, args...)
	if result.Err != nil {
		return nil, skillerr.Runtimef("git log failed: %v\nstderr: %s", result.Err, string(result.Stderr))
	}

	return parseHotspots(result.Stdout, workspace, in.MaxResults), nil
}

// queryCochanged finds files that are often changed together with the target file.
func queryCochanged(ctx context.Context, gitPath, workspace, path string, in input) ([]gitResult, error) {
	if path == workspace {
		return nil, skillerr.Validation("cochanged query requires a specific file path")
	}

	relPath := pathutil.RelTo(workspace, path)

	// Get commits that touched this file
	result := executil.Run(ctx, workspace, gitPath, "log", "--all", "--format=%H", "--", relPath)
	if result.Err != nil {
		return nil, skillerr.Runtimef("git log failed: %v\nstderr: %s", result.Err, string(result.Stderr))
	}

	commits := strings.Split(strings.TrimSpace(string(result.Stdout)), "\n")
	if len(commits) == 0 || commits[0] == "" {
		return []gitResult{}, nil
	}

	// For each commit, get other files changed
	cochangeMap := make(map[string]int)
	for _, commit := range commits {
		result := executil.Run(ctx, workspace, gitPath, "diff-tree", "--no-commit-id", "--name-only", "-r", commit)
		if result.Err != nil {
			continue
		}

		files := strings.Split(strings.TrimSpace(string(result.Stdout)), "\n")
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

// queryBlame executes git blame to show line-by-line authorship information.
func queryBlame(ctx context.Context, gitPath, workspace, path string, in input) ([]gitResult, error) {
	if path == workspace {
		return nil, skillerr.Validation("blame query requires a specific file path")
	}

	relPath := pathutil.RelTo(workspace, path)

	// git blame -e --line-porcelain
	result := executil.Run(ctx, workspace, gitPath, "blame", "-e", "--line-porcelain", "--", relPath)
	if result.Err != nil {
		return nil, skillerr.Runtimef("git blame failed: %v\nstderr: %s", result.Err, string(result.Stderr))
	}

	return parseBlame(result.Stdout, relPath, in.MaxResults), nil
}

// queryAuthors retrieves commit authors within the specified time range.
func queryAuthors(ctx context.Context, gitPath, workspace, path string, in input) ([]gitResult, error) {
	// Validate inputs to prevent command injection
	if err := validateGitSince(in.Since); err != nil {
		return nil, skillerr.WrapValidation("invalid since parameter", err)
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

	result := executil.Run(ctx, workspace, gitPath, args...)
	if result.Err != nil {
		return nil, skillerr.Runtimef("git log failed: %v\nstderr: %s", result.Err, string(result.Stderr))
	}

	return parseAuthors(result.Stdout, in.MaxResults), nil
}

// parseRecentChanges parses git log output into structured recent change results.
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

// parseHotspots converts file change counts into hotspot results.
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

// parseBlame parses git blame porcelain output into structured blame results.
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

// parseAuthors converts git log author output into structured author statistics.
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

// parseSinceArg converts shorthand time format to git-compatible "ago" format.
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
