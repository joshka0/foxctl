# Goal: Refactor Impact Tool

## Goal

Build a first-class refactor blast-radius tool that helps agents and humans
plan, execute, and review refactors by combining:

- git diff or explicit target inputs
- repoindex DAG relationships for structural coupling
- searchindex/turbovec semantic neighbors for conceptual coupling
- focused test and documentation suggestions

The end state is a `code/refactor_impact` style workflow, or an equivalent
extension of the existing branch-impact/refactor-scout command surface, that
answers:

> If I change, move, delete, or consolidate this file, symbol, package, or
> contract, what else should I inspect or update?

The tool should produce a reviewable impact packet. It should not auto-edit by
default.

## Context

- Current branch context: `feat/branch-impact-pack`.
- Existing related implementation:
  - `skills/code_branch_impact/`
  - `internal/intelligence/branchimpact/`
  - `internal/intelligence/searchindex/`
  - `internal/intelligence/turbovec/`
  - `internal/intelligence/retrieval/v2/`
  - `skills/code_refactor_scout/`
  - `docs/plans/features/refactor-intelligence-substrate.md`
  - `docs/general/refactor-scout.md`
  - `docs/architecture/turbovec-sidecar.md`
- Recent validation proved the combined lane can work:
  - searchindex canonical workspace id:
    `65589e17298cf7261801d2e3e84d3df7`
  - canonical family path: `/home/dev/repos/foxctl`
  - reindexed corpus: `9326/9326` embedded docs
  - embedding model: `qwen3-embedding-8b`, `4096` dimensions
  - branch-impact semantic lane available via `turbovec_vector`
- The key architectural distinction:
  - repoindex catches structural coupling: calls, contains, imports, refs
  - searchindex/turbovec catches conceptual coupling: similar contracts,
    repeated implementation shape, sibling responsibilities

## Constraints

- Do not make this an auto-refactor tool in the first slice. The first-class
  product is a blast-radius packet that can guide a human or agent.
- Keep behavior deterministic and reviewable. Sort outputs stably.
- Do not add dependencies unless explicitly approved.
- Do not introduce compatibility aliases or legacy fallback contracts.
- Do not use keyword heuristics for routing or classification. Use explicit
  input fields, typed signals, scored features, repoindex edges, searchindex
  scores, and tests.
- Preserve protocol envelope shape: `version`, `status`, `command`, `data`,
  `meta`, `error`.
- Preserve WASI/network policy for skills.
- Keep `internal/*` placement aligned with
  `docs/architecture/package-topology.md`.
- Prefer small composable modules:
  - pure target planning
  - pure candidate grouping/ranking
  - thin shell for git, repoindex, searchindex, and skill I/O
- Do not duplicate branch-impact and refactor-scout logic blindly. Reuse or
  extract only when it reduces total concepts and keeps ownership clear.
- Stop after three failed attempts at the same verification failure and report
  the exact blocker.

## Milestones

### Milestone 0: Baseline and API Choice

Done when:

- Inspect current command and skill shape for:
  - `code/branch_impact`
  - `code/refactor_scout`
  - repoindex DAG grep/search
  - searchindex/turbovec recall
- Decide whether the first implementation is:
  - a new `code/refactor_impact` skill, or
  - a narrow extension to `code/branch_impact`
- Record the choice in the PR notes or docs.
- Baseline checks pass:

```bash
go test ./skills/code_branch_impact ./internal/intelligence/branchimpact -count=1
go test ./internal/intelligence/searchindex ./internal/intelligence/turbovec -count=1
```

### Milestone 1: Target Input Model

Done when:

- Add a typed refactor target model with explicit target kinds:
  - `file`
  - `symbol`
  - `package`
  - `contract`
  - `diff`
- Add explicit refactor intent values:
  - `rename`
  - `move`
  - `delete`
  - `consolidate`
  - `type_tighten`
  - `api_contract_change`
  - `behavior_preserving_cleanup`
- Inputs support both:
  - explicit targets supplied by the caller
  - branch diff derived targets
- Tests cover normalization, invalid inputs, stable sorting, and max-target
  caps.

### Milestone 2: Structural Lane

Done when:

- The tool returns structural candidates from repoindex DAG expansion/search.
- Candidates include why they were selected:
  - direct changed target
  - caller/callee
  - containing file/package
  - import/ref edge
  - test relationship when available
- Candidates are grouped into at least:
  - `must_update`
  - `should_inspect`
  - `tests_to_run`
  - `docs_to_update`
- Tests use fake repoindex results and assert grouping, scoring, and ordering.

### Milestone 3: Semantic Lane

Done when:

- The tool queries searchindex/turbovec for semantic neighbors of each target.
- Semantic recall uses the canonical workspace id and correct embedding
  metadata.
- Top-k starvation is handled by pre-filter oversampling before changed-target
  filtering.
- Returned hits are hydrated with path, symbol, summary, line hints, and source.
- Candidates include similarity scores and source `turbovec_vector` or
  `searchindex_vector`.
- Tests cover:
  - changed target filtering
  - hydrated hit requirements
  - empty corpus behavior
  - embedding/provider unavailable behavior
  - deterministic ordering

### Milestone 4: Refactor Packet Output

Done when:

- Output includes a compact, reviewable packet:
  - `summary`
  - `targets`
  - `lanes`
  - `must_update`
  - `should_inspect`
  - `likely_duplicate`
  - `contract_boundary`
  - `tests_to_run`
  - `docs_to_update`
  - `context_only`
- Each candidate has:
  - path
  - symbols
  - line hints
  - rank/group
  - score
  - sources
  - reasons
  - optional target relationship
- Large outputs use CAS summary/artifact if they cross the repo threshold.
- Golden or table-driven tests cover output shape.

### Milestone 5: CLI/Skill Wiring

Done when:

- The workflow can be run from `./bin/foxctl run ...`.
- Examples cover:
  - explicit file target
  - explicit symbol target
  - current branch diff target
  - contract/API-change intent
- Skill manifest and docs use canonical terminology:
  - refactor impact
  - blast radius
  - structural lane
  - semantic lane
  - ContextWiki/searchindex only where appropriate
- `make skill SKILL=<skill-name>` passes for any new or modified skill.

### Milestone 6: End-to-End Validation

Done when:

- Run the tool on the current branch and confirm it returns both structural and
  semantic candidates.
- Run it against at least one focused explicit target in
  `internal/intelligence/searchindex/`.
- Verify no unrelated changes are required.
- Final self-review answers:
  - Does this reduce missed refactor blast radius?
  - Does it avoid auto-editing and hidden side effects?
  - Are structural and semantic lanes clearly separated?
  - Are false positives explainable?
  - What would fail code review?
  - Confidence score and residual risks.

## Verification

Run focused checks after each implementation slice:

```bash
go test ./internal/intelligence/searchindex ./internal/intelligence/turbovec -count=1
go test ./internal/intelligence/branchimpact ./skills/code_branch_impact -count=1
```

If a new skill is added, also run:

```bash
make skill SKILL=<skill-name>
./bin/foxctl run <skill-command> --ephemeral --input '<small-json-smoke>'
```

If docs change:

```bash
make check-doc-links
```

Before final completion:

```bash
make build
git diff --check
```

## Stop Conditions

- Stop before changing storage schema, protocol envelope shape, or public
  repoindex/searchindex contracts unless the goal is explicitly revised.
- Stop before adding a dependency.
- Stop if semantic recall requires production secrets or unavailable external
  services that cannot be mocked or locally configured.
- Stop after three failed attempts at the same failing check.
- Stop if the implementation starts auto-editing files rather than producing a
  refactor impact packet.
