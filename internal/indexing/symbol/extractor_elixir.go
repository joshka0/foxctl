package symbol

import (
	"context"
	"regexp"
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
	// defmodule ModuleName do
	elixirModulePattern = regexp.MustCompile(`^\s*defmodule\s+([A-Z][A-Za-z0-9_.]*)\s+do`)

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
func (e *ElixirExtractor) Extract(_ context.Context, filePath string, content []byte) ([]Symbol, error) {
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

// ExtractCalls is not supported yet for Elixir.
func (e *ElixirExtractor) ExtractCalls(_ context.Context, _ Symbol, _ []byte) ([]string, error) {
	return nil, nil
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
