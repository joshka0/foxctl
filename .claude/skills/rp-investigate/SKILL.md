---
name: "rp-investigate"
description: "Deep investigation with RepoPrompt MCP tools: tools gather evidence, follow-up reasoning synthesizes selected context"
repoprompt_managed: true
repoprompt_skills_version: 54
repoprompt_variant: mcp
---

# Deep Investigation Mode

Investigate: $ARGUMENTS

You are now in deep investigation mode for the issue described above. Follow this protocol rigorously.

## Investigation Protocol

This workflow leverages five complementary capabilities:

- **You (the agent)**: Orchestrate. Triage what the task needs, dispatch explore agents for external fact-gathering, run `context_builder`, dispatch a pair investigator, curate the file selection, and synthesize the final report. Default posture: coordination, not reconnaissance.
- **Explore agents** (`agent_run` with `model_id:"explore"`): Read-only sub-agents in a fresh context window, for narrow self-contained questions. Used in two places: (1) **before `context_builder`**, for facts outside the workspace (git archaeology, web searches, external docs — findings go to `## Background / Prior Research` in the report); (2) **spawned by the pair** for in-workspace checks.
- **Context Builder** (`context_builder`): Populates the file selection with full files or slices relevant to the task. Feed it the report path so prior research informs the selection.
- **Chat** (`oracle_send`): Deep analytical reasoning over the current file selection. Good for synthesis across selected files; not a lookup tool.
- **Pair investigator** (`agent_run` with `model_id:"pair"`): Full-capability agent for the main line of inquiry. Reads files, runs git, spawns its own explore agents, and writes findings into `## Investigator Findings` in the report.

This workflow is read-only. Output lands in the investigation report; no source code changes.

### How File Selection Drives the Workflow

**The pair's and explores' file reads don't populate your file selection** — they run in their own sessions. Selection curation is **your** job: the chat only sees what's in the selection in your window.

1. `context_builder` seeds the selection during Phase 2
2. After the pair returns, refresh the selection to match what the investigation surfaced — add files the pair referenced, add slices of large files where only a region is relevant, remove fully unrelated files
3. **Bias toward inclusion** — better for the chat to see a related file than miss one. Prune only files/codemaps that are clearly unrelated; when in doubt, keep them
4. **Never `op:"clear"` or `op:"set"`** — they wipe `context_builder`'s curation. Use `op:"add"` / `op:"remove"` / slices

### Core Principles
1. **Don't stop until confident** — pursue every lead until evidence is solid
2. **Delegate before reading** — phases below lay out the default order (explore → `context_builder` → pair → chat). You orchestrate; the pair writes findings directly to the report.
3. **Curate the selection between chat calls** — the pair's reads aren't visible in your selection; add files it surfaced, bias toward inclusion
4. **Direct tool calls are for follow-up** — reserve your own `read_file` / `file_search` / `git` for user-supplied leads, verifying agent findings, and grabbing final line-number evidence
5. **Don't duplicate in-flight work** — while agents are running, don't re-run their investigation or spin up overlapping fleets

### Phase 0: Workspace Verification (REQUIRED)

Before any investigation, bind to the target codebase using its working directory:

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
### Phase 1: Initial Assessment & Triage (Agent — you)

1. Read any provided files/reports (traces, logs, error reports)
2. Summarize symptoms and form initial hypotheses
3. **Create the investigation report file** — use `docs/investigations/<topic>-<YYYY-MM-DD>.md` (or match the repo's existing convention; look under `docs/investigations/` for examples). Note its absolute path; you'll feed it to `context_builder` and the pair.
4. **Triage external info needs.** Does the task require anything `context_builder` can't see in the workspace?
	- Git history (blame, log archaeology, "when did this regress", PR context)
	- Web searches or external documentation
	- Other facts outside the workspace

If yes, run Phase 1.5 first. Otherwise skip to Phase 2.

#### Phase 1.5: External Fact-Gathering (conditional)

Dispatch explore agents in parallel for external facts. As each returns, write a concise entry into the report's `## Background / Prior Research` section — commits, excerpts, links.

```json
// Explore agent for external fact-gathering (git archaeology / web / docs)
{"tool":"agent_run","args":{
	"op":"start",
	"model_id":"explore",
	"session_name":"<kind>: <specific question>",
	"message":"<Specific question>. Report relevant commits/file:line refs or links + short summary.",
	"detach":true
}}
```

> ⚠️ **Detached agents may block on permission approvals.** Poll periodically or use `op=wait` so you can approve requests and keep them unblocked. This applies to every detached agent in this workflow.

### Phase 2: Broad Context Gathering (via `context_builder` — REQUIRED)

`context_builder` discovers workspace files you'd miss manually. Pass detailed instructions + the report path so prior research informs its selection:

```
mcp__RepoPrompt__context_builder:
  instructions: |
	<task>Describe the specific issue or question to investigate</task>

	<context>
	See investigation report at `<absolute/path/to/investigation-report.md>` for symptoms, hypotheses, and any prior research (git history, external docs) gathered in Phase 1.5.

	Symptoms:
	- <symptom 1>
	- <symptom 2>

	Hypotheses to test:
	- <theory 1>
	- <theory 2>

	Areas likely involved:
	- <files, patterns, or subsystems>
	</context>

	response_type: question
```

Use `response_type: question` so the chat returns its initial assessment immediately. If `context_builder` produces a thin selection (few files, or misses obvious areas), re-run it with refined instructions rather than doing the broad search yourself.

### Phase 3: Pair Investigator (Main Line of Inquiry)

Dispatch a pair investigator for the main investigation. It handles multi-step reasoning and spawns its own explore agents for in-workspace reconnaissance.

**Skip the pair** only when the chat's hypotheses point to a single spot one `read_file` would resolve, or when Phase 1.5's external research already answers the task.

**Default: one pair** writing to `## Investigator Findings`. **Escalate to 2–3 parallel pairs** only when the chat's response surfaces genuinely disjoint hypothesis paths (distinct root-cause theories in different subsystems — e.g., "caching vs. threading vs. encoding"). Each gets a disjoint scope and its own `## Investigator Findings: <path>` sub-section; cap at 3.

Its brief should include:

- Hypothesis and what you want proved or disproved
- Relevant chat analysis points
- Absolute path to the report file, with instruction to append findings under `## Investigator Findings` (file:line refs, evidence, conclusions)
- Encouragement to fan out explore agents for parallel reconnaissance — seed 2–3 concrete candidate checks to kickstart delegation

```json
{"tool":"agent_run","args":{
	"op":"start",
	"model_id":"pair",
	"session_name":"Investigate: <hypothesis>",
	"message":"Investigate <hypothesis>. See `<report-path>` for context. Trace <flow>, verify <behavior>. Fan out explore agents for narrow reconnaissance; candidate checks: <check 1>, <check 2>, <check 3>. Append findings to `## Investigator Findings` in the report with file:line refs and evidence.",
	"detach":true
}}
```

**While the pair runs**, don't re-run its investigation. Monitor the session for permission approvals, handle user-supplied specifics (files the user pointed you at), run git on already-pinpointed code, and plan the next chat questions. Don't spin up parallel explore agents at your level — the pair is running its own.

**When the pair returns** (wait or poll):

```json
{"tool":"agent_run","args":{"op":"wait","session_id":"<pair_session_id>","timeout":60}}
```

Read its `## Investigator Findings` — primary evidence. Spot-check specific claims with `read_file` / `file_search` / `git` before folding into the root cause.

#### Housekeeping

Sessions persist after agents finish — useful when you might revisit output, but they pile up over a multi-agent workflow. Once you've recorded what an agent produced, you can dismiss its session:

```json
{"tool":"agent_manage","args":{"op":"cleanup_sessions","session_ids":["<session_id>"]}}
```

Explore-agent sessions are good to dismiss right away — narrow reconnaissance, no follow-up value. Keep heavier agent sessions if you might revisit them.

### Phase 4: Refocus Selection + Chat Deep Dives (iterate)

**Before each chat call, curate the selection.** The pair's file reads ran in another session — they aren't in your selection. Update it to match what the investigation surfaced:

- **Add** files the pair referenced in `## Investigator Findings`
- **Add slices** of large files where only a region is relevant
- **Remove** files that turned out to be fully unrelated — bias toward keeping; when in doubt, leave it
- **Never** `op:"clear"` or `op:"set"` — they wipe `context_builder`'s curation. Use `op:"add"` / `op:"remove"` / slices

Then ask a question that requires synthesis, not lookup:

```
// Add files the pair surfaced
mcp__RepoPrompt__manage_selection:
	op: add
	paths: [<files surfaced by the pair>]

// Or add a slice of a large file
mcp__RepoPrompt__manage_selection:
	op: add
	slices:
	- path: "Root/large/file.swift"
		ranges: [{start_line: 100, end_line: 250}]

// Ask a focused question — the chat sees the updated selection
mcp__RepoPrompt__`oracle_send`:
  chat_id: <from context_builder>
	message: |
	Here's what the pair found:
	- <evidence 1 with file:line>
	- <evidence 2 with file:line>

	<specific analytical question>
	mode: chat
```

**Repeat Phases 3–4** as needed. For new evidence between chat calls, steer the existing pair (it keeps its context) or dispatch a fresh explore for narrow external lookups. Don't burn a chat call on a question `read_file` / `file_search` / `git` could answer.

**Stop when**: root cause is identified with concrete file:line evidence, alternate hypotheses are ruled out with specific counter-evidence, and recommended fixes point at exact locations.

### Phase 5: Conclusions & Report (Agent — you)

`## Investigator Findings` and `## Background / Prior Research` are your factual baseline. Verify line references as you fold them into:

- **Root cause** — exact file paths, line numbers, code snippets
- **Eliminated hypotheses** — and the evidence that ruled them out
- **Recommended fixes** — specific, actionable, with file locations
- **Preventive measures** — how to avoid this recurring

---

## Role Summary

| Capability | Agent (you) | Context Builder | Chat (`oracle_send`) | Pair Investigator | Explore Agents |
|------------|-------------|-----------------|--------|-------------------|----------------|
| Triage / orchestrate | ✅ Primary | ❌ | ❌ | ❌ | ❌ |
| Dispatch sub-agents | ✅ | ❌ | ❌ | ✅ | ❌ |
| Discover files in workspace | ⚠️ Limited | ✅ Primary | ❌ | ✅ Good | ⚠️ Narrow |
| Populate file selection | ✅ (curate) | ✅ Primary (seed) | ❌ | ❌ | ❌ |
| Mutate selection to refocus chat | ✅ Primary | ❌ | ❌ | ❌ | ❌ |
| Read file contents & lines | ✅ | ❌ | Sees full selected files | ✅ | ✅ |
| Run git blame/log/diff | ✅ | ❌ | ❌ | ✅ | ✅ |
| **Web searches / external docs** | ❌ | ❌ | ❌ | ❌ | ✅ Primary |
| Multi-step cross-file reasoning | ⚠️ OK | ❌ | ✅ (on selection) | ✅ Primary | ❌ |
| Synthesize patterns & architecture | ⚠️ OK | ❌ | ✅ Primary | ✅ Good | ⚠️ OK |
| Form & refine hypotheses | ⚠️ OK | ❌ | ✅ Primary | ✅ Good | ❌ |
| Produce line-number evidence | ✅ (verify/augment) | ❌ | ❌ | ✅ Primary | ✅ |
| Write findings into report | ✅ (final synthesis) | ❌ | ❌ | ✅ Primary | ❌ |

---

## Report Template

Create a findings report as you investigate:

```markdown
# Investigation: [Title]

## Summary
[1-2 sentence summary of findings]

## Symptoms
- [Observed symptom 1]
- [Observed symptom 2]

## Background / Prior Research
<!-- Findings from Phase 1.5 explore agents: git archaeology, external docs, web searches.
     The agent populates this section before running the context builder. Omit if nothing outside the workspace was needed. -->

## Investigator Findings
<!-- The pair investigator appends its structured analysis here (file:line refs, evidence, conclusions).
     The agent leaves this section for the pair to populate and folds it into the root cause below.

     If running 2–3 parallel pair investigators on disjoint hypothesis paths, replace this single section
     with one sub-section per path, e.g.:
         ## Investigator Findings: <hypothesis path A>
         ## Investigator Findings: <hypothesis path B>
     Each pair writes only to its own sub-section to avoid write contention. -->

## Investigation Log

### [Phase] - [Area Investigated]
**Hypothesis:** [What you were testing]
**Findings:** [What you found]
**Evidence:** [Exact file paths, line numbers, code snippets, git commits]
**Conclusion:** [Confirmed/Eliminated/Needs more investigation]

## Root Cause
[Detailed explanation with precise evidence]

## Recommendations
1. [Fix 1 — specific file and location]
2. [Fix 2 — specific file and location]

## Preventive Measures
- [How to prevent this in future]
```

---

## Anti-patterns to Avoid

- 🚫 **Running `context_builder` with incomplete inputs** — before Phase 1.5 external research, or without the report path
- 🚫 Skipping Phase 0 — confirm the target codebase is loaded first
- 🚫 **Skipping `context_builder`** or doing broad manual reads — you'll miss context
- 🚫 **Duplicating in-flight work** — broad reads/searches or parallel explore agents at your level while the pair is investigating. Dispatch, then orchestrate.
- 🚫 **Stale file selection before chat calls** — the pair's reads aren't in your selection; add files it surfaced, bias toward inclusion, never `op:"clear"`/`op:"set"` (wipes `context_builder`'s curation)
- 🚫 Asking the chat for exact line numbers or using it for lookups — it can't produce reliable line numbers and it's not a lookup tool; verify yourself or delegate to a tool call
- 🚫 Calling the chat without new evidence between turns
- 🚫 **Parallel pair investigators on overlapping hypotheses** — only parallelize for genuinely disjoint paths; each pair gets its own `## Investigator Findings: <path>` sub-section
- 🚫 Dispatching the pair without the report path — it should append findings directly
- 🚫 Wrong tool for the job — explore agents for complex multi-step in-workspace investigation (use the pair), or broad prompts like "investigate the auth system" to explores (one specific check each)
- 🚫 Forgetting to poll dispatched agents — they may block on permission approvals

---

Now begin. Follow the phases above: assess → (if needed) gather external facts → `context_builder` → pair investigator → refresh selection → chat synthesis → report. You orchestrate, they investigate.