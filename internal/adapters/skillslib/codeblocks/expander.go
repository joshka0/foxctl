package codeblocks

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strings"

	codeexpander "github.com/jkatigb/agentctl/internal/intelligence/codecontext/expander"
	"github.com/jkatigb/agentctl/internal/platform/fsutil"
)

// Language represents a supported programming language.
type Language string

const (
	LangGo       Language = "go"
	LangPython   Language = "python"
	LangJS       Language = "javascript"
	LangTS       Language = "typescript"
	LangGDScript Language = "gdscript"
	LangElixir   Language = "elixir"
	LangRuby     Language = "ruby"
	LangLua      Language = "lua"
	LangGeneric  Language = "generic"
)

// Target controls what kind of symbol boundaries to expand to.
type Target string

const (
	TargetAny      Target = ""
	TargetBlock    Target = "block"
	TargetFunction Target = "function"
	TargetClass    Target = "class"
)

// ExpanderOption configures the behavior of an Expander.
type ExpanderOption func(*Expander)

// WithTarget sets the target symbol expansion preference.
func WithTarget(target Target) ExpanderOption {
	return func(e *Expander) {
		e.target = target
	}
}

// RawMatch is a single search match before expansion.
type RawMatch struct {
	File    string
	Line    int
	EndLine int
	Text    string
}

// Block represents an expanded code block containing one or more matches.
type Block struct {
	File       string `json:"file"`
	Language   string `json:"language"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	HeaderLine string `json:"header_line,omitempty"`
	SymbolName string `json:"symbol_name,omitempty"`
	SymbolKind string `json:"symbol_kind,omitempty"`
	Source     string `json:"source"`
	MatchLines []int  `json:"match_lines"`
	MatchCount int    `json:"match_count"`
}

// BlockPreview is a smaller version of Block for inline envelope data.
type BlockPreview struct {
	File       string `json:"file"`
	Language   string `json:"language"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	HeaderLine string `json:"header_line,omitempty"`
	SymbolName string `json:"symbol_name,omitempty"`
	SymbolKind string `json:"symbol_kind,omitempty"`
	MatchCount int    `json:"match_count"`
}

// PrepareBlockPreview builds preview entries from expanded blocks.
func PrepareBlockPreview(blocks []Block) []BlockPreview {
	previews := make([]BlockPreview, 0, len(blocks))
	for _, b := range blocks {
		previews = append(previews, BlockPreview{
			File:       b.File,
			Language:   b.Language,
			StartLine:  b.StartLine,
			EndLine:    b.EndLine,
			HeaderLine: b.HeaderLine,
			SymbolName: b.SymbolName,
			SymbolKind: b.SymbolKind,
			MatchCount: b.MatchCount,
		})
	}
	return previews
}

// Expander handles expanding matches to code blocks for a specific language.
type Expander struct {
	lang          Language
	maxBlockLines int
	target        Target
	goIndex       *goIndex
}

// NewExpander creates a new Expander for the given language.
func NewExpander(lang Language, maxBlockLines int, opts ...ExpanderOption) *Expander {
	if maxBlockLines <= 0 {
		maxBlockLines = 400
	}
	exp := &Expander{
		lang:          lang,
		maxBlockLines: maxBlockLines,
	}
	for _, opt := range opts {
		opt(exp)
	}
	return exp
}

// DetectLanguage determines the language from a file path.
func DetectLanguage(path string) Language {
	switch fsutil.DetectLanguage(path) {
	case "go":
		return LangGo
	case "python":
		return LangPython
	case "javascript":
		return LangJS
	case "typescript":
		return LangTS
	case "gdscript":
		return LangGDScript
	case "elixir":
		return LangElixir
	case "ruby":
		return LangRuby
	case "lua":
		return LangLua
	default:
		return LangGeneric
	}
}

// ExpandMatches expands raw matches into code blocks, deduplicating overlapping regions.
func (e *Expander) ExpandMatches(file string, lines []string, matches []RawMatch) []Block {
	if len(matches) == 0 || len(lines) == 0 {
		return nil
	}

	if e.lang == LangGo {
		e.goIndex = buildGoIndex(lines)
	} else {
		e.goIndex = nil
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
		matchLines := matchLinesFor(m)

		sameSymbol := currentBlock != nil &&
			start <= currentBlock.endLine &&
			currentBlock.symbolName == symbolName &&
			currentBlock.symbolKind == symbolKind &&
			currentBlock.headerLine == header

		// Check if this match belongs to the current block
		if sameSymbol {
			// Extend current block if needed
			if end > currentBlock.endLine {
				currentBlock.endLine = end
			}
			currentBlock.matchLines = append(currentBlock.matchLines, matchLines...)
			continue
		}

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
			matchLines: matchLines,
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

	matchLines := uniqueSortedInts(b.matchLines)

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
		MatchLines: matchLines,
		MatchCount: len(matchLines),
	}
}

// findBlockBoundaries finds the start and end lines of the code block containing lineIdx.
// Returns (startLine, endLine, headerLine, symbolName, symbolKind) using 0-indexed lines.
func (e *Expander) findBlockBoundaries(lines []string, lineIdx int) (int, int, string, string, string) {
	if e.target == TargetBlock {
		return e.findGenericBlock(lines, lineIdx)
	}

	var (
		startLine  int
		endLine    int
		header     string
		symbolName string
		symbolKind string
	)

	switch e.lang {
	case LangGo:
		startLine, endLine, header, symbolName, symbolKind = e.findGoBlock(lines, lineIdx)
	case LangPython:
		startLine, endLine, header, symbolName, symbolKind = e.findPythonBlock(lines, lineIdx)
	case LangJS, LangTS:
		startLine, endLine, header, symbolName, symbolKind = e.findJSTSBlock(lines, lineIdx)
	case LangGDScript:
		startLine, endLine, header, symbolName, symbolKind = e.findGDScriptBlock(lines, lineIdx)
	case LangElixir:
		startLine, endLine, header, symbolName, symbolKind = e.findElixirBlock(lines, lineIdx)
	case LangRuby:
		startLine, endLine, header, symbolName, symbolKind = e.findRubyBlock(lines, lineIdx)
	case LangLua:
		startLine, endLine, header, symbolName, symbolKind = e.findLuaBlock(lines, lineIdx)
	default:
		return e.findGenericBlock(lines, lineIdx)
	}

	if lineIdx < startLine || lineIdx > endLine {
		return e.findGenericBlock(lines, lineIdx)
	}

	return startLine, endLine, header, symbolName, symbolKind
}

// goIndex caches parsed Go symbols for AST-based range selection.
type goIndex struct {
	funcs    []goCandidate
	types    []goCandidate
	closures []goCandidate
}

// goCandidate represents a symbol span in Go source.
type goCandidate struct {
	startLine  int
	endLine    int
	headerLine int
	symbolName string
	symbolKind string
}

func buildGoIndex(lines []string) *goIndex {
	if len(lines) == 0 {
		return nil
	}
	src := strings.Join(lines, "\n")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if file == nil {
		return nil
	}
	if err != nil {
		// Best-effort parsing; keep partial AST when possible.
		_ = err
	}

	idx := &goIndex{}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			headerLine := lineForPos(fset, d.Pos()) - 1
			if headerLine < 0 {
				continue
			}
			startLine := headerLine
			if d.Doc != nil {
				if docLine := lineForPos(fset, d.Doc.Pos()) - 1; docLine >= 0 && docLine < startLine {
					startLine = docLine
				}
			}
			endLine := lineForPos(fset, d.End()) - 1
			if endLine < headerLine {
				endLine = headerLine
			}
			kind := "function"
			if d.Recv != nil {
				kind = "method"
			}
			idx.funcs = append(idx.funcs, goCandidate{
				startLine:  startLine,
				endLine:    endLine,
				headerLine: headerLine,
				symbolName: d.Name.Name,
				symbolKind: kind,
			})
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				headerLine := lineForPos(fset, ts.Pos()) - 1
				if headerLine < 0 {
					continue
				}
				startLine := headerLine
				if ts.Doc != nil {
					if docLine := lineForPos(fset, ts.Doc.Pos()) - 1; docLine >= 0 && docLine < startLine {
						startLine = docLine
					}
				} else if d.Doc != nil {
					if docLine := lineForPos(fset, d.Doc.Pos()) - 1; docLine >= 0 && docLine < startLine {
						startLine = docLine
					}
				}
				endLine := lineForPos(fset, ts.End()) - 1
				if endLine < headerLine {
					endLine = headerLine
				}
				idx.types = append(idx.types, goCandidate{
					startLine:  startLine,
					endLine:    endLine,
					headerLine: headerLine,
					symbolName: ts.Name.Name,
					symbolKind: "type",
				})
			}
		}
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.AssignStmt:
			for i, rhs := range n.Rhs {
				funcLit, ok := rhs.(*ast.FuncLit)
				if !ok || i >= len(n.Lhs) {
					continue
				}
				ident, ok := n.Lhs[i].(*ast.Ident)
				if !ok {
					continue
				}
				headerLine := lineForPos(fset, n.Pos()) - 1
				if headerLine < 0 {
					continue
				}
				endLine := lineForPos(fset, funcLit.End()) - 1
				if endLine < headerLine {
					endLine = headerLine
				}
				idx.closures = append(idx.closures, goCandidate{
					startLine:  headerLine,
					endLine:    endLine,
					headerLine: headerLine,
					symbolName: ident.Name,
					symbolKind: "closure",
				})
			}
		case *ast.ValueSpec:
			for i, val := range n.Values {
				funcLit, ok := val.(*ast.FuncLit)
				if !ok || i >= len(n.Names) {
					continue
				}
				headerLine := lineForPos(fset, n.Pos()) - 1
				if headerLine < 0 {
					continue
				}
				endLine := lineForPos(fset, funcLit.End()) - 1
				if endLine < headerLine {
					endLine = headerLine
				}
				idx.closures = append(idx.closures, goCandidate{
					startLine:  headerLine,
					endLine:    endLine,
					headerLine: headerLine,
					symbolName: n.Names[i].Name,
					symbolKind: "closure",
				})
			}
		}
		return true
	})

	for i := range idx.closures {
		idx.closures[i].startLine = includeLeadingLineComments(lines, idx.closures[i].startLine)
	}

	return idx
}

func (idx *goIndex) find(lineIdx int, target Target) (goCandidate, bool) {
	if idx == nil {
		return goCandidate{}, false
	}

	var candidates []goCandidate
	switch target {
	case TargetFunction:
		candidates = append(candidates, idx.closures...)
		candidates = append(candidates, idx.funcs...)
	case TargetClass:
		candidates = append(candidates, idx.types...)
	default:
		candidates = append(candidates, idx.closures...)
		candidates = append(candidates, idx.funcs...)
		candidates = append(candidates, idx.types...)
	}

	var best goCandidate
	found := false
	bestSpan := 0
	for _, c := range candidates {
		if lineIdx < c.startLine || lineIdx > c.endLine {
			continue
		}
		span := c.endLine - c.startLine
		if !found || span < bestSpan {
			best = c
			bestSpan = span
			found = true
		}
	}

	return best, found
}

func lineForPos(fset *token.FileSet, pos token.Pos) int {
	if pos == token.NoPos {
		return 0
	}
	return fset.Position(pos).Line
}

// Go patterns
var (
	goFuncPattern    = regexp.MustCompile(`^func\s+(\w+)`)
	goMethodPattern  = regexp.MustCompile(`^func\s+\([^)]+\)\s+(\w+)`)
	goTypePattern    = regexp.MustCompile(`^type\s+(\w+)`)
	goClosurePattern = regexp.MustCompile(`^(\w+)\s*(?::=|=)\s*func\s*\(`)
)

func (e *Expander) findGoBlock(lines []string, lineIdx int) (int, int, string, string, string) {
	if e.goIndex != nil {
		if candidate, ok := e.goIndex.find(lineIdx, e.target); ok {
			header := headerForLine(lines, candidate.headerLine)
			startLine := candidate.startLine
			endLine := candidate.endLine
			if lineIdx < startLine || lineIdx > endLine {
				return e.findGenericBlock(lines, lineIdx)
			}
			startLine, endLine = clampRange(startLine, endLine, lineIdx, e.maxBlockLines, candidate.headerLine)
			return startLine, endLine, header, candidate.symbolName, candidate.symbolKind
		}
	}

	wantFunctions := e.target == TargetAny || e.target == TargetFunction
	wantClasses := e.target == TargetAny || e.target == TargetClass

	// Search upward for func/type declaration
	startLine := lineIdx
	headerLine := lineIdx
	var header, symbolName, symbolKind string

	for i := lineIdx; i >= 0 && (lineIdx-i) < e.maxBlockLines; i-- {
		trimmed := strings.TrimSpace(lines[i])

		if wantFunctions {
			if match := goClosurePattern.FindStringSubmatch(trimmed); match != nil {
				closureEnd := e.findBraceEnd(lines, i)
				if closureEnd >= lineIdx {
					headerLine = i
					startLine = includeLeadingLineComments(lines, i)
					header = TrimLine(trimmed, 120)
					symbolName = match[1]
					symbolKind = "closure"
					endLine := closureEnd
					startLine, endLine = clampRange(startLine, endLine, lineIdx, e.maxBlockLines, headerLine)
					return startLine, endLine, header, symbolName, symbolKind
				}
			}
			if match := goMethodPattern.FindStringSubmatch(trimmed); match != nil {
				headerLine = i
				startLine = includeLeadingLineComments(lines, i)
				header = TrimLine(trimmed, 120)
				symbolName = match[1]
				symbolKind = "method"
				break
			}
			if match := goFuncPattern.FindStringSubmatch(trimmed); match != nil {
				headerLine = i
				startLine = includeLeadingLineComments(lines, i)
				header = TrimLine(trimmed, 120)
				symbolName = match[1]
				symbolKind = "function"
				break
			}
		}
		if wantClasses {
			if match := goTypePattern.FindStringSubmatch(trimmed); match != nil {
				headerLine = i
				startLine = includeLeadingLineComments(lines, i)
				header = TrimLine(trimmed, 120)
				symbolName = match[1]
				symbolKind = "type"
				break
			}
		}
	}

	// If no header found, fall back to blank-line boundaries
	if header == "" {
		return e.findGenericBlock(lines, lineIdx)
	}

	// Find end using brace matching
	endLine := e.findBraceEnd(lines, headerLine)
	if lineIdx < startLine || lineIdx > endLine {
		return e.findGenericBlock(lines, lineIdx)
	}
	startLine, endLine = clampRange(startLine, endLine, lineIdx, e.maxBlockLines, headerLine)

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
	matchIndent := IndentLevel(lines[lineIdx])
	startLine := lineIdx
	headerLine := lineIdx
	var header, symbolName, symbolKind string
	var defIndent int

	wantFunctions := e.target == TargetAny || e.target == TargetFunction
	wantClasses := e.target == TargetAny || e.target == TargetClass

	for i := lineIdx; i >= 0 && (lineIdx-i) < e.maxBlockLines; i-- {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		lineIndent := IndentLevel(line)

		// Check for async def first (must come before def check)
		if wantFunctions {
			if match := pyAsyncPattern.FindStringSubmatch(line); match != nil {
				defIndent = len(match[1])
				if defIndent <= matchIndent || i == lineIdx {
					headerLine = i
					startLine = includePythonDecorators(lines, i, defIndent)
					header = TrimLine(trimmed, 120)
					symbolName = match[2]
					symbolKind = "function"
					break
				}
			}

			// Check for def
			if match := pyFuncPattern.FindStringSubmatch(line); match != nil {
				defIndent = len(match[1])
				if defIndent <= matchIndent || i == lineIdx {
					headerLine = i
					startLine = includePythonDecorators(lines, i, defIndent)
					header = TrimLine(trimmed, 120)
					symbolName = match[2]
					symbolKind = "function"
					break
				}
			}
		}

		// Check for class
		if wantClasses {
			if match := pyClassPattern.FindStringSubmatch(line); match != nil {
				defIndent = len(match[1])
				if defIndent <= matchIndent || i == lineIdx {
					headerLine = i
					startLine = includePythonDecorators(lines, i, defIndent)
					header = TrimLine(trimmed, 120)
					symbolName = match[2]
					symbolKind = "class"
					break
				}
			}
		}

		// Stop at lower indentation non-definition (probably left the block)
		if lineIndent < matchIndent && trimmed != "" && !wantClasses {
			break
		}
	}

	if header == "" {
		return e.findGenericBlock(lines, lineIdx)
	}

	// Find end using indentation
	endLine := e.findIndentEnd(lines, headerLine, defIndent)
	if lineIdx < startLine || lineIdx > endLine {
		return e.findGenericBlock(lines, lineIdx)
	}
	startLine, endLine = clampRange(startLine, endLine, lineIdx, e.maxBlockLines, headerLine)

	return startLine, endLine, header, symbolName, symbolKind
}

// JS/TS patterns
var (
	jsFuncPattern           = regexp.MustCompile(`^(?:export\s+)?(?:async\s+)?function\s+(\w+)`)
	jsDefaultFuncPattern    = regexp.MustCompile(`^export\s+default\s+(?:async\s+)?function(?:\s+(\w+))?`)
	jsArrowParenPattern     = regexp.MustCompile(`^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?\([^)]*\)\s*=>`)
	jsArrowNoParenPattern   = regexp.MustCompile(`^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?[A-Za-z_$][\w$]*\s*=>`)
	jsArrowPattern          = regexp.MustCompile(`^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?(?:\([^)]*\)|[A-Za-z_$][\w$]*)\s*=>`)
	jsArrowPropertyPattern  = regexp.MustCompile(`^(\s*)([A-Za-z_$][\w$]*)\s*=\s*(?:async\s+)?(?:\([^)]*\)|[A-Za-z_$][\w$]*)\s*=>`)
	jsMethodPattern         = regexp.MustCompile(`^(\s*)(?:async\s+)?(\w+)\s*\([^)]*\)\s*{`)
	jsClassPattern          = regexp.MustCompile(`^(?:export\s+)?class\s+(\w+)`)
	jsInterfacePattern      = regexp.MustCompile(`^(?:export\s+)?interface\s+(\w+)`)
	jsTypePattern           = regexp.MustCompile(`^(?:export\s+)?type\s+(\w+)`)
	jsStatementStartPattern = regexp.MustCompile(`^\s*(?:const|let|var|class|function|export|interface|type|import|return|if|for|while|switch|try|catch|else|do|throw|break|continue)\b`)
)

func (e *Expander) findJSTSBlock(lines []string, lineIdx int) (int, int, string, string, string) {
	startLine := lineIdx
	headerLine := lineIdx
	var header, symbolName, symbolKind string

	wantFunctions := e.target == TargetAny || e.target == TargetFunction
	wantClasses := e.target == TargetAny || e.target == TargetClass

	for i := lineIdx; i >= 0 && (lineIdx-i) < e.maxBlockLines; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}

		if wantFunctions {
			// Check for default export function
			if match := jsDefaultFuncPattern.FindStringSubmatch(trimmed); match != nil {
				startLine = i
				headerLine = i
				header = TrimLine(trimmed, 120)
				symbolName = match[1]
				if symbolName == "" {
					symbolName = "default"
				}
				symbolKind = "function"
				break
			}

			// Check for function declaration
			if match := jsFuncPattern.FindStringSubmatch(trimmed); match != nil {
				startLine = i
				headerLine = i
				header = TrimLine(trimmed, 120)
				symbolName = match[1]
				symbolKind = "function"
				break
			}

			// Check for arrow function assignments
			if match := jsArrowParenPattern.FindStringSubmatch(trimmed); match != nil {
				startLine = i
				headerLine = i
				header = TrimLine(trimmed, 120)
				symbolName = match[1]
				symbolKind = "function"
				break
			}
			if match := jsArrowNoParenPattern.FindStringSubmatch(trimmed); match != nil {
				startLine = i
				headerLine = i
				header = TrimLine(trimmed, 120)
				symbolName = match[1]
				symbolKind = "function"
				break
			}
			if match := jsArrowPattern.FindStringSubmatch(trimmed); match != nil {
				startLine = i
				headerLine = i
				header = TrimLine(trimmed, 120)
				symbolName = match[1]
				symbolKind = "function"
				break
			}

			// Check for class field arrow functions
			if match := jsArrowPropertyPattern.FindStringSubmatch(trimmed); match != nil {
				startLine = i
				headerLine = i
				header = TrimLine(trimmed, 120)
				symbolName = match[2]
				symbolKind = "method"
				break
			}

			// Check for method in class/object
			if match := jsMethodPattern.FindStringSubmatch(lines[i]); match != nil {
				// Only use method if it is at the match line or before.
				startLine = i
				headerLine = i
				header = TrimLine(trimmed, 120)
				symbolName = match[2]
				symbolKind = "method"
				break
			}
		}

		if wantClasses {
			// Check for class
			if match := jsClassPattern.FindStringSubmatch(trimmed); match != nil {
				startLine = i
				headerLine = i
				header = TrimLine(trimmed, 120)
				symbolName = match[1]
				symbolKind = "class"
				break
			}

			// Check for interface (TS)
			if match := jsInterfacePattern.FindStringSubmatch(trimmed); match != nil {
				startLine = i
				headerLine = i
				header = TrimLine(trimmed, 120)
				symbolName = match[1]
				symbolKind = "interface"
				break
			}

			// Check for type (TS)
			if match := jsTypePattern.FindStringSubmatch(trimmed); match != nil {
				startLine = i
				headerLine = i
				header = TrimLine(trimmed, 120)
				symbolName = match[1]
				symbolKind = "type"
				break
			}
		}
	}

	if header == "" {
		return e.findGenericBlock(lines, lineIdx)
	}

	endLine := startLine
	switch symbolKind {
	case "function", "method", "class", "interface":
		endLine = e.findBraceEnd(lines, startLine)
	case "type":
		if strings.Contains(header, "{") {
			endLine = e.findBraceEnd(lines, startLine)
		}
	}

	if symbolKind == "function" || symbolKind == "method" {
		if strings.Contains(header, "=>") && !strings.Contains(header, "{") {
			if arrowBodyHasBrace(lines, startLine) {
				endLine = e.findBraceEnd(lines, startLine)
			} else {
				endLine = e.findArrowExpressionEnd(lines, startLine)
			}
		}
	}

	if lineIdx < startLine || lineIdx > endLine {
		return e.findGenericBlock(lines, lineIdx)
	}
	startLine, endLine = clampRange(startLine, endLine, lineIdx, e.maxBlockLines, headerLine)

	return startLine, endLine, header, symbolName, symbolKind
}

func (e *Expander) findArrowExpressionEnd(lines []string, startLine int) int {
	endLine := startLine
	maxLines := e.maxBlockLines
	if maxLines <= 0 {
		maxLines = len(lines)
	}
	limit := len(lines)
	if startLine+maxLines < limit {
		limit = startLine + maxLines
	}
	startIndent := IndentLevel(lines[startLine])
	for i := startLine + 1; i < limit; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			break
		}
		indent := IndentLevel(lines[i])
		if indent <= startIndent && jsStatementStartPattern.MatchString(lines[i]) {
			break
		}
		endLine = i
		if strings.Contains(lines[i], ";") {
			break
		}
	}
	return endLine
}

func arrowBodyHasBrace(lines []string, startLine int) bool {
	for i := startLine; i < len(lines) && i <= startLine+3; i++ {
		if strings.Contains(lines[i], "{") {
			return true
		}
		if i > startLine && strings.TrimSpace(lines[i]) == "" {
			break
		}
	}
	return false
}

// Elixir patterns
var (
	exDefmodulePattern = regexp.MustCompile(`^\s*defmodule\s+([A-Za-z0-9_.]+)\b`)
	exDefPattern       = regexp.MustCompile(`^\s*(defp?|defmacro|defguardp?)\s+([a-zA-Z_]\w*[!?=]?)\b`)
	exFnAssignPattern  = regexp.MustCompile(`^\s*([A-Za-z_]\w*)\s*(?::=|=)\s*fn\b`)
)

func (e *Expander) findElixirBlock(lines []string, lineIdx int) (int, int, string, string, string) {
	startLine := lineIdx
	headerLine := lineIdx
	var header, symbolName, symbolKind string

	wantFunctions := e.target == TargetAny || e.target == TargetFunction
	wantClasses := e.target == TargetAny || e.target == TargetClass

	for i := lineIdx; i >= 0 && (lineIdx-i) < e.maxBlockLines; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}

		if wantFunctions {
			if match := exFnAssignPattern.FindStringSubmatch(trimmed); match != nil {
				startLine = i
				headerLine = i
				header = TrimLine(trimmed, 120)
				symbolName = match[1]
				symbolKind = "closure"
				break
			}
			if match := exDefPattern.FindStringSubmatch(trimmed); match != nil {
				startLine = i
				headerLine = i
				header = TrimLine(trimmed, 120)
				symbolName = match[2]
				symbolKind = "function"
				break
			}
		}

		if wantClasses {
			if match := exDefmodulePattern.FindStringSubmatch(trimmed); match != nil {
				startLine = i
				headerLine = i
				header = TrimLine(trimmed, 120)
				symbolName = match[1]
				symbolKind = "module"
				break
			}
		}
	}

	if header == "" {
		return e.findGenericBlock(lines, lineIdx)
	}

	endLine := findElixirEnd(lines, startLine, e.maxBlockLines)
	if lineIdx < startLine || lineIdx > endLine {
		return e.findGenericBlock(lines, lineIdx)
	}
	startLine, endLine = clampRange(startLine, endLine, lineIdx, e.maxBlockLines, headerLine)

	return startLine, endLine, header, symbolName, symbolKind
}

type elixirScanState struct {
	inString     bool
	stringDelim  byte
	inHeredoc    bool
	heredocDelim string
}

func findElixirEnd(lines []string, startLine int, maxLines int) int {
	endLine := startLine
	depth := 0
	sawOpener := false
	state := elixirScanState{}

	limit := len(lines)
	if maxLines > 0 && startLine+maxLines < limit {
		limit = startLine + maxLines
	}

	for i := startLine; i < limit; i++ {
		openers, closers := elixirBlockDelta(lines[i], &state)
		if openers > 0 {
			sawOpener = true
		}
		depth += openers
		depth -= closers
		endLine = i
		if sawOpener && depth <= 0 {
			return i
		}
	}

	if sawOpener {
		return endLine
	}
	return startLine
}

func elixirBlockDelta(line string, state *elixirScanState) (int, int) {
	openers := 0
	closers := 0

	for i := 0; i < len(line); {
		if state.inHeredoc {
			if strings.HasPrefix(line[i:], state.heredocDelim) {
				state.inHeredoc = false
				i += len(state.heredocDelim)
				continue
			}
			i++
			continue
		}

		if state.inString {
			if line[i] == '\\' {
				i += 2
				continue
			}
			if line[i] == state.stringDelim {
				state.inString = false
				state.stringDelim = 0
			}
			i++
			continue
		}

		if strings.HasPrefix(line[i:], "\"\"\"") || strings.HasPrefix(line[i:], "'''") {
			state.inHeredoc = true
			if strings.HasPrefix(line[i:], "\"\"\"") {
				state.heredocDelim = "\"\"\""
			} else {
				state.heredocDelim = "'''"
			}
			i += 3
			continue
		}

		if line[i] == '#' {
			break
		}

		if line[i] == '"' || line[i] == '\'' {
			state.inString = true
			state.stringDelim = line[i]
			i++
			continue
		}

		if isElixirIdentStart(line[i]) {
			start := i
			i++
			for i < len(line) && isElixirIdentPart(line[i]) {
				i++
			}
			tok := line[start:i]
			switch tok {
			case "do":
				if !isInlineDo(line, i) {
					openers++
				}
			case "fn":
				openers++
			case "end":
				closers++
			}
			continue
		}

		i++
	}

	return openers, closers
}

func isInlineDo(line string, pos int) bool {
	for pos < len(line) && (line[pos] == ' ' || line[pos] == '\t') {
		pos++
	}
	return pos < len(line) && line[pos] == ':'
}

func isElixirIdentStart(ch byte) bool {
	return ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func isElixirIdentPart(ch byte) bool {
	return isElixirIdentStart(ch) || (ch >= '0' && ch <= '9')
}

// Ruby patterns
var (
	rbClassPattern   = regexp.MustCompile(`^\s*class\s+([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)`)
	rbModulePattern  = regexp.MustCompile(`^\s*module\s+([A-Za-z_]\w*(?:::[A-Za-z_]\w*)*)`)
	rbDefPattern     = regexp.MustCompile(`^\s*def\s+(?:self\.)?([A-Za-z_]\w*[!?=]?)`)
	rbClosurePattern = regexp.MustCompile(`^\s*([A-Za-z_]\w*)\s*=\s*(?:->|lambda\b)`)
)

func (e *Expander) findRubyBlock(lines []string, lineIdx int) (int, int, string, string, string) {
	startLine := lineIdx
	headerLine := lineIdx
	var header, symbolName, symbolKind string

	wantFunctions := e.target == TargetAny || e.target == TargetFunction
	wantClasses := e.target == TargetAny || e.target == TargetClass

	for i := lineIdx; i >= 0 && (lineIdx-i) < e.maxBlockLines; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}

		if wantFunctions {
			if match := rbClosurePattern.FindStringSubmatch(trimmed); match != nil {
				startLine = i
				headerLine = i
				header = TrimLine(trimmed, 120)
				symbolName = match[1]
				symbolKind = "closure"
				break
			}
			if match := rbDefPattern.FindStringSubmatch(trimmed); match != nil {
				startLine = i
				headerLine = i
				header = TrimLine(trimmed, 120)
				symbolName = match[1]
				symbolKind = "function"
				break
			}
		}

		if wantClasses {
			if match := rbClassPattern.FindStringSubmatch(trimmed); match != nil {
				startLine = i
				headerLine = i
				header = TrimLine(trimmed, 120)
				symbolName = match[1]
				symbolKind = "class"
				break
			}
			if match := rbModulePattern.FindStringSubmatch(trimmed); match != nil {
				startLine = i
				headerLine = i
				header = TrimLine(trimmed, 120)
				symbolName = match[1]
				symbolKind = "module"
				break
			}
		}
	}

	if header == "" {
		return e.findGenericBlock(lines, lineIdx)
	}

	endLine := findRubyEnd(lines, startLine, e.maxBlockLines)
	if lineIdx < startLine || lineIdx > endLine {
		return e.findGenericBlock(lines, lineIdx)
	}
	startLine, endLine = clampRange(startLine, endLine, lineIdx, e.maxBlockLines, headerLine)

	return startLine, endLine, header, symbolName, symbolKind
}

// Ruby block scanning (do/end).
type rubyScanState struct {
	inString    bool
	stringDelim byte
}

func findRubyEnd(lines []string, startLine int, maxLines int) int {
	endLine := startLine
	depth := 0
	sawOpener := false
	state := rubyScanState{}

	limit := len(lines)
	if maxLines > 0 && startLine+maxLines < limit {
		limit = startLine + maxLines
	}

	for i := startLine; i < limit; i++ {
		openers, closers := rubyBlockDelta(lines[i], &state)
		if openers > 0 {
			sawOpener = true
		}
		depth += openers
		depth -= closers
		endLine = i
		if sawOpener && depth <= 0 {
			return i
		}
	}

	if sawOpener {
		return endLine
	}
	return startLine
}

func rubyBlockDelta(line string, state *rubyScanState) (int, int) {
	openers := 0
	closers := 0

	for i := 0; i < len(line); {
		if state.inString {
			if line[i] == '\\' {
				i += 2
				continue
			}
			if line[i] == state.stringDelim {
				state.inString = false
				state.stringDelim = 0
			}
			i++
			continue
		}

		if line[i] == '#' {
			break
		}

		if line[i] == '"' || line[i] == '\'' {
			state.inString = true
			state.stringDelim = line[i]
			i++
			continue
		}

		if isRubyIdentStart(line[i]) {
			start := i
			i++
			for i < len(line) && isRubyIdentPart(line[i]) {
				i++
			}
			tok := line[start:i]
			switch tok {
			case "def", "class", "module", "case", "begin", "do", "for":
				openers++
			case "if", "unless", "while", "until":
				if isRubyStatementStart(line, start) {
					openers++
				}
			case "end":
				closers++
			}
			continue
		}

		i++
	}

	return openers, closers
}

func isRubyStatementStart(line string, start int) bool {
	if start <= 0 {
		return true
	}
	for i := start - 1; i >= 0; i-- {
		switch line[i] {
		case ' ', '\t':
			continue
		case ';':
			return true
		default:
			return false
		}
	}
	return true
}

func isRubyIdentStart(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func isRubyIdentPart(b byte) bool {
	return isRubyIdentStart(b) || (b >= '0' && b <= '9')
}

// Lua patterns
var (
	luaFuncPattern       = regexp.MustCompile(`^\s*(?:local\s+)?function\s+([A-Za-z_][\w\.:]*)`)
	luaAssignFuncPattern = regexp.MustCompile(`^\s*(?:local\s+)?([A-Za-z_][\w\.:]*)\s*=\s*function\b`)
)

func (e *Expander) findLuaBlock(lines []string, lineIdx int) (int, int, string, string, string) {
	startLine := lineIdx
	headerLine := lineIdx
	var header, symbolName, symbolKind string

	wantFunctions := e.target == TargetAny || e.target == TargetFunction

	for i := lineIdx; i >= 0 && (lineIdx-i) < e.maxBlockLines; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}

		if wantFunctions {
			if match := luaAssignFuncPattern.FindStringSubmatch(trimmed); match != nil {
				startLine = i
				headerLine = i
				header = TrimLine(trimmed, 120)
				symbolName = match[1]
				symbolKind = "closure"
				break
			}
			if match := luaFuncPattern.FindStringSubmatch(trimmed); match != nil {
				startLine = i
				headerLine = i
				header = TrimLine(trimmed, 120)
				symbolName = match[1]
				symbolKind = "function"
				break
			}
		}
	}

	if header == "" {
		return e.findGenericBlock(lines, lineIdx)
	}

	endLine := findLuaEnd(lines, startLine, e.maxBlockLines)
	if lineIdx < startLine || lineIdx > endLine {
		return e.findGenericBlock(lines, lineIdx)
	}
	startLine, endLine = clampRange(startLine, endLine, lineIdx, e.maxBlockLines, headerLine)

	return startLine, endLine, header, symbolName, symbolKind
}

// Lua block scanning (function/end, repeat/until).
type luaScanState struct {
	inString       bool
	stringDelim    byte
	inLongString   bool
	inBlockComment bool
}

func findLuaEnd(lines []string, startLine int, maxLines int) int {
	endLine := startLine
	depth := 0
	sawOpener := false
	state := luaScanState{}

	limit := len(lines)
	if maxLines > 0 && startLine+maxLines < limit {
		limit = startLine + maxLines
	}

	for i := startLine; i < limit; i++ {
		openers, closers := luaBlockDelta(lines[i], &state)
		if openers > 0 {
			sawOpener = true
		}
		depth += openers
		depth -= closers
		endLine = i
		if sawOpener && depth <= 0 {
			return i
		}
	}

	if sawOpener {
		return endLine
	}
	return startLine
}

func luaBlockDelta(line string, state *luaScanState) (int, int) {
	openers := 0
	closers := 0

	for i := 0; i < len(line); {
		if state.inBlockComment {
			if strings.HasPrefix(line[i:], "]]") {
				state.inBlockComment = false
				i += 2
				continue
			}
			i++
			continue
		}

		if state.inLongString {
			if strings.HasPrefix(line[i:], "]]") {
				state.inLongString = false
				i += 2
				continue
			}
			i++
			continue
		}

		if state.inString {
			if line[i] == '\\' {
				i += 2
				continue
			}
			if line[i] == state.stringDelim {
				state.inString = false
				state.stringDelim = 0
			}
			i++
			continue
		}

		if strings.HasPrefix(line[i:], "--[[") {
			state.inBlockComment = true
			i += 4
			continue
		}
		if strings.HasPrefix(line[i:], "--") {
			break
		}

		if strings.HasPrefix(line[i:], "[[") {
			state.inLongString = true
			i += 2
			continue
		}

		if line[i] == '"' || line[i] == '\'' {
			state.inString = true
			state.stringDelim = line[i]
			i++
			continue
		}

		if isLuaIdentStart(line[i]) {
			start := i
			i++
			for i < len(line) && isLuaIdentPart(line[i]) {
				i++
			}
			tok := line[start:i]
			switch tok {
			case "function", "if", "for", "while", "repeat":
				openers++
			case "end", "until":
				closers++
			}
			continue
		}

		i++
	}

	return openers, closers
}

func isLuaIdentStart(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func isLuaIdentPart(b byte) bool {
	return isLuaIdentStart(b) || (b >= '0' && b <= '9')
}

// GDScript patterns

var (
	gdFuncPattern   = regexp.MustCompile(`^(\s*)func\s+(\w+)`)
	gdClassPattern  = regexp.MustCompile(`^class_name\s+(\w+)`)
	gdInnerClass    = regexp.MustCompile(`^(\s*)class\s+(\w+)`)
	gdSignalPattern = regexp.MustCompile(`^signal\s+(\w+)`)
)

func (e *Expander) findGDScriptBlock(lines []string, lineIdx int) (int, int, string, string, string) {
	matchIndent := IndentLevel(lines[lineIdx])
	startLine := lineIdx
	headerLine := lineIdx
	var header, symbolName, symbolKind string
	var defIndent int

	wantFunctions := e.target == TargetAny || e.target == TargetFunction
	wantClasses := e.target == TargetAny || e.target == TargetClass

	for i := lineIdx; i >= 0 && (lineIdx-i) < e.maxBlockLines; i-- {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		lineIndent := IndentLevel(line)

		if wantFunctions {
			// Check for func
			if match := gdFuncPattern.FindStringSubmatch(line); match != nil {
				defIndent = len(match[1])
				if defIndent <= matchIndent || i == lineIdx {
					startLine = i
					headerLine = i
					header = TrimLine(trimmed, 120)
					symbolName = match[2]
					symbolKind = "function"
					break
				}
			}
		}

		if wantClasses {
			// Check for inner class
			if match := gdInnerClass.FindStringSubmatch(line); match != nil {
				defIndent = len(match[1])
				if defIndent <= matchIndent || i == lineIdx {
					startLine = i
					headerLine = i
					header = TrimLine(trimmed, 120)
					symbolName = match[2]
					symbolKind = "class"
					break
				}
			}

			// Check for class_name (top-level)
			if match := gdClassPattern.FindStringSubmatch(trimmed); match != nil {
				defIndent = lineIndent
				startLine = i
				headerLine = i
				header = TrimLine(trimmed, 120)
				symbolName = match[1]
				symbolKind = "class"
				break
			}
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
	endLine := e.findIndentEnd(lines, headerLine, defIndent)
	if lineIdx < startLine || lineIdx > endLine {
		return e.findGenericBlock(lines, lineIdx)
	}

	startLine, endLine = clampRange(startLine, endLine, lineIdx, e.maxBlockLines, headerLine)

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
			header = TrimLine(trimmed, 120)
			break
		}
	}

	return startLine, endLine, header, "", ""
}

func braceStyleForLang(lang Language) codeexpander.BraceStyle {
	switch lang {
	case LangGo:
		return codeexpander.GoBraceStyle()
	case LangJS, LangTS:
		return codeexpander.JSBraceStyle()
	default:
		return codeexpander.DefaultBraceStyle()
	}
}

// findBraceEnd finds the closing brace for a block starting at startLine.
func (e *Expander) findBraceEnd(lines []string, startLine int) int {
	if startLine < 0 || startLine >= len(lines) {
		return startLine
	}

	style := braceStyleForLang(e.lang)
	endLine := codeexpander.FindBraceEnd(lines, startLine, style)
	if endLine >= 0 {
		return endLine
	}

	maxLines := e.maxBlockLines
	if maxLines <= 0 {
		return startLine
	}
	limit := len(lines)
	if startLine+maxLines < limit {
		limit = startLine + maxLines
	}
	for i := startLine; i < limit; i++ {
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

		indent := IndentLevel(line)

		// If we're back to the same or lower indentation, block ended at previous line
		if indent <= baseIndent {
			break
		}

		endLine = i
	}

	return endLine
}

// IndentLevel returns the number of leading whitespace characters.
func IndentLevel(line string) int {
	count := 0
	for _, ch := range line {
		switch ch {
		case ' ':
			count++
		case '\t':
			count += 4 // Treat tabs as 4 spaces
		default:
			return count
		}
	}
	return count
}

// TrimLine truncates a line to the specified limit.
func TrimLine(line string, limit int) string {
	if len(line) <= limit {
		return line
	}
	return line[:limit] + "..."
}

func matchLinesFor(match RawMatch) []int {
	if match.Line <= 0 {
		return nil
	}
	if match.EndLine <= 0 || match.EndLine < match.Line {
		return []int{match.Line}
	}
	lines := make([]int, 0, match.EndLine-match.Line+1)
	for line := match.Line; line <= match.EndLine; line++ {
		lines = append(lines, line)
	}
	return lines
}

func uniqueSortedInts(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	sorted := append([]int(nil), values...)
	sort.Ints(sorted)
	unique := sorted[:0]
	last := 0
	for i, v := range sorted {
		if i == 0 || v != last {
			unique = append(unique, v)
			last = v
		}
	}
	return unique
}

func clampRange(start, end, lineIdx, maxLines, headerLine int) (int, int) {
	if maxLines <= 0 {
		return start, end
	}
	if end < start {
		return start, end
	}
	span := end - start + 1
	if span <= maxLines {
		return start, end
	}

	if headerLine >= start && headerLine <= end {
		minIdx := minInt(lineIdx, headerLine)
		maxIdx := maxInt(lineIdx, headerLine)
		if maxIdx-minIdx+1 <= maxLines {
			windowStart := minIdx
			windowEnd := windowStart + maxLines - 1
			if windowEnd < maxIdx {
				windowEnd = maxIdx
				windowStart = windowEnd - maxLines + 1
			}
			if windowStart < start {
				windowStart = start
				windowEnd = windowStart + maxLines - 1
			}
			if windowEnd > end {
				windowEnd = end
				windowStart = windowEnd - maxLines + 1
			}
			return windowStart, windowEnd
		}
	}

	windowStart := lineIdx - maxLines/2
	windowEnd := windowStart + maxLines - 1
	if windowStart < start {
		windowStart = start
		windowEnd = windowStart + maxLines - 1
	}
	if windowEnd > end {
		windowEnd = end
		windowStart = windowEnd - maxLines + 1
	}
	return windowStart, windowEnd
}

func includeLeadingLineComments(lines []string, startLine int) int {
	if startLine <= 0 {
		return startLine
	}
	i := startLine - 1
	for i >= 0 {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "*/") {
			i--
			continue
		}
		break
	}
	return i + 1
}

func includePythonDecorators(lines []string, defLine, defIndent int) int {
	startLine := defLine
	for i := defLine - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			break
		}
		if IndentLevel(lines[i]) != defIndent {
			break
		}
		if strings.HasPrefix(trimmed, "@") {
			startLine = i
			continue
		}
		break
	}
	return startLine
}

func headerForLine(lines []string, lineIdx int) string {
	if lineIdx < 0 || lineIdx >= len(lines) {
		return ""
	}
	trimmed := strings.TrimSpace(lines[lineIdx])
	if trimmed == "" {
		return ""
	}
	return TrimLine(trimmed, 120)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
