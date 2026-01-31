// Package main implements the code/context_grep skill.
// It provides three modes for code search with context expansion:
//   - Ripgrep mode: regex pattern search with function boundary expansion
//   - AST-grep mode: structural pattern matching using ast-grep
//   - Line expansion mode: expand specific line ranges to function boundaries
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/codeblocks"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/pathutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/rgutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/tools/ripgrep"
)

// Mode determines which search engine to use for code context search with different capabilities.
type Mode string

const (
	ModeRipgrep Mode = "ripgrep" // Default: regex search
	ModeASTGrep Mode = "ast"     // Structural pattern matching
	ModeLine    Mode = "line"    // Line range expansion
)

// input defines the skill input parameters for code context search with multiple modes and comprehensive options.
type input struct {
	// Common options
	Path          string `json:"path"`
	MaxBlocks     int    `json:"max_blocks"`
	MaxBlockLines int    `json:"max_block_lines"`

	// Mode selection (auto-detected if not specified)
	Mode Mode `json:"mode,omitempty"`

	// Ripgrep mode options
	Pattern         string   `json:"pattern"`
	CaseInsensitive bool     `json:"case_insensitive"`
	Glob            []string `json:"glob"`
	GlobNot         []string `json:"glob_not"`
	MaxMatches      int      `json:"max_matches"`
	Hidden          bool     `json:"hidden"`

	// AST-grep mode options
	ASTPattern string `json:"ast_pattern,omitempty"`
	Language   string `json:"language,omitempty"`
	ASTRule    string `json:"ast_rule,omitempty"` // YAML rule content

	// Line expansion mode options
	FilePath  string `json:"file_path,omitempty"`
	LineStart int    `json:"line_start,omitempty"`
	LineEnd   int    `json:"line_end,omitempty"`
	ExpandTo  string `json:"expand_to,omitempty"` // "function", "block", "class"
}

type (
	Block        = codeblocks.Block
	BlockPreview = codeblocks.BlockPreview
	rawMatch     = codeblocks.RawMatch
)

// main is the skill entry point for code/context_grep with multi-mode search capabilities.
func main() {
	skillmain.Main("code/context_grep", run)
}

// run orchestrates code context search with ripgrep, ast-grep, and line expansion modes.
//
// Index:
// - Purpose: Search code with context expansion using multiple search engines (ripgrep, ast-grep, line expansion)
// - Flow: apply defaults → detect mode → route to handler → execute search → expand matches → emit results
// - SideEffects: executes external tools (ripgrep, ast-grep); reads file contents; expands code blocks
// - FailureModes: missing tools, invalid patterns, file access errors, parsing failures
// - Observability: emits match counts, block previews, file statistics, and search mode used
// - Related: detectMode, runRipgrep, runASTGrep, runLineExpansion, codeblocks.ExpandMatches
// - Keywords: code/context_grep, code_search, context_expansion, ripgrep, ast_grep, line_expansion
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Apply defaults
	if in.MaxMatches <= 0 {
		in.MaxMatches = 10000
	}
	if in.MaxBlocks <= 0 {
		in.MaxBlocks = 50
	}
	if in.MaxBlockLines <= 0 {
		in.MaxBlockLines = 400
	}

	// Auto-detect mode if not specified
	mode := detectMode(in)

	switch mode {
	case ModeASTGrep:
		return runASTGrep(ctx, rc, in)
	case ModeLine:
		return runLineExpansion(ctx, rc, in)
	default:
		return runRipgrep(ctx, rc, in)
	}
}

// detectMode determines which mode to use based on input parameters, patterns, and available options.
func detectMode(in input) Mode {
	if in.Mode != "" {
		return in.Mode
	}
	if in.ASTPattern != "" || in.ASTRule != "" {
		return ModeASTGrep
	}
	if in.FilePath != "" && (in.LineStart > 0 || in.LineEnd > 0) {
		return ModeLine
	}
	return ModeRipgrep
}

// runRipgrep executes ripgrep search mode with regex pattern matching, file filtering, and context expansion.
func runRipgrep(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Check if ripgrep is available
	if err := rgutil.RequireRipgrep(); err != nil {
		return err
	}

	// Resolve workspace and path
	workspace, searchPath, err := skillmain.ResolvePath(rc, in.Path)
	if err != nil {
		return err
	}

	searchInput := rgutil.Normalize(rgutil.SearchInput{
		Pattern:         in.Pattern,
		CaseInsensitive: in.CaseInsensitive,
		Glob:            in.Glob,
		GlobNot:         in.GlobNot,
		MaxMatches:      in.MaxMatches,
		Hidden:          in.Hidden,
	})
	in.MaxMatches = searchInput.MaxMatches

	result, err := ripgrep.SearchJSON(ctx, rgutil.BuildSearchOpts(searchInput, workspace, searchPath, nil))
	if err != nil {
		return skillerr.WrapRuntime("ripgrep execution failed", err)
	}

	if len(result.Matches) == 0 {
		return emitEmptyResult(rc, in, ModeRipgrep)
	}

	rawMatches := make([]rawMatch, 0, len(result.Matches))
	for _, match := range result.Matches {
		rawMatches = append(rawMatches, rawMatch{
			File: match.Path,
			Line: match.Line,
			Text: match.Text,
		})
	}

	blocks := codeblocks.ExpandMatches(workspace, rawMatches, in.MaxBlocks, in.MaxBlockLines)
	return emitResult(ctx, rc, in, blocks, ModeRipgrep)
}

// runASTGrep executes ast-grep search mode with structural pattern matching, rule support, and context expansion.
func runASTGrep(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Check if ast-grep is available
	sgPath, err := executil.RequireAny([]string{"sg", "ast-grep"}, "install ast-grep")
	if err != nil {
		return skillerr.Runtime(
			"ast-grep (sg) not found in PATH",
			skillerr.WithCause(err),
			skillerr.WithHint("Install ast-grep and ensure `sg` or `ast-grep` is available in PATH."),
		)
	}

	workspace, searchPath, err := skillmain.ResolvePath(rc, in.Path)
	if err != nil {
		return err
	}

	// Build ast-grep command
	var args []string
	if in.ASTRule != "" {
		// Use rule mode with inline YAML
		args = []string{"scan", "--json", "--inline-rules", in.ASTRule}
	} else {
		// Use pattern mode
		args = []string{"run", "--json", "--pattern", in.ASTPattern}
		if in.Language != "" {
			args = append(args, "--lang", in.Language)
		}
	}
	args = append(args, searchPath)

	result := executil.Run(ctx, workspace, sgPath, args...)
	if result.Err != nil {
		// Exit code 1 may mean no matches
		if result.ExitCode == 1 {
			return emitEmptyResult(rc, in, ModeASTGrep)
		}
		return skillerr.Runtimef("ast-grep execution failed: %v\nstderr: %s", result.Err, string(result.Stderr))
	}

	// Parse ast-grep JSON output
	rawMatches, err := parseASTGrepOutput(result.Stdout, workspace, in.MaxMatches)
	if err != nil {
		return err
	}

	if len(rawMatches) == 0 {
		return emitEmptyResult(rc, in, ModeASTGrep)
	}

	blocks := codeblocks.ExpandMatches(workspace, rawMatches, in.MaxBlocks, in.MaxBlockLines)
	return emitResult(ctx, rc, in, blocks, ModeASTGrep)
}

// runLineExpansion expands specific line ranges to function boundaries with language detection and block merging.
func runLineExpansion(ctx context.Context, rc *skillmain.RunContext, in input) error {
	workspace := rc.PathValidator.Workspace()

	// Resolve file path
	filePath := in.FilePath
	if in.Path != "" && filePath == "" {
		filePath = in.Path
	}

	resolved, err := skillmain.ValidatePath(rc, filePath, skillmain.WithPathMessage("invalid file path"))
	if err != nil {
		return err
	}

	// Read file content
	content, err := os.ReadFile(resolved)
	if err != nil {
		return skillerr.WrapIO("read file", err)
	}

	lines := strings.Split(string(content), "\n")
	relPath := pathutil.RelTo(workspace, resolved)
	lang := codeblocks.DetectLanguage(relPath)

	// Apply defaults for line range
	lineStart := in.LineStart
	lineEnd := in.LineEnd
	if lineStart <= 0 {
		lineStart = 1
	}
	if lineEnd <= 0 || lineEnd > len(lines) {
		lineEnd = len(lines)
	}

	// Create matches for the line range
	var rawMatches []rawMatch
	for line := lineStart; line <= lineEnd; line++ {
		rawMatches = append(rawMatches, rawMatch{
			File: relPath,
			Line: line,
			Text: "",
		})
	}

	// Expand to function/block boundaries
	expander := codeblocks.NewExpander(lang, in.MaxBlockLines)
	blocks := expander.ExpandMatches(relPath, lines, rawMatches)

	// Deduplicate and merge blocks
	if len(blocks) > 1 {
		blocks = mergeOverlappingBlocks(blocks)
	}

	return emitResult(ctx, rc, in, blocks, ModeLine)
}

// parseASTGrepOutput parses ast-grep JSON output with NDJSON format, workspace path resolution, and error handling.
func parseASTGrepOutput(output []byte, workspace string, maxMatches int) ([]rawMatch, error) {
	var matches []rawMatch

	// ast-grep outputs NDJSON
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() && len(matches) < maxMatches {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var match struct {
			File  string `json:"file"`
			Range struct {
				Start struct {
					Line int `json:"line"`
				} `json:"start"`
			} `json:"range"`
			Text string `json:"text"`
		}

		if err := json.Unmarshal(line, &match); err != nil {
			continue
		}

		relPath := pathutil.RelTo(workspace, match.File)
		matches = append(matches, rawMatch{
			File: relPath,
			Line: match.Range.Start.Line + 1, // ast-grep uses 0-indexed
			Text: match.Text,
		})
	}

	return matches, scanner.Err()
}

// mergeOverlappingBlocks merges blocks that overlap or are adjacent with match aggregation and source preservation.
// NOTE: When blocks are merged, the Source field retains only the first block's content.
// Callers should use StartLine/EndLine to re-read source if complete content is needed.
func mergeOverlappingBlocks(blocks []Block) []Block {
	if len(blocks) <= 1 {
		return blocks
	}

	// Sort by start line
	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].StartLine < blocks[j].StartLine
	})

	var merged []Block
	current := blocks[0]

	for i := 1; i < len(blocks); i++ {
		b := blocks[i]
		if b.StartLine <= current.EndLine+1 {
			// Overlapping or adjacent - merge
			if b.EndLine > current.EndLine {
				current.EndLine = b.EndLine
			}
			current.MatchLines = append(current.MatchLines, b.MatchLines...)
			current.MatchCount += b.MatchCount
		} else {
			merged = append(merged, current)
			current = b
		}
	}
	merged = append(merged, current)

	return merged
}

// emitEmptyResult emits empty search results with mode, pattern information, and structured response format.
func emitEmptyResult(rc *skillmain.RunContext, in input, mode Mode) error {
	data := map[string]any{
		"mode":          string(mode),
		"pattern":       in.Pattern,
		"ast_pattern":   in.ASTPattern,
		"match_count":   0,
		"block_count":   0,
		"files_touched": 0,
		"preview":       []BlockPreview{},
		"blocks":        []Block{},
	}
	return skillout.Emit(rc, "code/context_grep", data)
}

// emitResult emits search results with preview generation, artifact storage, file statistics, and comprehensive metrics.
func emitResult(ctx context.Context, rc *skillmain.RunContext, in input, blocks []Block, mode Mode) error {
	// Prepare preview and persist full blocks when truncated.
	previewResult, err := skillout.PreviewAndPersistNDJSON(ctx, rc, blocks, rc.MaxPreview, "code_context_grep", true)
	if err != nil {
		return err
	}
	preview := codeblocks.PrepareBlockPreview(previewResult.Preview)
	artifact := previewResult.Artifact

	// Calculate totals
	totalMatches := 0
	fileHits := make(map[string]int)
	for _, b := range blocks {
		totalMatches += b.MatchCount
		fileHits[b.File] += b.MatchCount
	}

	data := map[string]any{
		"mode":          string(mode),
		"pattern":       in.Pattern,
		"ast_pattern":   in.ASTPattern,
		"match_count":   totalMatches,
		"block_count":   len(blocks),
		"files_touched": len(fileHits),
		"preview":       preview,
		"blocks":        blocks,
		"top_files":     skillout.SummarizeTopFiles(fileHits, 5),
	}
	skillout.AddArtifact(data, artifact)

	return skillout.Emit(rc, "code/context_grep", data)
}
