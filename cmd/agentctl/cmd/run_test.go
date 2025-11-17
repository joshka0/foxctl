package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

func TestRunCommandEmitsCompleteMeta(t *testing.T) {
	cfg := installTextGrepSkill(t)
	inputDir := t.TempDir()
	sample := filepath.Join(inputDir, "sample.txt")
	var buf bytes.Buffer
	for i := 0; i < 10; i++ {
		if _, err := fmt.Fprintf(&buf, "needle line %d\n", i); err != nil {
			t.Fatalf("build sample: %v", err)
		}
	}
	if err := os.WriteFile(sample, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	cmd := newRunCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{
		"--input", fmt.Sprintf(`{"path":%q,"pattern":"needle"}`, inputDir),
		"--workspace", inputDir,
		"text/grep",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("run command: %v\nstderr: %s", err, stderr.String())
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if _, err := time.Parse(time.RFC3339, env.Meta.TS); err != nil {
		t.Fatalf("meta.ts not RFC3339: %v", err)
	}
	if env.Meta.Source != "run" {
		t.Fatalf("expected meta.source=run got %q", env.Meta.Source)
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be a map got %T", env.Data)
	}
	artifact, _ := data["artifact"].(string)
	if artifact == "" {
		t.Fatalf("expected artifact in data")
	}
	if env.Meta.CASDigest != artifact {
		t.Fatalf("meta.cas_digest %q does not match artifact %q", env.Meta.CASDigest, artifact)
	}
}

func TestSkillsRunProducesInlineEnvelope(t *testing.T) {
	cfg := installTextGrepSkill(t)
	file := filepath.Join(t.TempDir(), "small.txt")
	if err := os.WriteFile(file, []byte("only once\nsecond line\n"), 0o644); err != nil {
		t.Fatalf("write small file: %v", err)
	}

	cmd := newSkillsRunCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{
		"--input", fmt.Sprintf(`{"path":%q,"pattern":"only"}`, file),
		"--workspace", filepath.Dir(file),
		"text/grep",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skills run: %v\nstderr: %s", err, stderr.String())
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Meta.CASDigest != "" {
		t.Fatalf("expected no cas digest for inline results, got %q", env.Meta.CASDigest)
	}
}

func TestRunCommandRememberSavesMemory(t *testing.T) {
	cfg := installTextGrepSkill(t)
	workdir := t.TempDir()
	sample := filepath.Join(workdir, "file.txt")
	if err := os.WriteFile(sample, []byte("needle here"), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}
	cmd := newRunCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	cmd.SetArgs([]string{
		"--input", fmt.Sprintf(`{"path":%q,"pattern":"needle"}`, workdir),
		"--remember", "remembered",
		"--workspace", workdir,
		"text/grep",
	})
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("run command: %v (stderr=%s)", err, stderr.String())
	}
	if stderr.Len() > 0 {
		t.Logf("stderr: %s", stderr.String())
	}
	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode run envelope: %v", err)
	}
	if env.Meta.Workspace != workdir {
		t.Fatalf("expected meta.workspace=%s got %s", workdir, env.Meta.Workspace)
	}
}

var skillBinaryCache sync.Map

func installTextGrepSkill(t *testing.T) config.Config {
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

	for _, dir := range []string{cfg.Home, cfg.Paths.CAS, cfg.Paths.Jobs, cfg.Paths.Cache, cfg.Paths.Skills} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	dest := filepath.Join(cfg.Paths.Skills, filepath.FromSlash("text/grep"))
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatalf("skill dir: %v", err)
	}
	copySkillFile(t, filepath.Join(repoRoot(t), "skills", "text_grep", "skill.yaml"), filepath.Join(dest, "skill.yaml"))

	binaryPath := filepath.Join(dest, "bin")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}
	installSkillBinary(t, binaryPath, "./skills/text_grep")
	return cfg
}

func installHTTPOpenAPISkill(t *testing.T, cfg config.Config) {
	t.Helper()
	dest := filepath.Join(cfg.Paths.Skills, filepath.FromSlash("http/openapi"))
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatalf("skill dir: %v", err)
	}
	copySkillFile(t, filepath.Join(repoRoot(t), "skills", "http_openapi", "skill.yaml"), filepath.Join(dest, "skill.yaml"))
	binaryPath := filepath.Join(dest, "bin")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}
	installSkillBinary(t, binaryPath, "./skills/http_openapi")
}

func installFSLsSkill(t *testing.T, cfg config.Config) {
	t.Helper()
	dest := filepath.Join(cfg.Paths.Skills, filepath.FromSlash("fs/ls"))
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatalf("fs/ls skill dir: %v", err)
	}
	copySkillFile(t, filepath.Join(repoRoot(t), "skills", "fs_ls", "skill.yaml"), filepath.Join(dest, "skill.yaml"))
	binaryPath := filepath.Join(dest, "bin")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}
	installSkillBinary(t, binaryPath, "./skills/fs_ls")
}

func installFSReadSkill(t *testing.T, cfg config.Config) {
	t.Helper()
	dest := filepath.Join(cfg.Paths.Skills, filepath.FromSlash("fs/read"))
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatalf("fs/read skill dir: %v", err)
	}
	copySkillFile(t, filepath.Join(repoRoot(t), "skills", "fs_read", "skill.yaml"), filepath.Join(dest, "skill.yaml"))
	binaryPath := filepath.Join(dest, "bin")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}
	installSkillBinary(t, binaryPath, "./skills/fs_read")
}

func buildSkillBinaryFromSource(t *testing.T, dest, pkg string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", dest, pkg)
	cmd.Dir = repoRoot(t)
	env := append([]string{}, os.Environ()...)
	env = append(env, "CGO_ENABLED=1", "GOFLAGS=-modcacherw")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build %s: %v\n%s", pkg, err, string(out))
	}
}

func buildAgentctlBinary(t *testing.T) string {
	t.Helper()
	destDir := t.TempDir()
	bin := filepath.Join(destDir, "agentctl")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/agentctl")
	cmd.Dir = repoRoot(t)
	env := append([]string{}, os.Environ()...)
	env = append(env, "CGO_ENABLED=1", "GOFLAGS=-modcacherw")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build agentctl: %v\n%s", err, string(out))
	}
	return bin
}

func copySkillFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

func installSkillBinary(t *testing.T, dest, pkg string) {
	t.Helper()
	src := cachedSkillBinary(t, pkg)
	copySkillFile(t, src, dest)
	if err := os.Chmod(dest, 0o755); err != nil {
		t.Fatalf("chmod skill binary: %v", err)
	}
}

func cachedSkillBinary(t *testing.T, pkg string) string {
	if bin, ok := skillBinaryCache.Load(pkg); ok {
		return bin.(string)
	}
	tmpDir, err := os.MkdirTemp("", "agentctl-skill-")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	name := "skill"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(tmpDir, name)
	buildSkillBinaryFromSource(t, out, pkg)
	skillBinaryCache.Store(pkg, out)
	return out
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("determine repo root: no caller info")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}
