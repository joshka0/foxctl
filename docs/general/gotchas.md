# Gotchas

Common pitfalls and their solutions.

---

## Build & Installation

### CGO Build Errors

**Problem:** Duplicate SQLite symbols when building with CGO.

```
duplicate symbol '_sqlite3_...' in:
    go-libsql/...
    go-sqlite3/...
```

**Cause:** Both `go-libsql` (Turso) and `go-sqlite3` embed SQLite.

**Solution:** Always use the Makefile target:

```bash
# Correct
make build-cgo

# Wrong
CGO_ENABLED=1 go build ./...
```

The Makefile uses `-tags=libsqlite3` to use system SQLite.

---

### Skill Binary Naming

**Problem:** Skill changes not taking effect.

**Cause:** Skill loader looks for `bin` or `bin-cgo`, NOT custom names.

**Solution:**

```bash
# Correct
go build -o ~/.agentctl/skills/my/skill/bin ./skills/my_skill

# Wrong - loader won't find it
go build -o ~/.agentctl/skills/my/skill/my_skill ./skills/my_skill
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
CGO_ENABLED=0 go build -o ~/.agentctl/skills/code/symbols/bin ./skills/code_symbols
```

---

## Environment & Configuration

### Skills Not Loading .env

**Problem:** API keys not found in skills.

**Cause:** Skills must explicitly load `.env` files.

**Solution:**

```go
import "github.com/jkatigb/agentctl/internal/platform/config"

func main() {
    config.LoadDotEnv() // BEFORE os.Getenv()
    apiKey := os.Getenv("VOYAGE_API_KEY")
    // ...
}
```

---

### .env Must Be a Real File (Not a Symlink)

**Problem:** agentctl fails in sandboxed or remote environments.

**Cause:** `~/.agentctl/.env` is a symlink to the repo's `.env` file. When the repo path doesn't exist (sandbox, remote, different machine), the symlink is broken.

**Solution:** Use a real file, not a symlink:

```bash
# If you have a symlink, replace it
rm ~/.agentctl/.env
cp /path/to/your/.env ~/.agentctl/.env

# Verify it's a real file
ls -la ~/.agentctl/.env
# Should show: -rw------- (not lrwxr-xr-x)
```

The `.env` loader checks these locations in order:
1. `~/.agentctl/.env` (global defaults)
2. `$AGENTCTL_HOME/.env` (if set)
3. `$PWD/.env` (project overrides)

---

### Memory Workspace Scoping

**Problem:** Memory queries return empty results.

**Cause:** Queries are scoped by workspace path.

**Solution:** Run from correct workspace or specify explicitly:

```bash
# Run from project directory
cd /path/to/project
agentctl memory search "auth"

# Or specify workspace
agentctl run memory/query --input '{"workspace": "/path/to/project"}'
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

**Solution:** Use same provider for storage and query.

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
agentctl run session/summarize --input '{"session_id": "..."}'
```

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
import "github.com/jkatigb/agentctl/internal/platform/workspace"

ws := workspace.Detect("")  // Handles AGENTCTL_WORKSPACE, git root, etc.
```

---

### Absolute Paths in .env

**Problem:** Observability not working.

**Cause:** Tilde (`~`) not expanded in env vars.

**Solution:** Use absolute path:

```bash
# Wrong
AGENTCTL_OBS_DIR=~/.agentctl/observability

# Correct
AGENTCTL_OBS_DIR=$HOME/.agentctl/observability
```

---

## Claude Max OAuth (Future)

**Problem:** Can't use Claude Max subscription with agent daemons.

**Cause:** dspy-go requires API keys, Claude Max uses OAuth.

**Current workaround:** Use OpenRouter:

```bash
export OPENROUTER_API_KEY=sk-or-...
export CLAUDE_MAX_MODEL=claude-haiku-4-5
```

---

## Quick Reference

| Gotcha | One-liner Fix |
|--------|---------------|
| CGO build error | `make build-cgo` |
| Skill not updating | `make skills-install` |
| API keys missing | Add `config.LoadDotEnv()` |
| Memory empty | Check workspace path |
| Vector mismatch | Use same embedding provider |
| Session unreadable | Handle `.gz` compression |
| File race condition | Open before validate |
| Wrong workspace | Use `workspace.Detect()` |
