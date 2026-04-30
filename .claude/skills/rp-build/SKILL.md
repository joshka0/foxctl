---
name: "rp-build"
description: "Build with RepoPrompt MCP tools context builder plan → implement"
repoprompt_managed: true
repoprompt_skills_version: 54
repoprompt_variant: mcp
---

# MCP Builder Mode

Task: $ARGUMENTS

Build deep context via `context_builder` to get a plan, then implement directly. Use follow-up reasoning only when navigating the selected code proves difficult or the plan leaves a concrete gap.

## The Workflow

0. **Verify workspace** – Confirm the target codebase is loaded
1. **Quick scan** – Understand how the task relates to the codebase
2. **Context builder** – Call `context_builder` with a clear prompt to get deep context + an architectural plan
3. **Only if needed, ask `oracle_send`** – Use it when navigating the selected code is difficult or the plan leaves a concrete unresolved gap
4. **Implement directly** – Use editing tools to make changes once the plan is clear

---

## Before you implement

Work through the phases in order:
1. Completed Phase 0 (Workspace Verification)
2. Completed Phase 1 (Quick Scan)
3. Called `context_builder` and received its plan

The quick scan is orientation only — `context_builder` does the deep exploration and produces the plan. Skipping it tends to produce shallow implementations that miss architectural patterns and edge cases.

---

## Phase 0: Workspace Verification (REQUIRED)

Before any exploration, bind to the target codebase using its working directory:

```json
{"tool":"bind_context","args":{"op":"bind","working_dirs":["/absolute/path/to/project"]}}
```
This auto-resolves to the window containing your project. No need to list windows first.

**If binding succeeds** → proceed to Phase 1
**If no match** → the codebase isn't loaded. Find and open the workspace:
```json
{"tool":"manage_workspaces","args":{"action":"list"}}
{"tool":"manage_workspaces","args":{"action":"switch","workspace":"<workspace_name>","open_in_new_window":true}}
```
Then retry the `working_dirs` bind.

---
## Phase 1: Quick Scan

Keep this phase brief — `context_builder` handles the deep exploration.

Start by getting a lay of the land with the file tree:
```json
{"tool":"get_file_tree","args":{"type":"files","mode":"auto"}}
```

Then use targeted searches to understand how the task maps to the codebase:
```json
{"tool":"file_search","args":{"pattern":"<key term from task>","mode":"path"}}
{"tool":"get_code_structure","args":{"paths":["RootName/likely/relevant/area"]}}
```

Use what you learn to **reformulate the user's prompt** with added clarity—reference specific modules, patterns, or terminology from the codebase.

Your goal is orientation, not deep understanding — `context_builder` does the heavy lifting.

---

## Phase 2: Context Builder

Call `context_builder` with your informed prompt. Use `response_type: "plan"` to get an actionable architectural plan.

```json
{"tool":"context_builder","args":{
  "instructions":"<reformulated prompt with codebase context>",
  "response_type":"plan"
}}
```

**What you get back:**
- Smart file selection (automatically curated within token budget)
- Architectural plan grounded in actual code
- `chat_id` for follow-up conversation



**Trust `context_builder`** – it explores deeply, aggregates the relevant context, and selects intelligently. Default to trusting the plan it returns. The `oracle_send` follow-up only reasons over that selected context; it cannot fill coverage gaps on its own.

---

## Phase 3: Ask `oracle_send` only if needed

`oracle_send` deep-reasons over the files selected by `context_builder`. It sees those selected files **completely** (full content, not summaries), but it **only sees what's in the selection** — nothing else.

**This phase is optional.** If the builder's plan is already clear and navigation through the selected code is straightforward, proceed straight to Phase 4.

Bring a follow-up to `oracle_send` only when:
- Navigating the selected code proves difficult even with the builder's plan
- You need cross-file reasoning over the files already selected
- The plan leaves a concrete unresolved gap you cannot close by reading the selected files directly

If the answer depends on files outside the current selection, `oracle_send` cannot answer it from thin air. Do **not** turn this workflow into manual selection management by default — if coverage is materially wrong, prefer rerunning `context_builder` with a better prompt.

```json
{"tool":"oracle_send","args":{
  "chat_id":"<from context_builder>",
  "message":"The plan points me to X and Y, but I'm still having trouble tracing how they connect across these selected files. What am I missing, and what edge cases should I watch for?",
  "mode":"plan",
  "new_chat":false
}}
```

**`oracle_send` excels at:**
- Deep reasoning over the context_builder output and selected files
- Spotting cross-file connections that piecemeal reading might miss
- Answering targeted "what am I missing in this selected context" questions

**Don't expect:**
- Knowledge of files outside the selection
- Repository exploration or missing-file discovery — that's `context_builder`'s job
- Implementation — that's your job

---

## Phase 4: Direct Implementation

Before implementing, verify you have:
- [ ] A builder result available (`chat_id` if follow-up is needed)
- [ ] An architectural plan grounded in actual code

If a specific point is still unclear, use `oracle_send` to clarify before proceeding.

Implement the plan directly. Don't use `oracle_send` with `mode:"edit"` — you implement directly.

**Primary tools:**
```json
// Modify existing files (search/replace)
{"tool":"apply_edits","args":{"path":"Root/File.swift","search":"old","replace":"new","verbose":true}}

// Create new files (auto-added to selection)
{"tool":"file_actions","args":{"action":"create","path":"Root/NewFile.swift","content":"..."}}

// Read specific sections during implementation
{"tool":"read_file","args":{"path":"Root/File.swift","start_line":50,"limit":30}}
```

**Ask `oracle_send` only when navigation or cross-file reasoning is the bottleneck:**
```json
{"tool":"oracle_send","args":{
  "chat_id":"<same chat_id>",
  "message":"I'm implementing X. The plan does not fully explain Y, and reading the selected files still leaves a gap. What pattern or connection am I missing here?",
  "mode":"chat",
  "new_chat":false
}}
```

---

## Key Guidelines

**Token limit:** Stay under ~160k tokens. Check with `manage_selection(op:"get")` if unsure. Context builder manages this, but be aware if you add files.

**Selection coverage:**
- `context_builder` should already have selected the files needed for the plan
- `oracle_send` can reason only over that selected context; it cannot discover missing files on its own
- If a material coverage gap blocks you, prefer rerunning `context_builder` with a better prompt over hand-curating selection
- Use `manage_selection` only as a last resort for a very small, targeted addition

**`oracle_send` sees only the selection:** If the answer depends on files outside the selection, `oracle_send` cannot provide it until coverage changes — and in this workflow, coverage changes should usually come from `context_builder`, not from manual curation.

---

## Anti-patterns to Avoid

- 🚫 Using `oracle_send` with `mode:"edit"` – implement directly with editing tools
- 🚫 Asking `oracle_send` about files it cannot see in the current selection
- 🚫 Treating Phase 3 as mandatory when the builder's plan is already clear
- 🚫 Reopening or second-guessing the builder's plan by default instead of trusting it
- 🚫 Leaning on manual `manage_selection` work to patch coverage gaps that should be handled by `context_builder`
- 🚫 Skipping `context_builder` and going straight to implementation – you'll miss context
- 🚫 Using `manage_selection` with `op:"clear"` – this undoes `context_builder`'s work; only use small targeted additions if absolutely necessary
- 🚫 Exceeding ~160k tokens – use slices if needed
- 🚫 Extended reading before calling `context_builder` – a quick skim is fine; let the builder do the heavy lifting
- 🚫 Reading full file contents during Phase 1 – save that for after `context_builder` builds context
- 🚫 Convincing yourself you understand enough to skip `context_builder` – you don't

---

**Your job:** Get a solid plan from `context_builder`, trust it by default, use `oracle_send` only when navigating the selected code proves difficult or the plan leaves a concrete unresolved gap, then implement directly and completely.