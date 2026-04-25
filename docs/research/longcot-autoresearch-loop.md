# LongCoT RLM Autoresearch Loop

This branch is an isolated experiment lane for RLM controller variants on
LongCoT. The loop follows the `autoresearch` pattern:

1. make one small controller/runtime/prompt change;
2. run a fixed LongCoT slice under a fixed budget;
3. append one TSV result row;
4. keep the change only if it improves the measured result or gives a useful
   simpler failure mode.

The metric is not a training loss. For now, the score is:

```text
primary:   official verifier correct count
secondary: verified attempts, wrong-format reduction, failure-stage progress
budget:    wall time, tokens, cost
```

Artifacts and the TSV ledger live under `.foxctl/longcot-autoresearch/`, which
is intentionally gitignored.

## Baseline Command

```bash
python3 scripts/longcot_autoresearch.py \
  --env-file /Users/joshka/repos/personal/foxctl/.env \
  --longcot-repo /Users/joshka/repos/githubs/LongCoT \
  --variant baseline_braid_helper \
  --provider openrouter \
  --model google/gemini-3.1-flash-lite-preview \
  --domain logic \
  --difficulty easy \
  --limit 1
```

## Initial Hypotheses

- `braid_beam_k3`: generate several candidate plans, verify all, then repair
  only the best frontier instead of doing one linear repair.
- `braid_state_transition`: require solve-like nodes to produce an executable
  state/action/goal model for planning domains.
- `braid_counterexample_repair`: preserve verifier failures as structured
  counterexamples and include blocked candidate hashes in repair prompts.
- `topology_router`: classify each LongCoT task as `state_transition`,
  `fixed_point`, `dag_dependencies`, `adversarial_search`, or
  `constraint_satisfaction`, then select the controller policy.

The first implementation target should be verifier-guided candidate beam,
because it is the smallest step from the current BRAID runner and directly
addresses the observed failure mode: a verifier rejected the first plan, but the
single repair generated `solution = []`.
