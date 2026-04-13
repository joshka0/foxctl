# Skills/Internal Refactor Tasks

Goal: reduce duplication and improve maintainability by consolidating shared logic in internal packages, then updating skills to use those helpers, then addressing anti-Go patterns.

## Sequence

- [x] Internal consolidation (skill resolution, execution, paths, CAS utilities).
- [x] Skill refactors to use new internal helpers (edits, ripgrep, op dispatch, path validation).
- [x] Anti-pattern cleanup (panic/os.Exit in non-test/library code, error handling consistency).
- [x] Embedding refactors (provider selection, op validation).
- [x] Error handling alignment across remaining op-based skills (oputil + usage hints).
- [x] Embedding worker consolidation (provider selection + error handling).
- [x] Config-driven embedder model overrides across skills.
- [ ] Verify model override end-to-end (config update + worker + tests).
- [ ] Per-scope embedding model overrides via config.

## Task Details

### 1) Internal consolidation

- [x] Unify skill resolution + artifact lookup across `internal/daemon/skill_resolver.go`, `internal/interfaces/web/api/skill_runner.go`, `internal/hooks/resolver.go`, `internal/domain/skill/resolver.go`.
- [x] Centralize skill search-path building (env override, config, dev paths).
- [x] Share exec/WASI execution primitives between web API runner and runservice/execution.
- [x] Consolidate CAS helpers and preview/hint building used by skills.
- [x] Consolidate path utilities (hooks/pathutil, skillslib/pathutil, platform/fsutil overlap).

### 2) Skill refactors (adopt internal helpers)

- [x] Edit engines: fold `skills/fs_apply_edit/main.go` and `skills/code_smart_write/main.go` into a shared edit helper.
- [x] Ripgrep: extract common option/default wiring for `skills/text_ripgrep/main.go`, `skills/code_context_ripgrep/main.go`, `skills/code_context_grep/main.go`.
- [x] CAS usage: unify preview/backup/hint behaviors (`skills/fs_read/main.go`, `skills/fs_apply_edit/main.go`, `skills/code_stats/main.go`, `skills/cove_verify/main.go`).
- [x] Operation dispatch validation: reuse `internal/adapters/skillslib/oputil` in `skills/mobile_android/main.go`, `skills/mobile_ios/main.go`, `skills/expo/main.go`, `skills/git_status/main.go`, `skills/todo/main.go`, `skills/mailbox/main.go`.
- [x] Path validation helpers: centralize `rc.PathValidator.ValidatePath` error wrapping and hints in skillmain helpers; adopt in path-heavy skills.

### 3) Anti-pattern cleanup

- [x] Replace panics in non-test helpers (`internal/platform/errors/errors.go`, `internal/adapters/skillslib/pathutil/pathutil.go`, `internal/storage/memory/factory.go`, `internal/daemon/client.go`) with error returns or confine to init-only `Must*`.
- [x] Remove `os.Exit` from `internal/adapters/skillslib/skillout/emit.go`; prefer returning errors to main.
- [x] Fix result-without-error pattern in `internal/interfaces/web/api/skill_runner.go` to return errors on failures.

### 4) Embedding refactors

- [x] Use `semantic.Embedder` across embedding skills to consolidate provider selection (`skills/embedding_memories/main.go`, `skills/embedding_tasks/main.go`, `skills/embedding_refresh/main.go`).
- [x] Standardize embedding queue operation validation with `oputil` (`skills/embedding_queue/main.go`).

### 5) Error handling alignment (op-based skills)

- [x] Replace ad-hoc op parsing/errors with `oputil` in remaining skills (graph, graph_cleanup, providers, json_transform, lsp_pylsp, lsp_tsserver, session_anchor, x402_payment, mobile).
- [x] Standardize bad op and missing payload errors via `skillerr` with usage hints.
- [x] Ensure output uses normalized `oputil` operation value.

### 6) Embedding worker consolidation

- [x] Replace manual provider selection and custom Gemini embedding in `skills/embedding_worker/main.go` with `semantic.Embedder`.
- [x] Ensure expected dimension checks and stored metadata use embedder results.

### 7) Config-driven embedder overrides

- [x] Allow `config.embedding.model` to override `semantic.NewEmbedder` selection across skills.
- [x] Thread model overrides through helper functions that construct embedders.

### 8) Verification

- [x] Update `~/.agentctl/config.yaml` embedding model to Voyage.
- [ ] Enqueue + run `embedding/worker` and confirm stored model metadata (blocked: voyage DNS lookup failure).
- [x] Run `go test ./skills/session_summarize`.

### 9) Per-scope embedder config

- [x] Add `embedding.models` to config for per-scope overrides (symbols, memory, tasks, sessions, codemaps).
- [x] Normalize override keys + values in config loading.
- [x] Apply scope-specific overrides when constructing embedders and storing model metadata.

## Testing Notes

- go test ./internal/intelligence/codemap/... and ./cmd/agentctl/cmd currently fail with duplicate sqlite3 symbols (likely needs `-tags=libsqlite3` or CGO/linker config alignment).
