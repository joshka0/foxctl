# Agent Sandbox Integration Design

**Status:** Investigation / Draft
**Date:** 2026-06-27
**Author:** Kai (Hermes), Josh

## Goal

Integrate [kubernetes-sigs/agent-sandbox](https://github.com/kubernetes-sigs/agent-sandbox) as a k8s-native execution backend for foxctl agent sessions on EKS. Each agent session gets its own isolated, stateful sandbox pod with persistent storage and hibernation support.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Sandbox granularity | Per session | Full isolation between agent sessions |
| Container runtime | Standard (gVisor later) | Start simple, isolate via namespace + network policy |
| Hibernation | Yes | Pause idle agent sessions to save EKS costs |
| Relationship to WASI | Complement | WASI stays for local dev; k8s sandbox for production EKS |

## Architecture

```
                    foxctl agent daemon (in k8s)
                           │
                    ┌──────┴──────┐
                    │ Session     │
                    │ Manager     │
                    └──────┬──────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
         Exec Runner   WASI Runner  K8s Sandbox Runner
         (local dev)   (isolated)   (EKS production)
              │            │            │
         subprocess    wazero WASM  SandboxClaim → Pod
                                      │
                                 ┌────┴────┐
                                 │ Sandbox │ (stable identity, PVC, hibernate)
                                 └─────────┘
```

## Integration Points

### 1. New Package: internal/runtime/execution/k8ssandbox/

Implements `SkillExecutor` via the agent-sandbox Go SDK.

```go
package k8ssandbox

// Runner executes skills inside k8s agent-sandbox pods.
type Runner struct {
    client    *sandbox.Client
    warmPool  string
    namespace string
    options   sandbox.Options
}

// NewRunner creates a k8s sandbox runner.
func NewRunner(cfg Config) (*Runner, error)

// Execute implements execution.SkillExecutor.
func (r *Runner) Execute(ctx context.Context, opts execution.ExecuteOptions) (*execution.Result, error)
```

Flow:
1. Create or reuse sandbox from warm pool (`client.CreateSandbox`)
2. Write skill binary/script to sandbox (`sb.Write`)
3. Run skill command (`sb.Run`)
4. Capture stdout/stderr from result
5. Return mapped `execution.Result`

### 2. Session-to-Sandbox Mapping

Each `AgentSession.ID` maps to a sandbox claim:

```
AgentSession.ID = "01KT4E0M4YJ8W5PJYKD028W7B6"
SandboxClaim   = "foxctl-session-01kt4e0m4yj8w5pjykd028w7b6"
```

- Sandbox is created when the session starts
- Sandbox persists across skill executions within the session
- Sandbox hibernates when session is idle (configurable TTL)
- Sandbox is deleted when session ends

### 3. Runner Selection

Add `distribution.type: "k8s-sandbox"` to skill.yaml for skills that should run in sandboxes. Or use a global runtime config to route all skill execution through sandboxes when running in EKS.

```yaml
# skill.yaml
distribution:
  type: k8s-sandbox
  k8sSandbox:
    warmPool: foxctl-agent-pool
    image: foxctl/skill-runner:latest
    hibernateAfterIdle: 30m
```

### 4. Deployment: deploy/kubernetes/sandbox-runtime/

```yaml
# SandboxWarmPool - pre-warmed pods ready for instant allocation
apiVersion: agents.x-k8s.io/v1beta1
kind: SandboxWarmPool
metadata:
  name: foxctl-agent-pool
  namespace: foxctl-agent
spec:
  template:
    spec:
      containers:
      - name: runner
        image: foxctl/skill-runner:latest
        resources:
          requests:
            memory: "512Mi"
            cpu: "500m"
          limits:
            memory: "2Gi"
            cpu: "2"
  minAvailable: 3
  maxAvailable: 10
```

### 5. Hibernation Support

When a session is idle:
1. Session manager calls `sandbox.Hibernate()` (when SDK supports it)
2. Pod state is saved to PVC
3. Pod scales to zero
4. On next session activity, `sandbox.Resume()` restores state

This saves EKS compute costs during idle periods.

## Go SDK Dependency

```
go get sigs.k8s.io/agent-sandbox/clients/go/sandbox@latest
```

Current version: v0.1.0 (v1beta1 CRDs). Requires Go 1.26+ — foxctl is on Go 1.25.

**Mitigation:** Vendor the client interface or wait for Go 1.26 bump. The SDK is a thin HTTP client over the sandbox router service — we could also implement the protocol directly if the Go version is a blocker.

## Configuration

```yaml
# foxctl config
runtime:
  executor: k8s-sandbox  # exec | wasi | k8s-sandbox
  k8sSandbox:
    warmPool: foxctl-agent-pool
    namespace: foxctl-agent
    mode: gateway         # gateway | port-forward | direct
    gatewayName: foxctl-gateway
    gatewayNamespace: foxctl-agent
    hibernateAfterIdle: 30m
    maxLifetime: 24h
```

## Connectivity Modes

| Mode | Use Case | Config |
|------|----------|--------|
| Gateway | Production EKS | `mode: gateway`, Gateway API + LoadBalancer |
| Port-forward | Local dev with k3s | `mode: port-forward`, SPDY tunnel |
| Direct URL | In-cluster agent daemon | `mode: direct`, k8s DNS service URL |

## Migration Path

1. **Phase 1:** POC — implement `k8ssandbox.Runner`, test against k3s
2. **Phase 2:** Deploy controller + warm pool to EKS staging
3. **Phase 3:** Route agent sessions through sandboxes in staging
4. **Phase 4:** Production rollout with gateway mode + gVisor

## Open Questions

- Go 1.25 vs 1.26: can we bump, or do we need to vendor/rewrite the SDK?
- Sandbox image: what base image? Alpine + foxctl binary? Custom skill-runner?
- Network policy: how restrictive? Allow egress to model APIs but block lateral movement?
- Observability: how to surface sandbox metrics (CPU/memory/hibernation status) in foxctl's obs API?
