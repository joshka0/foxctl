//go:build integration

// Package integration contains integration tests for agentctl subsystems.
// These tests require the "integration" build tag to run.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSWEGrep_CandidatesToSnippets tests the full candidates → SWE Grep flow.
// This is a D4 integration test per Phase 5 spec.
func TestSWEGrep_CandidatesToSnippets(t *testing.T) {
	repoRoot := repoRootFromCWD(t)
	runRoot := t.TempDir()
	prepareSweGrepDist(t, repoRoot, runRoot)
	// Ensure we don't accidentally use globally installed skills.
	t.Setenv("HOME", t.TempDir())

	// Skip if agentctl binary not available
	binPath := findAgentctlBinary(t)
	if binPath == "" {
		t.Skip("agentctl binary not found; run 'make skills-build' first")
	}

	// Create temp workspace
	workspaceDir := t.TempDir()

	// Create test files
	files := map[string]string{
		"auth/login.go": `package auth

import (
	"context"
	"net/http"
)

// Login handles user authentication.
// It validates credentials and creates a session.
func Login(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")

	if !validateCredentials(ctx, username, password) {
		http.Error(w, "invalid login", http.StatusUnauthorized)
		return
	}

	createSession(ctx, w, username)
	w.WriteHeader(http.StatusOK)
}

func validateCredentials(ctx context.Context, username, password string) bool {
	return username != "" && password != ""
}

func createSession(ctx context.Context, w http.ResponseWriter, username string) {}
`,
		"auth/logout.go": `package auth

import (
	"context"
	"net/http"
)

// Logout terminates the user session.
func Logout(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	session := getSession(ctx, r)
	if session != nil {
		destroySession(ctx, w, session)
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func getSession(ctx context.Context, r *http.Request) any { return nil }
func destroySession(ctx context.Context, w http.ResponseWriter, session any) {}
`,
		"config/config.go": `package config

import "os"

// Config holds application configuration.
type Config struct {
	Port     int
	Host     string
	LogLevel string
}

// Load reads configuration from environment.
func Load() *Config {
	return &Config{
		Port:     8080,
		Host:     os.Getenv("HOST"),
		LogLevel: os.Getenv("LOG_LEVEL"),
	}
}
`,
	}

	// Write test files
	for path, content := range files {
		fullPath := filepath.Join(workspaceDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	t.Run("login_question_finds_auth_snippets", func(t *testing.T) {
		input := map[string]any{
			"workspace_id": "test-ws",
			"question":     "How does user login work?",
			"candidates": []map[string]any{
				{"path": "auth/login.go", "priority": 0.95},
				{"path": "auth/logout.go", "priority": 0.8},
				{"path": "config/config.go", "priority": 0.5},
			},
		}

		envelope := runSWEGrep(t, binPath, runRoot, workspaceDir, input)

		// Verify envelope structure
		assertEnvelopeOK(t, envelope)

		data := getMap(t, envelope, "data")
		summary := getMap(t, data, "summary")

		// Should consider all 3 files
		filesConsidered := int(getFloat(t, summary, "files_considered"))
		if filesConsidered != 3 {
			t.Errorf("files_considered = %d, want 3", filesConsidered)
		}

		// Should find relevant files
		filesRelevant := int(getFloat(t, summary, "files_relevant"))
		if filesRelevant < 1 {
			t.Errorf("files_relevant = %d, want >= 1", filesRelevant)
		}

		// Should emit snippets
		snippetsEmitted := int(getFloat(t, summary, "snippets_emitted"))
		if snippetsEmitted < 1 {
			t.Errorf("snippets_emitted = %d, want >= 1", snippetsEmitted)
		}

		// Verify snippets_inline contains login-related content
		snippetsInline := getSlice(t, data, "snippets_inline")
		if len(snippetsInline) == 0 {
			t.Fatal("snippets_inline is empty")
		}

		firstSnippet, ok := snippetsInline[0].(map[string]any)
		if !ok {
			t.Fatalf("snippets_inline[0] is not a map: got %T", snippetsInline[0])
		}
		if !strings.Contains(getString(t, firstSnippet, "file"), "login.go") {
			t.Errorf("first snippet file = %q, want to contain 'login.go'", firstSnippet["file"])
		}
		if !strings.Contains(getString(t, firstSnippet, "preview"), "Login") {
			t.Errorf("first snippet preview should contain 'Login'")
		}
	})

	t.Run("config_question_finds_config_snippets", func(t *testing.T) {
		input := map[string]any{
			"workspace_id": "test-ws",
			"question":     "How is configuration loaded?",
			"candidates": []map[string]any{
				{"path": "config/config.go", "priority": 0.9},
			},
		}

		envelope := runSWEGrep(t, binPath, runRoot, workspaceDir, input)
		assertEnvelopeOK(t, envelope)

		data := getMap(t, envelope, "data")
		snippetsInline := getSlice(t, data, "snippets_inline")

		if len(snippetsInline) == 0 {
			t.Fatal("snippets_inline is empty")
		}

		// Should find config-related content
		found := false
		for _, s := range snippetsInline {
			snippet, ok := s.(map[string]any)
			if !ok {
				continue
			}
			preview, ok := snippet["preview"].(string)
			if ok && strings.Contains(preview, "Config") {
				found = true
				break
			}
		}
		if !found {
			t.Error("no snippet contains 'Config'")
		}
	})

	t.Run("limits_respected", func(t *testing.T) {
		input := map[string]any{
			"workspace_id": "test-ws",
			"question":     "login",
			"candidates": []map[string]any{
				{"path": "auth/login.go"},
				{"path": "auth/logout.go"},
				{"path": "config/config.go"},
			},
			"limits": map[string]any{
				"max_files":    2,
				"max_snippets": 1,
			},
		}

		envelope := runSWEGrep(t, binPath, runRoot, workspaceDir, input)
		assertEnvelopeOK(t, envelope)

		data := getMap(t, envelope, "data")
		summary := getMap(t, data, "summary")

		snippetsEmitted := int(getFloat(t, summary, "snippets_emitted"))
		if snippetsEmitted > 1 {
			t.Errorf("snippets_emitted = %d, want <= 1 (max_snippets limit)", snippetsEmitted)
		}
	})

	t.Run("cas_artifact_present", func(t *testing.T) {
		candidates := make([]map[string]any, 0, 12)
		for i := 0; i < 6; i++ {
			candidates = append(candidates,
				map[string]any{"path": "auth/login.go"},
				map[string]any{"path": "auth/logout.go"},
			)
		}
		input := map[string]any{
			"workspace_id": "test-ws",
			"question":     "login",
			"candidates":   candidates,
		}

		envelope := runSWEGrep(t, binPath, runRoot, workspaceDir, input, "AGENTCTL_INLINE_OUTPUT_KB=1")
		assertEnvelopeOK(t, envelope)

		data := getMap(t, envelope, "data")

		// Should have CAS artifact
		artifact, ok := data["artifact"].(string)
		if !ok || artifact == "" {
			t.Error("artifact digest missing")
		}
		if !strings.HasPrefix(artifact, "sha256:") {
			t.Errorf("artifact = %q, want sha256: prefix", artifact)
		}

		// meta.cas_digest is optional but must match data.artifact when present
		meta := getMap(t, envelope, "meta")
		if v, ok := meta["cas_digest"]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" && strings.TrimSpace(s) != artifact {
				t.Errorf("meta.cas_digest = %q, want %q", s, artifact)
			}
		}
	})

	t.Run("cas_artifact_omitted_when_inline_small", func(t *testing.T) {
		input := map[string]any{
			"workspace_id": "test-ws",
			"question":     "login",
			"candidates": []map[string]any{
				{"path": "auth/login.go"},
			},
		}

		envelope := runSWEGrep(t, binPath, runRoot, workspaceDir, input, "AGENTCTL_INLINE_OUTPUT_KB=128")
		assertEnvelopeOK(t, envelope)

		data := getMap(t, envelope, "data")
		if _, ok := data["artifact"]; ok {
			t.Fatalf("expected artifact to be omitted for small output")
		}

		meta := getMap(t, envelope, "meta")
		if v, ok := meta["cas_digest"]; ok {
			if s, ok := v.(string); ok && s != "" {
				t.Fatalf("expected meta.cas_digest to be omitted/empty for small output, got %q", s)
			}
		}
	})
}

// runSWEGrep invokes the code/swe_grep skill and returns the parsed envelope.
func runSWEGrep(t *testing.T, binPath, runRoot, workspaceDir string, input map[string]any, extraEnv ...string) map[string]any {
	t.Helper()

	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, "run", "code/swe_grep", "--cache=off", "--workspace", workspaceDir, "--input", string(inputJSON))
	cmd.Dir = runRoot
	cmd.Env = os.Environ()
	for _, kv := range extraEnv {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}
		prefix := parts[0] + "="
		filtered := cmd.Env[:0]
		for _, existing := range cmd.Env {
			if !strings.HasPrefix(existing, prefix) {
				filtered = append(filtered, existing)
			}
		}
		cmd.Env = append(filtered, kv)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("run swe_grep: %v\nstderr: %s", err, stderr.String())
	}

	// Parse envelope from stdout (skip job status line)
	output := stdout.String()
	lines := strings.SplitN(output, "\n", 2)
	envelopeJSON := output
	if len(lines) > 1 && strings.HasPrefix(lines[0], "job ") {
		envelopeJSON = lines[1]
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(envelopeJSON), &envelope); err != nil {
		t.Fatalf("parse envelope: %v\noutput: %s", err, output)
	}

	return envelope
}

func repoRootFromCWD(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "../.."))
}

func prepareSweGrepDist(t *testing.T, repoRoot, runRoot string) {
	t.Helper()
	outDir := filepath.Join(runRoot, "dist", "skills", "code_swe_grep")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir swe_grep dist: %v", err)
	}

	manifestSrc := filepath.Join(repoRoot, "skills", "code_swe_grep", "skill.yaml")
	manifestDst := filepath.Join(outDir, "skill.yaml")
	data, err := os.ReadFile(manifestSrc)
	if err != nil {
		t.Fatalf("read swe_grep manifest: %v", err)
	}
	if err := os.WriteFile(manifestDst, data, 0o644); err != nil {
		t.Fatalf("write swe_grep manifest: %v", err)
	}

	binOut := filepath.Join(outDir, "bin")
	cmd := exec.Command("go", "build", "-o", binOut, "./skills/code_swe_grep")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build swe_grep skill: %v\n%s", err, string(output))
	}
}

// assertEnvelopeOK verifies the envelope has status "ok" and version 1.
func assertEnvelopeOK(t *testing.T, envelope map[string]any) {
	t.Helper()

	version, ok := envelope["version"].(float64)
	if !ok || version != 1 {
		t.Errorf("envelope.version = %v, want 1", envelope["version"])
	}

	status, ok := envelope["status"].(string)
	if !ok || status != "ok" {
		t.Errorf("envelope.status = %q, want 'ok'", status)
		if errObj, ok := envelope["error"].(map[string]any); ok {
			t.Logf("error: %+v", errObj)
		}
	}

	command, ok := envelope["command"].(string)
	if !ok || command != "code/swe_grep" {
		t.Errorf("envelope.command = %q, want 'code/swe_grep'", command)
	}
}

// getMap safely extracts a map[string]any from a parent map.
func getMap(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("%q is not a map[string]any: got %T", key, parent[key])
	}
	return v
}

// getSlice safely extracts a []any from a parent map.
func getSlice(t *testing.T, parent map[string]any, key string) []any {
	t.Helper()
	v, ok := parent[key].([]any)
	if !ok {
		t.Fatalf("%q is not a []any: got %T", key, parent[key])
	}
	return v
}

// getFloat safely extracts a float64 from a parent map.
func getFloat(t *testing.T, parent map[string]any, key string) float64 {
	t.Helper()
	v, ok := parent[key].(float64)
	if !ok {
		t.Fatalf("%q is not a float64: got %T", key, parent[key])
	}
	return v
}

// getString safely extracts a string from a parent map.
func getString(t *testing.T, parent map[string]any, key string) string {
	t.Helper()
	v, ok := parent[key].(string)
	if !ok {
		t.Fatalf("%q is not a string: got %T", key, parent[key])
	}
	return v
}

// findAgentctlBinary locates the agentctl binary.
func findAgentctlBinary(t *testing.T) string {
	t.Helper()

	// Try common locations
	candidates := []string{
		"./bin/agentctl",
		"../../bin/agentctl",
		os.Getenv("AGENTCTL_BIN"),
	}

	for _, path := range candidates {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			// filepath.Abs error is safe to ignore for existing paths.
			absPath, _ := filepath.Abs(path) //nolint:errcheck
			return absPath
		}
	}

	return ""
}
