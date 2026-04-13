package expander

import (
	"regexp"
	"strings"

	"github.com/jkatigb/agentctl/internal/intelligence/codecontext/files"
)

// genericExpander provides fallback block detection for unknown languages.
// It uses heuristics based on common patterns:
// - Brace matching for C-like languages
// - Indentation for significant-whitespace languages
// - Simple keyword detection
type genericExpander struct{}

// Common patterns that might indicate function/class definitions
var (
	// Common function patterns across languages
	genericFuncPatterns = []*regexp.Regexp{
		regexp.MustCompile(`^\s*(?:func|function|def|fn|sub|proc)\s+(\w+)`),
		regexp.MustCompile(`^\s*(?:public|private|protected)?\s*(?:static)?\s*(\w+)\s*\([^)]*\)\s*\{`),
	}

	// Common class/type patterns
	genericTypePatterns = []*regexp.Regexp{
		regexp.MustCompile(`^\s*(?:class|struct|interface|type|enum)\s+(\w+)`),
	}
)

func init() {
	Register(&genericExpander{})
}

func (e *genericExpander) Language() string {
	return "generic"
}

func (e *genericExpander) FindBlock(content *files.FileContent, line int) (start, end int, symbol string, err error) {
	if line < 1 || line > content.LineCount() {
		return 0, 0, "", &ExpanderError{
			Language: "generic",
			Message:  "line out of range",
			Line:     line,
		}
	}

	lines := content.Lines
	lineIdx := line - 1

	// Detect if this looks like a brace-based or indent-based language
	usesIndent := e.detectIndentBased(lines)

	// Check if current line starts a block
	if blockStart, blockEnd, sym := e.findBlockFromStart(lines, lineIdx, usesIndent); blockStart >= 0 {
		return blockStart + 1, blockEnd + 1, sym, nil
	}

	// Search backwards for enclosing block
	blockStartIdx := e.findEnclosingBlockStart(lines, lineIdx, usesIndent)
	if blockStartIdx < 0 {
		// No block found - return context around the line
		contextLines := 5
		start := lineIdx - contextLines
		if start < 0 {
			start = 0
		}
		end := lineIdx + contextLines
		if end >= len(lines) {
			end = len(lines) - 1
		}
		return start + 1, end + 1, "", nil
	}

	// Find block end
	var blockEndIdx int
	if usesIndent {
		blockEndIdx = FindBlockByIndentation(lines, blockStartIdx, "#")
	} else {
		style := DefaultBraceStyle()
		blockEndIdx = FindBraceEnd(lines, blockStartIdx, style)
		if blockEndIdx < 0 {
			blockEndIdx = len(lines) - 1
		}
	}

	sym := e.extractSymbolName(lines[blockStartIdx])
	return blockStartIdx + 1, blockEndIdx + 1, sym, nil
}

func (e *genericExpander) ExpandToSymbol(content *files.FileContent, symbolName string) (start, end int, err error) {
	lines := content.Lines
	usesIndent := e.detectIndentBased(lines)

	for lineIdx, line := range lines {
		if e.lineDefinesSymbol(line, symbolName) {
			blockStart, blockEnd, _ := e.findBlockFromStart(lines, lineIdx, usesIndent)
			if blockStart >= 0 {
				return blockStart + 1, blockEnd + 1, nil
			}
			return lineIdx + 1, lineIdx + 1, nil
		}
	}

	return 0, 0, &ExpanderError{
		Language: "generic",
		Message:  "symbol not found",
		Symbol:   symbolName,
	}
}

// detectIndentBased attempts to determine if the file uses significant whitespace.
// Returns true if it looks like Python/GDScript style, false for brace-based.
func (e *genericExpander) detectIndentBased(lines []string) bool {
	braceCount := 0
	colonCount := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Count braces and colons as block indicators
		if strings.HasSuffix(trimmed, "{") {
			braceCount++
		}
		if strings.HasSuffix(trimmed, ":") && !strings.Contains(trimmed, "::") {
			colonCount++
		}
	}

	// If we see more colons than braces, likely indent-based
	return colonCount > braceCount
}

// findBlockFromStart checks if the line starts a code block.
func (e *genericExpander) findBlockFromStart(lines []string, lineIdx int, usesIndent bool) (start, end int, symbol string) {
	if lineIdx < 0 || lineIdx >= len(lines) {
		return -1, -1, ""
	}

	line := lines[lineIdx]

	// Check for function patterns
	for _, pattern := range genericFuncPatterns {
		if match := pattern.FindStringSubmatch(line); match != nil {
			var endIdx int
			if usesIndent {
				endIdx = FindBlockByIndentation(lines, lineIdx, "#")
			} else {
				style := DefaultBraceStyle()
				endIdx = FindBraceEnd(lines, lineIdx, style)
				if endIdx < 0 {
					endIdx = lineIdx
				}
			}
			return lineIdx, endIdx, match[1]
		}
	}

	// Check for type patterns
	for _, pattern := range genericTypePatterns {
		if match := pattern.FindStringSubmatch(line); match != nil {
			var endIdx int
			if usesIndent {
				endIdx = FindBlockByIndentation(lines, lineIdx, "#")
			} else {
				style := DefaultBraceStyle()
				endIdx = FindBraceEnd(lines, lineIdx, style)
				if endIdx < 0 {
					endIdx = lineIdx
				}
			}
			return lineIdx, endIdx, match[1]
		}
	}

	return -1, -1, ""
}

// findEnclosingBlockStart searches backwards for a block start.
func (e *genericExpander) findEnclosingBlockStart(lines []string, fromLineIdx int, usesIndent bool) int {
	if usesIndent {
		// Use indentation-based search
		targetIndent := CountLeadingWhitespace(lines[fromLineIdx])

		for i := fromLineIdx - 1; i >= 0; i-- {
			line := lines[i]
			if IsBlankOrComment(line, "#") {
				continue
			}

			indent := CountLeadingWhitespace(line)
			if indent >= targetIndent {
				continue
			}

			// Check if this matches any definition pattern
			for _, pattern := range genericFuncPatterns {
				if pattern.MatchString(line) {
					return i
				}
			}
			for _, pattern := range genericTypePatterns {
				if pattern.MatchString(line) {
					return i
				}
			}

			targetIndent = indent
		}
	} else {
		// Use brace-based search
		style := DefaultBraceStyle()
		braceCount := 0

		for lineIdx := fromLineIdx; lineIdx >= 0; lineIdx-- {
			line := lines[lineIdx]
			inString := false
			stringChar := rune(0)

			for i := 0; i < len(line); i++ {
				ch := rune(line[i])

				// Skip comments
				if i+2 <= len(line) && line[i:i+2] == "//" {
					break
				}

				// Handle strings
				if ch == '"' || ch == '\'' {
					if !inString {
						inString = true
						stringChar = ch
					} else if ch == stringChar {
						escapes := 0
						for j := i - 1; j >= 0 && line[j] == '\\'; j-- {
							escapes++
						}
						if escapes%2 == 0 {
							inString = false
						}
					}
					continue
				}
				if inString {
					continue
				}

				switch ch {
				case style.Close:
					braceCount++
				case style.Open:
					if braceCount == 0 {
						// Check lines above for definition
						for checkIdx := lineIdx; checkIdx >= 0 && checkIdx >= lineIdx-5; checkIdx-- {
							checkLine := lines[checkIdx]
							for _, pattern := range genericFuncPatterns {
								if pattern.MatchString(checkLine) {
									return checkIdx
								}
							}
							for _, pattern := range genericTypePatterns {
								if pattern.MatchString(checkLine) {
									return checkIdx
								}
							}
						}
						return lineIdx
					}
					braceCount--
				}
			}
		}
	}

	return -1
}

// extractSymbolName extracts a symbol name from a definition line.
func (e *genericExpander) extractSymbolName(line string) string {
	for _, pattern := range genericFuncPatterns {
		if match := pattern.FindStringSubmatch(line); match != nil {
			return match[1]
		}
	}
	for _, pattern := range genericTypePatterns {
		if match := pattern.FindStringSubmatch(line); match != nil {
			return match[1]
		}
	}
	return ""
}

// lineDefinesSymbol checks if a line defines the given symbol.
func (e *genericExpander) lineDefinesSymbol(line, symbolName string) bool {
	allPatterns := append(genericFuncPatterns, genericTypePatterns...)
	for _, pattern := range allPatterns {
		if match := pattern.FindStringSubmatch(line); len(match) > 1 && match[1] == symbolName {
			return true
		}
	}
	return false
}
