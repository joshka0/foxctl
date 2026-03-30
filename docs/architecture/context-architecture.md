---
vault_refs:
  - notes/repo/agentctl/index.md
  - notes/repo/agentctl/platform-and-web.md
  - notes/repo/agentctl/semantic-and-memory.md
---
# AgentCTL Context Architecture

Status: current first slice

This document describes the first implemented slice of the AgentCTL Context Architecture (ACA): a workspace-local control plane scaffold plus a computed orientation bundle.

The intended direction is for this layer to grow over time into an external, current database of what is happening in the workspace: active frontier state, resumable handoffs, repeated observations, tensions, and promotion-ready drafts.

## Intent

ACA separates:

- a control plane for active work continuity
- a knowledge plane for durable human-readable notes

The implemented slice now includes a real control-plane runtime, an Obsidian adapter and local vault index, MCP surfaces for ACA and vault operations, and a bounded daemon maintenance loop. It still stops short of the full long-horizon `contextd` design from the larger proposal.

## Implemented Surface

Command:

- `agentctl orient`
- `agentctl capture`
- `agentctl observe`
- `agentctl tension`
- `agentctl context show`
- `agentctl context report`
- `agentctl context retrieve`
- `agentctl context retrieve-inspect`
- `agentctl context retrieve-inspect-suite`
- `agentctl context contradictions`
- `agentctl context rethink`
- `agentctl context handoffs`
- `agentctl context observations`
- `agentctl context tensions`
- `agentctl context proposals`
- `agentctl context proposal`
- `agentctl context proposal merge`
- `agentctl context import-evidence`
- `agentctl context infer`
- `agentctl context promote`
- `agentctl context merge-promotion`
- `agentctl context task-history`
- `agentctl context task-history-summary`
- `agentctl context family-history-summary`
- `agentctl context next-proposal-merge`
- `agentctl context hooks install`
- `agentctl obsidian read`
- `agentctl obsidian search`
- `agentctl obsidian related`
- `agentctl obsidian create-note`
- `agentctl obsidian append-under-heading`
- `agentctl obsidian capture-session`
- `agentctl obsidian promote-evergreen`
- `agentctl obsidian merge-reviewed-draft`
- `agentctl obsidian index build`
- `agentctl obsidian index search`
- `agentctl obsidian index related`
- `agentctl obsidian index health`
- `agentctl obsidian index stats`
- `agentctl obsidian graph build`
- `agentctl obsidian graph promote`
- `agentctl obsidian bridge reconcile`
- `agentctl obsidian bridge report`
- `agentctl obsidian bridge apply`
- `agentctl obsidian bridge apply-batch`
- `agentctl obsidian bridge tidy`

Behavior:

- detects the current workspace root
- reads existing task and session state from the persistent stores under `~/.agentctl/`
- derives a bounded `top_of_mind` bundle
- scaffolds workspace-local ACA files under `.agentctl/` if they do not exist
- persists structured runtime artifacts for handoffs, observations, and tensions
- reads persisted runtime artifacts back out through a stable CLI surface
- builds a synthesized current-state report from top-of-mind, latest handoff, merged observations, and open tensions
- blends ACA control-plane state with ranked vault-index hits through `context retrieve`
- can inspect one ACA retrieval miss, or a full retrieval suite, record misses as observations, propose deterministic corrective actions, and persist suite reports to CAS through `context retrieve-inspect*`
- boosts vault hits with repo-index-derived file and symbol hints when the workspace repo index is available
- optionally reranks top vault hits semantically when rerank is enabled through environment configuration
- links open tensions to relevant indexed notes through `context contradictions`
- generates maintenance tasks from repeated or high-impact tensions
- infers structured observations and tensions from stop/subagent summaries
- drafts promotion notes into the vault inbox from handoff records or repeated observations
- can import transcript/text evidence into the vault inbox through `context import-evidence`, using deterministic extraction or an optional local OpenAI-compatible summarizer lane
- evidence imports now seed the ACA proposal lane, so repeated topic imports dedupe into one proposal record instead of only accumulating isolated inbox drafts
- evidence-backed proposals can include a suggested canonical target note and bounded review heading when ACA finds a credible landing note in the existing vault graph
- applying an evidence-backed proposal now prepares a reviewed-merge job toward that suggested target instead of merging canonical notes automatically
- `context proposal apply|merge` now return a stable `work_packet` object so hooks and agents can consume merge intent without re-parsing proposal payloads
- daemon maintenance now projects prepared low-risk proposal work packets into `proposal_merge` maintenance tasks, so the next merge step is surfaced automatically
- `context next-proposal-merge --claim` and `hooks proposal-next-merge` now claim the selected packet in proposal state, keeping it hidden until `proposal merge` or `proposal release-merge`
- the GUI Context explorer now surfaces the next prepared ACA merge packet for the selected agent workspace, with claim/release/merge actions and a small sidebar badge when work is pending
- explicitly review-merges promotion drafts into canonical vault notes through a bounded merge path
- merges repeated observations and tensions into stable records instead of blindly appending duplicates
- can persist deduped ACA memory proposals from retrieval-inspection flows and expose low-risk apply/reject surfaces for proposal review
- refreshes top-of-mind and rethink state in a leader-safe daemon maintenance loop when the daemon is running with a workspace
- exposes a first Phase 1 Obsidian adapter through CLI and MCP
- includes an initial Phase 2 vault index for note/headings/links/aliases
- indexes repo-linked note metadata from frontmatter:
  - `paths`
  - `symbols`
- uses repo index search results to strengthen retrieval ranking for notes whose `paths` or `symbols` match code-relevant files or symbols
- can optionally add a git co-change prior, using files commonly committed together as a task/query-conditioned ranking boost for notes whose `paths` land in the same change neighborhood
- can materialize `cochange_cluster` memory artifacts from recent git history through `agentctl context cochange build`, making those file-neighborhood summaries searchable through memory and semantic retrieval paths
- supports direct semantic note search from stored note embeddings through `agentctl obsidian index search --semantic`
- can generate an inbox-first repo graph draft bundle from the repo index through `agentctl obsidian graph build`
- can review-merge a generated graph draft bundle into canonical repo notes through `agentctl obsidian graph promote`
- can reconcile repo `docs/` against canonical vault notes through `agentctl obsidian bridge reconcile`
  - scans repo markdown under `docs/`
  - defaults to current docs and excludes `docs/archive/` unless explicitly included
  - scans canonical vault notes for `repo_docs` backlinks
  - prefers vault-index lexical+semantic candidates when the local vault index and embedding provider are available
  - drafts bridge notes and backlink suggestions into the vault inbox instead of rewriting repo docs or canonical notes
- can report bridge draft state through `agentctl obsidian bridge report`
  - classifies drafts as `draft`, `reviewed`, `partial`, or `applied`
  - compares suggested links against current repo-doc and vault-note frontmatter
- can apply reviewed bridge frontmatter patches through `agentctl obsidian bridge apply`
  - patches repo doc `vault_refs`
  - patches canonical vault note `repo_docs`
  - only touches frontmatter list metadata, not prose
- can apply reviewed bridge drafts in bulk through `agentctl obsidian bridge apply-batch`
  - defaults to `status: reviewed`
  - supports optional trust and doc-path filters
  - skips non-reviewed drafts instead of applying them implicitly
- can archive fully applied bridge drafts through `agentctl obsidian bridge tidy`
  - moves `state=applied` drafts from the inbox into `ops/docs-bridge-applied/<project>/`
  - marks archived copies `status: applied`
  - leaves `draft`, `reviewed`, and `partial` bridge notes in place
- writes:
  - `.agentctl/runtime/top_of_mind.json`
  - `.agentctl/exports/latest-orientation.md`
  - default policy files under `.agentctl/policy/`
  - starter Obsidian templates under `.agentctl/templates/obsidian-vault/`
  - handoff JSON files under `.agentctl/runtime/handoffs/`
  - observation records in `.agentctl/runtime/observations.ndjson`
  - tension records in `.agentctl/runtime/tensions.ndjson`
  - promotion job records in `.agentctl/runtime/promotion_jobs.ndjson`
  - draft markdown notes in `.agentctl/templates/obsidian-vault/inbox/drafted-from-agentctl/`

## Hook Wiring

ACA lifecycle wiring is now available through the existing Claude shell-hook path under `configs/hooks/` plus the sample Claude settings file at `configs/claude-settings.json`.

Bootstrap command:

- `agentctl context hooks install`
  - merges ACA `SessionStart`, `Stop`, and `SubagentStop` hook entries into workspace `.claude/settings.json`
  - preserves existing `env`, `permissions`, and unrelated hook entries

Current hook behavior:

- `SessionStart` via `configs/hooks/session-init.sh`
  - runs `agentctl orient`
  - injects the latest orientation markdown from `.agentctl/exports/latest-orientation.md`
- `Stop` via `configs/hooks/session-end.sh`
  - keeps the existing session capture path
  - writes an ACA handoff with `agentctl capture`
  - can emit structured observations or tensions through `agentctl context infer --apply`
  - can draft a promotion automatically when `AGENTCTL_ACA_AUTO_PROMOTE=1`
- `SubagentStop` via `configs/hooks/subagent-stop.sh`
  - writes a bounded ACA handoff for subagent completion
  - can emit structured observations or tensions through `agentctl context infer --apply`
  - can draft a promotion automatically when `AGENTCTL_ACA_AUTO_PROMOTE=1`
- task continuity hook wrapper via `configs/hooks/task-continuity-summary.sh`
  - uses `agentctl context task-history-summary`
  - emits prompt-ready continuity context plus `task_continuity_artifact`
- proposal work-packet hook wrapper via `configs/hooks/proposal-work-packet.sh`
  - uses `agentctl hooks proposal-packet`
  - emits prompt-ready proposal context plus `proposal_work_packet`
- next prepared proposal-merge hook wrapper via `configs/hooks/proposal-next-merge.sh`
  - uses `agentctl hooks proposal-next-merge`
  - emits prompt-ready next-merge context plus `proposal_work_packet`
  - claims the selected packet by default so it is not re-offered until merged or released

Task continuity delivery split:

- `agentctl context task-history-summary`
  - structured command for Codex, scripts, and agent runtimes
- `agentctl context family-history-summary`
  - structured repo-family transcript summary over persisted transcript-history records
  - supports:
    - `--focus-query` to bias selection toward one transcript lane
    - `--date-from YYYY-MM-DD`
    - `--date-to YYYY-MM-DD`
  - filters by transcript/session time *(stored as `source_started_at`)* before owner selection and recurring aggregation
  - returns per-item `support_metadata` for trust/debugging (`owner_count`, `latest_updated_at`, `latest_age_days`, `source_owners`)
- `configs/hooks/task-continuity-summary.sh`
  - thinner wrapper for hook injection payloads

## MCP Read Surface

The MCP facade now exposes first-class ACA read tools:

- `context_show`
- `context_report`
- `context_retrieve`
- `context_next_proposal_merge`
- `context_contradictions`
- `context_handoffs`
- `context_observations`
- `context_tensions`
- `context_proposals`
- `context_next_proposal_merge`
- `context_proposal_merge`
- `context_rethink`
- `context_promote`
- `context_merge_promotion`

These provide direct ACA access without routing through generic `agentctl_run`.

## Transcript Family History

The transcript-family surface is a repo-family continuity layer built on top of persisted transcript-history records.

Precondition:

- transcript history must be persisted first, usually through:
  - `agentctl sessions derive-memory --memory-lane insight --persist-history`
  - `agentctl sessions derive-memory-group --memory-lane insight --persist-history`

Selection model:

- scope first (`workspace`, `family`, `auto`)
- optional transcript-time date window (`--date-from` / `--date-to`)
- optional semantic lane bias (`--focus-query`)
- then recent/support-aware owner ranking

Summary modes:

- `deterministic`
  - ranked directly from persisted transcript-history records
- `llm`
  - full-family summary pass succeeded
- `llm_cleanup`
  - shortlist cleanup succeeded even when the broader family summary needed fallback/help

This is the surface to use when you want:

- “what has this repo family been doing recently?”
- “what happened in this one workstream last week?”
- “what keeps recurring across related worktrees?”

## Obsidian Adapter

Phase 1 Obsidian adapter paths now exist under `internal/tools/obsidian/`.

Implemented now:

- vault targeting by name or path
- official Obsidian CLI invocation for reads and search
- related-note lookup through wikilinks, backlinks, and aliases
- safe draft/inbox-first note creation
- bounded append-under-heading behavior
- session capture and evergreen draft creation through the adapter
- explicit reviewed merge from draft content into canonical note targets
- local vault indexing and graph-backed search/related lookup through `internal/storage/obsidianindex`
  - notes
  - headings
  - wikilinks
  - aliases
  - tags
  - note chunks
  - repo paths
  - repo symbols
  - note embeddings
  - chunk embeddings
- snippet-bearing lexical search from indexed chunks
- optional second-stage semantic reranking over retrieved vault hits via the shared rerank provider path
- inbox-first repo graph draft generation:
  - root repo MOC
  - package notes with `paths` and `symbols`
  - wiki links between generated package notes and the root map
  - package selection biased toward richer repo graph packages instead of simple path order
  - related-package suggestions from imports, importers, and symbol call/reference edges

Default repo-graph layout:

- draft bundle:
  - `inbox/drafted-from-agentctl/repo-graph/<project>/`
- canonical bundle:
  - `notes/repo/<project>/`
- basic vault health scans:
  - orphans
  - dead ends
  - unresolved links
  - oversized MOCs

Default docs-bridge layout:

- draft bundle:
  - `inbox/drafted-from-agentctl/docs-bridge/<project>/`

Bridge metadata contract:

- repo docs may carry:
  - `vault_refs`
- vault notes may carry:
  - `repo_docs`

The first bridge slice is reconcile-only:

- no arbitrary two-way markdown sync
- no silent rewriting of repo docs or canonical vault prose
- machine-generated bridge drafts and backlink suggestions live in the inbox until reviewed

Current limitation:

- this is still an early adapter/index layer; it does not yet include deeper repo-graph backlinks beyond repo-index hinting or the full multi-loop external `contextd`

## Daemon Maintenance

When the daemon is started with a workspace, leader workers now include a bounded ACA maintenance loop.

Current behavior:

- refreshes top-of-mind from task and session state on a ticker
- regenerates maintenance tasks from tensions
- if `AGENTCTL_OBSIDIAN_VAULT_PATH` is set:
  - rebuilds the local Obsidian index
  - recomputes vault health
  - folds health findings into ACA maintenance tasks

Controls:

- `AGENTCTL_ACA_MAINTENANCE_INTERVAL`
  - optional duration override for the refresh ticker
- `AGENTCTL_OBSIDIAN_VAULT_PATH`
  - optional vault path for health-driven maintenance refresh
- `AGENTCTL_OBSIDIAN_SEMANTIC_ENABLED`
  - explicit override for semantic note search in ACA retrieval
  - use `false` or `0` to force lexical-only behavior
- `AGENTCTL_OPENAI_COMPAT_BASE_URL`
- `AGENTCTL_OPENAI_COMPAT_EMBEDDING_MODEL`
- `AGENTCTL_OPENAI_COMPAT_API_KEY`
  - when an OpenAI-compatible embedding endpoint is configured, ACA retrieval now enables semantic search automatically and `context retrieve` effectively defaults to blended retrieval

Recommended `agentctl` setup:

```bash
export AGENTCTL_OBSIDIAN_SEMANTIC_PROVIDER=openai_compat
export AGENTCTL_OPENAI_COMPAT_BASE_URL=http://127.0.0.1:1234/v1
export AGENTCTL_OPENAI_COMPAT_EMBEDDING_MODEL=text-embedding-embeddinggemma-300m-qat
```

With that configuration in place:

- `context retrieve` uses blended retrieval by default
- `eval retrieval` can compare baseline, lexical, semantic, and blended modes against the same configured embedding endpoint

## Workspace Layout

The current scaffold created by `agentctl orient` is:

```text
.agentctl/
  runtime/
    top_of_mind.json
    current_run.json
    contextplane.db
    queue/
      tasks.ndjson
      blocked.ndjson
    handoffs/
    sessions/
    observations.ndjson
    tensions.ndjson
    promotion_jobs.ndjson
    events.ndjson
  policy/
    retrieval.yaml
    promotion.yaml
    task_types.yaml
  exports/
    latest-orientation.md
  templates/
    obsidian-vault/
      00-home/
        index.md
        active-frontier.md
      atlas/
        projects.md
      notes/
        moc/
          project-index.md
```

The workspace-local `.agentctl/runtime/` tree is the control-plane seed. The global storage root remains the source for historical task and session records used to compute orientation.

Persistence split:

- `top_of_mind.json` and handoff JSON files remain file-backed for easy inspection
- mutable ACA collections now use `.agentctl/runtime/contextplane.db`
  - observations
  - tensions
  - promotion jobs
  - maintenance tasks
  - memory proposals
  - evidence import runs
- legacy NDJSON files remain part of the scaffold, but mutable ACA state is now loaded into the dedicated store and updated there

## Retrieval Policy Note

ACA retrieval policy lives in `.agentctl/policy/retrieval.yaml`.

One useful opt-in setting is:

```yaml
aca:
  package_note_fallback: true
```

Enable this when:

- the repo has strong canonical package-note coverage
- package/runtime/controller queries are common
- agents need deterministic ACA retrieval behavior rather than prompt-only note targeting

This helped on `praze`-style package queries, where deterministic mapping from repo paths to canonical package-note paths improved ACA retrieval. It is less necessary on `agentctl`, where the default ACA vault lane is already strong.

A concrete example policy file is available at [docs/examples/aca-retrieval-policy-package-fallback.yaml](../examples/aca-retrieval-policy-package-fallback.yaml).

## Obsidian CLI Convention

When automating a live Obsidian vault through the official CLI, prefer the documented command form:

```bash
obsidian <command> vault="Vault Name" path="folder/note.md" ...
```

Examples:

```bash
obsidian create vault="Obsidian Vault" path="agentctl-lab/example.md" content="# Example"
obsidian read vault="Obsidian Vault" path="agentctl-lab/example.md"
obsidian append vault="Obsidian Vault" path="agentctl-lab/example.md" content="\n- appended"
```

Notes from local validation on the current installer:

- The CLI works reliably against the existing registered vault.
- Newly created files can be briefly unavailable to immediate follow-up `read` or `append` calls.
- For now, automation should tolerate a short delay or retry after `create` before assuming the file is queryable.

## Top-of-Mind Contract

`top_of_mind.json` currently persists:

- `workspace_id`
- `objective`
- `phase`
- `active_task_ids`
- `hard_constraints`
- `blockers`
- `recent_decisions`
- `open_loops`
- `next_actions`
- `relevant_refs`
- `updated_at`

The bundle is computed, not manually authored. Current derivation sources are:

- active task state
- pending and blocked tasks
- recent session decisions
- recent session gotchas
- recent session key questions
- plan and scope references from the active task

## Record Growth Rules

The control plane is intended to accumulate over time as a current external database of workspace state. To keep that database queryable, repeated records are merged instead of duplicated.

Current clustering behavior:

- observations merge by normalized `statement + project + area`
- merged observations increase `count`, keep the earliest `first_seen`, refresh `last_seen`, union `evidence_refs`, and keep the highest `confidence`
- tensions merge by normalized `kind + statement + status`
- merged tensions increase `count`, preserve the earliest `created_at`, refresh `last_seen`, union `related_refs`, and keep the highest-impact severity

## Current Boundaries

This slice intentionally still stops short of:

- semantic retrieval or embeddings as a requirement for vault recall
- repo-graph-driven symbol backlinks from the code index into vault notes
- a full reviewed merge queue with human review states beyond `drafted -> reviewed_merged`
- automatic installation into external user-home hook layouts such as `~/.claude/hooks/agentctl/`
- the full external `contextd` service described in the longer proposal

The goal of the current implementation is to make the dual-plane mechanics concrete and testable before expanding into richer automation.

## Code Map

Primary implementation paths:

- `internal/contextplane/`
- `cmd/agentctl/cmd/orient.go`

Related existing sources used by the orienter:

- `internal/storage/tasks/`
- `internal/storage/sessions/`
- `internal/storage/contextbuffer/`
- `internal/context/updater/`

## Follow-On Work

Expected next increments:

1. Add repo-graph-driven path and symbol backlinks from the code index into vault ranking and note lookup.
2. Promote beyond heuristic observation capture into richer semantic clustering and deduplication.
3. Extend reviewed merge into a fuller human-review state machine for canonical notes.
4. Add a richer bootstrap path that can also install ACA wrappers into external user-home hook layouts.
