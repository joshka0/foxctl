package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestIndexRepoBuildEmitsHumanProgressOnStderrByDefault(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "repo")
	writeIndexRepoProgressFile(t, workspace, "main.tf", `resource "local_file" "demo" {
  filename = "demo.txt"
  content  = "demo"
}
`)
	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	cmd := newIndexRepoBuildCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetContext(config.WithContext(ctx, cfg))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"--workspace", workspace,
		"--go=false",
		"--typescript=false",
		"--terraform",
		"--dry-run",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("index repo build: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("stdout is not a JSON envelope: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if env.Status != envelope.StatusOK || env.Command != "index.repo.build" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
	progress := stderr.String()
	for _, want := range []string{
		"repoindex build:",
		"phase=init",
		"families=terraform",
		"include_tests=false",
		"semantic_anchors=false",
		"phase=config",
		"phase=storage",
		"phase=build",
		"phase=start",
		"phase=terraform",
		"phase=done",
		"packages=",
		"files=",
		"nodes=",
		"edges=",
	} {
		if !strings.Contains(progress, want) {
			t.Fatalf("stderr progress missing %q:\n%s", want, progress)
		}
	}
}

func TestIndexRepoBuildProgressCanBeDisabled(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "repo")
	writeIndexRepoProgressFile(t, workspace, "main.tf", `resource "local_file" "demo" {
  filename = "demo.txt"
  content  = "demo"
}
`)
	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	cmd := newIndexRepoBuildCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetContext(config.WithContext(ctx, cfg))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"--workspace", workspace,
		"--go=false",
		"--typescript=false",
		"--terraform",
		"--dry-run",
		"--progress=false",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("index repo build: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), "repoindex build:") {
		t.Fatalf("progress should be disabled, got stderr:\n%s", stderr.String())
	}
}

func writeIndexRepoProgressFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
