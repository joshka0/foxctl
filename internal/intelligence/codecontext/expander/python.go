package expander

import (
	"regexp"
	"strings"

	"github.com/jkatigb/agentctl/internal/intelligence/codecontext/files"
)

// pythonExpander handles Python code block detection using indentation.
type pythonExpander struct{}

// Patterns for Python definitions
var (
	// Matches: def name(...):
	pyFuncPattern = regexp.MustCompile(`^\s*(?:async\s+)?def\s+(\w+)\s*\(`)

	// Matches: class Name(...):
	pyClassPattern = regexp.MustCompile(`^\s*class\s+(\w+)`)
)

const pyLineComment = "#"

func init() {
	Register(&pythonExpander{})
}

func (e *pythonExpander) Language() string {
	return "python"
}

func (e *pythonExpander) FindBlock(content *files.FileContent, line int) (start, end int, symbol string, err error) {
	if line < 1 || line > content.LineCount() {
		return 0, 0, "", &ExpanderError{
			Language: "python",
			Message:  "line out of range",
			Line:     line,
		}
	}

	lines := content.Lines
	lineIdx := line - 1 // Convert to 0-indexed

	// Check if current line starts a block
	if blockStart, blockEnd, sym := e.findBlockFromStart(lines, lineIdx); blockStart >= 0 {
		return blockStart + 1, blockEnd + 1, sym, nil
	}

	// Search backwards for the enclosing function/class
	blockStartIdx := e.findEnclosingBlockStart(lines, lineIdx)
	if blockStartIdx < 0 {
		// No enclosing block found - return the line
		return line, line, "", nil
	}

	// Find the end of the block
	blockEndIdx := FindBlockByIndentation(lines, blockStartIdx, pyLineComment)

	// Extract symbol name
	sym := e.extractSymbolName(lines[blockStartIdx])

	return blockStartIdx + 1, blockEndIdx + 1, sym, nil
}

func (e *pythonExpander) ExpandToSymbol(content *files.FileContent, symbolName string) (start, end int, err error) {
	lines := content.Lines

	for lineIdx, line := range lines {
		if e.lineDefinesSymbol(line, symbolName) {
			blockEndIdx := FindBlockByIndentation(lines, lineIdx, pyLineComment)
			return lineIdx + 1, blockEndIdx + 1, nil
		}
	}

	return 0, 0, &ExpanderError{
		Language: "python",
		Message:  "symbol not found",
		Symbol:   symbolName,
	}
}

// findBlockFromStart checks if the given line starts a function/class block.
func (e *pythonExpander) findBlockFromStart(lines []string, lineIdx int) (start, end int, symbol string) {
	if lineIdx < 0 || lineIdx >= len(lines) {
		return -1, -1, ""
	}

	line := lines[lineIdx]

	// Check for function definition
	if match := pyFuncPattern.FindStringSubmatch(line); match != nil {
		endIdx := FindBlockByIndentation(lines, lineIdx, pyLineComment)
		return lineIdx, endIdx, match[1]
	}

	// Check for class definition
	if match := pyClassPattern.FindStringSubmatch(line); match != nil {
		endIdx := FindBlockByIndentation(lines, lineIdx, pyLineComment)
		return lineIdx, endIdx, match[1]
	}

	return -1, -1, ""
}

// findEnclosingBlockStart searches backwards for the enclosing function/class.
func (e *pythonExpander) findEnclosingBlockStart(lines []string, fromLineIdx int) int {
	if fromLineIdx < 0 || fromLineIdx >= len(lines) {
		return -1
	}

	targetIndent := CountLeadingWhitespace(lines[fromLineIdx])

	// Search backwards for a function/class definition with less indentation
	for i := fromLineIdx - 1; i >= 0; i-- {
		line := lines[i]

		if IsBlankOrComment(line, pyLineComment) {
			continue
		}

		indent := CountLeadingWhitespace(line)
		if indent >= targetIndent {
			continue
		}

		// Check if this is a function or class definition
		if pyFuncPattern.MatchString(line) || pyClassPattern.MatchString(line) {
			return i
		}

		// Update target indent for nested search
		targetIndent = indent
	}

	return -1
}

// extractSymbolName extracts the symbol name from a definition line.
func (e *pythonExpander) extractSymbolName(line string) string {
	if match := pyFuncPattern.FindStringSubmatch(line); match != nil {
		return match[1]
	}
	if match := pyClassPattern.FindStringSubmatch(line); match != nil {
		return match[1]
	}
	return ""
}

// lineDefinesSymbol checks if a line defines the given symbol.
func (e *pythonExpander) lineDefinesSymbol(line, symbolName string) bool {
	if match := pyFuncPattern.FindStringSubmatch(line); match != nil && match[1] == symbolName {
		return true
	}
	if match := pyClassPattern.FindStringSubmatch(line); match != nil && match[1] == symbolName {
		return true
	}
	return false
}

// GetDecorators returns any decorator lines above the given function/class line.
// Python decorators (@decorator) are part of the function definition.
func (e *pythonExpander) GetDecorators(lines []string, defLineIdx int) int {
	startIdx := defLineIdx

	for i := defLineIdx - 1; i >= 0; i-- {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "@") {
			startIdx = i
		} else {
			break
		}
	}

	return startIdx
}
