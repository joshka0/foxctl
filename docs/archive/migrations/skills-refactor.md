Yep — this is the right next zoom level. The good news: **most of your “skills” don’t need big rewrites**. The main refactor is **making them “event/runtime-compatible”**: same skill can be invoked by (1) CC shell hooks, (2) OC plugin, and (3) your future in-process engine — without you duplicating logic in three places.

Below is how I’d **re-envision skills**, then the **specific refactors** I’d do (mostly small but high-leverage), grouped by skill type.

---

## 0) How to think about skills in the target architecture

### A) Skills become *portable workers*

A skill should be:

* **pure-ish**: stdin JSON → stdout envelope JSON
* **idempotent** where possible (especially hooks + graph/todo mutations)
* **workspace/session-aware** (via input + runner context env)
* **bounded outputs** (preview + artifact pointer)

### B) Tools ≠ skills (but tools can wrap skills)

In the new in-process engine:

* **Tools** are what the LLM “calls”
* Tools may run:

  * internal Go APIs directly (fast path), or
  * `foxctl run <skill>` (portable path) for “heavy compute” or “already implemented” features

So you don’t delete skills — you **stop making runtime depend on dspy-go**.

### C) Hook skills should be tiny + deterministic

Hook skills should **not** do expensive work (long LSP calls, embeddings, pagerank) inline.
Instead they should:

* decide approve/block/none
* optionally suggest arg rewrites
* optionally return *actions* (engine runs them async or “next turn”)

This is the key separation that makes CC/OC and your in-process engine all work.

---

## 1) Cross-cutting refactors you should do across skills

These are the “small changes that prevent 80% of future pain”.

### 1.1 Accept both `file_path` and `path` everywhere

Right now several hook skills do:

```go
var input struct{ FilePath string `json:"file_path"` }
```

That breaks when tool args use `path` (your own tools already use `path` a lot).

**Refactor**: add a shared helper (e.g. `internal/domain/hookutil.ExtractPath(raw)` or just in each skill):

* look for `file_path`, `path`, `file`, `current_path`
* handle absolute/relative normalization

This affects: `hooks/task_guard`, `hooks/file_guard`, `hooks/impact_analysis`, `hooks/test_feedback` (plus any future stop_guard).

### 1.2 Update “write op” detection to support both CC and canonical tools

Your hook guards currently rely on CC tool names:

* `Edit`, `Write`, `MultiEdit`, `NotebookEdit`

But in your canonical tool naming you’ll have:

* `edit.*` (write)
* `fs.*`, `code.*` (read/search)
* etc.

**Refactor** `hook.IsWriteOperation()` to treat any of these as writes:

* CC tool names OR
* tool_canonical prefixed with `edit.` OR
* `tool_kind == "write"`

That makes **one hook skill** work everywhere.

### 1.3 Add `workspace_id` and keep `workspace_root`

Some stores want a stable hashed ID, others still use the literal path.

**Refactor input shape**: always pass both if available.

* Dispatcher/adapters compute hash once (sha256 of canonical root) and pass `workspace_id`.
* Skills continue using `workspace_root` unless they *need* stable IDs.

### 1.4 Standardize “large output” behavior: preview + artifact + `artifact.read`

You already do this well in `text/ripgrep`, `text/replace`, etc.

Refactor goal:

* keep this pattern
* ensure the engine doesn’t need to remember “CAS commands”
* expose one canonical “read large result” capability (`artifact.read` tool OR `fs/cas_get` bridged as a tool)

So: no big skill rewrite, but **ensure every heavy skill outputs**:

* a bounded `preview`
* a digest pointer when truncated

### 1.5 Adopt consistent error codes + fail-open/closed

Hook skills especially should emit:

* decision: none/approve/block
* if internal failure and configured “fail_closed”, return block
* otherwise none

Your dispatcher will enforce the fail policy; hook skills just need to return “I failed” in a detectable way (e.g. `hook_output.meta.error = "..."` or envelope error).

---

## 2) Refactor plan by skill group

### Group A — Hook skills (highest ROI)

These are the skills that must survive CC + OC + in-process runtime.

#### A1) `hooks/task_guard`

**Now:** gates edits by ensuring active task, creates graph edge task→file.

**Refactors:**

1. **Write detection update** (see 1.2).
2. **Path extraction** accepts `file_path` + `path`.
3. **Output upgrades**:

   * when auto-creating tasks (auto mode), include `actions: [{type:"inject_context", ...}]` so in-process engine can inject the explanation *next turn*, while CC can still show `context`.
4. **Graph edge type normalization**:

   * you currently use `EdgeTypeModified`; shell scripts use `"modifies"`.
   * pick one canonical (`modified`) and normalize legacy in graph store/upsert.

Result: task_guard becomes the *single source of truth* for “task required”.

#### A2) `hooks/file_guard`

**Now:** checks blackboard reservations, warns/blocks, reserves.

**Refactors:**

1. Accept `path` and make relative-to-workspace consistently.
2. Ensure it can reserve for canonical `edit.*` tools (not just CC tools).
3. Make “mode strict” behavior consistent across CC/OC/engine:

   * return `block` + context
   * optionally action `send_mailbox` to notify other holder (nice future add)

#### A3) `hooks/impact_analysis`

**Now:** PostToolUse impact. Has debounce; uses gopls daemon for speed; spawns foxctl subcommands for symbols/refs.

**Refactors:**

1. Path extraction robustness.
2. Workspace detection must not depend on tool input shape; prefer `workspace_root` from hook input.
3. Convert *expensive work* into **actions** (optional but recommended):

   * For CC, you still want context after edit; but for OC & engine you can enqueue “run impact analysis” async and inject result next turn.
   * That avoids timeouts and makes behavior consistent.
4. Replace hardcoded `foxctl run code/symbols` spawning where possible with internal calls (later). Not required for migration.

#### A4) `hooks/knowledge_router`

**Now:** keyword/path matching from knowledge store.

**Refactors:**

* mostly none, just ensure prompt extraction supports:

  * `prompt` field
  * tool_input string fields
* optionally return `actions.inject_context` too (engine will use it cleanly)

#### A5) `hooks/overseer_inbox` + `hooks/mail_router`

You have two variants:

* `hooks/overseer_inbox`: recipient = overseer/broadcast
* `hooks/mail_router`: actor inbox filtering by task/session

**Refactor direction:**

* Keep both, but make them share one internal library `internal/runtime/hooks/mail/`:

  * `FetchInbox(workspace, actorID, taskID, mode)`
* Ensure both can be called from dispatcher with consistent behavior:

  * return context block
  * mark surfaced best-effort

#### A6) `hooks/session_end`

**Now:** triggers on `Stop`, writes metrics, links session→tasks in graph.

**Refactors:**

1. Canonical event name handling: treat `Stop` (CC) and `SessionEnd` (engine) the same.
2. Split “expensive side effects” into actions:

   * graph pagerank, cleanup, embedding flush, plan sync, etc. should become `actions.run_skill` rather than inline.
3. Keep current storage writes (metrics) inline (they’re fast/local).

#### A7) `hooks/test_feedback`

**Now:** reads testwatch store using a derived workspace hash.

**This one is a likely migration footgun.**

* Many other components use `workspace_root` (path) as workspace ID.
* testwatch currently hashes.

**Refactor choices:**

* Option 1 (minimal): keep hashing, but have dispatcher always pass `workspace_id` = the hash format this store expects.
* Option 2 (cleaner): migrate testwatch store to use path-based workspace ID and stop hashing.

I’d do Option 1 **during migration**, Option 2 **as cleanup**.

---

### Group B — Graph skills

You currently have:

* `graph/manage` (skills/graph) with `cleanup` operation
* `graph/cleanup` (skills/graph_cleanup) also does cleanup/stats/repair
* `graph/pagerank`

**Refactor goal: remove duplication + stabilize edge type strings.**

Recommended:

1. Make `graph/manage` the CRUD surface:

   * add_node, add_edge, delete_node, delete_edge, query, neighbors, stats
2. Make `graph/cleanup` the maintenance surface:

   * cleanup, stats, repair
3. Remove or deprecate `graph/manage cleanup` (keep temporarily as alias).
4. Normalize edge type names in one place:

   * accept `modifies`/`modified`
   * accept `worked_on`/`worked-on` etc if you have legacy
   * emit canonical value in outputs

This makes hooks and tools stop fighting over naming.

---

### Group C — Todo skills

`todo/manage` and `todo/continuation` are already aligned with the architecture (task graph analysis, stop gating, etc.). Main refactors:

1. Ensure `todo/manage` exposes **cheap “status probes”** for hooks:

   * `get_active`, `list (session_id filter)`, `summary counts`
     (you already do most of this)
2. Move “stop gating” logic out of shell scripts and into a hook skill:

   * create `hooks/stop_guard` that calls `todo/continuation`
   * CC stop hook and OC idle hook both become thin wrappers → dispatcher
3. Ensure `todo/continuation` can run with:

   * `workspace_id` (root)
   * `session_id`
   * and it never needs CC-specific assumptions

---

### Group D — Text/code utility skills (grep/replace/ripgrep/etc.)

These are already in great shape (preview + artifact). Minimal changes:

1. Make sure each has:

   * consistent `path` input naming (some use `Path`, some use `path`)
   * consistent output keys (`preview`, `count`, `artifact`)
2. Add/standardize `artifact.read` usage (tool-level more than skill-level)

No need to rewrite these.

---

## 3) What “skill refactor” looks like in practice

Here’s the “golden rule” I’d follow:

### For hook skills:

* **Never** spawn other skills unless it’s cheap **or** you return an action.
* Always:

  * parse inputs defensively
  * be deterministic
  * return `hook_output.context` for CC/OC
  * optionally return `actions.inject_context` for engine (same content)

### For non-hook skills:

* stay CLI-friendly and portable
* don’t embed platform-specific assumptions (CC vs OC)
* keep output bounded + artifact pointers

---

## 4) Biggest “must-fix” mismatches I see right now

These are worth addressing early because they’ll cause weird behavior across CC/OC:

1. **`file_path` vs `path`** in hook skills (breaks when tools differ)
2. **Write tool detection** only recognizes CC tool names
3. **Graph edge type naming drift** (`modifies` vs `modified`)
4. **workspace id drift** (path vs hashed) — especially testwatch
5. **Duplicate spawn tool / duplicate graph cleanup pathways** (later cleanup)

---

## 5) How much do we “refactor skills” vs “wrap them”?

My recommendation:

* **Refactor hook skills** to be cross-platform and return richer outputs (small changes, huge benefit).
* **Keep most other skills unchanged**; treat them as “portable workers”.
* Move platform glue (CC/OC specifics) into:

  * `hooks/dispatch` + config
  * very thin wrappers in shell/plugin

---
