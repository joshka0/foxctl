package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/cas"
)

func TestCASCommandsHandleLargeArtifacts(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Config{
		Home: tmp,
		Paths: config.Paths{
			CAS:   filepath.Join(tmp, "cas"),
			Jobs:  filepath.Join(tmp, "jobs"),
			Cache: filepath.Join(tmp, "cache"),
		},
		InlineOutputKB: 256,
		MaxCaptureKB:   10240,
	}

	inputPath := filepath.Join(tmp, "large.bin")
	data := bytes.Repeat([]byte("abcdefgh"), 512*1024) // ~4 MiB
	if err := os.WriteFile(inputPath, data, 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	ctx := config.WithContext(context.Background(), cfg)
	putCmd := newCASPutCommand()
	putCmd.SetContext(ctx)
	putOut := &bytes.Buffer{}
	putCmd.SetOut(putOut)
	putCmd.SetErr(&bytes.Buffer{})
	putCmd.SetArgs([]string{"--pin", inputPath})
	if err := putCmd.Execute(); err != nil {
		t.Fatalf("cas put: %v", err)
	}

	var putEnv struct {
		Data struct {
			Digest string `json:"digest"`
			Size   int64  `json:"size_bytes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(putOut.Bytes(), &putEnv); err != nil {
		t.Fatalf("decode put envelope: %v", err)
	}
	digest := putEnv.Data.Digest
	if digest == "" {
		t.Fatalf("digest missing; envelope=%s", putOut.Bytes())
	}

	outputPath := filepath.Join(tmp, "roundtrip.bin")
	getCmd := newCASGetCommand()
	getCmd.SetContext(ctx)
	getOut := &bytes.Buffer{}
	getCmd.SetOut(getOut)
	getCmd.SetErr(&bytes.Buffer{})
	getCmd.SetArgs([]string{"--output", outputPath, digest})
	if err := getCmd.Execute(); err != nil {
		t.Fatalf("cas get: %v", err)
	}

	result, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read roundtrip: %v", err)
	}
	if !bytes.Equal(result, data) {
		t.Fatalf("roundtrip data mismatch")
	}

	rmCmd := newCASRemoveCommand()
	rmCmd.SetContext(ctx)
	rmCmd.SetOut(&bytes.Buffer{})
	rmCmd.SetErr(&bytes.Buffer{})
	rmCmd.SetArgs([]string{digest})
	if err := rmCmd.Execute(); !errors.Is(err, cas.ErrPinned) {
		if err == nil {
			t.Fatalf("expected removal to fail for pinned digest")
		}
		t.Fatalf("expected ErrPinned, got %v", err)
	}

	rmForce := newCASRemoveCommand()
	rmForce.SetContext(ctx)
	rmForce.SetOut(&bytes.Buffer{})
	rmForce.SetErr(&bytes.Buffer{})
	rmForce.SetArgs([]string{"--force", digest})
	if err := rmForce.Execute(); err != nil {
		t.Fatalf("cas rm --force: %v", err)
	}
}

func TestCASGCCommand(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Config{
		Home: tmp,
		Paths: config.Paths{
			CAS:   filepath.Join(tmp, "cas"),
			Jobs:  filepath.Join(tmp, "jobs"),
			Cache: filepath.Join(tmp, "cache"),
		},
		InlineOutputKB: 32,
		MaxCaptureKB:   10240,
	}
	store, err := cas.NewStore(cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	ctx := config.WithContext(context.Background(), cfg)

	data := bytes.Repeat([]byte("x"), 1024)
	if _, err := store.Put(ctx, bytes.NewReader(data), "application/octet-stream", nil); err != nil {
		t.Fatalf("put: %v", err)
	}

	cmd := newCASGCCommand()
	cmd.SetContext(ctx)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--older-than", "0s", "--keep-pinned=false"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cas gc: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env["status"] != "ok" {
		t.Fatalf("unexpected status: %v", env["status"])
	}
	dataMap := env["data"].(map[string]any)
	if deleted := dataMap["objects_deleted"].(float64); deleted != 1 {
		t.Fatalf("expected 1 deletion, got %v", deleted)
	}

	objects, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(objects) != 0 {
		t.Fatalf("expected store to be empty after GC")
	}
}
