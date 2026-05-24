package turbovec

import (
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/platform/workspace"
)

func TestNewVectorIndexCanonicalizesWorkspacePath(t *testing.T) {
	repoPath := filepath.Join(t.TempDir(), "repo")
	vi := NewVectorIndex(repoPath, 3, IndexConfig{DataDir: t.TempDir()})

	want := workspace.CanonicalID(repoPath)
	if vi.workspace != want {
		t.Fatalf("workspace = %q, want %q", vi.workspace, want)
	}
	if got := filepath.Base(vi.indexFilePath()); got != want+".tvim" {
		t.Fatalf("indexFilePath base = %q, want %q", got, want+".tvim")
	}
}
