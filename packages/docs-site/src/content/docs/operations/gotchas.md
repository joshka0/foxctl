---
title: Gotchas and operating rules
description: Common foxctl pitfalls and the rules that prevent avoidable breakage, drawn from the canonical gotchas source.
---

These pitfalls are drawn from the canonical `docs/general/gotchas.md`. Every
entry below has a verified source — no invented issues.

## Build and Installation

### Legacy SQLite CGO storage lane

Old instructions may reference `go-libsql`, `github.com/mattn/go-sqlite3`, `-tags=libsqlite3`, `sqlite-vector`, or `foxctl-cgo`. **Do not revive that lane.** Turso is the canonical SQLite-family storage path and builds through the normal non-CGO targets:

```bash
make build
make test
```

`tursogo` and `dspy-go` may still appear as transitive modules, but foxctl does not import them as storage drivers. With `CGO_ENABLED=0`, the full suite passes. Do not treat the indirect module entry as a reason to restore CGO builds.

### Skill binary naming

The skill loader looks for a binary named `bin` (or `bin-cgo`), not custom names.

```bash
# Correct — loader will find it
go build -o ~/.foxctl/skills/my/skill/bin ./skills/my_skill

# Wrong — loader won't find it
go build -o ~/.foxctl/skills/my/skill/my_skill ./skills/my_skill
```

### Two-stage skill deploy

Building creates the binary in the source directory, not the install location. You must build *and* install:

```bash
# Install all skills
make skills-install

# Or install a single skill
CGO_ENABLED=0 go build -o ~/.foxctl/skills/code/symbols/bin ./skills/code_symbols
```

## Environment and Configuration

### Repo context requires a fresh repoindex

`gather_context` may return odd results, miss route mounts, or show `repo_index` with zero hits when the repo graph index is missing, empty, stale, or built for a different commit.

Check index status before judging search quality:

```bash
foxctl index repo status --workspace /path/to/repo
```

If `nodes_total` is `0` or `index_matches_head` is `false`, rebuild for the repo's languages:

```bash
foxctl index repo build --workspace /path/to/repo --go=false --typescript
```

For dirty worktrees, the index records HEAD plus dirty state. `gather_context` should still use live-overlay providers for changed files, but indexed route/import/symbol closure only works when the repoindex has useful nodes.

### Skills not loading `.env`

Skills must explicitly load `.env` files. API keys will not be found without this call:

```go
import "github.com/joshka0/foxctl/internal/platform/config"

func main() {
    config.LoadDotEnv() // BEFORE os.Getenv()
    baseURL := os.Getenv("FOXCTL_EMBEDDING_BASE_URL")
}
```

### `.env` must be a real file, not a symlink

If `~/.foxctl/.env` is a symlink to the repo's `.env` file, it breaks in sandboxed or remote environments where the repo path doesn't exist.

```bash
# Copy instead of symlinking
make env-sync  # Copies repo .env → ~/.foxctl/.env

# Verify it's a real file
ls -la ~/.foxctl/.env
# Should show: -rw------- (not lrwxr-xr-x)
```

The `.env` loader checks these locations in order:

1. `~/.foxctl/.env` (global defaults)
2. `$FOXCTL_HOME/.env` (if set)
3. `$PWD/.env` (project overrides)

### Memory workspace scoping

Memory queries return empty results when run from the wrong workspace. Queries are scoped by workspace path:

```bash
# Run from project directory
cd /path/to/project
foxctl memory search "auth"

# Or specify workspace explicitly
foxctl run memory/query --input '{"workspace": "/path/to/project"}'
```

### Embedding dimension mismatch

Mixing embedding providers with different vector dimensions causes search failures:

| Provider | Dimensions |
|----------|------------|
| Qwen3 Embedding 8B | 4096 |
| Qwen3 Embedding 4B | 2560 |
| Qwen3 Embedding 0.6B | 1024 |
| Gemini | 3072 |

Use the same provider for storage and queries. When switching providers, rebuild the affected vector store.

### Absolute paths in `.env`

Tilde (`~`) is not expanded in environment variables:

```bash
# Wrong
FOXCTL_OBS_DIR=~/.foxctl/observability

# Correct
FOXCTL_OBS_DIR=$HOME/.foxctl/observability
```

## Storage and Data

### Memory path

Memories not persisting usually means using the cache path instead of the storage path:

```go
// Correct — persistent storage
store, err := memory.Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)

// Wrong — cache is ephemeral
store, err := memory.Open(ctx, cfg.Paths.Cache, cfg.Paths.CAS)
```

### CAS store `Put` signature

Compile errors on `casStore.Put()` are caused by wrong argument count or types:

```go
// Correct — 4 arguments, returns CASObject
obj, err := casStore.Put(ctx, bytes.NewReader(data), "application/json", []string{"tag"})
digest := obj.Digest  // "sha256:..."

// Wrong — missing arguments
digest, err := casStore.Put(ctx, data)
```

## Sessions

### Session archives are gzipped

Session JSONL files are compressed as `.jsonl.gz`. Attempting to read them as plain text will fail:

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

### Context window vs. session summaries

Session summaries are per-window, not per-session. If summaries appear empty or missing, re-run summarization for all windows:

```bash
foxctl run session/summarize --input '{"session_id": "..."}'
```

## Rooms and Relay

### Room messages stored but not visible in tmux

A message can be durably written while one participant pane does not show it. Two common failure modes:

1. The live `room loop` is running in the wrong workspace, polling a different `board.db`.
2. The room member's `actor_id` does not match the actual tmux pane label.

Check all three layers:

```bash
# 1. Verify the live loop workspace
ps -Ao pid=,command= | rg 'foxctl room loop'
lsof -a -p <loop-pid> -d cwd

# 2. Verify room membership includes pane_id
foxctl room show <room-id> --workspace /path/to/workspace

# 3. Read the pane directly
foxctl mux read <pane-id> --lines 120
```

For tmux-backed room relay, `pane_id` is the authoritative delivery target. `actor_id` is for room semantics and matching.

## Security

### TOCTOU file reading

A potential race condition exists between path validation and file reading. Open the file immediately after validation:

```go
// Correct — open immediately, then validate
path, err := pathValidator.ValidatePath(requested)
if err != nil { return err }

f, err := os.Open(path)
if err != nil { return err }
defer f.Close()

// Re-validate for symlink escapes
resolved, err := filepath.EvalSymlinks(path)
if _, err := pathValidator.ValidatePath(resolved); err != nil {
    return fmt.Errorf("symlink escape: %w", err)
}

data, err := io.ReadAll(f)

// Wrong — race window between validate and read
path, _ := pathValidator.ValidatePath(requested)
data, _ := os.ReadFile(path)
```

## Platform

### Workspace detection

Using `os.Getwd()` directly returns the wrong workspace in sandboxed execution. Use platform workspace detection instead:

```go
import "github.com/joshka0/foxctl/internal/platform/workspace"

ws := workspace.Detect("")  // Handles FOXCTL_WORKSPACE, git root, etc.
```

## Operating Rules

These rules prevent avoidable breakage in daily foxctl work:

| Rule | Why it matters |
|------|----------------|
| Prefer `./bin/foxctl` in this checkout when PATH is ambiguous | Avoids bundled-wrapper command gaps |
| Preserve JSON envelope shape | Hooks, GUIs, and golden tests depend on it |
| Keep WASI skill network policy at `network: "none"` | Maintains Core v1 isolation |
| Use CAS for large outputs (>64 KB) | Prevents bloated envelopes and memory pressure |
| Use structured shell for noisy read-only retrieval | Keeps agent context compact |
| Do not route behavior with keyword heuristics | Avoids brittle hidden policy |
| Run `make check-doc-links` after markdown changes | CI enforces docs link hygiene |
| Never change `meta.*` envelope fields without spec updates | Downstream tooling relies on stable shape |
