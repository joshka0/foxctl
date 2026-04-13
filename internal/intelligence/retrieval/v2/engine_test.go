package retrievalv2

import (
	"context"
	"errors"
	"testing"

	"github.com/jkatigb/agentctl/internal/intelligence/searchindex"
)

type fakeIndex struct {
	exact          []searchindex.SearchHit
	lexicalQuery   string
	vectorQueryLen int

	lexical []searchindex.SearchHit
	vector  []searchindex.SearchHit

	exactErr   error
	lexicalErr error
	vectorErr  error
}

func (f *fakeIndex) ExactRecall(_ context.Context, workspaceID, query string, opts searchindex.ExactRecallOptions) ([]searchindex.SearchHit, error) {
	if f.exactErr != nil {
		return nil, f.exactErr
	}
	if opts.Limit == 0 {
		return nil, nil
	}
	if workspaceID == "" {
		return nil, errors.New("workspace missing")
	}
	return append([]searchindex.SearchHit(nil), f.exact...), nil
}

func (f *fakeIndex) LexicalRecall(_ context.Context, workspaceID, query string, opts searchindex.RecallOptions) ([]searchindex.SearchHit, error) {
	f.lexicalQuery = query
	if f.lexicalErr != nil {
		return nil, f.lexicalErr
	}
	if opts.Limit == 0 {
		return nil, nil
	}
	if workspaceID == "" {
		return nil, errors.New("workspace missing")
	}
	return append([]searchindex.SearchHit(nil), f.lexical...), nil
}

func (f *fakeIndex) VectorRecall(_ context.Context, workspaceID string, embedding []float32, opts searchindex.VectorRecallOptions) ([]searchindex.SearchHit, error) {
	f.vectorQueryLen = len(embedding)
	if f.vectorErr != nil {
		return nil, f.vectorErr
	}
	if opts.Limit == 0 {
		return nil, nil
	}
	if workspaceID == "" {
		return nil, errors.New("workspace missing")
	}
	return append([]searchindex.SearchHit(nil), f.vector...), nil
}

type fakeEmbedder struct {
	embedCalls int
	queryCalls int
	err        error
}

func (f *fakeEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	f.embedCalls++
	return []float32{0.2, 0.3}, f.err
}

func (f *fakeEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	return nil, f.err
}

func (f *fakeEmbedder) Model() string {
	return "fake"
}

func (f *fakeEmbedder) Dimensions() int {
	return 2
}

func (f *fakeEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	f.queryCalls++
	return []float32{0.2, 0.3}, f.err
}

func TestSearch_UsesLexicalAndVectorSources(t *testing.T) {
	idx := &fakeIndex{
		lexical: []searchindex.SearchHit{
			{Doc: searchindex.Document{ID: "symbol:user", Path: "auth/user.go", GroupKey: "auth/user.go", Kind: searchindex.KindFile, Title: "user"}, Score: 0.7},
		},
		vector: []searchindex.SearchHit{
			{Doc: searchindex.Document{ID: "symbol:user", Path: "auth/user.go", GroupKey: "auth/user.go", Kind: searchindex.KindFile, Title: "user"}, Score: 0.6},
			{Doc: searchindex.Document{ID: "symbol:login", Path: "auth/login.go", GroupKey: "auth/login.go", Kind: searchindex.KindFile, Title: "login"}, Score: 0.9},
		},
	}
	embed := &fakeEmbedder{}
	engine := NewEngine(idx, embed)

	response, err := engine.Search(context.Background(), SearchRequest{
		WorkspaceID: "ws",
		Query:       `path:"auth/user.go" login`,
		Group:       DefaultGroupOptions(),
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if response.Query == "" {
		t.Fatalf("query missing from response")
	}
	if !response.Embedded {
		t.Fatalf("expected query embedding to be used")
	}
	if response.Hits == nil || len(response.Hits) != 2 {
		t.Fatalf("expected 2 fused hits, got %d", len(response.Hits))
	}
	if embed.queryCalls != 1 {
		t.Fatalf("expected EmbedQuery called once, got %d", embed.queryCalls)
	}
	if response.Hits[0].Document.Path != "auth/user.go" {
		t.Fatalf("expected grouped user hit first, got %q", response.Hits[0].Document.Path)
	}

	if len(response.Groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(response.Groups))
	}
	if response.Groups[0].Path != "auth/user.go" {
		t.Fatalf("expected top group path auth/user.go, got %q", response.Groups[0].Path)
	}
}

func TestSearch_FallsBackToLexicalWhenVectorFails(t *testing.T) {
	errVector := errors.New("vector down")
	idx := &fakeIndex{
		lexical: []searchindex.SearchHit{
			{Doc: searchindex.Document{ID: "symbol:user", Path: "auth/user.go", GroupKey: "auth/user.go", Kind: searchindex.KindFile, Title: "user"}, Score: 0.8},
		},
		vector:    nil,
		vectorErr: errVector,
	}
	embed := &fakeEmbedder{}
	engine := NewEngine(idx, embed)
	resp, err := engine.Search(context.Background(), SearchRequest{
		WorkspaceID: "ws",
		Query:       "login",
		Sources:     SearchSourcesConfig{EnableLexical: true, EnableVector: true},
		MaxResults:  10,
	})
	if err != nil {
		t.Fatalf("search returned error: %v", err)
	}
	if resp.Hits[0].Document.Path != "auth/user.go" {
		t.Fatalf("expected lexical fallback hit, got %q", resp.Hits[0].Document.Path)
	}
	if resp.Stats.Sources[SourceVector].Err == nil {
		t.Fatalf("expected vector source error in stats")
	}
	if !errors.Is(resp.Stats.Sources[SourceVector].Err, errVector) {
		t.Fatalf("unexpected vector error: %v", resp.Stats.Sources[SourceVector].Err)
	}
}

func TestSearchRejectsEmptyQueryOrWorkspace(t *testing.T) {
	engine := NewEngine(&fakeIndex{}, &fakeEmbedder{})
	if _, err := engine.Search(context.Background(), SearchRequest{WorkspaceID: "ws", Query: ""}); err == nil {
		t.Fatal("expected empty query error")
	}
	if _, err := engine.Search(context.Background(), SearchRequest{WorkspaceID: "", Query: "x"}); err == nil {
		t.Fatal("expected missing workspace error")
	}
}

func TestShouldRunRepoIndex(t *testing.T) {
	plan := ParseQuery("searchSymbolsWithRetrieval")
	if shouldRunRepoIndex(plan, map[SourceID][]SourceHit{
		SourceLexical: {{ID: "a", Score: 1}},
	}) {
		t.Fatal("expected repoindex disabled when identifier query already has lexical hits")
	}

	plan = ParseQuery("call graph for repo index dag grep")
	if !shouldRunRepoIndex(plan, map[SourceID][]SourceHit{
		SourceLexical: {{ID: "a", Score: 0.1}},
	}) {
		t.Fatal("expected repoindex enabled for sparse structural query")
	}
}

func TestApplyFeatureBoosts(t *testing.T) {
	plan := ParseQuery("searchSymbolsWithRetrieval")
	hits := []FusedHit{
		{Document: searchindex.Document{Path: "a.go", SymbolName: "Other"}, Score: 0.5},
		{Document: searchindex.Document{Path: "b.go", SymbolName: "searchSymbolsWithRetrieval"}, Score: 0.4},
	}
	out := applyFeatureBoosts(plan, hits)
	if out[0].Document.Path != "b.go" {
		t.Fatalf("expected exact identifier match to rise to top, got %q", out[0].Document.Path)
	}
}

func TestTuneFuseForPlan_StructuralBoostsRepoIndex(t *testing.T) {
	plan := ParseQuery("repo index dag grep")
	opts := DefaultFuseOptions()
	tuned := tuneFuseForPlan(plan, opts)
	if tuned.SourceWeights[SourceRepoIndex] <= tuned.SourceWeights[SourceExact] {
		t.Fatalf("expected structural query to favor repo index over exact, got repo=%v exact=%v", tuned.SourceWeights[SourceRepoIndex], tuned.SourceWeights[SourceExact])
	}
}

func TestApplyFeatureBoosts_StructuralQueryPrefersRepoIndexSignals(t *testing.T) {
	plan := ParseQuery("repo index dag grep")
	hits := []FusedHit{
		{
			Document: searchindex.Document{Path: "internal/runtime/orchestration/workflow/dag.go", Title: "DAG"},
			Score:    0.5,
			SourceScores: map[SourceID]float64{
				SourceExact: 0.5,
			},
		},
		{
			Document: searchindex.Document{Path: "internal/intelligence/indexing/repoindex/dag_grep.go", Title: "dag_grep"},
			Score:    0.45,
			SourceScores: map[SourceID]float64{
				SourceRepoIndex: 0.45,
			},
		},
	}
	out := applyFeatureBoosts(plan, hits)
	if out[0].Document.Path != "internal/intelligence/indexing/repoindex/dag_grep.go" {
		t.Fatalf("expected structural repo hit first, got %q", out[0].Document.Path)
	}
}

func TestSearch_UsesExactSourceForIdentifierQuery(t *testing.T) {
	idx := &fakeIndex{
		exact: []searchindex.SearchHit{
			{Doc: searchindex.Document{ID: "symbol:exact", Path: "exact.go", SymbolName: "searchSymbolsWithRetrieval", Kind: searchindex.KindSymbol}, Score: 1.0},
		},
		vector: []searchindex.SearchHit{
			{Doc: searchindex.Document{ID: "symbol:vec", Path: "vec.go", SymbolName: "Other", Kind: searchindex.KindSymbol}, Score: 0.9},
		},
	}
	engine := NewEngine(idx, &fakeEmbedder{})
	resp, err := engine.Search(context.Background(), SearchRequest{WorkspaceID: "ws", Query: "searchSymbolsWithRetrieval"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(resp.Hits) == 0 || resp.Hits[0].Document.Path != "exact.go" {
		t.Fatalf("expected exact hit first, got %#v", resp.Hits)
	}
}
