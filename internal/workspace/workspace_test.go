package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectPrefersAgentctlDirectory(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "nested", "project")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	marker := filepath.Join(root, "nested", ".agentctl")
	if err := os.Mkdir(marker, 0o755); err != nil {
		t.Fatalf("mkdir marker: %v", err)
	}

	if got := Detect(child); got != filepath.Join(root, "nested") {
		t.Fatalf("expected workspace %s, got %s", filepath.Join(root, "nested"), got)
	}
}

func TestDetectFallsBackToStart(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "sandbox")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if got := Detect(child); got != child {
		t.Fatalf("expected %s, got %s", child, got)
	}
}
