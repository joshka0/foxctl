package retrievalv2

import (
	"context"
	"testing"

	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
)

type fakeRepoQuery struct {
	searchNodes []repoindex.Node
	dagNodes    []repoindex.Node
}

func (f *fakeRepoQuery) Search(_ context.Context, req RepoSearchRequest) ([]repoindex.Node, error) {
	if req.Query == "" {
		return nil, nil
	}
	return append([]repoindex.Node(nil), f.searchNodes...), nil
}

func (f *fakeRepoQuery) DAGGrep(_ context.Context, req RepoDAGGrepRequest) (repoindex.DAGGrepResult, error) {
	var result repoindex.DAGGrepResult
	result.Graph.Nodes = append(result.Graph.Nodes, f.dagNodes...)
	return result, nil
}

func TestRepoIndexSource_RecallSearch(t *testing.T) {
	source := RepoIndexSource{}
	hits, err := source.Recall(context.Background(), SourceCall{
		Query:     "hello",
		Limit:     5,
		RepoQuery: &fakeRepoQuery{searchNodes: []repoindex.Node{{Kind: repoindex.NodeSymbol, File: "a.go", Name: "Hello", SpanStart: 9}}},
	})
	if err != nil {
		t.Fatalf("Recall returned error: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("len(hits) = %d, want 1", len(hits))
	}
	if hits[0].Document.Path != "a.go" {
		t.Fatalf("path = %q", hits[0].Document.Path)
	}
}

func TestRepoIndexSource_RecallDAG(t *testing.T) {
	source := RepoIndexSource{}
	hits, err := source.Recall(context.Background(), SourceCall{
		Query:         "hello",
		Limit:         5,
		RepoQuery:     &fakeRepoQuery{dagNodes: []repoindex.Node{{Kind: repoindex.NodeFile, File: "b.go", SpanStart: 3}}},
		RepoIndexMode: "dag",
	})
	if err != nil {
		t.Fatalf("Recall returned error: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("len(hits) = %d, want 1", len(hits))
	}
	if hits[0].Document.Path != "b.go" {
		t.Fatalf("path = %q", hits[0].Document.Path)
	}
}

func TestRepoIndexSource_AutoPrefersSearchBeforeDAG(t *testing.T) {
	source := RepoIndexSource{}
	hits, err := source.Recall(context.Background(), SourceCall{
		Query:         "repo index dag grep",
		Limit:         5,
		RepoQuery:     &fakeRepoQuery{searchNodes: []repoindex.Node{{Kind: repoindex.NodeSymbol, File: "search.go", Name: "DAGGrepRequest", SpanStart: 12, SpanEnd: 20}}},
		RepoIndexMode: "auto",
	})
	if err != nil {
		t.Fatalf("Recall returned error: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("len(hits) = %d, want 1", len(hits))
	}
	if hits[0].Document.Path != "search.go" {
		t.Fatalf("path = %q", hits[0].Document.Path)
	}
}

func TestRepoIndexSource_AutoFallsBackToDAGWhenSearchProjectsNoAnchors(t *testing.T) {
	source := RepoIndexSource{}
	hits, err := source.Recall(context.Background(), SourceCall{
		Query: "identity center terraform module",
		Limit: 5,
		RepoQuery: &fakeRepoQuery{
			searchNodes: []repoindex.Node{{Kind: repoindex.NodeConcept, Name: "module.identity-center"}},
			dagNodes: []repoindex.Node{
				{Kind: repoindex.NodeConcept, Name: "module.identity-center"},
				{Kind: repoindex.NodeFile, File: "modules/identity-center/main.tf", SpanStart: 1},
			},
		},
		RepoIndexMode: "auto",
	})
	if err != nil {
		t.Fatalf("Recall returned error: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("len(hits) = %d, want 1", len(hits))
	}
	if hits[0].Document.Path != "modules/identity-center/main.tf" {
		t.Fatalf("path = %q", hits[0].Document.Path)
	}
}
