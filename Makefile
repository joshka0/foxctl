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

.PHONY: fmt lint test test-race build snapshot tidy check skills-build skills-test

fmt:
	@echo "Running gofumpt"
	@$(GOFUMPT) -w $(GOFILES)

lint:
	@echo "Running golangci-lint"
	@$(GOLANGCI) run ./...

test:
	@$(GO_CMD) test ./...

test-race:
	@$(GO_CMD) test -race ./...

build:
	@$(GO_CMD) build -o bin/$(BINARY) ./cmd/agentctl

skills-build:
	@for dir in $(SKILL_DIRS); do \
		if ls $$dir/*.go >/dev/null 2>&1; then \
			name=$$(basename $$dir); \
			out=dist/skills/$$name/$$name; \
			mkdir -p $$(dirname $$out); \
			$(GO_CMD) build -o $$out ./$$dir; \
		fi; \
	done

skills-test:
	@$(GO_CMD) test ./skills/...

snapshot:
	@command -v goreleaser >/dev/null || (echo "goreleaser not installed" && exit 1)
	@goreleaser release --snapshot --clean

tidy:
	@$(GO_CMD) mod tidy

check: fmt lint test build
