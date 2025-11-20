package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/client"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

type input struct {
	ServerCmd  string            `json:"server_cmd"`
	ServerArgs []string          `json:"server_args"`
	ServerEnv  map[string]string `json:"server_env"`
	ToolName   string            `json:"tool_name"`
	ToolArgs   map[string]any    `json:"tool_args"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("mcp/bridge", "ECONFIG", err)
	}
	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("mcp/bridge", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("mcp/bridge", "EARG", err)
	}
	if err := run(ctx, rc, in); err != nil {
		fail("mcp/bridge", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, in input) error {
	// Construct Env
	env := os.Environ()
	for k, v := range in.ServerEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Initialize Client
	// NewStdioMCPClient launches the process and starts the transport
	mcpClient, err := client.NewStdioMCPClient(in.ServerCmd, env, in.ServerArgs...)
	if err != nil {
		return fmt.Errorf("failed to create mcp client: %w", err)
	}
	defer mcpClient.Close()

	// Initialize the MCP session
	initReq := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "agentctl-mcp-bridge",
				Version: "1.0.0",
			},
			Capabilities: mcp.ClientCapabilities{},
		},
	}

	_, err = mcpClient.Initialize(ctx, initReq)
	if err != nil {
		return fmt.Errorf("mcp initialization failed: %w", err)
	}

	// Call the tool
	toolResult, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      in.ToolName,
			Arguments: in.ToolArgs,
		},
	})
	if err != nil {
		return fmt.Errorf("tool call failed: %w", err)
	}

	// Format Output
	data := map[string]any{
		"is_error": toolResult.IsError,
		"content":  toolResult.Content,
	}

	return rc.Emit("mcp/bridge", data, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
}

func parseInput(r io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}
	if in.ServerCmd == "" {
		return input{}, fmt.Errorf("server_cmd is required")
	}
	if in.ToolName == "" {
		return input{}, fmt.Errorf("tool_name is required")
	}
	return in, nil
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit mcp/bridge failure")
	os.Exit(1)
}
