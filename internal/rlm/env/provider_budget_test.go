package env

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalProviderBudgetCapsHitsWithoutReturningScanError(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "alpha.go"), "package main\n\nfunc Alpha() { _ = \"needle\" }\n")
	writeTestFile(t, filepath.Join(workspace, "beta.go"), "package main\n\nfunc Beta() { _ = \"needle\" }\n")

	budget := newLocalProviderBudget(context.Background(), 1)
	budget.MaxHits = 1
	_, exactHits, err := workspaceCodeProbeSearch(
		context.Background(),
		workspace,
		nil,
		[]string{"needle"},
		10,
		nil,
		budget,
	)
	if err != nil {
		t.Fatalf("workspaceCodeProbeSearch returned capped error: %v", err)
	}
	if len(exactHits) != 1 {
		t.Fatalf("exact hits=%d want 1: %#v", len(exactHits), exactHits)
	}
	if !budget.isCapped() {
		t.Fatalf("budget was not marked capped: %#v", budget.snapshot())
	}
	if got := budget.snapshot()["skip_reason"]; got != "max_hits" {
		t.Fatalf("skip_reason=%v want max_hits", got)
	}
}

func TestCodeSearchProviderTelemetryIncludesBudgetCap(t *testing.T) {
	t.Parallel()

	budget := newLocalProviderBudget(context.Background(), 1)
	budget.MaxFiles = 1
	if err := budget.beforeFile(context.Background()); err != nil {
		t.Fatalf("beforeFile: %v", err)
	}
	if err := budget.recordFile(10); err != nil {
		t.Fatalf("recordFile: %v", err)
	}
	if err := budget.beforeFile(context.Background()); err == nil {
		t.Fatal("expected max_files cap")
	}

	var telemetry []codeSearchProviderTelemetryItem
	recordCodeSearchProviderTelemetry(&telemetry, "local_probe", time.Millisecond, 0, nil, nil, budget)
	if len(telemetry) != 1 {
		t.Fatalf("telemetry len=%d", len(telemetry))
	}
	if telemetry[0].Budget["capped"] != true {
		t.Fatalf("telemetry budget=%v", telemetry[0].Budget)
	}
	if telemetry[0].Budget["skip_reason"] != "max_files" {
		t.Fatalf("telemetry budget=%v", telemetry[0].Budget)
	}
}
