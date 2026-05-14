### Binary Hardening for foxctl – Implementation Plan

This plan assumes the spec in `docs/spec/2025-12-03_binary_hardening.md` is the
source of truth. No code changes should land that contradict the spec.

Focus: build‑time hardening (flags, garble, UPX) and runtime hooks (anti‑debug,
panic handling) for the `foxctl` CLI, while keeping Core Profile v1 contracts
intact.

---

## Phase A – Spec + Guardrails (this PR)

**Status:** This document + the spec file; no code changes.

- **A1 – Spec:** Add `docs/spec/2025-12-03_binary_hardening.md` with:
  - Problem statement, goals, non‑goals, threat model.
  - Layered pipeline diagram.
  - Rollout + rollback sections.
- **A2 – Impl plan:** Add this document under `docs/impl_plan/` to break the
  work into PR‑sized phases.
- **Validation:**
  - Spec and plan reference existing Core Profile docs instead of redefining
    envelope/CAS behavior.
  - No changes to code, Makefile, or CI in this phase.

_You are here._

---

## Phase B – Baseline Hardened Build Targets (Go Flags Only)

**Goal:** Add hardened build targets using only standard Go flags: `-s -w`,
`-trimpath`, `-buildmode=pie` where supported.

### PR B1 – Makefile Targets for Hardened Builds

- **Scope**
  - Extend the top‑level `Makefile` with:
    - `HARDEN_LDFLAGS := -s -w`.
    - `HARDEN_GOFLAGS := -trimpath`.
    - `HARDEN_BUILD_FLAGS := -buildmode=pie` (guarded per‑platform if needed).
  - Add explicit hardened targets (names TBD, e.g.):
    - `build-hardened-linux` / `build-hardened-darwin`, or a single
      `build-hardened` that respects `GOOS`/`GOARCH`.
  - Compose with existing flags instead of replacing them:
    - Reuse current version/commit `ldflags` and simply append
      `$(HARDEN_LDFLAGS)`.
  - Do **not** change the default `build`/`release` targets yet.

- **Validation**
  - `make build-hardened` (for at least one platform) produces a binary that:
    - Runs `foxctl --version` successfully.
    - Runs at least one representative command that emits a JSON envelope.
  - Compare file size and `go tool nm` output between dev vs hardened builds to
    confirm that symbols and DWARF are removed.

### PR B2 – CI Job for Hardened Build Smoke Tests

- **Scope**
  - In `.github/workflows/ci.yml` (or equivalent):
    - Add a job that runs `make build-hardened` on `linux/amd64`.
    - Run minimal smoke tests against the hardened binary (e.g., `--version` +
      one CLI command).
  - Ensure this job:
    - Runs **after** standard lint/tests.
    - Fails the workflow if hardened build or smoke tests fail.

- **Validation**
  - CI passes with the new job enabled on main.
  - No changes to existing jobs’ behavior beyond the extra job runtime.

---

## Phase C – Garble Obfuscation (Hardened+Obfuscated Build)

**Goal:** Introduce an opt‑in hardened+obfuscated build that wraps `go build`
with `garble -literals -tiny`.

### PR C1 – Tooling & CI Image Prep

- **Scope**
  - Ensure `garble` is available in environments that build hardened+obf
    binaries:
    - Add `go install mvdan.cc/garble@<pinned>` to `deploy/docker/Dockerfile.ci` or an
      equivalent build image.
    - Optionally add a blank import in `tools.go` to keep `garble` pinned via
      `go.mod`.
  - Update any dev docs under `docs/start/` to mention the requirement for local
    hardened builds (e.g., `go install mvdan.cc/garble@...`).

- **Validation**
  - `make` (without hardened targets) remains unchanged.
  - CI image rebuild completes and hardened+obf jobs (added in C2) can find the
    `garble` binary.

### PR C2 – Makefile Targets for Hardened+Obfuscated Builds

- **Scope**
  - Add new Makefile targets, e.g. `build-hardened-garble`:
    - Invoke `garble -literals -tiny build` with:
      - The same main module path as the normal build.
      - The same version/commit `-ldflags`, plus `-s -w`.
      - `-trimpath` and `-buildmode=pie` where supported.
  - Make the target explicitly opt‑in; do not wire it into default `make build`.

- **Validation**
  - Running `make build-hardened-garble` locally produces a binary that:
    - Passes the same smoke tests as Phase B (version + a small command).
    - Shows obfuscated symbol names under `go tool nm`.

### PR C3 – Reflection & JSON/Envelope Shape Audit

- **Scope**
  - Audit packages that rely on reflection:
    - `encoding/json`, `text/template`, or custom reflection helpers.
  - For structs used in JSON or envelope I/O:
    - Ensure explicit tags (e.g., `json:"field"`) exist.
    - Confirm no behavior depends on Go identifier names at runtime.
  - Add or update golden tests under `test/golden/` to validate envelope and
    JSON shapes remain unchanged.

- **Validation**
  - Golden tests confirm that for fixed inputs, the serialized JSON/envelopes
    from dev vs hardened+obf builds are identical (modulo non‑semantic
    differences like timestamps).
  - No regressions in reflection‑heavy paths when using hardened+obf binaries.

### PR C4 – CI Job for Hardened+Obfuscated Builds

- **Scope**
  - Add a separate CI job that:
    - Runs `make build-hardened-garble` on at least `linux/amd64`.
    - Executes a lightweight smoke test suite (a subset of normal tests or just
      CLI invocations).
  - Keep this job optional for now (can be trigger‑based or run only on main and
    release branches).

- **Validation**
  - CI remains green on main with hardened+obf job enabled.
  - Failures in this job clearly indicate whether the issue is in garble, flags,
    or application behavior.

---

## Phase D – UPX Packing (Optional Release Flavor)

**Goal:** Optionally pack hardened binaries with UPX for smaller artifacts and
extra friction for static analysis tools.

### PR D1 – Tooling & Local/CI Availability

- **Scope**
  - Decide where UPX is required:
    - CI release image only, or both CI and local dev.
  - Install a pinned UPX version in the release build image (e.g., via package
    manager or prebuilt binary).
  - Document local installation steps (e.g., `brew install upx`).

- **Validation**
  - `upx --version` works in the release build environment.
  - No impact on non‑release CI jobs.

### PR D2 – Dist Targets for Packed Binaries

- **Scope**
  - Add Makefile `dist`-style targets that:
    - Depend on `build-hardened-garble` (or another hardened variant).
    - Run `upx --best --lzma` on the produced binary.
    - Optionally run `upx -t` to verify integrity.
  - Keep standard `dist` targets unchanged; `dist-packed` (name TBD) should be a
    separate, explicit target.

- **Validation**
  - Built artifacts from `dist-packed`:
    - Pass smoke tests (`--version` + 1–2 representative commands).
    - Are significantly smaller on disk than their unpacked counterparts.

### PR D3 – CI Release Job Wiring

- **Scope**
  - Update release workflows (if present) to:
    - Build standard artifacts as before.
    - Optionally build and upload packed hardened artifacts in parallel.
  - Clearly label packed binaries in release metadata (e.g., filename suffix).

- **Validation**
  - Release CI produces both standard and packed binaries without regressions.
  - Downloaded packed binaries from a test release pass the same smoke tests.

---

## Phase E – Runtime Hardening Hooks

**Goal:** Add runtime checks for debuggers (Linux) and panic handling that
suppresses stack traces while preserving Core Profile v1 behavior.

### PR E1 – `internal/hardening` Package (Debugger Detection)

- **Scope**
  - Introduce a small internal package, e.g. `internal/hardening`, with:
    - `func DetectDebugger(ctx context.Context) error` (or `bool`).
  - Linux implementation (build‑tagged):
    - Use `syscall.RawSyscall` with `SYS_PTRACE` and `PTRACE_TRACEME`.
    - Treat non‑zero errno as "debugger present".
  - Non‑Linux stubs:
    - Return "not debugged" (or `nil` error) for now.

- **Integration**
  - Call `DetectDebugger` early in `cmd/foxctl/main.go` before CLI dispatch.
  - On detection:
    - Emit a clearly‑typed error (log and/or envelope) with error code `EPOLICY`
      (debugger attachment is a policy violation per Core Profile v1 §13).
    - Exit with a non‑zero status.

- **Validation**
  - Unit tests:
    - Verify Linux implementation compiles and behaves as expected in simple
      scenarios (best‑effort; full debugger attach tests may be flaky).
    - Verify non‑Linux builds compile and call the stub implementation.
  - Manual verification on Linux:
    - Run under a debugger and confirm early exit behavior.

### PR E2 – Panic Wrapper at CLI Entry

- **Scope**
  - Wrap the main CLI execution path in `cmd/foxctl/main.go` with a
    `defer`/`recover` block that:
    - Catches unexpected panics.
    - Suppresses default Go stack trace printing.
    - Logs a concise error message.
    - Optionally emits a single error envelope when appropriate.
    - Exits non‑zero.
  - Ensure library code is not modified to rely on panics; this is a last‑resort
    safety net.

- **Validation**
  - Add tests that:
    - Trigger a controlled panic in a narrow code path and assert that
      output/logs do **not** contain internal function names or file paths.
    - Confirm the process exits with a non‑zero status.

### PR E3 – Behavior Parity & Golden Updates

- **Scope**
  - Re‑run key golden tests to ensure envelopes from normal error paths remain
    unchanged.
  - Add a small set of goldens (or snapshot tests) for panic scenarios if
    needed, documenting the new, reduced error output.

- **Validation**
  - All existing golden envelopes still match.
  - Any new goldens clearly distinguish between normal errors and panic
    failures.

---

## Phase F – CI, Docs, and Changelog Polishing

**Goal:** Make hardened builds a first‑class but opt‑in path, documented for
both contributors and users.

### PR F1 – CI Matrix & Gating

- **Scope**
  - Decide which hardened variants run on:
    - Every push vs. main only vs. release branches.
  - Ensure hardened build failures:
    - Block publishing hardened artifacts.
    - Do **not** silently bypass lint/tests.

- **Validation**
  - CI remains green with the selected set of hardened jobs.
  - Failure modes are clear and actionable (tooling vs. app regressions).

### PR F2 – Developer Docs

- **Scope**
  - Add or update docs under `docs/start/` to cover:
    - How to build hardened binaries locally (`make` targets, required tooling:
      garble, UPX).
    - How to run quick sanity checks.
    - Caveats around debugging hardened vs dev builds.

- **Validation**
  - New developers can follow the doc to produce a hardened binary from a clean
    checkout.

### PR F3 – Changelog Entry

- **Scope**
  - Add `docs/changelogs/YYYY-MM-DD_cli_binary_hardening.md` with:
    - Summary of new hardened build flavors.
    - Notes on behavior changes (panic output, debugger detection).
    - Any known limitations (e.g., UPX only on Linux, Windows TBD).

- **Validation**
  - Changelog is referenced from the relevant PRs and kept in sync as the
    implementation evolves.
