// Package main implements the skill/inspect skill for comprehensive skill analysis and documentation generation.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/textutil"
)

const command = "skill/inspect"

// input defines the skill input parameters for skill inspection with multiple view options and function filtering.
type input struct {
	SkillName string `json:"skill_name"`
	View      string `json:"view"`
	Function  string `json:"function"`
}

// skillInfo contains metadata about a skill including paths and identification information.
type skillInfo struct {
	Name         string
	Dir          string
	ManifestPath string
	MainGoPath   string
}

// main is the skill entry point for skill/inspect with comprehensive skill analysis capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates skill inspection with multiple view modes including manifest, types, API, implementation, and examples.
//
// Index:
// - Purpose: Analyze and document foxctl skills with multiple view modes for API discovery, type inspection, and example generation
// - Flow: validate input → find skill directory → dispatch to view handler → parse source code → generate documentation → emit results
// - SideEffects: reads skill files; parses Go source code; extracts type information; generates usage examples; analyzes manifests
// - FailureModes: missing skill directories, file access errors, Go parsing failures, invalid view specifications
// - Observability: emits skill metadata, type definitions, function signatures, API documentation, and generated examples
// - Related: findSkill, showManifest, showTypes, showAPI, showImplementation, showExamples
// - Keywords: skill/inspect, skill_analysis, api_documentation, type_extraction, example_generation, go_ast_parsing
func run(_ context.Context, rc *skillmain.RunContext, in input) error {
	// Validation (moved from parseInput)
	if in.SkillName == "" {
		return fmt.Errorf("skill_name is required")
	}
	if in.View == "" {
		in.View = "api"
	}

	// Find skill directory
	skillInfo, err := findSkill(in.SkillName)
	if err != nil {
		return err
	}

	var data map[string]any

	switch in.View {
	case "manifest":
		data, err = showManifest(skillInfo)
	case "types":
		data, err = showTypes(skillInfo)
	case "api":
		data, err = showAPI(skillInfo)
	case "implementation":
		data, err = showImplementation(skillInfo, in.Function)
	case "full":
		data, err = showFull(skillInfo)
	case "examples":
		data, err = showExamples(skillInfo)
	case "all":
		data, err = showAll(skillInfo)
	default:
		return fmt.Errorf("invalid view: %s", in.View)
	}

	if err != nil {
		return err
	}

	return skillout.Emit(rc, command, data)
}

// findSkill locates a skill directory by name and returns metadata including file paths.
func findSkill(name string) (*skillInfo, error) {
	// Convert skill name to directory (e.g., "fs/ls" -> "fs_ls")
	dirName := strings.ReplaceAll(name, "/", "_")

	skillsDir := "skills"
	skillDir := filepath.Join(skillsDir, dirName)

	// Check if directory exists
	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("skill not found: %s (looked in %s)", name, skillDir)
	}

	info := &skillInfo{
		Name:         name,
		Dir:          skillDir,
		ManifestPath: filepath.Join(skillDir, "skill.yaml"),
		MainGoPath:   filepath.Join(skillDir, "main.go"),
	}

	return info, nil
}

// showManifest displays the skill's YAML manifest file with size information.
func showManifest(info *skillInfo) (map[string]any, error) {
	content, err := os.ReadFile(info.ManifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	return map[string]any{
		"view":          "manifest",
		"skill_name":    info.Name,
		"manifest_path": info.ManifestPath,
		"manifest":      string(content),
		"size":          len(content),
	}, nil
}

// showTypes extracts and displays struct type definitions from the skill's Go source code.
func showTypes(info *skillInfo) (map[string]any, error) {
	// Read main Go file
	content, err := os.ReadFile(info.MainGoPath)
	if err != nil {
		return nil, fmt.Errorf("read main.go: %w", err)
	}

	// Parse Go file
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, info.MainGoPath, content, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse Go file: %w", err)
	}

	// Extract types
	types := extractTypes(f, fset)

	return map[string]any{
		"view":        "types",
		"skill_name":  info.Name,
		"types":       types,
		"types_count": len(types),
	}, nil
}

// showAPI combines manifest parameters with Go type definitions to display comprehensive API documentation.
func showAPI(info *skillInfo) (map[string]any, error) {
	// Get manifest
	manifestContent, err := os.ReadFile(info.ManifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	// Get types from Go code
	content, err := os.ReadFile(info.MainGoPath)
	if err != nil {
		return nil, fmt.Errorf("read main.go: %w", err)
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, info.MainGoPath, content, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse Go file: %w", err)
	}

	types := extractTypes(f, fset)

	// Extract parameters from manifest
	params := extractParametersFromManifest(string(manifestContent))

	return map[string]any{
		"view":        "api",
		"skill_name":  info.Name,
		"parameters":  params,
		"types":       types,
		"description": extractDescription(string(manifestContent)),
	}, nil
}

// showImplementation extracts function definitions with optional filtering and includes source code bodies.
func showImplementation(info *skillInfo, funcName string) (map[string]any, error) {
	// Read main Go file
	content, err := os.ReadFile(info.MainGoPath)
	if err != nil {
		return nil, fmt.Errorf("read main.go: %w", err)
	}

	// Parse Go file
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, info.MainGoPath, content, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse Go file: %w", err)
	}

	// Extract functions
	functions := extractFunctions(f, fset, string(content), funcName)

	data := map[string]any{
		"view":           "implementation",
		"skill_name":     info.Name,
		"functions":      functions,
		"function_count": len(functions),
	}

	if funcName != "" {
		data["filter"] = funcName
	}

	return data, nil
}

// showFull displays the complete source code of the skill with line count and size statistics.
func showFull(info *skillInfo) (map[string]any, error) {
	// Read main Go file
	content, err := os.ReadFile(info.MainGoPath)
	if err != nil {
		return nil, fmt.Errorf("read main.go: %w", err)
	}

	return map[string]any{
		"view":       "full",
		"skill_name": info.Name,
		"source":     string(content),
		"path":       info.MainGoPath,
		"size":       len(content),
		"lines":      textutil.CountLinesString(string(content)),
	}, nil
}

// showExamples generates usage examples based on skill parameters with basic and full usage patterns.
func showExamples(info *skillInfo) (map[string]any, error) {
	// Read manifest
	manifestContent, err := os.ReadFile(info.ManifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	// Extract parameters
	params := extractParametersFromManifest(string(manifestContent))

	// Generate examples
	examples := generateExamples(info.Name, params)

	return map[string]any{
		"view":       "examples",
		"skill_name": info.Name,
		"examples":   examples,
	}, nil
}

// showAll combines all view modes to provide comprehensive skill documentation in a single response.
func showAll(info *skillInfo) (map[string]any, error) {
	// Read manifest
	manifestContent, err := os.ReadFile(info.ManifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	content, err := os.ReadFile(info.MainGoPath)
	if err != nil {
		return nil, fmt.Errorf("read main.go: %w", err)
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, info.MainGoPath, content, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse Go file: %w", err)
	}

	types := extractTypes(f, fset)
	functions := extractFunctions(f, fset, string(content), "")
	params := extractParametersFromManifest(string(manifestContent))
	examples := generateExamples(info.Name, params)

	return map[string]any{
		"view":         "all",
		"skill_name":   info.Name,
		"manifest":     string(manifestContent),
		"types":        types,
		"functions":    functions,
		"parameters":   params,
		"examples":     examples,
		"source_size":  len(content),
		"source_lines": textutil.CountLinesString(string(content)),
	}, nil
}

// extractTypes parses Go AST to extract struct type definitions with field names, types, and JSON tags.
func extractTypes(f *ast.File, _ *token.FileSet) []map[string]any {
	var types []map[string]any

	ast.Inspect(f, func(n ast.Node) bool {
		typeSpec, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}

		structType, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
			return true
		}

		fields := []map[string]string{}
		for _, field := range structType.Fields.List {
			for _, name := range field.Names {
				fieldType := ""
				if field.Type != nil {
					fieldType = formatType(field.Type)
				}

				// Extract JSON tag
				jsonTag := ""
				if field.Tag != nil {
					tag := field.Tag.Value
					re := regexp.MustCompile(`json:"([^"]+)"`)
					if matches := re.FindStringSubmatch(tag); len(matches) > 1 {
						jsonTag = matches[1]
					}
				}

				fields = append(fields, map[string]string{
					"name":     name.Name,
					"type":     fieldType,
					"json_tag": jsonTag,
				})
			}
		}

		types = append(types, map[string]any{
			"name":   typeSpec.Name.Name,
			"kind":   "struct",
			"fields": fields,
		})

		return true
	})

	return types
}

// extractFunctions parses Go AST to extract function definitions with signatures, parameters, and source bodies.
func extractFunctions(f *ast.File, fset *token.FileSet, source string, filter string) []map[string]any {
	var functions []map[string]any

	ast.Inspect(f, func(n ast.Node) bool {
		funcDecl, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}

		funcName := funcDecl.Name.Name

		// Apply filter if specified
		if filter != "" && funcName != filter {
			return true
		}

		// Extract function signature
		params := []string{}
		if funcDecl.Type.Params != nil {
			for _, param := range funcDecl.Type.Params.List {
				paramType := formatType(param.Type)
				for _, name := range param.Names {
					params = append(params, fmt.Sprintf("%s %s", name.Name, paramType))
				}
			}
		}

		results := []string{}
		if funcDecl.Type.Results != nil {
			for _, result := range funcDecl.Type.Results.List {
				resultType := formatType(result.Type)
				if len(result.Names) > 0 {
					for _, name := range result.Names {
						results = append(results, fmt.Sprintf("%s %s", name.Name, resultType))
					}
				} else {
					results = append(results, resultType)
				}
			}
		}

		// Extract function body
		start := fset.Position(funcDecl.Pos()).Offset
		end := fset.Position(funcDecl.End()).Offset
		body := ""
		if start >= 0 && end <= len(source) {
			body = source[start:end]
		}

		functions = append(functions, map[string]any{
			"name":    funcName,
			"params":  params,
			"returns": results,
			"body":    body,
		})

		return true
	})

	return functions
}

// formatType converts Go AST type expressions to string representation with proper formatting.
func formatType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + formatType(t.X)
	case *ast.ArrayType:
		return "[]" + formatType(t.Elt)
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", formatType(t.Key), formatType(t.Value))
	case *ast.SelectorExpr:
		return fmt.Sprintf("%s.%s", formatType(t.X), t.Sel.Name)
	default:
		return "unknown"
	}
}

// extractParametersFromManifest parses YAML manifest to extract parameter definitions with types and descriptions.
func extractParametersFromManifest(manifest string) []map[string]string {
	var params []map[string]string

	// Simple YAML parsing for parameters
	scanner := bufio.NewScanner(strings.NewReader(manifest))
	inParams := false
	currentParam := map[string]string{}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "parameters:") {
			inParams = true
			continue
		}

		if inParams && strings.HasPrefix(trimmed, "returns:") {
			inParams = false
			continue
		}

		if inParams {
			if strings.HasPrefix(trimmed, "- name:") {
				// Save previous param
				if len(currentParam) > 0 {
					params = append(params, currentParam)
				}
				currentParam = map[string]string{
					"name": strings.TrimSpace(strings.TrimPrefix(trimmed, "- name:")),
				}
			} else if strings.HasPrefix(trimmed, "type:") {
				currentParam["type"] = strings.TrimSpace(strings.TrimPrefix(trimmed, "type:"))
			} else if strings.HasPrefix(trimmed, "required:") {
				currentParam["required"] = strings.TrimSpace(strings.TrimPrefix(trimmed, "required:"))
			} else if strings.HasPrefix(trimmed, "description:") {
				desc := strings.TrimSpace(strings.TrimPrefix(trimmed, "description:"))
				currentParam["description"] = strings.Trim(desc, "\"")
			} else if strings.HasPrefix(trimmed, "default:") {
				def := strings.TrimSpace(strings.TrimPrefix(trimmed, "default:"))
				currentParam["default"] = strings.Trim(def, "\"")
			}
		}
	}

	// Save last param
	if len(currentParam) > 0 {
		params = append(params, currentParam)
	}

	return params
}

// extractDescription retrieves the skill description from the YAML manifest using regex parsing.
func extractDescription(manifest string) string {
	re := regexp.MustCompile(`description:\s*"([^"]+)"`)
	matches := re.FindStringSubmatch(manifest)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// generateExamples creates JSON command examples based on skill parameters with basic and full usage patterns.
func generateExamples(skillName string, params []map[string]string) []map[string]string {
	examples := []map[string]string{}

	// Generate basic example
	basicInput := map[string]any{}
	for _, param := range params {
		if param["required"] == "true" {
			basicInput[param["name"]] = generateExampleValue(param)
		}
	}

	if len(basicInput) > 0 {
		if inputJSON, err := json.MarshalIndent(basicInput, "", "  "); err == nil {
			examples = append(examples, map[string]string{
				"name":        "basic",
				"description": "Basic usage with required parameters",
				"input":       string(inputJSON),
				"command":     fmt.Sprintf("echo '%s' | foxctl run %s", string(inputJSON), skillName),
			})
		}
	}

	// Generate full example
	fullInput := map[string]any{}
	for _, param := range params {
		fullInput[param["name"]] = generateExampleValue(param)
	}

	if len(fullInput) > 0 {
		if inputJSON, err := json.MarshalIndent(fullInput, "", "  "); err == nil {
			examples = append(examples, map[string]string{
				"name":        "full",
				"description": "Full usage with all parameters",
				"input":       string(inputJSON),
				"command":     fmt.Sprintf("echo '%s' | foxctl run %s", string(inputJSON), skillName),
			})
		}
	}

	return examples
}

// generateExampleValue creates appropriate example values based on parameter types and defaults.
func generateExampleValue(param map[string]string) any {
	if param["default"] != "" {
		return param["default"]
	}

	switch param["type"] {
	case "string":
		if param["name"] == "path" {
			return "./example"
		}
		return "example"
	case "boolean":
		return false
	case "integer":
		return 10
	case "array":
		return []string{"item1", "item2"}
	default:
		return "value"
	}
}
