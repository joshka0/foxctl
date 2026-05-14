---
title: Command map
description: Task-oriented map of the recommended foxctl command for each common workflow.
---

This page maps common tasks to the recommended foxctl command. Use [CLI reference](/reference/cli/) for flag-level details and the binary help output for exhaustive options.

## Skill execution

| Task | Command |
|---|---|
| Run a skill with job tracking | `foxctl run code/semantic_search --input '{"query": "auth"}'` |
| Run a skill without persistence | `foxctl run code/semantic_search --ephemeral --input '{"query": "auth"}'` |
| Run a skill with parameter flags | `foxctl skills run code/semantic_search --query "auth" --limit 10` |
| Pass input from a file | `foxctl run <skill> --input-file input.json` |
| Pass input from stdin envelope | `foxctl run <skill> --input stdin` |
| Load input from CAS | `foxctl run <skill> --input sha256:abc123...` |

## Code search and retrieval

| Task | Command |
|---|---|
| Semantic search across code | `foxctl run code/semantic_search --input '{"query": "database pooling"}'` |
| Search + extract snippets | `foxctl run code/smart_search --input '{"query": "session restore"}'` |
| Extract from known files | `foxctl run code/snippet_extract --input '{"query": "auth", "candidates": [...]}'` |
| Full function bodies matching a pattern | `foxctl run code/context_ripgrep --input '{"pattern": "func.*Handle", "path": "."}'` |
| Get a stored codemap | `foxctl run codemap/get --input '{"id": "01KES..."}'` |
| Generate a new codemap | `foxctl run codemap/generate --input '{"query": "trace auth flow"}'` |
| Search with tree output | `foxctl run code/semantic_search --input '{"query": "storage", "format": "tree"}'` |

## Repo graph and indexing

| Task | Command |
|---|---|
| Build repo graph (Go + TypeScript) | `foxctl index repo build --workspace . --go --typescript` |
| Build for non-Go repo | `foxctl index repo build --workspace . --go=false --typescript` |
| Build with Elixir | `foxctl index repo build --workspace . --go --typescript --elixir` |
| Search the index | `foxctl index repo search --workspace . --query "repoindex" --limit 10` |
| Expand from seed node | `foxctl index repo expand --workspace . --seed "<node-id>" --edge CALLS --edge REFERS_TO --depth 2` |
| DAG grep explanation subgraph | `foxctl run code/dag_grep --input '{"query": "buildEvidencePack", "workspace": ".", "render": "tree"}'` |

## Agent management

| Task | Command |
|---|---|
| Spawn a research agent | `foxctl agent spawn --role researcher --prompt "Research X" --exec-mode autonomous --max-auto-turns 3` |
| Spawn an overseer | `foxctl agent spawn --role overseer --prompt "Coordinate Y" --exec-mode autonomous` |
| Ask an agent a question | `foxctl agent ask <id> --question "What did you find?" --wait --timeout 120s` |
| Watch agent activity | `foxctl agent watch <id>` |
| Show agent hierarchy | `foxctl agent hierarchy [session-id]` |
| Resume a previous session | `foxctl agent resume <session-id> --prompt "Continue..."` |
| Kill an agent | `foxctl agent kill <id>` |

## Room coordination

| Task | Command |
|---|---|
| Send a message to a room | `foxctl room send --room-id <id> --text "Hello" --recipient <actor>` |
| Check inbox | `foxctl room inbox --room-id <id>` |
| Acknowledge a message | `foxctl room ack --room-id <id> --message-ids <id1>,<id2>` |
| Resolve a message | `foxctl room resolve --room-id <id> --message-ids <id1>` |
| Add a task | `foxctl room task add --room-id <id> --title "Fix bug" --description "..."` |
| List tasks | `foxctl room task list --room-id <id>` |
| Claim a task | `foxctl room task claim --room-id <id> --task-id <id>` |

See [Rooms](/collaboration/rooms/) for the full room protocol.

## Memory and context

| Task | Command |
|---|---|
| Store a memory item | `foxctl memory put --name "gotcha-x" --type "gotcha" --summary "..."` |
| Search memory | `foxctl memory search "query"` |
| Get task history summary | `foxctl context task-history-summary` |
| Recall relevant sessions | `foxctl context session-recall --query "auth debugging"` |
| Get session timeline | `foxctl context session-timeline --session-id <id>` |

See [Memory and continuity](/memory/continuity/) for the full memory API.

## ContextWiki and Obsidian

| Task | Command |
|---|---|
| Build vault graph | `foxctl obsidian graph build --workspace . --vault-path "/path/to/vault"` |
| Promote graph to vault | `foxctl obsidian graph promote --workspace . --vault-path "/path/to/vault"` |
| Reconcile bridge | `foxctl obsidian bridge reconcile --workspace . --vault-path "/path/to/vault"` |
| Rebuild index | `foxctl obsidian index build --vault-path "/path/to/vault"` |

See [Obsidian bridge](/context/obsidian-bridge/) for the full vault workflow.

## Storage and CAS

| Task | Command |
|---|---|
| Store an artifact | `foxctl cas put < artifact.json` |
| Retrieve by digest | `foxctl cas get sha256:abc123...` |
| Pin an artifact | `foxctl cas pin sha256:abc123...` |
| Garbage collect unpinned artifacts | `foxctl cas gc --older-than=168h --dry-run` |
| Create a backup | `foxctl backup create --name nightly` |
| List backups | `foxctl backup list` |

See [Storage](/storage/cas-and-persistence/) for store classes and driver configuration.

## Quality and CI

| Task | Command |
|---|---|
| Run unit tests | `make test` |
| Run race tests | `make test-race` |
| Run linter | `make lint` |
| Run all checks | `make check` |
| Check doc links | `make check-doc-links` |
| Enforce coverage floor | `make check-coverage` |
| Enforce strict coverage | `make check-coverage-strict` |
| Build docs site | `cd packages/docs-site && bun run build` |

See [CI and evals](/quality/ci-and-evals/) for the full quality pipeline.

## Help and discovery

| Task | Command |
|---|---|
| Top-level help | `foxctl --help` |
| Subcommand help | `foxctl <command> --help` |
| Version info | `foxctl version` |

