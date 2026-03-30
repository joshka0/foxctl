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

func TestRerankSearchNodes_PrioritizesTerraformDataForDataQueries(t *testing.T) {
	t.Parallel()

	scored := []repoindex.ScoredNode{
		{
			Node: repoindex.Node{
				ID:   "repo::res:tf:root:resource:aws_ssoadmin_permission_set.example",
				Kind: repoindex.NodeConcept,
				Pkg:  "tf:root",
				File: "modules/aws-project/main.tf",
				Name: "resource aws_ssoadmin_permission_set.example",
			},
			Score: -20.0,
		},
		{
			Node: repoindex.Node{
				ID:   "repo::res:tf:root:data:aws_ssoadmin_instances.current",
				Kind: repoindex.NodeConcept,
				Pkg:  "tf:root",
				File: "data.tf",
				Name: "data aws_ssoadmin_instances.current",
			},
			Score: -5.0,
		},
	}

	got := rerankSearchNodes("aws ssoadmin instances data source", scored)
	if len(got) != 2 {
		t.Fatalf("len(got)=%d want 2", len(got))
	}
	if got[0].Node.Name != "data aws_ssoadmin_instances.current" {
		t.Fatalf("top node=%q want data source", got[0].Node.Name)
	}
}

func TestRerankSearchNodes_PrioritizesRealModulesOverFixtures(t *testing.T) {
	t.Parallel()

	scored := []repoindex.ScoredNode{
		{
			Node: repoindex.Node{
				ID:   "repo::res:tf:test/fixtures/service-connection:module:service_connection",
				Kind: repoindex.NodeConcept,
				Pkg:  "tf:test/fixtures/service-connection",
				File: "test/fixtures/service-connection/main.tf",
				Name: "module service_connection",
			},
			Score: -5.0,
		},
		{
			Node: repoindex.Node{
				ID:   "repo::res:tf:modules/service-connection:module:service_connection",
				Kind: repoindex.NodeConcept,
				Pkg:  "tf:modules/service-connection",
				File: "modules/service-connection/main.tf",
				Name: "module service_connection",
			},
			Score: -6.0,
		},
	}

	got := rerankSearchNodes("service connection module path", scored)
	if len(got) != 2 {
		t.Fatalf("len(got)=%d want 2", len(got))
	}
	if got[0].Node.File != "modules/service-connection/main.tf" {
		t.Fatalf("top file=%q want real module path", got[0].Node.File)
	}
}

func TestNormalizeNaturalQuery(t *testing.T) {
	t.Parallel()

	got := NormalizeNaturalQuery("internal/ composite\\function\nentrypoint\trefactor")
	if got != "internal composite function entrypoint refactor" {
		t.Fatalf("got %q", got)
	}
}

func TestNewSearchRequestSanitizesNaturalQuery(t *testing.T) {
	t.Parallel()

	req, err := NewSearchRequest("internal/ actor/agent", 5)
	if err != nil {
		t.Fatalf("NewSearchRequest error = %v", err)
	}
	if req.Query != "internal actor agent" {
		t.Fatalf("query=%q", req.Query)
	}
}

func TestNewDAGGrepRequestSanitizesNaturalQuery(t *testing.T) {
	t.Parallel()

	req, err := NewDAGGrepRequest("internal/ composite function", "", 0, nil, nil, nil, "", 0, 0, 0, nil, "")
	if err != nil {
		t.Fatalf("NewDAGGrepRequest error = %v", err)
	}
	if req.Query != "internal composite function" {
		t.Fatalf("query=%q", req.Query)
	}
}

func TestParseEdgeTypesAcceptsNaturalAliases(t *testing.T) {
	t.Parallel()

	got, err := ParseEdgeTypes([]string{"references", "defines"})
	if err != nil {
		t.Fatalf("ParseEdgeTypes error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got)=%d", len(got))
	}
	if got[0] != repoindex.EdgeRefersTo || got[1] != repoindex.EdgeContains {
		t.Fatalf("got %#v", got)
	}
}
