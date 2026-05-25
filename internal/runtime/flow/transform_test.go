package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"
)

// ---------------------------------------------------------------------------
// Passthrough tests (VAL-M2-018)
// ---------------------------------------------------------------------------

func TestPassthrough(t *testing.T) {
	tests := []struct {
		name  string
		input any
	}{
		{"string", "hello"},
		{"number", 42.0},
		{"bool", true},
		{"nil", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := passthroughTransform(context.Background(), tc.input, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out != tc.input {
				t.Errorf("passthrough: got %v (%T), want %v (%T)", out, out, tc.input, tc.input)
			}
		})
	}

	// For non-comparable types (maps, slices), verify identity by pointer.
	t.Run("object", func(t *testing.T) {
		input := map[string]any{"key": "value"}
		out, err := passthroughTransform(context.Background(), input, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertEqualJSON(t, out, input)
	})

	t.Run("array", func(t *testing.T) {
		input := []any{1, 2, 3}
		out, err := passthroughTransform(context.Background(), input, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertEqualJSON(t, out, input)
	})
}

// ---------------------------------------------------------------------------
// RegexExtract tests (VAL-M2-019)
// ---------------------------------------------------------------------------

func TestRegexExtract(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		config  string
		want    any
		wantErr bool
	}{
		{
			name:   "numbered group 1",
			input:  "123-abc",
			config: `{"pattern":"(\\d+)-(\\w+)","group":1}`,
			want:   "123",
		},
		{
			name:   "numbered group 2",
			input:  "123-abc",
			config: `{"pattern":"(\\d+)-(\\w+)","group":2}`,
			want:   "abc",
		},
		{
			name:    "fractional numeric group returns error",
			input:   "123-abc",
			config:  `{"pattern":"(\\d+)-(\\w+)","group":1.5}`,
			wantErr: true,
		},
		{
			name:   "full match group 0",
			input:  "123-abc",
			config: `{"pattern":"(\\d+)-(\\w+)","group":0}`,
			want:   "123-abc",
		},
		{
			name:   "named group",
			input:  "user: alice, age: 30",
			config: `{"pattern":"user: (?P<name>\\w+)","group":"name"}`,
			want:   "alice",
		},
		{
			name:    "no match returns error",
			input:   "no digits here",
			config:  `{"pattern":"(\\d+)","group":1}`,
			wantErr: true,
		},
		{
			name:    "invalid regex pattern returns error",
			input:   "test",
			config:  `{"pattern":"[invalid","group":1}`,
			wantErr: true,
		},
		{
			name:    "non-string input returns error",
			input:   42,
			config:  `{"pattern":"(\\d+)","group":1}`,
			wantErr: true,
		},
		{
			name:    "invalid config JSON returns error",
			input:   "test",
			config:  `not json`,
			wantErr: true,
		},
		{
			name:    "missing pattern returns error",
			input:   "test",
			config:  `{"group":1}`,
			wantErr: true,
		},
		{
			name:   "missing group defaults to 0",
			input:  "123-abc",
			config: `{"pattern":"(\\d+)-(\\w+)"}`,
			want:   "123-abc",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := regexExtractTransform(context.Background(), tc.input, tc.config)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out != tc.want {
				t.Errorf("regex_extract: got %v (%T), want %v (%T)", out, out, tc.want, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Template tests (VAL-M2-020)
// ---------------------------------------------------------------------------

func TestTemplate(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		config  string
		want    string
		wantErr bool
	}{
		{
			name:   "simple field rendering",
			input:  map[string]any{"output": "hello"},
			config: `{"template":"Result: {{.output}}"}`,
			want:   "Result: hello",
		},
		{
			name:   "len function on array",
			input:  map[string]any{"items": []any{1, 2, 3}},
			config: `{"template":"{{.items | len}} items"}`,
			want:   "3 items",
		},
		{
			name:   "nested field",
			input:  map[string]any{"a": map[string]any{"b": "deep"}},
			config: `{"template":"value: {{.a.b}}"}`,
			want:   "value: deep",
		},
		{
			name:    "invalid template syntax returns error",
			input:   map[string]any{"key": "val"},
			config:  `{"template":"{{.key"}`,
			wantErr: true,
		},
		{
			name:   "nil input",
			input:  nil,
			config: `{"template":"empty"}`,
			want:   "empty",
		},
		{
			name:    "missing field returns error",
			input:   map[string]any{"key": "val"},
			config:  `{"template":"{{.nonexistent}}"}`,
			wantErr: true,
		},
		{
			name:    "invalid config JSON returns error",
			input:   "test",
			config:  `not json`,
			wantErr: true,
		},
		{
			name:    "missing template in config returns error",
			input:   "test",
			config:  `{}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := templateTransform(context.Background(), tc.input, tc.config)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got, ok := out.(string)
			if !ok {
				t.Fatalf("template: got %T, want string", out)
			}
			if got != tc.want {
				t.Errorf("template: got %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// JQFilter tests (VAL-M2-021, VAL-M2-047, VAL-M2-048)
// ---------------------------------------------------------------------------

func TestJQFilter(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		config  string
		want    any
		wantErr bool
	}{
		// Basic .field
		{
			name:   "dot field",
			input:  map[string]any{"name": "test", "other": 1.0},
			config: `{"filter":".name"}`,
			want:   "test",
		},
		// .array[] extracts array elements
		{
			name:   "array iterator",
			input:  map[string]any{"items": []any{1.0, 2.0, 3.0}},
			config: `{"filter":".items[]"}`,
			want:   []any{1.0, 2.0, 3.0},
		},
		// Nested .a.b.c
		{
			name: "nested dot path",
			input: map[string]any{
				"a": map[string]any{
					"b": map[string]any{
						"c": "deep",
					},
				},
			},
			config: `{"filter":".a.b.c"}`,
			want:   "deep",
		},
		// .[] on root array
		{
			name:   "root array iterator",
			input:  []any{"x", "y", "z"},
			config: `{"filter":".[]"}`,
			want:   []any{"x", "y", "z"},
		},
		// .field on non-object returns error
		{
			name:    "field on non-object error",
			input:   "just a string",
			config:  `{"filter":".name"}`,
			wantErr: true,
		},
		// .field on array returns error
		{
			name:    "field on array error",
			input:   []any{1, 2, 3},
			config:  `{"filter":".name"}`,
			wantErr: true,
		},
		// Missing field returns error
		{
			name:    "missing field returns error",
			input:   map[string]any{"name": "test"},
			config:  `{"filter":".nonexistent"}`,
			wantErr: true,
		},
		// Invalid filter expression returns error
		{
			name:    "invalid filter expression",
			input:   map[string]any{"name": "test"},
			config:  `{"filter":"..invalid"}`,
			wantErr: true,
		},
		// Invalid config JSON returns error
		{
			name:    "invalid config JSON",
			input:   map[string]any{"name": "test"},
			config:  `not json`,
			wantErr: true,
		},
		// Missing filter in config returns error
		{
			name:    "missing filter in config",
			input:   map[string]any{"name": "test"},
			config:  `{}`,
			wantErr: true,
		},
		// .[] | .field — extract field from each element in array
		{
			name:   "array iterator then field",
			input:  []any{map[string]any{"file": "a.go"}, map[string]any{"file": "b.go"}},
			config: `{"filter":".[].file"}`,
			want:   []any{"a.go", "b.go"},
		},
		// Nested array access: .items[] then nested field
		{
			name: "nested array iterator with field",
			input: map[string]any{
				"items": []any{
					map[string]any{"name": "first", "val": 1.0},
					map[string]any{"name": "second", "val": 2.0},
				},
			},
			config: `{"filter":".items[].name"}`,
			want:   []any{"first", "second"},
		},
		// Non-object input for jq_filter
		{
			name:    "number input returns error",
			input:   42,
			config:  `{"filter":".name"}`,
			wantErr: true,
		},
		// Null input
		{
			name:    "nil input with field filter returns error",
			input:   nil,
			config:  `{"filter":".name"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := jqFilterTransform(context.Background(), tc.input, tc.config)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertEqualJSON(t, out, tc.want)
		})
	}
}

// ---------------------------------------------------------------------------
// SplitLines tests (VAL-M2-022)
// ---------------------------------------------------------------------------

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		config  string
		want    []string
		wantErr bool
	}{
		{
			name:   "newline delimiter",
			input:  "line1\nline2\nline3",
			config: `{"delimiter":"\n"}`,
			want:   []string{"line1", "line2", "line3"},
		},
		{
			name:   "comma delimiter",
			input:  "a,b,c",
			config: `{"delimiter":","}`,
			want:   []string{"a", "b", "c"},
		},
		{
			name:   "empty string returns empty array",
			input:  "",
			config: `{"delimiter":"\n"}`,
			want:   []string{},
		},
		{
			name:   "single element",
			input:  "only",
			config: `{"delimiter":"\n"}`,
			want:   []string{"only"},
		},
		{
			name:   "tab delimiter",
			input:  "a\tb\tc",
			config: `{"delimiter":"\t"}`,
			want:   []string{"a", "b", "c"},
		},
		{
			name:    "non-string input returns error",
			input:   42,
			config:  `{"delimiter":","}`,
			wantErr: true,
		},
		{
			name:    "invalid config JSON returns error",
			input:   "test",
			config:  `not json`,
			wantErr: true,
		},
		{
			name:   "missing delimiter defaults to newline",
			input:  "a\nb",
			config: `{}`,
			want:   []string{"a", "b"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := splitLinesTransform(context.Background(), tc.input, tc.config)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got, ok := out.([]string)
			if !ok {
				t.Fatalf("split_lines: got %T, want []string", out)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("split_lines: got %d elements, want %d", len(got), len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("split_lines[%d]: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MapFields tests (VAL-M2-023)
// ---------------------------------------------------------------------------

func TestMapFields(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		config  string
		want    map[string]any
		wantErr bool
	}{
		{
			name:   "rename fields unmapped pass through",
			input:  map[string]any{"old_name": "value", "other": "kept"},
			config: `{"mapping":{"old_name":"new_name"}}`,
			want:   map[string]any{"new_name": "value", "other": "kept"},
		},
		{
			name:   "multiple renames",
			input:  map[string]any{"a": 1.0, "b": 2.0, "c": 3.0},
			config: `{"mapping":{"a":"alpha","b":"beta"}}`,
			want:   map[string]any{"alpha": 1.0, "beta": 2.0, "c": 3.0},
		},
		{
			name:   "mapping to existing field overwrites",
			input:  map[string]any{"input": "query", "query": "original"},
			config: `{"mapping":{"input":"query"}}`,
			want:   map[string]any{"query": "query"},
		},
		{
			name:   "empty mapping returns same object",
			input:  map[string]any{"key": "value"},
			config: `{"mapping":{}}`,
			want:   map[string]any{"key": "value"},
		},
		{
			name:    "non-object input returns error",
			input:   "just a string",
			config:  `{"mapping":{"a":"b"}}`,
			wantErr: true,
		},
		{
			name:    "array input returns error",
			input:   []any{1, 2, 3},
			config:  `{"mapping":{"a":"b"}}`,
			wantErr: true,
		},
		{
			name:    "nil input returns error",
			input:   nil,
			config:  `{"mapping":{"a":"b"}}`,
			wantErr: true,
		},
		{
			name:    "invalid config JSON returns error",
			input:   map[string]any{"key": "value"},
			config:  `not json`,
			wantErr: true,
		},
		{
			name:    "missing mapping in config returns error",
			input:   map[string]any{"key": "value"},
			config:  `{}`,
			wantErr: true,
		},
		{
			name:   "nested data unchanged",
			input:  map[string]any{"outer": map[string]any{"inner": "deep"}},
			config: `{"mapping":{"outer":"envelope"}}`,
			want:   map[string]any{"envelope": map[string]any{"inner": "deep"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := mapFieldsTransform(context.Background(), tc.input, tc.config)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got, ok := out.(map[string]any)
			if !ok {
				t.Fatalf("map_fields: got %T, want map[string]any", out)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("map_fields: got %d keys, want %d keys", len(got), len(tc.want))
			}
			for k, wantV := range tc.want {
				gotV, exists := got[k]
				if !exists {
					t.Errorf("map_fields: missing key %q", k)
					continue
				}
				assertEqualJSON(t, gotV, wantV)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Transform Registry tests (VAL-M2-024)
// ---------------------------------------------------------------------------

func TestTransformRegistry(t *testing.T) {
	// Verify all TransformKind values are registered.
	for _, kind := range ValidTransformKinds {
		t.Run(string(kind), func(t *testing.T) {
			fn, err := GetTransform(kind)
			if err != nil {
				t.Fatalf("GetTransform(%q): unexpected error: %v", kind, err)
			}
			if fn == nil {
				t.Fatalf("GetTransform(%q): got nil function", kind)
			}
		})
	}

	// Verify unknown kind returns error.
	t.Run("unknown kind", func(t *testing.T) {
		_, err := GetTransform(TransformKind("unknown"))
		if err == nil {
			t.Fatal("expected error for unknown transform kind, got nil")
		}
	})
}

func TestApplyTransform(t *testing.T) {
	t.Run("applies passthrough via registry", func(t *testing.T) {
		input := map[string]any{"key": "value"}
		out, err := ApplyTransform(context.Background(), TransformPassthrough, "", input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertEqualJSON(t, out, input)
	})

	t.Run("applies split_lines via registry", func(t *testing.T) {
		out, err := ApplyTransform(context.Background(), TransformSplitLines, `{"delimiter":","}`, "a,b,c")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, ok := out.([]string)
		if !ok {
			t.Fatalf("got %T, want []string", out)
		}
		if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
			t.Errorf("split_lines via ApplyTransform: got %v, want [a b c]", got)
		}
	})

	t.Run("unknown kind returns error", func(t *testing.T) {
		_, err := ApplyTransform(context.Background(), TransformKind("unknown"), "", "test")
		if err == nil {
			t.Fatal("expected error for unknown transform kind")
		}
	})
}

// ---------------------------------------------------------------------------
// Transform error produces error envelope test (VAL-M2-024)
// ---------------------------------------------------------------------------

func TestTransformErrorProducesGoError(t *testing.T) {
	// Verifies that transform errors are Go errors (not panics).
	// The caller wraps them in error envelopes.

	t.Run("regex no match is error not panic", func(t *testing.T) {
		_, err := regexExtractTransform(context.Background(), "no digits", `{"pattern":"(\\d+)","group":1}`)
		if err == nil {
			t.Fatal("expected error for no match")
		}
	})

	t.Run("invalid jq path is error not panic", func(t *testing.T) {
		_, err := jqFilterTransform(context.Background(), map[string]any{"a": 1.0}, `{"filter":"..invalid"}`)
		if err == nil {
			t.Fatal("expected error for invalid path")
		}
	})

	t.Run("unsupported jq recursive descent is rejected even when field exists", func(t *testing.T) {
		_, err := jqFilterTransform(context.Background(), map[string]any{"invalid": "present"}, `{"filter":"..invalid"}`)
		if err == nil {
			t.Fatal("expected recursive descent path to be rejected")
		}
	})

	t.Run("template bad syntax is error not panic", func(t *testing.T) {
		_, err := templateTransform(context.Background(), map[string]any{"a": 1.0}, `{"template":"{{.a"}`)
		if err == nil {
			t.Fatal("expected error for invalid template")
		}
	})

	t.Run("map_fields on non-object is error not panic", func(t *testing.T) {
		_, err := mapFieldsTransform(context.Background(), "string", `{"mapping":{"a":"b"}}`)
		if err == nil {
			t.Fatal("expected error for non-object input")
		}
	})

	t.Run("split_lines on non-string is error not panic", func(t *testing.T) {
		_, err := splitLinesTransform(context.Background(), 42, `{"delimiter":","}`)
		if err == nil {
			t.Fatal("expected error for non-string input")
		}
	})
}

func TestJQFilterPropertyRejectsEmptyPathSegments(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(leftRaw, rightRaw uint8) bool {
		left := fmt.Sprintf("a%d", leftRaw%32)
		right := fmt.Sprintf("b%d", rightRaw%32)
		filters := []string{
			"." + left + ".." + right,
			".." + right,
			"." + left + ".",
		}
		for _, filter := range filters {
			if _, err := parseJQPath(filter); err == nil {
				return false
			}
		}
		return true
	}, cfg)
	if err != nil {
		t.Fatalf("empty jq path segment property failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Transform non-object input handling (VAL-M2-047)
// ---------------------------------------------------------------------------

func TestTransformNonObjectInput(t *testing.T) {
	t.Run("jq_filter on number returns error", func(t *testing.T) {
		_, err := jqFilterTransform(context.Background(), 42, `{"filter":".name"}`)
		if err == nil {
			t.Fatal("expected error for number input to jq_filter")
		}
	})

	t.Run("template on nil input renders with nil data", func(t *testing.T) {
		out, err := templateTransform(context.Background(), nil, `{"template":"empty"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "empty" {
			t.Errorf("got %q, want %q", out, "empty")
		}
	})

	t.Run("map_fields on non-object returns error", func(t *testing.T) {
		_, err := mapFieldsTransform(context.Background(), "string", `{"mapping":{"a":"b"}}`)
		if err == nil {
			t.Fatal("expected error for non-object input to map_fields")
		}
	})
}

// ---------------------------------------------------------------------------
// Nested data in transforms (VAL-M2-048)
// ---------------------------------------------------------------------------

func TestNestedDataTransforms(t *testing.T) {
	t.Run("jq_filter navigates nested .a.b.c", func(t *testing.T) {
		input := map[string]any{
			"a": map[string]any{
				"b": map[string]any{
					"c": "deep_value",
				},
			},
		}
		out, err := jqFilterTransform(context.Background(), input, `{"filter":".a.b.c"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "deep_value" {
			t.Errorf("got %v, want %q", out, "deep_value")
		}
	})

	t.Run("template renders nested field values", func(t *testing.T) {
		input := map[string]any{
			"a": map[string]any{
				"b": "deep",
			},
		}
		out, err := templateTransform(context.Background(), input, `{"template":"value: {{.a.b}}"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "value: deep" {
			t.Errorf("got %q, want %q", out, "value: deep")
		}
	})

	t.Run("map_fields only remaps top-level fields", func(t *testing.T) {
		input := map[string]any{
			"outer": map[string]any{"inner": "deep"},
		}
		out, err := mapFieldsTransform(context.Background(), input, `{"mapping":{"outer":"wrapper"}}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := out.(map[string]any)
		wrapper, ok := got["wrapper"]
		if !ok {
			t.Fatal("expected 'wrapper' key in output")
		}
		inner := wrapper.(map[string]any)
		if inner["inner"] != "deep" {
			t.Errorf("nested data changed: got %v, want %q", inner["inner"], "deep")
		}
	})
}

// ---------------------------------------------------------------------------
// FileWrite tests (VAL-FILE-001 through VAL-FILE-005)
// ---------------------------------------------------------------------------

func TestFileWrite(t *testing.T) {
	t.Run("raw format writes data to file", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "output.txt")
		config := fmt.Sprintf(`{"path":%q,"format":"raw"}`, path)

		input := map[string]any{"topic": "test", "value": float64(42)}
		out, err := fileWriteTransform(context.Background(), input, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Check result contains expected fields.
		result, ok := out.(fileWriteResult)
		if !ok {
			t.Fatalf("got %T, want fileWriteResult", out)
		}
		if result.Path != path {
			t.Errorf("path: got %q, want %q", result.Path, path)
		}
		if result.Format != "raw" {
			t.Errorf("format: got %q, want %q", result.Format, "raw")
		}
		if result.Bytes == 0 {
			t.Error("bytes should be > 0")
		}

		// Verify file exists and has content.
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read output file: %v", err)
		}
		if len(data) == 0 {
			t.Error("output file is empty")
		}

		// Raw format with object input should be JSON.
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("raw format output should be valid JSON: %v", err)
		}
		if parsed["topic"] != "test" {
			t.Errorf("topic: got %v, want %q", parsed["topic"], "test")
		}
	})

	t.Run("raw format with string input", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "output.txt")
		config := fmt.Sprintf(`{"path":%q}`, path)

		out, err := fileWriteTransform(context.Background(), "hello world", config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read output file: %v", err)
		}
		if string(data) != "hello world" {
			t.Errorf("got %q, want %q", string(data), "hello world")
		}
		result := out.(fileWriteResult)
		if result.Format != "raw" {
			t.Errorf("default format: got %q, want %q", result.Format, "raw")
		}
	})

	t.Run("json format writes pretty-printed JSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "output.json")
		config := fmt.Sprintf(`{"path":%q,"format":"json"}`, path)

		input := map[string]any{"name": "test", "count": float64(5)}
		out, err := fileWriteTransform(context.Background(), input, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read output file: %v", err)
		}

		// JSON format should produce valid JSON with indentation.
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("json format output should be valid JSON: %v", err)
		}
		if parsed["name"] != "test" {
			t.Errorf("name: got %v, want %q", parsed["name"], "test")
		}
		// Verify it's pretty-printed (contains newlines).
		if !strings.Contains(string(data), "\n") {
			t.Error("json format should produce pretty-printed output with newlines")
		}

		result := out.(fileWriteResult)
		if result.Format != "json" {
			t.Errorf("format: got %q, want %q", result.Format, "json")
		}
	})

	t.Run("markdown format wraps in headers and bullets", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "output.md")
		config := fmt.Sprintf(`{"path":%q,"format":"markdown"}`, path)

		input := map[string]any{
			"title": "My Report",
			"items": []any{"alpha", "beta"},
		}
		out, err := fileWriteTransform(context.Background(), input, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read output file: %v", err)
		}

		content := string(data)

		// Markdown format should contain header.
		if !strings.Contains(content, "# Flow Output") {
			t.Error("markdown format should contain '# Flow Output' header")
		}

		// Should contain bullet points for map keys.
		if !strings.Contains(content, "- **title**: My Report") {
			t.Error("markdown format should contain bullet point for 'title' key")
		}

		// Should contain bullet point for items array (rendered as comma-separated).
		if !strings.Contains(content, "- **items**: alpha, beta") {
			t.Errorf("markdown format should contain bullet point for 'items' key; got:\n%s", content)
		}

		result := out.(fileWriteResult)
		if result.Format != "markdown" {
			t.Errorf("format: got %q, want %q", result.Format, "markdown")
		}
	})

	t.Run("creates parent directories automatically", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "deep", "nested", "dir", "output.txt")
		config := fmt.Sprintf(`{"path":%q}`, path)

		out, err := fileWriteTransform(context.Background(), "nested content", config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify file was created at the nested path.
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read output file at nested path: %v", err)
		}
		if string(data) != "nested content" {
			t.Errorf("got %q, want %q", string(data), "nested content")
		}

		result := out.(fileWriteResult)
		if result.Path != path {
			t.Errorf("path: got %q, want %q", result.Path, path)
		}
	})

	t.Run("template support in path from envelope data", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Template uses {{.topic}} from the input data.
		config := fmt.Sprintf(`{"path":%q}`, filepath.Join(tmpDir, "{{.topic}}-report.txt"))

		input := map[string]any{
			"topic": "golang",
			"data":  "some content",
		}
		out, err := fileWriteTransform(context.Background(), input, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedPath := filepath.Join(tmpDir, "golang-report.txt")
		result := out.(fileWriteResult)
		if result.Path != expectedPath {
			t.Errorf("resolved path: got %q, want %q", result.Path, expectedPath)
		}

		// Verify file exists at the interpolated path.
		if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
			t.Errorf("file not created at expected path %q", expectedPath)
		}
	})

	t.Run("nested template in path", func(t *testing.T) {
		tmpDir := t.TempDir()
		config := fmt.Sprintf(`{"path":%q}`, filepath.Join(tmpDir, "{{.category.name}}-output.txt"))

		input := map[string]any{
			"category": map[string]any{
				"name": "research",
			},
		}
		out, err := fileWriteTransform(context.Background(), input, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedPath := filepath.Join(tmpDir, "research-output.txt")
		result := out.(fileWriteResult)
		if result.Path != expectedPath {
			t.Errorf("resolved path: got %q, want %q", result.Path, expectedPath)
		}
	})

	t.Run("missing path returns error", func(t *testing.T) {
		_, err := fileWriteTransform(context.Background(), "test", `{"format":"raw"}`)
		if err == nil {
			t.Fatal("expected error for missing path, got nil")
		}
		if !strings.Contains(err.Error(), "path is required") {
			t.Errorf("error should mention path is required, got: %v", err)
		}
	})

	t.Run("empty path returns error", func(t *testing.T) {
		_, err := fileWriteTransform(context.Background(), "test", `{"path":""}`)
		if err == nil {
			t.Fatal("expected error for empty path, got nil")
		}
	})

	t.Run("invalid format returns error", func(t *testing.T) {
		_, err := fileWriteTransform(context.Background(), "test", `{"path":"/tmp/test.txt","format":"xml"}`)
		if err == nil {
			t.Fatal("expected error for invalid format, got nil")
		}
		if !strings.Contains(err.Error(), "invalid format") {
			t.Errorf("error should mention invalid format, got: %v", err)
		}
	})

	t.Run("invalid config JSON returns error", func(t *testing.T) {
		_, err := fileWriteTransform(context.Background(), "test", `not json`)
		if err == nil {
			t.Fatal("expected error for invalid config JSON")
		}
	})

	t.Run("result contains summary and bytes", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "output.txt")
		config := fmt.Sprintf(`{"path":%q}`, path)

		out, err := fileWriteTransform(context.Background(), "hello", config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		result := out.(fileWriteResult)
		if result.Summary == "" {
			t.Error("summary should not be empty")
		}
		if !strings.Contains(result.Summary, "Wrote") {
			t.Errorf("summary should contain 'Wrote', got: %q", result.Summary)
		}
		if !strings.Contains(result.Summary, path) {
			t.Errorf("summary should contain path, got: %q", result.Summary)
		}
		if result.Bytes != 5 { // "hello" is 5 bytes
			t.Errorf("bytes: got %d, want 5", result.Bytes)
		}
	})

	t.Run("nil input writes empty raw", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "output.txt")
		config := fmt.Sprintf(`{"path":%q}`, path)

		out, err := fileWriteTransform(context.Background(), nil, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read output file: %v", err)
		}
		if len(data) != 0 {
			t.Errorf("nil input should produce empty file, got %d bytes", len(data))
		}

		result := out.(fileWriteResult)
		if result.Bytes != 0 {
			t.Errorf("bytes for nil: got %d, want 0", result.Bytes)
		}
	})

	t.Run("markdown with array input", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "output.md")
		config := fmt.Sprintf(`{"path":%q,"format":"markdown"}`, path)

		input := []any{"alpha", "beta", "gamma"}
		_, err := fileWriteTransform(context.Background(), input, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read output file: %v", err)
		}

		content := string(data)
		if !strings.Contains(content, "- alpha") {
			t.Error("markdown array output should contain '- alpha'")
		}
		if !strings.Contains(content, "- beta") {
			t.Error("markdown array output should contain '- beta'")
		}
		if !strings.Contains(content, "- gamma") {
			t.Error("markdown array output should contain '- gamma'")
		}
	})

	t.Run("markdown with nested map", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "output.md")
		config := fmt.Sprintf(`{"path":%q,"format":"markdown"}`, path)

		input := map[string]any{
			"parent": map[string]any{
				"child1": "value1",
				"child2": float64(42),
			},
		}
		_, err := fileWriteTransform(context.Background(), input, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read output file: %v", err)
		}

		content := string(data)
		if !strings.Contains(content, "- **parent**:") {
			t.Error("markdown should contain parent header")
		}
		if !strings.Contains(content, "- **child1**: value1") {
			t.Error("markdown should contain child1 bullet")
		}
		if !strings.Contains(content, "- **child2**: 42") {
			t.Error("markdown should contain child2 bullet")
		}
	})

	t.Run("template in path with non-map input returns original path", func(t *testing.T) {
		// Non-map input can't resolve templates, but since the path doesn't
		// actually contain template expressions in this test, it should just work.
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "simple.txt")
		config := fmt.Sprintf(`{"path":%q}`, path)

		out, err := fileWriteTransform(context.Background(), "string input", config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		result := out.(fileWriteResult)
		if result.Path != path {
			t.Errorf("path: got %q, want %q", result.Path, path)
		}
	})

	t.Run("unresolved path template with non-object input fails before writing", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "{{.topic}}.txt")
		config := fmt.Sprintf(`{"path":%q}`, path)

		_, err := fileWriteTransform(context.Background(), "string input", config)
		if err == nil {
			t.Fatal("expected unresolved path template to fail")
		}

		matches, globErr := filepath.Glob(filepath.Join(tmpDir, "*"))
		if globErr != nil {
			t.Fatalf("glob temp dir: %v", globErr)
		}
		if len(matches) != 0 {
			t.Fatalf("unresolved path template wrote files: %v", matches)
		}
	})

	t.Run("registered in transform registry", func(t *testing.T) {
		fn, err := GetTransform(TransformFileWrite)
		if err != nil {
			t.Fatalf("file_write not registered: %v", err)
		}
		if fn == nil {
			t.Fatal("file_write transform function is nil")
		}
	})

	t.Run("applies via ApplyTransform", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "via-apply.txt")
		config := fmt.Sprintf(`{"path":%q}`, path)

		out, err := ApplyTransform(context.Background(), TransformFileWrite, config, "test content")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		result, ok := out.(fileWriteResult)
		if !ok {
			t.Fatalf("got %T, want fileWriteResult", out)
		}
		if result.Path != path {
			t.Errorf("path: got %q, want %q", result.Path, path)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read output file: %v", err)
		}
		if string(data) != "test content" {
			t.Errorf("got %q, want %q", string(data), "test content")
		}
	})
}

func TestFileWriteConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  FileWriteConfig
		wantErr bool
		errMsg  string
	}{
		{"valid raw", FileWriteConfig{Path: "/tmp/test.txt", Format: "raw"}, false, ""},
		{"valid json", FileWriteConfig{Path: "/tmp/test.json", Format: "json"}, false, ""},
		{"valid markdown", FileWriteConfig{Path: "/tmp/test.md", Format: "markdown"}, false, ""},
		{"valid empty format defaults to raw", FileWriteConfig{Path: "/tmp/test.txt"}, false, ""},
		{"missing path", FileWriteConfig{Format: "raw"}, true, "path is required"},
		{"empty path", FileWriteConfig{Path: "", Format: "raw"}, true, "path is required"},
		{"invalid format", FileWriteConfig{Path: "/tmp/test.txt", Format: "xml"}, true, "invalid format"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tc.errMsg != "" && !strings.Contains(err.Error(), tc.errMsg) {
					t.Errorf("error should contain %q, got: %v", tc.errMsg, err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// assertEqualJSON compares two values by serializing to JSON.
// This handles the case where []any{1,2,3} should equal []any{1.0,2.0,3.0}
// since JSON numbers unmarshal as float64.
func assertEqualJSON(t *testing.T, got, want any) {
	t.Helper()
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("failed to marshal got: %v", err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("failed to marshal want: %v", err)
	}
	if string(gotJSON) != string(wantJSON) {
		// Try a more lenient comparison: unmarshal both to generic and re-compare.
		var gotVal, wantVal any
		if err := json.Unmarshal(gotJSON, &gotVal); err != nil {
			t.Fatalf("failed to unmarshal got: %v", err)
		}
		if err := json.Unmarshal(wantJSON, &wantVal); err != nil {
			t.Fatalf("failed to unmarshal want: %v", err)
		}
		gotReJSON, _ := json.Marshal(gotVal)
		wantReJSON, _ := json.Marshal(wantVal)
		if string(gotReJSON) != string(wantReJSON) {
			t.Errorf("values differ:\ngot:  %s\nwant: %s", gotReJSON, wantReJSON)
		}
	}
}

// This is a compile-time check that all test helper functions are used.
var (
	_ = fmt.Sprintf
	_ = strings.Contains
)
