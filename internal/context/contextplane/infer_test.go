package contextplane

import "testing"

func TestInferInsights(t *testing.T) {
	result := InferInsights(
		"Compact handoffs work better than swollen transcripts, but stale vault notes create drift.",
		"foxctl",
		"runtime",
		[]string{"handoff:T-1038"},
	)
	if len(result.Observations) != 1 {
		t.Fatalf("observations=%d want 1", len(result.Observations))
	}
	if len(result.Tensions) != 1 {
		t.Fatalf("tensions=%d want 1", len(result.Tensions))
	}
	if result.Observations[0].Project != "foxctl" {
		t.Fatalf("project=%q", result.Observations[0].Project)
	}
	if result.Tensions[0].Kind != "contradiction" {
		t.Fatalf("kind=%q", result.Tensions[0].Kind)
	}
}
