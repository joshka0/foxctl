# Gemini Agent Context for foxctl

> For the canonical foxctl protocol, profiles, and invariants, see
> `AGENTS.md`. This file focuses on **Gemini-specific** integration and the MCP
> bridge skills.

## Project Overview
`foxctl` is a CLI tool for building structured, deterministic AI workflows ("Unix pipelines for AI agents").
It uses "skills" (sandboxed tools) and structured JSON envelopes for communication.

## Build Instructions
- **Build CLI**: `make build` (Output: `foxctl`)
- **Build Skills**: `make skills-build` (Output: `dist/skills/`)
- **Test**: `make test`
- **Lint**: `make lint`

## Usage Guide

### Running Skills
Skills are located in `skills/` (source) or `dist/skills/` (built).
The CLI finds skills in standard paths. You might need to set `FOXCTL_SKILL_PATH` if running from dev directories, but usually `foxctl run <category>/<skill>` works if installed or if pointing to local sources.

**Common Commands:**
```bash
# List files
./foxctl run fs/ls --path .

# Read file
./foxctl run fs/read --path README.md

# Grep
./foxctl run text/grep --pattern "func main" --path .
```

**Input/Output:**
- Inputs are passed as flags (simple) or JSON (complex).
- Output is always a JSON envelope.
- Look for `.data` in the JSON output for the actual result.

### Managing State
- **Memory**: `./foxctl memory ...`
- **Jobs**: `./foxctl jobs ...`

## Implemented Feature: MCP Tool Adapter

To enable `foxctl` to interact with Model Context Protocol (MCP) servers, we have implemented a generic **MCP Bridge Skill**.

### 1. Concept
We created a generic "bridge" binary (`skills/mcp_bridge`) that acts as an MCP Client. It:
1.  Connects to a specified MCP Server (via Stdio).
2.  Calls a specific Tool on that server.
3.  Returns the result in `foxctl` envelope format.

### 2. Components

#### A. The Bridge Skill (`skills/mcp_bridge`)
- **Source**: `skills/mcp_bridge/main.go`
- **Manifest**: `skills/mcp_bridge/skill.yaml`
- **Usage**: Accepts `server_cmd`, `server_args`, `tool_name`, `tool_args`.

#### B. The Installer Skill (`skills/mcp_install`)
- **Source**: `skills/mcp_install/main.go`
- **Manifest**: `skills/mcp_install/skill.yaml`
- **Purpose**: Introspects an MCP server and generates persistent `foxctl` skills for all available tools.
- **Usage**:
  ```bash
  # Install skills from a local python MCP server
  ./foxctl run mcp/install --input '{
    "server_cmd": "python3",
    "server_args": ["/absolute/path/to/server.py"],
    "output_dir": "./my_skills",
    "bridge_path": "/absolute/path/to/mcp_bridge"
  }'
  
  # Install skills from a remote MCP server (HTTP/SSE)
  # Example: Exa (requires Accept header for Streamable HTTP)
  ./foxctl run mcp/install --input '{
    "server_url": "https://mcp.exa.ai/mcp",
    "server_headers": {"Accept": "application/json, text/event-stream"},
    "output_dir": "./exa_skills",
    "bridge_path": "/absolute/path/to/mcp_bridge"
  }'
  ```

### 4. Troubleshooting & Common Patterns

When working with the MCP adapter, keep these "gotchas" in mind:

1.  **HTTP/SSE Headers**:
    *   Remote MCP servers (like Exa) often use "Streamable HTTP" over standard SSE.
    *   They may strictly enforce `Accept` headers.
    *   **Fix**: Always check if the server requires `Accept: application/json, text/event-stream` and pass it in `server_headers`.

2.  **Network Capabilities**:
    *   `foxctl`'s `exec` runner currently enforces a strict `network: none` policy for sandboxing.
    *   However, the `mcp_bridge` is a native binary that *inherits* the host's network access.
    *   **Fix**: Generated skills must declare `network: none` in their manifest to pass validation, even though the bridge performs network I/O.

3.  **Path Resolution**:
    *   The `mcp/install` skill generates a wrapper script (`bin`) that calls the bridge.
    *   **Fix**: Ensure `bridge_path` is an absolute path or in your global `$PATH`, as the generated skill runs in a specific workspace directory and might not find relative paths easily.

### 5. Status
- [x] Create `skills/mcp_bridge` structure.
- [x] Implement MCP Client logic using `github.com/mark3labs/mcp-go`.
- [x] Verify with test MCP server.
- [x] Implement `mcp/install` for auto-discovery.
- [x] Add SSE/Streamable HTTP support (verified with Exa).
- [ ] (Future) Automatic discovery of auth requirements.
