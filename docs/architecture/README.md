# Architecture Documentation

This folder contains **architectural** documentation for current in-repo behavior.

- [`system-architecture.md`](./system-architecture.md): canonical architecture map for `cmd/foxctl` + `internal/*`
- [`package-topology.md`](./package-topology.md): canonical grouping map for `internal/*`, including explicit legacy-runtime vs `v2` replacement boundaries
- [`context-architecture.md`](./context-architecture.md): workspace-local ACA control plane and computed top-of-mind slice
- [`rlm-gather-context.md`](./rlm-gather-context.md): RLM `gather_context` tool over contextengine retrieval, reduction, and certification
- [`jido-hybrid-runtime.md`](./jido-hybrid-runtime.md): canonical hybrid-runtime split between Jido orchestration and `foxctl` semantic ownership
- [`auth-identity.md`](./auth-identity.md): canonical auth/identity/token-broker/verification architecture
- [`chat-platform-adapter.md`](./chat-platform-adapter.md): chat adapter runtime
- [`kubernetes-runtime.md`](./kubernetes-runtime.md): Kubernetes deployment topology
- [`postgres-storage.md`](./postgres-storage.md): PostgreSQL and CAS architecture for shared control-plane state

When a topic is mostly an implementation plan (phased rollout, to-do list, checklist),
its doc stays under `docs/plans/*`.

Operational subsystem references (skills, storage, hooks, runtime details) live under
`docs/general/*` and should point back here for canonical architecture.

Planned repo-evolution tooling is tracked in
[`docs/plans/features/foxctl-evolve-plan.md`](../plans/features/foxctl-evolve-plan.md).
Until it ships, treat that document as rollout/design guidance rather than
current as-built architecture.

Current implementation-reference docs to prefer:

- [docs/architecture/system-architecture.md](./system-architecture.md)
- [docs/architecture/package-topology.md](./package-topology.md)
- [docs/architecture/context-architecture.md](./context-architecture.md)
- [docs/architecture/rlm-gather-context.md](./rlm-gather-context.md)
- [docs/architecture/jido-hybrid-runtime.md](./jido-hybrid-runtime.md)
- [docs/architecture/auth-identity.md](./auth-identity.md)
- [docs/guides/kubernetes.md](../guides/kubernetes.md)
- [docs/architecture/chat-platform-adapter.md](./chat-platform-adapter.md)
- [docs/architecture/kubernetes-runtime.md](./kubernetes-runtime.md)
- [docs/architecture/postgres-storage.md](./postgres-storage.md)
