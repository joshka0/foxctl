// Package main implements the code/smart_write skill.
// It provides symbol-aware file editing with dry-run diff preview.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pmezard/go-difflib/difflib"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

type input struct {
	Path         string `json:"path"`
	Edits        []edit `json:"edits"`
	DryRun       bool   `json:"dry_run"`
	ContextLines int    `json:"context_lines"`
}

type edit struct {
	Type         string `json:"type"` // "symbol", "lines", "replace"
	Symbol       string `json:"symbol,omitempty"`
	StartLine    int    `json:"start_line,omitempty"`
	EndLine      int    `json:"end_line,omitempty"`
	Search       string `json:"search,omitempty"`
	Replace      string `json:"replace,omitempty"`
	WithinSymbol string `json:"within_symbol,omitempty"`
	NewCode      string `json:"new_code,omitempty"`
	All          bool   `json:"all,omitempty"`
}

// symbolInfo holds info about a found symbol.
type symbolInfo struct {
	Name      string
	Kind      string
	StartLine int // 0-indexed
	EndLine   int // 0-indexed
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("code/smart_write", "ERUNTIME", err)
	}

	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("code/smart_write", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("code/smart_write", "EARG", err)
	}
	if err := run(ctx, rc, in); err != nil {
		fail("code/smart_write", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, in input) error {
	// Validate and resolve path
	absPath, err := rc.PathValidator.ValidatePath(in.Path)
	if err != nil {
		return fmt.Errorf("path validation failed: %w", err)
	}

	// Read original file
	originalBytes, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	original := string(originalBytes)
	lines := strings.Split(original, "\n")

	// Detect language for symbol finding
	lang := detectLanguage(absPath)

	// Apply edits
	result := lines
	symbolsFound := []string{}
	editCount := 0

	for _, e := range in.Edits {
		var edited []string
		var found []string
		var applied bool

		switch e.Type {
		case "symbol":
			edited, found, applied, err = applySymbolEdit(result, lang, e)
		case "lines":
			edited, found, applied, err = applyLinesEdit(result, e)
		case "replace":
			edited, found, applied, err = applyReplaceEdit(result, lang, e)
		default:
			return fmt.Errorf("unknown edit type: %s", e.Type)
		}

		if err != nil {
			return fmt.Errorf("edit failed: %w", err)
		}

		if applied {
			result = edited
			editCount++
			symbolsFound = append(symbolsFound, found...)
		}
	}

	modified := strings.Join(result, "\n")

	// Generate unified diff
	diff, err := generateUnifiedDiff(absPath, original, modified, in.ContextLines)
	if err != nil {
		return fmt.Errorf("generate diff: %w", err)
	}

	// Write file unless dry_run
	edited := false
	if !in.DryRun && original != modified {
		if err := os.WriteFile(absPath, []byte(modified), 0o644); err != nil {
			return fmt.Errorf("write file: %w", err)
		}
		edited = true
	}

	// Prepare response
	relPath := relativeTo(rc.PathValidator.Workspace(), absPath)

	data := map[string]any{
		"path":          relPath,
		"edited":        edited,
		"edit_count":    editCount,
		"symbols_found": symbolsFound,
		"dry_run":       in.DryRun,
	}

	// Always include diff for dry_run, optionally for actual edits
	if in.DryRun || diff != "" {
		data["diff"] = diff
	}

	if diff == "" && editCount == 0 {
		data["message"] = "no changes made"
	}

	return rc.Emit("code/smart_write", data, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func parseInput(r io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return input{}, errors.New("path is required")
	}
	if len(in.Edits) == 0 {
		return input{}, errors.New("at least one edit is required")
	}
	if in.ContextLines <= 0 {
		in.ContextLines = 3
	}
	return in, nil
}

// applySymbolEdit replaces an entire symbol by name.
func applySymbolEdit(lines []string, lang Language, e edit) ([]string, []string, bool, error) {
	if e.Symbol == "" {
		return nil, nil, false, errors.New("symbol name required for type='symbol'")
	}
	if e.NewCode == "" {
		return nil, nil, false, errors.New("new_code required for type='symbol'")
	}

	// Find the symbol
	symbols := findSymbols(lines, lang)
	var target *symbolInfo
	for i := range symbols {
		if symbols[i].Name == e.Symbol {
			target = &symbols[i]
			break
		}
	}

	if target == nil {
		return lines, nil, false, fmt.Errorf("symbol not found: %s", e.Symbol)
	}

	// Replace the symbol's lines with new code
	newCodeLines := strings.Split(e.NewCode, "\n")

	result := make([]string, 0, len(lines)-target.EndLine+target.StartLine+len(newCodeLines))
	result = append(result, lines[:target.StartLine]...)
	result = append(result, newCodeLines...)
	result = append(result, lines[target.EndLine+1:]...)

	return result, []string{e.Symbol}, true, nil
}

// applyLinesEdit replaces specific lines.
func applyLinesEdit(lines []string, e edit) ([]string, []string, bool, error) {
	if e.StartLine < 1 || e.EndLine < e.StartLine {
		return nil, nil, false, fmt.Errorf("invalid line range: %d-%d", e.StartLine, e.EndLine)
	}
	if e.NewCode == "" {
		return nil, nil, false, errors.New("new_code required for type='lines'")
	}

	// Convert to 0-indexed
	start := e.StartLine - 1
	end := e.EndLine - 1

	if start >= len(lines) {
		return nil, nil, false, fmt.Errorf("start_line %d exceeds file length %d", e.StartLine, len(lines))
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

// applyReplaceEdit does search/replace, optionally scoped to a symbol.
func applyReplaceEdit(lines []string, lang Language, e edit) ([]string, []string, bool, error) {
	if e.Search == "" {
		return nil, nil, false, errors.New("search required for type='replace'")
	}

	startLine := 0
	endLine := len(lines) - 1
	scopeName := "file"

	// If scoped to a symbol, find its boundaries
	if e.WithinSymbol != "" {
		symbols := findSymbols(lines, lang)
		var target *symbolInfo
		for i := range symbols {
			if symbols[i].Name == e.WithinSymbol {
				target = &symbols[i]
				break
			}
		}
		if target == nil {
			return lines, nil, false, fmt.Errorf("symbol not found: %s", e.WithinSymbol)
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

// generateUnifiedDiff creates a git-style unified diff.
func generateUnifiedDiff(path, original, modified string, contextLines int) (string, error) {
	if original == modified {
		return "", nil
	}

	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(original),
		B:        difflib.SplitLines(modified),
		FromFile: "a/" + filepath.Base(path),
		ToFile:   "b/" + filepath.Base(path),
		Context:  contextLines,
	}

	return difflib.GetUnifiedDiffString(diff)
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

// findSymbols finds all top-level symbols in the file.
func findSymbols(lines []string, lang Language) []symbolInfo {
	switch lang {
	case LangGo:
		return findGoSymbols(lines)
	case LangPython:
		return findPythonSymbols(lines)
	case LangJS, LangTS:
		return findJSSymbols(lines)
	case LangGDScript:
		return findGDScriptSymbols(lines)
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

func findGoSymbols(lines []string) []symbolInfo {
	var symbols []symbolInfo

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
			endLine := findBraceEnd(lines, i)
			symbols = append(symbols, symbolInfo{
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

func findPythonSymbols(lines []string) []symbolInfo {
	var symbols []symbolInfo

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := getIndentLevel(line)

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
			endLine := findIndentEnd(lines, i, 0)
			symbols = append(symbols, symbolInfo{
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

func findJSSymbols(lines []string) []symbolInfo {
	var symbols []symbolInfo

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
			endLine := findBraceEnd(lines, i)
			symbols = append(symbols, symbolInfo{
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

func findGDScriptSymbols(lines []string) []symbolInfo {
	var symbols []symbolInfo

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := getIndentLevel(line)

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
			endLine := findIndentEnd(lines, i, 0)
			symbols = append(symbols, symbolInfo{
				Name:      name,
				Kind:      kind,
				StartLine: i,
				EndLine:   endLine,
			})
		}
	}

	return symbols
}

// findBraceEnd finds the closing brace for a block starting at startLine.
func findBraceEnd(lines []string, startLine int) int {
	depth := 0
	foundOpen := false
	endLine := startLine
	maxLines := 1000 // Limit to prevent infinite loops

	for i := startLine; i < len(lines) && (i-startLine) < maxLines; i++ {
		line := lines[i]

		// Count braces, ignoring strings (simple heuristic)
		inString := false
		var stringChar rune
		for j, ch := range line {
			// Simple string detection
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

			switch ch {
			case '{':
				depth++
				foundOpen = true
			case '}':
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
func findIndentEnd(lines []string, startLine int, baseIndent int) int {
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

		indent := getIndentLevel(line)

		// If we're back to the same or lower indentation, block ended
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

func relativeTo(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return filepath.ToSlash(target)
	}
	if strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(target)
	}
	return filepath.ToSlash(rel)
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit code/smart_write failure")
	os.Exit(1)
}
