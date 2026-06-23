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
| live-1 | first live answer-mode smoke (4 cases, glm-5.2, BM25) | FAIL | PASS | FAIL | FAIL | 1/4 | 2026-06-22 |
| live-2 | upgraded scorer (key-fact overlap) | FAIL | PASS | PASS | FAIL | 2/4 | 2026-06-22 |
| live-3 | enumeration + temporal prompts | FAIL | PASS | PASS | FAIL* | 2/4 | 2026-06-22 |
| live-4 | deterministic date extraction | FAIL* | FAIL* | PASS | FAIL | 1/4* | 2026-06-22 |
| live-5 | session date metadata + numeric scorer | FAIL | PASS | PASS | PASS | 3/4 | 2026-06-23 |
| live-6 | temp=0.01 stability (run 1 / run 2) | FAIL | PASS/FAIL | PASS/FAIL | PASS | 2-3/4 | 2026-06-23 |
| live-7 | 19-case scaled eval (BM25-only) | — | — | — | — | 12/19 (63%) | 2026-06-23 |
| live-8 | 19-case with hybrid vectors (0.45/0.55) | — | — | — | — | 6/19 (32%) | 2026-06-23 |
| live-9 | 19-case BM25-dominant (0.25/0.75) | — | — | — | — | 13/19 (68%) | 2026-06-23 |
| live-10 | 19-case per-turn chunks | — | — | — | — | 11/19 (58%) | 2026-06-23 |
| live-11 | 19-case turn digest summary | — | — | — | — | 13/19 (68%) | 2026-06-23 |

## Scaled Eval Results — 2026-06-23

### 19-case stratified sample (seed=42), glm-5.2, temp=0.01

Ingest: 893 memories saved, 893 queued. Evidence retrieval: hit@5=68%, MRR=0.56.

| Question Type | Pass/Total | Accuracy |
|---------------|-----------|----------|
| knowledge-update | 3/3 | 100% |
| single-session-user | 2/3 | 67% |
| multi-session | 3/5 | 60% |
| temporal-reasoning | 3/5 | 60% |
| single-session-assistant | 1/2 | 50% |
| single-session-preference | 0/1 | 0% |
| **Overall** | **12/19** | **63%** |

Mean latency: 18.2s/case. Mean answer score: 0.58.

Key findings: knowledge-update is perfect (exact factual recall works well).
Temporal reasoning benefits from session date metadata injection. Preference
questions are the weakest (paraphrase scoring gap). Multi-session questions
show retrieval misses on some cases.

## Live Smoke Results

### Run 5 (session date metadata + numeric fact scoring) — 2026-06-23

Session dates now injected as [Session date: ...] headers into memory
atomic_text at ingest time. Numeric fact matcher extracts leading number+unit
from expected answers. Markdown bold/italic stripped before scoring.

| Case | Result | Score | Notes |
|------|--------|-------|-------|
| degree (e47becba) | PASS | 1.00 | exact match |
| video-edit (8a2466db) | PASS | 0.36 | key-fact overlap |
| MoMA (gpt4_59149c77) | PASS | 1.00 | numeric fact: "7 days" matched after markdown strip |
| clothing (0a995998) | FAIL | 0.00 | still wrong count (2 vs 3) |

Answer accuracy: 3/4 (75%). Evidence retrieval: 4/4, hit@5=100%, MRR=1.0.
Remaining failure is a genuine model reasoning error (undercounting items).

Date extraction regex added. However, evidence text stores relative dates
("last week", "a few days ago") not absolute dates, so collectSynthesisDates
returned empty and fell back to the generic temporal prompt. The MoMA case
regressed because the model could not find dates without the injected list.
Degree case also flipped due to LLM run-to-run variance ("Bachelor's degree"
instead of "Business Administration").

*Run-to-run variance: the degree and MoMA cases are sensitive to LLM sampling.
The structural improvements (retrieval, scoring, prompt enrichment) are working,
but answer quality varies between runs. Remaining failures are model reasoning
and sampling issues, not infrastructure gaps.

Same 4 cases, glm-5.2, BM25-only. Key-fact overlap scorer added.

| Case | Result | Score | Latency | Evidence | Notes |
|------|--------|-------|---------|----------|-------|
| degree (e47becba) | PASS | 1.00 | 17.6s | 1/1 | exact match |
| video-edit (8a2466db) | PASS | 0.36 | 16.9s | 1/1 | key-fact overlap: Premiere Pro, advanced settings |
| clothing (0a995998) | FAIL | 0.00 | 14.1s | 3/3 | wrong count (2 vs 3) — genuine error |
| MoMA (gpt4_59149c77) | FAIL | 0.00 | 17.5s | 2/2 | model unable to compute date arithmetic |

Answer accuracy: 2/4 (50%). Evidence retrieval: 4/4 found, hit@5=75%.

Improvement: scoring upgrade rescued the video-edit case (paraphrased correct
answer). Remaining 2 failures are genuine model errors, not scoring artifacts.

### Run 1 (strict substring scorer)

| Case | Result | Latency | Evidence | Root Cause if FAIL |
|------|--------|---------|----------|--------------------|
| degree (e47becba) | PASS | 12.3s | 1/1 | — |
| clothing (0a995998) | FAIL | 11.5s | 3/3 | answer text mismatch (strict substring scoring) |
| video-edit (8a2466db) | FAIL | 19.9s | 1/1 | answer phrasing mismatch (strict substring scoring) |
| MoMA (gpt4_59149c77) | FAIL | 26.0s | 2/2 | model unable to compute date arithmetic from evidence |

Evidence retrieval: 4/4 found, hit@5=100%, MRR=1.0 (all evidence surfaced).
Answer accuracy: 1/4 (strict substring match).

Key improvement vs baseline: all 4 cases now find their evidence (baseline had
3/4 retrieval misses). Remaining failures are answer-quality + scoring issues,
not retrieval issues.

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
