package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
)

const skillName = "heartwood/action"

type Input struct {
	HeartwoodRoot string         `json:"heartwood_root,omitempty"`
	Host          string         `json:"host"`
	DBName        string         `json:"db_name"`
	Token         string         `json:"token,omitempty"`
	TokenPath     string         `json:"token_path,omitempty"`
	WaitTimeoutMS int            `json:"wait_timeout_ms,omitempty"`
	Operation     string         `json:"operation"`
	Args          map[string]any `json:"args,omitempty"`
}

func main() {
	skillmain.Main(skillName, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	if in.Host == "" {
		return skillerr.Arg("host is required")
	}
	if in.DBName == "" {
		return skillerr.Arg("db_name is required")
	}
	if in.Operation == "" {
		return skillerr.Arg("operation is required")
	}

	root := in.HeartwoodRoot
	if root == "" {
		root = rc.Workspace
	}
	root = filepath.Clean(root)

	payload := map[string]any{
		"host":   in.Host,
		"dbName": in.DBName,
		"action": map[string]any{
			"operation": in.Operation,
			"args":      in.Args,
		},
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

	body, err := json.Marshal(payload)
	if err != nil {
		return skillerr.Runtime("marshal input", skillerr.WithCause(err))
	}

	cmd := exec.CommandContext(ctx, "pnpm", "--dir", root, "exec", "tsx", "scripts/heartwood-agentctl.ts", "action")
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
		return skillerr.Runtime(fmt.Sprintf("heartwood action failed: %s", msg), skillerr.WithCause(err))
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return skillerr.Runtime("parse heartwood action output", skillerr.WithCause(err))
	}
	return skillout.Emit(rc, skillName, out)
}
