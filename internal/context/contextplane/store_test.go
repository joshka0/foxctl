package contextplane

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceStoreEnsureLayoutAndSaveTopOfMind(t *testing.T) {
	workspace := t.TempDir()
	store := NewWorkspaceStore(workspace)

	layout, err := store.EnsureLayout()
	if err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	for _, path := range []string{
		layout.RuntimeDir,
		layout.QueueDir,
		layout.PolicyDir,
		layout.ExportsDir,
		filepath.Dir(layout.ObsidianHomeIndexPath),
		filepath.Dir(layout.ObsidianProjectMOCPath),
	} {
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Fatalf("expected directory %s to exist: err=%v", path, err)
		}
	}

	top := TopOfMind{
		WorkspaceID:     "foxctl",
		Objective:       "Formalize ACA",
		Phase:           "design",
		ActiveTaskIDs:   []string{"T-1042"},
		HardConstraints: []string{"Preserve provenance"},
		NextActions:     []string{"Scaffold runtime store"},
		UpdatedAt:       time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC),
	}
	if _, err := store.SaveTopOfMind(top); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}

	got, err := store.LoadTopOfMind()
	if err != nil {
		t.Fatalf("LoadTopOfMind: %v", err)
	}
	if got.Objective != top.Objective {
		t.Fatalf("objective=%q want %q", got.Objective, top.Objective)
	}

	if info, err := os.Stat(layout.OrientationExportPath); err != nil || info.Size() == 0 {
		t.Fatalf("expected markdown export at %s: err=%v", layout.OrientationExportPath, err)
	}
}
