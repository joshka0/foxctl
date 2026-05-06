// Package main implements the mcp/bridge skill - a bridge to MCP servers.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/mcputil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
)

const command = "mcp/bridge"

// input defines the skill input parameters for MCP server connections and tool calls.
type input struct {
	ServerCmd     string            `json:"server_cmd"`
	ServerArgs    []string          `json:"server_args"`
	ServerEnv     map[string]string `json:"server_env"`
	ServerURL     string            `json:"server_url"`
	ServerHeaders map[string]string `json:"server_headers"`
	ToolName      string            `json:"tool_name"`
	ToolArgs      map[string]any    `json:"tool_args"`
}

// arrayFlags allows parsing repeated flags like -arg "foo" -arg "bar" for command-line interface.
type arrayFlags []string

// String returns the concatenated string representation of the array flags.
func (i *arrayFlags) String() string {
	return strings.Join(*i, " ")
}

// Set adds a value to the array flags collection for command-line parsing.
func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

// main is the skill entry point for mcp/bridge with command-line flag parsing and error handling.
func main() {
	if err := runMain(); err != nil {
		os.Exit(1)
	}
}

// runMain orchestrates the MCP bridge skill with flag parsing, input validation, and error handling.
func runMain() error {
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
	rc, err := skillmain.Bootstrap(ctx, os.Stdout)
	if err != nil {
		return skillout.Fatal(os.Stdout, command, skillerr.WrapRuntime("bootstrap", err))
	}
	defer func() {
		errs.Ignore(rc.Close(), "run context close")
	}()

	in, err := parseInput(os.Stdin, serverCmd, serverURL, toolName, serverArgs, serverEnv, serverHeaders)
	if err != nil {
		var skillErr *skillerr.Error
		if errors.As(err, &skillErr) {
			return skillout.Fatal(os.Stdout, command, skillErr)
		}
		return skillout.Fatal(os.Stdout, command, skillerr.WrapArg("parse input", err))
	}
	if err := run(ctx, rc, in); err != nil {
		var skillErr *skillerr.Error
		if errors.As(err, &skillErr) {
			return skillout.Fatal(os.Stdout, command, skillErr)
		}
		return skillout.Fatal(os.Stdout, command, skillerr.WrapRuntime("execute", err))
	}
	return nil
}

// run orchestrates MCP server connections and tool execution with stdio and SSE transport support.
//
// Index:
//   Purpose: Bridge to MCP servers supporting both stdio and SSE transports for tool execution
//   Keywords: mcp/bridge, mcp_server, tool_execution, stdio_transport, sse_transport, protocol_bridge
//   Related: mcputil.NewClient, mcputil.Initialize, parseInput
//   Flow: validate input → create MCP client → initialize connection → call tool → format results → emit output
//   Resources: MCP server process; HTTP connection
//   Events: mcp-tool-called
//   OutputFields: is_error, content
//
// [[domain:mcp-bridge]]
// [[protocol:mcp-protocol]]
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	var mcpClient *client.Client
	var err error

	if in.ServerURL == "" && in.ServerCmd == "" {
		return skillerr.Arg(
			"either server_cmd or server_url is required",
			skillerr.WithHint("Provide server_cmd for stdio or server_url for SSE."),
		)
	}

	mcpClient, err = mcputil.NewClient(ctx, mcputil.Config{
		ServerCmd:     in.ServerCmd,
		ServerArgs:    in.ServerArgs,
		ServerEnv:     in.ServerEnv,
		ServerURL:     in.ServerURL,
		ServerHeaders: in.ServerHeaders,
	})
	if err != nil {
		return skillerr.WrapRuntime("failed to create MCP client", err)
	}
	defer func() {
		if err := mcpClient.Close(); err != nil {
			// Log close error but don't fail the operation.
			rc.Logger.Warn().Err(err).Msg("mcp bridge: failed to close MCP client")
		}
	}()

	if err := mcputil.Initialize(ctx, mcpClient, "foxctl-mcp-bridge", "1.0.0"); err != nil {
		return skillerr.WrapRuntime("mcp initialization failed", err)
	}

	// Call the tool
	toolResult, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      in.ToolName,
			Arguments: in.ToolArgs,
		},
	})
	if err != nil {
		return skillerr.WrapRuntime("tool call failed", err)
	}

	// Format Output
	data := map[string]any{
		"is_error": toolResult.IsError,
		"content":  toolResult.Content,
	}

	return skillout.Emit(rc, command, data)
}

// parseInput parses input from stdin and command-line flags supporting both tool mode and legacy mode.
func parseInput(r io.Reader, cmdFlag, urlFlag, toolFlag string, argsFlag, envFlag, headersFlag []string) (input, error) {
	// If flags are provided (at least tool and one of cmd/url), we are in "Tool Mode"
	if toolFlag != "" && (cmdFlag != "" || urlFlag != "") {
		// Parse Stdin as simple arguments map
		var toolArgs map[string]any
		if err := json.NewDecoder(r).Decode(&toolArgs); err != nil && err != io.EOF {
			return input{}, skillerr.WrapParse("decode tool args", err)
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
		return input{}, skillerr.WrapParse("decode input", err)
	}
	if in.ServerCmd == "" && in.ServerURL == "" {
		return input{}, skillerr.Arg(
			"server_cmd or server_url is required",
			skillerr.WithHint("Provide server_cmd for stdio or server_url for SSE."),
		)
	}
	if in.ToolName == "" {
		return input{}, skillerr.Arg("tool_name is required")
	}
	return in, nil
}
