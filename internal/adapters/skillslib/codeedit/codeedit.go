package codeedit

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/intelligence/codecontext/expander"
	platformfs "github.com/jkatigb/agentctl/internal/platform/fsutil"
)

// Edit defines a single smart edit operation.
type Edit struct {
	Type         string `json:"type" validate:"required,oneof=symbol lines replace"` // "symbol", "lines", "replace"
	Symbol       string `json:"symbol,omitempty"`
	StartLine    int    `json:"start_line,omitempty" validate:"omitempty,min=1"`
	EndLine      int    `json:"end_line,omitempty" validate:"omitempty,min=1"`
	Search       string `json:"search,omitempty"`
	Replace      string `json:"replace,omitempty"`
	WithinSymbol string `json:"within_symbol,omitempty"`
	NewCode      string `json:"new_code,omitempty"`
	All          bool   `json:"all,omitempty"`
}

// SymbolInfo holds info about a found symbol.
type SymbolInfo struct {
	Name      string
	Kind      string
	StartLine int // 0-indexed
	EndLine   int // 0-indexed
}

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

// ValidateEdits ensures edits are well-formed for their edit type.
func ValidateEdits(edits []Edit) error {
	for i, e := range edits {
		if strings.TrimSpace(e.Type) == "" {
			return skillerr.Arg(
				fmt.Sprintf("edits[%d].type is required", i),
				skillerr.WithHint("Use one of: symbol, lines, replace."),
			)
		}
		switch e.Type {
		case "symbol":
			if strings.TrimSpace(e.Symbol) == "" {
				return skillerr.Arg(
					fmt.Sprintf("edits[%d].symbol is required for type='symbol'", i),
					skillerr.WithHint("Provide edits[i].symbol and edits[i].new_code."),
				)
			}
			if e.NewCode == "" {
				return skillerr.Arg(
					fmt.Sprintf("edits[%d].new_code is required for type='symbol'", i),
					skillerr.WithHint("Provide edits[i].new_code with the full replacement code."),
				)
			}
		case "lines":
			if e.StartLine < 1 || e.EndLine < e.StartLine {
				return skillerr.Arg(
					fmt.Sprintf("invalid line range: %d-%d", e.StartLine, e.EndLine),
					skillerr.WithHint("Provide start_line >= 1 and end_line >= start_line."),
				)
			}
			if e.NewCode == "" {
				return skillerr.Arg(
					fmt.Sprintf("edits[%d].new_code is required for type='lines'", i),
					skillerr.WithHint("Provide edits[i].new_code with the replacement lines."),
				)
			}
		case "replace":
			if e.Search == "" {
				return skillerr.Arg(
					fmt.Sprintf("edits[%d].search is required for type='replace'", i),
					skillerr.WithHint("Provide edits[i].search with the text to replace."),
				)
			}
		default:
			return skillerr.Arg(
				fmt.Sprintf("unknown edit type: %s", e.Type),
				skillerr.WithHint("Use one of: symbol, lines, replace."),
			)
		}
	}

	return nil
}

// ApplyEdit applies a single edit operation.
func ApplyEdit(lines []string, lang Language, e Edit) ([]string, []string, bool, error) {
	switch e.Type {
	case "symbol":
		return ApplySymbolEdit(lines, lang, e)
	case "lines":
		return ApplyLinesEdit(lines, e)
	case "replace":
		return ApplyReplaceEdit(lines, lang, e)
	default:
		return nil, nil, false, skillerr.Arg(
			fmt.Sprintf("unknown edit type: %s", e.Type),
			skillerr.WithHint("Use one of: symbol, lines, replace."),
		)
	}
}

// ApplySymbolEdit replaces an entire symbol by name.
func ApplySymbolEdit(lines []string, lang Language, e Edit) ([]string, []string, bool, error) {
	if e.Symbol == "" {
		return nil, nil, false, skillerr.Arg(
			"symbol name required for type='symbol'",
			skillerr.WithHint("Provide edits[i].symbol and edits[i].new_code."),
		)
	}
	if e.NewCode == "" {
		return nil, nil, false, skillerr.Arg(
			"new_code required for type='symbol'",
			skillerr.WithHint("Provide edits[i].new_code with the full replacement code."),
		)
	}

	// Find the symbol
	symbols := FindSymbols(lines, lang)
	var target *SymbolInfo
	for i := range symbols {
		if symbols[i].Name == e.Symbol {
			target = &symbols[i]
			break
		}
	}

	if target == nil {
		return lines, nil, false, skillerr.Arg(
			fmt.Sprintf("symbol not found: %s", e.Symbol),
			skillerr.WithHint("Check the symbol name (case-sensitive) or run code/symbols to list available symbols."),
		)
	}

	// Replace the symbol's lines with new code
	newCodeLines := strings.Split(e.NewCode, "\n")

	result := make([]string, 0, len(lines)-target.EndLine+target.StartLine+len(newCodeLines))
	result = append(result, lines[:target.StartLine]...)
	result = append(result, newCodeLines...)
	result = append(result, lines[target.EndLine+1:]...)

	return result, []string{e.Symbol}, true, nil
}

// ApplyLinesEdit replaces specific lines.
func ApplyLinesEdit(lines []string, e Edit) ([]string, []string, bool, error) {
	if e.StartLine < 1 || e.EndLine < e.StartLine {
		return nil, nil, false, skillerr.Arg(
			fmt.Sprintf("invalid line range: %d-%d", e.StartLine, e.EndLine),
			skillerr.WithHint("Provide start_line >= 1 and end_line >= start_line."),
		)
	}
	if e.NewCode == "" {
		return nil, nil, false, skillerr.Arg(
			"new_code required for type='lines'",
			skillerr.WithHint("Provide edits[i].new_code with the replacement lines."),
		)
	}

	// Convert to 0-indexed
	start := e.StartLine - 1
	end := e.EndLine - 1

	if start >= len(lines) {
		return nil, nil, false, skillerr.Arg(
			fmt.Sprintf("start_line %d exceeds file length %d", e.StartLine, len(lines)),
			skillerr.WithHint("Choose a start_line within the file length."),
		)
	}
	if end >= len(lines) {
		end = len(lines) - 1
	}

	newCodeLines := strings.Split(e.NewCode, "\n")

	result := make([]string, 0, len(lines)-end+start+len(newCodeLines))
	result = append(result, lines[:start]...)
	result = append(result, newCodeLines...)
	result = append(result, lines[end+1:]...)

	found := []string{fmt.Sprintf("lines:%d-%d", e.StartLine, e.EndLine)}
	return result, found, true, nil
}

// ApplyReplaceEdit does search/replace, optionally scoped to a symbol.
func ApplyReplaceEdit(lines []string, lang Language, e Edit) ([]string, []string, bool, error) {
	if e.Search == "" {
		return nil, nil, false, skillerr.Arg(
			"search required for type='replace'",
			skillerr.WithHint("Provide edits[i].search with the text to replace."),
		)
	}

	startLine := 0
	endLine := len(lines) - 1
	scopeName := "file"

	// If scoped to a symbol, find its boundaries
	if e.WithinSymbol != "" {
		symbols := FindSymbols(lines, lang)
		var target *SymbolInfo
		for i := range symbols {
			if symbols[i].Name == e.WithinSymbol {
				target = &symbols[i]
				break
			}
		}
		if target == nil {
			return lines, nil, false, skillerr.Arg(
				fmt.Sprintf("symbol not found: %s", e.WithinSymbol),
				skillerr.WithHint("Check the symbol name (case-sensitive) or run code/symbols to list available symbols."),
			)
		}
		startLine = target.StartLine
		endLine = target.EndLine
		scopeName = e.WithinSymbol
	}

	// Apply search/replace within scope
	result := make([]string, len(lines))
	copy(result, lines)

	replaced := false
	for i := startLine; i <= endLine; i++ {
		if strings.Contains(result[i], e.Search) {
			if e.All {
				result[i] = strings.ReplaceAll(result[i], e.Search, e.Replace)
			} else {
				result[i] = strings.Replace(result[i], e.Search, e.Replace, 1)
			}
			replaced = true
			if !e.All {
				break // Only first occurrence
			}
		}
	}

	if !replaced {
		return lines, nil, false, nil // No matches, not an error
	}

	return result, []string{scopeName}, true, nil
}

// DetectLanguage returns the code language based on file extension.
func DetectLanguage(path string) Language {
	switch platformfs.DetectLanguage(path) {
	case string(LangGo):
		return LangGo
	case string(LangPython):
		return LangPython
	case string(LangJS):
		return LangJS
	case string(LangTS):
		return LangTS
	case string(LangGDScript):
		return LangGDScript
	default:
		return LangGeneric
	}
}

// FindSymbols finds all top-level symbols in the file.
func FindSymbols(lines []string, lang Language) []SymbolInfo {
	switch lang {
	case LangGo:
		return FindGoSymbols(lines)
	case LangPython:
		return FindPythonSymbols(lines)
	case LangJS, LangTS:
		return FindJSSymbols(lines)
	case LangGDScript:
		return FindGDScriptSymbols(lines)
	default:
		return nil
	}
}

// Go patterns
var (
	goFuncPattern   = regexp.MustCompile(`^func\s+(\w+)`)
	goMethodPattern = regexp.MustCompile(`^func\s+\([^)]+\)\s+(\w+)`)
	goTypePattern   = regexp.MustCompile(`^type\s+(\w+)`)
)

// FindGoSymbols identifies top-level Go symbols.
func FindGoSymbols(lines []string) []SymbolInfo {
	var symbols []SymbolInfo

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		var name, kind string

		// Check for method first (more specific pattern)
		if match := goMethodPattern.FindStringSubmatch(trimmed); match != nil {
			name = match[1]
			kind = "method"
		} else if match := goFuncPattern.FindStringSubmatch(trimmed); match != nil {
			name = match[1]
			kind = "function"
		} else if match := goTypePattern.FindStringSubmatch(trimmed); match != nil {
			name = match[1]
			kind = "type"
		}

		if name != "" {
			endLine := findBraceEnd(lines, i, expander.GoBraceStyle())
			symbols = append(symbols, SymbolInfo{
				Name:      name,
				Kind:      kind,
				StartLine: i,
				EndLine:   endLine,
			})
		}
	}

	return symbols
}

// Python patterns
var (
	pyFuncPattern  = regexp.MustCompile(`^def\s+(\w+)`)
	pyClassPattern = regexp.MustCompile(`^class\s+(\w+)`)
	pyAsyncPattern = regexp.MustCompile(`^async\s+def\s+(\w+)`)
)

// FindPythonSymbols identifies top-level Python symbols.
func FindPythonSymbols(lines []string) []SymbolInfo {
	var symbols []SymbolInfo

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := GetIndentLevel(line)

		// Only top-level symbols (no indentation)
		if indent > 0 {
			continue
		}

		var name, kind string

		if match := pyAsyncPattern.FindStringSubmatch(trimmed); match != nil {
			name = match[1]
			kind = "function"
		} else if match := pyFuncPattern.FindStringSubmatch(trimmed); match != nil {
			name = match[1]
			kind = "function"
		} else if match := pyClassPattern.FindStringSubmatch(trimmed); match != nil {
			name = match[1]
			kind = "class"
		}

		if name != "" {
			endLine := FindIndentEnd(lines, i, 0)
			symbols = append(symbols, SymbolInfo{
				Name:      name,
				Kind:      kind,
				StartLine: i,
				EndLine:   endLine,
			})
		}
	}

	return symbols
}

// JS/TS patterns
var (
	jsFuncPattern      = regexp.MustCompile(`^(?:export\s+)?(?:async\s+)?function\s+(\w+)`)
	jsArrowPattern     = regexp.MustCompile(`^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?\(`)
	jsClassPattern     = regexp.MustCompile(`^(?:export\s+)?class\s+(\w+)`)
	jsInterfacePattern = regexp.MustCompile(`^(?:export\s+)?interface\s+(\w+)`)
	jsTypePattern      = regexp.MustCompile(`^(?:export\s+)?type\s+(\w+)`)
)

// FindJSSymbols identifies top-level JS/TS symbols.
func FindJSSymbols(lines []string) []SymbolInfo {
	var symbols []SymbolInfo

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		var name, kind string

		if match := jsFuncPattern.FindStringSubmatch(trimmed); match != nil {
			name = match[1]
			kind = "function"
		} else if match := jsArrowPattern.FindStringSubmatch(trimmed); match != nil {
			name = match[1]
			kind = "function"
		} else if match := jsClassPattern.FindStringSubmatch(trimmed); match != nil {
			name = match[1]
			kind = "class"
		} else if match := jsInterfacePattern.FindStringSubmatch(trimmed); match != nil {
			name = match[1]
			kind = "interface"
		} else if match := jsTypePattern.FindStringSubmatch(trimmed); match != nil {
			name = match[1]
			kind = "type"
		}

		if name != "" {
			endLine := findBraceEnd(lines, i, expander.JSBraceStyle())
			symbols = append(symbols, SymbolInfo{
				Name:      name,
				Kind:      kind,
				StartLine: i,
				EndLine:   endLine,
			})
		}
	}

	return symbols
}

// GDScript patterns
var (
	gdFuncPattern  = regexp.MustCompile(`^func\s+(\w+)`)
	gdClassPattern = regexp.MustCompile(`^class_name\s+(\w+)`)
	gdInnerClass   = regexp.MustCompile(`^class\s+(\w+)`)
)

// FindGDScriptSymbols identifies top-level GDScript symbols.
func FindGDScriptSymbols(lines []string) []SymbolInfo {
	var symbols []SymbolInfo

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := GetIndentLevel(line)

		// Only top-level symbols
		if indent > 0 {
			continue
		}

		var name, kind string

		if match := gdFuncPattern.FindStringSubmatch(trimmed); match != nil {
			name = match[1]
			kind = "function"
		} else if match := gdClassPattern.FindStringSubmatch(trimmed); match != nil {
			name = match[1]
			kind = "class"
		} else if match := gdInnerClass.FindStringSubmatch(trimmed); match != nil {
			name = match[1]
			kind = "class"
		}

		if name != "" {
			endLine := FindIndentEnd(lines, i, 0)
			symbols = append(symbols, SymbolInfo{
				Name:      name,
				Kind:      kind,
				StartLine: i,
				EndLine:   endLine,
			})
		}
	}

	return symbols
}

func findBraceEnd(lines []string, startLine int, style expander.BraceStyle) int {
	endLine := expander.FindBraceEnd(lines, startLine, style)
	if endLine >= 0 {
		return endLine
	}

	maxLines := 1000
	for i := startLine; i < len(lines) && (i-startLine) < maxLines; i++ {
		endLine = i
	}
	return endLine
}

// FindBraceEnd finds the closing brace for a block starting at startLine.
// Prefer findBraceEnd with explicit styles for language-specific handling.
func FindBraceEnd(lines []string, startLine int) int {
	return findBraceEnd(lines, startLine, expander.DefaultBraceStyle())
}

// FindIndentEnd finds the end of an indentation-based block.
func FindIndentEnd(lines []string, startLine int, baseIndent int) int {
	endLine := startLine
	maxLines := 1000

	for i := startLine + 1; i < len(lines) && (i-startLine) < maxLines; i++ {
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

		indent := GetIndentLevel(line)

		// If we're back to the same or lower indentation, block ended
		if indent <= baseIndent {
			break
		}

		endLine = i
	}

	return endLine
}

// GetIndentLevel returns the number of leading whitespace characters.
func GetIndentLevel(line string) int {
	count := 0
	for _, ch := range line {
		switch ch {
		case ' ':
			count++
		case '\t':
			count += 4
		default:
			return count
		}
	}
	return count
}
