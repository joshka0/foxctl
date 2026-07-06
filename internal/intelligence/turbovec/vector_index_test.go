package turbovec

import (
	"errors"
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

// A vector whose length does not match the index dimension must be rejected at
// the client boundary (ErrDimMismatch) before it ever reaches the sidecar. This
// prevents the turbovecd "dim must be a multiple of 8" panic + restart loop.
// The check runs before EnsureConnected, so no daemon is required.
func TestVectorIndex_RejectsDimMismatch(t *testing.T) {
	vi := NewVectorIndex("ws-dim", 4096, IndexConfig{SocketPath: filepath.Join(t.TempDir(), "nope.sock")})

	bad := make([]float32, 100) // 100 != 4096

	if err := vi.AddVector("doc", bad); !errorsIs(err) {
		t.Fatalf("AddVector(bad dim) err = %v, want ErrDimMismatch", err)
	}
	if _, err := vi.Search(bad, 5); !errorsIs(err) {
		t.Fatalf("Search(bad dim) err = %v, want ErrDimMismatch", err)
	}
	if _, err := vi.SearchFiltered(bad, 5, []string{"doc"}); !errorsIs(err) {
		t.Fatalf("SearchFiltered(bad dim) err = %v, want ErrDimMismatch", err)
	}

	// A correctly-sized vector passes the dim check and fails later at connect
	// time (unreachable socket) — i.e. it is NOT rejected as a dim mismatch.
	good := make([]float32, 4096)
	if _, err := vi.Search(good, 5); err == nil || errorsIs(err) {
		t.Fatalf("Search(good dim) err = %v, want a non-dim (connection) error", err)
	}
}

func errorsIs(err error) bool { return errors.Is(err, ErrDimMismatch) }
