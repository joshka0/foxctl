# lsp/tsserver - TypeScript Language Server Skill

TypeScript/JavaScript language server operations via typescript-language-server.

## Prerequisites

```bash
npm install -g typescript typescript-language-server
```

## Operations

| Operation | Description | Required Params |
|-----------|-------------|-----------------|
| `symbols` | List symbols in a file | `file` |
| `definition` | Go to definition | `file`, `line`, `column` |
| `references` | Find all references | `file`, `line`, `column` |
| `workspace_symbol` | Search workspace | `query` |

## Usage

Examples assume the skill is installed. JSON goes through `--input`; use
`--input-file -` when piping raw JSON, or `foxctl skills run` for direct
parameter flags.

```bash
# List symbols in a file
foxctl run lsp/tsserver --input '{"operation": "symbols", "file": "src/index.ts"}'

# Find references
foxctl run lsp/tsserver --input '{"operation": "references", "file": "src/index.ts", "line": 10, "column": 5}'

# Workspace symbol search
foxctl run lsp/tsserver --input '{"operation": "workspace_symbol", "query": "Handler"}'
```

## Architecture

Unlike gopls which has a CLI mode, typescript-language-server uses JSON-RPC over stdio.
This skill manages the LSP lifecycle:

1. Starts the language server
2. Sends `initialize` request
3. Opens the target file via `textDocument/didOpen`
4. Executes the requested operation
5. Shuts down the server

## Comparison with gopls

| Feature | gopls | typescript-language-server |
|---------|-------|---------------------------|
| Protocol | CLI commands | JSON-RPC over stdio |
| Startup | Per-command | Managed lifecycle |
| File handling | Automatic | Requires `didOpen` |
| Output | Text parsing | JSON responses |

## Limitations

- Server startup adds latency (~100-200ms)
- Requires `package.json` or `tsconfig.json` for full functionality
- Some features may require the file to be saved to disk

## Troubleshooting

### Server not found
```bash
# Verify installation
which typescript-language-server
npm list -g typescript-language-server
```

### No results
- Ensure the file exists and is valid TypeScript/JavaScript
- Check that `tsconfig.json` or `jsconfig.json` exists in the workspace
