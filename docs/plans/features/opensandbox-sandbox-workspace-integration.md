# OpenSandbox Sandbox Workspace Integration

Status: proposed

## Goal

Let agents launched from the public `gui-agent` work on a real repository
workspace without giving the public control plane direct access to host paths or
shared writable state.

The target model is:

- each task/agent gets an isolated sandbox
- the sandbox gets a shallow clone of the repo as its real `WorkspaceRoot`
- the sandbox can read relevant memories/embeddings through a controlled
  retrieval surface
- any write-back into canonical stores or the main repo is explicit

## Why This Is Needed

The current public GUI stack can now:

- authenticate through `gui-auth-gateway`
- spawn agents through the web API
- run an agent turn with a reachable LLM

But the spawned agent does not automatically see the operator's local machine
workspace.

Current limitations:

- `workspace_id` is mostly a logical label, not a mounted filesystem root
- the web spawn path passes `workspace_id`, but not a real `WorkspaceRoot`
- the local OrbStack proof stack runs agents against pod-local filesystem state

Relevant code:

- [internal/interfaces/web/api/agents.go](../../../internal/interfaces/web/api/agents.go)
- [internal/runtime/daemon/service.go](../../../internal/runtime/daemon/service.go)
- [internal/agent/runtime/runtime.go](../../../internal/agent/runtime/runtime.go)
- [internal/interfaces/web/api/workspaces.go](../../../internal/interfaces/web/api/workspaces.go)

## Why OpenSandbox Fits

The local clone at `~/repos/githubs/OpenSandbox` shows the right primitives for
this problem:

- sandbox lifecycle API with create/get/delete/pause/resume
- Docker and Kubernetes runtimes
- volume support
- ingress/egress controls
- examples for coding-agent style execution

Most relevant surfaces from the clone:

- `server/README.md`
- `specs/sandbox-lifecycle.yml`
- `examples/agent-sandbox/README.md`
- `examples/host-volume-mount/README.md`
- `examples/kubernetes-pvc-volume-mount/README.md`
- `examples/codex-cli/README.md`
- `examples/claude-code/README.md`
- `oseps/0002-kubernetes-sigs-agent-sandbox-support.md`

The important match is that OpenSandbox already thinks in terms of:

- per-sandbox lifecycle
- mounted volumes or other workspace materialization
- agent-oriented command execution environments
- controlled network policy

## Proposed Architecture

### 1. Public Control Plane

Keep the current public-facing path:

- `gui-auth-gateway`
- authenticated `gui-agent`
- private `agentctl` service

That remains the operator/control layer.

### 2. Sandbox Provisioning Layer

Add an `agentctl` sandbox provider abstraction:

- create sandbox
- inspect sandbox
- execute bootstrap commands
- destroy sandbox

First provider target:

- `OpenSandbox`

This should sit behind a narrow adapter rather than spreading OpenSandbox API
calls across the web handlers.

### 3. Real Workspace Model

Every sandboxed agent should have:

- `WorkspaceID`: logical repo/workspace identity
- `WorkspaceRoot`: actual path inside the sandbox, for example `/workspace/repo`

That means the web/daemon spawn flow must grow a real workspace-root concept
for sandbox-backed agents.

### 4. Repository Materialization

Preferred first approach:

- create sandbox
- run an in-sandbox shallow clone of the target repo
- checkout the requested branch/revision
- use the clone path as `WorkspaceRoot`

Suggested clone shape:

```bash
git init /workspace/repo
git -C /workspace/repo remote add origin <repo-url>
git -C /workspace/repo fetch --depth 1 origin <ref>
git -C /workspace/repo checkout FETCH_HEAD
```

Why this first:

- simpler trust model than host-path mounts
- no direct dependency on local filesystem topology
- easier to reproduce in Docker, Kubernetes, and hosted environments

Alternative later:

- PVC-backed warm repo caches
- prewarmed sandbox pools
- volume snapshots

### 5. Memory / Embedding Access

Do not give sandboxes raw write access to canonical memory stores by default.

Recommended access model:

- shared read-only retrieval API from `agentctl`
- optional prebuilt context/evidence pack artifact injected into sandbox

Good candidates:

- ACA top-of-mind bundle
- handoff bundle
- repo-index search hits
- selected memory hits
- companion context summary

Bad candidate for first slice:

- direct writable DB access from sandbox into canonical stores

Direct read-only DB access is also weaker than a retrieval API because it leaks
schema details and broadens the sandbox blast radius.

### 6. Write-Back / Promotion

Treat sandbox output as proposed changes, not canonical truth.

Write-back path should be explicit:

- repo diff / patch / branch artifact
- optional promoted memory observations
- optional promotion drafts into ACA / Obsidian

This keeps the sandbox as a worker, not a storage authority.

## Security Model

OpenSandbox helps in three ways:

- isolates the execution workspace from the control plane pod
- lets us constrain egress
- gives us a cleaner lifecycle boundary for cleanup and auditing

Recommended defaults:

- deny-by-default egress, then allow only:
  - git host
  - package registries if needed
  - LLM provider
  - `agentctl` retrieval endpoint
- no direct host-path mounts in public or shared deployments
- no direct write access to canonical memory DBs
- explicit artifact/promotion write-back only

## AgentCTL Changes Needed

### Spawn / Runtime

Add a workspace execution mode concept:

- `local`
- `sandbox`

New agent metadata fields:

- `workspace_source`
- `workspace_root`
- `sandbox_id`
- `sandbox_provider`
- `repo_url`
- `repo_ref`

### Sandbox Adapter

Add a new internal adapter package, for example:

- `internal/sandbox/`

With responsibilities:

- OpenSandbox client
- sandbox create/delete/status
- clone bootstrap
- retrieval pack injection

### Retrieval Service

Add a narrow read-only retrieval surface for sandboxes:

- query by task / repo / agent
- return summarized evidence packs
- no direct mutation

This could be exposed as:

- authenticated internal HTTP endpoint
- artifact generation path in `agentctl`

### GUI

Add explicit operator choices:

- run locally
- run in sandbox

And show:

- sandbox status
- repo/ref used
- workspace root inside sandbox
- promotion/write-back actions

## Phased Plan

### Phase 1: Local Proof

Goal:

- prove `agentctl` can launch an OpenSandbox-backed task workspace locally

Scope:

- one provider: OpenSandbox
- one repo materialization path: shallow clone
- one agent mode: sandbox-backed coder
- one retrieval path: prebuilt read-only context pack

Success:

- create sandbox
- shallow clone repo into sandbox
- run agent with sandbox `WorkspaceRoot`
- return visible reply

### Phase 2: Controlled Retrieval

Goal:

- let sandboxed agents see useful memory without broad store access

Scope:

- retrieval pack builder
- read-only retrieval endpoint or artifact injection
- no direct writable DB mounts

Success:

- sandboxed agent can answer questions with ACA/memory-aware context

### Phase 3: Promotion

Goal:

- convert sandbox work into explicit durable outputs

Scope:

- diff artifact
- repo patch / branch output
- optional ACA promotion draft

Success:

- operator can review and apply sandbox output explicitly

### Phase 4: Kubernetes Runtime

Goal:

- run sandbox-backed workspaces behind the public GUI in cluster

Scope:

- OpenSandbox service in cluster
- internal auth between `agentctl` and OpenSandbox
- egress policy
- sandbox lifecycle cleanup

Success:

- public GUI launches sandbox-backed repo workspaces in-cluster

## Recommended First Implementation Slice

Implement:

1. `internal/sandbox/opensandbox` client
2. sandbox-backed spawn mode for one agent role
3. shallow clone bootstrap command
4. `WorkspaceRoot` plumbing from sandbox path into runtime
5. one read-only context pack artifact injected into sandbox

Do not implement yet:

- general direct memory DB access
- arbitrary host-path mounting
- multi-workspace browsing from the public GUI
- automatic write-back to canonical stores

## Concrete Answer To The Product Question

If we use OpenSandbox the right way, the agent would not see “any workspace.”

It would see:

- one isolated cloned repo workspace per sandbox
- plus explicitly granted retrieval context

That is the right trust boundary for a public control plane.
