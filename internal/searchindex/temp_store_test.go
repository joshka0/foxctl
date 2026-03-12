package searchindex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenEphemeral(t *testing.T) {
	t.Parallel()

	baseRoot := t.TempDir()
	store, cleanup, err := OpenEphemeral(context.Background(), baseRoot)
	if err != nil {
		t.Fatalf("OpenEphemeral() error = %v", err)
	}
	if store == nil {
		t.Fatal("expected non-nil store")
	}

	tmpParent := filepath.Join(baseRoot, "tmp-searchindex")
	entries, err := os.ReadDir(tmpParent)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", tmpParent, err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 ephemeral directory, got %d", len(entries))
	}
	tmpRoot := filepath.Join(tmpParent, entries[0].Name())

	if err := cleanup(); err != nil {
		t.Fatalf("cleanup() error = %v", err)
	}
	if _, err := os.Stat(tmpRoot); !os.IsNotExist(err) {
		t.Fatalf("expected temp root to be removed, stat err = %v", err)
	}
}
