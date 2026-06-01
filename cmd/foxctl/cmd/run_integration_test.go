//go:build integration

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestRunCommandEmitsCompleteMeta(t *testing.T) {
	cfg := installTextGrepSkill(t)
	inputDir := t.TempDir()
	sample := filepath.Join(inputDir, "sample.txt")
	var buf bytes.Buffer
	for i := 0; i < 10; i++ {
		if _, err := fmt.Fprintf(&buf, "needle line %d\n", i); err != nil {
			t.Fatalf("build sample: %v", err)
		}
	}
	if err := os.WriteFile(sample, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	cmd := newRunCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{
		"--input", fmt.Sprintf(`{"path":%q,"pattern":"needle"}`, inputDir),
		"--workspace", inputDir,
		"text/grep",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("run command: %v\nstderr: %s", err, stderr.String())
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if _, err := time.Parse(time.RFC3339, env.Meta.TS); err != nil {
		t.Fatalf("meta.ts not RFC3339: %v", err)
	}
	if env.Meta.Source != "run" {
		t.Fatalf("expected meta.source=run got %q", env.Meta.Source)
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be a map got %T", env.Data)
	}
	artifact, ok := data["artifact"].(string)
	if !ok {
		t.Fatalf("artifact is not a string: %T\nstdout: %s\nstderr: %s", data["artifact"], stdout.String(), stderr.String())
	}
	if artifact == "" {
		t.Fatalf("expected artifact in data")
	}
	if env.Meta.CASDigest != "" && env.Meta.CASDigest != artifact {
		t.Fatalf("meta.cas_digest %q does not match artifact %q", env.Meta.CASDigest, artifact)
	}
}

func TestSkillsRunProducesInlineEnvelope(t *testing.T) {
	cfg := installTextGrepSkill(t)
	file := filepath.Join(t.TempDir(), "small.txt")
	if err := os.WriteFile(file, []byte("only once\nsecond line\n"), 0o644); err != nil {
		t.Fatalf("write small file: %v", err)
	}

	cmd := newSkillsRunCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{
		"--input", fmt.Sprintf(`{"path":%q,"pattern":"only"}`, file),
		"--workspace", filepath.Dir(file),
		"text/grep",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skills run: %v\nstderr: %s", err, stderr.String())
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Meta.CASDigest != "" {
		t.Fatalf("expected no cas digest for inline results, got %q", env.Meta.CASDigest)
	}
}

func TestRunCommandRememberSavesMemory(t *testing.T) {
	cfg := installTextGrepSkill(t)
	workdir := t.TempDir()
	sample := filepath.Join(workdir, "file.txt")
	if err := os.WriteFile(sample, []byte("needle here"), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}
	cmd := newRunCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	cmd.SetArgs([]string{
		"--input", fmt.Sprintf(`{"path":%q,"pattern":"needle"}`, workdir),
		"--remember", "remembered",
		"--workspace", workdir,
		"text/grep",
	})
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("run command: %v (stderr=%s)", err, stderr.String())
	}
	if stderr.Len() > 0 {
		t.Logf("stderr: %s", stderr.String())
	}
	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode run envelope: %v", err)
	}
	if env.Meta.Workspace != workdir {
		t.Fatalf("expected meta.workspace=%s got %s", workdir, env.Meta.Workspace)
	}
}
