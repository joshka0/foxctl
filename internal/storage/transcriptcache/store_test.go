package transcriptcache

import (
	"context"
	"testing"
)

func TestSharedRoots_PrefersConfiguredStorageRoot(t *testing.T) {
	got := SharedRoots("/tmp/storage", "/tmp/home")
	if len(got) != 2 {
		t.Fatalf("roots=%v want 2 entries", got)
	}
	if got[0] != "/tmp/storage" {
		t.Fatalf("roots[0]=%q want %q", got[0], "/tmp/storage")
	}
	if got[1] != "/tmp/home/.codex/memories/foxctl-transcript-cache" {
		t.Fatalf("roots[1]=%q want fallback codex cache path", got[1])
	}
}

func TestStorePutAndGetByNormalizedHash(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	store, err := Open(ctx, root)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	entry := Entry{
		ArtifactKind:   "reference_blob",
		NormalizedHash: DigestText("normalized article"),
		SourceHash:     DigestText("raw wrapped article"),
		DerivationMode: "deterministic",
		ModelID:        "deterministic-v1",
		PromptVersion:  "reference_blob_summary_v1",
		Summary:        "Reference document summary: test",
		SourcePreview:  "article title",
	}
	if err := store.Put(ctx, entry); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	got, ok, err := store.GetByNormalizedHash(ctx, entry.ArtifactKind, entry.NormalizedHash, entry.PromptVersion, entry.ModelID)
	if err != nil {
		t.Fatalf("GetByNormalizedHash() error = %v", err)
	}
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Summary != entry.Summary {
		t.Fatalf("summary=%q want %q", got.Summary, entry.Summary)
	}
	if got.HitCount != 1 {
		t.Fatalf("hit_count=%d want 1", got.HitCount)
	}
}
