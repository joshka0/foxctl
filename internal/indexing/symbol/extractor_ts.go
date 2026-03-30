package symbol

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/jkatigb/agentctl/internal/codecontext/expander"
)

// TypeScriptExtractor extracts symbols from TypeScript sources using line-based heuristics.
type TypeScriptExtractor struct{}

// NewTypeScriptExtractor creates a new TypeScript extractor.
func NewTypeScriptExtractor() *TypeScriptExtractor {
	return &TypeScriptExtractor{}
}

// SupportedLanguages returns ["typescript", "javascript"].
func (e *TypeScriptExtractor) SupportedLanguages() []string {
	return []string{"typescript", "javascript"}
}

// Extract parses TypeScript source and returns top-level symbols.
func (e *TypeScriptExtractor) Extract(ctx context.Context, filePath string, content []byte) ([]Symbol, error) {
	if symbols, ok, err := extractTypeScriptSymbolsWithTreeSitter(ctx, filePath, content); ok || err != nil {
		return symbols, err
	}
	return e.extractHeuristic(filePath, content)
}

func (e *TypeScriptExtractor) extractHeuristic(filePath string, content []byte) ([]Symbol, error) {
	lines := strings.Split(string(content), "\n")
	lineOffsets := computeLineOffsets(lines)
	lang := "typescript"
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".js", ".jsx", ".mjs", ".cjs":
		lang = "javascript"
	}

	var symbols []Symbol
	for i, line := range lines {
		name, kind, signature, _, ok := parseTSDeclaration(line)
		if !ok {
			continue
		}

		startLine := i + 1
		endLine := startLine
		if kind == KindFunction || kind == KindClass || kind == KindInterface || kind == KindType || tsVariableLooksCallable(lines, i, kind) {
			braceLine := i
			if !strings.Contains(line, "{") {
				braceLine = findTSBraceLine(lines, i)
			}
			if braceLine >= 0 {
				endIdx := expander.FindBraceEnd(lines, braceLine, expander.JSBraceStyle())
				if endIdx >= braceLine {
					endLine = endIdx + 1
				}
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
		doc := extractTSLeadingDoc(lines, i)

		symbols = append(symbols, Symbol{
			ID:            ID(filePath, name),
			FilePath:      filePath,
			Name:          name,
			Language:      lang,
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

// ExtractCalls extracts best-effort call identifiers from a symbol body.
// This is heuristic and intentionally conservative (no type-checking).
func (e *TypeScriptExtractor) ExtractCalls(ctx context.Context, symbol Symbol, content []byte) ([]string, error) {
	if calls, ok, err := extractTypeScriptCallsWithTreeSitter(ctx, symbol, content); ok || err != nil {
		return calls, err
	}
	if symbol.StartByte < 0 || symbol.EndByte > len(content) || symbol.StartByte >= symbol.EndByte {
		return nil, nil
	}
	body := string(content[symbol.StartByte:symbol.EndByte])
	calls := extractTSCallNames(body)
	if len(calls) > 50 {
		calls = calls[:50]
	}
	return calls, nil
}

var identPattern = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*`)

func parseTSDeclaration(line string) (string, Kind, string, bool, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "//") {
		return "", "", "", false, false
	}

	signature := strings.TrimSpace(trimmed)
	exported := false
	defaultExport := false

	if strings.HasPrefix(trimmed, "export ") {
		exported = true
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
		if strings.HasPrefix(trimmed, "default ") {
			defaultExport = true
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "default "))
		}
	}

	trimmed = strings.TrimPrefix(trimmed, "declare ")
	trimmed = strings.TrimPrefix(trimmed, "abstract ")
	trimmed = strings.TrimPrefix(trimmed, "async ")
	trimmed = strings.TrimSpace(trimmed)

	switch {
	case strings.HasPrefix(trimmed, "function "):
		name := extractName(strings.TrimPrefix(trimmed, "function "))
		if name == "" && defaultExport {
			name = "default"
		}
		return name, KindFunction, signature, exported, name != ""
	case strings.HasPrefix(trimmed, "function("):
		name := extractName(strings.TrimPrefix(trimmed, "function"))
		if name == "" && defaultExport {
			name = "default"
		}
		return name, KindFunction, signature, exported, name != ""
	case strings.HasPrefix(trimmed, "class "):
		name := extractName(strings.TrimPrefix(trimmed, "class "))
		if name == "" && defaultExport {
			name = "default"
		}
		return name, KindClass, signature, exported, name != ""
	case strings.HasPrefix(trimmed, "interface "):
		name := extractName(strings.TrimPrefix(trimmed, "interface "))
		return name, KindInterface, signature, exported, name != ""
	case strings.HasPrefix(trimmed, "type "):
		name := extractName(strings.TrimPrefix(trimmed, "type "))
		return name, KindType, signature, exported, name != ""
	case strings.HasPrefix(trimmed, "enum "):
		name := extractName(strings.TrimPrefix(trimmed, "enum "))
		return name, KindType, signature, exported, name != ""
	case strings.HasPrefix(trimmed, "const "):
		name := extractName(strings.TrimPrefix(trimmed, "const "))
		return name, KindConstant, signature, exported, name != ""
	case strings.HasPrefix(trimmed, "let "):
		name := extractName(strings.TrimPrefix(trimmed, "let "))
		return name, KindVariable, signature, exported, name != ""
	case strings.HasPrefix(trimmed, "var "):
		name := extractName(strings.TrimPrefix(trimmed, "var "))
		return name, KindVariable, signature, exported, name != ""
	default:
		return "", "", "", false, false
	}
}

func extractName(input string) string {
	input = strings.TrimSpace(input)
	match := identPattern.FindString(input)
	return match
}

func tsVariableLooksCallable(lines []string, declIdx int, kind Kind) bool {
	if declIdx < 0 || declIdx >= len(lines) {
		return false
	}
	if kind != KindConstant && kind != KindVariable {
		return false
	}
	windowEnd := declIdx + 5
	if windowEnd > len(lines) {
		windowEnd = len(lines)
	}
	window := strings.Join(lines[declIdx:windowEnd], "\n")
	return strings.Contains(window, "=>") || strings.Contains(window, "function(") || strings.Contains(window, "function ")
}

func extractTSLeadingDoc(lines []string, declIdx int) string {
	if declIdx <= 0 {
		return ""
	}
	prev := strings.TrimSpace(strings.TrimRight(lines[declIdx-1], "\r"))
	if prev == "" {
		return ""
	}
	if strings.HasPrefix(prev, "//") {
		return extractTSLineCommentBlock(lines, declIdx-1)
	}
	if strings.Contains(prev, "*/") {
		return extractTSBlockComment(lines, declIdx-1)
	}
	return ""
}

func extractTSLineCommentBlock(lines []string, endIdx int) string {
	start := endIdx
	for start >= 0 {
		trimmed := strings.TrimSpace(strings.TrimRight(lines[start], "\r"))
		if strings.HasPrefix(trimmed, "//") {
			start--
			continue
		}
		break
	}
	start++

	var docLines []string
	for i := start; i <= endIdx; i++ {
		trimmed := strings.TrimSpace(strings.TrimRight(lines[i], "\r"))
		trimmed = strings.TrimPrefix(trimmed, "//")
		trimmed = strings.TrimSpace(trimmed)
		docLines = append(docLines, trimmed)
	}
	return strings.TrimSpace(strings.Join(docLines, "\n"))
}

func extractTSBlockComment(lines []string, endIdx int) string {
	start := -1
	for i := endIdx; i >= 0; i-- {
		trimmed := strings.TrimSpace(strings.TrimRight(lines[i], "\r"))
		if strings.Contains(trimmed, "/**") {
			start = i
			break
		}
		if strings.Contains(trimmed, "/*") && !strings.Contains(trimmed, "/**") {
			return ""
		}
	}
	if start == -1 {
		return ""
	}

	var docLines []string
	for i := start; i <= endIdx; i++ {
		line := strings.TrimSpace(strings.TrimRight(lines[i], "\r"))
		line = strings.TrimPrefix(line, "/**")
		line = strings.TrimPrefix(line, "/*")
		line = strings.TrimSuffix(line, "*/")
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "*") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		}
		line = strings.TrimSuffix(line, "*/")
		line = strings.TrimSpace(line)
		docLines = append(docLines, line)
	}
	return strings.TrimSpace(strings.Join(docLines, "\n"))
}

var tsCallKeywords = map[string]struct{}{
	// Statements/keywords that can be followed by "(" but aren't calls.
	"if": {}, "for": {}, "while": {}, "switch": {}, "catch": {}, "with": {},
	"return": {}, "throw": {}, "new": {}, "await": {}, "yield": {},
	"function": {}, "class": {}, "interface": {}, "type": {}, "enum": {},
	"import": {}, "export": {}, "extends": {}, "implements": {}, "super": {},
	"try": {}, "finally": {}, "case": {}, "default": {}, "do": {}, "else": {},
	"in": {}, "of": {},
}

func findTSBraceLine(lines []string, declIdx int) int {
	if declIdx < 0 || declIdx >= len(lines) {
		return -1
	}
	// Fast path: decl line already has brace.
	if strings.Contains(lines[declIdx], "{") {
		return declIdx
	}

	const maxLookahead = 32
	for j := declIdx + 1; j < len(lines) && j <= declIdx+maxLookahead; j++ {
		raw := lines[j]
		trimmed := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}

		// Stop if we hit another top-level declaration before seeing a brace.
		if leadingIndentTS(raw) == 0 {
			if _, _, _, _, ok := parseTSDeclaration(raw); ok {
				break
			}
		}

		if strings.Contains(trimmed, "{") {
			return j
		}

		// Stop at statement terminator (declarations without bodies).
		if strings.HasSuffix(trimmed, ";") {
			break
		}
	}
	return -1
}

func extractTSCallNames(body string) []string {
	if strings.TrimSpace(body) == "" {
		return nil
	}

	seen := make(map[string]bool)
	out := make([]string, 0, 16)

	prevIdentLower := ""
	for i := 0; i < len(body); {
		c := body[i]

		// Comments
		if c == '/' && i+1 < len(body) && body[i+1] == '/' {
			i += 2
			for i < len(body) && body[i] != '\n' {
				i++
			}
			prevIdentLower = ""
			continue
		}
		if c == '/' && i+1 < len(body) && body[i+1] == '*' {
			i += 2
			for i+1 < len(body) {
				if body[i] == '*' && body[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			prevIdentLower = ""
			continue
		}

		// Strings
		if c == '\'' || c == '"' || c == '`' {
			quote := c
			i++
			for i < len(body) {
				if body[i] == '\\' {
					i += 2
					continue
				}
				if body[i] == quote {
					i++
					break
				}
				i++
			}
			prevIdentLower = ""
			continue
		}

		if !isTSIdentStart(rune(c)) {
			i++
			continue
		}

		// Identifier token
		start := i
		i++
		for i < len(body) && isTSIdentPart(rune(body[i])) {
			i++
		}
		ident := body[start:i]
		identLower := strings.ToLower(ident)

		// Lookahead for call site: ident [<typeArgs>] "("
		j := i
		for j < len(body) && isTSWhitespace(body[j]) {
			j++
		}
		if j < len(body) && body[j] == '<' {
			if after := skipTSTypeArgs(body, j); after > j {
				j = after
				for j < len(body) && isTSWhitespace(body[j]) {
					j++
				}
			}
		}
		isCall := j < len(body) && body[j] == '('

		if isCall {
			// Exclude obvious non-call keywords and function declarations.
			if _, ok := tsCallKeywords[identLower]; !ok && prevIdentLower != "function" {
				if !seen[ident] {
					seen[ident] = true
					out = append(out, ident)
				}
			}
		}

		prevIdentLower = identLower
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

func isTSIdentStart(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r)
}

func isTSIdentPart(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func isTSWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// skipTSTypeArgs tries to skip a TypeScript generic type argument list starting at '<'.
// Returns the first index after the matching '>' if it looks like type args; otherwise 0.
func skipTSTypeArgs(body string, start int) int {
	if start < 0 || start >= len(body) || body[start] != '<' {
		return 0
	}
	depth := 0
	for i := start; i < len(body); i++ {
		switch body[i] {
		case '<':
			depth++
		case '>':
			depth--
			if depth == 0 {
				return i + 1
			}
		case '\n':
			// Heuristic: if type args span many lines, bail out.
			if i-start > 200 {
				return 0
			}
		}
		// Avoid pathological scans.
		if i-start > 400 {
			return 0
		}
	}
	return 0
}

func leadingIndentTS(line string) int {
	count := 0
	for i := 0; i < len(line); i++ {
		if line[i] == ' ' || line[i] == '\t' {
			count++
			continue
		}
		break
	}
	return count
}

func computeLineOffsets(lines []string) []int {
	offsets := make([]int, len(lines))
	offset := 0
	for i, line := range lines {
		offsets[i] = offset
		offset += len(line) + 1
	}
	if len(lines) == 0 {
		return []int{0}
	}
	return offsets
}
