// Package main implements the code/imports skill.
package main

import (
	"context"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/fsutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/langutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/pathutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/sliceutil"
)

// input defines the skill input parameters for import analysis across multiple programming languages.
type input struct {
	Path       string `json:"path"`
	QueryType  string `json:"query_type"`
	Language   string `json:"language"`
	IncludeStd bool   `json:"include_std"`
	MaxDepth   int    `json:"max_depth"`
	MaxResults int    `json:"max_results"`
}

// importInfo represents a single import statement with metadata about its type and usage.
type importInfo struct {
	File       string   `json:"file"`
	Import     string   `json:"import"`
	Alias      string   `json:"alias,omitempty"`
	Line       int      `json:"line,omitempty"`
	IsStdLib   bool     `json:"is_std_lib"`
	IsExternal bool     `json:"is_external"`
	ImportedBy []string `json:"imported_by,omitempty"`
	Imports    []string `json:"imports,omitempty"`
	UsageCount int      `json:"usage_count,omitempty"`
}

// graphNode represents a file node in the import dependency graph with bidirectional relationships.
type graphNode struct {
	File       string   `json:"file"`
	Imports    []string `json:"imports"`
	ImportedBy []string `json:"imported_by"`
	IsExternal bool     `json:"is_external"`
}

// main is the skill entry point for code/imports.
func main() {
	skillmain.Main("code/imports", run)
}

// run orchestrates import analysis across multiple languages with graph building and dependency tracking.
//
// Index:
//
//	Purpose: Analyze imports and dependencies in codebases across Go, Python, and JavaScript/TypeScript
//	Flow: apply defaults → resolve path → extract imports → process query type → build response with statistics
//	SideEffects: reads file contents; parses source code; builds dependency graphs; stores artifacts
//	FailureModes: file access errors, parsing failures, unsupported languages, invalid query types
//	Observability: emits import lists, dependency graphs, statistics, and external dependency analysis
//	Related: extractFromDirectory, extractFromFile, buildGraph, getDependencies, getDependents
//	Keywords: code/imports, import_analysis, dependency_graph, code_dependencies, cross_language
//
// [[domain:import-dependency-analysis]]
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Apply defaults
	if in.QueryType == "" {
		in.QueryType = "list"
	}
	if in.Language == "" {
		in.Language = "auto"
	}
	if in.MaxDepth <= 0 {
		in.MaxDepth = 3
	}
	if in.MaxResults <= 0 {
		in.MaxResults = 500
	}

	// Resolve workspace and search path
	workspace, searchPath, err := skillmain.ResolvePath(rc, in.Path)
	if err != nil {
		return err
	}

	// Check if path is a file or directory
	info, err := os.Stat(searchPath)
	if err != nil {
		return skillerr.WrapIO("stat path", err)
	}

	// Collect imports
	var allImports []importInfo
	fileImports := make(map[string][]string)

	if info.IsDir() {
		allImports, fileImports, err = extractFromDirectory(searchPath, workspace, in)
	} else {
		var imports []string
		allImports, imports, err = extractFromFile(searchPath, workspace, in)
		if err == nil {
			fileImports[pathutil.RelTo(workspace, searchPath)] = imports
		}
	}
	if err != nil {
		return err
	}

	// Process based on query type
	var results any
	var resultType string

	switch in.QueryType {
	case "list":
		results = allImports
		resultType = "imports"
	case "graph":
		results = buildGraph(fileImports, workspace)
		resultType = "graph"
	case "external":
		external := filterExternal(allImports)
		results = external
		resultType = "external_imports"
	case "deps", "dependents":
		// For these, we need a specific file
		if info.IsDir() {
			return skillerr.Validation("deps/dependents query requires a specific file path")
		}
		relPath := pathutil.RelTo(workspace, searchPath)
		if in.QueryType == "deps" {
			results = getDependencies(relPath, fileImports, in.MaxDepth)
		} else {
			results = getDependents(relPath, fileImports, in.MaxDepth)
		}
		resultType = in.QueryType
	default:
		results = allImports
		resultType = "imports"
	}

	// Prepare preview and artifact
	var (
		preview  any
		artifact *skillmain.Artifact
	)
	switch typed := results.(type) {
	case []importInfo:
		typed = sliceutil.Limit(typed, in.MaxResults)
		previewResult, err := skillout.PreviewAndPersistNDJSON(ctx, rc, typed, rc.MaxPreview, "code_imports", true)
		if err != nil {
			return err
		}
		preview = previewResult.Preview
		artifact = previewResult.Artifact
	case []graphNode:
		typed = sliceutil.Limit(typed, in.MaxResults)
		previewResult, err := skillout.PreviewAndPersistNDJSON(ctx, rc, typed, rc.MaxPreview, "code_imports", true)
		if err != nil {
			return err
		}
		preview = previewResult.Preview
		artifact = previewResult.Artifact
	case []string:
		typed = sliceutil.Limit(typed, in.MaxResults)
		previewResult, err := skillout.PreviewAndPersistNDJSON(ctx, rc, typed, rc.MaxPreview, "code_imports", true)
		if err != nil {
			return err
		}
		preview = previewResult.Preview
		artifact = previewResult.Artifact
	default:
		preview = results
	}

	// Count statistics
	stats := calculateStats(allImports)

	// Build response
	data := map[string]any{
		"query_type":  in.QueryType,
		"result_type": resultType,
		"preview":     preview,
		"statistics":  stats,
	}
	skillout.AddArtifact(data, artifact)

	return skillout.Emit(rc, "code/imports", data)
}

// extractFromDirectory walks a directory to extract imports from all supported language files.
func extractFromDirectory(dir, workspace string, in input) ([]importInfo, map[string][]string, error) {
	var imports []importInfo
	fileImports := make(map[string][]string)

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		// Skip hidden and common excludes
		if fsutil.ShouldSkipHiddenOrCommon(d.Name()) {
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
		lang := langutil.DetectAllowedWithHint(in.Language, path, langutil.CommonCodeLanguages)
		if lang == "" {
			return nil
		}

		// Extract imports from file
		fileImportList, importPaths, err := extractFromFile(path, workspace, in)
		if err != nil {
			return nil // Skip files with errors
		}

		relPath := pathutil.RelTo(workspace, path)
		fileImports[relPath] = importPaths
		imports = append(imports, fileImportList...)
		return nil
	})

	return imports, fileImports, err
}

// extractFromFile extracts imports from a single file using language-specific parsers.
func extractFromFile(path, workspace string, in input) ([]importInfo, []string, error) {
	// Detect language
	lang := langutil.DetectAllowedWithHint(in.Language, path, langutil.CommonCodeLanguages)
	if lang == "" {
		return nil, nil, skillerr.Validation("unsupported file type")
	}

	switch lang {
	case "go":
		return extractGoImports(path, workspace, in)
	case "python":
		return extractPythonImports(path, workspace, in)
	case "javascript", "typescript":
		return extractJSImports(path, workspace, in)
	default:
		return nil, nil, skillerr.Validationf("language not supported: %s", lang)
	}
}

// extractGoImports extracts imports from Go files using the Go parser with alias detection.
func extractGoImports(path, workspace string, in input) ([]importInfo, []string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, nil, skillerr.WrapParse("parse go file", err)
	}

	var imports []importInfo
	var importPaths []string
	relPath := pathutil.RelTo(workspace, path)

	for _, imp := range file.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)

		// Skip standard library imports if requested
		isStdLib := isGoStdLib(importPath)
		if !in.IncludeStd && isStdLib {
			continue
		}

		info := importInfo{
			File:       relPath,
			Import:     importPath,
			Line:       fset.Position(imp.Pos()).Line,
			IsStdLib:   isStdLib,
			IsExternal: !isStdLib, // Note: includes local module imports as we don't check go.mod
		}

		if imp.Name != nil {
			info.Alias = imp.Name.Name
		}

		imports = append(imports, info)
		importPaths = append(importPaths, importPath)
	}

	return imports, importPaths, nil
}

// extractPythonImports extracts imports from Python files with support for 'import' and 'from...import' statements.
func extractPythonImports(path, workspace string, _ input) ([]importInfo, []string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, skillerr.WrapIO("read file", err)
	}

	var imports []importInfo
	var importPaths []string
	relPath := pathutil.RelTo(workspace, path)
	lines := strings.Split(string(content), "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// import module
		if strings.HasPrefix(trimmed, "import ") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				// Handle comma-separated imports: import os, sys, re
				importsList := strings.Join(parts[1:], " ")
				for _, imp := range strings.Split(importsList, ",") {
					importPath := strings.TrimSpace(imp)
					if importPath == "" {
						continue
					}
					// Remove "as alias" part if present
					if idx := strings.Index(importPath, " as "); idx != -1 {
						importPath = strings.TrimSpace(importPath[:idx])
					}

					info := importInfo{
						File:       relPath,
						Import:     importPath,
						Line:       i + 1,
						IsStdLib:   isPythonStdLib(importPath),
						IsExternal: !isPythonStdLib(importPath),
					}
					imports = append(imports, info)
					importPaths = append(importPaths, importPath)
				}
			}
		}

		// from module import ...
		if strings.HasPrefix(trimmed, "from ") && strings.Contains(trimmed, " import ") {
			parts := strings.Split(trimmed, " import ")
			if len(parts) == 2 {
				importPath := strings.TrimSpace(strings.TrimPrefix(parts[0], "from"))

				info := importInfo{
					File:       relPath,
					Import:     importPath,
					Line:       i + 1,
					IsStdLib:   isPythonStdLib(importPath),
					IsExternal: !isPythonStdLib(importPath),
				}
				imports = append(imports, info)
				importPaths = append(importPaths, importPath)
			}
		}
	}

	return imports, importPaths, nil
}

// extractJSImports extracts imports from JavaScript/TypeScript files with ES6 import and CommonJS require support.
func extractJSImports(path, workspace string, _ input) ([]importInfo, []string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, skillerr.WrapIO("read file", err)
	}

	var imports []importInfo
	var importPaths []string
	relPath := pathutil.RelTo(workspace, path)
	lines := strings.Split(string(content), "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// import ... from '...'
		if strings.HasPrefix(trimmed, "import ") && strings.Contains(trimmed, " from ") {
			parts := strings.Split(trimmed, " from ")
			if len(parts) == 2 {
				importPath := strings.Trim(parts[1], `'";`)

				info := importInfo{
					File:       relPath,
					Import:     importPath,
					Line:       i + 1,
					IsStdLib:   false,
					IsExternal: !strings.HasPrefix(importPath, "."),
				}
				imports = append(imports, info)
				importPaths = append(importPaths, importPath)
			}
		}

		// require('...')
		if strings.Contains(trimmed, "require(") {
			start := strings.Index(trimmed, "require(") + 8
			end := strings.Index(trimmed[start:], ")")
			if end > 0 {
				importPath := strings.Trim(trimmed[start:start+end], `'"`)

				info := importInfo{
					File:       relPath,
					Import:     importPath,
					Line:       i + 1,
					IsStdLib:   false,
					IsExternal: !strings.HasPrefix(importPath, "."),
				}
				imports = append(imports, info)
				importPaths = append(importPaths, importPath)
			}
		}
	}

	return imports, importPaths, nil
}

// buildGraph constructs a dependency graph from file imports with bidirectional relationship tracking.
func buildGraph(fileImports map[string][]string, _ string) []graphNode {
	graph := make(map[string]*graphNode)

	// Initialize nodes
	for file := range fileImports {
		if graph[file] == nil {
			graph[file] = &graphNode{
				File:       file,
				Imports:    []string{},
				ImportedBy: []string{},
				IsExternal: false,
			}
		}
	}

	// Build edges
	for file, imports := range fileImports {
		for _, imp := range imports {
			graph[file].Imports = append(graph[file].Imports, imp)

			// For local imports, track reverse dependencies
			if graph[imp] != nil {
				graph[imp].ImportedBy = append(graph[imp].ImportedBy, file)
			}
		}
	}

	// Convert to slice
	var nodes []graphNode
	for _, node := range graph {
		nodes = append(nodes, *node)
	}

	// Sort by file name
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].File < nodes[j].File
	})

	return nodes
}

// getDependencies recursively finds all dependencies of a file up to the specified depth.
func getDependencies(file string, fileImports map[string][]string, maxDepth int) []string {
	visited := make(map[string]bool)
	var deps []string

	var traverse func(string, int)
	traverse = func(f string, depth int) {
		if depth > maxDepth || visited[f] {
			return
		}
		visited[f] = true

		for _, imp := range fileImports[f] {
			if !visited[imp] {
				deps = append(deps, imp)
			}
			traverse(imp, depth+1)
		}
	}

	traverse(file, 0)
	return deps
}

// getDependents recursively finds all files that depend on the specified file up to the specified depth.
//
// This function traverses the reverse dependency graph starting from the specified file and
// returns a list of all files that depend on the file, up to the specified depth.
//
// Index
//
//   - getDependents
//   - reverseMap
func getDependents(file string, fileImports map[string][]string, maxDepth int) []string {
	// Build reverse map
	reverseMap := make(map[string][]string)
	for f, imports := range fileImports {
		for _, imp := range imports {
			reverseMap[imp] = append(reverseMap[imp], f)
		}
	}

	visited := make(map[string]bool)
	var dependents []string

	var traverse func(string, int)
	traverse = func(f string, depth int) {
		if depth > maxDepth || visited[f] {
			return
		}
		visited[f] = true

		for _, dependent := range reverseMap[f] {
			if !visited[dependent] {
				dependents = append(dependents, dependent)
			}
			traverse(dependent, depth+1)
		}
	}

	traverse(file, 0)
	return dependents
}

// filterExternal filters imports to only include external (non-standard library) dependencies.
func filterExternal(imports []importInfo) []importInfo {
	var external []importInfo
	for _, imp := range imports {
		if imp.IsExternal {
			external = append(external, imp)
		}
	}
	return external
}

// calculateStats computes import statistics including total, unique, standard library, external, and local counts.
func calculateStats(imports []importInfo) map[string]any {
	total := len(imports)
	stdLib := 0
	external := 0
	local := 0
	uniqueImports := make(map[string]bool)

	for _, imp := range imports {
		if imp.IsStdLib {
			stdLib++
		} else if imp.IsExternal {
			// For now, IsExternal is true for anything not StdLib in Go/Python
			// In JS, we check for relative paths.
			// Since we modified Go IsExternal logic, we should treat !StdLib && !External as Local?
			// Or logic: if !IsStdLib, and (imp.IsExternal might be true for local modules in Go).
			// Wait, the logic in extractGoImports sets IsExternal = !isStdLib.
			// So local is effectively 0 for Go unless we have better detection.
			// However, in JS extractJSImports sets IsExternal = !strings.HasPrefix(importPath, ".")
			// So for JS, IsExternal is accurate for external vs local.
			// Let's just rely on the counts we have.
			external++
		} else {
			local++
		}
		uniqueImports[imp.Import] = true
	}

	return map[string]any{
		"total_imports":  total,
		"unique_imports": len(uniqueImports),
		"std_lib":        stdLib,
		"external":       external,
		"local":          local,
	}
}

// isGoStdLib determines if a Go import path belongs to the standard library.
func isGoStdLib(importPath string) bool {
	// Common Go standard library packages
	stdLibPrefixes := []string{
		"fmt", "os", "io", "strings", "bytes", "time", "sync", "context",
		"encoding/", "net/", "crypto/", "path/", "regexp", "sort", "strconv",
		"testing", "errors", "flag", "log", "math", "runtime", "bufio",
	}

	for _, prefix := range stdLibPrefixes {
		if strings.HasPrefix(importPath, prefix) {
			return true
		}
	}

	// Standard library doesn't contain dots (no domain names)
	return !strings.Contains(importPath, ".")
}

// isPythonStdLib determines if a Python import belongs to the standard library.
func isPythonStdLib(importPath string) bool {
	stdLibs := []string{
		"os", "sys", "re", "json", "time", "datetime", "collections",
		"itertools", "functools", "typing", "pathlib", "subprocess",
		"threading", "multiprocessing", "asyncio", "unittest", "logging",
	}

	for _, lib := range stdLibs {
		if importPath == lib || strings.HasPrefix(importPath, lib+".") {
			return true
		}
	}
	return false
}
