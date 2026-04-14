# Agent Tools Expansion TODO

## Overview

Expand the tools available to DSPy ReAct agents and LLMChatEngine agents to enable richer research and context-gathering capabilities.

## Current State

### Registered Tools (internal/agent/tools/)

| Category | Tools | Status |
|----------|-------|--------|
| Agent | `agent.ask`, `agent.spawn` | ✅ Complete |
| Filesystem | `fs.read_file`, `fs.list_dir` | ✅ Complete |
| Code | `code.search`, `code.symbol_search`, `code.snippet_extract` | ✅ Complete |
| Edit | `edit.patch`, `edit.structured_diff`, `edit.create` | ✅ Complete |
| Test | test tools | ✅ Complete |
| Todo | `todo.query`, `todo.insights`, `todo.add`, `todo.complete`, `todo.set_active`, `todo.ensure_active` | ✅ Complete |
| Mail | `mail.send`, `mail.inbox`, `mail.ack`, `mail.reserve`, `mail.release` | ✅ Complete |
| Blackboard | `bb.post`, `bb.search`, `bb.claim`, `bb.release`, `bb.list`, `bb.watch` | ✅ Complete |

### Missing Tools

| Category | Needed Tools | Purpose |
|----------|--------------|---------|
| Session | `session.recall`, `session.list` | Semantic session retrieval |
| Memory | `memory.query`, `memory.put` | Named memory access |
| Codemap | `codemap.get`, `codemap.generate` | Code relationship maps |
| Optimization | `optimize.reflect`, `optimize.feedback` | Self-improvement |
| Trajectory | `trajectory.list`, `trajectory.get` | Execution history |

---

## Implementation Plan

### Phase 1: Session Tools

Add tools for semantic session retrieval (wrapping `session/recall` skill):

```go
// internal/agent/tools/session_tools.go
func (r *Registry) registerSessionTools() error {
    // session.recall - semantic search over past sessions
    recallTool := dstools.NewFuncTool(
        "session.recall",
        "Search past coding sessions for relevant context. Returns summaries and learnings from previous work.",
        models.InputSchema{
            Type: "object",
            Properties: map[string]models.ParameterSchema{
                "query": {
                    Type:        "string",
                    Description: "Natural language query describing what you're looking for",
                    Required:    true,
                },
                "limit": {
                    Type:        "integer",
                    Description: "Maximum number of sessions to return (default 5)",
                },
            },
        },
        r.wrapWithTelemetry("session.recall", r.sessionRecall),
    )
    return r.tools.Register(recallTool)
}
```

### Phase 2: Memory Tools

Add tools for named memory access (wrapping `memory/query` and `memory/put` skills):

```go
// internal/agent/tools/memory_tools.go
func (r *Registry) registerMemoryTools() error {
    // memory.query - search named memories
    queryTool := dstools.NewFuncTool(
        "memory.query",
        "Query stored memories (gotchas, decisions, learnings) for relevant context.",
        models.InputSchema{
            Type: "object",
            Properties: map[string]models.ParameterSchema{
                "query": {
                    Type:        "string",
                    Description: "Search query",
                    Required:    true,
                },
                "type": {
                    Type:        "string",
                    Description: "Memory type filter: gotcha, decision, learning, insight",
                },
                "limit": {
                    Type:        "integer",
                    Description: "Maximum results (default 10)",
                },
            },
        },
        r.wrapWithTelemetry("memory.query", r.memoryQuery),
    )

    // memory.put - store new memory
    putTool := dstools.NewFuncTool(
        "memory.put",
        "Store a new memory (gotcha, decision, learning) for future reference.",
        models.InputSchema{
            Type: "object",
            Properties: map[string]models.ParameterSchema{
                "name": {
                    Type:        "string",
                    Description: "Short identifier for the memory",
                    Required:    true,
                },
                "type": {
                    Type:        "string",
                    Description: "Memory type: gotcha, decision, learning, insight",
                    Required:    true,
                },
                "summary": {
                    Type:        "string",
                    Description: "Brief description of the memory",
                    Required:    true,
                },
                "content": {
                    Type:        "string",
                    Description: "Full content/details",
                },
            },
        },
        r.wrapWithTelemetry("memory.put", r.memoryPut),
    )

    return errors.Join(
        r.tools.Register(queryTool),
        r.tools.Register(putTool),
    )
}
```

### Phase 3: Codemap Tools

Add tools for code relationship exploration:

```go
// internal/agent/tools/codemap_tools.go
func (r *Registry) registerCodemapTools() error {
    // codemap.get - retrieve existing codemap
    // codemap.generate - create new codemap with AI tracing
}
```

### Phase 4: Trajectory Tools

Add tools for execution history access (for self-reflection):

```go
// internal/agent/tools/trajectory_tools.go
func (r *Registry) registerTrajectoryTools() error {
    // trajectory.list - list recent trajectories
    // trajectory.get - get trajectory details
    // trajectory.events - get events for a trajectory
}
```

---

## Wire Into Registry

Update `internal/agent/tools/tools.go`:

```go
func NewRegistry(cfg Config, recorder TelemetryRecorder) (*Registry, error) {
    // ... existing registrations ...

    // New tool categories
    if err := r.registerSessionTools(); err != nil {
        return nil, err
    }
    if err := r.registerMemoryTools(); err != nil {
        return nil, err
    }
    if err := r.registerCodemapTools(); err != nil {
        return nil, err
    }
    if err := r.registerTrajectoryTools(); err != nil {
        return nil, err
    }

    return r, nil
}
```

---

## Tool Allowlists by Role

Different agent roles should have different tool access:

| Role | Tools Available |
|------|-----------------|
| `researcher` | All read tools: session.recall, memory.query, codemap.*, code.*, fs.read_file |
| `coder` | All tools including edit.*, fs.*, todo.* |
| `reviewer` | Read tools + code analysis tools |
| `companion` | Conversation tools + limited code context |

Implementation:

```go
// internal/agent/tools/config.go
var RoleAllowlists = map[string][]string{
    "researcher": {
        "code.search", "code.symbol_search", "code.snippet_extract",
        "fs.read_file", "fs.list_dir",
        "session.recall", "memory.query",
        "codemap.get", "codemap.generate",
    },
    "coder": {
        "*", // all tools
    },
    "reviewer": {
        "code.*", "fs.read_file", "fs.list_dir",
        "session.recall", "memory.query",
    },
    "companion": {
        "memory.query", "session.recall",
    },
}
```

---

## Testing

```bash
# Test session recall tool
foxctl agent ask <researcher-id> --question "Use session.recall to find past work on authentication"

# Test memory query tool
foxctl agent ask <researcher-id> --question "Use memory.query to find gotchas about SQLite"

# Test codemap tools
foxctl agent ask <researcher-id> --question "Use codemap.generate to trace the session lifecycle"
```

---

## Dependencies

- Phase 1 (Session): Requires `session/recall` skill to be working
- Phase 2 (Memory): Requires `memory/query` and `memory/put` skills
- Phase 3 (Codemap): Requires `codemap/generate` and `codemap/get` skills
- Phase 4 (Trajectory): Direct DB access via trajectory.Store

---

## Files to Create

| File | Purpose |
|------|---------|
| `internal/agent/tools/session_tools.go` | Session recall/list tools |
| `internal/agent/tools/memory_tools.go` | Memory query/put tools |
| `internal/agent/tools/codemap_tools.go` | Codemap get/generate tools |
| `internal/agent/tools/trajectory_tools.go` | Trajectory list/get tools |
| `internal/agent/tools/session_tools_test.go` | Tests |
| `internal/agent/tools/memory_tools_test.go` | Tests |

---

## Acceptance Criteria

- [ ] Session tools working in DSPy agent
- [ ] Memory tools working in DSPy agent
- [ ] Codemap tools working in DSPy agent
- [ ] Trajectory tools working in DSPy agent
- [ ] Role-based allowlists enforced
- [ ] LLMChatEngine can also use new tools
- [ ] All tools have telemetry/trajectory capture
