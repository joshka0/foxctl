#!/usr/bin/env bash
set -euo pipefail

if ! command -v wrangler >/dev/null 2>&1; then
  echo "Missing wrangler CLI. Install or run via a checked package manager before deploying." >&2
  exit 1
fi

export WRANGLER_LOG_PATH="${WRANGLER_LOG_PATH:-/private/tmp/wrangler-foxctl-logs}"
mkdir -p "${WRANGLER_LOG_PATH}"

export CLOUDFLARE_API_TOKEN="${CLOUDFLARE_API_TOKEN:-${CF_API_TOKEN:-${TF_VAR_cloudflare_api_token:-}}}"
export CLOUDFLARE_ACCOUNT_ID="${CLOUDFLARE_ACCOUNT_ID:-${CF_ACCOUNT_ID:-${TF_VAR_cloudflare_account_id:-}}}"

if [[ -z "${CLOUDFLARE_API_TOKEN}" || -z "${CLOUDFLARE_ACCOUNT_ID}" ]]; then
  echo "Missing secret requirements:" >&2
  if [[ -z "${CLOUDFLARE_API_TOKEN}" ]]; then
    echo "- CLOUDFLARE_API_TOKEN: needed by wrangler pages deploy" >&2
  fi
  if [[ -z "${CLOUDFLARE_ACCOUNT_ID}" ]]; then
    echo "- CLOUDFLARE_ACCOUNT_ID: needed by wrangler pages deploy" >&2
  fi
  exit 1
fi

branch="${CLOUDFLARE_PAGES_BRANCH:-main}"

bun run build:docs
(
  cd packages/docs-site
  wrangler pages deploy --branch "${branch}"
)
