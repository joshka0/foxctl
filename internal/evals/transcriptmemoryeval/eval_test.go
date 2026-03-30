package transcriptmemoryeval

import (
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/transcriptpipeline"
)

func TestLoadSuite(t *testing.T) {
	suite, err := LoadSuite(filepath.Join("..", "..", "..", "testdata", "evals", "transcriptmemory", "march25-root.yaml"))
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	if suite.Name != "march25-root" {
		t.Fatalf("suite.Name=%q", suite.Name)
	}
	if len(suite.Cases) == 0 {
		t.Fatal("expected cases")
	}
}

func TestClaimMetricsAndForbiddenHits(t *testing.T) {
	actual := []ActualClaim{
		{Text: "Use a classifier layer in the pipeline.", Kind: "architecture", Durability: "durable"},
		{Text: "Avoid brittle text-canonicalization.", Kind: "workflow_rule", Durability: "durable"},
		{Text: "Import shell moved into pipeline package.", Kind: "technical_context", Durability: "durable"},
	}
	expected := []ClaimExpectation{
		{Text: "Use a classifier layer in the pipeline.", Kind: "architecture"},
		{Text: "Avoid brittle text canonicalization.", Kind: "workflow_rule"},
	}
	precision, recall, kindAcc := claimMetrics(actual, expected)
	if precision <= 0.5 {
		t.Fatalf("precision=%.2f want > 0.5", precision)
	}
	if recall != 1.0 {
		t.Fatalf("recall=%.2f want 1.0", recall)
	}
	if kindAcc != 1.0 {
		t.Fatalf("kindAcc=%.2f want 1.0", kindAcc)
	}
	forbidden := []ClaimExpectation{{Text: "Import shell moved into pipeline package."}}
	if hits := forbiddenHits(actual, forbidden); hits != 1 {
		t.Fatalf("forbiddenHits=%d want 1", hits)
	}
}

func TestClaimsFromPersisted_UsesCandidateTypeFirst(t *testing.T) {
	got := claimsFromPersisted([]transcriptpipeline.PersistedMemory{
		{CandidateType: "classified_claim:workflow_rule", Summary: "Avoid brittle text."},
		{CandidateType: "group_topline_claim", Summary: "Hybrid runtime exists."},
	})
	if len(got) != 2 {
		t.Fatalf("claims=%d want 2", len(got))
	}
	if got[0].Kind != "workflow_rule" {
		t.Fatalf("claim[0].kind=%q", got[0].Kind)
	}
	if got[1].Kind != "architecture" {
		t.Fatalf("claim[1].kind=%q", got[1].Kind)
	}
}
