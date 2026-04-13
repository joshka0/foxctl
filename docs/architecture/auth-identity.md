# Auth and Identity Architecture (Canonical)

Canonical map for identity propagation, authorization, token brokerage, and verification.

## Metadata

| Field | Value |
|------|-------|
| Status | Current (with known TODO boundaries) |
| Canonical scope | `internal/domain/identity`, `internal/auth`, `internal/auth/broker`, `internal/intelligence/verification` |
| Last reviewed | 2026-02-17 |

## Runtime Topology

```mermaid
flowchart TD
    Adapters[chat adapters + web handlers]
    Identity[domain/identity]
    Auth[auth (Casbin)]
    Hooks[hook policy runner]
    Broker[authbroker]
    OAuth[web OAuth callback]
    Verify[verification/CoVe]

    Adapters --> Identity
    Identity --> Auth
    Identity --> Hooks
    Identity --> Broker
    OAuth --> Broker
    Adapters --> Verify
```

## Package Responsibilities

| Package | Responsibility | Primary types/functions |
|--------|----------------|-------------------------|
| `internal/domain/identity` | Canonical principal model + context propagation | `Principal`, `Subject()`, `ConversationKey()`, `WithPrincipal`, `FromContext` |
| `internal/auth` | Casbin authorization for HTTP and hook/tool resources | `NewEnforcer`, `Enforce`, `Middleware`, `PolicyHookRunner` |
| `internal/auth/broker` | OAuth token/auth-request lifecycle and encrypted persistence | `Broker`, `Store`, `TokenRow`, `AuthRequestRow`, `Encrypt/Decrypt`, `OpenSQLite/OpenPostgres` |
| `internal/intelligence/verification` | Chain-of-Verification (baseline → claims → parallel verify → refine) | `CoVe`, `Spawner`, `CoVeRequest`, `CoVeResponse` |

## Identity Propagation Contract

1. Adapter/handler constructs `identity.Principal` (`Platform`, `UserID`, tenant/actor fields when available).
2. Principal is attached to `context.Context` via `identity.WithPrincipal`.
3. Authorization/hook logic reads from the same context via `identity.FromContext`.
4. Conversation/session routing uses `Principal.ConversationKey(...)` for tenant-aware keys.

## Authorization Contract (`internal/auth`)

| Item | Current behavior |
|------|------------------|
| Enforcer backend | Embedded Casbin model + CSV adapter (`NewEnforcer`) |
| Subject mapping | `Principal.Subject()` (`user:<platform>:<id>` or `actor:<id>`) |
| Tenant mapping | Empty tenant maps to `*` domain |
| HTTP middleware resource | `api:<path>` with `read`/`write` action derived from method |
| Hook policy resource | `tool:<name>` with `execute` action |
| Fallback mode | Nil enforcer and anonymous principals pass through |

Known boundary: `NewPostgresEnforcer` is currently a placeholder and returns not-implemented.

## OAuth Broker Contract (`internal/auth/broker`)

| Area | Current behavior |
|-----|------------------|
| Provider abstraction | `microsoft_graph`, `google`, `github` via `Provider` enum |
| Token keying | `(tenant_id, subject, provider, scopes_hash)` |
| Scope normalization | `ScopesHash(scopes, audience)` sorts scopes before hashing |
| Encryption | AES-256-GCM via `Encrypt`/`Decrypt`; key must be 32 bytes |
| Storage backends | SQLite and Postgres implementations of `Store` |
| Cleanup | Expired token/auth-request delete APIs |

Known boundary: `/api/oauth/callback` handler exists but is currently stubbed (not yet wired to `Broker.CompleteAuth`).

## Verification Subsystem Boundary

`internal/intelligence/verification` is identity-adjacent, not identity-owning.

| Stage | Function |
|------|----------|
| Baseline generation | Draft initial answer |
| Claim extraction | Parse verifiable claims |
| Parallel verification | Worker pool checks each claim |
| Refinement | Produces corrected final answer |

Primary integration surface: `skills/cove_verify` (`verification/cove_verify`).

## Security Invariants

| Invariant | Why it matters |
|----------|----------------|
| Principal is the single identity carrier across layers | Avoids subject drift between adapters, middleware, and hooks |
| Resource/action naming is stable (`api:*`, `tool:*`) | Keeps Casbin policy durable and auditable |
| OAuth blobs are encrypted at rest | Reduces token disclosure risk |
| Tenant-aware conversation keys | Prevents cross-tenant key collisions |
| Verification is explicit opt-in workflow logic | Avoids hidden policy side effects |

## Related Docs

- [docs/architecture/system-architecture.md](system-architecture.md)
- [docs/general/runtime-orchestration.md](../general/runtime-orchestration.md)
- [docs/general/context-and-observability.md](../general/context-and-observability.md)
- [docs/spec/protocol_v1.md](../spec/protocol_v1.md)
