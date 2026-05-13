package evidence

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	refscope "github.com/joshka0/foxctl/internal/intelligence/refactor/scope"
	refsnapshot "github.com/joshka0/foxctl/internal/intelligence/refactor/snapshot"
	refsnapshotstore "github.com/joshka0/foxctl/internal/intelligence/refactor/snapshotstore"
	refstatus "github.com/joshka0/foxctl/internal/intelligence/refactor/status"
	"github.com/joshka0/foxctl/internal/storage/cas"
)

func TestLoadSnapshotBySnapshotID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storageRoot := t.TempDir()
	casRoot := t.TempDir()

	payload := refsnapshot.Payload{
		SnapshotID: "refsnap-123",
		CreatedAt:  time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		Mode:       refstatus.ModeParserOnly,
		Scope: refscope.Scope{
			Workspace: "/repo",
			RepoRoot:  "/repo",
			Path:      "internal",
			Absolute:  "/repo/internal",
			Mode:      "explicit",
			Language:  "go",
			IsDir:     true,
		},
		Summary: refsnapshot.Summary{
			FileCount:   2,
			SymbolCount: 5,
			LineCount:   120,
		},
		Files: []refsnapshot.FileSnapshot{
			{Path: "internal/a.go", Language: "go", LineCount: 12, Hash: "hash-a", SymbolCount: 2},
		},
		Symbols: []refsnapshot.SymbolSnapshot{
			{Path: "internal/a.go", SymbolID: "sym:a", Name: "A", Hash: "sym-hash-a"},
		},
	}
	artifact := putJSONArtifact(t, ctx, casRoot, payload)
	putSnapshotRecord(t, ctx, storageRoot, refsnapshotstore.Record{
		SnapshotID:     payload.SnapshotID,
		Workspace:      payload.Scope.Workspace,
		RepoRoot:       payload.Scope.RepoRoot,
		Path:           payload.Scope.Path,
		Language:       payload.Scope.Language,
		Mode:           string(payload.Mode),
		ArtifactDigest: artifact,
		FileCount:      payload.Summary.FileCount,
		SymbolCount:    payload.Summary.SymbolCount,
		CreatedAt:      payload.CreatedAt,
	})

	got, err := Load(ctx, storageRoot, casRoot, Options{SnapshotID: payload.SnapshotID})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Kind != ArtifactKindSnapshot {
		t.Fatalf("kind=%q want %q", got.Kind, ArtifactKindSnapshot)
	}
	if got.Artifact != artifact {
		t.Fatalf("artifact=%q want %q", got.Artifact, artifact)
	}
	if got.Snapshot == nil || got.Snapshot.SnapshotID != payload.SnapshotID {
		t.Fatalf("snapshot=%#v", got.Snapshot)
	}
	if got.SnapshotRecord == nil || got.SnapshotRecord.ArtifactDigest != artifact {
		t.Fatalf("snapshot_record=%#v", got.SnapshotRecord)
	}
}

func TestLoadHotspotPackByArtifact(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storageRoot := t.TempDir()
	casRoot := t.TempDir()

	pack := HotspotPack{
		SnapshotID:       "refsnap-999",
		SnapshotArtifact: "sha256:snapshot",
		IndexMode:        "parser_only",
		Reasons:          []string{"repoindex_missing"},
		Hotspots: []HotspotRow{
			{File: "internal/a.go", Symbol: "DoThing", RuleID: "function_hotspot", RecentChangeCount: 3, HotScore: 0.75},
		},
	}
	artifact := putJSONArtifact(t, ctx, casRoot, pack)

	got, err := Load(ctx, storageRoot, casRoot, Options{Artifact: artifact})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Kind != ArtifactKindHotspotPack {
		t.Fatalf("kind=%q want %q", got.Kind, ArtifactKindHotspotPack)
	}
	if got.HotspotPack == nil || len(got.HotspotPack.Hotspots) != 1 {
		t.Fatalf("hotspot_pack=%#v", got.HotspotPack)
	}
	if got.SnapshotID != pack.SnapshotID {
		t.Fatalf("snapshot_id=%q want %q", got.SnapshotID, pack.SnapshotID)
	}
}

func TestLoadRejectsConflictingSelectors(t *testing.T) {
	t.Parallel()

	_, err := Load(context.Background(), t.TempDir(), t.TempDir(), Options{
		Artifact:   "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		SnapshotID: "refsnap-1",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	loadErr, ok := err.(*LoadError)
	if !ok {
		t.Fatalf("err=%T want *LoadError", err)
	}
	if loadErr.Kind != ErrorKindInvalidInput {
		t.Fatalf("kind=%q want %q", loadErr.Kind, ErrorKindInvalidInput)
	}
}

func TestLoadSnapshotIDRejectsMismatchedArtifact(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storageRoot := t.TempDir()
	casRoot := t.TempDir()

	payload := refsnapshot.Payload{
		SnapshotID: "refsnap-actual",
		Mode:       refstatus.ModeParserOnly,
		Scope: refscope.Scope{
			Workspace: "/repo",
			RepoRoot:  "/repo",
			Path:      "internal",
			Language:  "go",
		},
		Summary: refsnapshot.Summary{FileCount: 1},
		Files:   []refsnapshot.FileSnapshot{{Path: "internal/a.go", Language: "go"}},
		Symbols: []refsnapshot.SymbolSnapshot{},
	}
	artifact := putJSONArtifact(t, ctx, casRoot, payload)
	putSnapshotRecord(t, ctx, storageRoot, refsnapshotstore.Record{
		SnapshotID:     "refsnap-requested",
		ArtifactDigest: artifact,
		Workspace:      "/repo",
		RepoRoot:       "/repo",
		Path:           "internal",
		Language:       "go",
		Mode:           string(refstatus.ModeParserOnly),
		CreatedAt:      time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC),
	})

	_, err := Load(ctx, storageRoot, casRoot, Options{SnapshotID: "refsnap-requested"})
	if err == nil {
		t.Fatal("expected mismatched snapshot artifact error")
	}
	loadErr, ok := err.(*LoadError)
	if !ok {
		t.Fatalf("err=%T want *LoadError", err)
	}
	if loadErr.Kind != ErrorKindInvalidArtifact {
		t.Fatalf("kind=%q want %q", loadErr.Kind, ErrorKindInvalidArtifact)
	}
}

func TestLoadRejectsInvalidJSONArtifact(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	casRoot := t.TempDir()
	artifact := putRawArtifact(t, ctx, casRoot, []byte("{not-json"))

	_, err := Load(ctx, t.TempDir(), casRoot, Options{Artifact: artifact})
	if err == nil {
		t.Fatal("expected invalid artifact error")
	}
	loadErr, ok := err.(*LoadError)
	if !ok {
		t.Fatalf("err=%T want *LoadError", err)
	}
	if loadErr.Kind != ErrorKindInvalidArtifact {
		t.Fatalf("kind=%q want %q", loadErr.Kind, ErrorKindInvalidArtifact)
	}
}

func putJSONArtifact(t *testing.T, ctx context.Context, casRoot string, payload any) string {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return putRawArtifact(t, ctx, casRoot, body)
}

func putRawArtifact(t *testing.T, ctx context.Context, casRoot string, body []byte) string {
	t.Helper()

	store, err := cas.NewStore(casRoot)
	if err != nil {
		t.Fatalf("open cas: %v", err)
	}
	defer store.Close()

	obj, err := store.Put(ctx, bytes.NewReader(body), "application/json", []string{"test"})
	if err != nil {
		t.Fatalf("put cas: %v", err)
	}
	return obj.Digest
}

func putSnapshotRecord(t *testing.T, ctx context.Context, storageRoot string, record refsnapshotstore.Record) {
	t.Helper()

	store, err := refsnapshotstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("open snapshot store: %v", err)
	}
	defer store.Close()
	if err := store.Put(ctx, record); err != nil {
		t.Fatalf("put snapshot record: %v", err)
	}
}
