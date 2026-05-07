---
name: pi-maestri-live
description: "Live-test the foxctl Pi extension through a Maestri shell: edit foxctl-pi, reload or restart interactive Pi, and exercise foxctl tools with maestri ask."
---

# pi-maestri-live

Use this skill when working on the `foxctl-pi` extension with a live interactive Pi session in Maestri.

The goal is a fast loop:

1. edit `/Users/joshka/repos/personal/foxctl-pi/foxctl.ts`
2. typecheck from `/Users/joshka/repos/githubs/pi-mono`
3. reload or restart Pi in the active Maestri shell
4. use `maestri ask "<shell-name>" ...` to make the Pi model exercise the extension tools

## Working Paths

- Pi repo: `/Users/joshka/repos/githubs/pi-mono`
- Extension source: `/Users/joshka/repos/personal/foxctl-pi/foxctl.ts`
- Pi project symlink: `/Users/joshka/repos/githubs/pi-mono/.pi/extensions/foxctl.ts`
- foxctl daemon URL: `http://localhost:8090`
- foxctl terminal gateway URL: `http://localhost:8765`
- OpenRouter env source: `/Users/joshka/.foxctl/.env`

## Start Or Inspect The Maestri Shell

Always begin with:

```bash
maestri list
maestri check "Shell"
```

Use the connected shell name from `maestri list`; current local sessions may be
named `Shell` instead of the older `Shell #6`.

If Pi is already running, prefer `/reload` after extension-only edits:

```bash
maestri ask "Shell #6" "/reload"
```

Important: `/reload` reloads extension code, skills, prompts, themes, and keybindings, but it does not broaden the original `--tools` allowlist. If you add tool names, restart Pi with an expanded allowlist.

## Start Interactive Pi

Use a free OpenRouter model and source the key without printing it:

```bash
maestri ask "Shell #6" 'cd /Users/joshka/repos/githubs/pi-mono && set -a && source /Users/joshka/.foxctl/.env && set +a && ./node_modules/.bin/pi --provider openrouter --model poolside/laguna-xs.2:free --thinking off --foxctl-url http://localhost:8090 --foxctl-gateway-url http://localhost:8765 --foxctl-workspace /Users/joshka/repos/personal/foxctl --foxctl-room pi-maestri-interactive --foxctl-actor actor:pi:maestri --foxctl-session pi:maestri:shell-6 --foxctl-room-bind --foxctl-context --tools foxctl_health,gather_context,foxctl_gather_context,foxctl_context,foxctl_tool_run,foxctl_fs_list,foxctl_fs_read,foxctl_filesystem_read,foxctl_fs_find,foxctl_text_grep,foxctl_code_search,foxctl_code_semantic_search,foxctl_code_context_grep,foxctl_repoindex_search,foxctl_repoindex_dag,foxctl_repoindex_expand,foxctl_repoindex_open,foxctl_refactor_scout,foxctl_refactor_plan,foxctl_memory_query,foxctl_memory_search,foxctl_session_recall,foxctl_room_status,foxctl_room_inbox,foxctl_room_bind_pi,foxctl_room_terminal_links,foxctl_room_terminal_register,foxctl_room_tasks,foxctl_room_task_create,foxctl_room_task_action,foxctl_room_message_ack,foxctl_room_messages_resolve,foxctl_room_loop,foxctl_foxprox_room_sessions,foxctl_foxprox_room_message,foxctl_foxprox_spawn,foxctl_foxprox_stop_session'
```

Replace `Shell #6` with the active shell name reported by `maestri list` when
needed.

After launch, send an empty ask or check the shell to confirm the TUI is active:

```bash
maestri ask "Shell #6" ""
maestri check "Shell #6"
```

Expected footer includes:

```text
foxctl 0.1.0 libsql room:pi-maestri-interactive
```

## Edit And Verify

Use `apply_patch` for edits. Then typecheck through Pi's local toolchain:

```bash
cd /Users/joshka/repos/githubs/pi-mono
./node_modules/.bin/tsgo --ignoreConfig --noEmit --module NodeNext --moduleResolution NodeNext --target ES2022 --strict --skipLibCheck .pi/extensions/foxctl.ts
```

For direct extension import smokes, set `NODE_PATH` because `.pi/extensions/foxctl.ts` is a symlink outside `pi-mono`:

```bash
NODE_PATH=/Users/joshka/repos/githubs/pi-mono/node_modules ./node_modules/.bin/tsx --eval '/* import smoke here */'
```

## Live Model Testing

Use `maestri ask` to make the Pi model call tools, not shell:

```bash
maestri ask "Shell #6" "Please call gather_context exactly once. Then summarize workspace, room, and inbox count from its result. Do not use bash."
```

Useful prompts:

```text
Use only foxctl extension tools, not bash. Validate the workspace-scoped foxctl wrappers by calling exactly these read-only tools once each if available: foxctl_code_search, foxctl_repoindex_search, foxctl_memory_search, foxctl_filesystem_read, foxctl_refactor_plan. Use workspace /Users/joshka/repos/personal/foxctl. Search for "Pi extension wrappers". For refactor, use read-only mode only. Summarize tool availability, one concrete result from each successful call, and any failure message.
```

```text
Call foxctl_fs_list for internal/interfaces/web/api, foxctl_fs_read for internal/interfaces/web/api/skill_runner.go, and foxctl_text_grep for "workspace_root" under internal/interfaces/web/api. Do not use bash. Summarize the file count, the read file title/path, and the strongest grep hit.
```

```text
Call foxctl_code_search for "Pi skill wrapper workspace root" and foxctl_code_context_grep for "skillWorkspaceRootFromInput". Do not use bash. Summarize the function or file each tool surfaced and whether the results agree.
```

```text
Call foxctl_repoindex_search for "semantic anchors Pi integration" and then foxctl_repoindex_dag with render tree for the strongest query you can form from the result. Do not use bash. If repoindex is stale or unavailable, report the exact error and stop.
```

```text
Use only foxctl extension tools, not bash. Call foxctl_tool_run with command rlm/query and input {"workspace":"/Users/joshka/repos/personal/foxctl","workspace_root":"/Users/joshka/repos/personal/foxctl","prompt":"First call load_evidence_ref with ref path:internal/interfaces/web/api/skill_runner.go. Then identify the function skillWorkspaceRootFromInput and the caller SkillRunner.Run in that loaded file. Return only file path, function names, and evidence ref. Do not search skill.yaml.","executor":"llm","llm_provider":"openrouter","llm_model":"poolside/laguna-xs.2:free","route_profile":"code_retrieval","tool_profile":"code-intel","plan_mode":"free","max_iterations":4,"max_depth":1,"max_subcalls":2}. Then summarize ok/status, answer, tool_names, retrieved_paths, and parent_total_tokens.
```

```text
Use foxctl_room_tasks and foxctl_room_loop for your configured room. Do not use bash. Tell me whether each tool is available and summarize task count plus loop enabled/managed_by.
```

```text
Call foxctl_room_terminal_links for your configured room. Summarize the browser URL, WebSocket URL, and the binary/text frame protocol. Do not claim this is a durable workbench attachment.
```

```text
Use your available foxctl tools to propose the next five Pi-facing tool names or aliases. Prefer user workflows over raw endpoint coverage.
```

```text
Call foxctl_room_inbox for your configured room. If there are actionable entries, explain which message IDs should be acknowledged or resolved, but do not mutate anything yet.
```

## Known Gotchas

- `maestri ask "Shell #6" "plain English"` sends text into the terminal. If Shell #6 is at a shell prompt, English becomes a shell command. Start Pi first or send real shell commands.
- If the Pi TUI is running, `maestri ask` sends text to Pi as user input.
- If a tool was added after Pi started and is not in `--tools`, Pi will say it is unavailable even after `/reload`. Restart with the expanded allowlist.
- Use `foxctl_tool_run` as an escape hatch for OpenAPI-enabled skills only; prefer focused read-only wrappers for normal live tests.
- Repoindex wrapper tests require a current repo graph index for the target workspace.
- `--foxctl-context` injects workspace/task/room context before each Pi agent turn. Leave `--foxctl-memory-context=true` when dogfooding memory; it adds prompt-keyed `memory/query` and `session/recall` evidence and degrades to error objects if the memory backend is unavailable.
- Poolside `poolside/laguna-xs.2:free` has worked for tool-call smoke tests through OpenRouter.
- Direct Node/tsx smokes need `NODE_PATH=/Users/joshka/repos/githubs/pi-mono/node_modules` because the extension source lives outside the repo.
- `foxctl_room_bind_pi` must not send role changes in binding updates; foxctl only allows coordinators to change member roles.

## Current Validated Tools

The live Pi loop has successfully exercised:

- `foxctl_health`
- `gather_context`
- `foxctl_room_status`
- `foxctl_room_inbox`
- `foxctl_room_bind_pi`
- `foxctl_room_terminal_links`
- `foxctl_room_terminal_register`
- `foxctl_room_tasks`
- `foxctl_room_loop`
- `foxctl_foxprox_room_sessions`
- `foxctl_tool_run` with OpenAPI skill `rlm/query`

The direct registration smoke confirmed:

- `foxctl_tool_run`
- `foxctl_fs_list`
- `foxctl_fs_read`
- `foxctl_filesystem_read`
- `foxctl_fs_find`
- `foxctl_text_grep`
- `foxctl_code_search`
- `foxctl_code_semantic_search`
- `foxctl_code_context_grep`
- `foxctl_repoindex_search`
- `foxctl_repoindex_dag`
- `foxctl_repoindex_expand`
- `foxctl_repoindex_open`
- `foxctl_refactor_scout`
- `foxctl_refactor_plan`
- `foxctl_memory_query`
- `foxctl_memory_search`
- `foxctl_session_recall`
- `foxctl_room_task_create`
- `foxctl_room_task_action`
- `foxctl_room_message_ack`
- `foxctl_room_messages_resolve`
