package workflow

import (
	"testing"
)

func TestTemplateEngine_RenderString(t *testing.T) {
	engine := NewTemplateEngine()
	ctx := NewExecutionContext(map[string]any{
		"name": "test",
		"path": "/foo/bar",
	})

	tests := []struct {
		name     string
		template string
		expected string
	}{
		{
			name:     "simple variable",
			template: "Hello {{.inputs.name}}",
			expected: "Hello test",
		},
		{
			name:     "no template",
			template: "plain text",
			expected: "plain text",
		},
		{
			name:     "multiple variables",
			template: "{{.inputs.name}} at {{.inputs.path}}",
			expected: "test at /foo/bar",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := engine.RenderString(tc.template, ctx)
			if err != nil {
				t.Fatalf("RenderString failed: %v", err)
			}
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestTemplateEngine_RenderWithStepResults(t *testing.T) {
	engine := NewTemplateEngine()
	ctx := NewExecutionContext(map[string]any{})

	// Add step result
	ctx.Set("find", &StepResult{
		StepID: "find",
		Status: StepCompleted,
		Data: map[string]any{
			"files": []any{"a.go", "b.go", "c.go"},
			"count": 3,
		},
	})

	tests := []struct {
		name     string
		template string
		expected string
	}{
		{
			name:     "access step data",
			template: "Found {{.find.data.count}} files",
			expected: "Found 3 files",
		},
		{
			name:     "access step status",
			template: "Status: {{.find.status}}",
			expected: "Status: completed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := engine.RenderString(tc.template, ctx)
			if err != nil {
				t.Fatalf("RenderString failed: %v", err)
			}
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestTemplateEngine_RenderMap(t *testing.T) {
	engine := NewTemplateEngine()
	ctx := NewExecutionContext(map[string]any{
		"path": "/test",
	})

	input := map[string]any{
		"file": "{{.inputs.path}}/main.go",
		"nested": map[string]any{
			"value": "{{.inputs.path}}/nested",
		},
	}

	result, err := engine.Render(input, ctx)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}

	if resultMap["file"] != "/test/main.go" {
		t.Errorf("expected '/test/main.go', got %v", resultMap["file"])
	}

	nested, ok := resultMap["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map, got %T", resultMap["nested"])
	}
	if nested["value"] != "/test/nested" {
		t.Errorf("expected '/test/nested', got %v", nested["value"])
	}
}

func TestTemplateEngine_EvaluateCondition(t *testing.T) {
	engine := NewTemplateEngine()

	tests := []struct {
		name      string
		condition string
		inputs    map[string]any
		expected  bool
	}{
		{
			name:      "true condition",
			condition: ".inputs.enabled",
			inputs:    map[string]any{"enabled": true},
			expected:  true,
		},
		{
			name:      "false condition",
			condition: ".inputs.enabled",
			inputs:    map[string]any{"enabled": false},
			expected:  false,
		},
		{
			name:      "comparison",
			condition: "gt .inputs.count 5",
			inputs:    map[string]any{"count": 10},
			expected:  true,
		},
		{
			name:      "empty string is false",
			condition: "",
			inputs:    map[string]any{},
			expected:  true, // Empty condition defaults to true
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := NewExecutionContext(tc.inputs)
			result, err := engine.EvaluateCondition(tc.condition, ctx)
			if err != nil {
				t.Fatalf("EvaluateCondition failed: %v", err)
			}
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestTemplateFunctions_Collections(t *testing.T) {
	// Test len
	if length([]any{1, 2, 3}) != 3 {
		t.Error("len failed")
	}

	// Test first
	if first([]any{1, 2, 3}) != 1 {
		t.Error("first failed")
	}

	// Test last
	if last([]any{1, 2, 3}) != 3 {
		t.Error("last failed")
	}

	// Test index
	if index([]any{"a", "b", "c"}, 1) != "b" {
		t.Error("index failed")
	}

	// Test keys
	m := map[string]any{"a": 1, "b": 2}
	k := keys(m)
	if len(k) != 2 {
		t.Error("keys failed")
	}

	// Test flatten
	nested := []any{[]any{1, 2}, []any{3, 4}}
	flat := flatten(nested)
	if len(flat) != 4 {
		t.Errorf("flatten failed: expected 4, got %d", len(flat))
	}

	// Test unique
	dup := []any{"a", "b", "a", "c", "b"}
	uniq := unique(dup)
	if len(uniq) != 3 {
		t.Errorf("unique failed: expected 3, got %d", len(uniq))
	}

	// Test reverse
	arr := []any{1, 2, 3}
	rev := reverse(arr)
	if rev[0] != 3 || rev[2] != 1 {
		t.Error("reverse failed")
	}
}

func TestTemplateFunctions_Strings(t *testing.T) {
	// These use standard library functions, just sanity check
	if pathBase("/foo/bar/baz.txt") != "baz.txt" {
		t.Error("pathBase failed")
	}

	if pathDir("/foo/bar/baz.txt") != "/foo/bar" {
		t.Error("pathDir failed")
	}

	if pathExt("/foo/bar/baz.txt") != ".txt" {
		t.Error("pathExt failed")
	}
}

func TestTemplateFunctions_Conditional(t *testing.T) {
	// Test empty
	if !empty("") {
		t.Error("empty string should be empty")
	}
	if !empty(nil) {
		t.Error("nil should be empty")
	}
	if empty("hello") {
		t.Error("non-empty string should not be empty")
	}

	// Test default
	if defaultVal("fallback", nil) != "fallback" {
		t.Error("default with nil should return fallback")
	}
	if defaultVal("fallback", "value") != "value" {
		t.Error("default with value should return value")
	}

	// Test ternary
	if ternary(true, "yes", "no") != "yes" {
		t.Error("ternary true failed")
	}
	if ternary(false, "yes", "no") != "no" {
		t.Error("ternary false failed")
	}

	// Test coalesce
	if coalesce(nil, "", "first") != "first" {
		t.Error("coalesce failed")
	}
}

func TestTemplateFunctions_Math(t *testing.T) {
	if add(1, 2) != 3 {
		t.Error("add failed")
	}
	if sub(5, 3) != 2 {
		t.Error("sub failed")
	}
	if mul(3, 4) != 12 {
		t.Error("mul failed")
	}
	if div(10, 2) != 5 {
		t.Error("div failed")
	}
	if mod(10, 3) != 1 {
		t.Error("mod failed")
	}
	if maxNum(3, 7) != 7 {
		t.Error("max failed")
	}
	if minNum(3, 7) != 3 {
		t.Error("min failed")
	}

	// Test div by zero
	if div(10, 0) != 0 {
		t.Error("div by zero should return 0")
	}
}

func TestTemplateFunctions_GetField(t *testing.T) {
	data := map[string]any{
		"foo": map[string]any{
			"bar": "baz",
		},
	}

	if getField(data, "foo.bar") != "baz" {
		t.Error("getField failed for nested path")
	}

	if getField(data, "foo.nonexistent") != nil {
		t.Error("getField should return nil for nonexistent path")
	}

	if getField(nil, "foo") != nil {
		t.Error("getField should return nil for nil input")
	}
}

func TestTemplateFunctions_Pick(t *testing.T) {
	data := map[string]any{
		"a": 1,
		"b": 2,
		"c": 3,
	}

	picked := pick(data, "a", "c")
	if len(picked) != 2 {
		t.Errorf("expected 2 keys, got %d", len(picked))
	}
	if picked["a"] != 1 || picked["c"] != 3 {
		t.Error("pick returned wrong values")
	}
	if _, ok := picked["b"]; ok {
		t.Error("pick should not include 'b'")
	}
}

func TestTemplateFunctions_Omit(t *testing.T) {
	data := map[string]any{
		"a": 1,
		"b": 2,
		"c": 3,
	}

	omitted := omit(data, "b")
	if len(omitted) != 2 {
		t.Errorf("expected 2 keys, got %d", len(omitted))
	}
	if _, ok := omitted["b"]; ok {
		t.Error("omit should not include 'b'")
	}
}

func TestTemplateFunctions_Merge(t *testing.T) {
	m1 := map[string]any{"a": 1, "b": 2}
	m2 := map[string]any{"b": 3, "c": 4}

	merged := merge(m1, m2)
	if merged["a"] != 1 {
		t.Error("merge should keep 'a' from m1")
	}
	if merged["b"] != 3 {
		t.Error("merge should override 'b' with m2 value")
	}
	if merged["c"] != 4 {
		t.Error("merge should include 'c' from m2")
	}
}
