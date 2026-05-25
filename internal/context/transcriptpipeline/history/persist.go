package history

import (
	"context"
	"strings"

	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage"
)

type SingleRunHistoryInput struct {
	WorkspacePath  string
	SessionID      string
	ConversationID string
	Records        []HistoryRecord
}

type GroupRunHistoryInput struct {
	WorkspacePath      string
	GroupID            string
	SessionIDs         []string
	MainlineSessionIDs []string
	Records            []HistoryRecord
}

type PersistRunReport struct {
	Persisted []PersistedHistoryRecord
	Removed   []string
}

func PersistSingleRunHistory(ctx context.Context, store storage.MemoryStore, input SingleRunHistoryInput, embed HistoryRecordEmbedFunc) (PersistRunReport, error) {
	if store == nil || len(input.Records) == 0 {
		return PersistRunReport{}, nil
	}
	ownerID := singleRunHistoryOwnerID(input)
	return persistRunHistory(ctx, store, NormalizeTranscriptHistoryWorkspace(input.WorkspacePath), ownerID, strings.TrimSpace(input.SessionID), input.Records, embed)
}

func PersistGroupedRunHistory(ctx context.Context, store storage.MemoryStore, groups []GroupRunHistoryInput, embed HistoryRecordEmbedFunc) ([]PersistRunReport, error) {
	if store == nil || len(groups) == 0 {
		return nil, nil
	}
	reports := make([]PersistRunReport, len(groups))
	for idx := range groups {
		input := groups[idx]
		if len(input.Records) == 0 {
			continue
		}
		report, err := persistRunHistory(ctx, store, NormalizeTranscriptHistoryWorkspace(input.WorkspacePath), groupRunHistoryOwnerID(input), groupRunHistorySessionID(input), input.Records, embed)
		if err != nil {
			return nil, err
		}
		reports[idx] = report
	}
	return reports, nil
}

func NormalizeTranscriptHistoryWorkspace(workspacePath string) string {
	target := strings.TrimSpace(workspacePath)
	if target == "" {
		target = workspace.Detect("")
	} else {
		target = workspace.Detect(target)
	}
	return workspace.Normalize(target)
}

func persistRunHistory(ctx context.Context, store storage.MemoryStore, workspaceID, ownerID, sessionID string, records []HistoryRecord, embed HistoryRecordEmbedFunc) (PersistRunReport, error) {
	persisted, err := PersistHistoryRecords(ctx, store, workspaceID, ownerID, sessionID, records, embed)
	if err != nil {
		return PersistRunReport{}, err
	}
	removed, err := ReconcileHistoryRecordPrefix(ctx, store, workspaceID, TranscriptHistoryPrefix(ownerID), persisted)
	if err != nil {
		return PersistRunReport{}, err
	}
	return PersistRunReport{Persisted: persisted, Removed: removed}, nil
}

func singleRunHistoryOwnerID(input SingleRunHistoryInput) string {
	if ownerID := strings.TrimSpace(input.SessionID); ownerID != "" {
		return ownerID
	}
	return strings.TrimSpace(input.ConversationID)
}

func groupRunHistoryOwnerID(input GroupRunHistoryInput) string {
	if ownerID := strings.TrimSpace(input.GroupID); ownerID != "" {
		return ownerID
	}
	return firstNonEmpty(input.SessionIDs...)
}

func groupRunHistorySessionID(input GroupRunHistoryInput) string {
	if sessionID := firstNonEmpty(input.MainlineSessionIDs...); sessionID != "" {
		return sessionID
	}
	return firstNonEmpty(input.SessionIDs...)
}
