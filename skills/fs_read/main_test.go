package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
	"github.com/jkatigb/agentctl/internal/skillslib"
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
	rc := newFsReadRunner(t, buf, tmp)
	defer func() { _ = rc.Close() }()

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
	preview, _ := data["preview"].(string)
	if preview == "" || preview != content {
		t.Fatalf("unexpected preview: %q", preview)
	}
	artifact, _ := data["artifact"].(string)
	if artifact == "" || env.Meta.CASDigest != artifact {
		t.Fatalf("cas digest mismatch: meta=%s artifact=%s", env.Meta.CASDigest, artifact)
	}
	if binary, _ := data["binary"].(bool); binary {
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
	rc := newFsReadRunner(t, buf, tmp)
	defer func() { _ = rc.Close() }()

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
	rc := newFsReadRunner(t, buf, tmp)
	defer func() { _ = rc.Close() }()

	if err := run(ctx, rc, input{Path: file}); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	dataMap := env["data"].(map[string]any)
	if binary, _ := dataMap["binary"].(bool); !binary {
		t.Fatalf("expected binary flag")
	}
	if _, ok := dataMap["preview"]; ok {
		t.Fatalf("expected no preview for binary content")
	}
	if hint, _ := dataMap["hint"].(string); hint == "" {
		t.Fatalf("expected hint for binary files")
	}
}

func newFsReadRunner(t *testing.T, stdout *bytes.Buffer, tmp string) *skillslib.RunnerContext {
	t.Helper()
	cfg := config.Config{
		Home:           tmp,
		InlineOutputKB: 4,
		MaxCaptureKB:   config.DefaultMaxCaptureKB,
		Paths: config.Paths{
			CAS:   filepath.Join(tmp, "cas"),
			Jobs:  filepath.Join(tmp, "jobs"),
			Cache: filepath.Join(tmp, "cache"),
		},
	}
	rc, err := skillslib.NewRunnerContext(cfg, stdout)
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	return rc
}
