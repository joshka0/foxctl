package symbol

import (
	"context"
	"regexp"
	"strings"
)

var csharpCallPattern = regexp.MustCompile(`(?:new\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*(?:<[^>]+>)?\s*\(`)

// CSharpExtractor extracts C# symbols.
type CSharpExtractor struct{}

// NewCSharpExtractor creates a C# symbol extractor.
func NewCSharpExtractor() *CSharpExtractor { return &CSharpExtractor{} }

// SupportedLanguages returns ["csharp"].
func (e *CSharpExtractor) SupportedLanguages() []string { return []string{"csharp"} }

// Extract parses C# symbols from content.
func (e *CSharpExtractor) Extract(ctx context.Context, filePath string, content []byte) ([]Symbol, error) {
	if syms, ok, err := extractCSharpSymbolsWithTreeSitter(ctx, filePath, content); ok || err != nil {
		return syms, err
	}
	return extractCSharpSymbolsRegex(filePath, content), nil
}

// ExtractCalls extracts best-effort C# invocation names from a symbol body.
func (e *CSharpExtractor) ExtractCalls(ctx context.Context, sym Symbol, content []byte) ([]string, error) {
	if calls, ok, err := extractCSharpCallsWithTreeSitter(ctx, sym, content); ok || err != nil {
		return calls, err
	}
	body, ok := extractSymbolBodyBytes(sym, content)
	if !ok {
		return nil, nil
	}
	return extractCSharpCallsRegex(string(body)), nil
}

func extractCSharpSymbolsRegex(filePath string, content []byte) []Symbol {
	lines := strings.Split(string(content), "\n")
	lineOffsets := computeLineOffsets(lines)
	out := make([]Symbol, 0, 8)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		name, kind := csharpRegexDeclaration(trimmed)
		if name == "" {
			continue
		}
		startByte := lineOffsets[i]
		endLine := csharpRegexEndLine(lines, i)
		endByte := len(content)
		if endLine-1 >= 0 && endLine-1 < len(lineOffsets) {
			endByte = lineOffsets[endLine-1] + len(lines[endLine-1])
		}
		if endByte <= startByte || endByte > len(content) {
			endByte = len(content)
		}
		out = append(out, Symbol{
			ID:            ID(filePath, name),
			FilePath:      filePath,
			Name:          name,
			Language:      "csharp",
			Kind:          kind,
			StartByte:     startByte,
			EndByte:       endByte,
			StartLine:     i + 1,
			EndLine:       endLine,
			Signature:     trimmed,
			BodyDigest:    ComputeDigest(content[startByte:endByte]),
			Documentation: extractCSharpLeadingDoc(lines, i),
		})
	}
	return out
}

func csharpRegexDeclaration(line string) (string, Kind) {
	fields := strings.Fields(line)
	for i, field := range fields {
		switch field {
		case "class":
			return csharpCleanIdent(nextField(fields, i+1)), KindClass
		case "interface":
			return csharpCleanIdent(nextField(fields, i+1)), KindInterface
		case "struct", "record", "enum":
			return csharpCleanIdent(nextField(fields, i+1)), KindType
		}
	}
	if strings.Contains(line, "(") && strings.Contains(line, ")") && !strings.HasPrefix(line, "if ") && !strings.HasPrefix(line, "for ") && !strings.HasPrefix(line, "while ") && !strings.HasPrefix(line, "switch ") {
		before := strings.TrimSpace(line[:strings.Index(line, "(")])
		parts := strings.Fields(before)
		if len(parts) > 0 {
			return csharpCleanIdent(parts[len(parts)-1]), KindMethod
		}
	}
	return "", ""
}

func nextField(fields []string, idx int) string {
	if idx < 0 || idx >= len(fields) {
		return ""
	}
	return fields[idx]
}

func csharpCleanIdent(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "{}();:,")
	if idx := strings.IndexAny(value, "<("); idx >= 0 {
		value = value[:idx]
	}
	return strings.TrimSpace(value)
}

func csharpRegexEndLine(lines []string, start int) int {
	depth := 0
	seenBrace := false
	for i := start; i < len(lines); i++ {
		for _, r := range lines[i] {
			switch r {
			case '{':
				depth++
				seenBrace = true
			case '}':
				depth--
				if seenBrace && depth <= 0 {
					return i + 1
				}
			case ';':
				if !seenBrace {
					return i + 1
				}
			}
		}
	}
	return len(lines)
}

func extractCSharpCallsRegex(body string) []string {
	matches := csharpCallPattern.FindAllStringSubmatch(body, -1)
	seen := map[string]bool{}
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		name := strings.TrimSpace(match[1])
		if name == "" || csharpControlKeyword(name) || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
		if len(out) >= 50 {
			break
		}
	}
	return out
}

func csharpControlKeyword(name string) bool {
	switch name {
	case "if", "for", "foreach", "while", "switch", "using", "lock", "catch":
		return true
	default:
		return false
	}
}

func extractCSharpLeadingDoc(lines []string, defLineIdx int) string {
	if defLineIdx <= 0 || defLineIdx > len(lines) {
		return ""
	}
	start := defLineIdx
	for start > 0 {
		line := strings.TrimSpace(lines[start-1])
		if strings.HasPrefix(line, "///") {
			start--
			continue
		}
		break
	}
	if start == defLineIdx {
		return ""
	}
	var b strings.Builder
	for i := start; i < defLineIdx; i++ {
		line := strings.TrimSpace(lines[i])
		line = strings.TrimSpace(strings.TrimPrefix(line, "///"))
		line = strings.TrimSpace(strings.TrimPrefix(line, "<summary>"))
		line = strings.TrimSpace(strings.TrimPrefix(line, "</summary>"))
		if line == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	return b.String()
}
