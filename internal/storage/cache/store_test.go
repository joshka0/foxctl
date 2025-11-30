package cache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/skill"
)

func TestBuildKeyDeterministic(t *testing.T) {
	manifest := skill.Manifest{
		Metadata: skill.Metadata{Name: "text/grep", Version: "0.1.0"},
	}
	key1, err := BuildKey(manifest, []byte(`{"a":1,"b":2}`), []string{"sha256:aaa", "sha256:bbb"})
	if err != nil {
		t.Fatalf("build key: %v", err)
	}
	key2, err := BuildKey(manifest, []byte(`{"b":2,"a":1}`), []string{"sha256:bbb", "sha256:aaa"})
	if err != nil {
		t.Fatalf("build key: %v", err)
	}
	if key1 != key2 {
		t.Fatalf("expected deterministic key, got %s vs %s", key1, key2)
	}
}

func TestBuildKey_VariesOnSkillName(t *testing.T) {
	m1 := skill.Manifest{Metadata: skill.Metadata{Name: "text/grep", Version: "0.1.0"}}
	m2 := skill.Manifest{Metadata: skill.Metadata{Name: "text/search", Version: "0.1.0"}}
	input := []byte(`{"query":"foo"}`)

	key1, err := BuildKey(m1, input, nil)
	if err != nil {
		t.Fatalf("build key: %v", err)
	}
	key2, err := BuildKey(m2, input, nil)
	if err != nil {
		t.Fatalf("build key: %v", err)
	}
	if key1 == key2 {
		t.Fatalf("different skill names should produce different keys")
	}
}

func TestBuildKey_VariesOnVersion(t *testing.T) {
	m1 := skill.Manifest{Metadata: skill.Metadata{Name: "text/grep", Version: "0.1.0"}}
	m2 := skill.Manifest{Metadata: skill.Metadata{Name: "text/grep", Version: "0.2.0"}}
	input := []byte(`{"query":"foo"}`)

	key1, err := BuildKey(m1, input, nil)
	if err != nil {
		t.Fatalf("build key: %v", err)
	}
	key2, err := BuildKey(m2, input, nil)
	if err != nil {
		t.Fatalf("build key: %v", err)
	}
	if key1 == key2 {
		t.Fatalf("different versions should produce different keys")
	}
}

func TestBuildKey_VariesOnArgs(t *testing.T) {
	manifest := skill.Manifest{Metadata: skill.Metadata{Name: "text/grep", Version: "0.1.0"}}

	key1, err := BuildKey(manifest, []byte(`{"query":"foo"}`), nil)
	if err != nil {
		t.Fatalf("build key: %v", err)
	}
	key2, err := BuildKey(manifest, []byte(`{"query":"bar"}`), nil)
	if err != nil {
		t.Fatalf("build key: %v", err)
	}
	if key1 == key2 {
		t.Fatalf("different args should produce different keys")
	}
}

func TestBuildKey_VariesOnDigests(t *testing.T) {
	manifest := skill.Manifest{Metadata: skill.Metadata{Name: "text/grep", Version: "0.1.0"}}
	input := []byte(`{"query":"foo"}`)

	key1, err := BuildKey(manifest, input, []string{"sha256:aaa"})
	if err != nil {
		t.Fatalf("build key: %v", err)
	}
	key2, err := BuildKey(manifest, input, []string{"sha256:bbb"})
	if err != nil {
		t.Fatalf("build key: %v", err)
	}
	key3, err := BuildKey(manifest, input, nil)
	if err != nil {
		t.Fatalf("build key: %v", err)
	}

	if key1 == key2 {
		t.Fatalf("different digests should produce different keys")
	}
	if key1 == key3 || key2 == key3 {
		t.Fatalf("presence vs absence of digests should produce different keys")
	}
}

func TestBuildKey_EmptyInput(t *testing.T) {
	manifest := skill.Manifest{Metadata: skill.Metadata{Name: "text/grep", Version: "0.1.0"}}

	// Empty string should be treated as empty object
	key1, err := BuildKey(manifest, []byte(""), nil)
	if err != nil {
		t.Fatalf("build key with empty string: %v", err)
	}

	// Whitespace should be treated as empty object
	key2, err := BuildKey(manifest, []byte("   "), nil)
	if err != nil {
		t.Fatalf("build key with whitespace: %v", err)
	}

	// Explicit empty object
	key3, err := BuildKey(manifest, []byte("{}"), nil)
	if err != nil {
		t.Fatalf("build key with empty object: %v", err)
	}

	// All should produce the same key (canonical empty object)
	if key1 != key2 {
		t.Fatalf("empty string and whitespace should produce same key, got %s vs %s", key1, key2)
	}
	if key2 != key3 {
		t.Fatalf("whitespace and {} should produce same key, got %s vs %s", key2, key3)
	}
}

func TestStorePutAndGet(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "cache")
	store, err := Open(ctx, root, Options{AutoTTL: time.Minute})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	entry := Entry{
		CacheKey:     "sha256:test",
		SkillName:    "text/grep",
		SkillVersion: "0.1.0",
		Workspace:    filepath.Join(root, "ws"),
		Result:       []byte(`{"version":1}`),
		Digests:      []string{"sha256:abc"},
	}
	if err := store.Put(ctx, entry); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, ok, err := store.Get(ctx, entry.CacheKey)
	if err != nil || !ok {
		t.Fatalf("get: %v ok=%v", err, ok)
	}
	if got.SkillName != entry.SkillName {
		t.Fatalf("unexpected skill name %s", got.SkillName)
	}
	if got.HitCount != 0 {
		t.Fatalf("expected hit count tracked lazily, got %d", got.HitCount)
	}
}

func TestOpenCreatesNestedRoot(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	root := filepath.Join(base, "nested", "cache")
	store, err := Open(ctx, root, Options{AutoTTL: time.Minute})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	if _, err := os.Stat(root); err != nil {
		t.Fatalf("expected root directory to exist: %v", err)
	}
}

func TestStoreEvictsExpired(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := Open(ctx, root, Options{AutoTTL: time.Millisecond * 10})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	entry := Entry{
		CacheKey:     "sha256:expire",
		SkillName:    "text/grep",
		SkillVersion: "0.1.0",
		Workspace:    "ws",
		Result:       []byte(`{"version":1}`),
	}
	if err := store.Put(ctx, entry); err != nil {
		t.Fatalf("put: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, ok, err := store.Get(ctx, entry.CacheKey); err != nil {
		t.Fatalf("get: %v", err)
	} else if ok {
		t.Fatalf("expected expired entry to be evicted")
	}
}
