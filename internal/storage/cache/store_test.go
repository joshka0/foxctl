package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/quick"
	"time"

	"github.com/joshka0/foxctl/internal/domain/skill"
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

func TestBuildKeyTreatsDigestInputsAsASet(t *testing.T) {
	manifest := skill.Manifest{Metadata: skill.Metadata{Name: "text/grep", Version: "0.1.0"}}
	input := []byte(`{"query":"foo"}`)

	key1, err := BuildKey(manifest, input, []string{"sha256:aaa", "sha256:bbb"})
	if err != nil {
		t.Fatalf("build key1: %v", err)
	}
	key2, err := BuildKey(manifest, input, []string{" sha256:bbb ", "sha256:aaa", "sha256:aaa", ""})
	if err != nil {
		t.Fatalf("build key2: %v", err)
	}

	if key1 != key2 {
		t.Fatalf("duplicate, empty, and padded digest inputs should not change cache key: %s vs %s", key1, key2)
	}
}

func TestBuildKeySeparatesSkillNameVersionAndArgs(t *testing.T) {
	left := skill.Manifest{Metadata: skill.Metadata{Name: "ab", Version: "c"}}
	right := skill.Manifest{Metadata: skill.Metadata{Name: "a", Version: "bc"}}
	input := []byte(`"d"`)

	key1, err := BuildKey(left, input, nil)
	if err != nil {
		t.Fatalf("build key1: %v", err)
	}
	key2, err := BuildKey(right, input, nil)
	if err != nil {
		t.Fatalf("build key2: %v", err)
	}

	if key1 == key2 {
		t.Fatalf("cache key must separate skill name, version, and args")
	}
}

func TestBuildKeyPropertyCanonicalizesJSONAndDigestSet(t *testing.T) {
	check := func(a, b string, n uint16, duplicate bool) bool {
		manifest := skill.Manifest{Metadata: skill.Metadata{Name: "text/grep", Version: "0.1.0"}}
		leftInput := []byte(`{"a":` + jsonString(a) + `,"b":` + jsonString(b) + `,"n":` + strconv.FormatUint(uint64(n), 10) + `}`)
		rightInput := []byte(`{"n":` + strconv.FormatUint(uint64(n), 10) + `,"b":` + jsonString(b) + `,"a":` + jsonString(a) + `}`)
		leftDigests := []string{"sha256:bbb", "sha256:aaa"}
		rightDigests := []string{"sha256:aaa", "sha256:bbb"}
		if duplicate {
			rightDigests = append(rightDigests, "sha256:aaa")
		}

		key1, err := BuildKey(manifest, leftInput, leftDigests)
		if err != nil {
			return false
		}
		key2, err := BuildKey(manifest, rightInput, rightDigests)
		if err != nil {
			return false
		}
		return key1 == key2
	}

	if err := quick.Check(check, &quick.Config{MaxCount: 128}); err != nil {
		t.Fatalf("cache key should depend on canonical JSON and digest set, not insertion order: %v", err)
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

func jsonString(s string) string {
	data, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(data)
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
	store := openTestCacheStore(t, ctx, time.Minute)

	entry := Entry{
		CacheKey:     "sha256:test",
		SkillName:    "text/grep",
		SkillVersion: "0.1.0",
		Workspace:    "ws",
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

func TestStorePutRejectsMissingIdentity(t *testing.T) {
	ctx := context.Background()

	for _, tt := range []struct {
		name  string
		entry Entry
		want  string
	}{
		{name: "cache key", entry: Entry{SkillName: "text/grep", SkillVersion: "0.1.0"}, want: "cache_key"},
		{name: "skill name", entry: Entry{CacheKey: "sha256:key", SkillVersion: "0.1.0"}, want: "skill_name"},
		{name: "skill version", entry: Entry{CacheKey: "sha256:key", SkillName: "text/grep"}, want: "skill_version"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := openTestCacheStore(t, ctx, time.Minute)
			if err := store.Put(ctx, tt.entry); err == nil {
				t.Fatalf("expected missing %s to be rejected", tt.want)
			} else if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want %s", err, tt.want)
			}
		})
	}
}

func TestCacheReadsRejectCorruptPersistedFields(t *testing.T) {
	ctx := context.Background()

	for _, tt := range []struct {
		column string
		value  any
		want   string
	}{
		{column: "hit_count", value: -1, want: "hit_count"},
		{column: "created_at", value: "", want: "created_at"},
		{column: "expires_at", value: "", want: "expires_at"},
		{column: "last_accessed", value: "", want: "last_accessed"},
	} {
		t.Run(tt.column, func(t *testing.T) {
			store := openTestCacheStore(t, ctx, time.Hour)
			entry := Entry{
				CacheKey:     "sha256:corrupt-" + tt.column,
				SkillName:    "text/grep",
				SkillVersion: "0.1.0",
				Workspace:    "ws",
				Result:       []byte(`{"version":1}`),
				Digests:      []string{"sha256:abc"},
			}
			if err := store.Put(ctx, entry); err != nil {
				t.Fatalf("put: %v", err)
			}
			if _, err := store.db.ExecContext(ctx,
				fmt.Sprintf(`UPDATE auto_cache SET %s = $1 WHERE cache_key = $2`, tt.column),
				tt.value, entry.CacheKey); err != nil {
				t.Fatalf("corrupt %s: %v", tt.column, err)
			}

			_, _, err := store.Get(ctx, entry.CacheKey)
			if err == nil {
				t.Fatalf("Get accepted corrupt %s", tt.column)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Get error=%v, want %s", err, tt.want)
			}

			_, err = store.Recent(ctx, "ws", 10)
			if err == nil {
				t.Fatalf("Recent accepted corrupt %s", tt.column)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Recent error=%v, want %s", err, tt.want)
			}
		})
	}
}

func TestDecodeEntryFieldsRejectsNegativeHitCountProperty(t *testing.T) {
	validTimestamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	rejectsGeneratedNegativeHitCounts := func(raw uint16) bool {
		entry := Entry{HitCount: -int(raw) - 1}
		err := decodeEntryFields(&entry, "[]", validTimestamp, validTimestamp, validTimestamp, "scan")
		return err != nil && strings.Contains(err.Error(), "hit_count")
	}
	if err := quick.Check(rejectsGeneratedNegativeHitCounts, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("negative hit count property failed: %v", err)
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
	store := openTestCacheStore(t, ctx, time.Millisecond*10)

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

func openTestCacheStore(t *testing.T, ctx context.Context, ttl time.Duration) *Store {
	t.Helper()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "cache"), Options{AutoTTL: ttl})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})
	return store
}
