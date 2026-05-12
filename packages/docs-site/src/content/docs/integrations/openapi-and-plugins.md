---
title: OpenAPI and Plugins
description: Call any OpenAPI 3.x API with the http/openapi skill, configure auth strategies, pagination, retries, and extend with out-of-process plugins.
---

The `http/openapi` skill is foxctl's generic HTTP client for calling any
OpenAPI 3.x REST API without code generation. It handles authentication,
pagination, retries, and large response management automatically.

For APIs that need custom auth or pagination, foxctl supports out-of-process
plugins that communicate via Protocol v1 JSON envelopes.

This page covers the OpenAPI skill path, spec management, built-in auth
strategies, pagination, the plugin architecture, and error handling.

## The `http/openapi` Skill

### Overview

Instead of generating skill stubs for each API endpoint, the `http/openapi`
skill reads an OpenAPI specification at runtime and invokes any operation by
`operationId`. This single skill can call any OpenAPI 3.x API.

### Key Capabilities

- **Zero-codegen API access** — call any operation defined in an OpenAPI 3.x spec
- **Spec-as-memory** — load specs from files, CAS artifacts, or named memories
- **Built-in auth** — Bearer, API Key, Basic Auth, OAuth2 Client Credentials
- **Automatic pagination** — Link headers, cursor-based, offset/limit
- **Smart retries** — exponential backoff with jitter for 429/5xx errors
- **CAS integration** — large responses stored as artifacts with inline summaries
- **Dry-run support** — preview requests without making API calls
- **Plugin extensibility** — custom auth and pagination via subprocess plugins

### Skill Input

The skill accepts these input fields:

| Field | Required | Description |
|-------|----------|-------------|
| `spec` | Yes | Spec location: file path, `sha256:<hex>`, or `memory:<name>` |
| `operationId` | Yes | Target operation ID from the OpenAPI doc |
| `params` | No | Parameters organized by location: `path`, `query`, `header`, `body` |
| `auth` | No | Auth configuration: scheme selection and credential overrides |
| `paging` | No | Pagination configuration: strategy, limits, field mappings |
| `retry` | No | Retry configuration: backoff timing and max attempts |
| `dry_run` | No | If true, return request plan without executing |

Example input:

```json
{
  "spec": "memory:github",
  "operationId": "listReposForUser",
  "params": {
    "path": { "username": "octocat" },
    "query": { "per_page": 100, "sort": "updated" }
  },
  "paging": { "strategy": "auto", "max_pages": 10 }
}
```

### Skill Output

Successful responses follow the Protocol v1 envelope:

- **Small responses** (< 32KB): body inline in `data.body`
- **Large responses** (≥ 32KB): stored in CAS with `data.artifact` pointer and
  `data.summary` containing a preview
- **Error responses**: inline error details with `data.hint` for remediation

## Spec Management

### Importing Specs

Store OpenAPI specifications as named memories for easy reference:

```bash
# From local file
foxctl openapi import ./github-api.yaml --as=github

# From URL
foxctl openapi import https://api.github.com/openapi.json --as=github

# With strict validation
foxctl openapi import ./spec.yaml --as=myapi --strict
```

Importing validates the spec, stores it in CAS, and creates a named memory
entry (`memory:github` → CAS digest).

### Listing Imported Specs

```bash
foxctl memory list --type=openapi_spec
```

### Validating Specs

```bash
# Basic validation
foxctl openapi validate memory:github

# Strict validation (all refs resolved, no warnings)
foxctl openapi validate memory:github --strict
```

### Describing Specs

List all operations available in a spec:

```bash
foxctl openapi describe memory:github
```

Filter by tag:

```bash
foxctl openapi describe memory:github --tag=issues
```

### Spec Caching

Specs are cached in memory for 24 hours (configurable). Cache is invalidated on
re-import or TTL expiry. To force a refresh, re-import the spec:

```bash
foxctl openapi import ./github-api.yaml --as=github
```

## Auth Strategies

### Built-in Schemes

| Scheme | HTTP Header | Setup |
|--------|------------|-------|
| **Bearer** | `Authorization: Bearer <token>` | `FOXCTL_BEARER_TOKEN` env var |
| **API Key** | Configurable header or query param | `FOXCTL_API_KEY` env var |
| **Basic Auth** | `Authorization: Basic base64(user:pass)` | `FOXCTL_BASIC_AUTH` env var |
| **OAuth2 Client Credentials** | Token exchange → Bearer | `FOXCTL_OAUTH2_CLIENT_ID` + `FOXCTL_OAUTH2_CLIENT_SECRET` |

Auth is auto-detected from the OpenAPI spec's `securitySchemes` definition. You
can override explicitly:

```json
{
  "auth": {
    "scheme": "bearer",
    "credentials": { "token": "ghp_..." }
  }
}
```

### Secret Sources

Credentials are loaded from, in order of preference:

1. Files under `/run/secrets/<name>` (container deployments)
2. Environment variables (`FOXCTL_*` or scheme-specific names)
3. Direct `auth.credentials` (testing only — not recommended for production)

### Security Rules

- All secrets are redacted as `"***"` in logs, error messages, and dry-run output
- Never hardcode credentials in scripts or config files
- Prefer environment variables or secret mounts over inline credentials

## Pagination

### Built-in Strategies

The skill auto-detects pagination strategy when `paging.strategy` is `"auto"`
(default). It tries strategies in order:

| Strategy | How it works | Used by |
|----------|-------------|---------|
| **Link headers** | RFC 5988 `Link: <url>; rel="next"` | GitHub, GitLab |
| **Cursor-based** | Body fields like `next_page_token`, `cursor` | Stripe, Google, Slack |
| **Offset/limit** | Classic `page`/`per_page` or `offset`/`limit` params | Traditional REST APIs |
| **Total-count heuristic** | Stops when items < page size | Fallback |

### Configuring Pagination

```json
{
  "paging": {
    "strategy": "cursor",
    "cursor_field": "pagination.next_token",
    "max_items": 1000,
    "max_pages": 10
  }
}
```

Disable pagination (fetch one page only):

```json
{ "paging": { "max_pages": 1 } }
```

### Response Metadata

All paginated responses include pagination metadata:

```json
{
  "pagination": {
    "has_more": false,
    "total_pages": 3,
    "total_items": 247,
    "strategy_used": "link"
  }
}
```

## Retry and Error Handling

### Retry Behavior

| Setting | Default |
|---------|---------|
| Retry codes | 429, 502, 503, 504 |
| Strategy | Exponential backoff with jitter |
| Max attempts | 5 (including initial request) |
| Backoff schedule | 250ms → 500ms → 1s → 2s → 4s (±20% jitter) |

The `Retry-After` header is always respected when the server provides it.

### Error Codes

| Code | HTTP | Meaning | Fix |
|------|------|---------|-----|
| `EOPENAPI` | — | Spec error | Validate spec, check operationId |
| `EARG` | 400, 422 | Invalid arguments | Check params, verify types |
| `EAUTH` | 401 | Auth failed | Verify credentials, check expiry |
| `EPAGINATION` | — | Pagination failure | Specify strategy manually |
| `ERATELIMIT` | 429 | Rate limit exceeded | Wait for reset, reduce rate |
| `ERUNTIME` | 5xx | Server error | Retry later, check API status |
| `ENOTFOUND` | 404 | Resource not found | Verify spec/memory name |
| `ETIMEOUT` | — | Request timeout | Increase timeout, check network |

Error responses include `data.hint` with actionable remediation. 4xx errors are
kept inline (not artifactized). 5xx errors after retries include
`error.code="ERUNTIME"` with any available `request-id`.

### Custom Retry Config

```json
{
  "retry": {
    "base_ms": 500,
    "factor": 2.0,
    "max_attempts": 3,
    "max_ms": 5000,
    "retry_codes": [429, 503]
  }
}
```

## Dry-Run Mode

Preview and validate requests without making actual API calls:

```json
{
  "spec": "memory:github",
  "operationId": "listReposForUser",
  "params": { "path": { "username": "octocat" } },
  "dry_run": true
}
```

Returns a request plan showing method, URL, headers (with secrets redacted),
body, pagination config, and retry config. Useful for debugging and validation.

## Plugin Architecture

For APIs that need custom auth or pagination behavior, `http/openapi` supports
out-of-process plugins. Plugins run as separate binaries and communicate via
JSON envelopes on stdin/stdout.

### Plugin Types

| Type | Command | Purpose |
|------|---------|---------|
| **Auth** | `plugin/auth` | Sign requests, inject headers/body |
| **Pagination** | `plugin/pagination` | Determine next page from response |

### Discovery

Plugins are found via:

1. `FOXCTL_PLUGIN_PATH` environment variable (colon-separated paths)
2. `openapi.plugin_path` config setting
3. Default: `~/.foxctl/plugins`

Plugin naming convention: `foxctl-plugin-<name>` (e.g.,
`foxctl-plugin-aws-sigv4`).

### Using a Plugin

Reference plugins by name in the auth config:

```json
{
  "auth": { "scheme": "plugin:aws-sigv4" }
}
```

Or via spec hints:

```yaml
x-foxctl:
  auth: plugin:aws-sigv4
  auth_config:
    service: s3
    region: us-east-1
```

### Plugin Transport

Plugins communicate through a simple stdin/stdout protocol:

1. Parent spawns plugin process
2. Parent writes request envelope to stdin, closes stdin
3. Plugin reads request, processes, writes response to stdout
4. Plugin exits with code 0 (success) or non-zero (error)
5. Parent reads response envelope, enforces timeout/limits

### Execution Constraints

The parent process enforces hard limits:

| Limit | Default | Max |
|-------|---------|-----|
| Wall time | 500ms | 5s |
| CPU time | 200ms | 2s |
| Memory | 64MB | 256MB |
| Input size | 128KB | 1MB |
| Output size | 32KB | 128KB |

Plugins must not make network calls (unless explicitly documented), write to
filesystem (except temp directories), or spawn child processes.

### Auth Plugin Protocol

**Input** (parent → plugin): Contains the pending HTTP request (method, URL,
headers, body), security scheme definition, credentials, and spec hints.

**Output** (plugin → parent): Returns modified headers and optional body
transform.

### Pagination Plugin Protocol

**Input** (parent → plugin): Contains the last HTTP response, paging state,
items fetched so far, and spec hints.

**Output** (plugin → parent): Returns `continue` (boolean), and either
`next_url`, `next_query`, or `next_cursor` to construct the next request.

Pagination stops when the plugin returns `continue: false`, or when `max_pages`
or `max_items` limits are reached.

## Examples

### List GitHub Repositories

```bash
# Import spec (once)
foxctl openapi import https://api.github.com/openapi.json --as=github

# Set auth
export FOXCTL_BEARER_TOKEN="ghp_..."

# Call API
foxctl run http/openapi \
  --input '{
    "spec": "memory:github",
    "operationId": "listReposForUser",
    "params": {"path": {"username": "octocat"}, "query": {"per_page": 100}},
    "paging": {"max_pages": 5}
  }'
```

### Create an Issue (POST)

```bash
foxctl run http/openapi \
  --input '{
    "spec": "memory:github",
    "operationId": "createIssue",
    "params": {
      "path": {"owner": "user", "repo": "my-repo"},
      "body": {"title": "Bug report", "body": "Description", "labels": ["bug"]}
    }
  }'
```

### Dry-Run a Destructive Operation

```bash
foxctl run http/openapi \
  --input '{
    "spec": "memory:github",
    "operationId": "deleteRepo",
    "params": {"path": {"owner": "user", "repo": "old-repo"}},
    "dry_run": true
  }'
```

## Cross-References

- [Providers and MCP](/integrations/providers-and-mcp) — LLM provider wiring
  and MCP serving
- [Protocol V1](/reference/protocol-v1) — envelope format and status rules
- [Skills Runtime](/skills/runtime-and-install) — skill execution paths
- [Hooks](/integrations/hooks) — editor and CI event integration
