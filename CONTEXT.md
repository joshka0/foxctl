# Foxctl Code Intelligence Context

Foxctl code intelligence turns repository facts into queryable context for
agents. The key boundary is between canonical repo graph data and disposable
projections built from it.

## Language

**Canonical repo graph**:
The repoindex-owned graph of repository nodes, edges, metadata, and file state.
_Avoid_: Ladybug graph, projection store

**Repoindex export projection**:
A deterministic file export derived from the canonical repo graph for external traversal proofs.
_Avoid_: Ladybug integration, graph engine

**Repoindex-local exporter**:
A Phase 1 exporter implemented in the repoindex package alongside canonical graph types and store reads.
_Avoid_: new top-level internal package, v2 runtime package

**Repoindex export snapshot**:
A read-only, ordered view of the canonical repo graph used as the input to export projections.
_Avoid_: graph expansion, query result

**Export-only repo row**:
A projection row derived from repo metadata to give external graph schemas a root without adding a canonical repoindex node kind.
_Avoid_: repoindex repo node, canonical repo kind

**Typed CSV export**:
A repoindex export projection split by node kind and edge type instead of generic node and edge tables.
_Avoid_: generic graph dump, ad hoc export

**Relationship metadata column**:
The compact JSON relationship property used to preserve edge-specific metadata until a proof query justifies typed columns.
_Avoid_: early schema explosion, lost edge metadata

**Node metadata column**:
The compact JSON node property used to preserve kind-specific details until a proof query justifies typed columns.
_Avoid_: per-kind schema sprawl, lost node metadata

**Canonical export JSON**:
Compact JSON re-serialized by the exporter so golden files are stable and malformed metadata fails fast.
_Avoid_: raw JSON passthrough, whitespace-dependent metadata

**Injected export time**:
The timestamp supplied to export building by the caller rather than read from the system clock inside core export logic.
_Avoid_: time.Now in exporter core, nondeterministic golden files

**Export artifact**:
The in-memory deterministic representation of CSV files and metadata before filesystem writing.
_Avoid_: filesystem-only export result, path-coupled builder

**Deterministic projection metadata**:
Portable export metadata that avoids local absolute storage paths and machine-specific debug fields.
_Avoid_: source store path in meta.json, debug-only metadata

**Repo root identity**:
A portable repository root identifier used in export artifacts instead of an absolute workspace path.
_Avoid_: repo_root absolute path, local checkout path

**Replace-all export write**:
A projection write that replaces the whole target export directory so omitted files cannot leave stale rows behind.
_Avoid_: selective overwrite, stale CSV files

**Passive edge export**:
An export rule that writes existing canonical edges without triggering new analysis or inference.
_Avoid_: build-time enrichment, export-side inference

**Synthetic export fixture**:
A hand-built repoindex export snapshot used to golden-test export behavior without parsing a real repository.
_Avoid_: temporary repo build, parser-dependent exporter test

**Writer-boundary golden**:
A golden test that compares the actual files produced by the export writer, including stale-file removal.
_Avoid_: artifact-only golden, untested filesystem semantics

**Context enrichment projection**:
A future export layer for contextplane evidence, memory claims, and agent notes that references canonical repo graph IDs.
_Avoid_: Phase 1 export, second code identity graph

**Ladybug integration**:
An opt-in path that imports or queries a Ladybug database through the `lbug` CLI.
_Avoid_: repoindex exporter, default graph path

**Ladybug schema artifact**:
A schema, import, rebuild, or query file that assumes the `lbug` CLI path is available and verified.
_Avoid_: Phase 1 placeholder, guessed integration script

**Envelope-shaped command**:
A user-facing foxctl command or skill that writes protocol envelopes on stdout and logs elsewhere.
_Avoid_: raw script output, mixed stdout logs

**Graph lens**:
A disposable external view over canonical repo graph data used to test traversal clarity.
_Avoid_: source of truth, replacement index

## Relationships

- A **Canonical repo graph** produces zero or more **Repoindex export projections**.
- Phase 1 uses a **Repoindex-local exporter**.
- A **Repoindex export snapshot** is the source read model for a **Repoindex export projection**.
- An **Export-only repo row** may root a **Typed CSV export** without changing the **Canonical repo graph**.
- A **Repoindex export projection** uses a **Typed CSV export** shape for Phase 1.
- A **Typed CSV export** preserves edge details in a **Relationship metadata column** until traversal proofs require promoted columns.
- A **Typed CSV export** preserves node-specific details in a **Node metadata column** until traversal proofs require promoted columns.
- A **Node metadata column** and **Relationship metadata column** use **Canonical export JSON**.
- A **Repoindex export projection** records an **Injected export time**.
- A **Repoindex export projection** uses **Deterministic projection metadata**.
- An **Export-only repo row** uses a **Repo root identity**, not an absolute checkout path.
- Phase 1 sets **Repo root identity** to the repo key.
- An **Export artifact** is built before a caller writes it to a projection directory.
- A **Replace-all export write** may replace the projection export directory, but not broader `.foxctl` state.
- Empirical edges use **Passive edge export** in Phase 1.
- Phase 1 golden tests use a **Synthetic export fixture**.
- Phase 1 writer tests use a **Writer-boundary golden**.
- A **Repoindex export projection** may feed a **Ladybug integration** after the CLI gate passes.
- A **Ladybug schema artifact** is created only after the Phase 0 CLI gate passes.
- A **Context enrichment projection** may enrich a **Graph lens** later, but must reference **Canonical repo graph** IDs.
- Any user-facing **Repoindex export projection** command must be an **Envelope-shaped command**.
- A **Graph lens** must be rebuildable from the **Canonical repo graph**.

## Example Dialogue

> **Dev:** "Can we implement the Ladybug work before `lbug` is installed?"
> **Domain expert:** "Only the **Repoindex export projection**. The **Ladybug integration** starts after the CLI gate passes."
>
> **Dev:** "Should contextplane notes be in the first export?"
> **Domain expert:** "No. Keep Phase 1 to a **Typed CSV export** of the **Canonical repo graph**; add a **Context enrichment projection** later if the graph lens earns it."
>
> **Dev:** "Can the first exporter print a plain path list?"
> **Domain expert:** "Only if it stays an internal test helper. A user-facing command must be an **Envelope-shaped command**."
>
> **Dev:** "Can we export edges by expanding every node?"
> **Domain expert:** "No. Expansion is a capped query surface. Use a **Repoindex export snapshot** for full export."
>
> **Dev:** "Should the first exporter live in a new internal package?"
> **Domain expert:** "No. Use a **Repoindex-local exporter** until the proof earns a wider boundary."
>
> **Dev:** "Should we add a repo node kind to repoindex for Ladybug?"
> **Domain expert:** "No. Use an **Export-only repo row** unless repoindex itself needs that model."
>
> **Dev:** "Should semantic edge metadata become Ladybug columns now?"
> **Domain expert:** "No. Keep a **Relationship metadata column** first, but treat typed relationship properties as the first expansion point if proof queries need them."
>
> **Dev:** "Should every node kind get custom columns in Phase 1?"
> **Domain expert:** "No. Use shared columns plus a **Node metadata column**; promote columns only when a proof query earns them."
>
> **Dev:** "Can invalid edge metadata pass through the CSV?"
> **Domain expert:** "No. Exported metadata uses **Canonical export JSON**, so malformed repoindex metadata fails the export."
>
> **Dev:** "Can the exporter call the system clock?"
> **Domain expert:** "No. Use an **Injected export time** so tests and artifacts are reproducible."
>
> **Dev:** "Should `meta.json` include the local repoindex DB path?"
> **Domain expert:** "No. Use **Deterministic projection metadata**; debug paths belong in a future command envelope."
>
> **Dev:** "Should `nodes_repo.csv` include the absolute workspace root?"
> **Domain expert:** "No. Use a **Repo root identity** so the projection artifact is portable."
>
> **Dev:** "What is the Phase 1 repo root identity?"
> **Domain expert:** "Use the repo key. Friendlier local names belong in command envelope metadata later."
>
> **Dev:** "Should export building decide the `.foxctl` output path?"
> **Domain expert:** "No. Build an **Export artifact** first, then write it to a caller-supplied directory."
>
> **Dev:** "Can the writer leave old CSVs in place?"
> **Domain expert:** "No. Use a **Replace-all export write** for the export directory so omitted files do not become stale data."
>
> **Dev:** "Should export generate co-change edges?"
> **Domain expert:** "No. Use **Passive edge export**: write stored co-change edges only if repoindex already has them."
>
> **Dev:** "Should the exporter golden test build a temporary Go repo?"
> **Domain expert:** "No. Use a **Synthetic export fixture** so the test covers export behavior, not parser behavior."
>
> **Dev:** "Is comparing the in-memory artifact enough?"
> **Domain expert:** "No. Use a **Writer-boundary golden** so file bytes and stale-file cleanup are covered."
>
> **Dev:** "Should we add Ladybug schema placeholders now?"
> **Domain expert:** "No. A **Ladybug schema artifact** waits until the `lbug` CLI gate passes."

## Flagged Ambiguities

- "Ladybug spike" can mean both the dependency-free **Repoindex export projection** and the CLI-backed **Ladybug integration**. Resolved: Phase 1 exporter work can proceed without `lbug`; import and query scripts wait for the CLI gate.
- "contextplane export" can imply mixing memory and code identity into the same base graph. Resolved: contextplane belongs to a later **Context enrichment projection** that points back to canonical repo graph IDs.
