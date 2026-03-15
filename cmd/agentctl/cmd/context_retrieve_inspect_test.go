package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
)

func TestContextRetrieveInspect_AppliesPolicyPatch(t *testing.T) {
	h := newACAInspectHarness(t)
	expectedPath := "internal/storage/memory/store.go"
	noteRelPath := filepath.Join("notes", "repo", filepath.Base(h.workspacePath), "packages", "internal-storage-memory.md")
	notePath := filepath.Join(h.vaultRoot, noteRelPath)
	writeTestVaultNote(t, notePath, `---
title: Storage Memory
type: map
trust: canonical
paths:
  - internal/storage/memory/store.go
---
Canonical package note for storage memory.
`)
	rebuildTestVaultIndex(t, h)

	cmd := newContextRetrieveInspectCommand()
	cmd.SetContext(config.WithContext(context.Background(), h.cfg))
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--workspace", h.workspacePath,
		"--vault-path", h.vaultRoot,
		"--query", "storage memory package",
		"--expected-path", expectedPath,
		"--apply",
		"--apply-policy-patch",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	env := decodeTestEnvelope(t, stdout.Bytes())
	data := envelopeDataMap(t, env)
	inspection := nestedMap(t, data, "inspection")
	if got := inspection["classification"]; got != "package_note_fallback_disabled" {
		t.Fatalf("classification=%v want package_note_fallback_disabled", got)
	}
	if got := data["policy_patch_applied"]; got != true {
		t.Fatalf("policy_patch_applied=%v want true", got)
	}
	rechecked := nestedMap(t, data, "rechecked")
	recheckedInspection := nestedMap(t, rechecked, "inspection")
	if got := recheckedInspection["classification"]; got == "package_note_fallback_disabled" {
		t.Fatalf("rechecked.classification=%v want progress beyond package_note_fallback_disabled envelope=%s", got, stdout.String())
	}
	body, err := os.ReadFile(filepath.Join(h.workspacePath, ".agentctl", "policy", "retrieval.yaml"))
	if err != nil {
		t.Fatalf("read retrieval policy: %v", err)
	}
	if !strings.Contains(string(body), "package_note_fallback: true") {
		t.Fatalf("retrieval policy missing fallback enable:\n%s", string(body))
	}
}

func TestContextRetrieveInspectSuite_EmitsArtifactAndAcceptsControlSuite(t *testing.T) {
	h := newACAInspectHarness(t)
	noteRelPath := filepath.Join("notes", "repo", filepath.Base(h.workspacePath), "packages", "internal-storage-memory.md")
	writeTestVaultNote(t, filepath.Join(h.vaultRoot, noteRelPath), `---
title: Storage Memory
type: map
trust: canonical
paths:
  - internal/storage/memory/store.go
---
Canonical package note for storage memory.
`)
	rebuildTestVaultIndex(t, h)

	targetSuitePath := filepath.Join(t.TempDir(), "target.yaml")
	if err := os.WriteFile(targetSuitePath, []byte(`name: target
queries:
  - id: storage-memory
    query: storage memory package
    expected_any_of:
      - internal/storage/memory/store.go
`), 0o644); err != nil {
		t.Fatalf("write target suite: %v", err)
	}
	controlSuitePath := filepath.Join(t.TempDir(), "control.yaml")
	controlSuiteBody := fmt.Sprintf(`name: control
queries:
  - id: storage-note
    query: storage memory package
    expected_any_of:
      - %s
`, filepath.ToSlash(noteRelPath))
	if err := os.WriteFile(controlSuitePath, []byte(controlSuiteBody), 0o644); err != nil {
		t.Fatalf("write control suite: %v", err)
	}

	cmd := newContextRetrieveInspectSuiteCommand()
	cmd.SetContext(config.WithContext(context.Background(), h.cfg))
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--workspace", h.workspacePath,
		"--vault-path", h.vaultRoot,
		"--suite", targetSuitePath,
		"--control-suite", controlSuitePath,
		"--apply",
		"--apply-policy-patch",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	env := decodeTestEnvelope(t, stdout.Bytes())
	data := envelopeDataMap(t, env)
	artifact, _ := data["artifact"].(string)
	if artifact == "" {
		t.Fatal("expected artifact")
	}
	if env.Meta.CASDigest != "" && env.Meta.CASDigest != artifact {
		t.Fatalf("meta.cas_digest=%q artifact=%q", env.Meta.CASDigest, artifact)
	}
	policyPatch := nestedMap(t, data, "policy_patch")
	if got := policyPatch["accepted"]; got != true {
		t.Fatalf("policy_patch.accepted=%v want true envelope=%s", got, stdout.String())
	}
	controlPatch := nestedMap(t, data, "control_patch")
	if got := controlPatch["accepted"]; got != true {
		t.Fatalf("control_patch.accepted=%v want true", got)
	}
	reportBytes, err := contextplane.ReadInspectionArtifact(context.Background(), h.cfg.Paths.CAS, artifact)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if !strings.Contains(string(reportBytes), `"suite": "target"`) {
		t.Fatalf("artifact missing target suite report:\n%s", string(reportBytes))
	}
}

func TestContextRetrieveInspectSuite_DraftsPromotionWhenObservationRepeats(t *testing.T) {
	h := newACAInspectHarness(t)

	targetSuitePath := filepath.Join(t.TempDir(), "target.yaml")
	if err := os.WriteFile(targetSuitePath, []byte(`name: target
queries:
  - id: missing-package-note
    query: waitlist notifications package
    expected_any_of:
      - internal/notifications/waitlist/service.go
`), 0o644); err != nil {
		t.Fatalf("write target suite: %v", err)
	}

	run := func() envelope.Envelope {
		cmd := newContextRetrieveInspectSuiteCommand()
		cmd.SetContext(config.WithContext(context.Background(), h.cfg))
		stdout := &bytes.Buffer{}
		cmd.SetOut(stdout)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{
			"--workspace", h.workspacePath,
			"--vault-path", h.vaultRoot,
			"--suite", targetSuitePath,
			"--apply",
			"--draft-when-promotable",
		})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute: %v", err)
		}
		return decodeTestEnvelope(t, stdout.Bytes())
	}

	first := run()
	firstData := envelopeDataMap(t, first)
	if drafts, ok := firstData["drafts"].([]any); ok && len(drafts) != 0 {
		t.Fatalf("first run drafts=%v want none", drafts)
	}

	second := run()
	secondData := envelopeDataMap(t, second)
	drafts, ok := secondData["drafts"].([]any)
	if !ok {
		t.Fatalf("drafts=%T", secondData["drafts"])
	}
	if len(drafts) != 1 {
		t.Fatalf("drafts=%v want 1 draft", drafts)
	}
	draft := drafts[0].(map[string]any)
	job := draft["job"].(map[string]any)
	if got := job["source_kind"]; got != "observation" {
		t.Fatalf("source_kind=%v want observation", got)
	}
	if got := job["status"]; got != "drafted" {
		t.Fatalf("status=%v want drafted", got)
	}
}

func TestContextRetrieveInspectRunsAndArtifactCommands(t *testing.T) {
	h := newACAInspectHarness(t)
	noteRelPath := filepath.Join("notes", "repo", filepath.Base(h.workspacePath), "packages", "internal-storage-memory.md")
	writeTestVaultNote(t, filepath.Join(h.vaultRoot, noteRelPath), `---
title: Storage Memory
type: map
trust: canonical
paths:
  - internal/storage/memory/store.go
---
Canonical package note for storage memory.
`)
	rebuildTestVaultIndex(t, h)

	targetSuitePath := filepath.Join(t.TempDir(), "target.yaml")
	if err := os.WriteFile(targetSuitePath, []byte(`name: target
queries:
  - id: storage-memory
    query: storage memory package
    expected_any_of:
      - internal/storage/memory/store.go
`), 0o644); err != nil {
		t.Fatalf("write target suite: %v", err)
	}

	runCmd := newContextRetrieveInspectSuiteCommand()
	runCmd.SetContext(config.WithContext(context.Background(), h.cfg))
	runOut := &bytes.Buffer{}
	runCmd.SetOut(runOut)
	runCmd.SetErr(&bytes.Buffer{})
	runCmd.SetArgs([]string{
		"--workspace", h.workspacePath,
		"--vault-path", h.vaultRoot,
		"--suite", targetSuitePath,
		"--apply",
		"--apply-policy-patch",
	})
	if err := runCmd.Execute(); err != nil {
		t.Fatalf("execute suite: %v", err)
	}
	runEnv := decodeTestEnvelope(t, runOut.Bytes())
	runData := envelopeDataMap(t, runEnv)
	runID, _ := runData["run_id"].(string)
	if runID == "" {
		t.Fatal("expected run_id")
	}
	artifact, _ := runData["artifact"].(string)
	if artifact == "" {
		t.Fatal("expected artifact")
	}

	listCmd := newContextRetrieveInspectRunsCommand()
	listCmd.SetContext(config.WithContext(context.Background(), h.cfg))
	listOut := &bytes.Buffer{}
	listCmd.SetOut(listOut)
	listCmd.SetErr(&bytes.Buffer{})
	listCmd.SetArgs([]string{"--workspace", h.workspacePath})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("execute runs list: %v", err)
	}
	listEnv := decodeTestEnvelope(t, listOut.Bytes())
	listData := envelopeDataMap(t, listEnv)
	runs, ok := listData["runs"].([]any)
	if !ok || len(runs) == 0 {
		t.Fatalf("runs=%T %v", listData["runs"], listData["runs"])
	}

	getCmd := newContextRetrieveInspectRunsCommand()
	getCmd.SetContext(config.WithContext(context.Background(), h.cfg))
	getOut := &bytes.Buffer{}
	getCmd.SetOut(getOut)
	getCmd.SetErr(&bytes.Buffer{})
	getCmd.SetArgs([]string{"--workspace", h.workspacePath, "--id", runID})
	if err := getCmd.Execute(); err != nil {
		t.Fatalf("execute run get: %v", err)
	}
	getEnv := decodeTestEnvelope(t, getOut.Bytes())
	getData := envelopeDataMap(t, getEnv)
	run := nestedMap(t, getData, "run")
	if got := run["artifact_digest"]; got != artifact {
		t.Fatalf("artifact_digest=%v want %s", got, artifact)
	}

	artifactCmd := newContextRetrieveInspectArtifactCommand()
	artifactCmd.SetContext(config.WithContext(context.Background(), h.cfg))
	artifactOut := &bytes.Buffer{}
	artifactCmd.SetOut(artifactOut)
	artifactCmd.SetErr(&bytes.Buffer{})
	artifactCmd.SetArgs([]string{"--artifact", artifact})
	if err := artifactCmd.Execute(); err != nil {
		t.Fatalf("execute artifact read: %v", err)
	}
	artifactEnv := decodeTestEnvelope(t, artifactOut.Bytes())
	artifactData := envelopeDataMap(t, artifactEnv)
	report := nestedMap(t, artifactData, "report")
	if got := report["suite"]; got != "target" {
		t.Fatalf("report.suite=%v want target", got)
	}
}

type acaInspectHarness struct {
	cfg           config.Config
	workspacePath string
	vaultRoot     string
}

func newACAInspectHarness(t *testing.T) acaInspectHarness {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cfg, err := config.Load(context.Background())
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	workspacePath := filepath.Join(tmp, "aca-inspect")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	store := contextplane.NewWorkspaceStore(workspacePath)
	if _, err := store.SaveTopOfMind(contextplane.TopOfMind{
		WorkspaceID:  filepath.Base(workspacePath),
		Objective:    "Improve ACA retrieval",
		Phase:        "experiment",
		RelevantRefs: []string{"path:internal/storage/memory/store.go"},
		UpdatedAt:    time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}
	vaultRoot := filepath.Join(tmp, "vault")
	if err := os.MkdirAll(vaultRoot, 0o755); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}
	return acaInspectHarness{
		cfg:           cfg,
		workspacePath: workspacePath,
		vaultRoot:     vaultRoot,
	}
}

func writeTestVaultNote(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir note dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}
}

func rebuildTestVaultIndex(t *testing.T, h acaInspectHarness) {
	t.Helper()
	index, err := obsidianindex.Open(context.Background(), h.cfg.Storage.Root, h.vaultRoot)
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	defer index.Close()
	if _, err := index.Rebuild(context.Background(), h.vaultRoot); err != nil {
		t.Fatalf("rebuild index: %v", err)
	}
}

func decodeTestEnvelope(t *testing.T, payload []byte) envelope.Envelope {
	t.Helper()
	var env envelope.Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return env
}

func envelopeDataMap(t *testing.T, env envelope.Envelope) map[string]any {
	t.Helper()
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("env.Data=%T", env.Data)
	}
	return data
}

func nestedMap(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("%s=%T", key, parent[key])
	}
	return value
}
