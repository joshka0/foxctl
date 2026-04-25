---
vault_refs:
  - notes/repo/foxctl/platform-and-web.md
  - notes/repo/foxctl/semantic-and-memory.md
  - notes/repo/foxctl/skills-runtime-wiring.md
  - notes/repo/foxctl/index.md
  - 00-home/index.md
---

# foxctl

> AI agent toolkit for local skills, retrieval, memory, provider integrations, and multi-agent workflows.

![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)
[![Go Version](https://img.shields.io/badge/go-1.26.1+-blue.svg)](https://golang.org/dl/)

`foxctl` is the repo-local control plane behind a lot of this project’s AI workflow:

- skill execution and installation
- semantic/code retrieval and repo indexing
- session continuity and memory storage
- MCP and web/API serving
- provider bootstrap for Claude Code, Codex, OpenCode, and Gemini
- durable room / agent / mailbox orchestration

The repository is primarily Go, with Bun-based packages for the web GUI.

## What’s Here

- `cmd/foxctl/` - Cobra CLI entrypoints
- `skills/` - installable skill implementations
- `configs/hooks/` - provider hook/runtime glue
- `configs/skills-pack/` - provider-facing skill packs
- `internal/` - runtime, storage, indexing, context, and web internals
- `cmd/foxctl_tui/` - canonical Go terminal agent shell
- `packages/gui-agent/` - web operator surface
- `archive/packages/tui-agent/` - archived TypeScript TUI workbench
- `archive/cmd/foxctl_viewer/` - archived legacy Go viewer TUI
- `docs/` - canonical architecture, guides, specs, and plans

### Internal Layout

This is the short version of the `internal/*` grouping. The canonical placement
rules live in [docs/architecture/package-topology.md](docs/architecture/package-topology.md).

```text
internal/
  domain/        core contracts and value types
  platform/      config, workspace, logging, cross-cutting helpers
  protocol/      wire/envelope/protocol helpers
  storage/       SQLite/libsql/postgres stores, CAS, durable state
  auth/          authn/authz and identity-facing helpers
  providers/     provider-specific integrations and compatibility layers
  runtime/       execution, daemon, orchestration, terminal, hooks, observability
  v2/            newer agent/runtime/orchestration lane only
  context/       memory, session continuity, transcript/context plane
  intelligence/  retrieval, indexing, codemaps, refactor/code intelligence
  interfaces/    web, gateway, chat adapter, OpenAPI transport layers
  tooling/       evals, standalone tools, runtime-neutral tooling
```

Two important rules:

- new top-level `internal/*` roots should be rare
- `internal/v2/*` is not the default destination for new code; it is scoped to
  the newer agent/runtime/orchestration stack

## Install

### Recommended

Run the interactive installer from the repo root:

```bash
./install.sh
```

That flow is intended to:

- verify or install core local dependencies
- optionally install CGO/SQLite headers for `foxctl-cgo`
- optionally install Bun for GUI and OpenCode plugin workflows
- build the CLI and skills
- wire up provider integrations via `scripts/init.sh`

If you run the installer outside an existing checkout, it clones the repo from GitLab.

### Non-interactive

```bash
./install.sh --yes
```

Useful flags:

```bash
./install.sh --yes --skip-cgo
./install.sh --yes --skip-bun
./install.sh --yes --skip-provider-setup
./install.sh --yes --repo-dir "$HOME/src/foxctl"
```

### Manual / Dev Setup

If you already have the toolchain installed and want the repo-native flow:

```bash
make init
```

`make init` runs:

- `build-all`
- `skills-install-all`
- `./scripts/init.sh`

If you only want the pure-Go CLI path:

```bash
make build
make skills-install
./scripts/init.sh
```

## Prerequisites

### Required

- `bash`
- `git`
- `make`
- `jq`
- Go `1.26.1+`

### Recommended For Full Setup

- C compiler + SQLite development headers/libs
  - needed for `make build-cgo`, `make test-cgo-short`, and `foxctl-cgo`
- Bun
  - needed for `packages/gui-agent` and OpenCode plugin install flows

On macOS, the full path usually means Homebrew `go`, `jq`, `sqlite`, and `bun`.

On Debian/Ubuntu, the full path usually means `build-essential`, `pkg-config`,
`libsqlite3-dev`, `jq`, and a current Go toolchain.

## Environment

`foxctl` loads env files in this order:

1. `~/.foxctl/.env`
2. the git-root `.env`
3. the current working directory `.env`

Common variables:

```bash
# Embeddings / semantic retrieval
VOYAGE_API_KEY=...
GEMINI_API_KEY=...

# Optional LLM-backed flows
OPENROUTER_API_KEY=...
ANTHROPIC_API_KEY=...

# Optional remote / cross-workspace storage
TURSO_DATABASE_URL=...
TURSO_AUTH_TOKEN=...
FOXCTL_POSTGRES_DSN=...
```

`scripts/init.sh` will copy repo `.env` to `~/.foxctl/.env` if present and no
global env file exists yet.

## First Run

Verify the install:

```bash
foxctl version
foxctl skills list
foxctl mcp status
```

Get oriented in a repo:

```bash
foxctl run code/semantic_search --input '{"format":"tree"}'
```

Build the repo graph index when you need call/reference navigation:

```bash
foxctl index repo build --workspace . --go --typescript --elixir
```

For non-Go repos, disable Go explicitly:

```bash
foxctl index repo build --workspace . --go=false --typescript --elixir
```

## Common Commands

```bash
# Skills
foxctl run code/semantic_search --input '{"query":"error handling","limit":10}'
foxctl run code/smart_search --input '{"query":"session restore"}'
foxctl run code/context_grep --input '{"pattern":"Run\\(ctx","path":"internal"}'

# Memory
foxctl memory put --name "gotcha-x" --type "gotcha" --summary "..."
foxctl memory search "gotcha"

# Sessions / continuity
foxctl sessions list
foxctl context task-history-summary --task-id <id>

# Agents
foxctl agent spawn --role researcher --prompt "trace the room runtime"
foxctl agent ask <id> --question "what did you find?" --wait

# Rooms / durable coordination
foxctl room create my-room
foxctl room status my-room
foxctl room inbox my-room

# Web / MCP
foxctl web serve --dev-cors
foxctl mcp serve --skills
```

## Provider Bootstrap

`scripts/init.sh` is the current source of truth for local provider wiring. It
handles:

- symlinking `foxctl` and `foxctl-cgo` into `~/.local/bin`
- creating `~/.foxctl/{storage,cache,cas,skills,jobs,observability,backups}`
- installing provider skill packs from `configs/skills-pack`
- configuring Claude Code hooks/settings
- wiring OpenCode plugin + skills + agents
- creating Codex MCP config and skill links
- linking Gemini skill packs
- starting the shared MCP daemon with `foxctl mcp serve --daemon --skills`

Today that bootstrap targets:

- Claude Code
- Codex
- OpenCode
- Gemini

## Web GUI And TUI

Install Bun dependencies for the web GUI:

```bash
bun install
```

Run the web operator surface:

```bash
make gui-agent
```

Run the canonical Go TUI with a local foxctl agent:

```bash
make go-tui-agent
```

Build the TUI binary without launching it:

```bash
make go-tui-build
```

The API server entrypoint is:

```bash
foxctl web serve --dev-cors
```

## Development

Core Go workflow:

```bash
make fmt
make lint
make test
make test-race
make test-cgo-short
```

Builds:

```bash
make build
make build-cgo
make skills-install-all
```

Important CGO rule: do not use raw `CGO_ENABLED=1 go build` for the full CLI.
Use `make build-cgo`, which adds `-tags=libsqlite3` and avoids duplicate SQLite
symbol problems.

For markdown/doc changes:

```bash
make check-doc-links
```

## Documentation

Start with:

- [AGENTS.md](AGENTS.md)
- [docs/README.md](docs/README.md)
- [docs/start/README.md](docs/start/README.md)
- [docs/architecture/package-topology.md](docs/architecture/package-topology.md)

Current high-signal docs:

- [docs/architecture/context-architecture.md](docs/architecture/context-architecture.md)
- [docs/architecture/jido-hybrid-runtime.md](docs/architecture/jido-hybrid-runtime.md)
- [docs/general/agent-daemon.md](docs/general/agent-daemon.md)
- [docs/guides/kubernetes.md](docs/guides/kubernetes.md)
- [docs/spec/agent_hierarchy.md](docs/spec/agent_hierarchy.md)

## foxprox

`foxprox/` is a self-contained Go module (`github.com/joshka/foxprox`) implementing
the Foxprox agent terminal coordination protocol — a local-first daemon for PTY-backed
sessions, multi-agent rooms, typed intents, and structured message delivery.

It lives in this repo via a `go.mod` replace directive and is wired into the foxctl
web server through a thin bridge layer (`internal/interfaces/foxproxbridge/`). The
module is fully independent: it has its own `go.mod`, its own tests, and can be
split into a separate repository at any point via `git subtree split`.

Key commands built from the module:

- `foxproxd` — the daemon
- `foxproxctl` — CLI client (sessions, rooms, messages)

Protocol spec: [foxprox/docs/Foxprox-v0.1.md](foxprox/docs/Foxprox-v0.1.md)

## License

Apache License 2.0
