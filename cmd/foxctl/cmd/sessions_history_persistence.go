package cmd

import (
	"context"
	"strings"

	"github.com/joshka0/foxctl/internal/context/transcriptpipeline"
	historypkg "github.com/joshka0/foxctl/internal/context/transcriptpipeline/history"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/platform/config"
	workspaceutil "github.com/joshka0/foxctl/internal/platform/workspace"
	memorystore "github.com/joshka0/foxctl/internal/storage/memory"
)

func buildHistoryRecordEmbedder(cfg config.Config) historypkg.HistoryRecordEmbedFunc {
	embedder, err := semantic.NewEmbedderFromConfig(semantic.ScopeMemory, cfg)
	if err != nil {
		return nil
	}
	return func(ctx context.Context, text string) ([]float32, error) {
		result, err := embedder.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		return result.Vec, nil
	}
}

func persistSingleHistoryRecords(ctx context.Context, store *memorystore.Store, result *transcriptpipeline.SingleRunResult, embed historypkg.HistoryRecordEmbedFunc) error {
	if store == nil || result == nil {
		return nil
	}
	workspaceID := normalizeTranscriptHistoryWorkspace(result.Parsed.WorkspacePath)
	ownerID := strings.TrimSpace(result.Parsed.SessionID)
	if ownerID == "" {
		ownerID = strings.TrimSpace(result.ConversationID)
	}
	persisted, err := historypkg.PersistHistoryRecords(ctx, store, workspaceID, ownerID, result.Parsed.SessionID, result.HistoryRecords, embed)
	if err != nil {
		return err
	}
	removed, err := historypkg.ReconcileHistoryRecordPrefix(ctx, store, workspaceID, historypkg.TranscriptHistoryPrefix(ownerID), persisted)
	if err != nil {
		return err
	}
	result.PersistedHistory = persisted
	result.RemovedHistory = removed
	return nil
}

func persistGroupedHistoryRecords(ctx context.Context, store *memorystore.Store, result *transcriptpipeline.GroupRunResult, embed historypkg.HistoryRecordEmbedFunc) error {
	if store == nil || result == nil {
		return nil
	}
	for idx := range result.Groups {
		item := &result.Groups[idx]
		workspaceID := normalizeTranscriptHistoryWorkspace(item.WorkspacePath)
		ownerID := strings.TrimSpace(item.GroupID)
		if ownerID == "" && len(item.SessionIDs) > 0 {
			ownerID = strings.TrimSpace(item.SessionIDs[0])
		}
		sessionID := ""
		if len(item.MainlineSessionIDs) > 0 {
			sessionID = strings.TrimSpace(item.MainlineSessionIDs[0])
		} else if len(item.SessionIDs) > 0 {
			sessionID = strings.TrimSpace(item.SessionIDs[0])
		}
		persisted, err := historypkg.PersistHistoryRecords(ctx, store, workspaceID, ownerID, sessionID, item.HistoryRecords, embed)
		if err != nil {
			return err
		}
		removed, err := historypkg.ReconcileHistoryRecordPrefix(ctx, store, workspaceID, historypkg.TranscriptHistoryPrefix(ownerID), persisted)
		if err != nil {
			return err
		}
		item.PersistedHistory = persisted
		item.RemovedHistory = removed
	}
	return nil
}

func normalizeTranscriptHistoryWorkspace(workspacePath string) string {
	target := strings.TrimSpace(workspacePath)
	if target == "" {
		target = workspaceutil.Detect("")
	} else {
		target = workspaceutil.Detect(target)
	}
	return workspaceutil.Normalize(target)
}
