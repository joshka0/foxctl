package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestRunRememberThenMemoryRelevantWorkflow(t *testing.T) {
	cfg := installTextGrepSkill(t)
	workspace := t.TempDir()
	sample := filepath.Join(workspace, "sample.txt")
	if err := os.WriteFile(sample, []byte("needle one\nneedle two\n"), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	runCmd := newRunCommand()
	runCmd.SetContext(config.WithContext(context.Background(), cfg))
	runStdout := &bytes.Buffer{}
	runStderr := &bytes.Buffer{}
	runCmd.SetOut(runStdout)
	runCmd.SetErr(runStderr)
	runCmd.SetArgs([]string{
		"--input", fmt.Sprintf(`{"path":%q,"pattern":"needle"}`, workspace),
		"--remember", "grep-first",
		"--workspace", workspace,
		"text/grep",
	})
	if err := runCmd.Execute(); err != nil {
		t.Fatalf("run command failed: %v (stderr=%s)", err, runStderr.String())
	}

	var runEnv envelope.Envelope
	if err := json.Unmarshal(runStdout.Bytes(), &runEnv); err != nil {
		t.Fatalf("decode run envelope: %v", err)
	}
	if runEnv.Status != "ok" {
		t.Fatalf("run status=%q want ok", runEnv.Status)
	}

	relCmd := newMemoryRelevantCommand()
	relCmd.SetContext(config.WithContext(context.Background(), cfg))
	relStdout := &bytes.Buffer{}
	relCmd.SetOut(relStdout)
	relCmd.SetErr(&bytes.Buffer{})
	relCmd.SetArgs([]string{"--workspace", workspace, "--limit", "10"})
	if err := relCmd.Execute(); err != nil {
		t.Fatalf("memory relevant failed: %v", err)
	}

	var relEnv envelope.Envelope
	if err := json.Unmarshal(relStdout.Bytes(), &relEnv); err != nil {
		t.Fatalf("decode memory relevant envelope: %v", err)
	}
	data, ok := relEnv.Data.(map[string]any)
	if !ok {
		t.Fatalf("memory relevant data=%T want map", relEnv.Data)
	}
	entries, ok := data["entries"].([]any)
	if !ok {
		t.Fatalf("memory relevant entries=%T want slice", data["entries"])
	}
	for _, entry := range entries {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if name, _ := m["name"].(string); name == "grep-first" {
			return
		}
	}
	t.Fatalf("expected remembered entry %q in relevant results, got %#v", "grep-first", entries)
}
