# Semantic Code Anchors

Status: Active implementation
Owner: Solo maintainer
Last Updated: 2026-05-06

## Goal

Turn high-signal code comments into typed semantic anchors that improve repo
graph search, ACA retrieval, embeddings, review, and agent memory without
turning source files into long-form memory storage.

The core idea is:

```go
// [[invariant:no-send-without-read]]
// [[risk:agent-terminal-desync]]
// [[test:internal/runtime/terminal/tmuxbridge/client_test.go#TestReadBeforeWrite]]
func (b *Bridge) Type(...) error {
	...
}
```

Those comments are not just notes. They are durable graph edges and embedding
anchors. They let agents find code by protocol, safety, product, or runtime
intent, not only by symbol names and raw code tokens.

The target retrieval path is:

```text
source anchor -> repo graph edge -> semantic envelope -> embedding -> ACA retrieval
```

The restraint is as important as the feature: code comments are semantic ports,
not the memory store.

## Existing System Fit

This plan extends the current ACA, repoindex, and embedding stack rather than
creating a parallel `agentgraph` system.

Relevant existing pieces:

- [AgentCTL context architecture](../../architecture/context-architecture.md)
  already separates the ACA control plane from the durable Obsidian knowledge
  plane.
- [Memory](../../general/memory.md) documents named memory persistence,
  workspace scoping, optional embeddings, and `code/semantic_search` memory
  scope.
- [Repo graph index](../../general/repoindex.md) already stores packages,
  files, symbols, concept nodes, structural edges, and comment-derived soft
  edges.
- `internal/intelligence/indexing/repoindex/comment_edges.go` already parses
  structured `Index:` doc comments into concept nodes and soft edges.
- `internal/intelligence/indexing/embeddingtext` already builds symbol embedding
  text from symbol metadata, docs, relationships, aliases, and optional code.
- `internal/intelligence/searchindex` already has typed code retrieval
  documents with anchors, keywords, metadata, and embeddings.
- `internal/storage/obsidianindex` already indexes vault notes, headings, links,
  repo paths, repo symbols, note embeddings, and chunk embeddings.
- `internal/context/memorycore` already has lifecycle, trust, provenance, and
  telemetry envelopes for memory records.
- `internal/context/contextplane` already has typed proposal governance,
  retrieval correction, co-change priors, and maintenance loops.

The existing `Index:` comment syntax is a useful prototype. Semantic anchors
should become the stricter, typed, human-friendly successor while keeping
`Index:` readable during migration.

## Current Branch Status

This file is now both a design contract and an implementation roadmap. The
branch has already moved past the original "recommended first slice"; the
remaining work is next-step implementation work, not parked future work.

| Slice | Current state | Evidence |
|-------|---------------|----------|
| PR-A1a evidence and memory gate | Landed | `internal/intelligence/evidence`; `internal/context/contextplane/memory_instruction_gate.go`; import-guard and memory-gate tests |
| PR-A1 parser and validator | Landed | `internal/intelligence/indexing/semanticanchors`; parser, canonical IDs, authority metadata, render-barrier tests |
| PR-A2 extraction and owner binding | Partial | Go graph binding is implemented; TypeScript/Python/Rust are comment/lint-only until repoindex owner-join fixtures exist |
| PR-A3 CLI lint/explain | Landed with hardening left | `foxctl index anchors lint` and `foxctl index anchors explain`; remaining work is stricter golden coverage for redaction, missing targets, and review wording |
| PR-B1 repoindex vocabulary and traversal normalization | Landed | semantic and empirical edge sets, structural-default normalization, `IncludeSemanticAnchors`, `IncludeOwnerContainers`, `IsAnchorConceptNode`, Obsidian concept filtering |
| PR-B2 graph emission | Landed behind flags | `foxctl index repo build --semantic-anchors`; semantic concept nodes/edges; validated `SemanticAnchorEdgeMeta`; existing `Index:` edges coexist |
| PR-B3 query/traversal proof | Landed with more eval coverage needed | expand and DAG grep can include semantic anchors separately from owner containers; public semantic projections validate edge metadata |
| PR-B4 eval fixture | Partial | CLI E2E fixture proves parser/repoindex/search/expand/`Index:` coexistence; full positive/negative eval suite still needed |
| PR-B.5 empirical git layer | Partial | `--cochange`, `EdgeCoChangesWith`, scorer, and repoindex emission exist; temp-git acceptance coverage still needs to prove giant commits, recency, caps, and freshness |
| PR-C semantic envelope | Partial | `searchindex.CodeEnvelopeProvider` and repoquery semantic-anchor envelope provider exist; remaining work is fuller digest, linked doc/test, and retrieval-quality eval coverage |
| PR-D ACA retrieval blend | Partial | `context retrieve --semantic-anchors`, validated semantic hints, boost path, inspect classifications, and proposal creation exist; eval and ranking gates are not complete |
| PR-E anchor proposals and curator | Partial | `semantic_anchor_patch` proposal kind and review-required proposal plumbing exist; source-diff generation/apply remains review-gated work |
| PR-F Obsidian bridge | Partial | anchor concept nodes are filtered out of concept-note drafts by default; automatic reviewed inbox concept-note generation remains |
| PR-G agent workflow | Partial | touched-file hook advisory and the `semantic-commenting` skill exist; graph diff output, linked-test review checks, and trust-critical anchor review gates remain |

Repo-local anchors are the default authoring style:

```go
// [[invariant:no-send-without-read]]
```

Configured scopes remain valid only when the repo/indexer defines the scope:

```go
// [[acme:protocol/read-guard]]
```

Path anchors stay unscoped:

```go
// [[doc:docs/terminal-safety.md#Terminal Safety]]
// [[test:internal/terminal/guard_test.go#TestGuardReadBeforeWrite]]
```

## Four-Plane Model

Keep four evidence classes separate. They can reinforce each other, but they do
not carry the same authority.

| Plane | Source | Meaning | Authority |
|------|--------|---------|-----------|
| Structural graph | AST/import/call/symbol extraction | What the code does structurally | High factual authority |
| Semantic anchors | Tiny source comments | What humans or agents intentionally connect | Evidence-level until reviewed |
| Git empirical layer | Co-change, freshness, volatility | What tends to move together or changed recently | Context signal, not truth |
| Durable knowledge | ACA proposals, memorycore, docs, Obsidian | Reviewed explanations and policies | Depends on lifecycle and trust |

Rules:

- Semantic anchors express intent: "this symbol is intended to enforce this
  invariant."
- Co-change expresses history: "these files have tended to move together."
- Freshness expresses recency: "this edge, test, or doc was recently extracted,
  touched, or verified."
- Durable knowledge expresses reviewed reasoning, doctrine, and policy.
- None of the first three silently become behavioral doctrine.
- A system may combine these signals for retrieval ranking, but any UI,
  proposal, agent context, or explain output must preserve the source plane of
  each signal.

Carry this distinction as structured metadata wherever possible:

```go
type EvidencePlane string

const (
	EvidencePlaneStructural     EvidencePlane = "structural_graph"
	EvidencePlaneSemanticAnchor EvidencePlane = "semantic_anchor"
	EvidencePlaneGitEmpirical   EvidencePlane = "git_empirical"
	EvidencePlaneDurable        EvidencePlane = "durable_knowledge"
)

type EvidenceAuthority string

const (
	EvidenceAuthorityFact          EvidenceAuthority = "fact"
	EvidenceAuthorityEvidenceOnly  EvidenceAuthority = "evidence_only"
	EvidenceAuthorityContextSignal EvidenceAuthority = "context_signal"
	EvidenceAuthorityReviewed      EvidenceAuthority = "reviewed"
	EvidenceAuthorityPolicy        EvidenceAuthority = "policy"
)

type EvidenceSource string

const (
	EvidenceSourceStructuralGraph EvidenceSource = "structural_graph"
	EvidenceSourceSemanticAnchor  EvidenceSource = "semantic_anchor"
	EvidenceSourceGitCoChange     EvidenceSource = "git_cochange"
	EvidenceSourceDurableMemory   EvidenceSource = "durable_memory"
)

type EvidenceClass string

const (
	EvidenceClassCodeFact       EvidenceClass = "code_fact"
	EvidenceClassSourceComment  EvidenceClass = "source_comment"
	EvidenceClassContextSignal  EvidenceClass = "context_signal"
	EvidenceClassReviewedMemory EvidenceClass = "reviewed_memory"
)

type AuthorityEffect string

const (
	AuthorityEffectNone              AuthorityEffect = "none"
	AuthorityEffectRetrievalRanking  AuthorityEffect = "retrieval_ranking"
	AuthorityEffectReviewSignal      AuthorityEffect = "review_signal"
	AuthorityEffectReviewWarning     AuthorityEffect = "review_warning"
	AuthorityEffectInstructionSource AuthorityEffect = "instruction_source"
)

type RenderSurface string

const (
	RenderSurfaceEvidencePack      RenderSurface = "evidence_pack"
	RenderSurfaceReview            RenderSurface = "review"
	RenderSurfaceReviewWarning     RenderSurface = "review_warning"
	RenderSurfaceEvidenceHint      RenderSurface = "evidence_hint"
	RenderSurfaceInstruction       RenderSurface = "instruction"
	RenderSurfacePolicy            RenderSurface = "policy"
	RenderSurfaceHardConstraint    RenderSurface = "hard_constraint"
	RenderSurfaceToolAuthorization RenderSurface = "tool_authorization"
	RenderSurfaceRuntimeGuardrail  RenderSurface = "runtime_guardrail"
)
```

Package ownership:

```text
internal/intelligence/evidence
```

This package owns `EvidencePlane`, `EvidenceAuthority`, `EvidenceSource`,
`EvidenceClass`, `AuthorityEffect`, `RenderSurface`,
`EvidenceMeta`, `ValidateAllowedAuthorityEffects`, `ValidateEvidenceMeta`, and
`ValidateRenderSurface`. It must not import repoindex, searchindex,
semanticanchors, contextplane, memorycore, storage, Obsidian packages, or
`internal/v2`. `semanticanchors`, repoindex explain, and later ACA/context
bundle assembly should all import this contract instead of re-declaring local
copies.

`semanticanchors` owns anchor-specific details only: `AnchorType`,
`AnchorTargetID`, `RepoScopedAnchorKey`, `AnchorResolution`,
`AnchorEdgeAction`, `AnchorValidationStatus`, `Finding`, and
`SemanticAnchorRelation`. Do not move anchor grammar or target-resolution
details into the shared evidence package. The `semanticanchors` core package
must not import repoindex, searchindex, contextplane, memorycore, storage,
Obsidian tooling, or `internal/v2`; it may import `internal/intelligence/evidence`.

These fields are mandatory on semantic/empirical edges and retrieval
explanations. Retrieval score may blend signals. Instruction eligibility may
not. Instruction eligibility must come only from explicit durable memory or
policy usage metadata.

Authority effects are use-site permissions, not proof that an edge is active in
that use site. A stored edge carries the effects it may be used for, then
context assembly validates the requested render surface.

Allowed authority effects must be validated, not trusted by convention:

```go
func ValidateAllowedAuthorityEffects(plane EvidencePlane, authority EvidenceAuthority, effects []AuthorityEffect) error

func ValidateEvidenceMeta(meta EvidenceMeta) error

func ValidateRenderSurface(meta EvidenceMeta, surface RenderSurface) error
```

Shared metadata shape:

```go
type EvidenceMeta struct {
	Source                  EvidenceSource
	SourcePlane             EvidencePlane
	EvidenceClass           EvidenceClass
	EvidenceAuthority       EvidenceAuthority
	AllowedAuthorityEffects []AuthorityEffect
}
```

PR-A1a should include an import-guard test that proves
`internal/intelligence/evidence` has no imports from repoindex, searchindex,
semanticanchors, contextplane, memorycore, storage, Obsidian, or `internal/v2`.

`ValidateRenderSurface` is dependency-pure. It validates only evidence plane,
evidence authority, and allowed-effect legality. It must not import memorycore
or inspect memorycore lifecycle, review, or usage state. Instruction-sensitive
context assembly must add a second validation step for durable records:

```go
// evidence package: dependency-pure
func ValidateRenderSurface(meta EvidenceMeta, surface RenderSurface) error

// internal/context/contextplane: may import memorycore and evidence
func ValidateMemoryRecordForInstruction(record memorycore.Record, surface evidence.RenderSurface, now time.Time) error
```

That memory-aware validator must construct durable-memory evidence metadata
internally from the record before rendering validation. It must not accept
caller-provided evidence metadata unless that metadata is cross-checked against
the same construction:

```go
func instructionEvidenceMetaForMemoryRecord(record memorycore.Record) evidence.EvidenceMeta
```

Construction rules:

```text
Source = durable_memory
SourcePlane = durable_knowledge
EvidenceClass = reviewed_memory
EvidenceAuthority = policy for memorycore.KindPolicyRule
EvidenceAuthority = reviewed for memorycore.KindProceduralSkill
AllowedAuthorityEffects = instruction_source
```

`ValidateMemoryRecordForInstruction` should first call
`evidence.ValidateRenderSurface(instructionEvidenceMetaForMemoryRecord(record), surface)`,
then apply active lifecycle, reviewed or validated status,
`Usage.InstructionEligible = true`, `Usage.EvidenceOnly = false`, and
instruction-capable record kind checks. The only instruction-capable kinds are
`memorycore.KindPolicyRule` and `memorycore.KindProceduralSkill`. It must also
reject superseded, tainted, failed-validation, expired `ValidUntil`, and expired
`TTLSeconds` records. Use the injected `now`; do not call `time.Now()` inside
the validator. The validator must reject a zero `now`.
`durable_knowledge` plus `instruction_source` is not sufficient by itself. The
target package for the memory gate is `internal/context/contextplane`, not
`internal/intelligence/evidence`.

PR-A1a should include this memory-aware validator as a companion contract test
surface, even though it does not add ACA retrieval behavior. Tests must prove
`ValidateRenderSurface` can only establish generic evidence/render legality and
is insufficient for durable instruction rendering without
`ValidateMemoryRecordForInstruction`.

Allowed effects:

| Plane | Allowed authority effect |
|------|---------------------------|
| `structural_graph` | `none`, `retrieval_ranking`, `review_signal` |
| `semantic_anchor` | `retrieval_ranking`, `review_signal` |
| `git_empirical` | `retrieval_ranking`; `review_warning` only for warning projections |
| `durable_knowledge` | `none`; `retrieval_ranking` when authority is `context_signal`, `reviewed`, or `policy`; `review_signal` when authority is `reviewed` or `policy`; `instruction_source` only when authority is `reviewed` or `policy`, then backed by the memory instruction gate |

`evidence.ValidateAllowedAuthorityEffects` may allow
`durable_knowledge + instruction_source` only as a generic maximum-use
declaration, and only when `EvidenceAuthority` is `reviewed` or `policy`.
Instruction rendering still requires `contextplane.ValidateMemoryRecordForInstruction`.
Non-instruction durable knowledge may use `retrieval_ranking` or
`review_signal` only through the authority combinations in the table above.
`durable_knowledge + AuthorityEffectNone` is exclusive, non-actionable, and
renders only in evidence packs as `permitted_use: none`.

Exact validator semantics:

```text
ValidateAllowedAuthorityEffects:
  - rejects empty effect lists
  - rejects unknown enum strings for plane, authority, or effect
  - rejects duplicate effects
  - treats AuthorityEffectNone as exclusive; if present it must be the only effect
  - rejects instruction_source unless plane=durable_knowledge and
    authority is reviewed or policy
  - rejects semantic_anchor, git_empirical, and structural_graph with
    instruction_source
  - rejects git_empirical with review_signal; git empirical warnings use
    review_warning only
  - allows durable_knowledge + retrieval_ranking only when authority is
    context_signal, reviewed, or policy
  - allows durable_knowledge + review_signal only when authority is reviewed or
    policy
  - allows durable_knowledge + none as an exclusive non-actionable effect

ValidateEvidenceMeta:
  - validates Source, SourcePlane, EvidenceClass, and EvidenceAuthority enum strings
  - calls ValidateAllowedAuthorityEffects for AllowedAuthorityEffects
  - is the shared entry point for metadata constructors and decoders
  - rejects incoherent source/plane/class/authority tuples
  - prevents a semantic-anchor or git source from validating as durable policy
    merely by setting SourcePlane to durable_knowledge

ValidateRenderSurface:
  - first validates the whole EvidenceMeta, including allowed effects
  - requires a legal effect for the requested surface
  - never treats AllowedAuthorityEffects as active authority by itself
```

Required `ValidateEvidenceMeta` tuple coherence:

```text
Source = semantic_anchor:
  SourcePlane = semantic_anchor
  EvidenceClass = source_comment
  EvidenceAuthority = evidence_only
  AllowedAuthorityEffects = exactly retrieval_ranking and review_signal

Source = git_cochange:
  SourcePlane = git_empirical
  EvidenceClass = context_signal
  EvidenceAuthority = context_signal
  AllowedAuthorityEffects = retrieval_ranking and/or review_warning only

Source = structural_graph:
  SourcePlane = structural_graph
  EvidenceClass = code_fact
  EvidenceAuthority = fact
  AllowedAuthorityEffects = none, retrieval_ranking, and/or review_signal

Source = durable_memory:
  SourcePlane = durable_knowledge
  EvidenceClass = reviewed_memory
  EvidenceAuthority = context_signal, reviewed, or policy
  AllowedAuthorityEffects follow the durable_knowledge authority table above
```

Any tuple outside those source-specific shapes must fail with an
`incoherent_evidence_tuple` validation reason. Source-specific constructors may
wrap this, but render/projection/context assembly must still call
`ValidateEvidenceMeta` or a wrapper that enforces the same tuple coherence.

Validation errors should expose stable reason codes for tests and projections:

```text
unknown_plane
unknown_authority
unknown_source
unknown_class
unknown_effect
empty_effects
duplicate_effect
none_not_exclusive
illegal_effect_for_plane
illegal_effect_for_authority
illegal_render_surface
missing_required_effect
incoherent_evidence_tuple
```

Use a typed validation error so tests and projection code can inspect the
reason without string matching:

```go
type ValidationReason string

type ValidationError struct {
	Reason ValidationReason
}
```

Memory instruction-gate errors should expose the same kind of stable reason
surface:

```go
type MemoryInstructionGateReason string
```

Required memory gate reasons:

```text
invalid_instruction_surface
zero_now
inactive_lifecycle
invalid_review_status
invalid_instruction_kind
instruction_not_eligible
evidence_only
superseded
tainted
failed_validation
valid_until_unparsable
valid_until_expired
ttl_base_missing
ttl_base_unparsable
ttl_expired
```

Memory expiry rules:

```text
Time parsing:
  - parse ValidUntil, ValidFrom, ObservedAt, and IngestedAt as RFC3339 or
    RFC3339Nano only
  - reject zero now

ValidUntil:
  - if present and unparsable: reject
  - reject when !now.Before(validUntil)

TTLSeconds:
  - if > 0, compute from ValidFrom, else ObservedAt, else IngestedAt
  - if all TTL base times are missing or unparsable: reject
  - if now >= base + TTLSeconds: reject
```

Memory instruction gate rules are intentionally stricter than generic curator
expiry helpers:

```text
ValidateMemoryRecordForInstruction:
  - reject surfaces other than instruction, policy, hard_constraint,
    tool_authorization, and runtime_guardrail
  - reject zero now
  - require Lifecycle.State == memorycore.LifecycleStateActive
  - require ReviewStatusReviewed or ReviewStatusValidated only
  - require KindPolicyRule or KindProceduralSkill only
  - require Usage.InstructionEligible == true
  - require Usage.EvidenceOnly == false
  - reject Lifecycle.SupersededBy != ""
  - do not reject solely because the record itself Supersedes another record
  - reject Trust.Tainted == true
  - reject ReviewStatusFailedValidation
  - reject present-but-unparsable ValidUntil
  - reject ValidUntil when !now.Before(validUntil)
  - reject TTLSeconds > 0 when the TTL base time is missing or unparsable
  - reject expired TTLSeconds
```

`ValidateMemoryRecordForInstruction` is not a general durable-memory render
validator. Evidence-pack, evidence-hint, review, and review-warning surfaces
must use `ValidateRenderSurface` plus their projection-specific validation
instead.

Forbidden combinations:

```text
semantic_anchor + instruction_source
git_empirical + instruction_source
structural_graph + instruction_source
```

`RenderSurfaceEvidenceHint` is evidence-labeled context only. It must never be
used for system/developer prompt doctrine, hard constraints, tool policy, tool
authorization, runtime guardrails, or imperative instruction text.
It rejects metadata containing `instruction_source` or `none`, even when another
evidence-hint-compatible effect such as `retrieval_ranking` is also present.

Render-surface truth table:

| Surface | Allowed evidence |
|------|------------------|
| `evidence_pack` | structural, semantic anchor, git empirical, and durable knowledge when `ValidateAllowedAuthorityEffects` passes; `instruction_source` is displayed only as durable metadata, not imperative text |
| `review` | structural and semantic anchor evidence with `review_signal`; durable knowledge with `review_signal` only when authority is reviewed or policy; not git empirical |
| `review_warning` | git empirical evidence with `review_warning` |
| `evidence_hint` | evidence-labeled context only; requires `retrieval_ranking`, `review_signal`, or `review_warning`; rejects `instruction_source` and `none`; never imperative instruction text |
| `instruction` | generic durable `instruction_source` only, then must pass `ValidateMemoryRecordForInstruction` |
| `policy` | generic durable `instruction_source` only, then must pass `ValidateMemoryRecordForInstruction` |
| `hard_constraint` | generic durable `instruction_source` only, then must pass `ValidateMemoryRecordForInstruction` |
| `tool_authorization` | generic durable `instruction_source` only, then must pass `ValidateMemoryRecordForInstruction` |
| `runtime_guardrail` | generic durable `instruction_source` only, then must pass `ValidateMemoryRecordForInstruction` |

`AuthorityEffectNone` never authorizes review, evidence-hint, instruction,
policy, hard-constraint, tool-authorization, or runtime-guardrail rendering.
It may appear in an evidence pack only as labeled non-actionable evidence with
`permitted_use: none`.

Enforcement call sites:

1. semantic anchor edge construction
2. edge/meta deserialization and explain rendering
3. retrieval/context bundle assembly
4. any future memorycore target-promotion path

Semantic anchor edges must be constructed with internally assigned
evidence-source, evidence-class, evidence-authority, and
allowed-authority-effect fields. Do not trust caller-provided authority
metadata.

Context assembly must call `ValidateRenderSurface` before rendering evidence
into any prompt, context bundle, review packet, or policy-adjacent surface.
`semantic_anchor`, `git_empirical`, and `structural_graph` evidence must fail
validation for instruction, policy, hard-constraint, tool-authorization, and
runtime-guardrail surfaces unless separately promoted through durable
memorycore state that is active, reviewed, validated, and
instruction-eligible.

PR-D may not render semantic anchor evidence into ACA/context bundles until
context assembly calls `evidence.ValidateRenderSurface` for each evidence node
and rejects instruction-sensitive surfaces for source anchors.

## Non-Goals

- Do not make comments the memory store.
- Do not let agents add long explanations, speculation, or analysis graffiti to
  source files.
- Do not replace tests, docs, ADRs, or Obsidian notes with inline anchors.
- Do not make embeddings mandatory for correctness. Graph and lexical behavior
  must still work.
- Do not add a new top-level storage or retrieval stack before extending
  repoindex/searchindex.
- Do not use ad hoc keyword heuristics to assign authority, promote anchors, or
  classify lifecycle.
- Do not encode co-change in source comments.
- Do not automatically promote co-change into semantic anchors.
- Do not put anchor extraction, graph indexing, or retrieval code under
  `internal/v2`.

## Hard Invariants

Reviewers should reject implementation patches that violate these invariants:

1. Inline source comments contain stable anchors only, not long-form memory.
2. Source anchors are evidence, not instructions.
3. Anchor authority is evidence-level until backed by docs, tests, reviewed
   memory, or active policy.
4. Semantic anchors are typed and parseable. Freeform "important" comments do
   not enter the graph.
5. Anchor extraction is deterministic and testable.
6. Anchor edges never override structural code facts from AST/repoindex.
7. Existing `Index:` comments remain readable during migration.
8. Embedding text is generated from a deterministic semantic envelope, not from
   prompt-time freeform synthesis.
9. Anchor proposals are reviewed through ACA/memorycore lifecycle before agents
   edit source comments automatically.
10. Anchor IDs must not include secrets, tokens, user PII, terminal output,
    URLs, absolute paths, path traversal, or transient session IDs.
11. Co-change and freshness are metadata and ranking signals, not semantic
    authority.
12. Package placement follows the intelligence/context/storage families, not
    `internal/v2`.

## Terminology

### Semantic Anchor

A small typed link embedded near code:

```ts
// [[invariant:mutuality-over-unilateral-access]]
// [[risk:premature-unilateral-access]]
```

It identifies a stable concept and lets the index create graph edges.

### Anchor Target

The node the anchor names, such as:

- `foxctl:invariant/no-send-without-read`
- `foxctl:risk/agent-terminal-desync`
- `test:internal/runtime/terminal/tmuxbridge/client_test.go#TestReadBeforeWrite`
- `doc:docs/general/tmux-collaboration.md#room-access`

### Anchor Occurrence

One physical occurrence of an anchor in a file at a line/span, bound to the
nearest symbol or to the file if no symbol binding is available.

### Anchor Target Authority

Lifecycle and trust associated with the target concept itself. This is distinct
from an occurrence. An occurrence says "this code links to target X." Target
authority says "target X is reviewed, validated, active, stale, deprecated, or
policy-bearing."

### Semantic Envelope

A generated embedding document for one code node. It combines symbol metadata,
doc comments, semantic anchors, linked docs/tests, graph neighbors, optional
capped co-change neighbors, and a capped code excerpt.

### Anchor Proposal

A reviewed ACA proposal to add, remove, or change anchors. Agents may propose
anchor changes, but source edits should stay visible in normal diffs.

### Curator

The lifecycle process that detects stale, vague, duplicated, orphaned, unused,
or under-supported anchors and proposes cleanup.

## Anchor Syntax

PR-A should support only inline wikilink anchors plus existing `Index:`
compatibility. Defer the block relation form until parser, graph, and lint
behavior are proven.

### Inline Wikilink Form

This is the canonical daily-use form:

```go
// [[foxctl:invariant/no-send-without-read]]
// [[foxctl:protocol/read-guard]]
// [[foxctl:risk/agent-terminal-desync]]
// [[test:internal/runtime/terminal/tmuxbridge/client_test.go#TestReadBeforeWrite]]
// [[doc:docs/general/tmux-collaboration.md#room-access]]
```

Supported grammar:

```text
[[scope:type/slug]]
[[type:slug]]
[[doc:path/to/doc.md#heading]]
[[test:path/to/file_test.go#TestName]]
```

Grammar rules:

```text
scope: optional, lowercase, [a-z0-9][a-z0-9_-]*
scope: PR-A allowlist only: foxctl, repo slug, or configured allowed scopes
type: required allowlisted type
slug: lowercase stable token, [a-z0-9][a-z0-9._/-]*
doc/test path: repo-relative only
doc/test path: no absolute paths, no path traversal, no env vars, no URLs
doc/test path: no backslashes and no control characters
```

Disambiguation rule:

```text
If the left-hand side before ":" is an allowlisted anchor type:
  parse as unscoped anchor: [[type:target]]
Else:
  parse as scoped anchor: [[scope:type/target]]
```

Examples:

```text
[[test:internal/foo_test.go#TestX]]
  -> type=test, target=internal/foo_test.go#TestX

[[foxctl:invariant/no-send-without-read]]
  -> scope=foxctl, type=invariant, target=no-send-without-read
```

Do not apply the generic lowercase slug rule to `doc:` and `test:` targets.
Those targets are repo-relative paths plus optional fragments and must preserve
case:

```text
[[test:internal/runtime/terminal/tmuxbridge/client_test.go#TestReadBeforeWrite]]
```

`doc:` and `test:` are unscoped-only in PR-A because they use path-special
validation. Reject scoped path anchors such as:

```text
[[foxctl:doc/docs/foo.md]]
[[foxctl:test/internal/foo_test.go#TestX]]
```

Unknown scopes are lint findings, not silently accepted graph concepts:

```text
[[randomscope:invariant/no-send-without-read]]
```

Approved PR-A forms:

```text
[[invariant:no-send-without-read]]
[[risk:agent-terminal-desync]]
[[foxctl:invariant/no-send-without-read]]
[[foxctl:protocol/read-guard]]
[[doc:docs/general/tmux-collaboration.md#room-access]]
[[test:internal/runtime/terminal/tmuxbridge/client_test.go#TestReadBeforeWrite]]
```

Rejected PR-A forms:

```text
[[foxctl:doc/docs/foo.md]]
[[foxctl:test/internal/foo_test.go#TestX]]
[[foo:bar]]
[[policy:...]]
[[http://example.com]]
[[doc:/absolute/path.md]]
[[doc:../../secret.md]]
```

Target validation:

- `doc:` and `test:` targets resolve through safe repo-relative path
  resolution.
- Concept slugs are validated separately from path anchors and must reject
  secret-like values, PII-looking values, traversal markers, URLs, terminal
  output, absolute paths, env var expansions, and transient session IDs.
- `doc:` fragments use deterministic heading-anchor matching.
- `test:` fragments require file existence and, at minimum, fragment/symbol
  presence in the target file.
- Missing or unresolved doc/test fragments are lint findings, not index build
  failures.
- Syntactically valid doc/test anchors with missing targets may be indexed as
  evidence-only stale-reference signals with
  `validation_status=missing_target`, but explain/review output must not
  describe them as valid documentation or verification.
- Missing `doc:` or `test:` targets must not emit active `DESCRIBED_BY` or
  `VERIFIED_BY` edges. If the target resolves, emit the typed relation.
  If the target is missing, emit exactly one neutral stale-reference edge:
  `DECLARES_ANCHOR_TARGET` with `validation_status=missing_target`.
- Missing `doc:` or `test:` targets and unresolved fragments/symbols must set
  `AnchorResolution.Relation = DECLARES_ANCHOR_TARGET`. If UX needs to show
  the originally intended relation, store it separately as advisory metadata;
  do not let edge/meta validation infer it from raw syntax.
- Malformed, unsafe, secret-like, path-traversal, URL, env-var, or scoped
  doc/test anchors produce lint findings only and must not emit repoindex
  edges, including neutral stale-reference edges.
- Explain output for unresolved path anchors must say "declares missing target,"
  not "described by" or "verified by."
- Lint and explain output must redact raw target text for secret-like,
  PII-like, token-like, terminal-output-like, and unsafe anchors. Use stable
  finding IDs, occurrence IDs, and redacted summaries instead of printing the
  unsafe target. Do not expose direct hashes of raw secret-bearing syntax or raw
  target text. If correlation needs the raw value, use an internal keyed or
  peppered hash that is not shown in CLI/explain output; public hashes for
  unsafe anchors must be derived from redacted text plus stable non-secret
  context. Raw syntax may be stored only transiently for validation and internal
  correlation.

Canonical public redaction:

```text
DisplaySyntax: [[redacted:<reason>]]
TargetDisplay: [redacted:<reason>]
safe_canonical_target_id: anchor:redacted:<reason>:<stable-nonsecret-context>
public SourceHash input:
  repo_key + owner_stable_key + path + reason + DisplaySyntax
```

`stable-nonsecret-context` may include repo key, owner stable key, source path,
anchor type, and finding reason. It must not include raw unsafe target bytes,
raw unsafe syntax bytes, direct hashes of those bytes, token substrings, PII, or
terminal output. `Finding.Message` must be generated from finding reason enums
and safe display fields only; it must not interpolate `RawSyntax` or
`RawTarget`.

Anchor findings should use deterministic reasons, not prose-only rejection. The
reason enum covers both target-safety failures and non-safety lint findings so
lint, explain, and edge metadata do not invent local prose strings.

```go
type AnchorFindingReason string

const (
	AnchorFindingUnsafeURL             AnchorFindingReason = "unsafe_url"
	AnchorFindingAbsolutePath          AnchorFindingReason = "absolute_path"
	AnchorFindingPathTraversal         AnchorFindingReason = "path_traversal"
	AnchorFindingBackslashPath         AnchorFindingReason = "backslash_path"
	AnchorFindingControlChar           AnchorFindingReason = "control_char"
	AnchorFindingEnvVarExpansion       AnchorFindingReason = "env_var_expansion"
	AnchorFindingSecretLike            AnchorFindingReason = "secret_like"
	AnchorFindingSessionLike           AnchorFindingReason = "session_like"
	AnchorFindingTooLong               AnchorFindingReason = "too_long"
	AnchorFindingPIILike               AnchorFindingReason = "pii_like"
	AnchorFindingNamespaceCollision    AnchorFindingReason = "namespace_collision"
	AnchorFindingScopedPathAnchor      AnchorFindingReason = "scoped_path_anchor"
	AnchorFindingUnknownScope          AnchorFindingReason = "unknown_scope"
	AnchorFindingUnknownType           AnchorFindingReason = "unknown_type"
	AnchorFindingMalformedTarget       AnchorFindingReason = "malformed_target"
	AnchorFindingMissingTarget         AnchorFindingReason = "missing_target"
	AnchorFindingUnresolvedFragment    AnchorFindingReason = "unresolved_fragment"
	AnchorFindingDuplicateOwnerTarget  AnchorFindingReason = "duplicate_owner_target"
	AnchorFindingUnboundOwner          AnchorFindingReason = "unbound_owner"
	AnchorFindingUnsupportedOwner      AnchorFindingReason = "unsupported_owner"
	AnchorFindingTooManyAnchors        AnchorFindingReason = "too_many_anchors"
	AnchorFindingTooManyBeacons        AnchorFindingReason = "too_many_beacons"
	AnchorFindingBeaconWithoutSupport  AnchorFindingReason = "beacon_without_support"
	AnchorFindingGeneratedOrVendor     AnchorFindingReason = "generated_or_vendor"
	AnchorFindingLongFormAdjacentNote  AnchorFindingReason = "long_form_adjacent_note"
)
```

If target validation wants a narrower subcategory, it may treat the URL/path,
secret, session, PII, namespace, unknown, and malformed reasons as safety
reasons. The stored `Finding.Reason` should still use the broader
`AnchorFindingReason`.

Keep PR-A `pii_like` detection narrow and deterministic. Block obvious emails,
phone-like strings, token-like strings, and secret prefixes. Do not attempt
general PII detection in the parser.

Reject namespace collisions and ambiguous separators deterministically:

- target strings containing `::`
- empty namespace segments around `:`
- ambiguous `[[foo:bar]]` inputs where `foo` is neither an allowlisted type nor
  an allowed scope
- URL-looking values
- Windows drive prefixes such as `C:\`
- colons in repo-relative `doc:` and `test:` paths except the leading
  `doc:`/`test:` syntax separator and optional fragment separator `#`

Ambiguous scope/type findings:

```text
[[foo:bar]]
  -> AnchorFindingUnknownScope when `foo` is not an allowlisted type

[[knownscope:unknown/target]]
  -> AnchorFindingUnknownType
```

Add golden tests for both cases so stable finding IDs do not drift.

Finding reason precedence:

1. malformed wrapper or empty body
2. URL-looking value
3. absolute path or Windows drive
4. path traversal
5. control character
6. env-var expansion
7. scoped `doc` or scoped `test`
8. unknown type
9. unknown scope
10. namespace collision / `::`
11. secret-like / token-like
12. session-like / terminal-output-like
13. concept slug malformed
14. doc/test target missing

Missing target is a stale-reference finding, not a safety failure:

```go
ValidationStatus: AnchorValidationMissingTarget
Finding.Reason:  AnchorFindingMissingTarget
EdgeAction:      AnchorEdgeMissingTarget
```

Anchor identity layers:

```text
[[invariant:no-send-without-read]]
  -> AnchorTargetID: anchor:repo:invariant:no-send-without-read

[[foxctl:invariant/no-send-without-read]]
  -> AnchorTargetID: anchor:foxctl:invariant:no-send-without-read

[[doc:docs/general/tmux-collaboration.md#room-access]]
  -> AnchorTargetID: anchor:repo:doc:docs/general/tmux-collaboration.md#room-access

[[test:internal/runtime/terminal/tmuxbridge/client_test.go#TestReadBeforeWrite]]
  -> AnchorTargetID: anchor:repo:test:internal/runtime/terminal/tmuxbridge/client_test.go#TestReadBeforeWrite
```

`repo` is a reserved target-scope token meaning "repo-local anchor target."
`repoKey` is stored outside the target string in the repo-scoped key and in the
repoindex namespace. Do not embed the concrete repo key inside repo-local target
IDs.

```go
type AnchorTargetID string

type RepoScopedAnchorKey struct {
	RepoKey  string
	TargetID AnchorTargetID
}

type RepoIndexAnchorNodeID string

func NewAnchorTargetID(scope, typ, target string) (AnchorTargetID, error)
func RepoScopedAnchorKeyFor(repoKey string, target AnchorTargetID) RepoScopedAnchorKey
func AnchorTargetNodeID(repoKey string, target AnchorTargetID) (RepoIndexAnchorNodeID, error)
func DecodeAnchorTargetNodeID(id string) (repoKey string, target AnchorTargetID, ok bool)
```

ID table:

```text
Input:
  [[invariant:no-send-without-read]]

AnchorTargetID:
  anchor:repo:invariant:no-send-without-read

RepoScopedAnchorKey:
  repo_key=foxctl
  target_id=anchor:repo:invariant:no-send-without-read

RepoIndexAnchorNodeID:
  foxctl::anchor:repo:invariant:no-send-without-read

Input:
  [[foxctl:invariant/no-send-without-read]]

AnchorTargetID:
  anchor:foxctl:invariant:no-send-without-read

RepoScopedAnchorKey:
  repo_key=foxctl
  target_id=anchor:foxctl:invariant:no-send-without-read

RepoIndexAnchorNodeID:
  foxctl::anchor:foxctl:invariant:no-send-without-read
```

Repoindex IDs are derived once:

```text
RepoIndexAnchorNodeID = AnchorTargetNodeID(repoKey, targetID)

Edge.Meta.TargetID stores AnchorTargetID.
Edge.Meta.RepoKey stores repoKey.
Edge.Dst stores RepoIndexAnchorNodeID.
```

Do not apply `repoindex.NamespacedID` twice and do not create concrete
repo-key-bearing target strings such as:

```text
foxctl::foxctl::anchor:repo:doc:docs/...
```

`AnchorTargetNodeID` must reject empty repo keys, repo keys containing `::`,
and target IDs containing `::`. `DecodeAnchorTargetNodeID` must reject double
namespacing such as `foxctl::foxctl::anchor:repo:doc:docs/foo.md`.
`NewAnchorTargetID` must reject `::` anywhere in parsed target input.

Because `semanticanchors` must not import repoindex, `AnchorTargetNodeID`
should implement the same delimiter rule directly instead of calling
`repoindex.NamespacedID` from the semantic-anchor core package. Cross-package
tests should prove anchor node IDs round-trip with `repoindex.SplitNamespacedID`
once PR-B introduces repoindex graph emission.

Binding rules:

- Anchors bind to the nearest following symbol when possible.
- If no symbol binding is possible, anchors bind to the file only when the
  anchor type is valid at file scope.
- Multiple anchors may appear above one symbol.
- Duplicates on the same owner are lint findings.
- Anchors inside generated/vendor files are lint findings by default.
- Max anchors per owner is 6. Warn above 4.
- Max beacon anchors per owner is 1.

Suggested PR-A file-scope types:

- `doc`
- `domain`
- `protocol`
- `decision`
- `beacon`, only when supported by another valid file-scope anchor

Suggested PR-A symbol-required types:

- `invariant`
- `risk`
- `test`
- `test-contract`

### Parsing Scope

PR-A should parse semantic anchors only from source-code comments, not from
arbitrary source text or string literals.

Parser policy:

```go
type AnchorPolicy struct {
	RepoKey               string
	AllowedScopes         map[string]bool
	AllowedTypes          map[AnchorType]AnchorTypePolicy
	MaxAnchorBytes        int
	MaxTargetBytes        int
	WarnAnchorsPerOwner   int
	MaxAnchorsPerOwner    int
	MaxBeaconsPerOwner    int
	MaxBlankLinesToOwner  int
	MaxOwnerLookaheadLines int
}

type AnchorTypePolicy struct {
	FileScopeAllowed   bool
	SymbolScopeAllowed bool
	Indexable          bool
	RequiresSupport    bool
}
```

Default constants:

```go
const (
	MaxAnchorBytes        = 256
	MaxTargetBytes        = 192
	MaxAnchorsPerOwner    = 6
	WarnAnchorsPerOwner   = 4
	MaxBeaconsPerOwner    = 1
	MaxBlankLinesToOwner  = 1
	MaxOwnerLookaheadLines = 3
)
```

Default policy constructor:

```go
func DefaultAnchorPolicy(repoKey string, configuredScopes []string) AnchorPolicy
```

Default scopes:

- `foxctl`
- repo-key-derived project slug
- explicitly configured scopes

Repo-key-derived project slug algorithm:

```text
1. lowercase repoKey
2. replace every run of characters outside [a-z0-9_-] with "-"
3. collapse repeated "-"
4. trim leading/trailing "-"
5. reject empty output
```

The derived slug must be deterministic and tested because unknown-scope
findings and anchor IDs depend on it.

Default indexable PR-A types:

- `invariant`
- `risk`
- `test`
- `test-contract`
- `doc`
- `protocol`
- `domain`
- `decision`

Set `beacon.Indexable = false` for narrow PR-A/PR-B.

Do not scan arbitrary Markdown prose for semantic anchors in PR-A. Obsidian and
Markdown already use `[[wikilinks]]`, and broad Markdown scanning would create
false anchors. PR-A may validate `doc:` targets and can later support explicit
frontmatter fields, but normal Markdown prose should not become source anchor
input.

Owner binding must be deterministic:

```text
comment anchor group binds to the next symbol only if:
1. the anchor group ends before the symbol starts
2. no non-comment code exists between the group and symbol
3. at most one blank line separates the group from the symbol
4. the symbol starts within a small line window, initially 3 lines
5. the anchor type is valid for symbol scope

top-of-file anchor before package/import/code -> file owner, only for file-scope types
mid-file anchor without a following owner -> unbound lint finding
anchor below symbol -> does not bind backward unless explicitly supported later
```

Language prelude lines attached to the following declaration do not break
binding:

- Python decorators
- TypeScript decorators
- Rust attributes
- Go build tags and package-doc prelude comments

Example:

```python
# [[risk:unsafe-cache-invalidation]]
@decorator
def refresh_cache():
    ...
```

Example:

```rust
// [[invariant:no-unsafe-ffi-without-wrapper]]
#[repr(C)]
pub struct Handle { ... }
```

Anchor-only comment lines should be stripped from normal symbol documentation
and GoDoc-style text before embedding. They may appear in the semantic envelope
anchor section, but not as doc prose graffiti.

Do not implement anchor discovery as regex-over-source. Regex is acceptable
inside already-identified comment text, but comment extraction and owner binding
must use structured language scanners, tree-sitter, Go parser comments, or an
equivalent comment/span source.

Unsupported declaration forms become unbound lint findings. Do not guess
bindings from arbitrary source text.

Owner identity contract:

```go
type AnchorOwner struct {
	NodeID     string
	StableKey string
	Kind      OwnerKind // file, symbol
	Path      string
	Name      string
	StartLine int
	EndLine   int
}
```

Anchor owner binding must produce the same owner node identity that repoindex
graph emission will use for that file or symbol. The extractor must not invent a
parallel symbol key that PR-B cannot join back to repoindex.

Shared owner resolver seam:

```go
type OwnerResolver interface {
	ResolveFileOwner(path string) AnchorOwner
	ResolveSymbolOwner(path string, lang string, span Span, qualifiedName string) (AnchorOwner, bool)
}
```

Alternatively, repoindex can expose deterministic helpers:

```go
func FileOwnerForRepoNode(repoKey, pkg, path string) AnchorOwner
func SymbolOwnerForRepoNode(node repoindex.Node) AnchorOwner
func OwnerStableKeyForNode(node repoindex.Node) string
```

Dependency direction must remain injected:

- `semanticanchors` defines `OwnerResolver`, `AnchorOwner`, and binding result
  types.
- repoindex or a repoindex-adjacent adapter implements the resolver.
- `semanticanchors` core never imports repoindex helpers directly.
- cross-package owner-join tests live outside semanticanchors core.

`semanticanchors` must consume an injected resolver rather than inventing owner
node IDs independently.

Concrete adapter package:

```text
internal/intelligence/indexing/repoindex/anchorowners
```

This adapter may import both repoindex and semanticanchors. Neither repoindex
core nor semanticanchors core should import the adapter.

`OwnerStableKey` rules:

- symbol owner: use the existing repoindex/symbol stable key when available
- symbol fallback: language plus package/module plus repo-relative path plus
  symbol qualified name
- file owner: `file:<normalized repo-relative path>`
- never include physical line numbers
- never include extraction timestamps

Language adapter seam:

```go
type CommentOwnerExtractor interface {
	ExtractOwnersAndComments(ctx context.Context, path string, src []byte) ([]AnchorOwner, []CommentSpan, []Finding)
}
```

PR-A2 support expectations:

- Go: solid support through parser/comment groups
- TypeScript, Python, and Rust: fixture-level support only after the repoindex
  owner join passes for that language
- uncertain declaration forms: emit `unbound` or `unsupported_owner` findings
  instead of guessing
- PR-A3 explain output must show `owner_stable_key`,
  `would_be_repoindex_node_id`, `binding_reason`, and any
  `binding_confidence` or finding

Go package comments and build tags should not accidentally bind to the first
function. Treat top-of-file package/prelude comments as file/package scope
unless there is a clear declaration-local anchor group.

Language support should be staged explicitly:

```go
type LanguageAnchorSupport string

const (
	AnchorSupportLintOnly     LanguageAnchorSupport = "lint_only"
	AnchorSupportGraphBinding LanguageAnchorSupport = "graph_binding"
)
```

No language should enable graph binding until its golden owner-join fixture
proves `AnchorOwner.NodeID` equals the repoindex node ID for that file or
symbol. Languages may still support lint-only extraction before graph binding.

### Anchor Resolution

Parsing, validation, target resolution, lint/explain, and graph emission should
share one canonical resolution output:

```go
type AnchorOccurrenceInternal struct {
	RawSyntax string `json:"-"`
	RawTarget string `json:"-"`
	Occurrence AnchorOccurrence
}

type AnchorOccurrence struct {
	DisplaySyntax    string
	TargetDisplay    string
	SafeNormalizedTarget string
	TargetType       AnchorType
	Scope            AnchorScope
	SourcePath       string
	LineSpan         SourceSpan
	// SourceHash is safe to display only when it is not derived directly from
	// secret-like or unsafe raw target text.
	SourceHash       string
	OccurrenceID     string
	OwnerBinding     AnchorOwnerBinding
}

type SourceSpan struct {
	StartLine int
	EndLine   int
	StartByte int
	EndByte   int
}

type AnchorOwnerBinding struct {
	OwnerStableKey string
	OwnerNodeID    string
	OwnerKind      string
	BindingReason  string
	BindingMode    LanguageAnchorSupport
	Supported      bool
}

type AnchorResolution struct {
	Occurrence       AnchorOccurrence
	TargetID         AnchorTargetID
	// Relation is the normalized edge relation to emit.
	// Missing doc/test targets and unresolved fragments/symbols use
	// SemanticAnchorRelationDeclaresTarget here.
	Relation         SemanticAnchorRelation
	// IntendedRelation is optional UX/debug metadata only; validators must not
	// use it as the graph relation.
	IntendedRelation SemanticAnchorRelation
	ValidationStatus AnchorValidationStatus
	Findings         []Finding

	// EdgeAction is the only value graph emission should inspect.
	EdgeAction AnchorEdgeAction
}

type AnchorEdgeAction string

const (
	AnchorEdgeNone          AnchorEdgeAction = "none"
	AnchorEdgeSemantic      AnchorEdgeAction = "semantic"
	AnchorEdgeMissingTarget AnchorEdgeAction = "missing_target"
)
```

Single-path edge action contract:

| Case | Edge action | Edge type |
|------|-------------|-----------|
| valid concept anchor | `semantic` | relation edge such as `ENFORCES` |
| resolved `doc:` | `semantic` | `DESCRIBED_BY` |
| resolved `test:` | `semantic` | `VERIFIED_BY` |
| syntactically valid but missing `doc:`/`test:` target | `missing_target` | `DECLARES_ANCHOR_TARGET`; `Relation` is `DECLARES_ANCHOR_TARGET` |
| malformed, unsafe, scoped path anchor, secret-like, URL, traversal, env-var | `none` | no edge |

Repoindex emission must switch on `AnchorResolution.EdgeAction`, not on raw
anchor type or local re-resolution. Linter and explain output must render the
same `AnchorResolution` results that graph emission consumes.
`NewSemanticAnchorEdgeMeta` must consume the normalized `Relation` from
`AnchorResolution`; it must not reconstruct `VERIFIED_BY` or `DESCRIBED_BY`
from raw `doc:` or `test:` syntax when `EdgeAction` is `missing_target`.

For concept anchors, "resolved" means parser canonicalization plus policy
validation succeeded; PR-A/PR-B must not require a durable memorycore or
Obsidian target record before emitting evidence-only concept edges. For `doc:`
and `test:` anchors, resolution requires safe repo-relative filesystem and
fragment/symbol checks.

Target resolution should use an explicit seam so graph emission never performs
its own doc/test lookup:

```go
type TargetResolver interface {
	ResolveDocTarget(path, fragment string) TargetResolution
	ResolveTestTarget(path, fragment string) TargetResolution
}

type TargetResolution struct {
	Status   AnchorValidationStatus
	Findings []Finding
}

func ResolveAnchorOccurrence(
	occ AnchorOccurrence,
	policy AnchorPolicy,
	targets TargetResolver,
) AnchorResolution
```

Existing path with unresolved doc fragment or test symbol uses the same neutral
edge action as a missing file, with the narrower finding reason:

```text
ValidationStatus: AnchorValidationMissingTarget
Finding.Reason:  AnchorFindingUnresolvedFragment
EdgeAction:      AnchorEdgeMissingTarget
Relation:        DECLARES_ANCHOR_TARGET
EdgeType:        DECLARES_ANCHOR_TARGET
```

It must never emit `DESCRIBED_BY` or `VERIFIED_BY` until the fragment or test
symbol resolves.

Long-form agent notes adjacent to anchors should be advisory in PR-A. They are
useful warnings, but blockers should stay deterministic: malformed syntax,
unknown type, unsafe path, secret-like target, duplicate owner/target,
unsupported beacon, generated/vendor code, or too many anchors on one owner.

Finding IDs should be stable enough for edge metadata and explain output:

```go
type Finding struct {
	ID       string
	Reason   AnchorFindingReason
	Severity AnchorFindingSeverity
	Message  string
}
```

`AnchorOccurrenceInternal` is parser/extractor-only and must never be returned
by public lint/explain JSON or stored in edge metadata. Public surfaces use
`AnchorOccurrence`, whose display fields are safe or redacted. The public
occurrence must not contain a raw or merely normalized unsafe target. If a
normalized target is exposed, it must be `SafeNormalizedTarget` and already
redacted under the canonical redaction rules. If the implementation keeps raw
fields on one struct instead of using the split above, `RawSyntax`,
`RawTarget`, and any unsafe normalized target must be tagged `json:"-"`; all
public surfaces must render only `DisplaySyntax`, `TargetDisplay`, safe IDs,
and safe/redacted target fields.

`LintFindingIDs` in `SemanticAnchorEdgeMeta` must reference these stable IDs,
not line-number-only diagnostics.

Default finding ID derivation:

```text
finding_id = sha256(
  schema_version,
  occurrence_id or fallback_source_hash,
  reason,
  safe_canonical_target_id,
  owner_stable_key when available
)
```

For unbound anchors, use a fallback identity based on repo key, repo-relative
path, redacted normalized anchor syntax, safe/redacted source hash, and reason.
Do not include line numbers, timestamps, or raw unsafe target text in finding
IDs. Raw unsafe target correlation, if needed, must use an internal keyed or
peppered hash that is not emitted in lint/explain output.

### Existing `Index:` Compatibility

The parser may also keep supporting the existing `Index:` doc block as a
compatibility source:

```go
// Index:
// - Purpose: Update semantic file embeddings for post-review changes
// - Related: deleteFileEmbedding, indexFile
// - Keywords: semantic_file_index, embeddings, post_review
```

Compatibility mapping:

| `Index:` field | Anchor relation |
|------|------------------|
| `Purpose` | envelope summary hint |
| `Keywords` | `related_to` concept nodes |
| `Related` | `related_to` symbol edges |
| `Flow` | `flows_to` symbol edges |
| `Resources` | existing resource concept edges |
| `Events` | existing event concept edges |
| `OutputFields` | existing output field concept edges |

Do not overload the existing `DocIndex` JSON shape with all new anchor metadata
unless the shape is made explicit. Prefer a distinct anchor extraction pass or a
structured meta wrapper.

Anchor-only comment lines should be removed before normal symbol documentation,
existing `DocIndex` parsing, and any embedding text generation. Semantic anchors
may appear later only in explicit semantic-envelope sections. This prevents
anchor graffiti from leaking into ordinary docs or embeddings by accident.

### Later Block Relation Form

Block relation syntax can be useful, but it creates a larger surface for
agent-generated comment bloat. Keep it outside the current goal until inline
anchors have passed the eval and review gates below.

Future candidate:

```go
/*
Semantic:
  enforces:
    - invariant:no-send-without-read
  protects_against:
    - risk:agent-terminal-desync
*/
```

## Anchor Taxonomy

Start with a small allowlist.

| Anchor type | PR-A | Default relation | Reason |
|------|------|------------------|--------|
| `invariant` | yes | `ENFORCES` | Highest retrieval/review value |
| `risk` | yes | `PROTECTS_AGAINST` | Strong review and safety signal |
| `test` | yes | `VERIFIED_BY` | Connects intent to verification |
| `test-contract` | yes | `VERIFIED_BY` | Behavior-level verification label |
| `doc` | yes | `DESCRIBED_BY` | Connects source to long-form knowledge |
| `decision` | yes | `DECIDED_BY` | Connects code to an ADR or choice |
| `protocol` | yes | `IMPLEMENTS_PROTOCOL` | Important for foxctl runtime behavior |
| `domain` | yes | `PARTICIPATES_IN` | Stable product/runtime domain concept |
| `beacon` | strict | `BEACON_FOR` | Retrieval beacon only |
| `principle` | defer | `IMPLEMENTS_PRINCIPLE` | Can become vague product-language sprawl |
| `policy` | defer | `GOVERNED_BY` | Too authority-laden before memorycore integration |
| `event` | defer | `EMITS_EVENT` or `HANDLES_EVENT` | Existing `Index:` parser already supports events |
| `resource` | defer | `TOUCHES_RESOURCE` | Existing `Index:` parser already supports resources |

`beacon` behavior for PR-A through narrow PR-B:

- parse and lint `beacon`
- report `beacon parsed, not indexed in this version` in explain output
- do not emit beacon concept nodes
- do not emit `EdgeBeaconFor`

Spam-prevention rule:

> A `beacon` anchor is invalid unless the same owner also has at least one
> `invariant`, `risk`, `protocol`, `doc`, or `test` anchor.

Bad:

```go
// [[beacon:important-agent-stuff]]
```

Good:

```go
// [[foxctl:beacon/agent-terminal-safety]]
// [[foxctl:invariant/no-send-without-read]]
// [[foxctl:risk/agent-terminal-desync]]
```

Do not add new anchor types casually. New types need parser tests, relation
mapping, retrieval behavior, and at least one usage example.

`decision` is allowed in PR-A because it is useful for linking code to ADRs or
choice records, but lint should warn when a decision anchor has no adjacent
`doc:` anchor or later-resolved reviewed note/ADR. A decision anchor alone is
not automatic decision authority.

## Graph Model

The first implementation should extend repoindex concept nodes and soft edges.
A dedicated anchor store is not needed for PR-A or PR-B.

### Nodes

Existing repoindex nodes:

- package
- file
- symbol
- concept

Semantic anchor nodes initially use concept nodes with strict prefixes:

```text
anchor:foxctl:invariant:no-send-without-read
anchor:foxctl:risk:agent-terminal-desync
anchor:repo:test:internal/runtime/terminal/tmuxbridge/client_test.go#TestReadBeforeWrite
anchor:repo:doc:docs/general/tmux-collaboration.md#room-access
```

If concept nodes become too overloaded, a later migration can introduce a
dedicated `semantic_anchor` node kind.

### Edges

Add typed soft edges:

```text
ENFORCES
IMPLEMENTS_PROTOCOL
PARTICIPATES_IN
PROTECTS_AGAINST
VERIFIED_BY
DESCRIBED_BY
DECIDED_BY
BEACON_FOR
ANCHOR_RELATED
DECLARES_ANCHOR_TARGET
```

Defer these unless their anchor types are enabled:

```text
IMPLEMENTS_PRINCIPLE
GOVERNED_BY
TOUCHES_RESOURCE
EMITS_EVENT
HANDLES_EVENT
```

Rules:

- Structural edges such as `CALLS`, `REFERS_TO`, and `IMPORTS` remain separate.
- Anchor edges carry lower default weight than structural edges.
- Missing or unresolved doc/test targets create lint findings, not hard index
  failures.
- Missing `doc:` and `test:` targets must not emit active `DESCRIBED_BY` or
  `VERIFIED_BY` edges. They must emit `DECLARES_ANCHOR_TARGET` with
  `validation_status=missing_target` until the target resolves.
  `AnchorResolution.Relation`, `edge.Type`, and `meta.Relation` must all be
  `DECLARES_ANCHOR_TARGET` for these missing-target edges.
- Anchor edges should be included in a semantic/doc edge set rather than mixed
  into structural traversal by default.

Define edge sets explicitly. Preserve the existing `EdgeSetStructural`
membership unless a separate migration and eval update says otherwise. The
important invariant is that semantic anchor and empirical edges are excluded
from nil/default traversal.

```go
EdgeSetStructural = []EdgeType{
	EdgeContains,
	EdgeImports,
	EdgeUsesSymbol,
	EdgeRefersTo,
	EdgeCalls,
	EdgeImplements,
	EdgeEmbeds,
	EdgeTests,
}

EdgeSetDoc = []EdgeType{
	EdgeHasKeyword,
	EdgeHasOutputField,
	EdgeTouchesResource,
	EdgeEmitsEvent,
	EdgeDocRelated,
	EdgeDocFlow,
}

EdgeSetSemanticAnchors = []EdgeType{
	EdgeEnforces,
	EdgeProtectsAgainst,
	EdgeVerifiedBy,
	EdgeDescribedBy,
	EdgeDecidedBy,
	EdgeImplementsProtocol,
	EdgeParticipatesIn,
	EdgeDeclaresAnchorTarget,
}

EdgeSetEmpirical = []EdgeType{
	EdgeCoChangesWith,
}

func DefaultExpandEdgeTypes() []EdgeType {
	return CopyEdgeSet(EdgeSetStructural)
}

func AllEdgeTypes() []EdgeType {
	return ConcatEdgeSets(
		EdgeSetStructural,
		EdgeSetDoc,
		EdgeSetSemanticAnchors,
		EdgeSetEmpirical,
	)
}
```

PR-B1 should define `EdgeCoChangesWith` as a reserved no-emission constant so
`EdgeSetEmpirical` and the all-edge helper are explicit before any empirical
edge emitter exists. PR-B.5 owns the first real co-change emission. If the
implementation chooses not to define the reserved constant in PR-B1, then
`EdgeSetEmpirical` must be empty until PR-B.5; do not leave the timing
implicit. `EdgeCoChangesWith` must not be part of `EdgeSetStructural`,
`EdgeSetDoc`, or default semantic traversal. `EdgeBeaconFor` may be defined as
a reserved edge type, but narrow PR-B must not emit it until negative retrieval
evals prove `beacon` does not hijack broad queries.

The constant should carry an explicit code comment:

```go
// Reserved in PR-B1. No emitter should produce this edge until PR-B.5.
EdgeCoChangesWith EdgeType = "CO_CHANGES_WITH"
```

Use a helper that copies edge-set slices when composing sets so callers cannot
accidentally mutate package-level defaults:

```go
func CopyEdgeSet(edges []EdgeType) []EdgeType
func ConcatEdgeSets(sets ...[]EdgeType) []EdgeType
func DeduplicateEdgeTypes(edges []EdgeType) []EdgeType
```

Default expansion must remain structural. If repoindex storage currently treats
nil edge filters as "all edges," PR-B must wrap query construction so nil or
default expansion does not include semantic anchor edges or empirical co-change
edges. Semantic anchor traversal requires explicit semantic edge types or
`IncludeSemanticAnchors`; empirical traversal requires explicit empirical edge
types only. An `IncludeEmpiricalEdges` convenience flag is deferred to PR-B.5
and must not be part of the PR-B1 contract.

Callers that intentionally want all edge types must request `AllEdgeTypes()`
or an explicitly copied `EdgeSetAll` helper. Nil or empty edge filters must not
mean "all edges" after PR-B1. Do not expose `EdgeSetDefault = EdgeSetStructural`
as a mutable slice alias; normalizers should use `DefaultExpandEdgeTypes()` or
copy any exported default value before mutation.

Required normalizer:

```go
type ExpandOptions struct {
	Direction Direction
	EdgeTypes []EdgeType
	Depth     int
	Budget    int
	PerNodeCap int

	IncludeSemanticAnchors bool
}

type DAGGrepRequest struct {
	// existing fields omitted
	EdgeTypes []EdgeType

	IncludeOwnerContainers bool
	IncludeSemanticAnchors bool

	// Deprecated: maps only to IncludeOwnerContainers.
	IncludeAnchors bool
}

func NormalizeExpandOptions(opts ExpandOptions) ExpandOptions {
	if len(opts.EdgeTypes) == 0 {
		opts.EdgeTypes = DefaultExpandEdgeTypes()
	} else {
		opts.EdgeTypes = CopyEdgeSet(opts.EdgeTypes)
	}
	if opts.IncludeSemanticAnchors {
		opts.EdgeTypes = ConcatEdgeSets(opts.EdgeTypes, EdgeSetSemanticAnchors)
	}
	opts.EdgeTypes = DeduplicateEdgeTypes(opts.EdgeTypes)
	return opts
}

func NormalizeDAGGrepRequest(req DAGGrepRequest) DAGGrepRequest {
	if len(req.EdgeTypes) == 0 {
		req.EdgeTypes = DefaultExpandEdgeTypes()
	} else {
		req.EdgeTypes = CopyEdgeSet(req.EdgeTypes)
	}
	if req.IncludeAnchors {
		req.IncludeOwnerContainers = true
	}
	if req.IncludeSemanticAnchors {
		req.EdgeTypes = ConcatEdgeSets(req.EdgeTypes, EdgeSetSemanticAnchors)
	}
	req.EdgeTypes = DeduplicateEdgeTypes(req.EdgeTypes)
	return req
}
```

`IncludeSemanticAnchors` appends `EdgeSetSemanticAnchors` to the requested edge
types. If `EdgeTypes` is empty, normalization starts from
`DefaultExpandEdgeTypes()` first, then appends semantic anchor edges. If
`EdgeTypes` is already explicit, the flag appends semantic anchor edges without
replacing the explicit set.
Callers that want semantic-only traversal should pass `EdgeSetSemanticAnchors`
explicitly. Legacy `IncludeAnchors` never affects edge types and maps only to
`IncludeOwnerContainers`.

`QueryEngine.Expand`, `QueryEngine.expandWeighted`, `QueryEngine.DAGGrep`,
repoquery adapters, CLI paths, agent tools, DAG/eval helper paths, and any
`fetchEdges` caller must use this normalizer or the same explicit defaulting
rule before reaching storage.

Normalization must be centralized as well as applied at call sites:

- `QueryEngine.Expand` normalizes once at entry.
- `QueryEngine.DAGGrep` normalizes once at entry.
- `QueryEngine.expandWeighted` receives a normalized `DAGGrepRequest`.
- `fetchEdges` defensively normalizes empty edge filters before store reads.
- repoquery adapters and CLI wrappers may normalize too, but they are not the
  only protection.

### Edge Metadata

Even in repoindex-first mode, every anchor edge should preserve occurrence
metadata in `Edge.Meta`. Use a versioned typed contract, not freeform JSON.

Required shape:

```go
const SemanticAnchorEdgeMetaSchemaVersion = 1

type SemanticAnchorEdgeMeta struct {
	MetaKind                string            `json:"meta_kind"` // semantic_anchor_edge
	SchemaVersion           int               `json:"schema_version"`
	RepoKey                 string            `json:"repo_key"`
	Source                  evidence.EvidenceSource    `json:"source"`
	SourcePlane             evidence.EvidencePlane     `json:"source_plane"`
	EvidenceClass           evidence.EvidenceClass     `json:"evidence_class"`
	EvidenceAuthority       evidence.EvidenceAuthority `json:"evidence_authority"`
	// Maximum permitted downstream uses. This does not mean the edge is
	// currently active for that use.
	AllowedAuthorityEffects []evidence.AuthorityEffect `json:"allowed_authority_effects"`
	DisplaySyntax           string                     `json:"display_syntax,omitempty"` // safe or redacted wikilink

	OccurrenceID string `json:"occurrence_id"`

	ExtractionVersion int    `json:"extraction_version"`
	ParserVersion     string `json:"parser_version"`

	Path      string `json:"path"`
	LineStart int   `json:"line_start"`
	LineEnd   int   `json:"line_end"`

	OwnerNodeID    string `json:"owner_node_id"`
	OwnerKind      string `json:"owner_kind"`
	OwnerStableKey string `json:"owner_stable_key,omitempty"`

	Relation      SemanticAnchorRelation `json:"relation"`
	TargetDisplay string                 `json:"target_display,omitempty"` // safe or redacted target
	TargetID      AnchorTargetID         `json:"target_id"`
	TargetType    string                 `json:"target_type"`
	TargetSlug    string                 `json:"target_slug,omitempty"`
	Scope         string                 `json:"scope,omitempty"`

	ValidationStatus   AnchorValidationStatus `json:"validation_status"`
	LintFindingIDs     []string               `json:"lint_finding_ids,omitempty"`
	MaxFindingSeverity AnchorFindingSeverity `json:"max_finding_severity,omitempty"`
	SourceHash         string                 `json:"source_hash,omitempty"`
}
```

`DisplaySyntax`, `TargetDisplay`, and public `SourceHash` fields are
presentation/correlation fields only. They must be safe or redacted before
storage when the anchor is unsafe. Do not store raw secret-like, PII-like,
token-like, terminal-output-like, or unsafe target text in semantic anchor edge
metadata.
`TargetSlug` is for concept anchors only. For `doc:` and `test:` path anchors,
leave `TargetSlug` empty; the canonical identity is `TargetID`, and path/test
case must be preserved there.

The JSON shape may flatten evidence fields for query convenience, but
construction must start from `evidence.EvidenceMeta` and validation must
reconstruct and validate that shared evidence shape. Do not define local copies
of evidence enums in `semanticanchors`.

Construct edge metadata through a resolution-first helper, not ad hoc struct
literals. Package ownership must avoid a repoindex import cycle:

```text
internal/intelligence/indexing/semanticanchors
  - AnchorResolution
  - AnchorEdgeAction
  - AnchorTargetID
  - RepoScopedAnchorKey
  - RepoIndexAnchorNodeID as a string type
  - SemanticAnchorEdgeMeta
  - NewSemanticAnchorEdgeMeta
  - DecodeSemanticAnchorEdgeMeta
  - ValidateSemanticAnchorEdgeMeta
  - may import internal/intelligence/evidence
  - must not import repoindex

internal/intelligence/indexing/repoindex
  - EdgeTypeForSemanticAnchorRelation
  - NewSemanticAnchorEdge, returning repoindex.Edge
  - ValidateSemanticAnchorEdge, cross-validating repoindex.Edge against
    semanticanchors metadata
```

Semantic-anchor core helper:

```go
func NewSemanticAnchorEdgeMeta(res AnchorResolution, owner AnchorOwner) (SemanticAnchorEdgeMeta, error)
```

`NewSemanticAnchorEdgeMeta` must fail closed if
`res.Occurrence.OwnerBinding` disagrees with the supplied `owner`. At minimum,
owner node ID, owner stable key when present, and owner kind must match. This
prevents graph emission from pairing a valid resolution with a different owner
argument.

Repoindex adapter helper:

```go
func NewSemanticAnchorEdge(res semanticanchors.AnchorResolution, owner semanticanchors.AnchorOwner) (Edge, error)
```

The constructor assigns `MetaKind`, `Source`, `SourcePlane`, `EvidenceClass`,
`EvidenceAuthority`, and `AllowedAuthorityEffects` internally. Callers must not
provide trusted authority metadata. It should build an `evidence.EvidenceMeta`
first, then flatten those values into JSON fields:

```go
meta := evidence.EvidenceMeta{
	Source:            evidence.EvidenceSourceSemanticAnchor,
	SourcePlane:       evidence.EvidencePlaneSemanticAnchor,
	EvidenceClass:     evidence.EvidenceClassSourceComment,
	EvidenceAuthority: evidence.EvidenceAuthorityEvidenceOnly,
	AllowedAuthorityEffects: []evidence.AuthorityEffect{
		evidence.AuthorityEffectRetrievalRanking,
		evidence.AuthorityEffectReviewSignal,
	},
}
```

Narrow PR-B semantic anchor edges should always use that evidence metadata.

The active use is decided later by `ValidateRenderSurface`. Stored semantic
anchor metadata must never carry `AuthorityEffectInstructionSource` as an
allowed effect.

Graph emission must follow one path:

```text
parse/validate/resolve once -> AnchorResolution -> repoindex.NewSemanticAnchorEdge
```

Graph-emission code must not inspect raw anchor strings, raw target type, or
raw doc/test existence independently of `AnchorResolution`.
`NewSemanticAnchorEdgeMeta` must not re-parse raw syntax. It may copy raw
syntax only for safe/debug fields after redaction rules are applied, and must
take normalized target, target type, scope, line span, source hash, occurrence
ID, relation, validation status, finding IDs, and owner binding facts from
`AnchorResolution.Occurrence` and the supplied owner.

Required enums:

```go
type SemanticAnchorRelation string

const (
	SemanticAnchorRelationEnforces           SemanticAnchorRelation = "ENFORCES"
	SemanticAnchorRelationProtectsAgainst    SemanticAnchorRelation = "PROTECTS_AGAINST"
	SemanticAnchorRelationVerifiedBy         SemanticAnchorRelation = "VERIFIED_BY"
	SemanticAnchorRelationDescribedBy        SemanticAnchorRelation = "DESCRIBED_BY"
	SemanticAnchorRelationDecidedBy          SemanticAnchorRelation = "DECIDED_BY"
	SemanticAnchorRelationImplementsProtocol SemanticAnchorRelation = "IMPLEMENTS_PROTOCOL"
	SemanticAnchorRelationParticipatesIn     SemanticAnchorRelation = "PARTICIPATES_IN"
	SemanticAnchorRelationDeclaresTarget     SemanticAnchorRelation = "DECLARES_ANCHOR_TARGET"
)

type AnchorValidationStatus string

const (
	AnchorValidationEvidenceOnly   AnchorValidationStatus = "evidence_only"
	AnchorValidationValidReference AnchorValidationStatus = "valid_reference"
	AnchorValidationMissingTarget  AnchorValidationStatus = "missing_target"
	AnchorValidationLintError      AnchorValidationStatus = "lint_error"
)

type AnchorFindingSeverity string

const (
	AnchorFindingInfo    AnchorFindingSeverity = "info"
	AnchorFindingWarning AnchorFindingSeverity = "warning"
	AnchorFindingError   AnchorFindingSeverity = "error"
)
```

Semantic-anchor core metadata codec:

```go
func DecodeSemanticAnchorEdgeMeta(raw json.RawMessage) (SemanticAnchorEdgeMeta, error)

func ValidateSemanticAnchorEdgeMeta(meta SemanticAnchorEdgeMeta) error {
	evidenceMeta := evidence.EvidenceMeta{
		Source:                  meta.Source,
		SourcePlane:             meta.SourcePlane,
		EvidenceClass:           meta.EvidenceClass,
		EvidenceAuthority:       meta.EvidenceAuthority,
		AllowedAuthorityEffects: meta.AllowedAuthorityEffects,
	}
	if err := evidence.ValidateEvidenceMeta(evidenceMeta); err != nil {
		return err
	}
	// validate meta kind is semantic_anchor_edge
	// validate schema version is SemanticAnchorEdgeMetaSchemaVersion
	// validate source and evidence class enums
	// validate relation enum
	// validate validation status enum
	// validate repo key
	// validate owner node/stable key
	// validate target ID
	return nil
}
```

Repoindex semantic-edge adapter helpers:

```go
func ValidateSemanticAnchorEdge(edge Edge) error {
	meta, err := DecodeSemanticAnchorEdgeMeta(edge.Meta)
	if err != nil {
		return err
	}
	if err := ValidateSemanticAnchorEdgeMeta(meta); err != nil {
		return err
	}
	// edge.Type matches meta.Relation
	// edge.Src equals meta.OwnerNodeID
	// edge.Dst equals AnchorTargetNodeID(meta.RepoKey, meta.TargetID)
	// meta.SourcePlane is semantic_anchor
	// meta.EvidenceAuthority is evidence_only
	// meta.AllowedAuthorityEffects excludes instruction_source
	// missing doc/test targets and unresolved fragments/symbols use
	// DECLARES_ANCHOR_TARGET in both edge.Type and meta.Relation
	// missing doc/test targets never use VERIFIED_BY or DESCRIBED_BY
	return nil
}

func DecodeAndValidateSemanticAnchorEdge(edge Edge) (SemanticAnchorEdgeMeta, bool, error)
func FilterValidSemanticAnchorEdges(edges []Edge) ([]Edge, []SemanticProjectionWarning)

func EdgeTypeForSemanticAnchorRelation(rel SemanticAnchorRelation) (EdgeType, bool)
```

Functions that accept or return `Edge` live in repoindex, not
semanticanchors. Functions that only decode or validate `SemanticAnchorEdgeMeta`
live in semanticanchors.
Add import guards for both directions: `semanticanchors` core must never import
repoindex, and repoindex should import semanticanchors only from the explicit
semantic-anchor adapter/emission/projection files, not from unrelated core
query or storage packages.

Every semantic projection or explain surface should use this codec rather than
decoding semantic anchor edge metadata ad hoc. Raw graph surfaces remain raw and
must not label semantic evidence.

Projection paths must validate before rendering semantic evidence:

- repoquery semantic projections
- CLI semantic output and anchor explain
- agent-tool semantic output
- DAG/expand explain surfaces
- future searchindex envelope providers
- future ACA retrieval
- future Obsidian bridge/graph output

Invalid metadata is omitted from semantic evidence or downgraded to opaque
evidence. It must never be trusted as semantic anchor evidence without
successful edge+meta validation.
If an invalid semantic-anchor edge is retained in a raw graph result, every
public renderer must label it only as an opaque graph edge. It must not display
`authority`, `permitted_use`, `evidence_only`, semantic-anchor relation
wording, or instruction eligibility fields unless `ProjectSemanticAnchorEdges`
or `DecodeAndValidateSemanticAnchorEdge` accepts it.
Public CLI, agent, and rendered outputs must either project through
`ProjectSemanticAnchorEdges` before rendering semantic fields, or show semantic
edge metadata only as opaque raw JSON with no authority, permitted-use,
evidence-plane, semantic relation, or instruction labels. Public non-debug
renderers should omit or redact semantic-anchor metadata unless validation
succeeds.

Raw storage calls such as outgoing/incoming edge reads may return persisted
edges as-is. Public repoindex surfaces that project semantic edges must enforce
validation centrally, preferably in `QueryEngine` or repoquery projection, not
only in leaf CLI rendering code.

The concrete boundary is "validated semantic projection":

```go
type SemanticProjectionWarning struct {
	EdgeID string
	Reason SemanticProjectionWarningReason
	Message string
}

type SemanticProjectionWarningReason string

const (
	SemanticProjectionWarningInvalidMeta       SemanticProjectionWarningReason = "invalid_meta"
	SemanticProjectionWarningIllegalAuthority  SemanticProjectionWarningReason = "illegal_authority"
	SemanticProjectionWarningMismatchedEdge    SemanticProjectionWarningReason = "mismatched_edge"
	SemanticProjectionWarningMissingTargetEdge SemanticProjectionWarningReason = "missing_target_edge"
)

type ValidatedSemanticProjection struct {
	Edges    []Edge
	Warnings []SemanticProjectionWarning
}

func ProjectSemanticAnchorEdges(edges []Edge) ValidatedSemanticProjection
```

`SemanticProjectionWarningMissingTargetEdge` is for invalid missing-target
metadata, such as `validation_status=missing_target` paired with `VERIFIED_BY`
or `DESCRIBED_BY`. A valid `DECLARES_ANCHOR_TARGET` edge with
`validation_status=missing_target` is valid semantic evidence and should not be
dropped or warned solely because the target is missing.

`ProjectSemanticAnchorEdges` wraps `FilterValidSemanticAnchorEdges`. Raw graph
results such as `ExpandResult` may remain raw for storage/debug callers, but
any public surface that labels edges as semantic evidence must project through
this boundary and carry or render warnings.

Filtering returns projection warnings, not anchor lint findings:

```go
func FilterValidSemanticAnchorEdges(edges []Edge) ([]Edge, []SemanticProjectionWarning)

func SemanticProjectionEdgeID(edge Edge) string {
	return edge.Src + "|" + edge.Dst + "|" + string(edge.Type)
}
```

`SemanticProjectionEdgeID` is the stable warning key because current repoindex
edges do not have standalone IDs.

PR-B should pick this API contract:

```go
func (q *QueryEngine) Expand(ctx context.Context, seed string, opts ExpandOptions) (ExpandResult, error)

func ProjectSemanticAnchorEdges(edges []Edge) ValidatedSemanticProjection
```

`ExpandResult` remains a raw graph result. Repoquery, CLI, agent-tool semantic
outputs, DAG grep semantic projections, anchor explain, and future context
adapters must call `ProjectSemanticAnchorEdges` explicitly before labeling any
edge as semantic evidence.

Read-path validation ownership:

| Surface | Validator |
|------|-----------|
| `QueryEngine.Expand` | raw graph result; normalize traversal only |
| `QueryEngine.DAGGrep` | raw graph result; normalize traversal only |
| repoquery semantic projection | `repoindex.ProjectSemanticAnchorEdges` |
| CLI explain | `repoindex.DecodeAndValidateSemanticAnchorEdge` |
| agent-tool semantic output | `repoindex.ProjectSemanticAnchorEdges` |
| DAG/expand explain surfaces | `repoindex.ProjectSemanticAnchorEdges` |
| raw store edge reads | raw graph edges with no semantic authority fields |

Do not defer occurrence metadata. Without it, stale-anchor lint and curator
proposals become much harder. Do not store durable target lifecycle or
memorycore authority in this edge meta during indexing; that authority belongs
to a separate target review/memory layer and can be joined during retrieval or
explain output.

Line numbers are for explain/lint UX. Durable occurrence references should use
`OccurrenceID`, `OwnerStableKey`, and `SourceHash` rather than physical line
numbers.

Occurrence identity:

```text
OccurrenceID = sha256(
  repoKey,
  ownerStableKey,
  relation,
  anchorTargetID,
  safeNormalizedAnchorSyntax,
  extractionSchemaVersion
)
```

`OccurrenceID` must not include line numbers, timestamps, or volatile extraction
state. For unsafe or rejected anchors, `safeNormalizedAnchorSyntax` must be the
redacted canonical form, not raw secret-like target text. If internal
correlation needs raw text, use a keyed or peppered internal hash that is not
shown in CLI/explain output. Public `SourceHash` separately hashes the
normalized physical anchor/comment group for drift detection only when that
group is safe or redacted.

### Existing Parser Collision

`internal/intelligence/indexing/repoindex/parser/index.go` currently parses
`Index:` doc metadata and `comment_edges.go` expects `Node.Meta` to contain
`DocIndex` JSON. Semantic anchors need a related but distinct extraction model:

```text
raw file comments
  -> anchor occurrences with line/span
  -> nearest symbol or file owner
  -> semantic anchor metadata
  -> repoindex nodes/edges
```

PR-A should either add a neighboring package:

```text
internal/intelligence/indexing/semanticanchors
```

or a distinct parser file:

```text
internal/intelligence/indexing/repoindex/parser/anchors.go
```

Keep the semantic anchor parser distinct from `DocIndex`.

PR-B must not overload `Node.Meta`. Keep existing `Index:` comment metadata
working unchanged. Semantic anchors use a separate extraction/emission path
unless a typed `NodeMeta` wrapper is introduced across all readers.

If metadata is later embedded in repoindex node meta, use an explicit wrapper
instead of making `Node.Meta` a dumping ground:

```go
type NodeMeta struct {
	DocIndex        *parser.DocIndex `json:"doc_index,omitempty"`
	SemanticAnchors []AnchorRef      `json:"semantic_anchors,omitempty"`
}
```

For PR-B, prefer a separate extraction pass so the current `DocIndex` path
continues to work unchanged.

### DAG Grep Naming

`repoindex.DAGGrepRequest` already has `IncludeAnchors`, but that currently
means adding file/package container anchors around symbol/file nodes. Once
semantic anchors exist, that name will be ambiguous.

Required split:

```go
IncludeOwnerContainers bool
IncludeSemanticAnchors bool
```

Keep the old field only as compatibility if needed:

```text
IncludeAnchors => IncludeOwnerContainers only
```

Never map legacy `IncludeAnchors` to semantic anchors. The split must cover
repoindex request types, repoquery service types, and CLI paths such as
`cmd/foxctl/cmd/context_repoindex_dag_inspect.go`, which currently passes
`IncludeAnchors: true`.

Split response terminology at the same time. Existing result fields or CLI
labels named `Anchors` refer to owner-container projections only. Rename them
to `OwnerContainers`, `ContainerNodes`, or similarly explicit owner-container
terms where possible. If compatibility requires retaining an `Anchors` field,
document it as deprecated owner-container-only output and never populate it
with semantic anchors. Semantic anchors must use distinct names such as
`SemanticAnchors`, `SemanticAnchorEdges`, or `SemanticEvidence`.

This split must land before semantic-anchor PR-B graph emission.

### Anchor Traversal

Store one semantic edge from owner to anchor:

```text
symbol/file owner -> semantic edge -> anchor concept node
```

Do not store reverse edges in V0. Reverse edges double edge count and can make
DAG explanations noisier.

Traversal contract:

```text
symbol/file seed -> expand out over semantic edges
anchor concept seed -> expand in over semantic edges
```

Low-level graph APIs should keep direction explicit, for example:

```bash
foxctl index repo expand --seed anchor:foxctl:invariant:no-send-without-read --direction in --edge ENFORCES
```

Explain/query UX may auto-select incoming traversal when the seed is an anchor
concept node.

PR-B2 must test both directions explicitly.

## Empirical Git Layer

Co-change, freshness, and volatility belong in a separate empirical layer. They
are not semantic anchors.

Rules:

- Do not encode co-change in source comments.
- Do not turn co-change directly into semantic anchors.
- Strong co-change may create an ACA proposal or inspect suggestion, never a
  source edit.
- Co-change can help retrieval and review as context signal after eval gating.
- Freshness is metadata about extraction, verification, and recency. It does not
  mean an anchor is true.

Good distinction:

```text
fresh anchor edge = recently extracted or recently verified
trusted anchor target = reviewed by docs, tests, or memorycore lifecycle
```

### Co-Change Storage

Use existing ACA co-change machinery as the starting point, but expose an
index-level empirical projection when needed. Avoid creating a second,
contradictory co-change implementation.

Target architecture:

```text
shared co-change collector/scorer
        |
        +--> ACA personalized prior
        |
        +--> repoindex inspect/top-K projection
```

The shared scorer should preserve the existing ACA semantics around commit
limits, max files per commit, half-life decay, giant commit handling, noisy path
filtering, and generated/lockfile downweighting.

Package direction matters: `contextplane` already depends on repoindex-adjacent
intelligence. Repoindex must not import `internal/context/contextplane`. Extract
shared co-change collection/scoring into a lower-level package such as:

```text
internal/intelligence/indexing/gitcochange
```

or:

```text
internal/intelligence/indexing/repoindex/cochange
```

Both ACA/contextplane and repoindex inspect can then consume the same collector.
The shared API should accept deterministic config:

```go
type Config struct {
	CommitLimit          int
	MaxFilesPerCommit    int
	HalfLifeDays         int
	TopKPerFile          int
	Now                  time.Time
	SkipGenerated        bool
	SkipLockfiles        bool
	GiantCommitSoftLimit int
	GiantCommitHardLimit int
}
```

Resolve giant commit behavior before PR-B.5 implementation:

```text
giant commits are downweighted until the hard limit, then skipped
```

Executable policy:

```go
if fileCount > GiantCommitHardLimit {
	skipCommit()
} else if fileCount > GiantCommitSoftLimit {
	weight *= softPenalty(fileCount)
} else {
	// normal weight
}
```

The scorer must use the injected `Now time.Time` from config and must not call
`time.Now()` internally.

Do not add all pairwise co-change edges to the normal repoindex `edges` table.
Co-change is high-cardinality, symmetric, and recomputed from git. Store it
separately or expose top-K virtual edges.

Candidate table:

```sql
repoindex_cochange_edges(
  repo_key TEXT NOT NULL,
  src_path TEXT NOT NULL,
  dst_path TEXT NOT NULL,
  score REAL NOT NULL,
  count INTEGER NOT NULL,
  weighted_count REAL NOT NULL,
  last_seen_commit TEXT,
  last_seen_at TEXT,
  freshness REAL NOT NULL,
  volatility REAL NOT NULL,
  source TEXT NOT NULL DEFAULT 'git',
  PRIMARY KEY(repo_key, src_path, dst_path)
);
```

Traversal behavior:

- expose top-K co-change neighbors as virtual `CO_CHANGES_WITH` edges in
  `expand` and DAG grep, or
- materialize only the top few neighbors per file.
- expose co-change only through the empirical edge set unless a caller
  explicitly opts in.

### Co-Change Guards

The empirical layer should:

- cap top-N neighbors per file
- downweight giant commits
- downweight formatting-only commits
- skip or downweight lockfiles and generated files
- weight recent commits more than old commits
- preserve symmetry
- avoid uncommitted changes corrupting committed history
- track freshness against indexed HEAD

### Pre-Commit and Review Warnings

Pre-commit or PR review should initially warn, not block.

Example:

```text
Changed:
  internal/runtime/terminal/tmuxbridge/client.go

Anchors touched:
  foxctl:invariant/no-send-without-read
  foxctl:risk/agent-terminal-desync

Likely co-change neighbors not touched:
  internal/runtime/terminal/tmuxbridge/client_test.go  score 0.82
  cmd/foxctl/cmd/tmux.go                               score 0.51

Suggested checks:
  go test ./internal/runtime/terminal/tmuxbridge
```

Block only on malformed anchors, secret/transient anchor targets, invalid
explicit doc/test paths, or committed generated artifacts that are stale.

## Semantic Envelope Contract

Embeddings should target deterministic semantic envelopes instead of raw code
chunks alone.

Example envelope:

```md
# Symbol: Bridge.Type

Kind: method
Package: internal/runtime/terminal/tmuxbridge
Path: internal/runtime/terminal/tmuxbridge/client.go

Signature:
...

Documentation:
...

Semantic anchors:
- enforces [[foxctl:invariant/no-send-without-read]]
- protects_against [[foxctl:risk/agent-terminal-desync]]
- verified_by [[test:internal/runtime/terminal/tmuxbridge/client_test.go#TestReadBeforeWrite]]
- described_by [[doc:docs/general/tmux-collaboration.md#room-access]]

Graph neighbors:
- calls Bridge.Read
- called_by agent terminal control path

Empirical co-change neighbors, only when enabled:
- internal/runtime/terminal/tmuxbridge/client_test.go
- cmd/foxctl/cmd/tmux.go

Review cautions:
- This symbol is linked to risk anchors.
- Evidence hint: this symbol has evidence-only anchor links. Review linked
  tests/docs before changing or removing them.

Source excerpt:
...
```

Review cautions are future optional sections. Keep them out of the first PR-C
embedding text. When added later, they must be deterministic templates derived
from anchor types and reviewed docs/tests, not freeform LLM commentary. If they
are included in embedding text, the template version must be part of the digest.

Co-change neighbors should be metadata-first. Make co-change-in-envelope an
explicit option:

```go
IncludeCoChangeNeighborsInEnvelope bool
```

Default it to false until retrieval evals show enough embedding value to justify
the re-embedding churn caused by changing git history.

Provider output must be sorted, capped, schema-versioned, source-plane labeled,
and free of raw memorycore, proposal, session, or terminal content.

### Digest Includes

Extend the existing symbol embedding digest deterministically with:

- envelope schema version
- provider version
- anchor extraction version
- max-list/cap configuration
- included section flags
- caution template version, if review cautions are emitted
- embedding model
- repo key when any anchor/doc/test section is included
- normalized anchor target IDs
- relation names
- owner binding identity
- linked doc/test target IDs when resolved
- resolved doc/test title or heading, if included in text
- capped structural graph neighbor IDs
- capped co-change neighbor IDs when included in envelope text
- symbol/code digest from the current embeddingtext contract
- normalized documentation digest

Useful explicit digest fields:

```go
type EnvelopeDigest struct {
	SchemaVersion            string `json:"schema_version"`
	ProviderVersion          string `json:"provider_version,omitempty"`
	EmbeddingModel           string `json:"embedding_model"`
	SymbolDigest             string `json:"symbol_digest,omitempty"`
	AnchorDigest             string `json:"anchor_digest,omitempty"`
	GraphNeighborDigest      string `json:"graph_neighbor_digest,omitempty"`
	LinkedDocDigest          string `json:"linked_doc_digest,omitempty"`
	LinkedTestDigest         string `json:"linked_test_digest,omitempty"`
	CoChangeDigest           string `json:"cochange_digest,omitempty"`
	CautionTemplateVersion   string `json:"caution_template_version,omitempty"`
	AnchorExtractionVersion  string `json:"anchor_extraction_version,omitempty"`
	SectionFlagsDigest       string `json:"section_flags_digest,omitempty"`
	CapConfigDigest          string `json:"cap_config_digest,omitempty"`
}
```

The anchor digest includes owner stable key, relation, canonical target ID,
target type, scope, validation category when it changes generated text, and
anchor extraction version. It excludes line numbers and extraction timestamps.

### Digest Excludes

Do not include:

- anchor occurrence line numbers
- exact freshness timestamps
- exact co-change scores
- volatile commit hashes
- raw unreviewed agent notes
- raw memorycore content
- terminal output
- secrets or redacted values
- full docs
- full tests
- live ACA state
- linter warning timestamps

Line movement from formatting should not force re-embedding if the anchor
remains bound to the same owner and target. Freshness and exact co-change scores
are ranking metadata, not embedding text, unless a later eval proves the
re-embedding churn is worth it.

### Metadata Versus Text

Embedding text may include stable/coarse empirical facts:

```text
Empirical co-change neighbors:
- client_test.go
- tmux.go
```

Metadata can include volatile values:

```json
{
  "cochange_score": 0.82,
  "last_seen_commit": "abc123",
  "freshness": 0.91,
  "last_verified_at": "2026-05-05T09:12:00Z"
}
```

## Authority and Lifecycle

Keep occurrence and target authority separate.

```text
anchor occurrence != anchor target authority
```

Occurrence:

```text
this file/symbol contains this anchor at this line
```

Occurrence metadata belongs in repoindex edge metadata. It tracks where the
anchor appears, what owner it binds to, what target it names, what relation it
creates, what parser version extracted it, and whether local validation passed.
Authority remains evidence-only.

Target:

```text
foxctl:invariant/no-send-without-read
```

Target lifecycle can map to memorycore concepts:

```text
candidate -> active -> stale -> archived
candidate -> rejected
active -> deprecated -> superseded
active -> validated
```

Authority ladder:

| Evidence | Effect |
|------|--------|
| Source anchor only | Evidence-level; useful for retrieval |
| Anchor plus existing test target | Stronger review signal |
| Anchor plus reviewed doc or ADR | Stronger semantic signal |
| Anchor plus memorycore validated record | Can influence behavior/routing |
| Anchor plus active policy memory | Can become instruction-like only if explicitly allowed |
| Co-change only | Context hint only; never authority |

Do not store every occurrence as a memorycore record. That would turn
memorycore into an index mirror. If an anchor target is promoted, use lifecycle
and trust for the target concept:

| Anchor type | Memorycore kind | Default lifecycle | Default usage |
|------|-----------------|-------------------|---------------|
| `invariant` | `semantic_fact` | candidate/unreviewed | evidence_only |
| `risk` | `semantic_fact` | candidate/unreviewed | evidence_only |
| `decision` | `decision` | candidate/unreviewed | evidence_only |
| `protocol` | `semantic_fact` or `procedural_skill` | candidate/unreviewed | evidence_only |
| `doc` | usually not a memory record | n/a | n/a |
| `test` | usually not a memory record | n/a | n/a |
| `policy` | deferred | n/a | n/a |

A source anchor alone should have low authority:

```go
TrustEnvelope{
	SourceTrust: "source_comment",
	Confidence: 0.55,
	Authority:  0.20,
}
```

Authority increases only when backed by reviewed docs, tests, validated memory,
or explicit human approval.

Promoted target records should use existing proposal/durable-memory pathways,
or a specific source lane if one is added in later target-promotion work. Do
not add or wire this in PR-A/PR-B:

```go
SourceLaneRepoSemanticAnchor SourceLane = "repo_semantic_anchor"
```

Default promoted target records remain evidence-only:

```text
Kind: semantic_fact / decision / procedural_skill
Trust.SourceTrust: source_comment
Trust.Confidence: 0.55
Trust.Authority: <= 0.20
Usage.EvidenceOnly: true
Usage.InstructionEligible: false
Lifecycle.State: candidate
ReviewStatus: unreviewed
```

Only reviewed docs/tests/validated memory should raise confidence. Only
explicit active policy should make an anchor-derived record
instruction-eligible.

ACA should eventually represent source anchor edits as typed proposals before
automatic source rewrites. Do not add or wire this in PR-A/PR-B unless it is a
dead declaration with no behavior:

```go
PolicyKindSemanticAnchorPatch PolicyKind = "semantic_anchor_patch"
```

Later, target lifecycle review may need a separate proposal kind:

```go
PolicyKindAnchorTargetReview PolicyKind = "semantic_anchor_target_review"
```

Co-change should initially create inspect findings, not source-edit proposals:

```text
Observed:
  tmuxbridge/client.go and tmuxbridge/client_test.go co-change frequently.

Suggestion:
  Consider linking Bridge.Type to test:internal/runtime/terminal/tmuxbridge/client_test.go#TestReadBeforeWrite.
```

Only after review should that become a source anchor patch.

### Authority Rendering Barrier

Semantic anchor, structural graph, and git empirical signals may be retrieved,
ranked, displayed, and used for review. They must not be rendered as
instructions, hard constraints, policy, tool authorization, hook doctrine, or
runtime guardrails.

Allowed render locations:

- evidence pack nodes
- retrieval inspect output
- repoindex explain output
- review warnings
- PR summaries
- lint findings
- optional context hints labeled `evidence_only`

Forbidden render locations:

- system/developer prompt text
- hard constraints
- policy files
- tool allowlists
- instruction memory
- active runtime guardrails
- memorycore records with `instruction_eligible=true`

Only durable memorycore records with active lifecycle, reviewed/validated
status, and explicit `Usage.InstructionEligible` may shape behavior as
instructions.

Explain and lint output should use boring evidence wording:

```text
authority: evidence_only
permitted_use: retrieval_ranking, review_signal
instruction_eligible: false
```

Avoid phrasing that makes render surfaces sound like active authority.

Fail-closed behavior:

```text
If edge metadata is missing, malformed, caller-provided, or declares illegal
allowed authority effects:
  drop the authority fields and treat the edge as evidence_only,
  or omit the edge from instruction-sensitive surfaces entirely.
```

## ACA Integration

Semantic anchors should become another high-signal input to ACA retrieval, not a
replacement for ACA.

### Retrieval

`context retrieve` can eventually blend:

- top-of-mind
- latest handoff
- observations and tensions
- Obsidian note hits
- repoindex path and symbol hints
- semantic anchor graph hits
- semantic envelope vector hits
- co-change and repo motif priors

Example query:

```text
agents keep typing into the wrong terminal pane
```

Expected retrieval path:

```text
query -> beacon:agent-terminal-safety
      -> risk:agent-terminal-desync
      -> invariant:no-send-without-read
      -> Bridge.Type / terminal control code
      -> linked tests and docs
```

Do not change ACA ranking until parser, graph edges, explain output, and a
small eval fixture are proven.

### Proposal Governance

ACA proposal examples:

- add an invariant anchor to a trust-critical function
- remove a stale test anchor whose target no longer exists
- split a vague `beacon` into a precise `risk` and `invariant`
- add a missing doc backlink for an anchor used by several symbols
- suggest a test anchor because strong co-change repeatedly links code and test

### Retrieval Inspection

`context retrieve-inspect` and retrieval evals can classify misses as:

- missing anchor
- stale anchor
- orphaned anchor target
- ambiguous anchor taxonomy
- envelope missing linked docs/tests
- graph edge exists but ranker underweights it
- strong co-change exists but no semantic anchor or test link exists

## Obsidian and Docs Integration

Keep durable reasoning outside source files.

Inline source:

```go
// [[foxctl:invariant/no-send-without-read]]
```

Durable note or doc:

```md
# No Send Without Read

repo_anchors:
  - foxctl:invariant/no-send-without-read
repo_symbols:
  - internal/runtime/terminal/tmuxbridge.Bridge.Type
repo_docs:
  - docs/general/tmux-collaboration.md
```

Rules:

- Source anchors are small ports into the graph.
- Obsidian notes or repo docs carry long-form explanation.
- `obsidian graph build` may eventually generate concept-note drafts for
  heavily used anchors.
- `obsidian bridge reconcile` can suggest `repo_anchors`, `repo_symbols`, and
  `repo_docs` backlinks for anchor nodes.
- Generated notes remain inbox-first until reviewed.
- Anchor concept nodes are excluded from `obsidian graph build` by default.
  Future anchor concept-note generation may add an explicit flag after PR-B4,
  but PR-B1/PR-B4 only need the default exclusion and internal option.
- `RepoGraphBuildOptions` should include
  `IncludeAnchorConcepts bool`, defaulting to false.
- The default code-level filter should exclude concept IDs whose raw repoindex
  node ID has the `anchor:` prefix.
- PR-B1 should add repoindex helpers such as `RawNodeID` and
  `IsAnchorConceptNode` so Obsidian filtering does not duplicate string parsing.
- PR-B1 must apply the default filter to every repo graph draft concept
  collection path, including concepts attached to package graph drafts and the
  global `ListNodesByKind(NodeConcept, ...)` concept-draft path.
- The filter must run before package concept counts, package concept scores, or
  package-selection heuristics are computed. Anchor concepts should not make a
  package look more note-worthy unless `IncludeAnchorConcepts` is explicitly
  true.

Example default filter:

```go
// internal/intelligence/indexing/repoindex
func RawNodeID(id string) string {
	_, raw := SplitNamespacedID(id)
	return raw
}

func IsAnchorConceptNode(node Node) bool {
	return node.Kind == NodeConcept &&
		strings.HasPrefix(RawNodeID(node.ID), "anchor:")
}

// internal/tooling/tools/obsidian
func includeConceptInRepoGraphDraft(node repoindex.Node, opts RepoGraphBuildOptions) bool {
	if repoindex.IsAnchorConceptNode(node) && !opts.IncludeAnchorConcepts {
		return false
	}
	return true
}
```

## Agent Workflow

When an agent edits code in an anchor-aware repo:

1. Read nearby anchors for the symbols being touched.
2. Retrieve linked invariants, risks, docs, decisions, and test contracts.
3. Inspect co-change neighbors as context, not truth.
4. Edit code.
5. Run the tests linked by `test` or `test-contract` anchors when feasible.
6. Run anchor lint/stale checks for touched files.
7. Emit a graph diff in the final or PR summary.
8. Propose new anchors only when they encode a stable invariant, risk, test
   contract, decision, protocol, domain concept, or supported retrieval beacon.

Example graph diff:

```text
Added:
  Bridge.Type PROTECTS_AGAINST foxctl:risk/agent-terminal-desync

Verified:
  foxctl:invariant/no-send-without-read by internal/runtime/terminal/tmuxbridge/client_test.go#TestReadBeforeWrite

Warning:
  foxctl:protocol/agent-terminal-safety has no doc anchor

Co-change context:
  internal/runtime/terminal/tmuxbridge/client_test.go frequently changes with this file
```

## Commands

First useful surfaces:

```bash
foxctl index anchors lint --workspace .
foxctl index anchors explain --workspace . --path internal/runtime/terminal/tmuxbridge/bridge.go
foxctl index repo build --workspace . --semantic-anchors
```

Empirical inspect surfaces:

```bash
foxctl index repo cochange --workspace . --path internal/runtime/terminal/tmuxbridge/client.go
foxctl context retrieve-inspect --include-cochange "agent terminal safety"
```

Later:

```bash
foxctl index anchors query --workspace . "where do we enforce read before write?"
foxctl index anchors stale --workspace .
foxctl context anchors propose --workspace . --path internal/... --kind semantic_anchor_patch
foxctl run code/semantic_search --input '{"query":"agent terminal safety","scope":["symbols"],"use_semantic_anchors":true}'
foxctl context retrieve "agent terminal safety"
```

The exact command names can change, but the split should remain:

- index/lint/explain belongs with `foxctl index`
- context retrieval and proposals belong with `foxctl context`
- vault note generation belongs with `foxctl obsidian`

## Package Placement

Follow the package topology boundary:

| Concern | Target package family |
|------|------------------------|
| Evidence/authority/render contract | `internal/intelligence/evidence` |
| Anchor syntax parser and normalizer | `internal/intelligence/indexing/semanticanchors` or repoindex parser package |
| Repo graph anchor nodes/edges | `internal/intelligence/indexing/repoindex` |
| Co-change and freshness projection | `internal/intelligence/indexing/repoindex` or neighboring intelligence package |
| Semantic envelope generation | `internal/intelligence/indexing/embeddingtext` and `internal/intelligence/searchindex` |
| Anchor lint/explain CLI | `cmd/foxctl/cmd` calling intelligence packages |
| ACA retrieval blending and proposals | `internal/context/contextplane` |
| Memory lifecycle/trust projection | `internal/context/memorycore` |
| Durable SQL tables if needed | `internal/storage/*` |
| Obsidian note/bridge integration | `internal/tooling/tools/obsidian` and `internal/storage/obsidianindex` |

Do not place this under `internal/v2`.

### Searchindex Enrichment Seam

`internal/intelligence/searchindex` should not directly know about repoindex,
ACA, memorycore, Obsidian, and git. Add a narrow optional provider seam for PR-C
instead:

Naming caution: `searchindex.Anchor` already means a retrieval-hit location.
Semantic code anchors should use a distinct type family such as
`semanticanchors.AnchorOccurrence` and `semanticanchors.AnchorTarget`. Do not
reuse `searchindex.Anchor` for source-comment anchors. PR-C should include a
compile-level naming/import test or equivalent guard that semantic source-anchor
types do not alias or reuse `searchindex.Anchor`.

```go
type SemanticEnvelopeBits struct {
	SchemaVersion    string                     `json:"schema_version"`
	ProviderVersion  string                     `json:"provider_version"`
	TextSections     []EnvelopeSection          `json:"text_sections,omitempty"`
	Keywords         []string                   `json:"keywords,omitempty"`
	Metadata         map[string]json.RawMessage `json:"metadata,omitempty"`
	Digest           EnvelopeDigest             `json:"digest"`
	Warnings         []string                   `json:"warnings,omitempty"`
}

type EnvelopeSection struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type CodeEnvelopeProvider interface {
	EnrichSymbol(ctx context.Context, workspaceID, symbolID, path string) (SemanticEnvelopeBits, error)
	EnrichFile(ctx context.Context, workspaceID, path string) (SemanticEnvelopeBits, error)
}
```

Then `BuildCodeOptions` can accept it optionally:

```go
EnvelopeProvider CodeEnvelopeProvider
```

If nil, existing searchindex behavior remains unchanged. The provider owns the
joins across repoindex anchor edges, linked docs/tests, capped graph neighbors,
and optional co-change metadata.

If the provider fails, searchindex should degrade gracefully and still build the
plain code document.

Metadata values must be deterministic JSON. If an implementation keeps
`map[string]any`, values must be JSON-serializable primitives, arrays, and maps
only. No live structs. Volatile timestamps/scores may appear in metadata only
when intentionally excluded from digest and embedding text.

When PR-C adds semantic envelope sections, the searchindex embedding text
builder must explicitly include the selected `TextSections`; attaching them to
document metadata alone is not enough.

## Data Model Direction

### V0: Repoindex Extension

Use existing repoindex nodes and edges:

- concept nodes for anchor targets
- soft edges for anchor relationships
- edge metadata for occurrence details

This is enough to validate parsing, graph search, and retrieval value.

### V1: Anchor Tables if Needed

Add dedicated tables only when one of these becomes painful:

- querying all occurrences by path, line, or status
- tracking target lifecycle separately from edge existence
- storing linter findings over time
- staging reviewed/unreviewed anchor proposals
- recording stale, archived, or superseded anchor targets

Candidate projection:

```sql
semantic_anchor_occurrences(
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  path TEXT NOT NULL,
  owner_node_id TEXT NOT NULL,
  line INTEGER NOT NULL,
  syntax TEXT NOT NULL,
  relation TEXT NOT NULL,
  target TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_slug TEXT NOT NULL,
  confidence REAL NOT NULL,
  status TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  extracted_at TEXT NOT NULL
);

semantic_anchor_targets(
  workspace_id TEXT NOT NULL,
  target TEXT NOT NULL,
  target_type TEXT NOT NULL,
  slug TEXT NOT NULL,
  title TEXT,
  canonical_note_path TEXT,
  review_status TEXT NOT NULL,
  lifecycle_state TEXT NOT NULL,
  PRIMARY KEY(workspace_id, target)
);
```

Use SQLite/libSQL-compatible SQL first. Do not require Neo4j.

## Lint Rules

Anchor lint should catch:

- unknown anchor type
- malformed target
- uppercase or unstable slug
- empty slug
- transient/session-like slug
- absolute path
- path traversal
- URL target in PR-A
- env var in path
- unknown scope
- scoped `doc` or scoped `test`
- backslash or control character in `doc`/`test` path
- anchor not bound to a symbol and not valid as file-level anchor
- duplicate anchors on one owner
- more than 6 anchors on one owner
- more than 1 beacon on one owner
- doc target path does not exist
- test target cannot be resolved
- `beacon` without a support anchor
- `decision` without a doc/reviewed-note support path
- inline long-form agent note adjacent to an anchor
- anchor target with no usages
- anchor occurrence whose owner symbol disappeared after refactor
- anchor in generated/vendor file
- anchor-like Markdown wikilink treated as source anchor
- secret, PII, token, terminal output, absolute path, path traversal, URL, env
  var expansion, or session ID pattern in any target, including concept slugs

Lint output should be machine-readable and human-readable.

PR-A lint severity:

| Severity | Findings |
|------|----------|
| error | malformed anchor, unsafe path, scoped `doc`/`test`, unknown type, unknown scope, duplicate owner/target, secret-like target, generated/vendor anchor, too many anchors over hard cap |
| warning | missing doc/test target, unresolved doc/test fragment, `decision` without doc support, `beacon` parsed but not indexed, long-form adjacent note |

## Curation Rules

Curator can propose changes when anchors are:

- unused in retrieval
- stale after refactor
- contradicted by tests or code
- duplicated across nearby symbols
- too vague to improve search
- missing linked docs or tests
- attached to generated/vendor code
- targeting deleted test or doc paths
- used by many symbols but missing a canonical note
- duplicated under multiple scopes

Curator must not silently rewrite source. It should create ACA proposals or
normal diffs for review.

Bad curator behavior:

- silently rewriting comments
- promoting co-change into semantic links
- treating the `invariant` type as authority
- archiving anchors only because they are old while still verified

## Implementation Sequence

### PR-A1a: Evidence and Memory Gate Only

Smallest safe first implementation slice. This should land before parser,
source scanning, repoindex, CLI explain, Obsidian, searchindex, ACA retrieval,
co-change, memory promotion, or source rewrite work.

Tasks:

- add `internal/intelligence/evidence`
- define evidence enums, `EvidenceMeta`, render surfaces, and stable
  validation reason codes
- implement `ValidateAllowedAuthorityEffects`, `ValidateEvidenceMeta`, and
  `ValidateRenderSurface`
- enforce tuple coherence so source/plane/class/authority/effects cannot drift
- add import-guard tests for `internal/intelligence/evidence`
- add `contextplane.ValidateMemoryRecordForInstruction(record, surface, now)`
- construct durable-memory `EvidenceMeta` internally from `memorycore.Record`
  through `instructionEvidenceMetaForMemoryRecord`; do not accept caller-shaped
  authority metadata for the memory gate
- add stable memory instruction-gate reason codes
- parse `ValidUntil`, `ValidFrom`, `ObservedAt`, and `IngestedAt` only as
  RFC3339/RFC3339Nano and fail closed on missing or malformed TTL bases
- add fail-closed tests for empty effects, duplicate effects, unknown enum
  strings, `AuthorityEffectNone`, durable reviewed/policy `review_signal`, git
  empirical `review_warning`, semantic/structural/git instruction rejection,
  durable instruction generic legality versus memory gate, zero `now`, lifecycle,
  review status, kind allowlist, usage flags, `SupersededBy`, `Supersedes`,
  taint, `ValidUntil`, and TTL equality

### PR-A1: Contracts, Parser, and Validator

Goal: define the grammar and authority contract without reading whole repos.

PR-A1 depends on PR-A1a. It must consume the already-landed evidence package and
memory instruction gate; it must not re-own those contracts or add a second
authority/rendering vocabulary.

Tasks:

- import `internal/intelligence/evidence` from PR-A1a; do not redeclare
  evidence enums locally
- keep the PR-A1a import guard and memory-gate tests green while adding parser
  contracts
- add `internal/intelligence/indexing/semanticanchors` and import
  `internal/intelligence/evidence`; do not redeclare evidence enums locally
- add an import-guard test proving semantic-anchor core does not import
  repoindex, searchindex, contextplane, memorycore, storage, Obsidian tooling,
  or `internal/v2`
- define `AnchorType`, `AnchorScope`, `AnchorTarget`, `AnchorOccurrence`,
  `Finding`, and `AnchorFindingReason`
- define `SemanticAnchorRelation` and `AnchorValidationStatus`
- add `ParseInlineAnchor`, `CanonicalizeAnchor`, and `ValidateAnchorTarget`
- add `AnchorPolicy` and default parser constants
- add `AnchorResolution` and `AnchorEdgeAction`
- add the `TargetResolver` seam for later doc/test resolution without graph
  emission doing its own lookups
- add `ResolveAnchorOccurrence` as the canonical parser/target-resolution
  function that produces `AnchorResolution`
- add typed identity helpers for `AnchorTargetID`, `RepoScopedAnchorKey`, and
  `RepoIndexAnchorNodeID`
- add `NewAnchorTargetID`, `RepoScopedAnchorKeyFor`, `AnchorTargetNodeID`, and
  `DecodeAnchorTargetNodeID`
- add `SemanticAnchorEdgeMeta`, `SemanticAnchorEdgeMetaSchemaVersion`,
  `NewSemanticAnchorEdgeMeta`, `DecodeSemanticAnchorEdgeMeta`, and
  `ValidateSemanticAnchorEdgeMeta` in semanticanchors only
- canonicalize unscoped concept anchors through the reserved `anchor:repo:`
  target scope
- enforce allowed scopes and unscoped-only `doc`/`test`
- add parser, canonicalization, target validation, and authority matrix tests
- add exact parser-facing tests proving source anchors still fail instruction
  surfaces through the PR-A1a render barrier
- add metadata codec/validation only after `ResolveAnchorOccurrence` tests pass

No repoindex, searchindex, ACA retrieval/ranking, or source rewrite changes.
PR-A1 should not touch `internal/context/contextplane` except to keep PR-A1a
tests compiling if exported type names changed during review.

### PR-A2: Comment Extraction and Owner Binding

Goal: produce bounded occurrences from real source files without graph writes.

Tasks:

- extract anchors from source-code comments only for Go, TypeScript, Python, and
  Rust
- ignore string literals and arbitrary Markdown wikilinks
- support declaration prelude lines such as decorators, attributes, build tags,
  and package docs
- implement the `CommentOwnerExtractor` adapter seam
- consume an injected `OwnerResolver`; repoindex or a repoindex-adjacent adapter
  implements the resolver outside semanticanchors core
- add golden join fixtures that compare anchor owner binding to actual
  repoindex node IDs
- enable Go for `graph_binding` first; keep TypeScript, Python, and Rust
  `lint_only` until their golden owner-join fixtures pass
- strip anchor-only comment lines from normal symbol documentation
- bind occurrences to symbol/file owners deterministically
- define and emit `AnchorOwner`
- emit machine-readable lint findings

No repoindex storage changes.

### PR-A3: CLI Lint and Explain Dry Run

Goal: make PR-A inspectable before graph emission.

Tasks:

- add `foxctl index anchors lint --workspace .`
- add `foxctl index anchors explain --workspace . --path internal/...`
- report parsed anchors, bound owners, anchor target IDs, repo-scoped keys,
  validation statuses, occurrence IDs, source hashes, finding IDs, and findings
- report raw anchor syntax only when it is safe to display; redact raw targets
  for secret-like, PII-like, token-like, terminal-output-like, and unsafe
  anchors and rely on finding IDs, occurrence IDs, safe/redacted source hashes,
  and redacted summaries. Do not print direct raw-syntax hashes for rejected
  unsafe anchors
- report `EdgeAction`
- report evidence authority as `evidence_only`, `permitted_use:
  retrieval_ranking, review_signal`, and `instruction_eligible: false`; do not
  imply instruction or policy authority
- add golden explain-output tests for the exact evidence wording above
- report would-be repoindex node IDs, owner stable keys, binding reasons,
  binding confidence/findings, and source hashes

Still no repoindex graph writes.

### PR-A: Inline Anchor Parser and Linter

Goal: umbrella milestone for PR-A1 through PR-A3.

Tasks:

- add parser for `[[scope:type/slug]]`, `[[type:slug]]`, `doc:` targets, and
  `test:` targets
- parse anchors only from source-code comments, not strings or arbitrary
  Markdown prose
- reject scoped `doc` and scoped `test` anchors
- implement deterministic owner binding without adding repoindex storage
- do not parse block relation form yet
- keep existing `Index:` parser untouched or wrap it as compatibility input
- add normalization and validation rules
- add linter for malformed, duplicate, unknown, unbound, secret/transient,
  anchor-count, file-scope, and unsupported `beacon` anchors
- add tests for Go, TypeScript, Python, Rust, doc targets, test targets,
  arbitrary Markdown wikilinks ignored, string literals ignored, and malformed
  inputs

Storage-readiness types:

```text
invariant
risk
test
test-contract
doc
protocol
domain
decision
```

Treat `beacon` as parsed-but-not-indexed or strict experimental until negative
retrieval tests prove it does not hijack broad queries.

No repoindex storage changes yet.

### PR-B: Repoindex Anchor Edges and Explain

Goal: umbrella milestone for PR-B1 through PR-B4.

Tasks:

- extend repoindex comment/anchor extraction to emit anchor concept nodes
- add typed anchor edge relations
- store `SemanticAnchorEdgeMeta` occurrence metadata on edges
- expose anchor edges through repoindex search, expand, and DAG grep
- add an explain surface for one file or symbol
- add fixture tests for symbol binding and edge traversal
- split DAG grep `IncludeAnchors` semantics into owner containers and semantic
  anchors
- keep default structural traversal unchanged
- preserve current `EdgeSetStructural` membership unless a separate migration
  and eval update changes it
- document `foxctl index repo build --semantic-anchors` or equivalent behavior

Keep existing `Index:` edges working.

### PR-B1: Repoindex Semantic Anchor Edge Types

Goal: add graph vocabulary without emitting anchors yet.

PR-B1 is a non-negotiable precondition for PR-B2 graph emission. Do not emit
semantic anchor concept nodes or edges until default traversal normalization,
the `IncludeAnchors` split, and Obsidian anchor-concept filtering have landed.

Tasks:

- add semantic edge constants such as `EdgeEnforces`,
  `EdgeProtectsAgainst`, `EdgeVerifiedBy`, `EdgeDescribedBy`,
  `EdgeDecidedBy`, `EdgeImplementsProtocol`, `EdgeParticipatesIn`, and
  `EdgeDeclaresAnchorTarget`
- define `EdgeBeaconFor` as reserved but do not emit it yet
- define `EdgeCoChangesWith` as a reserved no-emission constant in PR-B1 and
  put it in `EdgeSetEmpirical`; PR-B.5 owns actual co-change edge emission
- add `EdgeSetSemanticAnchors` and `EdgeSetEmpirical`
- keep semantic and empirical edge sets out of `EdgeSetStructural`
- preserve the current structural edge set membership: `CONTAINS`, `IMPORTS`,
  `USES_SYMBOL`, `REFERS_TO`, `CALLS`, `IMPLEMENTS`, `EMBEDS`, and `TESTS`
- use existing `EdgeSetDoc` as the canonical doc/comment edge set name
- add `CopyEdgeSet` and `ConcatEdgeSets` or equivalent helpers so default edge
  set slices are copied before composition or caller mutation
- add `EdgeTypeForSemanticAnchorRelation`
- add `RawNodeID` and `IsAnchorConceptNode` repoindex helpers
- add `IncludeAnchorConcepts bool` to `RepoGraphBuildOptions`, default false
- apply anchor concept filtering to both package-attached concept drafts and
  the global `ListNodesByKind(NodeConcept, ...)` concept-draft path
- apply the same filter before package concept counts/scores or package
  selection heuristics are computed
- add `DefaultExpandEdgeTypes()` as the structural default helper; do not expose
  `EdgeSetDefault = EdgeSetStructural` as a mutable alias
- add `AllEdgeTypes()` or a copy-safe `EdgeSetAll` helper for callers that
  intentionally want every edge type
- route `QueryEngine.Expand`, `QueryEngine.expandWeighted`,
  `QueryEngine.DAGGrep`, repoquery adapters, CLI paths, agent tools,
  DAG/eval helper paths, and any `fetchEdges` caller through
  `NormalizeExpandOptions` or `NormalizeDAGGrepRequest`
- split `IncludeAnchors` into `IncludeOwnerContainers` and
  `IncludeSemanticAnchors`; legacy `IncludeAnchors` maps to owner containers
  only
- rename or explicitly freeze result fields/CLI labels named `Anchors` as
  owner-container-only; semantic anchor output must use separate semantic
  anchor terminology
- ensure nil/default expansion does not include semantic or empirical edges

PR-B1 acceptance blockers:

- every repoindex expansion path with nil/empty edge types behaves as
  structural-only
- nil/empty DAG grep requests are normalized by `NormalizeDAGGrepRequest`
- legacy `IncludeAnchors` maps only to `IncludeOwnerContainers`
- Obsidian graph concept filtering excludes anchor concept nodes by default in
  both package draft and global concept draft paths
- Obsidian package concept counts/scores exclude anchor concept nodes by default

### PR-B2: Repoindex Graph Emission Behind Explicit Flag

Goal: emit semantic anchor graph edges only when requested.

Tasks:

- add a build option such as `IncludeSemanticAnchors bool`
- emit `owner node -> anchor concept node`
- attach full `SemanticAnchorEdgeMeta`
- construct edges through repoindex `NewSemanticAnchorEdge(res, owner)`; that
  adapter must consume `AnchorResolution` and use semanticanchors
  `NewSemanticAnchorEdgeMeta(res, owner)` internally
- use `DecodeSemanticAnchorEdgeMeta` and `ValidateSemanticAnchorEdgeMeta`
- validate edge and metadata together through `ValidateSemanticAnchorEdge`
- read paths use `DecodeAndValidateSemanticAnchorEdge` or
  `FilterValidSemanticAnchorEdges` before rendering semantic evidence
- public semantic read paths use `ProjectSemanticAnchorEdges` so invalid
  metadata produces warnings and is not rendered as semantic evidence
- graph emission consumes `AnchorResolution.EdgeAction`; it must not re-resolve
  raw anchors independently
- graph emission and edge metadata use `AnchorResolution.Relation` as the
  emitted relation; missing targets and unresolved fragments/symbols must
  already have `Relation = DECLARES_ANCHOR_TARGET`
- `NewSemanticAnchorEdgeMeta` rejects mismatches between
  `res.Occurrence.OwnerBinding` and the supplied `owner`
- fail closed on malformed metadata or illegal allowed authority effects
- malformed, unsafe, secret-like, path-traversal, URL, env-var, and scoped
  doc/test anchors emit lint findings only and no repoindex edge
- emit `VERIFIED_BY` and `DESCRIBED_BY` only for resolved `test:` and `doc:`
  targets; unresolved path targets emit `DECLARES_ANCHOR_TARGET` with
  `validation_status=missing_target`
- keep existing `Index:` parsing untouched
- do not emit `EdgeBeaconFor` in narrow PR-B

### PR-B3: Query and Explain Traversal Proof

Goal: prove traversal semantics and avoid `IncludeAnchors` ambiguity.

Tasks:

- split DAG/expand request flags into `IncludeOwnerContainers` and
  `IncludeSemanticAnchors`
- prove symbol -> anchor via outgoing traversal
- prove anchor -> symbol via incoming traversal
- keep low-level graph direction explicit
- keep default structural traversal unchanged
- prove Obsidian graph build excludes anchor concepts by default
- prove every semantic-anchor query, DAG, and explain path validates edge
  metadata with `DecodeAndValidateSemanticAnchorEdge` or
  `FilterValidSemanticAnchorEdges` before rendering semantic evidence
- prove `ProjectSemanticAnchorEdges` returns valid semantic edges plus warnings
  and that public semantic projections use it

### PR-B4: Small Eval Fixture

Goal: create the acceptance gate before embeddings and ACA ranking.

Tasks:

- add five positive query fixtures
- add three negative/control query fixtures
- verify explainability and no broad-anchor hijack
- treat this as a hard gate before PR-C searchindex envelope work or PR-D ACA
  retrieval blending

### PR-B4 Acceptance: Anchor-Aware Repoindex Eval Fixture

Goal: prove graph behavior before touching embeddings.

Tasks:

- prove anchor concept nodes are searchable
- prove expand works symbol -> anchor through outgoing traversal
- prove expand works anchor -> symbol through incoming traversal
- prove DAG grep can include semantic anchors separately from owner containers
- prove explain output is understandable
- prove existing `Index:` edges still work
- prove existing `Index:` comment edges still appear when semantic anchor
  emission is disabled and when it is enabled
- prove no current repoindex evals regress

### PR-A/PR-B Integration Boundary

Keep PR-A and narrow PR-B away from later integration surfaces:

- no `searchindex.BuildCodeOptions.EnvelopeProvider` until PR-C
- no `contextplane.RetrievalOptions.UseSemanticAnchors` until PR-D
- no `memorycore.SourceLaneRepoSemanticAnchor` behavior until target-promotion
  work
- no shared git co-change refactor until PR-B.5
- no `PolicyKindSemanticAnchorPatch` behavior until PR-E

PR-A/PR-B are parser, lint, graph vocabulary, graph emission behind a flag,
traversal, explain, and eval only.

### PR-B.5: Empirical Git Layer

Goal: expose co-change, freshness, and volatility as inspect-only empirical
signals.

PR-B.5 is not a dependency for PR-B4. Keep it future-only until PR-A and narrow
PR-B are stable.

Tasks:

- compute file-level co-change from recent git history
- extract shared co-change collection/scoring to a lower-level intelligence
  package instead of importing contextplane from repoindex
- cap top-N neighbors per file
- downweight giant commits, formatting commits, lockfiles, and generated files
- record freshness and volatility metadata
- expose inspect output
- optionally expose top-K virtual `CO_CHANGES_WITH` edges
- reuse the PR-B1 `EdgeSetEmpirical` and reserved `EdgeCoChangesWith` constant;
  PR-B.5 adds the first real co-change edge emitter and keeps it out of
  structural/default traversal
- do not change ACA ranking by default
- do not change embeddings by default
- do not create source anchors automatically

### PR-C: Semantic Envelope Builder

Goal: embed richer retrieval documents.

Tasks:

- extend `embeddingtext.SymbolInfo` or add a neighboring envelope type with
  semantic anchors, linked docs/tests, selected graph neighbors, and optional
  capped co-change metadata
- add an optional `CodeEnvelopeProvider` seam for searchindex enrichment
- keep co-change neighbors in metadata by default, not embedding text
- keep review cautions out of embedding text in the first slice
- update searchindex document building to include anchor text and metadata when
  the provider is present
- include anchor and envelope digests in embedding invalidation
- add deterministic envelope golden tests
- prove raw code plus anchors retrieves better than raw code alone on a small
  fixture suite

### PR-D: ACA Retrieval Blend Behind Flags and Evals

Goal: route task queries through anchors when useful.

Tasks:

- add anchor graph hits to `context retrieve` behind an explicit option
- require context assembly to call `evidence.ValidateRenderSurface` for every
  semantic-anchor evidence node before it can be rendered into any context
  bundle
- require context assembly to call `evidence.ValidateEvidenceMeta` or consume a
  validated wrapper produced by source-specific constructors before
  `ValidateRenderSurface`; tuple coherence must be preserved
- require semantic-anchor evidence to enter context bundles through a typed
  `EvidenceMeta` field or validated wrapper, not loose `map[string]any`
  metadata
- expose anchor-derived code hints in retrieval inspect output
- classify retrieval misses caused by missing/stale anchors
- add an eval suite with queries like "where is read-before-write enforced?"
- evaluate whether co-change and freshness boosts improve ranking
- keep lexical and graph behavior useful when embeddings are unavailable

### PR-E: Anchor Proposals and Curator

Goal: prevent comment spam and stale anchors.

Tasks:

- add `semantic_anchor_patch` proposal kind or equivalent
- let retrieval inspection and linter findings create deduped proposals
- track proposed change, source refs, confidence, blast radius, and review
  requirement
- add apply/reject surfaces that produce normal source diffs
- keep source rewrites review-gated by default
- surface co-change hypotheses as inspect findings, not automatic source edits

### PR-F: Obsidian Bridge and Concept Notes

Goal: connect anchors to durable notes without storing prose in code.

The default exclusion filter for anchor concept nodes is already implemented.
The remaining PR-F work is reviewed inbox-first concept-note generation and
bridge reconciliation.

Tasks:

- let `obsidian graph build` draft concept notes for high-value anchors
- let bridge reconciliation suggest `repo_anchors`, `repo_symbols`, and
  `repo_docs` backlinks
- add health checks for orphaned anchors and missing canonical notes
- keep all generated note changes inbox-first and reviewed

### PR-G: Agent Workflow and PR Review Surface

Goal: make anchor awareness routine for agents.

Tasks:

- add touched-file anchor lint to the developer loop
- add graph diff output for PRs or agent finals
- retrieve linked test contracts for touched anchors
- warn about likely co-change neighbors
- add review checks for trust-critical anchors changed without tests
- document agent rules for when to add, update, or remove anchors

## Required Tests

### PR-A1a Evidence Package Tests

Required validator cases:

- rejects empty effect lists, duplicate effects, unknown enum strings, and
  `AuthorityEffectNone` combined with any other effect
- permits `AuthorityEffectNone` only as exclusive non-actionable evidence-pack
  metadata
- validates tuple coherence for structural, semantic-anchor, git empirical, and
  durable-memory source shapes
- rejects structural, semantic-anchor, and git empirical metadata containing
  `instruction_source`
- rejects semantic-anchor source metadata pretending to be durable policy
- rejects git source metadata pretending to be semantic or durable
- rejects structural source metadata with the wrong class or authority
- rejects git empirical `review_signal` and permits git empirical
  `review_warning`
- permits durable reviewed/policy `review_signal`
- permits durable context-signal/reviewed/policy `retrieval_ranking`
- permits durable reviewed/policy `instruction_source`
- rejects durable `instruction_source` when authority is `context_signal`
- exposes typed validation errors with stable reason codes

Required render-surface cases:

- `RenderSurfaceEvidencePack` accepts valid structural, semantic-anchor, git
  empirical, and durable metadata, including `none`
- `RenderSurfaceEvidenceHint` accepts only evidence-labeled effects and rejects
  `none`
- `RenderSurfaceEvidenceHint` rejects metadata containing `instruction_source`,
  even when another compatible effect is also present
- `RenderSurfaceReview` accepts structural, semantic-anchor, and durable
  reviewed/policy `review_signal`
- `RenderSurfaceReview` rejects git empirical evidence
- `RenderSurfaceReviewWarning` accepts git empirical `review_warning` and
  rejects normal review signals
- instruction-sensitive surfaces accept only generic durable
  `instruction_source`
- semantic, structural, and git evidence fail `instruction`, `policy`,
  `hard_constraint`, `tool_authorization`, and `runtime_guardrail`

Required import guard:

- `internal/intelligence/evidence` must not import repoindex, searchindex,
  semanticanchors, contextplane, memorycore, storage, Obsidian tooling, or
  `internal/v2`

### PR-A1a Contextplane Memory Gate Tests

Positive cases:

- active reviewed `memorycore.KindPolicyRule`, `InstructionEligible=true`,
  `EvidenceOnly=false`, not tainted, not superseded, no expiry: passes
  `instruction`
- active validated `memorycore.KindProceduralSkill` under the same usage/trust
  conditions: passes `instruction`
- valid records pass `policy`, `hard_constraint`, `tool_authorization`, and
  `runtime_guardrail`

Negative cases:

- rejects `evidence_pack`, `evidence_hint`, `review`, and `review_warning`
- rejects zero `now`
- rejects non-active lifecycle states
- rejects unreviewed, needs-review, and failed-validation records
- rejects all kinds except `memorycore.KindPolicyRule` and
  `memorycore.KindProceduralSkill`
- rejects `InstructionEligible=false`
- rejects `EvidenceOnly=true`
- rejects non-empty `SupersededBy`
- does not reject solely because `Supersedes` is non-empty
- rejects `Trust.Tainted=true`
- rejects present-but-unparsable `ValidUntil`
- rejects `now == ValidUntil` and `now > ValidUntil`; accepts
  `now < ValidUntil`
- rejects `TTLSeconds > 0` with missing or unparsable TTL base
- rejects `now == base + TTLSeconds` and `now > base + TTLSeconds`; accepts
  `now < base + TTLSeconds`
- verifies TTL base precedence: `ValidFrom`, then `ObservedAt`, then
  `IngestedAt`
- verifies default named-memory/context-claim records are rejected because they
  are evidence-only and instruction-ineligible

Tests that can wait for later PRs:

- parser grammar, source comment extraction, owner binding, redaction of anchor
  syntax, `AnchorResolution`, semantic-anchor edge metadata, repoindex edge
  sets and DAG behavior, Obsidian anchor-concept filtering, searchindex
  semantic envelope, ACA retrieval blend, co-change scoring, and source
  rewrite/proposal flows

### PR-A Parser Tests

Required cases:

- valid scoped anchor: `[[foxctl:invariant/no-send-without-read]]`
- valid unscoped anchor: `[[invariant:no-send-without-read]]`
- unscoped concept anchor canonicalizes to an `AnchorTargetID` under
  `anchor:repo:*` plus a `RepoScopedAnchorKey` carrying `repoKey`
- unknown scope is a lint finding
- valid doc anchor: `[[doc:docs/general/tmux-collaboration.md#room-access]]`
- valid test anchor:
  `[[test:internal/runtime/terminal/tmuxbridge/client_test.go#TestReadBeforeWrite]]`
- scoped `doc` and scoped `test` anchors rejected:
  - `[[foxctl:doc/docs/foo.md]]`
  - `[[foxctl:test/internal/foo_test.go#TestX]]`
- multiple anchors on the same owner
- anchors in Go, TypeScript, Python, and Rust source-code comments
- arbitrary Markdown wikilinks ignored unless explicitly supported later
- anchors in string literals ignored
- valid `test:` target preserves uppercase test names
- concept anchors resolve by parser canonicalization and policy validation
  without requiring a durable memorycore or Obsidian target record
- doc/test target resolution is performed through `TargetResolver`, not graph
  emission code
- unresolved doc fragments or test symbols produce `AnchorFindingUnresolvedFragment`,
  `AnchorValidationMissingTarget`, `AnchorEdgeMissingTarget`, and only
  `DECLARES_ANCHOR_TARGET`
- valid-but-missing path anchors and unsafe path anchors are distinct:
  - syntactically valid missing file -> `AnchorEdgeMissingTarget` and
    `DECLARES_ANCHOR_TARGET`
  - path traversal, URL, absolute path, env var, or scoped path anchor ->
    `AnchorEdgeNone` and no edge
- missing doc/test files and unresolved fragments/symbols set
  `AnchorResolution.Relation = SemanticAnchorRelationDeclaresTarget`; the
  original intended relation is UX-only if stored at all
- metadata construction uses `AnchorResolution.Relation` even when `RawSyntax`
  appears to imply a different relation
- findings have stable IDs suitable for `LintFindingIDs`
- finding ID derivation excludes line numbers and timestamps
- duplicate anchors on one owner
- fuzz arbitrary `[[...]]` text to ensure no parser panic
- parser policy constants enforce maximum anchor/target byte lengths
- parser policy constants are not scattered as ad hoc regex literals
- `DefaultAnchorPolicy(repoKey, configuredScopes)` includes `foxctl`,
  repo-key-derived project slug, configured scopes, and the PR-A type allowlist
- finding reason precedence is deterministic for overlapping malformed inputs
- `NewAnchorTargetID` rejects `::`, concrete repo keys in repo-local targets,
  unknown scopes, unknown types, scoped path anchors, and malformed targets
- `AnchorTargetNodeID` and `DecodeAnchorTargetNodeID` round-trip repo-scoped
  anchor node IDs without double namespacing
- `AnchorTargetNodeID` rejects empty repo keys and repo keys containing `::`
- `AnchorTargetNodeID` outputs round-trip through `repoindex.SplitNamespacedID`
  once PR-B repoindex integration lands
- anchor target ID and repoindex node ID helpers do not double namespace anchors
- `internal/intelligence/evidence` import guard rejects imports from repoindex,
  searchindex, semanticanchors, contextplane, memorycore, storage, Obsidian
  tooling, or `internal/v2`
- semantic-anchor core import guard rejects imports from repoindex,
  searchindex, contextplane, memorycore, storage, Obsidian tooling, or
  `internal/v2`
- authority matrix forbids `instruction_source` in allowed effects outside
  durable knowledge
- `ValidateEvidenceMeta` rejects incoherent source/plane/class/authority
  tuples, including semantic-anchor source metadata pretending to be durable
  policy metadata
- evidence validation returns stable reason codes such as
  `incoherent_evidence_tuple`, `none_not_exclusive`, and
  `illegal_render_surface`
- evidence validators reject empty effect lists, duplicate effects, unknown
  enum strings, and `AuthorityEffectNone` combined with any other effect
- `AuthorityEffectNone` is exclusive and does not authorize review,
  evidence-hint, instruction, policy, hard-constraint, tool-authorization, or
  runtime-guardrail rendering
- semantic anchor `AllowedAuthorityEffects` permits retrieval/review as maximum
  downstream use only; it does not make the edge active for those uses
- semantic anchor with allowed effects `retrieval_ranking` and `review_signal`
  still fails `ValidateRenderSurface` for instruction, policy,
  hard-constraint, tool-authorization, and runtime-guardrail surfaces
- `durable_knowledge` plus `instruction_source` may pass generic render
  legality, but instruction rendering still fails without
  `ValidateMemoryRecordForInstruction`
- `git_empirical` may use `retrieval_ranking` and `review_warning`, but not
  normal `review_signal` or `instruction_source`
- `ValidateRenderSurface` accepts git empirical review output only on
  `RenderSurfaceReviewWarning`, not normal `RenderSurfaceReview`
- `ValidateRenderSurface` accepts durable reviewed/policy `review_signal` on
  `RenderSurfaceReview`
- `RenderSurfaceEvidenceHint` is evidence-labeled context only and cannot be
  used as instruction, policy, hard constraint, tool authorization, or runtime
  guardrail text
- `RenderSurfaceEvidenceHint` rejects `instruction_source` and
  `AuthorityEffectNone`; `RenderSurfaceEvidencePack` may show `none` only as
  labeled non-actionable evidence with `permitted_use: none`
- `ValidateRenderSurface` may permit `durable_knowledge` plus
  `instruction_source` as a generic legality check, but instruction rendering
  still fails without the separate memory-aware
  `ValidateMemoryRecordForInstruction` gate
- `ValidateMemoryRecordForInstruction` rejects zero `now`, inactive records,
  unreviewed records, statuses other than reviewed/validated,
  `InstructionEligible=false`, `EvidenceOnly=true`, and kinds other than
  `KindPolicyRule` or `KindProceduralSkill`
- `ValidateMemoryRecordForInstruction` rejects evidence-pack, evidence-hint,
  review, and review-warning surfaces; it only accepts instruction, policy,
  hard-constraint, tool-authorization, and runtime-guardrail surfaces
- `ValidateMemoryRecordForInstruction` rejects `SupersededBy`, taint,
  failed-validation, expired or unparsable `ValidUntil`, missing/unparsable TTL
  base times, and expired `TTLSeconds` records
- `ValidateMemoryRecordForInstruction` rejects equality boundaries:
  `now == ValidUntil` and `now == TTLBase + TTLSeconds`
- `ValidateMemoryRecordForInstruction` does not reject solely because a record
  supersedes another record
- `ValidateRenderSurface` does not import or inspect memorycore; durable
  instruction eligibility requires a separate memory-aware validator in the
  context/memory assembly package
- rendering barrier forbids semantic/structural/git evidence in instruction
  surfaces
- rendering barrier forbids semantic/structural/git evidence in policy,
  hard-constraint, tool-authorization, and runtime-guardrail surfaces
- malformed anchors:
  - `[[foxctl:Invariant/NoSend]]`
  - `[[risk:]]`
  - `[[foo:unknown-type/bar]]`
  - `[[foo:bar]]` -> `AnchorFindingUnknownScope`
  - `[[knownscope:unknown/target]]` -> `AnchorFindingUnknownType`
  - `[[doc:/absolute/path.md]]`
  - `[[doc:../../secret.md]]`
  - `[[doc:https://example.com/thing.md]]`
  - `[[doc:docs\\general\\tmux-collaboration.md]]`
  - `[[doc:C:\\Users\\secret.md]]`
  - `[[doc:docs/general:bad.md]]`
  - `[[invariant:foo::bar]]`
  - `[[policy:sk_live_...]]`
  - `[[session:2026-05-05-terminal-output]]`
- lint/explain output redacts raw target text for secret-like, PII-like,
  token-like, terminal-output-like, and unsafe anchor values
- unsafe-anchor public hashes are redacted/non-secret or hidden; no CLI/explain
  output exposes direct hashes of raw secret-bearing syntax
- public `AnchorOccurrence` JSON contains only safe display fields; raw syntax
  and raw target bytes are either held in `AnchorOccurrenceInternal` or tagged
  `json:"-"`
- `Finding.Message` uses reason enums and safe display fields only

### PR-A Linter Tests

Required findings:

- unknown anchor type
- uppercase slug
- empty slug
- transient/session-like slug
- absolute path
- path traversal
- doc target missing
- test target missing
- unknown scope
- scoped `doc`/`test`
- scoped path anchor
- unknown scope
- unknown type
- malformed target
- namespace collision
- backslash or control character in path target
- duplicate owner/target
- beacon without support anchor
- too many anchors on one owner
- too many beacon anchors on one owner
- anchor in generated/vendor file
- anchor not bound to symbol and not valid as file-level anchor
- inline long-form agent note adjacent to anchor
- severity is `error` or `warning` according to the PR-A severity table

### PR-A Symbol Binding Tests

Required binding cases:

- anchor directly above function
- anchor above method
- multiple anchors above one symbol
- anchor separated by blank/comment lines
- anchor separated by more than one blank line should not bind
- anchor where the next symbol starts more than 3 lines away should not bind
- anchor separated by a code line should not bind
- top-of-file file-level anchor
- invalid file-level `test`/`invariant` anchor
- two adjacent symbols
- block comment anchors
- anchors in Go, TypeScript, Python, and Rust comment forms
- Python decorators and TypeScript decorators are treated as symbol prelude
- Rust attributes are treated as symbol prelude
- Go build tags/package docs are handled in the top-of-file prelude
- Go package docs/build tags do not accidentally bind to the first function
- anchor-only comment lines are stripped from normal symbol documentation
- anchor-only comment lines are stripped before existing `DocIndex` parsing and
  before embedding text generation
- unsupported declaration forms produce unbound lint findings
- owner identity golden join fixtures run repoindex extraction and semantic
  anchor binding, then assert the anchor owner resolves to the same repoindex
  node ID for:
  - Go function
  - Go method
  - TypeScript function and class method
  - Python function and class method
  - Rust function and impl method
  - file-level owner
- a language is enabled for graph binding only when its golden repoindex-owner
  join fixture passes

### PR-B Repoindex Tests

Required graph cases:

- anchor concept node is created
- semantic edge is created with correct type
- edge metadata includes file, line, syntax, extraction version, target type, and
  scope
- edge metadata includes source plane, evidence class, evidence authority,
  allowed authority effects, owner node, relation, parser version, line span,
  and validation status
- edge metadata includes occurrence ID, owner stable key, and source hash
- edge metadata includes max finding severity when lint finding IDs are present
- edge metadata constructor assigns `SemanticAnchorEdgeMetaSchemaVersion`, and
  validation rejects unknown schema versions
- edge meta decode/validate helper rejects malformed metadata or illegal
  allowed authority effects
- `AnchorEdgeNone` produces zero semantic anchor nodes and zero semantic anchor
  edges
- `DecodeAndValidateSemanticAnchorEdge` and `FilterValidSemanticAnchorEdges`
  fail closed on malformed metadata
- malicious metadata that adds `instruction_source` to a semantic anchor's
  allowed effects is rejected by every semantic-anchor read path
- public repoindex semantic projections validate before rendering in repoquery
  semantic projection, CLI output, agent-tool semantic output, DAG/expand
  explain surfaces, and anchor explain
- `QueryEngine.Expand` and `QueryEngine.DAGGrep` remain raw graph outputs and
  only normalize traversal defaults
- `ProjectSemanticAnchorEdges` returns warnings for invalid semantic-anchor
  metadata and public projections surface those warnings
- semantic projection warnings use `SemanticProjectionWarningReason`, not
  prose-only matching
- invalid semantic-anchor edges are dropped from semantic projections or
  rendered only as opaque graph edges with no evidence authority fields
- raw `Expand`/`DAGGrep` renderers do not label invalid semantic-anchor edges
  with authority, permitted use, evidence plane, semantic-anchor relation
  wording, or instruction eligibility
- malicious metadata that injects `instruction_source` is rejected by every
  semantic projection surface, not only by the metadata codec
- `ValidateSemanticAnchorEdge` rejects mismatched edge type, source owner,
  destination anchor node ID, source plane, evidence authority, and allowed
  authority effects
- missing `doc:`/`test:` targets emit `DECLARES_ANCHOR_TARGET` with
  `validation_status=missing_target` and `Relation=DECLARES_ANCHOR_TARGET`,
  not `DESCRIBED_BY` or `VERIFIED_BY`
- existing `Index:` comment edges still work
- anchor and `Index:` edges can coexist
- ambiguous `Related` symbol resolution remains skipped
- anchor target search finds owner symbol/file
- expand from symbol reaches anchor concept through outgoing semantic traversal
- expand from anchor concept reaches owner symbol/file through incoming semantic traversal
- DAG grep can include semantic anchor nodes
- DAG grep can include semantic anchors separately from owner containers
- default structural edge traversal remains unchanged
- nil/default repoindex expansion excludes semantic and empirical edges
- nil/default expansion excludes `ENFORCES`, `PROTECTS_AGAINST`,
  `VERIFIED_BY`, `DESCRIBED_BY`, `DECLARES_ANCHOR_TARGET`, and
  `CO_CHANGES_WITH`
- nil/default expansion uses `DefaultExpandEdgeTypes()` or an explicitly copied
  default on `QueryEngine.Expand`, `QueryEngine.expandWeighted`,
  `QueryEngine.DAGGrep`, repoquery adapters, CLI paths, agent tools, and any
  `fetchEdges` caller
- `IncludeSemanticAnchors=true` appends `EdgeSetSemanticAnchors` after default
  or explicit edge-type normalization and deduplicates edge types
- `IncludeSemanticAnchors=false` never adds semantic edges unless semantic edge
  types were explicitly requested
- legacy `IncludeAnchors=true` maps only to owner containers and never changes
  edge types
- `fetchEdges` defensively normalizes empty edge filters before store reads,
  even if callers missed normalization
- callers that want every edge type use `AllEdgeTypes()` or a copy-safe
  `EdgeSetAll` helper explicitly, not nil or an empty edge filter
- `RawNodeID` and `IsAnchorConceptNode` identify namespaced anchor concepts
- file-level fallback works when no symbol owner exists
- `EdgeBeaconFor` is not emitted in narrow PR-B
- explain output reports parsed beacon anchors as not indexed in this version
- explain output never labels missing `doc:`/`test:` targets as valid
  documentation or verification
- Obsidian graph build excludes anchor concepts by default
- `obsidian graph build` emits zero anchor concept notes by default after
  semantic anchors are indexed
- Obsidian graph filtering applies to both package-attached concept drafts and
  the global `ListNodesByKind(NodeConcept, ...)` concept-draft path
- Obsidian package concept counts/scores and package-selection heuristics
  exclude anchor concepts by default

Fixture:

```go
// [[foxctl:invariant/no-send-without-read]]
// [[foxctl:risk/agent-terminal-desync]]
// [[test:internal/runtime/terminal/tmuxbridge/client_test.go#TestReadBeforeWrite]]
func (b *Bridge) Type(...) error { ... }
```

Expected edges:

```text
Bridge.Type ENFORCES anchor:foxctl:invariant:no-send-without-read
Bridge.Type PROTECTS_AGAINST anchor:foxctl:risk:agent-terminal-desync
Bridge.Type VERIFIED_BY anchor:repo:test:internal/runtime/terminal/tmuxbridge/client_test.go#TestReadBeforeWrite
```

### PR-B.5 Co-Change Tests

Use a temporary git repo fixture.

Required cases:

- A and B co-change repeatedly, producing a high score
- A and B only co-change in one giant formatting commit, producing a low score
- commits above the giant hard limit are skipped
- lockfile/generated files are skipped or downweighted
- recent commits outweigh old commits
- injected `Now` controls time-dependent scoring
- max files per commit cap works
- top-N per file cap works
- co-change is symmetric
- staged/uncommitted files do not corrupt committed co-change history
- freshness status changes when HEAD differs from indexed HEAD

### PR-C Envelope and Digest Tests

Required cases:

- envelope ordering is deterministic
- digest changes when anchor target changes
- digest changes when relation changes
- digest changes when linked doc/test target changes
- digest changes when provider version, cap config, or included section flags
  change
- digest does not change when an anchor line moves but owner and target stay the
  same
- digest does not include timestamps, scores, or commit hashes
- existing symbol digest tests still pass
- semantic source-anchor types do not alias or reuse `searchindex.Anchor`
- no raw memorycore, proposal, terminal, or unreviewed agent-note content enters
  envelope text
- co-change neighbors stay out of embedding text by default
- review cautions stay out of embedding text in the first PR-C slice

### Early Eval Fixtures

Add a small retrieval/DAG suite before ranking changes.

Queries:

```text
where is read before write enforced?
what protects against agent terminal desync?
which tests verify terminal send safety?
where is tmux bridge safety described?
what files usually change with tmuxbridge client?
```

Expected paths:

```text
internal/runtime/terminal/tmuxbridge/*
cmd/foxctl/cmd/tmux.go
related tests
docs/general/tmux-collaboration.md if linked
```

Negative/control queries:

```text
terminal styling
generic graph search
memory curator stale records
```

These should not be hijacked by broad anchors. Include at least one noisy-type
control for each of:

- `beacon`
- `domain`
- `protocol`
- `risk`

Existing repoindex DAG/search evals must pass with semantic anchors enabled and
default traversal unchanged.

## Verification Matrix

| Area | Required checks |
|------|-----------------|
| Docs | `make check-doc-links` |
| Parser | malformed/valid inline anchors, doc targets, test targets |
| Linter | invalid types, unsafe paths, duplicate anchors, unsupported beacons, owner caps |
| Symbol binding | anchors bind to nearest symbol or file deterministically |
| Repoindex | anchor nodes/edges appear in search/expand/DAG output with plane metadata |
| Co-change | temp git fixture covers recency, giant commits, caps, symmetry |
| Embedding envelope | golden tests prove stable ordering, provider behavior, and digest behavior |
| Retrieval | anchor-aware queries improve fixture suite results behind flags |
| ACA proposals | duplicate findings dedupe into one proposal |
| Obsidian bridge | generated anchor notes remain inbox-first |
| Safety | secrets/transient values are rejected by lint |

## Risks

### Risk: Source Files Become Agent Graffiti

Mitigation: inline anchors are tiny and typed. Block relation syntax and
long-form comments stay outside the current goal until evals justify them.
Longer notes live in ACA proposals, Obsidian notes, docs, or memorycore records.

### Risk: Bad Anchors Become False Authority

Mitigation: anchors are evidence until linked to reviewed docs, tests,
decisions, or validated memory. Retrieval should surface trust metadata.

### Risk: Co-Change Is Mistaken for Intent

Mitigation: co-change lives in the empirical layer. It can warn or suggest, but
it never creates source anchors or authority automatically.

### Risk: Taxonomy Fragmentation

Mitigation: start with a small allowlist and require parser/retrieval tests for
new anchor types.

### Risk: Embeddings Become Noisier

Mitigation: embed deterministic envelopes with capped lists and stable ordering.
Keep volatile co-change scores and freshness timestamps in metadata.

### Risk: Refactors Leave Anchors Stale

Mitigation: bind anchors to symbol IDs/spans, lint touched files, and let curator
propose stale-anchor cleanup.

### Risk: Parallel Graph Store Appears

Mitigation: PR-B extends repoindex first. Only add dedicated anchor tables if
occurrence lifecycle cannot fit in repoindex metadata.

### Risk: Parser Becomes Regex-Over-Source

Mitigation: PR-A extracts anchors only from language comments and binds them to
owners by span. Regex may parse already-isolated comment text, but it must not
scan arbitrary source text, string literals, or Markdown prose.

### Risk: Searchindex Never Sees Repoindex Anchors

Mitigation: PR-C adds an optional `CodeEnvelopeProvider` enrichment seam.
Searchindex consumes deterministic envelope bits instead of importing repoindex,
ACA, Obsidian, memorycore, or git directly.

### Risk: All Signals Become Scores

Mitigation: semantic and empirical edges carry mandatory evidence plane,
authority, and allowed authority-effect fields. Rankers may blend retrieval
scores, but instruction eligibility can only come from explicit durable memory
or active policy metadata.

## Reject Criteria

A patch should be rejected if it:

- adds long-form agent analysis to source comments
- scans arbitrary source/Markdown text for anchors instead of extracting from
  comments
- parses block relation syntax in PR-A
- allows scoped `doc` or scoped `test` anchors in PR-A
- silently accepts unknown scopes
- treats anchors as instructions without lifecycle/trust review
- allows semantic anchors, git empirical signals, or structural graph facts to
  include `AuthorityEffectInstructionSource` in allowed effects
- creates a new graph database instead of extending repoindex/searchindex first
- overloads `Node.Meta` with mixed anchor/doc JSON without an explicit wrapper
- uses untyped freeform comments as graph edges
- puts anchor extraction or retrieval under `internal/v2`
- silently rewrites source comments from an agent or daemon
- makes embeddings required for anchor correctness
- accepts secrets, PII, tokens, terminal output, URLs, absolute paths, path
  traversal, or transient session IDs in anchor targets
- emits repoindex edges for malformed, unsafe, secret-like, path-traversal,
  URL, env-var, or scoped doc/test anchors
- adds `principle`, `policy`, `event`, or `resource` to the PR-A allowlist
  without explicit tests and review
- adds a new anchor type without tests and relation mapping
- lets co-change automatically create or upgrade semantic anchors
- stores durable target authority in semantic anchor edge metadata
- adds semantic anchors or empirical edges to default structural traversal

## Remaining Work

This is now the active next-step backlog. Do not treat these items as deferred
by default; keep each item behind explicit flags, review gates, or eval proof
until verification says it is safe to broaden.

### Complete The Acceptance Gate

- Add the PR-B4 eval suite with five positive and three negative/control
  queries.
- Include controls for beacon, domain, protocol, and risk anchors.
- Prove default traversal is unchanged and semantic anchors do not hijack broad
  queries.
- Run existing repoindex DAG/search evals with semantic anchors enabled.

### Finish Language Owner Binding

- Add repoindex owner-join fixtures for TypeScript, Python, and Rust.
- Promote a language from lint-only to graph-binding only after its golden join
  passes.
- Keep Go as the currently proven graph-binding language.

### Harden CLI Lint And Explain

- Add golden output tests for redaction, missing doc/test targets, unresolved
  fragments, unbound owners, beacon-not-indexed wording, and evidence authority
  fields.
- Ensure unsafe raw syntax or hash text does not leak.

### Complete Empirical Co-Change Proof

- Add temp-git fixture tests for repeated co-change, giant formatting commits,
  hard limits, lockfile/generated skip or downweight, recency, injected `Now`,
  caps, symmetry, and freshness.
- Keep `--cochange` explicit and keep empirical edges out of structural
  defaults.

### Complete Semantic Envelope Proof

- Finish digest/golden tests for anchor target, relation, linked doc/test
  target, provider version, cap config, and section flags.
- Prove co-change remains metadata-only by default.
- Prove semantic source-anchor types do not alias `searchindex.Anchor`.

### Complete ACA Retrieval And Proposal Proof

- Add an anchor-aware retrieval eval suite for
  `context retrieve --semantic-anchors`.
- Prove semantic hints validate evidence metadata before rendering.
- Prove missing/stale semantic anchor inspect classifications create deduped
  `semantic_anchor_patch` proposals.
- Keep source diffs review-gated.

### Complete Obsidian Bridge Work

- Generate inbox-first concept-note drafts for high-value anchors.
- Add bridge reconciliation for `repo_anchors`, `repo_symbols`, and `repo_docs`
  backlinks.
- Add health checks for orphaned anchors and missing canonical notes.

### Complete Agent Workflow

- Add graph diff output for PRs or agent finals.
- Retrieve linked test contracts for touched anchors.
- Warn when trust-critical anchors changed without tests.
- Keep `semantic-commenting` as the portable skill contract and keep examples
  repo-local by default.

## Resolved Direction

1. Anchor targets stay typed concept nodes for the current goal. Dedicated
   occurrence tables can be added only if repoindex metadata cannot support
   lifecycle or curator needs.
2. Test anchors use repo-relative file paths with `#TestName` for now. Stable
   generated test-contract IDs are a later compatibility layer only after
   file-path anchors prove inadequate.
3. Agent-added anchor changes are allowed as normal reviewed source diffs, not
   daemon rewrites. Proposal flows may prepare patches, but application stays
   review-gated.
4. The first acceptance gate is the PR-B4 anchor-aware repoindex eval fixture,
   followed by ACA retrieval evals.
5. Co-change lives in repoindex as explicit empirical edges and ACA as retrieval
   priors; it must remain out of structural defaults and out of embedding text
   by default.

## Recommended Next Slice

Complete PR-B4, PR-C, and PR-D proof together as one verification-focused slice
before broadening authority or automation.

Concrete files/contracts:

```text
internal/tooling/evals/retrievaleval/
internal/intelligence/indexing/repoindex/
internal/intelligence/repoquery/
internal/intelligence/searchindex/
internal/context/contextplane/
cmd/foxctl/cmd/
```

Implement only:

1. anchor-aware repoindex eval suite
2. missing negative/control eval coverage
3. semantic envelope digest/golden coverage
4. ACA retrieval eval coverage behind `--semantic-anchors`
5. proposal dedupe/review proof for semantic-anchor patches
6. CLI lint/explain golden hardening
7. temp-git co-change fixture coverage if it blocks PR-D confidence

Explicit non-goals for the next slice:

- no block relation syntax
- no new anchor types
- no policy-bearing anchors
- no automatic source rewrites
- no semantic or empirical edges in default structural traversal
- no co-change text in embeddings by default
- no instruction or policy authority from semantic anchors
- no broad Markdown anchor parsing
- no automatic Obsidian note writes outside reviewed inbox flows

Implementation invariant:

> Anchors make intent findable; they do not make intent authoritative.

Expanded:

> Anchors make intent findable. Co-change makes history inspectable. Freshness
> makes index state visible. Durable memory makes reviewed knowledge reusable.
> Only explicit active policy or validated procedural memory may shape behavior
> as instruction.

First-PR invariant:

```text
Semantic anchors may affect retrieval and review surfaces.
They may not affect agent behavior as instructions unless separately promoted
through durable memorycore policy with explicit instruction eligibility.
```
