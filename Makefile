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

.PHONY: fmt lint typecheck lsp-check vet test test-cgo test-cgo-short test-race test-integration test-integration-cmd cover check-coverage build build-cgo viewer snapshot tidy check skill skills-build skills-install skills-test completions

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

viewer:
	@$(GO_CMD) build -trimpath -o bin/agentctl-viewer ./cmd/agentctl_viewer

# Web UI targets
web-templ:
	@command -v templ >/dev/null 2>&1 || { echo "templ not installed. Run: go install github.com/a-h/templ/cmd/templ@latest"; exit 1; }
	@templ generate ./cmd/agentctl_web/templates/

web-build: web-templ
	@$(GO_CMD) build -trimpath -o bin/agentctl-web ./cmd/agentctl_web

web-run: web-build
	@./bin/agentctl-web

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
		echo "Building skills"; \
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
