package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/domain/policy"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/cas"
	"github.com/rs/zerolog"
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
	rc := newTestRunContext(t, buf, tmp)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close run context: %v", err)
		}
	})

	if err := run(ctx, rc, Input{Path: file}); err != nil {
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
	previewNumbered, ok := data["preview_numbered"].(string)
	if !ok {
		t.Fatalf("preview_numbered is not a string: %T", data["preview_numbered"])
	}
	expectedNumbered := "1 | hello world\n2 | second line\n"
	if previewNumbered != expectedNumbered {
		t.Fatalf("unexpected preview_numbered: %q", previewNumbered)
	}
	artifact, ok := data["artifact"].(string)
	if !ok {
		t.Fatalf("artifact is not a string: %T", data["artifact"])
	}
	if artifact == "" {
		t.Fatalf("expected artifact in data")
	}
	if env.Meta.CASDigest != "" && env.Meta.CASDigest != artifact {
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
	rc := newTestRunContext(t, buf, tmp)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close run context: %v", err)
		}
	})

	if err := run(ctx, rc, Input{Path: file, MaxBytes: 8}); err != nil {
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
	rc := newTestRunContext(t, buf, tmp)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close run context: %v", err)
		}
	})

	if err := run(ctx, rc, Input{Path: file}); err != nil {
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

func TestFsReadRejectsSymlinkEscape(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	link := filepath.Join(workspace, "secret-link.txt")
	if err := os.Symlink(outsideFile, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunContext(t, buf, workspace)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close run context: %v", err)
		}
	})

	if err := run(ctx, rc, Input{Path: link}); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no success envelope, got %s", buf.String())
	}
}

func newTestRunContext(t *testing.T, stdout *bytes.Buffer, workspace string) *skillmain.RunContext {
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
	casPath := filepath.Join(state, "cas")
	casStore, err := cas.NewStore(casPath)
	if err != nil {
		t.Fatalf("open cas: %v", err)
	}

	pv, err := policy.NewPathValidator(workspace, nil)
	if err != nil {
		t.Fatalf("path validator: %v", err)
	}

	cfg := config.Config{
		Home:           state,
		InlineOutputKB: 4,
		MaxCaptureKB:   config.DefaultMaxCaptureKB,
		Paths: config.Paths{
			CAS:   casPath,
			Jobs:  filepath.Join(state, "jobs"),
			Cache: filepath.Join(state, "cache"),
		},
	}

	return &skillmain.RunContext{
		Config:        cfg,
		CASStore:      casStore,
		Workspace:     workspace,
		Logger:        zerolog.Nop(),
		PathValidator: pv,
		Validator:     validator.New(),
		Stdout:        stdout,
		Now:           time.Now,
		InlineKB:      cfg.InlineOutputKB,
		MaxPreview:    100,
	}
}
