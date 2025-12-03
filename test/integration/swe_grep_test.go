// Package integration contains integration tests for agentctl subsystems.
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
)

// TestSWEGrep_CandidatesToSnippets tests the full candidates → SWE Grep flow.
// This is a D4 integration test per Phase 5 spec.
func TestSWEGrep_CandidatesToSnippets(t *testing.T) {
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

func getSession(ctx context.Context, r *http.Request) interface{} { return nil }
func destroySession(ctx context.Context, w http.ResponseWriter, session interface{}) {}
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

		envelope := runSWEGrep(t, binPath, workspaceDir, input)

		// Verify envelope structure
		assertEnvelopeOK(t, envelope)

		data := envelope["data"].(map[string]any)
		summary := data["summary"].(map[string]any)

		// Should consider all 3 files
		filesConsidered := int(summary["files_considered"].(float64))
		if filesConsidered != 3 {
			t.Errorf("files_considered = %d, want 3", filesConsidered)
		}

		// Should find relevant files
		filesRelevant := int(summary["files_relevant"].(float64))
		if filesRelevant < 1 {
			t.Errorf("files_relevant = %d, want >= 1", filesRelevant)
		}

		// Should emit snippets
		snippetsEmitted := int(summary["snippets_emitted"].(float64))
		if snippetsEmitted < 1 {
			t.Errorf("snippets_emitted = %d, want >= 1", snippetsEmitted)
		}

		// Verify snippets_inline contains login-related content
		snippetsInline := data["snippets_inline"].([]any)
		if len(snippetsInline) == 0 {
			t.Fatal("snippets_inline is empty")
		}

		firstSnippet := snippetsInline[0].(map[string]any)
		if !strings.Contains(firstSnippet["file"].(string), "login.go") {
			t.Errorf("first snippet file = %q, want to contain 'login.go'", firstSnippet["file"])
		}
		if !strings.Contains(firstSnippet["preview"].(string), "Login") {
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

		envelope := runSWEGrep(t, binPath, workspaceDir, input)
		assertEnvelopeOK(t, envelope)

		data := envelope["data"].(map[string]any)
		snippetsInline := data["snippets_inline"].([]any)

		if len(snippetsInline) == 0 {
			t.Fatal("snippets_inline is empty")
		}

		// Should find config-related content
		found := false
		for _, s := range snippetsInline {
			snippet := s.(map[string]any)
			if strings.Contains(snippet["preview"].(string), "Config") {
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

		envelope := runSWEGrep(t, binPath, workspaceDir, input)
		assertEnvelopeOK(t, envelope)

		data := envelope["data"].(map[string]any)
		summary := data["summary"].(map[string]any)

		snippetsEmitted := int(summary["snippets_emitted"].(float64))
		if snippetsEmitted > 1 {
			t.Errorf("snippets_emitted = %d, want <= 1 (max_snippets limit)", snippetsEmitted)
		}
	})

	t.Run("cas_artifact_present", func(t *testing.T) {
		input := map[string]any{
			"workspace_id": "test-ws",
			"question":     "login",
			"candidates": []map[string]any{
				{"path": "auth/login.go"},
				{"path": "auth/logout.go"},
			},
		}

		envelope := runSWEGrep(t, binPath, workspaceDir, input)
		assertEnvelopeOK(t, envelope)

		data := envelope["data"].(map[string]any)

		// Should have CAS artifact
		artifact, ok := data["artifact"].(string)
		if !ok || artifact == "" {
			t.Error("artifact digest missing")
		}
		if !strings.HasPrefix(artifact, "sha256:") {
			t.Errorf("artifact = %q, want sha256: prefix", artifact)
		}

		kind, ok := data["artifact_kind"].(string)
		if !ok || kind != "application/x-swe-grep-snippets+ndjson" {
			t.Errorf("artifact_kind = %q, want application/x-swe-grep-snippets+ndjson", kind)
		}

		// meta.cas_digest should match data.artifact
		meta := envelope["meta"].(map[string]any)
		casDigest, ok := meta["cas_digest"].(string)
		if !ok || casDigest != artifact {
			t.Errorf("meta.cas_digest = %q, want %q", casDigest, artifact)
		}
	})
}

// runSWEGrep invokes the code/swe_grep skill and returns the parsed envelope.
func runSWEGrep(t *testing.T, binPath, workspaceDir string, input map[string]any) map[string]any {
	t.Helper()

	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*1e9) // 30s
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, "run", "code/swe_grep", "--cache=off", "--input", string(inputJSON))
	cmd.Dir = workspaceDir
	cmd.Env = append(os.Environ(), "AGENTCTL_WORKSPACE="+workspaceDir)

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
			absPath, _ := filepath.Abs(path)
			return absPath
		}
	}

	return ""
}
