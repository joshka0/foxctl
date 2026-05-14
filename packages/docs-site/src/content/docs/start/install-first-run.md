---
title: Install and first run
description: Install foxctl, verify the local environment, and run the first useful commands.
---

This guide gets you from a fresh checkout to running your first foxctl commands. It covers install paths, prerequisites, environment variables, and first-run verification.

## Prerequisites

### Required

| Dependency | Version | Notes |
|---|---|---|
| `bash` | any | Shell for scripts and hooks |
| `git` | any | Clone and update the repo |
| `make` | any | Build automation |
| `jq` | any | JSON processing in scripts |
| Go | `1.26.1+` | Core language; non-CGO builds by default |

### Recommended for full setup

| Dependency | Needed for |
|---|---|
| Bun | `packages/gui-agent` web GUI and OpenCode plugin install flows |

On macOS, the full path usually means Homebrew `go`, `jq`, and `bun`.

On Debian/Ubuntu, the full path usually means `git`, `make`, `jq`, `curl`, and a current Go toolchain.

## Install paths

### Interactive install (recommended)

Run the installer from the repo root:

```bash
./install.sh
```

This flow:

- Verifies or installs core local dependencies
- Optionally installs Bun for GUI and OpenCode plugin workflows
- Builds the CLI and skills
- Wires up provider integrations via `scripts/init.sh`

### Non-interactive install

```bash
./install.sh --yes
```

Useful flags:

```bash
./install.sh --yes --skip-bun
./install.sh --yes --skip-provider-setup
./install.sh --yes --repo-dir "$HOME/src/foxctl"
```

| Flag | Effect |
|---|---|
| `--skip-bun` | Skip Bun installation and `bun install` |
| `--skip-provider-setup` | Skip `scripts/init.sh` provider bootstrap |
| `--repo-dir <path>` | Set checkout directory when not run from a clone |

### Manual / development setup

If you already have the toolchain installed and want the repo-native flow:

```bash
make init
```

`make init` runs `build`, `skills-install-all`, and `./scripts/init.sh`.

If you only want the CLI path:

```bash
make build
make skills-install
./scripts/init.sh
```

If `foxctl` on `PATH` reports that a command is unavailable in bundled mode, run `./bin/foxctl ...` from this checkout.

## Environment variables

`foxctl` loads env files in this order:

1. `~/.foxctl/.env`
2. The git-root `.env`
3. The current working directory `.env`

### Common variables

```bash
# Embeddings / semantic retrieval
FOXCTL_EMBEDDING_PROVIDER=openai_compat
FOXCTL_EMBEDDING_MODEL=text-embedding-qwen3-embedding-8b
FOXCTL_EMBEDDING_BASE_URL=http://127.0.0.1:1234/v1
FOXCTL_EMBEDDING_API_KEY=...

# Optional LLM-backed flows
OPENROUTER_API_KEY=...
ANTHROPIC_API_KEY=...

# Optional remote / cross-workspace storage
TURSO_DATABASE_URL=...
TURSO_AUTH_TOKEN=...
FOXCTL_POSTGRES_DSN=...
```

`scripts/init.sh` copies repo `.env` to `~/.foxctl/.env` if present and no global env file exists yet.

### Sandbox paths

When running skills in a restricted filesystem sandbox, set writable paths:

```bash
FOXCTL_STORAGE_ROOT=/tmp/foxctl/storage
FOXCTL_PATHS_CAS=/tmp/foxctl/cas
FOXCTL_OBS_DIR=/tmp/foxctl/observability
```

## Verify the install

```bash
foxctl version
foxctl skills list
foxctl mcp status
```

Get oriented in a repo:

```bash
foxctl run code/semantic_search --input '{"format":"tree"}'
```

## Build the repo graph

When you need call and reference navigation:

```bash
foxctl index repo build --workspace . --go --typescript --elixir
```

For non-Go repos, disable Go explicitly:

```bash
foxctl index repo build --workspace . --go=false --typescript --elixir
```

## First useful workflows

| Need | Command family |
|---|---|
| Find code semantically | `foxctl run code/semantic_search` |
| Extract snippets | `foxctl run code/smart_search` |
| Build graph relationships | `foxctl index repo build` |
| Inspect graph neighborhoods | `foxctl index repo search` and `expand` |
| Start an agent | `foxctl agent spawn` |
| Coordinate a room | `foxctl room ...` |
| Refresh ContextWiki/Obsidian context | `foxctl obsidian ...` |

## Developer setup helpers

```bash
make skills-install      # Install built skills
make skills-install-all  # Install all skill variants including CGO
./scripts/init.sh        # Provider bootstrap and local wiring
```

## Troubleshooting common install issues

| Symptom | Cause | Fix |
|---|---|---|
| `foxctl` command not found | `~/.local/bin` not in `PATH` | Add `export PATH="$HOME/.local/bin:$PATH"` to your shell profile |
| `Command 'run' not available in bundled mode` | Using a wrapper binary from another install | Run `./bin/foxctl ...` from the repo checkout |
| Build fails with CGO errors | Default build is non-CGO; CGO skills are optional | Run `make build` (non-CGO) instead of CGO-enabled builds |
| `bun` not found during install | Bun not installed or not in `PATH` | Install Bun or use `./install.sh --yes --skip-bun` |

## Canonical sources

- [`README.md`](https://github.com/joshka0/foxctl/blob/main/README.md)
- [`docs/start/README.md`](https://github.com/joshka0/foxctl/blob/main/docs/start/README.md)
- [`AGENTS.md`](https://github.com/joshka0/foxctl/blob/main/AGENTS.md)
