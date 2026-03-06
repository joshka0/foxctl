# Architecture Documentation

This folder contains **architectural** documentation for current in-repo behavior.

- [`system-architecture.md`](./system-architecture.md): canonical architecture map for `cmd/agentctl` + `internal/*`
- [`jido-hybrid-runtime.md`](./jido-hybrid-runtime.md): canonical hybrid-runtime split between Jido orchestration and `agentctl` semantic ownership
- [`auth-identity.md`](./auth-identity.md): canonical auth/identity/token-broker/verification architecture
- [`chat-platform-adapter.md`](./chat-platform-adapter.md): chat adapter runtime
- [`kubernetes-runtime.md`](./kubernetes-runtime.md): Kubernetes deployment topology
- [`postgres-storage.md`](./postgres-storage.md): PostgreSQL and CAS architecture for shared control-plane state

When a topic is mostly an implementation plan (phased rollout, to-do list, checklist),
its doc stays under `docs/plans/*`.

Operational subsystem references (skills, storage, hooks, runtime details) live under
`docs/general/*` and should point back here for canonical architecture.

Current implementation-reference docs to prefer:

- [docs/architecture/system-architecture.md](./system-architecture.md)
- [docs/architecture/jido-hybrid-runtime.md](./jido-hybrid-runtime.md)
- [docs/architecture/auth-identity.md](./auth-identity.md)
- [docs/kubernetes.md](../kubernetes.md)
- [docs/architecture/chat-platform-adapter.md](./chat-platform-adapter.md)
- [docs/architecture/kubernetes-runtime.md](./kubernetes-runtime.md)
- [docs/architecture/postgres-storage.md](./postgres-storage.md)
