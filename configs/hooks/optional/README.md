# Optional Hooks

These hooks are **disabled by default** because they enhance tools that are blocked in foxctl mode.

## Hooks in this Directory

| Hook | Purpose | Tools Enhanced |
|------|---------|----------------|
| `semantic-search.sh` | Vector search on file operations | Grep, Glob |
| `smart-find.sh` | Enhanced file finding | Glob |
| `smart-grep.sh` | Enhanced grep with context | Grep |
| `smart-read.sh` | Enhanced file reading | Read |
| `read-context-suggestions.sh` | Suggest related files after reading | Read |

## When to Enable

Enable these hooks if you're **not** using foxctl mode and want enhanced tool behavior.

## How to Enable

Add the hooks to your `~/.claude/settings.json`:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Grep|Glob",
        "hooks": ["~/.claude/hooks/foxctl/optional/semantic-search.sh"]
      },
      {
        "matcher": "Glob",
        "hooks": ["~/.claude/hooks/foxctl/optional/smart-find.sh"]
      },
      {
        "matcher": "Grep",
        "hooks": ["~/.claude/hooks/foxctl/optional/smart-grep.sh"]
      },
      {
        "matcher": "Read",
        "hooks": ["~/.claude/hooks/foxctl/optional/smart-read.sh"]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Read",
        "hooks": ["~/.claude/hooks/foxctl/optional/read-context-suggestions.sh"]
      }
    ]
  }
}
```

Or run install.sh with optional hooks enabled:

```bash
./install.sh --with-optional-hooks
```
