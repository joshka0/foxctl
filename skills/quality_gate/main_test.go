package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skilltest"
)

func TestQualityGate_NeedsChangesForBlockingFinding(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := input{
		Subject: "auth pipeline",
		Findings: []findingIn{
			{
				ID:       "F-1",
				Severity: "high",
				Summary:  "Authentication bypass risk",
				Source:   "reviewer-a",
			},
		},
		Checks: []checkIn{
			{
				ID:       "C-1",
				Title:    "Tests pass",
				Required: true,
				Status:   "pass",
			},
		},
	}

	if err := run(context.Background(), rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	data := decodeData(t, buf.Bytes())
	if got := data["decision"].(string); got != "needs-changes" {
		t.Fatalf("expected needs-changes, got %s", got)
	}

	summary := data["summary"].(map[string]any)
	if blockers := int(summary["blocking_findings"].(float64)); blockers != 1 {
		t.Fatalf("expected 1 blocking finding, got %d", blockers)
	}
}

func TestQualityGate_ApprovedWithKnownRisksForNonBlockingFindings(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := input{
		Findings: []findingIn{
			{
				ID:       "F-1",
				Severity: "low",
				Summary:  "Naming inconsistency",
				Source:   "reviewer-b",
			},
		},
		Checks: []checkIn{
			{
				ID:       "C-1",
				Title:    "Core behavior verified",
				Required: true,
				Status:   "pass",
			},
		},
	}

	if err := run(context.Background(), rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	data := decodeData(t, buf.Bytes())
	if got := data["decision"].(string); got != "approved-with-known-risks" {
		t.Fatalf("expected approved-with-known-risks, got %s", got)
	}
}

func TestQualityGate_ApprovedWhenClean(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := input{
		Checks: []checkIn{
			{
				ID:       "C-1",
				Title:    "Build validation",
				Required: true,
				Status:   "pass",
			},
		},
	}

	if err := run(context.Background(), rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	data := decodeData(t, buf.Bytes())
	if got := data["decision"].(string); got != "approved" {
		t.Fatalf("expected approved, got %s", got)
	}
}

func TestQualityGate_DefaultPolicyFailsIncompleteRequiredChecks(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := input{
		Checks: []checkIn{
			{
				ID:       "C-1",
				Title:    "Required rollout validation",
				Required: true,
				Status:   "warn",
			},
		},
	}

	if err := run(context.Background(), rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	data := decodeData(t, buf.Bytes())
	if got := data["decision"].(string); got != "needs-changes" {
		t.Fatalf("expected needs-changes, got %s", got)
	}
}

func TestQualityGate_CustomPolicyAllowsIncompleteRequiredChecks(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	requireAll := false
	maxWarn := 5
	in := input{
		Checks: []checkIn{
			{
				ID:       "C-1",
				Title:    "Portability check",
				Required: true,
				Status:   "warn",
			},
		},
		Policy: policyInput{
			RequireAllRequiredChecks: &requireAll,
			MaxWarnFindings:          &maxWarn,
		},
	}

	if err := run(context.Background(), rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	data := decodeData(t, buf.Bytes())
	if got := data["decision"].(string); got != "approved-with-known-risks" {
		t.Fatalf("expected approved-with-known-risks, got %s", got)
	}
}

func TestQualityGate_FailsUnknownSeverityWhenConfigured(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	failUnknown := true
	in := input{
		Findings: []findingIn{
			{
				ID:       "F-1",
				Severity: "mystery",
				Summary:  "Uncategorized issue",
				Source:   "reviewer-c",
			},
		},
		Policy: policyInput{
			FailOnUnknownSeverity: &failUnknown,
		},
	}

	if err := run(context.Background(), rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	data := decodeData(t, buf.Bytes())
	if got := data["decision"].(string); got != "needs-changes" {
		t.Fatalf("expected needs-changes, got %s", got)
	}
	summary := data["summary"].(map[string]any)
	if warnings := int(summary["warning_findings"].(float64)); warnings != 0 {
		t.Fatalf("expected unknown severity blocker to not count as warning, got %d", warnings)
	}
}

func TestQualityGate_FailsWhenWarningBudgetExceeded(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	maxWarn := 1
	in := input{
		Findings: []findingIn{
			{ID: "F-1", Severity: "low", Summary: "first warning", Source: "reviewer-a"},
			{ID: "F-2", Severity: "medium", Summary: "second warning", Source: "reviewer-a"},
		},
		Policy: policyInput{
			MaxWarnFindings: &maxWarn,
		},
	}

	if err := run(context.Background(), rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	data := decodeData(t, buf.Bytes())
	if got := data["decision"].(string); got != "needs-changes" {
		t.Fatalf("expected needs-changes, got %s", got)
	}
}

func TestQualityGate_CustomBlockSeverities(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := input{
		Findings: []findingIn{
			{ID: "F-1", Severity: "medium", Summary: "medium issue", Source: "reviewer-d"},
		},
		Policy: policyInput{
			BlockOnSeverities: []string{"medium"},
		},
	}

	if err := run(context.Background(), rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	data := decodeData(t, buf.Bytes())
	if got := data["decision"].(string); got != "needs-changes" {
		t.Fatalf("expected needs-changes, got %s", got)
	}
	summary := data["summary"].(map[string]any)
	if blockers := int(summary["blocking_findings"].(float64)); blockers != 1 {
		t.Fatalf("expected one blocker from custom severity policy, got %d", blockers)
	}
}

func newTestContext(t *testing.T, stdout *bytes.Buffer) (*skillmain.RunContext, func()) {
	t.Helper()
	return skilltest.NewTestRunContext(t, stdout, nil)
}

func decodeData(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if got := env["status"].(string); got != "ok" {
		t.Fatalf("expected status ok, got %s", got)
	}
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data map, got %T", env["data"])
	}
	return data
}
