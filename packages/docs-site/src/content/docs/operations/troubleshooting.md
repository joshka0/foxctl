---
title: Troubleshooting
description: Diagnose and resolve common foxctl failures — build errors, skill issues, storage problems, agent stalls, and platform-specific quirks.
---

Troubleshooting pages should start from symptoms and end with verification.
Avoid broad speculation when a concrete command can show the state.

## First Checks

Before diving into specific issues, run these commands to gather context:

```bash
# Verify foxctl is installed and reachable
foxctl version

# Check Go version (for building from source)
go version

# Show current configuration
foxctl config show
```

Capture stderr for debugging — structured logs (zerolog JSON) are written to stderr, while Protocol v1 envelopes go to stdout:

```bash
foxctl run fs/ls --input '{"path":"."}' 2>debug.log
```

## Error Codes

All foxctl errors use structured error codes in the envelope's `error.code` field:

| Code | Meaning | Common Causes |
|------|---------|---------------|
| `EARG` | Invalid argument | Missing or malformed parameters |
| `ENOTFOUND` | Resource not found | Skill, file, or digest doesn't exist |
| `ETIMEOUT` | Operation timed out | Network issues, long-running skill |
| `ERUNTIME` | Runtime error | Skill crashed or panicked |
| `EENVELOPE` | Envelope validation failed | Malformed JSON, missing fields |
| `EPARSE` | Parse error | Invalid input format |
| `EPOLICY` | Policy violation | Path traversal, network access denied |
| `EIO` | I/O error | File access failure, CAS corruption |
| `EAUTH` | Authentication failed | Invalid credentials |
| `ERATELIMIT` | Rate limit exceeded | Too many API calls |
| `ECANCELED` | Operation canceled | User cancellation, context timeout |

## Installation Issues

### `foxctl: command not found`

The binary is not in your `PATH` or is not installed.

```bash
# If built from source, add to PATH
export PATH="$PATH:/path/to/foxctl/bin"

# Or install to a standard location
sudo cp ./foxctl /usr/local/bin/

# Verify
which foxctl
foxctl version
```

If `foxctl` on PATH reports `Command 'run' not available in bundled mode`, you are running a wrapper from another install. Run `./bin/foxctl` from the repo checkout instead, or rebuild with `make build`.

### Permission denied

```bash
chmod +x ./foxctl
```

### macOS: "foxctl is damaged and can't be opened"

Gatekeeper is blocking the binary.

```bash
xattr -d com.apple.quarantine ./foxctl
```

Or build from source to avoid the issue entirely:

```bash
make build
```

## Build Issues

### `go: cannot find main module`

You are not in the repository root:

```bash
cd /path/to/foxctl
make build
```

### `undefined: some_package.SomeFunc`

Missing or outdated dependencies:

```bash
go mod download
go mod tidy
make build
```

### Build fails with CGO errors

foxctl requires pure Go builds. Always set `CGO_ENABLED=0`:

```bash
CGO_ENABLED=0 make build
```

Do not revive the legacy CGO/SQLite storage lane (no `-tags=libsqlite3`, no `foxctl-cgo`, no `sqlite-vector`). Turso is the canonical SQLite-family storage path.

### `golangci-lint: command not found`

Install the linter:

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

### Tests fail with `package not found`

```bash
go mod download
go test ./...
```

## Skill Execution Issues

### `ENOTFOUND: skill not found`

The skill is not installed or not in the skill path.

```bash
# List installed skills
foxctl skills list

# Build and install all skills
make skills-install

# Build a single skill
CGO_ENABLED=0 go build -o ~/.foxctl/skills/code/symbols/bin ./skills/code_symbols
```

**Important:** The skill loader looks for a binary named `bin` (or `bin-cgo`), not a custom name. Always use the correct output path:

```bash
# Correct — loader will find it
go build -o ~/.foxctl/skills/my/skill/bin ./skills/my_skill

# Wrong — loader won't find it
go build -o ~/.foxctl/skills/my/skill/my_skill ./skills/my_skill
```

Skills must also be *installed* after building — the build step creates the binary in the source directory, but the loader searches `~/.foxctl/skills/`:

```bash
# Install all skills
make skills-install
```

### `EPOLICY: path outside workspace`

You attempted to access a file outside the workspace boundary:

```bash
# Check current workspace
foxctl config show | grep workspace

# Use relative paths within workspace
foxctl run fs/read --input '{"path":"./src/main.go"}'
```

### `ERUNTIME: skill execution failed`

The skill crashed or encountered an internal error:

```bash
# Check stderr for details
foxctl run <skill> 2>error.log
cat error.log

# Enable debug logging
export FOXCTL_LOG_LEVEL=debug
foxctl run <skill>

# Check skill manifest for requirements
foxctl skills describe <skill>
```

### WASI skill fails to run

WASI skills run in a sandboxed environment with no network access (`network: "none"`). If a skill needs network access, use an exec skill instead.

Rebuild WASI skills from source:

```bash
make skills-build
```

### `ETIMEOUT: skill execution timed out`

The default timeout is 5 minutes. For long-running operations, use the jobs system:

```bash
foxctl jobs submit <skill> --args '...'
foxctl jobs tail <job-id>
```

### API keys not found in skills

Skills must explicitly load `.env` files. Add `config.LoadDotEnv()` before any `os.Getenv()` calls:

```go
import "github.com/joshka0/foxctl/internal/platform/config"

func main() {
    config.LoadDotEnv() // BEFORE os.Getenv()
    apiKey := os.Getenv("FOXCTL_EMBEDDING_API_KEY")
}
```

## Job Issues

### Job stuck in `queued` state

```bash
# Check job queue
foxctl jobs list --status queued

# Submit a simple test job to verify the queue is processing
foxctl jobs submit fs/ls --path .
```

### Job stuck in `running` after a crash

Crash recovery may not have triggered:

```bash
# Cancel the stuck job
foxctl jobs cancel <job-id>

# Last resort: direct database update
sqlite3 ~/.foxctl/foxctl.db "UPDATE jobs SET status='error' WHERE id='<job-id>';"
```

### Job result not found

```bash
# Check job status
foxctl jobs get <job-id>

# If it errored, check stderr
foxctl jobs get <job-id> --show-stderr

# Check if the CAS artifact still exists
foxctl cas list | grep <digest>
```

## Memory and Search Issues

### Memory search returns no results

Memory queries are scoped by workspace path. Run from the correct project directory or specify the workspace explicitly:

```bash
# Run from project directory
cd /path/to/project
foxctl memory search "auth"

# Or specify workspace in the skill input
foxctl run memory/query --input '{"workspace": "/path/to/project"}'
```

### Semantic search returns poor results

The repo graph index may be missing, empty, or stale. Check index status before judging search quality:

```bash
foxctl index repo status --workspace /path/to/repo
```

If `nodes_total` is `0` or `index_matches_head` is `false`, rebuild the index for the repo's languages:

```bash
foxctl index repo build --workspace /path/to/repo --go=false --typescript
```

### Embedding dimension mismatch

Mixing embedding providers with different vector dimensions causes search failures. Use the same provider for storage and queries:

| Provider | Dimensions |
|----------|------------|
| Qwen3 Embedding 8B | 4096 |
| Qwen3 Embedding 4B | 2560 |
| Qwen3 Embedding 0.6B | 1024 |
| Gemini | 3072 |

If you switch providers, rebuild the affected vector store.

## Storage Issues

### CAS digest verification failed

A CAS artifact may be corrupted:

```bash
# Delete the corrupted artifact
foxctl cas delete <digest> --force

# Re-run the operation to regenerate it
foxctl run <skill> --cache off
```

### CAS storage growing too large

```bash
# Check total CAS usage
du -sh ~/.foxctl/cas/

# List unpinned artifacts
foxctl cas list --unpinned

# Dry-run garbage collection
foxctl cas gc --older-than 168h --dry-run

# Run garbage collection
foxctl cas gc --older-than 168h --confirm

# Pin important artifacts before GC to protect them
foxctl cas pin <digest>
```

### Database locking errors

SQLite has limited concurrency. If you see locking errors:

```bash
# Remove stale lock files
rm -f ~/.foxctl/foxctl.db-shm ~/.foxctl/foxctl.db-wal

# If persistent, backup and vacuum
cp ~/.foxctl/foxctl.db ~/.foxctl/foxctl.db.bak
sqlite3 ~/.foxctl/foxctl.db "VACUUM;"
```

### Database corruption

```bash
# Check integrity
sqlite3 ~/.foxctl/foxctl.db "PRAGMA integrity_check;"

# Restore from backup if available
cp ~/.foxctl/foxctl.db.bak ~/.foxctl/foxctl.db

# Or rebuild from scratch (loses history)
rm ~/.foxctl/foxctl.db
foxctl config init
```

## Agent Issues

### Agent not responding

```bash
# Check agent status
foxctl agent info <agent-id>

# Stream live events
foxctl agent watch <agent-id>

# Send a follow-up and wait
foxctl agent ask <agent-id> --question "What is your current state?" --wait --timeout 120s
```

### Room messages stored but not visible in tmux

Check all three layers — workspace alignment, room membership, and pane delivery:

```bash
# 1. Verify the live loop is in the correct workspace
ps -Ao pid=,command= | rg 'foxctl room loop'
lsof -a -p <loop-pid> -d cwd

# 2. Check room membership includes the pane
foxctl room show <room-id> --workspace /path/to/workspace

# 3. Read the pane directly
foxctl mux read <pane-id> --lines 120
```

For tmux-backed room relay, `pane_id` is the actual delivery target. `actor_id` is for room semantics and matching.

## Platform-Specific Issues

### Linux: `setrlimit: operation not permitted`

foxctl sets resource limits for safety. Run as a regular user (not root):

```bash
# Check current ulimits
ulimit -a
```

### Windows: Path separator errors

Use forward slashes in foxctl inputs, even on Windows:

```bash
foxctl run fs/read --input '{"path":"./src/main.go"}'
```

### `.env` file not loading in sandboxed environments

If `~/.foxctl/.env` is a symlink to the repo's `.env` and the repo path doesn't exist in the sandbox, the symlink breaks. Use a real file:

```bash
make env-sync  # Copies repo .env → ~/.foxctl/.env

# Verify it's a real file (not a symlink)
ls -la ~/.foxctl/.env
```

The `.env` loader checks these locations in order:

1. `~/.foxctl/.env` (global defaults)
2. `$FOXCTL_HOME/.env` (if set)
3. `$PWD/.env` (project overrides)

## Debugging Techniques

### Enable debug logging

```bash
export FOXCTL_LOG_LEVEL=debug
foxctl run <skill> 2>debug.log
```

### Inspect envelopes

```bash
# Pretty-print the JSON envelope
foxctl run fs/ls --input '{"path":"."}' | jq .

# Check specific fields
foxctl run fs/ls --input '{"path":"."}' | jq '.meta'
```

### Test with minimal skills

```bash
# These exercise core functionality without skill-specific logic
foxctl run fs/ls --input '{"path":"."}'

# If these fail, the issue is in core — not in a specific skill
```

### Reset to clean state

```bash
# Backup first
cp -r ~/.foxctl ~/.foxctl.bak

# Remove all state
rm -rf ~/.foxctl/

# Reinitialize
foxctl config init
foxctl skills list  # Should be empty
```

## Common Pitfalls

| Pitfall | Solution |
|---------|----------|
| Running from wrong directory | Run from repository root or specify workspace paths |
| Skills not found after build | Run `make skills-install` to install to `~/.foxctl/skills/` |
| Stale cache returning old results | Use `--cache off` to bypass cache |
| Secrets in command history | Use environment variables, not CLI arguments |
| Expecting network from WASI skills | WASI enforces `network: "none"` — use exec skills for network access |
| Wrong foxctl binary on PATH | Use `./bin/foxctl` from the repo checkout |
| Observability not working | Use absolute paths in `FOXCTL_OBS_DIR`, not `~` shorthand |

## Getting Help

Before reporting an issue:

1. Check this troubleshooting guide and [Gotchas](/operations/gotchas)
2. Enable debug logging: `export FOXCTL_LOG_LEVEL=debug`
3. Try a minimal reproduction — simplify to the smallest failing case
4. Check if it worked before — what changed?

When opening an issue, include:

```bash
# System info
uname -a
go version
foxctl version

# Reproduction with debug output
foxctl run <skill> --input '{}' 2>error.log
cat error.log
```
