package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/intelligence/repoquery"
	retrievalv2 "github.com/joshka0/foxctl/internal/intelligence/retrieval/v2"
	"github.com/joshka0/foxctl/internal/intelligence/searchindex"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

type memoryListByTypeSource struct {
	store storage.MemoryStore
}

type repoQueryAdapter struct {
	service *repoquery.QueryService
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

func (a repoQueryAdapter) Search(ctx context.Context, req retrievalv2.RepoSearchRequest) ([]repoindex.Node, error) {
	built, err := repoquery.NewSearchRequest(req.Query, req.Limit)
	if err != nil {
		return nil, err
	}
	return a.service.Search(ctx, built)
}

func (a repoQueryAdapter) DAGGrep(ctx context.Context, req retrievalv2.RepoDAGGrepRequest) (repoindex.DAGGrepResult, error) {
	built, err := repoquery.NewDAGGrepRequest(
		req.Query,
		"",
		req.Limit,
		nil,
		req.EdgeSets,
		nil,
		req.Direction,
		req.Depth,
		req.Budget,
		req.PerNodeCap,
		nil,
		req.Render,
	)
	if err != nil {
		return repoindex.DAGGrepResult{}, err
	}
	return a.service.DAGGrep(ctx, built)
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

	embedder, _ := semantic.NewProviderForScope(
		semantic.ScopeSymbols,
		cfg,
		semantic.WithGeminiKey(os.Getenv("GEMINI_API_KEY")),
	)

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
			engine = engine.WithRepoQueryService(repoQueryAdapter{service: repoSvc})
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
