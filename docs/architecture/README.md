# Architecture Documentation

This folder contains **architectural** documentation for current in-repo behavior.

- [`chat-platform-adapter.md`](./chat-platform-adapter.md): chat adapter runtime
- [`kubernetes-runtime.md`](./kubernetes-runtime.md): Kubernetes deployment topology
- [`postgres-storage.md`](./postgres-storage.md): PostgreSQL and CAS architecture for shared control-plane state

When a topic is mostly an implementation plan (phased rollout, to-do list, checklist),
its doc stays under `docs/plans/*`.

Current implementation-reference docs to prefer:

- [docs/kubernetes.md](../kubernetes.md)
- [docs/architecture/chat-platform-adapter.md](./chat-platform-adapter.md)
- [docs/architecture/kubernetes-runtime.md](./kubernetes-runtime.md)
- [docs/architecture/postgres-storage.md](./postgres-storage.md)
