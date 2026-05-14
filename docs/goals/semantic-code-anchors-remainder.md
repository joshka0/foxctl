# Goal: Complete Semantic Code Anchors Remainder

## Goal

Complete the remaining semantic-code-anchors implementation described in
`docs/plans/features/semantic-code-anchors.md`. Treat the remaining items as the
next active implementation step, not deferred work, while preserving the
authority, traversal, and review-gate invariants.

## Context

- Plan: `docs/plans/features/semantic-code-anchors.md`
- Branch: `feat/semantic-code-anchors`
- Already landed or partially implemented:
  - evidence metadata and memory instruction gate
  - semantic anchor parser, canonical IDs, authority metadata, and render-barrier
    tests
  - Go owner binding and repoindex graph emission behind explicit flags
  - repoindex semantic traversal vocabulary and filtering
  - semantic search envelope plumbing
  - ContextWiki retrieval hints behind `--semantic-anchors`
  - semantic anchor inspect/proposal plumbing
  - Obsidian default exclusion filter for anchor concept nodes
  - portable `semantic-commenting` skill contract
- Leave unrelated work alone, including
  `internal/interfaces/web/api/sink.go` unless the goal is explicitly expanded.

## Milestones

1. Complete the PR-B4 acceptance gate.
   - Add five positive and three negative/control anchor-aware repoindex eval
     queries.
   - Include beacon, domain, protocol, and risk controls.
   - Prove default traversal remains unchanged.
   - Prove broad queries are not hijacked by semantic anchors.
2. Finish language owner binding.
   - Add TypeScript, Python, and Rust owner-join fixtures.
   - Keep each non-Go language lint-only until its owner-join golden passes.
3. Harden CLI lint and explain output.
   - Add golden coverage for redaction, missing doc/test targets, unresolved
     fragments, unbound owners, beacon-not-indexed wording, and evidence
     authority fields.
   - Ensure unsafe raw syntax and hash text do not leak.
4. Complete empirical co-change proof.
   - Add temp-git fixture coverage for repeated co-change, giant formatting
     commits, hard limits, lockfile/generated skip or downweight, recency,
     injected `Now`, caps, symmetry, and freshness.
   - Keep `--cochange` explicit and out of structural defaults.
5. Complete semantic envelope proof.
   - Add digest/golden tests for anchor target, relation, linked doc/test
     target, provider version, cap config, and section flags.
   - Prove co-change remains metadata-only by default.
   - Prove semantic source-anchor types do not alias `searchindex.Anchor`.
6. Complete ContextWiki retrieval and proposal proof.
   - Add retrieval eval coverage for `context retrieve --semantic-anchors`.
   - Prove semantic hints validate evidence metadata before rendering.
   - Prove stale/missing anchor inspect classifications create deduped
     `semantic_anchor_patch` proposals.
7. Complete Obsidian and agent workflow work.
   - Generate inbox-first concept-note drafts for high-value anchors.
   - Add bridge reconciliation for `repo_anchors`, `repo_symbols`, and
     `repo_docs` backlinks.
   - Add health checks for orphaned anchors and missing canonical notes.
   - Add graph diff output for PRs or agent finals.
   - Retrieve linked test contracts for touched anchors.
   - Warn when trust-critical anchors changed without tests.

## Constraints

- Anchors make intent findable; they do not make intent authoritative.
- Semantic anchors may affect retrieval and review surfaces only.
- Semantic anchors, empirical signals, and structural graph facts must not gain
  `AuthorityEffectInstructionSource`.
- Do not add semantic or empirical edges to default structural traversal.
- Do not put co-change text in embeddings by default.
- Do not add block relation syntax, new anchor types, policy-bearing anchors, or
  broad Markdown anchor parsing in this goal.
- Do not apply automatic source rewrites. Proposal flows may prepare reviewed
  diffs only.
- Preserve the canonical envelope shape and existing `meta.*` contract.
- Preserve WASI `capabilities.network: "none"` for skill/runtime manifest work.
- Do not introduce keyword heuristics for routing, classification, promotion, or
  suppression behavior. Use explicit schemas, typed signals, scored features, or
  tested policy instead.
- Before adding or moving `internal/*` packages, read
  `docs/architecture/package-topology.md` and explain the package boundary.

## Verification

Use focused tests as each milestone lands, then run the combined gate before
marking the goal complete:

```bash
GOWORK=off go test -count=1 ./internal/intelligence/evidence ./internal/context/contextplane ./internal/intelligence/indexing/semanticanchors ./internal/intelligence/indexing/repoindex ./internal/intelligence/repoquery ./internal/intelligence/searchindex
```

```bash
GOWORK=off go test -count=1 ./cmd/foxctl/cmd -run 'Test.*(Semantic|Anchor|Evidence|MemoryRecord|CoChange|Obsidian|ContextRetrieve|IndexRepo)'
```

```bash
make check-doc-links
git diff --check
```

When docs, repo graph metadata, or bridge metadata changes, also refresh the ContextWiki
knowledge layer using the commands in `AGENTS.md` if the needed vault paths are
available.

## Done Criteria

- The plan's remaining-work items have matching code/tests or are explicitly
  moved to a new reviewed follow-up with rationale.
- Anchor-aware repoindex evals, semantic envelope goldens, ContextWiki retrieval evals,
  CLI lint/explain goldens, and co-change fixture tests pass.
- Default structural traversal behavior is proven unchanged.
- No semantic anchor, empirical edge, or generated hint becomes an instruction
  source.
- Source rewrite behavior remains review-gated.
- `make check-doc-links` and `git diff --check` pass.

## Stop Conditions

- Stop and report if a change requires policy-bearing anchors or instruction
  authority from semantic anchors.
- Stop and report if implementation requires a new storage model before the
  repoindex/searchindex extension path is exhausted.
- Stop and report after three failed attempts at the same test failure with the
  exact command and failure summary.
