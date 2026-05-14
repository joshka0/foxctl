package retrievaleval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEvaluateModeAndSummarize(t *testing.T) {
	mode := EvaluateModeWithBudgetAndDuration("lexical",
		[]string{
			"notes/repo/foxctl/index.md",
			"internal/runtime/terminal/tmuxbridge/client.go",
			"notes/repo/foxctl/packages/cmd-foxctl-cmd.md",
		},
		[]string{"notes/repo/foxctl/packages/cmd-foxctl-cmd.md"},
		[]string{"internal/runtime/terminal/tmuxbridge/client.go"},
		3,
		10,
		25*time.Millisecond,
		nil,
	)
	if mode.FirstCorrectRank != 3 {
		t.Fatalf("rank=%d want 3", mode.FirstCorrectRank)
	}
	if !mode.HitAt5 || !mode.HitAt10 {
		t.Fatalf("expected hit at 5 and 10")
	}
	if !mode.ForbiddenHit || len(mode.ForbiddenPaths) != 1 {
		t.Fatalf("expected forbidden hit to be recorded: %+v", mode)
	}
	if mode.Budget == nil {
		t.Fatal("expected budget metrics")
	}
	if mode.Budget.Limit != 10 {
		t.Fatalf("budget limit=%d want 10", mode.Budget.Limit)
	}
	if mode.Budget.ReturnedPaths != 3 || mode.Budget.ReturnedHits != 3 {
		t.Fatalf("budget returned paths/hits=%d/%d want 3/3", mode.Budget.ReturnedPaths, mode.Budget.ReturnedHits)
	}
	if mode.Budget.ReturnedPathBytes == 0 || mode.Budget.ReturnedPathTokenEstimate == 0 {
		t.Fatalf("expected non-zero path budget fields: %+v", mode.Budget)
	}
	if mode.DurationMS != 25 {
		t.Fatalf("duration_ms=%d want 25", mode.DurationMS)
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
	if summaries[0].MeanReciprocalRank != 1.0/3.0 {
		t.Fatalf("mrr=%.2f want %.2f", summaries[0].MeanReciprocalRank, 1.0/3.0)
	}
	if summaries[0].ForbiddenHits != 1 {
		t.Fatalf("forbidden_hits=%d want 1", summaries[0].ForbiddenHits)
	}
	if summaries[0].MeanReturnedHits != 3 {
		t.Fatalf("mean returned hits=%.1f want 3.0", summaries[0].MeanReturnedHits)
	}
	if summaries[0].MeanDurationMS != 25 {
		t.Fatalf("mean duration ms=%.1f want 25.0", summaries[0].MeanDurationMS)
	}
	if summaries[0].MeanPathBytes == 0 || summaries[0].MeanPathTokens == 0 {
		t.Fatalf("expected non-zero summary budget fields: %+v", summaries[0])
	}
	markdown := RenderMarkdown(RunResult{
		Suite:     "test",
		Workspace: ".",
		Limit:     10,
		Queries:   results,
		Summaries: summaries,
	})
	if !strings.Contains(markdown, "avg ms") || !strings.Contains(markdown, "avg path tokens") || !strings.Contains(markdown, "est_tokens=") {
		t.Fatalf("markdown missing budget fields:\n%s", markdown)
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

	forbiddenAlerts := BuildAlerts([]Summary{{Mode: "repoindex_semantic_dag", ForbiddenHits: 1}}, Policy{FailOnAlerts: true})
	if len(forbiddenAlerts) != 1 || forbiddenAlerts[0].Metric != "forbidden_hits" {
		t.Fatalf("forbidden alerts=%+v", forbiddenAlerts)
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

func TestLoadSemanticAnchorSuiteCoversAcceptanceGate(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "testdata", "evals", "retrieval", "foxctl-semantic-anchors.yaml")
	suite, err := LoadSuite(path)
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	if suite.Name != "foxctl-semantic-anchors" {
		t.Fatalf("name=%q", suite.Name)
	}
	if len(suite.Queries) != 8 {
		t.Fatalf("queries=%d want 8", len(suite.Queries))
	}
	positive, controls := 0, 0
	requiredControls := map[string]bool{
		"beacon":   false,
		"domain":   false,
		"protocol": false,
		"risk":     false,
	}
	for _, q := range suite.Queries {
		if len(q.ExpectedAnyOf) > 0 {
			positive++
		}
		if len(q.ForbiddenAnyOf) > 0 {
			controls++
			lower := strings.ToLower(q.ID + " " + q.Query + " " + q.Notes)
			for key := range requiredControls {
				if strings.Contains(lower, key) {
					requiredControls[key] = true
				}
			}
		}
	}
	if positive < 5 {
		t.Fatalf("positive queries=%d want at least 5", positive)
	}
	if controls < 3 {
		t.Fatalf("control queries=%d want at least 3", controls)
	}
	for key, ok := range requiredControls {
		if !ok {
			t.Fatalf("suite missing %s control coverage", key)
		}
	}
}
