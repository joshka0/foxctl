// Package main implements the mcp/install skill - installs MCP server configurations.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/mcputil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
)

const command = "mcp/install"

var logger = zerolog.New(os.Stderr).With().Timestamp().Str("skill", command).Logger()

// input defines the skill input parameters for MCP server installation with multiple connection modes.
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
	if err := runMain(); err != nil {
		os.Exit(1)
	}
}

// runMain is the main entry point with bootstrap and error handling for MCP installation.
func runMain() error {
	ctx := context.Background()
	rc, err := skillmain.Bootstrap(ctx, os.Stdout)
	if err != nil {
		return skillout.Fatal(os.Stdout, command, skillerr.WrapRuntime("bootstrap", err))
	}

	defer func() {
		errs.Ignore(rc.Close(), "run context close")
	}()

	in, err := parseInput(os.Stdin)
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

// run orchestrates MCP server installation with client creation, tool discovery, and skill generation.
//
// Index:
// - Purpose: Install MCP server configurations by discovering tools and generating foxctl skills
// - Flow: validate input → create MCP client → initialize connection → list tools → generate skills → emit results
// - SideEffects: creates skill directories; generates wrapper scripts; writes skill manifests; validates paths
// - FailureModes: missing server configuration, MCP client failures, tool discovery errors, file system errors
// - Observability: emits installation results, tool counts, generated paths, and comprehensive error tracking
// - Related: generateSkill, parseInput, mcputil.NewClient, mcputil.Initialize
// - Keywords: mcp/install, mcp_server, skill_generation, tool_discovery, bridge_client
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
			logger.Warn().Err(err).Msg("failed to close MCP client")
		}
	}()

	if err := mcputil.Initialize(ctx, mcpClient, "foxctl-mcp-install", "1.0.0"); err != nil {
		return skillerr.WrapRuntime("mcp initialization failed", err)
	}

	// List Tools

	toolsResult, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return skillerr.WrapRuntime("failed to list tools", err)
	}

	installed := []string{}

	// Ensure output directory exists

	if in.OutputDir == "" {
		in.OutputDir = "."
	}

	// Validate output directory (must be within workspace per foxctl policy)
	validDir, err := skillmain.ValidatePath(
		rc,
		in.OutputDir,
		skillmain.WithPathMessage("output directory validation failed"),
		skillmain.WithPathHint("Provide an output_dir within the workspace or an allowed root."),
	)
	if err != nil {
		return err
	}

	for _, tool := range toolsResult.Tools {

		if err := generateSkill(validDir, tool, in); err != nil {
			logger.Error().Err(err).Str("tool", tool.Name).Msg("failed to generate skill")
			continue
		}

		installed = append(installed, tool.Name)

	}

	// Emit success
	return skillout.Emit(rc, command, map[string]any{
		"installed": installed,
		"count":     len(installed),
		"path":      validDir,
	})
}

// generateSkill creates an foxctl skill wrapper for an MCP tool with script generation and manifest creation.
func generateSkill(baseDir string, tool mcp.Tool, in input) error {
	// Sanitize tool name to prevent path traversal
	sanitized := filepath.Base(tool.Name)
	if sanitized != tool.Name || sanitized == "." || sanitized == ".." {
		return skillerr.Validation(fmt.Sprintf("invalid tool name: %s", tool.Name))
	}

	// Create directory for the skill
	skillDir := filepath.Join(baseDir, sanitized)

	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return skillerr.WrapIO("create skill directory", err)
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

	// We use "$@" to pass through any extra args (though foxctl currently passes stdin)

	scriptContent := "#!/bin/sh\nexec "

	for _, part := range cmdParts {
		// Simple quoting

		scriptContent += fmt.Sprintf("'%s' ", strings.ReplaceAll(part, "'", "'\\''"))
	}

	scriptContent += "\"$@\"\n"

	binPath := filepath.Join(skillDir, "bin")

	if err := os.WriteFile(binPath, []byte(scriptContent), 0o755); err != nil {
		return skillerr.WrapIO("failed to write bin wrapper", err)
	}

	// Map MCP Schema to Foxctl Signature
	signatureParameters := []map[string]any{}

	// Collect property names and sort for deterministic output
	if tool.InputSchema.Properties != nil {
		propNames := make([]string, 0, len(tool.InputSchema.Properties))
		for propName := range tool.InputSchema.Properties {
			propNames = append(propNames, propName)
		}
		sort.Strings(propNames)

		for _, propName := range propNames {
			propDef := tool.InputSchema.Properties[propName]
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
		"apiVersion": "foxctl/v1",

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
			"network": "egress",

			"egressAllow": []string{"*"}, // MCP bridge needs network access to communicate with MCP servers

			"filesystem": []map[string]string{
				{"type": "workdir"},
			},
		},

		"signature": map[string]any{
			// Command uses bare tool name for CLI invocation (e.g., "list_files")
			// while metadata.name includes namespace prefix (e.g., "mcp_generated/list_files")
			"command": tool.Name,

			"parameters": signatureParameters,
		},
	}

	// Marshal to YAML

	data, err := yaml.Marshal(manifest)
	if err != nil {
		return skillerr.WrapRuntime("marshal skill manifest", err)
	}

	// Write skill.yaml

	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), data, 0o644); err != nil {
		return skillerr.WrapIO("write skill.yaml", err)
	}
	return nil
}

// parseInput validates and processes input parameters with bridge binary validation and defaults.
func parseInput(r io.Reader) (input, error) {
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

	if in.BridgePath == "" {
		in.BridgePath = "mcp_bridge" // Default assuming it's in PATH or aliased
	}

	// Validate bridge exists
	resolved, err := executil.RequireTool(in.BridgePath, "set bridge_path in input or ensure mcp_bridge is in PATH")
	if err != nil {
		return input{}, skillerr.Runtime(
			fmt.Sprintf("bridge binary not found: %s", in.BridgePath),
			skillerr.WithCause(err),
			skillerr.WithHint("Set bridge_path in input or ensure mcp_bridge is in PATH."),
		)
	}
	in.BridgePath = resolved

	return in, nil
}
