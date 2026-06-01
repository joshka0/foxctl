package todos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/context/todosync"
)

func TestFilePathForSessionRejectsTraversalByConstruction(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	for _, sessionID := range []string{
		"../escape",
		"..\\escape",
		"nested/session",
		"/tmp/escape",
		"session/../../escape",
		"session\x00id",
	} {
		path := store.FilePathForSession(sessionID)
		assertPathUnderDir(t, store.TodosDir(), path)
		if strings.Contains(filepath.Base(path), string(os.PathSeparator)) {
			t.Fatalf("session %q produced filename with path separator: %q", sessionID, path)
		}
	}
}

func TestFilePathForSessionPropertyStaysUnderTodosDir(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	prop := func(sessionID string) bool {
		path := store.FilePathForSession(sessionID)
		return pathUnderDir(store.TodosDir(), path)
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("generated session path escaped todos dir: %v", err)
	}
}

func TestWriteRequiresProviderStatePermission(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	if _, err := store.Write("session-1", []todosync.ClaudeTodo{{
		Content: "Do work",
		Status:  "pending",
	}}, WriteOptions{}); err == nil {
		t.Fatal("expected write without provider-state permission to be denied")
	}

	if _, err := os.Stat(store.TodosDir()); !os.IsNotExist(err) {
		t.Fatalf("denied write created provider state directory: err=%v", err)
	}
}

func TestWriteFileConflictPreservesExistingTodos(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	path := store.FilePathForSession("session-1")
	initial := []byte(`[{"content":"original","status":"pending","activeForm":"original"}]`)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create todos dir: %v", err)
	}
	if err := os.WriteFile(path, initial, 0o600); err != nil {
		t.Fatalf("write initial todo file: %v", err)
	}

	_, err := store.WriteFile(path, []todosync.ClaudeTodo{{
		Content:    "replacement",
		Status:     "completed",
		ActiveForm: "replacement",
	}}, WriteOptions{
		AllowProviderState: true,
		LastHash:           strings.Repeat("0", 64),
	})
	if err == nil {
		t.Fatal("expected stale hash conflict")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read todo file after conflict: %v", err)
	}
	if string(got) != string(initial) {
		t.Fatalf("conflict overwrote todo file:\ngot  %s\nwant %s", got, initial)
	}
}

func assertPathUnderDir(t *testing.T, root, candidate string) {
	t.Helper()

	if !pathUnderDir(root, candidate) {
		t.Fatalf("path %q escaped root %q", candidate, root)
	}
}

func pathUnderDir(root, candidate string) bool {
	root = filepath.Clean(root)
	candidate = filepath.Clean(candidate)
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
