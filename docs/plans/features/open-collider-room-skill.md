# Open-Collider as a Foxctl Room Skill Suite

| Field | Value |
|-------|-------|
| Status | Proposal |
| Scope | Port the open-collider bisociation engine to foxctl as a native skill suite, with progressive adoption of room protocol, foxprox multi-agent parallelism, and ContextWiki durable state |
| Related | [room-epic-pipeline.md](./room-epic-pipeline.md), [foxctl-evolve-plan.md](./foxctl-evolve-plan.md), [factory-mission-import.md](./factory-mission-import.md) |

## Conclusion

Open-collider is a structured bisociation engine: it forces an LLM to reason through a distant-domain mechanism before generating ideas, escaping the "default prompt basin." The foxctl port treats each phase of the engine as a **Skill**, the iteration lifecycle as a **Room Agile Epic**, and parallel idea generation as **foxprox multi-agent fan-out**.

The canonical pattern is three-phase:

1. **Phase 1 — Skill-only:** Go skills (`collider/domain_generate`, `collider/idea_generate`, `collider/score`, `collider/orchestrate`) replace the Python orchestrator. Parallelism is via goroutines calling ephemeral skill invocations. State lives in CAS artifacts and room messages.
2. **Phase 2 — Agent-driven:** The orchestrator becomes a room participant. It spawns foxprox sessions for domain strategists and idea generators, using room message fan-out for work distribution. Human operators curate via Pi inbox.
3. **Phase 3 — Heterogeneous models:** Each foxprox session uses a different CLI (`claude`, `codex`, `aider --model gemini`). The coordinator routes based on capability.

This plan covers Phase 1 in detail and sketches Phases 2–3.

---

## Category Boundary

This design keeps `bisociation engine` separate from `orchestration runtime`.

- The **bisociation engine** is prompt assembly, response parsing, threshold logic, and report generation — ported from Python to Go skills.
- The **orchestration runtime** is job tracking, room state, foxprox session lifecycle, and multi-agent routing — provided by foxctl.

The skill suite should not reimplement what foxctl already does (job persistence, CAS, room messages, task tracking). It should be a thin, deterministic layer on top of those primitives.

---

## Conceptual Mapping

| Open-Collider | Foxctl Primitive | Rationale |
|---|---|---|
| `brainstorm_state.json` | **Room state** + **ContextWiki observations** | Durable across restarts; queryable by other agents |
| `brainstorms/brainstorm_NNN/` | **Room Agile Epic** or dedicated **Room** | Lifecycle tracking, milestones per iteration |
| `iter_NNN/` | **Room Milestone** | Each iteration is a bounded phase with stories |
| Strategies (`fresh`/`deepen`/`refresh`) | **Room Stories** or **Tasks** with conditions | `has_loved` gates become task preconditions |
| Domain generation prompt | **Skill invocation** or **Agent prompt** dispatched via foxprox | Encapsulated, versioned, reusable |
| `(text_id, set_id)` combo | **Collision cell** = CAS artifact pair + room message | Addressable by digest; survives session death |
| `prompt.md` / `response.md` | **CAS artifacts** with digests | Reproducible, shareable, inspectable |
| Parallel idea gen (`asyncio.Semaphore`) | **Parallel skill jobs** (Phase 1) or **foxprox session fan-out** (Phase 2) | foxctl job runtime or room router handles parallelism |
| Scoring batches | **Judge skill** invoked with CAS refs | Loads ideas from CAS, not inline text |
| `loved_ideas.json` / `liked_ideas.json` | **Room message flags** + **ContextWiki promoted knowledge** | Human curation is durable; learnings accumulate |
| `domain_history.yaml` | **ContextWiki observation** or **vault note** | Exclusion lists become workspace knowledge |
| `REPORT.md` / `ITER_REPORT.md` | **CAS artifact** referenced by room message | Generated deliverable, not ephemeral output |

---

## Phase 1: Skill-Only Implementation

### Skill Inventory

#### `collider/domain_generate`

Generates a YAML domain bank for a given strategy.

```yaml
apiVersion: foxctl/v1
kind: Skill
metadata:
  name: collider/domain_generate
  version: 0.1.0
  description: "Generate a YAML domain bank using fresh, deepen, or refresh strategy"
distribution:
  type: exec
  exec:
    entry: skills/collider/domain_generate
io:
  format: JSON
  inline_output_kb: 64
signature:
  command: collider/domain_generate
  parameters:
    - name: strategy
      type: string
      required: true
      enum: [fresh, deepen, refresh]
    - name: project_dir
      type: string
      required: true
      description: "Absolute path to the open-collider project directory"
    - name: brainstorm_id
      type: string
      required: false
      description: "Target brainstorm session; uses current if omitted"
    - name: brief_digest
      type: string
      required: false
      description: "CAS digest of brief_validated.json; loads from project if omitted"
    - name: history_digest
      type: string
      required: false
      description: "CAS digest of domain_history.yaml"
    - name: loved_digest
      type: string
      required: false
      description: "CAS digest of loved_ideas.json (for deepen/refresh)"
    - name: liked_digest
      type: string
      required: false
      description: "CAS digest of liked_ideas.json (for refresh)"
    - name: model
      type: string
      required: false
      default: "claude-opus-4-20250514"
    - name: max_tokens
      type: integer
      required: false
      default: 16000
  returns:
    - name: domain_bank_digest
      type: string
      description: "CAS digest of the generated domain_bank.yaml"
    - name: sets_count
      type: integer
    - name: domains_count
      type: integer
    - name: model_used
      type: string
    - name: strategy
      type: string
capabilities:
  network: "egress"
  filesystem:
    - type: workdir
  pure: false
```

**Implementation notes:**
- Loads brief/history/loved/liked from CAS or filesystem.
- Delegates prompt assembly to strategy packages (`strategies/fresh.go`, `strategies/deepen.go`, `strategies/refresh.go`).
- Calls LLM via `internal/adapters/llm` ( Anthropic or OpenRouter, resolved from config).
- Parses YAML with `gopkg.in/yaml.v3`, validates `sets` key exists.
- Persists result to CAS, returns digest.

---

#### `collider/idea_generate`

Generates ideas for a single `(text_id, set_id)` collision.

```yaml
apiVersion: foxctl/v1
kind: Skill
metadata:
  name: collider/idea_generate
  version: 0.1.0
  description: "Generate ideas from a text input collided with a domain set"
distribution:
  type: exec
  exec:
    entry: skills/collider/idea_generate
io:
  format: JSON
  inline_output_kb: 128
signature:
  command: collider/idea_generate
  parameters:
    - name: project_dir
      type: string
      required: true
    - name: text_id
      type: string
      required: true
    - name: set_id
      type: string
      required: true
    - name: strategy_name
      type: string
      required: true
    - name: domain_bank_digest
      type: string
      required: true
    - name: iteration
      type: integer
      required: true
    - name: brainstorm_id
      type: string
      required: false
    - name: model
      type: string
      required: false
      default: "claude-sonnet-4-20250514"
    - name: temperature
      type: number
      required: false
      default: 0.9
    - name: max_tokens
      type: integer
      required: false
      default: 4000
  returns:
    - name: collision_id
      type: string
    - name: ideas
      type: array
      description: "List of parsed idea dicts with idea_num, text, combo, text_id, set_id"
    - name: prompt_digest
      type: string
    - name: response_digest
      type: string
    - name: model_used
      type: string
capabilities:
  network: "egress"
  filesystem:
    - type: workdir
  pure: false
```

**Implementation notes:**
- Loads domain bank from CAS, text input from `input_bank.yaml` + filesystem.
- Assembles prompt via `phases/idea_generator.go` (port of Python `IdeaGenerator`).
- Saves `prompt.md` and `response_{model}.md` to collision cell directory under `brainstorms/<id>/iter_NNN/strategy_<name>/<collision_id>/`.
- Persists both to CAS, returns digests.
- Parses response with multilingual regex (`Idea` / `Idée` / `Idee` / `Concept`).

---

#### `collider/score`

Scores a batch of ideas against the 5-axis rubric.

```yaml
apiVersion: foxctl/v1
kind: Skill
metadata:
  name: collider/score
  version: 0.1.0
  description: "Score a batch of ideas using the 5-axis judge rubric"
distribution:
  type: exec
  exec:
    entry: skills/collider/score
io:
  format: JSON
  inline_output_kb: 128
signature:
  command: collider/score
  parameters:
    - name: ideas_digest
      type: string
      required: true
      description: "CAS digest of ideas JSON array"
    - name: batch_id
      type: integer
      required: true
    - name: project_dir
      type: string
      required: true
    - name: judge_config_digest
      type: string
      required: false
      description: "CAS digest of judge_config.json with ref_high/ref_low"
    - name: model
      type: string
      required: false
      default: "claude-sonnet-4-20250514"
    - name: max_tokens
      type: integer
      required: false
      default: 8000
    - name: temperature
      type: number
      required: false
      default: 0.1
  returns:
    - name: scored_ideas
      type: array
    - name: batch_id
      type: integer
    - name: threshold_used
      type: number
    - name: retained_count
      type: integer
    - name: judge_response_digest
      type: string
capabilities:
  network: "egress"
  filesystem:
    - type: workdir
  pure: false
```

**Implementation notes:**
- Loads ideas from CAS (not inline — batches can be large).
- Loads `judge.md` prompt template from project `prompts/` via `PromptResolver`.
- Injects calibration references from `judge_config.json` if available.
- Calls judge model, parses scoring table with regex (supports `|`, `**4**/5`, etc.).
- Recalculates `score_aggregate` from configurable `judge_axes` weights.
- Applies threshold with drift (default 4.2 → floor 4.0, step 0.1).
- Sets `retained` flag per idea.

---

#### `collider/orchestrate`

Meta-skill that drives a full iteration or the full pipeline.

```yaml
apiVersion: foxctl/v1
kind: Skill
metadata:
  name: collider/orchestrate
  version: 0.1.0
  description: "Orchestrate a full open-collider brainstorm iteration"
distribution:
  type: exec
  exec:
    entry: skills/collider/orchestrate
io:
  format: JSON
  inline_output_kb: 32
signature:
  command: collider/orchestrate
  parameters:
    - name: project_dir
      type: string
      required: true
    - name: mode
      type: string
      required: true
      enum: [domain, ideas, score, finalize, full_iteration]
    - name: brainstorm_id
      type: string
      required: false
    - name: iteration
      type: integer
      required: false
      description: "Target iteration for finalize or score replay"
    - name: max_concurrent_ideas
      type: integer
      required: false
      default: 4
    - name: max_concurrent_scoring
      type: integer
      required: false
      default: 3
  returns:
    - name: iteration
      type: integer
    - name: ideas_generated
      type: integer
    - name: ideas_retained
      type: integer
    - name: strategies_detail
      type: object
    - name: report_digest
      type: string
      description: "CAS digest of generated REPORT.md or ITER_REPORT.md"
    - name: status
      type: string
      enum: [awaiting_curation, awaiting_flags, ready]
capabilities:
  network: "egress"
  filesystem:
    - type: workdir
  pure: false
```

**Implementation notes:**
- Maintains iteration state in `brainstorm_state.json` (local to project) AND emits room messages if run inside a room context.
- For `full_iteration`:
  1. Calls `init_iteration` equivalent → creates `iter_NNN/` directory.
  2. For each enabled strategy, calls `collider/domain_generate` (sequential).
  3. Samples combos (`sample_combos` in Go), fans out `collider/idea_generate` calls via goroutines + semaphore.
  4. Collects all ideas, batches them, fans out `collider/score` calls.
  5. Applies threshold, calls `finalize_iteration` → saves JSONs, updates domain history, generates report.
  6. Emits result envelope.
- If run with `--ephemeral`, skips job persistence for sub-invocations.
- If run job-tracked, each sub-invocation becomes a child job for traceability.

---

#### `collider/curate`

Applies love/like/trash flags and rebuilds loved/liked stores.

```yaml
apiVersion: foxctl/v1
kind: Skill
metadata:
  name: collider/curate
  version: 0.1.0
  description: "Apply curation flags and rebuild loved/liked idea stores"
distribution:
  type: exec
  exec:
    entry: skills/collider/curate
io:
  format: JSON
  inline_output_kb: 16
signature:
  command: collider/curate
  parameters:
    - name: project_dir
      type: string
      required: true
    - name: iteration
      type: integer
      required: true
    - name: flags
      type: object
      required: true
      description: "Map of idea_id -> 'loved' | 'liked' | 'trashed'"
    - name: brainstorm_id
      type: string
      required: false
  returns:
    - name: loved_count
      type: integer
    - name: liked_count
      type: integer
    - name: trashed_count
      type: integer
    - name: status
      type: string
      enum: [ready, awaiting_flags]
capabilities:
  network: "none"
  filesystem:
    - type: workdir
  pure: false
```

---

### Go Package Layout (Phase 1)

```
skills/collider/
  domain_generate/
    main.go
    skill.yaml
  idea_generate/
    main.go
    skill.yaml
  score/
    main.go
    skill.yaml
  orchestrate/
    main.go
    skill.yaml
  curate/
    main.go
    skill.yaml

internal/collider/
  config/           # load_config.go — merge defaults + project_config.yaml
  strategies/
    fresh.go
    deepen.go
    refresh.go
  phases/
    idea_generator.go      # sample_combos, assemble_prompt, parse_response
    idea_scorer.go         # assemble_prompt, parse_response, apply_threshold
  scoring/
    score_parser.go        # AxisScores, parse_scoring_table, extract_judge_notes
    data_loader.go         # TextInputMeta, DomainSetMeta, DataLoader
  prompt_resolver.go      # PromptResolver, load_brief_content
  skill_interface.go      # init_iteration, finalize_iteration, generate_report
  state.go                # _load_state, _save_state, _make_fresh_state
  reports.go              # generate_iter_report, generate_brainstorm_report
```

**Re-use strategy:**
- `internal/collider/*` is a library shared by all collider skills.
- Each skill's `main.go` is a thin wrapper: parse skill input → call library function → emit skill output.
- The library is a direct port of `open-collider/src/open_collider/*`, adapted to Go idioms and foxctl primitives (CAS, skillmain.RunContext).

---

## Phase 2: Agent-Driven with Foxprox

In Phase 2, `collider/orchestrate` ceases to be a standalone skill and becomes a **room participant agent** (or a skill that spawns room participants).

### Room Topology

```
Room: collider-<project>-<brainstorm-id>
├── Participant: collider-orchestrator (coordinator role)
│   └── Owns iteration lifecycle, task creation, dispatch
├── Participant: domain-strategist-fresh (agent via foxprox)
│   └── Runs claude with fresh-strategy prompt
├── Participant: domain-strategist-deepen (agent via foxprox, conditional)
│   └── Runs claude with deepen-strategy prompt
├── Participant: domain-strategist-refresh (agent via foxprox, conditional)
│   └── Runs claude with refresh-strategy prompt
├── Participant: idea-agent-1 .. idea-agent-N (foxprox sessions)
│   └── Each cycles through assigned combos
├── Participant: judge-agent (foxprox session or direct skill)
│   └── Loads idea batches from CAS, scores them
└── Participant: operator (human via Pi)
    └── Reviews inbox, flags ideas
```

### Orchestrator Behavior

1. **Init iteration** → posts room message:
   ```json
   {"kind": "collider.iteration.started", "iteration": 3, "strategies": ["fresh","deepen"]}
   ```

2. **Domain generation** → creates room tasks:
   ```
   Task: "Generate fresh domains" (assigned to domain-strategist-fresh)
   Task: "Generate deepen domains" (assigned to domain-strategist-deepen, depends on loved ideas)
   ```
   Each strategist agent receives a structured room message with brief digest, exclusion list, and expected output format. It responds with a room message containing the YAML domain bank.

3. **Combo dispatch** → after all domain banks arrive, the orchestrator:
   - Samples combos
   - Creates `collider/idea_generate` room tasks (or spawns foxprox sessions)
   - Each task/session receives: `collision_id`, `prompt_digest` (or inline prompt), `model`

4. **Collection** → polls room messages / task completion for idea artifacts.

5. **Scoring dispatch** → batches ideas, creates `collider/score` tasks.

6. **Finalization** → posts `collider.iteration.finalized` message with report digest. Sets room status to `awaiting_curation`.

### Foxprox Session Lifecycle

```go
// Pseudo-code for orchestrator spawning an idea agent via foxprox
c := foxproxclient.ForSocket("~/.foxctl/foxprox.sock")

sessionID, _ := c.CreateSession(ctx, foxprox.CreateSessionRequest{
    Cmd:        "claude",           // or "codex", "aider"
    SubmitKey:  "Enter",
    WorkingDir: projectDir,
})

c.JoinRoom(ctx, roomID, foxprox.JoinRoomRequest{
    AgentID:   "idea-agent-" + idx,
    SessionID: sessionID,
    Role:      "collider.idea_generator",
})

// Send structured intent via room message
// The adapter compiles it into the exact PTY bytes for claude
```

**Delivery policies:**
- Domain strategist sessions: `queue` or `safe-prompt-only` (wait for prompt readiness)
- Idea agent sessions: `immediate` or `queue` (agents should be idle between combos)
- Judge session: `safe-prompt-only` (needs clean context)

### Human Curation via Pi

After iteration finalization:
- Pi operator sees room message: `"Iteration 3 complete. 12 ideas retained. Awaiting curation."`
- Operator reviews `ITER_REPORT.md` artifact (or inline summary).
- Operator sends flags via `collider/curate` skill or direct room command:
  ```
  foxctl run collider/curate --input '{"project_dir":"...","iteration":3,"flags":{"idea_abc":"loved","idea_def":"trashed"}}'
  ```
- Curate skill rebuilds `loved_ideas.json`/`liked_ideas.json`, posts `collider.curation.completed` room message.

---

## Phase 3: Heterogeneous Models

Phase 3 is a configuration change, not new code.

Each strategy and each combo can specify a different model. The orchestrator routes to the appropriate foxprox session:

| Work Unit | Preferred Model | Foxprox Session |
|---|---|---|
| Domain generation (`fresh`) | Claude Opus (deep reasoning) | `claude` CLI |
| Domain generation (`deepen`) | Claude Sonnet (structure) | `claude` CLI |
| Domain generation (`refresh`) | Gemini 2.5 Pro (pattern extraction) | `aider --model gemini` |
| Idea generation (volume) | Claude Sonnet or Codex | `claude` / `codex` |
| Idea generation (nuanced) | Claude Opus | `claude` CLI |
| Scoring | Claude Sonnet (consistent rubric) | `claude` CLI |

The skill manifest for `collider/orchestrate` gains a `model_routing` parameter:

```json
{
  "model_routing": {
    "domain_fresh": "claude-opus-4-20250514",
    "domain_deepen": "claude-sonnet-4-20250514",
    "domain_refresh": "gemini-2.5-pro",
    "generation": "claude-sonnet-4-20250514",
    "scoring": "claude-sonnet-4-20250514"
  }
}
```

The orchestrator maintains a pool of foxprox sessions per model. When a combo needs generation, it picks the session matching the configured model.

---

## Data Model & CAS Artifacts

### Artifact Types

| Artifact | MIME Type | Produced By | Consumed By |
|---|---|---|---|
| `brief_validated.json` | `application/json` | User / import skill | domain_generate, idea_generate |
| `input_bank.yaml` | `text/yaml` | User | idea_generate |
| `domain_bank.yaml` | `text/yaml` | domain_generate | idea_generate, state |
| `domain_history.yaml` | `text/yaml` | finalize_iteration | domain_generate (fresh) |
| `judge_config.json` | `application/json` | User | score |
| `prompt.md` | `text/markdown` | idea_generate | Debugging, reproducibility |
| `response_{model}.md` | `text/markdown` | idea_generate | Debugging, reproducibility |
| `ideas.json` | `application/json` | idea_generate | score |
| `scored_ideas.json` | `application/json` | score | finalize_iteration, curate |
| `flags.json` | `application/json` | curate | curate (rebuild), state |
| `loved_ideas.json` | `application/json` | curate | domain_generate (deepen/refresh) |
| `liked_ideas.json` | `application/json` | curate | domain_generate (refresh) |
| `config.json` | `application/json` | finalize_iteration | Reporting |
| `ITER_REPORT.md` | `text/markdown` | finalize_iteration | Pi operator |
| `REPORT.md` | `text/markdown` | finalize_iteration | Pi operator |

### State Persistence

Local filesystem state (for Phase 1 parity):
- `brainstorm_state.json` — current iteration, brainstorm_id, status, totals
- `brainstorms/brainstorm_NNN/` — per-brainstorm directory
  - `iter_NNN/` — per-iteration directory
    - `domains/`, `strategy_*/`, `scored_ideas.json`, `flags.json`, `config.json`, `ITER_REPORT.md`
  - `loved_ideas.json`, `liked_ideas.json`, `domain_history.yaml`, `REPORT.md`

Room-integrated state (Phase 2+):
- Room messages carry `kind: collider.*` with artifact digests.
- ContextWiki observations capture:
  - `"Domain family X exhausted after 3 iterations — avoid in fresh"`
  - `"Scoring drifted to 4.0 on iter 5; consider increasing n_sets"`
- Vault notes promote evergreen learnings: `"Successful mechanism: feedback loops in biological systems → governance design"`

---

## Integration with ContextWiki

### Observations (auto-generated)

The orchestrator posts observations after each iteration:

```yaml
kind: contextwiki.observation
source: collider/orchestrate
content:
  iteration: 5
  strategies_used: [fresh, deepen]
  ideas_generated: 240
  ideas_retained: 12
  threshold_used: 4.0
  drift_occurred: true
  top_scoring_set: DS3
  weakest_set: DS7
```

### Tensions (auto-generated)

```yaml
kind: contextwiki.tension
source: collider/orchestrate
content:
  description: "Retention rate dropped below 5%"
  iteration: 5
  severity: warning
  suggested_action: "Increase n_domains_per_set or relax score_threshold"
```

### Promoted Knowledge

After several iterations, the operator (or a curator agent) promotes:

```yaml
# Vault note: collider-mechanisms/governance-feedback-loops.md
---
promoted_from: brainstorm_auth_refresh
mechanism: "Biological feedback loops as governance design pattern"
source_disciplines: [mycology, immunology, ecology]
idea_scores: [4.6, 4.5, 4.3]
transferable_to: ["decentralized org design", "protocol incentive design"]
---
```

This becomes searchable by other foxctl agents via `foxctl_search` or `foxctl_repoindex_search`.

---

## Open Questions

1. **Job vs. Message for sub-invocations:** Should `collider/orchestrate` spawn child jobs (traceable, recoverable) or ephemeral runs (lighter)? Probably: job-tracked for domain generation and scoring (rare, expensive), ephemeral for idea generation (many, fast).

2. **Foxprox session reuse:** Should idea-agent foxprox sessions be long-lived (spawn once, cycle many combos) or per-combo (spawn, run, destroy)? Long-lived is cheaper but requires session health checks.

3. **Scoring model calibration:** Should `judge_config.json` ref_high/ref_low be static or updated per iteration based on loved ideas? Proposal: static for consistency, with an optional `auto_calibrate` flag that extracts refs from top-loved ideas.

4. **Room vs. Project boundary:** One room per brainstorm, or one room per project with multiple brainstorms? Recommendation: one room per brainstorm for isolation; project-level aggregation via ContextWiki.

5. **Pi inbox integration:** Should retained ideas appear as individual inbox items for curation, or as a single report? Recommendation: single report with embedded idea cards; Pi can ack/reply with flags.

---

## Rollout Sequence

| Phase | Deliverable | Effort | Blockers |
|---|---|---|---|
| 1a | Port `internal/collider/` library (config, strategies, phases, scoring, reports) | Medium | None |
| 1b | Implement `collider/domain_generate`, `collider/idea_generate`, `collider/score` skills | Medium | 1a |
| 1c | Implement `collider/orchestrate` and `collider/curate` skills | Medium | 1b |
| 1d | Integration test: full iteration end-to-end with sample project | Small | 1c |
| 2a | Add room-message emission to orchestrator | Small | 1c |
| 2b | Foxprox session spawning from orchestrator | Medium | 2a, foxprox stability |
| 2c | Pi inbox curation flow (human operator) | Small | 2b |
| 3 | Model routing configuration + heterogeneous session pools | Small | 2b |

---

## Appendix: Why This Is Worth It

Open-collider in Python is a single-process, single-model, single-user tool. The foxctl port makes it:

- **Durable:** Room state survives process restarts; CAS artifacts survive workspace moves.
- **Observable:** Every sub-invocation is a job with logs, every idea is an artifact with a digest.
- **Collaborative:** Multiple humans or agents can curate in the same room.
- **Heterogeneous:** Different models for different phases, selected by capability.
- **Integrable:** Bisociation outputs feed into ContextWiki, repoindex, and downstream foxctl skills.

The core insight of open-collider is that creativity is not generation but **structured search**. The foxctl version extends that insight from the prompt level to the systems level: the brainstorm itself is a search process distributed across specialized actors, coordinated by durable messages, and reviewed by human operators.
