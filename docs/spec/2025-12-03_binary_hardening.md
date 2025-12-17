---
description: Binary hardening for the agentctl CLI (garble, PIE, UPX, runtime hooks)
---

# Binary Hardening for agentctl CLI

> **Status:** Draft (Phase 0 – design only; no code changes yet) **Version:**
> 0.1 **Author:** AI assistant (to be updated by human owner) **Related specs:**
>
> - `docs/spec/core_profile_v1.md` (envelope, CAS, jobs)
> - `docs/spec/review_gate.md` (review + CI invariants)
>
> **Goal:** Define a hardened build pipeline and runtime behavior for the
> `agentctl` CLI that makes reverse‑engineering and live debugging _more
> expensive_ while preserving Core Profile v1 contracts and existing CLI
> semantics.

---

## 1. Problem Statement & Goals

### 1.1 Context

`agentctl` is a single‑binary Go CLI that:

- Encodes significant runtime metadata into the binary (Go symbol/table, DWARF,
  file paths).
- Runs as a general developer tool and may embed sensitive logic (e.g.,
  integration patterns, API flow heuristics) that we would prefer not to expose
  trivially to reverse‑engineering.

### 1.2 Goals (MUST / SHOULD)

- **G1 (Wire compatibility)** – Hardened builds **MUST NOT** change:
  - JSON envelope schema (see `core_profile_v1.md`).
  - Exit codes and CLI flags.
  - CAS semantics and on‑disk layouts.
- **G2 (Optional hardened build)** – Provide one or more **opt‑in** hardened
  build targets (Makefile + CI). Default dev builds remain unchanged and
  debuggable.
- **G3 (Static analysis resistance)** – Hardened binaries **SHOULD**:
  - Strip Go symbol tables and DWARF.
  - Remove local build paths.
  - Obfuscate function/variable names and string literals where safe.
- **G4 (Runtime resistance)** – Hardened binaries **SHOULD**:
  - Detect common debugger attach patterns (Linux first).
  - Avoid emitting stack traces with internal function names on panic.
- **G5 (Defense in depth)** – Layer techniques (strip → obfuscate → pack)
  without assuming any single layer is sufficient.
- **G6 (Operational safety)** – Hardened builds **MUST** still be:
  - Reproducible in CI.
  - Testable via existing test/CI flows (smoke tests at minimum).

### 1.3 Non‑Goals (Explicitly Out of Scope)

- No DRM or license enforcement.
- No kernel‑level anti‑debug tricks or anti‑VM heuristics.
- No changes to WASI/exec policies, CAS formats, or on‑disk job layouts.
- No guarantees against a dedicated reverse‑engineer with unlimited time; we
  only raise the cost.

---

## 2. Threat Model (Informal)

### 2.1 Adversary Capabilities

We assume attackers may:

- Download a released `agentctl` binary.
- Run it in local VMs/containers with root access.
- Attach common debuggers (e.g., `gdb`, `lldb`, `dlv`), disassemblers, and
  decompilers.
- Inspect process memory and disk, including unpacked temporary files.

We **do not** assume attackers can:

- Break modern cryptography (e.g., SHA‑256 used in CAS).
- Compromise signing or distribution channels.

### 2.2 Objectives of the Adversary

- Understand internal control flow and heuristics in detail.
- Discover integration patterns and private protocol details.
- Build grey‑market clones or bypass guardrails.

### 2.3 Defensive Objectives

- **D1** – Remove low‑hanging metadata (symbols, DWARF, build paths).
- **D2** – Obfuscate control‑flow and constants enough to defeat quick pattern
  matching.
- **D3** – Make dynamic debugging more cumbersome.
- **D4** – Avoid leaking extra structural information at runtime (panic stack
  traces, verbose errors).

---

## 3. High‑Level Design

### 3.1 Layered Hardening Pipeline

At a high level, hardened builds follow this pipeline:

```mermaid
graph LR
  A[Go source] --> B[Standard go build]
  B --> C[Garble obfuscation build]
  C --> D[Hardened binary]
  D --> E[UPX packer (optional)]
  E --> F[Packed hardened binary]
```

- **Layer 1 – Baseline Go flags**: `-s`, `-w`, `-trimpath`, `-buildmode=pie`.
- **Layer 2 – Obfuscation**: `garble` with `-literals`, `-tiny`.
- **Layer 3 – Packing (optional)**: `upx --best --lzma` for smaller, compressed
  binaries.

The standard developer build remains `go build` with debuggable symbols and
without these flags.

### 3.2 Build Variants

We introduce three logical build variants (naming TBD in impl plan):

- **dev** – current behavior; no additional hardening.
- **hardened** – baseline flags only (`-s -w -trimpath -buildmode=pie`).
- **hardened+obf** – hardened + `garble` wrapping; optionally packed with UPX
  for release artifacts.

### 3.3 Platform Considerations

- **Linux (amd64/arm64)** – full pipeline expected to work:
  - `-buildmode=pie` for ASLR.
  - `ptrace`‑based anti‑debugging.
- **macOS (amd64/arm64)** – baseline hardening and garble expected to work; UPX
  support depends on available tooling and notarization constraints.
- **Windows** – out of scope for initial phase; documented explicitly in
  rollout.

---

## 4. Build‑Time Hardening Requirements

### 4.1 Baseline `go build` Flags (Hardened Variant)

For hardened builds, the Go toolchain **MUST** be invoked with:

- `-ldflags="-s -w"` – strip symbol table and DWARF.
- `-trimpath` – remove local filesystem paths from the binary.
- `-buildmode=pie` – produce a Position Independent Executable where supported.

These flags **MUST** be additive to existing `ldflags` used for version
stamping; the spec does not change how versions or commit hashes are embedded.

### 4.2 Obfuscation via `garble`

Hardened+obfuscated builds **SHOULD** use
[garble](https://github.com/burrowers/garble) as a drop‑in wrapper for
`go build`:

- Invoked as: `garble -literals -tiny build`.
- Must respect the same module root and main package as standard builds.
- Must apply the same `-ldflags`, `-trimpath`, and `-buildmode=pie` flags.

Constraints:

- Code that relies on reflection **MUST** be made explicit via struct tags
  (e.g., `json:"field"`) to preserve wire formats.
- Public JSON and envelope schemas **MUST NOT** change. Obfuscation is internal
  only.

### 4.3 Packing via UPX (Optional Layer)

Release jobs **MAY** pack already‑hardened binaries using UPX:

- Recommended flags: `upx --best --lzma`.
- The packed binary **MUST** pass minimal smoke tests (e.g.,
  `agentctl --version`, a basic command) before being published.

The spec acknowledges that UPX is reversible by skilled analysts and treats it
as a convenience and size optimization, not a core security guarantee.

---

## 5. Runtime Hardening Requirements

### 5.1 Anti‑Debugging (Linux First)

Hardened builds for Linux **SHOULD** perform a debugger‑presence check early in
process startup. A minimal approach based on `ptrace` is sufficient:

- Attempt `PTRACE_TRACEME` via
  `syscall.RawSyscall(syscall.SYS_PTRACE, PTRACE_TRACEME, ...)`.
- If the call fails with a non‑zero errno, treat this as "debugger present".

On detection, implementations **MAY**:

- Emit a single error envelope or log entry with a dedicated error code (e.g.,
  `EDEBUGGER`).
- Exit non‑zero without proceeding into normal CLI logic.

Non‑Linux targets **MAY** provide stubs that always report "not debugged" in the
first iteration of this spec.

### 5.2 Panic Handling & Stack Trace Suppression

For hardened builds, the `agentctl` entrypoint **SHOULD**:

- Wrap its main execution path in a `defer`‑`recover` block.
- On panic:
  - Suppress Go’s default stack trace from being printed directly to
    stdout/stderr.
  - Log a concise, non‑sensitive error message.
  - Optionally emit a single error envelope if this occurs during
    envelope‑producing commands.
  - Exit with a non‑zero status code.

Library code **MUST NOT** rely on panics for regular control flow; this spec
only constrains what is emitted when unexpected panics occur.

### 5.3 Behavior Parity

- Hardened binaries **MUST** accept the same CLI flags and produce the same
  envelopes for the same inputs, modulo:
  - Minor differences in timing.
  - Omission of stack traces and internal function names from error output.

---

## 6. Rollout Plan

### 6.1 Phased Adoption

1. **Phase A – Spec & Plan (this document + impl plan)**
   - No code or build changes.
2. **Phase B – Baseline hardened build target**
   - Add Makefile targets for `hardened` builds with standard Go flags.
   - Introduce a minimal CI job that builds and smoke‑tests a hardened binary.
3. **Phase C – Garble obfuscation**
   - Integrate `garble` in dedicated Makefile targets.
   - Pin garble version in tooling and CI images.
4. **Phase D – UPX packing (optional release flavor)**
   - Add post‑build packing and post‑pack smoke tests.
5. **Phase E – Runtime hooks**
   - Implement debugger detection and panic wrappers in the CLI entrypoint.

### 6.2 CI and Testing Requirements

- Hardened build jobs **MUST NOT** skip lint, vet, or unit tests on the
  underlying codebase.
- Smoke tests for hardened binaries **MUST** include at least:
  - `agentctl --version`.
  - One representative command that exercises envelope emission.
- Any failures in hardened jobs **MUST** block publishing hardened artifacts but
  **MUST NOT** block normal dev builds unless explicitly configured to do so.

---

## 7. Rollback Plan

If hardened builds cause regressions or operational issues, rollback proceeds as
follows:

1. **Stop publishing hardened artifacts**
   - Disable or comment out release‑time jobs that upload hardened binaries.
2. **Keep dev builds unchanged**
   - Since hardened builds are opt‑in, normal developer workflows remain
     unaffected.
3. **Revert Makefile / CI hooks if necessary**
   - Remove or disable dedicated hardened targets while preserving standard
     targets.
4. **Document incident and follow‑ups**
   - Add an entry to the relevant changelog and Gotchas docs describing the
     issue and chosen mitigation.

---

## 8. Open Questions & Future Work

- **OQ1:** Do we need a separate "debuggable" hardened mode (e.g., garble
  without `-tiny`) for internal testing?
- **OQ2:** Do we want per‑platform knobs (e.g., UPX only on Linux, not macOS)?
- **OQ3:** Are there specific subcommands that SHOULD NOT run anti‑debugging
  checks (e.g., internal debug tools)?
- **OQ4:** Should we introduce metrics around hardened binary usage (e.g.,
  version + build flavor)?

These questions are to be resolved in the implementation plan and subsequent
PRs; they are not blocking for adopting this spec as **Draft**.
