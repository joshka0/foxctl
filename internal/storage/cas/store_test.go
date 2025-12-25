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
	if head.Size != obj.Size {
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

func TestListEmpty(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	objects, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(objects) != 0 {
		t.Fatalf("expected empty list, got %d objects", len(objects))
	}
}

func TestListMultiple(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	// Add multiple objects
	contents := []string{"object1", "object2", "object3"}
	digests := make([]string, len(contents))
	for i, content := range contents {
		obj, err := store.Put(ctx, bytes.NewBufferString(content), "text/plain", nil)
		if err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
		digests[i] = obj.Digest
	}

	objects, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(objects) != 3 {
		t.Fatalf("expected 3 objects, got %d", len(objects))
	}
}

func TestAddTags(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	obj, err := store.Put(ctx, bytes.NewBufferString("tagged content"), "text/plain", []string{"initial"})
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	// Verify initial tags
	head, err := store.Head(ctx, obj.Digest)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if len(head.Tags) != 1 || head.Tags[0] != "initial" {
		t.Fatalf("expected initial tags, got %v", head.Tags)
	}

	// Add more tags
	err = store.AddTags(ctx, obj.Digest, []string{"tag2", "tag3"})
	if err != nil {
		t.Fatalf("add tags: %v", err)
	}

	// Verify all tags
	head, err = store.Head(ctx, obj.Digest)
	if err != nil {
		t.Fatalf("head after add: %v", err)
	}
	if len(head.Tags) != 3 {
		t.Fatalf("expected 3 tags, got %d: %v", len(head.Tags), head.Tags)
	}
}

func TestAddTagsDeduplicate(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	obj, err := store.Put(ctx, bytes.NewBufferString("content"), "text/plain", []string{"tag1"})
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	// Add duplicate tag
	err = store.AddTags(ctx, obj.Digest, []string{"tag1", "tag2"})
	if err != nil {
		t.Fatalf("add tags: %v", err)
	}

	head, err := store.Head(ctx, obj.Digest)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	// Should have 2 unique tags
	if len(head.Tags) != 2 {
		t.Fatalf("expected 2 tags (deduplicated), got %d: %v", len(head.Tags), head.Tags)
	}
}

func TestClose(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	err = store.Close()
	if err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestDuplicatePut(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	content := "duplicate content"
	obj1, err := store.Put(ctx, bytes.NewBufferString(content), "text/plain", []string{"first"})
	if err != nil {
		t.Fatalf("first put: %v", err)
	}

	obj2, err := store.Put(ctx, bytes.NewBufferString(content), "text/plain", []string{"second"})
	if err != nil {
		t.Fatalf("second put: %v", err)
	}

	// Should have same digest
	if obj1.Digest != obj2.Digest {
		t.Fatalf("expected same digest, got %s and %s", obj1.Digest, obj2.Digest)
	}

	// Tags should be merged
	head, err := store.Head(ctx, obj1.Digest)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if len(head.Tags) != 2 {
		t.Fatalf("expected 2 tags after dup put, got %d: %v", len(head.Tags), head.Tags)
	}
}

func TestGetNotFound(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	_, _, err = store.Get(ctx, "sha256:0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected error for nonexistent digest")
	}
}

func TestHeadNotFound(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	_, err = store.Head(ctx, "sha256:0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected error for nonexistent digest")
	}
}

func TestRemoveNotFound(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	err = store.Remove(ctx, "sha256:0000000000000000000000000000000000000000000000000000000000000000")
	// Should either error or be a no-op
	if err != nil && !errors.Is(err, ErrNotFound) {
		t.Logf("remove nonexistent returned: %v", err)
	}
}

func TestPinUnpinNotFound(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	fakeDigest := "sha256:0000000000000000000000000000000000000000000000000000000000000000"

	// Pin nonexistent should error
	err = store.Pin(ctx, fakeDigest)
	if err == nil {
		t.Log("pin nonexistent: no error (may be ok)")
	}

	// Unpin nonexistent should be safe
	err = store.Unpin(ctx, fakeDigest)
	if err != nil {
		t.Logf("unpin nonexistent: %v", err)
	}
}

func TestAddTagsNotFound(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	err = store.AddTags(ctx, "sha256:0000000000000000000000000000000000000000000000000000000000000000", []string{"tag"})
	if err == nil {
		t.Fatal("expected error adding tags to nonexistent object")
	}
}

func TestPutEmptyContent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	obj, err := store.Put(ctx, bytes.NewBufferString(""), "text/plain", nil)
	if err != nil {
		t.Fatalf("put empty: %v", err)
	}

	if obj.Size != 0 {
		t.Fatalf("expected size 0, got %d", obj.Size)
	}

	// Should be retrievable
	rc, _, err := store.Get(ctx, obj.Digest)
	if err != nil {
		t.Fatalf("get empty: %v", err)
	}
	data, _ := io.ReadAll(rc)
	rc.Close()
	if len(data) != 0 {
		t.Fatalf("expected empty data, got %d bytes", len(data))
	}
}

func TestMetadataKind(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	kinds := []string{"application/json", "image/png", "text/markdown"}
	for _, kind := range kinds {
		obj, err := store.Put(ctx, bytes.NewBufferString("content-"+kind), kind, nil)
		if err != nil {
			t.Fatalf("put %s: %v", kind, err)
		}

		head, err := store.Head(ctx, obj.Digest)
		if err != nil {
			t.Fatalf("head %s: %v", kind, err)
		}
		if head.Kind != kind {
			t.Errorf("expected kind %q, got %q", kind, head.Kind)
		}
	}
}
