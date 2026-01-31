# Phase X: Embedding Input Safety (Secret Scanning)

> **Goal:** Prevent secrets and sensitive tokens from being embedded or persisted.

## Overview

Embeddings can capture sensitive text from comments and docs. This phase adds a scan-and-redact step for any embedding input (symbols, file summaries, docs, query logs).

**Dependencies:** Phase 0 (standards)
**Estimated PRs:** 1

---

## PR X.1: Secret Scan on Embedding Inputs

### Summary

Add a secret scanning pass for embedding inputs before enqueue. Support redact or block modes, and emit structured observability events for audit.

### Deliverables

- `secretutil.ScanText()` (or wrapper around existing scanner)
- Policy config: `embedding.secret_policy = allow|redact|block` (default: redact)
- Event: `embedding.input_secret_detected`

### Integration Points

- Symbol embedding enqueue (Phase 2)
- File summary embedding enqueue (Phase 3)
- Query logging (if stored)

### Event Shape

```json
{
  "operation": "embedding.input_secret_detected",
  "status": "error",
  "component": "embedding",
  "data": {
    "workspace": "...",
    "symbol": "...",
    "file": "...",
    "severity": "high",
    "policy": "redact"
  }
}
```

### Acceptance Criteria

- [ ] Embedding inputs are scanned before enqueue
- [ ] Redaction preserves structure while removing secret payloads
- [ ] Block mode prevents enqueue and records an error event
- [ ] Policy is configurable via config/env
