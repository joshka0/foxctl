// Package main implements the code/complexity skill.
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
	"regexp"
	"sort"
	"strings"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

type input struct {
	Path         string `json:"path"`
	AnalysisMode string `json:"analysis_mode"`
	Metric       string `json:"metric"`
	Threshold    int    `json:"threshold"`
	Language     string `json:"language"`
	IncludeTests bool   `json:"include_tests"`
	MaxResults   int    `json:"max_results"`
}

type complexityResult struct {
	File                 string   `json:"file"`
	Function             string   `json:"function"`
	Line                 int      `json:"line"`
	CyclomaticComplexity int      `json:"cyclomatic_complexity"`
	CognitiveComplexity  int      `json:"cognitive_complexity,omitempty"`
	NestingDepth         int      `json:"nesting_depth"`
	FunctionLength       int      `json:"function_length"`
	ParameterCount       int      `json:"parameter_count,omitempty"`
	RiskLevel            string   `json:"risk_level"`
	Recommendations      []string `json:"recommendations,omitempty"`
}

type aggregateStats struct {
	TotalFunctions    int     `json:"total_functions"`
	AverageComplexity float64 `json:"average_complexity"`
	MaxComplexity     int     `json:"max_complexity"`
	HighRiskCount     int     `json:"high_risk_count"`
	MediumRiskCount   int     `json:"medium_risk_count"`
	LowRiskCount      int     `json:"low_risk_count"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("code/complexity", "ECONFIG", err)
	}

	rc, err := runner.NewContext(cfg, os.Stdout)
	if err != nil {
		fail("code/complexity", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("code/complexity", "EARG", err)
	}
	if err := run(ctx, rc, in); err != nil {
		fail("code/complexity", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.Context, in input) error {
	workspace := rc.PathValidator.Workspace()
	searchPath := workspace
	if in.Path != "" {
		validated, err := rc.PathValidator.ValidatePath(in.Path)
		if err != nil {
			return fmt.Errorf("path validation failed: %w", err)
		}
		searchPath = validated
	}

	info, err := os.Stat(searchPath)
	if err != nil {
		return fmt.Errorf("stat path: %w", err)
	}

	var results []complexityResult
	if info.IsDir() {
		results, err = analyzeDirectory(searchPath, workspace, in)
	} else {
		results, err = analyzeFile(searchPath, workspace, in)
	}
	if err != nil {
		return err
	}

	// Filter by analysis mode
	switch in.AnalysisMode {
	case "hotspots":
		filtered := make([]complexityResult, 0)
		for _, r := range results {
			complexity := r.CyclomaticComplexity
			if in.Metric == "cognitive" && r.CognitiveComplexity > 0 {
				complexity = r.CognitiveComplexity
			}
			if complexity >= in.Threshold {
				filtered = append(filtered, r)
			}
		}
		results = filtered

		// Sort by complexity descending
		sort.Slice(results, func(i, j int) bool {
			ci := results[i].CyclomaticComplexity
			cj := results[j].CyclomaticComplexity
			if in.Metric == "cognitive" {
				ci = results[i].CognitiveComplexity
				cj = results[j].CognitiveComplexity
			}
			return ci > cj
		})
	}

	// Limit results
	if len(results) > in.MaxResults {
		results = results[:in.MaxResults]
	}

	// Calculate aggregate statistics
	stats := calculateStats(results)

	// Prepare preview and artifact
	preview, truncated := preparePreview(results, rc.MaxPreview)
	artifact, err := persistResultsArtifact(ctx, rc, results, truncated)
	if err != nil {
		return err
	}

	data := map[string]any{
		"analysis_mode": in.AnalysisMode,
		"metric":        in.Metric,
		"threshold":     in.Threshold,
		"result_count":  len(results),
		"results":       preview,
		"statistics":    stats,
	}
	if artifact.Digest != "" {
		data["artifact"] = artifact.Digest
		data["artifact_kind"] = artifact.Kind
		data["artifact_size_bytes"] = artifact.Size
	}

	return rc.Emit("code/complexity", data, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func parseInput(r io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}
	if in.AnalysisMode == "" {
		in.AnalysisMode = "hotspots"
	}
	if in.Metric == "" {
		in.Metric = "cyclomatic"
	}
	if in.Threshold <= 0 {
		in.Threshold = 10
	}
	if in.Language == "" {
		in.Language = "auto"
	}
	if in.MaxResults <= 0 {
		in.MaxResults = 100
	}
	return in, nil
}

func analyzeDirectory(dir, workspace string, in input) ([]complexityResult, error) {
	var results []complexityResult

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if strings.HasPrefix(d.Name(), ".") || isCommonExclude(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		if !in.IncludeTests && isTestFile(d.Name()) {
			return nil
		}

		lang := detectLanguage(in.Language, filepath.Ext(path))
		if lang == "" {
			return nil
		}

		fileResults, err := analyzeFile(path, workspace, in)
		if err != nil {
			return nil
		}

		results = append(results, fileResults...)
		return nil
	})

	return results, err
}

func analyzeFile(path, workspace string, in input) ([]complexityResult, error) {
	lang := detectLanguage(in.Language, filepath.Ext(path))
	if lang == "" {
		return nil, fmt.Errorf("unsupported file type")
	}

	switch lang {
	case "go":
		return analyzeGoFile(path, workspace, in)
	case "python":
		return analyzePythonFile(path, workspace, in)
	case "javascript", "typescript":
		return analyzeJSFile(path, workspace, in)
	default:
		return nil, fmt.Errorf("language not supported: %s", lang)
	}
}

func analyzeGoFile(path, workspace string, _ input) ([]complexityResult, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse go file: %w", err)
	}

	var results []complexityResult
	relPath := relativeTo(workspace, path)

	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			if fn.Body == nil {
				continue
			}

			cyclomatic := calculateGoCyclomaticComplexity(fn)
			cognitive := calculateGoCognitiveComplexity(fn)
			nesting := calculateGoNestingDepth(fn)
			length := fset.Position(fn.End()).Line - fset.Position(fn.Pos()).Line + 1

			paramCount := 0
			if fn.Type.Params != nil {
				for _, field := range fn.Type.Params.List {
					paramCount += len(field.Names)
					if len(field.Names) == 0 {
						paramCount++ // unnamed parameter
					}
				}
			}

			result := complexityResult{
				File:                 relPath,
				Function:             fn.Name.Name,
				Line:                 fset.Position(fn.Pos()).Line,
				CyclomaticComplexity: cyclomatic,
				CognitiveComplexity:  cognitive,
				NestingDepth:         nesting,
				FunctionLength:       length,
				ParameterCount:       paramCount,
			}

			classifyRisk(&result)
			results = append(results, result)
		}
	}

	return results, nil
}

func calculateGoCyclomaticComplexity(fn *ast.FuncDecl) int {
	complexity := 1

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.IfStmt:
			complexity++
		case *ast.ForStmt, *ast.RangeStmt:
			complexity++
		case *ast.CaseClause:
			if len(node.List) > 0 {
				complexity++
			}
		case *ast.CommClause:
			if node.Comm != nil {
				complexity++
			}
		case *ast.BinaryExpr:
			if node.Op == token.LAND || node.Op == token.LOR {
				complexity++
			}
		}
		return true
	})

	return complexity
}

func calculateGoCognitiveComplexity(fn *ast.FuncDecl) int {
	complexity := 0

	var walk func(ast.Node, int)
	walk = func(n ast.Node, nesting int) {
		if n == nil {
			return
		}

		switch node := n.(type) {
		case *ast.BlockStmt:
			for _, stmt := range node.List {
				walk(stmt, nesting)
			}
		case *ast.IfStmt:
			complexity += 1 + nesting
			walk(node.Init, nesting)
			walk(node.Cond, nesting)
			walk(node.Body, nesting+1)
			walk(node.Else, nesting)
		case *ast.ForStmt:
			complexity += 1 + nesting
			walk(node.Init, nesting)
			walk(node.Cond, nesting)
			walk(node.Post, nesting)
			walk(node.Body, nesting+1)
		case *ast.RangeStmt:
			complexity += 1 + nesting
			walk(node.Key, nesting)
			walk(node.Value, nesting)
			walk(node.X, nesting)
			walk(node.Body, nesting+1)
		case *ast.SwitchStmt:
			complexity += 1 + nesting
			walk(node.Init, nesting)
			walk(node.Tag, nesting)
			walk(node.Body, nesting+1)
		case *ast.TypeSwitchStmt:
			complexity += 1 + nesting
			walk(node.Init, nesting)
			walk(node.Assign, nesting)
			walk(node.Body, nesting+1)
		case *ast.SelectStmt:
			complexity += 1 + nesting
			walk(node.Body, nesting+1)
		case *ast.CaseClause:
			for _, stmt := range node.Body {
				walk(stmt, nesting)
			}
		case *ast.CommClause:
			for _, stmt := range node.Body {
				walk(stmt, nesting)
			}
		case *ast.BinaryExpr:
			if node.Op == token.LAND || node.Op == token.LOR {
				complexity++
			}
			walk(node.X, nesting)
			walk(node.Y, nesting)
		case *ast.FuncLit:
			walk(node.Type, nesting)
			walk(node.Body, nesting+1)
		default:
			// For other nodes, use ast.Walk to recurse
			ast.Walk(visitorFunc(func(child ast.Node) ast.Visitor {
				if child == n {
					return visitorFunc(func(c ast.Node) ast.Visitor {
						walk(c, nesting)
						return nil // walk handles recursion
					})
				}
				return nil
			}), n)
		}
	}

	walk(fn.Body, 0)
	return complexity
}

type visitorFunc func(ast.Node) ast.Visitor

func (f visitorFunc) Visit(n ast.Node) ast.Visitor {
	return f(n)
}

func calculateGoNestingDepth(fn *ast.FuncDecl) int {
	maxDepth := 0

	var walk func(ast.Node, int)
	walk = func(n ast.Node, depth int) {
		if n == nil {
			return
		}

		if depth > maxDepth {
			maxDepth = depth
		}

		switch node := n.(type) {
		case *ast.BlockStmt:
			for _, stmt := range node.List {
				walk(stmt, depth)
			}
		case *ast.IfStmt:
			walk(node.Init, depth)
			walk(node.Cond, depth)
			walk(node.Body, depth+1)
			walk(node.Else, depth)
		case *ast.ForStmt:
			walk(node.Init, depth)
			walk(node.Cond, depth)
			walk(node.Post, depth)
			walk(node.Body, depth+1)
		case *ast.RangeStmt:
			walk(node.Key, depth)
			walk(node.Value, depth)
			walk(node.X, depth)
			walk(node.Body, depth+1)
		case *ast.SwitchStmt:
			walk(node.Init, depth)
			walk(node.Tag, depth)
			walk(node.Body, depth+1)
		case *ast.TypeSwitchStmt:
			walk(node.Init, depth)
			walk(node.Assign, depth)
			walk(node.Body, depth+1)
		case *ast.SelectStmt:
			walk(node.Body, depth+1)
		case *ast.CaseClause:
			for _, stmt := range node.Body {
				walk(stmt, depth)
			}
		case *ast.CommClause:
			for _, stmt := range node.Body {
				walk(stmt, depth)
			}
		case *ast.FuncLit:
			walk(node.Type, depth)
			walk(node.Body, depth+1)
		default:
			// For other nodes, use ast.Walk to recurse without increasing depth
			ast.Walk(visitorFunc(func(child ast.Node) ast.Visitor {
				if child == n {
					return visitorFunc(func(c ast.Node) ast.Visitor {
						walk(c, depth)
						return nil
					})
				}
				return nil
			}), n)
		}
	}

	walk(fn.Body, 0)
	return maxDepth
}

func analyzePythonFile(path, workspace string, _ input) ([]complexityResult, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	relPath := relativeTo(workspace, path)
	lines := strings.Split(string(content), "\n")

	// Enhanced regex patterns for Python
	funcPattern := regexp.MustCompile(`^(\s*)def\s+(\w+)\s*\((.*?)\):`)

	var results []complexityResult
	inFunction := false
	var currentFunc *complexityResult
	funcIndent := 0

	for i, line := range lines {
		// Function definition
		if match := funcPattern.FindStringSubmatch(line); match != nil {
			if currentFunc != nil {
				classifyRisk(currentFunc)
				results = append(results, *currentFunc)
			}

			funcIndent = len(match[1])
			params := strings.Split(match[3], ",")
			paramCount := 0
			for _, p := range params {
				if strings.TrimSpace(p) != "" && strings.TrimSpace(p) != "self" {
					paramCount++
				}
			}

			currentFunc = &complexityResult{
				File:                 relPath,
				Function:             match[2],
				Line:                 i + 1,
				CyclomaticComplexity: 1,
				NestingDepth:         0,
				FunctionLength:       0,
				ParameterCount:       paramCount,
			}
			inFunction = true
			continue
		}

		if inFunction && currentFunc != nil {
			indent := len(line) - len(strings.TrimLeft(line, " \t"))

			// End of function
			if strings.TrimSpace(line) != "" && indent <= funcIndent && !strings.HasPrefix(strings.TrimSpace(line), "#") {
				classifyRisk(currentFunc)
				results = append(results, *currentFunc)
				currentFunc = nil
				inFunction = false
				continue
			}

			currentFunc.FunctionLength++

			// Count complexity
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "if ") || strings.Contains(trimmed, " if ") {
				currentFunc.CyclomaticComplexity++
			}
			if strings.HasPrefix(trimmed, "elif ") {
				currentFunc.CyclomaticComplexity++
			}
			if strings.HasPrefix(trimmed, "for ") || strings.HasPrefix(trimmed, "while ") {
				currentFunc.CyclomaticComplexity++
			}
			if strings.HasPrefix(trimmed, "except ") || strings.HasPrefix(trimmed, "except:") {
				currentFunc.CyclomaticComplexity++
			}
			if strings.Contains(trimmed, " and ") || strings.Contains(trimmed, " or ") {
				currentFunc.CyclomaticComplexity++
			}

			// Estimate nesting
			relativeIndent := (indent - funcIndent) / 4
			if relativeIndent > currentFunc.NestingDepth {
				currentFunc.NestingDepth = relativeIndent
			}
		}
	}

	// Close last function
	if currentFunc != nil {
		classifyRisk(currentFunc)
		results = append(results, *currentFunc)
	}

	return results, nil
}

func analyzeJSFile(path, workspace string, _ input) ([]complexityResult, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	relPath := relativeTo(workspace, path)
	lines := strings.Split(string(content), "\n")

	// Enhanced regex patterns for JS/TS
	funcPatterns := []*regexp.Regexp{
		regexp.MustCompile(`^\s*function\s+(\w+)\s*\((.*?)\)`),
		regexp.MustCompile(`^\s*(?:const|let|var)\s+(\w+)\s*=\s*function\s*\((.*?)\)`),
		regexp.MustCompile(`^\s*(?:const|let|var)\s+(\w+)\s*=\s*\((.*?)\)\s*=>`),
		regexp.MustCompile(`^\s*(\w+)\s*\((.*?)\)\s*{`), // Method
	}

	var results []complexityResult
	inFunction := false
	var currentFunc *complexityResult
	braceDepth := 0
	funcStartDepth := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Try to match function definition
		if !inFunction {
			for _, pattern := range funcPatterns {
				if match := pattern.FindStringSubmatch(line); match != nil {
					params := strings.Split(match[2], ",")
					paramCount := 0
					for _, p := range params {
						if strings.TrimSpace(p) != "" {
							paramCount++
						}
					}

					currentFunc = &complexityResult{
						File:                 relPath,
						Function:             match[1],
						Line:                 i + 1,
						CyclomaticComplexity: 1,
						NestingDepth:         0,
						FunctionLength:       0,
						ParameterCount:       paramCount,
					}
					inFunction = true
					funcStartDepth = braceDepth
					break
				}
			}
		}

		if inFunction && currentFunc != nil {
			currentFunc.FunctionLength++

			// Track braces
			braceDepth += strings.Count(line, "{") - strings.Count(line, "}")

			// Count complexity
			if strings.Contains(trimmed, "if ") || strings.Contains(trimmed, "if(") {
				currentFunc.CyclomaticComplexity++
			}
			if strings.Contains(trimmed, "for ") || strings.Contains(trimmed, "for(") {
				currentFunc.CyclomaticComplexity++
			}
			if strings.Contains(trimmed, "while ") || strings.Contains(trimmed, "while(") {
				currentFunc.CyclomaticComplexity++
			}
			if strings.Contains(trimmed, "case ") {
				currentFunc.CyclomaticComplexity++
			}
			if strings.Contains(trimmed, "catch ") || strings.Contains(trimmed, "catch(") {
				currentFunc.CyclomaticComplexity++
			}
			if strings.Contains(trimmed, " && ") || strings.Contains(trimmed, " || ") {
				currentFunc.CyclomaticComplexity++
			}
			if strings.Contains(trimmed, "?") && strings.Contains(trimmed, ":") {
				currentFunc.CyclomaticComplexity++
			}

			// Nesting depth estimate
			relativeDepth := braceDepth - funcStartDepth
			if relativeDepth > currentFunc.NestingDepth {
				currentFunc.NestingDepth = relativeDepth
			}

			// End of function
			if braceDepth <= funcStartDepth && currentFunc.FunctionLength > 1 {
				classifyRisk(currentFunc)
				results = append(results, *currentFunc)
				currentFunc = nil
				inFunction = false
			}
		}
	}

	return results, nil
}

func classifyRisk(result *complexityResult) {
	complexity := result.CyclomaticComplexity

	// Risk levels based on cyclomatic complexity
	if complexity >= 20 {
		result.RiskLevel = "high"
		result.Recommendations = []string{
			"Consider breaking this function into smaller, focused functions",
			"Reduce branching logic with early returns or strategy patterns",
			"Extract complex conditions into well-named helper functions",
		}
	} else if complexity >= 10 {
		result.RiskLevel = "medium"
		result.Recommendations = []string{
			"Consider simplifying logic or extracting helper functions",
			"Review for opportunities to reduce nested conditions",
		}
	} else {
		result.RiskLevel = "low"
	}

	// Additional recommendations based on other metrics
	if result.FunctionLength > 100 {
		result.Recommendations = append(result.Recommendations,
			fmt.Sprintf("Function is %d lines long - consider splitting", result.FunctionLength))
	}
	if result.NestingDepth > 4 {
		result.Recommendations = append(result.Recommendations,
			fmt.Sprintf("Nesting depth of %d is high - flatten structure", result.NestingDepth))
	}
	if result.ParameterCount > 5 {
		result.Recommendations = append(result.Recommendations,
			fmt.Sprintf("Function has %d parameters - consider using a config object", result.ParameterCount))
	}
}

func calculateStats(results []complexityResult) aggregateStats {
	stats := aggregateStats{
		TotalFunctions: len(results),
	}

	if len(results) == 0 {
		return stats
	}

	total := 0
	for _, r := range results {
		complexity := r.CyclomaticComplexity
		total += complexity
		if complexity > stats.MaxComplexity {
			stats.MaxComplexity = complexity
		}

		switch r.RiskLevel {
		case "high":
			stats.HighRiskCount++
		case "medium":
			stats.MediumRiskCount++
		case "low":
			stats.LowRiskCount++
		}
	}

	stats.AverageComplexity = float64(total) / float64(len(results))
	return stats
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

func isTestFile(name string) bool {
	name = strings.ToLower(name)
	return strings.HasSuffix(name, "_test.go") ||
		strings.HasSuffix(name, "_test.py") ||
		strings.Contains(name, ".test.") ||
		strings.Contains(name, ".spec.")
}

func isCommonExclude(name string) bool {
	excludes := []string{
		".git", ".svn", "node_modules", "vendor", "__pycache__",
		".venv", "venv", "dist", "build", "target",
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

func preparePreview(results []complexityResult, limit int) ([]complexityResult, bool) {
	if len(results) <= limit {
		return results, false
	}
	return results[:limit], true
}

func persistResultsArtifact(ctx context.Context, rc *runner.Context, results []complexityResult, truncated bool) (runner.Artifact, error) {
	if !truncated {
		return runner.Artifact{}, nil
	}
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	for _, r := range results {
		if err := enc.Encode(r); err != nil {
			return runner.Artifact{}, fmt.Errorf("encode result: %w", err)
		}
	}
	return runner.PersistBuffer(ctx, rc, buf, "application/x-ndjson", "code_complexity")
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit code/complexity failure")
	os.Exit(1)
}
