---
name: pi-maestri-live
description: "Live-test the foxctl Pi extension through Maestri Shell #6: edit foxctl-pi, reload or restart interactive Pi, and exercise foxctl tools with maestri ask."
---

# pi-maestri-live

Use this skill when working on the `foxctl-pi` extension with a live interactive Pi session in Maestri.

The goal is a fast loop:

1. edit `/Users/joshka/repos/personal/foxctl-pi/foxctl.ts`
2. typecheck from `/Users/joshka/repos/githubs/pi-mono`
3. reload or restart Pi in Maestri Shell #6
4. use `maestri ask "Shell #6" ...` to make the Pi model exercise the extension tools

## Working Paths

- Pi repo: `/Users/joshka/repos/githubs/pi-mono`
- Extension source: `/Users/joshka/repos/personal/foxctl-pi/foxctl.ts`
- Pi project symlink: `/Users/joshka/repos/githubs/pi-mono/.pi/extensions/foxctl.ts`
- foxctl daemon URL: `http://localhost:8090`
- OpenRouter env source: `/Users/joshka/.foxctl/.env`

## Start Or Inspect Shell #6

Always begin with:

```bash
maestri list
maestri check "Shell #6"
```

If Pi is already running, prefer `/reload` after extension-only edits:

```bash
maestri ask "Shell #6" "/reload"
```

Important: `/reload` reloads extension code, skills, prompts, themes, and keybindings, but it does not broaden the original `--tools` allowlist. If you add tool names, restart Pi with an expanded allowlist.

## Start Interactive Pi

Use a free OpenRouter model and source the key without printing it:

```bash
maestri ask "Shell #6" 'cd /Users/joshka/repos/githubs/pi-mono && set -a && source /Users/joshka/.foxctl/.env && set +a && ./node_modules/.bin/pi --provider openrouter --model poolside/laguna-xs.2:free --thinking off --foxctl-url http://localhost:8090 --foxctl-workspace /Users/joshka/repos/personal/foxctl --foxctl-room pi-maestri-interactive --foxctl-actor actor:pi:maestri --foxctl-session pi:maestri:shell-6 --foxctl-room-bind --foxctl-context --tools foxctl_health,gather_context,foxctl_gather_context,foxctl_context,foxctl_room_status,foxctl_room_inbox,foxctl_room_bind_pi,foxctl_room_tasks,foxctl_room_task_create,foxctl_room_task_action,foxctl_room_message_ack,foxctl_room_messages_resolve,foxctl_room_loop,foxctl_foxprox_room_sessions,foxctl_foxprox_room_message,foxctl_foxprox_spawn,foxctl_foxprox_stop_session'
```

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
Use foxctl_room_tasks and foxctl_room_loop for your configured room. Do not use bash. Tell me whether each tool is available and summarize task count plus loop enabled/managed_by.
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
- `foxctl_room_tasks`
- `foxctl_room_loop`
- `foxctl_foxprox_room_sessions`

The direct registration smoke confirmed:

- `foxctl_room_task_create`
- `foxctl_room_task_action`
- `foxctl_room_message_ack`
- `foxctl_room_messages_resolve`

