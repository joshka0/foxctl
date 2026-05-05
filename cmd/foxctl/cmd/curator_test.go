package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage"
	memstore "github.com/joshka0/foxctl/internal/storage/memory"
)

func TestNewCuratorCommandHasSubcommands(t *testing.T) {
	cmd := newCuratorCommand()
	if cmd.Use != "curator" {
		t.Fatalf("expected use curator, got %s", cmd.Use)
	}
	got := map[string]bool{}
	for _, sub := range cmd.Commands() {
		got[sub.Name()] = true
	}
	for _, name := range []string{"status", "run", "report"} {
		if !got[name] {
			t.Fatalf("expected subcommand %s", name)
		}
	}
}

func TestCuratorRunApplyRequiresConfirmation(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	cmd := newCuratorRunCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--apply"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected error envelope without command error, got %v", err)
	}
	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Status != "error" || env.Error.Code != "EARG" {
		t.Fatalf("expected EARG error envelope, got status=%s code=%s", env.Status, env.Error.Code)
	}
}

func TestCuratorReportLatestReadsPersistedReport(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	workspaceID := workspace.ID(cfg.Home)
	store, err := memstore.Open(context.Background(), cfg.Storage.Root, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	payload := []byte(`{"report_id":"curator-test","mode":"dry_run","artifact":"sha256:test","summary":{"total_records":3}}`)
	if _, err := store.SaveResult(context.Background(), storage.MemorySaveOptions{
		Name:      "curator_report:curator-test",
		Type:      "curator_report",
		Workspace: workspaceID,
		Summary:   "Memory curator dry_run inspected 3 records",
		Result:    payload,
	}); err != nil {
		t.Fatalf("save curator report: %v", err)
	}
	requireClose(t, store, "memory store curator report seed")

	cmd := newCuratorReportLatestCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--workspace", cfg.Home})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("report latest: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Command != "foxctl.curator.report.latest" {
		t.Fatalf("unexpected command %s", env.Command)
	}
	data := env.Data.(map[string]any)
	if data["name"] != "curator_report:curator-test" {
		t.Fatalf("unexpected report name %v", data["name"])
	}
	if data["artifact"] != "sha256:test" {
		t.Fatalf("unexpected artifact %v", data["artifact"])
	}
}

func TestCuratorReportInputUsesApplyMode(t *testing.T) {
	cmd := newCuratorRunCommand()
	cmd.SetArgs([]string{"--apply", "--confirm-apply", "--workspace", "/repo", "--limit", "42"})
	if err := cmd.ParseFlags([]string{"--apply", "--confirm-apply", "--workspace", "/repo", "--limit", "42"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	input, err := curatorReportInput(cmd, curatorReportFlags{
		Workspace:    "/repo",
		Apply:        true,
		ConfirmApply: true,
		Limit:        42,
	})
	if err != nil {
		t.Fatalf("input: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(input, &got); err != nil {
		t.Fatalf("decode input: %v", err)
	}
	if got["mode"] != "apply" || got["workspace"] != "/repo" || got["limit"] != float64(42) {
		t.Fatalf("unexpected input: %#v", got)
	}
}
