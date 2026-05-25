package cmd

import (
	"context"

	"github.com/joshka0/foxctl/internal/context/transcriptpipeline"
	historypkg "github.com/joshka0/foxctl/internal/context/transcriptpipeline/history"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/platform/config"
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
	report, err := historypkg.PersistSingleRunHistory(ctx, store, historypkg.SingleRunHistoryInput{
		WorkspacePath:  result.Parsed.WorkspacePath,
		SessionID:      result.Parsed.SessionID,
		ConversationID: result.ConversationID,
		Records:        result.HistoryRecords,
	}, embed)
	if err != nil {
		return err
	}
	result.PersistedHistory = report.Persisted
	result.RemovedHistory = report.Removed
	return nil
}

func persistGroupedHistoryRecords(ctx context.Context, store *memorystore.Store, result *transcriptpipeline.GroupRunResult, embed historypkg.HistoryRecordEmbedFunc) error {
	if store == nil || result == nil {
		return nil
	}
	groups := make([]historypkg.GroupRunHistoryInput, len(result.Groups))
	for idx := range result.Groups {
		item := &result.Groups[idx]
		groups[idx] = historypkg.GroupRunHistoryInput{
			WorkspacePath:      item.WorkspacePath,
			GroupID:            item.GroupID,
			SessionIDs:         item.SessionIDs,
			MainlineSessionIDs: item.MainlineSessionIDs,
			Records:            item.HistoryRecords,
		}
	}
	reports, err := historypkg.PersistGroupedRunHistory(ctx, store, groups, embed)
	if err != nil {
		return err
	}
	for idx := range reports {
		result.Groups[idx].PersistedHistory = reports[idx].Persisted
		result.Groups[idx].RemovedHistory = reports[idx].Removed
	}
	return nil
}
