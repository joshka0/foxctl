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

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/fsutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/langutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/pathutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/sliceutil"
)

type input struct {
	Path       string `json:"path"`
	QueryType  string `json:"query_type"`
	Language   string `json:"language"`
	IncludeStd bool   `json:"include_std"`
	MaxDepth   int    `json:"max_depth"`
	MaxResults int    `json:"max_results"`
}

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

type graphNode struct {
	File       string   `json:"file"`
	Imports    []string `json:"imports"`
	ImportedBy []string `json:"imported_by"`
	IsExternal bool     `json:"is_external"`
}

func main() {
	skillmain.Main("code/imports", run)
}

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

func filterExternal(imports []importInfo) []importInfo {
	var external []importInfo
	for _, imp := range imports {
		if imp.IsExternal {
			external = append(external, imp)
		}
	}
	return external
}

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
