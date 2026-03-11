package retrievaleval

import (
	"path/filepath"
	"testing"
)

func TestEvaluateModeAndSummarize(t *testing.T) {
	mode := EvaluateMode("lexical",
		[]string{
			"notes/repo/agentctl/index.md",
			"notes/repo/agentctl/packages/cmd-agentctl-cmd.md",
		},
		[]string{"notes/repo/agentctl/packages/cmd-agentctl-cmd.md"},
		2,
		nil,
	)
	if mode.FirstCorrectRank != 2 {
		t.Fatalf("rank=%d want 2", mode.FirstCorrectRank)
	}
	if !mode.HitAt5 || !mode.HitAt10 {
		t.Fatalf("expected hit at 5 and 10")
	}

	results := []QueryResult{
		{
			ID:    "q1",
			Query: "cmd agentctl cmd",
			Modes: map[string]ModeResult{"lexical": mode},
		},
	}
	summaries := Summarize(results, []string{"lexical"})
	if len(summaries) != 1 {
		t.Fatalf("summaries=%d want 1", len(summaries))
	}
	if summaries[0].HitRateAt5 != 1.0 {
		t.Fatalf("hit@5=%.2f want 1.0", summaries[0].HitRateAt5)
	}
	if summaries[0].MeanReciprocalRank != 0.5 {
		t.Fatalf("mrr=%.2f want 0.5", summaries[0].MeanReciprocalRank)
	}
}

func TestLoadSuite(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "evals", "retrieval", "agentctl.yaml")
	suite, err := LoadSuite(path)
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	if suite.Name != "agentctl" {
		t.Fatalf("name=%q", suite.Name)
	}
	if len(suite.Queries) == 0 {
		t.Fatalf("expected queries")
	}
}
