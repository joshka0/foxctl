# Implementation Plan: Companion Memory Salience Policy

Status: proposed active plan
Owner: solo maintainer
Last Updated: 2026-03-19

## Problem Statement

The current companion memory runtime already has strong primitives:

- hybrid/event-driven storage
- trust-labeled layers
- deterministic extraction for hard state
- trajectory outcomes with optional human ratings

What it does not have is a learned per-event retention policy.

Today the main control surface is the coarse retention preset in
`internal/context/companion/memory_behavior.go`, which changes budgets and recall
limits by agent type, but does not decide whether a specific message, tool
result, or episode is worth promotion. This is especially visible for tool
output: `tool_result` events are treated as memory-worthy in the gate, but the
immediate Tier 1 extraction path still reads `event.Content`, so large payloads
often become noise instead of useful retained knowledge.

This needs to be solved now because:

- compaction quality is increasingly governed by salience, not just token caps
- tool-heavy agents produce far more memory candidates than should survive
- human feedback is already captured in trajectory outcomes but does not flow
  back into memory retention decisions
- the current system cannot distinguish "large but irrelevant output" from
  "large output that contained the one fact that mattered"

The expected user-visible outcome is:

- less junk retained across turns and sessions
- better promotion of durable facts, decisions, and technical context
- cheaper compaction because low-salience tool output is summarized or dropped
  earlier
- incremental improvement over time from feedback without introducing a new
  model-serving stack

Assumptions for this plan:

- first slice is companion-first, not a full cross-agent memory rewrite
- runtime behavior must remain deterministic and rollback-safe when the learned
  policy is unavailable
- no new external ML dependency is introduced in the first slice

## Architecture Decision

Add a learned salience policy layer between hybrid event ingestion and memory
promotion.

The policy layer is composed of two cheap scorers:

1. a router that scores each candidate event/span as:
   - `drop`
   - `keep_recent_only`
   - `summarize_candidate`
   - `inspect_deeply`
2. a promotion classifier that maps inspected content to:
   - `none`
   - `evidence_short_ttl`
   - `episode_summary`
   - `hard_state_candidate`
   - `workspace_memory_candidate`

The first implementation should use a linear-weight policy with explicit
thresholds, not a hosted classifier. In practice this is a trained routing
policy, but operationally it behaves like the existing learnable scorer system:
small, cheap, explainable, and easy to run in shadow mode.

### Chosen approach

- Keep the current hybrid pipeline as the canonical runtime path.
- Add deterministic feature extraction over message and tool-result events.
- Persist every policy decision in a shadow log with features, scores, and the
  recommended action.
- Bind conversations to workspace/session/trace metadata so delayed feedback
  from trajectories can be attributed back to companion retention decisions.
- Derive delayed labels from:
  - hard-state promotion
  - evidence/episode promotion
  - later recall/surfacing in layered context
  - explicit human rating and feedback on matching trajectories
- Train policy weights periodically from those delayed labels.
- Start in `shadow` mode where the policy only logs decisions and never changes
  behavior.
- Only after evaluation passes, allow low-risk enforcement for
  `keep_recent_only` and `summarize_candidate` decisions on task/ephemeral
  agents.

### Why this approach

- It fits the existing event-sourced companion runtime in
  `internal/context/companion/hybrid_pipeline.go`.
- It reuses the existing optimization pattern already present in
  `internal/agent/optimization/*`.
- It does not require a second memory system or a real-time inference service.
- It preserves determinism by making the current heuristic pipeline the fallback
  on every failure path.
- It gives you a clean supervision surface for future model upgrades without
  committing the runtime to them today.

### Alternatives considered

- Fine-tuned transformer or hosted classifier:
  rejected for the first slice because it adds serving, deployment, and
  availability dependencies before the labels are trustworthy.
- Heuristics only:
  rejected because the core problem is delayed credit assignment, and heuristics
  alone will not learn which retained context actually mattered to successful
  task completion.
- End-to-end RL from human feedback:
  rejected as too expensive, too hard to debug, and mismatched to the current
  repo’s deterministic/inspectable engineering style.
- Training directly on final user rating only:
  rejected because the signal is too sparse and too delayed for per-event
  retention decisions.

### Diagram

```text
companion_events
    |
    v
Tier 0 bookkeeping
open episode state + tool run tracking
    |
    v
salience feature extraction
message text / tool payload preview / event metadata / retention preset
    |
    v
router policy
drop | keep_recent_only | summarize_candidate | inspect_deeply
    |
    +--> shadow log only (default rollout)
    |
    v
Tier 1 extraction / episode sealing
hard state / evidence / episodes / assumptions
    |
    v
layered context assembly
refs surfaced to prompt
    |
    v
delayed labeler
promotion + recall + feedback + outcome attribution
    |
    v
weight trainer
new policy version
```

## Design Patterns

- **Strategy Pattern**
  - Where applied: `internal/context/companion/retention_policy.go`
  - Why chosen: allows a rule-only baseline, a shadow learned policy, and a
    future stronger classifier behind the same runtime interface.

- **Event Sourcing + Materialized Views**
  - Where applied: `internal/context/companion/memory.go`,
    `internal/context/companion/hybrid_pipeline.go`
  - Why chosen: retention decisions and delayed outcomes should be append-like
    audit records derived from immutable events, not hidden mutable state.

- **Shadow Mode / Dual-Run Pattern**
  - Where applied: `internal/context/companion/service.go`,
    `internal/context/companion/hybrid_pipeline.go`
  - Why chosen: allows offline evaluation of policy quality before it changes
    compaction behavior.

- **Repository Pattern**
  - Where applied: companion memory access in `internal/context/companion/memory.go`
  - Why chosen: keeps SQL, migrations, and policy training queries in one
    explicit boundary instead of scattering them through the chat loop.

- **Delayed Supervision / Credit Assignment**
  - Where applied: `internal/context/companion/retention_labels.go`,
    `internal/agent/optimization/retention_trainer.go`
  - Why chosen: final human feedback is only useful after it is attributed back
    to the events and spans that shaped the answer.

## File Changes

### `internal/context/companion/hybrid_types.go` (modified)
- **Purpose**: Define typed retention-policy and conversation-binding records.
- **Key changes**:
  - Add `ConversationBinding`
  - Add `RetentionDecision`
  - Add `RetentionOutcome`
  - Add enums for route, target layer, horizon, and policy mode
  - Keep the types colocated with existing hybrid companion row models

```go
type RetentionRoute string

const (
	RetentionRouteDrop             RetentionRoute = "drop"
	RetentionRouteKeepRecentOnly   RetentionRoute = "keep_recent_only"
	RetentionRouteSummarize        RetentionRoute = "summarize_candidate"
	RetentionRouteInspectDeeply    RetentionRoute = "inspect_deeply"
)
```

### `internal/context/companion/memory.go` (modified)
- **Purpose**: Extend companion storage with additive schema for policy
  bindings, decisions, outcomes, and model weights.
- **Key changes**:
  - Add `companion_conversation_bindings`
    - `conversation_id`
    - `workspace_id`
    - `session_id`
    - `trace_id`
    - `memory_retention`
  - Add `companion_retention_decisions`
    - per-event or per-span policy decision
    - stored features JSON
    - policy name/version
    - route/target/horizon
    - score payload
    - mode (`disabled|shadow|enforce_low_risk|enforce`)
  - Add `companion_retention_outcomes`
    - whether the event was promoted
    - whether it was recalled later
    - whether it was surfaced into layered context
    - linked human rating bucket when available
  - Add `companion_retention_policy_weights`
    - workspace-scoped linear weights and thresholds
    - sample count and training timestamp
  - Add repository helpers:
    - `UpsertConversationBinding`
    - `InsertRetentionDecision`
    - `RecordRetentionOutcome`
    - `GetRetentionWeights`
    - `SaveRetentionWeights`
    - `ListRetentionTrainingExamples`

### `internal/context/companion/retention_features.go` (new)
- **Purpose**: Compute deterministic feature vectors from events before policy
  routing.
- **Key changes**:
  - Normalize message text and tool-result previews into a common candidate
    representation
  - Extract low-cost features such as:
    - event kind
    - tool name
    - token count
    - payload size bucket
    - presence of decision/preference/goal signals
    - extraction count
    - whether the event closes an active tool run
    - whether the retention preset is `companion|durable|task|ephemeral`
  - Introduce a payload-preview helper for `tool_result` so routing is not
    limited to `event.Content`

### `internal/context/companion/retention_policy.go` (new)
- **Purpose**: Runtime inference interface and baseline policy
  implementations.
- **Key changes**:
  - Define `RetentionPolicy` interface
  - Add `RuleBaselinePolicy`
  - Add `LinearWeightPolicy`
  - Expose explainable score breakdowns so debug surfaces can show why an event
    was routed a certain way
  - Enforce bounded outputs only; no freeform labels

```go
type RetentionPolicy interface {
	Route(ctx context.Context, candidate RetentionCandidate) (RetentionDecision, error)
}
```

### `internal/context/companion/retention_labels.go` (new)
- **Purpose**: Convert later promotions, recalls, and feedback into delayed
  training labels.
- **Key changes**:
  - Mark decisions as successful when the source event:
    - produced hard state that remained active
    - produced evidence later surfaced into context
    - contributed to an episode later selected for context
  - Mark decisions as weak/negative when a large event:
    - was never promoted
    - was never recalled
    - correlates with low-rated outcomes
  - Keep labels additive and provenance-bearing
  - Refuse to infer labels when conversation/session binding is missing

### `internal/context/companion/hybrid_pipeline.go` (modified)
- **Purpose**: Hook the router into the existing hybrid pipeline without
  breaking the current path.
- **Key changes**:
  - After Tier 0 bookkeeping and before Tier 1 extraction:
    - build a candidate from the event
    - run the policy
    - persist a shadow decision row
  - For `tool_result`, feed a normalized payload preview to the policy and Tier
    1 extraction path
  - In `shadow` mode, retain current behavior
  - In `enforce_low_risk` mode, allow only:
    - `drop` for clearly empty/noisy tool results
    - `keep_recent_only` for low-value ephemeral content
  - Preserve the current fallback if policy evaluation errors

### `internal/context/companion/v2_context_adapter.go` (modified)
- **Purpose**: Emit stable refs for surfaced memory items so delayed labels can
  detect what was actually shown to the model.
- **Key changes**:
  - Extend `Refs` beyond `turn/<id>` to include:
    - `hard_state/<id>`
    - `episode/<id>`
    - `evidence/<id>`
  - Populate `Meta` with counts of surfaced item types
  - Keep current layered text output unchanged

### `internal/context/companion/service.go` (modified)
- **Purpose**: Bind conversation metadata, preserve bundle refs, and control the
  rollout mode.
- **Key changes**:
  - Add service config knobs:
    - `RetentionPolicyMode`
    - `RetentionPolicyName`
    - `RetentionPolicy`
  - Register or update conversation bindings from existing hook/runtime context:
    - workspace
    - session
    - trace
    - retention preset
  - Stop throwing away layered bundle refs in `getLayeredMemoryContext`
  - Record “surfaced in prompt” outcomes for memory refs used in the current
    turn
  - Keep current prompt construction unchanged on every failure path

### `internal/agent/optimization/retention_trainer.go` (new)
- **Purpose**: Train cheap policy weights from delayed labels and human
  trajectory feedback.
- **Key changes**:
  - Reuse the existing optimization style from `learnable_scorer.go`
  - Read recent conversation bindings and retention outcomes by workspace
  - Join outcomes to rated trajectories by `session_id` or `trace_id`
  - Update linear weights and thresholds in bounded steps
  - Persist policy versions for rollback and evaluation

### `internal/agent/optimization/retention_trainer_test.go` (new)
- **Purpose**: Cover training behavior and outcome attribution.
- **Key changes**:
  - Verify weight updates remain bounded
  - Verify sparse feedback does not overfit
  - Verify low-rated trajectories do not poison unrelated workspaces

### `internal/context/companion/retention_policy_test.go` (new)
- **Purpose**: Unit-test feature extraction, routing, and fallback behavior.
- **Key changes**:
  - Test message vs tool-result feature extraction
  - Test payload preview truncation and redaction compatibility
  - Test `shadow` vs `enforce_low_risk` mode behavior
  - Test deterministic outputs from fixed weights

### `internal/context/companion/retention_labels_test.go` (new)
- **Purpose**: Verify delayed label generation.
- **Key changes**:
  - Test promotion-driven positive labels
  - Test surfaced-ref labels
  - Test missing binding fallback
  - Test conflicting signals resolution

### `internal/context/companion/memory_test.go` (modified)
- **Purpose**: Extend existing companion-memory tests to cover schema and
  additive behavior.
- **Key changes**:
  - Verify new tables migrate cleanly
  - Verify old conversations still load with no retention-policy records
  - Verify policy failures never block memory processing

### `docs/general/companion-memory.md` (modified)
- **Purpose**: Document the salience policy once the implementation exists.
- **Key changes**:
  - Add a section describing:
    - policy modes
    - routing labels
    - delayed supervision
    - shadow rollout expectations

## Testing Strategy

### Unit Tests

- `internal/context/companion/retention_policy_test.go`
  - `TestRetentionFeatureExtraction_Message`
  - `TestRetentionFeatureExtraction_ToolResultPreview`
  - `TestLinearWeightPolicy_RouteDeterministic`
  - `TestRuleBaselinePolicy_Fallback`
  - `TestRetentionPolicy_ShadowDoesNotChangeBehavior`
- `internal/context/companion/retention_labels_test.go`
  - `TestRetentionLabels_FromHardStatePromotion`
  - `TestRetentionLabels_FromSurfacedRefs`
  - `TestRetentionLabels_MissingConversationBinding`
  - `TestRetentionLabels_ConflictingSignalsPreferObservedUse`
- `internal/agent/optimization/retention_trainer_test.go`
  - `TestRetentionTrainer_UpdatesWeightsWithinBounds`
  - `TestRetentionTrainer_IgnoresSparseWorkspace`
  - `TestRetentionTrainer_JoinsBySessionOrTrace`

Mocking strategy:

- use in-memory SQLite stores for companion and trajectory persistence
- stub the `RetentionPolicy` interface when testing pipeline wiring
- avoid mocking the feature extractor; it should stay pure and table-driven

### Integration Tests

- Companion pipeline integration:
  - ingest turns and tool results
  - run `BuildHybridContextLayers`
  - verify decisions are logged and existing hard-state behavior still works in
    `shadow` mode
- Feedback attribution integration:
  - create conversation binding
  - record decisions and outcomes
  - record trajectory outcome with human rating
  - train weights and verify a new policy version is stored
- Layered context integration:
  - build prompt context
  - verify surfaced refs become retention outcomes

### Edge Cases

- Large tool output with no useful information:
  - could create false positives because size alone looks important
  - handled by payload preview + negative labels when never promoted/recalled
- Useful fact buried inside large tool output:
  - could be wrongly dropped
  - handled by `shadow` rollout first and by preserving current behavior on
    policy errors
- Missing session or trace binding:
  - feedback cannot be attributed
  - handled by leaving the decision unlabelled rather than fabricating credit
- Sparse feedback:
  - could overfit the policy
  - handled by bounded updates, minimum sample thresholds, and workspace-local
    training
- Cross-workspace contamination:
  - could leak weights or labels
  - handled by workspace-scoped bindings, decision rows, and stored weights

## Error Handling

- Policy inference errors are non-fatal.
  - Runtime behavior falls back to the current hybrid path.
  - The turn continues.
- Missing or malformed weights are non-fatal.
  - Use the rule baseline policy.
- Missing conversation binding is non-fatal.
  - Decisions are still logged.
  - Delayed feedback training skips unbound rows.
- Training errors are non-fatal to chat/runtime behavior.
  - They surface through logs and debug counters only.
- Unsupported routing outputs are rejected at the boundary.
  - Only predefined labels are accepted.

Recovery strategy:

- keep `shadow` as the default mode
- version stored weights so rollbacks are a simple version switch
- never gate the user-visible chat response on policy availability

## Migration Notes

- All schema changes are additive.
- No existing envelope or protocol shape changes are required.
- Existing conversations can be lazily bound when the service next sees them.
- Existing retention presets in `internal/domain/agent/agent.go` remain the
  coarse control plane; the salience policy only refines behavior inside those
  presets.
- Rollback plan:
  - set policy mode to `disabled`
  - ignore retention decision tables
  - keep logged data for later analysis

## Dependencies

- No new third-party runtime dependencies in the first slice.
- Reuse existing:
  - companion SQLite store
  - trajectory store
  - optimization patterns already used by `learnable_scorer.go`
- Optional future dependency:
  - a stronger offline trainer or exporter, only after shadow-mode data proves
    the labels are useful

## Implementation Order

1. Add additive companion schema and typed models for bindings, decisions,
   outcomes, and weights.
2. Implement deterministic retention feature extraction, including tool-result
   payload preview support.
3. Add rule baseline and linear-weight policy implementations behind a shared
   interface.
4. Wire policy evaluation into `BuildHybridContextLayers` in `shadow` mode only.
5. Extend layered context assembly to return stable surfaced refs and record
   prompt-use outcomes.
6. Add delayed label derivation from promotions, surfaced refs, and rated
   trajectories.
7. Implement the trainer that updates policy weights per workspace with bounded
   changes.
8. Add enforcement for low-risk actions only and keep it disabled by default.
9. Update companion-memory docs after the implementation is stable.

Each step is independently testable, and steps 1 through 5 should ship before
any enforcement is enabled.

## Open Questions

- Event-level vs span-level routing:
  - Some useful memory units are a tool-call/result pair rather than a single
    event.
  - Default for the first slice: route individual events and let episode
    summaries capture multi-event structure.
- How much explicit user feedback text should influence labels:
  - Text feedback can be high-value but noisy.
  - Default: use rating as the primary supervised signal and treat free-text
    feedback as operator/debug context first.
- Whether workspace memory candidates should land in named memory in phase 1:
  - This is useful but expands scope beyond companion-first rollout.
  - Default: log the label but do not auto-write workspace memory in phase 1.
- Whether `OutcomeFilter` needs a `SessionID` field:
  - Helpful for faster joins, but not required for the first implementation.
  - Default: use `ListTrajectories` and filter in process until query pressure
    justifies the schema/API change.
