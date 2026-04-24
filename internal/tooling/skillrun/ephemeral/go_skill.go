package ephemeral

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"unicode"

	"github.com/joshka0/foxctl/internal/runtime/engine"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

const defaultGoSkillToolName = "ephemeral_go_skill"

// GoSkillSpec describes one short-lived Go helper.
type GoSkillSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Source      string          `json:"source"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// GoSkillResult is the normalized output of an ephemeral Go skill run.
type GoSkillResult struct {
	OK       bool           `json:"ok"`
	Output   map[string]any `json:"output,omitempty"`
	Error    string         `json:"error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// GoSkillRunner validates and executes one synthesized Go Solve helper.
type GoSkillRunner struct {
	spec GoSkillSpec
}

// NewGoSkillRunner validates a spec and returns an executable runner.
func NewGoSkillRunner(spec GoSkillSpec) (*GoSkillRunner, error) {
	spec.Name = strings.TrimSpace(spec.Name)
	if spec.Name == "" {
		spec.Name = defaultGoSkillToolName
	}
	if spec.Description == "" {
		spec.Description = "Run a short-lived Go domain helper synthesized for this attempt."
	}
	if len(spec.Parameters) == 0 {
		spec.Parameters = json.RawMessage(`{"type":"object","additionalProperties":true}`)
	}
	normalizedSource := normalizeGoSkillSource(spec.Source)
	if err := ValidateGoSkillSource(normalizedSource); err != nil {
		return nil, err
	}
	spec.Source = normalizedSource
	return &GoSkillRunner{spec: spec}, nil
}

func (r *GoSkillRunner) ToolDef() engine.ToolDef {
	return engine.ToolDef{
		Name:        r.spec.Name,
		Description: r.spec.Description,
		Parameters:  r.spec.Parameters,
	}
}

func (r *GoSkillRunner) Run(ctx context.Context, input map[string]any) (GoSkillResult, error) {
	if r == nil {
		return GoSkillResult{}, errors.New("ephemeral Go skill runner is nil")
	}
	if err := ctx.Err(); err != nil {
		return GoSkillResult{}, err
	}
	normalizedInput, err := normalizeJSONMap(input)
	if err != nil {
		return GoSkillResult{}, fmt.Errorf("normalize input: %w", err)
	}

	i := interp.New(interp.Options{})
	if err := i.Use(stdlib.Symbols); err != nil {
		return GoSkillResult{}, fmt.Errorf("load yaegi stdlib: %w", err)
	}
	if _, err := i.Eval(r.spec.Source); err != nil {
		return GoSkillResult{}, fmt.Errorf("eval source: %w", err)
	}
	solve, err := i.Eval("Solve")
	if err != nil {
		return GoSkillResult{}, fmt.Errorf("lookup Solve: %w", err)
	}
	fn, ok := solve.Interface().(func(map[string]any) map[string]any)
	if !ok {
		return GoSkillResult{}, fmt.Errorf("Solve has incompatible signature")
	}
	var raw map[string]any
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("Solve panic: %v", recovered)
			}
		}()
		raw = fn(normalizedInput)
	}()
	if err != nil {
		return GoSkillResult{}, err
	}
	output, err := normalizeJSONMap(raw)
	if err != nil {
		return GoSkillResult{}, fmt.Errorf("Solve returned non-JSON output: %w", err)
	}
	return GoSkillResult{
		OK:     boolField(output, "ok", true),
		Output: output,
		Metadata: map[string]any{
			"runner": "yaegi",
			"name":   r.spec.Name,
		},
	}, nil
}

// GoSkillToolExecutor exposes one ephemeral Go skill as an LLM tool.
type GoSkillToolExecutor struct {
	Runner *GoSkillRunner
}

func (e GoSkillToolExecutor) List() []engine.ToolDef {
	if e.Runner == nil {
		return nil
	}
	return []engine.ToolDef{e.Runner.ToolDef()}
}

func (e GoSkillToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if e.Runner == nil {
		return "", errors.New("ephemeral Go skill executor is not configured")
	}
	if strings.TrimSpace(name) != e.Runner.spec.Name {
		return "", fmt.Errorf("unknown ephemeral Go skill %q", name)
	}
	var input map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &input); err != nil {
			return "", fmt.Errorf("decode ephemeral skill args: %w", err)
		}
	}
	result, err := e.Runner.Run(ctx, input)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// ValidateGoSkillSource enforces the first conservative source contract.
func ValidateGoSkillSource(source string) error {
	source = normalizeGoSkillSource(source)
	if source == "" {
		return errors.New("ephemeral Go skill source is empty")
	}
	file, err := parser.ParseFile(token.NewFileSet(), "ephemeral_skill.go", "package ephemeral\n"+source, parser.AllErrors)
	if err != nil {
		return fmt.Errorf("parse Go skill source: %w", err)
	}
	var solveFound bool
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != "Solve" {
			continue
		}
		solveFound = true
		if err := validateSolveSignature(fn.Type); err != nil {
			return err
		}
		if fn.Body == nil {
			return errors.New("ephemeral Go skill Solve function must include a function body")
		}
	}
	if !solveFound {
		return errors.New("ephemeral Go skill source must define Solve(input map[string]any) map[string]any")
	}
	var violations []string
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.ImportSpec:
			path := strings.Trim(typed.Path.Value, `"`)
			if !allowedGoSkillImport(path) {
				violations = append(violations, "disallowed import "+path)
			}
		case *ast.SelectorExpr:
			if ident, ok := typed.X.(*ast.Ident); ok && disallowedGoSkillSelector(ident.Name, typed.Sel.Name) {
				violations = append(violations, "disallowed selector "+ident.Name+"."+typed.Sel.Name)
			}
		}
		return true
	})
	if len(violations) > 0 {
		return errors.New(strings.Join(violations, "; "))
	}
	return nil
}

func normalizeGoSkillSource(source string) string {
	source = strings.TrimSpace(source)
	if strings.HasPrefix(source, "package ") {
		lines := strings.Split(source, "\n")
		if len(lines) <= 1 {
			return ""
		}
		source = strings.TrimSpace(strings.Join(lines[1:], "\n"))
	}
	source = repairSynthesizedGoSource(source)
	return addMissingAllowedImports(source)
}

func repairSynthesizedGoSource(source string) string {
	source = strings.TrimSpace(source)
	source = strings.Trim(source, "`")
	source = strings.TrimSpace(source)
	if source == "" {
		return ""
	}
	if parsed, ok := unquoteWholeGoSource(source); ok {
		source = strings.TrimSpace(parsed)
	}
	source = normalizeEscapedGoSource(source)
	source = trimJSONSourceFragments(source)
	if extracted, ok := extractWrappedSolveSource(source); ok {
		source = extracted
	}
	return strings.TrimSpace(source)
}

func unquoteWholeGoSource(source string) (string, bool) {
	if len(source) < 2 {
		return "", false
	}
	var parsed string
	if err := json.Unmarshal([]byte(source), &parsed); err == nil {
		return parsed, true
	}
	return "", false
}

func normalizeEscapedGoSource(source string) string {
	// LLMs often return source that has already been JSON-decoded once, but
	// still contains string-escaped code such as `\n` or `\"`. Decode the
	// source-oriented escapes that are never valid Go punctuation outside
	// string literals.
	replacements := []struct {
		old string
		new string
	}{
		{`\\n`, "\n"},
		{`\n`, "\n"},
		{`\\t`, "\t"},
		{`\t`, "\t"},
		{`\\r`, "\r"},
		{`\r`, "\r"},
		{`\"`, `"`},
		{`\\{`, `{`},
		{`\\}`, `}`},
	}
	for _, replacement := range replacements {
		source = strings.ReplaceAll(source, replacement.old, replacement.new)
	}
	return source
}

func trimJSONSourceFragments(source string) string {
	source = strings.TrimSpace(source)
	for {
		trimmed := strings.TrimRightFunc(source, unicode.IsSpace)
		trimmed = strings.TrimSuffix(trimmed, ",")
		trimmed = strings.TrimSuffix(trimmed, ";")
		trimmed = strings.TrimSuffix(trimmed, `"`)
		trimmed = strings.TrimSuffix(trimmed, `'`)
		trimmed = strings.TrimRightFunc(trimmed, unicode.IsSpace)
		if trimmed == source {
			return source
		}
		source = trimmed
	}
}

func extractWrappedSolveSource(source string) (string, bool) {
	if sourceStartsWithGoDeclaration(source) {
		return "", false
	}
	idx := strings.Index(source, "func Solve")
	if idx < 0 {
		return "", false
	}
	return extractSolveFunctionSource(source[idx:])
}

func sourceStartsWithGoDeclaration(source string) bool {
	source = strings.TrimSpace(source)
	return strings.HasPrefix(source, "func ") || strings.HasPrefix(source, "import ") || strings.HasPrefix(source, "type ") || strings.HasPrefix(source, "const ") || strings.HasPrefix(source, "var ")
}

func extractSolveFunctionSource(source string) (string, bool) {
	source = strings.TrimSpace(source)
	open := strings.IndexByte(source, '{')
	if open < 0 {
		return source, true
	}
	depth := 0
	inString := false
	inRune := false
	escaped := false
	for i, ch := range source[open:] {
		abs := open + i
		if inString || inRune {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if inString && ch == '"' {
				inString = false
			}
			if inRune && ch == '\'' {
				inRune = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '\'':
			inRune = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.TrimSpace(source[:abs+1]), true
			}
		}
	}
	return source, true
}

func addMissingAllowedImports(source string) string {
	var imports []string
	for _, pkg := range []string{"encoding/json", "fmt", "math", "sort", "strconv", "strings"} {
		name := pkg
		if slash := strings.LastIndex(pkg, "/"); slash >= 0 {
			name = pkg[slash+1:]
		}
		if !strings.Contains(source, name+".") || strings.Contains(source, `"`+pkg+`"`) {
			continue
		}
		imports = append(imports, fmt.Sprintf("import %q", pkg))
	}
	if len(imports) == 0 {
		return source
	}
	return strings.Join(imports, "\n") + "\n" + source
}

func validateSolveSignature(fn *ast.FuncType) error {
	if fn == nil || fn.Params == nil || len(fn.Params.List) != 1 || fn.Results == nil || len(fn.Results.List) != 1 {
		return errors.New("Solve must have signature Solve(input map[string]any) map[string]any")
	}
	if !isMapStringAny(fn.Params.List[0].Type) || !isMapStringAny(fn.Results.List[0].Type) {
		return errors.New("Solve must use map[string]any input and output")
	}
	return nil
}

func isMapStringAny(expr ast.Expr) bool {
	mapType, ok := expr.(*ast.MapType)
	if !ok {
		return false
	}
	key, ok := mapType.Key.(*ast.Ident)
	if !ok || key.Name != "string" {
		return false
	}
	value, ok := mapType.Value.(*ast.Ident)
	return ok && value.Name == "any"
}

func allowedGoSkillImport(path string) bool {
	switch path {
	case "encoding/json", "fmt", "math", "sort", "strconv", "strings":
		return true
	default:
		return false
	}
}

func disallowedGoSkillSelector(pkg, name string) bool {
	switch pkg {
	case "os", "exec", "syscall", "unsafe", "net", "http", "ioutil":
		return true
	default:
		return false
	}
}

func normalizeJSONMap(input map[string]any) (map[string]any, error) {
	if input == nil {
		return map[string]any{}, nil
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func boolField(values map[string]any, key string, fallback bool) bool {
	raw, ok := values[key]
	if !ok {
		return fallback
	}
	if typed, ok := raw.(bool); ok {
		return typed
	}
	if v := reflect.ValueOf(raw); v.IsValid() && v.Kind() == reflect.Bool {
		return v.Bool()
	}
	return fallback
}
