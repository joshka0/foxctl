---
title: Cutover Test Plan (Supervisor + Hooks v1 + Lineage + Progressive Memory)
status: draft
last_updated: 2026-01-08
---

## 0. Test goals

Prove these invariants under real failure modes:

1. **Mailbox delivery semantics** preserved (lease/ack/nack, at-least-once)
2. **Supervisor enforces sequential per-actor processing**
3. **Hooks v1 deterministic merging** and correct block behavior
4. **Session lineage propagation** is end-to-end and queryable
5. **Progressive memory crash safety** (cursor-based, redacted summaries)
6. **Safe rollback**: disabling flags restores old behavior without data loss

---

## 1. Unit tests

### 1.1 Hook dispatcher merge semantics
- Given hook outputs A,B,C:
  - any `block` wins
  - UpdatedToolInput last-wins
  - UpdatedAssistantText last-wins
  - Actions appended in order
- Ensure stable ordering even when hooks return NONE.

**Tests**
- `TestMerge_BlockWins`
- `TestMerge_LastWins_ToolInput`
- `TestMerge_LastWins_AssistantText`
- `TestMerge_ActionsAppended`

### 1.2 Hook adapters
- Shell-style JSON `{decision:"approve", context:"x"}` becomes hook.Output.
- Malformed JSON → fail-open NONE/APPROVE with reason, never crash dispatcher.

### 1.3 Token estimator + budget trigger
- Verify `len/4` estimator and 80% headroom logic
- Verify `ContextBudgetExceeded` event triggered at threshold

---

## 2. Storage & migration tests

### 2.1 Schema migration idempotency
For each DB:
- Open → migrations run → close
- Open again → migrations run (no changes) → close
- Ensure no errors and tables exist

### 2.2 Column-add migrations
- Start from “old schema” fixture DB
- Run migrations
- Assert columns exist via PRAGMA table_info

---

## 3. Mailbox semantics tests (integration)

### 3.1 Lease expiry → redelivery
1. Insert message with visible_at = now
2. Poll with lease=1s → claim message
3. Do NOT ack
4. Sleep >1s
5. Poll again → message should be visible/claimable again

### 3.2 Nack backoff → delayed visibility
1. Poll message with lease=30s
2. Nack with backoff=5s
3. Poll immediately → should not return message
4. Sleep >5s → poll returns message

### 3.3 Ack removes message
- Poll → ack → poll returns nothing

---

## 4. MailboxWatcher tests

### 4.1 Wakeup creation
- Send message → assert mailbox_notify row exists
- Watcher emits WakeUp{to_ns} within time bound

### 4.2 Wakeup does not claim
- Ensure watcher never mutates mailbox rows; only supervisor Poll claims

---

## 5. Supervisor tests (integration)

### 5.1 One in-flight per actor
- Enqueue N messages to same actor namespace
- Ensure actor processes sequentially:
  - message2 is not claimed until message1 ack/nack completes

### 5.2 Multi-actor independence
- Enqueue messages to actorA and actorB
- Both can proceed concurrently (separate actors), but each actor sequential

### 5.3 Crash safety
- Simulate actor crash (process kill or forced panic)
- Ensure lease expires and message is re-processed

---

## 6. Session lineage tests

### 6.1 Create/resume/fork edges
- Create session S1 (agent_id=foxctl)
- Resume → creates S2 + edge continues(S2→S1)
- Fork → creates S3 + edge forked_from(S3→S2)
- Query chain depth and ensure deterministic ordering

### 6.2 One active per (workspace, agent_id)
- Attempt to start a second running session for same tuple → should fail unless forced

### 6.3 Env propagation to skills/hooks
- Spawn a skill execution and assert env includes:
  - AGENTCTL_SESSION_ID
  - AGENTCTL_WORKSPACE
  - AGENTCTL_AGENT_ID

---

## 7. Progressive memory tests

### 7.1 Cursor monotonicity
- Append turns 0..N
- Run summarize batch
- Ensure cursor advances only after summary write

### 7.2 Crash during summarization is safe
- Append enough turns to trigger summarization
- Force crash between “summary generated” and “cursor updated”
- Restart: summarization retries same batch deterministically; no duplicate index conflicts

### 7.3 Redaction is applied
- Include a fake secret in turns
- Ensure L1/L2 artifacts contain [REDACTED], not secret

---

## 8. End-to-end “cutover rehearsal” scenario

A black-box harness that:
1) Starts supervisor with flags ON
2) Creates an actor for a namespace
3) Sends an ask message
4) Runs hooks that:
   - inject context
   - block a tool call once
5) Confirms:
   - message acked
   - turn persisted
   - injected context surfaced next turn

Success criteria:
- deterministic final reply produced
- no dead letters
- no missed wakeups
- no data loss in sessions db

---

## 9. Rollback checklist

If any of these fail in staging:
- lease expiry behavior changes
- duplicate rate increases unexpectedly (beyond at-least-once norms)
- hooks block unexpectedly due to config errors
- supervisor stalls (no progress with pending mailbox)

Rollback steps:
1. Set flags to 0:
   - AGENTCTL_ACTOR_SUPERVISOR=0
   - AGENTCTL_HOOKS_V1=0
   - AGENTCTL_MAILBOX_WATCHER=0
2. Restart to old daemon mode
3. Confirm mailbox processing continues
4. Leave migrations in place (additive); no schema rollback required

````

---