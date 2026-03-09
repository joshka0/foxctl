# Simulator Agents

## Status

Current

## Purpose

This document defines the reusable pattern for simulator agents that act like
users against external app APIs.

The goal is to avoid one-off runtime hacks per integration. Instead, each app
integration should expose a stable two-tool contract through `agentctl`, and
then use either:

1. a deterministic scheduled probe
2. an agent-driven `state -> action -> state` simulation loop

## Core Contract

Every simulator-friendly app integration should expose:

- `app/state`
  - read-only
  - returns one identity-scoped snapshot
  - must be safe to call repeatedly

- `app/action`
  - bounded mutation surface
  - explicit `operation` enum
  - explicit typed args
  - should be replay-safe or at least idempotency-aware where possible

Heartwood is the reference example:

- [heartwood/state](../../skills/heartwood_state/main.go)
- [heartwood/action](../../skills/heartwood_action/main.go)

## Two Simulation Modes

### 1. Deterministic Tick Tool

Use this when you want:

- health checks
- read probes
- scripted state transitions
- fast regression verification

The runtime contract is:

- Jido tick root runs a configured `tick_tool`
- fixed `tick_tool_input`
- no helper LLM agent required

Current insertion point:

- `~/repos/githubs/jido/lib/jido/integrations/agentctl/actions/tick_bridge.ex`

Current smoke:

- [jido_heartwood_state_tick_smoke.sh](../../scripts/jido_heartwood_state_tick_smoke.sh)

### 2. Agent-Driven User Simulation

Use this when you want:

- user-like choices
- selective reactions to state
- multi-step behavior
- social flows

The loop is:

1. call `app_state`
2. decide next move
3. call `app_action`
4. call `app_state` again
5. summarize visible result

Current Heartwood smoke:

- [heartwood_agent_action_smoke.sh](../../scripts/heartwood_agent_action_smoke.sh)

Verified flow:

- `heartwood_action(upsert_profile)`
- then `heartwood_state`
- agent replies with final visible profile values

## Runtime Layer Responsibilities

### agentctl owns semantic tool behavior

That includes:

- skill wrappers
- tool registry wiring
- tool-name normalization
- agent-visible tool definitions
- allowlist filtering

Relevant files:

- [runtime.go](../../internal/agent/runtime/runtime.go)
- [heartwood_tools.go](../../internal/agent/tools/heartwood_tools.go)
- [tools.go](../../internal/agent/tools/tools.go)
- [registry_executor.go](../../internal/agent/tools/registry_executor.go)
- [registry.go](../../internal/agent/toolnames/registry.go)
- [signature.go](../../internal/agentprompt/signature.go)

### Jido owns scheduling/orchestration

That includes:

- tick scheduling
- runtime lifecycle
- root/child agent hierarchy
- deterministic `tick_tool` execution

Relevant files:

- `~/repos/githubs/jido/lib/jido/integrations/agentctl/actions/tick_bridge.ex`
- `~/repos/githubs/jido/lib/jido/integrations/agentctl/actions/run_tool.ex`
- `~/repos/githubs/jido/lib/jido/integrations/agentctl/actions/ask_bridge.ex`

## Implementation Checklist For New App Integrations

1. Add `app/state`
   - must accept identity/session token reuse
   - must return a compact, structured state object

2. Add `app/action`
   - keep `operation` explicit
   - validate args strictly
   - return the action result plus updated identity/token if needed

3. Add both tools to the agent tool surface
   - runtime tool defs
   - runtime executor
   - registry-backed tools
   - tool-name normalization
   - allowlist filtering

4. Add one deterministic smoke
   - `tick_tool=app/state`

5. Add one agent-driven smoke
   - state
   - one action
   - state verification

6. Only then add richer social/multi-actor simulation

## Lessons From Heartwood

The failures we hit were mostly not app-specific:

- tool existed but wrapper stdout was noisy
- tool names mismatched between slash, dot, and underscore forms
- allowlist filtering removed tools after registration
- daemon-backed agents used an older model-visible function registry than the runtime path
- Jido `agent.ask` is a one-tool bridge, not a full planner loop

Those are the exact seams new integrations should avoid.

## Recommended Use

For new app/API simulator integrations:

- start with deterministic `tick_tool` probes
- then add one constrained agent-driven action smoke
- then add richer persona/user simulators only after the action substrate is stable
