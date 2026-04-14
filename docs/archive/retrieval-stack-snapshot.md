# Retrieval Stack Snapshot

Compact benchmark view for the current retrieval stack.

Use this page as the fastest answer to:

- how `code_search_ensemble` is behaving by route family
- how ACA retrieval is doing across repos
- which repo or route still needs work

For the stable commands and full suite definitions, see:

- [code-search-evals.md](../general/code-search-evals.md)
- [retrieval-evals.md](../general/retrieval-evals.md)

## Cross-Repo Status

| Repo | Code-search status | ACA retrieval status | Current read |
|------|--------------------|----------------------|--------------|
| `foxctl` | strong on `code` and `package_ownership` | now clean on the checked-in ACA package suite | strongest balanced overall |
| `jido` | package-anchor transfer is strong | strongest | current best retrieval corpus |
| `praze` | infra/resource route is strong | acceptable, still behind | infra is good, general note retrieval still needs work |

## Code Search Ensemble

### foxctl package route

Source: `/tmp/agentctl_package_code_search_auto_current_v9.json`

- pass rate: `1.00`
- mean correctness: `1.00`
- route family: `package_ownership`
- mean bucket counts:
  - `primary_anchor=1.00`
  - `repo_evidence=2.75`
  - `secondary_anchor=0.25`

### foxctl repo-grounded code route

Source: `/tmp/agentctl_repo_grounded_wrapper_report_v1.json`

- pass rate: `1.00`
- mean correctness: `1.00`
- route family: `code`
- mean bucket counts:
  - `repo_evidence=3.43`

### praze infra route

Source: `/tmp/praze_infra_policy_report_v2.json`

- pass rate: `1.00`
- mean correctness: `1.00`
- route family: `infra_resource`
- mean bucket counts:
  - `primary_anchor=1.00`
  - `declarative_companion=2.50`
  - `semantic_companion=0.50`

## ACA Retrieval

### foxctl

Source: `/tmp/retrieval-policy-check/foxctl-20260329T131551Z.json`

- `aca_default`
  - `hit@5 1.00`
  - `hit@10 1.00`
  - `MRR 1.00`
- `aca_query_typed`
  - `hit@5 1.00`
  - `hit@10 1.00`
  - `MRR 1.00`

### jido

Source: `/tmp/retrieval-docs-seq/jido-20260327T133927Z.json`

- `aca_default`
  - `hit@5 1.00`
  - `hit@10 1.00`
  - `MRR 0.92`
- `aca_query_typed`
  - `hit@5 1.00`
  - `hit@10 1.00`
  - `MRR 0.92`

### praze

Source: `/tmp/retrieval-docs-seq/praze-20260327T133941Z.json`

- `aca_default`
  - `hit@5 0.73`
  - `hit@10 0.82`
  - `MRR 0.68`
- `aca_query_typed`
  - `hit@5 0.73`
  - `hit@10 0.82`
  - `MRR 0.68`

## Reading The Snapshot

- `foxctl` is now clean again on the checked-in ACA package suite, including the previously weak repo-root, config, and web-api note cases.
- `jido` is the strongest ACA retrieval repo, which suggests its canonical note corpus is the cleanest of the three.
- `praze` is now strong on infra/resource grounding, but its broader package-note retrieval still trails the other repos.

## Refresh Workflow

1. rerun the relevant wrapper scripts or make targets
2. update the source artifact paths if they changed
3. update the numeric bands here
