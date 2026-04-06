# Kubernetes Runtime Architecture (Current + In-Repo)

This page describes the in-repo Kubernetes topology and how it maps to the
current web/runtime behavior in code.

## High-level topology

```mermaid
flowchart LR
    Internet[Users / APIs] --> Ingress
    Ingress --> AgentCtl["agentctl Deployment\n(web API + websocket + optional chat adapter)"]
    AgentCtl --> Store[Logical stores via dbdriver]
    Store --> PostgreSQL[(PostgreSQL)] 
    Store --> SQLite[(SQLite local fallback)]
    AgentCtl --> CAS[(CAS: s3/sqlite/file)]
    AgentCtl --> Runtime["legacy agent runtime + v2 services"]
    Runtime --> Jido["optional Jido bridge"]
    AgentCtl --> Embeds["embedding / retrieval jobs"]
    AgentCtl --> Observability["/api/events / logs / probes"]
```

## What is represented in-repo today

`deploy/kubernetes` currently contains three practical operational modes:

1. Base manifests (legacy defaults)
2. PostgreSQL overlay
3. Local overlay (k3s/dev variants)

## Base manifests (legacy-biased snapshot)

`deploy/kubernetes/base/` is the base set used as the foundation:

- `deployment.yaml`: `agentctl` pod with web service and readiness/liveness probes using `agentctl health` exec checks.
- `configmap.yaml`: defaulting to `AGENTCTL_DB_DRIVER=turso` and old CAS key names (`AGENTCTL_CAS_BACKEND`/`AGENTCTL_CAS_BUCKET`).
- `workspace-deployment.yaml`: optional dedicated workspace pod with git-sync sidecar for mounted source.
- `cronjobs.yaml`: scheduled companion workloads (rerank, sqlite maintenance), still wired for the same env style as base.
- `embedding-worker.yaml`: separate worker deployment for background embedding pipeline.

This base is useful as a starting point or historical template but still
contains mixed configuration generations.

## PostgreSQL production overlay (implemented)

`deploy/kubernetes/overlays/postgres/` updates the base to the storage and runtime model currently reflected in code:

- `configmap-patch.yaml`
  - `AGENTCTL_DB_DRIVER: "postgres"`
  - `AGENTCTL_POSTGRES_MAX_CONNS`, `AGENTCTL_POSTGRES_REQUIRE_VECTOR`
  - `AGENTCTL_CAS_DRIVER: "s3"` and `AGENTCTL_CAS_S3_BUCKET`/region settings
- `deployment-patch.yaml`
  - Sets `AGENTCTL_POSTGRES_DSN` from Kubernetes secret
  - Replaces probe model to HTTP:
    - `GET /healthz` (liveness)
    - `GET /readyz` (readiness)

This overlay matches current server behavior where:

- `agentctl web serve` exposes application routes under `/api`
- `/api/v1` is deprecated
- root-level `/healthz` and `/readyz` are used for probes

## Local overlay (dev)

`deploy/kubernetes/overlays/local/`:

- Runs a single pod and local state directories in `emptyDir`.
- Executes `agentctl web serve --port=8080` from the container command.
- Includes local PostgreSQL + pgvector statefulset (`local-postgres.yaml`) when local multi-component testing is desired.

## Runtime notes for Kubernetes

- The web pod is the main entrypoint for:
  - `/api/*`
  - `/ws/console/*`
  - optional chat adapter ingress
- The runtime behind that pod is hybrid:
  - legacy mailbox-driven agent runtime still exists
  - v2 projections/orchestration/context surfaces also exist
- Jido-backed orchestration requires additional `AGENTCTL_JIDO_*` configuration;
  those variables are not broadly documented in the base manifests yet.

## Chat adapters in Kubernetes

Adapters are not selected in manifests. They are runtime CLI flags:

- `agentctl web serve --chat discord`
- `agentctl web serve --chat telegram`
- `agentctl web serve --chat teams`

Teams webhooks use `POST /api/teams/messages`; this endpoint must remain
reachable in whichever ingress/network policies are in use.

## Deployment architecture notes for multi-pod operation

- Shared control-plane state is required for horizontal scaling:
  - PostgreSQL-backed sessions/sessions-like stores
  - CAS in external object storage
  - Shared locks (companion turn lock via PostgreSQL when available)
- Local-only SQLite mode can still run for dev and smaller deployments but does
  not provide true shared-state semantics across replicas.

## Environment keys to use when documenting runbooks

Prefer:

- `AGENTCTL_DB_DRIVER`
- `AGENTCTL_POSTGRES_DSN` / `DATABASE_URL`
- `AGENTCTL_POSTGRES_MAX_CONNS`
- `AGENTCTL_POSTGRES_REQUIRE_VECTOR`
- `AGENTCTL_CAS_DRIVER`
- `AGENTCTL_CAS_S3_*`
- `AGENTCTL_WORKSPACE`, `AGENTCTL_STORAGE_ROOT`
- `AGENTCTL_JIDO_*` when enabling Jido-backed orchestration or companion bridges

Legacy keys still seen in base artifacts:

- `AGENTCTL_CAS_BACKEND`
- `AGENTCTL_CAS_BUCKET`

## Related docs

- Current architecture notes:
  - [docs/architecture/chat-platform-adapter.md](chat-platform-adapter.md)
  - [docs/architecture/postgres-storage.md](postgres-storage.md)
- Historical plans:
  - [docs/archive/impl_plan/k8s-sql-storage.md](../archive/impl_plan/k8s-sql-storage.md)
