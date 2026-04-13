# Skills/Internal Refactor Tasks

Goal: identify refactoring opportunities to consolidate skills into internal packages, generalize internal code for reuse, and flag anti-Go patterns for cleanup.

## Task List
- [x] Inventory internal packages for duplication or overly-specific helpers that can be generalized.
- [x] Identify skill-level helpers that should move into internal (error handling, config resolution, provider selection, envelopes).
- [x] Note anti-Go patterns (e.g., global state, duplicated branching, error wrapping inconsistencies) with suggested fixes.
- [x] Propose a prioritized sequence for refactors (internal first, then skills), including risks and test impact.

## Active TODOs
- [x] Update hooks/dispatch input to accept `meta` (fix hook decode errors from agentctl-hook).
- [x] Apply executil typed decode helper to remaining agentctl callsites.
- [x] Add executil.Start helper and replace remaining background Start callsites.
- [x] Move adb/idb helpers into skillslib/mobileutil and update mobile/expo skills to use them.
- [x] Extract adb device listing parsing into skillslib/mobileutil and reuse in mobile skills.
- [x] Extract idb list-target parsing into skillslib/mobileutil and reuse in mobile skills.
- [x] Extract adb input text escaping into skillslib/mobileutil and reuse in mobile skills.
- [x] Extract adb device info property collection into skillslib/mobileutil and reuse in mobile skills.
- [x] Extract Android launch component resolution into skillslib/mobileutil and reuse in mobile skills.
- [x] Extract owner/repo resolution into skillslib/ci and reuse in ci_* skills.
- [x] Embedder consolidation (internal/intelligence/indexing/semantic): centralize model/provider selection and update callers.
- [x] Consolidate brace/language detection (codeedit + codecontext + fsutil).
- [x] Skills->internal extraction: move code_smart_write path resolution + edit pipeline helpers into skillslib.
- [x] Split monolithic internal functions (agent daemon Run, todosync SyncFromProvider, trajectorycapture CaptureResult).
- [x] LSP helper extraction for lsp_* skills.
- [x] CI helpers extraction for ci_* skills.
- [x] Edit pipeline helpers for code_smart_write + text_replace edits.
- [x] Edit pipeline helpers for html edits.
- [x] Embedding pipeline helpers for embedding_* skills.
- [x] Hook workspace/actor resolution helpers in skillslib/hookutil; replace per-hook duplication.
- [x] internal/providers/llm: consolidate provider list construction helpers to reduce duplication.
- [x] Standardize non-hook workspace detection via skillslib/workspaceutil and update skills.
- [x] Replace local session ID resolution in skills with sessionkit.ResolveSessionID.
- [x] Migrate remaining manual envelope skills to skillmain/skillout.
- [x] Continue skills->internal consolidation for fs/text/todo helpers.
- [x] Expand anti-Go pattern scan and record follow-up refactor tickets.
- [x] Executil sweep: replace direct exec.Command usage in common skills with skillslib/executil helpers.
- [x] Next refactor batch: identify and extract another shared helper into internal for common skills.

## Internal Refactor Candidates
- [x] internal/agent/daemon/daemon.go: split Run into store init, optimization setup, tool registry build, and heartbeat lifecycle.
- [x] internal/intelligence/indexing/semantic/factory.go: extract shared config parsing for Create/CreateWithProvider.
- [x] internal/intelligence/indexing/semantic/embedder.go: add config-driven embedder constructor to reduce per-skill provider logic.
- [x] internal/intelligence/codecontext/expander/brace.go and internal/adapters/skillslib/codeedit/codeedit.go: unify brace matching logic.
- [x] internal/platform/fsutil/fsutil.go: replace large switch with map/table; share with codeedit language detection.
- [x] internal/todosync/sync.go: split SyncFromProvider into smaller helpers (mapping, dependency inference, updates).
- [x] internal/trajectorycapture/capture.go: split CaptureResult into event-kind, summary/meta, and persistence helpers.

## Skills -> Internal Extraction Candidates
- [x] skills/lsp_*: consolidate shared LSP client/types/dispatch into internal/adapters/skillslib/lsp.
- [x] skills/code_smart_write/main.go: move resolvePaths/processFile into skillslib edit helpers for reuse.
- [x] skills/ci_*: centralize GitHub token/repo resolution and HTTP helpers in internal/ci or skillslib/ci.
- [x] skills/code_context_*: centralize block expander/types into skillslib/codeblocks.
- [x] skills/code_context_*: move match grouping/expansion into skillslib/codeblocks.
- [x] skills/embedding_*: move shared queue/worker/provider selection logic into internal/intelligence/indexing/semantic.
- [x] skills/todo*: expose internal/todosync mapping helpers for consistent status handling.
- [x] skills/fs_*: shared include/exclude matcher and hidden filtering in internal/platform/fsutil or skillslib/fsfilter.
- [x] skills/session_* + code_semantic_search: centralize LLM provider selection in internal/providers/llm.
- [x] skills/*_ripgrep: centralize ripgrep availability checks into skillslib/rgutil.
- [x] skills/text_{grep,ripgrep}: share match struct/snippet trimming in skillslib/textmatch.
- [x] skills/text_{grep,replace}: share regex flag compilation in skillslib/textmatch.
- [x] skills/fs_* + text_*: centralize common exclude globs in skillslib/fs.
- [x] skills/text_* + code_context_ripgrep: centralize required pattern validation in skillslib/textmatch.
- [x] skills/code_{imports,symbols,snippet_extract,incremental_index}: share language detection in skillslib/langutil.
- [x] skills/code_*: centralize hidden/common exclude checks in skillslib/fsutil.
- [x] skills/code_security + text_replace: centralize binary content detection in skillslib/fsutil.
- [x] skills/fs_* + providers/setup_install: centralize symlink checks in skillslib/fsutil.
- [x] skills/fs_ls/fs_find/code_{security,complexity,symbols}: centralize slice limiting in skillslib/sliceutil.
- [x] skills/lsp_{gopls,pylsp,tsserver} + code_imports: reuse skillslib/sliceutil for max_results trimming.
- [x] skills/* artifact digests: use skillslib/skillout.AddArtifact helper for consistent injection.
- [x] skills/{hooks_test_feedback,hooks_stop_guard,memory_query,code_semantic_search,session_archive}: replace local truncate helpers with skillslib/skillout.TruncateString.
- [x] skills/codemap_{get,list}: replace local rune truncation with skillslib/skillout.TruncateRunes.
- [x] skills/session_{capture,summarize,restore}: replace local trim+truncate helpers with skillslib/skillout.TruncateSingleLine.
- [x] skills/{mobile_android,mobile_ios,expo,editor_godot}: replace CAS hint string building with skillslib/skillout.FormatCASHint.
- [x] skills/hooks_{test_feedback,stop_guard}: move workspace ID hashing to skillslib/hookutil.
- [x] skills/{session_summarize,todo_continuation}: replace local ensureSlice helpers with skillslib/sliceutil.Clone.
- [x] skills/{hooks_mail_router,hooks_overseer_inbox,todo_continuation}: replace local minInt helpers with skillslib/mathutil.MinInt.
- [x] skills/{session_anchor,todo_continuation}: replace local normalizeStrings helpers with skillslib/stringutil.NormalizeStrings.
- [x] skills/{mobile_android,mobile_ios}: replace preview tail logic with skillslib/textutil.JoinTail.
- [x] skills/{code_diff,code_snippet_extract}: replace local splitLines helpers with skillslib/textutil.SplitLines.
- [x] skills/{code_counsel,code_smart_read}: centralize secret scanning in skillslib/secretutil.
- [x] skills/hooks_*: centralize hook output envelopes via skillslib/hookutil.EmitOutput.
- [x] skills/{code_context_ripgrep,text_ripgrep}: centralize empty result payloads via skillslib/textmatch.EmptySearchResult.
- [x] skills/test_run: replace local truncate helper with skillslib/skillout.TruncateStringWithSuffix.
- [x] skills/{data_jq,git_status}: replace manual preview truncation with skillslib/skillout.TruncateStringWithSuffix.
- [x] skills/{session_summarize,session_extract_learnings}: replace local shortHash helpers with skillslib/hashutil.ShortHash.
- [x] skills/{fs_read,code_snippet_extract}: replace local countLines helpers with skillslib/textutil.CountLinesBytes.
- [x] skills/{memory_query,session_recall,code_semantic_search}: default limit/min_similarity via skillslib/mathutil helpers.
- [x] skills/{fs_apply_edit,skill_inspect}: replace local line counting with skillslib/textutil helpers.
- [x] skills/{embedding_memories,embedding_refresh,embedding_queue,plan_sync}: centralize workspace defaulting via skillslib/workspaceutil.
- [x] skills/{session_*,todo_continuation,epic_complete}: route workspace defaults through skillslib/workspaceutil.
- [x] skills/test_run: use skillslib/executil for command execution + stderr capture.
- [x] skills invoking agentctl CLI: centralize subprocess + envelope decode in skillslib/executil.
- [x] Continue executil sweep for remaining common skills (code_git, git_status, git_worktree, text_replace, data_jq, hooks_impact_analysis, hooks_subagent_start, session_restore).
- [x] Add helper to decode envelope data into typed structs to reduce marshal/unmarshal repetition.

## Anti-Go Patterns / Risks
- [x] Large monolithic functions (daemon Run, todosync.SyncFromProvider, trajectorycapture.CaptureResult) reduce testability.
- [x] Duplicate language/brace parsing logic across packages; consolidate to single source of truth.
- [x] skills/html_edit uses errors.New instead of skillerr; missing error codes/hints.
- [x] Repeated inline map type assertions for envelopes/metadata; extract typed helpers.
- [x] skills/setup_agentctl_mode bypasses skillmain/skillout (manual envelope + no validation); migrate to standard skill entrypoint.
- [x] skills/todo + hooks/impact_analysis log via fmt.Fprintf(os.Stderr) instead of structured logger; standardize on rc.Logger for consistent telemetry.
- [x] Remaining stderr logging in skills/context_filter, skills/code_semantic_search (rerank), skills/plan_sync, skills/mcp_bridge, skills/codemap_generate; migrate to rc.Logger.

## Phase 2 (Beyond Skills/Internal)
- [x] Inventory skill manifest/artifact resolution callsites (hooks, daemon, codemap, agent tools, web/api) and draft a shared helper in internal/domain/skill.
- [x] Consolidate skill run envelope parsing/error mapping into a shared helper for internal callers.
- [x] Standardize config.Load + workspace resolution across cmd/agentctl/cmd with a shared helper and consistent error wrapping.
- [x] Replace remaining inline map assertions in internal/agent/tools and runservice with maputil helpers or typed envelope decode.
- [x] Reduce duplicated testwatch config loading flows in cmd/agentctl/cmd/watch.go and cmd/agentctl/cmd/testwatch.go.

## Phase 3 (Next Refactor Pass)
- [x] Extend protocol.DecodeEnvelope usage to hook skill output parsing (hooks/skill_runner).
- [x] Centralize skill resolve+run+decode into a single helper for internal tool callers (agent/tools + codemap/tools).
- [x] Normalize hook + tool error mapping to include hint propagation consistently.
- [x] Audit and consolidate remaining JSON map assertions in test helpers into maputil where useful.
