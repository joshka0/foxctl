<div align="center">

# foxctl

**AI agent toolkit for local skills, retrieval, memory, and multi-agent workflows.**

![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)
[![Go Version](https://img.shields.io/badge/Go-1.26.1+-00ADD8.svg)](https://golang.org/dl/)
[![Bun](https://img.shields.io/badge/Bun-optional-000000.svg)](https://bun.sh/)

[Getting Started](#quick-start) · [Install](#install) · [Docs](#documentation) · [Development](#development)

</div>

---

foxctl is the repo-local control plane for AI-powered development workflows — skill execution, semantic code retrieval, session continuity, provider integrations, and durable multi-agent coordination.

## Features

| Capability | Description |
|---|---|
| **Skill Execution** | Installable, composable skills with JSON envelope I/O and WASI isolation |
| **Code Retrieval** | Semantic search, smart search, context grep, and repo graph indexing |
| **Session Continuity** | Persistent memory, transcript capture, and task-history summaries across sessions |
| **Multi-Agent Orchestration** | Durable rooms, mailbox coordination, overseer hierarchies, and agent lifecycle management |
| **Context Engine** | Unified pipeline from raw events → typed evidence → claims → impact → retrieval packs |
| **Provider Integrations** | Bootstrap for Claude Code, Codex, OpenCode, and Gemini via MCP serving |
| **MCP & Web Serving** | Built-in MCP server and web GUI for operator control |
| **Kubernetes Deployment** | Production-grade manifests and overlays for container orchestration |

## Quick Start

```bash
# Clone and install
git clone <repo-url> foxctl && cd foxctl
./install.sh

# Verify
foxctl version
foxctl skills list
foxctl mcp status

# Search your codebase
foxctl run code/semantic_search --input '{"format":"tree"}'

# Build the repo graph index for call/reference navigation
foxctl index repo build --workspace . --go --typescript --elixir
```

For non-Go repos, disable Go indexing explicitly:

```bash
foxctl index repo build --workspace . --go=false --typescript --elixir
```

## Install

### Recommended — Interactive Installer

```bash
./install.sh
```

The installer:

- Verifies or installs core local dependencies
- Optionally installs Bun for GUI and OpenCode plugin workflows
- Builds the CLI and skills
- Wires up provider integrations via `scripts/init.sh`

If you run the installer outside an existing checkout, it clones the repo from GitLab.

### Non-Interactive

```bash
./install.sh --yes
```

Useful flags:

```bash
./install.sh --yes --skip-bun
./install.sh --yes --skip-provider-setup
./install.sh --yes --repo-dir "$HOME/src/foxctl"
```

### Manual / Dev Setup

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

## Prerequisites

### Required

- `bash`, `git`, `make`, `jq`
- Go `1.26.1+`

### Recommended (Full Setup)

- Bun — needed for `packages/gui-agent` and OpenCode plugin install flows

On macOS: Homebrew `go`, `jq`, and `bun`.

On Debian/Ubuntu: `git`, `make`, `jq`, `curl`, and a current Go toolchain.

## Environment

`foxctl` loads env files in this order:

1. `~/.foxctl/.env`
2. The git-root `.env`
3. The current working directory `.env`

Common variables:

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

`scripts/init.sh` copies the repo `.env` to `~/.foxctl/.env` if present and no global env file exists yet.

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

## Context Engine

The **Unified Context Engine** transforms raw events (file writes, commits, corrections, task changes, tool calls) into structured, typed, and validated context primitives — replacing scattered string-based references and keyword heuristics with a single canonical type system.

**Pipeline:**

```
events → evidence → claims → impact/staleness → projections → retrieval packs → feedback
```

### Key Concepts

| Concept | Description |
|---|---|
| **EvidenceRef** | Typed reference (`file://`, `symbol://`, `note://`, etc.) replacing raw `[]string` |
| **EvidencePack** | Compact, serializable bundle of evidence items returned by retrieval lanes; large packs go to CAS |
| **MemoryClaim** | Durable assertion with typed lifecycle transitions (Candidate → Current → NeedsRevalidation → Stale → Superseded/Rejected) |
| **Impact Engine** | Computes downstream effects of changes, marks stale evidence, and resolves invalidation chains |
| **Retrieval Lanes** | Five composable lane services (code, memory, context, task, mixed) returning `EvidencePack` with telemetry |
| **ExtractionPolicy** | Replaces keyword heuristics with typed interfaces for companion signal extraction |

### Design Invariants

| Invariant | Rule |
|---|---|
| Pure core | `contextengine/` has zero IO imports — only stdlib |
| No keyword heuristics | Classification uses typed fields, scored features, or structured policies |
| Append-only events | `ContextEvent`, `RetrievalEpisode`, `RetrievalFeedback` never mutate |
| Typed transitions | Claim and staleness status changes go through explicit transition functions |
| CAS for large payloads | Evidence packs exceeding 64KB persist to CAS with summary inline |
| Bidirectional adapters | Adapters import contextengine + source package; no cycles |

### Dependency Direction

```
internal/context/contextengine/        ← pure, no IO imports
         ↑
internal/context/contextengine/adapters/ ← imports both sides
         ↑
internal/storage/contextengine/        ← imports contextengine only
         ↑
internal/context/contextplane/         ← uses adapters + contextengine
internal/context/companion/            ← uses adapters + contextengine
internal/rlm/                          ← uses adapters + contextengine
```

## Repository Layout

```
cmd/foxctl/                  Cobra CLI entrypoints
cmd/foxctl_tui/              Canonical Go terminal agent shell
skills/                      Installable skill implementations
configs/hooks/               Provider hook/runtime glue
configs/skills-pack/         Provider-facing skill packs
internal/                    Runtime, storage, indexing, context, and web internals
packages/gui-agent/          Web operator surface
packages/docs-site/          Starlight documentation site
archive/                     Archived packages and legacy tooling
docs/                        Canonical architecture, guides, specs, and plans
foxprox/                     Foxprox agent terminal coordination (independent module)
```

### Internal Packages

> For contributors working inside `internal/`. The canonical placement rules live in [docs/architecture/package-topology.md](docs/architecture/package-topology.md).

| Package | Responsibility |
|---|---|
| `domain/` | Core contracts and value types |
| `platform/` | Config, workspace, logging, cross-cutting helpers |
| `protocol/` | Wire/envelope/protocol helpers |
| `storage/` | SQLite/libsql/postgres stores, CAS, durable state |
| `auth/` | Authn/authz and identity-facing helpers |
| `providers/` | Provider-specific integrations and compatibility layers |
| `runtime/` | Execution, daemon, orchestration, terminal, hooks, observability |
| `v2/` | Newer agent/runtime/orchestration lane only |
| `context/` | Memory, session continuity, transcript/context plane |
| `intelligence/` | Retrieval, indexing, codemaps, refactor/code intelligence |
| `interfaces/` | Web, gateway, chat adapter, OpenAPI transport layers |
| `tooling/` | Evals, standalone tools, runtime-neutral tooling |

**Rules:**

- New top-level `internal/*` roots should be rare
- `internal/v2/*` is not the default destination for new code; it is scoped to the newer agent/runtime/orchestration stack

## Provider Bootstrap

`scripts/init.sh` is the current source of truth for local provider wiring. It handles:

- Symlinking `foxctl` into `~/.local/bin`
- Creating `~/.foxctl/{storage,cache,cas,skills,jobs,observability,backups}`
- Installing provider skill packs from `configs/skills-pack`
- Configuring Claude Code hooks/settings
- Wiring OpenCode plugin + skills + agents
- Creating Codex MCP config and skill links
- Linking Gemini skill packs
- Starting the shared MCP daemon with `foxctl mcp serve --daemon --skills`

Currently supported providers: Claude Code, Codex, OpenCode, Gemini

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

The API server entrypoint:

```bash
foxctl web serve --dev-cors
```

## foxprox

`foxprox/` is a self-contained Go module (`github.com/joshka/foxprox`) implementing the Foxprox agent terminal coordination protocol — a local-first daemon for PTY-backed sessions, multi-agent rooms, typed intents, and structured message delivery.

It lives in this repo via a `go.mod` replace directive and is wired into the foxctl web server through a thin bridge layer (`internal/interfaces/foxproxbridge/`). The module is fully independent: it has its own `go.mod`, its own tests, and can be split into a separate repository at any point via `git subtree split`.

Key commands built from the module:

- `foxproxd` — the daemon
- `foxproxctl` — CLI client (sessions, rooms, messages)

Protocol spec: [foxprox/docs/ATCP-v0.1.md](foxprox/docs/ATCP-v0.1.md)

## Development

Core Go workflow:

```bash
make fmt
make lint
make test
make test-race
```

Builds:

```bash
make build
make skills-install-all
```

The CLI build is non-CGO by default. Turso is the canonical SQLite-family storage path; do not reintroduce the old libsqlite3/sqlite-vector build lane.

For markdown/doc changes:

```bash
make check-doc-links
```

## Documentation

Start with:

- [AGENTS.md](AGENTS.md) — AI assistant guide and command reference
- [docs/README.md](docs/README.md) — Canonical documentation map
- [docs/start/README.md](docs/start/README.md) — Fast orientation guides
- [docs/architecture/package-topology.md](docs/architecture/package-topology.md) — `internal/*` placement rules

Current high-signal docs:

- [docs/architecture/context-architecture.md](docs/architecture/context-architecture.md) — Dual-plane context + Obsidian knowledge layer
- [docs/architecture/jido-hybrid-runtime.md](docs/architecture/jido-hybrid-runtime.md) — Go-native vs Jido runtime
- [docs/general/agent-daemon.md](docs/general/agent-daemon.md) — Agent daemon operations
- [docs/guides/kubernetes.md](docs/guides/kubernetes.md) — Kubernetes deployment
- [docs/spec/agent_hierarchy.md](docs/spec/agent_hierarchy.md) — Multi-agent hierarchy spec

## What's Here

- `cmd/foxctl/` - Cobra CLI entrypoints
- `skills/` - Installable skill implementations
- `configs/hooks/` - Provider hook/runtime glue
- `configs/skills-pack/` - Provider-facing skill packs
- `internal/` - Runtime, storage, indexing, context, and web internals
- `cmd/foxctl_tui/` - Canonical Go terminal agent shell
- `packages/gui-agent/` - Web operator surface
- `docs/archive/source/packages/tui-agent/` - Archived TypeScript TUI workbench
- `docs/archive/source/cmd/foxctl_viewer/` - Archived legacy Go viewer TUI
- `docs/` - Canonical architecture, guides, specs, and plans

## License

Apache License 2.0
