package expander

import (
	"regexp"
	"strings"

	"github.com/jkatigb/agentctl/internal/intelligence/codecontext/files"
)

// goExpander handles Go code block detection.
type goExpander struct{}

// Patterns for Go function/type definitions
var (
	// Matches: func Name(...) or func (r Receiver) Name(...)
	goFuncPattern = regexp.MustCompile(`^\s*func\s+(?:\([^)]+\)\s*)?(\w+)\s*\(`)

	// Matches: type Name struct/interface
	goTypePattern = regexp.MustCompile(`^\s*type\s+(\w+)\s+(?:struct|interface)\s*\{`)

	// Matches: type Name = ... or type Name SomeType
	goTypeAliasPattern = regexp.MustCompile(`^\s*type\s+(\w+)\s+`)

	// Matches method receiver: func (r *Type) Name(...)
	goMethodPattern = regexp.MustCompile(`^\s*func\s+\((\w+)\s+\*?(\w+)\)\s*(\w+)\s*\(`)
)

func init() {
	Register(&goExpander{})
}

func (e *goExpander) Language() string {
	return "go"
}

func (e *goExpander) FindBlock(content *files.FileContent, line int) (start, end int, symbol string, err error) {
	if line < 1 || line > content.LineCount() {
		return 0, 0, "", &ExpanderError{
			Language: "go",
			Message:  "line out of range",
			Line:     line,
		}
	}

	lines := content.Lines
	lineIdx := line - 1 // Convert to 0-indexed

	// First, check if current line starts a block
	if blockStart, blockEnd, sym := e.findBlockFromStart(lines, lineIdx); blockStart >= 0 {
		return blockStart + 1, blockEnd + 1, sym, nil
	}

	// Search backwards for the enclosing block
	blockStartIdx := e.findEnclosingBlockStart(lines, lineIdx)
	if blockStartIdx < 0 {
		// No enclosing block found - return the line with some context
		return line, line, "", nil
	}

	// Find the end of the block
	style := GoBraceStyle()
	blockEndIdx := FindBraceEnd(lines, blockStartIdx, style)
	if blockEndIdx < 0 {
		blockEndIdx = len(lines) - 1
	}

	// Extract symbol name
	sym := e.extractSymbolName(lines[blockStartIdx])

	return blockStartIdx + 1, blockEndIdx + 1, sym, nil
}

func (e *goExpander) ExpandToSymbol(content *files.FileContent, symbolName string) (start, end int, err error) {
	lines := content.Lines

	for lineIdx, line := range lines {
		// Check if this line defines the symbol
		if e.lineDefinesSymbol(line, symbolName) {
			blockStart, blockEnd, _ := e.findBlockFromStart(lines, lineIdx)
			if blockStart >= 0 {
				return blockStart + 1, blockEnd + 1, nil
			}
			// Single-line definition
			return lineIdx + 1, lineIdx + 1, nil
		}
	}

	return 0, 0, &ExpanderError{
		Language: "go",
		Message:  "symbol not found",
		Symbol:   symbolName,
	}
}

// findBlockFromStart checks if the given line starts a block and finds its extent.
// Returns 0-indexed start, end, and symbol name. Returns -1 for start if not a block start.
func (e *goExpander) findBlockFromStart(lines []string, lineIdx int) (start, end int, symbol string) {
	if lineIdx < 0 || lineIdx >= len(lines) {
		return -1, -1, ""
	}

	line := lines[lineIdx]
	trimmed := strings.TrimSpace(line)

	// Check for function definition
	if match := goFuncPattern.FindStringSubmatch(line); match != nil {
		style := GoBraceStyle()
		endIdx := FindBraceEnd(lines, lineIdx, style)
		if endIdx < 0 {
			endIdx = lineIdx
		}
		return lineIdx, endIdx, match[1]
	}

	// Check for method definition
	if match := goMethodPattern.FindStringSubmatch(line); match != nil {
		style := GoBraceStyle()
		endIdx := FindBraceEnd(lines, lineIdx, style)
		if endIdx < 0 {
			endIdx = lineIdx
		}
		// Return method name as symbol
		return lineIdx, endIdx, match[3]
	}

	// Check for type definition
	if match := goTypePattern.FindStringSubmatch(line); match != nil {
		style := GoBraceStyle()
		endIdx := FindBraceEnd(lines, lineIdx, style)
		if endIdx < 0 {
			endIdx = lineIdx
		}
		return lineIdx, endIdx, match[1]
	}

	// Check for const/var block
	if strings.HasPrefix(trimmed, "const (") || strings.HasPrefix(trimmed, "var (") {
		style := GoBraceStyle()
		// Use parentheses for const/var blocks
		parenStyle := BraceStyle{
			Open:               '(',
			Close:              ')',
			StringDelimiters:   style.StringDelimiters,
			LineComment:        style.LineComment,
			BlockCommentStart:  style.BlockCommentStart,
			BlockCommentEnd:    style.BlockCommentEnd,
			SupportsRawStrings: style.SupportsRawStrings,
			RawStringDelimiter: style.RawStringDelimiter,
		}
		endIdx := FindBraceEnd(lines, lineIdx, parenStyle)
		if endIdx < 0 {
			endIdx = lineIdx
		}
		if strings.HasPrefix(trimmed, "const") {
			return lineIdx, endIdx, "const"
		}
		return lineIdx, endIdx, "var"
	}

	return -1, -1, ""
}

// findEnclosingBlockStart searches backwards to find the start of an enclosing block.
func (e *goExpander) findEnclosingBlockStart(lines []string, fromLineIdx int) int {
	style := GoBraceStyle()
	braceCount := 0

	for lineIdx := fromLineIdx; lineIdx >= 0; lineIdx-- {
		line := lines[lineIdx]

		// Count braces on this line (simplified - doesn't handle all edge cases)
		inString := false
		inRawString := false
		stringChar := rune(0)

		for i := 0; i < len(line); i++ {
			ch := rune(line[i])

			// Skip line comments
			if i+2 <= len(line) && line[i:i+2] == "//" {
				break
			}

			// Handle raw strings
			if ch == '`' {
				inRawString = !inRawString
				continue
			}
			if inRawString {
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
					// Found unmatched opening brace - this could be our block
					// Check if this line or a previous line is a function/type definition
					for checkIdx := lineIdx; checkIdx >= 0 && checkIdx >= lineIdx-5; checkIdx-- {
						checkLine := lines[checkIdx]
						if goFuncPattern.MatchString(checkLine) ||
							goMethodPattern.MatchString(checkLine) ||
							goTypePattern.MatchString(checkLine) {
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
func (e *goExpander) extractSymbolName(line string) string {
	if match := goFuncPattern.FindStringSubmatch(line); match != nil {
		return match[1]
	}
	if match := goMethodPattern.FindStringSubmatch(line); match != nil {
		return match[3]
	}
	if match := goTypePattern.FindStringSubmatch(line); match != nil {
		return match[1]
	}
	if match := goTypeAliasPattern.FindStringSubmatch(line); match != nil {
		return match[1]
	}
	return ""
}

// lineDefinesSymbol checks if a line defines the given symbol.
func (e *goExpander) lineDefinesSymbol(line, symbolName string) bool {
	// Check function
	if match := goFuncPattern.FindStringSubmatch(line); match != nil && match[1] == symbolName {
		return true
	}
	// Check method
	if match := goMethodPattern.FindStringSubmatch(line); match != nil && match[3] == symbolName {
		return true
	}
	// Check type
	if match := goTypePattern.FindStringSubmatch(line); match != nil && match[1] == symbolName {
		return true
	}
	if match := goTypeAliasPattern.FindStringSubmatch(line); match != nil && match[1] == symbolName {
		return true
	}
	return false
}
