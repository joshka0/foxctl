package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain/lite"
)

const skillName = "heartwood/state"

type Input struct {
	HeartwoodRoot string `json:"heartwood_root,omitempty"`
	Host          string `json:"host"`
	DBName        string `json:"db_name"`
	Token         string `json:"token,omitempty"`
	TokenPath     string `json:"token_path,omitempty"`
	WaitTimeoutMS int    `json:"wait_timeout_ms,omitempty"`
	MessageLimit  int    `json:"message_limit,omitempty"`
}

func main() {
	lite.Main(skillName, run)
}

func run(ctx context.Context, rc *lite.RunContext, in Input) error {
	if in.Host == "" {
		return skillerr.Arg("host is required")
	}
	if in.DBName == "" {
		return skillerr.Arg("db_name is required")
	}

	root := in.HeartwoodRoot
	if root == "" {
		root = rc.Workspace
	}
	root = filepath.Clean(root)

	payload := map[string]any{
		"host":   in.Host,
		"dbName": in.DBName,
	}
	if in.Token != "" {
		payload["token"] = in.Token
	}
	if in.TokenPath != "" {
		payload["tokenPath"] = in.TokenPath
	}
	if in.WaitTimeoutMS > 0 {
		payload["waitTimeoutMs"] = in.WaitTimeoutMS
	}
	if in.MessageLimit > 0 {
		payload["messageLimit"] = in.MessageLimit
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return skillerr.Runtime("marshal input", skillerr.WithCause(err))
	}

	cmd := exec.CommandContext(ctx, "pnpm", "--dir", root, "exec", "tsx", "scripts/heartwood-foxctl.ts", "state")
	cmd.Stdin = bytes.NewReader(body)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if msg == "" {
			msg = err.Error()
		}
		return skillerr.Runtime(fmt.Sprintf("heartwood state failed: %s", msg), skillerr.WithCause(err))
	}

	rawJSON, err := extractJSONPayload(stdout.Bytes())
	if err != nil {
		return skillerr.Runtime("parse heartwood state output", skillerr.WithCause(err))
	}
	var out map[string]any
	if err := json.Unmarshal(rawJSON, &out); err != nil {
		return skillerr.Runtime("parse heartwood state output", skillerr.WithCause(err))
	}
	return lite.Emit(rc, skillName, out)
}

func extractJSONPayload(raw []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty output")
	}
	var parsed map[string]any
	if err := json.Unmarshal(trimmed, &parsed); err == nil {
		return trimmed, nil
	}

	lines := strings.Split(string(trimmed), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &parsed); err == nil {
			return []byte(line), nil
		}
	}

	return nil, fmt.Errorf("no JSON object found in output")
}
