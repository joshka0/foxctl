# Kubernetes Base Manifests

This directory is the base manifest layer for `deploy/kubernetes`.

## Status

The base is not the single source of production truth. It contains older
defaults that are still useful for composition, but some settings are from an
earlier Turso/CAS generation.

Prefer reading these docs first:

- [docs/kubernetes.md](../../docs/kubernetes.md)
- [docs/architecture/kubernetes-runtime.md](../../docs/architecture/kubernetes-runtime.md)
- [docs/architecture/postgres-storage.md](../../docs/architecture/postgres-storage.md)

## How to use this directory

- Use `deploy/kubernetes/base` as the Kustomize base layer.
- Apply an overlay for the runtime/storage model you actually want.
- Do not assume the base env vars represent the recommended production setup.

Typical entrypoints:

```bash
kubectl apply -k deploy/kubernetes/overlays/local
kubectl apply -k deploy/kubernetes/overlays/postgres
```

## What is in base

| File | Role |
|------|------|
| `deployment.yaml` | Main `foxctl` deployment |
| `configmap.yaml` | Base env vars, including some legacy defaults |
| `workspace-deployment.yaml` | Optional workspace pod with git-sync sidecar |
| `embedding-worker.yaml` | Background embedding worker deployment |
| `cronjobs.yaml` | Scheduled maintenance workloads |
| `service.yaml`, `ingress.yaml` | Service exposure |
| `rbac.yaml`, `network-policy.yaml`, `pdb.yaml`, `hpa.yaml` | Operational support objects |

## Important caveats

- The base `ConfigMap` still defaults `FOXCTL_DB_DRIVER` to `turso`.
- The base also still uses older CAS key names like `FOXCTL_CAS_BACKEND` and
  `FOXCTL_CAS_BUCKET`.
- Production-oriented PostgreSQL and newer CAS settings live in
  `deploy/kubernetes/overlays/postgres`.
- Local development overrides live in `deploy/kubernetes/overlays/local`.

## Runtime notes

- The deployed process is typically `foxctl web serve`.
- Live HTTP app routes are under `/api`.
- Kubernetes probes should target `/healthz` and `/readyz`.
- Optional chat adapters and optional Jido-backed orchestration require extra
  runtime configuration beyond what this base README used to describe.
