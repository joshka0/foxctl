package symbol

import (
	"context"
	"regexp"
	"strings"
)

var (
	rustIdentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*`)
	rustTokenPattern = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
	rustCallPattern  = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*(?:!|\()`)
)

// RustExtractor extracts symbols from Rust sources using tree-sitter when
// available and heuristic fallback otherwise.
type RustExtractor struct{}

// NewRustExtractor creates a new Rust extractor.
func NewRustExtractor() *RustExtractor {
	return &RustExtractor{}
}

// SupportedLanguages returns ["rust"].
func (e *RustExtractor) SupportedLanguages() []string {
	return []string{"rust"}
}

// Extract parses Rust source and returns top-level symbols plus impl methods.
func (e *RustExtractor) Extract(ctx context.Context, filePath string, content []byte) ([]Symbol, error) {
	if tsSymbols, ok, err := extractRustSymbolsWithTreeSitter(ctx, filePath, content); err != nil {
		return nil, err
	} else if ok {
		heuristic, hErr := e.extractHeuristic(filePath, content)
		if hErr != nil {
			return tsSymbols, nil
		}
		return mergeRustSymbols(tsSymbols, heuristic), nil
	}
	return e.extractHeuristic(filePath, content)
}

func (e *RustExtractor) extractHeuristic(filePath string, content []byte) ([]Symbol, error) {
	lines := strings.Split(string(content), "\n")
	lineOffsets := computeLineOffsets(lines)

	var symbols []Symbol
	braceDepth := 0
	currentImplType := ""
	currentImplDepth := -1

	for i, rawLine := range lines {
		line := stripRustLineComment(rawLine)
		trimmed := strings.TrimSpace(line)
		lineDepth := braceDepth

		if lineDepth == 0 {
			if implType, ok := parseRustImplHeader(trimmed); ok {
				currentImplType = implType
				currentImplDepth = lineDepth
			} else if name, kind, signature, blockLike, ok := parseRustDeclaration(trimmed); ok {
				symbols = append(symbols, buildRustHeuristicSymbol(filePath, content, lines, lineOffsets, i, name, kind, signature, blockLike)...)
			}
		} else if currentImplType != "" && lineDepth == currentImplDepth+1 {
			if name, signature, blockLike, ok := parseRustMethodDeclaration(trimmed, currentImplType); ok {
				symbols = append(symbols, buildRustHeuristicSymbol(filePath, content, lines, lineOffsets, i, name, KindMethod, signature, blockLike)...)
			}
		}

		braceDepth += rustBraceDelta(line)
		if currentImplType != "" && braceDepth <= currentImplDepth {
			currentImplType = ""
			currentImplDepth = -1
		}
	}

	return symbols, nil
}

// ExtractCalls extracts best-effort Rust call names from a symbol body.
func (e *RustExtractor) ExtractCalls(ctx context.Context, symbol Symbol, content []byte) ([]string, error) {
	var tsCalls []string
	if calls, ok, err := extractRustCallsWithTreeSitter(ctx, symbol, content); err != nil {
		return nil, err
	} else if ok {
		tsCalls = calls
	}
	body := extractSymbolBody(content, symbol)
	if strings.TrimSpace(body) == "" {
		return tsCalls, nil
	}
	return mergeRustNames(tsCalls, extractRustCallNames(body)), nil
}

func mergeRustSymbols(primary, fallback []Symbol) []Symbol {
	if len(primary) == 0 {
		return fallback
	}
	if len(fallback) == 0 {
		return primary
	}
	seen := make(map[string]bool, len(primary))
	out := make([]Symbol, 0, len(primary)+len(fallback))
	for _, sym := range primary {
		key := strings.TrimSpace(sym.Name)
		if key == "" {
			continue
		}
		seen[key] = true
		out = append(out, sym)
	}
	for _, sym := range fallback {
		key := strings.TrimSpace(sym.Name)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, sym)
	}
	return out
}

func mergeRustNames(primary, fallback []string) []string {
	if len(primary) == 0 {
		return fallback
	}
	if len(fallback) == 0 {
		return primary
	}
	seen := make(map[string]bool, len(primary))
	out := make([]string, 0, len(primary)+len(fallback))
	for _, name := range primary {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, name := range fallback {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func buildRustHeuristicSymbol(filePath string, content []byte, lines []string, lineOffsets []int, startIdx int, name string, kind Kind, signature string, blockLike bool) []Symbol {
	if strings.TrimSpace(name) == "" {
		return nil
	}

	startLine := startIdx + 1
	endLine := startLine
	if blockLike {
		endIdx := findRustBlockEnd(lines, startIdx)
		if endIdx >= startIdx {
			endLine = endIdx + 1
		}
	}

	startByte := lineOffsets[startIdx]
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
	doc := extractRustLeadingDoc(lines, startIdx)

	return []Symbol{{
		ID:            ID(filePath, name),
		FilePath:      filePath,
		Name:          name,
		Language:      "rust",
		Kind:          kind,
		StartByte:     startByte,
		EndByte:       endByte,
		StartLine:     startLine,
		EndLine:       endLine,
		Signature:     strings.TrimSpace(signature),
		BodyDigest:    ComputeDigest(body),
		Documentation: strings.TrimSpace(doc),
	}}
}

func parseRustDeclaration(line string) (name string, kind Kind, signature string, blockLike bool, ok bool) {
	if line == "" {
		return "", "", "", false, false
	}
	signature = strings.TrimSpace(line)
	trimmed := trimRustDeclPrefixes(signature)
	switch {
	case strings.HasPrefix(trimmed, "fn "):
		name = extractRustName(strings.TrimPrefix(trimmed, "fn "))
		return name, KindFunction, signature, true, name != ""
	case strings.HasPrefix(trimmed, "struct "):
		name = extractRustName(strings.TrimPrefix(trimmed, "struct "))
		return name, KindStruct, signature, true, name != ""
	case strings.HasPrefix(trimmed, "enum "):
		name = extractRustName(strings.TrimPrefix(trimmed, "enum "))
		return name, KindType, signature, true, name != ""
	case strings.HasPrefix(trimmed, "trait "):
		name = extractRustName(strings.TrimPrefix(trimmed, "trait "))
		return name, KindInterface, signature, true, name != ""
	case strings.HasPrefix(trimmed, "type "):
		name = extractRustName(strings.TrimPrefix(trimmed, "type "))
		return name, KindType, signature, false, name != ""
	case strings.HasPrefix(trimmed, "const "):
		name = extractRustName(strings.TrimPrefix(trimmed, "const "))
		return name, KindConstant, signature, false, name != ""
	case strings.HasPrefix(trimmed, "static "):
		name = extractRustName(strings.TrimPrefix(trimmed, "static "))
		return name, KindVariable, signature, false, name != ""
	default:
		return "", "", "", false, false
	}
}

func parseRustMethodDeclaration(line, implType string) (name string, signature string, blockLike bool, ok bool) {
	signature = strings.TrimSpace(line)
	trimmed := trimRustDeclPrefixes(signature)
	if !strings.HasPrefix(trimmed, "fn ") {
		return "", "", false, false
	}
	methodName := extractRustName(strings.TrimPrefix(trimmed, "fn "))
	if methodName == "" {
		return "", "", false, false
	}
	return implType + "." + methodName, signature, true, true
}

func trimRustDeclPrefixes(line string) string {
	line = strings.TrimSpace(line)
	for {
		switch {
		case strings.HasPrefix(line, "pub("):
			if idx := strings.Index(line, ")"); idx >= 0 {
				line = strings.TrimSpace(line[idx+1:])
				continue
			}
		case strings.HasPrefix(line, "pub "):
			line = strings.TrimSpace(strings.TrimPrefix(line, "pub "))
			continue
		case strings.HasPrefix(line, "default "):
			line = strings.TrimSpace(strings.TrimPrefix(line, "default "))
			continue
		case strings.HasPrefix(line, "async "):
			line = strings.TrimSpace(strings.TrimPrefix(line, "async "))
			continue
		case strings.HasPrefix(line, "const "):
			// Keep `const fn` intact but drop standalone const modifier first.
			if strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(line, "const ")), "fn ") {
				line = strings.TrimSpace(strings.TrimPrefix(line, "const "))
				continue
			}
		case strings.HasPrefix(line, "unsafe "):
			line = strings.TrimSpace(strings.TrimPrefix(line, "unsafe "))
			continue
		case strings.HasPrefix(line, "extern "):
			line = strings.TrimSpace(strings.TrimPrefix(line, "extern "))
			continue
		}
		break
	}
	return line
}

func parseRustImplHeader(line string) (string, bool) {
	if line == "" {
		return "", false
	}
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "unsafe ") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "unsafe "))
	}
	if !strings.HasPrefix(trimmed, "impl") {
		return "", false
	}
	header := trimmed
	if idx := strings.Index(header, "{"); idx >= 0 {
		header = header[:idx]
	}
	header = strings.TrimSpace(strings.TrimPrefix(header, "impl"))
	if strings.HasPrefix(header, "<") {
		if end := findMatchingRune(header, 0, '<', '>'); end >= 0 {
			header = strings.TrimSpace(header[end+1:])
		}
	}
	if idx := strings.LastIndex(header, " for "); idx >= 0 {
		header = strings.TrimSpace(header[idx+5:])
	}
	name := rustLastIdentifier(header)
	return name, name != ""
}

func extractRustName(input string) string {
	input = strings.TrimSpace(input)
	return rustIdentPattern.FindString(input)
}

func rustLastIdentifier(value string) string {
	matches := rustTokenPattern.FindAllString(value, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		name := strings.TrimSpace(matches[i])
		switch name {
		case "", "impl", "for", "where", "dyn", "mut", "const", "async", "unsafe", "pub", "crate", "super", "self", "Self":
			continue
		default:
			return name
		}
	}
	return ""
}

func findMatchingRune(value string, start int, open, close rune) int {
	if start < 0 || start >= len(value) || rune(value[start]) != open {
		return -1
	}
	depth := 0
	for i, r := range value[start:] {
		switch r {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return start + i
			}
		}
	}
	return -1
}

func findRustBlockEnd(lines []string, startIdx int) int {
	depth := 0
	seenBrace := false
	for i := startIdx; i < len(lines); i++ {
		line := stripRustLineComment(lines[i])
		for _, r := range line {
			switch r {
			case '{':
				depth++
				seenBrace = true
			case '}':
				if depth > 0 {
					depth--
				}
				if seenBrace && depth == 0 {
					return i
				}
			}
		}
		if !seenBrace && strings.Contains(line, ";") {
			return i
		}
	}
	return len(lines) - 1
}

func extractRustLeadingDoc(lines []string, startIdx int) string {
	var parts []string
	for i := startIdx - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		switch {
		case strings.HasPrefix(trimmed, "///"), strings.HasPrefix(trimmed, "//!"):
			text := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, "///"), "//!"))
			parts = append(parts, text)
		case trimmed == "":
			if len(parts) == 0 {
				continue
			}
			goto done
		default:
			goto done
		}
	}
done:
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func stripRustLineComment(line string) string {
	if idx := strings.Index(line, "//"); idx >= 0 {
		return line[:idx]
	}
	return line
}

func rustBraceDelta(line string) int {
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

func extractRustCallNames(body string) []string {
	matches := rustCallPattern.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		name := strings.TrimSpace(match[1])
		switch name {
		case "", "fn", "if", "for", "while", "loop", "match", "return":
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
