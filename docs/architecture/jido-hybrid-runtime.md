# Jido Hybrid Runtime

This is the canonical architecture note for the current hybrid `agentctl` + Jido runtime shape.

## Metadata

| Field | Value |
|------|-------|
| Status | Current |
| Canonical scope | Ownership split between `agentctl` and Jido, transport boundaries, deployment shape |
| Last reviewed | 2026-03-06 |

## Why This Exists

The repo is currently not a single-runtime system.

- `agentctl` still owns tool semantics, memory/session retrieval, companion context assembly, and most Go-side control-plane state.
- Jido is now a viable runtime substrate for hierarchical orchestration, especially where BEAM process supervision and parent/child trees are the right primitive.

The purpose of the hybrid architecture is to avoid rewriting mature `agentctl` semantics in Elixir while still gaining BEAM-native orchestration.

## Ownership Split

### Jido Owns

- agent process lifecycle
- parent/child hierarchy
- runtime signal delivery
- subtree inspection and await behavior
- orchestration runtime substrate for overseer-style dispatch

### agentctl Owns

- `code_*` skills such as semantic search, smart search, context grep, and codemap-related context
- `memory_*` retrieval and persistence
- `session_*` recall and timeline retrieval
- layered companion context shaping
- kanban/control-plane state, v2 events, and projections

That split is deliberate. Jido is the runtime. `agentctl` remains the semantic system.

## Topology

```mermaid
flowchart TD
    CLI[agentctl CLI / web / GUI / TUI]
    V2[v2 services + orchestration runtime]
    Proj[v2 events + projections]
    Ctx[companion + contextbuilder]
    JidoBridge[internal/v2/adapters/jido]
    JR[Jido JSON-RPC bridge]
    Jido[Jido runtime / AgentServer tree]
    Tools[agentctl tool + memory + session stack]

    CLI --> V2
    CLI --> Ctx
    V2 --> Proj
    V2 --> JidoBridge
    Ctx --> Tools
    JidoBridge --> JR
    JR --> Jido
    Jido --> Tools
    Tools --> Proj
```

## Runtime Boundaries

### Inside agentctl

Key packages:

- `internal/v2/adapters/jido`
- `internal/v2/runtime/orchestration`
- `internal/v2/services`
- `internal/companion`
- `internal/web/api`

Those packages translate canonical Go-side requests into JSON-RPC runtime calls:

- `runtime.start_agent`
- `runtime.spawn_child`
- `runtime.signal`
- `runtime.await`
- `runtime.get_children`
- `runtime.state`

### Inside Jido

The bridge side exposes:

- `agent.ask`
- `agentctl.tool.run`
- `agentctl.companion.context`
- `agentctl.child.spawn`

Those actions are generic. They call back into `agentctl` instead of reimplementing code search or memory/session semantics inside Elixir.

## Socket Model

There are two Unix sockets in the clean production shape:

| Socket | Purpose |
|------|---------|
| `AGENTCTL_JIDO_SOCKET` | JSON-RPC socket exposed by the Jido bridge |
| `AGENTCTL_DAEMON_SOCKET` | `agentctl` daemon socket used by bridge-side `daemon_rpc` tool execution |

This separation matters:

- the Jido bridge socket is the runtime-control boundary
- the `agentctl` daemon socket is the semantic-execution boundary

Keeping them separate lets you isolate transport failures and keep tool/memory/session execution authoritative on the Go side.

## Tool Policy Hand-off

Jido start/spawn payloads now carry bridge-side tool execution policy through `plugin_config`.

Current shape:

- `plugin_config.binary`
- `plugin_config.workspace`
- `plugin_config.transport = daemon_rpc`
- `plugin_config.daemon = true`
- `plugin_config.tool_command.profile`
- `plugin_config.tool_command.allowed_tools`
- `plugin_config.tool_command.default_timeout_ms`

That payload is derived from the shared v2 catalog/profile model on the Go side, so Jido-facing agent startup inherits the same portable-core vs extension-tool boundary used by v2 runtime governance.

## Control-Plane Flow

Current intended flow:

1. `agentctl` enqueues or projects work into the kanban/control-plane model.
2. v2 orchestration decides dispatch and converts that into Jido runtime child-spawn requests.
3. Jido owns the live child tree and runtime lifecycle.
4. Jido children call back into `agentctl` for `code_*`, `memory_*`, `session_*`, and companion context.
5. Runtime outcomes reconcile back into append-only v2 events and projections.

This is why kanban, overseer policy, and Jido runtime fit together without collapsing into one implementation layer.

## Memory and Retention

The hybrid shape also preserves retention policy on the semantic side.

- Jido agents can carry `profile` and `memory_retention`.
- Bridge-side companion context uses those values to decide how much `memory/query`, `session/recall`, and `session/timeline` context to assemble.
- `agentctl` remains the place where retrieval quality evolves, including vector search, semantic recall, and timeline shaping.

That is important because retention behavior is not just runtime state. It depends on the retrieval stack you already have in Go.

## Deployment Shape

Recommended production shape:

1. run your Jido instance and bridge under one OTP supervision tree
2. point the bridge at an explicit `agentctl` binary and daemon socket
3. let `agentctl` own storage, CAS, memory/session indexes, and control-plane projections

In other words:

- Jido should be supervised like runtime infrastructure
- `agentctl` should be treated as the semantic backend

## Related Docs

- [docs/architecture/system-architecture.md](system-architecture.md)
- [docs/general/runtime-orchestration.md](../general/runtime-orchestration.md)
- [docs/general/companion-memory.md](../general/companion-memory.md)
- [docs/spec/agent_hierarchy.md](../spec/agent_hierarchy.md)
- [Jido guide: Agentctl Bridge](https://github.com/agentjido/jido/blob/main/guides/agentctl-bridge.md)
