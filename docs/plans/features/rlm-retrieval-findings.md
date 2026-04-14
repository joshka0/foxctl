# RLM Retrieval Findings

Status: current experimental findings

## Scope

This note captures the current retrieval benchmark state for:

- ACA-only retrieval
- ACA + code retrieval
- direct repoindex search
- direct repoindex DAG expansion
- free-form RLM
- staged RLM

It is intentionally short and benchmark-focused.

## Current Eval Modes

The current retrieval harness in [cmd/foxctl/cmd/eval.go](../../../cmd/foxctl/cmd/eval.go) supports:

- `skill_context`
  - ACA-only retrieval
- `skill_default_plus_context`
  - mixed ACA + existing code retrieval lane
- `repoindex_search`
  - direct `repoquery.SearchWithProjection`
- `repoindex_dag`
  - direct `repoquery.DAGGrepWithProjection`
- `rlm_llm`
  - free-form RLM tool loop
- `rlm_llm_code_staged`
  - staged `code_retrieval` RLM path

Important note:

- `skill_default` still exists and is useful as the current code/repo retrieval lane, but the direct repoindex modes above are the cleaner structural comparison lanes.

## AgentCTL Mixed

Suite:

- [foxctl-mixed.yaml](../../../testdata/evals/retrieval/foxctl-mixed.yaml)

Current results:

- `skill_context`
  - `hit@5 0.86`
  - `MRR 0.79`
- `skill_default_plus_context`
  - `hit@5 0.86`
  - `MRR 0.71`
- `repoindex_search`
  - `hit@5 0.14`
  - `MRR 0.07`
- `repoindex_dag`
  - `hit@5 0.29`
  - `MRR 0.10`
- `rlm_llm`
  - `hit@5 0.14`
  - `MRR 0.07`
- `rlm_llm_code_staged`
  - `hit@5 0.00`
  - `MRR 0.00`
  - latest exact comparison run

Interpretation:

- ACA-only is clearly the strongest lane on `foxctl-mixed`
- the mixed ACA + code lane is still strong, but below ACA-only on ranking quality
- `repoindex_dag` is the strongest non-ACA structural lane
- free-form RLM is currently no better than plain `repoindex_search`
- staged RLM is still unstable on the exact comparison run

## Praze Mixed

Suite:

- [praze-mixed.yaml](../../../testdata/evals/retrieval/praze-mixed.yaml)

Current results:

- `skill_context`
  - `hit@5 0.12`
  - `MRR 0.06`
- `skill_default_plus_context`
  - `hit@5 0.12`
  - `MRR 0.03`
- `repoindex_search`
  - `hit@5 0.00`
  - `MRR 0.00`
- `repoindex_dag`
  - `hit@5 0.00`
  - `MRR 0.00`
- `rlm_llm`
  - `hit@5 0.00`
  - `MRR 0.00`
- `rlm_llm_code_staged`
  - `hit@5 0.00`
  - `MRR 0.00`
  - `available 3/8`

Interpretation:

- `praze-mixed` is currently a poor fit for the direct structural lanes and current RLM controllers
- ACA is still the least-bad lane there, but only marginally
- repoindex and RLM are not competitive on that suite as currently implemented

## Current Conclusion

Today:

1. ACA-only wins overall.
2. ACA + existing code retrieval is still a practical lane.
3. `repoindex_dag` is useful, but narrower than ACA.
4. RLM is real and tool-using, but not yet competitive as a retrieval controller.

That means:

- use ACA + existing retrieval stack for practical work
- treat RLM as experimental
- use repoindex/DAG where structural graph traversal is explicitly needed

## What This Suggests

The next retrieval-control improvements should prioritize:

1. stronger staged routing and phase success criteria
2. path/domain validation before later phases
3. query-type-specific structural retrieval lanes

Do not interpret current RLM numbers as “RLM is not real.”
Interpret them as:

- the runtime is real
- the tool loop is real
- the controller is still weak

## Related Docs

- [foxctl-rlm-next-steps.md](foxctl-rlm-next-steps.md)
- [foxctl-rlm-integration-outline.md](foxctl-rlm-integration-outline.md)
- [rlm_query_runtime.md](../../spec/rlm_query_runtime.md)
