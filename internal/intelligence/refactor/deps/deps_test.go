package deps

import (
	"context"
	"testing"

	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/intelligence/repoquery"
	refscope "github.com/jkatigb/agentctl/internal/intelligence/refactor/scope"
	refstatus "github.com/jkatigb/agentctl/internal/intelligence/refactor/status"
)

type fakeSearcher struct {
	output repoquery.SearchOutput
	err    error
}

func (f fakeSearcher) SearchWithProjection(_ context.Context, _ repoquery.SearchRequest) (repoquery.SearchOutput, error) {
	return f.output, f.err
}

func TestBuildRequestUsesExplicitSeeds(t *testing.T) {
	t.Parallel()

	result, err := BuildRequest(context.Background(), nil, Input{
		Scope: refscope.Scope{
			Path:     ".",
			Language: "go",
		},
		Status: refstatus.Status{Mode: refstatus.ModeIndexBacked},
		Seeds:  []string{"repo::sym:a", "repo::sym:a", "repo::sym:b"},
	})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if got, want := len(result.Request.Seeds), 2; got != want {
		t.Fatalf("seed count=%d want %d", got, want)
	}
	if result.Request.Direction != repoindex.DirOut {
		t.Fatalf("direction=%q want %q", result.Request.Direction, repoindex.DirOut)
	}
	if got := repoquery.EdgeTypeValues(result.Request.EdgeTypes); len(got) == 0 || got[0] != string(repoindex.EdgeContains) {
		t.Fatalf("edge types=%v want structural defaults", got)
	}
}

func TestBuildRequestResolvesQuerySeedsWithinScope(t *testing.T) {
	t.Parallel()

	searcher := fakeSearcher{
		output: repoquery.SearchOutput{
			Nodes: []repoindex.Node{
				{ID: "repo::sym:outside", Kind: repoindex.NodeSymbol, File: "pkg/other/file.go", Name: "Outside"},
				{ID: "repo::sym:inside", Kind: repoindex.NodeSymbol, File: "internal/intelligence/refactor/status/status.go", Name: "Evaluate"},
				{ID: "repo::pkg:inside", Kind: repoindex.NodePackage, Pkg: "go:internal/intelligence/refactor/status", Name: "status"},
			},
		},
	}

	result, err := BuildRequest(context.Background(), searcher, Input{
		Scope: refscope.Scope{
			Path:     "internal/intelligence/refactor",
			Language: "go",
		},
		Status:    refstatus.Status{Mode: refstatus.ModeParserOnly, Reasons: []string{"repoindex_head_mismatch"}},
		Query:     "Evaluate",
		SeedLimit: 5,
		EdgeSets:  []string{"structural"},
		Direction: "in",
	})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if got, want := len(result.Request.Seeds), 2; got != want {
		t.Fatalf("seed count=%d want %d", got, want)
	}
	if result.Request.Seeds[0] != "repo::sym:inside" {
		t.Fatalf("first seed=%q want repo::sym:inside", result.Request.Seeds[0])
	}
	if result.Request.Direction != repoindex.DirIn {
		t.Fatalf("direction=%q want %q", result.Request.Direction, repoindex.DirIn)
	}
	if result.SeedQuery == "" {
		t.Fatal("expected normalized seed query")
	}
	if len(result.Reasons) != 1 || result.Reasons[0] != "repoindex_head_mismatch" {
		t.Fatalf("reasons=%v want propagated status reasons", result.Reasons)
	}
}

func TestBuildRequestRejectsAmbiguousSeedInput(t *testing.T) {
	t.Parallel()

	_, err := BuildRequest(context.Background(), fakeSearcher{}, Input{
		Scope: refscope.Scope{Path: ".", Language: "go"},
		Seeds: []string{"repo::sym:a"},
		Query: "Alpha",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := err.(*BuildError); !ok {
		t.Fatalf("error=%T want *BuildError", err)
	}
}
