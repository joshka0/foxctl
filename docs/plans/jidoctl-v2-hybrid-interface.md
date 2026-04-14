# jidoctl v2 Hybrid Interface Sketch

Status: Draft
Owner: Runtime/Orchestration
Last Updated: 2026-03-05

## Implementation Snapshot (Current)

Completed in current slice:

1. Jido bridge plugin calls `foxctl` tools through daemon RPC transport (persistent, Unix socket) with CLI fallback.
2. `foxctl` v2 Jido dispatcher writes `run.started` and reconciles runtime signal acks into `run.completed`/`run.failed`.
3. `foxctl agent ask --dispatcher jido --wait` is supported via projection polling of `ask:<ask_id>` run state.
4. `foxctl agent ask-status <ask-id>` returns non-blocking run status + callback details from v2 projections/events.

Notes:

1. Terminal status for wait mode is returned as `agent/ask_wait` with run metadata and callback details (`callback_event_id`, `callback_status`, `callback_summary`, `callback_error`, `callback_metadata`).
2. Jido runtime state normalization now preserves JSON booleans/nulls (avoids `"true"`/`"nil"` coercion).

## Goal

Define a greenfield-friendly hybrid where:

1. `foxctl` remains the v2 control plane and contract owner.
2. Jido is used as the runtime kernel for multi-agent orchestration on BEAM.
3. Existing v2 guarantees (envelopes, append-only evidence, policy gates, durable mailbox semantics) are preserved.

## Non-Goals

1. Rewrite all `foxctl` tools in Elixir in phase 1.
2. Replace v2 event/projection storage contract with Jido internals.
3. Couple orchestration correctness to process-local (ephemeral) queues.

## Why Hybrid (Short Version)

Jido is strong at runtime concerns:

- process lifecycle and supervision
- parent/child orchestration primitives (`SpawnAgent`, `StopChild`)
- signal routing (`call/cast`, CloudEvents-style signal envelope)
- await/fan-out patterns (`await`, `await_all`, `await_child`, `await_any`)

`foxctl` v2 is strong at control-plane concerns:

- command envelopes and stable API contract
- append-only event history and read projections
- policy/hierarchy rules and idempotency
- companion/context/memory/tooling ecosystem

The hybrid model keeps each system in its strongest role.

## Responsibility Split

### Keep in `foxctl` (Go, v2)

1. Command/API surfaces (`spawn`, `ask`, `run`, `list`, `kill`, orchestration commands).
2. Envelope normalization and response shaping.
3. Event append and projection writes (source of truth).
4. Hierarchy and spawn policy enforcement.
5. Durable mailbox semantics and ask/reply correlation.
6. Tool catalog and skills (`code/*`, `session/*`, `memory/*`, `codemap/*`).

### Move to Jido Runtime (Elixir)

1. Agent process execution lifecycle.
2. Parent/child orchestration execution.
3. Signal dispatch and routing.
4. Await/coordination primitives.
5. Runtime plugins/sensors for hooks and scheduling.

## Proposed Interface Boundary

Use a local runtime bridge (JSON-RPC over Unix socket) between Go and Elixir.

Rationale:

1. Strict process boundary, easier fault isolation.
2. Works with current Go control plane without invasive runtime embedding.
3. Supports progressive migration by command/capability.

### Bridge API (Control Plane -> Jido Runtime)

1. `runtime.start_agent`
2. `runtime.stop_agent`
3. `runtime.signal`
4. `runtime.spawn_child`
5. `runtime.await`
6. `runtime.get_children`
7. `runtime.state`
8. `runtime.health`

All bridge requests/responses are wrapped by v2 envelopes at Go edges.

### Minimal Request Shapes

```json
{
  "runtime.start_agent": {
    "agent_id": "session-123",
    "profile": "overseer",
    "initial_state": {},
    "metadata": {
      "depth": 1,
      "max_depth": 4,
      "request_id": "req-abc"
    }
  }
}
```

```json
{
  "runtime.signal": {
    "agent_id": "session-123",
    "signal": {
      "id": "sig-1",
      "type": "agent.ask",
      "source": "/foxctl/v2",
      "subject": "/agents/session-123",
      "data": {
        "prompt": "What did you find?",
        "correlation_id": "ask-456"
      }
    },
    "mode": "call",
    "timeout_ms": 120000
  }
}
```

## Semantic Mapping

### Signals and Envelope Mapping

Jido signal fields map naturally to v2 event metadata:

1. `signal.id` -> `event_id`
2. `signal.type` -> `event_type`
3. `signal.source` -> `source`
4. `signal.subject` -> `scope_ref`
5. `signal.data` -> `payload`

Go continues to own canonical event IDs and idempotency keys.

### Mailbox Semantics

Important distinction:

1. BEAM process mailboxes are native and fast, but process-local and ephemeral.
2. v2 mailbox contract needs durable delivery and ask/reply correlation beyond process lifetime.

Therefore:

1. Keep durable mailbox in `foxctl` storage.
2. Use Jido mailbox only as execution queue after Go has accepted and persisted message intent.
3. Ack/reply is considered complete only after Go append + projection write succeeds.

## v2 Service Integration Points

### `SpawnService`

Flow:

1. Validate hierarchy/policy/idempotency in Go.
2. Append `agent.spawn.requested`.
3. Call `runtime.start_agent`.
4. Append `agent.spawn.started` or `agent.spawn.failed`.

### `AskService`

Flow:

1. Persist mailbox ask (`ask_id`, `correlation_id`) in Go.
2. Call `runtime.signal` (`type=agent.ask`).
3. Await result or timeout via `runtime.await` (or signal callback path).
4. Persist reply + projection update.

### `RunService` and `LongLivedRunService`

Flow:

1. Go scheduler/orchestration decides work.
2. Runtime executes with Jido parent/child strategy.
3. Runtime emits signals/events back to Go bridge.
4. Go persists canonical events and updates read models.

## Proposed New Modules

### Go (`foxctl`)

1. `internal/v2/adapters/jido/client.go`
2. `internal/v2/adapters/jido/types.go`
3. `internal/v2/adapters/jido/signal_codec.go`
4. `internal/v2/adapters/jido/runtime_adapter.go`
5. `internal/v2/adapters/jido/jsonrpc_client.go`
6. `internal/v2/adapters/jido/reconciler.go`
7. `internal/v2/adapters/jido/tool_exec.go`
8. `internal/v2/adapters/jido/runtime_adapter_test.go`

### Elixir (`jidoctl` runtime package)

1. `Agentctl.Jido.RuntimeServer` (bridge endpoint)
2. `Agentctl.Jido.AgentRegistry`
3. `Agentctl.Jido.SignalRouter`
4. `Agentctl.Jido.PolicyHooks` (optional pre-flight callbacks)
5. `Agentctl.Jido.EventPublisher` (back to Go)

## Capability Mapping for v2 Agentctl Features

### Native Keep (Go-first)

1. `code/symbols`, `code/complexity`, `code/imports`
2. `code/semantic_search`, `code/smart_search`, `code/context_ripgrep`
3. `code/smart_write`, `code/snippet_extract`
4. `session/restore`, `session/summarize`, `session/recall`
5. `memory/put`, `memory/search`, `memory/query`
6. `codemap/generate`, `codemap/search`

These remain service calls from runtime actions, not reimplemented inside Jido initially.

## Tool Execution Contract (`Jido` -> `foxctl` binary)

For phase 1, Jido agents execute `foxctl` tools by invoking the Go binary
locally and consuming standard envelopes.

Canonical command shape:

```bash
foxctl run <tool-name> --workspace <workspace> --input-file -
```

Notes:

1. `stdin` carries the JSON input payload.
2. `stdout` must contain the envelope; `stderr` is logs only.
3. The runtime enforces an allowlist for callable tools.
4. Timeouts are per tool call and independent from orchestration loop timeouts.
5. This contract is represented in `internal/v2/adapters/jido/tool_exec.go`.

Dispatcher runtime selection (current CLI cut):

1. `foxctl agent ask --dispatcher mailbox|jido`
2. `AGENTCTL_V2_ASK_DISPATCHER=mailbox|jido`
3. Jido bridge socket settings:
   - `AGENTCTL_JIDO_SOCKET`
   - `AGENTCTL_JIDO_RPC_PATH`
   - `AGENTCTL_JIDO_RPC_TIMEOUT_MS`
   - `AGENTCTL_JIDO_SIGNAL_SOURCE`

Callback/reconcile path:

1. Dispatch writes `run.started` on stream `ask:<ask_id>`.
2. Terminal updates (`run.completed` / `run.failed`) are applied through the Jido reconciler (`internal/v2/adapters/jido/reconciler.go`) when runtime callbacks are consumed.

### Optional Later (Elixir-native wrappers)

1. Elixir tool adapters that call Go services over RPC.
2. Eventually selective native implementations where BEAM advantages are material.

## Tree-sitter and Hierarchical Memory in Hybrid

Use the existing v2 stack as authoritative and expose it to Jido agents as tools.

1. Tree-sitter and repo graph stay in Go services/indexers.
2. Hierarchical memory stays in v2 memory/session/event layers.
3. Jido actions call these capabilities through tool RPC and receive envelope responses.

This avoids regression in search/edit quality while unlocking BEAM orchestration.

## Failure and Consistency Model

### Rule

Runtime side effects are not durable truth until Go commits corresponding events.

### Required Safety

1. Idempotency key on every mutating bridge call.
2. At-least-once callback handling with dedupe on Go side.
3. Replay-safe startup: Go can reconstruct expected runtime state from events and reconcile.
4. Timeout policy explicitly split:
   - bridge transport timeout
   - Jido processing timeout
   - control-plane deadline

## Suggested Rollout Slices

1. Slice A: `runtime.health`, `runtime.start_agent`, `runtime.stop_agent`.
2. Slice B: `runtime.signal` for `ask` path with durable mailbox bridge.
3. Slice C: child orchestration (`spawn_child`, `get_children`, `await_child`).
4. Slice D: orchestration board integration and retry/reconcile loops.
5. Slice E: optional plugin/sensor hooks for companion and scheduling events.

## Open Design Decisions

1. Transport: Unix socket JSON-RPC (recommended) vs gRPC.
2. Event push mode: callback stream from Elixir vs Go pull/reconcile polling.
3. Agent identity source: Go-issued IDs only (recommended).
4. Policy location: strict in Go only (recommended), optional soft guards in Elixir.

## What This Unlocks

1. Better multi-agent orchestration primitives with less custom runtime code.
2. More natural hooks/scheduling via BEAM process model.
3. Retention of current v2 strengths in tooling, memory, and evidence durability.

## Exit Criteria for First Usable Hybrid

1. `spawn` and `ask` command paths can run end-to-end through Jido runtime adapter.
2. Existing envelope and projection tests pass unchanged (or with additive fixtures only).
3. Durable ask/reply survives runtime restart.
4. Orchestration card state stays consistent under retries and duplicate callbacks.
