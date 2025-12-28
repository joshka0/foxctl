package skill

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestToFlagName(t *testing.T) {
	tests := []struct {
		param string
		want  string
	}{
		{"path", "path"},
		{"analysis_mode", "analysis-mode"},
		{"include_tests", "include-tests"},
		{"max_results", "max-results"},
		{"simpleFlag", "simpleFlag"},
	}

	for _, tt := range tests {
		t.Run(tt.param, func(t *testing.T) {
			got := toFlagName(tt.param)
			if got != tt.want {
				t.Errorf("toFlagName(%q) = %q, want %q", tt.param, got, tt.want)
			}
		})
	}
}

func TestNewFlagSet_StringParameter(t *testing.T) {
	params := []Parameter{
		{Name: "path", Type: "string", Default: ".", Description: "Path to file"},
	}

	fs := NewFlagSet("test", params)

	if err := fs.Parse([]string{"--path", "/tmp/test"}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	result := fs.ToJSON()
	if result["path"] != "/tmp/test" {
		t.Errorf("path = %v, want /tmp/test", result["path"])
	}
}

func TestNewFlagSet_BooleanParameter(t *testing.T) {
	params := []Parameter{
		{Name: "include_tests", Type: "boolean", Default: false, Description: "Include tests"},
	}

	fs := NewFlagSet("test", params)

	if err := fs.Parse([]string{"--include-tests"}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	result := fs.ToJSON()
	if result["include_tests"] != true {
		t.Errorf("include_tests = %v, want true", result["include_tests"])
	}
}

func TestNewFlagSet_IntegerParameter(t *testing.T) {
	params := []Parameter{
		{Name: "threshold", Type: "integer", Default: 10, Description: "Threshold value"},
	}

	fs := NewFlagSet("test", params)

	if err := fs.Parse([]string{"--threshold", "25"}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	result := fs.ToJSON()
	if result["threshold"] != 25 {
		t.Errorf("threshold = %v, want 25", result["threshold"])
	}
}

func TestNewFlagSet_ArrayParameter(t *testing.T) {
	params := []Parameter{
		{Name: "tags", Type: "array", Description: "List of tags"},
	}

	fs := NewFlagSet("test", params)

	if err := fs.Parse([]string{"--tags", "foo,bar,baz"}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	result := fs.ToJSON()
	tags, ok := result["tags"].([]string)
	if !ok {
		t.Fatalf("tags is not []string: %T", result["tags"])
	}
	if !reflect.DeepEqual(tags, []string{"foo", "bar", "baz"}) {
		t.Errorf("tags = %v, want [foo bar baz]", tags)
	}
}

func TestNewFlagSet_ObjectParameter(t *testing.T) {
	params := []Parameter{
		{Name: "config", Type: "object", Description: "Config object"},
	}

	fs := NewFlagSet("test", params)

	if err := fs.Parse([]string{"--config", `{"key":"value"}`}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	result := fs.ToJSON()
	config, ok := result["config"].(map[string]any)
	if !ok {
		t.Fatalf("config is not map[string]any: %T", result["config"])
	}
	if config["key"] != "value" {
		t.Errorf("config.key = %v, want value", config["key"])
	}
}

func TestFlagSet_MergeWithInput(t *testing.T) {
	params := []Parameter{
		{Name: "path", Type: "string", Default: "."},
		{Name: "mode", Type: "string", Default: "auto"},
		{Name: "count", Type: "integer", Default: 10},
	}

	fs := NewFlagSet("test", params)

	// Parse only --path flag
	if err := fs.Parse([]string{"--path", "/custom"}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Input JSON overrides mode
	inputJSON := []byte(`{"mode": "manual", "count": 5}`)

	merged, err := fs.MergeWithInput(inputJSON)
	if err != nil {
		t.Fatalf("MergeWithInput() error = %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(merged, &result); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	// Flag should take precedence over everything
	if result["path"] != "/custom" {
		t.Errorf("path = %v, want /custom (from flag)", result["path"])
	}
	// Input JSON should override default
	if result["mode"] != "manual" {
		t.Errorf("mode = %v, want manual (from input JSON)", result["mode"])
	}
	// Input JSON value should be preserved
	if result["count"].(float64) != 5 {
		t.Errorf("count = %v, want 5 (from input JSON)", result["count"])
	}
}

func TestFlagSet_Validate_Required(t *testing.T) {
	params := []Parameter{
		{Name: "path", Type: "string", Required: true},
	}

	fs := NewFlagSet("test", params)

	// Don't set the required parameter
	merged := map[string]any{}

	err := fs.Validate(merged)
	if err == nil {
		t.Error("expected validation error for missing required parameter")
	}
}

func TestFlagSet_Validate_Enum(t *testing.T) {
	params := []Parameter{
		{Name: "mode", Type: "string", Enum: []string{"auto", "manual", "hybrid"}},
	}

	fs := NewFlagSet("test", params)

	// Valid value
	merged := map[string]any{"mode": "auto"}
	if err := fs.Validate(merged); err != nil {
		t.Errorf("Validate() error = %v for valid enum value", err)
	}

	// Invalid value
	merged = map[string]any{"mode": "invalid"}
	if err := fs.Validate(merged); err == nil {
		t.Error("expected validation error for invalid enum value")
	}
}

func TestFlagSet_Defaults(t *testing.T) {
	params := []Parameter{
		{Name: "path", Type: "string", Default: "."},
		{Name: "verbose", Type: "boolean", Default: true},
		{Name: "count", Type: "integer", Default: 42},
	}

	fs := NewFlagSet("test", params)

	// Parse with no flags
	if err := fs.Parse([]string{}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Merge with empty input
	merged, err := fs.MergeWithInput([]byte("{}"))
	if err != nil {
		t.Fatalf("MergeWithInput() error = %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(merged, &result); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	// Defaults should be present
	if result["path"] != "." {
		t.Errorf("path = %v, want .", result["path"])
	}
	if result["verbose"] != true {
		t.Errorf("verbose = %v, want true", result["verbose"])
	}
	if result["count"].(float64) != 42 {
		t.Errorf("count = %v, want 42", result["count"])
	}
}

func TestGenerateParameterHelp(t *testing.T) {
	params := []Parameter{
		{Name: "path", Type: "string", Required: true, Description: "Path to file"},
		{Name: "mode", Type: "string", Enum: []string{"auto", "manual"}, Default: "auto"},
		{Name: "count", Type: "integer", Default: 10},
	}

	help := GenerateParameterHelp(params)

	// Check that it contains expected elements
	if !contains(help, "--path") {
		t.Error("help should contain --path")
	}
	if !contains(help, "(required)") {
		t.Error("help should contain (required)")
	}
	if !contains(help, "auto, manual") {
		t.Error("help should contain enum values")
	}
	if !contains(help, "default: 10") {
		t.Error("help should contain default value")
	}
}

func TestBuildUsage(t *testing.T) {
	tests := []struct {
		name  string
		param Parameter
		want  []string // substrings that should be present
	}{
		{
			name:  "simple description",
			param: Parameter{Name: "path", Description: "Path to file"},
			want:  []string{"Path to file"},
		},
		{
			name:  "required parameter",
			param: Parameter{Name: "path", Required: true, Description: "Path"},
			want:  []string{"(required)"},
		},
		{
			name:  "with enum",
			param: Parameter{Name: "mode", Enum: []string{"a", "b"}},
			want:  []string{"[a|b]"},
		},
		{
			name:  "with default",
			param: Parameter{Name: "count", Default: 10},
			want:  []string{"(default: 10)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage := buildUsage(tt.param)
			for _, substr := range tt.want {
				if !contains(usage, substr) {
					t.Errorf("buildUsage() = %q, should contain %q", usage, substr)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestFlagSet_FloatParameter(t *testing.T) {
	params := []Parameter{
		{Name: "ratio", Type: "number", Default: 0.5},
	}

	fs := NewFlagSet("test", params)

	if err := fs.Parse([]string{"--ratio", "0.75"}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	result := fs.ToJSON()
	if result["ratio"] != 0.75 {
		t.Errorf("ratio = %v, want 0.75", result["ratio"])
	}
}

func TestFlagSet_MultipleFlags(t *testing.T) {
	params := []Parameter{
		{Name: "path", Type: "string"},
		{Name: "analysis_mode", Type: "string", Enum: []string{"file", "function", "hotspots"}},
		{Name: "threshold", Type: "integer", Default: 10},
		{Name: "include_tests", Type: "boolean", Default: false},
	}

	fs := NewFlagSet("code/complexity", params)

	args := []string{
		"--path", ".",
		"--analysis-mode", "hotspots",
		"--threshold", "15",
		"--include-tests",
	}

	if err := fs.Parse(args); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	result := fs.ToJSON()

	if result["path"] != "." {
		t.Errorf("path = %v, want .", result["path"])
	}
	if result["analysis_mode"] != "hotspots" {
		t.Errorf("analysis_mode = %v, want hotspots", result["analysis_mode"])
	}
	if result["threshold"] != 15 {
		t.Errorf("threshold = %v, want 15", result["threshold"])
	}
	if result["include_tests"] != true {
		t.Errorf("include_tests = %v, want true", result["include_tests"])
	}
}

func TestFlagSet_EmptyParams(t *testing.T) {
	fs := NewFlagSet("test", nil)

	if err := fs.Parse([]string{}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	result := fs.ToJSON()
	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
}

func TestGenerateParameterHelp_Empty(t *testing.T) {
	help := GenerateParameterHelp(nil)
	if help != "  (no parameters)\n" {
		t.Errorf("GenerateParameterHelp(nil) = %q, want '  (no parameters)\\n'", help)
	}
}

func TestFlagSet_EmptyStringDefault(t *testing.T) {
	// Regression test: empty string defaults should be included in output
	params := []Parameter{
		{Name: "prefix", Type: "string", Default: ""},
	}

	fs := NewFlagSet("test", params)

	if err := fs.Parse([]string{}); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	result := fs.ToJSON()

	// Empty string default should be present in result
	val, exists := result["prefix"]
	if !exists {
		t.Error("prefix should exist in result even with empty string default")
	}
	if val != "" {
		t.Errorf("prefix = %q, want empty string", val)
	}
}
