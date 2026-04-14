---
title: Refactor Scout and ACA
type: map
status: draft
trust: reviewed
provenance_refs:
  - repo:foxctl
updated: 2026-03-30
---

# Refactor Scout and ACA

`foxctl` has a local refactor workflow that should feed ACA rather than live
outside it.

## What Exists

- `foxctl refactor scout`
- `foxctl refactor advisor`

Primary seam kinds:

- `workflow_abstraction`
- `thin_wrapper_api_layer`
- `shared_operation_family`

Primary usage:

1. run the scout on a narrow single-language scope
2. inspect the top seams and hotspots
3. choose one seam family or function hotspot
4. promote durable conclusions into the knowledge plane

## ACA Placement

- `L0` Active Run:
  current scout invocation and current shortlist
- `L1` Top of Mind:
  current top seam families worth acting on
- `L2` Operational Memory:
  accepted vs rejected seam judgments and why
- `L3` Durable Knowledge Graph:
  recurring structural lessons about the repo

## Navigation

- [[active-frontier]]
- [[project-index]]
