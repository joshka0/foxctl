# OpenAPI Skill Specification

**Version:** 1.0.0\
**Status:** Draft\
**Last Updated:** 2025-11-12

This is the complete specification for the `http/openapi` skill, a generic
OpenAPI 3.x client built into foxctl Core Profile v1.

---

## Table of Contents

1. [Overview](#1-overview)
2. [Skill Signature](#2-skill-signature)
3. [Input Envelope Schema](#3-input-envelope-schema)
4. [Output Envelope Schema](#4-output-envelope-schema)
5. [Spec Management](#5-spec-management)
6. [Authentication Schemes](#6-authentication-schemes)
7. [Pagination Strategies](#7-pagination-strategies)
8. [Retry & Rate Limiting](#8-retry--rate-limiting)
9. [Dry-Run Mode](#9-dry-run-mode)
10. [Response Processing](#10-response-processing)
11. [Plugin Protocol](#11-plugin-protocol)
12. [Error Codes](#12-error-codes)
13. [Examples](#13-examples)
14. [Golden Fixtures](#14-golden-fixtures)
15. [Configuration](#15-configuration)
16. [Testing Recommendations](#16-testing-recommendations)
17. [Migration Guide](#17-migration-guide)

---

## 1. Overview

### 1.1 Purpose

The `http/openapi` skill is a **generic, zero-codegen solution** for invoking
any OpenAPI 3.x REST API operation. Instead of generating skill stubs for each
API endpoint, this single skill can call any operation by reading the OpenAPI
specification at runtime.

### 1.2 Key Capabilities

- **Universal API access**: Call any OpenAPI 3.x operation without codegen
- **Spec-as-memory**: Load specs from files, CAS artifacts, or named memories
- **Built-in auth**: Bearer, API Key, Basic Auth, OAuth2 Client Credentials
- **Automatic pagination**: Link headers, cursor-based, offset/limit,
  total-count heuristics
- **Smart retries**: Exponential backoff with jitter for 429/5xx errors
- **CAS integration**: Large responses automatically stored as artifacts with
  summaries
- **Dry-run support**: Preview requests without making actual API calls
- **Plugin extensibility**: Custom auth and pagination strategies via subprocess
  plugins

### 1.3 Use Cases

- Calling third-party REST APIs (GitHub, Stripe, Slack, etc.)
- Internal microservices with OpenAPI specs
- API exploration and testing
- Building API integrations without writing custom skills

### 1.4 Design Goals

- **Token efficiency**: Large responses go to CAS, only summaries in envelopes
- **Zero configuration**: Works out-of-the-box for standard APIs
- **Extensible**: Plugin system for non-standard auth/pagination
- **Production-ready**: Rate limiting, retries, error handling built-in

This specification extracts and expands upon sections 3.1-3.10 of the Core
Profile v1 specification, providing a complete implementation guide with
examples and test fixtures.

---

## 2. Skill Signature

### 2.1 Complete Manifest

```yaml
apiVersion: foxctl/v1
kind: Skill
metadata:
  name: http/openapi
  version: 1.0.0
  description: "Invoke OpenAPI 3.x operations with automatic pagination, auth, and retries"
  tags: ["http", "api", "openapi", "rest"]
distribution:
  type: exec # May be WASI in future versions
  artifact: sha256:... # Actual digest computed at build time
io:
  format: JSON
signature:
  command: http/openapi
  parameters:
    - name: spec
      type: string
      required: true
      description: "OpenAPI spec location: path, sha256:<hex>, or memory:<name>"

    - name: operationId
      type: string
      required: true
      description: "The operationId from the OpenAPI spec to invoke"

    - name: params
      type: object
      required: false
      description: "Parameters organized by location: path, query, header, body"

    - name: auth
      type: object
      required: false
      description: "Auth configuration: scheme selection and credentials override"

    - name: paging
      type: object
      required: false
      description: "Pagination configuration: strategy, limits, field mappings"

    - name: retry
      type: object
      required: false
      description: "Retry configuration: backoff timing and max attempts"

    - name: dry_run
      type: boolean
      required: false
      description: "If true, return request plan without executing"

  returns:
    - name: summary
      type: object
      description: "Response metadata: status_code, headers, pagination info"

    - name: body
      type: any
      description: "Response body (inline if small, see artifact if large)"

    - name: artifact
      type: string
      description: "CAS digest (sha256:...) when response exceeds inline threshold"

capabilities:
  network: egress # Requires network access for API calls
```

### 2.2 Version Compatibility

- **Minimum foxctl version**: 1.0.0
- **OpenAPI versions supported**: 3.0.x, 3.1.x
- **Swagger 2.0**: Not supported (convert to OpenAPI 3.x first)
- **Skill version policy**: Semantic versioning (major.minor.patch)

---

## 3. Input Envelope Schema

This section defines the complete input schema for the `http/openapi` skill.

### 3.1 Full Schema Example

```json
{
  "spec": "memory:github",
  "operationId": "listReposForUser",
  "params": {
    "path": { "username": "octocat" },
    "query": { "per_page": 100, "sort": "updated" },
    "header": { "Accept": "application/vnd.github.v3+json" },
    "body": null
  },
  "auth": {
    "scheme": "bearer",
    "secret_name": "github_token",
    "credentials": {}
  },
  "paging": {
    "strategy": "auto",
    "max_pages": 10,
    "max_items": 1000
  },
  "retry": {
    "base_ms": 250,
    "factor": 2.0,
    "max_attempts": 5,
    "max_ms": 8000
  },
  "dry_run": false
}
```

### 3.2 Field Descriptions

#### 3.2.1 `spec` (required, string)

Specifies where to load the OpenAPI specification from:

**File path**:

```json
{ "spec": "/path/to/openapi.yaml" }
{ "spec": "./relative/spec.json" }
```

**CAS digest**:

```json
{ "spec": "sha256:abc123..." }
```

**Named memory**:

```json
{ "spec": "memory:github" }
```

The skill loads and caches specs for 24 hours (configurable).

#### 3.2.2 `operationId` (required, string)

The unique identifier for the API operation to invoke. Must match an operationId
defined in the spec.

**Example**: `listReposForUser`, `createIssue`, `getUserByName`

**Discovery**: Use `foxctl openapi describe memory:github` to list all
available operations.

#### 3.2.3 `params` (optional, object)

Parameters organized by OpenAPI parameter locations:

```json
{
  "params": {
    "path": {
      "username": "octocat",
      "repo": "Hello-World"
    },
    "query": {
      "per_page": 100,
      "sort": "created",
      "direction": "desc"
    },
    "header": {
      "Accept": "application/vnd.github.v3+json",
      "X-Custom-Header": "value"
    },
    "body": {
      "title": "New Issue",
      "body": "Issue description"
    }
  }
}
```

**Type coercion**: String values automatically coerced to spec-defined types
(integers, booleans, etc.).

**Validation**: Parameters validated against spec (lenient by default, strict if
`--strict` config enabled).

#### 3.2.4 `auth` (optional, object)

Authentication configuration with three sub-fields:

**`scheme`** (string): Explicitly select security scheme

- `"bearer"` - HTTP Bearer token auth
- `"apiKey"` - API key in header or query
- `"basic"` - HTTP Basic auth
- `"oauth2"` - OAuth2 client credentials flow
- `"plugin:<name>"` - Custom plugin (e.g., `"plugin:aws-sigv4"`)
- `"auto"` or omitted - Auto-detect from spec

**`secret_name`** (string): Select specific secret bundle

- Useful when multiple APIs use different credentials
- Default: Uses standard secret names (`bearer_token`, `api_key`, etc.)

**`credentials`** (object): Direct credential override

- **Not recommended** - prefer secrets/environment variables
- Useful for testing or one-off operations
- Keys depend on scheme: `token`, `api_key`, `username`, `password`, etc.

**Example**:

```json
{
  "auth": {
    "scheme": "bearer",
    "credentials": { "token": "ghp_..." }
  }
}
```

#### 3.2.5 `paging` (optional, object)

Pagination configuration:

**`strategy`** (string): Detection strategy

- `"auto"` (default) - Try all strategies in order
- `"link"` - RFC 5988 Link headers
- `"cursor"` - Cursor-based (next_page_token, etc.)
- `"offset"` - Offset/limit or page/per_page
- `"none"` - Disable pagination (fetch one page only)

**Limits**:

- `max_pages` (integer) - Maximum pages to fetch
- `max_items` (integer) - Maximum total items to fetch

**Field mappings** (for non-standard APIs):

- `cursor_field` (string) - JSON path to cursor field (e.g.,
  `"data.next_cursor"`)
- `page_param` (string) - Page parameter name
- `per_page_param` (string) - Page size parameter name
- `offset_param` (string) - Offset parameter name
- `limit_param` (string) - Limit parameter name

**Example**:

```json
{
  "paging": {
    "strategy": "cursor",
    "cursor_field": "pagination.next_cursor",
    "max_items": 500
  }
}
```

#### 3.2.6 `retry` (optional, object)

Retry and backoff configuration:

- `base_ms` (integer, default: 250) - Initial retry delay in milliseconds
- `factor` (float, default: 2.0) - Backoff multiplier (2.0 = exponential)
- `max_attempts` (integer, default: 5) - Maximum attempts (includes initial
  request)
- `max_ms` (integer, default: 8000) - Maximum delay between retries
- `retry_codes` (array, default: [429, 502, 503, 504]) - HTTP codes to retry

**Backoff calculation**:
`delay = min(base_ms * (factor ^ attempt), max_ms) * jitter`

**Jitter**: Random factor between 0.8-1.2 to prevent thundering herd

**Retry-After**: Always respected when server provides this header

**Example**:

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

#### 3.2.7 `dry_run` (optional, boolean, default: false)

If true, constructs and validates the request without executing it. Returns a
request plan showing:

- HTTP method and URL
- Headers (with secrets redacted)
- Body (if applicable)
- Pagination configuration
- Retry configuration

Useful for debugging and validation.

---

## 4. Output Envelope Schema

All responses follow the standard JSON envelope format defined in Core Profile
v1.

### 4.1 Success Response (Inline)

For responses under the inline threshold (default: 32KB):

```json
{
  "version": 1,
  "status": "ok",
  "command": "http/openapi",
  "data": {
    "summary": {
      "status_code": 200,
      "headers": {
        "content-type": "application/json; charset=utf-8",
        "x-ratelimit-remaining": "4999",
        "x-ratelimit-reset": "1699564800",
        "etag": "W/\"abc123\""
      },
      "pagination": {
        "has_more": false,
        "total_pages": 1,
        "total_items": 25,
        "strategy_used": "link"
      }
    },
    "body": [
      { "id": 123, "name": "repo1" },
      { "id": 456, "name": "repo2" }
    ]
  },
  "meta": {
    "ts": "2025-11-12T10:30:00Z",
    "duration_ms": 342,
    "source": "run",
    "runner": "exec",
    "job_id": "01HQXY...",
    "workspace": "/home/user/project"
  },
  "error": { "code": null, "message": null }
}
```

### 4.2 Success Response (CAS Artifact)

For large responses (≥ 32KB), stored in content-addressable storage:

```json
{
  "version": 1,
  "status": "ok",
  "command": "http/openapi",
  "data": {
    "summary": {
      "status_code": 200,
      "headers": {
        "content-type": "application/json",
        "etag": "W/\"def456\"",
        "x-ratelimit-remaining": "4950"
      },
      "pagination": {
        "has_more": false,
        "total_pages": 5,
        "total_items": 247,
        "strategy_used": "link"
      },
      "kind": "application/json",
      "size_bytes": 1048576,
      "record_count": 247,
      "preview": {
        "first_keys": ["id", "name", "created_at", "updated_at"],
        "sample_record": {
          "id": 123,
          "name": "first-repo",
          "created_at": "2025-01-01T00:00:00Z"
        }
      }
    },
    "artifact": "sha256:def456..."
  },
  "meta": {
    "ts": "2025-11-12T10:30:00Z",
    "duration_ms": 2145,
    "source": "run",
    "runner": "exec",
    "job_id": "01HQXY..."
  },
  "error": { "code": null, "message": null }
}
```

**Retrieving the artifact**:

```bash
foxctl cas get sha256:def456... > full_response.json
```

### 4.3 Error Response

```json
{
  "version": 1,
  "status": "error",
  "command": "http/openapi",
  "data": {
    "summary": {
      "status_code": 401,
      "headers": {
        "www-authenticate": "Bearer realm=\"api\""
      }
    },
    "hint": "Check auth credentials. Expected bearer token in Authorization header. Set AGENTCTL_BEARER_TOKEN or use --auth.credentials"
  },
  "meta": {
    "ts": "2025-11-12T10:30:00Z",
    "duration_ms": 156,
    "source": "run",
    "runner": "exec"
  },
  "error": {
    "code": "EAUTH",
    "message": "Authentication failed: 401 Unauthorized"
  }
}
```

### 4.4 Dry-Run Response

```json
{
  "version": 1,
  "status": "ok",
  "command": "http/openapi",
  "data": {
    "summary": {
      "request_plan": {
        "method": "GET",
        "url": "https://api.github.com/users/octocat/repos",
        "query": { "per_page": "100" },
        "headers": {
          "Accept": "application/vnd.github.v3+json",
          "Authorization": "Bearer ***",
          "User-Agent": "foxctl/1.0.0"
        },
        "body": null
      },
      "pagination_config": {
        "strategy": "auto",
        "max_pages": null,
        "max_items": null
      },
      "retry_config": {
        "base_ms": 250,
        "factor": 2.0,
        "max_attempts": 5,
        "max_ms": 8000
      }
    }
  },
  "meta": {
    "ts": "2025-11-12T10:30:00Z",
    "duration_ms": 3,
    "source": "run"
  },
  "error": { "code": null, "message": null }
}
```

---

## 5. Spec Management

### 5.1 Importing Specs

Store OpenAPI specifications as named memories for easy reference:

```bash
# From local file
foxctl openapi import ./github-api.yaml --as=github

# From URL
foxctl openapi import https://api.github.com/openapi.json --as=github

# With strict validation
foxctl openapi import ./spec.yaml --as=myapi --strict
```

**What happens**:

1. Spec validated (lenient by default)
2. Spec stored in CAS
3. Named memory entry created: `memory:github` → CAS digest
4. Metadata recorded: source URL, import timestamp, version

### 5.2 Listing Imported Specs

```bash
foxctl memory list --type=openapi_spec
```

Output:

```text
NAME      TYPE          WORKSPACE         SIZE    UPDATED
github    openapi_spec  /home/user/proj   125KB   2h ago
stripe    openapi_spec  /home/user/proj   456KB   1d ago
internal  openapi_spec  /home/user/proj   89KB    3d ago
```

### 5.3 Validating Specs

```bash
# Basic validation
foxctl openapi validate memory:github

# Strict validation (all refs resolved, no warnings)
foxctl openapi validate memory:github --strict
```

**Validation checks**:

- ✅ Valid OpenAPI 3.x structure
- ✅ All `$ref` references resolved
- ✅ SecuritySchemes defined for all security requirements
- ✅ OperationIds are unique
- ⚠️ Warnings for missing descriptions, examples (strict only)

### 5.4 Describing Specs

List all operations available in a spec:

```bash
foxctl openapi describe memory:github
```

Output:

```text
OpenAPI: 3.0.3
Title: GitHub REST API
Version: 1.1.4

Operations (247 total):

OPERATION ID              METHOD  PATH                           TAGS
────────────────────────────────────────────────────────────────────────
listReposForUser          GET     /users/{username}/repos        [repos]
createRepoForUser         POST    /user/repos                    [repos]
getRepo                   GET     /repos/{owner}/{repo}          [repos]
updateRepo                PATCH   /repos/{owner}/{repo}          [repos]
deleteRepo                DELETE  /repos/{owner}/{repo}          [repos]
listIssuesForRepo         GET     /repos/{owner}/{repo}/issues   [issues]
createIssue               POST    /repos/{owner}/{repo}/issues   [issues]
...
```

**Filtering by tag**:

```bash
foxctl openapi describe memory:github --tag=issues
```

### 5.5 Spec Caching

Specs are cached in memory for performance:

- **Default TTL**: 24 hours
- **Cache key**: Spec name + CAS digest
- **Invalidation**: Auto on re-import, TTL expiry, or manual clear

**Manual cache clear**:

```bash
foxctl cache clear --type=openapi_spec
foxctl cache clear --type=openapi_spec --name=github
```

---

## 6. Authentication Schemes

### 6.1 Overview

Built-in support for common authentication patterns. Credentials loaded from:

1. **Secrets mount** (preferred): `/run/secrets/<name>`
2. **Environment variables**: `AGENTCTL_*` prefixed
3. **Direct credentials**: Passed in `auth.credentials` field (testing only)

### 6.2 Bearer Token (HTTP Bearer)

**OpenAPI spec**:

```yaml
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT # Optional
```

**Setup**:

```bash
export AGENTCTL_BEARER_TOKEN="ghp_abc123..."
```

**Usage** (auto-detected):

```json
{
  "spec": "memory:github",
  "operationId": "listReposForUser",
  "params": { "path": { "username": "octocat" } }
}
```

**Override**:

```json
{
  "auth": {
    "scheme": "bearer",
    "credentials": { "token": "ghp_xyz..." }
  }
}
```

**HTTP header**: `Authorization: Bearer ghp_abc123...`

### 6.3 API Key (Header or Query)

**OpenAPI spec**:

```yaml
components:
  securitySchemes:
    apiKeyAuth:
      type: apiKey
      in: header # or "query"
      name: X-API-Key
```

**Setup**:

```bash
export AGENTCTL_API_KEY="sk_live_abc123..."
```

**HTTP header**: `X-API-Key: sk_live_abc123...`\
**Or query**: `?api_key=sk_live_abc123...`

### 6.4 HTTP Basic Auth

**OpenAPI spec**:

```yaml
components:
  securitySchemes:
    basicAuth:
      type: http
      scheme: basic
```

**Setup**:

```bash
export AGENTCTL_BASIC_AUTH="username:password"
# Or separate:
export AGENTCTL_BASIC_USERNAME="user"
export AGENTCTL_BASIC_PASSWORD="pass"
```

**HTTP header**: `Authorization: Basic dXNlcjpwYXNz` (base64 encoded)

### 6.5 OAuth2 Client Credentials

**OpenAPI spec**:

```yaml
components:
  securitySchemes:
    oauth2:
      type: oauth2
      flows:
        clientCredentials:
          tokenUrl: https://auth.example.com/oauth/token
          scopes:
            read: Read access
            write: Write access
```

**Setup**:

```bash
export AGENTCTL_OAUTH2_CLIENT_ID="client_abc123"
export AGENTCTL_OAUTH2_CLIENT_SECRET="secret_xyz789"
```

**Flow**:

1. Skill exchanges credentials for access token (first request)
2. Token cached for lifetime (respects `expires_in`)
3. Automatic refresh on expiration
4. Bearer token used in API requests

### 6.6 Custom Auth (Plugins)

For non-standard auth (AWS SigV4, HMAC signatures, custom schemes):

**OpenAPI spec hint**:

```yaml
x-foxctl:
  auth: plugin:aws-sigv4
  auth_config:
    service: s3
    region: us-east-1
```

**Usage**:

```json
{
  "spec": "memory:aws-api",
  "operationId": "listBuckets",
  "auth": { "scheme": "plugin:aws-sigv4" }
}
```

See [Section 11: Plugin Protocol](#11-plugin-protocol) for implementation
details.

### 6.7 Multiple Auth Schemes

When spec defines multiple security schemes:

```yaml
security:
  - bearerAuth: []
  - apiKeyAuth: []
```

**Auto-selection**: Tries schemes in order until credentials found

**Manual selection**:

```json
{
  "auth": { "scheme": "bearer" }
}
```

### 6.8 Security Best Practices

**Credential redaction**: All secrets automatically redacted in:

- Log output (replaced with `***`)
- Error messages
- Dry-run output
- Job metadata
- Progress messages

**Secret storage**:

- ✅ Use environment variables or `/run/secrets/`
- ✅ Use secret management systems (Vault, AWS Secrets Manager)
- ❌ Don't hardcode in scripts or config files
- ❌ Don't pass in `auth.credentials` for production use

---

## 7. Pagination Strategies

### 7.1 Overview

The skill auto-detects and handles pagination for list operations. Four built-in
strategies:

1. **Link headers** (RFC 5988) - GitHub, GitLab
2. **Cursor-based** - Stripe, Google APIs
3. **Offset/limit** - Traditional REST APIs
4. **Total-count heuristics** - Inferred from response size

### 7.2 Automatic Detection

Default behavior (`strategy: "auto"`):

1. Check response headers for `Link: <url>; rel="next"`
2. Search response body for cursor fields (`next`, `next_page_token`, `cursor`)
3. Check spec for offset/limit parameters
4. Use heuristic: stop when `items.length < page_size`

### 7.3 Link Header Pagination

**RFC 5988 Web Linking standard**, used by GitHub, GitLab, etc.

**Response header**:

```text
Link: <https://api.github.com/users/octocat/repos?page=2>; rel="next",
      <https://api.github.com/users/octocat/repos?page=10>; rel="last"
```

**Auto-detected**: No configuration needed

**Limiting**:

```json
{
  "paging": { "max_pages": 5 }
}
```

### 7.4 Cursor-Based Pagination

**Stripe, Google Cloud, Slack, etc.**

**Response body**:

```json
{
  "data": [/* items */],
  "next_page_token": "eyJwYWdlIjoy..."
}
```

**Auto-detected fields**: `next_page_token`, `next_cursor`, `next`, `cursor`,
`pagination.cursor`

**Manual configuration**:

```json
{
  "paging": {
    "strategy": "cursor",
    "cursor_field": "pagination.next_token",
    "max_items": 1000
  }
}
```

**Nested fields**: Use JSON path notation: `"response.meta.next_cursor"`

### 7.5 Offset/Limit Pagination

**Classic REST pagination**: `GET /api/items?offset=0&limit=50`

**Parameter variations**:

- `offset` + `limit`
- `page` + `per_page`
- `skip` + `take`

**Auto-detection**: Finds params in spec

**Manual configuration**:

```json
{
  "paging": {
    "strategy": "offset",
    "offset_param": "offset",
    "limit_param": "limit",
    "max_items": 500
  }
}
```

### 7.6 Total-Count Heuristic

When no explicit pagination markers exist:

**Stop condition**: Response has fewer items than requested page size

**Example**: Request 100 items, receive 45 → assume last page

**Limitation**: May miss items if last page happens to be exactly page_size

### 7.7 Pagination Control

**Disable pagination** (fetch only first page):

```json
{ "paging": { "max_pages": 1 } }
```

**Limit by items**:

```json
{ "paging": { "max_items": 500 } }
```

**Limit by pages**:

```json
{ "paging": { "max_pages": 10 } }
```

**Both limits** (whichever reached first):

```json
{ "paging": { "max_pages": 10, "max_items": 500 } }
```

### 7.8 Pagination Metadata

All responses include `summary.pagination`:

```json
{
  "pagination": {
    "has_more": false,
    "total_pages": 3,
    "total_items": 247,
    "strategy_used": "link",
    "cursor_final": null
  }
}
```

---

## 8. Retry & Rate Limiting

### 8.1 Default Retry Behavior

**Retry codes**: 429 (Rate Limit), 502, 503, 504 (Server Errors)\
**Strategy**: Exponential backoff with jitter\
**Max attempts**: 5 (including initial request)\
**Backoff schedule**: 250ms, 500ms, 1s, 2s, 4s (with ±20% jitter)

### 8.2 Retry-After Header

When server returns `Retry-After` header, it's always respected:

**Seconds format**: `Retry-After: 60`\
**HTTP date format**: `Retry-After: Wed, 21 Oct 2025 07:28:00 GMT`

**Behavior**: Wait specified duration before retry, overriding backoff
calculation.

### 8.3 Rate Limit Tracking

**Common headers tracked**:

```text
X-RateLimit-Limit: 5000
X-RateLimit-Remaining: 4999
X-RateLimit-Reset: 1699564800
RateLimit-Limit: 5000
RateLimit-Remaining: 4999
RateLimit-Reset: 1699564800
```

**Included in response summary**:

```json
{
  "summary": {
    "headers": {
      "x-ratelimit-remaining": "4999",
      "x-ratelimit-reset": "1699564800"
    }
  }
}
```

**Unix timestamp conversion**: Reset timestamp converted to ISO 8601 in logs.

### 8.4 Custom Retry Configuration

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

**Linear backoff** (factor=1.0): 500ms, 500ms, 500ms\
**Exponential backoff** (factor=2.0): 500ms, 1000ms, 2000ms\
**Aggressive backoff** (factor=3.0): 500ms, 1500ms, 4500ms

### 8.5 Pagination Retry Budget

Each page gets its own retry budget:

- **Per-page**: max_attempts retries
- **Total**: pages × max_attempts requests maximum
- **Example**: 10 pages × 5 attempts = up to 50 requests

**Failure handling**: If one page fails after retries, return partial results
with error.

### 8.6 Disabling Retries

```json
{ "retry": { "max_attempts": 1 } }
```

**Use case**: Testing, debugging, or when retries handled externally.

### 8.7 Rate Limit Errors

When rate limit exceeded after all retries:

```json
{
  "status": "error",
  "data": {
    "summary": {
      "status_code": 429,
      "attempts": 5,
      "headers": {
        "x-ratelimit-reset": "1699564800"
      }
    },
    "hint": "Rate limit resets at 2025-11-12T11:00:00Z (in 60 seconds)"
  },
  "error": {
    "code": "ERATELIMIT",
    "message": "Rate limit exceeded after 5 attempts"
  }
}
```

---

_[This is a comprehensive starting point. The spec continues with sections 9-17
covering Dry-Run Mode, Response Processing, Plugin Protocol, Error Codes,
Examples, Golden Fixtures, Configuration, Testing, and Migration Guide. Due to
length, I'll provide the complete document in a single file.]_

## 9. Dry-Run Mode

### 9.1 Purpose

Preview and validate requests without making actual API calls.

**Use cases**:

- Parameter validation
- Request debugging
- Auth header verification (redacted)
- Pagination configuration testing
- Learning API behavior

### 9.2 Enabling Dry-Run

```json
{
  "spec": "memory:github",
  "operationId": "listReposForUser",
  "params": { "path": { "username": "octocat" } },
  "dry_run": true
}
```

### 9.3 Response Format

```json
{
  "status": "ok",
  "data": {
    "summary": {
      "request_plan": {
        "method": "GET",
        "url": "https://api.github.com/users/octocat/repos",
        "query": { "per_page": "100" },
        "headers": {
          "Accept": "application/vnd.github.v3+json",
          "Authorization": "Bearer ***",
          "User-Agent": "foxctl/1.0.0"
        },
        "body": null
      },
      "pagination_config": {
        "strategy": "auto",
        "max_pages": null,
        "max_items": null
      },
      "retry_config": {
        "base_ms": 250,
        "factor": 2.0,
        "max_attempts": 5,
        "max_ms": 8000
      },
      "validation": {
        "spec_valid": true,
        "operation_found": true,
        "params_valid": true,
        "auth_available": true
      }
    }
  }
}
```

### 9.4 What Gets Validated

✅ **Validated in dry-run**:

- Spec can be loaded and parsed
- OperationId exists in spec
- Required parameters provided
- Parameter types match spec
- URL construction succeeds
- Auth credentials present

❌ **Not validated**:

- Network connectivity
- API availability
- Auth credentials correctness
- Response structure

### 9.5 Security

All credentials **redacted** in output:

- `Authorization: Bearer ***`
- `X-API-Key: ***`
- `Authorization: Basic ***`

Full request plan logged at DEBUG level with secrets masked.

---

## 10. Response Processing

### 10.1 Size-Based Handling

**Threshold**: `inline_output_kb` (default: 32KB)

**Small responses** (< 32KB):

- Included inline in `data.body`
- Full response immediately available

**Large responses** (≥ 32KB):

- Stored in CAS
- `data.artifact` contains digest
- `data.summary` contains preview

### 10.2 Summary Generation Rules

**Always included**:

```json
{
  "summary": {
    "status_code": 200,
    "headers": {/* selected headers */}
  }
}
```

**For successful responses**:

```json
{
  "summary": {
    "status_code": 200,
    "headers": {/* ... */},
    "pagination": {/* if paginated */},
    "kind": "application/json",
    "size_bytes": 1048576
  }
}
```

**For CAS artifacts**:

```json
{
  "summary": {
    /* ... */,
    "record_count": 247,
    "preview": {
      "first_keys": ["id", "name"],
      "sample_record": { /* first item */ }
    }
  }
}
```

### 10.3 Header Selection

**Rate limiting**:

- `x-ratelimit-*`, `ratelimit-*`
- `retry-after`

**Caching**:

- `etag`
- `last-modified`
- `cache-control`

**Pagination**:

- `link`
- `x-total-count`, `x-total-pages`

**Tracing**:

- `x-request-id`, `request-id`
- `x-trace-id`, `trace-id`

**Content**:

- `content-type`
- `content-length`

### 10.4 Pagination Response Aggregation

**Array responses**: Items concatenated

```javascript
// Page 1: [{"id": 1}, {"id": 2}]
// Page 2: [{"id": 3}, {"id": 4}]
// Result: [{"id": 1}, {"id": 2}, {"id": 3}, {"id": 4}]
```

**Object responses**: Wrapped with page metadata

```json
{
  "pages": [
    { "data": [...], "cursor": "abc" },
    { "data": [...], "cursor": "def" }
  ],
  "aggregated": [ /* all items */ ]
}
```

### 10.5 Error Response Handling

**4xx Client Errors**: Inline, not artifactized

```json
{
  "status": "error",
  "data": {
    "summary": { "status_code": 422 },
    "error_body": {
      "message": "Validation failed",
      "errors": [
        { "field": "email", "message": "Invalid format" }
      ]
    },
    "hint": "Fix validation errors in request body"
  },
  "error": {
    "code": "EARG",
    "message": "422 Unprocessable Entity"
  }
}
```

**5xx Server Errors**: Include server context

```json
{
  "status": "error",
  "data": {
    "summary": {
      "status_code": 503,
      "headers": { "x-request-id": "req_123" }
    },
    "hint": "Server temporarily unavailable. Retry later."
  },
  "error": {
    "code": "ERUNTIME",
    "message": "503 Service Unavailable"
  }
}
```

### 10.6 Preview Generation

**JSON arrays**:

```json
{
  "record_count": 247,
  "preview": {
    "first_keys": ["id", "name", "created_at"],
    "sample_record": {
      "id": 123,
      "name": "example",
      "created_at": "2025-01-01T00:00:00Z"
    }
  }
}
```

**JSON objects**:

```json
{
  "preview": {
    "first_keys": ["users", "total", "page"],
    "structure": "object"
  }
}
```

**Non-JSON** (HTML, XML, plain text):

```json
{
  "kind": "text/html",
  "preview": {
    "first_bytes": "<!DOCTYPE html>\n<html lang=\"en\">..."
  }
}
```

---

## 11. Plugin Protocol

### 11.1 Overview

Extend the skill with custom auth and pagination via out-of-process plugins.

**Plugin types**:

- **Auth plugins**: Custom signing (AWS SigV4, HMAC, etc.)
- **Pagination plugins**: Non-standard pagination logic

**Communication**: JSON envelopes on stdin/stdout

### 11.2 Discovery

Plugins found via:

1. `AGENTCTL_PLUGIN_PATH` environment variable (colon-separated)
2. `openapi.plugin_path` config setting
3. Default: `~/.foxctl/plugins`

**Naming**: `foxctl-plugin-<name>` (e.g., `foxctl-plugin-aws-sigv4`)

### 11.3 Auth Plugin Interface

**Command**: `plugin/auth`

**Input**:

```json
{
  "version": 1,
  "command": "plugin/auth",
  "data": {
    "request": {
      "method": "GET",
      "url": "https://s3.amazonaws.com/bucket/key",
      "headers": { "Host": "s3.amazonaws.com" },
      "body": null
    },
    "context": {
      "security_scheme": {
        "type": "apiKey",
        "in": "header",
        "name": "Authorization"
      },
      "credentials": {
        "access_key": "AKIAIOSFODNN7EXAMPLE",
        "secret_key": "***"
      },
      "spec_hints": {
        "x-foxctl": {
          "auth": "plugin:aws-sigv4",
          "auth_config": { "service": "s3", "region": "us-east-1" }
        }
      }
    }
  }
}
```

**Output** (success):

```json
{
  "version": 1,
  "status": "ok",
  "command": "plugin/auth",
  "data": {
    "headers": {
      "Authorization": "AWS4-HMAC-SHA256 Credential=...",
      "X-Amz-Date": "20251112T103000Z",
      "X-Amz-Content-SHA256": "e3b0c..."
    },
    "body": null
  },
  "error": { "code": null, "message": null }
}
```

**Output** (error):

```json
{
  "version": 1,
  "status": "error",
  "command": "plugin/auth",
  "data": {
    "hint": "Missing required credential: secret_key"
  },
  "error": {
    "code": "EAUTH",
    "message": "Failed to sign request: missing secret_key"
  }
}
```

### 11.4 Pagination Plugin Interface

**Command**: `plugin/pagination`

**Input**:

```json
{
  "version": 1,
  "command": "plugin/pagination",
  "data": {
    "last_response": {
      "status": 200,
      "headers": { "content-type": "application/json" },
      "body": {
        "results": [/* items */],
        "meta": { "next_token": "eyJwYWdlIjoy..." }
      }
    },
    "requested_max_items": 1000,
    "items_fetched_so_far": 250
  }
}
```

**Output** (continue):

```json
{
  "version": 1,
  "status": "ok",
  "command": "plugin/pagination",
  "data": {
    "continue": true,
    "next_url": null,
    "next_query": { "page_token": "eyJwYWdlIjoy..." },
    "next_cursor": "eyJwYWdlIjoy..."
  },
  "error": { "code": null, "message": null }
}
```

**Output** (stop):

```json
{
  "version": 1,
  "status": "ok",
  "command": "plugin/pagination",
  "data": { "continue": false },
  "error": { "code": null, "message": null }
}
```

### 11.5 Example Plugin Implementation

**File**: `~/.foxctl/plugins/foxctl-plugin-aws-sigv4`

```python
#!/usr/bin/env python3
import json
import sys
import hmac
import hashlib
from datetime import datetime

def sign_aws_request(request, access_key, secret_key, region, service):
    """
    AWS Signature Version 4 signing.
    Simplified example - production should use boto3 or similar.
    """
    method = request["method"]
    url = request["url"]
    headers = request.get("headers", {})
    body = request.get("body", b"")
    
    # Create canonical request
    timestamp = datetime.utcnow().strftime('%Y%m%dT%H%M%SZ')
    date = timestamp[:8]
    
    # Payload hash
    if body:
        payload_hash = hashlib.sha256(body.encode()).hexdigest()
    else:
        payload_hash = hashlib.sha256(b'').hexdigest()
    
    # Canonical headers
    headers['X-Amz-Date'] = timestamp
    headers['X-Amz-Content-SHA256'] = payload_hash
    
    # Create signature (simplified - full implementation needed)
    credential_scope = f"{date}/{region}/{service}/aws4_request"
    string_to_sign = f"AWS4-HMAC-SHA256\n{timestamp}\n{credential_scope}\n..."
    
    # Sign
    signing_key = f"AWS4{secret_key}".encode()
    signature = hmac.new(signing_key, string_to_sign.encode(), hashlib.sha256).hexdigest()
    
    # Authorization header
    headers['Authorization'] = (
        f"AWS4-HMAC-SHA256 "
        f"Credential={access_key}/{credential_scope}, "
        f"SignedHeaders=host;x-amz-content-sha256;x-amz-date, "
        f"Signature={signature}"
    )
    
    return headers

def main():
    try:
        input_data = json.load(sys.stdin)
        request = input_data["data"]["request"]
        context = input_data["data"]["context"]
        
        credentials = context["credentials"]
        config = context.get("spec_hints", {}).get("x-foxctl", {}).get("auth_config", {})
        
        signed_headers = sign_aws_request(
            request,
            credentials["access_key"],
            credentials["secret_key"],
            config.get("region", "us-east-1"),
            config.get("service", "s3")
        )
        
        output = {
            "version": 1,
            "status": "ok",
            "command": "plugin/auth",
            "data": {
                "headers": signed_headers,
                "body": request.get("body")
            },
            "error": {"code": None, "message": None}
        }
        
        json.dump(output, sys.stdout)
        sys.exit(0)
        
    except Exception as e:
        error_output = {
            "version": 1,
            "status": "error",
            "command": "plugin/auth",
            "data": {"hint": str(e)},
            "error": {"code": "EAUTH", "message": f"Plugin error: {e}"}
        }
        json.dump(error_output, sys.stdout)
        sys.exit(1)

if __name__ == "__main__":
    main()
```

**Make executable**:

```bash
chmod +x ~/.foxctl/plugins/foxctl-plugin-aws-sigv4
```

### 11.6 Spec Hints

Guide plugin usage via `x-foxctl` vendor extensions:

```yaml
x-foxctl:
  auth: plugin:aws-sigv4
  auth_config:
    service: s3
    region: us-east-1
  pagination: plugin:custom-cursor
  pagination_config:
    cursor_path: "response.metadata.continuation"
```

---

## 12. Error Codes

### 12.1 Complete Error Catalog

| Code          | HTTP    | Meaning               | Remediation                                         |
| ------------- | ------- | --------------------- | --------------------------------------------------- |
| `EOPENAPI`    | -       | OpenAPI spec error    | Validate spec, check operationId exists             |
| `EARG`        | 400,422 | Invalid arguments     | Check required params, verify types                 |
| `EAUTH`       | 401     | Authentication failed | Verify credentials, check token expiry              |
| `EPAGINATION` | -       | Pagination failure    | Specify strategy manually, check response structure |
| `ERATELIMIT`  | 429     | Rate limit exceeded   | Wait for reset, reduce request rate                 |
| `ERUNTIME`    | 5xx     | Network/server error  | Retry later, check API status                       |
| `ENOTFOUND`   | 404     | Resource not found    | Verify spec/memory name, check path params          |
| `ETIMEOUT`    | -       | Request timeout       | Increase timeout, check network                     |
| `EPOLICY`     | -       | Policy violation      | Check egress allow list, verify capabilities        |

### 12.2 Error Response Structure

```json
{
  "status": "error",
  "command": "http/openapi",
  "data": {
    "summary": {/* context */},
    "hint": "Actionable remediation suggestion"
  },
  "error": {
    "code": "E<TYPE>",
    "message": "Human-readable description"
  }
}
```

### 12.3 Error Examples

#### EOPENAPI

```json
{
  "error": {
    "code": "EOPENAPI",
    "message": "OperationId 'listRepos' not found in spec"
  },
  "data": {
    "hint": "Available operations: listReposForUser, createRepo, getRepo. Use 'foxctl openapi describe memory:github' to list all."
  }
}
```

#### EARG

```json
{
  "error": {
    "code": "EARG",
    "message": "Missing required parameter: username"
  },
  "data": {
    "summary": {
      "missing_params": ["username"],
      "provided_params": []
    },
    "hint": "Add username to params.path: {\"path\":{\"username\":\"octocat\"}}"
  }
}
```

#### EAUTH

```json
{
  "error": {
    "code": "EAUTH",
    "message": "Authentication failed: 401 Unauthorized"
  },
  "data": {
    "summary": {
      "status_code": 401,
      "headers": { "www-authenticate": "Bearer" }
    },
    "hint": "Set AGENTCTL_BEARER_TOKEN or use --auth.credentials"
  }
}
```

#### EPAGINATION

```json
{
  "error": {
    "code": "EPAGINATION",
    "message": "Could not auto-detect pagination strategy"
  },
  "data": {
    "summary": {
      "attempted_strategies": ["link", "cursor", "offset"],
      "response_keys": ["data", "metadata"]
    },
    "hint": "Specify manually: --paging.strategy=cursor --paging.cursor_field=metadata.next"
  }
}
```

#### ERATELIMIT

```json
{
  "error": {
    "code": "ERATELIMIT",
    "message": "Rate limit exceeded after 5 attempts"
  },
  "data": {
    "summary": {
      "status_code": 429,
      "attempts": 5,
      "headers": {
        "x-ratelimit-reset": "1699564800"
      }
    },
    "hint": "Rate limit resets at 2025-11-12T11:00:00Z (60 seconds)"
  }
}
```

### 12.4 Partial Success Errors

When pagination fails mid-stream:

```json
{
  "status": "error",
  "data": {
    "summary": {
      "pages_fetched": 3,
      "items_fetched": 150
    },
    "partial_result": {
      "artifact": "sha256:abc123...",
      "summary": { "record_count": 150 }
    },
    "hint": "Partial data available in artifact. Error on page 4."
  },
  "meta": { "partial": true },
  "error": {
    "code": "ERUNTIME",
    "message": "Connection timeout on page 4"
  }
}
```

---

## 13. Examples

This section provides real-world usage examples.

### 13.1 GitHub - List Repositories

```bash
# Import spec (once)
foxctl openapi import https://api.github.com/openapi.json --as=github

# Set auth
export AGENTCTL_BEARER_TOKEN="ghp_..."

# Basic request
foxctl run http/openapi \
  --spec=memory:github \
  --operationId=listReposForUser \
  --params='{"path":{"username":"octocat"}}'

# With pagination
foxctl run http/openapi \
  --spec=memory:github \
  --operationId=listReposForUser \
  --params='{"path":{"username":"octocat"},"query":{"per_page":100}}' \
  --paging='{"max_pages":5}'
```

**Response**:

```json
{
  "status": "ok",
  "data": {
    "summary": {
      "status_code": 200,
      "pagination": { "total_items": 42, "total_pages": 1 }
    },
    "body": [
      { "id": 1296269, "name": "Hello-World", "private": false }
    ]
  }
}
```

### 13.2 Stripe - List Customers

```bash
# Import
curl -o stripe.json https://raw.githubusercontent.com/stripe/openapi/master/openapi/spec3.json
foxctl openapi import ./stripe.json --as=stripe

# Auth
export AGENTCTL_BEARER_TOKEN="sk_test_..."

# Request (cursor pagination auto-detected)
foxctl run http/openapi \
  --spec=memory:stripe \
  --operationId=CustomersList \
  --params='{"query":{"limit":100}}' \
  --paging='{"max_items":500}'
```

### 13.3 POST Request - Create Issue

```bash
foxctl run http/openapi \
  --spec=memory:github \
  --operationId=createIssue \
  --params='{
    "path": {
      "owner": "user",
      "repo": "my-repo"
    },
    "body": {
      "title": "Bug report",
      "body": "Description of the bug",
      "labels": ["bug"]
    }
  }'
```

### 13.4 Dry-Run - Preview Request

```bash
foxctl run http/openapi \
  --spec=memory:github \
  --operationId=deleteRepo \
  --params='{"path":{"owner":"user","repo":"old-repo"}}' \
  --dry_run
```

**Response** shows request plan without executing:

```json
{
  "status": "ok",
  "data": {
    "summary": {
      "request_plan": {
        "method": "DELETE",
        "url": "https://api.github.com/repos/user/old-repo",
        "headers": { "Authorization": "Bearer ***" }
      }
    }
  }
}
```

### 13.5 Error Handling

```bash
# Missing parameter
foxctl run http/openapi \
  --spec=memory:github \
  --operationId=listReposForUser
# Error: EARG - missing username

# Auth failure
unset AGENTCTL_BEARER_TOKEN
foxctl run http/openapi \
  --spec=memory:github \
  --operationId=getAuthenticatedUser
# Error: EAUTH - 401 Unauthorized
```

### 13.6 Custom Pagination

```bash
# Non-standard API with nested cursor
foxctl run http/openapi \
  --spec=memory:custom-api \
  --operationId=listItems \
  --paging='{
    "strategy": "cursor",
    "cursor_field": "response.metadata.next_cursor",
    "max_items": 1000
  }'
```

---

## 14. Golden Fixtures

Test fixtures for validating implementations.

### 14.1 Directory Structure

```text
tests/fixtures/openapi/
├── inputs/           # Request inputs
├── outputs/          # Expected responses
└── specs/            # Test OpenAPI specs
```

### 14.2 Input Fixtures

`tests/fixtures/openapi/inputs/github-list-repos.json`:

```json
{
  "spec": "memory:github",
  "operationId": "listReposForUser",
  "params": {
    "path": { "username": "octocat" },
    "query": { "per_page": 100, "sort": "updated" }
  }
}
```

`tests/fixtures/openapi/inputs/error-missing-operationid.json`:

```json
{
  "spec": "memory:github",
  "params": { "path": { "username": "octocat" } }
}
```

### 14.3 Output Fixtures

`tests/fixtures/openapi/outputs/success-inline.json`:

```json
{
  "version": 1,
  "status": "ok",
  "command": "http/openapi",
  "data": {
    "summary": {
      "status_code": 200,
      "headers": { "content-type": "application/json" },
      "pagination": { "has_more": false, "total_items": 2 }
    },
    "body": [
      { "id": 1296269, "name": "Hello-World" },
      { "id": 1296270, "name": "Spoon-Knife" }
    ]
  },
  "error": { "code": null, "message": null }
}
```

`tests/fixtures/openapi/outputs/error-eauth-401.json`:

```json
{
  "version": 1,
  "status": "error",
  "command": "http/openapi",
  "data": {
    "summary": { "status_code": 401 },
    "hint": "Check bearer token"
  },
  "error": {
    "code": "EAUTH",
    "message": "Authentication failed: 401 Unauthorized"
  }
}
```

### 14.4 Spec Fixtures

`tests/fixtures/openapi/specs/minimal.yaml`:

```yaml
openapi: 3.0.0
info:
  title: Minimal Test API
  version: 1.0.0
servers:
  - url: https://api.example.com
paths:
  /users/{id}:
    get:
      operationId: getUser
      parameters:
        - name: id
          in: path
          required: true
          schema: { type: integer }
      responses:
        "200":
          description: Success
          content:
            application/json:
              schema:
                type: object
                properties:
                  id: { type: integer }
                  name: { type: string }
```

### 14.5 Usage

```bash
# Test with fixture
cat tests/fixtures/openapi/inputs/github-list-repos.json | \
  foxctl run http/openapi --input-file=-

# Validate output
foxctl run http/openapi --input-file=tests/fixtures/openapi/inputs/github-list-repos.json | \
  jq -S . > actual.json
diff tests/fixtures/openapi/outputs/success-inline.json actual.json
```

---

## 15. Configuration

### 15.1 Config File

`~/.foxctl/config.yaml`:

```yaml
openapi:
  strict_validate: false
  plugin_path: ~/.foxctl/plugins:/usr/local/lib/foxctl/plugins
  spec_cache_ttl: 24h
  default_retry:
    base_ms: 250
    factor: 2.0
    max_attempts: 5
    max_ms: 8000
  default_paging:
    max_pages: 100
    max_items: 10000
```

### 15.2 Environment Variables

```bash
export AGENTCTL_OPENAPI_STRICT_VALIDATE=true
export AGENTCTL_OPENAPI_PLUGIN_PATH=~/.foxctl/plugins
export AGENTCTL_OPENAPI_SPEC_CACHE_TTL=12h

# Auth
export AGENTCTL_BEARER_TOKEN="..."
export AGENTCTL_API_KEY="..."
export AGENTCTL_BASIC_AUTH="user:pass"
export AGENTCTL_OAUTH2_CLIENT_ID="..."
export AGENTCTL_OAUTH2_CLIENT_SECRET="..."
```

### 15.3 Workspace Config

Project-specific defaults in `.foxctl/config.yaml`:

```yaml
openapi:
  operation_defaults:
    - pattern: "github/*"
      retry: { max_attempts: 3 }
      paging: { max_pages: 10 }
      headers:
        Accept: "application/vnd.github.v3+json"
```

---

## 16. Testing Recommendations

### 16.1 Unit Tests

```bash
# Spec validation
foxctl openapi validate tests/fixtures/openapi/specs/minimal.yaml

# Dry-run validation
foxctl run http/openapi \
  --spec=tests/fixtures/openapi/specs/minimal.yaml \
  --operationId=getUser \
  --params='{"path":{"id":123}}' \
  --dry_run
```

### 16.2 Integration Tests

```bash
# Public API (no auth)
foxctl openapi import https://jsonplaceholder.typicode.com/openapi.json --as=test
foxctl run http/openapi --spec=memory:test --operationId=getPosts

# Authenticated API
export AGENTCTL_BEARER_TOKEN="${TEST_TOKEN}"
foxctl run http/openapi --spec=memory:github --operationId=getAuthenticatedUser
```

### 16.3 Smoke Tests

```bash
foxctl openapi test memory:github
```

Output:

```text
✓ getUser (GET /users/{username})
✓ listReposForUser (GET /users/{username}/repos)
✗ createRepo (POST /user/repos) - requires auth

Passed: 198/247
Skipped: 49/247
```

---

## 17. Migration Guide

### 17.1 From curl/HTTP Tools

**Before**:

```bash
curl -H "Authorization: Bearer $TOKEN" \
     "https://api.github.com/users/octocat/repos?per_page=100"
```

**After**:

```bash
foxctl openapi import https://api.github.com/openapi.json --as=github
foxctl run http/openapi \
  --spec=memory:github \
  --operationId=listReposForUser \
  --params='{"path":{"username":"octocat"},"query":{"per_page":100}}'
```

### 17.2 From Codegen

**Before**: Generate client → write integration code\
**After**: Import spec → use generic skill

**Benefits**: No codegen, instant updates, no dependencies

### 17.3 From Custom Skills

**Optional wrapper generation**:

```bash
foxctl openapi generate memory:github --install --group-by=tag
foxctl run github/list-repos-for-user --username=octocat
```

---

## Appendix A: References

- [OpenAPI 3.0 Specification](https://spec.openapis.org/oas/v3.0.3)
- [OpenAPI 3.1 Specification](https://spec.openapis.org/oas/v3.1.0)
- [RFC 5988: Web Linking](https://tools.ietf.org/html/rfc5988)
- [RFC 6750: OAuth 2.0 Bearer Token](https://tools.ietf.org/html/rfc6750)
- [foxctl Core Profile v1](./core_profile_v1.md)

## Appendix B: Changelog

**Version 1.0.0 (2025-11-12)**:

- Initial specification
- OpenAPI 3.0.x and 3.1.x support
- Built-in auth: Bearer, API Key, Basic, OAuth2
- Built-in pagination: Link, Cursor, Offset, Heuristic
- Plugin system
- Retry and rate limiting
- CAS integration
- Dry-run mode
- Complete error catalog
- Golden fixtures

---

## End of OpenAPI Skill Specification v1.0.0
