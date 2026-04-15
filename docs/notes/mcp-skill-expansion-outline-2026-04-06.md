# MCP Skill Expansion Outline

Date: 2026-04-06

## Current Narrow MCP Surface

The current `--optimized-retrieval` MCP profile is intentionally small:

- `structured_shell`
- `repo_index_build`
- `repo_index_status`
- `repo_index_search`
- `repo_index_expand`
- `repo_index_dag_grep`
- `code/semantic_search`
- `code/smart_search`
- `code/symbols`
- `code/snippet_extract`
- `code/context_grep`
- `code/dag_grep`
- `codemap/get`
- `code/refactor_scout`
- `context_show`

This is the right default because these tools now support compact `inline_mode`
behavior, preview-first output, and artifact-backed truncation.

## Expansion Principles

Add new MCP tools only when they satisfy all of these:

1. They are high-signal for agent workflows.
2. They have bounded, structured outputs.
3. They do not duplicate existing MCP tools too closely.
4. They are useful without requiring broad product-specific context.
5. They do not flood agents with low-value choices.

## Tier 1 Candidates

These were the strongest next additions and are now exposed in the narrow MCP profile:

### `code/symbols`

Why:
- High-signal structural lookup.
- Strong companion to `context_grep` and `repo_index_search`.
- Good for “what symbols exist here?” queries before deeper expansion.

Use cases:
- Find exported functions or methods by name.
- Inspect API shape without opening full files.
- Support symbol-aware planning before edits.

### `code/refactor_scout`

Why:
- Already tuned as a local-first retrieval/ranking tool.
- Good MCP fit because it returns compact ranked findings.
- Strong for refactoring/planning assistants.

Use cases:
- Hotspot discovery.
- Complexity triage.
- Candidate ranking before second-stage judgment.

### `context_show`

Why:
- Useful for task continuity and current workspace orientation.
- Small, structured output.
- Helps external agents know what context plane state already exists.

Use cases:
- Determine current retrieval state.
- Inspect workspace context before running broader search.

### `repo_index_build` / `repo_index_status`

Why:
- Reindexing is part of keeping the compact graph tools useful.
- A narrow MCP profile still needs a way to refresh and inspect repo index state.

Use cases:
- Refresh graph data after major code movement.
- Diagnose stale repo-index results without using the full MCP surface.

## Tier 2 Candidates

These are good additions, but should come after Tier 1.

### `context_retrieve`

Why:
- Strong retrieval tool, but more context-system-specific.
- Valuable when an MCP client is expected to interact with ACA/Obsidian state.

Risk:
- More concept-heavy than the narrow retrieval tools.

### `context_report`

Why:
- Helpful for summarizing context state or handoff readiness.
- Good for coordination-oriented clients.

Risk:
- More useful for agent orchestration than simple code research.

### `refactor_advisor`

Why:
- Strong second-stage ranking tool over `refactor_scout`.
- Good if we want MCP clients to stay entirely local-first for refactor workflows.

Risk:
- Pulls in model/provider concerns depending on configuration.

## Tier 3 Candidates

These are useful, but more specialized.

### `context_task_history_summary`

Why:
- Helpful for continuity and prior-task recall.

Risk:
- More niche than code/repo retrieval.

### `obsidian_read` / `obsidian_related`

Why:
- Good when MCP clients need durable knowledge notes.

Risk:
- Repo-external knowledge can widen the tool surface quickly.

### `code/refactor_advisor`

Why:
- Valuable for refactoring flows.

Risk:
- Better added only after `refactor_scout` proves stable via MCP clients.

## Not Recommended For The Narrow Profile

Avoid adding these to the default narrow MCP set:

- broad web/browser tools
- generic `foxctl_run`
- write/edit tools
- task mutation tools
- agent lifecycle tools
- mailbox or room coordination tools

These are useful, but not for the “small, optimized retrieval surface” goal.

## Recommended Next Steps

1. Add `code/symbols` to the optimized MCP profile.
2. Add `code/refactor_scout` as the first planning/refactor addition.
3. Add `context_show` if we want a minimal context-plane bridge.
4. Re-evaluate after real MCP usage before adding Tier 2 tools.
