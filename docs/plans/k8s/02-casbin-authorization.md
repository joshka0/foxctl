# Plan 02: Casbin Authorization Layer

**Status**: Proposed
**Depends on**: 01-principal-and-tenant-isolation
**Blocks**: None (can proceed in parallel with 03)

## Problem

Authorization is currently emergent from hook configuration and tool allowlists:

- Hook skills independently decide allow/block with no centralized policy model
- No `(subject, action, resource)` triple — tool names are implicit "actions"
- No tenant/org scoping in authorization decisions
- Web API has zero auth middleware (only CORS)
- `FilteredToolExecutor` is a flat name-based allowlist, not role-aware
- Teams JWT auth is adapter-specific and doesn't feed into a generalized authz layer

## Why Casbin (Not OPA)

| Factor | Casbin | OPA |
|--------|--------|-----|
| Deployment | In-process library | Sidecar/service |
| Latency | Sub-millisecond (in-memory) | Network hop |
| Go integration | Native, 20+ middleware adapters | REST/gRPC client |
| Policy language | PERM model (simple CONF) | Rego (learning curve) |
| Storage | Postgres adapter (gorm-adapter) | External bundle server |
| Fit for embedded authz | Excellent | Overkill |

## Design

### Policy Model

```ini
# internal/auth/model.conf (RBAC with tenant + resource attributes)

[request_definition]
r = sub, tenant, obj, act

[policy_definition]
p = sub, tenant, obj, act, eft

[role_definition]
g = _, _, _   # user, role, tenant (tenant-scoped roles)

[policy_effect]
e = some(where (p.eft == allow)) && !some(where (p.eft == deny))

[matchers]
m = g(r.sub, p.sub, r.tenant) && \
    (p.tenant == "*" || p.tenant == r.tenant) && \
    keyMatch2(r.obj, p.obj) && \
    (p.act == "*" || p.act == r.act)
```

This supports:
- **Tenant-scoped roles**: Alice is `admin` in tenant A but `viewer` in tenant B
- **Wildcard tenants**: Global policies with `tenant = "*"`
- **Resource patterns**: `keyMatch2` supports `/api/companion/*`, `tool:fs.*`
- **Explicit deny**: Deny rules override allow rules

### Example Policies

```csv
# Role assignments (user, role, tenant)
g, user:teams:alice, admin, tenant:contoso
g, user:teams:bob, viewer, tenant:contoso
g, user:discord:charlie, admin, *

# Tool execution policies (role, tenant, resource, action, effect)
p, admin, *, tool:*, execute, allow
p, viewer, *, tool:search, execute, allow
p, viewer, *, tool:memory, execute, allow
p, viewer, *, tool:fs.write, execute, deny
p, viewer, *, tool:exec.*, execute, deny

# API access policies
p, admin, *, /api/companion/*, *, allow
p, viewer, *, /api/companion/conversations, read, allow
p, viewer, *, /api/companion/chat, write, allow

# Agent spawning
p, admin, *, agent:spawn, execute, allow
p, viewer, *, agent:spawn, execute, deny
```

### Architecture

```
HTTP Request / Chat Message
         |
         v
   Auth Middleware (JWT/session → Principal)
         |
         v
   context.Context carries Principal
         |
         ├─── API Handler: enforcer.Enforce(principal, tenant, path, method)
         |
         └─── Hook Dispatcher
                  |
                  v
              PolicyHookRunner (PreToolUse)
                  |
                  v
              enforcer.Enforce(principal, tenant, "tool:"+toolName, "execute")
                  |
                  ├── allow → continue
                  └── deny → DecisionBlock
```

## Files to Create

| File | Purpose |
|------|---------|
| `internal/auth/enforcer.go` | Casbin enforcer factory + helpers |
| `internal/auth/model.conf` | RBAC model definition (embedded via `embed`) |
| `internal/auth/middleware.go` | HTTP auth middleware (extract JWT → Principal → enforce) |
| `internal/auth/policy_hook.go` | Hook runner that delegates to Casbin enforcer |
| `internal/auth/enforcer_test.go` | Unit tests with in-memory policy |
| `internal/auth/middleware_test.go` | HTTP middleware tests |

## Files to Modify

### `internal/runtime/hooks/dispatcher.go`

Add `PolicyHookRunner` as a built-in hook that fires before skill-based hooks:

```go
// In Dispatch(), before iterating hook entries:
if d.policyRunner != nil && input.Event == EventPreToolUse {
    decision, err := d.policyRunner.Evaluate(ctx, input)
    if err != nil {
        // fail-closed for policy errors
        return &Result{Output: hooks.NewBlock("policy error: " + err.Error(), nil)}, nil
    }
    if decision == DecisionBlock {
        return &Result{Output: hooks.NewBlock("denied by policy", nil)}, nil
    }
}
```

### `internal/runtime/hooks/types.go`

Ensure `Input.Principal` (from Plan 01) carries through to policy evaluation.

### `internal/interfaces/web/server.go`

Wire auth middleware into the HTTP mux:

```go
// In Handler():
if s.enforcer != nil {
    apiMux = auth.Middleware(s.enforcer)(apiMux)
}
```

### `internal/context/companion/service.go`

Pass enforcer to companion service for inline tool authorization checks.

### `go.mod`

```
github.com/casbin/casbin/v2 v2.x.x
github.com/casbin/gorm-adapter/v3 v3.x.x  // for Postgres policy storage
```

## Implementation Steps

### Step 1: Casbin Enforcer Factory

```go
// internal/auth/enforcer.go
package auth

import (
    _ "embed"
    "github.com/casbin/casbin/v2"
    "github.com/casbin/casbin/v2/model"
    stringadapter "github.com/casbin/casbin/v2/persist/string-adapter"
)

//go:embed model.conf
var modelConf string

// NewEnforcer creates a Casbin enforcer with the built-in RBAC model.
// adapter can be a string adapter (for testing), file adapter, or gorm adapter.
func NewEnforcer(policyCSV string) (*casbin.Enforcer, error) {
    m, err := model.NewModelFromString(modelConf)
    if err != nil {
        return nil, fmt.Errorf("parse casbin model: %w", err)
    }
    a := stringadapter.NewAdapter(policyCSV)
    return casbin.NewEnforcer(m, a)
}

// NewPostgresEnforcer creates an enforcer backed by Postgres policy storage.
func NewPostgresEnforcer(dsn string) (*casbin.Enforcer, error) {
    m, err := model.NewModelFromString(modelConf)
    if err != nil {
        return nil, fmt.Errorf("parse casbin model: %w", err)
    }
    a, err := gormadapter.NewAdapter("postgres", dsn)
    if err != nil {
        return nil, fmt.Errorf("create gorm adapter: %w", err)
    }
    return casbin.NewEnforcer(m, a)
}

// Enforce checks if the principal can perform action on resource in tenant.
func Enforce(e *casbin.Enforcer, principal identity.Principal, resource, action string) (bool, error) {
    sub := principal.UserID
    if sub == "" {
        sub = principal.ActorID
    }
    tenant := principal.TenantID
    if tenant == "" {
        tenant = "*"
    }
    return e.Enforce(sub, tenant, resource, action)
}
```

### Step 2: Policy Hook Runner

```go
// internal/auth/policy_hook.go
package auth

// PolicyHookRunner evaluates Casbin policies at PreToolUse.
type PolicyHookRunner struct {
    enforcer *casbin.Enforcer
}

func (r *PolicyHookRunner) Evaluate(ctx context.Context, input hooks.Input) (hooks.Decision, error) {
    if r.enforcer == nil {
        return hooks.DecisionApprove, nil
    }
    resource := "tool:" + input.ToolName
    allowed, err := Enforce(r.enforcer, input.Principal, resource, "execute")
    if err != nil {
        return hooks.DecisionBlock, err
    }
    if !allowed {
        return hooks.DecisionBlock, nil
    }
    return hooks.DecisionApprove, nil
}
```

### Step 3: HTTP Auth Middleware

```go
// internal/auth/middleware.go
package auth

func Middleware(enforcer *casbin.Enforcer) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            principal := identity.FromContext(r.Context())
            if principal.IsAnonymous() {
                // No auth configured — pass through (single-tenant mode)
                next.ServeHTTP(w, r)
                return
            }
            method := strings.ToLower(r.Method)
            action := "read"
            if method == "post" || method == "put" || method == "patch" || method == "delete" {
                action = "write"
            }
            allowed, err := Enforce(enforcer, principal, r.URL.Path, action)
            if err != nil || !allowed {
                http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

## Default Policy (Single-Tenant Fallback)

When no Casbin enforcer is configured (single-tenant, local dev), all authorization passes. The `PolicyHookRunner` returns `DecisionApprove` when `enforcer == nil`. HTTP middleware passes through when `Principal.IsAnonymous()`.

This preserves 100% backward compatibility.

## Verification

1. `go test ./internal/auth/...` — enforcer + middleware tests
2. Unit test: admin role can execute all tools
3. Unit test: viewer role blocked from `fs.write`, `exec.*`
4. Unit test: tenant isolation (user in tenant A cannot access tenant B resources)
5. Integration: wire enforcer into hook dispatcher, verify PreToolUse block
6. Integration: HTTP middleware blocks unauthorized API requests
