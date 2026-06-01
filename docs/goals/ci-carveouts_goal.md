# Goal: CI Carve-Outs After Core/Skills Split

## Goal
Continue reducing foxctl CI wall time and improving failure locality by carving
the remaining mixed jobs into behavior-owned lanes. Preserve or improve merge
confidence: every split must protect a real foxctl behavior, fail with a useful
signal, and avoid coverage theater.

## Context
- Work from the current CI split branch or a successor branch for MR !52:
  `feat/ci-core-skills-split`.
- MR !52 already split short tests, skills builds, race shards, integration
  lanes, and manifest validation. The latest observed MR pipeline was green at
  about 18 minutes, down from the earlier 1 hour plus pipeline.
- The remaining CI work is not to add more tests by default. It is to make the
  pipeline map more directly to foxctl's actual Modules, Contracts, and
  high-value risk areas.
- Use foxctl domain language from `CONTEXT.md`: Skill, Skill Manifest, Skill
  Invocation, Runtime, Command, Job, Contract, Policy, Adapter, Workspace,
  Context system, Context engine, and Artifact.
- Use architecture language from the architecture skill: Module, Interface,
  Implementation, Depth, Seam, Adapter, Leverage, and Locality.
- Relevant local guidance:
  - `docs/architecture/package-topology.md`
  - `docs/architecture/skill-pack-split-analysis.md`
  - `docs/architecture/skill-pack-split-analysis-pi.md`
- OpenAPI-heavy work is not a priority for this goal.
- Mutation testing must stay out of default CI. It should remain a deliberate
  manual/process choice for test robustness audits, not a merge gate.

## Constraints
- Optimize for confidence per CI minute, not job count, coverage percentage, or
  broad test theater.
- Keep changes small and reviewable. Prefer one coherent commit per carve-out.
- Do not add new dependencies or a generic CI framework.
- Do not rewrite unrelated Makefile targets, scripts, or CI jobs while touching
  one lane.
- Preserve public developer workflow unless caller evidence proves a wrapper is
  unused. Search real callers before deleting compatibility targets.
- Do not keep obsolete aggregate jobs or wrappers after a split unless there is
  a current caller or a clear transition need.
- Do not add path rules that skip Skill gates when core Skill Contract,
  Skill Manifest, Runtime, Command, or shared Adapter code changes.
- Do not merge the MR without explicit human approval.
- Use Pi and Hermes, if available, for non-blocking adversarial review of CI
  lane value, brittle tests, and sprawl risk. Ask them for concrete delete,
  demote, or rewrite recommendations rather than broad commentary.

## Milestones
1. Split static analysis by ownership. **(Implemented)**
   - `static-analysis` was split into `format-check`, `go-lint`, and `repo-hygiene`
     running in parallel in the `check` stage.
   - Each job names the behavior it protects (formatting, static-analysis bugs,
     repo bloat / tech-debt bounds).
   - Decision note:
     `~/docs/plans/decision-notes/ci-carveouts/2026-06-01-static-analysis-split.md`
   - See also milestone 3 for the path-rules work that was applied to these jobs.

2. Split build lanes only where the Module seam is real. **(Reviewed; deferred)**
   - Inspect `build`, `build-core`, frontend, Skill, and tool targets before
     changing CI.
   - Split only when the Interface between lanes is meaningful, such as CLI,
     web/frontend, Skill binaries, or standalone tooling.
   - Avoid pass-through Makefile targets whose Interface is as complex as their
     Implementation.
   - Current decision: keep `build-core` intact because the CLI and mail
     binaries share the same internal Implementation surface; splitting now
     would create shallow Jobs with little Locality.

3. Add safer impact-aware rules. **(Implemented for current slice)**
   - Use existing impact scripts and target structure first.
   - Add path/rules logic only where it cannot hide Contract breakage.
   - Core Contract, Runtime, storage/CAS, Skill Manifest, and shared Adapter
     changes must trigger the relevant downstream lanes.
   - Documentation-only changes may skip expensive lanes when GitLab rules and
     local verification prove the skip is safe.
   - `repo-hygiene`, `docs-links`, and `all-checks` run on a light image so
     docs-only/frontend-only pipelines are not coupled to a skipped per-commit
     Go CI image.
   - Light/Bun checks run independently during prepare, while integration,
     race-smoke, and build lanes avoid a post-image pod burst that exceeds the
     dev runner memory quota.
   - Decision note:
     `~/docs/plans/decision-notes/ci-carveouts/2026-06-01-impact-aware-rules.md`

4. Rebalance expensive race lanes with data. **(Reviewed; deferred)**
   - Gather current job duration data from the MR pipeline before editing the
     matrix.
   - Move packages between race shards only when the runtime data shows an
     imbalance.
   - Preserve race coverage for core Runtime, storage, Context system, Context
     engine, RLM, and Skill packages.
   - Current decision: keep the current race shape until several post-split
     pipeline durations show a stable imbalance worth changing.

5. Isolate frontend and generated-artifact checks if they remain mixed. **(Partially implemented)**
   - Frontend/package-lock/config changes should trigger frontend-specific
     checks without dragging unrelated core lanes.
   - Generated Artifact checks should be explicit and cheap enough for the
     stage they run in.
   - Current slice scopes `typescript-frontend` to non-docs frontend package
     metadata, TypeScript/MJS frontend scripts, and the web Interface surface.
   - Current slice adds a separate `docs-site` lane for Starlight/Astro docs
     validation so docs-site Markdown is checked by the correct Contract.

6. Document CI lane ownership.
   - Add or update a short repo doc only if it reduces future confusion.
   - The doc should map each CI lane to the foxctl Module, Contract, or Job risk
     it protects.
   - Do not create a large process document.

## Ruthless Test Strategy
- For every added, kept, or moved check, answer:
  - What real behavior does this protect?
  - What important bug would this catch?
  - Would deleting this check reduce confidence in a meaningful way?
  - Is it fast enough for the feedback stage where it runs?
  - When it fails, will the owner and likely cause be obvious?
- Delete, demote, or leave manual any check that fails this filter.
- Prefer behavior checks over private implementation checks.
- Prefer one higher-signal lane over many thin lanes that mostly prove wiring.

## Small Composable Code
- Make the smallest coherent CI/Makefile/script change for the current
  milestone.
- Keep target names domain-specific and easy to delete.
- Avoid introducing mode flags, broad helper scripts, or speculative job
  templates.
- If a split creates duplicate logic, consolidate only when the duplicate rule
  is the same concept and would otherwise need identical future fixes.

## Architecture Requirements
- Treat CI lanes as Interfaces over behavior-owned Modules. The test surface
  should cross the same Seam a maintainer would use to reason about failures.
- Prefer deeper lanes: a small, understandable Job Interface that covers a
  meaningful Implementation area.
- Reject shallow pass-through Jobs that add naming and scheduling cost without
  Leverage.
- Use Locality as the design test: a failing lane should tell maintainers where
  to look.

## Zero Tech Debt Requirements
- State the intended end state before editing each milestone.
- Search real callers before preserving old aggregate targets or compatibility
  wrappers.
- Delete stale CI comments, obsolete TODOs, and unused job aliases introduced by
  earlier iterations.
- Keep one canonical behavior for each lane after the transition is complete.
- Do not leave `.freeze`, mutation defaults, or temporary runner workarounds in
  the final shape.

## Verification
Run focused verification after each milestone, then full verification before
completion:

```bash
PATH="/usr/local/go/bin:$PATH" git diff --check
PATH="/usr/local/go/bin:$PATH" go run ./scripts/checkmanifests
PATH="/usr/local/go/bin:$PATH" make test-short-core-impacted BASE_REF=gitlab/main HEAD_REF=HEAD
PATH="/usr/local/go/bin:$PATH" make test-short-skills-impacted BASE_REF=gitlab/main HEAD_REF=HEAD
PATH="/usr/local/go/bin:$PATH" make skills-build-impacted BASE_REF=gitlab/main HEAD_REF=HEAD
PATH="/usr/local/go/bin:$PATH" make test-integration-impacted BASE_REF=gitlab/main HEAD_REF=HEAD
```

Also verify:
- GitLab CI lint API reports a valid pipeline with no warnings.
- The MR pipeline is green before declaring the goal complete.
- Record pipeline duration and the slowest remaining jobs after each pushed
  carve-out.
- If CI fails, inspect the failing Job logs, fix the smallest true cause, and
  rerun only the relevant lane when possible.

## Done When
- Static analysis, build, impact-aware skip rules, race shards, and frontend or
  artifact checks have each been reviewed against the ruthless test filter.
- Any implemented carve-out has a green local verification path and a green MR
  pipeline.
- Remaining expensive lanes are either justified by behavior risk or documented
  as future work with a concrete reason.
- Obsolete wrappers, temporary CI workarounds, and low-value checks introduced
  during the split are deleted.
- The final report lists:
  - commits made,
  - jobs added/removed/renamed,
  - slowest remaining jobs,
  - pipeline duration before and after,
  - residual risks,
  - confidence score.

## Stop Conditions
- Stop after three failed attempts at the same CI failure and summarize the
  blocker with logs and the exact command or Job that failed.
- Stop before changing public Skill, Runtime, storage, RLM, or manifest
  Contracts unless the change is explicitly required and verified.
- Stop before adding dependencies or changing GitLab runner infrastructure.
- Stop if a proposed split would reduce confidence without a measurable runtime
  or diagnosis benefit.
