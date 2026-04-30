---
name: "rp-investigate-cli"
description: "Deep investigation with rp-cli commands: tools gather evidence, follow-up reasoning synthesizes selected context"
repoprompt_managed: true
repoprompt_skills_version: 54
repoprompt_variant: cli
---

# Deep Investigation Mode (CLI)

Investigate: $ARGUMENTS

You are now in deep investigation mode for the issue described above. Follow this protocol rigorously.

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
## Investigation Protocol

This workflow leverages five complementary capabilities:

- **You (the agent)**: Orchestrate. Triage what the task needs, dispatch explore agents for external fact-gathering, run `builder`, dispatch a pair investigator, curate the file selection, and synthesize the final report. Default posture: coordination, not reconnaissance.
- **Explore agents** (`agent_run` with `model_id:"explore"`): Read-only sub-agents in a fresh context window, for narrow self-contained questions. Used in two places: (1) **before `builder`**, for facts outside the workspace (git archaeology, web searches, external docs — findings go to `## Background / Prior Research` in the report); (2) **spawned by the pair** for in-workspace checks.
- **Context Builder** (`builder`): Populates the file selection with full files or slices relevant to the task. Feed it the report path so prior research informs the selection.
- **Chat** (`oracle_send`): Deep analytical reasoning over the current file selection. Good for synthesis across selected files; not a lookup tool.
- **Pair investigator** (`agent_run` with `model_id:"pair"`): Full-capability agent for the main line of inquiry. Reads files, runs git, spawns its own explore agents, and writes findings into `## Investigator Findings` in the report.

This workflow is read-only. Output lands in the investigation report; no source code changes.

### How File Selection Drives the Workflow

**The pair's and explores' file reads don't populate your file selection** — they run in their own sessions. Selection curation is **your** job: the chat only sees what's in the selection in your window.

1. `builder` seeds the selection during Phase 2
2. After the pair returns, refresh the selection to match what the investigation surfaced — add files the pair referenced, add slices of large files where only a region is relevant, remove fully unrelated files
3. **Bias toward inclusion** — better for the chat to see a related file than miss one. Prune only files/codemaps that are clearly unrelated; when in doubt, keep them
4. **Never `op:"clear"` or `op:"set"`** — they wipe `builder`'s curation. Use `op:"add"` / `op:"remove"` / slices

### Core Principles
1. **Don't stop until confident** — pursue every lead until evidence is solid
2. **Delegate before reading** — phases below lay out the default order (explore → `builder` → pair → chat). You orchestrate; the pair writes findings directly to the report.
3. **Curate the selection between chat calls** — the pair's reads aren't visible in your selection; add files it surfaced, bias toward inclusion
4. **Direct tool calls are for follow-up** — reserve your own `read_file` / `file_search` / `git` for user-supplied leads, verifying agent findings, and grabbing final line-number evidence
5. **Don't duplicate in-flight work** — while agents are running, don't re-run their investigation or spin up overlapping fleets

### Phase 0: Workspace Verification (REQUIRED)

Before any investigation, bind to the target codebase using its working directory:

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
### Phase 1: Initial Assessment & Triage (Agent — you)

1. Read any provided files/reports (traces, logs, error reports)
2. Summarize symptoms and form initial hypotheses
3. **Create the investigation report file** — use `docs/investigations/<topic>-<YYYY-MM-DD>.md` (or match the repo's existing convention; look under `docs/investigations/` for examples). Note its absolute path; you'll feed it to `builder` and the pair.
4. **Triage external info needs.** Does the task require anything `builder` can't see in the workspace?
	- Git history (blame, log archaeology, "when did this regress", PR context)
	- Web searches or external documentation
	- Other facts outside the workspace

If yes, run Phase 1.5 first. Otherwise skip to Phase 2.

#### Phase 1.5: External Fact-Gathering (conditional)

Dispatch explore agents in parallel for external facts. As each returns, write a concise entry into the report's `## Background / Prior Research` section — commits, excerpts, links.

```bash
rp-cli -w <window_id> -e 'agent_run op=start model_id=explore session_name="<kind>: <question>" message="<question>. Report commits/links and summary." detach=true'
```

> ⚠️ **Detached agents may block on permission approvals.** Poll periodically or use `op=wait` so you can approve requests and keep them unblocked. This applies to every detached agent in this workflow.

### Phase 2: Broad Context Gathering (via `builder` — REQUIRED)

`builder` discovers workspace files you'd miss manually. Pass detailed instructions + the report path so prior research informs its selection:

```bash
rp-cli -w <window_id> -e 'builder "<task>Investigate: specific issue</task>

<context>
See investigation report at <absolute/path/to/investigation-report.md> for symptoms, hypotheses, and prior research.

Symptoms:
- <symptom 1>
- <symptom 2>

Hypotheses to test:
- <theory 1>
- <theory 2>

Areas likely involved:
- <files/patterns/subsystems>
</context>
" --response-type question'
```

Use `response_type: question` so the chat returns its initial assessment immediately. If `builder` produces a thin selection (few files, or misses obvious areas), re-run it with refined instructions rather than doing the broad search yourself.

### Phase 3: Pair Investigator (Main Line of Inquiry)

Dispatch a pair investigator for the main investigation. It handles multi-step reasoning and spawns its own explore agents for in-workspace reconnaissance.

**Skip the pair** only when the chat's hypotheses point to a single spot one `read_file` would resolve, or when Phase 1.5's external research already answers the task.

**Default: one pair** writing to `## Investigator Findings`. **Escalate to 2–3 parallel pairs** only when the chat's response surfaces genuinely disjoint hypothesis paths (distinct root-cause theories in different subsystems — e.g., "caching vs. threading vs. encoding"). Each gets a disjoint scope and its own `## Investigator Findings: <path>` sub-section; cap at 3.

Its brief should include:

- Hypothesis and what you want proved or disproved
- Relevant chat analysis points
- Absolute path to the report file, with instruction to append findings under `## Investigator Findings` (file:line refs, evidence, conclusions)
- Encouragement to fan out explore agents for parallel reconnaissance — seed 2–3 concrete candidate checks to kickstart delegation

```bash
rp-cli -w <window_id> -e 'agent_run op=start model_id=pair session_name="Investigate: <hypothesis>" message="Investigate <hypothesis>. See <report-path> for context. Trace <flow>. Fan out explore agents; candidate checks: <check 1>, <check 2>, <check 3>. Append findings to ## Investigator Findings in the report." detach=true'
```

**While the pair runs**, don't re-run its investigation. Monitor the session for permission approvals, handle user-supplied specifics (files the user pointed you at), run git on already-pinpointed code, and plan the next chat questions. Don't spin up parallel explore agents at your level — the pair is running its own.

**When the pair returns** (wait or poll):

```bash
rp-cli -w <window_id> -e 'agent_run op=wait session_id=<pair_uuid> timeout=60'
```

Read its `## Investigator Findings` — primary evidence. Spot-check specific claims with `read_file` / `file_search` / `git` before folding into the root cause.

#### Housekeeping

Sessions persist after agents finish — useful when you might revisit output, but they pile up over a multi-agent workflow. Once you've recorded what an agent produced, you can dismiss its session:

```bash
rp-cli -w <window_id> -e 'agent_manage op=cleanup_sessions session_ids=["<session_id>"]'
```

Explore-agent sessions are good to dismiss right away — narrow reconnaissance, no follow-up value. Keep heavier agent sessions if you might revisit them.

### Phase 4: Refocus Selection + Chat Deep Dives (iterate)

**Before each chat call, curate the selection.** The pair's file reads ran in another session — they aren't in your selection. Update it to match what the investigation surfaced:

- **Add** files the pair referenced in `## Investigator Findings`
- **Add slices** of large files where only a region is relevant
- **Remove** files that turned out to be fully unrelated — bias toward keeping; when in doubt, leave it
- **Never** `op:"clear"` or `op:"set"` — they wipe `builder`'s curation. Use `op:"add"` / `op:"remove"` / slices

Then ask a question that requires synthesis, not lookup:

```bash
rp-cli -w <window_id> -e 'select add <files surfaced by the pair>'
rp-cli -w <window_id> -e 'select add Root/large/file.swift:100-250'

rp-cli -w <window_id> -t '<tab_id>' -e 'chat "Here is what the pair found:
- <evidence 1 with file:line>
- <evidence 2 with file:line>

<specific question>" --mode chat'
```

> Pass `-t <tab_id>` to continue the same chat conversation.

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

- 🚫 **Running `builder` with incomplete inputs** — before Phase 1.5 external research, or without the report path
- 🚫 Skipping Phase 0 — confirm the target codebase is loaded first
- 🚫 **Skipping `builder`** or doing broad manual reads — you'll miss context
- 🚫 **Duplicating in-flight work** — broad reads/searches or parallel explore agents at your level while the pair is investigating. Dispatch, then orchestrate.
- 🚫 **Stale file selection before chat calls** — the pair's reads aren't in your selection; add files it surfaced, bias toward inclusion, never `op:"clear"`/`op:"set"` (wipes `builder`'s curation)
- 🚫 Asking the chat for exact line numbers or using it for lookups — it can't produce reliable line numbers and it's not a lookup tool; verify yourself or delegate to a tool call
- 🚫 Calling the chat without new evidence between turns
- 🚫 **Parallel pair investigators on overlapping hypotheses** — only parallelize for genuinely disjoint paths; each pair gets its own `## Investigator Findings: <path>` sub-section
- 🚫 Dispatching the pair without the report path — it should append findings directly
- 🚫 Wrong tool for the job — explore agents for complex multi-step in-workspace investigation (use the pair), or broad prompts like "investigate the auth system" to explores (one specific check each)
- 🚫 Forgetting to poll dispatched agents — they may block on permission approvals
- 🚫 **CLI:** Forgetting `-w <window_id>` — stateless invocations need explicit window targeting

---

Now begin. First run `rp-cli -e 'windows'` to find the correct window. Follow the phases above: assess → (if needed) gather external facts → `builder` → pair investigator → refresh selection → chat synthesis → report. You orchestrate, they investigate.