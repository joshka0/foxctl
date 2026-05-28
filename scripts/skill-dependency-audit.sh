#!/usr/bin/env bash
# Convenience wrapper for the Go-based skill dependency audit.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

GO_BIN="${GO:-}"
if [ -z "$GO_BIN" ]; then
	if command -v go >/dev/null 2>&1; then
		GO_BIN="$(command -v go)"
	elif [ -x /usr/local/go/bin/go ]; then
		GO_BIN=/usr/local/go/bin/go
	else
		GO_BIN=go
	fi
fi

env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED="${CGO_ENABLED:-0}" \
	"$GO_BIN" run ./scripts/skill_dependency_audit "$@"
