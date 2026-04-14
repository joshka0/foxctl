Totally — here’s a **post-migration cleanup + compatibility plan** that keeps CC + OpenCode working, while aggressively removing dead paths. It’s designed around one rule:

**All “logic” lives in foxctl hook skills + shared hook config.
All platform-specific stuff becomes thin adapters.**

---

```md
# docs/impl_plan/post-migration-cleanup-and-compat.md

---
title: Post-Migration Cleanup & CC/OC Compatibility
status: draft
last_updated: 2026-01-08
---

## 0) What “done” looks like

After migration, we should be able to say:

1) **Core behavior is consistent** across:
- foxctl actor runtime (Supervisor + Actors)
- Claude Code hooks (shell adapters)
- OpenCode plugin (TS adapter)

2) **Single source of truth for hook behavior**
- one hooks config format (YAML)
- one dispatcher implementation
- hook skills do all real work

3) **No dead runtime paths**
- old poll daemon / dspy runtime path either removed or explicitly “legacy”
- no duplicated tools (e.g., `agent.spawn` defined twice)
- no “almost used” subsystems left half wired

4) **Skills remain stable**
- `foxctl run <skill>` keeps working for CC + OC
- stable envelope outputs
- stable skill names (or explicit aliases)

---

## 1) Compatibility architecture (target steady state)

### 1.1 Core (foxctl)
- `internal/runtime/actor/*` (Supervisor, Watcher, EventBus, Actors)
- `internal/runtime/hooks/dispatcher/*` (hooks v1)
- `skills/hooks_dispatch` (thin skill wrapper around dispatcher; optional but recommended)
- `internal/agent/engine/*` (Engine interface; dspy_engine optional; llmchat_engine optional)

### 1.2 Platform adapters (thin shims)

#### Claude Code adapter
- **Goal:** Claude Code hook events → canonical foxctl events → dispatcher
- Keep `.claude/settings.json` referencing adapter scripts.
- Adapter scripts do:
  - normalize CC payload into `hook.Input`
  - call `foxctl run hooks/dispatch` (or `hooks/dispatch` skill name)
  - map `hook.Output` back into CC hook output JSON

#### OpenCode adapter
- **Goal:** OpenCode hook events → canonical foxctl events → dispatcher
- Keep `configs/opencode-hooks/` plugin.
- Plugin does:
  - on tool hooks: call `foxctl run hooks/dispatch` (for PreToolUse/PostToolUse equivalents)
  - for context injection: keep `experimental.chat.system.transform` reading a shared “pending-context” inbox file OR a lightweight `foxctl` tool call
  - custom tools remain (memory/search/symbols/task/inbox)

### 1.3 Canonical hooks config
- `~/.foxctl/hooks.yaml` and `<workspace>/.foxctl/hooks.yaml`
- Used by:
  - actor runtime
  - Claude Code adapter
  - OpenCode plugin adapter

**Outcome:** one hook config → same behavior everywhere.

---

## 2) Canonical event mapping (CC + OC → foxctl)

### 2.1 Canonical event names (v1)
- SessionStart
- MessageReceived
- UserPromptSubmit
- LLMRequest
- LLMResponse
- PreToolUse
- PostToolUse
- StopRequested
- PostAgentTurn
- ContextBudgetExceeded
- SessionEnd

### 2.2 Claude Code mapping
- SessionStart: `SessionStart` (source=startup|resume|compact)
- UserPromptSubmit: `UserPromptSubmit`
- PreToolUse: `PreToolUse` (tool_name from CC: Read/Edit/Write/Bash/Grep/Glob/TodoWrite)
- PostToolUse: `PostToolUse`
- Stop: `SessionEnd`
- PreCompact: `ContextBudgetExceeded` (or dedicated event if you want; but keep canonical set)

### 2.3 OpenCode mapping
- event.session.created: `SessionStart`
- chat.message: `UserPromptSubmit` (and/or `MessageReceived` depending on how you model it)
- tool.execute.before: `PreToolUse`
- tool.execute.after: `PostToolUse`
- experimental.session.compacting: `ContextBudgetExceeded` + `SessionEnd` (pre-compact save)
- session.idle: optional (emit `MessageReceived` or internal “IdleTick” if you keep it non-canonical)

---

## 3) Cleanup strategy: keep adapters, delete duplicated logic

### 3.1 Consolidate hook logic into hook skills
Move any “real logic” out of shell scripts / TS plugin into Go skills where possible.
Adapters should only:
- normalize inputs
- call dispatcher
- translate output back to platform format

**Targets to migrate out of shell into hook skills:**
- task guard logic
- file guard / reservations
- security scanner / secret patterns
- todo continuation gate
- overseer inbox surfacing
- semantic search augmentation
- lsp diagnostics trigger

Shell/TS should not reimplement these.

### 3.2 Consolidate / remove duplicate tool definitions
Current internal state shows likely duplication:
- `agent.spawn` exists in `agent_tools.go` AND `spawn_tools.go`.

**Cleanup rule:** exactly one canonical tool implementation per name.
- choose one `agent.spawn` tool path
- delete the other
- add a unit test that fails if registry registers duplicate names

### 3.3 Normalize tool naming cross-platform
Internally (actor runtime / toolkit) use `fs.read_file`, `edit.apply_patch`, etc.
Platform tools (CC/OC) are `Read/Edit/Write/Bash/...`.

**Hook Input must include both:**
- `tool.name` (platform name, e.g., “Edit”)
- `tool.canonical` (foxctl canonical, e.g., “edit.apply_patch”) where applicable
- `tool.kind` (read/write/exec/search)

This avoids brittle matchers and keeps CC/OC behavior stable.

---

## 4) Skills compatibility guarantees (must keep working)

### 4.1 CLI contract stays stable
- `foxctl run <skill> --input <json>` remains stable
- envelope remains stable
- skill names remain stable OR you ship aliases

**Alias policy**
If you rename a skill:
- keep an alias binary/entry for ≥1 release cycle
- emit warning in envelope meta: `"deprecated": true`

### 4.2 Workspace/session identity propagation
Ensure these are always available to skills:
- `AGENTCTL_WORKSPACE`
- `AGENTCTL_SESSION_ID`
- `AGENTCTL_AGENT_ID`

Both CC adapters and OpenCode plugin should:
- set these env vars when calling `foxctl run ...`
- or pass in hook.Input fields and dispatcher sets env for downstream skills

---

## 5) Hook adapters (final form)

### 5.1 Claude Code: single adapter script
Replace dozens of hook scripts with ONE canonical adapter:

`configs/hooks/claude/foxctl-hook.sh`

It:
- reads CC hook payload
- maps to `hook.Input`
- calls `foxctl run hooks/dispatch`
- prints CC-compatible response (`{decision, reason, context, hookSpecificOutput...}`)

Optional: keep old scripts as **thin wrappers** calling the new adapter for 1 release:
- `configs/hooks/task-guard.sh` → exec foxctl-hook.sh --event PreToolUse --matcher Edit|Write...
Then delete them.

### 5.2 OpenCode plugin: call dispatcher from hooks
Update `configs/opencode-hooks/index.ts` so:
- in `tool.execute.before`, normalize into `hook.Input{event:PreToolUse,...}`
- call `foxctl run hooks/dispatch`
- apply `UpdatedToolInput` when available (where OpenCode API allows)
- store `inject_context` actions into pending-context file for system.transform

Keep the “pending-context file handoff” because OpenCode can only inject context via system.transform.
But remove any duplicated “policy” logic from TS if it’s already in hook skills.

---

## 6) Dead code removal checklist (post-cutover)

### 6.1 Remove legacy runtime
Once supervisor path is default and stable:
- remove / archive:
  - `internal/agent/daemon/*` poll loop (or keep only as “legacy mode” behind build tag)
  - `internal/agent/runtime/*` dspy session runtime (if replaced by actor runtime)

If you want a safe intermediate step:
- move to `internal/legacy/dspy_runtime/*`
- gate build with `//go:build legacy_dspy`
- default builds exclude it

### 6.2 DSPy dependency removal (optional final step)
Only after llmchat_engine (or equivalent) is stable:
- remove `github.com/XiaoConstantine/dspy-go` and `mcp-go` deps from go.mod
- delete dspy adapters + tool wrappers that exist only for dspy types
- keep your canonical toolkit/tools interfaces

### 6.3 Remove duplicate/obsolete hook scripts
- delete non-adapter shell scripts in `configs/hooks/*` once the single adapter covers all events
- keep:
  - adapter scripts
  - small utility libs (async wrapper) only if still used

### 6.4 Remove duplicated “overseer” implementations
You currently have:
- `internal/agent/runtime/overseer*`
- `internal/intelligence/analysis/overseer/*`
Decide:
- analysis overseer = scoring + post-review handling (keep)
- runtime overseer = actor/supervisor spawn policy (keep)
But remove any “old daemon overseer loop” if supervisor covers it.

### 6.5 Remove unused env vars / configs
After cutover:
- deprecate legacy vars (example):
  - `AGENTCTL_LLM_PROVIDER` variants used only by dspy initialization paths
- keep:
  - `AGENTCTL_SESSION_*`, `AGENTCTL_WORKSPACE`, `AGENTCTL_AGENT_ID`
  - embedding/search flags
  - hook flags

---

## 7) Regression checklist (must pass on CC and OC)

### 7.1 Claude Code
- PreToolUse:
  - task guard blocks edits without active task (strict mode)
  - secret scanning blocks obvious keys
  - file guard reserves as needed (or warns)
- PostToolUse:
  - live-index triggers indexing
  - diagnostics hook surfaces errors
- UserPromptSubmit:
  - anchor and todo mode still work (if desired)
- SessionStart/Stop:
  - session identity established and stored
  - session capture/restore still works

### 7.2 OpenCode
- system.transform:
  - injected context appears at top
  - urgent messages appear and are marked surfaced
- tool hooks:
  - blocking works where allowed
  - context injection is done via pending-context file consistently
- custom tools:
  - memory/search/inbox/symbols/task still resolve to foxctl skills

### 7.3 Skills
- `code/semantic_search` runs under both:
  - inside actor runtime (internal tools)
  - invoked from CC/OC adapters
- `session/*` skills still work from hooks
- `todo/*` skills still work and stop-gating still works

---

## 8) Final cleanup “done” gate

You can delete “legacy” code when:
- supervisor is default
- hook dispatcher is default
- CC adapter + OC plugin both call dispatcher
- all hook behavior lives in hook skills (adapters are thin)
- dspy engine is either removed or clearly optional behind build tags
- there are no duplicate tool names and no unused packages referenced by tests

---

## 9) Suggested PR order for cleanup (after migration is stable)

1) PR-A: Introduce Claude single adapter + migrate CC hook configs to use it (keep old scripts as wrappers)
2) PR-B: Update OpenCode plugin to call hooks/dispatch and remove duplicated policy logic from TS
3) PR-C: Kill duplicate tool names (agent.spawn), add registry duplicate detection test
4) PR-D: Move old daemon/runtime to `internal/legacy/*` + build tag; default build excludes
5) PR-E: Remove legacy shell hooks and legacy runtime entirely (once stable)
6) PR-F: Optional: remove dspy-go dependency + delete dspy engine adapter

```
