# ACA Self-Corrective Loop

Status: first slice

This note captures the first implemented step toward making ACA retrieval
self-corrective.

## Goal

When ACA retrieval misses an expected path, the system should not rely only on
human intuition to recover. It should:

1. record the miss
2. classify the likely root cause
3. propose a deterministic corrective action
4. optionally apply the safest corrective action

## Current Slice

Implemented through:

- `foxctl context retrieve-inspect`
- `foxctl context retrieve-inspect-suite`

That command:

1. runs ACA retrieval for a query
2. compares the result against expected repo or note paths
3. emits a structured observation
4. classifies the miss into one of:
   - `matched`
   - `package_note_fallback_disabled`
   - `missing_package_note`
   - `bridge_metadata_gap`
   - `ranking_mismatch`
   - `missing_vault_coverage`
5. proposes one deterministic correction:
   - policy patch
   - package note target
   - repo-path metadata patch
   - manual review

The suite variant runs the same classification logic across a retrieval eval
suite, summarizes the miss classes, and can apply the same safe
`package_note_fallback` patch once before rerunning the suite.

Current suite behavior:

- persists the full report to CAS
- returns an artifact digest plus inline summary
- records a first-class ACA correction run row with suite, artifact digest,
  summary, policy outcome, and draft count
- can compare the patched suite against an optional control suite
- reverts the policy patch if the target suite does not improve, does not remove
  the targeted `package_note_fallback_disabled` miss class, or the control
  suite regresses
- can draft promotion notes automatically for repeated `missing_package_note`
  observations when `--draft-when-promotable` is enabled

ACA hit accounting now treats note `repo_paths` as valid evidence for repo-file
targets, so canonical package notes can satisfy repo-path expectations without
requiring the note path itself to be the expected target.

Read surfaces:

- `foxctl context retrieve-inspect-runs`
  - list or fetch persisted ACA correction runs
- `foxctl context retrieve-inspect-artifact --artifact <digest>`
  - expand the full CAS-backed correction report

Correction-effectiveness eval:

- `foxctl eval corrections --suite foxctl-inspectors --workspace <repo> --vault-path <vault>`
  - scores expected `classification`
  - scores expected fix-family substring matches when a fix is expected

## Safe Auto-Apply

The only auto-apply path in this slice is:

- enabling `aca.package_note_fallback`

via:

```bash
foxctl context retrieve-inspect \
  --workspace . \
  --vault-path "/path/to/vault" \
  --query "storage memory package" \
  --expected-path internal/storage/memory/store.go \
  --apply \
  --apply-policy-patch
```

That path:

- persists the miss as an ACA observation
- patches `.foxctl/policy/retrieval.yaml`
- reruns the retrieval
- returns the post-patch inspection result

For suite-level inspection, the equivalent command is:

```bash
foxctl context retrieve-inspect-suite \
  --workspace . \
  --vault-path "/path/to/vault" \
  --suite foxctl-mixed \
  --control-suite praze-mixed \
  --apply \
  --apply-policy-patch \
  --draft-when-promotable
```

## Why This Shape

This keeps the loop auditable and deterministic.

The command does not:

- rewrite canonical notes automatically
- rewrite arbitrary prompt text
- mutate bridge metadata without review

Instead it uses ACA’s existing safe surfaces:

- observations
- retrieval policy
- promotion drafts

## Next Steps

1. add batch/suite-level ACA miss ingestion from retrieval evals
2. let repeated `missing_package_note` observations draft note proposals
3. evaluate proposed fixes against control suites before promotion
