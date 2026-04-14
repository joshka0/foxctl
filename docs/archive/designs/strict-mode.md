# Strict Mode Design

## Tool Mappings

| Default Tool | Redirects To | Mode |
|--------------|--------------|------|
| Edit/Write/MultiEdit | `fs/apply_edit` | Dry-run preview |
| Grep | `code/smart_search` | Semantic + vector |
| Glob | `code/semantic_search` | Meaning-based discovery |
| WebFetch | `web/extract` | MCP extraction |
| Read | `code/context_grep` | Expand to function boundaries |

## `code/context_grep` Enhancement

Rename `context_ripgrep` → `context_grep` with three modes:

```go
type Input struct {
    // Existing - regex mode
    Pattern string `json:"pattern"`
    Path    string `json:"path"`
    
    // New: AST mode
    ASTPattern string `json:"ast_pattern,omitempty"`
    Language   string `json:"language,omitempty"`
    
    // New: Line expansion mode (for Read redirect)
    FilePath   string `json:"file_path,omitempty"`
    LineStart  int    `json:"line_start,omitempty"`
    LineEnd    int    `json:"line_end,omitempty"`
    ExpandTo   string `json:"expand_to,omitempty"`  // "function", "block", "class"
}
```

## Implementation Phases

### Phase 1: Shell Hooks MVP
- `strict-mode-detect.sh` (UserPromptSubmit) - toggle via `/strict on|off`
- `strict-mode-enforce.sh` (PreToolUse) - intercept and redirect
- Basic Edit → fs/apply_edit wiring

### Phase 2: Enhanced context_grep
- Rename context_ripgrep → context_grep
- Add ast-grep integration
- Add line-range expansion mode
- Wire up Read → context_grep in strict mode

### Phase 3: Full Mappings
- Grep → code/smart_search
- Glob → code/semantic_search
- WebFetch → web/extract
- Rules configuration file

### Phase 4: Cross-tool (Codex/Cursor)
- MCP proxy pattern for tools without hooks

## Architecture

```
UserPromptSubmit ─────────────────────────────────────────────────────
│
├── /strict on|off|status  →  strict-mode-detect.sh
│                               │
│                               ▼
│                         State File: ~/.foxctl/cache/session-modes/
│                               strict-{session_hash}.json
│
PreToolUse ───────────────────────────────────────────────────────────
│
└── Edit|Write|Grep|Glob|WebFetch|Read  →  strict-mode-enforce.sh
                                              │
                                              ├── Check state
                                              ├── Match tool → skill
                                              ├── Execute skill
                                              └── Return block + context
```
