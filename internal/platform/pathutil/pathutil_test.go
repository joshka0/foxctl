package pathutil

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestExtractPathFromInputUsesFieldPrecedence(t *testing.T) {
	input := ToolPathInput{
		FilePath:    "/first.go",
		Path:        "/second.go",
		File:        "/third.go",
		CurrentPath: "/fourth.go",
	}

	if got := ExtractPathFromInput(input); got != "/first.go" {
		t.Fatalf("ExtractPathFromInput() = %q, want /first.go", got)
	}
}

func TestDecodeToolPathInputToleratesMixedArrays(t *testing.T) {
	raw := json.RawMessage(`{
		"file_path": "/root.go",
		"edits": [
			{"path": "/a.go"},
			42,
			{"file": "/b.go"},
			{"ignored": "value"}
		],
		"files": ["/c.go", 17, "", "/d.go"]
	}`)

	input, ok := DecodeToolPathInput(raw)
	if !ok {
		t.Fatal("DecodeToolPathInput returned !ok")
	}

	want := []string{"/a.go", "/b.go", "/c.go", "/d.go", "/root.go"}
	if got := ExtractPathsFromInput(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtractPathsFromInput() = %#v, want %#v", got, want)
	}
}
