package expander

import (
	"regexp"

	"github.com/jkatigb/agentctl/internal/codecontext/files"
)

// gdscriptExpander handles GDScript code block detection using indentation.
type gdscriptExpander struct{}

// Patterns for GDScript definitions
var (
	// Matches: func name(...):
	gdFuncPattern = regexp.MustCompile(`^\s*func\s+(\w+)\s*\(`)

	// Matches: class_name Name or class Name:
	gdClassPattern = regexp.MustCompile(`^\s*(?:class_name|class)\s+(\w+)`)

	// Matches: signal name(...)
	gdSignalPattern = regexp.MustCompile(`^\s*signal\s+(\w+)`)

	// Matches: var name or const name or export var name
	gdVarPattern = regexp.MustCompile(`^\s*(?:export\s+)?(?:onready\s+)?(?:var|const)\s+(\w+)`)
)

const gdLineComment = "#"

func init() {
	Register(&gdscriptExpander{})
}

func (e *gdscriptExpander) Language() string {
	return "gdscript"
}

func (e *gdscriptExpander) FindBlock(content *files.FileContent, line int) (start, end int, symbol string, err error) {
	if line < 1 || line > content.LineCount() {
		return 0, 0, "", &ExpanderError{
			Language: "gdscript",
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

	// Search backwards for the enclosing function/class
	blockStartIdx := e.findEnclosingBlockStart(lines, lineIdx)
	if blockStartIdx < 0 {
		return line, line, "", nil
	}

	// Find the end of the block
	blockEndIdx := FindBlockByIndentation(lines, blockStartIdx, gdLineComment)

	sym := e.extractSymbolName(lines[blockStartIdx])
	return blockStartIdx + 1, blockEndIdx + 1, sym, nil
}

func (e *gdscriptExpander) ExpandToSymbol(content *files.FileContent, symbolName string) (start, end int, err error) {
	lines := content.Lines

	for lineIdx, line := range lines {
		if e.lineDefinesSymbol(line, symbolName) {
			// For functions, expand the block
			if gdFuncPattern.MatchString(line) {
				blockEndIdx := FindBlockByIndentation(lines, lineIdx, gdLineComment)
				return lineIdx + 1, blockEndIdx + 1, nil
			}
			// For vars/signals, just return the line
			return lineIdx + 1, lineIdx + 1, nil
		}
	}

	return 0, 0, &ExpanderError{
		Language: "gdscript",
		Message:  "symbol not found",
		Symbol:   symbolName,
	}
}

// findBlockFromStart checks if the given line starts a function/class block.
func (e *gdscriptExpander) findBlockFromStart(lines []string, lineIdx int) (start, end int, symbol string) {
	if lineIdx < 0 || lineIdx >= len(lines) {
		return -1, -1, ""
	}

	line := lines[lineIdx]

	// Check for function definition
	if match := gdFuncPattern.FindStringSubmatch(line); match != nil {
		endIdx := FindBlockByIndentation(lines, lineIdx, gdLineComment)
		return lineIdx, endIdx, match[1]
	}

	// Check for class definition
	if match := gdClassPattern.FindStringSubmatch(line); match != nil {
		endIdx := FindBlockByIndentation(lines, lineIdx, gdLineComment)
		return lineIdx, endIdx, match[1]
	}

	return -1, -1, ""
}

// findEnclosingBlockStart searches backwards for the enclosing function/class.
func (e *gdscriptExpander) findEnclosingBlockStart(lines []string, fromLineIdx int) int {
	if fromLineIdx < 0 || fromLineIdx >= len(lines) {
		return -1
	}

	targetIndent := CountLeadingWhitespace(lines[fromLineIdx])

	for i := fromLineIdx - 1; i >= 0; i-- {
		line := lines[i]

		if IsBlankOrComment(line, gdLineComment) {
			continue
		}

		indent := CountLeadingWhitespace(line)
		if indent >= targetIndent {
			continue
		}

		// Check if this is a function or class definition
		if gdFuncPattern.MatchString(line) || gdClassPattern.MatchString(line) {
			return i
		}

		targetIndent = indent
	}

	return -1
}

// extractSymbolName extracts the symbol name from a definition line.
func (e *gdscriptExpander) extractSymbolName(line string) string {
	if match := gdFuncPattern.FindStringSubmatch(line); match != nil {
		return match[1]
	}
	if match := gdClassPattern.FindStringSubmatch(line); match != nil {
		return match[1]
	}
	if match := gdSignalPattern.FindStringSubmatch(line); match != nil {
		return match[1]
	}
	if match := gdVarPattern.FindStringSubmatch(line); match != nil {
		return match[1]
	}
	return ""
}

// lineDefinesSymbol checks if a line defines the given symbol.
func (e *gdscriptExpander) lineDefinesSymbol(line, symbolName string) bool {
	patterns := []*regexp.Regexp{gdFuncPattern, gdClassPattern, gdSignalPattern, gdVarPattern}
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(line); len(match) > 1 && match[1] == symbolName {
			return true
		}
	}
	return false
}
