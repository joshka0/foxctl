package cas

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSQLiteStoreRejectsCorruptPersistedTags(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(ctx, SQLiteConfig{DBPath: filepath.Join(t.TempDir(), "cas.db")})
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	obj, err := store.Put(ctx, strings.NewReader("payload"), "text/plain", []string{"initial"})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE cas_objects SET tags = ? WHERE digest = ?", "{", obj.Digest); err != nil {
		t.Fatalf("corrupt tags: %v", err)
	}

	assertCorruptCASTagsRejected(t, store, obj.Digest)
}

func TestTursoStoreRejectsCorruptPersistedTags(t *testing.T) {
	ctx := context.Background()
	store, err := NewTursoStore(ctx, TursoConfig{ReplicaPath: filepath.Join(t.TempDir(), "cas.turso")})
	if err != nil {
		t.Fatalf("NewTursoStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	obj, err := store.Put(ctx, strings.NewReader("payload"), "text/plain", []string{"initial"})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if _, err := store.db.ExecContext(ctx, "UPDATE cas_objects SET tags = ? WHERE digest = ?", "{", obj.Digest); err != nil {
		t.Fatalf("corrupt tags: %v", err)
	}

	assertCorruptCASTagsRejected(t, store, obj.Digest)
}

func assertCorruptCASTagsRejected(t *testing.T, store interface {
	Head(context.Context, string) (Object, error)
	Get(context.Context, string) (io.ReadCloser, Metadata, error)
	List(context.Context) ([]Object, error)
	AddTags(context.Context, string, []string) error
}, digest string,
) {
	t.Helper()
	ctx := context.Background()

	if _, err := store.Head(ctx, digest); !casReadErrorNamesTags(err) {
		t.Fatalf("Head() error=%v, want corrupt tags error", err)
	}
	if rc, _, err := store.Get(ctx, digest); !casReadErrorNamesTags(err) {
		if rc != nil {
			_ = rc.Close()
		}
		t.Fatalf("Get() error=%v, want corrupt tags error", err)
	}
	if _, err := store.List(ctx); !casReadErrorNamesTags(err) {
		t.Fatalf("List() error=%v, want corrupt tags error", err)
	}
	if err := store.AddTags(ctx, digest, []string{"new"}); !casReadErrorNamesTags(err) {
		t.Fatalf("AddTags() error=%v, want corrupt tags error", err)
	}
}

func casReadErrorNamesTags(err error) bool {
	return err != nil && strings.Contains(err.Error(), "tags")
}
