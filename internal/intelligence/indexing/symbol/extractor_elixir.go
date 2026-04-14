package symbol

import (
	"context"
	"regexp"
	"sort"
	"strings"
)

// ElixirExtractor extracts symbols from Elixir sources using line-based heuristics.
type ElixirExtractor struct{}

// NewElixirExtractor creates a new Elixir extractor.
func NewElixirExtractor() *ElixirExtractor {
	return &ElixirExtractor{}
}

// SupportedLanguages returns ["elixir"].
func (e *ElixirExtractor) SupportedLanguages() []string {
	return []string{"elixir"}
}

// Elixir declaration patterns
var (
	// defmodule ModuleName do / defprotocol ModuleName do
	elixirModulePattern = regexp.MustCompile(`^\s*(?:defmodule|defprotocol)\s+([A-Z][A-Za-z0-9_.]*)\s+do`)

	// def func_name(args) do  OR  def func_name(args), do:
	elixirDefPattern = regexp.MustCompile(`^\s*def\s+([a-z_][a-z0-9_?!]*)\s*(?:\(|,|\s+do)`)

	// defp func_name(args) do  OR  defp func_name(args), do:
	elixirDefpPattern = regexp.MustCompile(`^\s*defp\s+([a-z_][a-z0-9_?!]*)\s*(?:\(|,|\s+do)`)

	// defmacro macro_name(args) do
	elixirDefmacroPattern = regexp.MustCompile(`^\s*defmacro\s+([a-z_][a-z0-9_?!]*)\s*(?:\(|,|\s+do)`)

	// defmacrop macro_name(args) do
	elixirDefmacropPattern = regexp.MustCompile(`^\s*defmacrop\s+([a-z_][a-z0-9_?!]*)\s*(?:\(|,|\s+do)`)

	// @type name :: type_def
	elixirTypePattern = regexp.MustCompile(`^\s*@type\s+([a-z_][a-z0-9_]*)\s*::`)

	// @typep name :: type_def (private type)
	elixirTypepPattern = regexp.MustCompile(`^\s*@typep\s+([a-z_][a-z0-9_]*)\s*::`)

	// @callback name(args) :: return_type
	elixirCallbackPattern = regexp.MustCompile(`^\s*@callback\s+([a-z_][a-z0-9_?!]*)\s*\(`)

	// Keywords that start/end blocks for tracking nesting
	elixirBlockStart = regexp.MustCompile(`\b(do|fn)\s*$|\bdo\s*:|\bfn\s*->`)
	elixirBlockEnd   = regexp.MustCompile(`^\s*end\b`)
)

// Extract parses Elixir source and returns top-level symbols.
func (e *ElixirExtractor) Extract(ctx context.Context, filePath string, content []byte) ([]Symbol, error) {
	if symbols, ok, err := extractElixirSymbolsWithTreeSitter(ctx, filePath, content); ok || err != nil {
		return symbols, err
	}
	return e.extractHeuristic(filePath, content)
}

func (e *ElixirExtractor) extractHeuristic(filePath string, content []byte) ([]Symbol, error) {
	lines := strings.Split(string(content), "\n")
	lineOffsets := computeLineOffsets(lines)

	var symbols []Symbol
	var pendingDoc string
	var pendingTypeDoc string

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if doc, endIdx, ok := parseElixirDocAttribute(lines, i, "@doc"); ok {
			pendingDoc = doc
			i = endIdx
			continue
		}
		if doc, endIdx, ok := parseElixirDocAttribute(lines, i, "@typedoc"); ok {
			pendingTypeDoc = doc
			i = endIdx
			continue
		}

		name, kind, signature, ok := parseElixirDeclaration(line)
		if !ok {
			continue
		}

		startLine := i + 1
		endLine := startLine

		// Find block end for constructs that use do...end
		if kind == KindClass || kind == KindFunction {
			// Check if this line contains a block start (do or fn)
			if elixirBlockStart.MatchString(line) || strings.HasSuffix(strings.TrimSpace(line), "do") {
				endIdx := findElixirBlockEnd(lines, i)
				if endIdx >= i {
					endLine = endIdx + 1
				}
			} else if strings.Contains(line, ", do:") {
				// Single-line do: syntax - block is just this line
				endLine = startLine
			}
		}

		startByte := lineOffsets[i]
		endByte := lineOffsets[len(lineOffsets)-1]
		if endLine-1 < len(lineOffsets) {
			endByte = lineOffsets[endLine-1] + len(lines[endLine-1])
		}
		if startByte > len(content) {
			startByte = len(content)
		}
		if endByte > len(content) {
			endByte = len(content)
		}
		if endByte < startByte {
			endByte = startByte
		}

		body := content[startByte:endByte]
		doc := ""
		switch kind {
		case KindClass:
			doc = extractElixirModuleDoc(lines, i, endLine-1)
		case KindType:
			doc = pendingTypeDoc
			pendingTypeDoc = ""
		default:
			doc = pendingDoc
			pendingDoc = ""
		}

		symbols = append(symbols, Symbol{
			ID:            ID(filePath, name),
			FilePath:      filePath,
			Name:          name,
			Language:      "elixir",
			Kind:          kind,
			StartByte:     startByte,
			EndByte:       endByte,
			StartLine:     startLine,
			EndLine:       endLine,
			Signature:     signature,
			BodyDigest:    ComputeDigest(body),
			Documentation: strings.TrimSpace(doc),
		})
	}

	return symbols, nil
}

// ExtractCalls extracts best-effort module references from an Elixir symbol body.
//
// In practice, this is more useful than trying to resolve local function calls,
// because Elixir frequently omits parentheses and functions are not namespaced in
// the v1 symbol model.
func (e *ElixirExtractor) ExtractCalls(ctx context.Context, symbol Symbol, content []byte) ([]string, error) {
	if refs, ok, err := extractElixirCallsWithTreeSitter(ctx, symbol, content); ok || err != nil {
		return refs, err
	}
	if symbol.StartByte < 0 || symbol.EndByte > len(content) || symbol.StartByte >= symbol.EndByte {
		return nil, nil
	}
	body := string(content[symbol.StartByte:symbol.EndByte])
	refs := mergeElixirRefs(extractElixirModuleRefs(body), extractElixirLocalCallRefs(body))
	if len(refs) > 50 {
		refs = refs[:50]
	}
	return refs, nil
}

// extractElixirModuleDoc finds a moduledoc string inside a module block.
func extractElixirModuleDoc(lines []string, startIdx, endIdx int) string {
	if startIdx < 0 {
		return ""
	}
	if endIdx >= len(lines) {
		endIdx = len(lines) - 1
	}
	for i := startIdx + 1; i <= endIdx && i < len(lines); i++ {
		if doc, end, ok := parseElixirDocAttribute(lines, i, "@moduledoc"); ok {
			_ = end
			return doc
		}
	}
	return ""
}

func parseElixirDocAttribute(lines []string, startIdx int, attr string) (string, int, bool) {
	line := strings.TrimSpace(strings.TrimRight(lines[startIdx], "\r"))
	if !strings.HasPrefix(line, attr) {
		return "", startIdx, false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, attr))
	if rest == "false" || rest == "nil" {
		return "", startIdx, true
	}
	doc, endIdx, ok := parseElixirDocValue(lines, startIdx, rest)
	if !ok {
		return "", startIdx, true
	}
	return doc, endIdx, true
}

func parseElixirDocValue(lines []string, startIdx int, rest string) (string, int, bool) {
	if strings.HasPrefix(rest, "\"\"\"") {
		return parseElixirTripleQuoted(lines, startIdx, rest, "\"\"\"")
	}
	if strings.HasPrefix(rest, "\"") {
		trimmed := strings.TrimPrefix(rest, "\"")
		if idx := strings.LastIndex(trimmed, "\""); idx >= 0 {
			return strings.TrimSpace(trimmed[:idx]), startIdx, true
		}
		return strings.TrimSpace(trimmed), startIdx, true
	}
	return "", startIdx, false
}

func parseElixirTripleQuoted(lines []string, startIdx int, rest, quote string) (string, int, bool) {
	content := strings.TrimPrefix(rest, quote)
	if idx := strings.Index(content, quote); idx >= 0 {
		return strings.TrimSpace(content[:idx]), startIdx, true
	}
	parts := []string{content}
	for i := startIdx + 1; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		if idx := strings.Index(line, quote); idx >= 0 {
			parts = append(parts, line[:idx])
			return strings.TrimSpace(strings.Join(parts, "\n")), i, true
		}
		parts = append(parts, line)
	}
	return strings.TrimSpace(strings.Join(parts, "\n")), len(lines) - 1, true
}

var (
	elixirDepLinePattern   = regexp.MustCompile(`^\s*(?:alias|import|require|use|@behaviour)\s+(.+)$`)
	elixirRemoteCallModule = regexp.MustCompile(`\b([A-Z][A-Za-z0-9_]*(?:\.[A-Z][A-Za-z0-9_]*)*)\s*\.`)
	elixirStructModule     = regexp.MustCompile(`%\s*([A-Z][A-Za-z0-9_]*(?:\.[A-Z][A-Za-z0-9_]*)*)\s*\{`)
	elixirLocalCallPattern = regexp.MustCompile(`\b([a-z_][a-z0-9_?!]*)\s*\(`)
)

func extractElixirModuleRefs(body string) []string {
	if strings.TrimSpace(body) == "" {
		return nil
	}

	seen := make(map[string]bool)
	out := make([]string, 0, 16)

	emit := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}

	// 1) Line-based dependency declarations (alias/import/require/use/@behaviour).
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		line = stripElixirLineComment(line)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		match := elixirDepLinePattern.FindStringSubmatch(trimmed)
		if match == nil {
			continue
		}
		for _, mod := range parseElixirModuleExpr(match[1]) {
			emit(mod)
		}
	}

	// 2) Remote calls / module-qualified accesses: Foo.Bar.baz(...)
	for _, match := range elixirRemoteCallModule.FindAllStringSubmatch(body, -1) {
		emit(match[1])
	}

	// 3) Struct literals: %Foo.Bar{...}
	for _, match := range elixirStructModule.FindAllStringSubmatch(body, -1) {
		emit(match[1])
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeElixirRefs(groups ...[]string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, 16)
	for _, group := range groups {
		for _, item := range group {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func extractElixirLocalCallRefs(body string) []string {
	if strings.TrimSpace(body) == "" {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, 16)
	for _, match := range elixirLocalCallPattern.FindAllStringSubmatch(body, -1) {
		if len(match) < 2 {
			continue
		}
		name := strings.TrimSpace(match[1])
		if !isElixirLocalCallCandidate(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func isElixirLocalCallCandidate(name string) bool {
	switch strings.TrimSpace(name) {
	case "", "def", "defp", "defmacro", "defmacrop", "defmodule", "defprotocol", "defimpl",
		"if", "unless", "case", "cond", "with", "for", "quote", "unquote", "alias",
		"import", "require", "use", "raise", "try", "catch", "rescue", "receive", "super":
		return false
	default:
		return true
	}
}

func stripElixirLineComment(line string) string {
	if line == "" {
		return ""
	}
	// Best-effort: ignore # inside quotes (common in doctests). Keep it simple.
	if idx := strings.Index(line, "#"); idx >= 0 {
		return line[:idx]
	}
	return line
}

func parseElixirModuleExpr(expr string) []string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil
	}
	// Drop keyword/options, e.g. "Foo.Bar, as: Baz" or "Foo.Bar, warn: false".
	if idx := strings.Index(expr, ","); idx >= 0 {
		expr = strings.TrimSpace(expr[:idx])
	}

	// Handle brace expansion: MyApp.{Foo, Bar}
	if open := strings.Index(expr, "{"); open >= 0 {
		close := strings.Index(expr, "}")
		if close > open {
			prefix := strings.TrimSpace(strings.TrimSuffix(expr[:open], "."))
			list := expr[open+1 : close]
			parts := strings.Split(list, ",")
			out := make([]string, 0, len(parts))
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				if prefix != "" {
					out = append(out, prefix+"."+part)
				} else {
					out = append(out, part)
				}
			}
			return out
		}
	}

	return []string{expr}
}

// parseElixirDeclaration parses a line and extracts symbol info if it's a declaration.
// Returns (name, kind, signature, ok).
func parseElixirDeclaration(line string) (string, Kind, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", "", false
	}

	signature := trimmed

	// Check for module definition
	if match := elixirModulePattern.FindStringSubmatch(line); match != nil {
		return match[1], KindClass, signature, true
	}

	// Check for public function definition
	if match := elixirDefPattern.FindStringSubmatch(line); match != nil {
		return match[1], KindFunction, signature, true
	}

	// Check for private function definition
	if match := elixirDefpPattern.FindStringSubmatch(line); match != nil {
		return match[1], KindFunction, signature, true
	}

	// Check for macro definition
	if match := elixirDefmacroPattern.FindStringSubmatch(line); match != nil {
		return match[1], KindFunction, signature, true
	}

	// Check for private macro definition
	if match := elixirDefmacropPattern.FindStringSubmatch(line); match != nil {
		return match[1], KindFunction, signature, true
	}

	// Check for type definition
	if match := elixirTypePattern.FindStringSubmatch(line); match != nil {
		return match[1], KindType, signature, true
	}

	// Check for private type definition
	if match := elixirTypepPattern.FindStringSubmatch(line); match != nil {
		return match[1], KindType, signature, true
	}

	// Check for callback definition
	if match := elixirCallbackPattern.FindStringSubmatch(line); match != nil {
		return match[1], KindInterface, signature, true
	}

	return "", "", "", false
}

// findElixirBlockEnd finds the line index of the matching 'end' for a block starting at startIdx.
// Elixir uses do...end blocks with nesting.
func findElixirBlockEnd(lines []string, startIdx int) int {
	depth := 0

	for i := startIdx; i < len(lines); i++ {
		line := lines[i]

		// Count block starters on this line
		// Look for: do, fn -> (anonymous function)
		// Avoid counting 'do:' as block start (it's single-line syntax)
		if !strings.Contains(line, ", do:") && !strings.Contains(line, "do:") {
			// Count various block starters
			depth += countElixirBlockStarts(line)
		}

		// Count 'end' keywords
		if elixirBlockEnd.MatchString(line) {
			depth--
			if depth <= 0 {
				return i
			}
		}
	}

	// No matching end found, return last line
	return len(lines) - 1
}

// countElixirBlockStarts counts block-starting keywords in a line.
func countElixirBlockStarts(line string) int {
	count := 0

	// Count standalone 'do' at end of line or followed by newline
	if strings.HasSuffix(strings.TrimSpace(line), "do") {
		count++
	}

	// Count 'fn' anonymous function starts (fn -> or fn x ->)
	fnCount := strings.Count(line, " fn ")
	fnCount += strings.Count(line, "(fn ")
	fnCount += strings.Count(line, ",fn ")
	if strings.HasPrefix(strings.TrimSpace(line), "fn ") {
		fnCount++
	}
	count += fnCount

	// Count case/cond/if/unless/with blocks that have 'do'
	// These are covered by the 'do' check above

	return count
}
