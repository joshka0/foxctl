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

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/spf13/cobra"
)

func TestEndToEndCacheMemoryWorkflow(t *testing.T) {
	h := newCASHarness(t)

	t.Run("text-grep memory workflow", func(t *testing.T) {
		env, _ := withRunExecutor(h.ctx, t, []string{
			"--input", fmt.Sprintf(`{"path":%q,"pattern":"needle"}`, h.samplePath),
			"--remember", "grep-first",
			"--workspace", h.workdir,
			"text/grep",
		})
		h.assertCASArtifact(t, env)
		jobID := extractJobID(t, env)
		h.saveJobAsMemory(t, jobID, "grep-job")

		stdout := h.execMemoryCommand(t, newMemoryGetCommand(), "--workspace", h.workdir, "grep-job")
		h.assertMemoryMetadata(t, stdout.Bytes(), "grep-job")

		env2, _ := withRunExecutor(h.ctx, t, []string{
			"--input", fmt.Sprintf(`{"path":%q,"pattern":"needle"}`, h.samplePath),
			"--workspace", h.workdir,
			"text/grep",
		})
		if env2.Meta.Source == "cache" {
			t.Fatalf("expected cache to be disabled")
		}
	})

	t.Run("openapi memory relevance", func(t *testing.T) {
		env, _ := withRunExecutor(h.ctx, t, []string{
			"--input", h.openapiInput,
			"--remember", "openapi-plan",
			"--workspace", h.workdir,
			"http/openapi",
		})
		assertCommand(t, env, "http/openapi")
		h.saveJobAsMemory(t, extractJobID(t, env), "openapi-plan")

		relevant := h.execMemoryCommand(t, newMemoryRelevantCommand(), "--workspace", h.workdir, "--limit", "5")
		h.assertRelevantContains(t, relevant.Bytes(), "openapi-plan")
	})
}

type casHarness struct {
	cfg          config.Config
	ctx          context.Context
	workdir      string
	samplePath   string
	openapiInput string
}

func newCASHarness(t *testing.T) *casHarness {
	t.Helper()
	if testing.Short() {
		t.Skip("slow end-to-end cache/memory workflow")
	}
	cfg := installTextGrepSkill(t)
	installHTTPOpenAPISkill(t, cfg)
	workdir := t.TempDir()
	samplePath := filepath.Join(workdir, "sample.txt")
	buildSampleFile(t, samplePath)

	// Create a minimal OpenAPI spec for testing
	specPath := filepath.Join(workdir, "test-spec.yaml")
	testSpec := `openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /todos:
    get:
      operationId: listTodos
      parameters:
        - name: limit
          in: query
          schema:
            type: integer
      responses:
        '200':
          description: Success
`
	if err := os.WriteFile(specPath, []byte(testSpec), 0o644); err != nil {
		t.Fatalf("write test spec: %v", err)
	}

	ctx := config.WithContext(context.Background(), cfg)
	return &casHarness{
		cfg:          cfg,
		ctx:          ctx,
		workdir:      workdir,
		samplePath:   samplePath,
		openapiInput: fmt.Sprintf(`{"spec":%q,"operationId":"listTodos","params":{"query":{"limit":5}},"dry_run":true}`, specPath),
	}
}

func buildSampleFile(t *testing.T, path string) {
	t.Helper()
	var builder strings.Builder
	for i := 0; i < 400; i++ {
		if _, err := builder.WriteString("needle line "); err != nil {
			t.Fatalf("build sample prefix: %v", err)
		}
		if _, err := fmt.Fprint(&builder, i); err != nil {
			t.Fatalf("build sample idx: %v", err)
		}
		if _, err := builder.WriteString("\n"); err != nil {
			t.Fatalf("build sample newline: %v", err)
		}
	}
	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}
}

func withRunExecutor(ctx context.Context, t *testing.T, args []string) (envelope.Envelope, *bytes.Buffer) {
	t.Helper()
	cmd := newRunCommand()
	cmd.SetContext(ctx)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("run command failed: %v (stderr=%s)", err, stderr.String())
	}
	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return env, stderr
}

func (h *casHarness) saveJobAsMemory(t *testing.T, jobID, name string) {
	t.Helper()
	args := []string{"--as", name, "--workspace", h.workdir, jobID}
	_ = h.execMemoryCommand(t, newMemorySaveCommand(), args...)
}

func (h *casHarness) execMemoryCommand(t *testing.T, cmd *cobra.Command, args ...string) *bytes.Buffer {
	t.Helper()
	cmd.SetContext(h.ctx)
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("memory command failed: %v", err)
	}
	return stdout
}

func (h *casHarness) assertCASArtifact(t *testing.T, env envelope.Envelope) {
	t.Helper()
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be an object, got %T", env.Data)
	}
	artifact, _ := data["artifact"].(string)
	artifact = strings.TrimSpace(artifact)
	if artifact == "" {
		t.Fatalf("expected data.artifact to be set")
	}
	if !strings.HasPrefix(artifact, "sha256:") {
		t.Fatalf("expected sha256: prefix in artifact, got: %s", artifact)
	}
	if env.Meta.CASDigest != "" && env.Meta.CASDigest != artifact {
		t.Fatalf("meta.cas_digest %q does not match artifact %q", env.Meta.CASDigest, artifact)
	}
	casPath := filepath.Join(h.cfg.Paths.CAS, "sha256", artifact[len("sha256:"):])
	if _, err := os.Stat(casPath); err != nil {
		t.Fatalf("cas artifact missing: %v", err)
	}
}

func (h *casHarness) assertMemoryMetadata(t *testing.T, payload []byte, name string) {
	t.Helper()
	var env envelope.Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("decode memory envelope: %v", err)
	}
	if env.Meta.Memory == nil || env.Meta.Memory.Name != name {
		t.Fatalf("expected memory metadata for %s", name)
	}
}

func (h *casHarness) assertRelevantContains(t *testing.T, payload []byte, name string) {
	t.Helper()
	var env envelope.Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("decode relevant envelope: %v", err)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("env.Data is not a map: %T", env.Data)
	}
	entries, ok := data["entries"].([]any)
	if !ok {
		t.Fatalf("entries is not a slice: %T", data["entries"])
	}
	if len(entries) == 0 {
		t.Fatalf("expected at least one relevant entry")
	}
	for _, entry := range entries {
		if m, ok := entry.(map[string]any); ok {
			if entryName, ok := m["name"].(string); ok && entryName == name {
				return
			}
		}
	}
	t.Fatalf("expected %s in relevant entries: %#v", name, entries)
}

func assertCommand(t *testing.T, env envelope.Envelope, command string) {
	t.Helper()
	if env.Command != command {
		t.Fatalf("expected %s command, got %s", command, env.Command)
	}
}

func extractJobID(t *testing.T, env envelope.Envelope) string {
	t.Helper()
	if env.Meta.JobID == "" {
		t.Fatalf("expected job_id in envelope meta, got empty")
	}
	return env.Meta.JobID
}
