package autoselect

import (
	"context"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/codecontext"
	ccadapt "github.com/jkatigb/agentctl/internal/codecontext/adapters"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	sysconfig "github.com/jkatigb/agentctl/internal/platform/config"
	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/repoquery"
	retrievalv2 "github.com/jkatigb/agentctl/internal/retrieval/v2"
	"github.com/jkatigb/agentctl/internal/searchindex"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

// Options configures shared file auto-selection for read/search/analyze skills.
type Options struct {
	WorkspacePath string
	WorkspaceID   string
	Query         string
	MaxFiles      int
	RepoIndexMode string
	EmbedProvider semantic.EmbeddingProvider
}

// Match describes one ranked file candidate selected for downstream codecontext use.
type Match struct {
	Path     string
	SymbolID string
	Score    float64
	Source   string
}

// Result contains codecontext candidates plus presentation-friendly match details.
type Result struct {
	WorkspaceID string
	Candidates  []codecontext.Candidate
	Matches     []Match
}

// Select chooses relevant files using the shared retrieval-v2 path.
func Select(ctx context.Context, cfg sysconfig.Config, opts Options) (*Result, error) {
	workspacePath := ws.Normalize(opts.WorkspacePath)
	if workspacePath == "" {
		return nil, fmt.Errorf("workspace path is required")
	}
	workspaceID := resolveWorkspaceID(workspacePath, opts.WorkspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	if strings.TrimSpace(opts.Query) == "" {
		return nil, fmt.Errorf("query is required")
	}
	if opts.MaxFiles <= 0 {
		return nil, fmt.Errorf("max files must be > 0")
	}

	indexStore, cleanup, err := searchindex.OpenEphemeral(ctx, cfg.Storage.Root)
	if err != nil {
		return nil, fmt.Errorf("open ephemeral search index: %w", err)
	}
	defer func() { _ = cleanup() }()

	memStore, err := memory.OpenWithConfig(ctx, cfg)
	if err == nil {
		defer memStore.Close()
		if _, err := searchindex.BuildCodeDocuments(ctx, memoryListByTypeSource{store: memStore}, indexStore, workspaceID, searchindex.BuildCodeOptions{
			Limit:         opts.MaxFiles * 10,
			EmbedProvider: opts.EmbedProvider,
		}); err != nil {
			return nil, fmt.Errorf("build code search documents: %w", err)
		}
	}

	engine := retrievalv2.NewEngine(indexStore, opts.EmbedProvider)
	if repoStore, err := repoindex.Open(ctx, cfg.Storage.Root, workspacePath); err == nil {
		defer repoStore.Close()
		engine = engine.WithRepoQueryService(repoquery.NewQueryService(repoindex.NewQueryEngine(repoStore)))
	}

	repoMode := NormalizeRepoIndexMode(opts.RepoIndexMode)
	req := retrievalv2.DefaultSearchRequest(workspaceID, opts.Query)
	req.MaxResults = opts.MaxFiles
	req.Sources.EnableLexical = true
	req.Sources.EnableVector = opts.EmbedProvider != nil
	req.Sources.EnableRepoIndex = repoMode != "off"
	req.Sources.RepoIndexMode = repoMode

	resp, err := engine.Search(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("search retrieval v2: %w", err)
	}

	out := &Result{
		WorkspaceID: workspaceID,
		Candidates:  ccadapt.GroupsToCandidates(groupsToAdapterGroups(resp.Groups)),
		Matches:     make([]Match, 0, len(resp.Groups)),
	}
	for _, g := range resp.Groups {
		source := ""
		symbolID := ""
		if len(g.Anchors) > 0 {
			source = string(g.Anchors[0].Source)
			symbolID = g.Anchors[0].SymbolID
		}
		out.Matches = append(out.Matches, Match{
			Path:     g.Path,
			SymbolID: symbolID,
			Score:    g.Score,
			Source:   source,
		})
	}
	return out, nil
}

func groupsToAdapterGroups(groups []retrievalv2.Group) []ccadapt.Group {
	out := make([]ccadapt.Group, 0, len(groups))
	for _, group := range groups {
		converted := ccadapt.Group{
			Path:    group.Path,
			Score:   group.Score,
			Summary: group.Summary,
			Anchors: make([]ccadapt.AnchorHit, 0, len(group.Anchors)),
		}
		for _, anchor := range group.Anchors {
			converted.Anchors = append(converted.Anchors, ccadapt.AnchorHit{
				Anchor:     anchor.Anchor,
				Score:      anchor.Score,
				Source:     string(anchor.Source),
				SymbolID:   anchor.SymbolID,
				SymbolName: anchor.SymbolName,
			})
		}
		out = append(out, converted)
	}
	return out
}

// NormalizeRepoIndexMode canonicalizes repo-index mode values used by the retrieval skills.
func NormalizeRepoIndexMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "auto":
		return "auto"
	case "search":
		return "search"
	case "dag", "dag_grep", "repo_index_dag":
		return "dag"
	case "off", "none", "disabled":
		return "off"
	default:
		return "auto"
	}
}

func resolveWorkspaceID(workspacePath, override string) string {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		return trimmed
	}
	return ws.ID(workspacePath)
}

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
