package expander

import (
	"regexp"

	"github.com/joshka0/foxctl/internal/intelligence/codecontext/files"
)

// jstsExpander handles JavaScript and TypeScript code block detection.
type jstsExpander struct {
	lang string
}

// Patterns for JS/TS definitions
var (
	// Matches: function name(...) or async function name(...)
	jsFuncPattern = regexp.MustCompile(`^\s*(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*\(`)

	// Matches: const/let/var name = function(...) or arrow function
	jsArrowFuncPattern = regexp.MustCompile(`^\s*(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?(?:function|\([^)]*\)\s*=>|\w+\s*=>)`)

	// Matches: class Name
	jsClassPattern = regexp.MustCompile(`^\s*(?:export\s+)?(?:abstract\s+)?class\s+(\w+)`)

	// Matches: interface Name (TypeScript)
	tsInterfacePattern = regexp.MustCompile(`^\s*(?:export\s+)?interface\s+(\w+)`)

	// Matches: type Name = (TypeScript)
	tsTypePattern = regexp.MustCompile(`^\s*(?:export\s+)?type\s+(\w+)\s*=`)

	// Matches: method in class: name(...) { or async name(...) {
	jsMethodPattern = regexp.MustCompile(`^\s*(?:async\s+)?(?:static\s+)?(?:get\s+|set\s+)?(\w+)\s*\([^)]*\)\s*(?::\s*\w+)?\s*\{`)
)

func init() {
	Register(&jstsExpander{lang: "javascript"})
	Register(&jstsExpander{lang: "typescript"})
}

func (e *jstsExpander) Language() string {
	return e.lang
}

func (e *jstsExpander) FindBlock(content *files.FileContent, line int) (start, end int, symbol string, err error) {
	if line < 1 || line > content.LineCount() {
		return 0, 0, "", &ExpanderError{
			Language: e.lang,
			Message:  "line out of range",
			Line:     line,
		}
	}

	lines := content.Lines
	lineIdx := line - 1

	// Check if current line starts a block
	if blockStart, blockEnd, sym := e.findBlockFromStart(lines, lineIdx); blockStart >= 0 {
		return blockStart + 1, blockEnd + 1, sym, nil
	}

	// Search backwards for the enclosing block
	blockStartIdx := e.findEnclosingBlockStart(lines, lineIdx)
	if blockStartIdx < 0 {
		return line, line, "", nil
	}

	// Find the end of the block
	style := JSBraceStyle()
	blockEndIdx := FindBraceEnd(lines, blockStartIdx, style)
	if blockEndIdx < 0 {
		blockEndIdx = len(lines) - 1
	}

	sym := e.extractSymbolName(lines[blockStartIdx])
	return blockStartIdx + 1, blockEndIdx + 1, sym, nil
}

func (e *jstsExpander) ExpandToSymbol(content *files.FileContent, symbolName string) (start, end int, err error) {
	lines := content.Lines

	for lineIdx, line := range lines {
		if e.lineDefinesSymbol(line, symbolName) {
			blockStart, blockEnd, _ := e.findBlockFromStart(lines, lineIdx)
			if blockStart >= 0 {
				return blockStart + 1, blockEnd + 1, nil
			}
			return lineIdx + 1, lineIdx + 1, nil
		}
	}

	return 0, 0, &ExpanderError{
		Language: e.lang,
		Message:  "symbol not found",
		Symbol:   symbolName,
	}
}

// findBlockFromStart checks if the given line starts a function/class/interface block.
func (e *jstsExpander) findBlockFromStart(lines []string, lineIdx int) (start, end int, symbol string) {
	if lineIdx < 0 || lineIdx >= len(lines) {
		return -1, -1, ""
	}

	line := lines[lineIdx]
	style := JSBraceStyle()

	// Check for function definition
	if match := jsFuncPattern.FindStringSubmatch(line); match != nil {
		endIdx := FindBraceEnd(lines, lineIdx, style)
		if endIdx < 0 {
			endIdx = lineIdx
		}
		return lineIdx, endIdx, match[1]
	}

	// Check for arrow/const function
	if match := jsArrowFuncPattern.FindStringSubmatch(line); match != nil {
		endIdx := FindBraceEnd(lines, lineIdx, style)
		if endIdx < 0 {
			endIdx = lineIdx
		}
		return lineIdx, endIdx, match[1]
	}

	// Check for class definition
	if match := jsClassPattern.FindStringSubmatch(line); match != nil {
		endIdx := FindBraceEnd(lines, lineIdx, style)
		if endIdx < 0 {
			endIdx = lineIdx
		}
		return lineIdx, endIdx, match[1]
	}

	// TypeScript-specific: interface
	if e.lang == "typescript" {
		if match := tsInterfacePattern.FindStringSubmatch(line); match != nil {
			endIdx := FindBraceEnd(lines, lineIdx, style)
			if endIdx < 0 {
				endIdx = lineIdx
			}
			return lineIdx, endIdx, match[1]
		}

		// TypeScript type alias - may not have braces
		if match := tsTypePattern.FindStringSubmatch(line); match != nil {
			// Type aliases can span multiple lines with braces or be single line
			endIdx := FindBraceEnd(lines, lineIdx, style)
			if endIdx < 0 {
				// Single line type or no braces - find semicolon or end of line
				endIdx = lineIdx
			}
			return lineIdx, endIdx, match[1]
		}
	}

	// Check for method definition (within a class)
	if match := jsMethodPattern.FindStringSubmatch(line); match != nil {
		endIdx := FindBraceEnd(lines, lineIdx, style)
		if endIdx < 0 {
			endIdx = lineIdx
		}
		return lineIdx, endIdx, match[1]
	}

	return -1, -1, ""
}

// findEnclosingBlockStart searches backwards for the enclosing function/class.
func (e *jstsExpander) findEnclosingBlockStart(lines []string, fromLineIdx int) int {
	style := JSBraceStyle()
	braceCount := 0

	for lineIdx := fromLineIdx; lineIdx >= 0; lineIdx-- {
		line := lines[lineIdx]

		inString := false
		inTemplate := false
		stringChar := rune(0)

		for i := 0; i < len(line); i++ {
			ch := rune(line[i])

			// Skip line comments
			if i+2 <= len(line) && line[i:i+2] == "//" {
				break
			}

			// Handle template literals
			if ch == '`' {
				inTemplate = !inTemplate
				continue
			}
			if inTemplate {
				continue
			}

			// Handle regular strings
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
					// Found unmatched opening brace
					for checkIdx := lineIdx; checkIdx >= 0 && checkIdx >= lineIdx-5; checkIdx-- {
						checkLine := lines[checkIdx]
						if jsFuncPattern.MatchString(checkLine) ||
							jsArrowFuncPattern.MatchString(checkLine) ||
							jsClassPattern.MatchString(checkLine) ||
							jsMethodPattern.MatchString(checkLine) {
							return checkIdx
						}
						if e.lang == "typescript" && tsInterfacePattern.MatchString(checkLine) {
							return checkIdx
						}
					}
					return lineIdx
				}
				braceCount--
			}
		}
	}

	return -1
}

// extractSymbolName extracts the symbol name from a definition line.
func (e *jstsExpander) extractSymbolName(line string) string {
	if match := jsFuncPattern.FindStringSubmatch(line); match != nil {
		return match[1]
	}
	if match := jsArrowFuncPattern.FindStringSubmatch(line); match != nil {
		return match[1]
	}
	if match := jsClassPattern.FindStringSubmatch(line); match != nil {
		return match[1]
	}
	if match := jsMethodPattern.FindStringSubmatch(line); match != nil {
		return match[1]
	}
	if e.lang == "typescript" {
		if match := tsInterfacePattern.FindStringSubmatch(line); match != nil {
			return match[1]
		}
		if match := tsTypePattern.FindStringSubmatch(line); match != nil {
			return match[1]
		}
	}
	return ""
}

// lineDefinesSymbol checks if a line defines the given symbol.
func (e *jstsExpander) lineDefinesSymbol(line, symbolName string) bool {
	patterns := []*regexp.Regexp{
		jsFuncPattern,
		jsArrowFuncPattern,
		jsClassPattern,
		jsMethodPattern,
	}
	if e.lang == "typescript" {
		patterns = append(patterns, tsInterfacePattern, tsTypePattern)
	}

	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(line); len(match) > 1 && match[1] == symbolName {
			return true
		}
	}
	return false
}
