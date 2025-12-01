package postreview

import (
	"encoding/json"
	"testing"
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
