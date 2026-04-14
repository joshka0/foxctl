package retrievaleval

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEvaluateModeAndSummarize(t *testing.T) {
	mode := EvaluateMode("lexical",
		[]string{
			"notes/repo/foxctl/index.md",
			"notes/repo/foxctl/packages/cmd-foxctl-cmd.md",
		},
		[]string{"notes/repo/foxctl/packages/cmd-foxctl-cmd.md"},
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
			Query: "cmd foxctl cmd",
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

func TestLoadPolicyAndBuildAlerts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	body := []byte(`
suite: foxctl
limit: 10
format: markdown
modes:
  - aca_default
  - aca_query_typed
fail_on_alerts: true
thresholds:
  aca_default:
    min_hit_rate_at_5: 0.80
    min_hit_rate_at_10: 0.90
    min_mrr: 0.75
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	policy, err := LoadPolicy(path)
	if err != nil {
		t.Fatalf("LoadPolicy() error = %v", err)
	}
	if policy.Suite != "foxctl" {
		t.Fatalf("suite=%q want foxctl", policy.Suite)
	}
	if !policy.FailOnAlerts {
		t.Fatalf("FailOnAlerts=false want true")
	}
	if got := policy.Thresholds["aca_default"].MinMeanReciprocalRank; got != 0.75 {
		t.Fatalf("min_mrr=%.2f want 0.75", got)
	}

	alerts := BuildAlerts([]Summary{{
		Mode:               "aca_default",
		HitRateAt5:         0.70,
		HitRateAt10:        0.85,
		MeanReciprocalRank: 0.60,
	}}, policy)
	if len(alerts) != 3 {
		t.Fatalf("len(alerts)=%d want 3", len(alerts))
	}
}

func TestLoadSuite(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "testdata", "evals", "retrieval", "foxctl.yaml")
	suite, err := LoadSuite(path)
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	if suite.Name != "foxctl" {
		t.Fatalf("name=%q", suite.Name)
	}
	if len(suite.Queries) == 0 {
		t.Fatalf("expected queries")
	}
}
