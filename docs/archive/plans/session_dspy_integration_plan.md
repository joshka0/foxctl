# Plan: Session ID Propagation + DSPy Optimization Integration

## Overview

Unify session tracking across all foxctl storage systems and add end-of-session elicitation for DSPy trajectory optimization.

## Goals

1. **Session ID everywhere** - Propagate `claude_session_id` from hooks through all entities
2. **Cross-scope correlation** - Enable queries like "what tasks were created in session X?"
3. **End-of-session feedback** - Collect human rating (1-5) + optional feedback via Stop hook
4. **DSPy optimization** - Use successful trajectories to improve agent prompts
5. **Claude API integration** - Evaluate official Anthropic SDK vs dspy-go built-in

---

## Part 1: Session ID Propagation

### Current State

| Component | Has session_id? | Source |
|-----------|-----------------|--------|
| `hook.Input` | ✓ | Claude Code |
| `sessions` table | ✓ (is the session) | - |
| `tasks` | ❌ | needs migration |
| `trajectories` | ❌ | needs migration |
| `trajectory_events` | ❌ | needs migration |
| `user_requests` | ❌ | needs migration |
| `named_memory` | ❌ | needs migration |
| `blackboard` | ❌ | optional |
| `board_messages` | ❌ | optional |

### Schema Migrations

```sql
-- tasks
ALTER TABLE tasks ADD COLUMN claude_session_id TEXT;
CREATE INDEX idx_tasks_session ON tasks(claude_session_id);

-- trajectories
ALTER TABLE trajectories ADD COLUMN claude_session_id TEXT;
CREATE INDEX idx_trajectories_session ON trajectories(workspace_id, claude_session_id);

-- trajectory_events (via trajectory_id FK, no change needed)

-- user_requests
ALTER TABLE user_requests ADD COLUMN claude_session_id TEXT;

-- named_memory
ALTER TABLE named_memory ADD COLUMN claude_session_id TEXT;
CREATE INDEX idx_memory_session ON named_memory(claude_session_id);
```

### Propagation Points

1. **Hooks receive session_id** (`hook.Input.SessionID`)
2. **RunnerContext carries session_id** (add to skillslib/runner)
3. **Task creation** (todo/manage skill)
4. **Memory creation** (memory/save, remember hooks)
5. **Trajectory capture** (trajectorycapture package)

### Files to Modify

- `internal/domain/hook/types.go` - already has SessionID ✓
- `internal/adapters/skillslib/runner/context.go` - add SessionID field
- `internal/storage/tasks/store.go` - add migration + column
- `internal/storage/trajectory/store.go` - add migration + column
- `internal/storage/memory/store.go` - add migration + column
- `skills/todo_manage/main.go` - propagate to task creation
- `internal/runtime/trajectorycapture/capture.go` - propagate to trajectory

---

## Part 2: End-of-Session Elicitation

### Hook Configuration

Add to `.claude/settings.json`:

```json
{
  "hooks": {
    "Stop": [{
      "matcher": "",
      "hooks": [{
        "type": "command",
        "command": ".claude/hooks/session-feedback.sh"
      }]
    }]
  }
}
```

### Feedback Skill

Create `skills/session_feedback/`:

**Input:**
```json
{
  "session_id": "abc123",
  "transcript_path": "~/.claude/projects/.../00893aaf.jsonl"
}
```

**Flow:**
1. Parse transcript to extract trajectory
2. Find matching trajectory in DB by session_id
3. Prompt user for rating (1-5) via terminal
4. Optional: prompt for text feedback
5. Store outcome via `trajectory.Store.SetOutcome()`

**Output:**
```json
{
  "session_id": "abc123",
  "trajectory_id": "01HXYZ...",
  "rating": 4,
  "feedback": "Good but missed one edge case"
}
```

### Implementation Files

- `skills/session_feedback/main.go`
- `skills/session_feedback/skill.yaml`
- `.claude/hooks/session-feedback.sh`
- `.claude/skills/session-feedback/Skill.md`

---

## Part 3: DSPy Trajectory Optimization

### Existing Infrastructure

Already have:
- `internal/storage/trajectory/store.go` - stores trajectories with outcomes
- `internal/agent/optimization/mcp_collector.go` - learns tool patterns
- `internal/agent/optimization/pattern_store.go` - persists patterns
- `trajectory.Outcome` type with `HumanRating`, `Feedback`, `Success`

### Integration Flow

```
┌─────────────────────────────────────────────────────────────┐
│  Session Ends → Stop Hook → session_feedback skill          │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  trajectory.Store.SetOutcome(ctx, wsID, trajID, outcome)    │
│  - HumanRating: 1-5                                         │
│  - Feedback: optional text                                  │
│  - Success: derived from rating >= 3                        │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  Optimization Pipeline (periodic or on-demand)              │
│  1. Query: ListByOutcome(filter{MinRating: 4})              │
│  2. Extract: successful tool call sequences                 │
│  3. Learn: MCPPatternCollector.CollectFromTrajectory()      │
│  4. Apply: Update agent prompts with learned patterns       │
└─────────────────────────────────────────────────────────────┘
```

### Optimization Skill

Create `skills/optimize_from_feedback/`:

```go
func run(ctx context.Context) error {
    // Get highly-rated trajectories from last 7 days
    trajs, _ := trajStore.ListByOutcome(ctx, OutcomeFilter{
        MinRating: 4,
        Since:     time.Now().AddDate(0, 0, -7),
    })

    for _, traj := range trajs {
        // Extract patterns from successful runs
        collector.CollectFromTrajectory(ctx, traj)
    }

    // Get optimized hints for agents
    hints, _ := collector.GetHints(ctx, "researcher", "code exploration")

    return nil
}
```

---

## Part 4: Claude API Integration (Research)

### Option A: Keep dspy-go Built-in (Recommended Initially)

Already works:
```go
case "anthropic":
    config := core.ProviderConfig{Name: "anthropic", APIKey: apiKey}
    llm, err = llms.NewAnthropicLLMFromConfig(ctx, config, core.ModelID(model))
```

Pros:
- Zero changes needed
- Optimizers work out of the box
- Battle-tested

Cons:
- Uses older `anthropic-go v0.0.8`
- Missing newer Claude features

### Option B: Official Anthropic SDK Adapter

Create adapter for `anthropics/anthropic-sdk-go` (v1.19.0):

```go
type AnthropicAdapter struct {
    client *anthropic.Client
    model  string
}

func (a *AnthropicAdapter) Generate(ctx context.Context, prompt string, opts ...core.GenerateOption) (*core.LLMResponse, error) {
    msg, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
        Model:     anthropic.F(a.model),
        MaxTokens: anthropic.Int(4096),
        Messages:  anthropic.F([]anthropic.MessageParam{...}),
    })
    // Convert to core.LLMResponse
}
```

Pros:
- Latest features (extended thinking, tool use streaming)
- Bedrock/Vertex support
- Active maintenance

Cons:
- Need to maintain adapter
- Test optimizer compatibility

### Recommendation

Start with Option A (no changes). Evaluate Option B when:
- Need extended thinking beta
- Need Bedrock/Vertex AI
- Need streaming tool use

---

## Implementation Order

### Phase 1: Session ID Propagation
1. Add migrations to tasks, trajectories, memory stores
2. Update RunnerContext to carry session_id
3. Propagate session_id in skills (todo_manage, memory)
4. Update trajectorycapture to use session_id

### Phase 2: End-of-Session Feedback
1. Create session_feedback skill
2. Create Stop hook wrapper
3. Test feedback flow end-to-end
4. Add to settings.json

### Phase 3: Optimization Pipeline
1. Add optimization skill
2. Integrate with pattern collector
3. Add CLI command: `foxctl optimize`
4. Test with sample trajectories

### Phase 4: (Optional) Claude SDK Adapter
1. Evaluate need based on feature requirements
2. Create adapter if needed
3. Test optimizer compatibility

---

## Testing Strategy

1. **Unit tests** - Each migration, store method
2. **Integration tests** - Hook → skill → store flow
3. **E2E test** - Full session with feedback
4. **Manual test** - Run Claude Code session, rate it, verify storage

---

## Environment Variables

```bash
# Enable feedback collection (default: true in terminal)
FOXCTL_SESSION_FEEDBACK=true

# Minimum rating threshold for optimization (default: 4)
FOXCTL_OPTIMIZATION_MIN_RATING=4

# Days to look back for optimization (default: 7)
FOXCTL_OPTIMIZATION_WINDOW_DAYS=7
```

---

## Success Criteria

1. [ ] All entities created in a session are tagged with `claude_session_id`
2. [ ] Can query: "show me all tasks from session X"
3. [ ] Stop hook prompts for rating on session end
4. [ ] Ratings stored in trajectory outcomes
5. [ ] Optimization pipeline extracts patterns from high-rated sessions
