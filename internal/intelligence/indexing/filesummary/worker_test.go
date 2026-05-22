package filesummary

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkerBuildInputRejectsSymlinkEscapingWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	workspaceDir := filepath.Join(tmpDir, "workspace")
	outsideDir := filepath.Join(tmpDir, "outside")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outsideDir, "secret.go")
	if err := os.WriteFile(outsideFile, []byte("package secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(workspaceDir, "link.go")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	worker := &Worker{workspace: workspaceDir}
	_, err := worker.buildInput("link.go")
	if err == nil {
		t.Fatal("expected escaping symlink to be rejected")
	}
	if !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("error=%q want path escape rejection", err)
	}
}
