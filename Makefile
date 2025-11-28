SHELL := /bin/bash
unexport GOROOT
unexport GOBIN
unexport GOTOOLDIR
GO ?= go
GO_CMD := env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=0 $(GO)
GO_CMD_CGO := env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=1 $(GO)
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
	@env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=1 $(GO) test -race ./...

cover:
	@mkdir -p coverage
	@$(GO_CMD) test ./... -covermode=atomic -coverprofile=coverage/coverage.out
	@$(GO_CMD) tool cover -func=coverage/coverage.out

check-coverage:
	@echo "Checking test coverage..."
	@mkdir -p coverage
	@$(GO_CMD) test ./... -coverprofile=coverage/coverage.out -covermode=atomic 2>&1 | grep -v "no test files" || true
	@$(GO_CMD) tool cover -func=coverage/coverage.out | tee coverage/coverage.txt
	@awk '/^total:/ {gsub("%",""); if ($$3 < 40.0) { \
		print "❌ Coverage " $$3 "% is below 40% threshold"; exit 1; } \
		else { print "✅ Coverage " $$3 "% meets threshold"; exit 0; }}' \
		coverage/coverage.txt

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
