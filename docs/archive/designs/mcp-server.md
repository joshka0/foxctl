# foxctl MCP Server Design

## Overview

Expose foxctl skills as MCP (Model Context Protocol) tools, enabling Codex CLI, Cursor, and other MCP-compatible AI assistants to use the same enhanced tooling.

## Architecture

```
┌─────────────────┐     stdio      ┌──────────────────┐
│  Codex / Cursor │ ◄────────────► │ foxctl-mcp     │
│  (MCP Client)   │    JSON-RPC    │ (MCP Server)     │
└─────────────────┘                └────────┬─────────┘
                                            │
                                            ▼
                                   ┌──────────────────┐
                                   │ Skill Executor   │
                                   │ (in-process)     │
                                   └────────┬─────────┘
                                            │
                              ┌─────────────┼─────────────┐
                              ▼             ▼             ▼
                        ┌─────────┐   ┌─────────┐   ┌─────────┐
                        │ code/   │   │ fs/     │   │ ci/     │
                        │ skills  │   │ skills  │   │ skills  │
                        └─────────┘   └─────────┘   └─────────┘
```

## Tool Mappings

### Strict Mode Tools (Primary)

These replace standard file/search operations:

| MCP Tool Name | foxctl Skill | Description |
|---------------|----------------|-------------|
| `smart_search` | code/smart_search | Semantic search + snippet extraction |
| `semantic_search` | code/semantic_search | Vector similarity search |
| `context_grep` | code/context_grep | Regex/AST search with function expansion |
| `apply_edit` | fs/apply_edit | Safe file editing with dry-run |
| `smart_write` | code/smart_write | Context-aware file writing |

### CI/GitHub Tools

| MCP Tool Name | foxctl Skill | Description |
|---------------|----------------|-------------|
| `pr_comments` | ci/prcomments | Get PR review comments |
| `checks` | ci/checks | Get CI status and checks |
| `pr_diff` | ci/prdiff | Get PR diff |

### Memory/Context Tools

| MCP Tool Name | foxctl Skill | Description |
|---------------|----------------|-------------|
| `memory_query` | memory/query | Query workspace memories |
| `memory_put` | memory/put | Store new memory |
| `codemap_generate` | codemap/generate | Generate code relationship map |

## Implementation

### Package Structure

```
cmd/foxctl-mcp/
├── main.go           # MCP server entry point
├── tools.go          # Tool definitions and handlers
└── transport.go      # Stdio transport wrapper

internal/mcp/
├── server.go         # MCP server wrapper
├── tools/
│   ├── search.go     # Search tool implementations
│   ├── edit.go       # Edit tool implementations
│   └── ci.go         # CI tool implementations
└── schema/
    └── gen.go        # JSON schema generation from skill manifests
```

### Tool Definition Pattern

```go
package main

import (
    "github.com/modelcontextprotocol/go-sdk/mcp"
)

// SmartSearchInput defines the input schema
type SmartSearchInput struct {
    Query     string `json:"query" jsonschema:"required,description=Search query or question"`
    Path      string `json:"path,omitempty" jsonschema:"description=Path to search within"`
    MaxFiles  int    `json:"max_files,omitempty" jsonschema:"description=Maximum files to return"`
}

// SmartSearchOutput defines the output schema
type SmartSearchOutput struct {
    Snippets []Snippet `json:"snippets"`
    Summary  string    `json:"summary"`
}

func SmartSearch(ctx context.Context, req *mcp.CallToolRequest, input SmartSearchInput) (*mcp.CallToolResult, SmartSearchOutput, error) {
    // Execute skill
    result, err := executor.Run(ctx, "code/smart_search", map[string]any{
        "question": input.Query,
        "workspace_id": workspace,
        "limits": map[string]any{
            "max_candidates": input.MaxFiles,
        },
    })
    if err != nil {
        return nil, SmartSearchOutput{}, err
    }

    // Transform result
    output := transformSmartSearchResult(result)
    return nil, output, nil
}
```

### Server Initialization

```go
func main() {
    server := mcp.NewServer(&mcp.Implementation{
        Name:    "foxctl",
        Version: version.Version,
    }, nil)

    // Register strict mode tools
    mcp.AddTool(server, &mcp.Tool{
        Name:        "smart_search",
        Description: "Semantic code search with snippet extraction",
    }, SmartSearch)

    mcp.AddTool(server, &mcp.Tool{
        Name:        "context_grep",
        Description: "Regex/AST pattern search with function-level context",
    }, ContextGrep)

    mcp.AddTool(server, &mcp.Tool{
        Name:        "apply_edit",
        Description: "Apply file edits with dry-run preview",
    }, ApplyEdit)

    // Run over stdio
    if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
        log.Fatal(err)
    }
}
```

## Client Configuration

### Codex CLI (~/.codex/config.json)

```json
{
  "mcpServers": {
    "foxctl": {
      "command": "foxctl-mcp",
      "args": ["--workspace", "${workspaceFolder}"]
    }
  }
}
```

### Cursor (settings.json)

```json
{
  "mcp.servers": {
    "foxctl": {
      "command": "foxctl-mcp",
      "args": ["--workspace", "${workspaceFolder}"]
    }
  }
}
```

### Claude Code (~/.claude/settings.json)

```json
{
  "mcpServers": {
    "foxctl": {
      "command": "foxctl-mcp",
      "args": []
    }
  }
}
```

## Workspace Detection

The MCP server needs workspace context for:
- Scoping semantic search
- Finding skill manifests
- Memory queries

Options:
1. **CLI argument**: `--workspace /path/to/project`
2. **Environment variable**: `FOXCTL_WORKSPACE`
3. **Auto-detect**: Use cwd or git root

```go
func detectWorkspace(args []string) string {
    // 1. Check --workspace flag
    if ws := flagWorkspace; ws != "" {
        return ws
    }
    // 2. Check env
    if ws := os.Getenv("FOXCTL_WORKSPACE"); ws != "" {
        return ws
    }
    // 3. Auto-detect git root
    return workspace.Detect("")
}
```

## Error Handling

MCP tools should return structured errors:

```go
type ToolError struct {
    Code    string `json:"code"`
    Message string `json:"message"`
    Details any    `json:"details,omitempty"`
}

// Return as tool result with isError=true
return &mcp.CallToolResult{
    IsError: true,
    Content: []mcp.Content{
        mcp.TextContent{Text: err.Error()},
    },
}, nil, nil
```

## Streaming Support

For long-running operations (codemap generation), use MCP progress notifications:

```go
func CodemapGenerate(ctx context.Context, req *mcp.CallToolRequest, input CodemapInput) (*mcp.CallToolResult, CodemapOutput, error) {
    // Send progress updates
    session.SendProgress(ctx, &mcp.Progress{
        Token:   req.ProgressToken,
        Current: 0,
        Total:   100,
        Message: "Analyzing code structure...",
    })

    // ... generate codemap ...

    session.SendProgress(ctx, &mcp.Progress{
        Token:   req.ProgressToken,
        Current: 100,
        Total:   100,
        Message: "Complete",
    })

    return nil, output, nil
}
```

## Build & Install

```bash
# Build MCP server binary
go build -o ~/.local/bin/foxctl-mcp ./cmd/foxctl-mcp/

# Or via make
make mcp-server
```

## Testing

```bash
# Test with MCP inspector
npx @anthropic-ai/mcp-inspector foxctl-mcp

# Test tool directly
echo '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"smart_search","arguments":{"query":"func main"}}}' | foxctl-mcp
```

## Phase 4 Implementation Steps

1. **Add go-sdk dependency**
   ```bash
   go get github.com/modelcontextprotocol/go-sdk@latest
   ```

2. **Create cmd/foxctl-mcp/main.go** - Basic server with tool registration

3. **Implement core tools**
   - smart_search
   - context_grep
   - apply_edit

4. **Add workspace detection**

5. **Test with Codex/Cursor**

6. **Add remaining tools** (CI, memory, codemap)

## Security Considerations

- MCP server runs with user permissions
- No additional sandboxing (inherits from client)
- Workspace scoping prevents access outside project
- dry_run mode for destructive operations
