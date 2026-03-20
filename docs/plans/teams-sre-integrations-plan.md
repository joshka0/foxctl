# Implementation Plan: Teams-Driven SRE Investigations and Controlled Remediations

> **Type:** Implementation plan
> **Status:** Draft
> **Author:** Codex + Josh
> **Date:** 2026-03-20
> **Related internal docs:** [chat-platform-adapter.md](../architecture/chat-platform-adapter.md), [kubernetes-runtime.md](../architecture/kubernetes-runtime.md), [simulator-agents.md](../architecture/simulator-agents.md), [jido-hybrid-runtime.md](../architecture/jido-hybrid-runtime.md), [context-architecture.md](../architecture/context-architecture.md), [review_gate.md](../spec/review_gate.md), [agent_hierarchy.md](../spec/agent_hierarchy.md)
> **Reference implementation shape:** IncidentFox Teams bot, K8s gateway, approval UI, and credential-proxy patterns in `~/repos/githubs/incidentfox/`

## Problem Statement

We want agentctl to support Teams-first SRE workflows for real organizational use:

- Teams-triggered investigations into AWS infrastructure
- Grafana Cloud and log/metrics-driven triage
- controlled read and write operations against EKS clusters
- Azure DevOps inspection and selected management operations

IncidentFox already demonstrates a workable product shape for this domain, but its implementation is split across Python services, a separate config service, a dedicated web UI, a sandbox runtime, and a cluster gateway. agentctl already has a materially different foundation:

- a production Teams adapter in Go
- a shared chat/session bridge
- daemon-backed agents and agent hierarchy specs
- an MCP facade and skill runtime
- ACA / Obsidian for durable knowledge capture
- existing review-gate and mailbox approval primitives

The correct move is not to port IncidentFox service-for-service. The correct move is to import the useful product boundaries and rebuild them around agentctl's existing Go + Elixir runtime.

## Objective

Build an agentctl-native SRE ChatOps layer that:

1. uses Microsoft Teams as a primary incident entry point
2. routes each Teams channel or conversation to the correct workspace/room binding
3. supports read-only investigations across AWS, Grafana Cloud, EKS, and Azure DevOps first
4. introduces approval-gated write actions for Kubernetes and Azure DevOps after read paths are stable
5. records run history, evidence, approvals, and outcomes in agentctl-native stores and ACA artifacts
6. remains safe for multi-team and enterprise deployment

## Core Domain Model

For enterprise use, the primary hierarchy should be:

1. `organization`
2. `workspace`
3. `room`
4. `run`

with these orthogonal entities:

- `principal`
- `binding`
- `agent profile`
- `resource scope`
- `approval request`
- `artifact` / `memory`

Definitions:

- `organization`
  - enterprise or tenant boundary
- `workspace`
  - security, policy, memory, integration, and connector boundary
- `room`
  - collaboration and runtime boundary inside a workspace
- `run`
  - one investigation, approval flow, or workflow execution instance
- `binding`
  - Teams, email, or document surface mapped into a room
- `agent profile`
  - reusable capability pack, not the main ownership object
- `resource scope`
  - AWS account, EKS cluster, Grafana org, Azure DevOps org/project, or similar target surface

### Schema Summary

| Entity | Primary scope | Owns / links | Purpose |
|------|------|------|------|
| `organization` | enterprise | workspaces, principals, global policy | top-level tenant boundary |
| `workspace` | security / policy | rooms, integrations, connectors, durable memory, audit policy | main isolation boundary |
| `room` | collaboration / runtime | bindings, runs, participants, room policy | canonical collaboration context |
| `binding` | external surface -> room | Teams/email/document identifiers, mode, profile hint | maps user-facing surfaces into the platform |
| `run` | execution | evidence, approvals, trace, artifacts | one investigation or workflow instance |
| `principal` | identity | user or service attribution | who asked, approved, or executed |
| `agent profile` | capability template | allowed tools, model policy, workflow defaults | reusable agent behavior pack |
| `resource scope` | infrastructure / SaaS target | AWS account, cluster, Grafana org, Azure DevOps project | what a run is allowed to touch |
| `approval request` | mutation control | run-linked decision and audit trail | human gate for risky actions |
| `artifact` / `memory` | durable evidence | CAS refs, summaries, ACA promotion inputs | evidence and long-lived knowledge |

Practical rule:

- `workspace` is the security and policy boundary
- `room` is the collaboration and execution boundary
- Teams is the primary UX surface, not the canonical domain model

Teams mapping rule:

- Teams tenant/team/channel/thread/chat -> `binding` -> `workspace` + `room`
- agents participate in rooms
- runs execute in rooms
- approvals and evidence are linked to runs inside rooms

## Assumptions

- We will prefer agentctl-native stores and APIs over introducing a separate config-service clone.
- We will treat the existing Teams adapter as reusable ingress, not legacy code to bypass.
- We will not weaken WASI's `network:"none"` guarantee; networked SRE integrations will live in Go services or exec/native skills, not in WASI skills.
- We will prefer typed tool surfaces over generic remote shell access.
- We will defer broad autonomous remediation until approvals, audit, and rollback scaffolding exist.

## Non-Goals For The First Usable Slice

- full IncidentFox parity, including RAPTOR-style knowledge systems, per-investigation sandbox pods, and auto-provisioned org/team setup
- generic arbitrary bash execution from Teams channels
- exposing long-lived SaaS secrets directly to agent prompts or tool arguments
- direct cluster admin access from the main web pod via shared kubeconfig as the default production model
- broad write actions across AWS before approval and rollback contracts exist

## Current AgentCTL Surface To Reuse

The following surfaces already exist and should be treated as foundations:

- Teams ingress and message/update mechanics in `internal/chatadapter/teams/*`
- generic natural-language chat routing via `internal/chatadapter/session_bridge.go`
- daemon-backed agent APIs and the existing agent hierarchy protocol
- Teams Adaptive Card interactions for stop/retry/details style actions
- SSE / observability activity feeds that already drive Teams agent status cards
- MCP tool registration and skill exposure under `agentctl mcp serve`
- review-gate and mailbox approval patterns
- ACA / Obsidian capture and retrieval for durable incident learnings
- in-repo Kubernetes deployment patterns for `agentctl web serve`

Important constraint:

- the existing workspace `teams` store models internal workspace teams and memberships, not Microsoft Teams channel bindings; it should not be overloaded for chat routing

## Product Fit: What To Import From IncidentFox

IncidentFox suggests the right feature boundaries even where we should not copy the implementation:

1. **Chat binding and routing**
   The bot must know which workspace/room/agent profile a Teams channel or thread belongs to.
2. **Dedicated investigation runs**
   Incident-style requests need a bounded run identity, trace, evidence pack, and outcome, not just free-form chat history.
3. **Connector model for clusters**
   Outbound cluster connections are safer than centrally holding broad kubeconfigs.
4. **Credential isolation**
   The model should never see the raw token set for Grafana, Azure DevOps, or cloud APIs.
5. **Approval queue for mutations**
   Operational writes need an explicit pending/approved/rejected/executed lifecycle.
6. **Trace and evidence UX**
   Teams and future web UI surfaces need run progress, tool traces, evidence summaries, and action approvals.

## Architecture Decision

Build this as six agentctl-native layers.

### 1. Chat Binding Layer

Add a new storage surface for Teams bot bindings (in Teams terms: channel/conversation routing).

Proposed store:

- `internal/storage/chatbindings`

Proposed logical records:

- `chat_bindings`
  - `workspace_id`
  - `room_id`
  - `binding_id`
  - `platform` (`teams`)
  - `tenant_id`
  - `external_channel_id`
  - `external_conversation_id`
  - `binding_scope` (`channel`, `conversation`, `dm`)
  - `mode` (`session`, `investigation`)
  - `entrance_profile`
  - `resource_scope_refs`
  - `status`
  - `created_at`
  - `updated_at`

Why a new store:

- it separates Microsoft Teams routing from the internal workspace-teams model
- it keeps future Slack/Discord routing in the same schema family
- it gives us a stable lookup point for chat ingress before any agent run starts
- it lets Teams stay the primary user surface while rooms remain the canonical collaboration object

### 2. Ops Integration Config Layer

Add a workspace-scoped operational integration store rather than a separate config-service clone.

Proposed store:

- `internal/storage/opscfg`

Proposed logical records:

- `ops_integrations`
  - `workspace_id`
  - `integration_type` (`aws`, `grafana`, `azuredevops`, `kubernetes_connector`)
  - `resource_scope_id`
  - `config_json`
  - `secret_ref`
  - `capabilities_json`
  - `health_status`
  - `last_checked_at`
  - `created_at`
  - `updated_at`

Guidelines:

- `config_json` contains non-secret routing and capability metadata
- `secret_ref` points to environment-backed, Kubernetes-backed, or secret-manager-backed credentials
- the agent never receives the raw secret material

### 3. Investigation Run Layer

Introduce a bounded incident/investigation run model instead of treating every Teams request as generic chat.

Proposed store:

- `internal/storage/opsruns`

Proposed logical records:

- `ops_runs`
  - `workspace_id`
  - `room_id`
  - `run_id`
  - `source_platform`
  - `source_channel_key`
  - `conversation_key`
  - `trigger_text`
  - `agent_profile`
  - `status` (`running`, `completed`, `failed`, `awaiting_approval`, `canceled`)
  - `summary`
  - `artifact_digest`
  - `created_by`
  - `created_at`
  - `updated_at`

- `ops_run_events`
  - `run_id`
  - `sequence`
  - `event_type`
  - `payload_json`
  - `created_at`

Execution model:

- Teams-bound SRE messages should create or reuse an `ops_run`
- the run should own event capture, evidence references, and approval linkage
- free-form bot DMs can continue to use the generic `SessionBridge`
- bound incident channels should use `mode=investigation` and route through a dedicated run controller
- the room, not the Teams thread ID, should be the canonical collaboration object

### 4. Typed SRE Tool Layer

Use the simulator-agent contract for integrations:

- `app/state` for read-only snapshots
- `app/action` for bounded mutations with explicit operation enums

Initial tool families:

- `aws/state`
- `grafana/state`
- `azuredevops/state`
- `kubernetes/state`
- `kubernetes/action`
- `azuredevops/action`

Representative operations:

- `aws/state`
  - `describe_alarms`
  - `describe_service_health`
  - `describe_ecs_service`
  - `describe_eks_cluster`
  - `lookup_recent_deployments`

- `grafana/state`
  - `query_loki`
  - `query_mimir`
  - `fetch_dashboard_annotations`
  - `list_firing_alerts`

- `azuredevops/state`
  - `list_pipeline_runs`
  - `get_pipeline_run`
  - `list_work_items`
  - `list_pull_requests`

- `kubernetes/state`
  - `list_pods`
  - `describe_pod`
  - `get_events`
  - `get_logs`
  - `get_deployment_status`

- `kubernetes/action`
  - `restart_deployment`
  - `scale_deployment`
  - `rollback_deployment`

- `azuredevops/action`
  - `rerun_pipeline`
  - `create_work_item`
  - `update_work_item_state`

Contract requirements:

- operations are explicit enums
- arguments are typed and validated strictly
- mutating operations accept idempotency keys
- large results are written to CAS and returned as `summary + artifact`
- tools should emit structured evidence metadata that can be attached to `ops_runs`

### 5. Cluster Connector Layer

For production EKS access, prefer an outbound connector model over central kubeconfig access.

Proposed shape:

- control-plane service mode inside agentctl: `agentctl connector k8s serve`
- lightweight in-cluster agent deployment: `agentctl-k8s-agent`
- outbound SSE or WebSocket connection from cluster -> agentctl connector
- internal typed execution API from run controller -> connector -> cluster agent

Proposed internal package split:

- `internal/connectors/k8sbridge`
- `internal/connectors/k8sbridge/auth`
- `internal/connectors/k8sbridge/store`
- `internal/connectors/k8sbridge/models`

Security rules:

- cluster agents authenticate with short-lived connector registration tokens
- workspace and resource-scope ownership are checked on every command dispatch
- command set is allowlisted and typed
- the connector owns transport and request correlation
- the main agent runtime never gets generic shell-on-cluster capability

Development exception:

- local or single-cluster development may allow direct kubeconfig-backed `kubernetes/state`
- production should default to the connector bridge for EKS

### 6. Approval + Audit Layer

Reuse the mental model of review gates, but do not reuse task review artifacts directly for ops mutations.

Proposed store:

- `internal/storage/opsactions`

Proposed logical records:

- `ops_action_requests`
  - `workspace_id`
  - `room_id`
  - `request_id`
  - `run_id`
  - `integration_type`
  - `action_name`
  - `action_payload_json`
  - `risk_level`
  - `idempotency_key`
  - `status` (`pending`, `approved`, `rejected`, `executing`, `completed`, `failed`, `rolled_back`)
  - `requested_by`
  - `approved_by`
  - `rejected_by`
  - `reason`
  - `artifact_digest`
  - `created_at`
  - `updated_at`

Approval sources:

- Teams Adaptive Card buttons
- CLI / API approval endpoint
- optional mailbox-backed approval acknowledgement as a thin first slice

Audit requirements:

- every approval decision is append-only
- every executed mutation records before/after evidence and artifact digests
- rollback metadata is captured when rollback is available

## Jido Fit and Extension Path

The Jido material is useful here, but only if we keep the current hybrid ownership split intact:

- Jido should own runtime lifecycle, signals, workflows, and reusable plugin packs
- agentctl should continue to own tool semantics, memory/session retrieval, durable stores, CAS artifacts, and control-plane projections

That means the SRE plan should use Jido to structure and supervise workflows, not to reimplement AWS, Grafana, Kubernetes, or retrieval semantics inside Elixir.

### Sensors and Real-Time Events

Jido's sensors model is a strong fit for external event ingress:

- a sensor is a long-lived process that turns external input into typed Signals
- webhook delivery can also bypass a sensor and inject a Signal directly
- `signal_routes/1` can change dynamically based on runtime context

How to apply that here:

- keep Teams HTTP webhook handling in Go
- normalize inbound Teams, CloudWatch, Grafana, Azure DevOps, and connector-heartbeat events into canonical ops events in agentctl
- optionally forward those canonical ops events into Jido as runtime Signals such as:
  - `ops.teams.message`
  - `ops.alert.cloudwatch`
  - `ops.alert.grafana`
  - `ops.webhook.azuredevops`
  - `ops.connector.k8s.status`

This gives us one event grammar for both SRE and later workflow domains.

The biggest immediate value is context-aware routing:

- maintenance mode can route a signal to a hold/deferral action
- pending approval state can route a mutation request into an approval action instead of execution
- degraded connector health can route Kubernetes actions into fallback read-only or failure-summary paths

### Memory and Retrieval-Augmented Agents

Jido's memory material is useful, but it should stay subordinate to agentctl's existing semantic memory stack.

Use Jido memory features for:

- runtime-local structured working state
- append-only thread state inside one live workflow
- lightweight checkpoint and restore behavior for runtime-supervised agents
- short-lived retrieval caches over small workflow-local corpora

Do not use Jido as the primary durable knowledge system for this plan.

Instead:

- agentctl remains the durable retrieval and memory system
- ACA / Obsidian remains the long-lived human-readable knowledge plane
- `memory/query`, `session/recall`, `session/timeline`, and ACA retrieval stay on the Go side
- Jido runtime hooks should call back into agentctl to augment prompts before reasoning steps

Practical rule:

- Jido memory = runtime-local and checkpoint-oriented
- agentctl memory = durable and organization-facing

### Plugins and Composable Agents

Jido plugins are a strong fit for packaging reusable capability bundles with isolated state slices, signal routes, and per-agent config.

We should use plugin-style thinking for cross-domain capability packs such as:

- `InvestigationRunPlugin`
- `ApprovalGatePlugin`
- `ObservabilityIngressPlugin`
- `K8sConnectorPlugin`
- `EmailThreadPlugin`
- `WordDocumentPlugin`

What plugins should own:

- runtime-local state slices
- signal routing tables
- per-agent or per-workflow config
- action bundles that orchestrate existing agentctl semantics

What plugins should not own:

- the canonical store schemas for bindings, integrations, approvals, or runs
- direct durable retrieval implementation
- cloud-specific business logic that already belongs in Go tool surfaces

### Workflows and Directives

Jido workflows are a good fit for deterministic multi-step investigation or remediation phases.

This plan should use workflow-style action chains for steps like:

1. resolve binding
2. create or resume run
3. retrieve prior context
4. fan out read-only evidence collection
5. synthesize summary
6. emit approval directive if a write is needed
7. after approval, execute mutation and capture evidence

The important constraint is that these actions should stay thin:

- workflow actions should call back into agentctl APIs, services, or tools
- they should not reimplement SRE integrations inside Elixir
- directive output is the right place for approval prompts, notifications, and follow-up signals

## Generic Workflow Extension Beyond SRE

If we keep the abstractions generic, this plan can extend to non-SRE workflow agents later without a new platform rewrite.

### Email Agent

Potential shape:

- bindings map mailbox/folder/address -> workspace/room/profile
- sensors or webhooks emit:
  - `email.message.received`
  - `email.thread.replied`
  - `email.send.approval_requested`
- plugin packs:
  - `EmailThreadPlugin`
  - `DraftOutboxPlugin`
  - `ApprovalGatePlugin`
- workflow chain:
  - ingest -> classify -> retrieve context -> draft -> approve -> send

### Microsoft Word / Document Agent

Potential shape:

- bindings map document/library/sharepoint scope -> workspace/room/profile
- sensors or webhooks emit:
  - `word.document.changed`
  - `word.comment.created`
  - `word.review.requested`
- plugin packs:
  - `DocumentContextPlugin`
  - `RevisionBufferPlugin`
  - `ApprovalGatePlugin`
- workflow chain:
  - ingest -> retrieve template/policy context -> propose edit -> approve -> write back

The core platform objects should therefore stay generic:

- `chat_bindings` should evolve toward `bindings`, not a Teams-only forever schema
- `ops_runs` should be defined broadly enough to become domain workflow runs
- `ops_action_requests` should stay usable for any approval-gated side effect
- signal naming should follow domain-prefixed patterns and avoid SRE-only assumptions in the base runtime

## Agent Execution Model

We should not send every Teams message through the plain `consolews` chat path.

Recommended split:

- `mode=session`
  - existing natural-language assistant behavior
  - good for DM/chat usage

- `mode=investigation`
  - creates an `ops_run`
  - resolves the correct SRE profile
  - executes through a dedicated run controller
  - writes traces and evidence
  - can pause on pending approvals

Recommended default profile set:

- `incident-investigator`
- `k8s-investigator`
- `observability-investigator`
- `remediator`

Subagent spawning should continue to honor [agent_hierarchy.md](../spec/agent_hierarchy.md), especially for controlled delegation during long investigations.

## Security Model

### Credential Handling

Do not pass raw credentials into prompts or tool args.

Preferred mechanisms:

- AWS
  - IRSA or instance/workload identity
  - STS assume-role per workspace or resource-scope integration config
  - no static keys in prompts

- Grafana Cloud
  - service account token resolved from `secret_ref`
  - token injected only inside the Go integration boundary

- Azure DevOps
  - service connection or PAT resolved from `secret_ref`
  - read-only scopes first

### Tool Exposure

- no generic bash or arbitrary HTTP tool for Teams-bound ops runs
- all prod integrations are typed and allowlisted
- mutating operations require approval and idempotency keys
- high-volume evidence goes to CAS, not inline chat text

### Multi-Tenant / Multi-Team Isolation

- binding lookup must resolve to exactly one workspace/room scope
- connector and integration calls must verify ownership on every request
- run traces and artifacts are workspace-scoped and room-linked

Practical rule:

- authorization is evaluated at workspace scope first
- room membership and room policy can further narrow access
- room is never the primary security boundary

## Enterprise Gap Analysis: Microsoft + AWS + Bedrock

If this platform is going to be adopted by an enterprise that primarily uses
Microsoft and AWS, the current plan still needs an explicit enterprise platform
layer around the agent runtime.

The missing pieces are mostly not "more tools". The missing pieces are identity,
policy, compliance, admin, and lifecycle controls that make agent use safe and
operationally trustworthy.

### 1. Identity and SSO

Missing:

- Microsoft Entra ID sign-in for human users
- tenant, workspace, and room mapping from enterprise identity and binding state
- stable per-user actor identity across Teams, GUI, API, and approvals
- group-based authorization inputs for agent and tool policy

Needed outcomes:

- every Teams interaction resolves to a verified enterprise user
- every approval record has a real enterprise principal behind it
- every agent run can attribute prompts, approvals, and mutations to a user or
  service principal

Recommended additions:

- Entra ID OIDC/OAuth integration for web/admin surfaces
- Teams user identity mapping to internal actor IDs
- group-to-role mapping for approvers, operators, and viewers

### 2. Authorization and Policy

Missing:

- a formal authorization layer for who can use which agents and integrations
- blast-radius-aware policy for mutations
- policy distinction between read-only, approval-required, and prohibited tools

Needed outcomes:

- "can ask" and "can approve" are not the same permission
- tool access can be scoped by workspace, room, profile, environment, and risk
- agent profiles inherit explicit allowed-tool sets and approval classes

Recommended additions:

- role and policy model over:
  - bindings
  - runs
  - approvals
  - integrations
  - connectors
- explicit `access_class` and `approval_class` metadata on mutating tools

### 3. Bedrock Runtime Integration

If Bedrock is the model backend, the plan needs a first-class Bedrock runtime
surface rather than treating the model as an interchangeable endpoint detail.

Missing:

- Bedrock provider/gateway integration in the agent runtime path
- IAM role strategy for agent execution
- model routing rules by workspace, profile, room, or workflow
- region failover and retry policy
- prompt/tool guardrails at the Bedrock boundary

Needed outcomes:

- agents can use Bedrock without static API keys
- IAM policies determine which runtime can call which Bedrock model
- model selection is policy-aware, not just config-string-driven

Recommended additions:

- Bedrock provider support in the model/runtime layer
- IRSA or workload identity for runtime components
- per-profile model policy:
  - default model
  - allowed models
  - max token budget
  - latency class
  - fallback model
- optional Bedrock Guardrails integration where policy or compliance requires it

### 4. Secrets and Credential Brokering

The current plan says secrets should be brokered, but enterprise adoption needs
this to be elevated to a first-class subsystem.

Missing:

- credential broker architecture for Grafana, Azure DevOps, and other SaaS APIs
- secret rotation, revocation, and expiry behavior
- separation between secret metadata and secret material

Needed outcomes:

- the model never sees raw long-lived secrets
- tool execution receives only the credentials needed for one operation
- secret rotation does not require agent prompt changes

Recommended additions:

- a dedicated credential broker / resolver boundary
- `secret_ref` contract that can target:
  - AWS Secrets Manager
  - SSM Parameter Store
  - Kubernetes Secrets
  - enterprise secret broker later
- short-lived credential issuance when possible

### 5. Audit, Compliance, and Retention

Missing:

- durable audit policy for prompts, tool invocations, approvals, and mutations
- retention/deletion rules by data class
- export capability for regulated environments

Needed outcomes:

- every prompt, tool call, approval, mutation, and artifact can be audited
- enterprises can answer "who asked", "what ran", "what changed", and "what evidence supported it"
- sensitive data can be retained or purged according to policy

Recommended additions:

- append-only audit stream for:
  - run creation
  - evidence collection
  - approval actions
  - mutation execution
  - rollback events
- CAS-backed evidence export bundles
- data classification flags on stored artifacts and traces

### 6. Teams Product Surface

The current plan includes Teams ingress and approvals, but enterprise use also
needs stronger Teams-native operator UX.

Missing:

- agent discovery and capability explanation in Teams
- better run links between Teams and the web/admin surface
- resumable in-thread investigation and approval flow
- clear confidence/evidence display for non-expert users

Needed outcomes:

- users can understand what an agent can do before they invoke it
- approvals and follow-ups happen in-thread without losing context
- investigations can hand off cleanly to humans

Recommended additions:

- Teams cards for:
  - agent profile / capability summary
  - run status
  - approval request
  - evidence summary
  - handoff / follow-up actions

### 7. Admin Control Plane

Missing:

- a real admin/operator surface for managing enterprise agent behavior

Needed outcomes:

- bindings, integrations, model policy, connector state, and approvals can be
  managed without hand-editing config or DB state

Recommended additions:

- admin UI or API for:
  - Teams bindings
  - integration config
  - connector inventory
  - approval queues
  - agent profiles
  - model policy
  - audit export

### 8. Runtime Isolation and Execution Classes

Missing:

- a stronger execution-class model beyond "read" versus "write"

Needed outcomes:

- risky actions run in more constrained execution domains
- investigation, planning, and mutation execution can be isolated from each
  other

Recommended additions:

- execution classes such as:
  - `read_only`
  - `approval_required`
  - `connector_only`
  - `restricted_mutation`
- optional runtime segmentation for:
  - investigation workers
  - approval handlers
  - mutation executors

### 9. Service, Incident, and Environment Model

Missing:

- first-class organizational objects that let the platform reason about what it
  is acting on

Needed outcomes:

- incidents, services, environments, clusters, owners, and escalation targets
  are explicit
- routing and evidence collection are tied to known service topology

Recommended additions:

- canonical domain records for:
  - services
  - environments
  - incidents
  - clusters
  - owning teams
  - escalation targets

### 10. Connector Lifecycle Management

Missing:

- lifecycle and inventory controls for deployed connectors

Needed outcomes:

- connectors can be registered, rotated, revoked, upgraded, and monitored
- stale or unhealthy connectors are visible to operators

Recommended additions:

- connector inventory and health model
- registration token issuance
- rotation and revocation APIs
- version compatibility checks
- drift and heartbeat monitoring

### 11. Enterprise Workflow Extensibility

If you want this to become a general enterprise agent platform later, the plan
should explicitly preserve support for non-SRE workflow families.

Likely next domains:

- email workflows
- document and Microsoft Word workflows
- approval-driven back-office or ops workflows

Needed outcomes:

- the same platform can host multiple workflow families without another runtime
  rewrite
- SRE is one domain, not the one-off special case forever

Recommended additions:

- generic workflow-run primitives over the SRE run model
- domain-prefixed signal taxonomy
- plugin packs for email, document, and approval workflows
- durable policy and audit that are not SRE-specific

## Enterprise Prerequisite Slice

Before broad internal rollout, the platform should reach the following minimum
enterprise-ready bar:

1. Entra ID auth and stable user attribution
2. RBAC/ABAC for agents, tools, approvals, and evidence visibility
3. Bedrock runtime integration with IAM-based access control
4. brokered secret handling for SaaS integrations
5. Teams bindings and threaded room/run UX
6. connector-based EKS access with ownership checks
7. approval-gated mutations with append-only audit
8. admin surface for bindings, profiles, integrations, and connector health
9. evidence-pack export and retention policy
10. ACA promotion and durable knowledge capture for completed investigations

## Phase 0.5: Enterprise Prerequisites

This should happen after the foundational PRs but before broad production
rollout.

Scope:

- Entra ID identity mapping
- role/policy enforcement for runs, tools, and approvals
- Bedrock provider/runtime integration
- credential broker and secret-ref contract hardening
- audit stream and evidence export model
- connector lifecycle inventory
- admin control-plane APIs

Exit criteria:

- a real enterprise user can authenticate and be attributed end-to-end
- Bedrock-backed agent runs work without static model secrets in prompts
- mutating operations are blocked by policy without approval
- audit and evidence records are exportable and attributable
- connectors can be listed, rotated, and revoked

## Phase 0.5 PR Sequence (Execution-Ready)

Phase 0.5 is where the platform stops being an internal SRE prototype and
starts becoming an enterprise agent control plane. These slices should land
before broad Microsoft/AWS rollout.

## PR-0.5.1: Entra Identity Mapping + Enterprise Principal Attribution

Goal:

1. Add enterprise identity resolution so Teams users, GUI users, and API callers
   can be mapped to stable principals and workspace/room scope.

Primary files:

1. `internal/identity/enterprise/types.go` (new)
2. `internal/identity/enterprise/resolver.go` (new)
3. `internal/identity/enterprise/resolver_test.go` (new)
4. `internal/chatadapter/teams/driver.go` (attach mapped principal metadata)
5. `internal/web/api/*` relevant handlers (principal extraction / attribution)
6. `docs/architecture/auth-identity.md` (clarify enterprise principal flow)

Package scaffolding:

```text
internal/
  identity/
    enterprise/
      types.go
      resolver.go
      resolver_test.go
```

Acceptance:

1. Every Teams-originated run has a stable enterprise principal, not just a raw
   Teams user ID.
2. GUI/API requests can carry the same actor identity model.
3. Approval records and audit rows store enterprise-resolvable principals.

Tests:

1. `go test ./internal/identity/enterprise ./internal/chatadapter/teams ./internal/web/...`

## PR-0.5.2: Authorization Layer For Runs, Tools, and Approvals

Goal:

1. Introduce explicit authorization over who can use which agents, integrations,
   runs, and approval actions.

Primary files:

1. `internal/agentpolicy/ops_authorizer.go` (new)
2. `internal/agentpolicy/ops_authorizer_test.go` (new)
3. `internal/ops/policy/types.go` (new)
4. `internal/ops/policy/types_test.go` (new)
5. `internal/web/api/ops_*` handlers (policy enforcement)
6. `internal/v2/adapters/jido/types.go` or adjacent profile policy payloads

Package scaffolding:

```text
internal/
  ops/
    policy/
      types.go
      types_test.go
internal/
  agentpolicy/
    ops_authorizer.go
    ops_authorizer_test.go
```

Contract decisions to lock:

1. separate permissions for:
   - invoke investigation
   - view evidence
   - request mutation
   - approve mutation
2. `access_class` and `approval_class` metadata for mutating operations
3. policy outcomes must be explicit and auditable, not hidden side effects

Acceptance:

1. "can ask" and "can approve" are distinct.
2. Tool access is enforceable by workspace/room/profile/environment.
3. Denied requests surface clear policy outcomes in traces and API results.

Tests:

1. `go test ./internal/agentpolicy ./internal/ops/policy ./internal/web/...`

## PR-0.5.3: Bedrock Runtime Integration + Model Policy

Goal:

1. Make Bedrock a first-class enterprise runtime option with policy-aware model
   selection and IAM-based access.

Primary files:

1. `internal/llm/bedrock/` or current model provider package extension
2. `internal/platform/config/config.go` (wire Bedrock enterprise config if missing)
3. `internal/companion/service.go` and/or runtime model call path
4. `internal/v2/runtime/runner/model_call.go`
5. `docs/plans/aws-bedrock-provider.md` (cross-link / align where needed)

Package scaffolding:

```text
internal/
  llm/
    bedrock/
      client.go
      client_test.go
      policy.go
      policy_test.go
```

Policy to lock:

1. per-profile Bedrock model allowlist
2. default model, fallback model, latency class, and token budget
3. IAM-based execution path with no static model secrets in prompts

Acceptance:

1. Bedrock-backed runs work from enterprise runtime components.
2. Model choice is policy-aware, not just string-configured.
3. The runtime can deny disallowed models before invocation.

Tests:

1. `go test ./internal/llm/bedrock ./internal/v2/runtime/runner ./internal/companion/...`

## PR-0.5.4: Secret Broker + `secret_ref` Hardening

Goal:

1. Turn secret brokering from a design note into a concrete execution boundary.

Primary files:

1. `internal/secrets/ref.go` (new)
2. `internal/secrets/resolver.go` (new)
3. `internal/secrets/resolver_test.go` (new)
4. `internal/storage/opscfg/store.go` (validate secret-ref contract)
5. integration callers for Grafana, Azure DevOps, AWS helper paths

Package scaffolding:

```text
internal/
  secrets/
    ref.go
    resolver.go
    resolver_test.go
```

Supported first targets:

1. AWS Secrets Manager
2. SSM Parameter Store
3. Kubernetes Secrets

Acceptance:

1. integrations store metadata, not raw secrets
2. tool execution receives resolved credentials only at execution time
3. secret target validation is strict and typed

Tests:

1. `go test ./internal/secrets ./internal/storage/opscfg ...`

## PR-0.5.5: Audit Stream + Evidence Export

Goal:

1. Make runs, approvals, mutations, and evidence exportable and attributable.

Primary files:

1. `internal/storage/opsaudit/` (new)
2. `internal/ops/audit/` service layer (new)
3. `internal/web/api/ops_audit.go` (new)
4. `internal/storage/opsruns/store.go` (audit hooks)
5. `internal/storage/opsactions/store.go` (audit hooks)

Package scaffolding:

```text
internal/
  storage/
    opsaudit/
      doc.go
      store.go
      store_test.go
  ops/
    audit/
      service.go
      service_test.go
```

Acceptance:

1. prompts, approvals, mutation execution, and rollback outcomes emit audit rows
2. evidence packs can be exported with CAS artifact references
3. audit rows include enterprise principal attribution

Tests:

1. `go test ./internal/storage/opsaudit ./internal/ops/audit ./internal/web/...`

## PR-0.5.6: Connector Inventory + Lifecycle Controls

Goal:

1. Add the enterprise lifecycle model for deployed cluster connectors.

Primary files:

1. `internal/storage/connectors/` (new)
2. `internal/connectors/k8sbridge/inventory.go` (new)
3. `internal/connectors/k8sbridge/inventory_test.go` (new)
4. `internal/web/api/connectors.go` (new)

Package scaffolding:

```text
internal/
  storage/
    connectors/
      doc.go
      store.go
      store_test.go
internal/
  connectors/
    k8sbridge/
      inventory.go
      inventory_test.go
```

Lifecycle actions to support:

1. register
2. list
3. rotate token
4. revoke
5. mark unhealthy
6. track version/drift

Acceptance:

1. operators can list, rotate, and revoke connectors
2. connector ownership and health are visible without inspecting pods manually
3. stale connectors are detectable from API state

Tests:

1. `go test ./internal/storage/connectors ./internal/connectors/k8sbridge ./internal/web/...`

## PR-0.5.7: Admin Control-Plane APIs

Goal:

1. Expose enterprise management APIs for bindings, profiles, integrations,
   approvals, audit export, and connector inventory.

Primary files:

1. `internal/web/api/ops_admin.go` (new)
2. `internal/web/server.go` (route wiring)
3. `cmd/agentctl/cmd/ops.go` (admin-oriented command coverage)
4. `cmd/agentctl/cmd/mcp.go` (optional admin MCP exposure later)

Acceptance:

1. admins can inspect bindings, profiles, integrations, approvals, and
   connectors through stable APIs
2. audit export is available through a bounded API or artifact flow
3. these APIs respect enterprise principal and authorization policy

Tests:

1. `go test ./internal/web/... ./cmd/agentctl/cmd/...`

## API and Command Surface

### New API Surface

Proposed HTTP routes:

- `POST /api/ops/investigations`
- `GET /api/ops/runs`
- `GET /api/ops/runs/{id}`
- `GET /api/ops/runs/{id}/trace`
- `GET /api/ops/approvals`
- `POST /api/ops/approvals/{id}/approve`
- `POST /api/ops/approvals/{id}/reject`
- `GET /api/ops/connectors/k8s/clusters`

### CLI / Skill Surface

Proposed management skills or commands:

- `chat/bindings.list`
- `chat/bindings.upsert`
- `chat/bindings.remove`
- `ops/integrations.list`
- `ops/integrations.upsert`
- `ops/integrations.health`
- `ops/approvals.list`
- `ops/approvals.approve`
- `ops/approvals.reject`

These can later be exposed as first-class MCP tools through `agentctl mcp serve`.

## Phase 0 PR Sequence (Execution-Ready)

Phase 0 should stop at foundations and contracts. It should not ship live AWS, Grafana, or Kubernetes execution yet.

## PR-1: Ops Domain Types + Stores

Goal:

1. Add the canonical data model and storage layers for bindings, integrations, runs, and action requests.

Primary files:

1. `internal/ops/types.go` (new)
2. `internal/ops/types_test.go` (new)
3. `internal/storage/chatbindings/doc.go` (new)
4. `internal/storage/chatbindings/store.go` (new)
5. `internal/storage/chatbindings/store_test.go` (new)
6. `internal/storage/opscfg/doc.go` (new)
7. `internal/storage/opscfg/store.go` (new)
8. `internal/storage/opscfg/store_test.go` (new)
9. `internal/storage/opsruns/doc.go` (new)
10. `internal/storage/opsruns/store.go` (new)
11. `internal/storage/opsruns/store_test.go` (new)
12. `internal/storage/opsactions/doc.go` (new)
13. `internal/storage/opsactions/store.go` (new)
14. `internal/storage/opsactions/store_test.go` (new)

Package scaffolding:

```text
internal/
  ops/
    types.go
    types_test.go
  storage/
    chatbindings/
      doc.go
      store.go
      store_test.go
    opscfg/
      doc.go
      store.go
      store_test.go
    opsruns/
      doc.go
      store.go
      store_test.go
    opsactions/
      doc.go
      store.go
      store_test.go
```

Acceptance:

1. All four stores have deterministic schema migrations and bounded CRUD APIs.
2. `ops_action_requests` enforce append-safe state transitions and idempotency-key fields.
3. `workspace_id` is required everywhere; `room_id` is required for bindings, runs, and approval requests.
4. Run events support ordered append and stable replay.

Tests:

1. `go test ./internal/ops ./internal/storage/chatbindings ./internal/storage/opscfg ./internal/storage/opsruns ./internal/storage/opsactions`

## PR-2: Teams Binding Resolution + Investigation Entry Contract

Goal:

1. Extend Teams ingress so bound channels can choose `session` versus `investigation` mode and seed an `ops_run` without yet invoking external SRE integrations.

Primary files:

1. `internal/ops/bindings/resolver.go` (new)
2. `internal/ops/bindings/resolver_test.go` (new)
3. `internal/ops/investigator/service.go` (new)
4. `internal/ops/investigator/service_test.go` (new)
5. `internal/web/api/ops_investigations.go` (new)
6. `internal/web/server.go` (route wiring)
7. `internal/chatadapter/teams/driver.go` (binding lookup + mode dispatch)
8. `internal/chatadapter/teams/events.go` (associate root activity / run id where helpful)

Package scaffolding:

```text
internal/
  ops/
    bindings/
      resolver.go
      resolver_test.go
    investigator/
      service.go
      service_test.go
  web/
    api/
      ops_investigations.go
```

Acceptance:

1. Teams DM or unbound channels can keep existing `SessionBridge` behavior.
2. Bound Teams channels in `mode=investigation` resolve to a room and create an `ops_run` with an initial event record.
3. The API layer can create and query a run skeleton without live cloud calls.
4. The Teams adapter does not hard-code SRE behavior for all conversations.

Tests:

1. `go test ./internal/ops/bindings ./internal/ops/investigator ./internal/chatadapter/teams ./internal/web/...`

## PR-3: Jido Signal + Plugin + Workflow Contract

Goal:

1. Define how investigation-mode runs are represented at the Jido boundary without moving semantic ownership out of agentctl.

Primary files:

1. `internal/v2/adapters/jido/ops_signal_codec.go` (new)
2. `internal/v2/adapters/jido/ops_signal_codec_test.go` (new)
3. `internal/v2/adapters/jido/types.go` (extend metadata for ops runtime use)
4. `internal/v2/services/long_lived_run_service.go` (wire investigation-runtime entry if needed)
5. `internal/v2/services/long_lived_run_service_test.go` (update expectations)
6. `docs/architecture/jido-hybrid-runtime.md` (clarify ops signal and plugin usage if needed)
7. `docs/plans/teams-sre-integrations-plan.md` (keep contract aligned)

Package scaffolding:

```text
internal/v2/adapters/jido/
  ops_signal_codec.go
  ops_signal_codec_test.go
```

Contract decisions to lock:

1. canonical signal names for ops events
2. `plugin_config` usage for ops profiles and allowed tools
3. runtime-local plugin responsibilities versus Go-side durable stores
4. memory-retention defaults for investigation, approval, and workflow-specific agents

Acceptance:

1. A normalized ops event can be converted into a Jido runtime signal with explicit metadata.
2. Jido payloads carry profile, allowed-tools, and retention policy without embedding cloud logic.
3. The contract remains aligned with `docs/architecture/jido-hybrid-runtime.md`: Jido owns runtime, agentctl owns semantics.

Tests:

1. `go test ./internal/v2/adapters/jido ./internal/v2/services`

## PR-4: Management Surface + Generic Workflow Naming

Goal:

1. Add admin and operator-facing configuration surfaces for bindings and integrations, while keeping the abstractions generic enough for future email and document workflows.

Primary files:

1. `skills/chat_bindings/main.go` (new)
2. `skills/chat_bindings/skill.yaml` (new)
3. `skills/ops_integrations/main.go` (new)
4. `skills/ops_integrations/skill.yaml` (new)
5. `cmd/agentctl/cmd/ops.go` (new or extend existing command groups)
6. `cmd/agentctl/cmd/mcp.go` (optional first-class exposure later)

Package scaffolding:

```text
skills/
  chat_bindings/
    main.go
    skill.yaml
  ops_integrations/
    main.go
    skill.yaml
cmd/agentctl/cmd/
  ops.go
```

Naming rules to lock in this PR:

1. prefer `binding`, `run`, `integration`, `action_request`, and `signal_source`
2. avoid encoding `teams` or `sre` into the base domain objects unless the concept is truly platform-specific
3. keep future email and document workflows representable without schema renames

Acceptance:

1. Operators can create and inspect bindings without hand-editing DB state.
2. Integration metadata can be stored without secrets.
3. The command and skill surfaces stay generic enough for later non-SRE workflows.

Tests:

1. `go test ./skills/chat_bindings ./skills/ops_integrations ./cmd/agentctl/cmd/...`

## Proposed Rollout Slices

## Phase 0: Foundations and Contracts

Scope:

- add the new stores: `chatbindings`, `opscfg`, `opsruns`, `opsactions`
- define typed request/response models for `state` and `action` tool families
- define the `mode=investigation` routing contract for Teams bindings
- define the approval state machine and idempotency rules
- lock the Jido signal/plugin/workflow boundary for investigation-mode runs

Exit criteria:

- storage schemas and Go access layers exist with tests
- docs/spec references are clear enough to guide later implementation
- the PR-1 through PR-4 checklist above is complete

## Phase 1: Teams Binding + Investigation Entry

Scope:

- extend Teams handling to resolve a binding before choosing session vs investigation mode
- keep generic `SessionBridge` for DMs/free-form chat
- add an internal run controller for `mode=investigation`
- send quick Teams acknowledgement and update progress through existing Adaptive Card/message mechanics

Exit criteria:

- a bound Teams channel or thread can trigger a persisted room-linked `ops_run`
- progress and final summary return to the same Teams thread or conversation

## Phase 2: Read-Only SRE Tools

Scope:

- implement `aws/state`, `grafana/state`, `azuredevops/state`, `kubernetes/state`
- support compact evidence summaries plus CAS artifacts
- attach tool evidence to `ops_runs`

Exit criteria:

- one investigation can correlate at least two sources
- all outputs remain bounded and artifact-backed for large responses

## Phase 3: K8s Connector Bridge

Scope:

- build the outbound cluster connector and registration/auth path
- list connected clusters and route typed Kubernetes requests through the bridge
- support EKS production access without central kubeconfig sprawl

Exit criteria:

- at least one EKS cluster can connect outbound
- read-only `kubernetes/state` operations work through the connector
- workspace/resource-scope ownership is enforced on every dispatch

## Phase 4: Approval-Gated Mutations

Scope:

- implement `kubernetes/action` and `azuredevops/action`
- create `ops_action_requests`
- expose approve/reject through Teams and API
- resume paused runs after approval or rejection

Exit criteria:

- a write action cannot execute without an approval record
- completed actions write audit and evidence artifacts

## Phase 5: Run Trace, Dashboard, and ACA Capture

Scope:

- add run listing, trace, approval, and connector health APIs
- add a first UI surface or reuse existing GUI pages for ops runs and approvals
- write ACA handoff / incident summaries into Obsidian-backed workflows

Exit criteria:

- operators can review past runs and pending approvals
- completed investigations can be promoted into durable notes/runbooks

## Phase 6: Hardening and Enterprise Rollout

Scope:

- quotas, rate limits, retries, and backpressure
- threat-model review for credential boundaries
- docs/runbooks and Kubernetes deployment overlays
- staged rollout flags

Exit criteria:

- production-safe defaults are documented
- multi-team rollout can happen behind feature flags

## Probable Touchpoints

New packages and commands likely needed:

- `internal/storage/chatbindings`
- `internal/storage/opscfg`
- `internal/storage/opsruns`
- `internal/storage/opsactions`
- `internal/connectors/k8sbridge/*`
- `internal/ops/investigator/*`
- `skills/aws_state`
- `skills/grafana_state`
- `skills/azuredevops_state`
- `skills/kubernetes_state`
- `skills/kubernetes_action`
- `skills/azuredevops_action`
- `cmd/agentctl/cmd/ops.go`
- `cmd/agentctl/cmd/chat_bindings.go`

Existing areas likely to change:

- `internal/chatadapter/teams/*`
- `internal/web/server.go`
- `internal/web/api/*`
- `cmd/agentctl/cmd/mcp.go`
- `docs/architecture/chat-platform-adapter.md`
- `docs/architecture/kubernetes-runtime.md`

## Feature Flags and Safety Gates

Recommended flags:

- `AGENTCTL_OPS_TEAMS_BINDINGS=1`
- `AGENTCTL_OPS_INVESTIGATIONS=1`
- `AGENTCTL_OPS_READONLY_TOOLS=1`
- `AGENTCTL_K8S_CONNECTOR=1`
- `AGENTCTL_OPS_APPROVALS=1`
- `AGENTCTL_OPS_WRITE_ACTIONS=1`

Safety rules:

- all write paths require idempotency keys
- approval state is append-only
- large evidence always goes to CAS
- connector queues are bounded and single-writer-owned

## Testing Strategy

### Unit Tests

- store CRUD and ownership enforcement
- binding resolution logic
- typed tool input validation
- approval state transitions
- connector request/response correlation

### Integration Tests

- Teams bound channel -> `ops_run` creation
- read-only investigation using mocked AWS/Grafana/Azure DevOps backends
- connector-backed Kubernetes read path
- approval-required mutation path

### Smoke Tests

- Teams message in bound channel returns a run summary
- same run shows trace events and evidence artifact pointers
- approval button or API call resumes a paused mutation flow

## Risks and Mitigations

### Risk: Overloading existing Teams chat behavior with SRE-specific assumptions

Mitigation:

- keep `mode=session` and `mode=investigation` separate
- do not replace generic DM/chat behavior

### Risk: Secrets leaking into model context

Mitigation:

- use `secret_ref` indirection and server-side credential resolution
- keep raw credentials outside prompts, logs, and chat messages

### Risk: Centralized EKS access becoming too privileged

Mitigation:

- prefer outbound connector model
- scope access per org/team/cluster
- allow only typed operations

### Risk: Approval UX arrives too late, encouraging unsafe direct writes

Mitigation:

- ship read-only tools first
- block all write tools behind `AGENTCTL_OPS_APPROVALS`

### Risk: Creating a shadow config-service inside agentctl

Mitigation:

- keep configuration storage local and typed
- add only the records needed for bindings, integrations, runs, and approvals
- avoid building a generic dynamic config subsystem unless later justified

## Open Questions

1. Should the first UI surface live in the existing GUI, a minimal web API + JSON consumer, or Teams-only cards first?
2. Should the connector bridge run inside `agentctl web serve` initially or as a separate service mode from day one?
3. Which Azure DevOps write operations are actually needed first: rerun pipeline, work-item changes, PR actions, or release approvals?
4. For Grafana Cloud, do we need only Loki/Mimir/API access first, or also dashboard annotation and on-call alert correlation?
5. Which AWS surfaces matter first for your org: CloudWatch alarms/log groups, EKS, ECS, Lambda, or deployment metadata?

## Recommended First Patch

If we optimize for the shortest path to organizational value, the first patch should be:

1. `chatbindings` store
2. `opsruns` store
3. Teams `mode=investigation` routing
4. one read-only `kubernetes/state` and one read-only `grafana/state` slice
5. persisted run trace + CAS evidence summary

That yields a usable first experience without committing to the full connector and approval stack on day one.
