SHELL := /bin/bash
unexport GOROOT
unexport GOBIN
unexport GOTOOLDIR
GO ?= go
GO_CMD := env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=0 $(GO)
GO_CMD_CGO := env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=1 $(GO)

# RACE_PKGS currently includes all packages. If you need to exclude any package
# from race testing, document the rationale here and add an entry to AGENTS.md.
RACE_PKGS := $(shell $(GO_CMD_CGO) list ./...)
BINARY ?= agentctl
GOFUMPT ?= gofumpt
GOLANGCI ?= golangci-lint
GOFILES := $(shell find cmd internal skills -name '*.go')
SKILL_DIRS := $(shell find skills -mindepth 1 -maxdepth 1 -type d)

.PHONY: fmt lint vet test test-race cover check-coverage build snapshot tidy check skills-build skills-test completions

fmt:
	@echo "Running gofumpt"
	@$(GOFUMPT) -w $(GOFILES)

lint:
	@echo "Running golangci-lint"
	@GOFLAGS=-buildvcs=false $(GOLANGCI) run ./...

vet:
	@$(GO_CMD) vet ./...

test:
	@$(GO_CMD) test ./...

test-short:
	@$(GO_CMD) test -short ./...

test-race:
	@$(GO_CMD_CGO) test -race $(RACE_PKGS)

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

skills-build:
	@mkdir -p dist/skills
	@for dir in $(SKILL_DIRS); do \
		name=$$(basename $$dir); \
		outdir=dist/skills/$$name; \
		mkdir -p $$outdir; \
		if ls $$dir/*.go >/dev/null 2>&1; then \
			$(GO_CMD) build -o $$outdir/bin ./$$dir; \
		fi; \
		if [ -f $$dir/module.wasm ]; then \
			cp $$dir/module.wasm $$outdir/module.wasm; \
		fi; \
		if [ -f $$dir/skill.yaml ]; then \
			cp $$dir/skill.yaml $$outdir/skill.yaml; \
		fi; \
	done

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
