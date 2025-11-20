# Gemini Agent Context for agentctl

## Project Overview
`agentctl` is a CLI tool for building structured, deterministic AI workflows ("Unix pipelines for AI agents").
It uses "skills" (sandboxed tools) and structured JSON envelopes for communication.

## Build Instructions
- **Build CLI**: `make build` (Output: `bin/agentctl`)
- **Build Skills**: `make skills-build` (Output: `dist/skills/`)
- **Test**: `make test`
- **Lint**: `make lint`

## Usage Guide

### Running Skills
Skills are located in `skills/` (source) or `dist/skills/` (built).
The CLI finds skills in standard paths. You might need to set `AGENTCTL_SKILL_PATH` if running from dev directories, but usually `bin/agentctl run <category>/<skill>` works if installed or if pointing to local sources.

**Common Commands:**
```bash
# List files
./bin/agentctl run fs/ls --path .

# Read file
./bin/agentctl run fs/read --path README.md

# Grep
./bin/agentctl run text/grep --pattern "func main" --path .
```

**Input/Output:**
- Inputs are passed as flags (simple) or JSON (complex).
- Output is always a JSON envelope.
- Look for `.data` in the JSON output for the actual result.

### Managing State
- **Memory**: `./bin/agentctl memory ...`
- **Jobs**: `./bin/agentctl jobs ...`

## Implemented Feature: MCP Tool Adapter

To enable `agentctl` to interact with Model Context Protocol (MCP) servers, we have implemented a generic **MCP Bridge Skill**.

### 1. Concept
We created a generic "bridge" binary (`skills/mcp_bridge`) that acts as an MCP Client. It:
1.  Connects to a specified MCP Server (via Stdio).
2.  Calls a specific Tool on that server.
3.  Returns the result in `agentctl` envelope format.

### 2. Components

#### A. The Bridge Skill (`skills/mcp_bridge`)
- **Source**: `skills/mcp_bridge/main.go`
- **Manifest**: `skills/mcp_bridge/skill.yaml`
- **Usage**: Accepts `server_cmd`, `server_args`, `tool_name`, `tool_args`.

#### B. The Installer Skill (`skills/mcp_install`)
- **Source**: `skills/mcp_install/main.go`
- **Manifest**: `skills/mcp_install/skill.yaml`
- **Purpose**: Introspects an MCP server and generates persistent `agentctl` skills for all available tools.
- **Usage**:
  ```bash
  # Install skills from a local python MCP server
  ./bin/agentctl run mcp/install --input '{
    "server_cmd": "python3",
    "server_args": ["/absolute/path/to/server.py"],
    "output_dir": "./my_skills",
    "bridge_path": "/absolute/path/to/mcp_bridge"
  }'
  
  # Install skills from a remote MCP server (HTTP/SSE)
  # Example: Exa (requires Accept header for Streamable HTTP)
  ./bin/agentctl run mcp/install --input '{
    "server_url": "https://mcp.exa.ai/mcp",
    "server_headers": {"Accept": "application/json, text/event-stream"},
    "output_dir": "./exa_skills",
    "bridge_path": "/absolute/path/to/mcp_bridge"
  }'
  ```

### 3. Usage Example

To use an MCP tool (e.g., a Python script `server.py` with tool `weather`):

```bash
# Run via agentctl (assuming built)
./bin/agentctl run mcp/bridge --input '{
  "server_cmd": "python3",
  "server_args": ["server.py"],
  "tool_name": "weather",
  "tool_args": {"city": "Paris"}
}'
```

To expose this permanently, create a wrapper skill YAML that calls this bridge with hardcoded `server_cmd` arguments.

### 4. Status
- [x] Create `skills/mcp_bridge` structure.
- [x] Implement MCP Client logic using `github.com/mark3labs/mcp-go`.
- [x] Verify with test MCP server.
- [ ] (Future) Automatic discovery/generation of wrapper skills.
