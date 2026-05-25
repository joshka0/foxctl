package history

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"

	workspacepkg "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

func TestPersistSingleRunHistory_SelectsSessionOwnerBeforeConversation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openHistoryMemoryStore(t, ctx)

	report, err := PersistSingleRunHistory(ctx, store, SingleRunHistoryInput{
		WorkspacePath:  t.TempDir(),
		SessionID:      " sess-owner ",
		ConversationID: "conv-owner",
		Records:        []HistoryRecord{testHistoryRecord("owner-selection")},
	}, nil)
	if err != nil {
		t.Fatalf("PersistSingleRunHistory() error = %v", err)
	}
	if len(report.Persisted) != 1 {
		t.Fatalf("persisted=%d want 1", len(report.Persisted))
	}
	if !strings.HasPrefix(report.Persisted[0].Name, TranscriptHistoryPrefix("sess-owner")) {
		t.Fatalf("name=%q want session owner prefix %q", report.Persisted[0].Name, TranscriptHistoryPrefix("sess-owner"))
	}
}

func TestPersistSingleRunHistory_FallsBackToConversationOwner(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openHistoryMemoryStore(t, ctx)

	report, err := PersistSingleRunHistory(ctx, store, SingleRunHistoryInput{
		WorkspacePath:  t.TempDir(),
		ConversationID: " conv-owner ",
		Records:        []HistoryRecord{testHistoryRecord("conversation-owner")},
	}, nil)
	if err != nil {
		t.Fatalf("PersistSingleRunHistory() error = %v", err)
	}
	if len(report.Persisted) != 1 {
		t.Fatalf("persisted=%d want 1", len(report.Persisted))
	}
	if !strings.HasPrefix(report.Persisted[0].Name, TranscriptHistoryPrefix("conv-owner")) {
		t.Fatalf("name=%q want conversation owner prefix %q", report.Persisted[0].Name, TranscriptHistoryPrefix("conv-owner"))
	}
}

func TestPersistGroupedRunHistory_SelectsOwnerAndSessionDeterministically(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openHistoryMemoryStore(t, ctx)
	groupWorkspace := t.TempDir()
	sessionWorkspace := t.TempDir()

	reports, err := PersistGroupedRunHistory(ctx, store, []GroupRunHistoryInput{{
		WorkspacePath:      groupWorkspace,
		GroupID:            " group-owner ",
		SessionIDs:         []string{"fallback-session"},
		MainlineSessionIDs: []string{" mainline-session "},
		Records:            []HistoryRecord{testHistoryRecord("group-owner")},
	}, {
		WorkspacePath: sessionWorkspace,
		SessionIDs:    []string{" first-session ", "second-session"},
		Records:       []HistoryRecord{testHistoryRecord("session-owner")},
	}}, nil)
	if err != nil {
		t.Fatalf("PersistGroupedRunHistory() error = %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("reports=%d want 2", len(reports))
	}
	if !strings.HasPrefix(reports[0].Persisted[0].Name, TranscriptHistoryPrefix("group-owner")) {
		t.Fatalf("first name=%q want group owner prefix", reports[0].Persisted[0].Name)
	}
	firstEntry, err := store.Get(ctx, reports[0].Persisted[0].Name, NormalizeTranscriptHistoryWorkspace(groupWorkspace))
	if err != nil {
		t.Fatalf("Get(group record) error = %v", err)
	}
	if firstEntry.SessionID != "mainline-session" {
		t.Fatalf("session id=%q want mainline-session", firstEntry.SessionID)
	}
	if !strings.HasPrefix(reports[1].Persisted[0].Name, TranscriptHistoryPrefix("first-session")) {
		t.Fatalf("second name=%q want first session owner prefix", reports[1].Persisted[0].Name)
	}
	secondEntry, err := store.Get(ctx, reports[1].Persisted[0].Name, NormalizeTranscriptHistoryWorkspace(sessionWorkspace))
	if err != nil {
		t.Fatalf("Get(session record) error = %v", err)
	}
	if secondEntry.SessionID != "first-session" {
		t.Fatalf("second session id=%q want first-session", secondEntry.SessionID)
	}
}

func TestPersistGroupedRunHistorySkipsBlankOwnerAndMainlineCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openHistoryMemoryStore(t, ctx)
	workspacePath := t.TempDir()

	reports, err := PersistGroupedRunHistory(ctx, store, []GroupRunHistoryInput{{
		WorkspacePath:      workspacePath,
		SessionIDs:         []string{" ", " session-owner "},
		MainlineSessionIDs: []string{"\t", " mainline-session "},
		Records:            []HistoryRecord{testHistoryRecord("blank-candidates")},
	}}, nil)
	if err != nil {
		t.Fatalf("PersistGroupedRunHistory() error = %v", err)
	}
	if len(reports) != 1 || len(reports[0].Persisted) != 1 {
		t.Fatalf("reports=%+v want one persisted record", reports)
	}
	if !strings.HasPrefix(reports[0].Persisted[0].Name, TranscriptHistoryPrefix("session-owner")) {
		t.Fatalf("name=%q want first non-empty session owner prefix", reports[0].Persisted[0].Name)
	}
	entry, err := store.Get(ctx, reports[0].Persisted[0].Name, NormalizeTranscriptHistoryWorkspace(workspacePath))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if entry.SessionID != "mainline-session" {
		t.Fatalf("session id=%q want first non-empty mainline session", entry.SessionID)
	}
}

func TestPersistSingleRunHistory_NormalizesWorkspaceInEntryAndPayload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openHistoryMemoryStore(t, ctx)
	root := t.TempDir()
	workspacePath := filepath.Join(root, "nested", "..", "nested")
	wantWorkspace := NormalizeTranscriptHistoryWorkspace(workspacePath)

	report, err := PersistSingleRunHistory(ctx, store, SingleRunHistoryInput{
		WorkspacePath: workspacePath,
		SessionID:     "sess-workspace",
		Records:       []HistoryRecord{testHistoryRecord("workspace-normalization")},
	}, nil)
	if err != nil {
		t.Fatalf("PersistSingleRunHistory() error = %v", err)
	}
	entry, err := store.Get(ctx, report.Persisted[0].Name, wantWorkspace)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if wantEntryWorkspace := workspacepkg.CanonicalID(wantWorkspace); entry.Workspace != wantEntryWorkspace {
		t.Fatalf("entry workspace=%q want canonical key %q", entry.Workspace, wantEntryWorkspace)
	}
	var payload map[string]any
	if err := json.Unmarshal(entry.Result, &payload); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if payload["workspace_path"] != wantWorkspace {
		t.Fatalf("workspace_path=%v want %q", payload["workspace_path"], wantWorkspace)
	}
	if payload["workspace_family_path"] != wantWorkspace {
		t.Fatalf("workspace_family_path=%v want %q", payload["workspace_family_path"], wantWorkspace)
	}
}

func TestPersistSingleRunHistoryPropertyWorkspaceCanonicalizesEntryAndPreservesPayloadPath(t *testing.T) {
	t.Parallel()

	property := func(seed string) bool {
		ctx := context.Background()
		store := openHistoryMemoryStore(t, ctx)
		workspacePath := filepath.Join(t.TempDir(), "workspace-"+stablePathSegment(seed), "..", "workspace-"+stablePathSegment(seed))
		wantWorkspace := NormalizeTranscriptHistoryWorkspace(workspacePath)

		report, err := PersistSingleRunHistory(ctx, store, SingleRunHistoryInput{
			WorkspacePath: workspacePath,
			SessionID:     "sess-workspace-property",
			Records:       []HistoryRecord{testHistoryRecord("workspace-property")},
		}, nil)
		if err != nil {
			t.Logf("PersistSingleRunHistory() error = %v", err)
			return false
		}
		if len(report.Persisted) != 1 {
			t.Logf("persisted=%d want 1", len(report.Persisted))
			return false
		}
		entry, err := store.Get(ctx, report.Persisted[0].Name, wantWorkspace)
		if err != nil {
			t.Logf("Get() error = %v", err)
			return false
		}
		if entry.Workspace != workspacepkg.CanonicalID(wantWorkspace) {
			t.Logf("entry workspace=%q want %q", entry.Workspace, workspacepkg.CanonicalID(wantWorkspace))
			return false
		}
		var payload map[string]any
		if err := json.Unmarshal(entry.Result, &payload); err != nil {
			t.Logf("unmarshal result: %v", err)
			return false
		}
		return payload["workspace_path"] == wantWorkspace &&
			payload["workspace_family_path"] == workspacepkg.FamilyPath(wantWorkspace)
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("workspace persistence property failed: %v", err)
	}
}

func TestPersistHistoryRun_NoOpsEmptyInputs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openHistoryMemoryStore(t, ctx)

	report, err := PersistSingleRunHistory(ctx, store, SingleRunHistoryInput{}, nil)
	if err != nil {
		t.Fatalf("PersistSingleRunHistory(empty) error = %v", err)
	}
	if len(report.Persisted) != 0 || len(report.Removed) != 0 {
		t.Fatalf("single report=%+v want empty", report)
	}
	reports, err := PersistGroupedRunHistory(ctx, store, nil, nil)
	if err != nil {
		t.Fatalf("PersistGroupedRunHistory(nil) error = %v", err)
	}
	if reports != nil {
		t.Fatalf("nil grouped reports=%v want nil", reports)
	}
	reports, err = PersistGroupedRunHistory(ctx, store, []GroupRunHistoryInput{{}}, nil)
	if err != nil {
		t.Fatalf("PersistGroupedRunHistory(empty item) error = %v", err)
	}
	if len(reports) != 1 || len(reports[0].Persisted) != 0 || len(reports[0].Removed) != 0 {
		t.Fatalf("grouped reports=%+v want one empty report", reports)
	}
}

func TestPersistSingleRunHistory_ReconcilesOwnerPrefix(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openHistoryMemoryStore(t, ctx)
	workspacePath := t.TempDir()
	workspaceID := NormalizeTranscriptHistoryWorkspace(workspacePath)
	prefix := TranscriptHistoryPrefix("sess-reconcile")
	if _, err := store.Save(ctx, memory.NamedEntry{
		Name:      prefix + "insight:stale",
		Type:      "history_insight",
		Workspace: workspaceID,
		Summary:   "stale",
		Result:    []byte(`{}`),
	}); err != nil {
		t.Fatalf("Save(stale) error = %v", err)
	}

	report, err := PersistSingleRunHistory(ctx, store, SingleRunHistoryInput{
		WorkspacePath: workspacePath,
		SessionID:     "sess-reconcile",
		Records:       []HistoryRecord{testHistoryRecord("prefix-keep")},
	}, nil)
	if err != nil {
		t.Fatalf("PersistSingleRunHistory() error = %v", err)
	}
	if len(report.Removed) != 1 || report.Removed[0] != prefix+"insight:stale" {
		t.Fatalf("removed=%v want stale entry", report.Removed)
	}
	if _, err := store.Get(ctx, prefix+"insight:stale", workspaceID); err == nil {
		t.Fatal("expected stale entry to be deleted")
	}
}

func TestPersistSingleRunHistory_EmbedderSuccessAndFailureAreBestEffort(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openHistoryMemoryStore(t, ctx)
	workspacePath := t.TempDir()
	workspaceID := NormalizeTranscriptHistoryWorkspace(workspacePath)

	success, err := PersistSingleRunHistory(ctx, store, SingleRunHistoryInput{
		WorkspacePath: workspacePath,
		SessionID:     "sess-embed-success",
		Records:       []HistoryRecord{testHistoryRecord("embed-success")},
	}, func(context.Context, string) ([]float32, error) {
		return []float32{0.1, 0.2, 0.3}, nil
	})
	if err != nil {
		t.Fatalf("PersistSingleRunHistory(success embed) error = %v", err)
	}
	if len(success.Persisted) != 1 || !success.Persisted[0].Embedded {
		t.Fatalf("success persisted=%+v want embedded record", success.Persisted)
	}
	if got, err := store.GetEmbedding(ctx, success.Persisted[0].Name, workspaceID); err != nil || len(got) != 3 {
		t.Fatalf("GetEmbedding()=(%v,%v) want length 3 nil error", got, err)
	}

	failure, err := PersistSingleRunHistory(ctx, store, SingleRunHistoryInput{
		WorkspacePath: workspacePath,
		SessionID:     "sess-embed-failure",
		Records:       []HistoryRecord{testHistoryRecord("embed-failure")},
	}, func(context.Context, string) ([]float32, error) {
		return nil, errors.New("embed unavailable")
	})
	if err != nil {
		t.Fatalf("PersistSingleRunHistory(failing embed) error = %v", err)
	}
	if len(failure.Persisted) != 1 || failure.Persisted[0].Embedded {
		t.Fatalf("failure persisted=%+v want saved but not embedded", failure.Persisted)
	}
	if _, err := store.Get(ctx, failure.Persisted[0].Name, workspaceID); err != nil {
		t.Fatalf("Get(failure saved record) error = %v", err)
	}
}

func openHistoryMemoryStore(t *testing.T, ctx context.Context) *memory.Store {
	t.Helper()
	store, err := memory.Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("memory.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func testHistoryRecord(seed string) HistoryRecord {
	return HistoryRecord{
		RecordID:       "record-" + seed,
		Kind:           HistoryRecordKindInsight,
		Summary:        "Summary for " + seed,
		RetrievalText:  "Retrieval text for " + seed,
		Confidence:     0.8,
		NormalizedHash: "sha256:" + strings.Repeat("a", 63) + seed[:1],
	}
}

func stablePathSegment(seed string) string {
	segment := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		default:
			return '-'
		}
	}, seed)
	segment = strings.Trim(segment, "-")
	if segment == "" {
		return "empty"
	}
	if len(segment) > 32 {
		return segment[:32]
	}
	return segment
}
