package postreview

import (
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/indexing"
)

func TestUnmarshalFiles_EmptyReturnsNonNilSlice(t *testing.T) {
	tests := []string{"", "[]"}

	for _, input := range tests {
		files, err := unmarshalFiles(input)
		if err != nil {
			t.Fatalf("unmarshalFiles(%q) error: %v", input, err)
		}
		if files == nil {
			t.Fatalf("unmarshalFiles(%q) returned nil slice", input)
		}
		if len(files) != 0 {
			t.Fatalf("unmarshalFiles(%q) expected empty slice, got len=%d", input, len(files))
		}

		b, err := json.Marshal(files)
		if err != nil {
			t.Fatalf("marshal files(%q) error: %v", input, err)
		}
		if string(b) != "[]" {
			t.Fatalf("marshal files(%q) expected '[]', got %q", input, string(b))
		}
	}
}

func TestUnmarshalFiles_ValidNonEmpty(t *testing.T) {
	input := `[
		{"path":"a.go","digest":"sha256:abc","size_bytes":123,"language":"go","change_kind":"modified"},
		{"path":"b.go","change_kind":"deleted"}
	]`

	files, err := unmarshalFiles(input)
	if err != nil {
		t.Fatalf("unmarshalFiles valid input error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("unmarshalFiles valid input expected 2 files, got %d", len(files))
	}

	if files[0].Path != "a.go" || files[0].Digest != "sha256:abc" || files[0].SizeBytes != 123 || files[0].Language != "go" {
		t.Fatalf("unmarshalFiles first element mismatch: %+v", files[0])
	}
	if files[0].ChangeKind != indexing.ChangeKindModified {
		t.Fatalf("unmarshalFiles first element ChangeKind = %q, want %q", files[0].ChangeKind, indexing.ChangeKindModified)
	}
	if files[1].Path != "b.go" {
		t.Fatalf("unmarshalFiles second element Path = %q, want %q", files[1].Path, "b.go")
	}
	if files[1].ChangeKind != indexing.ChangeKindDeleted {
		t.Fatalf("unmarshalFiles second element ChangeKind = %q, want %q", files[1].ChangeKind, indexing.ChangeKindDeleted)
	}

	out, err := marshalFiles(files)
	if err != nil {
		t.Fatalf("marshalFiles round-trip error: %v", err)
	}
	var round []indexing.FileChange
	if err := json.Unmarshal([]byte(out), &round); err != nil {
		t.Fatalf("json.Unmarshal round-trip error: %v", err)
	}
	if len(round) != len(files) {
		t.Fatalf("round-trip expected %d files, got %d", len(files), len(round))
	}
	for i := range files {
		if round[i].Path != files[i].Path || round[i].ChangeKind != files[i].ChangeKind {
			t.Fatalf("round-trip element %d mismatch: got %+v, want %+v", i, round[i], files[i])
		}
	}
}

func TestUnmarshalFiles_InvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"not-json", "not json"},
		{"truncated", "["},
		{"object-instead-of-array", `{"path":"a.go"}`},
		{"wrong-types", `[{"path":1}]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := unmarshalFiles(tt.input)
			if err == nil {
				t.Fatalf("unmarshalFiles(%s) expected error, got nil (files len=%d)", tt.name, len(files))
			}
		})
	}
}

func TestUnmarshalFiles_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantLen int
	}{
		{"null-literal", "null", 0},
		{"array-with-null", "[null]", 1},
		{"array-with-empty-object", "[{}]", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := unmarshalFiles(tt.input)
			if err != nil {
				t.Fatalf("unmarshalFiles(%s) unexpected error: %v", tt.name, err)
			}
			if files == nil {
				t.Fatalf("unmarshalFiles(%s) returned nil slice", tt.name)
			}
			if len(files) != tt.wantLen {
				t.Fatalf("unmarshalFiles(%s) len=%d, want %d", tt.name, len(files), tt.wantLen)
			}
		})
	}
}

func TestUnmarshalMetadata_EmptyReturnsNonNilMap(t *testing.T) {
	tests := []string{"", "{}"}

	for _, input := range tests {
		meta, err := unmarshalMetadata(input)
		if err != nil {
			t.Fatalf("unmarshalMetadata(%q) error: %v", input, err)
		}
		if meta == nil {
			t.Fatalf("unmarshalMetadata(%q) returned nil map", input)
		}
		if len(meta) != 0 {
			t.Fatalf("unmarshalMetadata(%q) expected empty map, got len=%d", input, len(meta))
		}

		b, err := json.Marshal(meta)
		if err != nil {
			t.Fatalf("marshal metadata(%q) error: %v", input, err)
		}
		if string(b) != "{}" {
			t.Fatalf("marshal metadata(%q) expected '{}', got %q", input, string(b))
		}
	}
}

func TestUnmarshalMetadata_ValidInput(t *testing.T) {
	input := `{"s":"value","n":42,"b":true,"nested":{"k":"v"},"arr":[1,2]}`

	meta, err := unmarshalMetadata(input)
	if err != nil {
		t.Fatalf("unmarshalMetadata valid input error: %v", err)
	}
	if len(meta) != 5 {
		t.Fatalf("unmarshalMetadata expected 5 keys, got %d", len(meta))
	}

	if v, ok := meta["s"].(string); !ok || v != "value" {
		t.Fatalf("metadata[\"s\"] = %#v, want string 'value'", meta["s"])
	}
	n, ok := meta["n"].(float64)
	if !ok || n != 42 {
		t.Fatalf("metadata[\"n\"] = %#v, want number 42", meta["n"])
	}
	if v, ok := meta["b"].(bool); !ok || !v {
		t.Fatalf("metadata[\"b\"] = %#v, want bool true", meta["b"])
	}

	nested, ok := meta["nested"].(map[string]any)
	if !ok {
		t.Fatalf("metadata[\"nested\"] has type %T, want map[string]any", meta["nested"])
	}
	if v, ok := nested["k"].(string); !ok || v != "v" {
		t.Fatalf("metadata[\"nested\"][\"k\"] = %#v, want string 'v'", nested["k"])
	}

	arr, ok := meta["arr"].([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("metadata[\"arr\"] = %#v, want []any of len 2", meta["arr"])
	}
	if v, ok := arr[0].(float64); !ok || v != 1 {
		t.Fatalf("metadata[\"arr\"][0] = %#v, want number 1", arr[0])
	}
	if v, ok := arr[1].(float64); !ok || v != 2 {
		t.Fatalf("metadata[\"arr\"][1] = %#v, want number 2", arr[1])
	}
}

func TestUnmarshalMetadata_InvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"not-json", "not json"},
		{"truncated", "{"},
		{"array-instead-of-object", "[]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := unmarshalMetadata(tt.input)
			if err == nil {
				t.Fatalf("unmarshalMetadata(%s) expected error, got nil (len=%d)", tt.name, len(meta))
			}
		})
	}
}

func TestUnmarshalMetadata_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantLen   int
		wantHasK  bool
		wantKType string
	}{
		{"null-literal-empty", "null", 0, false, ""},
		{"null-value", `{"k":null}`, 1, true, "nil"},
		{"nested-object", `{"k":{"n":1}}`, 1, true, "object"},
		{"array-value", `{"k":[1,2]}`, 1, true, "array"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := unmarshalMetadata(tt.input)
			if err != nil {
				t.Fatalf("unmarshalMetadata(%s) unexpected error: %v", tt.name, err)
			}
			if meta == nil {
				t.Fatalf("unmarshalMetadata(%s) returned nil map", tt.name)
			}
			if len(meta) != tt.wantLen {
				t.Fatalf("unmarshalMetadata(%s) len=%d, want %d", tt.name, len(meta), tt.wantLen)
			}
			v, ok := meta["k"]
			if tt.wantHasK && !ok {
				t.Fatalf("unmarshalMetadata(%s) expected key 'k'", tt.name)
			}
			if !tt.wantHasK && ok {
				t.Fatalf("unmarshalMetadata(%s) did not expect key 'k'", tt.name)
			}
			if !tt.wantHasK {
				return
			}
			switch tt.wantKType {
			case "nil":
				if v != nil {
					t.Fatalf("unmarshalMetadata(%s) expected k=nil, got %#v", tt.name, v)
				}
			case "object":
				if _, ok := v.(map[string]any); !ok {
					t.Fatalf("unmarshalMetadata(%s) expected k to be object, got %T", tt.name, v)
				}
			case "array":
				if _, ok := v.([]any); !ok {
					t.Fatalf("unmarshalMetadata(%s) expected k to be array, got %T", tt.name, v)
				}
			}
		})
	}
}
