// Package main implements the mcp/install skill - installs MCP server configurations.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"gopkg.in/yaml.v3"
)

type input struct {
	ServerCmd string `json:"server_cmd"`

	ServerArgs []string `json:"server_args"`

	ServerEnv map[string]string `json:"server_env"`

	ServerURL string `json:"server_url"`

	ServerHeaders map[string]string `json:"server_headers"`

	OutputDir string `json:"output_dir"`

	BridgePath string `json:"bridge_path"` // Path to mcp_bridge binary. Defaults to "mcp_bridge" (in path)

}

func main() {

	ctx := context.Background()

	cfg, err := config.Load(ctx)

	if err != nil {

		fail("mcp/install", "ECONFIG", err)

	}

	rc, err := runner.NewRunnerContext(cfg, os.Stdout)

	if err != nil {

		fail("mcp/install", "ERUNTIME", err)

	}

	defer func() {

		errs.Ignore(rc.Close(), "runner context close")

	}()

	in, err := parseInput(os.Stdin)

	if err != nil {

		fail("mcp/install", "EARG", err)

	}

	if err := run(ctx, rc, in); err != nil {

		fail("mcp/install", "ERUNTIME", err)

	}

}

func run(ctx context.Context, rc *runner.RunnerContext, in input) error {

	var mcpClient *client.Client

	var err error

	if in.ServerURL != "" {

		// SSE/HTTP Transport

		mcpClient, err = client.NewStreamableHttpClient(in.ServerURL, transport.WithHTTPHeaders(in.ServerHeaders))

		if err != nil {

			return fmt.Errorf("failed to create HTTP client: %w", err)

		}

		if err := mcpClient.Start(ctx); err != nil {

			return fmt.Errorf("failed to start transport: %w", err)

		}

	} else if in.ServerCmd != "" {

		// Stdio Transport

		env := os.Environ()

		for k, v := range in.ServerEnv {

			env = append(env, fmt.Sprintf("%s=%s", k, v))

		}

		mcpClient, err = client.NewStdioMCPClient(in.ServerCmd, env, in.ServerArgs...)

		if err != nil {

			return fmt.Errorf("failed to create stdio client: %w", err)

		}

	} else {

		return fmt.Errorf("either server_cmd or server_url is required")

	}

	defer func() {
		if err := mcpClient.Close(); err != nil {
			// Log close error but don't fail the operation
			fmt.Fprintf(os.Stderr, "warning: failed to close MCP client: %v\n", err)
		}
	}()

	// Initialize

	initReq := mcp.InitializeRequest{

		Params: mcp.InitializeParams{

			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,

			ClientInfo: mcp.Implementation{

				Name: "agentctl-mcp-install",

				Version: "1.0.0",
			},

			Capabilities: mcp.ClientCapabilities{},
		},
	}

	_, err = mcpClient.Initialize(ctx, initReq)

	if err != nil {

		return fmt.Errorf("mcp initialization failed: %w", err)

	}

	// List Tools

	toolsResult, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})

	if err != nil {

		return fmt.Errorf("failed to list tools: %w", err)

	}

	installed := []string{}

	// Ensure output directory exists

	if in.OutputDir == "" {

		in.OutputDir = "."

	}

	// Validate output directory using PathValidator if possible,

	// but for "installing skills" we often write to a skills directory which might be outside workspace.

	// However, agentctl policy enforces strict workspace.

	// So we assume in.OutputDir is within the workspace.

	validDir, err := rc.PathValidator.ValidatePath(in.OutputDir)

	if err != nil {

		return fmt.Errorf("output directory validation failed: %w", err)

	}

	for _, tool := range toolsResult.Tools {

		if err := generateSkill(validDir, tool, in); err != nil {

			// Warn but continue?

			fmt.Fprintf(os.Stderr, "failed to generate skill for %s: %v\n", tool.Name, err)

			continue

		}

		installed = append(installed, tool.Name)

	}

	// Emit success

	return rc.Emit("mcp/install", map[string]any{

		"installed": installed,

		"count": len(installed),

		"path": validDir,
	}, "application/json", envelope.Meta{

		Source: "run",

		Runner: "exec",
	})

}

func generateSkill(baseDir string, tool mcp.Tool, in input) error {

	// Create directory for the skill

	skillDir := filepath.Join(baseDir, tool.Name)

	if err := os.MkdirAll(skillDir, 0755); err != nil {

		return err

	}

	// Construct Command parts

	cmdParts := []string{in.BridgePath}

	if in.ServerURL != "" {

		// SSE Mode

		cmdParts = append(cmdParts, "-server-url", in.ServerURL)

		for k, v := range in.ServerHeaders {

			cmdParts = append(cmdParts, "-header", fmt.Sprintf("%s=%s", k, v))

		}

	} else {

		// Stdio Mode

		cmdParts = append(cmdParts, "-server-cmd", in.ServerCmd)

		for _, arg := range in.ServerArgs {

			cmdParts = append(cmdParts, "-server-arg", arg)

		}

		for k, v := range in.ServerEnv {

			cmdParts = append(cmdParts, "-env", fmt.Sprintf("%s=%s", k, v))

		}

	}

	cmdParts = append(cmdParts, "-tool", tool.Name)

	// Generate wrapper script 'bin'

	// We use "$@" to pass through any extra args (though agentctl currently passes stdin)

	scriptContent := "#!/bin/sh\nexec "

	for _, part := range cmdParts {

		// Simple quoting

		scriptContent += fmt.Sprintf("'%s' ", strings.ReplaceAll(part, "'", "'\\''"))

	}

	scriptContent += "\"$@\"\n"

	binPath := filepath.Join(skillDir, "bin")

	if err := os.WriteFile(binPath, []byte(scriptContent), 0755); err != nil {

		return fmt.Errorf("failed to write bin wrapper: %w", err)

	}

	// Map MCP Schema to Agentctl Signature

	signatureParameters := []map[string]any{}

	if tool.InputSchema.Properties != nil {

		for propName, propDef := range tool.InputSchema.Properties {

			if defMap, ok := propDef.(map[string]any); ok {

				param := map[string]any{

					"name": propName,
				}

				if desc, ok := defMap["description"]; ok {

					param["description"] = desc

				}

				if t, ok := defMap["type"]; ok {

					param["type"] = t

				}

				// Check required

				required := false

				for _, req := range tool.InputSchema.Required {

					if req == propName {

						required = true

						break

					}

				}

				param["required"] = required

				signatureParameters = append(signatureParameters, param)

			}

		}

	}

	manifest := map[string]any{

		"apiVersion": "agentctl/v1",

		"kind": "Skill",

		"metadata": map[string]any{

			"name": "mcp_generated/" + tool.Name,

			"version": "1.0.0",

			"description": tool.Description,
		},

		"distribution": map[string]any{

			"type": "exec",

			"exec": map[string]string{

				"entry": "bin",
			},
		},

		"capabilities": map[string]any{

			"network": "none",

			"filesystem": []map[string]string{

				{"type": "workdir"},
			},
		},

		"signature": map[string]any{

			"command": tool.Name, // Or mcp_generated/tool.Name? Signature command usually matches usage

			"parameters": signatureParameters,
		},
	}

	// Marshal to YAML

	data, err := yaml.Marshal(manifest)

	if err != nil {

		return err

	}

	// Write skill.yaml

	return os.WriteFile(filepath.Join(skillDir, "skill.yaml"), data, 0644)

}

func parseInput(r io.Reader) (input, error) {

	var in input

	if err := json.NewDecoder(r).Decode(&in); err != nil {

		return input{}, fmt.Errorf("decode input: %w", err)

	}

	if in.ServerCmd == "" && in.ServerURL == "" {

		return input{}, fmt.Errorf("server_cmd or server_url is required")

	}

	if in.BridgePath == "" {

		in.BridgePath = "mcp_bridge" // Default assuming it's in PATH or aliased

	}

	return in, nil

}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit mcp/install failure")
	os.Exit(1)
}
