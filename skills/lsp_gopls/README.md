# lsp/gopls - Go Language Server Skill

Go language server operations via gopls CLI.

## Prerequisites

```bash
go install golang.org/x/tools/gopls@latest
```

## Operations

| Operation | Description | Required Params |
|-----------|-------------|-----------------|
| `symbols` | List symbols in a file | `file` |
| `definition` | Go to definition | `file`, `line`, `column` |
| `references` | Find all references | `file`, `line`, `column` |
| `call_hierarchy` | Show callers/callees | `file`, `line`, `column` |
| `workspace_symbol` | Search workspace | `query` |
| `implementation` | Find interface impls | `file`, `line`, `column` |
| `check` | Get diagnostics | `file` |

## Usage

```bash
# List symbols in a file
agentctl run lsp/gopls --input '{"operation": "symbols", "file": "main.go"}'

# Find references
agentctl run lsp/gopls --input '{"operation": "references", "file": "main.go", "line": 25, "column": 6}'

# Call hierarchy
agentctl run lsp/gopls --input '{"operation": "call_hierarchy", "file": "main.go", "line": 25, "column": 6}'

# Workspace symbol search
agentctl run lsp/gopls --input '{"operation": "workspace_symbol", "query": "Handler"}'
```

## Output Format

```json
{
  "version": 1,
  "status": "ok",
  "command": "lsp/gopls",
  "data": {
    "operation": "symbols",
    "symbols": [
      {"name": "Handler", "kind": "Interface", "line": 10, "column": 6},
      {"name": "NewHandler", "kind": "Function", "line": 25, "column": 6}
    ],
    "count": 2
  }
}
```

## Extension Pattern

This skill follows a pattern that can be replicated for other language servers.
See `docs/lsp-skills.md` for the extension guide.
