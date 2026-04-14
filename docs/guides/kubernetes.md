# Kubernetes Deployment Guide

## Current architecture-first guide

This document now points to the current architecture docs and the manifest reality in-repo:

- [Kubernetes runtime architecture](../architecture/kubernetes-runtime.md)
- [PostgreSQL + CAS architecture](../architecture/postgres-storage.md)
- [Chat adapter architecture](../architecture/chat-platform-adapter.md)

## Baseline and overlays

`deploy/kubernetes` contains a base manifest set plus overlays:

- `base/` — legacy defaults (Turso-oriented, exec probes, older CAS key names).
- `overlays/postgres/` — production-oriented PostgreSQL runtime with HTTP probes and CAS driver keys.
- `overlays/local/` — single-node/dev layout.
- `overlays/public-gui/` — Better Auth gateway in front of `foxctl web serve` for public `gui-agent` hosting.

Use the overlay that matches your control-plane strategy.

## What changed from prior docs

- Chat adapters are now a first-class runtime feature in `foxctl web serve` and documented under `architecture/chat-platform-adapter.md`.
- PostgreSQL is fully supported and wired through storage driver configuration (`docs/architecture/postgres-storage.md`), with dedicated k8s overlay configuration.
- Teams webhooks use `/api/teams/messages` and `/healthz`, `/readyz` probe endpoints.
- The live HTTP base path is `/api`; `/api/v1` is deprecated and returns an error response.
- CAS env variable migration is underway: prefer `AGENTCTL_CAS_DRIVER`/`AGENTCTL_CAS_S3_*` over old `AGENTCTL_CAS_BACKEND`/`AGENTCTL_CAS_BUCKET`.
- Jido-backed orchestration is optional and requires extra `AGENTCTL_JIDO_*` runtime configuration beyond the base manifests.

## Runbook pointers

- For local/manual deploy: use `deploy/kubernetes/base` and `deploy/kubernetes/overlays/local`.
- For shared state production: apply `deploy/kubernetes/overlays/postgres` and pair with an external PostgreSQL + S3-compatible object store.
- For public `gui-agent` hosting: apply `deploy/kubernetes/overlays/public-gui`, which composes the PostgreSQL overlay and fronts the private `foxctl` service with `gui-auth-gateway`.
- For implementation backlog and historical migration tasks, see:
  - [docs/archive/impl_plan/chat-platform-adapter.md](../archive/impl_plan/chat-platform-adapter.md)
  - [docs/archive/impl_plan/k8s-sql-storage.md](../archive/impl_plan/k8s-sql-storage.md)
