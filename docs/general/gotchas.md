# Gotchas

Common pitfalls and their solutions.

---

## Build & Installation

### Legacy SQLite CGO Storage Lane

**Problem:** Old instructions can point agents toward `go-libsql`,
`github.com/mattn/go-sqlite3`, `-tags=libsqlite3`, `sqlite-vector`, or
`foxctl-cgo`.

**Solution:** Do not revive that lane. Turso is the canonical SQLite-family
storage path and builds through the normal non-CGO targets:

```bash
make build
make test
```

**Current state:** `tursogo` and `dspy-go` may still pull
`github.com/mattn/go-sqlite3` as a transitive module, but foxctl does not import
it as a storage driver. With `CGO_ENABLED=0`, it resolves through non-CGO files
and the full suite passes. Do not treat the indirect module entry as a reason to
restore `CGO_ENABLED=1`, `-tags=libsqlite3`, `foxctl-cgo`, or sqlite-vector.

---

### Skill Binary Naming

**Problem:** Skill changes not taking effect.

**Cause:** Skill loader looks for `bin` or `bin-cgo`, NOT custom names.

**Solution:**

```bash
# Correct
go build -o ~/.foxctl/skills/my/skill/bin ./skills/my_skill

# Wrong - loader won't find it
go build -o ~/.foxctl/skills/my/skill/my_skill ./skills/my_skill
```

---

### Two-Stage Skill Deploy

**Problem:** Skill changes not reflected after `go build`.

**Cause:** Build creates binary in source dir, not install location.

**Solution:** Build AND install:

```bash
# Option 1: Install all skills
make skills-install

# Option 2: Single skill
CGO_ENABLED=0 go build -o ~/.foxctl/skills/code/symbols/bin ./skills/code_symbols
```

---

## Environment & Configuration

### Repo Context Requires a Fresh Repoindex

**Problem:** `gather_context` returns odd repo-search results, misses route
mounts, action files, or structural neighbors, and provider telemetry shows
`repo_index` / `repo_index_coverage` with zero hits.

**Cause:** The repo graph index may be missing, empty, stale, or built for a
different commit. Local/live providers can still return plausible files, which
can make the result look like a ranking problem when it is actually missing
graph/symbol data.

**Solution:** Check index status before judging search quality:

```bash
foxctl index repo status --workspace /path/to/repo
```

If `nodes_total` is `0`, `index_matches_head` is false for a clean repo, or the
language set is wrong, rebuild for the repo's languages:

```bash
foxctl index repo build --workspace /path/to/repo --go=false --typescript --elixir=false
```

For dirty worktrees, the index records the HEAD plus dirty state. `gather_context`
should still use live-overlay providers for changed/untracked files, but indexed
route/import/symbol closure only works when the repoindex has useful nodes.

### Skills Not Loading .env

**Problem:** API keys not found in skills.

**Cause:** Skills must explicitly load `.env` files.

**Solution:**

```go
import "github.com/joshka0/foxctl/internal/platform/config"

func main() {
    config.LoadDotEnv() // BEFORE os.Getenv()
    apiKey := os.Getenv("VOYAGE_API_KEY")
    // ...
}
```

---

### .env Must Be a Real File (Not a Symlink)

**Problem:** foxctl fails in sandboxed or remote environments.

**Cause:** `~/.foxctl/.env` is a symlink to the repo's `.env` file. When the repo path doesn't exist (sandbox, remote, different machine), the symlink is broken.

**Solution:** Use a real file, not a symlink:

```bash
# Manual sync
make env-sync  # Copies repo .env → ~/.foxctl/.env

# Or with auto-watch (requires: brew install fswatch)
make env-watch       # Start watching
make env-watch-stop  # Stop watching

# Verify it's a real file
ls -la ~/.foxctl/.env
# Should show: -rw------- (not lrwxr-xr-x)
```

The `.env` loader checks these locations in order:
1. `~/.foxctl/.env` (global defaults)
2. `$FOXCTL_HOME/.env` (if set)
3. `$PWD/.env` (project overrides)

---

### Memory Workspace Scoping

**Problem:** Memory queries return empty results.

**Cause:** Queries are scoped by workspace path.

**Solution:** Run from correct workspace or specify explicitly:

```bash
# Run from project directory
cd /path/to/project
foxctl memory search "auth"

# Or specify workspace
foxctl run memory/query --input '{"workspace": "/path/to/project"}'
```

---

### Embedding Dimension Mismatch

**Problem:** Vector search fails or returns wrong results.

**Cause:** Mixing embedding providers with different dimensions.

| Provider | Dimensions |
|----------|------------|
| Voyage AI | 1024 |
| Gemini | 3072 |
| Mistral/Codestral | 1024 |

**Solution:** Use the same provider for storage and query, then rebuild the
affected scope. See [docs/general/embedding-rebuilds.md](embedding-rebuilds.md)
for the exact commands.

---

## Storage & Data

### Memory Path

**Problem:** Memories not persisting.

**Cause:** Using cache path instead of storage path.

**Solution:**

```go
// Correct - persistent storage
store, err := memory.Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)

// Wrong - cache is ephemeral
store, err := memory.Open(ctx, cfg.Paths.Cache, cfg.Paths.CAS)
```

---

### CAS Store Put Signature

**Problem:** Compile error on `casStore.Put()`.

**Cause:** Wrong argument count or types.

**Solution:**

```go
// Correct - 4 arguments, returns CASObject
obj, err := casStore.Put(ctx, bytes.NewReader(data), "application/json", []string{"tag"})
digest := obj.Digest  // "sha256:..."

// Wrong - missing arguments
digest, err := casStore.Put(ctx, data)
```

---

## Sessions

### Session Archives are Gzipped

**Problem:** Can't read session JSONL files.

**Cause:** Archives are compressed as `.jsonl.gz`.

**Solution:**

```go
if strings.HasSuffix(path, ".gz") {
    gzReader, err := gzip.NewReader(file)
    if err != nil {
        return err
    }
    defer gzReader.Close()
    reader = gzReader
}
```

---

### Context Window vs Session Summaries

**Problem:** Session summaries empty or missing.

**Cause:** Summaries are per-window, not per-session.

**Solution:** Summarize each context window:

```bash
# Re-run summarization (handles all windows)
foxctl run session/summarize --input '{"session_id": "..."}'
```

---

## Rooms & Relay

### Room Messages Stored but Not Visible in a tmux Pane

**Problem:** A room message is durably written, but one participant pane does not
show it even though other panes in the same room do.

**Cause:** There are two common failure modes:

1. The live `room loop` is running in the wrong workspace, so it is polling a
   different `board.db` than the room you are inspecting.
2. The room member's `actor_id` does not match the actual tmux pane label, so
   relay matching succeeds logically but delivery targets the wrong tmux pane
   unless `pane_id` is used authoritatively.

**Solution:** Check all three layers before assuming fanout is broken:

```bash
# 1. Verify the live loop points at the intended workspace + room
ps -Ao pid=,command= | rg 'foxctl room loop'
lsof -a -p <loop-pid> -d cwd

# 2. Verify the room membership records include pane_id for tmux members
foxctl room show <room-id> --workspace /path/to/workspace

# 3. Read the pane directly to distinguish relay failure from display/composer state
foxctl mux read <pane-id> --lines 120
```

For tmux-backed room relay, treat `pane_id` as the delivery target when it is
present. `actor_id` is for room semantics and matching; `pane_id` is for the
actual write target.

---

## Security

### TOCTOU File Reading

**Problem:** Potential race condition in file operations.

**Cause:** Gap between validation and read.

**Solution:** Open immediately after validation:

```go
// Correct - open immediately, then validate
path, err := pathValidator.ValidatePath(requested)
if err != nil { return err }

f, err := os.Open(path)  // Open immediately
if err != nil { return err }
defer f.Close()

// Re-validate for symlink escapes
resolved, err := filepath.EvalSymlinks(path)
if _, err := pathValidator.ValidatePath(resolved); err != nil {
    return fmt.Errorf("symlink escape: %w", err)
}

// Read from open descriptor
data, err := io.ReadAll(f)

// Wrong - race window
path, _ := pathValidator.ValidatePath(requested)
data, _ := os.ReadFile(path)  // File could change between validate and read
```

---

## Platform

### Workspace Detection

**Problem:** Wrong workspace in sandboxed execution.

**Cause:** Using `os.Getwd()` directly.

**Solution:** Use platform workspace detection:

```go
import "github.com/joshka0/foxctl/internal/platform/workspace"

ws := workspace.Detect("")  // Handles FOXCTL_WORKSPACE, git root, etc.
```

---

### Absolute Paths in .env

**Problem:** Observability not working.

**Cause:** Tilde (`~`) not expanded in env vars.

**Solution:** Use absolute path:

```bash
# Wrong
FOXCTL_OBS_DIR=~/.foxctl/observability

# Correct
FOXCTL_OBS_DIR=$HOME/.foxctl/observability
```

---

## Claude Max OAuth (Future)

**Problem:** Can't use Claude Max subscription with agent daemons.

**Cause:** LLM providers require API keys, Claude Max uses OAuth.

**Current workaround:** Use OpenRouter:

```bash
export OPENROUTER_API_KEY=sk-or-...
export CLAUDE_MAX_MODEL=claude-haiku-4-5
```

---

## Dependencies

### Telegram Bot API Library is Unmaintained

**Status:** `go-telegram-bot-api/telegram-bot-api` v5.5.1 — last release Dec 2021, last commit Oct 2022.

**Impact:** Missing 3+ years of Telegram Bot API features (v6.0–v9.3). No security patches.

**Recommended migration:** [`go-telegram/bot`](https://github.com/go-telegram/bot) — 1.6k stars, actively maintained, context-based API, zero dependencies, listed on Telegram's official Bot API samples.

**Migration scope:** Not a drop-in replacement. Requires refactoring bot initialization, message/callback handling, and update processing in `internal/interfaces/chatadapter/telegram/`.

---

## Quick Reference

| Gotcha | One-liner Fix |
|--------|---------------|
| Legacy SQLite CGO storage reference | Use default Turso storage path |
| Skill not updating | `make skills-install` |
| API keys missing | Add `config.LoadDotEnv()` |
| Memory empty | Check workspace path |
| Vector mismatch | Use same embedding provider |
| Session unreadable | Handle `.gz` compression |
| File race condition | Open before validate |
| Wrong workspace | Use `workspace.Detect()` |
