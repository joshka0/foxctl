package cas

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
)

func TestPutHeadGet(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	data := bytes.NewBufferString("hello world")
	obj, err := store.Put(ctx, data, "text/plain", []string{"greeting"})
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	head, err := store.Head(ctx, obj.Digest)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if head.Metadata.Size != obj.Metadata.Size {
		t.Fatalf("size mismatch")
	}

	rc, meta, err := store.Get(ctx, obj.Digest)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	dataOut, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if string(dataOut) != "hello world" {
		t.Fatalf("unexpected data %q", string(dataOut))
	}
	if meta.Digest != obj.Digest {
		t.Fatalf("metadata mismatch")
	}
}

func TestGetDetectsCorruption(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	obj, err := store.Put(ctx, bytes.NewBufferString("good data"), "text/plain", nil)
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	path, err := store.pathForDigest(obj.Digest)
	if err != nil {
		t.Fatalf("path for digest: %v", err)
	}
	if err := os.WriteFile(path, []byte("tampered"), 0o644); err != nil {
		t.Fatalf("write tampered: %v", err)
	}

	rc, _, err := store.Get(ctx, obj.Digest)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, readErr := io.ReadAll(rc); readErr != nil {
		t.Fatalf("read tampered data: %v", readErr)
	}
	if err := rc.Close(); err == nil {
		t.Fatalf("expected digest mismatch error")
	}
}

func TestConcurrentPuts(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	var wg sync.WaitGroup
	const writers = 10
	results := make(chan string, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			obj, err := store.Put(ctx, bytes.NewBufferString("same"), "text/plain", nil)
			if err != nil {
				t.Errorf("put: %v", err)
				return
			}
			results <- obj.Digest
		}()
	}
	wg.Wait()
	close(results)
	digest := ""
	for d := range results {
		if digest == "" {
			digest = d
			continue
		}
		if d != digest {
			t.Fatalf("expected same digest across writers")
		}
	}

	objects, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("expected single stored object, got %d", len(objects))
	}
}

func TestPutHandlesLargeData(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	size := 3 * 1024 * 1024
	chunk := bytes.Repeat([]byte("0123456789abcdef"), size/16)
	reader := bytes.NewReader(chunk)
	obj, err := store.Put(ctx, reader, "application/octet-stream", []string{"large"})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if obj.Size != int64(size) {
		t.Fatalf("expected size %d got %d", size, obj.Size)
	}

	rc, meta, err := store.Get(ctx, obj.Digest)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close reader: %v", err)
		}
	}()
	buf, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(buf) != size {
		t.Fatalf("expected %d bytes got %d", size, len(buf))
	}
	if meta.Size != int64(size) {
		t.Fatalf("metadata size mismatch")
	}
}

func TestRemovePinnedRequiresUnpin(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	obj, err := store.Put(ctx, bytes.NewBufferString("data"), "text/plain", nil)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := store.Pin(ctx, obj.Digest); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if err := store.Remove(ctx, obj.Digest); !errors.Is(err, ErrPinned) {
		t.Fatalf("expected ErrPinned, got %v", err)
	}
	if err := store.Unpin(ctx, obj.Digest); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	if err := store.Remove(ctx, obj.Digest); err != nil {
		t.Fatalf("remove after unpin: %v", err)
	}
}
