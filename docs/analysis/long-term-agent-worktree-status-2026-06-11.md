# Long-Term Agent Worktree Status - 2026-06-11

## Objective

The current worktree is pursuing a structural long-term-agent memory goal:
wire structured tool/session evidence into provenance-bearing candidate memory
claims, keep those claims scoped to tasks/sessions, retrieve them through
existing foxctl context and memory primitives, and update/revalidate them
through explicit feedback.

The design constraint has been to improve general long-term agent capability
first, then use LongMem-style evaluation as validation. The implementation
should avoid benchmark-specific IDs, hard-coded answers, and ad hoc query
special cases.

## Current Worktree Shape

At the time this report was written:

- Branch: `feat/ci-core-skills-split`
- Tracked files changed before committing: 65
- Untracked files before committing: 26
  - These 65/26 figures are a pre-commit working-tree snapshot. The committed
    stack below totals `99 files changed` against `main`; the difference is the
    snapshot timing, not a discrepancy in the work.
- Deleted tracked files before committing:
  - `internal/intelligence/indexing/embedding/worker.go`
  - `internal/storage/memory/search.go`
  - `internal/storage/memory/vector.go`

The diff is broad and should be treated as multiple logical slices, not a
single final merge unit.

## Commit Stack Created

After this report was drafted, the worktree was split into these commits:

- `73b43637` - `Wire long-term memory claim feedback`
- `efa378d5` - `Add memory recall embedding queue plumbing`
- `787e82f6` - `Add RLM longmem evaluation flow`

This report is committed separately so the implementation slices stay
reviewable.

## Implemented Work

### Long-Term Claim And Feedback Wiring

- Candidate memory claims are routed through the existing `contextengine`
  ownership boundary rather than a new memory side store.
- Task/session scope is carried through context retrieval and memory recall.
- ContextWiki/autonomous memory draft application can write deterministic
  candidate claims with provenance.
- Retrieval feedback can record explicit used refs, append typed events, and
  mark related claims for revalidation.
- Dirty edit lifecycle events can mark related current claims for
  revalidation without promoting candidates.
- `foxctl retrieval-feedback` provides an additive operator/automation caller.
- `foxctl rlm run` has opt-in answer feedback; answer-cited refs are kept
  informational unless the caller supplies explicit used refs.

### Memory Recall And Embedding Plumbing

- Named-memory recall is wired into `gather_context` through memory packs and
  typed `named_memory:` refs.
- `load_evidence_ref` can load named-memory bodies.
- Legacy memory search was moved toward the new lexical/BM25 recall package.
- Embedding queue processing moved toward a daemon/processor split.
- The memory query skill and embedding worker skill were updated around the new
  recall/queue paths.

### RLM Evidence Handling

- RLM tool-surfaced evidence refs are tracked as candidate metadata, not
  automatically promoted to `Result.EvidenceRefs`.
- Final answer-used evidence refs are tracked separately from surfaced refs.
- Staged RLM final synthesis is forced to run without tools.
- Evidence ref extraction and citation matching were pulled into focused
  helpers.
- Staged verification can deterministically build an `evidence_ledger` when the
  model skips the required ledger.
- The deterministic ledger repair now uses the trailing `Question:` text rather
  than the full task instruction prompt.
- Final synthesis now treats surfaced refs as diagnostic unless accepted ledger
  evidence exists.

### LongMem Evaluation Harness

- Added `foxctl eval longmem` with ingest, queue status, retrieval, and answer
  modes.
- Added anti-leakage checks so expected answers/evidence are not embedded into
  the workspace memories.
- Increased LongMem source text preservation to keep late facts in long source
  sessions.
- Added answer-mode scoring through an injected answer runner.
- Removed expected evidence IDs from `AnswerRequest`; expected answers and
  evidence stay in the scorer.
- Tightened scoring so refusals and partial reverse-substring answers do not
  count as correct.

## Verification

Focused verification currently passes:

```bash
env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=0 /usr/local/go/bin/go test -count=1 \
  ./internal/rlm \
  ./internal/rlm/env \
  ./internal/tooling/evals/longmemeval \
  -run 'TestBuildPlan|TestLLMRunnerStagedAggregatesSurfacedToolEvidenceRefs|TestEvidenceLedgerQueryFromTaskPromptUsesTrailingQuestion|TestBuildSynthesisPromptBansUnacceptedSurfacedEvidence|TestLLMRunnerStagedRepairsEmptyLedgerWithCanonicalQuery|TestLLMRunnerStagedAutoBuildsEvidenceLedgerWhenVerificationMissesRequiredTool|TestStagedPromptsUseLoadEvidenceRefWhenLegacyAbsent|TestLLMRunnerEvidenceGuidanceMentionsCompositeTools|TestBuildEvidenceLedger|TestEvidenceLedgerIgnoresMetadataOnlyAnswerValues|TestAggregateLoadedEvidenceRefs|TestEvidenceLedgerPayloadTextForQuery|TestRunAnswerModeScoresWithFakeRunner|TestAnswerMatchScoreRejectsQuotedExpectedValueInRefusal|TestBuildPlanKeepsLateSourceTurnsInAtomicText|TestBuildPlanKeepsAnswerAndEvidenceMetadataOutOfSemanticContent|TestBuildPlanAllowsAnswerTextWhenItComesFromSourceTranscript|TestApplyPlanIsIdempotentAndQueuesMemoryJobs'
```

Last run:

```text
ok  	github.com/joshka0/foxctl/internal/rlm
ok  	github.com/joshka0/foxctl/internal/rlm/env
ok  	github.com/joshka0/foxctl/internal/tooling/evals/longmemeval
```

Full repository tests have not been rerun for this report. Earlier broad
command-package runs had unrelated failures, so merge readiness still requires
a separate full quality gate.

## Historical LongMem Smoke Status

The latest live answer-mode artifact directory under `/tmp` was cleaned before
this report was written, so the JSON reports are not currently present. The last
observed live four-case ZAI/GLM run had:

- answer accuracy: `1/4`
- correct case: daily commute, `45 minutes each way`
- coupon case: expected memory surfaced, but the ledger rejected it
- degree case: answer-mode retrieval missed the expected memory
- model-kit case: answer-mode retrieval missed the expected memories
- mean answer latency: roughly 50 seconds per case

This is useful validation, but it is not merge-proof evidence because the
artifact file is gone.

## What Works

- General claim/feedback/revalidation wiring has a coherent architecture path:
  existing contextengine/contextplane/storage seams are used instead of a new
  standalone memory system.
- RLM evidence accounting is stricter: surfaced evidence, accepted ledger
  evidence, and answer-cited evidence are separate concepts.
- LongMem ingest no longer truncates late facts in long sessions.
- The eval API no longer gives expected evidence IDs to the answer runner.
- Focused regression tests cover the key benchmark-integrity fixes.

## What Does Not Work Yet

- LongMem answer-mode is still weak: only one of four smoke cases answered
  correctly in the last live run.
- Coupon remains a ledger inference false negative when the answer depends on
  separated conversational context.
- Degree and model-kit cases are retrieval/query-planning misses.
- Ledger verification prompts are still large, often tens of thousands of
  tokens in live traces.
- The worktree is too broad for a clean review without commit slicing and
  follow-up decomposition.

## Main Risks

- `internal/rlm/env/tool_exec.go` is **10,000 lines** in a single file on the
  RLM environment path. This is the headline structural risk of the worktree,
  not just a style nit. There is no co-located `tool_exec_test.go`; coverage for
  this file is spread across `adapter_test.go`, `tools_test.go`,
  `tool_profiles_test.go`, and others, so the split in "Recommended Next Slice"
  step 1 must also untangle which tests exercise the extracted helpers.
- The ledger still depends on regex/slot heuristics and can false reject unusual
  phrasing.
- Final synthesis guardrails are prompt-level; there is not yet a
  post-generation enforcement pass.
- Some changes are interdependent across CLI, storage, RLM, and daemon paths,
  so bisectability depends on careful commit boundaries.

## Recommended Next Slice

1. Split the large RLM environment evidence/ledger helpers out of
   `tool_exec.go`.
2. Add a direct tool-level reproduction for the coupon ledger rejection.
3. Improve query planning/retrieval for degree and model-kit cases.
4. Reduce evidence ledger prompt size before more live provider runs.
5. Run a full quality gate after the commit stack is clean.
