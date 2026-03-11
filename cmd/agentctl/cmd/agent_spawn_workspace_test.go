package cmd

import (
	"os"
	"path/filepath"
	"testing"

	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
)

func TestCurrentSpawnWorkspaceRootAndID(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(orig)
	})

	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	gotRoot := currentSpawnWorkspaceRoot()
	wantRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd after chdir: %v", err)
	}
	if gotRoot != wantRoot {
		t.Fatalf("workspace root=%q want %q", gotRoot, wantRoot)
	}

	gotID := currentSpawnWorkspaceID()
	wantID := ws.ID(wantRoot)
	if gotID != wantID {
		t.Fatalf("workspace id=%q want %q", gotID, wantID)
	}

	override := filepath.Join(tmp, "nested")
	if err := os.MkdirAll(override, 0o755); err != nil {
		t.Fatalf("mkdir override: %v", err)
	}
	spawnWorkspace = override
	t.Cleanup(func() { spawnWorkspace = "" })

	gotOverride := currentSpawnWorkspaceRoot()
	wantOverride, err := filepath.Abs(override)
	if err != nil {
		t.Fatalf("abs override: %v", err)
	}
	if gotOverride != wantOverride {
		t.Fatalf("workspace root override=%q want %q", gotOverride, wantOverride)
	}
}
