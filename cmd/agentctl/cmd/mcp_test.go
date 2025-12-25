package cmd

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestGetArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    any
		wantLen int
		wantKey string
		wantVal any
	}{
		{
			name:    "valid map",
			args:    map[string]any{"query": "test"},
			wantLen: 1,
			wantKey: "query",
			wantVal: "test",
		},
		{
			name:    "nil args",
			args:    nil,
			wantLen: 0,
		},
		{
			name:    "wrong type",
			args:    "not a map",
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = tt.args

			got := getArgs(req)
			if len(got) != tt.wantLen {
				t.Errorf("getArgs() len = %d, want %d", len(got), tt.wantLen)
			}
			if tt.wantKey != "" {
				if v, ok := got[tt.wantKey]; !ok || v != tt.wantVal {
					t.Errorf("getArgs()[%s] = %v, want %v", tt.wantKey, v, tt.wantVal)
				}
			}
		})
	}
}

func TestExtractLibraryID(t *testing.T) {
	tests := []struct {
		name    string
		content []mcp.Content
		want    string
	}{
		{
			name: "path in text",
			content: []mcp.Content{
				mcp.TextContent{Text: "Library ID: /vercel/next.js"},
			},
			want: "/vercel/next.js",
		},
		{
			name: "just path",
			content: []mcp.Content{
				mcp.TextContent{Text: "/mongodb/docs"},
			},
			want: "/mongodb/docs",
		},
		{
			name:    "empty content",
			content: []mcp.Content{},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &mcp.CallToolResult{Content: tt.content}
			got := extractLibraryID(result)
			if got != tt.want {
				t.Errorf("extractLibraryID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInitBackendConfigs(t *testing.T) {
	// Set env for test
	t.Setenv("TAVILY_API_KEY", "test-key")

	// Reset backends
	backends.configs = make(map[string]mcpServerConfig)

	initBackendConfigs()

	// Check tavily was configured
	if cfg, ok := backends.configs["tavily"]; !ok {
		t.Error("tavily config not set")
	} else {
		if cfg.Command != "npx" {
			t.Errorf("tavily command = %q, want npx", cfg.Command)
		}
		if cfg.Env["TAVILY_API_KEY"] != "test-key" {
			t.Errorf("tavily env key = %q, want test-key", cfg.Env["TAVILY_API_KEY"])
		}
	}

	// context7 should always be configured (no API key needed)
	if _, ok := backends.configs["context7"]; !ok {
		t.Error("context7 config not set")
	}
}
