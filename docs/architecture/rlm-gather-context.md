# RLM Gather Context

`gather_context` is the RLM-facing context controller for bounded repo, memory,
task, and session context. It is intentionally a thin RLM tool over
`internal/context/contextengine`; the RLM layer parses tool input and returns the
bundle, while retrieval, reduction, and certification stay in `contextengine`.

## Runtime Shape

```text
RLM model
  -> gather_context
      -> contextengine.GatherContext
          -> existing retrieval lanes
          -> EvidencePack(s)
          -> ContextBundle reducer
          -> ContextCertificate checks
  -> answer from the bounded ContextBundle
```

The tool returns a `ContextBundle`, not raw search output. Facts in the bundle
must cite evidence node IDs, and runtime certification decides whether the
bundle is `sufficient`, `partial`, or `blocked`.

## Source Lanes

The first implementation reuses existing contextengine lanes instead of adding a
parallel source-profile subsystem:

| Lane | Primary data |
| --- | --- |
| `code` | repoindex, semantic code search, exact local probes |
| `memory` | memory claims, with explicit trust-tier status filters |
| `context` | ACA top-of-mind, latest handoff, ACA vault/contextplane retrieval hits, and session recall when available |
| `task` | task store results |
| `mixed` | fused lane retrieval through existing `RetrieveMixed` behavior |

`load_evidence_ref` is the follow-up inspection tool for refs returned by the
bundle. `retrieve_code`, `retrieve_memory`, `retrieve_context`, and
`retrieve_mixed` remain raw retrieval/debug tools. The `context` lane reads the
RLM bootstrapper's ACA projection for top-of-mind state, adds the latest handoff
as first-class context evidence, and can append contextplane retrieval packs from
the Obsidian/ACA index when a vault path is configured.

## Regression Gate

Use the gather-context eval to test whether the controller finds the required
repo context with bounded emitted context:

```bash
env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=1 go build -tags=libsqlite3 -o /tmp/foxctl-rlm-cgo ./cmd/foxctl
/tmp/foxctl-rlm-cgo eval gather-context \
  --workspace . \
  --eval-dataset-file /tmp/gather-context-eval.jsonl \
  --lane code \
  --max-context-chars 6000 \
  --pass-threshold 0.8
```

To compare against a mini/subagent run, pass external agent JSONL records in the
same format accepted by `foxctl eval agents --external-results`:

```bash
/tmp/foxctl-rlm-cgo eval gather-context \
  --workspace . \
  --eval-dataset-file /tmp/gather-context-eval.jsonl \
  --lane mixed \
  --agent-baseline-results /tmp/mini-agent-results.jsonl
```

The report includes pass/path/fact deltas, mean duration speedup, and baseline
token/cost fields. This is the regression gate for the core claim: bounded
`gather_context` should recover repo context faster and with fewer model tokens
than a general subagent on the same cases.

Eval cases can assert both path recall and fact recall:

```json
{"id":"bundle-contract","question":"Where is ContextBundle defined and what contract matters?","expected_paths":["internal/context/contextengine/context_bundle.go"],"required_facts":["facts in the bundle must cite evidence node IDs"]}
```

For mixed-source cases, put per-case controls under `metadata`:

```json
{"id":"mixed-memory","question":"Recall the ContextBundle certification decision","required_facts":["runtime certification"],"metadata":{"lanes":["memory","context"],"memory_statuses":["current","candidate"],"goal":"mixed_context_eval"}}
```

Compare this report against `foxctl eval agents` runs for mini/subagent
baselines. The useful comparison metrics are pass rate, path/fact recall,
duration, emitted context chars, and token/cost fields from the agent eval
report.

## Invariants

- `gather_context` is read-only.
- ContextBundle facts must reference existing evidence IDs.
- Candidate, stale, contradicted, or unloadable evidence must downgrade or fail
  the certificate.
- Parent answerers should use the bundle first and only call raw retrieval tools
  for focused follow-up or debugging.
