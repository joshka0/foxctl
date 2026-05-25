package history

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

func TestPersistSingleInsightHistory_NoOpNilStore(t *testing.T) {
	t.Parallel()

	got, err := PersistSingleInsightHistory(context.Background(), nil, SingleInsightHistory{}, nil)
	if err != nil {
		t.Fatalf("PersistSingleInsightHistory() error = %v", err)
	}
	if len(got.Persisted) != 0 || len(got.Removed) != 0 {
		t.Fatalf("result=%+v want empty", got)
	}
}

func TestPersistGroupedInsightHistory_NoOpEmptyGroups(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openHistoryPersistenceStore(t, ctx)
	got, err := PersistGroupedInsightHistory(ctx, store, GroupedInsightHistory{}, nil)
	if err != nil {
		t.Fatalf("PersistGroupedInsightHistory() error = %v", err)
	}
	if got != nil {
		t.Fatalf("results=%v want nil", got)
	}
}

func TestInsightHistoryOwnerSelection(t *testing.T) {
	t.Parallel()

	if got := singleInsightHistoryOwnerID(SingleInsightHistory{SessionID: " sess-1 ", ConversationID: "conv-1"}); got != "sess-1" {
		t.Fatalf("single owner=%q want session id", got)
	}
	if got := singleInsightHistoryOwnerID(SingleInsightHistory{ConversationID: " conv-1 "}); got != "conv-1" {
		t.Fatalf("single fallback owner=%q want conversation id", got)
	}
	group := GroupedInsightHistoryItem{
		GroupID:            " group-1 ",
		SessionIDs:         []string{" sess-1 "},
		MainlineSessionIDs: []string{" main-1 "},
	}
	if got := groupedInsightHistoryOwnerID(group); got != "group-1" {
		t.Fatalf("group owner=%q want group id", got)
	}
	if got := groupedInsightHistorySessionID(group); got != "main-1" {
		t.Fatalf("group session=%q want mainline session id", got)
	}
	group.GroupID = ""
	group.MainlineSessionIDs = nil
	if got := groupedInsightHistoryOwnerID(group); got != "sess-1" {
		t.Fatalf("group fallback owner=%q want first session id", got)
	}
	if got := groupedInsightHistorySessionID(group); got != "sess-1" {
		t.Fatalf("group fallback session=%q want first session id", got)
	}
}

func TestPersistSingleInsightHistory_NormalizesWorkspaceAndFamilyPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openHistoryPersistenceStore(t, ctx)
	rawWorkspace := filepath.Join(t.TempDir(), "workspace", "..", "workspace")
	record := testHistoryPersistenceRecord("record-workspace", "workspace normalization is persisted")

	got, err := PersistSingleInsightHistory(ctx, store, SingleInsightHistory{
		WorkspacePath: rawWorkspace,
		SessionID:     "sess-workspace",
		Records:       []HistoryRecord{record},
	}, nil)
	if err != nil {
		t.Fatalf("PersistSingleInsightHistory() error = %v", err)
	}
	if len(got.Persisted) != 1 {
		t.Fatalf("persisted=%d want 1", len(got.Persisted))
	}

	normalized := normalizeTranscriptHistoryWorkspace(rawWorkspace)
	entry, err := store.Get(ctx, got.Persisted[0].Name, normalized)
	if err != nil {
		t.Fatalf("Get(%q, %q) error = %v", got.Persisted[0].Name, normalized, err)
	}
	payload := decodeHistoryPayload(t, entry)
	if payload["workspace_path"] != normalized {
		t.Fatalf("workspace_path=%q want %q", payload["workspace_path"], normalized)
	}
	if payload["workspace_family_path"] != workspace.FamilyPath(normalized) {
		t.Fatalf("workspace_family_path=%q want %q", payload["workspace_family_path"], workspace.FamilyPath(normalized))
	}
}

func TestPersistGroupedInsightHistory_ReconcilesPrefix(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openHistoryPersistenceStore(t, ctx)
	workspaceID := normalizeTranscriptHistoryWorkspace(t.TempDir())
	prefix := TranscriptHistoryPrefix("group-1")
	staleName := prefix + "answer:stale"
	if _, err := store.Save(ctx, storage.NamedEntry{
		Name:      staleName,
		Type:      "history_answer",
		Workspace: workspaceID,
		Summary:   "stale",
		Result:    []byte(`{}`),
	}); err != nil {
		t.Fatalf("Save(stale) error = %v", err)
	}

	got, err := PersistGroupedInsightHistory(ctx, store, GroupedInsightHistory{Groups: []GroupedInsightHistoryItem{{
		GroupID:            "group-1",
		WorkspacePath:      workspaceID,
		SessionIDs:         []string{"sidecar-1"},
		MainlineSessionIDs: []string{"mainline-1"},
		Records:            []HistoryRecord{testHistoryPersistenceRecord("record-group", "group record survives reconcile")},
	}}}, nil)
	if err != nil {
		t.Fatalf("PersistGroupedInsightHistory() error = %v", err)
	}
	if len(got) != 1 || len(got[0].Persisted) != 1 {
		t.Fatalf("results=%+v want one persisted result", got)
	}
	if len(got[0].Removed) != 1 || got[0].Removed[0] != staleName {
		t.Fatalf("removed=%v want [%q]", got[0].Removed, staleName)
	}
	if _, err := store.Get(ctx, got[0].Persisted[0].Name, workspaceID); err != nil {
		t.Fatalf("expected persisted grouped record: %v", err)
	}
	if _, err := store.Get(ctx, staleName, workspaceID); err == nil {
		t.Fatal("expected stale grouped record to be removed")
	}
}

func TestPersistSingleInsightHistory_EmbedsSuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openHistoryPersistenceStore(t, ctx)
	workspaceID := normalizeTranscriptHistoryWorkspace(t.TempDir())
	calls := 0
	got, err := PersistSingleInsightHistory(ctx, store, SingleInsightHistory{
		WorkspacePath: workspaceID,
		SessionID:     "sess-embed",
		Records:       []HistoryRecord{testHistoryPersistenceRecord("record-embed", "embedding text")},
	}, func(_ context.Context, text string) ([]float32, error) {
		calls++
		if text != "embedding text" {
			t.Fatalf("embed text=%q want retrieval text", text)
		}
		return []float32{0.1, 0.2, 0.3}, nil
	})
	if err != nil {
		t.Fatalf("PersistSingleInsightHistory() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("embed calls=%d want 1", calls)
	}
	if !got.Persisted[0].Embedded {
		t.Fatalf("embedded=false want true")
	}
	vec, err := store.GetEmbedding(ctx, got.Persisted[0].Name, workspaceID)
	if err != nil {
		t.Fatalf("GetEmbedding() error = %v", err)
	}
	if len(vec) != 3 {
		t.Fatalf("embedding len=%d want 3", len(vec))
	}
}

func TestPersistSingleInsightHistory_EmbedderFailureStillPersists(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openHistoryPersistenceStore(t, ctx)
	workspaceID := normalizeTranscriptHistoryWorkspace(t.TempDir())
	got, err := PersistSingleInsightHistory(ctx, store, SingleInsightHistory{
		WorkspacePath: workspaceID,
		SessionID:     "sess-embed-fail",
		Records:       []HistoryRecord{testHistoryPersistenceRecord("record-embed-fail", "embedding failure still saves record")},
	}, func(context.Context, string) ([]float32, error) {
		return nil, errors.New("embedding service unavailable")
	})
	if err != nil {
		t.Fatalf("PersistSingleInsightHistory() error = %v", err)
	}
	if len(got.Persisted) != 1 {
		t.Fatalf("persisted=%d want 1", len(got.Persisted))
	}
	if got.Persisted[0].Embedded {
		t.Fatal("embedded=true want false after embedder failure")
	}
	if _, err := store.Get(ctx, got.Persisted[0].Name, workspaceID); err != nil {
		t.Fatalf("expected record to persist despite embedder failure: %v", err)
	}
}

func openHistoryPersistenceStore(t *testing.T, ctx context.Context) *memory.Store {
	t.Helper()
	store, err := memory.Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("memory.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return store
}

func testHistoryPersistenceRecord(id, text string) HistoryRecord {
	return HistoryRecord{
		RecordID:      id,
		Kind:          HistoryRecordKindInsight,
		Summary:       strings.ToUpper(text[:1]) + text[1:],
		RetrievalText: text,
		Confidence:    0.8,
		InsightKind:   "direction",
		InsightStatus: "active",
	}
}

func decodeHistoryPayload(t *testing.T, entry storage.NamedEntry) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(entry.Result, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return payload
}
