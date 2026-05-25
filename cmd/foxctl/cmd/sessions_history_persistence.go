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
	persisted, err := historypkg.PersistSingleInsightHistory(ctx, store, historypkg.SingleInsightHistory{
		WorkspacePath:  result.Parsed.WorkspacePath,
		SessionID:      result.Parsed.SessionID,
		ConversationID: result.ConversationID,
		Records:        result.HistoryRecords,
	}, embed)
	if err != nil {
		return err
	}
	result.PersistedHistory = persisted.Persisted
	result.RemovedHistory = persisted.Removed
	return nil
}

func persistGroupedHistoryRecords(ctx context.Context, store *memorystore.Store, result *transcriptpipeline.GroupRunResult, embed historypkg.HistoryRecordEmbedFunc) error {
	if store == nil || result == nil {
		return nil
	}
	input := historypkg.GroupedInsightHistory{Groups: make([]historypkg.GroupedInsightHistoryItem, 0, len(result.Groups))}
	for idx := range result.Groups {
		item := &result.Groups[idx]
		input.Groups = append(input.Groups, historypkg.GroupedInsightHistoryItem{
			GroupID:            item.GroupID,
			WorkspacePath:      item.WorkspacePath,
			SessionIDs:         item.SessionIDs,
			MainlineSessionIDs: item.MainlineSessionIDs,
			Records:            item.HistoryRecords,
		})
	}
	persisted, err := historypkg.PersistGroupedInsightHistory(ctx, store, input, embed)
	if err != nil {
		return err
	}
	for idx := range persisted {
		result.Groups[idx].PersistedHistory = persisted[idx].Persisted
		result.Groups[idx].RemovedHistory = persisted[idx].Removed
	}
	return nil
}
