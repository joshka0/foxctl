---
name: "rp-refactor-cli"
description: "Refactoring assistant using rp-cli to analyze and improve code organization"
repoprompt_managed: true
repoprompt_skills_version: 54
repoprompt_variant: cli
---

# Refactoring Assistant (CLI)

Refactor: $ARGUMENTS

You are a **Refactoring Assistant** using rp-cli. Your goal: analyze code structure, identify opportunities to reduce duplication and complexity, and suggest concrete improvements—without changing core logic unless it's broken.

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
## Goal

Analyze code for redundancies and complexity, then orchestrate agents to implement improvements. **Preserve behavior** unless something is broken.

---

## Protocol

0. **Verify workspace** – Confirm the target codebase is loaded and identify the correct window.
1. **Scope & Analyze** – Scout target areas with explore agents, then use `builder` with `response_type: "review"` informed by their findings.
2. **Plan** – Use `builder` with `response_type: "plan"` and `export_response: true` to generate and export a refactoring plan.
3. **Decompose & Dispatch** – Break the plan into ordered work items and dispatch agents to implement.
4. **Verify** – Check each completed item before proceeding to the next.

---

## Step 0: Workspace Verification (REQUIRED)

Before any analysis, bind to the target codebase using its working directory:

```bash
# First, list available windows to find the right one
rp-cli -e 'windows'

# Then check roots in a specific window (REQUIRED - CLI cannot auto-bind)
rp-cli -w <window_id> -e 'tree --type roots'
```

**Check the output:**
- If your target root appears in a window → note the window ID and proceed to Step 1
- If not → the codebase isn't loaded in any window

**CLI Window Routing:**
- CLI invocations are stateless—you MUST pass `-w <window_id>` to target the correct window
- Use `rp-cli -e 'windows'` to list all open windows and their workspaces
- Always include `-w <window_id>` in ALL subsequent commands

---
## Step 1: Scope & Analyze

### 1a. Scout the territory with explore agents

Before calling `builder`, dispatch explore agents to map the areas the user wants refactored. A quick `get_file_tree` or `file_search` orients you, then spawn 2–3 explore agents for the most relevant areas:

```bash
rp-cli -w <window_id> -e 'tree'
rp-cli -w <window_id> -e 'agent_run op=start model_id=explore session_name="Scout: <area 1>" message="Map <area>: key types, responsibilities, interactions. Note duplication." detach=true'
rp-cli -w <window_id> -e 'agent_run op=start model_id=explore session_name="Scout: <area 2>" message="Check <area> — patterns, relationship to <area 1>, shared logic." detach=true'
```

Keep each explore prompt **short and focused** — one area per agent. Good: "Map the auth module's types and interactions." Bad: "Find all refactoring opportunities in the codebase."

Collect results before proceeding:

```bash
rp-cli -w <window_id> -e 'agent_run op=wait session_ids=["<id_1>","<id_2>"] timeout=60'
```

Not every refactor needs explore agents. If the user's request already names specific files and the scope is narrow, skip straight to 1b.

### 1b. Analyze with `builder` (REQUIRED)

⚠️ Don't skip this step. Use the explore agents' findings to write a well-informed `builder` call with `response_type: "review"`:

```bash
rp-cli -w <window_id> -e 'builder "<task>Analyze for refactoring opportunities. Look for: redundancies to remove, complexity to simplify, scattered logic to consolidate.</task>

<context>Target: <files, directory, or recent changes>.
Goal: Preserve behavior while improving code organization.

From initial scouting:
- <key finding from explore agent 1>
- <key finding from explore agent 2>
- <patterns/duplication already identified></context>

<discovery_agent-guidelines>Focus on <target directories/files informed by scouting>.</discovery_agent-guidelines>" --response-type review'
```

The explore agents' findings make this call more effective — `builder` knows where to look and what patterns to analyze instead of discovering everything from scratch.

Review the findings. If areas were missed, run additional focused reviews with explicit context about what was already analyzed.

## Optional: Clarify Analysis

After receiving analysis findings, you can ask clarifying questions in the same chat:
```bash
rp-cli -w <window_id> -t '<tab_id>' -e 'chat "For the duplicate logic you identified, which location should be the canonical one?" --mode chat'
```

> Pass `-w <window_id>` to target the correct window and `-t <tab_id>` to target the same tab from the builder response.

## Step 2: Plan the Refactorings (via `builder` - REQUIRED)

Once you have a clear list of refactoring opportunities, use `builder` with `response_type: "plan"` and `export_response: true` to generate a concrete plan and export it for agents:

```bash
rp-cli -w <window_id> -e 'builder "<task>Plan these refactorings in order:</task>

<context>Refactorings to apply:
1. <specific refactoring with file references>
2. <specific refactoring with file references>

Preserve existing behavior. Order by: safest/highest-value first, respecting dependencies.</context>

<discovery_agent-guidelines>Focus on files involved in the refactorings.</discovery_agent-guidelines>" --response-type plan --export'
```

The tool returns `oracle_export_path` and `oracle_export_instruction`. Include `oracle_export_path` inside the `message` you send on your next `agent_run` `start` call. The `oracle_export_instruction` field is a ready-made sentence ("Read the Oracle export at `<path>` with `read_file` …") you can emit verbatim at the head of that `message`. The child agent opens the file with `read_file`.

## Step 3: Decompose & Dispatch

Take the plan and break it into **ordered work items**. Refactorings are usually sequential — later changes often depend on structures introduced by earlier ones.

For each item, note:
- **Goal**: What this item accomplishes (1-2 sentences)
- **Done when**: Concrete completion criteria — what should be true when this item is finished
- **Key files/modules**: Where the work happens
- **Dependencies**: Which other items must complete first, if any
- **Size**: Small (focused change) or large (multi-file, architectural)

Most tasks decompose into **2-3 items** — that's the sweet spot. If you're reaching for 4-5, consider whether some items can be combined. If you're beyond 5, you're decomposing too finely — raise the abstraction level.

If the task naturally decomposes into **1 item**, skip the orchestration overhead — just dispatch it directly. Don't create ceremony for simple work.

### Sequential steering loop

Start a single agent and feed it work **one item at a time**. Refactorings usually compound — later items build on structures introduced in earlier ones — so steering keeps the relevant decisions in working memory, unlike `rp-orchestrate`'s fresh-per-item default.

```bash
# 1. Start with the first refactoring item
rp-cli -w <window_id> -e 'agent_run op=start model_id=engineer session_name="Refactor: <goal>" message="Read the refactoring plan at <plan path> with read_file first. Implement item 1: <brief>. Preserve existing behavior."'

# 2. Verify, then steer the next item
rp-cli -w <window_id> -e 'read "<key file from item 1>"'
rp-cli -w <window_id> -e 'agent_run op=steer session_id="<session_id>" message="Item 1 looks good. Item 2: <brief>" wait=true'

# 3. If something's off, steer a correction
rp-cli -w <window_id> -e 'agent_run op=steer session_id="<session_id>" message="Item 1 missed <gap>. Fix first." wait=true'
```

Verify each item against the plan's "done when" criteria before steering the next. A quick `read_file` or `file_search` on key files costs little and catches drift early.

**Use `engineer` role** for refactoring items — the plan already makes the path clear, so the agent just needs precise execution. Use `pair` only if an item involves architectural decisions not covered by the plan.

Since refactor relies on extended steering, it's worth checking whether the `engineer` role is powered by a Codex-family model (which handles long steering sessions best).

To check which model is powering a role:

```bash
rp-cli -w <window_id> -e 'agent_manage op=list_agents roles_only=true'
```

A role whose display name starts with `Codex CLI` (or an explicit `model_id` with a `codexExec:*` prefix) signals the role is well-suited to extended steering.

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

### When to use parallel dispatch

Refactorings that touch **completely independent modules** can run concurrently.

If dispatching independent items as fresh agents concurrently, **each agent's brief must mention the sibling**:

> "Another agent is concurrently working on <brief description of sibling task> in <modules>. Avoid modifying files in that area. If you find yourself blocked by or conflicting with that work, stop and report back rather than pushing through."

**Use `detach: true`** when dispatching concurrent items — otherwise the orchestrator blocks on the first agent and can't start the second.

Then pass `session_ids` (array) to `agent_run op=wait` to block until the **first** session finishes or needs input. The response tells you which session won and which are still pending.

```bash
# Dispatch both concurrently
rp-cli -w <window_id> -e 'agent_run op=start model_id=engineer session_name="1/N: <goal A>" message="<brief A>" detach=true'
rp-cli -w <window_id> -e 'agent_run op=start model_id=engineer session_name="2/N: <goal B>" message="<brief B>" detach=true'

# Then wait for the first session that needs attention
rp-cli -w <window_id> -e 'agent_run op=wait session_ids=["<uuid1>","<uuid2>"] timeout=60'

# Or poll all current snapshots without blocking
rp-cli -w <window_id> -e 'agent_run op=poll session_ids=["<uuid1>","<uuid2>"]'
```

Handle the finished agent, then wait again on the remaining `pending_session_ids`. While waiting, summarize completed work or prepare the next brief — be a pipeline, not a sequential loop.

Only parallelize when items have **zero file overlap**. When in doubt, run sequentially — refactoring conflicts are painful to untangle.

### Housekeeping

Sessions persist after agents finish — useful when you might revisit output, but they pile up over a multi-agent workflow. Once you've recorded what an agent produced, you can dismiss its session:

```bash
rp-cli -w <window_id> -e 'agent_manage op=cleanup_sessions session_ids=["<session_id>"]'
```

Explore-agent sessions are good to dismiss right away — narrow reconnaissance, no follow-up value. Keep heavier agent sessions if you might revisit them.

## Step 4: Monitor & Verify

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

---

## Anti-patterns to Avoid

- 🚫 This workflow requires `builder` for both analysis (Step 1) and planning (Step 2) — don't skip either.
- 🚫 Skipping Step 0 (Workspace Verification) – you must confirm the target codebase is loaded first
- 🚫 Skipping Step 1's `builder` call with `response_type: "review"` and attempting to analyze manually
- 🚫 Skipping Step 2's `builder` call with `response_type: "plan"` — you need a concrete plan before dispatching agents
- 🚫 Extended reading before the first `builder` call – a quick skim is fine; let the builder do the heavy lifting
- 🚫 Implementing refactorings yourself — you are the coordinator; dispatch agents to do the work
- 🚫 Dispatching all items at once without verifying each one — refactorings compound; verify before proceeding
- 🚫 Parallelizing items that share files — sequential is safer for dependent refactorings
- 🚫 Forgetting to check on dispatched agents — they may block on permission approvals; poll periodically to keep them unblocked
- 🚫 Assuming you understand the code structure without `builder`'s architectural analysis
- 🚫 **CLI:** Forgetting to pass `-w <window_id>` – CLI invocations are stateless and require explicit window targeting