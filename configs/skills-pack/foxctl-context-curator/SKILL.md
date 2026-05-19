---
name: foxctl-context-curator
description: "Run periodic maintenance on the foxctl context plane: memory records, observations, tensions, handoffs, and vault drafts."
user-invocable: true
---

# Foxctl Context Curator

Use this skill when the context plane needs maintenance — stale records piling
up, unresolved tensions, orphaned vault drafts, or general context hygiene.

This skill teaches agents to run the context curator proactively, interpret its
proposals, and act on them.

## When to use me

- The workspace has been running for a while and context may be stale
- You want to clean up before starting a new milestone or epic
- Observations are piling up and some may be outdated
- Tensions have been open for too long
- Vault inbox has drafts that need triage (promote or archive)
- Memory records may have duplicates or low-utility entries
- You're handing off to another agent and want a clean context plane

## Mental model

The context plane has five stores that need maintenance:

| Store | What accumulates | What goes stale |
|---|---|---|
| **Memory** | Knowledge records, patterns, decisions | Unused records, duplicates, superseded entries |
| **Observations** | Facts learned about the codebase | Low-confidence or single-seen entries |
| **Tensions** | Architectural concerns, performance issues | Open tensions that have been addressed |
| **Handoffs** | Session continuity notes | Old handoff files no longer relevant |
| **Vault** | Promoted evergreen notes + inbox drafts | Drafts that never got promoted |

The curator runs deterministically — no LLM calls. It applies age, confidence,
count, and utility heuristics to produce proposals. You then decide what to act on.

## Workflow

### Step 1: Run the curator report

```
foxctl_context_curator(mode="dry_run", stale_after_days=30)
```

This returns a unified report with:
- `summary`: counts per store
- `proposals`: list of actions to consider

### Step 2: Review proposals

Each proposal has:
- `source`: which store (memory, observation, tension, handoff, vault_draft)
- `record_id`: the item to act on
- `action`: suggested action (review, archive, address_or_close, promote_or_archive, consolidate)
- `reasons`: why this item was flagged

### Step 3: Act on proposals

Use existing tools to act:

| Proposal source | Action | Tool to use |
|---|---|---|
| Memory | archive/demote | Run `memory/curator_report` in `apply` mode |
| Memory | consolidate | Review the cluster, keep the best record |
| Observation | review | `foxctl_context_observe` to update or supersede |
| Tension | address_or_close | `foxctl_context_tension` to resolve |
| Handoff | archive | Delete old handoff files if no longer needed |
| Vault draft | promote_or_archive | `foxctl_vault_promote` to promote, or delete stale drafts |

### Step 4: Record what you did

After acting on proposals, record a curator observation:

```
foxctl_context_curator(mode="dry_run")
```

Run again to verify the proposals are resolved.

## Curator heuristics

The curator applies these rules:

**Memory** (delegates to `memory/curator_report`):
- Active records with 0 uses after `stale_after_days` → demote to stale
- Stale records after `archive_after_days` (default 90) → archive
- Low success rate (< 50%) after 3+ uses → demote to stale
- Similar summaries (Jaccard ≥ 0.90) → duplicate consolidation cluster
- Overlapping summaries (Jaccard ≥ 0.55) with 3+ shared signals → overlap cluster
- Records with `superseded_by` set → supersession proposal

**Observations**:
- Confidence < 0.5 → review
- Seen only once and older than `stale_after_days` → review

**Tensions**:
- Status `open` and older than `stale_after_days / 2` → address or close
- Recurring (count > 3) → flag for attention

**Handoffs**:
- File older than `stale_after_days` → archive

**Vault drafts**:
- Inbox item older than `stale_after_days` → promote or archive

## Proactive patterns

### Periodic maintenance (every session or every N sessions)
```
1. Run foxctl_context_curator(mode="dry_run")
2. If proposals > 0, review and act
3. Record observation: "Context curator run: N proposals, M resolved"
```

### Pre-milestone cleanup
```
1. Run curator with stale_after_days=7 (aggressive)
2. Resolve all open tensions (close addressed ones)
3. Promote any vault drafts worth keeping
4. Archive stale handoffs
5. Run curator again to verify clean state
```

### Handoff preparation
```
1. Run curator to check for stale context
2. Clean up before capturing handoff via foxctl_context_capture
3. Include curator summary in the handoff note
```

## Rules

- Always start with `dry_run` — never apply without reviewing proposals first
- Don't resolve tensions you haven't verified are actually addressed
- Don't archive memory records that might be needed later — demote to stale instead
- Promote vault drafts that have lasting value, archive the rest
- Record what you did as an observation so the next curator run knows
- If the report has 0 proposals, the context plane is healthy — record that too
