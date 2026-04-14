package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/platform/config"
)

var skillBinaryCache sync.Map

var (
	stableGoModCache   string
	stableGoBuildCache string
)

type goListPackage struct {
	Dir          string
	GoFiles      []string
	CgoFiles     []string
	CFiles       []string
	CXXFiles     []string
	MFiles       []string
	HFiles       []string
	FFiles       []string
	SFiles       []string
	SwigFiles    []string
	SwigCXXFiles []string
	SysoFiles    []string
	EmbedFiles   []string
}

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

func newSkillTestConfig(t *testing.T) config.Config {
	t.Helper()
	orig := newDaemonClient
	t.Cleanup(func() { newDaemonClient = orig })
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	agentHome := filepath.Join(tmp, ".foxctl")
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
		Storage: config.StorageSettings{
			Root: filepath.Join(agentHome, "storage"),
		},
	}

	for _, dir := range []string{
		cfg.Home,
		cfg.Paths.CAS,
		cfg.Paths.Jobs,
		cfg.Paths.Cache,
		cfg.Paths.Skills,
		cfg.Storage.Root,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	return cfg
}

func installTextGrepSkill(t *testing.T) config.Config {
	t.Helper()
	cfg := newSkillTestConfig(t)
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

func installTextGrepManifestOnly(t *testing.T) config.Config {
	t.Helper()
	cfg := newSkillTestConfig(t)
	dest := filepath.Join(cfg.Paths.Skills, "text_grep")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatalf("skill dir: %v", err)
	}
	copySkillFile(t, filepath.Join(repoRoot(t), "skills", "text_grep", "skill.yaml"), filepath.Join(dest, "skill.yaml"))
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
	cfg := newSkillTestConfig(t)
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

func installTodoManifestOnly(t *testing.T) config.Config {
	t.Helper()
	cfg := newSkillTestConfig(t)
	dest := filepath.Join(cfg.Paths.Skills, filepath.FromSlash("todo/manage"))
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatalf("skill dir: %v", err)
	}
	copySkillFile(t, filepath.Join(repoRoot(t), "skills", "todo", "skill.yaml"), filepath.Join(dest, "skill.yaml"))
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
	out := buildOrReuseCachedSkillBinary(t, pkg)
	skillBinaryCache.Store(pkg, out)
	return out
}

func buildOrReuseCachedSkillBinary(t *testing.T, pkg string) string {
	t.Helper()
	cacheDir := skillBinaryDiskCacheDir(t)
	key := skillBinaryFingerprint(t, pkg)
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	out := filepath.Join(cacheDir, key+ext)
	if _, err := os.Stat(out); err == nil {
		return out
	}

	lockPath := out + ".lock"
	if waitForCachedSkillBinary(out, lockPath) {
		return out
	}

	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			if waitForCachedSkillBinary(out, lockPath) {
				return out
			}
		}
		t.Fatalf("create skill cache lock: %v", err)
	}
	lockFile.Close()
	defer os.Remove(lockPath)

	if _, err := os.Stat(out); err == nil {
		return out
	}

	tmpDir, err := os.MkdirTemp(cacheDir, "build-")
	if err != nil {
		t.Fatalf("create skill cache temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpOut := filepath.Join(tmpDir, "skill"+ext)
	buildSkillBinaryFromSource(t, tmpOut, pkg)
	if err := os.Rename(tmpOut, out); err != nil {
		if _, statErr := os.Stat(out); statErr == nil {
			return out
		}
		t.Fatalf("move cached skill binary into place: %v", err)
	}
	return out
}

func skillBinaryDiskCacheDir(t *testing.T) string {
	t.Helper()
	root := stableGoBuildCache
	if root == "" {
		root = filepath.Join(os.TempDir(), "foxctl-go-build-cache")
	}
	dir := filepath.Join(root, "foxctl-skill-binaries")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create skill binary cache dir: %v", err)
	}
	return dir
}

func waitForCachedSkillBinary(out, lockPath string) bool {
	for i := 0; i < 300; i++ {
		if _, err := os.Stat(out); err == nil {
			return true
		}
		if _, err := os.Stat(lockPath); os.IsNotExist(err) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func skillBinaryFingerprint(t *testing.T, pkg string) string {
	t.Helper()
	h := sha256.New()
	for _, part := range []string{runtime.GOOS, "\n", runtime.GOARCH, "\n", pkg, "\n"} {
		if _, err := io.WriteString(h, part); err != nil {
			t.Fatalf("write skill fingerprint: %v", err)
		}
	}

	root := repoRoot(t)
	for _, name := range []string{"go.mod", "go.sum", "go.work", "go.work.sum"} {
		addFileToHash(h, filepath.Join(root, name))
	}

	pkgs := goListPackagesForBuild(t, pkg)
	files := collectLocalPackageFiles(root, pkgs)
	sort.Strings(files)
	for _, file := range files {
		addFileToHash(h, file)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func goListPackagesForBuild(t *testing.T, pkg string) []goListPackage {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", "-json", pkg)
	cmd.Dir = repoRoot(t)
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
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list %s: %v", pkg, err)
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	var pkgs []goListPackage
	for {
		var p goListPackage
		if err := dec.Decode(&p); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode go list output for %s: %v", pkg, err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

func collectLocalPackageFiles(root string, pkgs []goListPackage) []string {
	seen := make(map[string]struct{})
	var files []string
	add := func(path string) {
		if path == "" {
			return
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		path = filepath.Clean(path)
		if !strings.HasPrefix(path, root+string(filepath.Separator)) && path != root {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		files = append(files, path)
	}
	for _, pkg := range pkgs {
		if pkg.Dir == "" {
			continue
		}
		dir := filepath.Clean(pkg.Dir)
		if !strings.HasPrefix(dir, root+string(filepath.Separator)) && dir != root {
			continue
		}
		fileGroups := [][]string{
			pkg.GoFiles,
			pkg.CgoFiles,
			pkg.CFiles,
			pkg.CXXFiles,
			pkg.MFiles,
			pkg.HFiles,
			pkg.FFiles,
			pkg.SFiles,
			pkg.SwigFiles,
			pkg.SwigCXXFiles,
			pkg.SysoFiles,
			pkg.EmbedFiles,
		}
		for _, group := range fileGroups {
			for _, name := range group {
				add(filepath.Join(dir, name))
			}
		}
	}
	return files
}

func addFileToHash(h io.Writer, path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	_, _ = io.WriteString(h, path)
	_, _ = io.WriteString(h, "\n")
	_, _ = io.WriteString(h, fmt.Sprintf("%d\n", info.Size()))
	_, _ = io.WriteString(h, fmt.Sprintf("%d\n", info.ModTime().UnixNano()))
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("determine repo root: no caller info")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}
