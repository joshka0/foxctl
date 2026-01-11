// Package main implements the code/snippet_extract skill.
// It reads live workspace files and extracts high-signal code snippets
// based on a natural-language question and candidate files/symbols.
//
// See docs/spec/code_symbol_index_and_swe_grep.md §5 for the full contract.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/domain/policy"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/indexing/symbol"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

// Command is the envelope command for this skill.
const Command = "code/snippet_extract"

// Error codes per Core Profile v1 §13 and spec §5.4.
const (
	ErrCodeArg              = "EARG"                     // Invalid arguments
	ErrCodeRuntime          = "ERUNTIME"                 // Skill process error/crash
	ErrCodePolicy           = "EPOLICY"                  // Capability/policy violation (path escape)
	ErrCodeNotFound         = "ENOTFOUND"                // Resource not found (file missing)
	ErrCodeIO               = "EIO"                      // Filesystem or I/O error
	ErrCodeNoCandidates     = "E_SWE_GREP_NO_CANDIDATES" // No usable candidates provided (spec §5.4)
	ErrCodeCapabilityPolicy = "EPOLICY"
)

// ValidationError wraps an error with a specific error code for the envelope.
type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// Candidate represents a single candidate file/symbol from upstream retrieval.
type Candidate struct {
	Path     string  `json:"path"`
	SymbolID string  `json:"symbol_id,omitempty"`
	Priority float64 `json:"priority,omitempty"`
}

// Limits controls resource usage during snippet extraction.
type Limits struct {
	MaxFiles        int `json:"max_files,omitempty"`
	MaxSnippets     int `json:"max_snippets,omitempty"`
	MaxBytesPerFile int `json:"max_bytes_per_file,omitempty"`
}

// Default limits when not specified.
const (
	DefaultMaxFiles        = 50
	DefaultMaxSnippets     = 100
	DefaultMaxBytesPerFile = 64 * 1024 // 64 KB
	DefaultInlineKB        = 32        // 32 KB inline threshold
	MaxPreviewBytes        = 512       // Max bytes for inline preview
)

// Snippet extraction tuning constants.
const (
	ContextLines       = 3  // Lines of context above/below matching lines
	MaxLinesPerSnippet = 80 // Maximum lines in a single snippet block
	MinKeywordLen      = 3  // Minimum length for a keyword to be considered
)

// ArtifactKind is the MIME type for SWE Grep NDJSON artifacts.
const ArtifactKind = "application/x-swe-grep-snippets+ndjson"

// FileResult holds validated path and content for a candidate file.
type FileResult struct {
	Path      string  // Relative path from input
	AbsPath   string  // Validated absolute path
	SymbolID  string  // Optional symbol ID from input
	Priority  float64 // Priority from input
	Content   []byte  // File content (may be truncated per MaxBytesPerFile)
	Truncated bool    // True if content was truncated
	Skipped   bool    // True if file was skipped (not found, validation error, etc.)
	SkipErr   string  // Reason for skipping, if skipped
	ErrCode   string  // Error code if skipped due to error
}

// Snippet represents an extracted code snippet for output.
// For NDJSON artifacts, all fields are included.
// For inline previews, Text is truncated to MaxPreviewBytes.
type Snippet struct {
	File      string  `json:"file"`
	SymbolID  string  `json:"symbol_id,omitempty"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Text      string  `json:"text"`
	Priority  float64 `json:"priority,omitempty"`
}

// SnippetPreview is the inline representation with truncated text.
type SnippetPreview struct {
	File      string  `json:"file"`
	SymbolID  string  `json:"symbol_id,omitempty"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Preview   string  `json:"preview"`
	Priority  float64 `json:"priority,omitempty"`
}

// SessionContext provides context from related past sessions.
// See docs/designs/unified_semantic_search.md Phase 3.
type SessionContext struct {
	SessionID string   `json:"session_id"`
	Summary   string   `json:"summary"`
	Gotchas   []string `json:"gotchas,omitempty"`
	Decisions []string `json:"decisions,omitempty"`
	KeyFiles  []string `json:"key_files,omitempty"`
}

// MemoryContext provides context from related named memories.
type MemoryContext struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Type    string `json:"type,omitempty"`
}

// Context search defaults.
const (
	DefaultMaxRelatedSessions = 3
	ContextSearchTimeout      = 500 * time.Millisecond
)

// Input is the expected JSON input per spec §5.2.
// Note: This skill requires candidates. If you don't have candidates,
// use code/smart_search instead which auto-generates them.
type Input struct {
	WorkspaceID string      `json:"workspace_id"`
	Question    string      `json:"question"` // Required (validated after Query alias applied)
	Query       string      `json:"query"`    // Alias for question (for convenience)
	Candidates  []Candidate `json:"candidates" validate:"min=1"`
	Limits      Limits      `json:"limits,omitempty"`
}

func main() {
	skillmain.Main(Command, run)
}

// run is the main skill logic.
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Apply query alias for question (convenience for callers using "query")
	if in.Question == "" && in.Query != "" {
		in.Question = in.Query
	}

	// Validate question is provided (after alias applied)
	if in.Question == "" {
		return &ValidationError{
			Code:    ErrCodeArg,
			Message: "question or query is required",
		}
	}

	// Apply workspace default
	if in.WorkspaceID == "" {
		in.WorkspaceID = rc.PathValidator.Workspace()
	}

	// Validate candidates have usable paths
	usable := 0
	for _, c := range in.Candidates {
		if c.Path != "" {
			usable++
		}
	}
	if usable == 0 {
		return &ValidationError{
			Code:    ErrCodeNoCandidates,
			Message: "no usable candidates provided. Hint: use code/smart_search if you don't have candidates - it auto-generates them from indexes",
		}
	}

	// Normalize limits
	if in.Limits.MaxFiles < 0 {
		in.Limits.MaxFiles = 0
	}
	if in.Limits.MaxSnippets < 0 {
		in.Limits.MaxSnippets = 0
	}
	if in.Limits.MaxBytesPerFile < 0 {
		in.Limits.MaxBytesPerFile = 0
	}

	start := time.Now()

	// Apply default limits
	limits := applyDefaultLimits(in.Limits)

	// Process candidates: validate paths and read files
	fileResults := processFiles(ctx, rc, in.Candidates, limits)
	if err := fatalErrorForFileResults(fileResults); err != nil {
		return err
	}

	// Count stats
	filesConsidered := 0
	filesRelevant := 0
	for _, fr := range fileResults {
		filesConsidered++
		if !fr.Skipped && len(fr.Content) > 0 {
			filesRelevant++
		}
	}

	// Extract snippets from file results using question-aware matching
	snippets := extractSnippets(fileResults, in.Question, limits.MaxSnippets)

	// Create inline previews
	previews := makeInlinePreviews(snippets)

	// Build response data
	data := map[string]any{
		"summary": map[string]int{
			"files_considered": filesConsidered,
			"files_relevant":   filesRelevant,
			"snippets_emitted": len(snippets),
		},
		"snippets_inline": previews,
	}

	// Search for related sessions when embeddings are available
	// This provides context hints from past sessions that solved similar problems
	// Use actual workspace path from runner context for filtering (not the input workspace_id
	// which may be a logical name rather than an absolute path)
	workspacePath := rc.PathValidator.Workspace()
	if relatedSessions, hint := searchRelatedSessions(ctx, rc.Config, workspacePath, in.Question, DefaultMaxRelatedSessions); len(relatedSessions) > 0 {
		data["related_sessions"] = relatedSessions
	} else if hint != "" {
		// Surface non-fatal hint when session context is unavailable
		data["related_sessions_hint"] = hint
	}

	if len(snippets) > 0 {
		thresholdBytes := inlineThresholdBytes(rc)
		inlineSizeBytes, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("measure inline payload: %w", err)
		}
		if thresholdBytes > 0 && len(inlineSizeBytes) > thresholdBytes {
			artifact, err := persistSnippetsArtifact(ctx, rc, snippets)
			if err != nil {
				return fmt.Errorf("persist snippets artifact: %w", err)
			}
			if artifact.Digest != "" {
				data["artifact"] = artifact.Digest
			}

			if thresholdBytes > 0 {
				if trimmed, err := trimPreviewsToFit(data, previews, thresholdBytes); err == nil {
					data["snippets_inline"] = trimmed
				}
			}
		}
	}

	// Observability: log summary stats (D5)
	// Question is hashed to avoid leaking sensitive content
	hasArtifact := data["artifact"] != nil
	durationMS := time.Since(start).Milliseconds()

	logSummary(in.WorkspaceID, in.Question, len(in.Candidates), filesConsidered, filesRelevant, len(snippets), hasArtifact)

	// Write NDJSON event to observability dir (if configured)
	ev := observability.NewSweGrepEvent(
		in.WorkspaceID,
		in.Question,
		len(in.Candidates),
		filesConsidered,
		filesRelevant,
		len(snippets),
		hasArtifact,
		durationMS,
		"run",
	)
	// Best-effort event logging; error is not actionable.
	_ = observability.WriteSweGrepEvent(ctx, ev) //nolint:errcheck

	return skillout.Emit(rc, Command, data)
}

func fatalErrorForFileResults(results []FileResult) *ValidationError {
	for _, fr := range results {
		if !fr.Skipped {
			return nil
		}
	}

	// Deterministic precedence:
	// 1) Guard violations
	// 2) File not found
	// 3) Any other I/O error
	// 4) Fall back to EARG
	for _, fr := range results {
		if fr.ErrCode == ErrCodePolicy {
			return &ValidationError{Code: ErrCodePolicy, Message: fr.SkipErr}
		}
	}
	for _, fr := range results {
		if fr.ErrCode == ErrCodeNotFound {
			return &ValidationError{Code: ErrCodeNotFound, Message: fr.SkipErr}
		}
	}
	for _, fr := range results {
		if fr.ErrCode != "" {
			return &ValidationError{Code: fr.ErrCode, Message: fr.SkipErr}
		}
	}

	return &ValidationError{Code: ErrCodeArg, Message: "no readable candidate files"}
}

// logSummary writes a structured log entry to stderr with summary stats.
// The question is hashed to avoid leaking sensitive content per D5.
func logSummary(workspaceID, question string, numCandidates, filesConsidered, filesRelevant, snippetsEmitted int, hasArtifact bool) {
	log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	log.Info().
		Str("skill", Command).
		Str("workspace_id", workspaceID).
		Str("question_hash", observability.HashQuestion(question)).
		Int("candidates", numCandidates).
		Int("files_considered", filesConsidered).
		Int("files_relevant", filesRelevant).
		Int("snippets_emitted", snippetsEmitted).
		Bool("has_artifact", hasArtifact).
		Msg("swe_grep_complete")
}

// extractSnippets creates snippets from file results using question-aware matching.
// It extracts keywords from the question and finds matching blocks in each file.
// Snippets are sorted by priority (descending) so highest-value snippets come first.
func extractSnippets(results []FileResult, question string, maxSnippets int) []Snippet {
	keywords := extractKeywords(question)
	snippets := make([]Snippet, 0, len(results)*2) // Pre-allocate for multiple snippets per file

	// Extract all snippets first (without limiting)
	for _, fr := range results {
		if fr.Skipped || len(fr.Content) == 0 {
			continue
		}

		// Extract snippets for this file (get all, we'll rank and trim later)
		fileSnippets := extractSnippetsForFile(fr, keywords, maxSnippets)
		snippets = append(snippets, fileSnippets...)
	}

	// Sort snippets by priority (highest first)
	sort.Slice(snippets, func(i, j int) bool {
		return snippets[i].Priority > snippets[j].Priority
	})

	// Trim to maxSnippets
	if len(snippets) > maxSnippets {
		snippets = snippets[:maxSnippets]
	}

	return snippets
}

// extractSnippetsForFile extracts matching snippet blocks from a single file.
// If a symbol_id is provided, tries to extract the complete symbol body first.
// Falls back to keyword-based matching otherwise.
func extractSnippetsForFile(fr FileResult, keywords []string, remaining int) []Snippet {
	if remaining <= 0 {
		return nil
	}

	lines := splitLines(fr.Content)
	if len(lines) == 0 {
		return nil
	}

	// If symbol_id is provided, try symbol-aware extraction first
	if fr.SymbolID != "" {
		if snippet := extractSymbolBody(fr, lines); snippet != nil {
			return []Snippet{*snippet}
		}
		// Fall through to keyword-based extraction if symbol detection fails
	}

	// Find which lines match any keyword
	matchingLines := findMatchingLines(lines, keywords)
	if len(matchingLines) == 0 {
		// No keyword matches - if file has content, return a fallback snippet
		// from the beginning of the file (useful when question is vague)
		return createFallbackSnippet(fr, lines, remaining)
	}

	// Group matching lines into blocks with context
	blocks := groupIntoBlocks(matchingLines, len(lines))

	// Convert blocks to snippets
	snippets := make([]Snippet, 0, min(len(blocks), remaining))
	for _, block := range blocks {
		if len(snippets) >= remaining {
			break
		}

		text := joinLines(lines, block.start, block.end)
		snippets = append(snippets, Snippet{
			File:      fr.Path,
			SymbolID:  fr.SymbolID,
			StartLine: block.start + 1, // 1-indexed
			EndLine:   block.end + 1,   // 1-indexed, inclusive
			Text:      text,
			Priority:  fr.Priority,
		})
	}

	return snippets
}

// extractSymbolBody extracts the complete body of a symbol.
// For Go files, it uses the AST-based GoExtractor for accurate boundaries.
// For other languages, it falls back to heuristic-based detection.
// The symbol_id format is "path:name", e.g., "pkg/auth/login.go:Login".
// Returns nil if the symbol cannot be found or detected.
func extractSymbolBody(fr FileResult, lines []string) *Snippet {
	// Parse symbol name from symbol_id (format: path:name)
	symbolName := parseSymbolName(fr.SymbolID)
	if symbolName == "" {
		return nil
	}

	// Detect language from file extension
	lang := detectLanguage(fr.Path)

	// For Go files, use AST-based extractor for accurate boundaries
	if lang == "go" {
		if snippet := extractGoSymbolBody(fr, lines, symbolName); snippet != nil {
			return snippet
		}
		// Fall through to heuristic if AST extraction fails
	}

	// For other languages or as fallback, use heuristic-based detection
	return extractSymbolBodyHeuristic(fr, lines, symbolName, lang)
}

// extractGoSymbolBody uses the AST-based GoExtractor for accurate symbol boundaries.
func extractGoSymbolBody(fr FileResult, lines []string, symbolName string) *Snippet {
	extractor := symbol.NewGoExtractor()
	symbols, err := extractor.Extract(context.Background(), fr.Path, fr.Content)
	if err != nil || len(symbols) == 0 {
		return nil
	}

	// Find matching symbol by name
	var matchedSymbol *symbol.Symbol
	for i := range symbols {
		s := &symbols[i]
		// Exact match
		if s.Name == symbolName {
			matchedSymbol = s
			break
		}
		// For methods, try matching just the method name
		// (symbols have format "ReceiverType.MethodName")
		if idx := strings.LastIndex(s.Name, "."); idx >= 0 {
			methodName := s.Name[idx+1:]
			if methodName == symbolName {
				matchedSymbol = s
				break
			}
		}
	}

	if matchedSymbol == nil {
		return nil
	}

	// Convert to 0-indexed for lines array access
	startLine := matchedSymbol.StartLine - 1
	endLine := matchedSymbol.EndLine - 1

	// Bounds check
	if startLine < 0 || endLine >= len(lines) || startLine > endLine {
		return nil
	}

	// Cap at MaxLinesPerSnippet
	if endLine-startLine+1 > MaxLinesPerSnippet {
		endLine = startLine + MaxLinesPerSnippet - 1
	}

	text := joinLines(lines, startLine, endLine)
	return &Snippet{
		File:      fr.Path,
		SymbolID:  fr.SymbolID,
		StartLine: startLine + 1, // Back to 1-indexed
		EndLine:   endLine + 1,   // Back to 1-indexed
		Text:      text,
		Priority:  fr.Priority,
	}
}

// extractSymbolBodyHeuristic uses pattern matching for non-Go languages.
func extractSymbolBodyHeuristic(fr FileResult, lines []string, symbolName string, lang string) *Snippet {
	// Find the symbol definition line
	defLine := findSymbolDefinition(lines, symbolName, lang)
	if defLine < 0 {
		return nil
	}

	// Find the end of the symbol body
	endLine := findSymbolEnd(lines, defLine, lang)
	if endLine < defLine {
		endLine = defLine
	}

	// Cap at MaxLinesPerSnippet
	if endLine-defLine+1 > MaxLinesPerSnippet {
		endLine = defLine + MaxLinesPerSnippet - 1
	}

	text := joinLines(lines, defLine, endLine)
	return &Snippet{
		File:      fr.Path,
		SymbolID:  fr.SymbolID,
		StartLine: defLine + 1, // 1-indexed
		EndLine:   endLine + 1, // 1-indexed
		Text:      text,
		Priority:  fr.Priority,
	}
}

// parseSymbolName extracts the symbol name from a symbol_id.
// Format: "path:name" -> "name"
func parseSymbolName(symbolID string) string {
	idx := strings.LastIndex(symbolID, ":")
	if idx < 0 || idx >= len(symbolID)-1 {
		return ""
	}
	return symbolID[idx+1:]
}

// detectLanguage returns a normalized language identifier from file extension.
func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".gd":
		return "gdscript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".hpp", ".cc", ".cxx":
		return "cpp"
	default:
		return ""
	}
}

// findSymbolDefinition finds the 0-indexed line where a symbol is defined.
// Returns -1 if not found.
func findSymbolDefinition(lines []string, name string, lang string) int {
	// Build patterns based on language
	var patterns []string
	switch lang {
	case "go":
		patterns = []string{
			fmt.Sprintf("func %s(", name),  // function
			fmt.Sprintf("func (%s", name),  // receiver might match
			fmt.Sprintf(") %s(", name),     // method
			fmt.Sprintf("type %s ", name),  // type definition
			fmt.Sprintf("type %s\t", name), // type definition with tab
		}
	case "python":
		patterns = []string{
			fmt.Sprintf("def %s(", name),
			fmt.Sprintf("class %s(", name),
			fmt.Sprintf("class %s:", name),
		}
	case "typescript", "javascript":
		patterns = []string{
			fmt.Sprintf("function %s(", name),
			fmt.Sprintf("function %s (", name),
			fmt.Sprintf("const %s = ", name),
			fmt.Sprintf("let %s = ", name),
			fmt.Sprintf("class %s ", name),
			fmt.Sprintf(" %s(", name),          // method
			fmt.Sprintf(" %s: function", name), // object method
		}
	case "gdscript":
		patterns = []string{
			fmt.Sprintf("func %s(", name),
			fmt.Sprintf("class_name %s", name),
		}
	case "rust":
		patterns = []string{
			fmt.Sprintf("fn %s(", name),
			fmt.Sprintf("fn %s<", name),
			fmt.Sprintf("struct %s ", name),
			fmt.Sprintf("struct %s<", name),
			fmt.Sprintf("impl %s ", name),
		}
	case "java", "c", "cpp":
		patterns = []string{
			fmt.Sprintf(" %s(", name), // generic function/method match
		}
	default:
		// Generic pattern for unknown languages
		patterns = []string{
			fmt.Sprintf(" %s(", name),
			fmt.Sprintf("\t%s(", name),
		}
	}

	for i, line := range lines {
		for _, pattern := range patterns {
			if strings.Contains(line, pattern) {
				return i
			}
		}
	}
	return -1
}

// findSymbolEnd finds the 0-indexed line where a symbol body ends.
// Uses bracket counting for brace-based languages, indentation for Python/GDScript.
func findSymbolEnd(lines []string, startLine int, lang string) int {
	if startLine < 0 || startLine >= len(lines) {
		return startLine
	}

	switch lang {
	case "python", "gdscript":
		return findIndentationEnd(lines, startLine)
	default:
		return findBraceEnd(lines, startLine)
	}
}

// findBraceEnd finds the end of a brace-delimited block (Go, JS, TS, Rust, etc.)
// using simple brace counting.
//
// LIMITATION: This is a heuristic approach that counts '{' and '}' characters
// without parsing the actual syntax. It does NOT handle braces inside:
//   - String literals (e.g., "{ not a block }")
//   - Comments (e.g., // { or /* { */)
//   - Character literals (e.g., '{')
//   - Template literals (e.g., `${ expr }`)
//
// For accurate symbol boundary detection, prefer using AST-based extraction
// (e.g., extractGoSymbolBody for Go files) when available.
func findBraceEnd(lines []string, startLine int) int {
	braceCount := 0
	foundOpen := false

	for i := startLine; i < len(lines); i++ {
		line := lines[i]
		for _, ch := range line {
			switch ch {
			case '{':
				braceCount++
				foundOpen = true
			case '}':
				braceCount--
				if foundOpen && braceCount == 0 {
					return i
				}
			}
		}
	}

	// If no closing brace found, return a reasonable limit
	maxEnd := startLine + MaxLinesPerSnippet - 1
	if maxEnd >= len(lines) {
		maxEnd = len(lines) - 1
	}
	return maxEnd
}

// findIndentationEnd finds the end of an indentation-based block (Python, GDScript)
func findIndentationEnd(lines []string, startLine int) int {
	if startLine >= len(lines) {
		return startLine
	}

	// Get the indentation of the definition line
	baseIndent := countLeadingWhitespace(lines[startLine])

	// Find the first non-empty line after the definition to get body indentation
	bodyIndent := -1
	for i := startLine + 1; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue // Skip empty lines and comments
		}
		bodyIndent = countLeadingWhitespace(line)
		break
	}

	if bodyIndent <= baseIndent {
		// No body found, return definition line only
		return startLine
	}

	// Find where indentation returns to base level or less
	lastContentLine := startLine
	for i := startLine + 1; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue // Skip blank lines
		}
		indent := countLeadingWhitespace(line)
		if indent <= baseIndent && !strings.HasPrefix(trimmed, "#") {
			// Found a line at the same or less indentation - we're done
			break
		}
		lastContentLine = i
	}

	return lastContentLine
}

// countLeadingWhitespace counts leading spaces and tabs (tabs count as 4 spaces)
func countLeadingWhitespace(s string) int {
	count := 0
	for _, ch := range s {
		switch ch {
		case ' ':
			count++
		case '\t':
			count += 4
		default:
			return count
		}
	}
	return count
}

// lineBlock represents a contiguous block of lines.
type lineBlock struct {
	start int // 0-indexed start line
	end   int // 0-indexed end line (inclusive)
}

// extractKeywords extracts meaningful keywords from a question.
// It lowercases, splits on non-alphanumeric, and filters short/stop words.
func extractKeywords(question string) []string {
	// Common stop words to filter out
	stopWords := map[string]bool{
		"the": true, "and": true, "for": true, "are": true, "but": true,
		"not": true, "you": true, "all": true, "can": true, "had": true,
		"her": true, "was": true, "one": true, "our": true, "out": true,
		"has": true, "have": true, "been": true, "this": true, "that": true,
		"what": true, "when": true, "where": true, "which": true, "who": true,
		"how": true, "does": true, "from": true, "with": true, "into": true,
		"about": true, "would": true, "could": true, "should": true,
	}

	lower := strings.ToLower(question)

	// Split on non-alphanumeric characters
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	keywords := make([]string, 0, len(words))
	seen := make(map[string]bool)

	for _, w := range words {
		if len(w) < MinKeywordLen {
			continue
		}
		if stopWords[w] {
			continue
		}
		if seen[w] {
			continue
		}
		seen[w] = true
		keywords = append(keywords, w)
	}

	return keywords
}

// splitLines splits content into lines, preserving empty lines.
func splitLines(content []byte) []string {
	if len(content) == 0 {
		return nil
	}
	s := string(content)
	// Remove trailing newline to avoid empty last element
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

// findMatchingLines returns 0-indexed line numbers that contain any keyword.
func findMatchingLines(lines []string, keywords []string) []int {
	if len(keywords) == 0 {
		return nil
	}

	matching := make([]int, 0)
	for i, line := range lines {
		lower := strings.ToLower(line)
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				matching = append(matching, i)
				break
			}
		}
	}
	return matching
}

// groupIntoBlocks groups matching line indices into contiguous blocks with context.
// Adjacent or overlapping matches are merged. Each block is capped at MaxLinesPerSnippet.
func groupIntoBlocks(matchingLines []int, totalLines int) []lineBlock {
	if len(matchingLines) == 0 {
		return nil
	}

	blocks := make([]lineBlock, 0)
	var current *lineBlock

	for _, lineIdx := range matchingLines {
		// Calculate block boundaries with context
		start := max(0, lineIdx-ContextLines)
		end := min(totalLines-1, lineIdx+ContextLines)

		if current == nil {
			current = &lineBlock{start: start, end: end}
		} else if start <= current.end+1 {
			// Merge overlapping or adjacent blocks
			current.end = max(current.end, end)
			// Cap at MaxLinesPerSnippet and finalize
			if current.end-current.start+1 > MaxLinesPerSnippet {
				current.end = current.start + MaxLinesPerSnippet - 1
				blocks = append(blocks, *current)
				current = nil // Force next match to start a fresh block
			}
		} else {
			// Start a new block
			blocks = append(blocks, *current)
			current = &lineBlock{start: start, end: end}
		}
	}

	if current != nil {
		blocks = append(blocks, *current)
	}

	return blocks
}

// joinLines joins lines from start to end (inclusive, 0-indexed) into a single string.
func joinLines(lines []string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end >= len(lines) {
		end = len(lines) - 1
	}
	if start > end {
		return ""
	}
	return strings.Join(lines[start:end+1], "\n")
}

// createFallbackSnippet creates a snippet from the beginning of a file
// when no keyword matches are found. This handles vague questions.
func createFallbackSnippet(fr FileResult, lines []string, remaining int) []Snippet {
	if remaining <= 0 || len(lines) == 0 {
		return nil
	}

	// Take up to MaxLinesPerSnippet lines from the beginning
	end := min(len(lines)-1, MaxLinesPerSnippet-1)
	text := joinLines(lines, 0, end)

	return []Snippet{{
		File:      fr.Path,
		SymbolID:  fr.SymbolID,
		StartLine: 1,
		EndLine:   end + 1, // 1-indexed
		Text:      text,
		Priority:  fr.Priority,
	}}
}

// makeInlinePreviews creates truncated previews for inline embedding.
func makeInlinePreviews(snippets []Snippet) []SnippetPreview {
	previews := make([]SnippetPreview, len(snippets))
	for i, s := range snippets {
		preview := s.Text
		if len(preview) > MaxPreviewBytes {
			// Truncate at MaxPreviewBytes, try to break at newline
			preview = preview[:MaxPreviewBytes]
			if lastNL := findLastNewline(preview); lastNL > MaxPreviewBytes/2 {
				preview = preview[:lastNL+1]
			}
			preview += "..."
		}
		previews[i] = SnippetPreview{
			File:      s.File,
			SymbolID:  s.SymbolID,
			StartLine: s.StartLine,
			EndLine:   s.EndLine,
			Preview:   preview,
			Priority:  s.Priority,
		}
	}
	return previews
}

// persistSnippetsArtifact writes full snippets as NDJSON to CAS.

func inlineThresholdBytes(rc *skillmain.RunContext) int {
	if rc == nil {
		return DefaultInlineKB * 1024
	}
	if rc.InlineKB > 0 {
		return rc.InlineKB * 1024
	}
	return DefaultInlineKB * 1024
}

func trimPreviewsToFit(data map[string]any, previews []SnippetPreview, thresholdBytes int) ([]SnippetPreview, error) {
	tmp := make([]SnippetPreview, 0, len(previews))
	for _, p := range previews {
		tmp = append(tmp, p)
		data["snippets_inline"] = tmp
		b, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		if len(b) > thresholdBytes {
			return tmp[:len(tmp)-1], nil
		}
	}
	return tmp, nil
}

func persistSnippetsArtifact(ctx context.Context, rc *skillmain.RunContext, snippets []Snippet) (skillmain.Artifact, error) {
	if len(snippets) == 0 {
		return skillmain.Artifact{}, nil
	}

	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	for _, s := range snippets {
		if err := enc.Encode(s); err != nil {
			return skillmain.Artifact{}, fmt.Errorf("encode snippet: %w", err)
		}
	}
	return skillmain.PersistBuffer(ctx, rc, buf, ArtifactKind, "code_swe_grep")
}

// countLines counts the number of lines in content.
func countLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	count := bytes.Count(content, []byte{'\n'})
	// Add 1 if content doesn't end with newline
	if content[len(content)-1] != '\n' {
		count++
	}
	return count
}

// findLastNewline returns the index of the last newline in s, or -1 if not found.
func findLastNewline(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '\n' {
			return i
		}
	}
	return -1
}

// applyDefaultLimits fills in default values for unset limits.
func applyDefaultLimits(l Limits) Limits {
	if l.MaxFiles <= 0 {
		l.MaxFiles = DefaultMaxFiles
	}
	if l.MaxSnippets <= 0 {
		l.MaxSnippets = DefaultMaxSnippets
	}
	if l.MaxBytesPerFile <= 0 {
		l.MaxBytesPerFile = DefaultMaxBytesPerFile
	}
	return l
}

// processFiles validates and reads candidate files up to limits.
// It opens files immediately after validation to eliminate TOCTOU race conditions.
func processFiles(ctx context.Context, rc *skillmain.RunContext, candidates []Candidate, limits Limits) []FileResult {
	results := make([]FileResult, 0, len(candidates))
	filesProcessed := 0

	for _, c := range candidates {
		// Check context cancellation at each iteration
		if ctx.Err() != nil {
			break
		}

		if c.Path == "" {
			continue // Skip candidates with empty paths
		}
		if filesProcessed >= limits.MaxFiles {
			break // Respect MaxFiles limit
		}

		fr := FileResult{
			Path:     c.Path,
			SymbolID: c.SymbolID,
			Priority: c.Priority,
		}

		// Validate path through PathValidator
		absPath, err := rc.PathValidator.ValidatePath(c.Path)
		if err != nil {
			fr.Skipped = true
			fr.SkipErr, fr.ErrCode = classifyPathError(err)
			results = append(results, fr)
			filesProcessed++
			continue
		}

		// Resolve symlinks to get the canonical path, then re-validate
		// to ensure the resolved path is still within the workspace.
		resolvedPath, err := filepath.EvalSymlinks(absPath)
		if err != nil {
			fr.Skipped = true
			fr.SkipErr, fr.ErrCode = classifyFileError(err)
			results = append(results, fr)
			filesProcessed++
			continue
		}

		// Re-validate the resolved path to catch symlink escapes
		if _, err := rc.PathValidator.ValidatePath(resolvedPath); err != nil {
			fr.Skipped = true
			fr.SkipErr, fr.ErrCode = classifyPathError(err)
			results = append(results, fr)
			filesProcessed++
			continue
		}
		fr.AbsPath = resolvedPath

		// Open file immediately after validation to eliminate TOCTOU window.
		// We pass the open file descriptor to readFromFile.
		file, err := os.Open(resolvedPath)
		if err != nil {
			fr.Skipped = true
			fr.SkipErr, fr.ErrCode = classifyFileError(err)
			results = append(results, fr)
			filesProcessed++
			continue
		}

		// Get file info from the open descriptor (not the path) for integrity
		info, err := file.Stat()
		if err != nil {
			errs.Ignore(file.Close(), "close file after stat error")
			fr.Skipped = true
			fr.SkipErr, fr.ErrCode = classifyFileError(err)
			results = append(results, fr)
			filesProcessed++
			continue
		}

		// Skip directories
		if info.IsDir() {
			errs.Ignore(file.Close(), "close directory")
			fr.Skipped = true
			fr.SkipErr = "path is a directory"
			fr.ErrCode = ErrCodeArg
			results = append(results, fr)
			filesProcessed++
			continue
		}

		// Read file content with limit using the already-open descriptor
		content, truncated, err := readFromFile(ctx, file, info, limits.MaxBytesPerFile)
		errs.Ignore(file.Close(), "close file after read")

		if err != nil {
			fr.Skipped = true
			fr.SkipErr, fr.ErrCode = classifyFileError(err)
			results = append(results, fr)
			filesProcessed++
			continue
		}

		fr.Content = content
		fr.Truncated = truncated
		results = append(results, fr)
		filesProcessed++
	}

	return results
}

// classifyPathError returns a human-readable reason and error code for path validation failure.
func classifyPathError(err error) (string, string) {
	switch {
	case errors.Is(err, policy.ErrPathEscape):
		return "path escapes workspace", ErrCodePolicy
	case errors.Is(err, policy.ErrSymlinkEscape):
		return "symlink escapes workspace", ErrCodePolicy
	case errors.Is(err, policy.ErrInvalidPath):
		return "invalid path", ErrCodeArg
	case errors.Is(err, policy.ErrNullByte):
		return "path contains null byte", ErrCodeArg
	case errors.Is(err, policy.ErrNotAbsolute):
		return "path must resolve to absolute location", ErrCodeArg
	default:
		return fmt.Sprintf("path validation failed: %v", err), ErrCodePolicy
	}
}

// classifyFileError returns a human-readable reason and error code for file read failure.
func classifyFileError(err error) (string, string) {
	if os.IsNotExist(err) {
		return "file not found", ErrCodeNotFound
	}
	if os.IsPermission(err) {
		return "permission denied", ErrCodeCapabilityPolicy
	}
	return fmt.Sprintf("read error: %v", err), ErrCodeIO
}

// readFromFile reads from an already-open file up to maxBytes.
// It accepts the os.FileInfo to verify the file hasn't been replaced.
// The caller is responsible for closing the file.
func readFromFile(ctx context.Context, file *os.File, info os.FileInfo, maxBytes int) ([]byte, bool, error) {
	// Check cancellation before reading
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	// Determine read size: use file size if known and smaller than limit
	readSize := maxBytes + 1 // +1 to detect truncation
	fileSize := info.Size()
	if fileSize >= 0 && fileSize < int64(readSize) {
		readSize = int(fileSize) + 1 // May still be less if file is tiny
	}

	buf := make([]byte, readSize)
	n, err := io.ReadFull(file, buf)

	// Check cancellation after read
	if ctx.Err() != nil {
		return nil, false, ctx.Err()
	}

	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, false, err
	}

	if n > maxBytes {
		return buf[:maxBytes], true, nil
	}
	return buf[:n], false, nil
}

// readFileWithLimit reads a file up to maxBytes, returning content and whether it was truncated.
// Deprecated: Use readFromFile with an already-open file to avoid TOCTOU races.
// Kept for testing purposes.
func readFileWithLimit(ctx context.Context, path string, maxBytes int) ([]byte, bool, error) {
	// Check cancellation before opening
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer func() {
		errs.Ignore(file.Close(), "close file")
	}()

	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}

	return readFromFile(ctx, file, info, maxBytes)
}

// searchRelatedSessions searches for sessions related to the question using embeddings.
// Returns sessions and a hint when unavailable/non-fatal.
func searchRelatedSessions(ctx context.Context, cfg config.Config, workspaceID, question string, limit int) ([]SessionContext, string) {
	provider, hint := createEmbeddingProvider(cfg)
	if provider == nil {
		return nil, hint
	}

	// Generate query embedding with timeout
	embedCtx, embedCancel := context.WithTimeout(ctx, ContextSearchTimeout)
	defer embedCancel()

	queryVec, err := provider.Embed(embedCtx, question)
	if err != nil {
		return nil, fmt.Sprintf("session context unavailable: embedding failed: %v", err)
	}

	// Get AGENTCTL_HOME for storage path
	agentctlHome := os.Getenv("AGENTCTL_HOME")
	if agentctlHome == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, "session context unavailable: home not resolved"
		}
		agentctlHome = filepath.Join(homeDir, ".agentctl")
	}

	// Open sessions store
	storageRoot := filepath.Join(agentctlHome, "storage")
	sessionStore, err := sessions.Open(ctx, storageRoot)
	if err != nil {
		return nil, "session context unavailable: open sessions store failed"
	}
	defer sessionStore.Close()

	// Search for similar sessions with timeout (fetch extra for workspace filtering)
	searchCtx, searchCancel := context.WithTimeout(ctx, ContextSearchTimeout)
	defer searchCancel()

	results, err := sessionStore.SearchSimilar(searchCtx, queryVec, limit*2)
	if err != nil {
		return nil, "session context unavailable: search failed"
	}

	// Convert to SessionContext with workspace scoping
	contexts := make([]SessionContext, 0, len(results))
	for _, r := range results {
		s := r.Session
		// Filter by workspace if specified
		if workspaceID != "" && s.WorkspacePath != "" && s.WorkspacePath != workspaceID {
			continue
		}
		sc := SessionContext{
			SessionID: s.ID,
			Summary:   s.Summary,
		}
		if len(s.Gotchas) > 0 {
			sc.Gotchas = s.Gotchas
		}
		if len(s.Decisions) > 0 {
			sc.Decisions = s.Decisions
		}
		if len(s.KeyFiles) > 0 {
			sc.KeyFiles = s.KeyFiles
		}
		contexts = append(contexts, sc)
		if len(contexts) >= limit {
			break
		}
	}

	if len(contexts) == 0 {
		return nil, "no related sessions found for this workspace"
	}
	return contexts, ""
}

// createEmbeddingProvider creates an embedding provider from config/env.
// Returns provider (or nil) and a hint when unavailable.
// Prefers Voyage with ScopeModelRecommendation for consistency.
func createEmbeddingProvider(cfg config.Config) (semantic.EmbeddingProvider, string) {
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")

	if voyageKey == "" && geminiKey == "" {
		return nil, "no embedding API key set (VOYAGE_API_KEY or GEMINI_API_KEY); session context disabled (BM25-only)"
	}

	// Prefer Voyage with scope-based model selection
	if voyageKey != "" {
		model, _ := semantic.ScopeModelRecommendation(semantic.ScopeSessions)
		provider, err := semantic.NewVoyageProvider(semantic.VoyageConfig{
			APIKey: voyageKey,
			Model:  model,
		})
		if err != nil {
			return nil, fmt.Sprintf("failed to create Voyage provider: %v; session context disabled", err)
		}
		return provider, ""
	}

	// Gemini fallback (not recommended - dimension mismatch with Voyage)
	provider, err := semantic.NewGeminiProvider(semantic.GeminiConfig{
		APIKey: geminiKey,
		Model:  "gemini-embedding-001",
	})
	if err != nil {
		return nil, fmt.Sprintf("failed to create Gemini provider: %v; session context disabled", err)
	}
	return provider, ""
}

