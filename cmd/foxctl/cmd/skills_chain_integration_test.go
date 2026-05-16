//go:build integration

package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
)

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

func buildFoxctlBinary(t *testing.T) string {
	t.Helper()
	destDir := t.TempDir()
	bin := filepath.Join(destDir, "foxctl")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/foxctl")
	cmd.Dir = repoRoot(t)
	// Build CLI without CGO to use pure-Go SQLite (modernc.org/sqlite) and avoid
	// linker conflicts in sqlite-specific CGO paths.
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
		t.Fatalf("go build foxctl: %v\n%s", err, string(out))
	}
	return bin
}

func TestFsReadSkillChainsThroughBash(t *testing.T) {
	cfg := installTextGrepSkill(t)
	installFSLsSkill(t, cfg)
	installFSReadSkill(t, cfg)

	foxctlBin := buildFoxctlBinary(t)

	workdir := t.TempDir()
	sample := filepath.Join(workdir, "chain.txt")
	content := "pipeline ready\n"
	if err := os.WriteFile(sample, []byte(content), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	script := `
set -euo pipefail
python3 -c 'import json,os; print(json.dumps({"path": os.environ["WORKDIR"]}))' \
  | "$FOXCTL_BIN" skills run fs/ls --workspace "$WORKDIR" --input-file - \
  | python3 -c 'import json,sys; data=json.load(sys.stdin); path=data["data"]["preview"][0]["path"]; print(json.dumps({"path": path, "max_bytes": 128}))' \
  | "$FOXCTL_BIN" skills run fs/read --workspace "$WORKDIR" --input-file -
`
	cmd := exec.Command("bash", "-lc", script)
	cmd.Dir = repoRoot(t)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	env := append(
		os.Environ(),
		fmt.Sprintf("FOXCTL_BIN=%s", foxctlBin),
		fmt.Sprintf("WORKDIR=%s", workdir),
	)
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		t.Fatalf("bash pipeline failed: %v\nstderr: %s", err, stderr.String())
	}

	var envOut envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envOut); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envOut.Command != "fs/read" {
		t.Fatalf("expected fs/read envelope, got %s", envOut.Command)
	}
	data, ok := envOut.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", envOut.Data)
	}
	preview, ok := data["preview"].(string)
	if !ok {
		t.Fatalf("preview is not a string: %T", data["preview"])
	}
	if !strings.Contains(preview, "pipeline ready") {
		t.Fatalf("preview missing content: %q", preview)
	}
	artifact, ok := data["artifact"].(string)
	if !ok {
		t.Fatalf("artifact is not a string: %T", data["artifact"])
	}
	if artifact == "" {
		t.Fatalf("expected artifact in data")
	}
	if envOut.Meta.CASDigest != "" && envOut.Meta.CASDigest != artifact {
		t.Fatalf("cas digest mismatch: meta=%s artifact=%s", envOut.Meta.CASDigest, artifact)
	}
}
