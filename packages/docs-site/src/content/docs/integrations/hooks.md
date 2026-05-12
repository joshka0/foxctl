---
title: Hooks
description: Hook configuration, event dispatch, action merging, safety rules, and the hook script catalog.
---

Hooks connect editor events, CI runs, agent turns, and session lifecycle to
foxctl skills and commands. They are the primary integration surface for
automating context injection, memory management, code analysis, and workflow
gating.

This page covers hook configuration, the event model, merge semantics, safety
rules, and the full catalog of installed hook scripts.

## Configuration

### Config Paths

| Path | Scope |
|------|-------|
| `<workspace>/.foxctl/hooks.yaml` | Workspace-level hooks |
| `~/.foxctl/hooks.yaml` | Global hooks |

Workspace config overrides global config by hook `id`. Hooks are loaded and
merged at startup.

### Configuration Contract

| Setting | Default | Description |
|---------|---------|-------------|
| Merge behavior | Later file wins by `id` | Workspace overrides global |
| Execution ordering | Ascending `priority` per event | Lower priority numbers run first |
| Default run timeout | `2000ms` per run entry | Prevents runaway hooks |
| Default failure mode | Fail-open | Hook failure does not block unless overridden |

### Registration

Hook scripts live in `configs/hooks/`. For Claude Code, hooks are registered in
`.claude/settings.json`. Other editors and CI systems use their own registration
mechanisms pointing at the same scripts.

## Event Model

### Canonical Events (v1)

Hooks fire on these events during session and agent lifecycle:

| Event | When it fires |
|-------|---------------|
| `SessionStart` | New session begins |
| `MessageReceived` | Inbound message arrives |
| `UserPromptSubmit` | User submits a prompt |
| `LLMRequest` | Request sent to LLM provider |
| `LLMResponse` | Response received from LLM |
| `PreToolUse` | Before a tool call executes |
| `PostToolUse` | After a tool call completes |
| `StopRequested` | Session stop requested |
| `PostAgentTurn` | Agent turn finishes |
| `ContextBudgetExceeded` | Context window budget exhausted |
| `SessionEnd` | Session terminates |
| `SubagentStart` | Subagent spawned |
| `SubagentStop` | Subagent completes |

### Hook Input

The hook input (`hooks.Input`) includes:

- Event identity (event name, hook ID)
- Principal, workspace, and session metadata
- Provider capabilities
- Tool metadata (`tool_name`, `tool_canonical`, `tool_kind`, input/observation)
- Optional hook-specific config

### Hook Output

The hook output (`hooks.Output`) includes:

| Field | Purpose |
|-------|---------|
| `decision` | `none`, `approve`, or `block` |
| `reason` | Human-readable explanation (especially for block) |
| `context` | Context text to inject |
| `updated_tool_input` | Last-wins mutation for pre-tool execution |
| `updated_assistant_text` | Last-wins mutation for post-turn text |
| `actions[]` | Ordered structured actions |
| `meta` | Debug and observability metadata |

### Structured Actions

Actions are ordered, typed operations that the dispatcher executes:

| Action | Purpose |
|--------|---------|
| `run_skill` | Execute a skill |
| `inject_context` | Immediately inject context into the session |
| `enqueue_context` | Queue context for later drain |
| `send_mailbox` | Send a message to an agent mailbox |
| `bb_post` | Post to a bulletin board |
| `bb_claim` | Claim a bulletin board entry |

## Merge Semantics

When multiple hooks fire on the same event, their outputs are merged
deterministically:

| Rule | Behavior |
|------|----------|
| **Decision precedence** | `block` > `approve` > `none` |
| **Reason selection** | First non-empty reason for the final decision class |
| **Tool input mutation** | Last-wins |
| **Assistant text mutation** | Last-wins |
| **Context** | Concatenated in execution order, separated by `\n\n` |
| **Actions** | Concatenated in execution order |
| **Meta** | Shallow merge, last-wins by key |

### Provider/Event Constraints

Not all events support all output types:

- `PreToolUse` **cannot** directly inject context — use the `enqueue_context`
  action for later drain
- Inject-capable events (`PostToolUse`, `UserPromptSubmit`, `SessionStart`) can
  apply immediate context injection
- Blocking support depends on provider capabilities — the dispatcher honors
  `decision:block` only when the provider supports it

## Dispatcher Flow

1. Select enabled hooks matching the event
2. Evaluate matchers (`actor`, `tool`, `path`, `namespace`, `prompt`)
3. Execute each run entry via the registry runner (skill or shell) with timeout
   and failure mode
4. Collect outputs and merge deterministically
5. Emit observability metadata and return the final merged output

Skills are resolved via the hook resolver/registry from installed skill paths.
Shell-based hooks are supported through the shell runner.

## Safety Rules

All hooks must respect these invariants:

- **JSON envelopes only on stdout** — skill stdout must remain valid Protocol v1
  JSON envelopes; no freeform text
- **Logs to stderr** — hook logs and diagnostic output go to stderr, never stdout
- **Secrets must be redacted** — never expose API keys, tokens, or credentials
  in hook output
- **Path access through policy** — file access must go through `policy.PathValidator`
- **Deterministic for CI** — test-feedback and review hooks should be
  deterministic enough for CI use

## Hook Script Catalog

All hook scripts live in `configs/hooks/`. Most are thin wrappers over
`foxctl hooks <name>` commands.

### Session Lifecycle

| Script | Purpose |
|--------|---------|
| `session-init.sh` | Session initialization (wrapper over `foxctl hooks session-start`) |
| `session-end.sh` | Session cleanup (wrapper over `foxctl hooks session-end`) |
| `session-compact.sh` | Context compaction logic |
| `session-restore-postcompact.sh` | Restore context after compaction |

### Agent and Task

| Script | Purpose |
|--------|---------|
| `subagent-stop.sh` | Subagent completion handler |
| `overseer-inbox.sh` | Overseer inbox processing |
| `overseer-inbox-post.sh` | Post-processing for overseer inbox |
| `task-continuity-summary.sh` | Prompt-ready wrapper for `foxctl context task-history-summary` |
| `task-file-link.sh` | Link task context to files |
| `task-guard.sh` | Task boundary enforcement |

### Memory and Context

| Script | Purpose |
|--------|---------|
| `memory-detector.sh` | Detect memory-worthy patterns |
| `memory-recall.sh` | Recall relevant memories |
| `memory-lifecycle.sh` | Memory lifecycle management |
| `context-updater-drain.sh` | Drain queued context updates |
| `anchor-detect.sh` | Detect semantic anchors |

### Code Intelligence

| Script | Purpose |
|--------|---------|
| `code-analysis.sh` | Run code analysis on changes |
| `live-index.sh` | Update repo index incrementally |
| `lsp-diagnostics.sh` | Capture LSP diagnostic data |
| `embedding-flush.sh` | Flush embedding queues |

### Planning and Graph

| Script | Purpose |
|--------|---------|
| `plan-sync.sh` | Synchronize plan state |
| `graph-maintenance.sh` | Repo graph maintenance tasks |

### TODO and Continuity

| Script | Purpose |
|--------|---------|
| `todo-sync.sh` | Synchronize TODO state |
| `todo-continuation.sh` | Continue TODO-driven work |
| `todo-advisor.sh` | Suggest next TODO actions |

### Build and Security

| Script | Purpose |
|--------|---------|
| `build-guard.sh` | Guard against build failures |
| `security-scanner.sh` | Run security scans on changes |
| `test-feedback.sh` | Capture and relay test results |

### Advanced Hooks

| Script | Purpose |
|--------|---------|
| `foxctl-mode.sh` | Mode enforcement |
| `foxctl-mode-enforce.sh` | Strict mode enforcement |
| `bash-advisor.sh` | Bash best-practice suggestions |
| `skill-advisor.sh` | Skill recommendation logic |
| `counsel-detect.sh` | Counsel pattern detection |
| `counsel-suggest.sh` | Counsel suggestion generation |
| `knowledge-router.sh` | Route knowledge queries |
| `hooks-error-drain.sh` | Drain hook errors |
| `context-detect.sh` | Detect context patterns |

## Task Continuity Usage

Use the structured command for Codex, scripts, and agent runtimes:

```bash
foxctl context task-history-summary
```

Use the shell wrapper only when a hook needs prompt-ready JSON:

```bash
configs/hooks/task-continuity-summary.sh
```

## Adding a Custom Hook

1. Create a script in `configs/hooks/`
2. Register it in `.claude/settings.json` (for Claude Code) or your editor's
   hook configuration
3. The script should output context injection text to stdout (or nothing if no
   injection needed)
4. Logs go to stderr

Hook scripts should follow the output contract: valid JSON envelopes on stdout,
human-readable logs on stderr.

## Cross-References

- [Skills Runtime](/skills/runtime-and-install) — how skills are executed
- [Agents Lifecycle](/agents/lifecycle) — session and agent events
- [Memory and Continuity](/memory/continuity) — task-history summaries and
  memory queries
- [Protocol V1](/reference/protocol-v1) — envelope format and status rules
