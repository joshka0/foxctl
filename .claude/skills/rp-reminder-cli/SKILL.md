---
name: "rp-reminder-cli"
description: "Reminder to use rp-cli"
repoprompt_managed: true
repoprompt_skills_version: 54
repoprompt_variant: cli
---

# RepoPrompt Tools Reminder (CLI)

Continue your current workflow using rp-cli instead of built-in alternatives.

## File & Code

| Task | Use | Not |
|------|-----|-----|
| Search paths/content | `search` | grep, find, Glob |
| Read file (whole or sliced) | `read` | cat, head, Read |
| Directory tree | `tree` | ls, find |
| Signatures / overview | `structure` | reading whole files |
| Edit file | `edit` | sed, Edit |
| Create / delete / move | `file` | touch, rm, mv, Write |
| Git status / diff / log / blame | `git` | shelling out for analysis |

## Context & Planning

| Tool | Use for |
|------|---------|
| `manage_selection` | Curate the file set used by chat, builder, and exports. Refresh before each planning call. Modes: `full`, `slices`, `codemap_only`. |
| `workspace_context` | Snapshot current prompt + selection + token budget; also exports. |
| `prompt` | Read/set the shared prompt; list or select copy presets. |
| `context_builder` | Heavy discovery sub-agent — describe the task, it curates files + rewrites the prompt. `response_type`: `clarify` / `plan` / `question` / `review`. Pass `export_response:true` to hand the result to a child agent. |
| `chat` (`ask_oracle`) | Chat-mode reasoning over the current selection. Continue existing chats (`new_chat:false`) rather than opening new ones. Modes: `chat` / `plan` / `review`. |
| `oracle_chat_log` | Recover recent Oracle messages after compaction. |
| `ask_user` | Ask the user when ambiguity is load-bearing — don't guess at requirements. |

## Agent Delegation — `agent_run` / `agent_manage`

Dispatch a sub-agent when a side investigation or delegated chunk of work would otherwise flood this session's context.

**Role labels** (pass as `model_id` on `agent_run op=start`):

| Role | Use for |
|------|---------|
| `explore` | Fast **read-only** probes — git archaeology, "where is X wired?", narrow lookups, web/doc search. One question per probe. |
| `engineer` | Balanced implementation work delegated to a child agent. |
| `pair` | Multi-step reasoning with back-and-forth — lead investigator or main implementer of a decomposed item. |
| `design` | Architecture / review / extended analysis — primary deliverable is a markdown report under `docs/reviews/`, `docs/designs/`, or `docs/analysis/`. Expect the report path in the summary. |

**Key `agent_run` ops:** `start` (creates a new session/tab — never pass `session_id` here), `wait` / `poll` (accept `session_id` **or** `session_ids` array), `steer` (continue an existing session), `respond` (answer a pending `interaction_id`), `cancel`.

**Key `agent_manage` ops:** `list_agents` (discover roles + compound model_ids), `list_sessions`, `get_log`, `cleanup_sessions` (delete finished MCP-started sessions).

**Fan-out pattern:** call `agent_run op=start` with `detach:true` for each probe, then `agent_run op=wait session_ids=[…]` to block on the batch. Always follow a `detach` with a `wait` — don't leave probes unattended.

**Export handoff:** when `context_builder` or `ask_oracle` returns `oracle_export_path`, include that path inside the child agent's next `message` so it reads the export with `read_file`.

## Quick Reference

```bash
# Search · Read · Edit · File ops
rp-cli -w <window_id> -e 'search "keyword"'
rp-cli -w <window_id> -e 'read Root/file.swift --start-line 50 --limit 30'
rp-cli -w <window_id> -e 'call apply_edits {"path":"Root/file.swift","search":"old","replace":"new"}'
rp-cli -w <window_id> -e 'file create Root/new.swift "content..."'

# Selection · Builder · Oracle
rp-cli -w <window_id> -e 'select add Root/path/file.swift'
rp-cli -w <window_id> -e 'builder "<task>" --response-type plan'
rp-cli -w <window_id> -e 'chat "..." --mode plan'

# Delegate · Fan-out · Steer · Cleanup
rp-cli -w <window_id> -e 'agent_run op=start model_id=explore session_name="Probe: X" message="<question>" detach=true'
rp-cli -w <window_id> -e 'agent_run op=wait session_ids=["<uuid1>","<uuid2>"] timeout=60'
rp-cli -w <window_id> -e 'agent_run op=steer session_id="<uuid>" message="now do Y" wait=true'
rp-cli -w <window_id> -e 'agent_manage op=cleanup_sessions session_ids=["<uuid>"]'
```

Continue with your task using these tools.