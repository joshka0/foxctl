SHELL := /bin/bash
unexport GOROOT
unexport GOBIN
unexport GOTOOLDIR
GO ?= go
GO_CMD := env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=0 $(GO)
BINARY ?= agentctl
GOFUMPT ?= $(GO_CMD) run mvdan.cc/gofumpt@v0.6.0
GOLANGCI ?= $(GO_CMD) run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.58.2
GOFILES := $(shell find cmd internal skills -name '*.go')
SKILL_DIRS := $(shell find skills -mindepth 1 -maxdepth 1 -type d)

.PHONY: fmt lint vet test test-race cover build snapshot tidy check skills-build skills-test completions

fmt:
	@echo "Running gofumpt"
	@$(GOFUMPT) -w $(GOFILES)

lint:
	@echo "Running golangci-lint"
	@$(GOLANGCI) run ./...

vet:
	@$(GO_CMD) vet ./...

test:
	@$(GO_CMD) test ./...

test-race:
	@env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=1 $(GO) test -race ./...

cover:
	@mkdir -p coverage
	@$(GO_CMD) test ./... -covermode=atomic -coverprofile=coverage/coverage.out
	@$(GO_CMD) tool cover -func=coverage/coverage.out

build:
	@set -euo pipefail; \
	VERSION=$$(git describe --tags --always --dirty 2>/dev/null || echo dev); \
	COMMIT=$$(git rev-parse --short HEAD 2>/dev/null || echo unknown); \
	DATE=$$(date -u +%Y-%m-%dT%H:%M:%SZ); \
	$(GO_CMD) build -trimpath \
		-ldflags="-s -w \
		-X github.com/jkatigb/agentctl/internal/buildinfo.Version=$$VERSION \
		-X github.com/jkatigb/agentctl/internal/buildinfo.Commit=$$COMMIT \
		-X github.com/jkatigb/agentctl/internal/buildinfo.Date=$$DATE" \
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

check: fmt lint vet test build

completions: build
	@mkdir -p dist
	@bin/$(BINARY) completion bash > dist/completion.bash
	@bin/$(BINARY) completion zsh > dist/_agentctl
