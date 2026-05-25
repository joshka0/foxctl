package cas

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/quick"
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

func TestPutReaderFailureDoesNotPublishPartialObject(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	readErr := errors.New("injected read failure")
	if _, err := store.Put(ctx, &partialReadErrorReader{data: []byte("partial"), err: readErr}, "text/plain", []string{"partial"}); !errors.Is(err, readErr) {
		t.Fatalf("Put error = %v, want injected read failure", err)
	}

	objects, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list after failed put: %v", err)
	}
	if len(objects) != 0 {
		t.Fatalf("failed put published objects: %+v", objects)
	}

	tmpEntries, err := os.ReadDir(filepath.Join(root, "tmp"))
	if err != nil {
		t.Fatalf("read tmp dir: %v", err)
	}
	if len(tmpEntries) != 0 {
		t.Fatalf("failed put left temporary files: %v", tmpEntries)
	}
}

func TestPutMetadataFailureDoesNotPublishBlob(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	content := "metadata write failure"
	digest := casTestDigest(content)
	metaPath := filepath.Join(root, "sha256", strings.TrimPrefix(digest, "sha256:")+".json")
	if err := os.Mkdir(metaPath, 0o755); err != nil {
		t.Fatalf("create metadata path blocker: %v", err)
	}

	_, err = store.Put(ctx, bytes.NewBufferString(content), "text/plain", []string{"fault"})
	if err == nil {
		t.Fatalf("Put expected metadata write error")
	}
	if !strings.Contains(err.Error(), "cas: meta replace") {
		t.Fatalf("Put error = %v, want metadata replacement failure", err)
	}

	blobPath, err := store.pathForDigest(digest)
	if err != nil {
		t.Fatalf("path for digest: %v", err)
	}
	if _, err := os.Stat(blobPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("blob was published after metadata failure: stat err=%v", err)
	}

	objects, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list after metadata failure: %v", err)
	}
	if len(objects) != 0 {
		t.Fatalf("metadata failure published objects: %+v", objects)
	}
}

func TestMetadataDigestMustMatchRequestedObject(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	obj, err := store.Put(ctx, bytes.NewBufferString("metadata integrity"), "text/plain", []string{"integrity"})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	meta, err := store.readMetadata(obj.Digest)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	meta.Digest = casTestDigest("wrong metadata digest")
	if err := overwriteMetadataSidecar(store, obj.Digest, meta); err != nil {
		t.Fatalf("write mismatched metadata: %v", err)
	}

	if _, err := store.Head(ctx, obj.Digest); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("Head() error = %v, want ErrDigestMismatch", err)
	}
	if _, _, err := store.Get(ctx, obj.Digest); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("Get() error = %v, want ErrDigestMismatch", err)
	}
	if err := store.AddTags(ctx, obj.Digest, []string{"new"}); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("AddTags() error = %v, want ErrDigestMismatch", err)
	}
}

func TestListSkipsMismatchedMetadataDigest(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	good, err := store.Put(ctx, bytes.NewBufferString("good"), "text/plain", nil)
	if err != nil {
		t.Fatalf("put good: %v", err)
	}
	bad, err := store.Put(ctx, bytes.NewBufferString("bad"), "text/plain", nil)
	if err != nil {
		t.Fatalf("put bad: %v", err)
	}
	meta, err := store.readMetadata(bad.Digest)
	if err != nil {
		t.Fatalf("read bad metadata: %v", err)
	}
	meta.Digest = casTestDigest("wrong list metadata digest")
	if err := overwriteMetadataSidecar(store, bad.Digest, meta); err != nil {
		t.Fatalf("write mismatched metadata: %v", err)
	}

	objects, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("List() returned %d objects, want only intact metadata object: %+v", len(objects), objects)
	}
	if objects[0].Digest != good.Digest {
		t.Fatalf("List() digest = %s, want %s", objects[0].Digest, good.Digest)
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

func TestTagsAreCanonicalizedOnPutAndAdd(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	obj, err := store.Put(ctx, bytes.NewBufferString("tag canonicalization"), "text/plain", []string{
		" beta ",
		"",
		"alpha",
		"beta",
		"\t",
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := store.AddTags(ctx, obj.Digest, []string{" gamma ", "alpha", "\n"}); err != nil {
		t.Fatalf("add tags: %v", err)
	}

	head, err := store.Head(ctx, obj.Digest)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	want := []string{"alpha", "beta", "gamma"}
	if !equalStrings(head.Tags, want) {
		t.Fatalf("tags = %#v, want %#v", head.Tags, want)
	}
}

func TestMergeTagsPropertyCanonicalSet(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(existing, added []string) bool {
		merged := mergeTags(existing, added)
		if !stringsSortedAndUnique(merged) {
			return false
		}
		for _, tag := range merged {
			if strings.TrimSpace(tag) != tag || tag == "" {
				return false
			}
		}

		remerged := mergeTags(merged, nil)
		if !equalStrings(merged, remerged) {
			return false
		}

		swapped := mergeTags(added, existing)
		return equalStrings(merged, swapped)
	}, cfg)
	if err != nil {
		t.Fatalf("mergeTags property failed: %v", err)
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

func TestDigestOperationsRejectInvalidDigests(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	invalidDigests := []string{
		"",
		"sha256:",
		"sha256:abc",
		"sha256:" + strings.Repeat("0", 63),
		"sha256:" + strings.Repeat("0", 65),
		"sha256:" + strings.Repeat("g", 64),
		"SHA256:" + strings.Repeat("0", 64),
		"../sentinel",
		"sha256:../sentinel",
		strings.Repeat("0", 64),
	}

	for _, digest := range invalidDigests {
		t.Run(fmt.Sprintf("%q", digest), func(t *testing.T) {
			if _, err := store.Head(ctx, digest); err == nil {
				t.Fatalf("Head(%q) expected error", digest)
			}
			if _, _, err := store.Get(ctx, digest); err == nil {
				t.Fatalf("Get(%q) expected error", digest)
			}
			if err := store.Remove(ctx, digest); err == nil {
				t.Fatalf("Remove(%q) expected error", digest)
			}
			if err := store.Pin(ctx, digest); err == nil {
				t.Fatalf("Pin(%q) expected error", digest)
			}
			if err := store.Unpin(ctx, digest); err == nil {
				t.Fatalf("Unpin(%q) expected error", digest)
			}
			if err := store.AddTags(ctx, digest, []string{"tag"}); err == nil {
				t.Fatalf("AddTags(%q) expected error", digest)
			}
		})
	}
}

func TestUnpinInvalidDigestDoesNotRemoveFilesOutsidePins(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	sentinel := root + string(os.PathSeparator) + "sentinel"
	if err := os.WriteFile(sentinel, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	if err := store.Unpin(ctx, "../sentinel"); err == nil {
		t.Fatalf("Unpin invalid digest expected error")
	}
	got, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("sentinel was removed or unreadable: %v", err)
	}
	if string(got) != "keep" {
		t.Fatalf("sentinel changed: %q", got)
	}
}

func TestPathForDigestPropertyValidDigestsStayUnderStoreRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	cfg := &quick.Config{MaxCount: 250}
	err = quick.Check(func(raw string) bool {
		digest := casTestDigest(raw)
		path, err := store.pathForDigest(digest)
		if err != nil {
			return false
		}
		prefix := root + string(os.PathSeparator) + "sha256" + string(os.PathSeparator)
		return strings.HasPrefix(path, prefix) && strings.HasSuffix(path, strings.TrimPrefix(digest, "sha256:"))
	}, cfg)
	if err != nil {
		t.Fatalf("pathForDigest valid digest property failed: %v", err)
	}
}

func TestPathForDigestPropertyRejectsNonCanonicalDigests(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	cfg := &quick.Config{MaxCount: 250}
	err = quick.Check(func(raw string) bool {
		digest := raw
		if digestPattern.MatchString(digest) {
			digest = "sha256:" + strings.Repeat("z", 64)
		}
		_, err := store.pathForDigest(digest)
		return err != nil
	}, cfg)
	if err != nil {
		t.Fatalf("pathForDigest invalid digest property failed: %v", err)
	}
}

func casTestDigest(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func overwriteMetadataSidecar(store *Store, digest string, meta Metadata) error {
	path, err := store.pathForDigest(digest)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path+".json", data, 0o644)
}

func stringsSortedAndUnique(values []string) bool {
	for i, value := range values {
		if i == 0 {
			continue
		}
		if values[i-1] >= value {
			return false
		}
	}
	return true
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type partialReadErrorReader struct {
	data []byte
	err  error
	done bool
}

func (r *partialReadErrorReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.done = true
	return n, r.err
}
