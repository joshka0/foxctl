---
title: CLI reference
description: Command families, input modes, and script wiring for foxctl.
---

The foxctl CLI is organized by job, not by binary implementation. Each command family targets a specific workflow. Use [Command map](/reference/command-map/) for task-to-command lookup.

## Command families

| Family | Use for | See also |
|---|---|---|
| `foxctl run` | Skill execution with job tracking, envelopes, and CAS artifacts | [Skills runtime](/skills/runtime-and-install/) |
| `foxctl skills` | Skill discovery, install, and direct execution with parameter flags | [Skills runtime](/skills/runtime-and-install/) |
| `foxctl index repo` | Repo graph build, search, expand, and DAG queries | [Repoindex and DAG grep](/retrieval/repoindex-and-dag-grep/) |
| `foxctl agent` | Spawn, ask, watch, resume, and manage agents | [Agent lifecycle](/agents/lifecycle/) |
| `foxctl room` | Durable room messages, tasks, and coordination | [Rooms](/collaboration/rooms/) |
| `foxctl obsidian` | ACA vault graph, bridge, and index refresh | [Obsidian bridge](/context/obsidian-bridge/) |
| `foxctl context` | Context summaries and evidence import | [ACA](/context/aca/) |
| `foxctl mcp` | MCP status and serving | [Providers and MCP](/integrations/providers-and-mcp/) |
| `foxctl openapi` | OpenAPI-backed provider integration | [OpenAPI and plugins](/integrations/openapi-and-plugins/) |
| `foxctl shell` | Structured read-only command-shaped retrieval | — |
| `foxctl cas` | CAS artifact storage, lookup, pin, and garbage collection | [Storage](/storage/cas-and-persistence/) |
| `foxctl backup` | Backup and restore support | [Storage](/storage/cas-and-persistence/) |
| `foxctl hooks` | Hook runtime checks and registration | [Hooks](/integrations/hooks/) |
| `foxctl ci` | CI status and support workflows | [CI and evals](/quality/ci-and-evals/) |
| `foxctl db` | Database inspection and management | [Storage](/storage/cas-and-persistence/) |

## Run command choice

Three execution paths exist for running skills:

### `foxctl run <skill>` — Job-tracked execution

The default path. Opens the jobs store, writes CAS/trajectory metadata, and supports async/dedupe behavior.

```bash
foxctl run code/semantic_search --input '{"query": "auth middleware"}'
```

### `foxctl run <skill> --ephemeral` — Stateless execution

Skips job persistence. Use for sandboxed agents, hooks, smoke tests, or one-off retrieval when `~/.foxctl` is not writable.

```bash
foxctl run code/semantic_search --ephemeral --input '{"query": "auth middleware"}'
```

### `foxctl skills run <skill>` — Direct skill execution

Executes the skill directly with manifest-derived parameter flags. Use when validating skill parameters or when a simple flag form is clearer than raw JSON.

```bash
foxctl skills run code/semantic_search --query "auth middleware" --limit 10
```

### Binary path note

If `foxctl` on PATH reports `Command 'run' not available in bundled mode`, you are using a wrapper from another install. Run `./bin/foxctl ...` from the repo checkout or rebuild:

```bash
make build
./bin/foxctl run code/semantic_search --input '{"query": "test"}'
```

## Input modes

Skills accept input in four modes:

### Raw JSON

```bash
foxctl run <skill> --input '{"key":"value"}'
```

### From file or stdin

```bash
foxctl run <skill> --input-file input.json
foxctl run <skill> --input-file -
```

### Envelope extraction

Reads an envelope from stdin and passes only its `data` field to the skill:

```bash
foxctl run <skill> --input stdin
```

### From CAS

Loads raw JSON from CAS by digest:

```bash
foxctl run <skill> --input sha256:abc123...
```

## Agent commands

The `foxctl agent` family manages persistent, multi-agent coordination:

| Command | Purpose |
|---|---|
| `foxctl agent spawn` | Create and start a new agent |
| `foxctl agent list` | List all agents |
| `foxctl agent info <id>` | Detailed agent status |
| `foxctl agent ask <id> --question "..." --wait` | Send a question and wait for reply |
| `foxctl agent kill <id>` | Stop an agent |
| `foxctl agent resume <session-id> --prompt "..."` | Continue a previous session |
| `foxctl agent hierarchy [session-id]` | Show agent tree |
| `foxctl agent watch <id>` | Live activity stream |

### Spawn flags

| Flag | Default | Description |
|---|---|---|
| `--role` | — | `overseer`, `researcher`, `coder`, `planner`, `reviewer` |
| `--exec-mode` | `reactive` | `reactive`, `autonomous`, `proactive` |
| `--llm-provider` | auto-detect | `openrouter`, `cerebras`, `groq`, `openai`, `anthropic` |
| `--llm-model` | provider default | Model name (e.g., `openrouter/aurora-alpha`) |
| `--max-iterations` | 10 | Max tool calls per engine run |
| `--max-auto-turns` | 1 | Max autonomous continuation turns |
| `--max-context-tokens` | 0 | Context budget (0 = no limit) |

See [Agent lifecycle](/agents/lifecycle/) for the full workflow.

## Repo index commands

| Command | Purpose |
|---|---|
| `foxctl index repo build --workspace . --go --typescript` | Build or rebuild the repo graph |
| `foxctl index repo search --workspace . --query "term"` | Search the index |
| `foxctl index repo expand --workspace . --seed <node-id> --edge CALLS --depth 2` | Expand from seed nodes |

Add `--go=false` for non-Go repos. See [Repoindex and DAG grep](/retrieval/repoindex-and-dag-grep/) for full usage.

## Memory commands

```bash
foxctl memory put --name "gotcha-x" --type "gotcha" --summary "..."
foxctl memory search "query"
```

See [Memory and continuity](/memory/continuity/) for the full memory API.

## ACA / Obsidian refresh

When repo docs or structure change, refresh the ACA knowledge layer:

```bash
foxctl obsidian graph build --workspace . --vault-path "/path/to/vault"
foxctl obsidian graph promote --workspace . --vault-path "/path/to/vault"
foxctl obsidian bridge reconcile --workspace . --vault-path "/path/to/vault"
foxctl obsidian index build --vault-path "/path/to/vault"
```

See [Obsidian bridge](/context/obsidian-bridge/) for the full vault workflow.

## Discovering commands

For exhaustive flag lists, use the built-in help:

```bash
foxctl --help
foxctl <command> --help
foxctl <command> <subcommand> --help
```
