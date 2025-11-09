package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/cas"
	"github.com/jkatigb/agentctl/internal/config"
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
