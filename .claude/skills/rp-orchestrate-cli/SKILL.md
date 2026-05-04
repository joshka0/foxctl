---
name: "rp-orchestrate-cli"
description: "Plan, decompose, and delegate complex tasks across multiple agents using rp-cli"
repoprompt_managed: true
repoprompt_skills_version: 54
repoprompt_variant: cli
---

# CLI Orchestrator (CLI)

Raw request: $ARGUMENTS

## Using rp-cli

This workflow uses **rp-cli** (RepoPrompt CLI) instead of MCP tool calls. Run commands via:

```bash
rp-cli -e '<command>'
```

**Quick reference:**

| MCP Tool | CLI Command |
|----------|-------------|
| `get_file_tree` | `rp-cli -e 'tree'` |
| `file_search` | `rp-cli -e 'search "pattern"'` |
| `get_code_structure` | `rp-cli -e 'structure path/'` |
| `read_file` | `rp-cli -e 'read path/file.swift'` |
| `manage_selection` | `rp-cli -e 'select add path/'` |
| `context_builder` | `rp-cli -e 'builder "instructions" --response-type plan'` |
| `oracle_send` | `rp-cli -e 'chat "message" --mode plan'` |
| `apply_edits` | `rp-cli -e 'call apply_edits {"path":"...","search":"...","replace":"..."}'` |
| `file_actions` | `rp-cli -e 'call file_actions {"action":"create","path":"..."}'` |

Chain commands with `&&`:
```bash
rp-cli -e 'select set src/ && context'
```

Use `rp-cli -e 'describe <tool>'` for help on a specific tool, `rp-cli --tools-schema` for machine-readable JSON schemas, or `rp-cli --help` for CLI usage.

JSON args (`-j`) accept inline JSON, file paths (`.json` auto-detected), `@file`, or `@-` (stdin). Raw newlines in strings are auto-repaired.

**⚠️ TIMEOUT WARNING:** The `builder` and `chat` commands can take several minutes to complete. When invoking rp-cli, **set your command timeout to at least 2700 seconds (45 minutes)** to avoid premature termination.

---
You are an orchestrator: **plan**, **decompose**, **delegate**. Implementation and deep context-gathering happen in sub-agents. Keep your own context lean for coordination.

## Phase 0: Workspace Verification (REQUIRED)

Before any planning, bind to the target codebase using its working directory:

```bash
# First, list available windows to find the right one
rp-cli -e 'windows'

# Then check roots in a specific window (REQUIRED - CLI cannot auto-bind)
rp-cli -w <window_id> -e 'tree --type roots'
```

**Check the output:**
- If your target root appears in a window → note the window ID and proceed to Phase 1
- If not → the codebase isn't loaded in any window

**CLI Window Routing:**
- CLI invocations are stateless—you MUST pass `-w <window_id>` to target the correct window
- Use `rp-cli -e 'windows'` to list all open windows and their workspaces
- Always include `-w <window_id>` in ALL subsequent commands

---
## Phase 1: Contextualize the Task

Translate the user's prompt into the codebase's actual nouns — concrete modules, filenames, patterns — so builder can focus immediately instead of disambiguating. 1-2 navigation calls (tree or search) is usually enough.

Example:
- Raw: *"Add retry logic to the API layer"*
- Contextualized: *"Add retry logic to `NetworkService` (HTTP wrapper) — see `APIClient` for the existing auth retry pattern."*

Shortcuts:
- **User named the file/module** → use their reference, skip the scan.
- **User provided a plan file** → read it, skip straight to Phase 2.
- **Still ambiguous after 2 calls** → dispatch a narrow explore agent with one specific question.

Keep this light — builder handles the deep reading.

```bash
rp-cli -w <window_id> -e 'tree'
rp-cli -w <window_id> -e 'search "<key term>"'
rp-cli -w <window_id> -e 'builder "<contextualized task>" --response-type plan --export'
```

If you can't disambiguate from a quick scan, dispatch a narrow explore agent first:

```bash
rp-cli -w <window_id> -e 'agent_run op=start model_id=explore session_name="Explore: <area>" message="Check <specific thing>"'
```

Explore agents are cheap — spawn multiple in parallel for different areas, but keep each prompt narrow. They tend to overthink broad instructions.

---

## Sharing the plan with sub-agents

Once you have a plan — whether generated via builder or provided by the user — you'll want sub-agents to see it. Use `export_response:true` to write any generated plan to a shareable file. This works on:
- **`context_builder`** (with `response_type: "plan"`, `"question"`, or `"review"`) — exports the generated response
- **`oracle_send`** — exports any oracle response, including follow-ups to a context_builder chat

For user-provided plan files, you already have a path — just reference it in dispatch briefs.

The tool returns `oracle_export_path` and `oracle_export_instruction`. Include `oracle_export_path` inside the `message` you send on your next `agent_run` `start` call. The `oracle_export_instruction` field is a ready-made sentence ("Read the Oracle export at `<path>` with `read_file` …") you can emit verbatim at the head of that `message`. The child agent opens the file with `read_file`. Do **not** ask child agents to continue your Oracle chat — they are in different tabs.

**The export is a shared document.** Sub-agents treat it as **read-only** context. As the orchestrator, you own this file — use it as a living checklist by updating it (via `apply_edits`) to mark items complete, note deferred work, or track progress across phases.

```bash
# Generate and export the plan, then reference the returned path in agent_run message.
rp-cli -w <window_id> -e 'builder "<task description>" --response-type plan --export'
rp-cli -w <window_id> -e 'agent_run op=start model_id=pair session_name="Orchestrate: <goal>" message="Read the plan at <plan path> with read_file first. Implement <work item>."'
```

---

## Phase 2: Decompose into Work Items

Take the plan (from `builder` or a user-provided plan file) and break it into **up to 5 discrete work items**.

For each item, note:
- **Goal**: What this item accomplishes (1-2 sentences)
- **Done when**: Concrete completion criteria — what should be true when this item is finished
- **Key files/modules**: Where the work happens
- **Dependencies**: Which other items must complete first, if any
- **Size**: Small (focused change) or large (multi-file, architectural)

Most tasks decompose into **2-3 items** — that's the sweet spot. If you're reaching for 4-5, consider whether some items can be combined. If you're beyond 5, you're decomposing too finely — raise the abstraction level.

If the task naturally decomposes into **1 item**, skip the orchestration overhead — just dispatch it directly. Don't create ceremony for simple work.

---

## Phase 3: Dispatch

### Default: fresh agent per item

For multi-item work, dispatch a **fresh agent per item**. The plan file provides continuity — each agent reads it first, sees what's already done, and reasons with a clean context budget.

The pattern is a **verify-then-dispatch-fresh loop**:

1. **Dispatch** the first work item with a self-contained brief + plan reference.
2. **Wait** for the agent to finish.
3. **Verify** against the plan — did it meet the "done when" criteria from Phase 2? A quick scan of the agent's output and, if needed, a lightweight `file_search` or `read_file` on key deliverables catches drift before it compounds.
4. **Update the plan file** to record progress so the next agent sees current state.
5. **Dispatch the next item fresh**, referencing the updated plan.

Do **not** fire-and-forget the full list. Catching drift early — before the next agent builds on a flawed foundation — is your value as the orchestrator.

```bash
# 1. Dispatch item 1 as a fresh agent
rp-cli -w <window_id> -e 'agent_run op=start model_id=pair session_name="Orchestrate 1/N: <goal>" message="Read the plan at <plan path> with read_file first. Your job is item 1: <brief>."'

# 2. Verify output, spot-check key files
rp-cli -w <window_id> -e 'read "<key file from item 1>"'

# 3. Update plan file to record progress
rp-cli -w <window_id> -e 'call apply_edits {"path":"<plan path>","search":"- [ ] Item 1:","replace":"- [x] Item 1:"}'

# 4. Dispatch item 2 as a new fresh agent
rp-cli -w <window_id> -e 'agent_run op=start model_id=pair session_name="Orchestrate 2/N: <goal>" message="Read the plan at <plan path> with read_file first. Item 1 is complete. Your job is item 2: <brief>."'
```

### When steering one agent through multiple items works better

Sometimes it's better to keep a single agent alive and steer it through work. Consider steering when:

- **Tightly coupled items** — item 2 builds directly on a decision the agent made in item 1's working memory.
- **Codex-family sub-agents** — Codex sessions compact reliably, making extended steering a natural fit.
- **Many tiny items** — spawn overhead outweighs context cost.

To check which model is powering a role:

```bash
rp-cli -w <window_id> -e 'agent_manage op=list_agents roles_only=true'
```

A role whose display name starts with `Codex CLI` (or an explicit `model_id` with a `codexExec:*` prefix) signals the role is well-suited to extended steering.

When steering, the loop is the same but step 5 becomes `agent_run op=steer` on the existing `session_id` instead of a fresh dispatch:

```bash
rp-cli -w <window_id> -e 'agent_run op=steer session_id="<session_id>" message="Item 1 looks good. Moving on to item 2: <brief>" wait=true'
```

### Choosing the right agent role

- **`pair`** — The default for complex work. Architectural decisions, multi-file changes, deep reasoning.
- **`engineer`** — Well-scoped items where the goal and approach are already clear from the plan.
- **`design`** — UI, layout, visual polish, copy/text editing, anything user-facing.
- **`explore`** — Short reconnaissance only (already used in Phase 1 escalation path).

Stick to these role labels. The specific model behind a role isn't your concern unless the user names one.

When in doubt, use `pair`. The tasks reaching this workflow are complex by nature. Use `engineer` when the plan already makes the path obvious and the item just needs execution.

When questions arise during coordination, reason through them yourself. If you're uncertain, negotiate with the agent already working on the relevant task — it has the deepest context. Steer it with your thinking and work toward consensus rather than dictating a direction.

### Writing the dispatch brief

The agents you dispatch are fully capable — they have tools, they'll read AGENTS.md and project instructions, they can explore and reason. Your job is to orient them, not direct them.

**Scope is your most important job.** When you pass a plan export, the sub-agent can see the full plan — but it doesn't know which part is its responsibility unless you say so. Always be explicit about what it should do *now* and what it should leave alone. A few patterns:

- **Paraphrase for narrow tasks**: If the work is small and self-contained, just describe it in the dispatch message. The agent doesn't need the full plan.
- **Point to a section for broader tasks**: Reference the plan path in the `message` and tell the agent which part to focus on (e.g. "Read the plan at <path> with read_file first. Your job is item 2 in the plan. Items 1 and 3 are handled separately.").
- **State the boundary**: "Do only X. Stop when X is done." is more effective than hoping the agent infers scope from context.

You can always steer additional work later, or spin up a separate agent for the next item.

**Include:** The goal, relevant file paths/modules, and discoveries from planning that the agent wouldn't find on its own. If a separate user plan file exists, point to the relevant section. For small tasks, tell the agent to skip oracle review.

**Don't include:** Project conventions already in CLAUDE.md, step-by-step instructions, or code snippets the agent can read itself.

**Pass forward discoveries, not instructions.**

### Parallel dispatch

If dispatching independent items as fresh agents concurrently, **each agent's brief must mention the sibling**:

> "Another agent is concurrently working on <brief description of sibling task> in <modules>. Avoid modifying files in that area. If you find yourself blocked by or conflicting with that work, stop and report back rather than pushing through."

**Use `detach: true`** when dispatching concurrent items — otherwise the orchestrator blocks on the first agent and can't start the second.

Then pass `session_ids` (array) to `agent_run op=wait` to block until the **first** session finishes or needs input. The response tells you which session won and which are still pending.

```bash
# Dispatch both concurrently
rp-cli -w <window_id> -e 'agent_run op=start model_id=pair session_name="1/N: <goal A>" message="<brief A>" detach=true'
rp-cli -w <window_id> -e 'agent_run op=start model_id=pair session_name="2/N: <goal B>" message="<brief B>" detach=true'

# Then wait for the first session that needs attention
rp-cli -w <window_id> -e 'agent_run op=wait session_ids=["<uuid1>","<uuid2>"] timeout=60'

# Or poll all current snapshots without blocking
rp-cli -w <window_id> -e 'agent_run op=poll session_ids=["<uuid1>","<uuid2>"]'
```

Handle the finished agent, then wait again on the remaining `pending_session_ids`. While waiting, summarize completed work or prepare the next brief — be a pipeline, not a sequential loop.

### Housekeeping

Sessions persist after agents finish — useful when you might revisit output, but they pile up over a multi-agent workflow. Once you've recorded what an agent produced, you can dismiss its session:

```bash
rp-cli -w <window_id> -e 'agent_manage op=cleanup_sessions session_ids=["<session_id>"]'
```

Explore-agent sessions are good to dismiss right away — narrow reconnaissance, no follow-up value. Keep heavier agent sessions if you might revisit them.

Plan and review exports generated during orchestration (via `export_response:true` on `builder` or `chat`) accumulate under `prompt-exports/` as files like `oracle-plan-<date>-<slug>.md` or `oracle-review-<date>-<slug>.md`. Once an export has been superseded by a newer plan, consumed by the sub-agent it was meant for, or otherwise made irrelevant by completed work, delete it so the folder reflects only live, in-progress plans. When unsure, leave it.

```bash
rp-cli -w <window_id> -e 'call file_actions {"action":"delete","path":"prompt-exports/<stale-export>.md"}'
```

---

## Phase 4: Monitor and Verify

You own the plan. It's your job to ensure each phase respected it.

As each agent completes:

1. **Verify against the plan.** Check the agent's output against the "done when" criteria from the plan. Don't just skim — confirm the goal was actually met. A quick `read_file` or `file_search` on key deliverables costs little and catches drift before it compounds. If the plan said "add error handling to all three endpoints" and the agent only touched two, that's your catch. Mark the item as done (or note gaps) in the export file so you have a running record.
2. **If something's off**, steer a correction before moving on — never proceed with unresolved gaps:
```bash
rp-cli -w <window_id> -e 'agent_run op=steer session_id="<session_id>" message="The goal was X but Y appears missing." wait=true'
```
3. **Summarize to the user**: Brief status update — what completed, what's still running.

After all items complete, give the user a **final rollup**:
- What was accomplished per item
- Any failures or partial completions
- Any conflicts or coordination issues that surfaced
- Suggested follow-ups if anything was deferred

### Quick reference: orchestrator operations

| Operation | Tool call |
|-----------|-----------|
| Start a fresh agent | `agent_run op=start model_id=<role> session_name="..." message="..." detach=true/false` |
| Steer an existing agent | `agent_run op=steer session_id="..." message="..." wait=true` |
| Wait for an agent | `agent_run op=wait session_id="..."` |
| Wait for first of multiple agents | `agent_run op=wait session_ids=["...", "..."] timeout=60` |
| Poll without blocking | `agent_run op=poll session_id="..."` |
| Poll multiple agents | `agent_run op=poll session_ids=["...", "..."]` |
| Dismiss a completed session | `agent_manage op=cleanup_sessions session_ids=["..."]` |
| Read plan/context | `read_file`, `get_file_tree`, `file_search` |
| Reason with oracle | `oracle_send` — requires file selection from `builder` |

---

## Key Principles

- **You are the coordinator, not the implementer.** Read to verify sub-agent work, not to build your own mental model. Keep your context focused on coordination.
- **Trust the agents.** They're smart, they have tools, they read project instructions. Give them goals and reference points, not turn-by-turn directions.
- **Be strategic about parallelism.** Independent items can run concurrently, but always warn agents about siblings working in adjacent areas.
- **Graceful scaling.** 1 item = just dispatch it. 2-3 items = straightforward. 4-5 items = be deliberate about dependencies and parallelism.
- **Escalation point.** You're the one with the full picture. Sub-agents should surface coordination problems to you rather than solving them unilaterally.

## Anti-patterns

- 🚫 Implementing code yourself — you're the orchestrator, dispatch an agent
- 🚫 Skipping Phase 0 (Workspace Verification) — you must confirm the target codebase is loaded first
- 🚫 Extended code reading before delegating — a quick skim is fine; deep reads belong in builder or explore agents
- 🚫 Writing detailed step-by-step instructions for dispatched agents — they can reason for themselves
- 🚫 Dispatching parallel agents to overlapping files without warning them about each other
- 🚫 Waiting idle for an agent when you could be dispatching the next independent item or preparing the next brief
- 🚫 Forgetting to check on dispatched agents — they may block on permission approvals; poll periodically to keep them unblocked
- 🚫 Creating 5 work items when the task is naturally 2 — decompose to the right granularity, not a target number
- 🚫 Repeating project conventions from CLAUDE.md in dispatch briefs — the agents will read those themselves