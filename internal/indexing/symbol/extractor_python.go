package symbol

import (
	"context"
	"regexp"
	"strings"
)

var (
	pythonIdentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*`)
	pythonCallPattern  = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)(?:\.([A-Za-z_][A-Za-z0-9_]*))?\s*\(`)
)

// PythonExtractor extracts symbols from Python sources using line-based heuristics.
type PythonExtractor struct{}

// NewPythonExtractor creates a new Python extractor.
func NewPythonExtractor() *PythonExtractor {
	return &PythonExtractor{}
}

// SupportedLanguages returns ["python"].
func (e *PythonExtractor) SupportedLanguages() []string {
	return []string{"python"}
}

// Extract parses Python source and returns top-level symbols.
func (e *PythonExtractor) Extract(ctx context.Context, filePath string, content []byte) ([]Symbol, error) {
	if symbols, ok, err := extractPythonSymbolsWithTreeSitter(ctx, filePath, content); ok || err != nil {
		return symbols, err
	}
	return e.extractHeuristic(filePath, content)
}

func (e *PythonExtractor) extractHeuristic(filePath string, content []byte) ([]Symbol, error) {
	lines := strings.Split(string(content), "\n")
	lineOffsets := computeLineOffsets(lines)

	var symbols []Symbol
	for i, line := range lines {
		name, kind, signature, ok := parsePythonDeclaration(line)
		if !ok {
			continue
		}

		startLine := i + 1
		endLine := findPythonBlockEnd(lines, i)

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
		doc := extractPythonDocstring(lines, i)

		symbols = append(symbols, Symbol{
			ID:            ID(filePath, name),
			FilePath:      filePath,
			Name:          name,
			Language:      "python",
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
func (e *PythonExtractor) ExtractCalls(ctx context.Context, symbol Symbol, content []byte) ([]string, error) {
	if calls, ok, err := extractPythonCallsWithTreeSitter(ctx, symbol, content); ok || err != nil {
		return calls, err
	}
	body := extractSymbolBody(content, symbol)
	if strings.TrimSpace(body) == "" {
		return nil, nil
	}
	return extractPythonCallNames(body), nil
}

func parsePythonDeclaration(line string) (string, Kind, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", "", false
	}
	if leadingIndent(line) != 0 {
		return "", "", "", false
	}

	signature := trimmed
	if strings.HasPrefix(trimmed, "async def ") {
		trimmed = strings.TrimPrefix(trimmed, "async ")
	}
	if strings.HasPrefix(trimmed, "def ") {
		name := extractPythonName(strings.TrimPrefix(trimmed, "def "))
		return name, KindFunction, signature, name != ""
	}
	if strings.HasPrefix(trimmed, "class ") {
		name := extractPythonName(strings.TrimPrefix(trimmed, "class "))
		return name, KindClass, signature, name != ""
	}
	return "", "", "", false
}

func extractPythonName(input string) string {
	input = strings.TrimSpace(input)
	match := pythonIdentPattern.FindString(input)
	return match
}

func findPythonBlockEnd(lines []string, startIdx int) int {
	startIndent := leadingIndent(lines[startIdx])
	for i := startIdx + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		if leadingIndent(lines[i]) <= startIndent {
			return i
		}
	}
	return len(lines)
}

func extractPythonDocstring(lines []string, startIdx int) string {
	startIndent := leadingIndent(lines[startIdx])
	for i := startIdx + 1; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if leadingIndent(line) <= startIndent {
				return ""
			}
			continue
		}
		if leadingIndent(line) <= startIndent {
			return ""
		}
		doc, _, ok := parsePythonDocstring(lines, i)
		if ok {
			return doc
		}
		return ""
	}
	return ""
}

func parsePythonDocstring(lines []string, startIdx int) (string, int, bool) {
	line := strings.TrimSpace(lines[startIdx])
	quote, startPos := findPythonQuoteStart(line)
	if quote == "" {
		return "", startIdx, false
	}
	content := line[startPos+len(quote):]
	if quote == "\"\"\"" || quote == "'''" {
		if idx := strings.Index(content, quote); idx >= 0 {
			return strings.TrimSpace(content[:idx]), startIdx, true
		}
		parts := []string{content}
		for i := startIdx + 1; i < len(lines); i++ {
			segment := lines[i]
			if idx := strings.Index(segment, quote); idx >= 0 {
				parts = append(parts, segment[:idx])
				return strings.TrimSpace(strings.Join(parts, "\n")), i, true
			}
			parts = append(parts, segment)
		}
		return strings.TrimSpace(strings.Join(parts, "\n")), len(lines) - 1, true
	}

	if idx := strings.LastIndex(content, quote); idx >= 0 {
		return strings.TrimSpace(content[:idx]), startIdx, true
	}
	return "", startIdx, false
}

func findPythonQuoteStart(line string) (string, int) {
	i := 0
	for i < len(line) {
		c := line[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			i++
			continue
		}
		break
	}
	if strings.HasPrefix(line[i:], "\"\"\"") {
		return "\"\"\"", i
	}
	if strings.HasPrefix(line[i:], "'''") {
		return "'''", i
	}
	if strings.HasPrefix(line[i:], "\"") {
		return "\"", i
	}
	if strings.HasPrefix(line[i:], "'") {
		return "'", i
	}
	return "", 0
}

func leadingIndent(line string) int {
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

func extractPythonCallNames(body string) []string {
	matches := pythonCallPattern.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		name := strings.TrimSpace(match[1])
		if strings.TrimSpace(match[2]) != "" {
			name = strings.TrimSpace(match[2])
		}
		switch name {
		case "", "def", "class", "if", "for", "while", "return", "with":
			continue
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
