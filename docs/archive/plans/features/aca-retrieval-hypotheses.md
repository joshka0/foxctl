# ACA Retrieval Hypotheses

Status: experimental

## Goal

Identify deterministic, testable ACA improvements that can be evaluated without changing the default execution path first.

The emphasis here is:

- programmatic scoring
- deterministic behavior
- suitability for execution layers such as agents

## Eval Modes

The current branch exposes these ACA ablation modes in [eval.go](../../../../cmd/foxctl/cmd/eval.go):

- `aca_control_only`
- `aca_vault_only`
- `aca_repo_hints`
- `aca_canonical_only`
- `aca_package_fallback`
- `aca_query_typed`

These are designed to test the hypotheses below.

## Hypothesis 1: Control-Plane Refs Are Not Enough

Claim:

- top-of-mind plus latest handoff refs alone are too weak to answer most repo/package queries

Test mode:

- `aca_control_only`

Expected result:

- poor hit rate on mixed retrieval suites

Why it matters:

- if this is true, agents should not rely on ACA control-plane state alone for retrieval
- they need durable note retrieval or code retrieval to accompany it

## Hypothesis 2: Vault Retrieval Carries Most ACA Signal

Claim:

- most ACA performance comes from durable note retrieval, not from direct control-plane bundles

Test mode:

- `aca_vault_only`

Expected result:

- should outperform `aca_control_only`
- may approach or match the default ACA lane on note-oriented queries

Why it matters:

- if true, agent execution should treat vault retrieval as the primary ACA retrieval surface

## Hypothesis 3: Repo Hints Improve ACA on Code-Centric Queries

Claim:

- deterministic code-hint scoring from repoindex paths and symbols improves ACA retrieval for package/runtime/config/api queries

Test mode:

- `aca_repo_hints`

Mechanism:

- boost vault hits whose `repo_paths` or `symbols` align with repo-derived code hints

Expected result:

- improves code-centric mixed queries over `aca_vault_only`

Why it matters:

- this is a deterministic bridge between ACA and repoindex that agents can use reliably

## Hypothesis 4: Trust Gating Improves Ranking Stability

Claim:

- restricting ACA retrieval to `canonical` and `reviewed` notes improves ranking quality by reducing raw/draft noise

Test mode:

- `aca_canonical_only`

Mechanism:

- filter vault hits by trust before final ranking

Expected result:

- lower noise on architecture/package queries
- possibly lower recall on thin-note repos

Why it matters:

- this is a deterministic quality gate agents can rely on when they need strong confidence over broad recall

## Hypothesis 5: Package-Note Fallback Improves Weak Package Queries

Claim:

- deterministic package-note fallback from repo paths improves ACA on package/runtime/controller queries where direct vault search is weak

Test mode:

- `aca_package_fallback`

Mechanism:

- derive repo path hints
- map those paths deterministically to canonical package-note paths
- query the vault index for those exact package-note candidates
- add a deterministic boost when they exist

Expected result:

- improves package- and package-note-oriented queries, especially on repos with broad package note coverage

Why it matters:

- this is deterministic and agent-friendly
- it turns repo structure into durable ACA retrieval hints without requiring model judgment
- it is now wired into workspace policy through `.foxctl/policy/retrieval.yaml` via `aca.package_note_fallback`

## Hypothesis 6: Query-Typed Bias Beats Flat ACA Weighting

Claim:

- programmatic query-type-aware weighting improves ACA retrieval over one flat set of weights

Test mode:

- `aca_query_typed`

Mechanism:

- boost note types differently depending on query class
  - package/runtime/config/api/web queries prefer canonical + map notes
  - policy/decision queries prefer ADR + pattern notes
  - incident/failure/gotcha queries prefer incident + investigation notes

Expected result:

- better ranking quality than `aca_canonical_only` on mixed suites

Why it matters:

- this is deterministic and execution-layer-friendly
- it gives agents a stable query routing policy instead of a vague instruction

## Recommended Test Order

1. `aca_control_only`
2. `aca_vault_only`
3. `aca_repo_hints`
4. `aca_canonical_only`
5. `aca_package_fallback`
6. `aca_query_typed`

This order isolates:

- control plane
- vault baseline
- repo-aware deterministic boosting
- trust gating
- deterministic package-note fallback
- query-type-aware weighting

## Current Recommendation

If we want ACA to stay execution-layer-friendly, we should bias toward:

- deterministic weighting
- deterministic trust gating
- deterministic repo-hint boosting

and avoid over-relying on prompt-only ACA behavior.

## Related Docs

- [rlm-retrieval-findings.md](rlm-retrieval-findings.md)
- [foxctl-rlm-next-steps.md](foxctl-rlm-next-steps.md)
