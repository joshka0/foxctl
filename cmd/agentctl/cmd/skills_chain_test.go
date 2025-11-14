package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/envelope"
)

func TestFsReadSkillChainsThroughBash(t *testing.T) {
	cfg := installTextGrepSkill(t)
	installFSLsSkill(t, cfg)
	installFSReadSkill(t, cfg)

	agentctlBin := buildAgentctlBinary(t)

	workdir := t.TempDir()
	sample := filepath.Join(workdir, "chain.txt")
	content := "pipeline ready\n"
	if err := os.WriteFile(sample, []byte(content), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	script := `
set -euo pipefail
python3 -c 'import json,os; print(json.dumps({"path": os.environ["WORKDIR"]}))' \
  | "$AGENTCTL_BIN" skills run fs/ls --workspace "$WORKDIR" --input-file - \
  | python3 -c 'import json,sys; data=json.load(sys.stdin); path=data["data"]["preview"][0]["path"]; print(json.dumps({"path": path, "max_bytes": 128}))' \
  | "$AGENTCTL_BIN" skills run fs/read --workspace "$WORKDIR" --input-file -
`
	cmd := exec.Command("bash", "-lc", script)
	cmd.Dir = repoRoot(t)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	env := append(os.Environ(),
		fmt.Sprintf("AGENTCTL_BIN=%s", agentctlBin),
		fmt.Sprintf("WORKDIR=%s", workdir),
	)
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		t.Fatalf("bash pipeline failed: %v\nstderr: %s", err, stderr.String())
	}

	var envOut envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &envOut); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envOut.Command != "fs/read" {
		t.Fatalf("expected fs/read envelope, got %s", envOut.Command)
	}
	data, ok := envOut.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", envOut.Data)
	}
	preview, _ := data["preview"].(string)
	if !strings.Contains(preview, "pipeline ready") {
		t.Fatalf("preview missing content: %q", preview)
	}
	artifact, _ := data["artifact"].(string)
	if artifact == "" || envOut.Meta.CASDigest != artifact {
		t.Fatalf("cas digest mismatch: meta=%s artifact=%s", envOut.Meta.CASDigest, artifact)
	}
}
