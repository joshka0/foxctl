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
	container := ""
	containerDepth := -1
	pendingContainer := ""
	braceDepth := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			braceDepth += csharpBraceDelta(line)
			continue
		}
		if pendingContainer != "" && strings.Contains(line, "{") {
			container = pendingContainer
			containerDepth = braceDepth + 1
			pendingContainer = ""
		}
		name, kind := csharpRegexDeclaration(trimmed)
		if name != "" {
			symbolName := name
			if container != "" && (kind == KindMethod || kind == KindVariable) && !strings.Contains(name, ".") {
				symbolName = container + "." + name
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
				ID:            ID(filePath, symbolName),
				FilePath:      filePath,
				Name:          symbolName,
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
			if csharpContainerKind(kind) {
				if strings.Contains(line, "{") {
					container = name
					containerDepth = braceDepth + 1
				} else {
					pendingContainer = name
				}
			}
		}
		braceDepth += csharpBraceDelta(line)
		if containerDepth >= 0 && braceDepth < containerDepth {
			container = ""
			containerDepth = -1
		}
	}
	return out
}

func csharpRegexDeclaration(line string) (string, Kind) {
	if csharpStatementKeyword(line) {
		return "", ""
	}
	fields := strings.Fields(line)
	for i, field := range fields {
		switch field {
		case "class":
			return csharpCleanIdent(nextField(fields, i+1)), KindClass
		case "interface":
			return csharpCleanIdent(nextField(fields, i+1)), KindInterface
		case "struct", "enum":
			return csharpCleanIdent(nextField(fields, i+1)), KindType
		case "record":
			next := nextField(fields, i+1)
			if next == "class" || next == "struct" {
				next = nextField(fields, i+2)
			}
			return csharpCleanIdent(next), KindType
		}
	}
	if name, ok := csharpPropertyDeclaration(line); ok {
		return name, KindVariable
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

func csharpStatementKeyword(line string) bool {
	for _, prefix := range []string{"return ", "throw ", "if ", "for ", "foreach ", "while ", "switch ", "catch ", "using "} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func csharpPropertyDeclaration(line string) (string, bool) {
	if !strings.Contains(line, "{") || !strings.Contains(line, "}") || !(strings.Contains(line, " get;") || strings.Contains(line, " set;")) {
		return "", false
	}
	before := strings.TrimSpace(line[:strings.Index(line, "{")])
	parts := strings.Fields(before)
	if len(parts) == 0 {
		return "", false
	}
	name := csharpCleanIdent(parts[len(parts)-1])
	return name, name != ""
}

func csharpContainerKind(kind Kind) bool {
	switch kind {
	case KindClass, KindInterface, KindType:
		return true
	default:
		return false
	}
}

func csharpBraceDelta(line string) int {
	delta := 0
	for _, r := range line {
		switch r {
		case '{':
			delta++
		case '}':
			delta--
		}
	}
	return delta
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
