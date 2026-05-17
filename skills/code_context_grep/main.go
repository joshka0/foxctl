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
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/codeblocks"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/pathutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/rgutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/tooling/tools/ripgrep"
)

// Mode determines which search engine to use for code context search with different capabilities.
type Mode string

const (
	ModeRipgrep Mode = "ripgrep" // Default: regex search
	ModeASTGrep Mode = "ast"     // Structural pattern matching
	ModeLine    Mode = "line"    // Line range expansion
)

type InlineMode string

const (
	InlineModeAuto         InlineMode = "auto"
	InlineModeFull         InlineMode = "full"
	InlineModePreview      InlineMode = "preview"
	InlineModeArtifactOnly InlineMode = "artifact_only"
)

const maxASTGrepLineBytes = 4 * 1024 * 1024

// input defines the skill input parameters for code context search with multiple modes and comprehensive options.
type input struct {
	// Common options
	Path             string `json:"path"`
	MaxBlocks        int    `json:"max_blocks"`
	MaxBlockLines    int    `json:"max_block_lines"`
	MaxBlocksPerFile int    `json:"max_blocks_per_file"`
	MaxBytesPerFile  int    `json:"max_bytes_per_file"`
	InlineMode       string `json:"inline_mode,omitempty"`

	// Mode selection (auto-detected if not specified)
	Mode Mode `json:"mode,omitempty"`

	// Ripgrep mode options
	Pattern         string   `json:"pattern"`
	PatternMode     string   `json:"pattern_mode,omitempty"` // "regex" or "literal"
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

	// Definition expansion
	IncludeDefinitions  bool `json:"include_definitions,omitempty"`
	MaxDefinitionBlocks int  `json:"max_definition_blocks,omitempty"`
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
//
//	Purpose: Search code with context expansion using multiple search engines (ripgrep, ast-grep, line expansion)
//	Flow: apply defaults → detect mode → route to handler → execute search → expand matches → emit results
//	SideEffects: executes external tools (ripgrep, ast-grep); reads file contents; expands code blocks
//	FailureModes: missing tools, invalid patterns, file access errors, parsing failures
//	Observability: emits match counts, block previews, file statistics, and search mode used
//	Related: detectMode, runRipgrep, runASTGrep, runLineExpansion, codeblocks.ExpandMatches
//	Keywords: code/context_grep, code_search, context_expansion, ripgrep, ast_grep, line_expansion
//
// [[domain:code-context-search]]
// [[protocol:multi-mode-code-search]]
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
	if in.MaxBlocksPerFile <= 0 {
		in.MaxBlocksPerFile = 5
	}
	if in.MaxBlocksPerFile > in.MaxBlocks {
		in.MaxBlocksPerFile = in.MaxBlocks
	}
	if in.MaxBytesPerFile <= 0 {
		in.MaxBytesPerFile = 1_000_000
	}
	if in.MaxDefinitionBlocks <= 0 {
		in.MaxDefinitionBlocks = 5
	}
	if in.MaxDefinitionBlocks > in.MaxBlocks {
		in.MaxDefinitionBlocks = in.MaxBlocks
	}

	// Auto-detect mode if not specified
	mode := detectMode(in)
	if err := validateForMode(in, mode); err != nil {
		return err
	}

	target, err := parseExpandTarget(in.ExpandTo)
	if err != nil {
		return err
	}

	switch mode {
	case ModeASTGrep:
		return runASTGrep(ctx, rc, in, target)
	case ModeLine:
		return runLineExpansion(ctx, rc, in, target)
	default:
		return runRipgrep(ctx, rc, in, target)
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
	if (in.FilePath != "" || in.Path != "") && (in.LineStart > 0 || in.LineEnd > 0) {
		return ModeLine
	}
	return ModeRipgrep
}

func validateForMode(in input, mode Mode) error {
	switch mode {
	case ModeRipgrep:
		if strings.TrimSpace(in.Pattern) == "" {
			return skillerr.Arg(
				"pattern is required for ripgrep mode",
				skillerr.WithHint("Set 'pattern', or use 'ast_pattern'/'ast_rule' for ast mode, or 'file_path' + 'line_start/line_end' for line mode."),
			)
		}
	case ModeASTGrep:
		if strings.TrimSpace(in.ASTPattern) == "" && strings.TrimSpace(in.ASTRule) == "" {
			return skillerr.Arg(
				"ast_pattern or ast_rule is required for ast mode",
				skillerr.WithHint("Provide 'ast_pattern' for simple structural matches or 'ast_rule' for YAML rule mode."),
			)
		}
	case ModeLine:
		filePath := strings.TrimSpace(in.FilePath)
		if filePath == "" {
			filePath = strings.TrimSpace(in.Path)
		}
		if filePath == "" {
			return skillerr.Arg(
				"file_path is required for line mode",
				skillerr.WithHint("Provide 'file_path' and at least one of 'line_start' or 'line_end'."),
			)
		}
		if in.LineStart <= 0 && in.LineEnd <= 0 {
			return skillerr.Arg(
				"line_start or line_end is required for line mode",
				skillerr.WithHint("Example: {\"file_path\":\"main.go\",\"line_start\":10,\"line_end\":20}"),
			)
		}
	}
	return nil
}

func parseExpandTarget(value string) (codeblocks.Target, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "any":
		return codeblocks.TargetAny, nil
	case "block":
		return codeblocks.TargetBlock, nil
	case "function":
		return codeblocks.TargetFunction, nil
	case "class":
		return codeblocks.TargetClass, nil
	default:
		return codeblocks.TargetAny, skillerr.Arg(
			"expand_to must be one of: function, block, class",
			skillerr.WithHint("Set 'expand_to' to 'function', 'class', or 'block' to control expansion scope."),
		)
	}
}

func parsePatternMode(value string) (string, bool, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", "regex":
		return "regex", false, nil
	case "literal", "fixed":
		return "literal", true, nil
	default:
		return "regex", false, skillerr.Arg(
			"pattern_mode must be one of: regex, literal",
			skillerr.WithHint("Set pattern_mode to literal to treat the pattern as a literal string."),
		)
	}
}

func parseInlineMode(value string) (InlineMode, error) {
	switch InlineMode(strings.ToLower(strings.TrimSpace(value))) {
	case "", InlineModeAuto:
		return InlineModeAuto, nil
	case InlineModeFull:
		return InlineModeFull, nil
	case InlineModePreview:
		return InlineModePreview, nil
	case InlineModeArtifactOnly:
		return InlineModeArtifactOnly, nil
	default:
		return InlineModeAuto, skillerr.Arg(
			"inline_mode must be one of: auto, full, preview, artifact_only",
			skillerr.WithHint("Use inline_mode:\"preview\" to force compact inline blocks, or artifact_only to rely on the CAS artifact."),
		)
	}
}

func hintForRipgrepError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "regex") || strings.Contains(msg, "parse error") || strings.Contains(msg, "invalid escape") || strings.Contains(msg, "unrecognized escape") {
		return "If passing JSON, escape backslashes (\"\\bfoo\\(\") or set pattern_mode:\"literal\" to treat the pattern as a literal string."
	}
	return ""
}

// runRipgrep executes ripgrep search mode with regex pattern matching, file filtering, and context expansion.
func runRipgrep(ctx context.Context, rc *skillmain.RunContext, in input, target codeblocks.Target) error {
	// Check if ripgrep is available
	if err := rgutil.RequireRipgrep(); err != nil {
		return err
	}

	// Resolve workspace and path
	workspace, searchPath, err := skillmain.ResolvePath(rc, in.Path)
	if err != nil {
		return err
	}

	patternMode, fixedStrings, err := parsePatternMode(in.PatternMode)
	if err != nil {
		return err
	}
	in.PatternMode = patternMode

	searchInput := rgutil.Normalize(rgutil.SearchInput{
		Pattern:         in.Pattern,
		CaseInsensitive: in.CaseInsensitive,
		FixedStrings:    fixedStrings,
		Glob:            in.Glob,
		GlobNot:         in.GlobNot,
		MaxMatches:      in.MaxMatches,
		Hidden:          in.Hidden,
	})
	in.MaxMatches = searchInput.MaxMatches

	result, err := ripgrep.SearchJSON(ctx, rgutil.BuildSearchOpts(searchInput, workspace, searchPath, nil))
	if err != nil {
		hint := hintForRipgrepError(err)
		if hint != "" {
			return skillerr.WrapRuntime("ripgrep execution failed", err, skillerr.WithHint(hint))
		}
		return skillerr.WrapRuntime("ripgrep execution failed", err)
	}

	if len(result.Matches) == 0 {
		return emitEmptyResult(rc, in, ModeRipgrep)
	}

	rawMatches := make([]rawMatch, 0, len(result.Matches))
	for _, match := range result.Matches {
		rawMatches = append(rawMatches, rawMatch{
			File:    match.Path,
			Line:    match.Line,
			EndLine: match.Line,
			Text:    match.Text,
		})
	}

	blocks := codeblocks.ExpandMatchesWithOptions(workspace, rawMatches, codeblocks.ExpandOptions{
		MaxBlocks:        in.MaxBlocks,
		MaxBlockLines:    in.MaxBlockLines,
		MaxBlocksPerFile: in.MaxBlocksPerFile,
		MaxBytesPerFile:  int64(in.MaxBytesPerFile),
		Target:           target,
	})
	blocks, err = addDefinitionBlocks(ctx, in, workspace, searchPath, blocks, rawMatches)
	if err != nil {
		return err
	}
	blocks = sortBlocksByUsefulness(blocks)
	blocks = limitBlocks(blocks, in.MaxBlocks)
	return emitResult(ctx, rc, in, blocks, ModeRipgrep)
}

// runASTGrep executes ast-grep search mode with structural pattern matching, rule support, and context expansion.
func runASTGrep(ctx context.Context, rc *skillmain.RunContext, in input, target codeblocks.Target) error {
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

	blocks := codeblocks.ExpandMatchesWithOptions(workspace, rawMatches, codeblocks.ExpandOptions{
		MaxBlocks:        in.MaxBlocks,
		MaxBlockLines:    in.MaxBlockLines,
		MaxBlocksPerFile: in.MaxBlocksPerFile,
		MaxBytesPerFile:  int64(in.MaxBytesPerFile),
		Target:           target,
	})
	blocks, err = addDefinitionBlocks(ctx, in, workspace, searchPath, blocks, rawMatches)
	if err != nil {
		return err
	}
	blocks = sortBlocksByUsefulness(blocks)
	blocks = limitBlocks(blocks, in.MaxBlocks)
	return emitResult(ctx, rc, in, blocks, ModeASTGrep)
}

// runLineExpansion expands specific line ranges to function boundaries with language detection and block merging.
func runLineExpansion(ctx context.Context, rc *skillmain.RunContext, in input, target codeblocks.Target) error {
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

	if in.MaxBytesPerFile > 0 {
		info, err := os.Stat(resolved)
		if err != nil {
			return skillerr.WrapIO("stat file", err)
		}
		if info.Size() > int64(in.MaxBytesPerFile) {
			return skillerr.Arg(
				"file exceeds max_bytes_per_file",
				skillerr.WithHint("Increase 'max_bytes_per_file' if you need to expand a larger file."),
			)
		}
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
	expander := codeblocks.NewExpander(lang, in.MaxBlockLines, codeblocks.WithTarget(target))
	blocks := expander.ExpandMatches(relPath, lines, rawMatches)

	// Deduplicate and merge blocks
	if len(blocks) > 1 {
		blocks = mergeOverlappingBlocks(blocks, lines)
	}

	blocks = sortBlocksByUsefulness(blocks)
	maxBlocks := in.MaxBlocks
	if in.MaxBlocksPerFile > 0 && in.MaxBlocksPerFile < maxBlocks {
		maxBlocks = in.MaxBlocksPerFile
	}
	blocks = limitBlocks(blocks, maxBlocks)

	return emitResult(ctx, rc, in, blocks, ModeLine)
}

// parseASTGrepOutput parses ast-grep JSON output with NDJSON format, workspace path resolution, and error handling.
func parseASTGrepOutput(output []byte, workspace string, maxMatches int) ([]rawMatch, error) {
	var matches []rawMatch

	// ast-grep outputs NDJSON
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), maxASTGrepLineBytes)
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
				End struct {
					Line int `json:"line"`
				} `json:"end"`
			} `json:"range"`
			Text string `json:"text"`
		}

		if err := json.Unmarshal(line, &match); err != nil {
			continue
		}

		relPath := pathutil.RelTo(workspace, match.File)
		startLine := match.Range.Start.Line + 1 // ast-grep uses 0-indexed
		endLine := startLine
		if match.Range.End.Line >= match.Range.Start.Line {
			endLine = match.Range.End.Line + 1
		}
		matches = append(matches, rawMatch{
			File:    relPath,
			Line:    startLine,
			EndLine: endLine,
			Text:    match.Text,
		})
	}

	return matches, scanner.Err()
}

// mergeOverlappingBlocks merges adjacent blocks that resolve to the same symbol.
func mergeOverlappingBlocks(blocks []Block, lines []string) []Block {
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
		sameSymbol := current.SymbolName == b.SymbolName &&
			current.SymbolKind == b.SymbolKind &&
			current.HeaderLine == b.HeaderLine
		if sameSymbol && b.StartLine <= current.EndLine+1 {
			// Overlapping or adjacent - merge
			if b.EndLine > current.EndLine {
				current.EndLine = b.EndLine
			}
			current.MatchLines = append(current.MatchLines, b.MatchLines...)
			continue
		}
		current = finalizeMergedBlock(current, lines)
		merged = append(merged, current)
		current = b
	}
	current = finalizeMergedBlock(current, lines)
	merged = append(merged, current)

	return merged
}

// emitEmptyResult emits empty search results with mode, pattern information, and structured response format.
func emitEmptyResult(rc *skillmain.RunContext, in input, mode Mode) error {
	data := map[string]any{
		"mode":                string(mode),
		"inline_mode":         string(InlineModeFull),
		"pattern":             in.Pattern,
		"pattern_mode":        in.PatternMode,
		"ast_pattern":         in.ASTPattern,
		"match_count":         0,
		"block_count":         0,
		"blocks_shown":        0,
		"blocks_truncated":    false,
		"files_touched":       0,
		"preview":             []BlockPreview{},
		"blocks":              []Block{},
		"rendered_context":    "",
		"render_format":       "markdown",
		"top_symbols":         []map[string]any{},
		"max_blocks_per_file": in.MaxBlocksPerFile,
		"max_bytes_per_file":  in.MaxBytesPerFile,
		"expand_to":           in.ExpandTo,
		"include_definitions": in.IncludeDefinitions,
	}
	return skillout.Emit(rc, "code/context_grep", data)
}

func estimateJSONSize(v any) int {
	payload, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(payload)
}

func ensureBlocksArtifact(ctx context.Context, rc *skillmain.RunContext, blocks []Block, artifact *skillmain.Artifact) (*skillmain.Artifact, error) {
	if artifact != nil || rc == nil || rc.CASStore == nil || len(blocks) == 0 {
		return artifact, nil
	}
	if !rc.ShouldTruncate(estimateJSONSize(blocks)) {
		return artifact, nil
	}
	persisted, err := skillmain.PersistJSON(ctx, rc, blocks, "code_context_grep_blocks")
	if err != nil {
		return artifact, skillerr.WrapRuntime("persist context_grep blocks to CAS", err)
	}
	return &persisted, nil
}

func resolveInlineSelection(requested string, totalBlocks, previewBlocks int, hasArtifact, fullTooLarge bool) (InlineMode, bool, bool, error) {
	inlineMode, err := parseInlineMode(requested)
	if err != nil {
		return InlineModeAuto, false, false, err
	}

	switch inlineMode {
	case InlineModeFull:
		return InlineModeFull, false, false, nil
	case InlineModePreview:
		return InlineModePreview, true, previewBlocks < totalBlocks, nil
	case InlineModeArtifactOnly:
		return InlineModeArtifactOnly, false, totalBlocks > 0, nil
	case InlineModeAuto:
		if hasArtifact && previewBlocks > 0 && previewBlocks < totalBlocks {
			return InlineModePreview, true, true, nil
		}
		if hasArtifact && fullTooLarge && previewBlocks == 0 && totalBlocks > 0 {
			return InlineModeArtifactOnly, false, true, nil
		}
		return InlineModeFull, false, false, nil
	default:
		return InlineModeAuto, false, false, nil
	}
}

func finalizeMergedBlock(block Block, lines []string) Block {
	block.MatchLines = uniqueSortedInts(block.MatchLines)
	block.MatchCount = len(block.MatchLines)
	if len(lines) == 0 {
		return block
	}
	startIdx := block.StartLine - 1
	endIdx := block.EndLine - 1
	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx >= len(lines) {
		endIdx = len(lines) - 1
	}
	if startIdx <= endIdx {
		block.Source = strings.Join(lines[startIdx:endIdx+1], "\n")
	}
	return block
}

func sortBlocksByUsefulness(blocks []Block) []Block {
	sort.SliceStable(blocks, func(i, j int) bool {
		if blocks[i].MatchCount != blocks[j].MatchCount {
			return blocks[i].MatchCount > blocks[j].MatchCount
		}
		lenI := blocks[i].EndLine - blocks[i].StartLine
		lenJ := blocks[j].EndLine - blocks[j].StartLine
		if lenI != lenJ {
			return lenI < lenJ
		}
		if blocks[i].File != blocks[j].File {
			return blocks[i].File < blocks[j].File
		}
		return blocks[i].StartLine < blocks[j].StartLine
	})
	return blocks
}

func limitBlocks(blocks []Block, maxBlocks int) []Block {
	if maxBlocks <= 0 || len(blocks) <= maxBlocks {
		return blocks
	}
	return blocks[:maxBlocks]
}

func renderContextMarkdown(blocks []Block) string {
	if len(blocks) == 0 {
		return ""
	}

	var builder strings.Builder
	for i, block := range blocks {
		if i > 0 {
			builder.WriteString("\n\n")
		}

		header := block.HeaderLine
		if header == "" {
			header = block.SymbolName
		}

		builder.WriteString(fmt.Sprintf("[%d] %s:%d-%d", i+1, block.File, block.StartLine, block.EndLine))
		if header != "" {
			builder.WriteString(" — ")
			builder.WriteString(header)
		}
		if block.MatchCount > 0 {
			builder.WriteString("\n(matches: ")
			builder.WriteString(strconv.Itoa(block.MatchCount))
			builder.WriteString(")\n\n")
		} else {
			builder.WriteString("\n\n")
		}

		if block.Language != "" {
			builder.WriteString("```")
			builder.WriteString(block.Language)
			builder.WriteString("\n")
		} else {
			builder.WriteString("```\n")
		}
		builder.WriteString(block.Source)
		if !strings.HasSuffix(block.Source, "\n") {
			builder.WriteString("\n")
		}
		builder.WriteString("```")
	}

	return builder.String()
}

func summarizeTopSymbols(blocks []Block, limit int) []map[string]any {
	type key struct {
		file   string
		symbol string
	}
	counts := make(map[key]int)
	for _, block := range blocks {
		if block.SymbolName == "" {
			continue
		}
		symbol := block.SymbolName
		if block.SymbolKind != "" {
			symbol = block.SymbolKind + " " + block.SymbolName
		}
		counts[key{file: block.File, symbol: symbol}] += block.MatchCount
	}

	type entry struct {
		file   string
		symbol string
		count  int
	}
	entries := make([]entry, 0, len(counts))
	for k, v := range counts {
		entries = append(entries, entry{file: k.file, symbol: k.symbol, count: v})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		if entries[i].symbol != entries[j].symbol {
			return entries[i].symbol < entries[j].symbol
		}
		return entries[i].file < entries[j].file
	})
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}

	summary := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		summary = append(summary, map[string]any{
			"symbol":      entry.symbol,
			"file":        entry.file,
			"match_count": entry.count,
		})
	}
	return summary
}

func addDefinitionBlocks(ctx context.Context, in input, workspace, searchPath string, blocks []Block, rawMatches []rawMatch) ([]Block, error) {
	if !in.IncludeDefinitions || in.MaxDefinitionBlocks <= 0 {
		return blocks, nil
	}
	if !hasGoFile(rawMatches) {
		return blocks, nil
	}
	if err := rgutil.RequireRipgrep(); err != nil {
		return blocks, err
	}

	names := extractCallNames(rawMatches, in.MaxDefinitionBlocks*3)
	if len(names) == 0 {
		return blocks, nil
	}

	defMatches, err := findGoDefinitions(ctx, workspace, searchPath, names, in.MaxDefinitionBlocks)
	if err != nil {
		return blocks, err
	}
	if len(defMatches) == 0 {
		return blocks, nil
	}

	defBlocks := codeblocks.ExpandMatchesWithOptions(workspace, defMatches, codeblocks.ExpandOptions{
		MaxBlocks:        in.MaxDefinitionBlocks,
		MaxBlockLines:    in.MaxBlockLines,
		MaxBlocksPerFile: 1,
		MaxBytesPerFile:  int64(in.MaxBytesPerFile),
		Target:           codeblocks.TargetFunction,
	})
	if len(defBlocks) == 0 {
		return blocks, nil
	}

	seen := make(map[string]bool)
	for _, block := range blocks {
		seen[blockKey(block)] = true
	}
	for _, block := range defBlocks {
		key := blockKey(block)
		if seen[key] {
			continue
		}
		blocks = append(blocks, block)
		seen[key] = true
	}

	return blocks, nil
}

var callPattern = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*\(`)

var callKeywords = map[string]struct{}{
	"if":      {},
	"for":     {},
	"switch":  {},
	"return":  {},
	"func":    {},
	"type":    {},
	"var":     {},
	"go":      {},
	"defer":   {},
	"range":   {},
	"select":  {},
	"case":    {},
	"map":     {},
	"make":    {},
	"new":     {},
	"len":     {},
	"cap":     {},
	"append":  {},
	"copy":    {},
	"delete":  {},
	"panic":   {},
	"recover": {},
	"close":   {},
}

func extractCallNames(rawMatches []rawMatch, maxNames int) []string {
	if maxNames <= 0 {
		return nil
	}
	names := make([]string, 0, maxNames)
	seen := make(map[string]bool)
	for _, match := range rawMatches {
		if match.Text == "" {
			continue
		}
		for _, sub := range callPattern.FindAllStringSubmatch(match.Text, -1) {
			name := sub[1]
			if _, skip := callKeywords[name]; skip {
				continue
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
			if len(names) >= maxNames {
				return names
			}
		}
	}
	return names
}

func hasGoFile(rawMatches []rawMatch) bool {
	for _, match := range rawMatches {
		if strings.HasSuffix(match.File, ".go") {
			return true
		}
	}
	return false
}

func findGoDefinitions(ctx context.Context, workspace, searchPath string, names []string, maxMatches int) ([]rawMatch, error) {
	if maxMatches <= 0 {
		return nil, nil
	}
	if searchPath == "" {
		searchPath = "."
	}

	defMatches := make([]rawMatch, 0, maxMatches)
	seen := make(map[string]bool)
	for _, name := range names {
		if len(defMatches) >= maxMatches {
			break
		}

		pattern := fmt.Sprintf(`^func\s+(?:\([^)]*\)\s+)?%s\b`, regexp.QuoteMeta(name))
		searchInput := rgutil.Normalize(rgutil.SearchInput{
			Pattern:    pattern,
			MaxMatches: maxMatches - len(defMatches),
			Glob:       []string{"*.go"},
		})
		result, err := ripgrep.SearchJSON(ctx, rgutil.BuildSearchOpts(searchInput, workspace, searchPath, nil))
		if err != nil {
			return defMatches, skillerr.WrapRuntime("ripgrep execution failed", err)
		}

		for _, match := range result.Matches {
			key := match.Path + ":" + strconv.Itoa(match.Line)
			if seen[key] {
				continue
			}
			seen[key] = true
			defMatches = append(defMatches, rawMatch{
				File:    match.Path,
				Line:    match.Line,
				EndLine: match.Line,
				Text:    match.Text,
			})
			if len(defMatches) >= maxMatches {
				break
			}
		}
	}

	return defMatches, nil
}

func blockKey(block Block) string {
	return fmt.Sprintf("%s:%d:%d", block.File, block.StartLine, block.EndLine)
}

func uniqueSortedInts(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	unique := append([]int(nil), values...)
	sort.Ints(unique)
	result := unique[:0]
	last := 0
	for i, v := range unique {
		if i == 0 || v != last {
			result = append(result, v)
			last = v
		}
	}
	return result
}

// emitResult emits search results with preview generation, artifact storage, file statistics, and comprehensive metrics.
func emitResult(ctx context.Context, rc *skillmain.RunContext, in input, blocks []Block, mode Mode) error {
	// Prepare preview and persist full blocks when truncated.
	previewResult, err := skillout.PreviewAndPersistNDJSON(ctx, rc, blocks, rc.MaxPreview, "code_context_grep", true)
	if err != nil {
		return err
	}
	artifact, err := ensureBlocksArtifact(ctx, rc, blocks, previewResult.Artifact)
	if err != nil {
		return err
	}
	fullTooLarge := rc != nil && rc.ShouldTruncate(estimateJSONSize(blocks))
	resolvedInlineMode, usePreviewBlocks, blocksTruncated, err := resolveInlineSelection(in.InlineMode, len(blocks), len(previewResult.Preview), artifact != nil, fullTooLarge)
	if err != nil {
		return err
	}
	inlineBlocks := blocks
	if usePreviewBlocks {
		inlineBlocks = previewResult.Preview
	}
	if resolvedInlineMode == InlineModeArtifactOnly {
		inlineBlocks = []Block{}
	}
	preview := codeblocks.PrepareBlockPreview(previewResult.Preview)
	renderedContext := renderContextMarkdown(previewResult.Preview)

	// Calculate totals
	totalMatches := 0
	fileHits := make(map[string]int)
	for _, b := range blocks {
		totalMatches += b.MatchCount
		fileHits[b.File] += b.MatchCount
	}
	topSymbols := summarizeTopSymbols(blocks, 5)

	data := map[string]any{
		"mode":                string(mode),
		"inline_mode":         string(resolvedInlineMode),
		"pattern":             in.Pattern,
		"pattern_mode":        in.PatternMode,
		"ast_pattern":         in.ASTPattern,
		"match_count":         totalMatches,
		"block_count":         len(blocks),
		"blocks_shown":        len(inlineBlocks),
		"blocks_truncated":    blocksTruncated,
		"files_touched":       len(fileHits),
		"preview":             preview,
		"blocks":              inlineBlocks,
		"rendered_context":    renderedContext,
		"render_format":       "markdown",
		"top_files":           skillout.SummarizeTopFiles(fileHits, 5),
		"top_symbols":         topSymbols,
		"max_blocks_per_file": in.MaxBlocksPerFile,
		"max_bytes_per_file":  in.MaxBytesPerFile,
		"expand_to":           in.ExpandTo,
		"include_definitions": in.IncludeDefinitions,
	}
	skillout.AddArtifact(data, artifact)

	return skillout.Emit(rc, "code/context_grep", data)
}
