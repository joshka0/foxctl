---
title: Compact Handoff Pattern
type: pattern
project: agentctl
status: reviewed
trust: canonical
tags:
  - aca
  - handoff
paths:
  - internal/context/contextplane/
primary_anchor_path: internal/context/contextplane/store.go
symbols:
  - WorkspaceStore
provenance_refs:
  - observation:O-887
updated: 2026-03-09
---

# Compact Handoff Pattern

Compact handoffs work better than swollen transcripts for bounded worker phases.

## Recent Findings

- Keep handoff summaries short and evidence-backed.
