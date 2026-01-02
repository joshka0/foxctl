---
name: agentctl Smart Edit
description: Symbol-aware code editing with dry-run diff preview. Edit by function name, line range, or scoped search/replace.
---

# Smart Code Editing

Precise, AST-aware edits to code files. Preferred over raw text replace.

## Edit Types

```bash
# Replace entire function by name
agentctl run code/smart_write --input '{
  "path": "main.go",
  "dry_run": true,
  "edits": [{"type": "symbol", "symbol": "HandleRequest", "new_code": "func HandleRequest(...) { ... }"}]
}'

# Replace lines 45-60
agentctl run code/smart_write --input '{
  "path": "config.go",
  "edits": [{"type": "lines", "start_line": 45, "end_line": 60, "new_code": "// new code"}]
}'

# Search/replace within a function only
agentctl run code/smart_write --input '{
  "path": "main.go",
  "edits": [{"type": "replace", "search": "oldVar", "replace": "newVar", "within_symbol": "main", "all": true}]
}'
```

## Options

| Option | Default | Description |
|--------|---------|-------------|
| `dry_run` | false | Preview diff without writing |
| `context_lines` | 3 | Lines of context in diff |

## Supported Languages

Go, Python, JavaScript, TypeScript, GDScript

## When to Use

- **smart_write**: Edit code by symbol name or scoped replace (preferred)
- **text/replace**: Bulk regex replace across many files
- **html/edit**: DOM-aware HTML editing with CSS selectors

Full docs: `~/repos/personal/agentctl/configs/skills/agentctl-smart-edit/Skill.md`
