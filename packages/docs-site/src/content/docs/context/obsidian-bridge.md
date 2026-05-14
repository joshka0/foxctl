---
title: Obsidian bridge
description: Build, promote, reconcile, and index the Obsidian knowledge layer that connects repo context to a durable vault.
---

The Obsidian bridge turns repo documentation and code structure into a queryable
knowledge layer stored in an Obsidian vault. It is the knowledge-plane half of
foxctl's dual-plane [ContextWiki](/context/contextwiki/): ContextWiki manages
active workspace state (top-of-mind, handoffs, observations, tensions), while
the Obsidian bridge provides durable human-readable notes that survive across
sessions.

## Why it exists

Code changes fast. Documentation drifts. Agents working in a repo need access to
both current code truth and accumulated knowledge about decisions, gotchas, and
architectural intent. The Obsidian bridge solves this by:

1. Generating structured vault notes from the repo graph and docs tree.
2. Linking repo documentation to vault notes with bidirectional metadata.
3. Indexing notes, headings, wikilinks, aliases, tags, and embeddings for fast
   retrieval.
4. Keeping the knowledge layer queryable by ContextWiki retrieval, hooks, and agent
   workflows.

The bridge never silently rewrites canonical prose. It drafts into an inbox,
waits for review, and patches only frontmatter metadata.

## Vault refresh flow

The canonical four-step refresh runs after repo docs, repo graph, or bridge
metadata change:

### 1. Build the graph projection

Generate an inbox-first repo graph draft bundle from the repo index:

```bash
foxctl obsidian graph build --workspace . --vault-path "/path/to/vault"
```

This produces:

- A root repo map-of-contents (MOC) note
- Package notes with `paths` and `symbols` frontmatter
- Wikilinks between generated package notes and the root map
- Package selection biased toward richer repo graph packages

Drafts land in `inbox/drafted-from-foxctl/repo-graph/<project>/`.

### 2. Promote reviewed graph content

Review-merge generated graph drafts into canonical vault notes:

```bash
foxctl obsidian graph promote --workspace . --vault-path "/path/to/vault"
```

This merges reviewed drafts from the inbox into `notes/repo/<project>/`.

### 3. Reconcile bridge metadata

Scan repo `docs/` against canonical vault notes and draft backlink suggestions:

```bash
foxctl obsidian bridge reconcile --workspace . --vault-path "/path/to/vault"
```

Reconcile behavior:

- Scans repo markdown under `docs/` (excludes `docs/archive/` unless
  explicitly included)
- Scans canonical vault notes for `repo_docs` backlinks
- Prefers vault-index lexical and semantic candidates when available
- Drafts bridge notes and backlink suggestions into the inbox instead of
  rewriting repo docs or canonical notes

### 4. Build the vault index

Index notes, headings, links, and embeddings for retrieval:

```bash
foxctl obsidian index build --vault-path "/path/to/vault"
```

The vault index covers:

| Index target | Purpose |
|---|---|
| Notes | Core note records |
| Headings | Section-level granularity |
| Wikilinks | Inter-note relationships |
| Aliases | Alternate note names |
| Tags | Topic grouping |
| Note chunks | Snippet-bearing lexical search |
| Repo paths | Code-to-note mapping from frontmatter `paths` |
| Repo symbols | Symbol-to-note mapping from frontmatter `symbols` |
| Note embeddings | Semantic search (when embedding provider configured) |
| Chunk embeddings | Snippet-level semantic search |

## When to run the refresh

Run the full four-step flow:

- After canonical docs change in `docs/`
- After repo graph or semantic anchor changes
- After bridge metadata changes
- Before relying on the vault as current retrieval evidence

The daemon maintenance loop can also rebuild the vault index automatically when
`FOXCTL_OBSIDIAN_VAULT_PATH` is set.

## Bridge metadata contract

The bridge uses bidirectional frontmatter to link repo docs and vault notes:

- Repo docs may carry `vault_refs` — a list of vault note paths
- Vault notes may carry `repo_docs` — a list of repo doc paths

Bridge drafts are classified by status:

| Status | Meaning |
|---|---|
| `draft` | Generated but unreviewed |
| `reviewed` | Ready to apply |
| `partial` | Only some links matched |
| `applied` | Frontmatter patched successfully |

## Bridge management commands

### Report bridge state

View current draft classifications and link status:

```bash
foxctl obsidian bridge report --workspace . --vault-path "/path/to/vault"
```

### Apply reviewed drafts

Patch frontmatter for a single reviewed draft:

```bash
foxctl obsidian bridge apply --workspace . --vault-path "/path/to/vault"
```

This patches repo doc `vault_refs` and vault note `repo_docs`. It only touches
frontmatter list metadata, not prose.

### Apply in bulk

Apply all reviewed drafts at once:

```bash
foxctl obsidian bridge apply-batch --workspace . --vault-path "/path/to/vault"
```

Defaults to `status: reviewed`. Supports optional trust and doc-path filters.
Skips non-reviewed drafts instead of applying them implicitly.

### Archive applied drafts

Move fully applied drafts out of the inbox:

```bash
foxctl obsidian bridge tidy --workspace . --vault-path "/path/to/vault"
```

Moves `state=applied` drafts to `ops/docs-bridge-applied/<project>/` and marks
them `status: applied`. Leaves `draft`, `reviewed`, and `partial` notes in
place.

## Vault search and retrieval

Once indexed, vault notes are queryable through several paths:

| Command | Purpose |
|---|---|
| `foxctl obsidian search --query ...` | Search vault notes |
| `foxctl obsidian related --path ...` | Find related notes via wikilinks, backlinks, aliases |
| `foxctl obsidian index search --query ...` | Indexed lexical search |
| `foxctl obsidian index search --semantic` | Semantic search over note embeddings |
| `foxctl obsidian index related --path ...` | Index-backed related note lookup |

ContextWiki retrieval blends vault-index hits with repo-index file and symbol hints when
the workspace repo index is available. When an embedding provider is configured,
`context retrieve` defaults to blended retrieval.

## Semantic search setup

To enable semantic vault search, configure an OpenAI-compatible embedding
endpoint:

```bash
export FOXCTL_OBSIDIAN_SEMANTIC_PROVIDER=openai_compat
export FOXCTL_OPENAI_COMPAT_BASE_URL=http://127.0.0.1:1234/v1
export FOXCTL_OPENAI_COMPAT_EMBEDDING_MODEL=text-embedding-embeddinggemma-300m-qat
```

With this configuration:

- `context retrieve` uses blended retrieval (lexical + semantic) by default
- `foxctl obsidian index search --semantic` returns embedding-ranked results
- `eval retrieval` can compare lexical, semantic, and blended modes

Control flags:

| Flag | Purpose |
|---|---|
| `FOXCTL_OBSIDIAN_SEMANTIC_ENABLED` | Force semantic on/off (`false` or `0` to disable) |
| `FOXCTL_OPENAI_COMPAT_API_KEY` | Optional API key for the embedding endpoint |

## Vault health and maintenance

| Command | Purpose |
|---|---|
| `foxctl obsidian index health` | Scan for orphans, dead ends, unresolved links, oversized MOCs |
| `foxctl obsidian index stats` | Index statistics |

When the daemon is running with a workspace and `FOXCTL_OBSIDIAN_VAULT_PATH`
is set, the maintenance loop:

- Rebuilds the local Obsidian index on a ticker
- Recomputes vault health
- Folds health findings into ContextWiki maintenance tasks

Control the maintenance interval with `FOXCTL_CONTEXTWIKI_MAINTENANCE_INTERVAL`
(`FOXCTL_ACA_MAINTENANCE_INTERVAL` is still accepted as a legacy alias). The
environment variable keeps the old prefix for compatibility.

## Default vault layout

```text
inbox/
  drafted-from-foxctl/
    repo-graph/<project>/
    docs-bridge/<project>/
notes/
  repo/<project>/
ops/
  docs-bridge-applied/<project>/
```

Drafts start in the inbox. Reviewed merges move to canonical locations. Applied
bridge drafts archive under `ops/`.

## Relationship to other context systems

| System | Role |
|---|---|
| [ContextWiki](/context/contextwiki/) | Workspace control plane — top-of-mind, handoffs, observations, tensions |
| [Context engine](/context/context-engine/) | Typed evidence substrate — evidence packs, retrieval episodes, feedback |
| Obsidian bridge | Knowledge plane — durable notes, vault index, docs reconciliation |

The clean boundary is:

```text
ContextWiki decides what workspace context is useful
  -> Obsidian bridge provides durable knowledge notes
  -> Context engine records typed evidence and retrieval telemetry
```

## Canonical sources

- [docs/architecture/context-architecture.md](https://github.com/joshka0/foxctl/blob/main/docs/architecture/context-architecture.md)
- [docs/general/memory.md](https://github.com/joshka0/foxctl/blob/main/docs/general/memory.md)
- [AGENTS.md](https://github.com/joshka0/foxctl/blob/main/AGENTS.md)
