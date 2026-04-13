package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/intelligence/searchindex"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

type memoryListByTypeSource struct {
	store storage.MemoryStore
}

func (s memoryListByTypeSource) ListByType(ctx context.Context, workspaceID, entryType string, limit int) ([]storage.NamedEntry, error) {
	if limit > 0 {
		entries, _, err := s.store.ListFiltered(ctx, workspaceID, storage.MemoryListFilter{Types: []string{entryType}}, limit, 0)
		return entries, err
	}
	var out []storage.NamedEntry
	offset := 0
	for {
		page, total, err := s.store.ListFiltered(ctx, workspaceID, storage.MemoryListFilter{Types: []string{entryType}}, 200, offset)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		offset += len(page)
		if len(page) == 0 || offset >= total {
			break
		}
	}
	return out, nil
}

func main() {
	ctx := context.Background()
	config.LoadDotEnv()

	workspace, err := filepath.Abs(".")
	if err != nil {
		panic(err)
	}

	cfg, err := config.Load(ctx)
	if err != nil {
		panic(err)
	}

	memStore, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		panic(err)
	}
	defer memStore.Close()

	indexStore, err := searchindex.Open(ctx, cfg.Storage.Root)
	if err != nil {
		panic(err)
	}
	defer indexStore.Close()

	var embedder semantic.EmbeddingProvider
	if key := strings.TrimSpace(os.Getenv("VOYAGE_API_KEY")); key != "" {
		provider, err := semantic.NewProviderForScope(semantic.ScopeSymbols, cfg, semantic.WithVoyageKey(key))
		if err == nil {
			embedder = provider
		} else {
			panic(err)
		}
	}
	if embedder == nil {
		panic("VOYAGE_API_KEY missing or provider unavailable")
	}

	if err := indexStore.DeleteWorkspace(ctx, workspace); err != nil {
		panic(err)
	}
	result, err := searchindex.BuildCodeDocuments(ctx, memoryListByTypeSource{store: memStore}, indexStore, workspace, searchindex.BuildCodeOptions{
		Limit:          0,
		EmbedProvider:  embedder,
		EmbedBatchSize: 32,
		Progress: func(p searchindex.BuildProgress) {
			fmt.Fprintf(os.Stderr, "searchindex batch stage=%s batch=%d/%d docs=%d embedded=%d errors=%d\n",
				p.Stage, p.Batch, p.TotalBatches, p.Docs, p.Embedded, p.Errors)
		},
	})
	if err != nil {
		panic(err)
	}
	persisted, err := indexStore.CountWorkspace(ctx, workspace)
	if err != nil {
		panic(err)
	}
	if result.Upserted > 0 && persisted == 0 {
		panic(fmt.Sprintf("searchindex verification failed: upserted=%d but persisted rows=0 for workspace %s", result.Upserted, workspace))
	}

	fmt.Printf("workspace=%s symbols=%d files=%d upserted=%d persisted=%d skipped=%d errors=%d model=%s\n",
		workspace, result.SymbolBuilt, result.FileBuilt, result.Upserted, persisted, result.Skipped, result.Errors, embedder.Model())
}
