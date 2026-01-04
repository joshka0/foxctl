SHELL := /bin/bash
unexport GOROOT
unexport GOBIN
unexport GOTOOLDIR
GO ?= go
GO_CMD := env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=0 $(GO)
GO_CMD_CGO := env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=1 $(GO)

# RACE_PKGS includes all packages except internal/storage/vector.
# The vector package links github.com/mattn/go-sqlite3 for sqlite-vector support,
# which conflicts with github.com/tursodatabase/go-libsql's embedded SQLite
# symbols (both define sqlite3_data_directory). See AGENTS.md "Testing Requirements"
# for details and manual vector test commands.
RACE_PKGS := $(shell $(GO_CMD_CGO) list ./... | grep -v 'github.com/jkatigb/agentctl/internal/storage/vector')
BINARY ?= agentctl
GOFUMPT ?= gofumpt
GOLANGCI ?= golangci-lint
GOFILES := $(shell find cmd internal skills -name '*.go')
SKILL_DIRS := $(shell find skills -mindepth 1 -maxdepth 1 -type d)

.PHONY: fmt lint typecheck lsp-check vet test test-cgo test-cgo-short test-race test-integration test-integration-cmd cover check-coverage build build-cgo build-all viewer snapshot tidy check skill skills-build skills-build-cgo skills-build-all skills-install skills-install-cgo skills-install-all skills-test completions init ts-install ts-dev-tui ts-dev-gui ts-build-tui ts-tui ts-build ts-typecheck

fmt:
	@echo "Running gofumpt"
	@$(GOFUMPT) -w $(GOFILES)

lint:
	@echo "Running golangci-lint"
	@GOFLAGS=-buildvcs=false $(GOLANGCI) run ./...

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
	@$(GO_CMD) test ./...

test-short:
	@$(GO_CMD) test -short ./...

test-cgo:
	@$(GO_CMD_CGO) test -tags=libsqlite3 ./...

test-cgo-short:
	@$(GO_CMD_CGO) test -short -tags=libsqlite3 ./...

test-race:
	@$(GO_CMD_CGO) test -race -short $(RACE_PKGS)

test-integration:
	@$(GO_CMD) test -tags=integration ./test/integration/... -timeout 15m -v

test-integration-cmd:
	@$(GO_CMD) test -tags=integration ./cmd/agentctl/cmd/... -timeout 15m -v

cover:
	@mkdir -p coverage
	@$(GO_CMD) test ./... -covermode=atomic -coverprofile=coverage/coverage.out
	@$(GO_CMD) tool cover -func=coverage/coverage.out

# Coverage thresholds (see AGENTS.md):
# - Line coverage:    85%
# - Function coverage: 80%
# - Branch coverage:   75% (approximated from line coverage due to Go tool limitations)
check-coverage:
	@echo "Checking test coverage (line/function/branch)..."
	@mkdir -p coverage
	@$(GO_CMD) test ./... -coverprofile=coverage/coverage.out -covermode=atomic 2>&1 | grep -v "no test files" || true
	@$(GO_CMD) tool cover -func=coverage/coverage.out | tee coverage/coverage.txt
	@echo ""
	@echo "=== Coverage Summary ==="
	@awk '
		/^total:/ {
			gsub("%","",$$3);
			line = $$3;
		}
		/^total:/ {
			# For now, function and branch coverage use the same total metric;
			# this can be refined if more detailed tooling is added.
			func = line;
			branch = line;
		}
		END {
			status = 0;
			if (line < 85.0) {
				print "❌ Line coverage", line "% is below 85% threshold";
				status = 1;
			} else {
				print "✅ Line coverage", line "% meets 85% threshold";
			}
			if (func < 80.0) {
				print "❌ Function coverage", func "% is below 80% threshold";
				status = 1;
			} else {
				print "✅ Function coverage", func "% meets 80% threshold";
			}
			if (branch < 75.0) {
				print "❌ Branch coverage", branch "% is below 75% threshold";
				status = 1;
			} else {
				print "✅ Branch coverage", branch "% meets 75% threshold";
			}
			exit status;
		}
	' coverage/coverage.txt

build:
	@set -euo pipefail; \
	VERSION=$$(git describe --tags --always --dirty 2>/dev/null || echo dev); \
	COMMIT=$$(git rev-parse --short HEAD 2>/dev/null || echo unknown); \
	DATE=$$(date -u +%Y-%m-%dT%H:%M:%SZ); \
	$(GO_CMD) build -trimpath \
		-ldflags="-s -w \
		-X github.com/jkatigb/agentctl/internal/platform/buildinfo.Version=$$VERSION \
		-X github.com/jkatigb/agentctl/internal/platform/buildinfo.Commit=$$COMMIT \
		-X github.com/jkatigb/agentctl/internal/platform/buildinfo.Date=$$DATE" \
		-o bin/$(BINARY) ./cmd/agentctl
	@$(GO_CMD) build -trimpath -ldflags="-s -w" -o bin/agentctl-mail ./cmd/agentctl-mail

build-cgo:
	@set -euo pipefail; \
	VERSION=$$(git describe --tags --always --dirty 2>/dev/null || echo dev); \
	COMMIT=$$(git rev-parse --short HEAD 2>/dev/null || echo unknown); \
	DATE=$$(date -u +%Y-%m-%dT%H:%M:%SZ); \
	$(GO_CMD_CGO) build -tags=libsqlite3 -trimpath \
		-ldflags="-s -w \
		-X github.com/jkatigb/agentctl/internal/platform/buildinfo.Version=$$VERSION \
		-X github.com/jkatigb/agentctl/internal/platform/buildinfo.Commit=$$COMMIT \
		-X github.com/jkatigb/agentctl/internal/platform/buildinfo.Date=$$DATE" \
		-o bin/$(BINARY)-cgo ./cmd/agentctl

build-all: build build-cgo
	@echo "Built bin/$(BINARY) and bin/$(BINARY)-cgo"

init: build-all skills-install-all
	@./scripts/init.sh

viewer:
	@$(GO_CMD) build -trimpath -o bin/agentctl-viewer ./cmd/agentctl_viewer

install-mail:
	@./scripts/install-mail.sh

# Web UI targets
web-templ:
	@command -v templ >/dev/null 2>&1 || { echo "templ not installed. Run: go install github.com/a-h/templ/cmd/templ@latest"; exit 1; }
	@templ generate ./cmd/agentctl_web/templates/

web-build: web-templ
	@$(GO_CMD) build -trimpath -o bin/agentctl-web ./cmd/agentctl_web

web-run: web-build
	@./bin/agentctl-web

# TypeScript/Bun targets
.PHONY: ts-install ts-dev-tui ts-dev-gui ts-build ts-typecheck ts-typecheck-fast ts-lint ts-lint-fix ts-check

ts-install:
	@command -v bun >/dev/null 2>&1 || { echo "bun not installed. See: https://bun.sh"; exit 1; }
	@bun install

# Build the standalone TUI binary
ts-build-tui: ts-install
	@mkdir -p bin
	@bun build --compile --minify packages/tui/src/index.tsx --outfile bin/agentctl-tui
	@echo "Built bin/agentctl-tui"

ts-dev-tui: ts-install
	@cd packages/tui && bun run dev

# Starts both API server and GUI (dev:all = server + vite)
ts-dev-gui: ts-install
	@cd packages/gui && bun run dev:all

# Runs both API server and TUI binary together
ts-tui: ts-build-tui
	@echo "Starting API server and TUI..."
	@bun run --cwd packages/gui server > /dev/null 2>&1 & \
	SERVER_PID=$$!; \
	trap "kill $$SERVER_PID 2>/dev/null || true" EXIT; \
	AGENTCTL_API_URL=http://localhost:8090 ./bin/agentctl-tui

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
			$(GO_CMD) build -o "$$outdir/bin" "./$$dir"; \
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
			outdir="dist/skills/$$name"; \
			echo " - $$name"; \
			mkdir -p "$$outdir"; \
			if ls "$$dir"/*.go >/dev/null 2>&1; then \
				$(GO_CMD) build -o "$$outdir/bin" "./$$dir"; \
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
			outdir="dist/skills/$$name"; \
			mkdir -p "$$outdir"; \
			if ls "$$dir"/*.go >/dev/null 2>&1; then \
				echo " - $$name (cgo)"; \
				$(GO_CMD_CGO) build -tags=libsqlite3 -o "$$outdir/bin-cgo" "./$$dir"; \
			fi; \
		done

skills-build-all: skills-build skills-build-cgo
	@echo "Built both non-CGO and CGO skill variants"

skills-install: skills-build build
	@set -euo pipefail; \
		echo "Installing skills"; \
		for dir in $(SKILL_DIRS); do \
			name=$$(basename "$$dir"); \
			manifest="$$dir/skill.yaml"; \
			binary="dist/skills/$$name/bin"; \
			if [ -f "$$manifest" ] && [ -f "$$binary" ]; then \
				echo " - $$name"; \
				bin/$(BINARY) skills install --manifest "$$manifest" --binary "$$binary" --force 2>&1 | grep -E '(installed|error|failed)' || true; \
			elif [ -f "$$manifest" ]; then \
				echo " - $$name (no binary, skip)"; \
			fi; \
		done; \
		echo "Done. Run 'bin/agentctl skills list' to verify."

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
				destDir="$${HOME}/.agentctl/skills/$$skillName"; \
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

check: fmt lint vet test check-coverage build

completions: build
	@mkdir -p dist
	@bin/$(BINARY) completion bash > dist/completion.bash
	@bin/$(BINARY) completion zsh > dist/_agentctl
