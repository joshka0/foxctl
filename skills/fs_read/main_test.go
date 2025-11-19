package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

func TestFsReadReturnsPreviewAndCasArtifact(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	file := filepath.Join(tmp, "notes.txt")
	content := "hello world\nsecond line\n"
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, tmp)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	if err := run(ctx, rc, input{Path: file}); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Command != "fs/read" {
		t.Fatalf("expected fs/read command, got %s", env.Command)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", env.Data)
	}
	preview, ok := data["preview"].(string)
	if !ok {
		t.Fatalf("preview is not a string: %T", data["preview"])
	}
	if preview == "" || preview != content {
		t.Fatalf("unexpected preview: %q", preview)
	}
	artifact, ok := data["artifact"].(string)
	if !ok {
		t.Fatalf("artifact is not a string: %T", data["artifact"])
	}
	if artifact == "" || env.Meta.CASDigest != artifact {
		t.Fatalf("cas digest mismatch: meta=%s artifact=%s", env.Meta.CASDigest, artifact)
	}
	if binary, ok := data["binary"].(bool); !ok {
		t.Fatalf("binary is not a bool: %T", data["binary"])
	} else if binary {
		t.Fatalf("expected text file to be marked non-binary")
	}
}

func TestFsReadHonorsMaxBytesAndTruncates(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	file := filepath.Join(tmp, "long.txt")
	if err := os.WriteFile(file, []byte("abcdefghijklmnopqrstuvwxyz"), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, tmp)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	if err := run(ctx, rc, input{Path: file, MaxBytes: 8}); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	data := env["data"].(map[string]any)
	if !data["truncated"].(bool) {
		t.Fatalf("expected truncated flag")
	}
	if got := data["preview"].(string); got != "abcdefgh" {
		t.Fatalf("unexpected preview %q", got)
	}
	if previewBytes := int(data["preview_bytes"].(float64)); previewBytes != 8 {
		t.Fatalf("expected 8 preview bytes, got %d", previewBytes)
	}
}

func TestFsReadMarksBinaryContent(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	file := filepath.Join(tmp, "bin.dat")
	data := []byte{0x00, 0x01, 0x02, 0xff}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, tmp)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	if err := run(ctx, rc, input{Path: file}); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	dataMap := env["data"].(map[string]any)
	if binary, ok := dataMap["binary"].(bool); !ok {
		t.Fatalf("binary is not a bool: %T", dataMap["binary"])
	} else if !binary {
		t.Fatalf("expected binary flag")
	}
	if _, ok := dataMap["preview"]; ok {
		t.Fatalf("expected no preview for binary content")
	}
	if hint, ok := dataMap["hint"].(string); !ok {
		t.Fatalf("hint is not a string: %T", dataMap["hint"])
	} else if hint == "" {
		t.Fatalf("expected hint for binary files")
	}
}

func newTestRunnerContext(t *testing.T, stdout *bytes.Buffer, workspace string) *runner.RunnerContext {
	t.Helper()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldwd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	state := t.TempDir()
	cfg := config.Config{
		Home:           state,
		InlineOutputKB: 4,
		MaxCaptureKB:   config.DefaultMaxCaptureKB,
		Paths: config.Paths{
			CAS:   filepath.Join(state, "cas"),
			Jobs:  filepath.Join(state, "jobs"),
			Cache: filepath.Join(state, "cache"),
		},
	}
	rc, err := runner.NewRunnerContext(cfg, stdout)
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	return rc
}
