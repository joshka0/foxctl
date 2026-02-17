# Implementation Plan: GUI Agent List Redesign

> Status: A+ (plan-build-rp, 3 iterations) | Research: Opus scout-research (8 scouts + 2 Opus reviewers)

## Problem Statement

- The GUI sidebar shows a flat list of agents with random generated names ("clever-peak", "warm-maple") and no grouping
- When spawning 8 scouts for 2 research topics, they appear as 8 ungrouped items with names that convey zero context
- Stopped agents accumulate forever with no archival or batch cleanup
- The `parent_id` hierarchy field exists but is unused (deferred to P3)

## Scope and Locked Decisions

- Display name priority is **role-first**: `agent.role || agent.name || agent.slug || 'Agent'`
- Subtitle: `prompt_summary` if available, otherwise machine-name fallback (`agent.name || agent.slug || agent.id.slice(0, 8)`)
- Heartbeat fallback: zero-time (`0001-01-01T...`) and empty/invalid timestamps fall back to `created_at`
- Group order: **Active → Errored → Recent → Archive** (static)
- Active/Errored default **open**; Recent/Archive default **collapsed** (one-click expand)
- Recency window: hardcoded 24h
- Batch actions (Kill All Stopped / Trash Old): in P2 scope
- Parent/child hierarchy: **P3 future work only**

## Architecture Decision

**Approach: Shared utility module + status-based collapsible sections**

- Centralize all agent display logic (names, icons, timestamps) in a single utility module
- Replace the flat `agents.map()` in ConversationsList with CollapsibleSection groups
- Extract per-agent rendering into a reusable `AgentConversationGroup` component

**Why**: The display pattern `agent.name || agent.slug || agent.role || 'Agent'` is duplicated in 4 files. Centralizing prevents drift. Collapsible sections use existing UI patterns and scale to 50+ agents.

**Rejected alternatives**:
- Virtual scrolling: Overkill — collapsible sections with archive collapse handles scale
- Redux/Zustand state: Agent sections are derived data — `useMemo` is sufficient

## Design Patterns

| Pattern | Where Applied | Why |
|---------|---------------|-----|
| **Utility Module** | `agent-utils.ts` | Single source of truth for display, icons, timestamps |
| **Derived State** | `agentSections` useMemo | Sections computed from agent list without extra state |
| **Component Extraction** | `AgentConversationGroup` | Reduce ConversationsList complexity, enable reuse |
| **Adapter** | Backend `summarizePrompt` | Rune-safe truncation in one place |

---

## File Changes

### 1. `packages/gui-agent/src/lib/agent-utils.ts` (NEW)

Canonical utility module for names, icons, subtitles, activity timestamps.

```typescript
import type { LucideIcon } from 'lucide-react'
import {
  Bot, Brain, Search, Zap, Hash, FileText, Eye, Cpu, Activity, Bug, Users,
} from 'lucide-react'
import type { Agent } from '@/api/types'

const ZERO_TIME_PREFIX = '0001-01-01T'

const roleIcons: Record<string, LucideIcon> = {
  researcher: Brain,
  coder: Cpu,
  reviewer: Eye,
  planner: Activity,
  semantic_scout: Search,
  dag_scout: Zap,
  symbol_scout: Hash,
  annotation_scout: FileText,
  overseer: Users,
  fixer: Bug,
}

const truncate = (value: string, maxLen: number): string => {
  const rs = [...value]
  if (maxLen <= 0 || rs.length <= maxLen) return value
  return `${rs.slice(0, maxLen).join('')}...`
}

export function getRoleIcon(role?: string): LucideIcon {
  return roleIcons[role || ''] || Bot
}

export function getAgentDisplayName(agent: Agent): string {
  return agent.role || agent.name || agent.slug || 'Agent'
}

export function getAgentSubtitle(agent: Agent): string {
  return agent.name || agent.slug || (agent.id ? agent.id.slice(0, 8) : 'agent')
}

export function getPromptSummaryOrSubtitle(agent: Agent, maxLen = 120): string {
  const summary = (agent.prompt_summary || '').trim()
  if (summary) return truncate(summary, maxLen)
  return truncate(getAgentSubtitle(agent), maxLen)
}

export function getAgentActivityTimestamp(agent: Agent): number {
  const candidates = [agent.heartbeat_at, agent.created_at]
  for (const ts of candidates) {
    if (!ts) continue
    if (ts.startsWith(ZERO_TIME_PREFIX)) continue
    const parsed = Date.parse(ts)
    if (Number.isFinite(parsed) && parsed > 0) return parsed
  }
  return 0
}
```

### 2. `packages/gui-agent/src/api/types.ts` (EDIT)

Add `prompt_summary?: string` to `Agent` interface.

### 3. `packages/gui-agent/src/components/conversations/ConversationsList.tsx` (EDIT — primary refactor)

Reference anchors:
- `groupedConversations` memo: ~line 222
- Search input: ~line 1170-1188
- Flat agent list render: ~lines 1256-1417

**3a. Add filtered agents + section memos:**

```typescript
const ONE_DAY_MS = 24 * 60 * 60 * 1000

const filteredAgents = useMemo(() => {
  if (!searchQuery) return agents
  const q = searchQuery.toLowerCase()
  return agents.filter((a) =>
    (a.name || '').toLowerCase().includes(q) ||
    (a.slug || '').toLowerCase().includes(q) ||
    (a.role || '').toLowerCase().includes(q) ||
    a.id.toLowerCase().includes(q)
  )
}, [agents, searchQuery])

const agentSections = useMemo(() => {
  const sections = {
    active: [] as Agent[],
    errored: [] as Agent[],
    recent: [] as Agent[],
    archive: [] as Agent[],
  }
  const cutoff = Date.now() - ONE_DAY_MS

  for (const agent of filteredAgents) {
    const state = (agent.state || '').toLowerCase()
    if (state === 'running' || state === 'idle') {
      sections.active.push(agent)
      continue
    }
    if (state === 'error') {
      sections.errored.push(agent)
      continue
    }
    const ts = getAgentActivityTimestamp(agent)
    if (ts > 0 && ts >= cutoff) {
      sections.recent.push(agent)
    } else {
      sections.archive.push(agent)
    }
  }
  return sections
}, [filteredAgents])
```

**3b. Extract `AgentConversationGroup` component** — full JSX with role icon, display name, prompt summary subtitle, expand/collapse, conversation list, rename/delete actions. (See detailed component in RepoPrompt chat `plan-gui-agent-list-rede-7A5AB3`.)

**3c. Replace flat `agents.map()` (lines 1256-1417) with sectioned CollapsibleSections:**

```tsx
{agentSections.active.length > 0 && (
  <CollapsibleSection title="Active" icon={<Play />} defaultOpen badge={String(agentSections.active.length)}>
    {agentSections.active.map(agent => <AgentConversationGroup key={agent.id} agent={agent} ... />)}
  </CollapsibleSection>
)}
{agentSections.errored.length > 0 && (
  <CollapsibleSection title="Errored" icon={<Bug />} defaultOpen badge={String(agentSections.errored.length)}>
    {agentSections.errored.map(agent => <AgentConversationGroup key={agent.id} agent={agent} ... />)}
  </CollapsibleSection>
)}
{agentSections.recent.length > 0 && (
  <CollapsibleSection title="Recent" icon={<Clock />} badge={String(agentSections.recent.length)}>
    {agentSections.recent.map(agent => <AgentConversationGroup key={agent.id} agent={agent} ... />)}
  </CollapsibleSection>
)}
{agentSections.archive.length > 0 && (
  <CollapsibleSection title="Archive" icon={<Folder />}>
    {agentSections.archive.map(agent => <AgentConversationGroup key={agent.id} agent={agent} ... />)}
  </CollapsibleSection>
)}
```

**3d. Batch actions (P2):**

```typescript
const handleKillAllStopped = async () => {
  const targets = filteredAgents.filter((a) => a.state === 'stopped')
  if (targets.length === 0) return
  if (!window.confirm(`Kill ${targets.length} stopped agents?`)) return
  const result = await Promise.allSettled(targets.map((a) => killAgent(a.id)))
  const failed = result.filter((r) => r.status === 'rejected').length
  alert(`Kill stopped: ${targets.length - failed} succeeded, ${failed} failed.`)
  await refetchAgents()
}

const handleTrashOldAgents = async () => {
  const cutoff = Date.now() - ONE_DAY_MS
  const targets = filteredAgents.filter((a) => {
    const ts = getAgentActivityTimestamp(a)
    return a.state === 'stopped' && ts > 0 && ts < cutoff
  })
  if (targets.length === 0) return
  if (!window.confirm(`Trash ${targets.length} old agents?`)) return
  const result = await Promise.allSettled(targets.map((a) => trashAgent(a.id)))
  const failed = result.filter((r) => r.status === 'rejected').length
  alert(`Trash old: ${targets.length - failed} succeeded, ${failed} failed.`)
  await refetchAgents()
}
```

**3e. Replace all remaining inline display-name/icon patterns** with shared utility calls.

### 4. `packages/gui-agent/src/components/agents/AgentList.tsx` (EDIT)

- Import utilities, replace `agent.name || agent.slug || agent.role || 'Agent'` with `getAgentDisplayName(agent)`
- Replace `Bot` icon with `getRoleIcon(agent.role)`
- Use `getPromptSummaryOrSubtitle(agent)` in card subtitle

### 5. `packages/gui-agent/src/components/layout/AgentSidebar.tsx` (EDIT)

- Same utility replacement for display name and role icon

### 6. `packages/gui-agent/src/components/agents/AgentDetailView.tsx` (EDIT)

- Same utility replacement for header, metadata, system prompt strings

### 7. `internal/web/api/agents.go` (EDIT — P2)

- Add `PromptSummary string` to `AgentResponse`
- Add `summarizePrompt` helper (rune-safe truncation at 100 chars)
- Populate in list, detail, and patch response paths

```go
func summarizePrompt(prompt string, maxLen int) string {
    prompt = strings.TrimSpace(prompt)
    if maxLen <= 0 || prompt == "" {
        return ""
    }
    runes := []rune(prompt)
    if len(runes) <= maxLen {
        return prompt
    }
    return string(runes[:maxLen]) + "..."
}
```

---

## Testing Strategy

- **Type check** after each step: `tsc --noEmit` from `packages/gui-agent`
- **Unit tests** for `agent-utils.ts`: role-first name mapping, full icon table, heartbeat fallback (zero-time/empty/invalid/valid), prompt summary truncation
- **Visual verification** for ConversationsList: sections render Active→Errored→Recent→Archive, correct open/collapsed defaults, selection/expansion preserved
- **Batch actions**: verify targets filter correctly (stopped only, old only), partial failures reported
- **Backend**: verify `prompt_summary` in list/detail responses, absent for empty prompts, rune-safe truncation

## Error Handling

- `getAgentActivityTimestamp` returns `0` on zero-time, missing, invalid — never throws
- `summarizePrompt` handles blank/whitespace input, uses rune-based slicing for UTF-8 safety
- Batch actions use `Promise.allSettled` with failure count reporting
- Unknown `state` values fall into Recent/Archive path by default

## Migration Notes

- Backend `prompt_summary` is additive and non-breaking
- No storage schema migration required
- Display names become role-first immediately — intentional UX change
- Frontend handles missing `prompt_summary` gracefully (subtitle fallback)

## Implementation Order

1. Add `packages/gui-agent/src/lib/agent-utils.ts` → `tsc --noEmit`
2. Add `prompt_summary?: string` to `packages/gui-agent/src/api/types.ts` → `tsc --noEmit`
3. ConversationsList: add `filteredAgents` + `agentSections` memos (no render changes) → `tsc --noEmit`
4. ConversationsList: extract `AgentConversationGroup` component → `tsc --noEmit`
5. ConversationsList: replace flat map with sectioned CollapsibleSections → visual verify
6. ConversationsList: add batch action handlers + UI buttons → visual verify
7. ConversationsList: replace remaining name/icon/subtitle patterns → `tsc --noEmit`
8. Update AgentList.tsx, AgentSidebar.tsx, AgentDetailView.tsx with utilities → `tsc --noEmit`
9. Backend P2: `summarizePrompt` + response field in agents.go → `make build`
10. Full frontend type check + UI consistency sweep

## Risks

- **CollapsibleSection nesting**: Status sections wrapping agents wrapping conversations = 3 levels. Mitigated by defaulting Archive collapsed.
- **Batch action flood**: Many agents = many parallel API calls. `Promise.allSettled` handles gracefully.
- **`heartbeat_at` zero value**: Handled by `getAgentActivityTimestamp` fallback chain.

## Open Questions

None.
