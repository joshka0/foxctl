# K8s Multi-Tenant Conversation Runtime — Plan Index

## Vision

Transform foxctl from a single-process CLI tool into a multi-tenant conversation runtime deployable on Kubernetes, with proper authorization, background job processing, and horizontal scaling.

## Current Status

- Plans 01-04b: Complete
- Plan 04c: In Progress
- Plan 04d: Complete
- Plan 05: In Progress
- Plan 06: Deferred

## Dependency Graph

```
01-principal-and-tenant-isolation
    |
    ├──> 02-casbin-authorization
    |
    ├──> 03-river-background-jobs
    |         |
    |         └──> 03b-approval-workflows (future)
    |
    └──> 04-turn-serialization-and-distribution
              |
              ├──> 04a: In-memory TurnLock (bug fix, do now)
              ├──> 04b: Pg advisory locks (multi-pod)
              ├──> 04c: River conversation routing (scaling)
              └──> 04d: K8s session affinity (optimization)
```

## Plans

| # | Plan | Status | Priority | Key Dependency |
|---|------|--------|----------|----------------|
| [01](01-principal-and-tenant-isolation.md) | Principal Type & Tenant Isolation | Complete | **P0** | None |
| [02](02-casbin-authorization.md) | Casbin Authorization Layer | Complete | P1 | 01 |
| [03](03-river-background-jobs.md) | River Background Jobs | Complete | P1 | 01 |
| [04](04-turn-serialization-and-distribution.md) | Turn Serialization & Distribution | In Progress (04c); Complete (04a, 04b, 04d) | **P0** (4a) / P2 (4b-d) | 01, 03 |
| [05](05-oauth-authbroker.md) | OAuth AuthBroker | In Progress | P2 | 01, 03 |

## What We Explicitly Defer

| Technology | Why Deferred |
|-----------|-------------|
| **Proto.Actor Cluster** | Alpha in Go; Postgres advisory locks + River suffice to ~50 pods |
| **Temporal Workflows** | River covers 90% of needs; Temporal adds infra ops burden |
| **OPA** | Casbin is simpler for embedded authz; OPA is for distributed policy |
| **Full CQRS/Event Sourcing** | Existing L0/L1/L2 memory layers handle this domain-specifically |

## New Dependencies

| Library | Version | Purpose | Plan |
|---------|---------|---------|------|
| `github.com/casbin/casbin/v2` | v2.x | RBAC/ABAC authorization | 02 |
| `github.com/casbin/gorm-adapter/v3` | v3.x | Postgres policy storage | 02 |
| `github.com/riverqueue/river` | v0.26+ | Background job queue | 03 |
| `github.com/riverqueue/river/riverdriver/riverpgxv5` | v0.26+ | River pgx driver | 03 |
| `github.com/jackc/pgx/v5` | v5.x | Postgres pool for River | 03 |

## Implementation Order

1. **Plan 01** + **Plan 04a** (concurrent, no external deps) — fix identity + concurrency bugs
2. **Plan 03** (River) — replace homegrown job primitives
3. **Plan 02** (Casbin) — add authorization layer
4. **Plan 04b-d** (distribution) — scale to multi-pod when needed
