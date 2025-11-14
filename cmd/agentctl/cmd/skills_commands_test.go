package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
)

func TestSkillsInstallCommandInstallsExecSkill(t *testing.T) {
	cfg := newTestConfig(t)
	manifestPath := filepath.Join(repoRoot(t), "skills", "text_grep", "skill.yaml")
	binaryPath := cachedSkillBinary(t, "./skills/text_grep")

	cmd := newSkillsInstallCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"--manifest", manifestPath, "--binary", binaryPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("install command: %v (stderr=%s)", err, stderr.String())
	}

	dest := filepath.Join(cfg.Paths.Skills, filepath.FromSlash("text/grep"))
	if _, err := os.Stat(filepath.Join(dest, "skill.yaml")); err != nil {
		t.Fatalf("expected skill.yaml to be installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "bin")); err != nil {
		t.Fatalf("expected binary to be installed: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "installed text/grep" {
		t.Fatalf("unexpected install output: %q", got)
	}
}

func TestSkillsListCommandListsInstalledSkills(t *testing.T) {
	cfg := installTextGrepSkill(t)
	installHTTPOpenAPISkill(t, cfg)

	cmd := newSkillsListCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skills list: %v (stderr=%s)", err, stderr.String())
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode list envelope: %v", err)
	}
	skills, ok := env.Data.(map[string]any)["skills"].([]any)
	if !ok {
		t.Fatalf("expected skills array in response: %T", env.Data)
	}
	if len(skills) < 2 {
		t.Fatalf("expected at least two skills, got %d", len(skills))
	}
}

func TestSkillsDescribeCommandProvidesDetails(t *testing.T) {
	cfg := installTextGrepSkill(t)

	cmd := newSkillsDescribeCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"text/grep"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skills describe: %v (stderr=%s)", err, stderr.String())
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode describe envelope: %v", err)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map response, got %T", env.Data)
	}
	if got := data["name"]; got != "text/grep" {
		t.Fatalf("expected skill name text/grep, got %v", got)
	}
}

func TestSkillsSearchCommandMatchesByName(t *testing.T) {
	cfg := installTextGrepSkill(t)
	installHTTPOpenAPISkill(t, cfg)

	cmd := newSkillsSearchCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"grep"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skills search: %v (stderr=%s)", err, stderr.String())
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode search envelope: %v", err)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map response, got %T", env.Data)
	}
	skills, _ := data["skills"].([]any)
	if len(skills) != 1 {
		t.Fatalf("expected one match, got %d", len(skills))
	}
}

func TestSkillsUninstallCommandRemovesSkill(t *testing.T) {
	cfg := installTextGrepSkill(t)

	cmd := newSkillsUninstallCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"text/grep"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skills uninstall: %v (stderr=%s)", err, stderr.String())
	}

	if _, err := os.Stat(filepath.Join(cfg.Paths.Skills, filepath.FromSlash("text/grep"))); !os.IsNotExist(err) {
		t.Fatalf("expected skill directory removed, got err=%v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "uninstalled text/grep" {
		t.Fatalf("unexpected uninstall output: %q", got)
	}
}

func TestSkillsUpgradeCommandUpdatesManifest(t *testing.T) {
	cfg := installTextGrepSkill(t)
	manifestSrc := filepath.Join(repoRoot(t), "skills", "text_grep", "skill.yaml")
	data, err := os.ReadFile(manifestSrc)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	replaced := strings.Replace(string(data), "version: 0.1.0", "version: 0.2.0", 1)
	manifestPath := filepath.Join(t.TempDir(), "upgraded.yaml")
	if err := os.WriteFile(manifestPath, []byte(replaced), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	binaryPath := cachedSkillBinary(t, "./skills/text_grep")

	cmd := newSkillsUpgradeCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"text/grep", "--manifest", manifestPath, "--binary", binaryPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skills upgrade: %v (stderr=%s)", err, stderr.String())
	}

	installed, err := os.ReadFile(filepath.Join(cfg.Paths.Skills, filepath.FromSlash("text/grep"), "skill.yaml"))
	if err != nil {
		t.Fatalf("read installed manifest: %v", err)
	}
	if !strings.Contains(string(installed), "version: 0.2.0") {
		t.Fatalf("expected manifest version updated, got:\n%s", string(installed))
	}
	if got := strings.TrimSpace(stdout.String()); got != "upgraded text/grep to version 0.2.0" {
		t.Fatalf("unexpected upgrade output: %q", got)
	}
}

func newTestConfig(t *testing.T) config.Config {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentHome := filepath.Join(tmp, ".agentctl")
	cfg := config.Config{
		Home:           agentHome,
		InlineOutputKB: config.DefaultInlineOutputKB,
		MaxCaptureKB:   config.DefaultMaxCaptureKB,
		Paths: config.Paths{
			CAS:    filepath.Join(agentHome, "cas"),
			Jobs:   filepath.Join(agentHome, "jobs"),
			Cache:  filepath.Join(agentHome, "cache"),
			Skills: filepath.Join(agentHome, "skills"),
		},
	}
	return cfg
}
