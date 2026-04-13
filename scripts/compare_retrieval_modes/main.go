package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	retrievalv2 "github.com/jkatigb/agentctl/internal/intelligence/retrieval/v2"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/repoquery"
	"github.com/jkatigb/agentctl/internal/searchindex"
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
	query := "searchSymbolsWithRetrieval"
	if len(os.Args) > 1 && strings.TrimSpace(os.Args[1]) != "" {
		query = os.Args[1]
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
		}
	}

	if os.Getenv("SEARCHINDEX_SKIP_REINDEX") != "1" {
		if err := indexStore.DeleteWorkspace(ctx, workspace); err != nil {
			panic(err)
		}
		if _, err := searchindex.BuildCodeDocuments(ctx, memoryListByTypeSource{store: memStore}, indexStore, workspace, searchindex.BuildCodeOptions{
			Limit:         0,
			EmbedProvider: embedder,
		}); err != nil {
			panic(err)
		}
	}

	var repoSvc *repoquery.QueryService
	if repoStore, err := repoindex.Open(ctx, cfg.Storage.Root, workspace); err == nil {
		defer repoStore.Close()
		repoSvc = repoquery.NewQueryService(repoindex.NewQueryEngine(repoStore))
	}

	for _, mode := range []string{"off", "search", "dag", "auto"} {
		engine := retrievalv2.NewEngine(indexStore, embedder)
		if repoSvc != nil {
			engine = engine.WithRepoQueryService(repoSvc)
		}

		req := retrievalv2.DefaultSearchRequest(workspace, query)
		req.MaxResults = 8
		req.Sources.EnableLexical = true
		req.Sources.EnableVector = embedder != nil
		req.Sources.EnableRepoIndex = mode != "off"
		req.Sources.RepoIndexMode = mode

		resp, err := engine.Search(ctx, req)
		if err != nil {
			fmt.Printf("mode=%s error=%v\n", mode, err)
			continue
		}

		fmt.Printf("mode=%s total_groups=%d sources=%v\n", mode, len(resp.Groups), summarizeSources(resp))
		for i, group := range resp.Groups {
			if i >= 5 {
				break
			}
			anchorDesc := ""
			if len(group.Anchors) > 0 {
				anchorDesc = fmt.Sprintf(" anchor=%s line=%d source=%s", group.Anchors[0].SymbolID, group.Anchors[0].Anchor.StartLine, group.Anchors[0].Source)
			}
			fmt.Printf("  %d. %s score=%.4f%s\n", i+1, group.Path, group.Score, anchorDesc)
		}
	}
}

func summarizeSources(resp retrievalv2.SearchResponse) map[string]int {
	out := map[string]int{}
	for source, stats := range resp.Stats.Sources {
		out[string(source)] = stats.Returned
	}
	return out
}
