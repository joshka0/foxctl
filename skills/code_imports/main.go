// Package main implements the code/imports skill.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib"
	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
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
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("code/imports", "ECONFIG", err)
	}

	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("code/imports", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("code/imports", "EARG", err)
	}
	if err := run(ctx, rc, in); err != nil {
		fail("code/imports", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, in input) error {
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

	// Collect imports
	var allImports []importInfo
	fileImports := make(map[string][]string)

	if info.IsDir() {
		allImports, fileImports, err = extractFromDirectory(searchPath, workspace, in)
	} else {
		var imports []string
		allImports, imports, err = extractFromFile(searchPath, workspace, in)
		if err == nil {
			fileImports[relativeTo(workspace, searchPath)] = imports
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
			return fmt.Errorf("deps/dependents query requires a specific file path")
		}
		relPath := relativeTo(workspace, searchPath)
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

	// Limit results if it's a slice
	results = limitResults(results, in.MaxResults)

	// Prepare preview and artifact
	preview, truncated := preparePreview(results, rc.MaxPreview)
	artifact, err := persistImportsArtifact(ctx, rc, results, truncated)
	if err != nil {
		return err
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
	if artifact.Digest != "" {
		data["artifact"] = artifact.Digest
		data["artifact_kind"] = artifact.Kind
		data["artifact_size_bytes"] = artifact.Size
	}

	return rc.Emit("code/imports", data, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func parseInput(r io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}
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
	return in, nil
}

func extractFromDirectory(dir, workspace string, in input) ([]importInfo, map[string][]string, error) {
	var imports []importInfo
	fileImports := make(map[string][]string)

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

		// Extract imports from file
		fileImportList, importPaths, err := extractFromFile(path, workspace, in)
		if err != nil {
			return nil // Skip files with errors
		}

		relPath := relativeTo(workspace, path)
		fileImports[relPath] = importPaths
		imports = append(imports, fileImportList...)
		return nil
	})

	return imports, fileImports, err
}

func extractFromFile(path, workspace string, in input) ([]importInfo, []string, error) {
	// Detect language
	lang := detectLanguage(in.Language, filepath.Ext(path))
	if lang == "" {
		return nil, nil, fmt.Errorf("unsupported file type")
	}

	switch lang {
	case "go":
		return extractGoImports(path, workspace, in)
	case "python":
		return extractPythonImports(path, workspace, in)
	case "javascript", "typescript":
		return extractJSImports(path, workspace, in)
	default:
		return nil, nil, fmt.Errorf("language not supported: %s", lang)
	}
}

func extractGoImports(path, workspace string, in input) ([]importInfo, []string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, nil, fmt.Errorf("parse go file: %w", err)
	}

	var imports []importInfo
	var importPaths []string
	relPath := relativeTo(workspace, path)

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
			IsExternal: !isStdLib && !strings.Contains(importPath, workspace),
		}

		if imp.Name != nil {
			info.Alias = imp.Name.Name
		}

		imports = append(imports, info)
		importPaths = append(importPaths, importPath)
	}

	return imports, importPaths, nil
}

func extractPythonImports(path, workspace string, in input) ([]importInfo, []string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	var imports []importInfo
	var importPaths []string
	relPath := relativeTo(workspace, path)
	lines := strings.Split(string(content), "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// import module
		if strings.HasPrefix(trimmed, "import ") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				importPath := parts[1]
				importPath = strings.Split(importPath, ",")[0] // Handle multi-import

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

func extractJSImports(path, workspace string, in input) ([]importInfo, []string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	var imports []importInfo
	var importPaths []string
	relPath := relativeTo(workspace, path)
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

func buildGraph(fileImports map[string][]string, workspace string) []graphNode {
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
	uniqueImports := make(map[string]bool)

	for _, imp := range imports {
		if imp.IsStdLib {
			stdLib++
		}
		if imp.IsExternal {
			external++
		}
		uniqueImports[imp.Import] = true
	}

	return map[string]any{
		"total_imports":  total,
		"unique_imports": len(uniqueImports),
		"std_lib":        stdLib,
		"external":       external,
		"local":          total - stdLib - external,
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

func limitResults(results any, max int) any {
	switch r := results.(type) {
	case []importInfo:
		if len(r) > max {
			return r[:max]
		}
	case []graphNode:
		if len(r) > max {
			return r[:max]
		}
	case []string:
		if len(r) > max {
			return r[:max]
		}
	}
	return results
}

func preparePreview(results any, max int) (any, bool) {
	switch r := results.(type) {
	case []importInfo:
		preview, truncated := skillslib.PreparePreview(r, max)
		if truncated {
			dup := make([]importInfo, len(preview))
			copy(dup, preview)
			return dup, true
		}
		return preview, false
	case []graphNode:
		preview, truncated := skillslib.PreparePreview(r, max)
		if truncated {
			dup := make([]graphNode, len(preview))
			copy(dup, preview)
			return dup, true
		}
		return preview, false
	case []string:
		preview, truncated := skillslib.PreparePreview(r, max)
		return preview, truncated
	default:
		return results, false
	}
}

func persistImportsArtifact(ctx context.Context, rc *runner.RunnerContext, results any, truncated bool) (runner.Artifact, error) {
	if !truncated {
		return runner.Artifact{}, nil
	}
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(results); err != nil {
		return runner.Artifact{}, fmt.Errorf("encode results: %w", err)
	}
	return runner.PersistBuffer(ctx, rc, buf, "application/json", "code_imports")
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit code/imports failure")
	os.Exit(1)
}
