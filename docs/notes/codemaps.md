# Codemaps to Generate

This document lists the core **code areas** in `foxctl` that should have
Codemaps.\
Each entry has:

- **Name**: Suggested `@[Codemap Name]` handle.
- **Goal**: Why this codemap exists.
- **Prompt**: Text to feed to a codemapper.

---

## 1. Core Envelope Protocol & CLI Flow

- **Name**: `@[foxctl Envelope & CLI Pipeline]`
- **Goal**: Understand how JSON envelopes are built, validated, and written from
  Cobra commands through the domain layer.
- **Prompt**:\
  “Map the complete envelope pipeline in foxctl, starting from Cobra command
  handlers under `cmd/foxctl/cmd/` through the domain `envelope` package. Show
  how success/error envelopes are constructed (`OK`, `Error`), validated
  (`Validate`), canonicalized, and written to stdout. Highlight key functions in
  `internal/domain/envelope/envelope.go`, how commands in
  `cmd/foxctl/cmd/*.go` call into them, and where RFC3339 timestamps and
  RFC8785 canonical JSON are applied.”

---

## 2. Skill System: Installation → Execution → CAS

- **Name**:
  `@[foxctl Skill System: Installation, Execution, Jobs, CAS, and Plugin Architecture]`
- **Goal**: Single map of the whole skill lifecycle (you already have an
  example; this is the canonical prompt).
- **Prompt**:\
  “Map the full skill lifecycle in foxctl from installation to execution.
  Start at the skill installer in `internal/domain/skill/installer.go` (Install,
  LoadManifest, validateManifest), then show how installed skills are invoked by
  the runner (`internal/runtime/execution/runner`, `skills/*/main.go`) using envelope
  I/O. Include how large outputs go through CAS
  (`internal/storage/cas/store.go`), how jobs are created and tracked for skill
  runs, and where plugins (auth/pagination) integrate with the http/openapi
  skill. The map should show entrypoints, key structs, and the flow of
  envelopes, jobs, and CAS digests.”

---

## 3. Job Submission, SQLite Storage & WFQ Scheduler

- **Name**: `@[foxctl Job System: Types, Storage, and WFQ Scheduler]`
- **Goal**: Understand job lifecycle and how work is scheduled.
- **Prompt**:\
  “Map the job system in foxctl, from job creation to completion. Start at the
  job CLI or API entrypoints, then show the `Job` type and `HashArgs` in
  `internal/storage/jobs/types/types.go`, how jobs are persisted to SQLite, and
  how they move through states (`queued`, `running`, `ok`, `error`, `canceled`).
  Then trace how the WFQ scheduler in `internal/runtime/execution/scheduler/wfq.go`
  picks jobs: `NewWFQScheduler`, schedulable Job struct, `SetWeight`, and worker
  loop. Highlight where execution is delegated to the execution layer (WASI/exec
  runners).”

---

## 4. CAS Storage & Integrity Verification

- **Name**: `@[foxctl CAS: Put, Get, Integrity, Deduplication]`
- **Goal**: Make it easy to reason about CAS behavior and where to hook new
  uses.
- **Prompt**:\
  “Map the content-addressable storage (CAS) system in foxctl. Start at
  `internal/storage/cas/store.go` (`NewStore`, `Put`,
  [Get](cci:1://file://~/repos/personal/claude-harness/foxctl/internal/agent/runtime/runtime.go:425:0-431:1)),
  showing directory layout (sha256, pins, tmp). Detail how `Put` streams data,
  computes SHA‑256 digests, handles duplicates, and finalizes uploads. Show how
  CAS objects are referenced from envelopes (via `data.artifact` and optional
  `meta.cas_digest`) and from jobs/skills. Include the integrity verification
  step on
  [Get](cci:1://file://~/repos/personal/claude-harness/foxctl/internal/agent/runtime/runtime.go:425:0-431:1).”

---

## 5. Execution Runners: WASI & Exec with Network Policy

- **Name**: `@[foxctl Runners: WASI, Exec, and Network Policy]`
- **Goal**: Clarify how skills actually run and how network/FS policy is
  enforced.
- **Prompt**:\
  “Map the execution runners in foxctl, focusing on WASI and exec. Start at
  `internal/runtime/execution/runner` and relevant subpackages (`wasi`, `exec`). Show
  how a job or skill invocation is turned into a subprocess or WASI invocation,
  how environment, filesystem roots, and network policy are configured, and how
  envelopes are piped via stdin/stdout. Highlight where `policy.PathValidator`
  is used for fs/text tools, and where network ‘none’ vs ‘egress’ is enforced
  according to Core Profile v1.”

---

## 6. Dspy-Go Agent Runtime, Tools & Sessions

- **Name**: `@[foxctl Dspy Agent Runtime & Tools Registry]`
- **Goal**: Help contributors understand the dspy-go integration (sessions,
  tools, runtime).
- **Prompt**:\
  “Map the dspy-go agent runtime in `internal/agent/runtime` and its integration
  with tools under `internal/agent/tools`. Start from the CLI entrypoint
  [cmd/foxctl/cmd/dspy_agent.go](cci:7://file://~/repos/personal/claude-harness/foxctl/cmd/foxctl/cmd/dspy_agent.go:0:0-0:0)
  (`dspy-agent` commands), through
  [getOrCreateRuntime](cci:1://file://~/repos/personal/claude-harness/foxctl/cmd/foxctl/cmd/dspy_agent.go:87:0-118:1)
  and
  [runtime.NewRuntime](cci:1://file://~/repos/personal/claude-harness/foxctl/internal/agent/runtime/runtime.go:83:0-99:1),
  to
  [Runtime.Spawn](cci:1://file://~/repos/personal/claude-harness/foxctl/internal/agent/runtime/runtime.go:101:0-207:1),
  [Session](cci:2://file://~/repos/personal/claude-harness/foxctl/internal/agent/runtime/runtime.go:66:0-81:1)
  management, and how tools are registered
  ([agenttools.NewRegistry](cci:1://file://~/repos/personal/claude-harness/foxctl/internal/agent/tools/tools.go:55:0-111:1)).
  Show how dspy-go `ReActAgent` is created
  ([createAgent](cci:1://file://~/repos/personal/claude-harness/foxctl/internal/agent/runtime/runtime.go:209:0-291:1)),
  how timeouts and iteration limits are applied, and how tool calls and traces
  are recorded in
  [types.ToolCall](cci:2://file://~/repos/personal/claude-harness/foxctl/internal/agent/types/types.go:301:0-319:1)
  /
  [ExecutionTrace](cci:2://file://~/repos/personal/claude-harness/foxctl/internal/agent/types/types.go:322:0-325:1).”

---

## 7. Overseer, Hierarchy, Mailbox & Spawn Protocol

- **Name**:
  `@[foxctl Overseer & Agent Hierarchy: Spawn, Mailbox, Depth Limits]`
- **Goal**: Understand how the overseer coordinates agent trees and spawn
  requests.
- **Prompt**:\
  “Map the overseer and agent hierarchy model in foxctl. Start with the
  overseer in
  [internal/agent/runtime/overseer.go](cci:7://file://~/repos/personal/claude-harness/foxctl/internal/agent/runtime/overseer.go:0:0-0:0)
  and the hierarchy spec in `docs/spec/agent_hierarchy.md` /
  `docs/spec/overseer_profile.md`. Show how
  [OverseerConfig](cci:2://file://~/repos/personal/claude-harness/foxctl/internal/agent/runtime/overseer.go:27:0-42:1),
  [HandleSpawnRequest](cci:1://file://~/repos/personal/claude-harness/foxctl/internal/agent/runtime/overseer.go:68:0-201:1),
  and `ValidateSpawnDepth` work, how
  [SpawnRequest](cci:2://file://~/repos/personal/claude-harness/foxctl/internal/agent/types/types.go:340:0-369:1)
  /
  [SpawnResponse](cci:2://file://~/repos/personal/claude-harness/foxctl/internal/agent/types/types.go:387:0-402:1)
  and
  [SpawnedAgent](cci:2://file://~/repos/personal/claude-harness/foxctl/internal/agent/types/types.go:405:0-417:1)
  /
  [DeniedAgent](cci:2://file://~/repos/personal/claude-harness/foxctl/internal/agent/types/types.go:420:0-429:1)
  types in `internal/agent/types` are used, and how the mailbox/blackboard
  concepts (from `docs/spec/mailbox_blackboard.md`) tie into spawn
  request/response subjects. Include how depth limits (`Depth`, `MaxDepth`,
  `LocalMaxDepth`) and concurrency limits (`MaxConcurrentAgents`) are enforced.”

---

## 8. LLM Planning Stack (Auto/Gemini/OpenAI/OpenRouter)

- **Name**:
  `@[foxctl Planning LLM Stack: Auto, Providers, and Integration Tests]`
- **Goal**: Make it clear how planning picks providers/models and where
  integration tests live.
- **Prompt**:\
  “Map the planning LLM stack in `internal/intelligence/planning/llm`. Start with the auto
  planner in `auto.go` and show how it selects providers and models based on
  config/env. Then map the OpenAI-compatible planner
  ([openai.go](cci:7://file://~/repos/personal/claude-harness/foxctl/internal/intelligence/planning/llm/openai.go:0:0-0:0)),
  including
  [OpenAIConfig](cci:2://file://~/repos/personal/claude-harness/foxctl/internal/intelligence/planning/llm/openai.go:14:0-20:1),
  [NewOpenAIPlanner](cci:1://file://~/repos/personal/claude-harness/foxctl/internal/intelligence/planning/llm/openai.go:29:0-90:1)
  provider detection (OPENROUTER_API_KEY, GROQ_API_KEY, OPENAI_API_KEY), BaseURL
  and model selection, and
  [Plan](cci:1://file://~/repos/personal/claude-harness/foxctl/internal/intelligence/planning/llm/openai.go:125:0-195:1).
  Include how integration tests in `internal/intelligence/planning/llm/planner_test.go` are
  structured (e.g., OpenRouter integration) and how CI wires env vars and test
  gating.”

---

## 9. OpenAPI Skill & Plugin Protocol (Auth + Pagination)

- **Name**: `@[foxctl OpenAPI Skill & Plugin Protocol]`
- **Goal**: Provide a single view of how the http/openapi skill works and where
  plugins hook in.
- **Prompt**:\
  “Map the http/openapi skill and plugin protocol in foxctl. Start at the core
  OpenAPI implementation under `internal/interfaces/openapi/*` (auth, pagination, client,
  loader, retry), then show how the `http/openapi` skill wraps it
  (skills/http_openapi). Trace the request flow from CLI or agent through spec
  loading, auth selection, pagination, and retries, down to HTTP client calls.
  Then map the plugin protocol from `docs/spec/plugin_protocol.md`: how
  auth/pagination plugins are invoked as subprocesses, the envelope format they
  receive and return, and where resource limits (timeout/CPU/memory) are
  enforced.”

---

## 10. Knowledge System & Factory Droids / Claude Code

- **Name**: `@[foxctl Knowledge System & Factory Droids]`
- **Goal**: Explain the knowledge registry and built-in Factory droid documents.
- **Prompt**:\
  “Map the knowledge system in foxctl, focusing on builtin Factory droids.
  Start at `internal/context/knowledge/builtin` (`factory.go`, `data/droids/*.md`) and
  show how `SeedFactoryKnowledge` / `ListFactoryDroids` work. Then connect to
  the CLI flows for `foxctl knowledge sync/search` (if present) and the specs
  in `docs/spec/knowledge_registry.md` and
  `docs/spec/knowledge_factory_bridge.md`. Highlight how knowledge items are
  named (`factory/droid/<slug>`), how they are stored in SQLite, and how they
  are intended to be surfaced by hooks (e.g., future knowledge router).”

---

## 11. Test Infrastructure, Test Watcher & Feedback Hooks

- **Name**: `@[foxctl Test Infra: Test-Watch, Feedback Hooks, and CI Targets]`
- **Goal**: Onboard people to the test feedback loop and Makefile targets.
- **Prompt**:\
  “Map the test infrastructure in foxctl, focusing on the test watcher and CI
  targets. Start with `internal/storage/testwatch/` and
  `internal/tooling/testwatch/runner.go`, showing how test watch configs are parsed, how
  fsnotify watching works, and how test runs are triggered and parsed. Then map
  the CLI commands in `cmd/foxctl/cmd/testwatch.go` and `watch.go`. Finally,
  connect to the CI-facing Makefile targets (`test`, `test-short`, `test-race`,
  `check-coverage`) and the coverage thresholds defined in
  [AGENTS.md](cci:7://file://~/repos/personal/claude-harness/foxctl/AGENTS.md:0:0-0:0)
  (`lines`, `functions`, `branches`). Show how these pieces work together to
  provide developer feedback.”

---

## 12. Core Profile v1: End-to-End Envelope, Jobs & CAS Flow

- **Name**: `@[foxctl Core Profile v1 End-to-End Flow]`
- **Goal**: A top-level “big picture” map tying envelopes, jobs, CAS, and skills
  together per the spec.
- **Prompt**:\
  “Produce an end-to-end Core Profile v1 codemap for foxctl. Starting from a
  high-level CLI invocation (e.g., running a skill or command), show how a JSON
  envelope is constructed, validated, and written; how large results are
  offloaded to CAS with summaries; how jobs are created, scheduled, and executed
  via WASI/exec runners; and how hooks/memory/knowledge integrate. Use
  `docs/spec/core_profile_v1.md` as the canonical reference, then anchor each
  spec concept to its concrete implementation in `internal/envelope`,
  `internal/storage/cas`, `internal/storage/jobs`, `internal/runtime/execution/*`, and
  `skills/*`.”

---
