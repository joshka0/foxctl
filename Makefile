SHELL := /bin/bash
unexport GOROOT
unexport GOBIN
unexport GOTOOLDIR
GO ?= go
GO_CMD := env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=0 $(GO)
GO_CMD_CGO := env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=1 $(GO)
GOCACHE_DIR := $(shell $(GO) env GOCACHE)

RACE_PKGS := $(shell $(GO_CMD_CGO) list ./...)
RACE_SHARDS := core-cmd core-internal runtime context-tooling intelligence platform-interfaces storage v2 skills-a-g skills-h-o skills-p-x
BINARY ?= foxctl
GOFUMPT ?= gofumpt
GOLANGCI ?= golangci-lint
GOLANGCI_TIMEOUT ?= 10m
LINT_TARGETS ?= ./...
COVERAGE_LINE_MIN ?= 40.0
COVERAGE_FUNC_MIN ?= 40.0
COVERAGE_BRANCH_MIN ?= 40.0
COVERAGE_STRICT_LINE_MIN ?= 85.0
COVERAGE_STRICT_FUNC_MIN ?= 80.0
COVERAGE_STRICT_BRANCH_MIN ?= 75.0
GOFILES := $(shell find cmd internal skills -name '*.go')
SKILL_DIRS := $(shell find skills -mindepth 1 -maxdepth 1 -type d)
# Skills requiring CGO (excluded from non-CGO builds).
CGO_SKILLS :=
OPTIONAL_CGO_SKILLS :=

.PHONY: fmt lint typecheck lsp-check vet test test-race test-race-shard test-race-impacted test-race-shard-impacted test-integration test-integration-impacted test-integration-cmd cover check-coverage check-coverage-strict check-doc-links check-large-files check-tech-debt check-duplication test-timing build build-all viewer snapshot tidy check skill skills-build skills-build-cgo skills-build-all skills-impact skills-build-impacted packages-impact test-short-impacted skills-install skills-install-cgo skills-install-all skills-test completions init go-tui-build go-tui-spawn go-tui-agent go-tui go-tui-smoke tui ts-install ts-dev-tui ts-dev-gui ts-build-tui ts-tui ts-build ts-typecheck env-sync env-watch env-watch-stop db-backup db-backup-list db-backup-clean gepa-prompt gepa-cycle gepa-dataset-export gepa-dataset-export-ranked gepa-claude-export gepa-claude-rewrite gepa-leaderboard gepa-compare-batch gepa-judge-baseline eval-code-search-foxctl-package eval-code-search-praze-infra eval-code-search-foxctl-repo-grounded eval-code-search-foxctl-change-impact eval-code-search-foxctl-trace-symbol eval-code-search-foxctl-bridge-esoteric eval-retrieval-foxctl eval-retrieval-foxctl-mixed eval-retrieval-foxctl-cochange eval-retrieval-jido eval-retrieval-praze eval-retrieval-praze-mixed eval-retrieval-praze-k8s

fmt:
	@echo "Running gofumpt"
	@$(GOFUMPT) -w $(GOFILES)

lint:
	@echo "Running golangci-lint"
	@lint_scope=""; \
	base_ref=""; \
	if [ -n "$$CI_MERGE_REQUEST_DIFF_BASE_SHA" ] && git cat-file -e "$$CI_MERGE_REQUEST_DIFF_BASE_SHA^{commit}" >/dev/null 2>&1; then \
		base_ref="$$CI_MERGE_REQUEST_DIFF_BASE_SHA"; \
	elif [ -n "$$CI_MERGE_REQUEST_TARGET_BRANCH_NAME" ] && git rev-parse --verify "origin/$$CI_MERGE_REQUEST_TARGET_BRANCH_NAME" >/dev/null 2>&1; then \
		base_ref="$$(git merge-base HEAD "origin/$$CI_MERGE_REQUEST_TARGET_BRANCH_NAME")"; \
	elif [ -n "$$CI_DEFAULT_BRANCH" ] && git rev-parse --verify "origin/$$CI_DEFAULT_BRANCH" >/dev/null 2>&1; then \
		base_ref="$$(git merge-base HEAD "origin/$$CI_DEFAULT_BRANCH")"; \
	elif git rev-parse --verify origin/main >/dev/null 2>&1; then \
		head_ref="$$(git rev-parse HEAD 2>/dev/null || true)"; \
		main_ref="$$(git rev-parse origin/main 2>/dev/null || true)"; \
		if [ -n "$$head_ref" ] && [ -n "$$main_ref" ] && [ "$$head_ref" != "$$main_ref" ]; then \
			base_ref="$$(git merge-base HEAD origin/main)"; \
		fi; \
	fi; \
	if [ -n "$$base_ref" ]; then \
		lint_scope="--new-from-rev=$$base_ref"; \
		echo "Using diff-aware lint from $$base_ref"; \
	fi; \
	GOFLAGS=-buildvcs=false $(GOLANGCI) run --timeout $(GOLANGCI_TIMEOUT) $$lint_scope $(LINT_TARGETS)

# Type-check all packages (faster than gopls per-file, catches type errors)
# This is essentially what the compiler does during build
typecheck:
	@echo "Type-checking all packages..."
	@$(GO_CMD) build ./...
	@echo "Type-check passed"

# LSP-based diagnostics using gopls (slower, but catches hints/warnings)
# Use for specific files: make lsp-check FILES="internal/storage/cas/store.go"
# Or for a package: make lsp-check FILES="$(find internal/storage -name '*.go')"
LSP_SEVERITY ?= warning
LSP_FILES ?= 
lsp-check:
	@echo "Running gopls check (severity=$(LSP_SEVERITY))..."
	@command -v gopls >/dev/null 2>&1 || { echo "gopls not installed. Run: go install golang.org/x/tools/gopls@latest"; exit 1; }
	@if [ -z "$(LSP_FILES)" ]; then \
		echo "Usage: make lsp-check LSP_FILES=\"file1.go file2.go\""; \
		echo "  or:  make lsp-check LSP_FILES=\"\$$(find internal/storage -name '*.go')\""; \
		echo ""; \
		echo "Note: For full codebase checks, use 'make lint' (golangci-lint) which is faster."; \
		echo "      gopls check is best for targeted file-level diagnostics."; \
		exit 0; \
	fi
	@for f in $(LSP_FILES); do \
		gopls check -severity=$(LSP_SEVERITY) "$$f" || true; \
	done

vet:
	@$(GO_CMD) vet ./...

test:
	@if [ -n "$(GOCACHE_DIR)" ]; then mkdir -p "$(GOCACHE_DIR)"; fi
	@$(GO_CMD) test ./...

test-short:
	@if [ -n "$(GOCACHE_DIR)" ]; then mkdir -p "$(GOCACHE_DIR)"; fi
	@$(GO_CMD) test -short ./...

gepa-prompt:
	@$(GO_CMD) run ./cmd/foxctl optimize prompt $(ARGS)

gepa-cycle:
	@$(GO_CMD) run ./cmd/foxctl optimize prompt cycle $(ARGS)

gepa-compare-batch:
	@$(GO_CMD) run ./cmd/foxctl optimize prompts compare-batch $(ARGS)

gepa-judge-baseline:
	@python3 scripts/preference_judge_baseline.py $(ARGS)

gepa-dataset-export:
	@$(GO_CMD) run ./cmd/foxctl optimize dataset export $(ARGS)

gepa-dataset-export-ranked:
	@$(GO_CMD) run ./cmd/foxctl optimize dataset export-ranked $(ARGS)

gepa-claude-export:
	@$(GO_CMD) run ./cmd/foxctl optimize dataset claude export $(ARGS)

gepa-claude-rewrite:
	@$(GO_CMD) run ./cmd/foxctl optimize dataset claude rewrite $(ARGS)

gepa-leaderboard:
	@$(GO_CMD) run ./cmd/foxctl optimize dataset claude leaderboard $(ARGS)

eval-code-search-foxctl-package:
	@bash ./scripts/eval_code_search_foxctl_package.sh $(ARGS)

eval-code-search-praze-infra:
	@bash ./scripts/eval_code_search_praze_infra.sh $(ARGS)

eval-code-search-foxctl-repo-grounded:
	@bash ./scripts/eval_code_search_foxctl_repo_grounded.sh $(ARGS)

eval-code-search-foxctl-change-impact:
	@bash ./scripts/eval_code_search_foxctl_change_impact.sh $(ARGS)

eval-code-search-foxctl-trace-symbol:
	@bash ./scripts/eval_code_search_foxctl_trace_symbol.sh $(ARGS)

eval-code-search-foxctl-bridge-esoteric:
	@bash ./scripts/eval_code_search_foxctl_bridge_esoteric.sh $(ARGS)

eval-retrieval-foxctl:
	@bash ./scripts/eval_retrieval_foxctl.sh $(ARGS)

eval-retrieval-foxctl-mixed:
	@bash ./scripts/eval_retrieval_foxctl_mixed.sh $(ARGS)

eval-retrieval-foxctl-cochange:
	@bash ./scripts/eval_retrieval_foxctl_cochange.sh $(ARGS)

eval-retrieval-jido:
	@bash ./scripts/eval_retrieval_jido.sh $(ARGS)

eval-retrieval-praze:
	@bash ./scripts/eval_retrieval_praze.sh $(ARGS)

eval-retrieval-praze-mixed:
	@bash ./scripts/eval_retrieval_praze_mixed.sh $(ARGS)

eval-retrieval-praze-k8s:
	@bash ./scripts/eval_retrieval_praze_k8s.sh $(ARGS)

RACE_P ?= 1

test-race:
	@set -euo pipefail; \
		for shard in $(RACE_SHARDS); do \
			$(MAKE) --no-print-directory test-race-shard SHARD="$$shard"; \
		done

test-race-shard:
ifndef SHARD
	$(error SHARD is required. Usage: make test-race-shard SHARD=runtime)
endif
	@set -euo pipefail; \
		case "$(SHARD)" in \
			core-cmd) include_re='^github.com/joshka0/foxctl/(cmd|plugins|scripts|test)(/|$$)' ;; \
			core-internal) include_re='^github.com/joshka0/foxctl/internal/(adapters|agent|auth|console|domain|protocol|providers|rlm)(/|$$)' ;; \
			runtime) include_re='^github.com/joshka0/foxctl/internal/runtime(/|$$)' ;; \
			context-tooling) include_re='^github.com/joshka0/foxctl/internal/(context|tooling)(/|$$)' ;; \
			intelligence) include_re='^github.com/joshka0/foxctl/internal/intelligence(/|$$)' ;; \
			platform-interfaces) include_re='^github.com/joshka0/foxctl/internal/(interfaces|platform)(/|$$)' ;; \
			storage) include_re='^github.com/joshka0/foxctl/internal/storage(/|$$)' ;; \
			v2) include_re='^github.com/joshka0/foxctl/internal/v2(/|$$)' ;; \
			skills-a-g) include_re='^github.com/joshka0/foxctl/skills($$|/[a-g][^/]*($$|/))' ;; \
			skills-h-o) include_re='^github.com/joshka0/foxctl/skills/[h-o][^/]*($$|/)' ;; \
			skills-p-x) include_re='^github.com/joshka0/foxctl/skills/[p-x][^/]*($$|/)' ;; \
			*) echo "Unknown race shard: $(SHARD)" >&2; exit 1 ;; \
		esac; \
		pkgs="$$( $(GO_CMD_CGO) list ./... | grep -E "$$include_re" || true )"; \
		pkgs="$$( printf '%s\n' "$$pkgs" | sed '/^$$/d' | paste -sd' ' - )"; \
		if [ -z "$$pkgs" ]; then \
			echo "No packages for race shard $(SHARD)"; \
			exit 0; \
		fi; \
		echo "Race-testing shard $(SHARD): $$pkgs"; \
		$(GO_CMD_CGO) test -race -short -p $(RACE_P) $$pkgs

test-race-impacted:
ifndef BASE_REF
	$(error BASE_REF is required. Usage: make test-race-impacted BASE_REF=origin/main [HEAD_REF=HEAD])
endif
	@set -euo pipefail; \
		pkgs="$$( $(GO_CMD) run ./scripts/skills_impact --mode packages --base-ref "$(BASE_REF)" --head-ref "$(HEAD_REF)" --format names )"; \
		pkgs="$$( echo "$$pkgs" | tr ' ' '\n' | grep -vx 'github.com/joshka0/foxctl/test/integration' || true )"; \
		pkgs="$$( printf '%s\n' "$$pkgs" | sed '/^$$/d' | paste -sd' ' - )"; \
		if [ -z "$$pkgs" ]; then \
			echo "No impacted packages"; \
			exit 0; \
		fi; \
		echo "Race-testing impacted packages: $$pkgs"; \
		$(GO_CMD_CGO) test -race -short -p $(RACE_P) $$pkgs

test-race-shard-impacted:
ifndef SHARD
	$(error SHARD is required. Usage: make test-race-shard-impacted SHARD=runtime BASE_REF=origin/main [HEAD_REF=HEAD])
endif
ifndef BASE_REF
	$(error BASE_REF is required. Usage: make test-race-shard-impacted SHARD=runtime BASE_REF=origin/main [HEAD_REF=HEAD])
endif
	@set -euo pipefail; \
		case "$(SHARD)" in \
			core-cmd) include_re='^github.com/joshka0/foxctl/(cmd|plugins|scripts|test)(/|$$)' ;; \
			core-internal) include_re='^github.com/joshka0/foxctl/internal/(adapters|agent|auth|console|domain|protocol|providers|rlm)(/|$$)' ;; \
			runtime) include_re='^github.com/joshka0/foxctl/internal/runtime(/|$$)' ;; \
			context-tooling) include_re='^github.com/joshka0/foxctl/internal/(context|tooling)(/|$$)' ;; \
			intelligence) include_re='^github.com/joshka0/foxctl/internal/intelligence(/|$$)' ;; \
			platform-interfaces) include_re='^github.com/joshka0/foxctl/internal/(interfaces|platform)(/|$$)' ;; \
			storage) include_re='^github.com/joshka0/foxctl/internal/storage(/|$$)' ;; \
			v2) include_re='^github.com/joshka0/foxctl/internal/v2(/|$$)' ;; \
			skills-a-g) include_re='^github.com/joshka0/foxctl/skills($$|/[a-g][^/]*($$|/))' ;; \
			skills-h-o) include_re='^github.com/joshka0/foxctl/skills/[h-o][^/]*($$|/)' ;; \
			skills-p-x) include_re='^github.com/joshka0/foxctl/skills/[p-x][^/]*($$|/)' ;; \
			*) echo "Unknown race shard: $(SHARD)" >&2; exit 1 ;; \
		esac; \
		pkgs="$$( $(GO_CMD) run ./scripts/skills_impact --mode packages --base-ref "$(BASE_REF)" --head-ref "$(HEAD_REF)" --format names )"; \
		pkgs="$$( echo "$$pkgs" | tr ' ' '\n' | grep -E "$$include_re" | grep -vx 'github.com/joshka0/foxctl/test/integration' || true )"; \
		pkgs="$$( printf '%s\n' "$$pkgs" | sed '/^$$/d' | paste -sd' ' - )"; \
		if [ -z "$$pkgs" ]; then \
			echo "No impacted packages for race shard $(SHARD)"; \
			exit 0; \
		fi; \
		echo "Race-testing impacted shard $(SHARD): $$pkgs"; \
		$(GO_CMD_CGO) test -race -short -p $(RACE_P) $$pkgs

test-integration:
	@$(GO_CMD) test -tags=integration ./test/integration/... -timeout 15m -v

test-integration-impacted:
ifndef BASE_REF
	$(error BASE_REF is required. Usage: make test-integration-impacted BASE_REF=origin/main [HEAD_REF=HEAD])
endif
	@set -euo pipefail; \
		pkgs="$$( $(GO_CMD) run ./scripts/skills_impact --mode packages --base-ref "$(BASE_REF)" --head-ref "$(HEAD_REF)" --format names )"; \
		if ! echo "$$pkgs" | tr ' ' '\n' | grep -qx 'github.com/joshka0/foxctl/test/integration'; then \
			echo "Integration package not impacted"; \
			exit 0; \
		fi; \
		echo "Running impacted integration package"; \
		$(GO_CMD) test -tags=integration ./test/integration/... -timeout 15m -v

test-integration-cmd:
	@$(GO_CMD) test -tags=integration ./cmd/foxctl/cmd/... -timeout 15m -v

cover:
	@mkdir -p coverage
	@$(GO_CMD) test ./... -covermode=atomic -coverprofile=coverage/coverage.out
	@$(GO_CMD) tool cover -func=coverage/coverage.out

# Coverage thresholds are configurable. The default check enforces the current
# repository floor; use check-coverage-strict for aspirational local targets.
check-coverage:
	@echo "Checking test coverage (line/function/branch)..."
	@echo "Thresholds: line >= $(COVERAGE_LINE_MIN)%, function >= $(COVERAGE_FUNC_MIN)%, branch >= $(COVERAGE_BRANCH_MIN)%"
	@mkdir -p coverage
	@set -euo pipefail; \
		test_log="coverage/test.out"; \
		if ! $(GO_CMD) test ./... -coverprofile=coverage/coverage.out -covermode=atomic > "$$test_log" 2>&1; then \
			grep -v "no test files" "$$test_log" || true; \
			exit 1; \
		fi; \
		grep -v "no test files" "$$test_log" || true
	@$(GO_CMD) tool cover -func=coverage/coverage.out | tee coverage/coverage.txt
	@echo ""
	@echo "=== Coverage Summary ==="
	@awk -v line_min="$(COVERAGE_LINE_MIN)" -v func_min="$(COVERAGE_FUNC_MIN)" -v branch_min="$(COVERAGE_BRANCH_MIN)" '\
		/^total:/ { \
			gsub("%", "", $$3); \
			line = $$3 + 0; \
			func_cov = line; \
			branch = line; \
		} \
		END { \
			status = 0; \
			if (line < line_min) { \
				printf "FAIL Line coverage %.1f%% is below %.1f%% threshold\n", line, line_min; \
				status = 1; \
			} else { \
				printf "OK Line coverage %.1f%% meets %.1f%% threshold\n", line, line_min; \
			} \
			if (func_cov < func_min) { \
				printf "FAIL Function coverage %.1f%% is below %.1f%% threshold\n", func_cov, func_min; \
				status = 1; \
			} else { \
				printf "OK Function coverage %.1f%% meets %.1f%% threshold\n", func_cov, func_min; \
			} \
			if (branch < branch_min) { \
				printf "FAIL Branch coverage %.1f%% is below %.1f%% threshold\n", branch, branch_min; \
				status = 1; \
			} else { \
				printf "OK Branch coverage %.1f%% meets %.1f%% threshold\n", branch, branch_min; \
			} \
			exit status; \
		} \
	' coverage/coverage.txt

check-coverage-strict:
	@$(MAKE) check-coverage \
		COVERAGE_LINE_MIN=$(COVERAGE_STRICT_LINE_MIN) \
		COVERAGE_FUNC_MIN=$(COVERAGE_STRICT_FUNC_MIN) \
		COVERAGE_BRANCH_MIN=$(COVERAGE_STRICT_BRANCH_MIN)

build:
	@set -euo pipefail; \
	VERSION=$$(git describe --tags --always --dirty 2>/dev/null || echo dev); \
	COMMIT=$$(git rev-parse --short HEAD 2>/dev/null || echo unknown); \
	DATE=$$(date -u +%Y-%m-%dT%H:%M:%SZ); \
	$(GO_CMD) build -trimpath \
		-ldflags="-s -w \
		-X github.com/joshka0/foxctl/internal/platform/buildinfo.Version=$$VERSION \
		-X github.com/joshka0/foxctl/internal/platform/buildinfo.Commit=$$COMMIT \
		-X github.com/joshka0/foxctl/internal/platform/buildinfo.Date=$$DATE" \
		-o bin/$(BINARY) ./cmd/foxctl
	@$(GO_CMD) build -trimpath -ldflags="-s -w" -o bin/foxctl-mail ./cmd/foxctl-mail

build-all: build
	@echo "Built bin/$(BINARY)"

init: build skills-install-all
	@./scripts/init.sh

viewer:
	@echo "foxctl-viewer is archived under archive/cmd/foxctl_viewer."
	@echo "Use the canonical Go TUI instead: make go-tui-agent"
	@exit 2

install-mail:
	@./scripts/install-mail.sh

# Go TUI targets
GO_TUI_API_PORT ?= 8090
GO_TUI_API_URL ?= http://127.0.0.1:$(GO_TUI_API_PORT)
GO_TUI_WEB_LOG ?= /tmp/foxctl-go-tui-web.log
GO_TUI_WORKSPACE ?= $(CURDIR)
GO_TUI_PROFILE ?= explorer
GO_TUI_SESSION_ID ?=
GO_TUI_LLM_PROVIDER ?= lmstudio
GO_TUI_LLM_MODEL ?=
GO_TUI_LMSTUDIO_BASE_URL ?= http://localhost:1234/v1
GO_TUI_SYSTEM_PROMPT ?= You are a local foxctl agent running through the Go TUI. Be concise, inspect the repo before making claims, and use available foxctl tools when useful.
GO_TUI_SPAWN_AGENT ?= 0
GO_TUI_AGENT_ROLE ?= coder
GO_TUI_AGENT_NAME ?= Local Fox
GO_TUI_AGENT_EXEC_MODE ?= reactive
GO_TUI_AGENT_PROMPT ?= You are a local foxctl coding agent backed by LMStudio. Help implement and review the current workspace.
GO_TUI_SMOKE_ASK ?= ping from make go-tui-smoke
GO_TUI_SMOKE_TIMEOUT ?= 3s

# Simplified TUI variables (operator cockpit mode)
TUI_API_PORT ?= 8090
TUI_API_URL ?= http://127.0.0.1:$(TUI_API_PORT)
TUI_WEB_LOG ?= /tmp/foxctl-tui-daemon.log
TUI_SCREEN ?= agents

go-tui-build:
	@mkdir -p bin
	@$(GO_CMD) build -trimpath -o bin/foxctl-tui ./cmd/foxctl_tui
	@echo "Built bin/foxctl-tui"

go-tui-spawn: build go-tui-build
	@set -euo pipefail; \
	echo "Ensuring foxctl web API at $(GO_TUI_API_URL)..."; \
	if ! curl -sf "$(GO_TUI_API_URL)/api/health" >/dev/null 2>&1; then \
		echo "Starting web API on :$(GO_TUI_API_PORT) (logs: $(GO_TUI_WEB_LOG))"; \
		LMSTUDIO_BASE_URL=$${LMSTUDIO_BASE_URL:-$(GO_TUI_LMSTUDIO_BASE_URL)} FOXCTL_DB_DRIVER=$${FOXCTL_DB_DRIVER:-sqlite} FOXCTL_V2_EVENTS_DB_DRIVER=$${FOXCTL_V2_EVENTS_DB_DRIVER:-sqlite} ./bin/foxctl web serve --dev-cors --port $(GO_TUI_API_PORT) > "$(GO_TUI_WEB_LOG)" 2>&1 & \
		server_pid=$$!; \
		trap 'kill '"$$server_pid"' 2>/dev/null || true' EXIT; \
		for _ in $$(seq 1 40); do \
			curl -sf "$(GO_TUI_API_URL)/api/health" >/dev/null 2>&1 && break; \
			sleep 0.25; \
		done; \
		curl -sf "$(GO_TUI_API_URL)/api/health" >/dev/null || (echo "API health check failed; tailing $(GO_TUI_WEB_LOG)"; tail -n 120 "$(GO_TUI_WEB_LOG)"; exit 1); \
	else \
		server_pid=""; \
		trap ':' EXIT; \
	fi; \
	agent_id=""; \
	if [ "$(GO_TUI_SPAWN_AGENT)" = "1" ]; then \
		echo "Spawning foxctl agent with provider=$(GO_TUI_LLM_PROVIDER) model=$(GO_TUI_LLM_MODEL)..."; \
		agent_json="$$(python3 -c 'import json,sys; d={"role":sys.argv[1],"name":sys.argv[2],"prompt":sys.argv[3],"workspace_root":sys.argv[4],"exec_mode":sys.argv[5]}; provider=sys.argv[6].strip(); model=sys.argv[7].strip(); base=sys.argv[8].strip(); d.update({"llm_provider":provider} if provider else {}); d.update({"llm_model":model} if model else {}); d.update({"llm_base_url":base} if base else {}); print(json.dumps(d))' "$(GO_TUI_AGENT_ROLE)" "$(GO_TUI_AGENT_NAME)" "$(GO_TUI_AGENT_PROMPT)" "$(GO_TUI_WORKSPACE)" "$(GO_TUI_AGENT_EXEC_MODE)" "$(GO_TUI_LLM_PROVIDER)" "$(GO_TUI_LLM_MODEL)" "$(GO_TUI_LMSTUDIO_BASE_URL)")"; \
		agent_id="$$(curl -sf -X POST "$(GO_TUI_API_URL)/api/agents/spawn" -H 'Content-Type: application/json' -d "$$agent_json" | python3 -c 'import json,sys; data=json.load(sys.stdin); print(data.get("actor_id") or data.get("agent_id") or "")')"; \
		echo "Spawned foxctl agent $$agent_id"; \
	fi; \
	session_id="$(GO_TUI_SESSION_ID)"; \
	if [ -z "$$agent_id" ] && [ -z "$$session_id" ]; then \
		echo "Creating console session for $(GO_TUI_WORKSPACE) with provider=$(GO_TUI_LLM_PROVIDER) model=$(GO_TUI_LLM_MODEL)..."; \
		session_json="$$(python3 -c 'import json,sys; d={"workspace":sys.argv[1],"profile":sys.argv[2]}; provider=sys.argv[3].strip(); model=sys.argv[4].strip(); prompt=sys.argv[5].strip(); d.update({"llm_provider":provider} if provider else {}); d.update({"llm_model":model} if model else {}); d.update({"system_prompt":prompt} if prompt else {}); print(json.dumps(d))' "$(GO_TUI_WORKSPACE)" "$(GO_TUI_PROFILE)" "$(GO_TUI_LLM_PROVIDER)" "$(GO_TUI_LLM_MODEL)" "$(GO_TUI_SYSTEM_PROMPT)")"; \
		session_id="$$(curl -sf -X POST "$(GO_TUI_API_URL)/api/console/sessions" -H 'Content-Type: application/json' -d "$$session_json" | python3 -c 'import json,sys; print(json.load(sys.stdin)["session"]["id"])')"; \
	fi; \
	if [ -n "$$agent_id" ]; then \
		echo "Launching Go TUI attached to foxctl agent $$agent_id"; \
		./bin/foxctl-tui --api-base-url "$(GO_TUI_API_URL)" --agent-id "$$agent_id" --workspace "$(GO_TUI_WORKSPACE)"; \
	else \
		echo "Launching Go TUI attached to console session $$session_id"; \
		./bin/foxctl-tui --api-base-url "$(GO_TUI_API_URL)" --console-session-id "$$session_id" --workspace "$(GO_TUI_WORKSPACE)"; \
	fi

go-tui-agent: GO_TUI_SPAWN_AGENT=1
go-tui-agent: GO_TUI_PROFILE=foxctl-agent
go-tui-agent: go-tui-spawn

go-tui: go-tui-spawn

go-tui-smoke: build go-tui-build
	@set -euo pipefail; \
	if [ -z "$(GO_TUI_SESSION_ID)" ]; then \
		echo "GO_TUI_SESSION_ID is required for go-tui-smoke. Use make go-tui-spawn to create and attach a session."; \
		exit 2; \
	fi; \
	./bin/foxctl-tui --smoke-console --api-base-url "$(GO_TUI_API_URL)" --console-session-id "$(GO_TUI_SESSION_ID)" --smoke-ask "$(GO_TUI_SMOKE_ASK)" --smoke-cancel --smoke-timeout "$(GO_TUI_SMOKE_TIMEOUT)"

# Simplified TUI: build + daemon + operator cockpit (no agents, no LLM required)
tui: build go-tui-build
	@set -euo pipefail; \
	echo "Ensuring foxctl web API at $(TUI_API_URL)..."; \
	if ! curl -sf "$(TUI_API_URL)/api/health" >/dev/null 2>&1; then \
		echo "Starting web API on :$(TUI_API_PORT) (logs: $(TUI_WEB_LOG))"; \
		FOXCTL_DB_DRIVER=$${FOXCTL_DB_DRIVER:-sqlite} FOXCTL_V2_EVENTS_DB_DRIVER=$${FOXCTL_V2_EVENTS_DB_DRIVER:-sqlite} ./bin/foxctl web serve --dev-cors --port $(TUI_API_PORT) > "$(TUI_WEB_LOG)" 2>&1 & \
		server_pid=$$!; \
		trap 'kill '"$$server_pid"' 2>/dev/null || true' EXIT; \
		for _ in $$(seq 1 40); do \
			curl -sf "$(TUI_API_URL)/api/health" >/dev/null 2>&1 && break; \
			sleep 0.25; \
		done; \
		curl -sf "$(TUI_API_URL)/api/health" >/dev/null || (echo "API health check failed; tailing $(TUI_WEB_LOG)"; tail -n 120 "$(TUI_WEB_LOG)"; exit 1); \
	else \
		server_pid=""; \
		trap ':' EXIT; \
	fi; \
	echo "Launching Go TUI ($(TUI_SCREEN) screen)..."; \
	./bin/foxctl-tui --screen $(TUI_SCREEN) --api-base-url "$(TUI_API_URL)"; \
	if [ -n "$$server_pid" ]; then \
		kill $$server_pid 2>/dev/null || true; \
	fi

# Web UI targets
web-templ:
	@command -v templ >/dev/null 2>&1 || { echo "templ not installed. Run: go install github.com/a-h/templ/cmd/templ@latest"; exit 1; }
	@templ generate ./cmd/foxctl_web/templates/

web-build: web-templ
	@$(GO_CMD) build -trimpath -o bin/foxctl-web ./cmd/foxctl_web

web-run: web-build
	@./bin/foxctl-web

# TypeScript/Bun targets
.PHONY: ts-install ts-dev-tui ts-dev-gui ts-build ts-typecheck ts-typecheck-fast ts-lint ts-lint-fix ts-check

ts-install:
	@command -v bun >/dev/null 2>&1 || { echo "bun not installed. See: https://bun.sh"; exit 1; }
	@bun install

ts-build-tui:
	@echo "The TypeScript TUI is archived under archive/packages/tui-agent."
	@echo "Use the canonical Go TUI instead: make go-tui-build"
	@exit 2

ts-dev-tui:
	@echo "The TypeScript TUI is archived under archive/packages/tui-agent."
	@echo "Use the canonical Go TUI instead: make go-tui-agent"
	@exit 2

# Starts API server + gui-agent (Vite) development workflow
ts-dev-gui: gui-agent

.PHONY: gui-agent gui-agent-dev gui-agent-build gui-agent-vite gui-smoke-seed room-board-live-e2e

GUI_API_PORT ?= 8090
GUI_VITE_PORT ?= 5174
GUI_WEB_LOG ?= /tmp/foxctl-web.log
GUI_DB_DRIVER ?= sqlite
GUI_V2_EVENTS_DB_DRIVER ?= sqlite

# Build and restart gui-agent with API server (full rebuild workflow)
gui-agent-dev: build ts-install
	@echo "Building gui-agent frontend..."
	@cd packages/gui-agent && bun run build
	@echo "Stopping any running servers..."
	@pkill -f 'foxctl web serve' 2>/dev/null || true
	@lsof -ti :$(GUI_API_PORT) | xargs kill 2>/dev/null || true
	@lsof -ti :$(GUI_VITE_PORT) | xargs kill 2>/dev/null || true

room-board-live-e2e:
	@./scripts/room_board_live_e2e.sh
	@sleep 1
	@echo "Starting web server on :$(GUI_API_PORT)..."
	@FOXCTL_DB_DRIVER=$${FOXCTL_DB_DRIVER:-$(GUI_DB_DRIVER)} FOXCTL_V2_EVENTS_DB_DRIVER=$${FOXCTL_V2_EVENTS_DB_DRIVER:-$(GUI_V2_EVENTS_DB_DRIVER)} ./bin/foxctl web serve --dev-cors --port $(GUI_API_PORT) --ui-dir packages/gui-agent/dist > $(GUI_WEB_LOG) 2>&1 &
	@sleep 2
	@echo "Web server running at http://localhost:$(GUI_API_PORT) (logs: $(GUI_WEB_LOG))"
	@curl -sf http://localhost:$(GUI_API_PORT)/api/health > /dev/null || (echo "Health check failed; tailing $(GUI_WEB_LOG)"; tail -n 80 $(GUI_WEB_LOG); exit 1)
	@curl -sf "http://localhost:$(GUI_API_PORT)/api/orchestration/board-get?request_id=make-gui-preflight" > /dev/null || (echo "Orchestration preflight failed; tailing $(GUI_WEB_LOG)"; tail -n 120 $(GUI_WEB_LOG); exit 1)
	@echo "API + orchestration preflight: OK"

# Build gui-agent frontend only (no server restart)
gui-agent-build: ts-install
	@cd packages/gui-agent && bun run build

# Build Go backend + start API server + Vite dev mode with hot reload
gui-agent: build ts-install
	@echo "Stopping any running servers..."
	@pkill -f 'foxctl web serve' 2>/dev/null || true
	@lsof -ti :$(GUI_API_PORT) | xargs kill 2>/dev/null || true
	@lsof -ti :$(GUI_VITE_PORT) | xargs kill 2>/dev/null || true
	@sleep 1
	@echo "Starting API server on :$(GUI_API_PORT)..."
	@FOXCTL_DB_DRIVER=$${FOXCTL_DB_DRIVER:-$(GUI_DB_DRIVER)} FOXCTL_V2_EVENTS_DB_DRIVER=$${FOXCTL_V2_EVENTS_DB_DRIVER:-$(GUI_V2_EVENTS_DB_DRIVER)} ./bin/foxctl web serve --dev-cors --port $(GUI_API_PORT) > $(GUI_WEB_LOG) 2>&1 &
	@sleep 2
	@curl -sf http://localhost:$(GUI_API_PORT)/api/health > /dev/null || (echo "Health check failed; tailing $(GUI_WEB_LOG)"; tail -n 80 $(GUI_WEB_LOG); exit 1)
	@curl -sf "http://localhost:$(GUI_API_PORT)/api/orchestration/board-get?request_id=make-gui-preflight" > /dev/null || (echo "Orchestration preflight failed; tailing $(GUI_WEB_LOG)"; tail -n 120 $(GUI_WEB_LOG); exit 1)
	@echo "API + orchestration preflight: OK"
	@echo "Starting Vite dev server on :$(GUI_VITE_PORT)..."
	@cd packages/gui-agent && bun run dev -- --port $(GUI_VITE_PORT)

# Run gui-agent in Vite dev mode with hot reload (no Go rebuild)
gui-agent-vite: ts-install
	@lsof -ti :$(GUI_VITE_PORT) | xargs kill 2>/dev/null || true
	@cd packages/gui-agent && bun run dev -- --port $(GUI_VITE_PORT)

gui-smoke-seed:
	@bash scripts/gui_smoke_seed.sh "$(CURDIR)"

ts-tui:
	@echo "The TypeScript TUI is archived under archive/packages/tui-agent."
	@echo "Use the canonical Go TUI instead: make go-tui-agent"
	@exit 2

ts-build: ts-install
	@bun run build

ts-typecheck: ts-install
	@bun run typecheck

# Fast TypeScript check using tsgo (10x faster than tsc)
ts-typecheck-fast: ts-install
	@bun run typecheck:fast

# Lint TypeScript packages using oxlint (fast Rust-based linter)
ts-lint: ts-install
	@bun run lint

ts-lint-fix: ts-install
	@bun run lint:fix

# Combined check: lint + typecheck (fast)
ts-check: ts-install
	@bun run check

# Build and install a single skill: make skill SKILL=todo
# This is the preferred way to rebuild a skill during development
skill:
ifndef SKILL
	$(error SKILL is required. Usage: make skill SKILL=todo)
endif
	@set -euo pipefail; \
		dir="skills/$(SKILL)"; \
		if [ ! -d "$$dir" ]; then \
			echo "Error: skill directory $$dir not found"; \
			exit 1; \
		fi; \
		echo "Building skill: $(SKILL)"; \
		outdir="dist/skills/$(SKILL)"; \
		mkdir -p "$$outdir"; \
		if ls "$$dir"/*.go >/dev/null 2>&1; then \
			$(GO_CMD) build -trimpath -ldflags="-s -w" -o "$$outdir/bin" "./$$dir"; \
		fi; \
		if [ -f "$$dir/module.wasm" ]; then \
			cp "$$dir/module.wasm" "$$outdir/module.wasm"; \
		fi; \
		if [ -f "$$dir/skill.yaml" ]; then \
			cp "$$dir/skill.yaml" "$$outdir/skill.yaml"; \
		fi; \
		echo "Installing skill: $(SKILL)"; \
		if [ -f "$$dir/skill.yaml" ] && [ -f "$$outdir/bin" ]; then \
			$(MAKE) build 2>/dev/null || true; \
			bin/$(BINARY) skills install --manifest "$$dir/skill.yaml" --binary "$$outdir/bin" --force; \
		fi; \
		echo "Done: $(SKILL)"

skills-build:
	@set -euo pipefail; \
		echo "Building skills (non-CGO)"; \
		mkdir -p dist/skills; \
		for dir in $(SKILL_DIRS); do \
			name=$$(basename "$$dir"); \
			if echo " $(CGO_SKILLS) " | grep -q " $$name "; then \
				echo " - $$name (skipped, requires CGO)"; \
				continue; \
			fi; \
			if ls "$$dir"/*.go >/dev/null 2>&1 && grep -qE '^//go:build cgo|^// +build cgo' "$$dir"/*.go; then \
				echo " - $$name (skipped, requires CGO)"; \
				continue; \
			fi; \
			outdir="dist/skills/$$name"; \
			echo " - $$name"; \
			mkdir -p "$$outdir"; \
			if ls "$$dir"/*.go >/dev/null 2>&1; then \
				$(GO_CMD) build -trimpath -ldflags="-s -w" -o "$$outdir/bin" "./$$dir"; \
			fi; \
			if [ -f "$$dir/module.wasm" ]; then \
				cp "$$dir/module.wasm" "$$outdir/module.wasm"; \
			fi; \
			if [ -f "$$dir/skill.yaml" ]; then \
				cp "$$dir/skill.yaml" "$$outdir/skill.yaml"; \
			fi; \
		done

skills-build-cgo:
	@set -euo pipefail; \
		echo "Building skills (CGO)"; \
		mkdir -p dist/skills; \
		for dir in $(SKILL_DIRS); do \
			name=$$(basename "$$dir"); \
			needs_cgo=0; \
			if echo " $(CGO_SKILLS) $(OPTIONAL_CGO_SKILLS) " | grep -q " $$name "; then \
				needs_cgo=1; \
			elif ls "$$dir"/*.go >/dev/null 2>&1 && grep -qE '^//go:build cgo|^// +build cgo' "$$dir"/*.go; then \
				needs_cgo=1; \
			fi; \
			if [ "$$needs_cgo" -ne 1 ]; then \
				echo " - $$name (skipped, no CGO variant needed)"; \
				continue; \
			fi; \
			outdir="dist/skills/$$name"; \
			mkdir -p "$$outdir"; \
			if ls "$$dir"/*.go >/dev/null 2>&1; then \
				echo " - $$name (cgo)"; \
				$(GO_CMD_CGO) build -trimpath -ldflags="-s -w" -o "$$outdir/bin-cgo" "./$$dir"; \
			fi; \
		done

skills-build-all: skills-build skills-build-cgo
	@echo "Built both non-CGO and CGO skill variants"

skills-impact:
	@$(GO_CMD) run ./scripts/skills_impact $(ARGS)

packages-impact:
	@$(GO_CMD) run ./scripts/skills_impact --mode packages $(ARGS)

BASE_REF ?=
HEAD_REF ?= HEAD

skills-build-impacted:
ifndef BASE_REF
	$(error BASE_REF is required. Usage: make skills-build-impacted BASE_REF=origin/main [HEAD_REF=HEAD])
endif
	@set -euo pipefail; \
		skills="$$( $(GO_CMD) run ./scripts/skills_impact --base-ref "$(BASE_REF)" --head-ref "$(HEAD_REF)" --format names )"; \
		if [ -z "$$skills" ]; then \
			echo "No impacted skills"; \
			exit 0; \
		fi; \
		echo "Building impacted skills: $$skills"; \
		mkdir -p dist/skills; \
		for name in $$skills; do \
			dir="skills/$$name"; \
			outdir="dist/skills/$$name"; \
			mkdir -p "$$outdir"; \
			if [ ! -d "$$dir" ]; then \
				echo "Skipping missing skill dir $$dir"; \
				continue; \
			fi; \
			if ls "$$dir"/*.go >/dev/null 2>&1; then \
				if echo " $(CGO_SKILLS) " | grep -q " $$name "; then \
					echo " - $$name (skipped in non-CGO impacted build; requires CGO)"; \
				elif grep -qE '^//go:build cgo|^// +build cgo' "$$dir"/*.go; then \
					echo " - $$name (skipped in non-CGO impacted build; requires CGO)"; \
				else \
					echo " - $$name"; \
					$(GO_CMD) build -trimpath -ldflags="-s -w" -o "$$outdir/bin" "./$$dir"; \
				fi; \
			fi; \
			if [ -f "$$dir/module.wasm" ]; then \
				cp "$$dir/module.wasm" "$$outdir/module.wasm"; \
			fi; \
			if [ -f "$$dir/skill.yaml" ]; then \
				cp "$$dir/skill.yaml" "$$outdir/skill.yaml"; \
			fi; \
		done

test-short-impacted:
ifndef BASE_REF
	$(error BASE_REF is required. Usage: make test-short-impacted BASE_REF=origin/main [HEAD_REF=HEAD])
endif
	@set -euo pipefail; \
		pkgs="$$( $(GO_CMD) run ./scripts/skills_impact --mode packages --base-ref "$(BASE_REF)" --head-ref "$(HEAD_REF)" --format names )"; \
		pkgs="$$( echo "$$pkgs" | tr ' ' '\n' | grep -vx 'github.com/joshka0/foxctl/test/integration' | xargs )"; \
		if [ -z "$$pkgs" ]; then \
			echo "No impacted packages"; \
			exit 0; \
		fi; \
		echo "Testing impacted packages: $$pkgs"; \
		$(GO_CMD) test -short -v -count=1 -timeout 20m $$pkgs

skills-install: skills-build-all build
	@set -euo pipefail; \
		echo "Installing skills"; \
		for dir in $(SKILL_DIRS); do \
			name=$$(basename "$$dir"); \
			manifest="$$dir/skill.yaml"; \
			binary="dist/skills/$$name/bin"; \
			binaryCgo="dist/skills/$$name/bin-cgo"; \
			if [ -f "$$manifest" ] && [ -f "$$binary" ]; then \
				echo " - $$name"; \
				bin/$(BINARY) skills install --manifest "$$manifest" --binary "$$binary" --force 2>&1 | grep -E '(installed|error|failed)' || true; \
			elif [ -f "$$manifest" ] && [ -f "$$binaryCgo" ]; then \
				echo " - $$name (cgo-only)"; \
				bin/$(BINARY) skills install --manifest "$$manifest" --binary "$$binaryCgo" --force 2>&1 | grep -E '(installed|error|failed)' || true; \
			elif [ -f "$$manifest" ]; then \
				echo " - $$name (no binary, skip)"; \
			fi; \
			if [ -f "$$manifest" ] && [ -f "$$binaryCgo" ]; then \
				skillName=$$(grep -E '^  name:' "$$manifest" | head -1 | sed 's/.*name: *//'); \
				destDir="$${HOME}/.foxctl/skills/$$skillName"; \
				if [ -d "$$destDir" ]; then \
					cp "$$binaryCgo" "$$destDir/bin-cgo"; \
				fi; \
			fi; \
		done; \
		echo "Done. Run 'bin/foxctl skills list' to verify."

# Install CGO skill binaries alongside non-CGO ones
skills-install-cgo: skills-build-cgo
	@set -euo pipefail; \
		echo "Installing CGO skill binaries"; \
		for dir in $(SKILL_DIRS); do \
			name=$$(basename "$$dir"); \
			manifest="$$dir/skill.yaml"; \
			binaryCgo="dist/skills/$$name/bin-cgo"; \
			if [ -f "$$manifest" ] && [ -f "$$binaryCgo" ]; then \
				skillName=$$(grep -E '^  name:' "$$manifest" | head -1 | sed 's/.*name: *//'); \
				destDir="$${HOME}/.foxctl/skills/$$skillName"; \
				if [ -d "$$destDir" ]; then \
					echo " - $$skillName (cgo)"; \
					cp "$$binaryCgo" "$$destDir/bin-cgo"; \
				fi; \
			fi; \
		done; \
		echo "Done."

skills-install-all: skills-install skills-install-cgo
	@echo "Installed both non-CGO and CGO skill variants"

skills-test:
	@for dir in $(SKILL_DIRS); do \
		if ls $$dir/*.go >/dev/null 2>&1; then \
			$(GO_CMD) test ./$$dir; \
		fi; \
	done

snapshot:
	@command -v goreleaser >/dev/null || (echo "goreleaser not installed" && exit 1)
	@goreleaser release --snapshot --clean

tidy:
	@$(GO_CMD) mod tidy

check-doc-links:
	@bash scripts/check_doc_links.sh

check-large-files:
	@large_file_base=""; \
	if [ -n "$$CI_MERGE_REQUEST_DIFF_BASE_SHA" ] && git cat-file -e "$$CI_MERGE_REQUEST_DIFF_BASE_SHA^{commit}" >/dev/null 2>&1; then \
		large_file_base="$$CI_MERGE_REQUEST_DIFF_BASE_SHA"; \
	elif [ -n "$$CI_MERGE_REQUEST_TARGET_BRANCH_NAME" ] && git rev-parse --verify "origin/$$CI_MERGE_REQUEST_TARGET_BRANCH_NAME" >/dev/null 2>&1; then \
		large_file_base="$$(git merge-base HEAD "origin/$$CI_MERGE_REQUEST_TARGET_BRANCH_NAME")"; \
	elif [ -n "$$CI_DEFAULT_BRANCH" ] && git rev-parse --verify "origin/$$CI_DEFAULT_BRANCH" >/dev/null 2>&1; then \
		large_file_base="$$(git merge-base HEAD "origin/$$CI_DEFAULT_BRANCH")"; \
	elif git rev-parse --verify origin/main >/dev/null 2>&1; then \
		head_ref="$$(git rev-parse HEAD 2>/dev/null || true)"; \
		main_ref="$$(git rev-parse origin/main 2>/dev/null || true)"; \
		if [ -n "$$head_ref" ] && [ -n "$$main_ref" ] && [ "$$head_ref" != "$$main_ref" ]; then \
			large_file_base="$$(git merge-base HEAD origin/main)"; \
		fi; \
	fi; \
	if [ -n "$$large_file_base" ]; then \
		echo "Checking newly added large files from $$large_file_base"; \
	fi; \
	CHECK_LARGE_FILES_BASE_REF="$$large_file_base" bash scripts/check_large_files.sh

check-tech-debt:
	@bash scripts/check_tech_debt.sh

check-duplication:
	@command -v jscpd >/dev/null 2>&1 || { echo "jscpd not installed. Run: npm i -g jscpd"; exit 1; }
	@jscpd --config .jscpd.json .

test-timing:
	@$(GO_CMD) test ./... -v -count=1 2>&1 | grep -E '(^--- |PASS|FAIL|panic)' | awk '{print $$0}'

check: fmt lint vet test check-coverage check-doc-links check-large-files check-tech-debt build

completions: build
	@mkdir -p dist
	@bin/$(BINARY) completion bash > dist/completion.bash
	@bin/$(BINARY) completion zsh > dist/_foxctl

# Environment file sync (repo → ~/.foxctl)
# Use real files, not symlinks, for sandbox/remote compatibility
.PHONY: env-sync env-watch env-watch-stop

env-sync:
	@if [ -f .env ]; then \
		mkdir -p ~/.foxctl; \
		cp .env ~/.foxctl/.env; \
		echo "Synced .env → ~/.foxctl/.env"; \
	else \
		echo "No .env file found in repo root"; \
		exit 1; \
	fi

# Watch .env and auto-sync on changes (requires fswatch: brew install fswatch)
ENV_WATCH_PID := /tmp/foxctl-env-watch.pid
env-watch:
	@command -v fswatch >/dev/null 2>&1 || { echo "fswatch not installed. Run: brew install fswatch"; exit 1; }
	@if [ -f $(ENV_WATCH_PID) ] && kill -0 $$(cat $(ENV_WATCH_PID)) 2>/dev/null; then \
		echo "env-watch already running (PID $$(cat $(ENV_WATCH_PID)))"; \
	else \
		echo "Starting env-watch..."; \
		nohup bash -c 'fswatch -o "$(CURDIR)/.env" | while read; do cp "$(CURDIR)/.env" ~/.foxctl/.env && echo "[env-watch] synced .env"; done' \
			> /tmp/foxctl-env-watch.log 2>&1 & echo $$! > $(ENV_WATCH_PID); \
		sleep 0.5; \
		echo "env-watch started (PID $$(cat $(ENV_WATCH_PID))), log: /tmp/foxctl-env-watch.log"; \
	fi

env-watch-stop:
	@if [ -f $(ENV_WATCH_PID) ]; then \
		PID=$$(cat $(ENV_WATCH_PID)); \
		pkill -P $$PID 2>/dev/null; \
		kill $$PID 2>/dev/null && echo "env-watch stopped (PID $$PID)" || echo "env-watch not running"; \
		rm -f $(ENV_WATCH_PID); \
	else \
		echo "env-watch not running"; \
	fi

# Database backup (daily backups, keeps last 2)
.PHONY: db-backup db-backup-list db-backup-clean

db-backup:
	@BACKUP_DIR="$$HOME/.foxctl/backups"; \
	STORAGE_DIR="$$HOME/.foxctl/storage"; \
	mkdir -p "$$BACKUP_DIR"; \
	TIMESTAMP=$$(date +%Y%m%d_%H%M%S); \
	BACKUP_PATH="$$BACKUP_DIR/$$TIMESTAMP"; \
	mkdir -p "$$BACKUP_PATH"; \
	echo "Creating backup at $$BACKUP_PATH..."; \
	count=0; \
	for db in "$$STORAGE_DIR"/*.db; do \
		if [ -f "$$db" ] && [ -s "$$db" ]; then \
			dbname=$$(basename "$$db"); \
			sqlite3 "$$db" ".backup '$$BACKUP_PATH/$${dbname}.bak'" 2>/dev/null || \
			cp "$$db" "$$BACKUP_PATH/$${dbname}.bak"; \
			count=$$((count + 1)); \
		fi; \
	done; \
	echo "Backed up $$count databases to: $$BACKUP_PATH"

db-backup-list:
	`@BACKUP_DIR`="$$HOME/.foxctl/backups"; \
	echo "Available backups:"; \
	backups=$$(ls -dt "$$BACKUP_DIR"/2* 2>/dev/null | head -10); \
	if [ -z "$$backups" ]; then \
		echo "  (no backups found)"; \
	else \
		echo "$$backups" | while read dir; do \

			files=$$(ls "$$dir"/*.bak 2>/dev/null | wc -l | tr -d ' '); \
			size=$$(du -sh "$$dir" 2>/dev/null | cut -f1); \
			echo "  $$(basename $$dir)  $$size  ($$files DBs)"; \
		done; \
	fi

db-backup-clean:
	@BACKUP_DIR="$$HOME/.foxctl/backups"; \
	echo "Keeping 2 most recent backups, removing older ones..."; \
	ls -dt "$$BACKUP_DIR"/2* 2>/dev/null | tail -n +3 | while read dir; do \
		echo "  Removing: $$(basename $$dir)"; \
		rm -rf "$$dir"; \
	done; \
	echo "Cleanup complete"
