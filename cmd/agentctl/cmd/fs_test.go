package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	memstore "github.com/jkatigb/agentctl/internal/storage/memory"
)

func TestFSReadCommandOutputsPreview(t *testing.T) {
	if testing.Short() {
		t.Skip("slow end-to-end fs/read preview test")
	}
	cfg := installTextGrepSkill(t)
	installFSReadSkill(t, cfg)

	tmp := t.TempDir()
	file := filepath.Join(tmp, "notes.txt")
	if err := os.WriteFile(file, []byte("fs read preview\n"), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	cmd := newFSReadCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"--workspace", tmp, file})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("fs read command: %v\nstderr: %s", err, stderr.String())
	}
	if stderr.Len() > 0 {
		t.Logf("fs read stderr: %s", stderr.String())
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Command != "fs/read" {
		t.Fatalf("expected fs/read command, got %s", env.Command)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", env.Data)
	}
	if preview, _ := data["preview"].(string); preview == "" {
		t.Fatalf("expected preview text: %#v", data)
	}
}

func TestFSReadCommandRemember(t *testing.T) {
	if testing.Short() {
		t.Skip("slow end-to-end fs/read remember test")
	}
	cfg := installTextGrepSkill(t)
	installFSReadSkill(t, cfg)

	workdir := t.TempDir()
	file := filepath.Join(workdir, "remember.txt")
	if err := os.WriteFile(file, []byte("remember me"), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	cmd := newFSReadCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{
		"--workspace", workdir,
		"--remember", "snapshot",
		file,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("fs read remember: %v\nstderr: %s", err, stderr.String())
	}
	if stderr.Len() > 0 {
		t.Logf("fs read remember stderr: %s", stderr.String())
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode fs/read envelope: %v", err)
	}
	workspacePath := env.Meta.Workspace
	if workspacePath == "" {
		workspacePath = workdir
	}

	store, err := memstore.Open(context.Background(), cfg.Paths.Cache, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close memory store: %v", err)
		}
	})

	entry, err := store.Get(context.Background(), "snapshot", workspacePath)
	if err != nil {
		t.Fatalf("memory entry missing for workspace %s: %v", workspacePath, err)
	}
	if entry.Name != "snapshot" {
		t.Fatalf("unexpected memory name %s", entry.Name)
	}
	if entry.Summary == "" {
		t.Fatalf("expected summary for remembered entry")
	}
}
