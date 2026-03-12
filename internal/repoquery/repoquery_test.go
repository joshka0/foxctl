package repoquery

import (
	"testing"

	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
)

func TestRerankSearchNodes_PrioritizesInfraNodesForMatchingQueries(t *testing.T) {
	t.Parallel()

	scored := []repoindex.ScoredNode{
		{
			Node:  repoindex.Node{ID: "repo::kw:shell_runner", Kind: repoindex.NodeConcept, Name: "shell_runner"},
			Score: -5.0,
		},
		{
			Node:  repoindex.Node{ID: "repo::res:deployment:default/api", Kind: repoindex.NodeConcept, Name: "Deployment/default/api"},
			Score: -20.0,
		},
		{
			Node:  repoindex.Node{ID: "repo::res:resource:aws_s3_bucket.app", Kind: repoindex.NodeConcept, Name: "resource aws_s3_bucket.app"},
			Score: -20.0,
		},
	}

	got := rerankSearchNodes("terraform kubernetes shell", scored)
	if len(got) != 3 {
		t.Fatalf("len(got)=%d want 3", len(got))
	}
	if got[0].Node.Name != "Deployment/default/api" && got[0].Node.Name != "resource aws_s3_bucket.app" {
		t.Fatalf("top node=%q want infra concept", got[0].Node.Name)
	}
}

func TestDetectInfraTopics(t *testing.T) {
	t.Parallel()

	topics := detectInfraTopics("terraform kubernetes shell command")
	for _, key := range []string{"terraform", "kubernetes", "shell"} {
		if !topics[key] {
			t.Fatalf("expected topic %q in %#v", key, topics)
		}
	}
}
