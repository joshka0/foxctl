package history

import (
	"context"
	"strings"

	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage"
)

// SingleInsightHistory is the shared persistence input for one transcript
// insight-lane result.
type SingleInsightHistory struct {
	WorkspacePath  string
	SessionID      string
	ConversationID string
	Records        []HistoryRecord
}

// GroupedInsightHistory is the shared persistence input for grouped transcript
// insight-lane results.
type GroupedInsightHistory struct {
	Groups []GroupedInsightHistoryItem
}

type GroupedInsightHistoryItem struct {
	GroupID            string
	WorkspacePath      string
	SessionIDs         []string
	MainlineSessionIDs []string
	Records            []HistoryRecord
}

type HistoryPersistenceResult struct {
	Persisted []PersistedHistoryRecord
	Removed   []string
}

func PersistSingleInsightHistory(ctx context.Context, store storage.MemoryStore, input SingleInsightHistory, embed HistoryRecordEmbedFunc) (HistoryPersistenceResult, error) {
	if store == nil {
		return HistoryPersistenceResult{}, nil
	}
	workspaceID := normalizeTranscriptHistoryWorkspace(input.WorkspacePath)
	ownerID := singleInsightHistoryOwnerID(input)
	persisted, removed, err := persistInsightHistoryRecords(ctx, store, workspaceID, ownerID, input.SessionID, input.Records, embed)
	if err != nil {
		return HistoryPersistenceResult{}, err
	}
	return HistoryPersistenceResult{Persisted: persisted, Removed: removed}, nil
}

func PersistGroupedInsightHistory(ctx context.Context, store storage.MemoryStore, input GroupedInsightHistory, embed HistoryRecordEmbedFunc) ([]HistoryPersistenceResult, error) {
	if store == nil || len(input.Groups) == 0 {
		return nil, nil
	}
	out := make([]HistoryPersistenceResult, 0, len(input.Groups))
	for _, group := range input.Groups {
		workspaceID := normalizeTranscriptHistoryWorkspace(group.WorkspacePath)
		ownerID := groupedInsightHistoryOwnerID(group)
		sessionID := groupedInsightHistorySessionID(group)
		persisted, removed, err := persistInsightHistoryRecords(ctx, store, workspaceID, ownerID, sessionID, group.Records, embed)
		if err != nil {
			return nil, err
		}
		out = append(out, HistoryPersistenceResult{Persisted: persisted, Removed: removed})
	}
	return out, nil
}

func singleInsightHistoryOwnerID(input SingleInsightHistory) string {
	if ownerID := strings.TrimSpace(input.SessionID); ownerID != "" {
		return ownerID
	}
	return strings.TrimSpace(input.ConversationID)
}

func groupedInsightHistoryOwnerID(input GroupedInsightHistoryItem) string {
	if ownerID := strings.TrimSpace(input.GroupID); ownerID != "" {
		return ownerID
	}
	if len(input.SessionIDs) == 0 {
		return ""
	}
	return strings.TrimSpace(input.SessionIDs[0])
}

func groupedInsightHistorySessionID(input GroupedInsightHistoryItem) string {
	if len(input.MainlineSessionIDs) > 0 {
		if sessionID := strings.TrimSpace(input.MainlineSessionIDs[0]); sessionID != "" {
			return sessionID
		}
	}
	if len(input.SessionIDs) == 0 {
		return ""
	}
	return strings.TrimSpace(input.SessionIDs[0])
}

func normalizeTranscriptHistoryWorkspace(workspacePath string) string {
	target := strings.TrimSpace(workspacePath)
	if target == "" {
		target = workspace.Detect("")
	} else {
		target = workspace.Detect(target)
	}
	return workspace.Normalize(target)
}

func persistInsightHistoryRecords(ctx context.Context, store storage.MemoryStore, workspaceID, ownerID, sessionID string, records []HistoryRecord, embed HistoryRecordEmbedFunc) ([]PersistedHistoryRecord, []string, error) {
	persisted, err := PersistHistoryRecords(ctx, store, workspaceID, ownerID, sessionID, records, embed)
	if err != nil {
		return nil, nil, err
	}
	removed, err := ReconcileHistoryRecordPrefix(ctx, store, workspaceID, TranscriptHistoryPrefix(ownerID), persisted)
	if err != nil {
		return nil, nil, err
	}
	return persisted, removed, nil
}
