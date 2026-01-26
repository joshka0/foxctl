package symbol

import (
	"context"
	"regexp"
	"strings"

	"github.com/jkatigb/agentctl/internal/codecontext/expander"
)

// TypeScriptExtractor extracts symbols from TypeScript sources using line-based heuristics.
type TypeScriptExtractor struct{}

// NewTypeScriptExtractor creates a new TypeScript extractor.
func NewTypeScriptExtractor() *TypeScriptExtractor {
	return &TypeScriptExtractor{}
}

// SupportedLanguages returns ["typescript"].
func (e *TypeScriptExtractor) SupportedLanguages() []string {
	return []string{"typescript"}
}

// Extract parses TypeScript source and returns top-level symbols.
func (e *TypeScriptExtractor) Extract(_ context.Context, filePath string, content []byte) ([]Symbol, error) {
	lines := strings.Split(string(content), "\n")
	lineOffsets := computeLineOffsets(lines)

	var symbols []Symbol
	for i, line := range lines {
		name, kind, signature, _, ok := parseTSDeclaration(line)
		if !ok {
			continue
		}

		startLine := i + 1
		endLine := startLine
		if kind == KindFunction || kind == KindClass || kind == KindInterface || kind == KindType {
			hasBrace := strings.Contains(line, "{")
			if kind == KindType && !hasBrace {
				for j := i + 1; j < len(lines); j++ {
					trimmed := strings.TrimSpace(lines[j])
					if trimmed == "" {
						continue
					}
					if strings.Contains(trimmed, ";") {
						break
					}
					if strings.HasPrefix(trimmed, "{") {
						hasBrace = true
						break
					}
					if (strings.HasPrefix(trimmed, "|") || strings.HasPrefix(trimmed, "&")) && strings.Contains(trimmed, "{") {
						hasBrace = true
						break
					}
				}
			}
			if hasBrace {
				endIdx := expander.FindBraceEnd(lines, i, expander.JSBraceStyle())
				if endIdx >= i {
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

		symbols = append(symbols, Symbol{
			ID:         ID(filePath, name),
			FilePath:   filePath,
			Name:       name,
			Language:   "typescript",
			Kind:       kind,
			StartByte:  startByte,
			EndByte:    endByte,
			StartLine:  startLine,
			EndLine:    endLine,
			Signature:  signature,
			BodyDigest: ComputeDigest(body),
		})
	}

	return symbols, nil
}

// ExtractCalls is not supported yet for TypeScript.
func (e *TypeScriptExtractor) ExtractCalls(_ context.Context, _ Symbol, _ []byte) ([]string, error) {
	return nil, nil
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
