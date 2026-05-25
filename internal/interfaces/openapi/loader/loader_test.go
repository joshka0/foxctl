package loader

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/cas"
	memstore "github.com/joshka0/foxctl/internal/storage/memory"
)

func TestLoadFromFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join("testdata", "valid-3.0.json")
	l := New(nil, nil)
	spec, err := l.Load(context.Background(), path)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	if spec.Version != "3.0.3" {
		t.Fatalf("expected version 3.0.3 got %s", spec.Version)
	}
	op, err := spec.GetOperation("listUsers")
	if err != nil {
		t.Fatalf("get operation: %v", err)
	}
	if op.Method != "GET" || op.Path != "/users" {
		t.Fatalf("unexpected operation: %#v", op)
	}
	spec2, err := l.Load(context.Background(), path)
	if err != nil {
		t.Fatalf("reload spec: %v", err)
	}
	if spec2 != spec {
		t.Fatalf("expected cached spec pointer reuse")
	}
}

func TestLoadFromCAS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := cas.NewStore(tmp)
	if err != nil {
		t.Fatalf("cas store: %v", err)
	}
	data, err := os.ReadFile(filepath.Join("testdata", "valid-3.1.yaml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	obj, err := store.Put(ctx, bytesReader(data), "application/openapi+yaml", nil)
	if err != nil {
		t.Fatalf("cas put: %v", err)
	}
	l := New(store, nil)
	spec, err := l.Load(ctx, obj.Digest)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	if spec.Version != "3.1.0" {
		t.Fatalf("unexpected version %s", spec.Version)
	}
	if _, err := spec.GetOperation("createWidget"); err != nil {
		t.Fatalf("operation lookup: %v", err)
	}
}

func TestLoadFromMemoryWithDigest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	casPath := filepath.Join(root, "cas")
	memoryPath := filepath.Join(root, "memory")
	if err := os.MkdirAll(casPath, 0o755); err != nil {
		t.Fatalf("mkdir cas: %v", err)
	}
	if err := os.MkdirAll(memoryPath, 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	casStore, err := cas.NewStore(casPath)
	if err != nil {
		t.Fatalf("cas store: %v", err)
	}
	specBytes, err := os.ReadFile(filepath.Join("testdata", "valid-3.0.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	obj, err := casStore.Put(ctx, bytesReader(specBytes), "application/openapi+json", nil)
	if err != nil {
		t.Fatalf("cas put: %v", err)
	}
	memStore, err := memstore.Open(ctx, memoryPath, casPath)
	if err != nil {
		t.Fatalf("memory open: %v", err)
	}
	t.Cleanup(func() {
		if err := memStore.Close(); err != nil {
			t.Fatalf("memory close: %v", err)
		}
	})
	entry := storage.NamedEntry{
		Name:      "sample",
		Type:      "openapi_spec",
		Workspace: memoryPath,
		Summary:   "sample",
		Result:    []byte(`{"status":"ok","command":"test"}`),
		Digests:   []string{obj.Digest},
	}
	if _, err := memStore.Save(ctx, entry); err != nil {
		t.Fatalf("memory save: %v", err)
	}
	l := New(casStore, memStore, WithWorkspace(memoryPath))
	spec, err := l.Load(ctx, "memory:sample")
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	if spec.Source != "memory:sample" {
		t.Fatalf("expected memory source got %s", spec.Source)
	}
	if len(spec.Operations) == 0 {
		t.Fatalf("expected operations indexed")
	}
}

func TestLoadFromMemoryInlineSpecWithStatusCommandFields(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	casPath := filepath.Join(root, "cas")
	memoryPath := filepath.Join(root, "memory")
	if err := os.MkdirAll(casPath, 0o755); err != nil {
		t.Fatalf("mkdir cas: %v", err)
	}
	if err := os.MkdirAll(memoryPath, 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	memStore, err := memstore.Open(ctx, memoryPath, casPath)
	if err != nil {
		t.Fatalf("memory open: %v", err)
	}
	t.Cleanup(func() {
		if err := memStore.Close(); err != nil {
			t.Fatalf("memory close: %v", err)
		}
	})

	specBytes := []byte(`{
  "openapi": "3.0.3",
  "info": {
    "title": "Command API",
    "version": "1.0.0"
  },
  "paths": {
    "/commands": {
      "get": {
        "operationId": "getCommandStatus",
        "responses": {
          "200": {
            "description": "OK",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "status": { "type": "string" },
                    "command": { "type": "string" }
                  }
                }
              }
            }
          }
        }
      }
    }
  }
}`)
	entry := storage.NamedEntry{
		Name:      "inline-openapi",
		Type:      "openapi_spec",
		Workspace: memoryPath,
		Summary:   "inline spec",
		Result:    specBytes,
	}
	if _, err := memStore.Save(ctx, entry); err != nil {
		t.Fatalf("memory save: %v", err)
	}

	l := New(nil, memStore, WithWorkspace(memoryPath))
	spec, err := l.Load(ctx, "memory:inline-openapi")
	if err != nil {
		t.Fatalf("load inline spec: %v", err)
	}
	if spec.Digest != "" {
		t.Fatalf("expected inline memory spec without digest, got %s", spec.Digest)
	}
	op, err := spec.GetOperation("getCommandStatus")
	if err != nil {
		t.Fatalf("operation lookup: %v", err)
	}
	if op.Path != "/commands" || op.Method != "GET" {
		t.Fatalf("unexpected operation: %#v", op)
	}
}

func TestInvalidSpecs(t *testing.T) {
	t.Parallel()
	l := New(nil, nil)
	if _, err := l.Load(context.Background(), filepath.Join("testdata", "invalid-version.json")); err == nil {
		t.Fatalf("expected version error")
	}
	if _, err := l.Load(context.Background(), filepath.Join("testdata", "malformed.yaml")); err == nil {
		t.Fatalf("expected parse error")
	}
}

func bytesReader(data []byte) *bytes.Reader {
	return bytes.NewReader(data)
}
