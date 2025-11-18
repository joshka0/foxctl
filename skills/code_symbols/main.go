// Package main implements the code/symbols skill.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib"
	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

type input struct {
	Path           string `json:"path"`
	SymbolType     string `json:"symbol_type"`
	Language       string `json:"language"`
	IncludePrivate bool   `json:"include_private"`
	IncludeDocs    bool   `json:"include_docs"`
	MaxResults     int    `json:"max_results"`
}

type symbol struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Signature  string   `json:"signature"`
	Exported   bool     `json:"exported"`
	Doc        string   `json:"doc,omitempty"`
	Receiver   string   `json:"receiver,omitempty"`
	Fields     []string `json:"fields,omitempty"`
	Methods    []string `json:"methods,omitempty"`
	Parameters []string `json:"parameters,omitempty"`
	Returns    []string `json:"returns,omitempty"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("code/symbols", "ECONFIG", err)
	}

	rc, err := runner.NewContext(cfg, os.Stdout)
	if err != nil {
		fail("code/symbols", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("code/symbols", "EARG", err)
	}
	if err := run(ctx, rc, in); err != nil {
		fail("code/symbols", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.Context, in input) error {
	// Resolve workspace and search path
	workspace := rc.PathValidator.Workspace()
	searchPath := workspace
	if in.Path != "" {
		validated, err := rc.PathValidator.ValidatePath(in.Path)
		if err != nil {
			return fmt.Errorf("path validation failed: %w", err)
		}
		searchPath = validated
	}

	// Check if path is a file or directory
	info, err := os.Stat(searchPath)
	if err != nil {
		return fmt.Errorf("stat path: %w", err)
	}

	var symbols []symbol
	if info.IsDir() {
		symbols, err = extractFromDirectory(searchPath, workspace, in)
	} else {
		symbols, err = extractFromFile(searchPath, workspace, in)
	}
	if err != nil {
		return err
	}

	// Filter by symbol type
	if in.SymbolType != "all" {
		filtered := make([]symbol, 0)
		for _, s := range symbols {
			if s.Type == in.SymbolType {
				filtered = append(filtered, s)
			}
		}
		symbols = filtered
	}

	// Filter by exported status
	if !in.IncludePrivate {
		filtered := make([]symbol, 0)
		for _, s := range symbols {
			if s.Exported {
				filtered = append(filtered, s)
			}
		}
		symbols = filtered
	}

	// Limit results
	if len(symbols) > in.MaxResults {
		symbols = symbols[:in.MaxResults]
	}

	// Prepare preview and artifact
	preview, truncated := preparePreview(symbols, rc.MaxPreview)
	artifact, err := persistSymbolsArtifact(ctx, rc, symbols, truncated)
	if err != nil {
		return err
	}

	// Count by type
	typeCounts := make(map[string]int)
	for _, s := range symbols {
		typeCounts[s.Type]++
	}

	// Build response
	data := map[string]any{
		"symbol_count": len(symbols),
		"preview":      preview,
		"type_counts":  typeCounts,
		"symbol_type":  in.SymbolType,
		"language":     in.Language,
	}
	if artifact.Digest != "" {
		data["artifact"] = artifact.Digest
		data["artifact_kind"] = artifact.Kind
		data["artifact_size_bytes"] = artifact.Size
	}

	return rc.Emit("code/symbols", data, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func parseInput(r io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}
	if in.SymbolType == "" {
		in.SymbolType = "all"
	}
	if in.Language == "" {
		in.Language = "auto"
	}
	if in.MaxResults <= 0 {
		in.MaxResults = 500
	}
	return in, nil
}

func extractFromDirectory(dir, workspace string, in input) ([]symbol, error) {
	var symbols []symbol

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		// Skip hidden and common excludes
		if strings.HasPrefix(d.Name(), ".") || isCommonExclude(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Check language
		lang := detectLanguage(in.Language, filepath.Ext(path))
		if lang == "" {
			return nil
		}

		// Extract symbols from file
		fileSymbols, err := extractFromFile(path, workspace, in)
		if err != nil {
			return nil // Skip files with errors
		}

		symbols = append(symbols, fileSymbols...)
		return nil
	})

	return symbols, err
}

func extractFromFile(path, workspace string, in input) ([]symbol, error) {
	// Detect language
	lang := detectLanguage(in.Language, filepath.Ext(path))
	if lang == "" {
		return nil, fmt.Errorf("unsupported file type")
	}

	switch lang {
	case "go":
		return extractGoSymbols(path, workspace, in)
	case "python":
		return extractPythonSymbols(path, workspace, in)
	case "javascript", "typescript":
		return extractJSSymbols(path, workspace, in)
	default:
		return nil, fmt.Errorf("language not supported: %s", lang)
	}
}

func extractGoSymbols(path, workspace string, in input) ([]symbol, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse go file: %w", err)
	}

	var symbols []symbol
	relPath := relativeTo(workspace, path)

	// Extract declarations
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			sym := extractGoFunction(d, fset, relPath, in)
			if sym != nil {
				symbols = append(symbols, *sym)
			}
		case *ast.GenDecl:
			symbols = append(symbols, extractGoGenDecl(d, fset, relPath, in)...)
		}
	}

	return symbols, nil
}

func extractGoFunction(decl *ast.FuncDecl, fset *token.FileSet, file string, in input) *symbol {
	if decl.Name == nil {
		return nil
	}

	sym := &symbol{
		Name:     decl.Name.Name,
		Type:     "function",
		File:     file,
		Line:     fset.Position(decl.Pos()).Line,
		Exported: ast.IsExported(decl.Name.Name),
	}

	// Check if it's a method
	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		sym.Type = "method"
		if recv := decl.Recv.List[0].Type; recv != nil {
			sym.Receiver = exprToString(recv)
		}
	}

	// Extract parameters
	if decl.Type.Params != nil {
		for _, param := range decl.Type.Params.List {
			paramType := exprToString(param.Type)
			for _, name := range param.Names {
				sym.Parameters = append(sym.Parameters, name.Name+":"+paramType)
			}
			if len(param.Names) == 0 {
				sym.Parameters = append(sym.Parameters, paramType)
			}
		}
	}

	// Extract return types
	if decl.Type.Results != nil {
		for _, result := range decl.Type.Results.List {
			returnType := exprToString(result.Type)
			if len(result.Names) > 0 {
				for _, name := range result.Names {
					sym.Returns = append(sym.Returns, name.Name+":"+returnType)
				}
			} else {
				sym.Returns = append(sym.Returns, returnType)
			}
		}
	}

	// Build signature
	sig := sym.Name + "("
	sig += strings.Join(sym.Parameters, ", ")
	sig += ")"
	if len(sym.Returns) > 0 {
		if len(sym.Returns) == 1 && !strings.Contains(sym.Returns[0], ":") {
			sig += " " + sym.Returns[0]
		} else {
			sig += " (" + strings.Join(sym.Returns, ", ") + ")"
		}
	}
	sym.Signature = sig

	// Extract documentation
	if in.IncludeDocs && decl.Doc != nil {
		sym.Doc = strings.TrimSpace(decl.Doc.Text())
	}

	return sym
}

func extractGoGenDecl(decl *ast.GenDecl, fset *token.FileSet, file string, in input) []symbol {
	var symbols []symbol

	for _, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			sym := &symbol{
				Name:     s.Name.Name,
				File:     file,
				Line:     fset.Position(s.Pos()).Line,
				Exported: ast.IsExported(s.Name.Name),
			}

			switch t := s.Type.(type) {
			case *ast.StructType:
				sym.Type = "struct"
				sym.Signature = "type " + sym.Name + " struct"
				if t.Fields != nil {
					for _, field := range t.Fields.List {
						fieldType := exprToString(field.Type)
						for _, name := range field.Names {
							sym.Fields = append(sym.Fields, name.Name+":"+fieldType)
						}
						if len(field.Names) == 0 {
							sym.Fields = append(sym.Fields, fieldType) // embedded
						}
					}
				}
			case *ast.InterfaceType:
				sym.Type = "interface"
				sym.Signature = "type " + sym.Name + " interface"
				if t.Methods != nil {
					for _, method := range t.Methods.List {
						if len(method.Names) > 0 {
							methodName := method.Names[0].Name
							sym.Methods = append(sym.Methods, methodName)
						}
					}
				}
			default:
				sym.Type = "type"
				sym.Signature = "type " + sym.Name + " " + exprToString(s.Type)
			}

			if in.IncludeDocs && decl.Doc != nil {
				sym.Doc = strings.TrimSpace(decl.Doc.Text())
			}

			symbols = append(symbols, *sym)

		case *ast.ValueSpec:
			for _, name := range s.Names {
				sym := &symbol{
					Name:     name.Name,
					Type:     decl.Tok.String(), // "const" or "var"
					File:     file,
					Line:     fset.Position(s.Pos()).Line,
					Exported: ast.IsExported(name.Name),
				}

				if s.Type != nil {
					sym.Signature = decl.Tok.String() + " " + sym.Name + " " + exprToString(s.Type)
				} else {
					sym.Signature = decl.Tok.String() + " " + sym.Name
				}

				if in.IncludeDocs && decl.Doc != nil {
					sym.Doc = strings.TrimSpace(decl.Doc.Text())
				}

				symbols = append(symbols, *sym)
			}
		}
	}

	return symbols
}

func exprToString(expr ast.Expr) string {
	if expr == nil {
		return ""
	}

	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + exprToString(e.X)
	case *ast.ArrayType:
		return "[]" + exprToString(e.Elt)
	case *ast.MapType:
		return "map[" + exprToString(e.Key) + "]" + exprToString(e.Value)
	case *ast.SelectorExpr:
		return exprToString(e.X) + "." + e.Sel.Name
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.StructType:
		return "struct{}"
	case *ast.FuncType:
		return "func"
	case *ast.ChanType:
		switch e.Dir {
		case ast.SEND:
			return "chan<- " + exprToString(e.Value)
		case ast.RECV:
			return "<-chan " + exprToString(e.Value)
		default:
			return "chan " + exprToString(e.Value)
		}
	case *ast.Ellipsis:
		return "..." + exprToString(e.Elt)
	default:
		return fmt.Sprintf("%T", expr)
	}
}

func extractPythonSymbols(path, workspace string, _ input) ([]symbol, error) {
	// Simple regex-based extraction for Python
	// A full AST parser would be better but this provides basic functionality
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var symbols []symbol
	relPath := relativeTo(workspace, path)
	lines := strings.Split(string(content), "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Function definition
		if strings.HasPrefix(trimmed, "def ") {
			parts := strings.SplitN(trimmed, "(", 2)
			if len(parts) == 2 {
				name := strings.TrimSpace(strings.TrimPrefix(parts[0], "def"))
				sym := symbol{
					Name:      name,
					Type:      "function",
					File:      relPath,
					Line:      i + 1,
					Exported:  !strings.HasPrefix(name, "_"),
					Signature: trimmed,
				}
				symbols = append(symbols, sym)
			}
		}

		// Class definition
		if strings.HasPrefix(trimmed, "class ") {
			parts := strings.SplitN(trimmed, "(", 2)
			name := strings.TrimSpace(strings.TrimPrefix(parts[0], "class"))
			if strings.Contains(name, ":") {
				name = strings.TrimSuffix(name, ":")
			}
			sym := symbol{
				Name:      name,
				Type:      "type",
				File:      relPath,
				Line:      i + 1,
				Exported:  !strings.HasPrefix(name, "_"),
				Signature: trimmed,
			}
			symbols = append(symbols, sym)
		}
	}

	return symbols, nil
}

func extractJSSymbols(path, workspace string, _ input) ([]symbol, error) {
	// Simple regex-based extraction for JavaScript/TypeScript
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var symbols []symbol
	relPath := relativeTo(workspace, path)
	lines := strings.Split(string(content), "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Function declarations
		if strings.HasPrefix(trimmed, "function ") {
			parts := strings.SplitN(trimmed, "(", 2)
			if len(parts) == 2 {
				name := strings.TrimSpace(strings.TrimPrefix(parts[0], "function"))
				sym := symbol{
					Name:      name,
					Type:      "function",
					File:      relPath,
					Line:      i + 1,
					Exported:  !strings.HasPrefix(name, "_"),
					Signature: trimmed,
				}
				symbols = append(symbols, sym)
			}
		}

		// Class declarations
		if strings.HasPrefix(trimmed, "class ") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				name := parts[1]
				name = strings.TrimSuffix(name, "{")
				sym := symbol{
					Name:      name,
					Type:      "type",
					File:      relPath,
					Line:      i + 1,
					Exported:  !strings.HasPrefix(name, "_"),
					Signature: trimmed,
				}
				symbols = append(symbols, sym)
			}
		}

		// Interface declarations (TypeScript)
		if strings.HasPrefix(trimmed, "interface ") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				name := parts[1]
				name = strings.TrimSuffix(name, "{")
				sym := symbol{
					Name:      name,
					Type:      "interface",
					File:      relPath,
					Line:      i + 1,
					Exported:  !strings.HasPrefix(name, "_"),
					Signature: trimmed,
				}
				symbols = append(symbols, sym)
			}
		}
	}

	return symbols, nil
}

func detectLanguage(requested, ext string) string {
	if requested != "auto" {
		return requested
	}

	langMap := map[string]string{
		".go":  "go",
		".py":  "python",
		".js":  "javascript",
		".ts":  "typescript",
		".jsx": "javascript",
		".tsx": "typescript",
	}

	return langMap[ext]
}

func isCommonExclude(name string) bool {
	excludes := []string{
		".git", ".svn", ".hg",
		"node_modules", "vendor", "__pycache__",
		".venv", "venv", ".tox",
		"dist", "build", "target",
	}
	for _, exclude := range excludes {
		if name == exclude {
			return true
		}
	}
	return false
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

func preparePreview(symbols []symbol, limit int) ([]symbol, bool) {
	preview, truncated := skillslib.PreparePreview(symbols, limit)
	if truncated {
		dup := make([]symbol, len(preview))
		copy(dup, preview)
		preview = dup
	}
	return preview, truncated
}

func persistSymbolsArtifact(ctx context.Context, rc *runner.Context, symbols []symbol, truncated bool) (runner.Artifact, error) {
	if !truncated {
		return runner.Artifact{}, nil
	}
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	for _, s := range symbols {
		if err := enc.Encode(s); err != nil {
			return runner.Artifact{}, fmt.Errorf("encode symbol: %w", err)
		}
	}
	return runner.PersistBuffer(ctx, rc, buf, "application/x-ndjson", "code_symbols")
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit code/symbols failure")
	os.Exit(1)
}
