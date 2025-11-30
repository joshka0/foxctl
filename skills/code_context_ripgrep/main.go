// Package main implements the code/context_ripgrep skill.
// It performs ripgrep searches and expands matches to their surrounding
// code blocks (functions, methods, classes) using language-specific heuristics.
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
	MaxBlocks       int      `json:"max_blocks"`
	MaxBlockLines   int      `json:"max_block_lines"`
	Hidden          bool     `json:"hidden"`
}

// Block represents an expanded code block containing one or more matches.
type Block struct {
	File       string `json:"file"`
	Language   string `json:"language"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	HeaderLine string `json:"header_line,omitempty"`
	SymbolName string `json:"symbol_name,omitempty"`
	SymbolKind string `json:"symbol_kind,omitempty"`
	Source     string `json:"source"`
	MatchLines []int  `json:"match_lines"`
	MatchCount int    `json:"match_count"`
}

// BlockPreview is a smaller version of Block for inline envelope data.
type BlockPreview struct {
	File       string `json:"file"`
	Language   string `json:"language"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	HeaderLine string `json:"header_line,omitempty"`
	SymbolName string `json:"symbol_name,omitempty"`
	SymbolKind string `json:"symbol_kind,omitempty"`
	MatchCount int    `json:"match_count"`
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

// rawMatch is a single ripgrep match before expansion.
type rawMatch struct {
	File string
	Line int
	Text string
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("code/context_ripgrep", "ERUNTIME", err)
	}

	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("code/context_ripgrep", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("code/context_ripgrep", "EARG", err)
	}
	if err := run(ctx, rc, in); err != nil {
		fail("code/context_ripgrep", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, in input) error {
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

	// Parse ripgrep JSON output into raw matches
	rawMatches, err := parseRipgrepOutput(output, workspace, in.MaxMatches)
	if err != nil {
		return err
	}

	if len(rawMatches) == 0 {
		return emitEmptyResult(rc, in)
	}

	// Group matches by file
	matchesByFile := groupByFile(rawMatches)

	// Expand matches to blocks
	var blocks []Block
	fileHits := make(map[string]int)

	for file, fileMatches := range matchesByFile {
		if len(blocks) >= in.MaxBlocks {
			break
		}

		absPath := filepath.Join(workspace, file)
		content, err := os.ReadFile(absPath)
		if err != nil {
			// Skip files we can't read
			continue
		}

		lines := strings.Split(string(content), "\n")
		lang := detectLanguage(file)
		expander := NewExpander(lang, in.MaxBlockLines)

		fileBlocks := expander.ExpandMatches(file, lines, fileMatches)
		for _, block := range fileBlocks {
			if len(blocks) >= in.MaxBlocks {
				break
			}
			blocks = append(blocks, block)
			fileHits[file] += block.MatchCount
		}
	}

	// Prepare preview and artifact
	preview := prepareBlockPreview(blocks, rc.MaxPreview)
	artifact, err := persistBlocksArtifact(ctx, rc, blocks)
	if err != nil {
		return err
	}

	// Build response data
	totalMatches := 0
	for _, b := range blocks {
		totalMatches += b.MatchCount
	}

	data := map[string]any{
		"pattern":          in.Pattern,
		"case_insensitive": in.CaseInsensitive,
		"match_count":      totalMatches,
		"block_count":      len(blocks),
		"files_touched":    len(fileHits),
		"preview":          preview,
		"top_files":        summarizeTopFiles(fileHits, 5),
		"max_blocks":       in.MaxBlocks,
	}
	if artifact.Digest != "" {
		data["artifact"] = artifact.Digest
		data["artifact_kind"] = artifact.Kind
		data["artifact_size_bytes"] = artifact.Size
	}

	return rc.Emit("code/context_ripgrep", data, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
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
	if in.MaxBlocks <= 0 {
		in.MaxBlocks = 50
	}
	if in.MaxBlockLines <= 0 {
		in.MaxBlockLines = 400
	}
	return in, nil
}

func resolveWorkspace(rc *runner.RunnerContext, candidate string) (string, string, error) {
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
		"--json",
		"--no-heading",
		"--line-number",
		"--no-messages",
		"--max-count", fmt.Sprintf("%d", in.MaxMatches),
	}

	if in.CaseInsensitive {
		args = append(args, "--ignore-case")
	}

	if in.Hidden {
		args = append(args, "--hidden")
	}

	for _, glob := range in.Glob {
		args = append(args, "--glob", glob)
	}

	for _, glob := range in.GlobNot {
		args = append(args, "--glob", "!"+glob)
	}

	if len(in.GlobNot) == 0 {
		args = append(args,
			"--glob", "!.git",
			"--glob", "!node_modules",
			"--glob", "!vendor",
			"--glob", "!__pycache__",
			"--glob", "!.godot",
		)
	}

	args = append(args, "--", in.Pattern, searchPath)
	return args
}

func parseRipgrepOutput(output []byte, workspace string, maxMatches int) ([]rawMatch, error) {
	var matches []rawMatch

	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() && len(matches) < maxMatches {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg rgMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}

		if msg.Type == "match" {
			var matchData rgMatchData
			if err := json.Unmarshal(msg.Data, &matchData); err != nil {
				continue
			}

			lineText := strings.TrimSuffix(matchData.Lines.Text, "\n")
			relPath := relativeTo(workspace, matchData.Path.Text)

			matches = append(matches, rawMatch{
				File: relPath,
				Line: matchData.LineNumber,
				Text: lineText,
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan ripgrep output: %w", err)
	}

	return matches, nil
}

func groupByFile(matches []rawMatch) map[string][]rawMatch {
	result := make(map[string][]rawMatch)
	for _, m := range matches {
		result[m.File] = append(result[m.File], m)
	}
	return result
}

func emitEmptyResult(rc *runner.RunnerContext, in input) error {
	data := map[string]any{
		"pattern":          in.Pattern,
		"case_insensitive": in.CaseInsensitive,
		"match_count":      0,
		"block_count":      0,
		"files_touched":    0,
		"preview":          []BlockPreview{},
		"top_files":        [][2]any{},
		"max_blocks":       in.MaxBlocks,
	}
	return rc.Emit("code/context_ripgrep", data, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func prepareBlockPreview(blocks []Block, limit int) []BlockPreview {
	if limit <= 0 {
		limit = 10
	}
	count := len(blocks)
	if count > limit {
		count = limit
	}

	previews := make([]BlockPreview, count)
	for i := 0; i < count; i++ {
		b := blocks[i]
		previews[i] = BlockPreview{
			File:       b.File,
			Language:   b.Language,
			StartLine:  b.StartLine,
			EndLine:    b.EndLine,
			HeaderLine: b.HeaderLine,
			SymbolName: b.SymbolName,
			SymbolKind: b.SymbolKind,
			MatchCount: b.MatchCount,
		}
	}
	return previews
}

func persistBlocksArtifact(ctx context.Context, rc *runner.RunnerContext, blocks []Block) (runner.Artifact, error) {
	// Always persist full blocks to CAS if we have any
	if len(blocks) == 0 {
		return runner.Artifact{}, nil
	}

	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	for _, b := range blocks {
		if err := enc.Encode(b); err != nil {
			return runner.Artifact{}, fmt.Errorf("encode block: %w", err)
		}
	}
	return runner.PersistBuffer(ctx, rc, buf, "application/x-ndjson", "code_context_ripgrep")
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
	if len(counts) == 0 {
		return [][2]any{}
	}
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

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit code/context_ripgrep failure")
	os.Exit(1)
}
