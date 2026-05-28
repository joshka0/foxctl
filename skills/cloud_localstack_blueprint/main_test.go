package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain/lite"
)

func newTestRunContext(t *testing.T, stdout *bytes.Buffer) *lite.RunContext {
	t.Helper()
	state := t.TempDir()
	cfg := lite.LiteConfig{
		Home:           state,
		InlineOutputKB: 32,
		Paths: lite.LitePaths{
			CAS:   filepath.Join(state, "cas"),
			Cache: filepath.Join(state, "cache"),
		},
		CAS: lite.LiteCASPolicy{Store: true, Expose: "off"},
	}
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	rc, err := lite.BuildRunContext(cfg, stdout)
	if err != nil {
		t.Fatalf("build run context: %v", err)
	}
	return rc
}

func TestRunPlanModeEventPipeline(t *testing.T) {
	ctx := context.Background()
	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout)
	defer func() { _ = rc.Close() }()

	in := input{
		Scenario: "event-pipeline",
		Mode:     "plan",
		RunID:    "testrun1",
		Endpoint: "http://127.0.0.1:4566",
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	// Decode envelope emitted to stdout.
	var env struct {
		Status string `json:"status"`
		Data   output `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if env.Status != "ok" {
		t.Fatalf("expected status ok, got %q", env.Status)
	}
	out := env.Data

	if out.Mode != "plan" {
		t.Errorf("mode = %q, want plan", out.Mode)
	}
	if out.RunID != "testrun1" {
		t.Errorf("run_id = %q, want testrun1", out.RunID)
	}
	if len(out.Resources) != 2 {
		t.Fatalf("resources = %d, want 2", len(out.Resources))
	}
	// Plan mode: all resources should be "planned", never "created".
	for _, r := range out.Resources {
		if r.Status != "planned" {
			t.Errorf("plan mode resource %q status = %q, want planned", r.Name, r.Status)
		}
	}
	if len(out.Commands) < 3 {
		t.Errorf("commands = %d, want >= 3", len(out.Commands))
	}
	if len(out.ProductionNotes) == 0 {
		t.Error("production_notes empty")
	}
}

func TestRunPlanModeGitopsState(t *testing.T) {
	ctx := context.Background()
	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout)
	defer func() { _ = rc.Close() }()

	in := input{
		Scenario: "gitops-state",
		Mode:     "plan",
		RunID:    "testrun2",
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	var env struct {
		Data output `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(env.Data.Resources) != 1 {
		t.Fatalf("resources = %d, want 1 for gitops-state", len(env.Data.Resources))
	}
	if env.Data.Resources[0].Type != "s3_bucket" {
		t.Errorf("resource type = %q, want s3_bucket", env.Data.Resources[0].Type)
	}
}

func TestRunDefaultsEndpointAndMode(t *testing.T) {
	ctx := context.Background()
	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout)
	defer func() { _ = rc.Close() }()

	in := input{Scenario: "eks-observability", RunID: "def1"}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	var env struct {
		Data output `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Data.Endpoint != "http://127.0.0.1:4566" {
		t.Errorf("default endpoint = %q, want http://127.0.0.1:4566", env.Data.Endpoint)
	}
	if env.Data.Mode != "plan" {
		t.Errorf("default mode = %q, want plan", env.Data.Mode)
	}
}

func TestRunRejectsUnknownScenario(t *testing.T) {
	ctx := context.Background()
	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout)
	defer func() { _ = rc.Close() }()

	in := input{Scenario: "bogus-scenario", Mode: "plan"}
	err := run(ctx, rc, in)
	if err == nil {
		t.Fatal("expected error for unknown scenario")
	}
	if !strings.Contains(err.Error(), "unknown scenario") {
		t.Errorf("error = %q, want unknown scenario", err.Error())
	}
}

func TestRunRejectsInvalidMode(t *testing.T) {
	ctx := context.Background()
	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout)
	defer func() { _ = rc.Close() }()

	in := input{Scenario: "event-pipeline", Mode: "destroy"}
	err := run(ctx, rc, in)
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
	if !strings.Contains(err.Error(), "plan or apply") {
		t.Errorf("error = %q, want plan or apply hint", err.Error())
	}
}

func TestRunRejectsWhitespaceEndpoint(t *testing.T) {
	ctx := context.Background()
	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout)
	defer func() { _ = rc.Close() }()

	in := input{Scenario: "event-pipeline", Endpoint: "http://127.0.0.1:4566 bad"}
	err := run(ctx, rc, in)
	if err == nil {
		t.Fatal("expected error for whitespace endpoint")
	}
	if !strings.Contains(err.Error(), "whitespace") {
		t.Errorf("error = %q, want whitespace hint", err.Error())
	}
}

func TestRunAutoRunID(t *testing.T) {
	ctx := context.Background()
	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout)
	defer func() { _ = rc.Close() }()

	in := input{Scenario: "event-pipeline", Mode: "plan"}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	var env struct {
		Data output `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Data.RunID == "" {
		t.Error("expected auto-generated run_id, got empty")
	}
}
