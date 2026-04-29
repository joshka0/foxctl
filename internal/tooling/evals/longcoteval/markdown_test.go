package longcoteval

import (
	"strings"
	"testing"
	"time"
)

func TestRenderMarkdownIncludesWarningAndDeterministicSections(t *testing.T) {
	t.Parallel()

	result := RunResult{
		RunID:       "run-1",
		GeneratedAt: time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC),
		Questions:   []Question{{ID: "q1"}},
		Attempts:    []Attempt{attempt("q1", ConditionBaselineNoToolsOfficial, true, 10, 1)},
	}
	result.Summary = Summarize(result.Attempts, []Comparison{{Baseline: ConditionBaselineNoToolsOfficial, Candidate: ConditionRLMNoToolsSingle}})

	md := RenderMarkdown(result)
	for _, want := range []string{
		"# LongCoT × RLM Eval",
		"not LongCoT leaderboard comparable",
		"## Condition Summary",
		"`baseline_no_tools_official_prompt`",
		"## Paired Comparisons",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
}

func TestRenderMarkdownIncludesReviewTelemetry(t *testing.T) {
	t.Parallel()

	attempt := attempt("q1", ConditionRLMReplRecursive, true, 10, 1)
	attempt.RLM = &RLMAttemptMeta{Metadata: map[string]any{
		"review": map[string]any{
			"review_recursive_requested": true,
			"review_recursive_used":      true,
		},
	}}
	result := RunResult{
		RunID:       "run-1",
		GeneratedAt: time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC),
		Questions:   []Question{{ID: "q1"}},
		Attempts:    []Attempt{attempt},
	}
	result.Summary = Summarize(result.Attempts, nil)

	md := RenderMarkdown(result)
	for _, want := range []string{
		"## RLM Review Telemetry",
		"recursive",
		"`rlm_repl_recursive`",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
}
