package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
)

func TestEndToEndCacheMemoryWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("slow end-to-end cache/memory workflow")
	}
	cfg := installTextGrepSkill(t)
	installHTTPOpenAPISkill(t, cfg)

	workdir := t.TempDir()
	sampleFile := filepath.Join(workdir, "sample.txt")
	var builder strings.Builder
	for i := 0; i < 400; i++ {
		if _, err := builder.WriteString("needle line "); err != nil {
			t.Fatalf("build sample prefix: %v", err)
		}
		if _, err := builder.WriteString(fmt.Sprint(i)); err != nil {
			t.Fatalf("build sample idx: %v", err)
		}
		if _, err := builder.WriteString("\n"); err != nil {
			t.Fatalf("build sample newline: %v", err)
		}
	}
	if err := os.WriteFile(sampleFile, []byte(builder.String()), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	ctx := config.WithContext(context.Background(), cfg)

	// Initial run to generate CAS artifact and remember entry
	env1, stderr1 := runSkillCommand(ctx, t, []string{
		"--input", fmt.Sprintf(`{"path":%q,"pattern":"needle"}`, sampleFile),
		"--remember", "grep-first",
		"--workspace", workdir,
		"--cache", "auto",
		"text/grep",
	})
	if env1.Meta.CASDigest == "" {
		t.Fatalf("expected CAS digest on first run")
	}
	casPath := filepath.Join(cfg.Paths.CAS, "sha256", env1.Meta.CASDigest[len("sha256:"):])
	if _, err := os.Stat(casPath); err != nil {
		t.Fatalf("cas artifact missing: %v", err)
	}
	jobID := extractJobID(t, stderr1.String())

	// Save job output under another name
	saveCmd := newMemorySaveCommand()
	saveCmd.SetContext(ctx)
	saveCmd.SetArgs([]string{"--as", "grep-job", "--workspace", workdir, jobID})
	saveCmd.SetOut(&bytes.Buffer{})
	saveCmd.SetErr(&bytes.Buffer{})
	if err := saveCmd.Execute(); err != nil {
		t.Fatalf("memory save: %v", err)
	}

	// Ensure memory get returns memory metadata
	getCmd := newMemoryGetCommand()
	getCmd.SetContext(ctx)
	getStdout := &bytes.Buffer{}
	getCmd.SetOut(getStdout)
	getCmd.SetErr(&bytes.Buffer{})
	getCmd.SetArgs([]string{"--workspace", workdir, "grep-job"})
	if err := getCmd.Execute(); err != nil {
		t.Fatalf("memory get: %v", err)
	}
	var memEnv envelope.Envelope
	if err := json.Unmarshal(getStdout.Bytes(), &memEnv); err != nil {
		t.Fatalf("decode memory get envelope: %v", err)
	}
	if memEnv.Meta.Memory == nil || memEnv.Meta.Memory.Name != "grep-job" {
		t.Fatalf("expected memory metadata for grep-job")
	}

	// Second run should hit cache
	env2, _ := runSkillCommand(ctx, t, []string{
		"--input", fmt.Sprintf(`{"path":%q,"pattern":"needle"}`, sampleFile),
		"--workspace", workdir,
		"--cache", "auto",
		"text/grep",
	})
	if env2.Meta.Source != "cache" {
		t.Fatalf("expected cache hit, got source=%s", env2.Meta.Source)
	}

	// Run http/openapi skill (dry-run) and remember result
	openapiInput := `{"base_url":"https://api.example.com","path":"/todos","method":"GET","dry_run":true,"query":{"limit":"5"}}`
	env3, stderr3 := runSkillCommand(ctx, t, []string{
		"--input", openapiInput,
		"--remember", "openapi-plan",
		"--workspace", workdir,
		"http/openapi",
	})
	if env3.Command != "http/openapi" {
		t.Fatalf("expected http/openapi command, got %s", env3.Command)
	}
	jobID2 := extractJobID(t, stderr3.String())
	saveOpenCmd := newMemorySaveCommand()
	saveOpenCmd.SetContext(ctx)
	saveOpenCmd.SetArgs([]string{"--as", "openapi-plan", "--workspace", workdir, jobID2})
	saveOpenCmd.SetOut(&bytes.Buffer{})
	saveOpenCmd.SetErr(&bytes.Buffer{})
	if err := saveOpenCmd.Execute(); err != nil {
		t.Fatalf("memory save openapi: %v", err)
	}

	// Relevant memories should include both entries
	relCmd := newMemoryRelevantCommand()
	relCmd.SetContext(ctx)
	relStdout := &bytes.Buffer{}
	relCmd.SetOut(relStdout)
	relCmd.SetErr(&bytes.Buffer{})
	relCmd.SetArgs([]string{"--workspace", workdir, "--limit", "5"})
	if err := relCmd.Execute(); err != nil {
		t.Fatalf("memory relevant: %v", err)
	}
	var relEnv envelope.Envelope
	if err := json.Unmarshal(relStdout.Bytes(), &relEnv); err != nil {
		t.Fatalf("decode relevant: %v", err)
	}
	data, _ := relEnv.Data.(map[string]any)
	entries, _ := data["entries"].([]any)
	if len(entries) == 0 {
		t.Fatalf("expected at least one relevant entry")
	}
	foundOpenAPI := false
	for _, entry := range entries {
		if m, ok := entry.(map[string]any); ok {
			if name, _ := m["name"].(string); name == "openapi-plan" {
				foundOpenAPI = true
			}
		}
	}
	if !foundOpenAPI {
		t.Fatalf("expected openapi-plan in relevant entries: %#v", entries)
	}
}

func runSkillCommand(ctx context.Context, t *testing.T, args []string) (envelope.Envelope, *bytes.Buffer) {
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

func extractJobID(t *testing.T, stderr string) string {
	t.Helper()
	re := regexp.MustCompile(`job ([0-9A-Z]{26}) state`)
	m := re.FindStringSubmatch(stderr)
	if len(m) != 2 {
		t.Fatalf("failed to parse job id from stderr: %s", stderr)
	}
	return m[1]
}
