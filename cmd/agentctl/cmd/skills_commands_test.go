package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
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

	// Installer normalizes skill names: text/grep -> text_grep (flat directory)
	dest := filepath.Join(cfg.Paths.Skills, "text_grep")
	if _, err := os.Stat(filepath.Join(dest, "skill.yaml")); err != nil {
		t.Fatalf("expected skill.yaml to be installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "bin")); err != nil {
		t.Fatalf("expected binary to be installed: %v", err)
	}
	data := decodeEnvelopeData(t, stdout.Bytes())
	if got := data["name"]; got != "text/grep" {
		t.Fatalf("unexpected skill name: %v", got)
	}
	if got := data["version"]; got != "0.1.0" {
		t.Fatalf("unexpected skill version: %v", got)
	}
	// Path uses normalized name (text_grep) not canonical name (text/grep)
	if got := data["path"]; got != filepath.Join(cfg.Paths.Skills, "text_grep") {
		t.Fatalf("unexpected skill path: %v", got)
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

func TestSkillsHelpCommandProvidesHelp(t *testing.T) {
	cfg := installTextGrepSkill(t)

	cmd := newSkillsHelpCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"text/grep", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skills help: %v (stderr=%s)", err, stderr.String())
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode help envelope: %v", err)
	}
	if env.Status != envelope.StatusOK {
		t.Fatalf("expected ok status, got %s", env.Status)
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map response, got %T", env.Data)
	}
	if got := data["skill"]; got != "text/grep" {
		t.Fatalf("expected skill text/grep, got %v", got)
	}
	if _, ok := data["parameters"]; !ok {
		t.Fatalf("expected parameters in help data")
	}

	helpVal, ok := data["help"].(map[string]any)
	if !ok {
		t.Fatalf("expected help object in help data, got %T", data["help"])
	}
	if got := helpVal["short"]; got != "Recursive regex search with include/exclude globs." {
		t.Fatalf("unexpected help.short: %v", got)
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
	skills, ok := data["skills"].([]any)
	if !ok {
		t.Fatalf("skills is not a slice: %T", data["skills"])
	}
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

	// Uninstall removes normalized path (text_grep)
	if _, err := os.Stat(filepath.Join(cfg.Paths.Skills, "text_grep")); !os.IsNotExist(err) {
		t.Fatalf("expected skill directory removed, got err=%v", err)
	}
	data := decodeEnvelopeData(t, stdout.Bytes())
	if got := data["name"]; got != "text/grep" {
		t.Fatalf("unexpected uninstall name: %v", got)
	}
	if got := data["path"]; got != filepath.Join(cfg.Paths.Skills, "text_grep") {
		t.Fatalf("unexpected uninstall path: %v", got)
	}
}

func TestSkillsUpgradeCommandUpdatesManifest(t *testing.T) {
	cfg := installTextGrepSkill(t)
	manifestSrc := filepath.Join(repoRoot(t), "skills", "text_grep", "skill.yaml")
	manifestData, err := os.ReadFile(manifestSrc)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	replaced := strings.Replace(string(manifestData), "version: 0.1.0", "version: 0.2.0", 1)
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

	// Upgrade writes to normalized path (text_grep)
	installed, err := os.ReadFile(filepath.Join(cfg.Paths.Skills, "text_grep", "skill.yaml"))
	if err != nil {
		t.Fatalf("read installed manifest: %v", err)
	}
	if !strings.Contains(string(installed), "version: 0.2.0") {
		t.Fatalf("expected manifest version updated, got:\n%s", string(installed))
	}
	envData := decodeEnvelopeData(t, stdout.Bytes())
	if got := envData["name"]; got != "text/grep" {
		t.Fatalf("unexpected upgrade skill: %v", got)
	}
	if got := envData["version"]; got != "0.2.0" {
		t.Fatalf("unexpected upgrade version: %v", got)
	}
	if got := envData["path"]; got != filepath.Join(cfg.Paths.Skills, "text_grep") {
		t.Fatalf("unexpected upgrade path: %v", got)
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

func decodeEnvelopeData(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var env envelope.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected object payload, got %T", env.Data)
	}
	return data
}
