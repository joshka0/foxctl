// Package main implements the text/ripgrep skill.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib"
	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

type input struct {
	Path            string   `json:"path"`
	Pattern         string   `json:"pattern"`
	CaseInsensitive bool     `json:"case_insensitive"`
	Glob            []string `json:"glob"`
	GlobNot         []string `json:"glob_not"`
	MaxMatches      int      `json:"max_matches"`
	ContextLines    int      `json:"context_lines"`
	Hidden          bool     `json:"hidden"`
}

type match struct {
	File    string `json:"file"`
	Line    int    `json:"line_no"`
	Text    string `json:"line"`
	Snippet string `json:"snippet"`
}

// ripgrep JSON output types
type rgMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type rgMatchData struct {
	Path struct {
		Text string `json:"text"`
	} `json:"path"`
	Lines struct {
		Text string `json:"text"`
	} `json:"lines"`
	LineNumber int `json:"line_number"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("text/ripgrep", "ECONFIG", err)
	}

	rc, err := runner.NewContext(cfg, os.Stdout)
	if err != nil {
		fail("text/ripgrep", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("text/ripgrep", "EARG", err)
	}
	if err := run(ctx, rc, in); err != nil {
		fail("text/ripgrep", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.Context, in input) error {
	// Check if ripgrep is available
	if _, err := exec.LookPath("rg"); err != nil {
		return fmt.Errorf("ripgrep (rg) not found in PATH: %w", err)
	}

	// Resolve workspace and path
	workspace, searchPath, err := resolveWorkspace(rc, in.Path)
	if err != nil {
		return err
	}

	// Build ripgrep command
	args := buildRipgrepArgs(in, searchPath)

	// Execute ripgrep
	cmd := exec.CommandContext(ctx, "rg", args...)
	cmd.Dir = workspace

	output, err := cmd.Output()
	if err != nil {
		// Exit code 1 means no matches found, which is not an error
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return emitEmptyResult(rc, in)
		}
		return fmt.Errorf("ripgrep execution failed: %w", err)
	}

	// Parse ripgrep JSON output
	matches, fileHits, err := parseRipgrepOutput(output, workspace, in.MaxMatches)
	if err != nil {
		return err
	}

	// Prepare preview and artifact
	preview, truncated := preparePreview(matches, rc.MaxPreview)
	artifact, err := persistMatchesArtifact(ctx, rc, matches, truncated)
	if err != nil {
		return err
	}

	// Build response data
	data := map[string]any{
		"pattern":          in.Pattern,
		"case_insensitive": in.CaseInsensitive,
		"match_count":      len(matches),
		"files_touched":    len(fileHits),
		"preview":          preview,
		"top_files":        summarizeTopFiles(fileHits, 5),
		"max_matches":      in.MaxMatches,
	}
	if artifact.Digest != "" {
		data["artifact"] = artifact.Digest
		data["artifact_kind"] = artifact.Kind
		data["artifact_size_bytes"] = artifact.Size
	}

	return rc.Emit("text/ripgrep", data, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func parseInput(r io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}
	if strings.TrimSpace(in.Pattern) == "" {
		return input{}, fmt.Errorf("pattern is required")
	}
	if in.MaxMatches <= 0 {
		in.MaxMatches = 10000
	}
	if in.ContextLines < 0 {
		in.ContextLines = 0
	}
	return in, nil
}

func resolveWorkspace(rc *runner.Context, candidate string) (string, string, error) {
	workspace := rc.PathValidator.Workspace()
	if strings.TrimSpace(candidate) == "" {
		return workspace, workspace, nil
	}
	resolved, err := rc.PathValidator.ValidatePath(candidate)
	if err != nil {
		return "", "", fmt.Errorf("path validation failed: %w", err)
	}
	return workspace, resolved, nil
}

func buildRipgrepArgs(in input, searchPath string) []string {
	args := []string{
		"--json",                                        // JSON output
		"--no-heading",                                  // Included in JSON output
		"--line-number",                                 // Include line numbers
		"--no-messages",                                 // Suppress error messages
		"--max-count", fmt.Sprintf("%d", in.MaxMatches), // Limit matches per file
	}

	// Case sensitivity
	if in.CaseInsensitive {
		args = append(args, "--ignore-case")
	}

	// Context lines
	if in.ContextLines > 0 {
		args = append(args, "--context", fmt.Sprintf("%d", in.ContextLines))
	}

	// Hidden files
	if in.Hidden {
		args = append(args, "--hidden")
	}

	// Include globs
	for _, glob := range in.Glob {
		args = append(args, "--glob", glob)
	}

	// Exclude globs
	for _, glob := range in.GlobNot {
		args = append(args, "--glob", "!"+glob)
	}

	// Default exclusions (if no custom glob_not provided)
	if len(in.GlobNot) == 0 {
		args = append(args,
			"--glob", "!.git",
			"--glob", "!node_modules",
			"--glob", "!vendor",
		)
	}

	// Pattern and search path
	args = append(args, "--", in.Pattern, searchPath)

	return args
}

func parseRipgrepOutput(output []byte, workspace string, maxMatches int) ([]match, map[string]int, error) {
	var matches []match
	fileHits := make(map[string]int)

	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() && len(matches) < maxMatches {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg rgMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			// Skip unparseable lines
			continue
		}

		if msg.Type == "match" {
			var matchData rgMatchData
			if err := json.Unmarshal(msg.Data, &matchData); err != nil {
				continue
			}

			// Extract line text (ripgrep includes newline)
			lineText := strings.TrimSuffix(matchData.Lines.Text, "\n")
			snippet := trimLine(lineText, 240)

			// Make path relative to workspace
			relPath := relativeTo(workspace, matchData.Path.Text)

			matches = append(matches, match{
				File:    relPath,
				Line:    matchData.LineNumber,
				Text:    lineText,
				Snippet: snippet,
			})

			fileHits[relPath]++
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("scan ripgrep output: %w", err)
	}

	return matches, fileHits, nil
}

func emitEmptyResult(rc *runner.Context, in input) error {
	data := map[string]any{
		"pattern":          in.Pattern,
		"case_insensitive": in.CaseInsensitive,
		"match_count":      0,
		"files_touched":    0,
		"preview":          []match{},
		"top_files":        [][2]any{},
		"max_matches":      in.MaxMatches,
	}
	return rc.Emit("text/ripgrep", data, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func preparePreview(matches []match, limit int) ([]match, bool) {
	preview, truncated := skillslib.PreparePreview(matches, limit)
	if truncated {
		dup := make([]match, len(preview))
		copy(dup, preview)
		preview = dup
	}
	return preview, truncated
}

func persistMatchesArtifact(ctx context.Context, rc *runner.Context, matches []match, truncated bool) (runner.Artifact, error) {
	if !truncated {
		return runner.Artifact{}, nil
	}
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	for _, m := range matches {
		if err := enc.Encode(m); err != nil {
			return runner.Artifact{}, fmt.Errorf("encode match: %w", err)
		}
	}
	return runner.PersistBuffer(ctx, rc, buf, "application/x-ndjson", "text_ripgrep")
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

func summarizeTopFiles(counts map[string]int, limit int) [][2]any {
	type kv struct {
		File  string
		Count int
	}
	var list []kv
	for file, count := range counts {
		list = append(list, kv{File: file, Count: count})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Count == list[j].Count {
			return list[i].File < list[j].File
		}
		return list[i].Count > list[j].Count
	})
	if len(list) > limit {
		list = list[:limit]
	}
	var out [][2]any
	for _, item := range list {
		out = append(out, [2]any{item.File, item.Count})
	}
	return out
}

func trimLine(line string, limit int) string {
	if len(line) <= limit {
		return line
	}
	return line[:limit] + "..."
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit text/ripgrep failure")
	os.Exit(1)
}
