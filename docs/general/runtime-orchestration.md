# Runtime Orchestration

Machine-friendly map of how runs/agents move through the runtime stack.

## Scope

| In scope | Out of scope |
|---------|---------------|
| Agent daemon loop, run pipeline, queue/workflow orchestration, session services | Deep per-store schema details (see `storage.md`) |

## Core Packages

| Package(s) | Responsibility |
|-----------|----------------|
| `internal/agent` | Agent runtime, role/tool wiring, daemon-facing orchestration |
| `internal/daemon` | Long-lived daemon service and background workers |
| `internal/execution` | Exec/WASI runners, scheduling, quota enforcement |
| `internal/engine` | Tool-loop engine and provider/tool routing |
| `internal/runservice` | Skill run lifecycle + job persistence integration |
| `internal/queue` | SQLite-backed reusable queue primitive |
| `internal/workflow` | DAG workflow execution for chained skills |
| `internal/sessionkit` | Session archive/snapshot/parsing utilities |
| `internal/skillrun` | Resolve + execute + decode helper for skills |

## Execution Path

1. CLI/daemon receives a run or ask request.
2. Runtime resolves skill/tool contract and execution strategy.
3. Execution layer runs via exec/WASI and captures envelopes/artifacts.
4. Runservice persists job/session/trajectory metadata.
5. Queue/workflow layers schedule follow-up work when configured.

## Agent Execution Modes

| Mode | Behavior |
|------|----------|
| `reactive` | Waits for mailbox asks and responds turn-by-turn |
| `autonomous` | Runs bounded autonomous turns and exits |
| `proactive` | Runs autonomous turns and continues periodic polling/think cycles |

## Related Docs

- `docs/general/agent-daemon.md`
- `docs/general/storage.md`
- `docs/architecture/system-architecture.md`
