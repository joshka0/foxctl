package langutil

import "testing"

func TestDetectAllowedWithHintRequiresMatchingExtension(t *testing.T) {
	if got := DetectAllowedWithHint("elixir", "file.go", CommonCodeLanguages); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
	if got := DetectAllowedWithHint("go", "file.go", CommonCodeLanguages); got != "go" {
		t.Fatalf("expected go, got %q", got)
	}
	if got := DetectAllowedWithHint("typescript", "file.tsx", CommonCodeLanguages); got != "typescript" {
		t.Fatalf("expected typescript, got %q", got)
	}
}
