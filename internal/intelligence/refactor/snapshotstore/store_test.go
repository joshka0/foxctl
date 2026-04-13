package snapshotstore

import (
	"context"
	"testing"
	"time"
)

func TestStorePutAndGetRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	record := Record{
		SnapshotID:     "refsnap-123",
		Workspace:      "/repo",
		RepoRoot:       "/repo",
		Path:           "internal",
		Language:       "go",
		IncludeTests:   true,
		Mode:           "index_backed",
		GitHeadSHA:     "abc123",
		IndexHeadSHA:   "abc123",
		ArtifactDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		FileCount:      12,
		SymbolCount:    48,
		CreatedAt:      time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
	}
	if err := store.Put(ctx, record); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get(ctx, record.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SnapshotID != record.SnapshotID {
		t.Fatalf("snapshot_id=%q want %q", got.SnapshotID, record.SnapshotID)
	}
	if got.ArtifactDigest != record.ArtifactDigest {
		t.Fatalf("artifact=%q want %q", got.ArtifactDigest, record.ArtifactDigest)
	}
	if !got.IncludeTests {
		t.Fatal("expected include_tests=true")
	}
	if got.CreatedAt.UTC() != record.CreatedAt.UTC() {
		t.Fatalf("created_at=%s want %s", got.CreatedAt.UTC(), record.CreatedAt.UTC())
	}
}

func TestStorePutRequiresSnapshotIDAndArtifact(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.Put(ctx, Record{ArtifactDigest: "sha256:test"}); err == nil {
		t.Fatal("expected snapshot id validation error")
	}
	if err := store.Put(ctx, Record{SnapshotID: "refsnap-1"}); err == nil {
		t.Fatal("expected artifact validation error")
	}
}
