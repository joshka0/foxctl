package ripgrep

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
)

// DefaultExcludeGlobs are patterns excluded by default from searches.
var DefaultExcludeGlobs = []string{
	".git",
	"node_modules",
	"vendor",
	"dist",
	"build",
	"__pycache__",
	".venv",
	"*.min.js",
	"*.min.css",
}

// Match represents a single match from ripgrep.
type Match struct {
	// Path is the file path (relative to workspace if provided).
	Path string `json:"path"`

	// Line is the 1-based line number.
	Line int `json:"line"`

	// Column is the 1-based column number (start of match).
	Column int `json:"column,omitempty"`

	// Text is the line content (may be trimmed).
	Text string `json:"text"`

	// ContextBefore are lines before the match.
	ContextBefore []string `json:"context_before,omitempty"`

	// ContextAfter are lines after the match.
	ContextAfter []string `json:"context_after,omitempty"`
}

// SearchOpts configures a ripgrep search.
type SearchOpts struct {
	// Pattern is the search pattern (required).
	Pattern string

	// Path is the search path (defaults to current directory).
	Path string

	// WorkingDir is the directory to run rg from.
	WorkingDir string

	// CaseInsensitive enables case-insensitive search.
	CaseInsensitive bool

	// FixedStrings treats pattern as literal string (not regex).
	FixedStrings bool

	// WordBoundary matches only at word boundaries.
	WordBoundary bool

	// ContextLines adds lines before and after each match.
	ContextLines int

	// MaxMatches limits total matches returned.
	MaxMatches int

	// MaxMatchesPerFile limits matches per file.
	MaxMatchesPerFile int

	// Hidden includes hidden files and directories.
	Hidden bool

	// IncludeGlobs are glob patterns to include.
	IncludeGlobs []string

	// ExcludeGlobs are glob patterns to exclude.
	// If empty, DefaultExcludeGlobs is used.
	ExcludeGlobs []string

	// FileTypes are ripgrep type filters (e.g., "go", "py", "ts").
	FileTypes []string

	// NoDefaultExcludes disables DefaultExcludeGlobs.
	NoDefaultExcludes bool
}

// SearchResult holds the results of a ripgrep search.
type SearchResult struct {
	// Matches are the found matches.
	Matches []Match `json:"matches"`

	// FileCount is the number of files with matches.
	FileCount int `json:"file_count"`

	// MatchCount is the total number of matches.
	MatchCount int `json:"match_count"`

	// Truncated is true if results were limited by MaxMatches.
	Truncated bool `json:"truncated,omitempty"`
}

// SearchJSON runs ripgrep with JSON output and parses matches.
//
// Index:
// - Purpose: Execute rg search and parse JSON matches
// - Flow: validate pattern -> build args -> run rg -> parse JSON output
// - SideEffects: spawns rg process
// - FailureModes: missing pattern, rg exec errors, JSON parse errors
// - Related: runRipgrep, parseJSONOutput
// - Keywords: ripgrep, rg, json, matches, max_matches, runRipgrep
func SearchJSON(ctx context.Context, opts SearchOpts) (*SearchResult, error) {
	if opts.Pattern == "" {
		return nil, fmt.Errorf("ripgrep: pattern required")
	}

	args := buildArgs(opts, true)
	result, err := runRipgrep(ctx, opts.WorkingDir, args)
	if err != nil {
		return nil, err
	}

	return parseJSONOutput(result.Stdout, opts.WorkingDir, opts.MaxMatches)
}

// FilesWithMatches returns file paths containing matches.
// This is faster than SearchJSON when you only need file paths.
func FilesWithMatches(ctx context.Context, opts SearchOpts) ([]string, error) {
	if opts.Pattern == "" {
		return nil, fmt.Errorf("ripgrep: pattern required")
	}

	args := buildArgs(opts, false)
	args = append([]string{"--files-with-matches"}, args...)

	result, err := runRipgrep(ctx, opts.WorkingDir, args)
	if err != nil {
		return nil, err
	}

	return parseFilePaths(result.Stdout, opts.WorkingDir), nil
}

// Available returns true if ripgrep is installed and accessible.
func Available() bool {
	return executil.HasTool("rg")
}

// CmdResult holds the raw output from ripgrep.
type CmdResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// runRipgrep executes ripgrep and handles common error cases.
func runRipgrep(ctx context.Context, dir string, args []string) (*CmdResult, error) {
	cmd := exec.CommandContext(ctx, "rg", args...)
	if dir != "" {
		cmd.Dir = dir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := &CmdResult{
		Stdout: stdout.Bytes(),
		Stderr: stderr.Bytes(),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			// Exit code 1 = no matches found (not an error)
			if result.ExitCode == 1 {
				return result, nil
			}
			// Exit code 2 = error
			return nil, fmt.Errorf("ripgrep failed (exit %d): %s", result.ExitCode, stderr.String())
		}
		// Check if ripgrep is not installed
		if strings.Contains(err.Error(), "executable file not found") {
			return nil, fmt.Errorf("ripgrep not installed: %w", err)
		}
		return nil, fmt.Errorf("ripgrep exec: %w", err)
	}

	return result, nil
}

// buildArgs constructs ripgrep command arguments.
func buildArgs(opts SearchOpts, jsonOutput bool) []string {
	var args []string

	if jsonOutput {
		args = append(args, "--json")
	}

	args = append(
		args,
		"--no-heading",
		"--line-number",
		"--no-messages",
	)

	if opts.CaseInsensitive {
		args = append(args, "--ignore-case")
	}

	if opts.FixedStrings {
		args = append(args, "--fixed-strings")
	}

	if opts.WordBoundary {
		args = append(args, "--word-regexp")
	}

	if opts.ContextLines > 0 {
		args = append(args, "--context", fmt.Sprintf("%d", opts.ContextLines))
	}

	if opts.MaxMatchesPerFile > 0 {
		args = append(args, "--max-count", fmt.Sprintf("%d", opts.MaxMatchesPerFile))
	}

	if opts.Hidden {
		args = append(args, "--hidden")
	}

	// File types
	for _, ft := range opts.FileTypes {
		args = append(args, "--type", ft)
	}

	// Include globs
	for _, glob := range opts.IncludeGlobs {
		args = append(args, "--glob", glob)
	}

	// Exclude globs
	excludes := opts.ExcludeGlobs
	if len(excludes) == 0 && !opts.NoDefaultExcludes {
		excludes = DefaultExcludeGlobs
	}
	for _, glob := range excludes {
		if !strings.HasPrefix(glob, "!") {
			glob = "!" + glob
		}
		args = append(args, "--glob", glob)
	}

	// Pattern and path (use -- to separate from flags)
	args = append(args, "--", opts.Pattern)
	if opts.Path != "" {
		args = append(args, opts.Path)
	}

	return args
}

// rgMessage is a ripgrep JSON output message.
type rgMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// rgMatchData is the data field for a "match" type message.
type rgMatchData struct {
	Path struct {
		Text string `json:"text"`
	} `json:"path"`
	Lines struct {
		Text string `json:"text"`
	} `json:"lines"`
	LineNumber int          `json:"line_number"`
	Submatches []rgSubmatch `json:"submatches"`
}

type rgSubmatch struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// parseJSONOutput parses ripgrep JSON output into matches.
func parseJSONOutput(output []byte, workspace string, maxMatches int) (*SearchResult, error) {
	if maxMatches <= 0 {
		maxMatches = 10000 // reasonable default
	}

	var matches []Match
	filesWithMatches := make(map[string]bool)

	scanner := bufio.NewScanner(bytes.NewReader(output))
	// Increase buffer size for long lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg rgMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue // skip unparseable lines
		}

		if msg.Type == "match" {
			var data rgMatchData
			if err := json.Unmarshal(msg.Data, &data); err != nil {
				continue
			}

			relPath := relativeTo(workspace, data.Path.Text)
			filesWithMatches[relPath] = true

			// Extract column from first submatch
			column := 0
			if len(data.Submatches) > 0 {
				column = data.Submatches[0].Start + 1 // 1-based
			}

			match := Match{
				Path:   relPath,
				Line:   data.LineNumber,
				Column: column,
				Text:   strings.TrimSuffix(data.Lines.Text, "\n"),
			}

			matches = append(matches, match)

			if len(matches) >= maxMatches {
				return &SearchResult{
					Matches:    matches,
					FileCount:  len(filesWithMatches),
					MatchCount: len(matches),
					Truncated:  true,
				}, nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse ripgrep output: %w", err)
	}

	return &SearchResult{
		Matches:    matches,
		FileCount:  len(filesWithMatches),
		MatchCount: len(matches),
	}, nil
}

// parseFilePaths extracts file paths from --files-with-matches output.
func parseFilePaths(output []byte, workspace string) []string {
	var paths []string
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		path := strings.TrimSpace(scanner.Text())
		if path != "" {
			paths = append(paths, relativeTo(workspace, path))
		}
	}
	return paths
}

// relativeTo makes a path relative to the workspace if possible.
func relativeTo(workspace, path string) string {
	if workspace == "" {
		return filepath.ToSlash(path)
	}
	rel, err := filepath.Rel(workspace, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}
