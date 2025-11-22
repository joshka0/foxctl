package main

import "testing"

func TestFindFailedStep_LastFailureWins(t *testing.T) {
	job := &JobDetails{
		Steps: []JobStep{
			{Name: "setup", Conclusion: "success"},
			{Name: "lint", Conclusion: "failure"},
			{Name: "test", Conclusion: "error"},
		},
	}

	step := findFailedStep(job)
	if step == nil {
		t.Fatalf("expected a failed step, got nil")
	}
	if step.Name != "test" {
		t.Fatalf("expected last failing step 'test', got %q", step.Name)
	}
}

func TestFindFailedStep_NoFailures(t *testing.T) {
	job := &JobDetails{
		Steps: []JobStep{
			{Name: "setup", Conclusion: "success"},
			{Name: "lint", Conclusion: "success"},
		},
	}

	step := findFailedStep(job)
	if step != nil {
		t.Fatalf("expected nil when there are no failing steps, got %+v", step)
	}
}

func TestClassifyCIStatus_AllSuccess(t *testing.T) {
	overall, hasBlocking, allSuccess, hasNeutral := classifyCIStatus(3, 0, 0, 0, 3)
	if overall != "success" || hasBlocking || !allSuccess || hasNeutral {
		t.Fatalf("unexpected classification for all success: overall=%s hasBlocking=%v allSuccess=%v hasNeutral=%v", overall, hasBlocking, allSuccess, hasNeutral)
	}
}

func TestClassifyCIStatus_Failed(t *testing.T) {
	overall, hasBlocking, allSuccess, hasNeutral := classifyCIStatus(3, 1, 0, 0, 2)
	if overall != "failed" || !hasBlocking || allSuccess || hasNeutral {
		t.Fatalf("unexpected classification for failed: overall=%s hasBlocking=%v allSuccess=%v hasNeutral=%v", overall, hasBlocking, allSuccess, hasNeutral)
	}
}

func TestClassifyCIStatus_Cancelled(t *testing.T) {
	overall, hasBlocking, allSuccess, hasNeutral := classifyCIStatus(2, 0, 1, 0, 1)
	if overall != "cancelled" || !hasBlocking || allSuccess || hasNeutral {
		t.Fatalf("unexpected classification for cancelled: overall=%s hasBlocking=%v allSuccess=%v hasNeutral=%v", overall, hasBlocking, allSuccess, hasNeutral)
	}
}

func TestClassifyCIStatus_MixedWithNeutral(t *testing.T) {
	overall, hasBlocking, allSuccess, hasNeutral := classifyCIStatus(2, 0, 0, 2, 0)
	if overall != "mixed" || hasBlocking || allSuccess || !hasNeutral {
		t.Fatalf("unexpected classification for mixed neutrals: overall=%s hasBlocking=%v allSuccess=%v hasNeutral=%v", overall, hasBlocking, allSuccess, hasNeutral)
	}
}

func TestCountConclusionsAndFilterBlocking(t *testing.T) {
	checks := []CheckRun{
		{Name: "success", Conclusion: "success"},
		{Name: "failure", Conclusion: "failure"},
		{Name: "timed", Conclusion: "timed_out"},
		{Name: "cancel", Conclusion: "cancelled"},
		{Name: "neutral", Conclusion: "neutral"},
		{Name: "skip", Conclusion: "skipped"},
		{Name: "action", Conclusion: "action_required"},
		{Name: "stale", Conclusion: "stale"},
	}

	total, failed, cancelled, neutral, success := countConclusions(checks)
	if total != 8 || failed != 4 || cancelled != 1 || neutral != 2 || success != 1 {
		t.Fatalf("unexpected counts: total=%d failed=%d cancelled=%d neutral=%d success=%d", total, failed, cancelled, neutral, success)
	}

	filtered := filterBlockingChecks(checks)
	if len(filtered) != 5 {
		t.Fatalf("expected 5 blocking checks, got %d: %#v", len(filtered), filtered)
	}
	seen := map[string]bool{}
	for _, c := range filtered {
		seen[c.Conclusion] = true
	}
	for _, conc := range []string{"failure", "timed_out", "cancelled", "action_required", "stale"} {
		if !seen[conc] {
			t.Fatalf("expected conclusion %q to be treated as blocking, got %#v", conc, seen)
		}
	}
}
