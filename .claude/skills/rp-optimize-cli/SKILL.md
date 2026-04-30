---
name: "rp-optimize-cli"
description: "Iterative performance optimization loop using rp-cli: instrument with debug-only metrics, establish a baseline, then plan → delegate one optimize+harden cycle → re-measure → ask oracle for next plan, looping until the oracle is satisfied or the target metric is met"
repoprompt_managed: true
repoprompt_skills_version: 54
repoprompt_variant: cli
---

# CLI Optimizer (CLI)

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
You are an **optimization orchestrator**. Performance work only improves what you can measure, so the loop is always: **instrument → baseline → plan → delegate → re-measure → decide**. Keep looping until the oracle signals the gains have plateaued, the target metric is met, or the iteration cap is reached.

Implementation and deep code reading happen in sub-agents. You own measurement, planning, verification, and the stop decision. Keep your own context lean for coordination and the running scoreboard.

## Phase 0: Workspace Verification (REQUIRED)

Before any optimization work, bind to the target codebase using its working directory:

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
## Phase 1: Define the Target

Before touching code, pin down three things:

1. **What are we optimizing?** A single, nameable metric: wall-clock latency of operation X, peak memory during Y, allocations per request, cold-start time, frame time, throughput, etc. If the user said "make it faster", translate that into a concrete metric before proceeding.
2. **Stop criterion.** What does "good enough" look like? A hard threshold ("under 50 ms at p95"), a relative target ("30% faster than baseline"), or an open-ended "push until oracle says diminishing returns". Record this — it's how you'll know when to stop looping.
3. **Scope.** Which module, function, or subsystem is in play? Optimization only stays honest when the measurement covers the thing you're changing and nothing more.

Translate the user's prompt into the codebase's actual nouns (concrete modules, filenames) so `builder` can focus immediately. 1–2 navigation calls (tree or search) is usually enough.

Example:
- Raw: *"Speed up search"*
- Contextualized: *"Reduce p95 latency of `PathMatcher.match` under the `RepoPromptTests/PathMatchingTests` fixtures — target ≤ 40% of the current baseline. Stay inside `Services/PathResolution/`."*

Shortcuts:
- **User named the function/file and the metric** → use their framing, skip the scan.
- **User pointed at an existing benchmark** → read it first; that file already defines scope and measurement.
- **Still ambiguous after 2 navigation calls** → dispatch a narrow explore agent with one specific question.

```bash
rp-cli -w <window_id> -e 'tree'
rp-cli -w <window_id> -e 'search "<hot path name>"'
```

Before leaving Phase 1, **read AGENTS.md** (or the project's equivalent testing/benchmarking doc) in the target repo. It tells you how to run tests, how to launch a live debug harness, and which measurement commands are sanctioned for this codebase. Every later phase depends on those conventions — don't improvise a measurement path.

---

## Phase 2: Instrument & Establish Baseline

### 2a. Add debug-only metrics (if they don't already exist)

Find or add metrics that measure the target. Rules of thumb:

- **Gate everything behind a debug/test build flag** so production never pays the measurement cost. Examples of the right gate for each language: `#if DEBUG` (Swift), `#[cfg(debug_assertions)]` or `#[cfg(feature = "bench")]` (Rust), `if (process.env.NODE_ENV !== 'production')` (Node), `if __debug__:` (Python), a build tag (`//go:build debug`) (Go), etc. Follow whatever convention the repo already uses.
- **Keep instrumentation code in a secondary test/support file**, not inside the module being measured. Think `FooMetrics.swift`, `foo_bench.rs`, `foo.bench.ts`, or `tests/perf/foo_metrics.py` — anything that lives alongside tests or in a dedicated `perf/`/`bench/` folder rather than next to production code. If the production code must expose a hook, make it the smallest possible surface (a single `debugTrace` callback, an `#if DEBUG` counter increment) and leave the collection, aggregation, and reporting in the support file.
- **Measure the metric you defined in Phase 1, nothing else.** Wall-clock timing, allocation counters, cache hit rates — whatever the target calls for. Resist the temptation to instrument "while we're here".
- **Verify it compiles in release mode with instrumentation stripped.** If a build flag is easy to get wrong, run the release build once before trusting the baseline.

If the repo already has a benchmark or profiling harness for this area, prefer extending it over inventing a new one. Check for existing `*Tests`, `*Bench*`, `*Perf*`, or `benchmarks/` directories first.

### 2b. Capture the baseline

Run the measurement per AGENTS.md. This may be:
- A unit/integration test that prints timings
- A dedicated benchmark target
- A live debug harness driven through the project's CLI (whatever the repo's AGENTS.md documents — for example, the project may expose a debug CLI that launches the app and exercises the feature)
- A profiler attached to a representative workload

Run it **more than once**. A single sample isn't a baseline — it's a data point. Take 3–5 runs, discard obvious outliers (cold caches, GC pauses), and record the median or p95 depending on what the metric calls for. If variance is high enough that optimizations would get lost in the noise, either increase sample size or narrow the measurement scope before continuing.

### 2c. Start a scoreboard

Create a living record of every measurement under `prompt-exports/` so the numbers survive across iterations and sub-agent handoffs:

```bash
rp-cli -w <window_id> -e 'call file_actions {"action":"create","path":"prompt-exports/optimize-<slug>-runs.md","content":"# Optimize: <target>\n\n**Metric:** <name + units>\n**Stop criterion:** <threshold or oracle-satisfied>\n**Scope:** <files/modules>\n\n## Runs\n\n| # | Change | Median | p95 | Notes |\n|---|---|---|---|---|\n| baseline | — | <n> | <n> | <env, commit> |\n"}'
```

Every future run appends a row. This file is the source of truth for progress and the input the oracle will reason over.

---

## Phase 3: The Optimization Loop

One iteration = one attributed change + one re-measurement. Running multiple optimizations in parallel destroys causality — you won't know which change produced which delta. Keep the loop **serial** by default.

Loop until one of the **termination criteria** in Phase 3e fires.

### 3a. Plan the next optimization

Select the scoreboard, the target source files, and the benchmark/test file into context, then ask `builder` for a plan scoped to **one change**:

```bash
rp-cli -w <window_id> -e 'select set <target source files> <benchmark or test> prompt-exports/optimize-<slug>-runs.md'
rp-cli -w <window_id> -e 'builder "<task>Propose the single next optimization to pursue for <metric>. One change, not a list. Include: the specific change, why you expect it to move the metric, risks to behavior/correctness, how to verify no regressions.</task>

<context>Current baseline and prior runs are in prompt-exports/optimize-<slug>-runs.md. Target: <threshold or directional goal>. Scope is limited to <modules>.</context>" --response-type plan --export'
```

The tool returns `oracle_export_path` and `oracle_export_instruction`. Include `oracle_export_path` inside the `message` you send on your next `agent_run` `start` call — the child agent opens the file with `read_file`.

If the plan proposes more than one change, pick the one with the **best expected delta per unit of risk** and note the others in the scoreboard as candidates for later iterations.

### 3b. Dispatch one full optimize-and-harden loop

Dispatch **one `pair` agent** for the selected change. The brief should make clear that a single iteration includes both landing the optimization **and** hardening it — not just "write the code":

```bash
rp-cli -w <window_id> -e 'agent_run op=start model_id=pair session_name="Optimize <N>: <change summary>" message="Read the plan at <plan path> with read_file first. Run one full optimize-and-harden loop: implement the change in <files>; run the project test command per AGENTS.md and fix breaks; re-run the measurement and append a new row to prompt-exports/optimize-<slug>-runs.md; if regressed, revert or iterate once to fix; then report changes, new metric value, updated tests, and concerns. Stay inside <scope>. Skip oracle review — orchestrator handles that."'
```

Use `pair` as the default — optimization decisions are usually deep reasoning (algorithmic changes, data-structure swaps, caching trade-offs). Use `engineer` only when the plan reduces to a mechanical change and the pair-level trade-off thinking was already done at plan time.

### 3c. Writing the dispatch brief

The agents you dispatch are fully capable — they have tools, they'll read AGENTS.md and project instructions, they can explore and reason. Your job is to orient them, not direct them.

**Scope is your most important job.** When you pass a plan export, the sub-agent can see the full plan — but it doesn't know which part is its responsibility unless you say so. Always be explicit about what it should do *now* and what it should leave alone. A few patterns:

- **Paraphrase for narrow tasks**: If the work is small and self-contained, just describe it in the dispatch message. The agent doesn't need the full plan.
- **Point to a section for broader tasks**: Reference the plan path in the `message` and tell the agent which part to focus on (e.g. "Read the plan at <path> with read_file first. Your job is item 2 in the plan. Items 1 and 3 are handled separately.").
- **State the boundary**: "Do only X. Stop when X is done." is more effective than hoping the agent infers scope from context.

You can always steer additional work later, or spin up a separate agent for the next item.

**Include:** The goal, relevant file paths/modules, and discoveries from planning that the agent wouldn't find on its own. If a separate user plan file exists, point to the relevant section. For small tasks, tell the agent to skip oracle review.

**Don't include:** Project conventions already in CLAUDE.md, step-by-step instructions, or code snippets the agent can read itself.

**Pass forward discoveries, not instructions.**

### 3d. Verify & re-measure

You own the plan. It's your job to ensure each phase respected it.

As each agent completes:

1. **Verify against the plan.** Check the agent's output against the "done when" criteria from the plan. Don't just skim — confirm the goal was actually met. A quick `read_file` or `file_search` on key deliverables costs little and catches drift before it compounds. If the plan said "add error handling to all three endpoints" and the agent only touched two, that's your catch. Mark the item as done (or note gaps) in the export file so you have a running record.
2. **If something's off**, steer a correction before moving on — never proceed with unresolved gaps:
```bash
rp-cli -w <window_id> -e 'agent_run op=steer session_id="<session_id>" message="The goal was X but Y appears missing." wait=true'
```
3. **Summarize to the user**: Brief status update — what completed, what's still running.

Beyond the generic verification steps, optimization-specific checks:

- **Did the scoreboard get a new row?** If the agent forgot to record, ask them to run the measurement once more and append properly.
- **Did tests actually run?** "Ran the tests" in a summary isn't the same as tests passing — confirm by reading the agent's output or re-running yourself.
- **Is the delta real?** Small single-digit percentage shifts in noisy benchmarks can be variance. If the gain is inside the noise band you saw during baseline collection, treat it as "inconclusive" and note that in the scoreboard before asking the oracle to interpret.
- **Spot-check behavior.** At minimum, read the diffed files and re-run one correctness-sensitive test adjacent to the change. Optimizations often subtly change semantics (early returns, caching invalidation).

If the change regressed the metric or broke correctness that the sub-agent didn't catch, **steer** the same agent to fix it before opening a new loop. Rolling back counts as progress — record the attempt in the scoreboard so the next plan knows that path was tried.

### 3e. Ask the oracle for the next plan (and the stop decision)

After a successful iteration, refresh the selection (latest source files + benchmark + scoreboard) and ask the oracle both questions in one call:

```bash
rp-cli -w <window_id> -e 'select set <files changed this iteration> <benchmark or test> prompt-exports/optimize-<slug>-runs.md'
rp-cli -w <window_id> -e 'chat "Plan: We just landed <change summary>. Metric moved from <baseline> to <new>. Scoreboard is in the selection. Given the stop criterion (<criterion>), should we run another iteration? If yes, what is the single best next optimization. If no, explain why we have hit diminishing returns or the target." --mode plan'
```

The oracle's answer determines the loop's next step:

- **"Keep going, try X next"** → go back to Phase 3a with X as the seed for the next plan. Reuse the same scoreboard and instrumentation; only re-instrument if the hot path moved.
- **"We're done" / "Diminishing returns" / target met** → exit the loop, go to Phase 4.
- **"Can't tell — measurement is too noisy" or "behavior may have regressed"** → fix the measurement or correctness issue before planning another change.

### Termination criteria (stop conditions)

Exit the loop when **any** of these fire:

1. **Oracle says so.** The oracle signals "good enough" or "diminishing returns".
2. **Target metric met.** The stop criterion from Phase 1 is satisfied in the latest measurement.
3. **Iteration cap.** 5 loops by default. If you're about to run loop 6, surface the scoreboard to the user and ask whether to continue before dispatching.
4. **Oracle can't propose a plausible next move.** Two consecutive "I'm not sure what to try" responses means the search has stalled.
5. **Regression budget exhausted.** If correctness keeps breaking faster than performance improves, stop and escalate to the user.

### Parallelism note

The loop is serial by design — attribution collapses when you run multiple changes at once. The one legitimate use of parallelism is **evaluating alternatives** for the same slot: dispatch two pair agents to try two different candidate optimizations on branches or temporary copies, pick the winner, discard the loser. This is advanced and rarely worth the coordination cost; don't reach for it unless the oracle explicitly suggests it.

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

## Phase 4: Final Rollup

After all iterations complete, give the user a **final rollup**:
- What was accomplished per iteration
- Any failures or partial completions
- Any conflicts or coordination issues that surfaced
- Suggested follow-ups if anything was deferred

Specifically for optimize:

- **Starting metric → final metric**, with iteration count. A one-line summary: "`PathMatcher.match` p95: 124ms → 38ms over 4 iterations (-69%)."
- **Which changes landed**, in order, with their individual deltas.
- **Which changes were tried and reverted**, with the reason — useful so the next person doesn't repeat dead ends.
- **State of the instrumentation.** If the debug-only metrics are worth keeping, say so; otherwise suggest removal. The scoreboard file under `prompt-exports/` can stay as historical record or be deleted — default to keeping it and let the user decide.
- **Known follow-ups.** Anything the oracle flagged but wasn't pursued this session.

### Quick reference: optimize operations

| Operation | Tool call |
|-----------|-----------|
| Scope/measure baseline | `read_file` on AGENTS.md, run project test/bench command |
| Append a scoreboard row | `apply_edits` on `prompt-exports/optimize-<slug>-runs.md` |
| Plan the next change | `builder` with `response_type: "plan"`, `export_response: true` |
| Dispatch one pair loop | `agent_run op=start model_id=pair session_name="..." message="..."` |
| Steer a fix on the same agent | `agent_run op=steer session_id="..." message="..." wait=true` |
| Wait for the agent | `agent_run op=wait session_id="..."` |
| Ask oracle: continue or stop? | `oracle_send` with `mode: "plan"` |
| Dismiss a completed session | `agent_manage op=cleanup_sessions session_ids=["..."]` |
| Reason with oracle on deltas | `oracle_send` — requires scoreboard + changed files in selection |

---

## Key Principles

- **Measure before you change, measure after you change.** No baseline, no optimization — just guessing.
- **One attributed change per iteration.** Causality is cheap to preserve and expensive to recover.
- **Instrumentation belongs in debug builds and test/support files.** Production code stays clean; measurement lives alongside tests.
- **The scoreboard is the shared truth.** Every iteration appends; nothing gets overwritten. Sub-agents and the oracle both read from it.
- **The oracle is the stop signal.** You don't decide when to stop on gut feel — you ask, with the scoreboard in selection, and respect the answer.
- **You are the coordinator, not the implementer.** Read to verify sub-agent work and to interpret measurements. Implementation belongs in `pair`.

## Anti-patterns

- 🚫 Starting to optimize before defining the metric and stop criterion — you won't know when you're done
- 🚫 Shipping measurement overhead to production — always gate metrics behind a debug/test build flag
- 🚫 Putting instrumentation in the same file as the code being measured — it belongs in a secondary test/support file
- 🚫 Skipping Phase 0 (Workspace Verification) — you must confirm the target codebase is loaded first
- 🚫 Taking a single sample as a baseline — one number isn't a measurement, it's a guess
- 🚫 Running multiple optimizations in one loop iteration — you'll never know which change produced which delta
- 🚫 Forgetting to re-run tests after the optimization — speed without correctness isn't a win
- 🚫 Skipping the oracle check and looping on your own judgment — the oracle sees the whole scoreboard; use it
- 🚫 Overwriting scoreboard rows instead of appending — historical data is how you spot regressions and dead ends
- 🚫 Implementing optimizations yourself instead of dispatching `pair` — you're the coordinator
- 🚫 **CLI:** Forgetting to pass `-w <window_id>` — CLI invocations are stateless and require explicit window targeting