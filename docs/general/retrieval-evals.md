# Retrieval Evals

Stable ContextWiki retrieval evals are packaged as:

- checked-in suites under `testdata/evals/retrieval/`
- checked-in policy files in the same folder
- checked-in wrapper scripts under `scripts/`
- matching make targets in `Makefile`

The wrappers standardize:

- suite name
- workspace
- vault path
- retrieval modes
- expected metric thresholds where we have a stable healthy band

By default they evaluate:

- `aca_default`
- `aca_query_typed`

Those mode IDs keep the old `aca_` prefix for compatibility with existing
fixtures and reports. In prose, call the lane ContextWiki retrieval.

## Stable Commands

Use these commands for the common suites:

- `make eval-retrieval-foxctl`
- `make eval-retrieval-foxctl-mixed`
- `make eval-retrieval-foxctl-cochange`
- `JIDO_WORKSPACE=/path/to/jido make eval-retrieval-jido`
- `PRAZE_WORKSPACE=/path/to/praze make eval-retrieval-praze`
- `PRAZE_WORKSPACE=/path/to/praze make eval-retrieval-praze-mixed`
- `PRAZE_WORKSPACE=/path/to/praze make eval-retrieval-praze-k8s`

The wrappers use:

- `FOXCTL_STORAGE_ROOT` if set
- `FOXCTL_VAULT_PATH` if set, otherwise `~/.foxctl/templates/obsidian-vault`
  when present, falling back to the repo-local `.foxctl/templates/obsidian-vault`
- `--policy-file <checked-in policy>` internally

## Checked-In Suites

- `testdata/evals/retrieval/foxctl.yaml`
- `testdata/evals/retrieval/foxctl-mixed.yaml`
- `testdata/evals/retrieval/foxctl-cochange.yaml`
- `testdata/evals/retrieval/jido.yaml`
- `testdata/evals/retrieval/praze.yaml`
- `testdata/evals/retrieval/praze-mixed.yaml`
- `testdata/evals/retrieval/praze-k8s-mixed.yaml`

## Checked-In Policies

- `testdata/evals/retrieval/foxctl-policy.yaml`
- `testdata/evals/retrieval/foxctl-mixed-policy.yaml`
- `testdata/evals/retrieval/foxctl-cochange-policy.yaml`
- `testdata/evals/retrieval/jido-policy.yaml`
- `testdata/evals/retrieval/praze-policy.yaml`
- `testdata/evals/retrieval/praze-mixed-policy.yaml`
- `testdata/evals/retrieval/praze-k8s-mixed-policy.yaml`

Some of these currently just pin suite defaults and modes.
The `foxctl`, `jido`, and `praze` policies also carry mode-level minimum bands for:

- `hit@5`
- `hit@10`
- `MRR`

Each per-query mode result also reports a simple budget block:

- requested hit `limit`
- returned hit/path count
- returned path bytes
- estimated returned path tokens

Each mode result also records `duration_ms` for the retrieval call path, and the
summary table reports average duration per available mode.

When semantic retrieval is enabled, semantic-dependent modes perform a single
embedder health probe before the query loop. If the LM Studio/OpenAI-compatible
endpoint is down or the model is not loaded, those modes are marked unavailable
with the provider error instead of silently falling back to lexical results.

These budget fields intentionally measure the returned path list first. They do
not yet measure full materialized context payload size, prompt tokens, or model
usage tokens.

## Refreshing ContextWiki Inputs

If a suite looks pathologically bad, refresh the current foxctl ContextWiki/Obsidian
inputs before treating the numbers as a ranking regression. Use the active
foxctl vault path:

```bash
VAULT_PATH="${FOXCTL_VAULT_PATH:-$HOME/.foxctl/templates/obsidian-vault}"
foxctl obsidian graph build --workspace . --vault-path "$VAULT_PATH"
foxctl obsidian graph promote --workspace . --vault-path "$VAULT_PATH"
foxctl obsidian bridge reconcile --workspace . --vault-path "$VAULT_PATH"
foxctl obsidian index build --vault-path "$VAULT_PATH"
```

If the default home vault does not exist, the wrapper scripts fall back to the
repo-local `.foxctl/templates/obsidian-vault` fixture.

## Current Expected Bands

These are the latest verified ContextWiki retrieval outputs available locally.

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

Interpretation:

- currently clean on the checked-in `foxctl` ContextWiki package suite
- route-aware note ranking, corrected workspace identity, and bounded package fallback are now enough to clear the suite under policy gating

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

Interpretation:

- very strong package-note retrieval across the checked-in `jido` suite

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

Interpretation:

- acceptable but not yet strong enough to call stable at the same level as `jido`
- misses are concentrated in broader `praze` package/note disambiguation rather than the infra-focused path

## Updating The Bands

To refresh these numbers:

1. run the wrapper for the target suite
2. save the JSON/markdown output
3. update this page with the new hit-rate and MRR values

Examples:

- `FOXCTL_STORAGE_ROOT=... FOXCTL_VAULT_PATH=... bash ./scripts/eval_retrieval_foxctl.sh --save --save-dir /tmp/retrieval-docs`
- `JIDO_WORKSPACE=/path/to/jido bash ./scripts/eval_retrieval_jido.sh --save --save-dir /tmp/retrieval-docs`
- `PRAZE_WORKSPACE=/path/to/praze bash ./scripts/eval_retrieval_praze.sh --save --save-dir /tmp/retrieval-docs`

## Alert Gating

To turn threshold misses into a non-zero exit:

- `bash ./scripts/eval_retrieval_foxctl.sh --fail-on-alerts`

The command also supports:

- `--policy-file <path>`

When a policy file includes thresholds, the eval result now includes:

- `policy`
- `policy_file`
- `alerts`
