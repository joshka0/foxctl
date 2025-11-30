package main

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Language represents a supported programming language.
type Language string

const (
	LangGo       Language = "go"
	LangPython   Language = "python"
	LangJS       Language = "javascript"
	LangTS       Language = "typescript"
	LangGDScript Language = "gdscript"
	LangGeneric  Language = "generic"
)

// Expander handles expanding matches to code blocks for a specific language.
type Expander struct {
	lang          Language
	maxBlockLines int
}

// NewExpander creates a new Expander for the given language.
func NewExpander(lang Language, maxBlockLines int) *Expander {
	if maxBlockLines <= 0 {
		maxBlockLines = 400
	}
	return &Expander{
		lang:          lang,
		maxBlockLines: maxBlockLines,
	}
}

// detectLanguage determines the language from a file path.
func detectLanguage(path string) Language {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return LangGo
	case ".py", ".pyw", ".pyi":
		return LangPython
	case ".js", ".jsx", ".mjs", ".cjs":
		return LangJS
	case ".ts", ".tsx", ".mts", ".cts":
		return LangTS
	case ".gd":
		return LangGDScript
	default:
		return LangGeneric
	}
}

// ExpandMatches expands raw matches into code blocks, deduplicating overlapping regions.
func (e *Expander) ExpandMatches(file string, lines []string, matches []rawMatch) []Block {
	if len(matches) == 0 || len(lines) == 0 {
		return nil
	}

	// Sort matches by line number
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Line < matches[j].Line
	})

	var blocks []Block
	var currentBlock *blockBuilder

	for _, m := range matches {
		// ripgrep uses 1-indexed lines
		lineIdx := m.Line - 1
		if lineIdx < 0 || lineIdx >= len(lines) {
			continue
		}

		// Find block boundaries for this match
		start, end, header, symbolName, symbolKind := e.findBlockBoundaries(lines, lineIdx)

		// Check if this match belongs to the current block
		if currentBlock != nil && start <= currentBlock.endLine {
			// Extend current block if needed
			if end > currentBlock.endLine {
				currentBlock.endLine = end
			}
			currentBlock.matchLines = append(currentBlock.matchLines, m.Line)
		} else {
			// Finalize previous block if any
			if currentBlock != nil {
				blocks = append(blocks, currentBlock.build(file, e.lang, lines))
			}
			// Start new block
			currentBlock = &blockBuilder{
				startLine:  start,
				endLine:    end,
				headerLine: header,
				symbolName: symbolName,
				symbolKind: symbolKind,
				matchLines: []int{m.Line},
			}
		}
	}

	// Finalize last block
	if currentBlock != nil {
		blocks = append(blocks, currentBlock.build(file, e.lang, lines))
	}

	return blocks
}

// blockBuilder accumulates state for building a Block.
type blockBuilder struct {
	startLine  int
	endLine    int
	headerLine string
	symbolName string
	symbolKind string
	matchLines []int
}

func (b *blockBuilder) build(file string, lang Language, lines []string) Block {
	// Clamp to valid range
	if b.startLine < 0 {
		b.startLine = 0
	}
	if b.endLine >= len(lines) {
		b.endLine = len(lines) - 1
	}

	// Extract source
	source := strings.Join(lines[b.startLine:b.endLine+1], "\n")

	return Block{
		File:       file,
		Language:   string(lang),
		StartLine:  b.startLine + 1, // Convert back to 1-indexed
		EndLine:    b.endLine + 1,
		HeaderLine: b.headerLine,
		SymbolName: b.symbolName,
		SymbolKind: b.symbolKind,
		Source:     source,
		MatchLines: b.matchLines,
		MatchCount: len(b.matchLines),
	}
}

// findBlockBoundaries finds the start and end lines of the code block containing lineIdx.
// Returns (startLine, endLine, headerLine, symbolName, symbolKind) using 0-indexed lines.
func (e *Expander) findBlockBoundaries(lines []string, lineIdx int) (int, int, string, string, string) {
	switch e.lang {
	case LangGo:
		return e.findGoBlock(lines, lineIdx)
	case LangPython:
		return e.findPythonBlock(lines, lineIdx)
	case LangJS, LangTS:
		return e.findJSTSBlock(lines, lineIdx)
	case LangGDScript:
		return e.findGDScriptBlock(lines, lineIdx)
	default:
		return e.findGenericBlock(lines, lineIdx)
	}
}

// Go patterns
var (
	goFuncPattern   = regexp.MustCompile(`^func\s+(\w+)`)
	goMethodPattern = regexp.MustCompile(`^func\s+\([^)]+\)\s+(\w+)`)
	goTypePattern   = regexp.MustCompile(`^type\s+(\w+)`)
)

func (e *Expander) findGoBlock(lines []string, lineIdx int) (int, int, string, string, string) {
	// Search upward for func/type declaration
	startLine := lineIdx
	var header, symbolName, symbolKind string

	for i := lineIdx; i >= 0 && (lineIdx-i) < e.maxBlockLines; i-- {
		trimmed := strings.TrimSpace(lines[i])

		// Check for function
		if match := goMethodPattern.FindStringSubmatch(trimmed); match != nil {
			startLine = i
			header = trimLine(trimmed, 120)
			symbolName = match[1]
			symbolKind = "method"
			break
		}
		if match := goFuncPattern.FindStringSubmatch(trimmed); match != nil {
			startLine = i
			header = trimLine(trimmed, 120)
			symbolName = match[1]
			symbolKind = "function"
			break
		}
		// Check for type declaration
		if match := goTypePattern.FindStringSubmatch(trimmed); match != nil {
			startLine = i
			header = trimLine(trimmed, 120)
			symbolName = match[1]
			symbolKind = "type"
			break
		}
	}

	// If no header found, fall back to blank-line boundaries
	if header == "" {
		return e.findGenericBlock(lines, lineIdx)
	}

	// Find end using brace matching
	endLine := e.findBraceEnd(lines, startLine)

	// Clamp to max block lines
	if endLine-startLine > e.maxBlockLines {
		endLine = startLine + e.maxBlockLines
	}

	return startLine, endLine, header, symbolName, symbolKind
}

// Python patterns
var (
	pyFuncPattern  = regexp.MustCompile(`^(\s*)def\s+(\w+)`)
	pyClassPattern = regexp.MustCompile(`^(\s*)class\s+(\w+)`)
	pyAsyncPattern = regexp.MustCompile(`^(\s*)async\s+def\s+(\w+)`)
)

func (e *Expander) findPythonBlock(lines []string, lineIdx int) (int, int, string, string, string) {
	// Search upward for def/class at same or lower indentation
	matchIndent := getIndentLevel(lines[lineIdx])
	startLine := lineIdx
	var header, symbolName, symbolKind string
	var defIndent int

	for i := lineIdx; i >= 0 && (lineIdx-i) < e.maxBlockLines; i-- {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		lineIndent := getIndentLevel(line)

		// Check for async def first (must come before def check)
		if match := pyAsyncPattern.FindStringSubmatch(line); match != nil {
			defIndent = len(match[1])
			if defIndent <= matchIndent || i == lineIdx {
				startLine = i
				header = trimLine(trimmed, 120)
				symbolName = match[2]
				symbolKind = "function"
				break
			}
		}

		// Check for def
		if match := pyFuncPattern.FindStringSubmatch(line); match != nil {
			defIndent = len(match[1])
			if defIndent <= matchIndent || i == lineIdx {
				startLine = i
				header = trimLine(trimmed, 120)
				symbolName = match[2]
				symbolKind = "function"
				break
			}
		}

		// Check for class
		if match := pyClassPattern.FindStringSubmatch(line); match != nil {
			defIndent = len(match[1])
			if defIndent <= matchIndent || i == lineIdx {
				startLine = i
				header = trimLine(trimmed, 120)
				symbolName = match[2]
				symbolKind = "class"
				break
			}
		}

		// Stop at lower indentation non-definition (probably left the block)
		if lineIndent < matchIndent && trimmed != "" {
			break
		}
	}

	if header == "" {
		return e.findGenericBlock(lines, lineIdx)
	}

	// Find end using indentation
	endLine := e.findIndentEnd(lines, startLine, defIndent)

	if endLine-startLine > e.maxBlockLines {
		endLine = startLine + e.maxBlockLines
	}

	return startLine, endLine, header, symbolName, symbolKind
}

// JS/TS patterns
var (
	jsFuncPattern      = regexp.MustCompile(`^(?:export\s+)?(?:async\s+)?function\s+(\w+)`)
	jsArrowPattern     = regexp.MustCompile(`^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?\(`)
	jsMethodPattern    = regexp.MustCompile(`^(\s*)(?:async\s+)?(\w+)\s*\([^)]*\)\s*{`)
	jsClassPattern     = regexp.MustCompile(`^(?:export\s+)?class\s+(\w+)`)
	jsInterfacePattern = regexp.MustCompile(`^(?:export\s+)?interface\s+(\w+)`)
	jsTypePattern      = regexp.MustCompile(`^(?:export\s+)?type\s+(\w+)`)
)

func (e *Expander) findJSTSBlock(lines []string, lineIdx int) (int, int, string, string, string) {
	startLine := lineIdx
	var header, symbolName, symbolKind string

	for i := lineIdx; i >= 0 && (lineIdx-i) < e.maxBlockLines; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}

		// Check for function declaration
		if match := jsFuncPattern.FindStringSubmatch(trimmed); match != nil {
			startLine = i
			header = trimLine(trimmed, 120)
			symbolName = match[1]
			symbolKind = "function"
			break
		}

		// Check for arrow function
		if match := jsArrowPattern.FindStringSubmatch(trimmed); match != nil {
			startLine = i
			header = trimLine(trimmed, 120)
			symbolName = match[1]
			symbolKind = "function"
			break
		}

		// Check for class
		if match := jsClassPattern.FindStringSubmatch(trimmed); match != nil {
			startLine = i
			header = trimLine(trimmed, 120)
			symbolName = match[1]
			symbolKind = "class"
			break
		}

		// Check for interface (TS)
		if match := jsInterfacePattern.FindStringSubmatch(trimmed); match != nil {
			startLine = i
			header = trimLine(trimmed, 120)
			symbolName = match[1]
			symbolKind = "interface"
			break
		}

		// Check for type (TS)
		if match := jsTypePattern.FindStringSubmatch(trimmed); match != nil {
			startLine = i
			header = trimLine(trimmed, 120)
			symbolName = match[1]
			symbolKind = "type"
			break
		}

		// Check for method in class/object
		if match := jsMethodPattern.FindStringSubmatch(lines[i]); match != nil {
			// Only use method if it's at the match line or before
			startLine = i
			header = trimLine(trimmed, 120)
			symbolName = match[2]
			symbolKind = "method"
			break
		}
	}

	if header == "" {
		return e.findGenericBlock(lines, lineIdx)
	}

	// Find end using brace matching
	endLine := e.findBraceEnd(lines, startLine)

	if endLine-startLine > e.maxBlockLines {
		endLine = startLine + e.maxBlockLines
	}

	return startLine, endLine, header, symbolName, symbolKind
}

// GDScript patterns
var (
	gdFuncPattern   = regexp.MustCompile(`^(\s*)func\s+(\w+)`)
	gdClassPattern  = regexp.MustCompile(`^class_name\s+(\w+)`)
	gdInnerClass    = regexp.MustCompile(`^(\s*)class\s+(\w+)`)
	gdSignalPattern = regexp.MustCompile(`^signal\s+(\w+)`)
)

func (e *Expander) findGDScriptBlock(lines []string, lineIdx int) (int, int, string, string, string) {
	matchIndent := getIndentLevel(lines[lineIdx])
	startLine := lineIdx
	var header, symbolName, symbolKind string
	var defIndent int

	for i := lineIdx; i >= 0 && (lineIdx-i) < e.maxBlockLines; i-- {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		lineIndent := getIndentLevel(line)

		// Check for func
		if match := gdFuncPattern.FindStringSubmatch(line); match != nil {
			defIndent = len(match[1])
			if defIndent <= matchIndent || i == lineIdx {
				startLine = i
				header = trimLine(trimmed, 120)
				symbolName = match[2]
				symbolKind = "function"
				break
			}
		}

		// Check for inner class
		if match := gdInnerClass.FindStringSubmatch(line); match != nil {
			defIndent = len(match[1])
			if defIndent <= matchIndent || i == lineIdx {
				startLine = i
				header = trimLine(trimmed, 120)
				symbolName = match[2]
				symbolKind = "class"
				break
			}
		}

		// Check for class_name (top-level)
		if match := gdClassPattern.FindStringSubmatch(trimmed); match != nil {
			startLine = i
			header = trimLine(trimmed, 120)
			symbolName = match[1]
			symbolKind = "class"
			break
		}

		// Check for signal
		if match := gdSignalPattern.FindStringSubmatch(trimmed); match != nil {
			// Signals are single-line, but we might be in a connected function
			continue
		}

		// Stop at lower indentation
		if lineIndent < matchIndent && trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			break
		}
	}

	if header == "" {
		return e.findGenericBlock(lines, lineIdx)
	}

	// Find end using indentation (GDScript uses Python-like indentation)
	endLine := e.findIndentEnd(lines, startLine, defIndent)

	if endLine-startLine > e.maxBlockLines {
		endLine = startLine + e.maxBlockLines
	}

	return startLine, endLine, header, symbolName, symbolKind
}

// findGenericBlock uses blank-line boundaries for unknown languages.
func (e *Expander) findGenericBlock(lines []string, lineIdx int) (int, int, string, string, string) {
	// Search upward for blank line or file start
	startLine := lineIdx
	for i := lineIdx - 1; i >= 0 && (lineIdx-i) < e.maxBlockLines/2; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			startLine = i + 1
			break
		}
		startLine = i
	}

	// Search downward for blank line or file end
	endLine := lineIdx
	for i := lineIdx + 1; i < len(lines) && (i-lineIdx) < e.maxBlockLines/2; i++ {
		if strings.TrimSpace(lines[i]) == "" {
			endLine = i - 1
			break
		}
		endLine = i
	}

	// Get first non-empty line as header
	header := ""
	for i := startLine; i <= endLine; i++ {
		if trimmed := strings.TrimSpace(lines[i]); trimmed != "" {
			header = trimLine(trimmed, 120)
			break
		}
	}

	return startLine, endLine, header, "", ""
}

// findBraceEnd finds the closing brace for a block starting at startLine.
func (e *Expander) findBraceEnd(lines []string, startLine int) int {
	depth := 0
	foundOpen := false
	endLine := startLine

	for i := startLine; i < len(lines) && (i-startLine) < e.maxBlockLines; i++ {
		line := lines[i]

		// Count braces, ignoring strings (simple heuristic)
		inString := false
		var stringChar rune
		for j, ch := range line {
			// Simple string detection (doesn't handle all edge cases)
			if (ch == '"' || ch == '\'' || ch == '`') && (j == 0 || line[j-1] != '\\') {
				if !inString {
					inString = true
					stringChar = ch
				} else if ch == stringChar {
					inString = false
				}
				continue
			}

			if inString {
				continue
			}

			if ch == '{' {
				depth++
				foundOpen = true
			} else if ch == '}' {
				depth--
				if foundOpen && depth == 0 {
					return i
				}
			}
		}
		endLine = i
	}

	return endLine
}

// findIndentEnd finds the end of an indentation-based block.
func (e *Expander) findIndentEnd(lines []string, startLine int, baseIndent int) int {
	endLine := startLine

	// Skip the definition line itself
	for i := startLine + 1; i < len(lines) && (i-startLine) < e.maxBlockLines; i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Empty lines don't end blocks
		if trimmed == "" {
			endLine = i
			continue
		}

		// Comment lines don't end blocks
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
			endLine = i
			continue
		}

		indent := getIndentLevel(line)

		// If we're back to the same or lower indentation, block ended at previous line
		if indent <= baseIndent {
			break
		}

		endLine = i
	}

	return endLine
}

// getIndentLevel returns the number of leading whitespace characters.
func getIndentLevel(line string) int {
	count := 0
	for _, ch := range line {
		if ch == ' ' {
			count++
		} else if ch == '\t' {
			count += 4 // Treat tabs as 4 spaces
		} else {
			break
		}
	}
	return count
}

// trimLine truncates a line to the specified limit.
func trimLine(line string, limit int) string {
	if len(line) <= limit {
		return line
	}
	return line[:limit] + "..."
}
