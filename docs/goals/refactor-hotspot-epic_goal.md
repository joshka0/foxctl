# Goal: Refactor Hotspot Epic

## Goal

Work through the current refactor-scout hotspot backlog as a multi-phase,
behavior-preserving refactor epic.

The objective is not to eliminate every numeric scout warning. The objective is
to turn the live high-confidence targets into deeper modules: smaller
interfaces, clearer seams, more locality, and tests that cover behavior rather
than private implementation details.

Each phase must:

1. Re-run the relevant scout command and confirm the phase target is still live.
2. Refactor only the target seam for that phase.
3. Preserve public CLI, skill, JSON, rule ID, evidence field, score, ordering,
   and manifest contracts unless explicitly listed in this goal.
4. Add or update focused behavior tests that would catch a realistic regression.
5. Run focused verification and then re-run the scout to record what moved.
6. Stop before broadening to unrelated files.

Use the same engineering standards as `$small-composable-code`,
`$ruthless-test-strategy`, and `$improve-codebase-architecture`: small
behavior-preserving changes, tests through stable interfaces, deletion-test
discipline, and architecture vocabulary based on module, interface,
implementation, depth, seam, adapter, leverage, and locality.

## Context

- Current branch context: `feat/rlm-helper-pipeline-hardening`.
- The worktree may contain unrelated RLM hardening, docs, generated validation,
  and prior refactor-scout changes. Preserve those changes.
- The current refactor-scout pipeline already supports target lanes:
  `small-composable-code`, `semantic-commenting`, and
  `improve-codebase-architecture`.
- Recent scout passes found these main target areas:
  - `skills/code_refactor_scout/main.go`
  - `skills/code_refactor_scout/semantic_bool*.go`
  - `skills/code_refactor_scout/ts_duplicate_recovery_cgo.go`
  - `skills/code_refactor_scout/elixir_slop_cgo.go`
  - `skills/code_refactor_scout/db_slop_elixir_cgo.go`
  - `skills/code_refactor_scout/deadcode.go`
  - `skills/code_refactor_scout/evidence.go`
  - `skills/code_refactor_scout/confidence.go`
  - `internal/intelligence/refactor/**`
  - selected command-handler families under `cmd/foxctl/cmd/**`
- The most recent top `skills/code_refactor_scout` targets were:
  - `main.go:216 run`
  - `main.go:1864 topStructuralSimilarityClustersByFile`
  - `main.go:1905 topCallFamilyClustersByFile`
  - `main.go:1950 topCallFamilyClustersByDirectory`
  - `main.go:2004 topStructuralSimilarityClustersByDirectory`
  - `main.go:2066 buildCallFamilyClusterInScope`
  - `main.go:2203 buildStructuralSimilarityClusterInScope`
  - `main.go:2731 finalizeSameFileExtractionFindings`
  - `main.go:3398 summarizeFiles`
  - `main.go:3450 summarizeSymbols`
  - `main.go:3572 buildEntrypointLane`
  - `main.go:4660 goStmtFingerprint`
  - `main.go:4914 detectGoBoolReturnWrapper`
- Relevant docs and review gates:
  - `AGENTS.md`
  - `docs/general/refactor-scout.md`
  - `docs/architecture/package-topology.md`
  - `docs/plans/features/refactor-intelligence-substrate.md`
  - `docs/plans/features/refactor-deterministic-detection-backlog.md`

## Global Constraints

- Do not add dependencies.
- Do not change protocol envelope fields or `meta.*`.
- Do not change WASI skill network policy.
- Do not rename commands, flags, skill manifest fields, rule IDs, evidence
  fields, JSON fields, or target strings unless this goal is updated first.
- Do not change repoindex schema or storage layout.
- Do not use keyword heuristics for routing, classification, promotion, or
  suppression. Prefer typed fields, rule IDs, scored features, and tests.
- Do not broaden into active RLM helper-factory hardening unless explicitly
  requested. Leave `internal/rlm/**` alone.
- Keep generated artifacts out of git unless they are intentionally documented.
- Before adding or moving anything under `internal/*`, read
  `docs/architecture/package-topology.md` and keep the work inside the existing
  family boundary.
- For `cmd/foxctl/cmd/**`, prefer extracting thin helper modules around one
  command family at a time. Do not introduce a generic command framework.
- For cgo-gated files, preserve stub behavior and run cgo tests when available.
- Stop after three failed attempts at the same check and summarize the blocker
  with exact command output.

## Milestones

### Phase 0: Baseline and Scout Snapshot

Done when:

- `git status --short --branch` has been inspected.
- Unrelated dirty files are identified and left untouched.
- The current scout binary builds:

```bash
GOWORK=off go build -o /tmp/code_refactor_scout ./skills/code_refactor_scout
```

- Baseline scout runs have been recorded for:

```bash
printf '%s\n' '{"path":"./skills/code_refactor_scout","language":"go","target":"all","min_score":50,"max_results":100,"view":"raw"}' \
  | FOXCTL_STORAGE_ROOT=/tmp/foxctl-refactor-hotspot-epic /tmp/code_refactor_scout

printf '%s\n' '{"path":"./internal/intelligence/refactor","language":"go","target":"all","min_score":50,"max_results":100,"view":"raw"}' \
  | FOXCTL_STORAGE_ROOT=/tmp/foxctl-refactor-hotspot-epic /tmp/code_refactor_scout

printf '%s\n' '{"path":"./cmd/foxctl/cmd","language":"go","target":"all","min_score":50,"max_results":100,"view":"raw"}' \
  | FOXCTL_STORAGE_ROOT=/tmp/foxctl-refactor-hotspot-epic /tmp/code_refactor_scout
```

- Focused baseline tests pass or known pre-existing failures are recorded:

```bash
GOWORK=off go test -count=1 ./skills/code_refactor_scout
GOWORK=off go test -count=1 ./internal/intelligence/refactor/changes ./internal/intelligence/refactor/hot ./internal/intelligence/refactor/scope ./internal/intelligence/refactor/status ./internal/intelligence/refactor/evidence
GOWORK=off go test -count=1 ./cmd/foxctl/cmd -run 'TestRefactor'
```

### Phase 1: Cluster Selection Module in `main.go`

Target files:

- `skills/code_refactor_scout/main.go`
- focused tests in `skills/code_refactor_scout/main_test.go` or a new focused
  test file in the same package

Targets:

- `topStructuralSimilarityClustersByFile`
- `topCallFamilyClustersByFile`
- `topCallFamilyClustersByDirectory`
- `topStructuralSimilarityClustersByDirectory`
- `buildCallFamilyClusterInScope`
- `buildStructuralSimilarityClusterInScope`

Done when:

- The repeated cluster-selection shape is concentrated behind a small private
  module or helper set.
- The public cluster finding shape is unchanged.
- Existing structural and call-family cluster tests remain behavior-focused.
- New tests cover deterministic ordering, best-per-scope selection, visited-key
  behavior, and directory/file-scope differences if existing tests do not.

Verification:

```bash
GOWORK=off go test -count=1 ./skills/code_refactor_scout -run 'TestFinalizeStructuralSimilarityClusterFindings|TestFinalizeCallFamilyClusterFindings|TestSuppressRedundantClusterFindings|Test.*Cluster'
GOWORK=off go test -count=1 ./skills/code_refactor_scout
```

### Phase 2: `run` Orchestration Split

Target files:

- `skills/code_refactor_scout/main.go`
- `skills/code_refactor_scout/main_test.go`

Target:

- `run`

Done when:

- `run` remains the public skill entrypoint but delegates to narrow private
  stages such as input normalization, scope resolution, evidence collection,
  filtering, output assembly, and artifact persistence.
- Side effects stay at orchestration edges.
- The JSON output contract is unchanged.
- Tests verify target filtering, focus filtering, presentation view behavior,
  artifact summaries, and signal fields through the skill output.

Verification:

```bash
GOWORK=off go test -count=1 ./skills/code_refactor_scout
GOWORK=off go test -count=1 ./cmd/foxctl/cmd -run 'TestRefactor'
```

### Phase 3: Presentation and Summary Builders

Target files:

- `skills/code_refactor_scout/main.go`
- focused tests in `skills/code_refactor_scout/main_test.go`

Targets:

- `finalizeSameFileExtractionFindings`
- `summarizeFiles`
- `summarizeSymbols`
- `buildEntrypointLane`
- `buildDBAccessLane`
- `buildRepeatedPatternLane`
- `buildSkillTargetLane`
- `synthesizeCompositeFindings`

Done when:

- Repeated grouping, sampling, sorting, and lane-item construction logic is
  moved behind small private helpers.
- No new public presentation schema is introduced.
- Golden-sensitive order and lane limits remain deterministic.

Verification:

```bash
GOWORK=off go test -count=1 ./skills/code_refactor_scout -run 'TestBuildScoutPresentation|Test.*Lane|Test.*Summary|Test.*Composite|Test.*SameFile'
GOWORK=off go test -count=1 ./skills/code_refactor_scout
```

### Phase 4: Go AST and Fingerprint Helpers

Target files:

- `skills/code_refactor_scout/main.go`
- focused tests in `skills/code_refactor_scout/main_test.go`

Targets:

- `goStmtFingerprint`
- `goFuncSignature`
- `splitTopLevel`
- `analyzeGoFuncDecl`
- `detectGoBoolReturnWrapper`

Done when:

- Branch-heavy AST logic is split into small pure helpers with explicit inputs
  and outputs.
- Fingerprint output remains stable for representative statement/expression
  fixtures.
- No rules or score formulas change.

Verification:

```bash
GOWORK=off go test -count=1 ./skills/code_refactor_scout -run 'Test.*Go|Test.*Fingerprint|Test.*Bool|Test.*Signature'
GOWORK=off go test -count=1 ./skills/code_refactor_scout
```

### Phase 5: Semantic Bool Family

Target files:

- `skills/code_refactor_scout/semantic_bool.go`
- `skills/code_refactor_scout/semantic_bool_elixir_cgo.go`
- `skills/code_refactor_scout/semantic_bool_python_cgo.go`
- `skills/code_refactor_scout/semantic_bool_ts_cgo.go`
- related stub/test files

Targets:

- `simplifySemanticBoolExpr`
- `lowerGoSemanticBoolExprDetailed`
- `lowerElixirSemanticBoolExprDetailed`
- `detectPythonBoolReturnWrapper`
- `lowerPythonSemanticBoolExprDetailed`
- `detectTypeScriptBoolReturnWrapper`
- `lowerTypeScriptSemanticBoolExprDetailed`
- `detectElixirBoolReturnWrapper`

Done when:

- Shared lowering/result-shape logic is extracted only where two or more real
  adapters use it.
- Language-specific syntax handling remains local to each adapter.
- Stub behavior remains unchanged for non-cgo builds.
- Tests cover the observable simplified boolean findings by language.

Verification:

```bash
GOWORK=off go test -count=1 ./skills/code_refactor_scout -run 'Test.*SemanticBool|Test.*BoolReturn|Test.*Elixir|Test.*Python|Test.*TypeScript'
GOWORK=off go test -count=1 ./skills/code_refactor_scout
```

### Phase 6: TypeScript Duplicate Recovery

Target files:

- `skills/code_refactor_scout/ts_duplicate_recovery_cgo.go`
- related tests and stubs

Targets:

- `analyzeTypeScriptDuplicateRecoveryBlocks`
- `analyzeTypeScriptDuplicatedErrorRemaps`
- `analyzeTypeScriptRepeatedGuardLadders`
- `collectTSDuplicateRecoveryGroups`
- `collectTSRepeatedGuardGroups`
- `tsNodeFingerprint`

Done when:

- The TypeScript duplicate-recovery analyzers reuse a shared symbol/group scan
  shape comparable to the Elixir cgo analyzers where it reduces complexity.
- Rule IDs, line calculations, evidence fields, and sorting stay unchanged.
- Fingerprint helpers are pure and covered by representative fixtures.

Verification:

```bash
GOWORK=off go test -count=1 ./skills/code_refactor_scout -run 'Test.*TypeScript|Test.*Duplicate|Test.*Guard|Test.*Recovery'
GOWORK=off go test -count=1 ./skills/code_refactor_scout
```

### Phase 7: Residual Elixir, DB Slop, and Dead-Code Hotspots

Target files:

- `skills/code_refactor_scout/elixir_slop_cgo.go`
- `skills/code_refactor_scout/db_slop_elixir_cgo.go`
- `skills/code_refactor_scout/deadcode.go`
- related tests and stubs

Targets:

- `elixirNodeFingerprint`
- `elixirRecoveryCandidate`
- `elixirGuardAtoms`
- `elixirPreloadAfterGetChainCandidate`
- `elixirPostTransactionPreloadCandidate`
- `elixirAnonymousTransactionScriptCandidate`
- `elixirMultiTransactionScriptCandidate`
- `buildDeadPackageFindings`
- `buildDeadFileFindings`
- `summarizeDeadCodeInbounds`
- `buildDeadStructuralRoots`

Done when:

- Candidate extraction and fingerprinting logic is broken into pure helpers.
- Dead-code file/package classification remains conservative and unchanged.
- Existing cgo/stub behavior is preserved.

Verification:

```bash
GOWORK=off go test -count=1 ./skills/code_refactor_scout -run 'Test.*Elixir|Test.*DB|Test.*Transaction|Test.*Preload|Test.*Dead|Test.*Package|Test.*Inbound'
GOWORK=off go test -count=1 ./skills/code_refactor_scout
```

### Phase 8: Evidence and Confidence Modules

Target files:

- `skills/code_refactor_scout/evidence.go`
- `skills/code_refactor_scout/confidence.go`
- related tests

Targets:

- `buildScoutEvidence`
- `attachEvidenceToHotspots`
- `scoreFindingConfidence`

Done when:

- Evidence loading, confidence scoring, and hotspot attachment have small
  interfaces and pure scoring helpers.
- Confidence factors and evidence keys remain unchanged.
- Tests cover confidence-score determinism and missing/partial evidence
  behavior.

Verification:

```bash
GOWORK=off go test -count=1 ./skills/code_refactor_scout -run 'Test.*Evidence|Test.*Confidence|Test.*Hotspot'
GOWORK=off go test -count=1 ./skills/code_refactor_scout
```

### Phase 9: Internal Refactor Substrate

Target files:

- `internal/intelligence/refactor/evidence/evidence.go`
- `internal/intelligence/refactor/hot/hot.go`
- `internal/intelligence/refactor/hot/symbols.go`
- `internal/intelligence/refactor/status/status.go`
- `internal/intelligence/refactor/changes/changes.go`
- `internal/intelligence/refactor/scope/scope.go`
- `internal/intelligence/refactor/snapshot/snapshot.go`
- related tests

Targets:

- `evidence.Load`
- `hot.collectHotFiles`
- `hot.BuildSymbolHotspots`
- `status.rebaseToIndexedWorkspace`
- `changes.collectGitFileChanges`
- `scope.ResolveResolvedPath`
- `snapshot.Builder.Build`
- `status.Evaluate`

Done when:

- Each target function delegates to pure helpers for parsing, ranking,
  filtering, or path normalization.
- Public package APIs remain compatible.
- Tests cover order, limits, path handling, and fallback/error modes.

Verification:

```bash
GOWORK=off go test -count=1 ./internal/intelligence/refactor/changes ./internal/intelligence/refactor/hot ./internal/intelligence/refactor/scope ./internal/intelligence/refactor/status ./internal/intelligence/refactor/evidence ./internal/intelligence/refactor/snapshot
```

### Phase 10: Command Handler Families

Target files:

- `cmd/foxctl/cmd/actorsys.go`
- `cmd/foxctl/cmd/agent.go`
- `cmd/foxctl/cmd/agent_ask_runtime.go`
- `cmd/foxctl/cmd/agent_room.go`
- `cmd/foxctl/cmd/auth.go`
- `cmd/foxctl/cmd/bb.go`
- `cmd/foxctl/cmd/cas.go`
- `cmd/foxctl/cmd/ci.go`
- `cmd/foxctl/cmd/codemap.go`
- `cmd/foxctl/cmd/console.go`
- `cmd/foxctl/cmd/context_retrieve_inspect*.go`
- `cmd/foxctl/cmd/db.go`
- `cmd/foxctl/cmd/eval*.go`
- `cmd/foxctl/cmd/evolve*.go`
- `cmd/foxctl/cmd/flow.go`

Target families:

- `actorsys`: `runActorSys*`, `respawnRegisteredActors`
- `agent`: `runAgent*`, `waitForReply`, Jido ask runtime setup
- `eval`: eval command constructors, runners, result summarizers, prompt
  builders
- `evolve`: evolve command constructors, lifecycle, run, inspect helpers
- `flow`: flow mutation and display handlers
- supporting command families: auth, bb, cas, ci, codemap, console, db,
  context retrieve inspect

Done when:

- Work proceeds one command family at a time.
- Each command family extracts reusable command-shape logic, validation,
  rendering, or request-building helpers without inventing a broad framework.
- Existing Cobra command names, flags, output contracts, and routing behavior
  remain unchanged.
- Focused command tests pass for the touched family.

Verification examples:

```bash
GOWORK=off go test -count=1 ./cmd/foxctl/cmd -run 'TestActorSys'
GOWORK=off go test -count=1 ./cmd/foxctl/cmd -run 'TestAgent'
GOWORK=off go test -count=1 ./cmd/foxctl/cmd -run 'TestEval'
GOWORK=off go test -count=1 ./cmd/foxctl/cmd -run 'TestEvolve'
GOWORK=off go test -count=1 ./cmd/foxctl/cmd -run 'TestFlow'
```

Run only the family-specific tests for the phase, then run a broader command
test if the family touches shared helpers:

```bash
GOWORK=off go test -count=1 ./cmd/foxctl/cmd
```

### Phase 11: Final Scout Pass and Review

Done when:

- Rebuild the scout binary and re-run the three baseline scout commands from
  Phase 0.
- Record which top targets moved, which remain, and which should not be chased
  because they are acceptable orchestration surfaces or would require a broader
  product/API decision.
- Run final verification:

```bash
GOWORK=off go test -count=1 ./skills/code_refactor_scout
GOWORK=off go test -count=1 ./internal/intelligence/refactor/changes ./internal/intelligence/refactor/hot ./internal/intelligence/refactor/scope ./internal/intelligence/refactor/status ./internal/intelligence/refactor/evidence ./internal/intelligence/refactor/snapshot
GOWORK=off go test -count=1 ./cmd/foxctl/cmd -run 'TestRefactor'
make check-doc-links
git diff --check
```

- Final self-review includes:
  - modules that became deeper
  - interfaces that became smaller or stayed intentionally unchanged
  - leverage and locality gained
  - tests added and why they are behavior-focused
  - residual hotspots and whether they are next-phase work or acceptable
  - residual risks
  - confidence score

## Verification Rules

- Always run `gofmt -w` on touched Go files before tests.
- For markdown changes, run `make check-doc-links`.
- For cgo-gated scout files, run the relevant package tests in the local
  environment and note if cgo is unavailable.
- For any semantic anchors or `Index:` blocks added or moved, run:

```bash
GOWORK=off go test -count=1 ./internal/intelligence/indexing/semanticanchors ./internal/intelligence/indexing/repoindex
GOWORK=off go test -count=1 ./cmd/foxctl/cmd -run TestIndexRepoSemanticAnchorsE2EIndexCommentsCoexist
```

## Stop Conditions

- Stop after three failed attempts at the same verification failure.
- Stop before changing public output contracts, command names, flags,
  dependency graph, repoindex schema, or storage layout.
- Stop before introducing a new public package or exported interface unless the
  deletion test shows it earns depth and the package placement is justified by
  `docs/architecture/package-topology.md`.
- Stop if a target requires a product decision rather than a behavior-preserving
  refactor.
- Stop if unrelated branch changes conflict with the target seam and summarize
  exactly which files are blocking integration.
