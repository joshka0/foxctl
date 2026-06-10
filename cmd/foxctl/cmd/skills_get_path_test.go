package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestSkillsGetFoxctlOutputsTextGuide(t *testing.T) {
	cfg := newTestConfig(t)
	cmd := newSkillsGetCommand()
	cmd.SilenceUsage = true
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"foxctl"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skills get foxctl: %v (stderr=%s)", err, stderr.String())
	}

	got := stdout.String()
	for _, want := range []string{"# foxctl", "foxctl skills get <name>", "protocol envelopes"} {
		if !strings.Contains(got, want) {
			t.Fatalf("guide missing %q\n%s", want, got)
		}
	}
	if strings.HasPrefix(strings.TrimSpace(got), "{") {
		t.Fatalf("default guide output should be text, got JSON-like output:\n%s", got)
	}
}

func TestSkillsGetSkillOutputsGeneratedGuide(t *testing.T) {
	cfg := installTextGrepManifestOnly(t)
	t.Setenv("FOXCTL_SKILLS_PATH", cfg.Paths.Skills)
	cmd := newSkillsGetCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"text/grep"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skills get text/grep: %v (stderr=%s)", err, stderr.String())
	}

	got := stdout.String()
	for _, want := range []string{"# text/grep", "Recursive regex search", "foxctl skills run text/grep", "--pattern <pattern>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("guide missing %q\n%s", want, got)
		}
	}
}

func TestSkillsGetSkillOutputsJSONEnvelope(t *testing.T) {
	cfg := installTextGrepManifestOnly(t)
	t.Setenv("FOXCTL_SKILLS_PATH", cfg.Paths.Skills)
	cmd := newSkillsGetCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"text/grep", "--format", "json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skills get text/grep json: %v (stderr=%s)", err, stderr.String())
	}

	data := decodeEnvelopeData(t, stdout.Bytes())
	if got := data["name"]; got != "text/grep" {
		t.Fatalf("unexpected guide name: %v", got)
	}
	if guide, ok := data["guide"].(string); !ok || !strings.Contains(guide, "Recursive regex search") {
		t.Fatalf("unexpected guide: %#v", data["guide"])
	}
	if path, ok := data["path"].(string); !ok || path == "" {
		t.Fatalf("expected guide path, got %#v", data["path"])
	}
}

func TestSkillsGetPrefersSkillGuideFile(t *testing.T) {
	cfg := installTextGrepManifestOnly(t)
	t.Setenv("FOXCTL_SKILLS_PATH", cfg.Paths.Skills)
	skillPath := filepath.Join(cfg.Paths.Skills, "text_grep", "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("# Agent guide\n\nUse the agent workflow."), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	readmePath := filepath.Join(cfg.Paths.Skills, "text_grep", "README.md")
	if err := os.WriteFile(readmePath, []byte("# Human guide\n\nUse the human workflow."), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	cmd := newSkillsGetCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"text/grep"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skills get text/grep authored guide: %v (stderr=%s)", err, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "Use the agent workflow.") {
		t.Fatalf("expected SKILL.md guide, got:\n%s", got)
	}
	if strings.Contains(got, "Use the human workflow.") {
		t.Fatalf("expected SKILL.md to take precedence over README.md, got:\n%s", got)
	}
}

func TestSkillsGetMissingSkillReturnsError(t *testing.T) {
	cfg := newTestConfig(t)

	stdout, _, err := executeRootForSkillsTest(t, cfg, "skills", "get", "missing/skill")
	if err == nil {
		t.Fatal("expected missing skill error")
	}
	if stdout != "" {
		t.Fatalf("missing skill should not write stdout, got:\n%s", stdout)
	}
	for _, want := range []string{"missing/skill", "foxctl skills"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("missing skill error should mention %q, got %v", want, err)
		}
	}
}

func TestSkillsPathPrintsRootAndSkillPath(t *testing.T) {
	cfg := installTextGrepManifestOnly(t)
	t.Setenv("FOXCTL_SKILLS_PATH", cfg.Paths.Skills)

	cmd := newSkillsPathCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skills path root: %v (stderr=%s)", err, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != cfg.Paths.Skills {
		t.Fatalf("expected skills root %q, got %q", cfg.Paths.Skills, got)
	}

	for _, name := range []string{"text/grep", "text_grep"} {
		cmd = newSkillsPathCommand()
		cmd.SetContext(config.WithContext(context.Background(), cfg))
		stdout = &bytes.Buffer{}
		stderr = &bytes.Buffer{}
		cmd.SetOut(stdout)
		cmd.SetErr(stderr)
		cmd.SetArgs([]string{name})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("skills path %s: %v (stderr=%s)", name, err, stderr.String())
		}
		if got, want := strings.TrimSpace(stdout.String()), filepath.Join(cfg.Paths.Skills, "text_grep"); got != want {
			t.Fatalf("expected skill path %q for %s, got %q", want, name, got)
		}
	}
}

func TestSkillsPathUsesCanonicalResolverSearchOrder(t *testing.T) {
	cfg := newTestConfig(t)
	staleDir := filepath.Join(cfg.Paths.Skills, "text_grep")
	if err := os.MkdirAll(staleDir, 0o755); err != nil {
		t.Fatalf("mkdir stale skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staleDir, "skill.yaml"), []byte("not: [valid"), 0o644); err != nil {
		t.Fatalf("write stale skill manifest: %v", err)
	}

	envRoot := t.TempDir()
	envSkillDir := filepath.Join(envRoot, "text_grep")
	if err := os.MkdirAll(envSkillDir, 0o755); err != nil {
		t.Fatalf("mkdir env skill dir: %v", err)
	}
	copySkillFile(t, filepath.Join(repoRoot(t), "skills", "text_grep", "skill.yaml"), filepath.Join(envSkillDir, "skill.yaml"))
	t.Setenv("FOXCTL_SKILLS_PATH", envRoot)

	cmd := newSkillsPathCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"text/grep"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skills path text/grep: %v (stderr=%s)", err, stderr.String())
	}
	if got, want := strings.TrimSpace(stdout.String()), envSkillDir; got != want {
		t.Fatalf("expected canonical resolver path %q, got %q", want, got)
	}
}

func TestSkillsPathMissingSkillReturnsError(t *testing.T) {
	cfg := newTestConfig(t)

	stdout, _, err := executeRootForSkillsTest(t, cfg, "skills", "path", "missing/skill")
	if err == nil {
		t.Fatal("expected missing skill error")
	}
	if stdout != "" {
		t.Fatalf("missing skill should not write stdout, got:\n%s", stdout)
	}
	for _, want := range []string{"missing/skill", "foxctl skills"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("missing skill error should mention %q, got %v", want, err)
		}
	}
}

func executeRootForSkillsTest(t *testing.T, cfg config.Config, args ...string) (string, string, error) {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	rootCmd.SetContext(config.WithContext(context.Background(), cfg))
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetContext(context.Background())
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	err := rootCmd.Execute()
	return stdout.String(), stderr.String(), err
}
