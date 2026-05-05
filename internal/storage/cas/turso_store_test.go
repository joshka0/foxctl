package cas

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestTursoStoreLocalNoCGOPath(t *testing.T) {
	ctx := context.Background()
	store, err := NewTursoStore(ctx, TursoConfig{
		ReplicaPath: filepath.Join(t.TempDir(), "cas.turso"),
	})
	if err != nil {
		t.Fatalf("NewTursoStore(local) error = %v", err)
	}
	defer func() { _ = store.Close() }()

	obj, err := store.Put(ctx, strings.NewReader("payload"), "text/plain", []string{"local"})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	body, meta, err := store.Get(ctx, obj.Digest)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != "payload" {
		t.Fatalf("body = %q, want payload", string(data))
	}
	if meta.Kind != "text/plain" {
		t.Fatalf("Kind = %q, want text/plain", meta.Kind)
	}
}
