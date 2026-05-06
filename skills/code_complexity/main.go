// Package main implements the code/complexity skill.
// Analyzes code complexity metrics including cyclomatic and cognitive complexity for multiple languages.
package main

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/fsutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/sliceutil"
)

// Input defines the input parameters for code/complexity operations.
type Input struct {
	Path         string `json:"path"`
	AnalysisMode string `json:"analysis_mode" validate:"omitempty,oneof=hotspots overview"`
	Metric       string `json:"metric" validate:"omitempty,oneof=cyclomatic cognitive"`
	Threshold    int    `json:"threshold" validate:"gte=0"`
	Language     string `json:"language" validate:"omitempty,oneof=auto go python javascript typescript"`
	IncludeTests bool   `json:"include_tests"`
	MaxResults   int    `json:"max_results" validate:"gte=0"`
}

// complexityResult represents complexity analysis results for a single function.
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

// aggregateStats represents aggregate complexity statistics across all analyzed functions.
type aggregateStats struct {
	TotalFunctions    int     `json:"total_functions"`
	AverageComplexity float64 `json:"average_complexity"`
	MaxComplexity     int     `json:"max_complexity"`
	HighRiskCount     int     `json:"high_risk_count"`
	MediumRiskCount   int     `json:"medium_risk_count"`
	LowRiskCount      int     `json:"low_risk_count"`
}

// main is the skill entry point for code/complexity.
func main() {
	skillmain.Main("code/complexity", run)
}

// run orchestrates code complexity analysis across files and directories.
//
// Index:
//
//	Purpose: Analyze code complexity metrics (cyclomatic, cognitive) for functions across multiple languages
//	Flow: resolve path → detect file type → analyze files (Go AST, Python/JS regex) → filter by mode/threshold → calculate statistics → emit results
//	SideEffects: file system traversal; AST parsing; artifact persistence for large result sets
//	FailureModes: invalid path, unsupported file types, parse errors, file system errors
//	Observability: emits analysis_mode/metric/threshold/result_count/results/statistics/artifact
//	Related: analyzeDirectory, analyzeFile, analyzeGoFile, analyzePythonFile, analyzeJSFile, calculateStats
//	Keywords: code/complexity, cyclomatic, cognitive, nesting, function_length, hotspots, overview, threshold
//
// [[domain:code-complexity-analysis]]
// [[doc:docs/general/skills.md#Running Skills]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Apply defaults
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

	workspace, searchPath, err := skillmain.ResolvePath(rc, in.Path)
	if err != nil {
		return err
	}

	info, err := os.Stat(searchPath)
	if err != nil {
		return skillerr.WrapIO("stat path", err)
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
	results = sliceutil.Limit(results, in.MaxResults)

	// Calculate aggregate statistics
	stats := calculateStats(results)

	previewResult, err := skillout.PreviewAndPersistNDJSON(ctx, rc, results, rc.MaxPreview, "code_complexity", true)
	if err != nil {
		return err
	}

	data := map[string]any{
		"analysis_mode": in.AnalysisMode,
		"metric":        in.Metric,
		"threshold":     in.Threshold,
		"result_count":  len(results),
		"results":       previewResult.Preview,
		"statistics":    stats,
	}
	skillout.AddArtifact(data, previewResult.Artifact)

	return skillout.Emit(rc, "code/complexity", data)
}

// analyzeDirectory walks a directory and analyzes all supported source files.
func analyzeDirectory(dir, workspace string, in Input) ([]complexityResult, error) {
	var results []complexityResult

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if fsutil.ShouldSkipHiddenOrCommon(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		if !in.IncludeTests && fsutil.IsTestFile(d.Name()) {
			return nil
		}

		lang := fsutil.DetectLanguageWithHint(in.Language, path)
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

// analyzeFile analyzes a single file for complexity metrics based on its language.
func analyzeFile(path, workspace string, in Input) ([]complexityResult, error) {
	lang := fsutil.DetectLanguageWithHint(in.Language, path)
	if lang == "" {
		return nil, skillerr.Validation("unsupported file type")
	}

	switch lang {
	case "go":
		return analyzeGoFile(path, workspace, in)
	case "python":
		return analyzePythonFile(path, workspace, in)
	case "javascript", "typescript":
		return analyzeJSFile(path, workspace, in)
	default:
		return nil, skillerr.Validationf("language not supported: %s", lang)
	}
}

// analyzeGoFile analyzes Go code using AST parsing for accurate complexity metrics.
func analyzeGoFile(path, workspace string, _ Input) ([]complexityResult, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, skillerr.WrapParse("parse go file", err)
	}

	var results []complexityResult
	relPath := fsutil.RelativeTo(workspace, path)

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

// calculateGoCyclomaticComplexity calculates cyclomatic complexity for Go functions using AST.
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

// calculateGoCognitiveComplexity calculates cognitive complexity for Go functions using AST.
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

// visitorFunc implements ast.Visitor interface for recursive AST traversal.
type visitorFunc func(ast.Node) ast.Visitor

func (f visitorFunc) Visit(n ast.Node) ast.Visitor {
	return f(n)
}

// calculateGoNestingDepth calculates maximum nesting depth for Go functions using AST.
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

// analyzePythonFile analyzes Python code using regex patterns for complexity metrics.
func analyzePythonFile(path, workspace string, _ Input) ([]complexityResult, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	relPath := fsutil.RelativeTo(workspace, path)
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

// analyzeJSFile analyzes JavaScript/TypeScript code using regex patterns for complexity metrics.
func analyzeJSFile(path, workspace string, _ Input) ([]complexityResult, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	relPath := fsutil.RelativeTo(workspace, path)
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

// classifyRisk classifies complexity risk level and generates recommendations.
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

// calculateStats computes aggregate statistics from complexity analysis results.
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
