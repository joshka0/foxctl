# Follow Up: Durable Execution

Status as of 2026-05-06: Layer 1 is complete for the LLM-chat durable
execution baseline. That means durable identity, turn request registration,
idempotent event append, and LLM-chat model/tool effect journaling are in place.

Do not treat this as a claim of exactly-once execution. The completed baseline
is replay-safe enough to avoid re-running completed model/tool effects and to
fail closed on ambiguous incomplete effects.

## Remaining Work

### Operator Recovery For Incomplete Effects

Add an operator path to inspect and resolve effect rows that are stuck in
`intent` without a terminal result.

Minimum useful shape:

- list incomplete model/tool effects by run, request, turn, and age
- show stored input, replay policy, and any linked run failure
- mark unresolved effects as failed with an operator reason
- optionally attach an externally recovered result when the operator can prove
  the provider/tool completed

This should be a recovery surface, not automatic replay for side-effecting work.

### RLM REPL Composite Effect

Keep RLM REPL deferred from the Layer 1 baseline. Treat it as a composite
tool-like effect from the outer runner.

Two plausible shapes:

- one terminal artifact that records the full RLM REPL result and metadata
- a nested effect graph if child LLM/tool calls need independent recovery

Do not journal RLM REPL by pretending it is a single ordinary model call unless
the composite side effects are accounted for.

### Atomic Event Append Plus Projection Apply

The runner event stream is idempotent, but event append and projection apply are
still separate effects. A crash between them can leave projections stale until a
replay/rebuild path repairs them.

Follow-up work should either:

- add an atomic append-plus-project transaction where the store can support it,
  or
- provide an explicit projection rebuild/reconcile operation with tests proving
  it repairs missed projection updates.

### Provider Idempotency

Model intents now prevent blind replay after a crash, but they do not recover a
provider response if the process crashes after the provider returns and before
`CompleteModelEffect`.

Only add automatic provider retry when the provider adapter has a real
idempotency key or response lookup contract. Otherwise keep the current
fail-closed behavior.

### Tool Replay Policy Audit

The default for incomplete tool intents is fail-closed. Retrying is allowed only
when the tool definition explicitly declares `read_only` or `idempotent`.

Follow-up work:

- audit default v2 tool definitions for correct replay policy
- keep write/process/agent-spawn style tools fail-closed unless they have their
  own idempotency key
- add tests for any new idempotent replay policy before enabling it

## Not Next

Do not add gob checkpoints or stage-level runner resume as the next step. Those
were intentionally avoided because runner stages contain non-idempotent side
effects. Any future checkpoint design must be built on atomic side effects or
the effect journal contracts above.
