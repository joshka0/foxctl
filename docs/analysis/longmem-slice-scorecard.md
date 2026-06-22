# LongMem Slice Scorecard

> Tracks per-slice progress against the 4 canonical LongMem smoke cases.
> Companion to
> [long-term-agent-worktree-status-2026-06-11.md](long-term-agent-worktree-status-2026-06-11.md)
> and
> [longmem-comparative-review-2026-06-13.md](longmem-comparative-review-2026-06-13.md).

## Canonical Cases

| Case | Question (abbreviated) | Failure Mode | Root Cause |
|------|------------------------|--------------|------------|
| coupon | "What coupon did I redeem..." | ledger false-negative | regex value extraction strips entity near noise words; `directAnswer` gate blocks row |
| degree | "What degree did I get?" | retrieval query-planning miss | no HyDE decomposition; single-pass; named-memory is fallback lane |
| model-kit | "Do I have any model kits?" | semantic scope miss | candidate lifecycle gate hardcoded at 0.9 suppresses synonymy before ledger |
| daily-commute | "How long is my commute?" | (positive case — last known: correct) | works: direct fact match |

## Score History

Each row is a committed slice. Score is answer-mode correct/total (1/4 baseline).

| Slice | Description | coupon | degree | model-kit | commute | Score | Date |
|-------|-------------|--------|--------|-----------|---------|-------|------|
| baseline | pre-slice state (last live run) | MISS | MISS | MISS | PASS | 1/4 | 2026-06-11 |
| 1a | feedback event mapping + ApplyInvalidation promotion | — | — | — | — | — | done |
| 1b | runner auto-emits AnswerAccepted feedback | — | — | — | — | — | done |
| 2 | adaptive candidate gate + parallel BM25 | — | — | — | — | — | done |
| 3 | ledger semantic answer-value classification | — | — | — | — | — | done |
| 4 | REQUIRED_DATA fallback re-query | — | — | — | — | — | done |
| 5 | HyDE decomposition + named-memory first-class lane | — | — | — | — | — | done |
| 6 | hybrid eval DefaultDeps + scorecard harness | — | — | — | — | — | done |

## Running the Harness

```bash
# Retrieval-only (no LLM calls, deterministic):
make longmem-slice-retrieval

# Answer-mode (requires RLM model config):
make longmem-slice-answer

# Specific slice regression:
make longmem-slice-test
```

## Scoring Notes

- Retrieval mode: deterministic hit@5/10/50/100 + MRR. No embedder (BM25-only in
  DefaultDeps). Records which expected memories surface and at what rank.
- Answer mode: exact/contains match + insufficiency-phrase guard + evidence-name
  membership. Requires `--answer-strategy retrieve-memory` and model config.
- Anti-leakage: enforced at ingest by `CheckLeakage`. Per-case findings recorded
  in artifacts.
- The eval `DefaultDeps` currently wires BM25-only retrieval (no vector path).
  Slice 6 upgrades this to hybrid. Until then, retrieval scores underrepresent
  production quality.
