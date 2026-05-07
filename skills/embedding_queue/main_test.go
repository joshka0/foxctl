package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/domain/policy"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/cas"
	"github.com/rs/zerolog"
)

func newTestRunContext(t *testing.T, stdout *bytes.Buffer, workspace string) *skillmain.RunContext {
	t.Helper()
	t.Setenv("FOXCTL_WORKSPACE", workspace)
	state := t.TempDir()
	casPath := filepath.Join(state, "cas")
	casStore, err := cas.NewStore(casPath)
	if err != nil {
		t.Fatalf("open cas: %v", err)
	}

	pv, err := policy.NewPathValidator(workspace, nil)
	if err != nil {
		t.Fatalf("path validator: %v", err)
	}

	cfg := config.Config{
		Home:           state,
		InlineOutputKB: 32,
		MaxCaptureKB:   10240,
		Paths: config.Paths{
			CAS:   casPath,
			Jobs:  filepath.Join(state, "jobs"),
			Cache: filepath.Join(state, "cache"),
		},
	}

	return &skillmain.RunContext{
		Config:        cfg,
		CASStore:      casStore,
		Workspace:     workspace,
		Logger:        zerolog.Nop(),
		PathValidator: pv,
		Validator:     validator.New(),
		Stdout:        stdout,
		Now:           time.Now,
		InlineKB:      cfg.InlineOutputKB,
		MaxPreview:    100,
	}
}

func TestEnqueue(t *testing.T) {
	work := t.TempDir()
	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout, work)
	defer rc.Close()

	in := Input{
		Operation:   "enqueue",
		WorkspaceID: "test-ws",
		Symbols: []SymbolInput{
			{
				SymbolID:   "main.go:Handler",
				FilePath:   "main.go",
				SymbolName: "Handler",
				Content:    "func Handler() {}",
			},
		},
		Deduplicate: true,
	}

	err := run(context.Background(), rc, in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if env.Status != "ok" {
		t.Errorf("expected status ok, got %s", env.Status)
	}

	data := env.Data.(map[string]any)
	if data["queued"].(float64) != 1 {
		t.Errorf("expected 1 queued, got %v", data["queued"])
	}
}

func TestEnqueuePreservesSymbolIdentityFields(t *testing.T) {
	work := t.TempDir()
	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout, work)
	defer rc.Close()

	in := Input{
		Operation:   "enqueue",
		WorkspaceID: "test-ws",
		Symbols: []SymbolInput{{
			SymbolID:   "legacy-id",
			FilePath:   "pkg/foo/foo.go",
			SymbolName: "Handler",
			Language:   "go",
			PackageID:  "go:pkg/foo",
			SymbolKey:  "func Handler",
			MemoryName: "symbol://test-ws/go:pkg/foo::func Handler",
			Content:    "func Handler() {}",
		}},
	}

	if err := run(context.Background(), rc, in); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	store, err := embedding.OpenStore(context.Background(), rc.Config.Paths.Cache)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	job, err := store.ClaimNext(context.Background())
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if job.MemoryName != "symbol://test-ws/go:pkg/foo::func Handler" {
		t.Fatalf("memory name=%q", job.MemoryName)
	}
	if job.PackageID != "go:pkg/foo" || job.SymbolKey != "func Handler" || job.Language != "go" {
		t.Fatalf("identity language/package/key=%q/%q/%q", job.Language, job.PackageID, job.SymbolKey)
	}
}

func TestEnqueueMemories(t *testing.T) {
	work := t.TempDir()
	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout, work)
	defer rc.Close()

	in := Input{
		Operation:   "enqueue",
		WorkspaceID: "test-ws",
		Memories: []MemoryInput{{
			Name:    "decision:queue",
			Type:    "decision",
			Content: "[May 2026] [decision] Queue memory embeddings",
		}},
	}

	if err := run(context.Background(), rc, in); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}
	if env.Status != "ok" {
		t.Errorf("expected status ok, got %s", env.Status)
	}
	data := env.Data.(map[string]any)
	if data["queued"].(float64) != 1 {
		t.Errorf("expected 1 queued, got %v", data["queued"])
	}
}

func TestStats(t *testing.T) {
	work := t.TempDir()
	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout, work)
	defer rc.Close()

	in := Input{
		Operation: "stats",
	}

	err := run(context.Background(), rc, in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if env.Status != "ok" {
		t.Errorf("expected status ok, got %s", env.Status)
	}

	data := env.Data.(map[string]any)
	if data["stats"] == nil {
		t.Error("expected stats in output")
	}
}

func TestGetNotFound(t *testing.T) {
	work := t.TempDir()
	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout, work)
	defer rc.Close()

	in := Input{
		Operation:   "get",
		WorkspaceID: "test-ws",
		SymbolID:    "nonexistent.go:Foo",
	}

	err := run(context.Background(), rc, in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if env.Status != "ok" {
		t.Errorf("expected status ok, got %s", env.Status)
	}

	data := env.Data.(map[string]any)
	if data["message"] != "Embedding not found" {
		t.Errorf("expected 'Embedding not found' message, got %v", data["message"])
	}
}

func TestCleanup(t *testing.T) {
	work := t.TempDir()
	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout, work)
	defer rc.Close()

	in := Input{
		Operation:      "cleanup",
		OlderThanHours: 0, // Clean all
	}

	err := run(context.Background(), rc, in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if env.Status != "ok" {
		t.Errorf("expected status ok, got %s", env.Status)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
