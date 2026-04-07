# MCP Health Matrix — 2026-04-06

This note captures the current health of the focused `agentctl_*` MCP profiles after the retrieval, collaboration, and mobile debugging work.

## Summary

| MCP profile | Status | Notes |
|---|---|---|
| `agentctl_retrieval` | healthy | Strongest surface. Optimized outputs, preview-first retrieval, structured shell, repo graph, semantic/context tools. |
| `agentctl_context` | healthy | Context-plane and session tools are callable and useful. |
| `agentctl_refactor` | healthy | Refactor scout and related code-intel tools are usable. |
| `agentctl_room` | healthy | Durable room coordination works. |
| `agentctl_mux` | healthy | tmux/zellij collaboration surface works. |
| `agentctl_mobile` | healthy with environment limits | `mobile_ios` and `mobile_expo` are useful now; richer iOS UI paths still depend on `idb`. |
| `agentctl_godot` | environment-bound | Requires a Godot project, export presets, and a running Godot bridge/editor where applicable. |
| `agentctl_api` | environment-bound | Wrapper works; requires a real OpenAPI spec path or URL. |

## Healthy Profiles

### `agentctl_retrieval`

Current strengths:

- `structured_shell` works and returns compact command-aware output.
- Repo graph tools work:
  - `repo_index_search`
  - `repo_index_expand`
  - `repo_index_dag_grep`
  - `repo_index_build`
  - `repo_index_status`
- Optimized code retrieval tools are in place:
  - `code_context_grep`
  - `code_dag_grep`
  - `code_semantic_search`
  - `code_smart_search`
  - `code_snippet_extract`
  - `codemap_get`

Status:

- Best overall MCP profile.
- Good default for repo research and code retrieval.

### `agentctl_context`

Current strengths:

- Context-plane read/query tools are working.
- Useful for session continuity and context inspection.

Status:

- Stable and practical.

### `agentctl_refactor`

Current strengths:

- `code/refactor_scout`
- `code/symbols`
- `code/context_grep`
- `code/snippet_extract`
- `code/semantic_search`

Status:

- Strong enough for guided refactor discovery and planning.

### `agentctl_room`

Current strengths:

- `room`
- `room_task`
- `agent_room`

Status:

- Healthy command-backed collaboration surface.

### `agentctl_mux`

Current strengths:

- `mux`

Status:

- Healthy command-backed live terminal collaboration surface.

## Mobile Profile

### `agentctl_mobile`

Current strengths:

- `mobile_ios`
  - `simctl`-first on `iOS 26.4` for core simulator actions
  - device listing works
  - screenshot capture works through MCP
- `mobile_expo`
  - `debug_status`
  - `debug_snapshot`
  - Expo dev-menu actions
  - Metro-aware status via process/log inspection

Current limits:

- `idb` is currently unavailable on this machine due to a broken shim, and the code now correctly treats it as unavailable.
- Because of that, richer iOS paths that still depend on `idb` are unavailable:
  - UI tree
  - some interaction/debugging actions

Status:

- Good and now genuinely useful.
- Not yet a full debugger replacement.

## Environment-Bound Profiles

### `agentctl_godot`

Current limits:

- Needs an actual Godot project.
- Build/export paths need `export_presets.cfg`.
- Editor bridge paths need a running GodotAIBridge session.

Status:

- Correctly wired, but only useful when the Godot environment is present.

### `agentctl_api`

Current limits:

- Requires a real OpenAPI spec path or URL.

Status:

- Correctly wired, but environment/input dependent.

## Main Remaining Gaps

1. Restore a working `idb` installation or replacement path.
2. Improve Metro collection beyond heuristic process/log detection.
3. Add stronger Hermes / React Native DevTools awareness if we want deeper RN debugging support.

## Recommended Defaults

- Use `agentctl_retrieval` as the default repo-research MCP.
- Use `agentctl_mobile` for simulator/debug workflows.
- Use `agentctl_room` and `agentctl_mux` for collaboration.
- Treat `agentctl_godot` and `agentctl_api` as opt-in, environment-bound surfaces.
