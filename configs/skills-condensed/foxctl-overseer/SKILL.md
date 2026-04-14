---
name: foxctl Overseer
description: Multi-agent orchestration with task scoring, graph analysis, and recommendations. Prioritize tasks using PageRank and critical path.
---

# Overseer Coordination

Intelligent task prioritization combining graph metrics with coordination signals.

## Get Recommendations

```bash
foxctl run todo/manage --input '{"operation": "recommend", "limit": 5}'
```

Returns scored tasks with: `score`, `critical_path`, `pagerank`, `unread_admin`, `unread_overseer`

## Scoring Formula

- **30%** Critical path position
- **20%** PageRank centrality
- **25%** Admin message count
- **15%** Overseer message count
- **10%** Recency factor

## Graph Analysis

```bash
foxctl run todo/manage --input '{"operation": "graph_insights"}'
```

Returns: `pagerank`, `critical_path`, `cycles`, `topological_order`

## Agent Hierarchy

```
Depth 0: Overseer (planner)
  └── Depth 1: Agent (coder)
       └── Depth 2: Subagent (coder)
```

Default max depth: 3 levels

Full docs: `~/.foxctl/share/configs/skills/foxctl-overseer/Skill.md`
