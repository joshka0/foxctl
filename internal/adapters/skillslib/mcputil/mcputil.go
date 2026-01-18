// Package mcputil provides shared helpers for MCP client setup in skills.
package mcputil

import (
	"context"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// Config captures MCP connection settings.
type Config struct {
	ServerCmd     string
	ServerArgs    []string
	ServerEnv     map[string]string
	ServerURL     string
	ServerHeaders map[string]string
}

// NewClient returns a connected MCP client using HTTP (server_url) or stdio (server_cmd).
func NewClient(ctx context.Context, cfg Config) (*client.Client, error) {
	if cfg.ServerURL != "" {
		c, err := client.NewStreamableHttpClient(cfg.ServerURL, transport.WithHTTPHeaders(cfg.ServerHeaders))
		if err != nil {
			return nil, fmt.Errorf("create http client: %w", err)
		}
		return c, nil
	}

	if cfg.ServerCmd == "" {
		return nil, fmt.Errorf("server_cmd or server_url is required")
	}

	env := os.Environ()
	for k, v := range cfg.ServerEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	c, err := client.NewStdioMCPClient(cfg.ServerCmd, env, cfg.ServerArgs...)
	if err != nil {
		return nil, fmt.Errorf("create stdio client: %w", err)
	}
	return c, nil
}

// Initialize performs the MCP initialize handshake.
func Initialize(ctx context.Context, c *client.Client, name, version string) error {
	req := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    name,
				Version: version,
			},
			Capabilities: mcp.ClientCapabilities{},
		},
	}
	if _, err := c.Initialize(ctx, req); err != nil {
		return fmt.Errorf("mcp initialization failed: %w", err)
	}
	return nil
}
