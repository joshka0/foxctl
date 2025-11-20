// Package main implements the mcp/bridge skill - a bridge to MCP servers.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

type input struct {
	ServerCmd     string            `json:"server_cmd"`
	ServerArgs    []string          `json:"server_args"`
	ServerEnv     map[string]string `json:"server_env"`
	ServerURL     string            `json:"server_url"`
	ServerHeaders map[string]string `json:"server_headers"`
	ToolName      string            `json:"tool_name"`
	ToolArgs      map[string]any    `json:"tool_args"`
}

// arrayFlags allows parsing repeated flags like -arg "foo" -arg "bar"
type arrayFlags []string

func (i *arrayFlags) String() string {
	return strings.Join(*i, " ")
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func main() {
	// Parse flags
	var (
		serverCmd     string
		serverURL     string
		toolName      string
		serverArgs    arrayFlags
		serverEnv     arrayFlags
		serverHeaders arrayFlags
	)
	flag.StringVar(&serverCmd, "server-cmd", "", "Command to start the MCP server")
	flag.StringVar(&serverURL, "server-url", "", "URL of the MCP server (SSE)")
	flag.StringVar(&toolName, "tool", "", "Name of the tool to call")
	flag.Var(&serverArgs, "server-arg", "Argument for the server command (can be repeated)")
	flag.Var(&serverEnv, "env", "Environment variable for the server in KEY=VALUE format (can be repeated)")
	flag.Var(&serverHeaders, "header", "HTTP header for SSE connection in KEY=VALUE format (can be repeated)")
	flag.Parse()

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

	in, err := parseInput(os.Stdin, serverCmd, serverURL, toolName, serverArgs, serverEnv, serverHeaders)
	if err != nil {
		fail("mcp/bridge", "EARG", err)
	}
	if err := run(ctx, rc, in); err != nil {
		fail("mcp/bridge", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, in input) error {
	var mcpClient *client.Client
	var err error

	if in.ServerURL != "" {
		// SSE/HTTP Transport
		// We use StreamableHTTP transport which supports both single-response and streaming responses via POST
		mcpClient, err = client.NewStreamableHttpClient(in.ServerURL, transport.WithHTTPHeaders(in.ServerHeaders))
		if err != nil {
			return fmt.Errorf("failed to create HTTP client: %w", err)
		}
		// StreamableHTTP does not require explicit Start() for request-response,
		// but we can call it to be safe (it handles background listening if enabled).
		// client.NewStreamableHttpClient does NOT enable continuous listening by default.
		// If we want notifications, we might need it.
		// But for now, let's just use it.
		// Start() in StreamableHTTP is strictly for "Continuous Listening" (GET).
		// Exa rejected GET. So we probably shouldn't call Start() if it triggers GET.
		// Checking StreamableHTTP.Start():
		// if c.getListeningEnabled { ... createGETConnectionToServer ... }
		// By default getListeningEnabled is false. So Start() does nothing.
		// So we can call it or not.
		if err := mcpClient.Start(ctx); err != nil {
			return fmt.Errorf("failed to start transport: %w", err)
		}

	} else if in.ServerCmd != "" {
		// Stdio Transport
		env := os.Environ()
		for k, v := range in.ServerEnv {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		// NewStdioMCPClient starts the transport automatically
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

func parseInput(r io.Reader, cmdFlag, urlFlag, toolFlag string, argsFlag, envFlag, headersFlag []string) (input, error) {
	// If flags are provided (at least tool and one of cmd/url), we are in "Tool Mode"
	if toolFlag != "" && (cmdFlag != "" || urlFlag != "") {
		// Parse Stdin as simple arguments map
		var toolArgs map[string]any
		if err := json.NewDecoder(r).Decode(&toolArgs); err != nil && err != io.EOF {
			return input{}, fmt.Errorf("decode tool args: %w", err)
		}

		// Parse Env flags
		envMap := make(map[string]string)
		for _, e := range envFlag {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				envMap[parts[0]] = parts[1]
			}
		}

		// Parse Header flags
		headerMap := make(map[string]string)
		for _, h := range headersFlag {
			parts := strings.SplitN(h, "=", 2)
			if len(parts) == 2 {
				headerMap[parts[0]] = parts[1]
			}
		}

		return input{
			ServerCmd:     cmdFlag,
			ServerArgs:    argsFlag,
			ServerEnv:     envMap,
			ServerURL:     urlFlag,
			ServerHeaders: headerMap,
			ToolName:      toolFlag,
			ToolArgs:      toolArgs,
		}, nil
	}

	// Legacy Mode: Parse everything from Stdin
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}
	if in.ServerCmd == "" && in.ServerURL == "" {
		return input{}, fmt.Errorf("server_cmd or server_url is required")
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
