package wasirunner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/skill"
)

func TestRunnerRunEcho(t *testing.T) {
	t.Parallel()

	manifest := loadManifest(t)
	modulePath := repoPath(t, "skills", "wasi_echo", "module.wasm")

	r := Runner{
		Manifest:   manifest,
		ModulePath: modulePath,
	}
	stdout, stderr, err := r.Run(context.Background(), []byte("{}"))
	if err != nil {
		t.Fatalf("run: %v (stderr=%s)", err, string(stderr))
	}
	if len(stderr) != 0 {
		t.Fatalf("unexpected stderr: %s", string(stderr))
	}

	var env struct {
		Version int               `json:"version"`
		Status  string            `json:"status"`
		Command string            `json:"command"`
		Data    map[string]string `json:"data"`
	}
	if err := json.Unmarshal(stdout, &env); err != nil {
		t.Fatalf("unmarshal stdout: %v\nstdout=%s", err, string(stdout))
	}
	if env.Command != "wasi.echo" {
		t.Fatalf("unexpected command %q", env.Command)
	}
	if env.Status != "ok" {
		t.Fatalf("unexpected status %q", env.Status)
	}
	if got := env.Data["message"]; got != "hello from wasi" {
		t.Fatalf("unexpected data.message %q", got)
	}
}

func TestRunnerRejectsNetwork(t *testing.T) {
	t.Parallel()

	manifest := loadManifest(t)
	manifest.Capabilities.Network = "egress"
	modulePath := repoPath(t, "skills", "wasi_echo", "module.wasm")

	r := Runner{
		Manifest:   manifest,
		ModulePath: modulePath,
	}
	if _, _, err := r.Run(context.Background(), []byte("{}")); err == nil || !strings.Contains(err.Error(), "network") {
		t.Fatalf("expected network error, got %v", err)
	}
}

func loadManifest(t *testing.T) skill.Manifest {
	t.Helper()
	path := repoPath(t, "skills", "wasi_echo", "skill.yaml")
	manifest, err := skill.LoadManifest(path)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	return manifest
}

func repoPath(t *testing.T, elems ...string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	path := filepath.Join(append([]string{root}, elems...)...)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return path
}
