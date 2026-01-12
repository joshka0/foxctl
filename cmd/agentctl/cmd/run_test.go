package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/jkatigb/agentctl/internal/platform/config"
)

var skillBinaryCache sync.Map

var stableGoModCache string
var stableGoBuildCache string

func init() {
	stableGoModCache = strings.TrimSpace(goEnv("GOMODCACHE"))
	stableGoBuildCache = strings.TrimSpace(goEnv("GOCACHE"))
}

func goEnv(key string) string {
	out, err := exec.Command("go", "env", key).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func withEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			continue
		}
		out = append(out, e)
	}
	return append(out, prefix+value)
}

func installTextGrepSkill(t *testing.T) config.Config {
	t.Helper()
	orig := newDaemonClient
	t.Cleanup(func() { newDaemonClient = orig })
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

	// Use normalized path (text_grep) to match how the Installer creates directories
	dest := filepath.Join(cfg.Paths.Skills, "text_grep")
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
	// Use normalized path (http_openapi) to match how the Installer creates directories
	dest := filepath.Join(cfg.Paths.Skills, "http_openapi")
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

func installTodoSkill(t *testing.T) config.Config {
	t.Helper()
	orig := newDaemonClient
	t.Cleanup(func() { newDaemonClient = orig })
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

	dest := filepath.Join(cfg.Paths.Skills, filepath.FromSlash("todo/manage"))
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatalf("skill dir: %v", err)
	}
	copySkillFile(t, filepath.Join(repoRoot(t), "skills", "todo", "skill.yaml"), filepath.Join(dest, "skill.yaml"))

	binaryPath := filepath.Join(dest, "bin")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}
	installSkillBinary(t, binaryPath, "./skills/todo")
	return cfg
}

func buildSkillBinaryFromSource(t *testing.T, dest, pkg string) {
	t.Helper()
	args := []string{"build", "-o", dest, pkg}
	cmd := exec.Command("go", args...)
	cmd.Dir = repoRoot(t)
	// Skills are pure Go and don't require CGO. Force CGO_ENABLED=0 to avoid
	// inheriting CGO_ENABLED=1 from test runners (e.g., `make test-cgo-short`),
	// which can cause CGO toolchain issues on some systems (see Gotchas G1).
	env := append([]string{}, os.Environ()...)
	env = withEnv(env, "CGO_ENABLED", "0")
	env = withEnv(env, "GOFLAGS", "-modcacherw -buildvcs=false")
	if stableGoModCache != "" {
		env = withEnv(env, "GOMODCACHE", stableGoModCache)
	}
	if stableGoBuildCache != "" {
		env = withEnv(env, "GOCACHE", stableGoBuildCache)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build %s: %v\n%s", pkg, err, string(out))
	}
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
	t.Helper()
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
