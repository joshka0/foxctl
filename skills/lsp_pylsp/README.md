# lsp/pylsp - Python Language Server Skill

Python language server operations via python-lsp-server (pylsp).

## Prerequisites

```bash
# Install with pip
pip install python-lsp-server

# Or with extras for additional features
pip install 'python-lsp-server[all]'

# Or using uv (fast package manager)
uv tool install python-lsp-server
```

## Operations

| Operation | Description | Required Params |
|-----------|-------------|-----------------|
| `symbols` | List symbols in a file | `file` |
| `definition` | Go to definition | `file`, `line`, `column` |
| `references` | Find all references | `file`, `line`, `column` |
| `workspace_symbol` | Search workspace | `query` |
| `hover` | Get hover documentation | `file`, `line`, `column` |

## Usage

```bash
# List symbols in a file
agentctl run lsp/pylsp --input '{"operation": "symbols", "file": "src/main.py"}'

# Find definition
agentctl run lsp/pylsp --input '{"operation": "definition", "file": "src/main.py", "line": 10, "column": 5}'

# Find references
agentctl run lsp/pylsp --input '{"operation": "references", "file": "src/main.py", "line": 10, "column": 5}'

# Workspace symbol search
agentctl run lsp/pylsp --input '{"operation": "workspace_symbol", "query": "Handler"}'

# Get hover documentation
agentctl run lsp/pylsp --input '{"operation": "hover", "file": "src/main.py", "line": 10, "column": 5}'
```

## Output Format

```json
{
  "version": 1,
  "status": "ok",
  "command": "lsp/pylsp",
  "data": {
    "operation": "symbols",
    "symbols": [
      {"name": "MyClass", "kind": "Class", "file": "src/main.py", "line": 5, "column": 1},
      {"name": "my_function", "kind": "Function", "file": "src/main.py", "line": 15, "column": 1}
    ],
    "count": 2
  }
}
```

## Features

pylsp supports various Python analysis plugins:

| Plugin | Feature | Install |
|--------|---------|---------|
| pylint | Linting | `pip install pylsp-pylint` |
| mypy | Type checking | `pip install pylsp-mypy` |
| black | Formatting | `pip install python-lsp-black` |
| rope | Refactoring | Included |
| jedi | Completion/navigation | Included |

## Architecture

Like typescript-language-server, pylsp uses JSON-RPC over stdio. This skill:

1. Starts the pylsp server
2. Sends `initialize` request
3. Opens the target file via `textDocument/didOpen`
4. Executes the requested operation
5. Shuts down the server

## Limitations

- Server startup adds latency (~200-300ms)
- Some features work better with a proper Python project structure
- Diagnostics are pushed via notifications (limited support)

## Troubleshooting

### Server not found
```bash
# Verify installation
which pylsp
pip show python-lsp-server
```

### No results for workspace_symbol
- Ensure you have a proper Python project structure
- The server needs time to index the workspace

### Slow responses
- Consider installing jedi for faster completion: `pip install jedi`
- Reduce workspace size or use more specific queries

## Alternative: pyright

For TypeScript-style type checking, you can use pyright instead:

```bash
npm install -g pyright
# Then use lsp/pyright (if implemented) or configure pylsp to use pyright
```
