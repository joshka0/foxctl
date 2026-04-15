# LSP Skills Extension Guide

This document describes how to add new language server skills to foxctl.

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                   foxctl                          │
│  ┌───────────┐  ┌───────────┐  ┌───────────────┐   │
│  │ lsp/gopls │  │lsp/tsserver│ │lsp/rust-analyzer│  │
│  └─────┬─────┘  └─────┬─────┘  └───────┬───────┘   │
│        │              │                │            │
│        ▼              ▼                ▼            │
│  ┌─────────────────────────────────────────────┐   │
│  │        Language Server CLI Wrappers          │   │
│  └─────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────┘
         │              │                │
         ▼              ▼                ▼
    ┌─────────┐  ┌────────────┐  ┌──────────────┐
    │  gopls  │  │ ts-server  │  │rust-analyzer │
    └─────────┘  └────────────┘  └──────────────┘
```

## Supported Language Servers

| Language | Server | Install | Status |
|----------|--------|---------|--------|
| Go | gopls | `go install golang.org/x/tools/gopls@latest` | ✅ Implemented |
| TypeScript/JS | typescript-language-server | `npm i -g typescript-language-server` | ✅ Implemented |
| Python | pylsp | `pip install python-lsp-server` | ✅ Implemented |
| Rust | rust-analyzer | `rustup component add rust-analyzer` | 📋 Template |

## Common Operations

All LSP skills should support these core operations:

| Operation | Description | Input |
|-----------|-------------|-------|
| `symbols` | List file symbols | `file` |
| `definition` | Go to definition | `file`, `line`, `column` |
| `references` | Find all references | `file`, `line`, `column` |
| `hover` | Get hover information | `file`, `line`, `column` |
| `workspace_symbol` | Search workspace | `query` |

Optional operations:

| Operation | Description | Notes |
|-----------|-------------|-------|
| `call_hierarchy` | Callers/callees | Not all LSPs support |
| `implementation` | Interface implementations | Language-dependent |
| `rename` | Rename symbol | Mutating operation |
| `diagnostics` | Get errors/warnings | Via `check` operation |

## Creating a New LSP Skill

### 1. Directory Structure

```
skills/lsp_<server>/
├── main.go          # Skill implementation
├── main_test.go     # Tests
├── skill.yaml       # Manifest
└── README.md        # Documentation
```

### 2. Skill Manifest Template

```yaml
apiVersion: foxctl/v1
kind: Skill
metadata:
  name: lsp/<server>
  version: 0.1.0
  description: "<Language> language server operations via <server>"
  tags: ["lsp", "<language>", "<server>", "navigation", "references"]
distribution:
  type: exec
  exec:
    entry: skills/lsp_<server>/lsp_<server>
io:
  format: JSON
  inline_output_kb: 64
signature:
  command: lsp/<server>
  parameters:
    - name: operation
      type: string
      required: true
      enum: ["definition", "references", "symbols", "workspace_symbol", "hover"]
      description: "LSP operation to perform"
    - name: file
      type: string
      required: false
      description: "File path"
    - name: line
      type: integer
      required: false
      description: "Line number (1-based)"
    - name: column
      type: integer
      required: false
      description: "Column number (1-based)"
    - name: query
      type: string
      required: false
      description: "Search query"
    - name: max_results
      type: integer
      required: false
      default: 50
      description: "Maximum results"
returns:
  - name: results
    type: array
capabilities:
  network: "none"
  filesystem:
    - type: workdir
  pure: true
```

### 3. Implementation Pattern

```go
package main

import (
    "context"
    "os/exec"
    // ... standard imports
)

type input struct {
    Operation  string `json:"operation"`
    File       string `json:"file"`
    Line       int    `json:"line"`
    Column     int    `json:"column"`
    Query      string `json:"query"`
    MaxResults int    `json:"max_results"`
}

func main() {
    // 1. Load config and create runner context
    // 2. Parse input
    // 3. Check LSP availability
    // 4. Route to operation handler
    // 5. Emit output
}

func runSymbols(ctx context.Context, serverPath, workspace string, in input) ([]Symbol, error) {
    // Execute: <server> <symbols-command> <file>
    // Parse output
    // Return structured result
}

// ... other operation handlers
```

### 4. CLI Output Parsing

Each LSP server has different CLI output formats. Key patterns:

**gopls:**
```
# symbols: "Name Kind Line:Col-Line:Col"
NewHandler Function 24:6-24:16

# references: "/path/file.go:line:col-col"
/path/file.go:28:7-17

# definition: "/path/file.go:line:col: defined here as <text>"
/path/file.go:24:6-16: defined here as func NewHandler...
```

**typescript-language-server:**
Uses JSON-RPC over stdio. Requires managing the LSP lifecycle:
```go
// Start server
cmd := exec.Command("typescript-language-server", "--stdio")
stdin, _ := cmd.StdinPipe()
stdout, _ := cmd.StdoutPipe()
cmd.Start()

// Send initialize request
// Send textDocument/didOpen
// Send actual request
// Parse JSON-RPC response
```

## Testing

### Unit Tests

Test parsing functions without requiring the LSP server:

```go
func TestParseSymbolLine(t *testing.T) {
    sym, err := parseSymbolLine("MyStruct Struct 12:6-12:14", "/workspace")
    // assertions
}
```

### Integration Tests

Skip if LSP not available:

```go
func skipIfNoServer(t *testing.T) {
    if _, err := exec.LookPath("<server>"); err != nil {
        t.Skip("<server> not available")
    }
}
```

## Claude Code Integration

### Skill Documentation

Create `.claude/skills/foxctl-lsp-<lang>/Skill.md`:

```markdown
---
name: foxctl LSP <Language>
description: <Language> language server operations
---

# LSP <Language> Analysis

...usage examples...
```

### Pre-impl Integration

Update `.claude/commands/pre-impl.md` to include the new language:

```markdown
### Semantic Analysis (<Language> projects)

Use LSP for deeper understanding:

\`\`\`bash
foxctl run lsp/<server> --input '{"operation": "references", ...}'
\`\`\`
```

## Future: MCP Integration

Some language servers now support MCP directly:

- **gopls**: `gopls serve -mcp.listen=localhost:8092`
- **rust-analyzer**: MCP support planned

This allows running the LSP as an MCP server, eliminating the need for CLI wrappers.

## Troubleshooting

### Common Issues

1. **Server not found**: Ensure the server is in PATH
2. **Parse errors**: LSP output format may differ between versions
3. **Workspace issues**: Some operations require proper project structure (go.mod, package.json, etc.)

### Debug Mode

Set `FOXCTL_DEBUG=1` to see raw LSP output:

```bash
FOXCTL_DEBUG=1 foxctl run lsp/gopls --input '...'
```
