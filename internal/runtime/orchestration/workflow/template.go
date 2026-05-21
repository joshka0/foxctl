package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

// TemplateEngine renders template expressions within workflow inputs.
type TemplateEngine struct {
	funcs template.FuncMap
}

// NewTemplateEngine creates a template engine with standard functions.
func NewTemplateEngine() *TemplateEngine {
	return &TemplateEngine{
		funcs: standardFuncs(),
	}
}

// standardFuncs returns the built-in template functions.
func standardFuncs() template.FuncMap {
	return template.FuncMap{
		// Type conversion (use safe versions for templates - they don't return errors)
		"toJSON":   toJSONSafe,
		"fromJSON": fromJSONSafe,
		"toString": toString,
		"toInt":    toIntSafe,
		"toBool":   toBool,

		// String functions
		"upper":      strings.ToUpper,
		"lower":      strings.ToLower,
		"trim":       strings.TrimSpace,
		"trimPrefix": strings.TrimPrefix,
		"trimSuffix": strings.TrimSuffix,
		"replace":    strings.ReplaceAll,
		"split":      strings.Split,
		"join":       strings.Join,
		"contains":   strings.Contains,
		"hasPrefix":  strings.HasPrefix,
		"hasSuffix":  strings.HasSuffix,

		// Collection functions
		"len":     length,
		"first":   first,
		"last":    last,
		"index":   index,
		"slice":   sliceFunc,
		"keys":    keys,
		"values":  values,
		"filter":  filter,
		"map":     mapFunc,
		"flatMap": flatMap,
		"flatten": flatten,
		"unique":  unique,
		"sort":    sortSlice,
		"reverse": reverse,
		"append":  appendSlice,
		"concat":  concat,

		// Conditional functions
		"default":  defaultVal,
		"coalesce": coalesce,
		"ternary":  ternary,
		"empty":    empty,

		// Comparison
		"eq": eq,
		"ne": ne,
		"lt": lt,
		"le": le,
		"gt": gt,
		"ge": ge,

		// Logic
		"and": and,
		"or":  or,
		"not": not,

		// Path functions
		"base":     pathBase,
		"dir":      pathDir,
		"ext":      pathExt,
		"clean":    pathClean,
		"joinPath": joinPath,

		// Data access
		"get":    getField,
		"pluck":  pluck,
		"pick":   pick,
		"omit":   omit,
		"merge":  merge,
		"hasKey": hasKey,

		// Math
		"add": add,
		"sub": sub,
		"mul": mul,
		"div": div,
		"mod": mod,
		"max": maxNum,
		"min": minNum,
	}
}

// Render processes a value, expanding any template expressions.
func (e *TemplateEngine) Render(value any, ctx *ExecutionContext) (any, error) {
	return e.render(value, e.buildData(ctx))
}

// RenderString renders a single template string.
func (e *TemplateEngine) RenderString(tmpl string, ctx *ExecutionContext) (string, error) {
	data := e.buildData(ctx)
	return e.renderString(tmpl, data)
}

// buildData constructs the template data from execution context.
func (e *TemplateEngine) buildData(ctx *ExecutionContext) map[string]any {
	data := make(map[string]any)

	// Add inputs
	data["inputs"] = ctx.Inputs

	// Add step results
	for id, result := range ctx.Steps {
		stepData := map[string]any{
			"status": result.Status,
			"data":   result.Data,
		}
		if result.Error != "" {
			stepData["error"] = result.Error
		}
		if len(result.Iterations) > 0 {
			var iterData []any
			for _, iter := range result.Iterations {
				iterData = append(iterData, iter.Data)
			}
			stepData["iterations"] = iterData
		}
		data[id] = stepData
	}

	// Add loop variables
	for k, v := range ctx.Vars {
		data[k] = v
	}

	return data
}

// render recursively processes a value, expanding templates.
func (e *TemplateEngine) render(value any, data map[string]any) (any, error) {
	switch v := value.(type) {
	case string:
		return e.renderValue(v, data)
	case map[string]any:
		result := make(map[string]any)
		for k, val := range v {
			rendered, err := e.render(val, data)
			if err != nil {
				return nil, fmt.Errorf("rendering %s: %w", k, err)
			}
			result[k] = rendered
		}
		return result, nil
	case []any:
		result := make([]any, len(v))
		for i, val := range v {
			rendered, err := e.render(val, data)
			if err != nil {
				return nil, fmt.Errorf("rendering [%d]: %w", i, err)
			}
			result[i] = rendered
		}
		return result, nil
	default:
		return value, nil
	}
}

// templatePattern matches {{...}} expressions.
var templatePattern = regexp.MustCompile(`\{\{.*?\}\}`)

// renderValue handles string values, detecting and expanding templates.
func (e *TemplateEngine) renderValue(s string, data map[string]any) (any, error) {
	// Check if the entire string is a single template expression
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "{{") && strings.HasSuffix(s, "}}") {
		// Check if there's only one expression
		matches := templatePattern.FindAllString(s, -1)
		if len(matches) == 1 {
			// Single expression - evaluate and return typed value
			return e.evaluateExpression(s, data)
		}
	}

	// Multiple expressions or mixed content - render as string
	return e.renderString(s, data)
}

// evaluateExpression evaluates a template and returns a typed value.
func (e *TemplateEngine) evaluateExpression(tmpl string, data map[string]any) (any, error) {
	// Render to string first
	result, err := e.renderString(tmpl, data)
	if err != nil {
		return nil, err
	}

	// Try to parse as JSON to preserve types
	var parsed any
	if err := json.Unmarshal([]byte(result), &parsed); err == nil {
		return parsed, nil
	}

	// Return as string if not valid JSON
	return result, nil
}

// renderString renders a template to a string.
func (e *TemplateEngine) renderString(tmpl string, data map[string]any) (string, error) {
	// Quick check for non-template strings
	if !strings.Contains(tmpl, "{{") {
		return tmpl, nil
	}

	t, err := template.New("").Funcs(e.funcs).Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}

// EvaluateCondition evaluates a condition template to a boolean.
func (e *TemplateEngine) EvaluateCondition(condition string, ctx *ExecutionContext) (bool, error) {
	if condition == "" {
		return true, nil
	}

	data := e.buildData(ctx)

	// Wrap in if block to get boolean result
	tmpl := fmt.Sprintf("{{if %s}}true{{else}}false{{end}}", strings.TrimPrefix(strings.TrimSuffix(condition, "}}"), "{{"))

	result, err := e.renderString(tmpl, data)
	if err != nil {
		return false, err
	}

	return result == "true", nil
}

// Template functions implementation

func toJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal to JSON: %w", err)
	}
	return string(b), nil
}

// toJSONSafe wraps toJSON for template use, returning empty string on error.
func toJSONSafe(v any) string {
	s, _ := toJSON(v)
	return s
}

func fromJSON(s string) (any, error) {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, fmt.Errorf("unmarshal JSON: %w", err)
	}
	return v, nil
}

// fromJSONSafe wraps fromJSON for template use, returning nil on error.
func fromJSONSafe(s string) any {
	v, _ := fromJSON(s)
	return v
}

func toString(v any) string {
	return fmt.Sprintf("%v", v)
}

func toInt(v any) (int, error) {
	switch val := v.(type) {
	case int:
		return val, nil
	case int64:
		return int(val), nil
	case float64:
		return int(val), nil
	case string:
		i, err := strconv.Atoi(val)
		if err != nil {
			return 0, fmt.Errorf("convert string to int: %w", err)
		}
		return i, nil
	default:
		return 0, fmt.Errorf("cannot convert %T to int", v)
	}
}

// toIntSafe wraps toInt for template use, returning 0 on error.
func toIntSafe(v any) int {
	i, _ := toInt(v)
	return i
}

func toBool(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true" || val == "1" || val == "yes"
	case int, int64, float64:
		return toIntSafe(v) != 0
	default:
		return v != nil
	}
}

func length(v any) int {
	if v == nil {
		return 0
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Array, reflect.Slice, reflect.Map, reflect.String:
		return rv.Len()
	default:
		return 0
	}
}

func first(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice && rv.Len() > 0 {
		return rv.Index(0).Interface()
	}
	return nil
}

func last(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice && rv.Len() > 0 {
		return rv.Index(rv.Len() - 1).Interface()
	}
	return nil
}

func index(v any, i int) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice && i >= 0 && i < rv.Len() {
		return rv.Index(i).Interface()
	}
	return nil
}

func sliceFunc(v any, start, end int) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return nil
	}
	if start < 0 {
		start = 0
	}
	if end > rv.Len() {
		end = rv.Len()
	}
	return rv.Slice(start, end).Interface()
}

func keys(v any) []string {
	// Initialize to empty slice so JSON serializes as [] not null
	result := make([]string, 0)
	if v == nil {
		return result
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Map {
		return result
	}
	for _, k := range rv.MapKeys() {
		result = append(result, fmt.Sprintf("%v", k.Interface()))
	}
	return result
}

func values(v any) []any {
	// Initialize to empty slice so JSON serializes as [] not null
	result := make([]any, 0)
	if v == nil {
		return result
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Map {
		return result
	}
	for _, k := range rv.MapKeys() {
		result = append(result, rv.MapIndex(k).Interface())
	}
	return result
}

func filter(v any, key string) []any {
	// Initialize to empty slice so JSON serializes as [] not null
	result := make([]any, 0)
	// Simple filter: extract items where key is truthy
	if v == nil {
		return result
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return result
	}
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i).Interface()
		if val := getField(item, key); toBool(val) {
			result = append(result, item)
		}
	}
	return result
}

func mapFunc(v any, key string) []any {
	// Initialize to empty slice so JSON serializes as [] not null
	result := make([]any, 0)
	// Extract a field from each item in slice
	if v == nil {
		return result
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return result
	}
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i).Interface()
		result = append(result, getField(item, key))
	}
	return result
}

func flatMap(v any, key string) []any {
	// Extract and flatten arrays from each item
	mapped := mapFunc(v, key)
	return flatten(mapped)
}

func flatten(v any) []any {
	// Initialize to empty slice so JSON serializes as [] not null
	result := make([]any, 0)
	if v == nil {
		return result
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return result
	}
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i).Interface()
		if inner := reflect.ValueOf(item); inner.Kind() == reflect.Slice {
			for j := 0; j < inner.Len(); j++ {
				result = append(result, inner.Index(j).Interface())
			}
		} else {
			result = append(result, item)
		}
	}
	return result
}

func unique(v any) []any {
	// Initialize to empty slice so JSON serializes as [] not null
	result := make([]any, 0)
	if v == nil {
		return result
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return result
	}
	seen := make(map[string]bool)
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i).Interface()
		key := toJSONSafe(item)
		if !seen[key] {
			seen[key] = true
			result = append(result, item)
		}
	}
	return result
}

func sortSlice(v any) []any {
	// Simple string sort
	if v == nil {
		return make([]any, 0) // Empty slice for JSON serialization
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return make([]any, 0) // Empty slice for JSON serialization
	}
	if rv.Len() == 0 {
		return make([]any, 0) // Empty slice for JSON serialization
	}
	result := make([]string, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		result[i] = toString(rv.Index(i).Interface())
	}
	// Sort
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[i] > result[j] {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	out := make([]any, len(result))
	for i, s := range result {
		out[i] = s
	}
	return out
}

func reverse(v any) []any {
	if v == nil {
		return make([]any, 0) // Empty slice for JSON serialization
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return make([]any, 0) // Empty slice for JSON serialization
	}
	if rv.Len() == 0 {
		return make([]any, 0) // Empty slice for JSON serialization
	}
	result := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		result[rv.Len()-1-i] = rv.Index(i).Interface()
	}
	return result
}

func appendSlice(v any, items ...any) []any {
	result := toSlice(v)
	return append(result, items...)
}

func concat(slices ...any) []any {
	var result []any
	for _, s := range slices {
		result = append(result, toSlice(s)...)
	}
	return result
}

func toSlice(v any) []any {
	if v == nil {
		return make([]any, 0) // Empty slice for JSON serialization
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return []any{v}
	}
	if rv.Len() == 0 {
		return make([]any, 0) // Empty slice for JSON serialization
	}
	result := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		result[i] = rv.Index(i).Interface()
	}
	return result
}

func defaultVal(defaultValue, value any) any {
	if empty(value) {
		return defaultValue
	}
	return value
}

func coalesce(values ...any) any {
	for _, v := range values {
		if !empty(v) {
			return v
		}
	}
	return nil
}

func ternary(condition bool, trueVal, falseVal any) any {
	if condition {
		return trueVal
	}
	return falseVal
}

func empty(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return rv.Len() == 0
	case reflect.Slice, reflect.Map, reflect.Array:
		return rv.Len() == 0
	case reflect.Bool:
		return !rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() == 0
	case reflect.Pointer, reflect.Interface:
		return rv.IsNil()
	default:
		return false
	}
}

func eq(a, b any) bool   { return toJSONSafe(a) == toJSONSafe(b) }
func ne(a, b any) bool   { return !eq(a, b) }
func lt(a, b any) bool   { return toIntSafe(a) < toIntSafe(b) }
func le(a, b any) bool   { return toIntSafe(a) <= toIntSafe(b) }
func gt(a, b any) bool   { return toIntSafe(a) > toIntSafe(b) }
func ge(a, b any) bool   { return toIntSafe(a) >= toIntSafe(b) }
func and(a, b bool) bool { return a && b }
func or(a, b bool) bool  { return a || b }
func not(a bool) bool    { return !a }

func pathBase(s string) string {
	i := len(s) - 1
	for i >= 0 && s[i] != '/' {
		i--
	}
	return s[i+1:]
}

func pathDir(s string) string {
	i := len(s) - 1
	for i >= 0 && s[i] != '/' {
		i--
	}
	if i < 0 {
		return "."
	}
	return s[:i]
}

func pathExt(s string) string {
	for i := len(s) - 1; i >= 0 && s[i] != '/'; i-- {
		if s[i] == '.' {
			return s[i:]
		}
	}
	return ""
}

func pathClean(s string) string {
	// Simple clean: remove trailing slashes and double slashes
	s = strings.ReplaceAll(s, "//", "/")
	return strings.TrimSuffix(s, "/")
}

func joinPath(parts ...string) string {
	return strings.Join(parts, "/")
}

func getField(v any, path string) any {
	if v == nil {
		return nil
	}

	parts := strings.Split(path, ".")
	current := v

	for _, part := range parts {
		if current == nil {
			return nil
		}

		rv := reflect.ValueOf(current)

		// Handle pointer
		if rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return nil
			}
			rv = rv.Elem()
		}

		switch rv.Kind() {
		case reflect.Map:
			mapVal := rv.MapIndex(reflect.ValueOf(part))
			if !mapVal.IsValid() {
				return nil
			}
			current = mapVal.Interface()
		case reflect.Struct:
			field := rv.FieldByName(part)
			if !field.IsValid() {
				return nil
			}
			current = field.Interface()
		default:
			return nil
		}
	}

	return current
}

func pluck(v any, key string) []any {
	return mapFunc(v, key)
}

func pick(v any, keys ...string) map[string]any {
	// Initialize to empty map so JSON serializes as {} not null
	result := make(map[string]any)
	if v == nil {
		return result
	}
	m, ok := v.(map[string]any)
	if !ok {
		return result
	}
	for _, k := range keys {
		if val, ok := m[k]; ok {
			result[k] = val
		}
	}
	return result
}

func omit(v any, keys ...string) map[string]any {
	// Initialize to empty map so JSON serializes as {} not null
	result := make(map[string]any)
	if v == nil {
		return result
	}
	m, ok := v.(map[string]any)
	if !ok {
		return result
	}
	skip := make(map[string]bool)
	for _, k := range keys {
		skip[k] = true
	}
	for k, val := range m {
		if !skip[k] {
			result[k] = val
		}
	}
	return result
}

func merge(maps ...any) map[string]any {
	result := make(map[string]any)
	for _, m := range maps {
		if mm, ok := m.(map[string]any); ok {
			for k, v := range mm {
				result[k] = v
			}
		}
	}
	return result
}

func hasKey(m any, key string) bool {
	if m == nil {
		return false
	}
	mm, ok := m.(map[string]any)
	if !ok {
		return false
	}
	_, exists := mm[key]
	return exists
}

func add(a, b any) int { return toIntSafe(a) + toIntSafe(b) }
func sub(a, b any) int { return toIntSafe(a) - toIntSafe(b) }
func mul(a, b any) int { return toIntSafe(a) * toIntSafe(b) }
func div(a, b any) int {
	bv := toIntSafe(b)
	if bv == 0 {
		return 0
	}
	return toIntSafe(a) / bv
}

func mod(a, b any) int {
	bv := toIntSafe(b)
	if bv == 0 {
		return 0
	}
	return toIntSafe(a) % bv
}

func maxNum(a, b any) int {
	av, bv := toIntSafe(a), toIntSafe(b)
	if av > bv {
		return av
	}
	return bv
}

func minNum(a, b any) int {
	av, bv := toIntSafe(a), toIntSafe(b)
	if av < bv {
		return av
	}
	return bv
}
