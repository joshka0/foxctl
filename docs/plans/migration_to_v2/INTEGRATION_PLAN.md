# V2 Migration Integration Plan

> **Last Updated**: 2026-01-08
> **Purpose**: Map completed work to new architecture docs, identify gaps and refactors

---

## Status Summary

### Completed (in this branch)

| Component | Status | Location |
|-----------|--------|----------|
| Hook v1 Types | ✅ | `internal/runtime/hooks/types.go` |
| Hook Dispatcher | ✅ | `internal/runtime/hooks/dispatcher.go` |
| Hook Merge Logic | ✅ | `internal/runtime/hooks/merge.go` |
| Shell Adapter | ✅ | `internal/runtime/hooks/shell_runner.go` |
| Skill Adapter | ✅ | `internal/runtime/hooks/skill_runner.go` |
| Hook Registry | ✅ | `internal/runtime/hooks/registry.go` |
| Mailbox Watcher | ✅ | `internal/actor/watcher.go` |
| Supervisor | ✅ | `internal/actor/supervisor.go` |
| AgentEngine Interface | ✅ | `internal/runtime/engine/engine.go` |
| DSPyAdapter | ✅ | `internal/runtime/engine/dspy_adapter.go` |
| LLMChatEngine | ✅ | `internal/runtime/engine/llmchat_engine.go` |
| ToolRunner | ✅ | `internal/runtime/engine/tool_runner.go` |
| AgentActor (renamed from DspyActor) | ✅ | `internal/actor/agent_actor.go` |
| Hooks in Actor Loop | ✅ | `internal/actor/agent_actor.go` |

### Required by New Docs (Gaps)

| Component | Doc Source | Priority | Effort |
|-----------|------------|----------|--------|
| `hooks/dispatch` skill | `dispatcher-hooks.md` | **P0** | Medium |
| `hooks.yaml` config loader | `hooks-interface.md` | **P0** | Medium |
| Unified CC adapter script | `hooks-interface.md` | **P1** | Small |
| Skills cross-platform refactor | `skills-refactor.md` | **P1** | Medium |
| Session lineage schema | `migration-plan.md` Phase 4 | **P2** | Medium |
| Progressive memory L0/L1/L2 | `migration-plan.md` Phase 5 | **P2** | Large |

---

## Gap Analysis: New Docs vs Current Implementation

### 1. hooks/dispatch Skill

**New Docs Say** (`dispatcher-hooks.md`):
- Single `hooks/dispatch` skill that loads `hooks.yaml`, matches hooks, runs them, merges outputs
- Adapters (CC/OC) become thin wrappers that call this skill
- Returns `data.hook_output` with merged result

**Current State**:
- We have `internal/runtime/hooks/dispatcher.go` with `Dispatcher` interface
- It's a Go library, not a standalone skill
- Shell hooks call skills directly, not through a central dispatcher

**Gap**: Need to create `skills/hooks_dispatch/` that:
1. Accepts `hook.Input` JSON
2. Loads `hooks.yaml` from workspace/global
3. Matches hooks for event + tool
4. Runs matched hook skills via executor
5. Merges outputs using existing merge logic
6. Returns `data.hook_output`

**Refactor Plan**:
```
skills/hooks_dispatch/
├── main.go           # Skill entry point
├── config.go         # hooks.yaml loader
├── matcher.go        # Event + tool matching
└── runner.go         # Run hooks and merge
```

### 2. hooks.yaml Config Support

**New Docs Say** (`hooks-interface.md`):
```yaml
version: 1
defaults:
  timeout_ms: 2500
  ephemeral: true
hooks:
  - event: PreToolUse
    match:
      tool_name: "^(Edit|Write)$"
      tool_kind: "write"
    run:
      - skill: hooks/task_guard
      - skill: hooks/file_guard
```

**Current State**:
- We have `internal/runtime/hooks/registry.go` but it doesn't load YAML config
- Hooks are registered programmatically or via shell scripts in `.claude/settings.json`

**Gap**: Need config loader that:
1. Loads from `<workspace>/.agentctl/hooks.yaml` then `~/.agentctl/hooks.yaml`
2. Supports `match.tool_name` (regex), `match.tool_kind`, `match.tool_canonical`
3. Supports `run[]` with ordered skills
4. Supports `timeout_ms`, `fail_mode` per hook

**Refactor Plan**:
```go
// internal/runtime/hooks/config.go
type HooksConfig struct {
    Version  int            `yaml:"version"`
    Defaults HookDefaults   `yaml:"defaults"`
    Hooks    []HookSpec     `yaml:"hooks"`
}

type HookSpec struct {
    Event    string      `yaml:"event"`
    Match    HookMatch   `yaml:"match"`
    Run      []HookRun   `yaml:"run"`
    Priority int         `yaml:"priority"`
}

type HookMatch struct {
    ToolName      string `yaml:"tool_name"`      // regex
    ToolCanonical string `yaml:"tool_canonical"` // regex
    ToolKind      string `yaml:"tool_kind"`      // read|write|exec|search|any
    PromptRegex   string `yaml:"prompt_regex"`   // optional
}
```

### 3. Unified CC Adapter Script

**New Docs Say** (`hooks-interface.md`):
- One script `agentctl-hook.sh` handles all CC events
- Normalizes CC payload → `hook.Input`
- Calls `agentctl run hooks/dispatch`
- Translates `hook.Output` → CC response

**Current State**:
- Multiple shell scripts per hook type
- Each script has its own payload parsing

**Gap**: Create single adapter at `configs/hooks/claude/agentctl-hook.sh`

**Impact**: Simplifies Claude Code settings to one entry per event type

### 4. Skills Cross-Platform Refactor

**New Docs Say** (`skills-refactor.md`):
1. Accept both `file_path` and `path` everywhere
2. Update write detection for CC + canonical tools
3. Add `workspace_id` alongside `workspace_root`
4. Standardize large output handling (preview + artifact)
5. Adopt consistent error codes

**Current State**:
- Some skills use `file_path`, others use `path`
- Write detection only recognizes CC tool names
- No `workspace_id` support

**Gap**: Update these hook skills:
- `hooks/task_guard` - path extraction, write detection
- `hooks/file_guard` - path extraction
- `hooks/impact_analysis` - path extraction, workspace detection
- `hooks/knowledge_router` - prompt extraction
- `hooks/test_feedback` - workspace ID handling

**Refactor Plan**:
```go
// internal/runtime/hooks/pathutil/extract.go
func ExtractPath(input json.RawMessage) string {
    // Try file_path, path, file, current_path in order
}

// internal/runtime/hooks/toolutil/classify.go
func IsWriteOperation(toolName, toolCanonical, toolKind string) bool {
    // CC: Edit, Write, MultiEdit, NotebookEdit
    // Canonical: edit.*
    // Kind: write
}
```

---

## Implementation Order

### Phase A: Central Dispatcher (P0)

**Goal**: Single dispatcher skill that all adapters call

1. **A1**: Add `hooks.yaml` config loader to `internal/runtime/hooks/`
   - YAML parsing with validation
   - Workspace + global config merge
   - Match resolution

2. **A2**: Create `skills/hooks_dispatch/` skill
   - Receives `hook.Input`
   - Loads config, matches hooks
   - Runs matched skills via executor
   - Merges outputs
   - Returns `data.hook_output`

3. **A3**: Create unified CC adapter script
   - Single `agentctl-hook.sh` for all events
   - Payload normalization
   - Calls `hooks/dispatch`
   - Response translation

### Phase B: Skills Compatibility (P1)

**Goal**: Hook skills work across CC/OC/agentctl runtime

4. **B1**: Add path extraction utility
   - `internal/runtime/hooks/pathutil/` package
   - Handles `file_path`, `path`, `file`, `current_path`

5. **B2**: Add tool classification utility
   - `internal/runtime/hooks/toolutil/` package
   - `IsWriteOperation()`, `IsSearchOperation()`, etc.

6. **B3**: Update hook skills to use utilities
   - `hooks/task_guard`
   - `hooks/file_guard`
   - `hooks/impact_analysis`
   - Others as needed

### Phase C: Session Lineage (P2)

**Goal**: Session identity propagation everywhere

7. **C1**: Session schema migration
   - Add `parent_session_id`, `agent_id`, `status` columns
   - Create `session_edges` table

8. **C2**: Environment propagation
   - `AGENTCTL_SESSION_ID`, `AGENTCTL_WORKSPACE`, `AGENTCTL_AGENT_ID`
   - Identity fallback file

### Phase D: Progressive Memory (P2)

**Goal**: Crash-safe L0/L1/L2 memory

9. **D1**: Actor turns schema
   - `actor_turns` table for L0
   - `actor_context_inbox` for injected context
   - `actor_memory_state` for cursors

10. **D2**: Cursor-based summarization
    - L0→L1 batch summarization
    - L1→L2 distillation
    - Crash recovery

---

## File Changes Summary

### New Files to Create

```
skills/hooks_dispatch/
├── main.go
├── skill.yaml
└── config.go

internal/runtime/hooks/
├── config.go         # hooks.yaml loader (NEW)
├── yaml_types.go     # YAML struct types (NEW)
└── loader.go         # Config file resolution (NEW)

internal/runtime/hooks/pathutil/
├── extract.go        # Path extraction (NEW)
└── normalize.go      # Path normalization (NEW)

internal/runtime/hooks/toolutil/
├── classify.go       # Tool classification (NEW)
└── kind.go           # Tool kind constants (NEW)

configs/hooks/claude/
└── agentctl-hook.sh  # Unified adapter (NEW)
```

### Existing Files to Modify

```
internal/runtime/hooks/registry.go     # Add hooks.yaml integration
internal/runtime/hooks/dispatcher.go   # Wire config loader

skills/hooks_task_guard/main.go      # Use pathutil, toolutil
skills/hooks_file_guard/main.go      # Use pathutil
skills/hooks_impact_analysis/main.go # Use pathutil, workspace
```

---

## Testing Strategy

### Unit Tests
- `TestConfigLoader_WorkspaceOverridesGlobal`
- `TestMatcher_ToolNameRegex`
- `TestMatcher_ToolKind`
- `TestPathExtract_AllVariants`
- `TestToolClassify_WriteOps`

### Integration Tests
- `TestDispatchSkill_PreToolUse`
- `TestDispatchSkill_StopRequested`
- `TestCCAdapter_NormalizesPayload`
- `TestCCAdapter_TranslatesResponse`

### End-to-End Tests
- CC Edit hook triggers task_guard via dispatch
- CC Stop hook triggers continuation via dispatch
- OC write hook triggers file_guard via dispatch

---

## Rollout Plan

1. **Week 1**: Phase A (central dispatcher)
   - A1: hooks.yaml loader
   - A2: hooks/dispatch skill
   - A3: CC adapter script
   - Feature flag: `AGENTCTL_DISPATCH_V2=0`

2. **Week 2**: Phase B (skills compat)
   - B1-B3: Path/tool utilities + skill updates
   - Test with existing hooks

3. **Week 3**: Cutover
   - Enable `AGENTCTL_DISPATCH_V2=1`
   - Update `.claude/settings.json` to use unified adapter
   - Deprecate old shell hooks

4. **Later**: Phase C/D (lineage + memory)
   - Separate PRs after dispatcher is stable

---

## Questions to Resolve

1. **Config priority**: Should workspace config completely override global, or merge?
   - Recommendation: Merge (add hooks, don't replace)

2. **Fail mode default**: Should hooks fail open or closed by default?
   - Recommendation: Fail open (current behavior)

3. **Action execution**: Should `hooks/dispatch` execute actions or return them?
   - Recommendation: Return them (let caller decide)

4. **Backward compat**: How long to support old shell hooks?
   - Recommendation: Indefinite via adapter pattern
