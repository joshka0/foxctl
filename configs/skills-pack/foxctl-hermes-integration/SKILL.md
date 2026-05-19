---
name: foxctl-hermes-integration
description: "Hermes agent integration with foxctl: 71 tools for intelligence, coordination, flow orchestration, context curation, and vault management."
user-invocable: false
---

# Foxctl Hermes Integration

You have 71 foxctl tools across 11 categories. Use them to understand code,
coordinate with other agents, and maintain the context plane.

## Tool categories

### When you need code understanding
- `foxctl_repo_search`, `foxctl_repo_dag`, `foxctl_repo_expand` — repo index
- `foxctl_code_grep`, `foxctl_semantic_search`, `foxctl_code_symbols` — code analysis
- `foxctl_text_grep`, `foxctl_fs_read`, `foxctl_fs_find` — filesystem search
- `foxctl_codemap_list`, `foxctl_codemap_get` — code relationship maps

### When you need to remember or recall
- `foxctl_memory_search`, `foxctl_memory_put` — vector-indexed knowledge base
- `foxctl_session_recall` — past session context

### When you need to coordinate with other agents
- `foxctl_room_task_*` — claim, complete, block work items (see foxctl-agent-coordination)
- `foxctl_pipe_emit`, `foxctl_pipe_receive` — structured data channels
- `foxctl_agent_list`, `foxctl_room_status` — discover room participants
- `foxctl_publish_context` — broadcast your state

### When you need to orchestrate multi-agent workflows
- `foxctl_flow_*` — build and run DAG workflows (see foxctl-flow-orchestration)
- `foxctl_flow_build_pipeline` — linear agent chains
- `foxctl_flow_build_fan_out` — parallel agent fan-out

### When you need to maintain the context plane
- `foxctl_context_curator` — unified cleanup report (see foxctl-context-curator)
- `foxctl_context_*` — observations, tensions, handoffs, inference
- `foxctl_vault_*` — vault search, promote, bridge

### When you need room-agile lifecycle
- `foxctl_epic_*`, `foxctl_milestone_show` — epic management
- `foxctl_story_*` — story lifecycle (start, review, validate)

## Proactive behavior

At session start or periodically during idle time:

1. **Check inbox**: `foxctl_room_inbox()` — any messages from other agents?
2. **Check tasks**: `foxctl_room_task_list(status="pending")` — unclaimed work?
3. **Run curator**: `foxctl_context_curator()` — context plane healthy?
4. **Publish state**: `foxctl_publish_context()` — let others know what you're doing

## Related skills

- `foxctl-room-agent` — base room participant protocol
- `foxctl-agent-coordination` — full coordination stack reference
- `foxctl-flow-orchestration` — multi-agent DAG workflows
- `foxctl-context-curator` — context plane maintenance
- `foxctl-room-agile` — agile lifecycle in rooms
