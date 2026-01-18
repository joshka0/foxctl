# agentctl Integration for Codex

Codex does not support Claude Code-style hook events (PreToolUse/PostToolUse).
Apply these rules as self-enforced guidelines.

## Tool Redirections (Self-Enforce)

### File Editing → fs/apply_edit
**NEVER use sed, awk, or manual file writes.**

```bash
# Get exact text first
agentctl run code/context_grep --input '{"mode": "line", "file_path": "path/to/file.go", "line_start": 10, "line_end": 30}'

# Preview change
agentctl run fs/apply_edit --input '{
  "path": "path/to/file.go",
  "edits": [{"search": "exact old text", "replace": "new text"}],
  "dry_run": true
}'

# Apply (set dry_run: false)
agentctl run fs/apply_edit --input '{
  "path": "path/to/file.go",
  "edits": [{"search": "exact old text", "replace": "new text"}],
  "dry_run": false
}'
```

### Code Search → code/smart_search or code/semantic_search
**NEVER use grep/rg/find for code exploration.**

```bash
# Conceptual search (finds code by meaning)
agentctl run code/semantic_search --input '{"query": "authentication middleware", "scope": ["symbols"], "limit": 10}'

# Smart search (combines search + snippet extraction)
agentctl run code/smart_search --input '{"question": "how does error handling work"}'

# Pattern search with context (full function bodies)
agentctl run code/context_ripgrep --input '{"pattern": "func.*Auth", "path": ".", "max_blocks": 10}'
```

### File Reading → code/context_grep or fs/read
**For large files, read specific sections, not entire files.**

```bash
# Read specific lines
agentctl run code/context_grep --input '{"mode": "line", "file_path": "path/to/file.go", "line_start": 50, "line_end": 100}'

# Read full file (small files only)
agentctl run fs/read --input '{"path": "path/to/file.go"}'
```

### Web Search → web/search
**NEVER use raw web fetching without curation.**

```bash
# Basic search (uses Exa/Tavily)
agentctl run web/search --input '{"query": "React hooks best practices", "max_results": 10}'

# Search with content extraction (recommended)
agentctl run web/search --input '{
  "query": "OpenAI API authentication",
  "extract": true,
  "extract_limit": 3
}'
```

### Web Fetch → web/extract
**Use for extracting content with optional query filtering.**

```bash
# Extract content from URL
agentctl run web/extract --input '{"urls": ["https://docs.example.com/api"]}'

# Extract with query filtering
agentctl run web/extract --input '{
  "urls": ["https://docs.example.com/api"],
  "query": "authentication",
  "max_content_kb": 50
}'
```

### Tests → make test-*
**NEVER use `go test` directly in this repo.**

```bash
make test-short      # Quick tests
make test-cgo-short  # CGO tests
make test            # Full tests
make test-race       # Race detection
```

### GitHub CI → agentctl ci
**NEVER use `gh pr/api/checks` directly.**

```bash
agentctl ci checks --pr 123       # Check CI status
agentctl ci prcomments --pr 123   # Get PR comments
```

---

## Skill Packs (Pick One)

For common workflows, use skill packs instead of individual skills:

| Pack | Use Case |
|------|----------|
| `$agentctl-all` | Combined entrypoint (when unsure) |
| `$agentctl-core` | File ops + fast search |
| `$agentctl-code` | Code analysis + semantic search |
| `$agentctl-dev` | Tests + CI + verification |
| `$agentctl-orchestrate` | Tasks + sessions + inbox |

---

## Self-Enforced Behaviors

### 1. Task Guard (Before Edits)
- Ensure explicit task exists before editing files
- Create task or ask for title if none exists
- Keep diffs small and reviewable

### 2. Structure-First Reading
- Find symbols/entrypoints before reading code
- Read only relevant sections, not entire files
- Use `code/context_grep` with line ranges for large files

### 3. Semantic Search for Concepts
- Literal searches: use `code/context_ripgrep`
- Conceptual searches: use `code/semantic_search`

### 4. Security Scanner (Sensitive Changes)
When touching auth, crypto, path validation, serialization:
```bash
agentctl run code/security --input '{"path": "internal/auth/"}'
```

### 5. LSP + Tests After Edits
- Run typecheck/lint first, fix errors
- Run narrowest tests, then broaden
- Acknowledge failures, fix or create follow-up task

### 6. Task Sync + Stop Guard
- Maintain explicit checklist
- Keep exactly one item "in progress"
- Do not conclude with unfinished tasks

### 7. Memory Capture
When user says "remember", "gotcha", "decision", or you discover a pitfall:
```bash
agentctl memory put --name "gotcha-name" --type "gotcha" --summary "Short description"
```

---

## Quick Reference

| Task | Command |
|------|---------|
| Search code | `agentctl run code/smart_search --input '{"question": "..."}'` |
| Read lines | `agentctl run code/context_grep --input '{"mode": "line", "file_path": "...", "line_start": N, "line_end": M}'` |
| Edit file | `agentctl run fs/apply_edit --input '{"path": "...", "edits": [...], "dry_run": true}'` |
| Web search | `agentctl run web/search --input '{"query": "...", "extract": true}'` |
| Web fetch | `agentctl run web/extract --input '{"urls": [...], "query": "..."}'` |
| Run tests | `make test-short` |
| CI status | `agentctl ci checks --pr <num>` |
| Add task | `agentctl todo add --title "..." --description "..."` |
| Add memory | `agentctl memory put --name "..." --type "gotcha" --summary "..."` |

---

*Repo-local AGENTS.md takes precedence over these global rules.*
