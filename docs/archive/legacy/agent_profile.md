# agentctl Agent Profile v1

> **Purpose:** Extend the Core Profile with **multi‑agent orchestration**
> (spawn/kill/restart), **mailbox** ask/reply/cmd/event, **blackboard** for
> coordination, **namespaces & quotas**, and an optional **OCI** runner.
> **Compatibility:** Superset of Core. All Core wire contracts (JSON/NDJSON
> envelope, `data.artifact` with optional `meta.cas_digest` matching it,
> mandatory `summary` for large outputs) remain unchanged. **Audience:**
> Developers building multi‑agent systems, supervisors, and orchestration layers
> that require delegation, isolation, and coordination.

---

## 0. Scope & Non‑Goals

- **In scope (Agent Profile):**

  - Agent tree (parent/child) with **policy narrowing** and **skill
    allowlists**.
  - **Mailbox** (ask/reply/cmd/event) with TTL, leasing, and correlation.
  - **Blackboard** (topics, TTL, leases, search, scoped sharing).
  - **Namespace quotas** (CPU/mem/concurrency/rate limits) + **WFQ** scheduler.
  - **OCI** runner with hardening (rootless, no‑new‑privileges, read‑only root).
  - **Attestations & audit** across agents/namespaces.

- **Not in scope (here):** GUI/dashboard, on‑device LLM hosting, cloud cluster
  management, and trust policy PKI details. (You can build on top using the same
  envelopes.)

---

## 1. Normative Basis

All **Core** rules apply (envelope, streaming, artifacts, summaries, caching,
error codes). This profile **adds** new commands, tables, and behaviors; it
**MUST NOT** alter Core wire contracts.

---

## 2. Concepts & Model

### 2.1 Agent

A **logical actor** that:

- Lives in a **namespace** `ns` (hierarchical: `org/proj/main`,
  `org/proj/main/child-01H…`).
- Has a **role** and **prompt** (observability only; used by outer LLM).
- Has a **skill allowlist** (subset of installed skills).
- Has a **policy** (caps, secrets, network egress rules, timeouts, output
  limits).
- Owns a **mailbox** and optional **blackboard** subscriptions.

**Agent states:** `starting | running | stopped | error`. Heartbeats track
liveness.

### 2.2 Namespace & quotas

A **namespace** groups agents, jobs, artifacts, policies, quotas. Quotas bound:

- **max_concurrent_jobs**, **CPU/mem budgets**, **LLM call rate**, **egress
  bytes/sec** (optional).
- Children **inherit upper bounds**; narrowing **MUST** not exceed parent.

### 2.3 Mailbox (actor channel)

Per‑agent queue for point‑to‑point messages:

- **Types:** `agent.ask`, `agent.reply`, `agent.cmd`, `agent.event`.
- **Semantics:** at‑least‑once delivery with **visibility leases**; explicit
  ack/delete.
- **Correlation:** `headers.correlation = ask_id` links ask↔reply.

### 2.4 Blackboard (topic bus)

Append‑only coordination bus:

- Records with `topic`, `payload`, `ttl`, optional `cas_ref`.
- Operations: `post`, `watch` (NDJSON stream), `search`, **claim** via lease
  (task queues).
- **Scoped sharing:** parent↔child mirroring via **all | scoped | none**.

### 2.5 Runners

Adds **OCI** to Core runners:

- **WASI** (preferred), **exec**, **OCI** (for heavy deps like browsers/ML).
- Runner selection remains **least‑privilege** that satisfies capabilities.

---

## 3. Envelope & Message Shapes

> **Canonical envelope:** same as Core (§2.1). Streaming NDJSON (§2.2)
> unchanged.

### 3.1 Mailbox message (stored)

```json
{
  "id": "01HMB7…",                    // ULID
  "from_ns": "org/app/main/child-a",
  "to_ns": "org/app/main",
  "type": "agent.ask",                // "agent.ask"|"agent.reply"|"agent.cmd"|"agent.event"
  "ttl_ms": 300000,
  "headers": { "correlation": "ask_91ad", "kind": "context" },
  "payload": {                        // TOON JSON envelope (opaque to mailbox)
    "version": 1, "status": "ok", "command": "agent.ask", "data": {...}, "meta": {...}, "error": {...}
  },
  "visible_at": 1730973600,           // epoch secs; lease visibility timeout
  "attempt": 0,
  "ts": 1730973610
}
```

### Delivery semantics

- **At‑least‑once**: messages may be delivered more than once; consumers
  **MUST** de‑dupe by `id` (daemons typically persist a dedupe set).
- **Visibility lease**: dispatchers poll messages where `visible_at <= now`,
  then update `visible_at` (default 30s) and increment `attempt` as a lease.
  `ack` deletes; `nack` requeues after a visibility timeout (often exponential
  backoff based on `attempt`).
- **TTL**: if `ttl_ms > 0` and `ts*1000 + ttl_ms < now_ms`, the message is
  expired and should be acked without processing.

### 3.2 Ask/Reply payloads (inside `payload`)

```json
// agent.ask
{
  "version": 1, "status": "ok", "command": "agent.ask",
  "data": {
    "ask_id": "ask_91ad",
    "kind": "context|secret|approval|toolhint|other",
    "question": "Which package should I target for unit tests?",
    "needs_by_ms": 180000,
    "context": { "task_id": "task-42", "repo_digest": "sha256:..." }
  },
  "meta": { "ts": "...", "duration_ms": 0, "source": "run" },
  "error": { "code": null, "message": null }
}

// agent.reply
{
  "version": 1, "status": "ok", "command": "agent.reply",
  "data": {
    "ask_id": "ask_91ad",
    "answer": { "test_target": "./services/api/... -run=^TestUsers$" }
  },
  "meta": { "ts": "...", "duration_ms": 0, "source": "run" },
  "error": { "code": null, "message": null }
}
```

### 3.3 Commands & events

```json
// agent.cmd (parent -> child)
{
  "version": 1, "status": "ok", "command": "agent.cmd",
  "data": { "cmd_id":"cmd_12aa", "action":"run_skill", "skill":"test/run", "args":{ "pkg":"./...", "run":"^TestUsers$" } },
  "meta": { "ts":"..." }, "error": { "code": null, "message": null }
}

// agent.event (child heartbeats/metrics)
{
  "version": 1, "status":"ok", "command":"agent.event",
  "data": { "event_id":"evt_77c2", "kind":"heartbeat", "job_count":2, "cache_hits":3 },
  "meta": { "ts":"..." }, "error": { "code": null, "message": null }
}
```

---

## 4. Agent Lifecycle & Policy Narrowing

### 4.1 Agent record (DB)

```json
{
  "id": "01HAGNT…",                         // ULID
  "parent_id": "01HAGNT_PARENT",            // ULID or null
  "ns": "org/proj/main/child-01H7E…",
  "role": "codex",
  "prompt": "You are a code-focused agent ...",
  "skills_allow": ["repo/read","repo/write","test/run","mem/search","mem/put"],
  "policy": {
    "cpu": 2, "memMB": 2048, "timeout": "20m",
    "network": "none" | "egress",
    "egressAllow": ["api.github.com:443"],        // Agent-level narrowing
    "max_output_kb": 1024,
    "envAllow": [], "secrets": ["db_password"],
    "filesystem": [{ "type":"workdir" }, { "type":"ro", "from":"/opt/dict", "to":"/dict" }]
  },
  "share_bb": "all|scoped|none",
  "state": "starting|running|stopped|error",
  "created_at": 1730973600,
  "heartbeat_at": 1730973630
}
```

### 4.2 Spawn

#### CLI

```bash
agentctl agent spawn \
  --ns=org/app/main \
  --role=codex \
  --prompt-file=prompt.md \
  --skills-allow=repo/read,repo/write,test/run,mem/search,mem/put \
  --policy=@policy.json \
  --share-bb=scoped
```

#### Response (envelope)

```json
{
  "version": 1,
  "status": "ok",
  "command": "agent/spawn",
  "data": {
    "agent_id": "01HAGNT…",
    "ns": "org/app/main/child-01H…",
    "role": "codex"
  },
  "meta": { "ts": "...", "duration_ms": 5, "source": "run" },
  "error": { "code": null, "message": null }
}
```

### 4.3 Policy narrowing (normative)

When spawning a child:

- **CPU/MEM/TIMEOUT/OUTPUT:** child values **MUST NOT** exceed parent’s.
- **Network:** child `network` **MUST** be ≤ parent (i.e., `"none"` ≤
  `"egress"`). Child `egressAllow` **MUST** be a **subset** of parent
  `egressAllow` if parent had `"egress"`.
- **Secrets:** child `secrets` **MUST** be a **subset** of parent `secrets`.
- **Env:** child `envAllow` **MUST** be a **subset** of parent `envAllow`.
- **Filesystem:** child mounts **MUST** be a subset or stricter than parent’s
  (e.g., read‑only where parent allowed).
- **Skills:** child `skills_allow` **MUST** be a subset of installed skills and
  (if provided) parent’s allowlist.

If any rule fails → **EPOLICY** with a diff listing violating fields.

### 4.4 Supervision

- Parent **MAY** set **restart policy**: `never | on-failure | always` (+
  backoff).
- On child unresponsive past `heartbeat_timeout` → **agent.event**
  `kind:"liveness-failed"` and optional auto‑restart according to policy.
- Parent **MAY** kill or restart child; child **MUST** be terminated gracefully
  before force kill.

---

## 5. Blackboard

### 5.1 Record

```json
{
  "id": "01HBB…",
  "ns": "org/proj/main",
  "topic": "/tasks.todo",
  "ts": 1730973600,
  "ttl_sec": 86400,
  "payload": {
    "title": "Scan pricing pages",
    "seed_url": "https://example.com/pricing",
    "priority": 8,
    "task_id": "task-1234"
  },
  "cas_ref": null,
  "lease": null // or { holder:"01HAGNT…", "until": 1730973800 }
}
```

### 5.2 Operations (CLI)

```bash
agentctl bb post <topic> --ns=<ns> --data=@payload.json [--ttl=24h] [--cas=sha256:...]
agentctl bb watch <topic> --ns=<ns> [--filter='<expr>']                # NDJSON envelopes
agentctl bb search <topic> --ns=<ns> --query="<fts query>"
agentctl bb claim  <id> --ns=<ns> --lease=60s
agentctl bb release <id> --ns=<ns>
```

### 5.3 Scoped sharing

- `share_bb=all`: child sees parent topics and siblings; child posts visible to
  parent.
- `share_bb=scoped`: child sees a mirrored subset; runtime **SHOULD** prefix
  child topics `/child/<id>` and mirror selected topics up/down.
- `share_bb=none`: no mirroring.

Mirroring rules are configured at spawn or via policy.

---

## 6. Scheduling, Quotas & Circuit Breakers

### 6.1 Queues & WFQ

- Each namespace has its **own job queue**.
- Global scheduler applies **Weighted Fair Queuing** using per‑ns weights
  derived from quotas.
- **No starvation:** scheduler **MUST** prevent indefinite delay for queued jobs
  in any ns with available quota.

### 6.2 Quotas (per namespace)

- **max_concurrent_jobs** (integer).
- **CPU/mem** budgets (cgroups for exec/OCI).
- **Rate limits** (e.g., LLM calls/min, egress bytes/sec) — **SHOULD** be
  enforced if configured.
- Child quotas **MUST NOT** exceed parent’s (policy narrowing).

### 6.3 Circuit breakers & DLQ

- Per `(namespace, skill)`: if `> N` failures in window `T`, breaker **opens**
  (new jobs → `ESKILLDOWN`).
- Jobs exceeding retry thresholds go to a **dead‑letter queue**:

  ```bash
  agentctl jobs dlq list --ns=<ns>
  agentctl jobs dlq requeue <job_id>
  agentctl jobs dlq delete <job_id>
  ```

---

## 7. OCI Runner (optional, Agent Profile)

### 7.1 Selection

Runner chooses **OCI** when:

- Skill `distribution.type=oci`, **or**
- heavy dependencies require it and policy allows.

### 7.2 Hardening (normative)

OCI runs **MUST** enforce:

- **Rootless** user; `no-new-privileges`.
- **cap_drop=ALL**; seccomp baseline; minimal `pids` limit.
- **Read‑only** root FS; dedicated writable `/work`.
- **Network** according to policy (`none` or `egress` with allowlist).
- **Env** stripped except `envAllow`.
- **Secrets** only via `/run/secrets/<name>` mounts.
- **Timeout/CPU/mem** via cgroups.

### 7.3 Image provenance

- **SHOULD** verify image digest (`@sha256:`).
- **SHOULD** validate signatures per trust policy (e.g., cosign/rekor).
- **SHOULD** cache pulls; enforce max image size; prune by GC.

---

## 8. CLI (Agent Profile additions)

```bash
# Agents
agentctl agent spawn  --ns=<parent_ns> --role=<role> --prompt-file=<path> \
                      --skills-allow="a,b,c" --policy=<policy.json> --share-bb=all|scoped|none
agentctl agent list   [--limit=<n>]
agentctl agent info   <agent-id>
agentctl agent run    <agent-id>
agentctl agent ask    <agent-id> --question "..." [--kind=context] [--wait --timeout=5m]
agentctl agent cmd    <agent-id> --action <run_turn|do_work|run_skill> [--skill <name>] --args '{"k":"v"}'
agentctl agent watch  <agent-id>                      # NDJSON progress stream (state, heartbeat, mailbox samples)
agentctl agent kill   <agent-id> [--graceful --timeout=30]

# Blackboard
agentctl bb post|watch|search|claim|release (see §5.2)

# Policy & quotas
agentctl policy show  [--ns=<ns>]
agentctl policy edit  [--ns=<ns>] < policy.json
agentctl quotas show  [--ns=<ns>]
agentctl quotas edit  [--ns=<ns>] < quotas.json

# Audit
agentctl audit export --ns=<ns> [--since=<ts>]       # NDJSON attestations
```

### 8.1 Running an Agent Daemon

An agent can run as a long-lived daemon that polls for mailbox messages:

```bash
agentctl agent run <agent-id>
```

The daemon:

- Polls `mailbox.Store` for `agent.ask`, `agent.cmd`, and `agent.event` messages
  addressed to the agent's namespace.
- Executes DSPy turns with the configured tool registry (filtered by
  `skills_allow`).
- Sends `agent.reply` messages back to the caller namespace with
  `headers.correlation=<ask_id>`.
- Updates agent state and heartbeats while running.

### 8.2 Sending Asks and Waiting for Replies

```bash
# Fire-and-forget ask
agentctl agent ask <agent-id> --question "What files need refactoring?"

# Wait for reply (blocks until response or timeout)
agentctl agent ask <agent-id> --question "..." --wait --timeout 5m
```

With `--wait`, the CLI waits for a correlated `agent.reply` mailbox message and
acks it after printing the reply envelope.

All outputs are **TOON JSON envelopes**; streams are **NDJSON**.

---

## 9. Persistence (DDL additions)

> Aligned with Core naming (`size_bytes`, `result_path`, etc.).

```sql
-- agents
CREATE TABLE agents (
  id           TEXT PRIMARY KEY,       -- ULID
  parent_id    TEXT,
  ns           TEXT UNIQUE NOT NULL,
  role         TEXT,
  prompt       TEXT,
  skills_allow TEXT NOT NULL,          -- JSON array
  policy       TEXT NOT NULL,          -- JSON
  share_bb     TEXT NOT NULL CHECK (share_bb IN ('all','scoped','none')),
  state        TEXT NOT NULL CHECK (state IN ('starting','running','stopped','error')),
  created_at   INTEGER NOT NULL,
  heartbeat_at INTEGER
);
CREATE INDEX idx_agents_ns ON agents(ns);
CREATE INDEX idx_agents_parent ON agents(parent_id);

-- mailbox
CREATE TABLE mailbox (
  id           TEXT PRIMARY KEY,       -- ULID
  from_ns      TEXT NOT NULL,
  to_ns        TEXT NOT NULL,
  type         TEXT NOT NULL,
  ttl_ms       INTEGER NOT NULL,
  headers      TEXT,                   -- JSON
  payload      TEXT NOT NULL,          -- Envelope JSON
  visible_at   INTEGER NOT NULL,
  attempt      INTEGER NOT NULL DEFAULT 0,
  ts           INTEGER NOT NULL
);
CREATE INDEX idx_mailbox_to_visible ON mailbox(to_ns, visible_at);

-- blackboard
CREATE TABLE blackboard (
  id        TEXT PRIMARY KEY,          -- ULID
  ns        TEXT NOT NULL,
  topic     TEXT NOT NULL,
  ts        INTEGER NOT NULL,
  ttl_sec   INTEGER NOT NULL,
  payload   TEXT NOT NULL,             -- JSON
  cas_ref   TEXT,
  lease     TEXT                       -- JSON {holder, until}
);
CREATE INDEX idx_bb_ns_topic ON blackboard(ns, topic);
CREATE VIRTUAL TABLE blackboard_fts USING fts5(payload, content='blackboard', content_rowid='rowid');

-- quotas (per namespace)
CREATE TABLE ns_quotas (
  ns                   TEXT PRIMARY KEY,
  max_concurrent_jobs  INTEGER,
  cpu_limit            INTEGER,
  memMB_limit          INTEGER,
  llm_calls_per_min    INTEGER,
  egress_bytes_per_min INTEGER
);

-- attestations (augment Core)
CREATE TABLE run_attestations (
  id          TEXT PRIMARY KEY,        -- ULID
  job_id      TEXT NOT NULL,
  ns          TEXT NOT NULL,
  skill       TEXT NOT NULL,
  skill_ver   TEXT,
  skill_digest TEXT,
  policy_hash TEXT,
  runner      TEXT NOT NULL,           -- wasi|exec|oci
  resources   TEXT NOT NULL,           -- JSON {cpu_ms, wall_ms, mem_peak_mb, bytes_out}
  created_at  INTEGER NOT NULL
);
```

---

## 10. Observability & Audit

- **Attestations** after each job (as above); `agentctl audit export` emits
  NDJSON envelopes.
- **Agent events**: heartbeats, liveness failures, restarts — stream via
  `agentctl agent watch`.
- **Metrics (optional):** per‑ns queue depth, breaker state, delivery attempts,
  OCI pulls, cache hit rates.

---

## 11. Security & Trust (Agent)

- **Secrets** are never forwarded across agents automatically; parent **MAY**
  grant labels to a child via policy narrowing.
- **Network egress**: same rules as Core; child allowlist **MUST** be subset of
  parent’s.
- **OCI** images **SHOULD** be validated; prefer digest‑pinned references and
  signature verification.

---

## 12. Error Mapping (Agent addenda)

| Condition                      | `error.code` |
| ------------------------------ | ------------ |
| Spawn fails policy narrowing   | `EPOLICY`    |
| Mailbox TTL expired            | `ENOTFOUND`  |
| Lease conflict / not holder    | `EPOLICY`    |
| Blackboard claim invalid       | `EPOLICY`    |
| Breaker open for (ns, skill)   | `ESKILLDOWN` |
| OCI hardening preflight failed | `EPOLICY`    |

_(Retain Core codes `EARG`, `ERUNTIME`, `EOUTPUT`, `ETIMEOUT`, `EIO` etc.)_

---

## 13. Defaults (Agent)

- `heartbeat_interval`: 5s, `heartbeat_timeout`: 20s.
- Mailbox `visibility_timeout`: 30s; `max_delivery_attempts`: 10.
- Blackboard `ttl_sec`: default 24h if unspecified.
- Circuit breaker: `N=5` fails in `T=1m` opens breaker for `cooldown=5m`.
- OCI pulls cached; max image size 3 GiB; prune weekly.

---

## 14. Examples

### 14.1 Parent spawns two children, routes asks, and supervises

```bash
# Spawn codex and validator (scoped blackboard)
A1=$(agentctl agent spawn --ns=org/app/main --role=codex --prompt-file=codex.md \
      --skills-allow=repo/read,repo/write,test/run,mem/search --policy=@codex-policy.json --share-bb=scoped | jq -r '.data.agent_id')

A2=$(agentctl agent spawn --ns=org/app/main --role=validator --prompt-file=validator.md \
      --skills-allow=test/run,analysis/coverage --policy=@validator-policy.json --share-bb=scoped | jq -r '.data.agent_id')

# Watch both mailboxes (separate terminals)
agentctl agent watch "$A1"
agentctl agent watch "$A2"

# Parent routes an ask to validator
agentctl agent send "$A2" --type=agent.cmd --payload=@run-tests.json
```

### 14.2 Blackboard task queue with leases

```bash
# Parent posts tasks
agentctl bb post /tasks.todo --ns=org/app/main --data='{"task_id":"t1","url":"https://example/pricing"}'
agentctl bb post /tasks.todo --ns=org/app/main --data='{"task_id":"t2","url":"https://example/docs"}'

# Child claims, works, and releases
rec=$(agentctl bb search /tasks.todo --ns=org/app/main --query="pricing" | jq -r '.data.results[0].id')
agentctl bb claim "$rec" --ns=org/app/main --lease=120s
# ... do work ...
agentctl bb release "$rec" --ns=org/app/main
```

### 14.3 Policy narrowing violation (egress)

```bash
# Parent allows only api.github.com
cat > child-policy.json <<'JSON'
{ "network":"egress", "egressAllow":["*.amazonaws.com:443"] }
JSON
agentctl agent spawn --ns=org/app/main --role=crawler --policy=@child-policy.json
# -> status:error, EPOLICY (child egressAllow not subset of parent)
```

### 14.4 OCI skill run (hardened)

```bash
agentctl run browser/nav --url=https://example.com --timeout=30s
# Runner selects OCI; rootless, cap_drop=ALL, read-only root; egress constrained by allowlist.
```

---

## 15. Versioning & Forward Compatibility

- This profile is **v1** and extends Core v1.
- Backward‑compatible additions MAY add fields to agent/mailbox/blackboard
  records.
- Breaking changes **MUST** bump the **Agent Profile** version and, if needed,
  the TOON `version` for new envelopes.

---

## 16. Implementation Notes (overview)

- Agents are **logical actors** in‑process; they do not require separate OS
  processes.
- Mailbox delivery uses DB row leasing (update `visible_at` atomically).
- Blackboard watchers tail an index by `ts` and block on a notification channel
  (DB or file watcher).
- Scheduler keeps a per‑ns heap keyed by **virtual finish time** (WFQ).
- OCI runner can shell out to a rootless runtime (e.g., `nerdctl`, `podman`) or
  embed a library; hardening checks run before launch.

---

**End — Agent Profile v1**

---
