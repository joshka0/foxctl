# Hooks

Machine-friendly reference for hook configuration, event dispatch, and action merging.

## Metadata

| Field | Value |
|------|-------|
| Status | Current |
| Canonical packages | `internal/hooks/config.go`, `internal/hooks/types.go`, `internal/hooks/dispatcher.go`, `internal/hooks/merge.go`, `internal/hooks/registry.go` |
| Last reviewed | 2026-02-17 |

## Configuration Contract

| Item | Value |
|-----|-------|
| Workspace config path | `<workspace>/.agentctl/hooks.yaml` |
| Global config path | `~/.agentctl/hooks.yaml` |
| Merge behavior | Later file overrides earlier definitions by hook `id` |
| Execution ordering | Enabled hooks sorted by ascending `priority` per event |
| Default run timeout | `2000ms` per run entry |
| Default failure mode | Fail-open (unless overridden per run entry) |

## Canonical Events (v1)

| Event |
|------|
| `SessionStart` |
| `MessageReceived` |
| `UserPromptSubmit` |
| `LLMRequest` |
| `LLMResponse` |
| `PreToolUse` |
| `PostToolUse` |
| `StopRequested` |
| `PostAgentTurn` |
| `ContextBudgetExceeded` |
| `SessionEnd` |
| `SubagentStart` |
| `SubagentStop` |

## Hook Input/Output Contract

### Input (`hooks.Input`)

Core fields include event identity, principal/workspace/session metadata, provider capabilities, tool metadata (`tool_name`, `tool_canonical`, `tool_kind`, input/observation), and optional hook-specific config.

### Output (`hooks.Output`)

| Field | Purpose |
|------|---------|
| `decision` | `none`, `approve`, or `block` |
| `reason` | Human-readable explanation (especially for block) |
| `context` | Context to inject (if provider/event supports direct injection) |
| `updated_tool_input` | Last-wins mutation for pre-tool execution |
| `updated_assistant_text` | Last-wins mutation for post-turn text |
| `actions[]` | Ordered structured actions (`run_skill`, `inject_context`, `enqueue_context`, `send_mailbox`, `bb_post`, `bb_claim`) |
| `meta` | Debug/observability metadata |

## Merge Semantics (`internal/hooks/merge.go`)

| Rule | Behavior |
|-----|----------|
| Decision precedence | `block` > `approve` > `none` |
| Reason selection | First non-empty reason for final decision class |
| Tool input mutation | Last-wins |
| Assistant text mutation | Last-wins |
| Context | Concatenated in execution order (`\n\n`) |
| Actions | Concatenated in execution order |
| Meta | Shallow merge, last-wins by key |

## Provider/Event Capability Constraints

| Constraint | Effect |
|-----------|--------|
| `PreToolUse` cannot directly inject context | Use `enqueue_context` action for later drain |
| Inject-capable events (`PostToolUse`, `UserPromptSubmit`, `SessionStart`) | Can apply immediate context injection |
| Blocking support depends on provider capabilities | Dispatcher honors `decision:block` when supported |

## Dispatcher Flow

1. Select enabled hooks matching event.
2. Evaluate matchers (`actor/tool/path/namespace/prompt`).
3. Execute each run entry via registry runner (skill or shell) with timeout/fail mode.
4. Collect outputs and merge deterministically.
5. Emit observability metadata and return final merged output.

## Operational Notes

- Skills are resolved via hook resolver/registry from installed skill paths.
- Shell-based hooks are supported through shell runner.
- Prefer structured action outputs over ad-hoc stdout text for reliable automation.

## Related Docs

- [docs/general/runtime-orchestration.md](runtime-orchestration.md)
- [docs/general/context-and-observability.md](context-and-observability.md)
- [docs/general/agent-policy-and-prompts.md](agent-policy-and-prompts.md)
- [docs/spec/task_hooks_memory.md](../spec/task_hooks_memory.md)
