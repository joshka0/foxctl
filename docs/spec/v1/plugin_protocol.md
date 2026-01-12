# Plugin Protocol v1

**Version:** 1.0.0
**Status:** Final Draft
**Last Updated:** 2025-11-15

> **Purpose:** This document defines the plugin protocol for extending agentctl with custom authentication and pagination strategies. Plugins are out-of-process executables that communicate via JSON envelopes over stdin/stdout.

---

## Table of Contents

1. [Overview](#1-overview)
2. [Transport & Execution](#2-transport--execution)
3. [Plugin Request Format](#3-plugin-request-format)
4. [Plugin Response Format](#4-plugin-response-format)
5. [Auth Plugin](#5-auth-plugin)
6. [Pagination Plugin](#6-pagination-plugin)
7. [Discovery & Environment](#7-discovery--environment)
8. [Security & Limits](#8-security--limits)
9. [Error Handling](#9-error-handling)
10. [Examples](#10-examples)

---

## 1. Overview

### 1.1 Purpose

The plugin system allows extending agentctl's `http/openapi` skill with custom:
- **Authentication schemes** (e.g., AWS SigV4, HMAC, custom OAuth flows)
- **Pagination strategies** (e.g., vendor-specific cursor formats, GraphQL-style pagination)

### 1.2 Why Plugins?

Built-in auth and pagination cover ~90% of APIs (Bearer, API Key, Basic, OAuth2 client credentials; Link headers, cursor, offset/limit). For the remaining "snowflake" APIs with custom requirements, plugins provide extensibility **without** bloating core.

### 1.3 Design Principles

- **Out-of-process**: Plugins run as subprocesses, isolated from agentctl
- **Envelope-based**: All I/O uses Protocol v1 JSON envelopes
- **Language-agnostic**: Any language that can read/write JSON on stdin/stdout
- **Sandboxed**: Parent enforces strict timeouts, memory, and IO limits
- **Discoverable**: Convention-based discovery via `AGENTCTL_PLUGIN_PATH`

### 1.4 Plugin Types

| Type | Command | Purpose |
|------|---------|---------|
| **Auth** | `plugin/auth` | Sign requests, inject headers/body |
| **Pagination** | `plugin/pagination` | Determine next page from response |

---

## 2. Transport & Execution

### 2.1 Communication Protocol

**Transport**: JSON envelopes over **stdin** (parent → plugin) and **stdout** (plugin → parent)

**Format**: Single JSON envelope per invocation (not streaming in v1)

**Lifecycle**:
1. Parent spawns plugin process
2. Parent writes request envelope to stdin, closes stdin
3. Plugin reads request, processes, writes response to stdout
4. Plugin exits with code 0 (success) or non-zero (error)
5. Parent reads response envelope, enforces timeout/limits

### 2.2 Execution Constraints

The parent process **MUST** enforce:
- **Timeout**: Wall clock timeout (default 500ms, configurable via plugin manifest)
- **CPU limit**: CPU time limit (default 200ms)
- **Memory limit**: Max resident set size (default 64MB)
- **Input size**: Max stdin size (default 128KB)
- **Output size**: Max stdout size (default 32KB)

Plugins MUST NOT:
- Make network calls (unless explicitly allowed and documented)
- Write to filesystem (except temp directories if needed)
- Spawn child processes
- Use excessive resources

### 2.3 Environment Variables

The parent MAY set:
- `AGENTCTL_PLUGIN_NAME`: Plugin name being invoked
- `AGENTCTL_PLUGIN_VERSION`: Plugin version (from manifest)
- `AGENTCTL_PLUGIN_COMMAND`: Command being invoked (`plugin/auth` or `plugin/pagination`)
- `AGENTCTL_WORKSPACE`: Current workspace path
- `AGENTCTL_JOB_ID`: Job ID if executing in job context

Plugins SHOULD NOT rely on environment variables for secrets. Secrets are passed via the request envelope `context.credentials` field.

---

## 3. Plugin Request Format

### 3.1 Request Envelope (Parent → Plugin)

```jsonc
{
  "version": 1,
  "command": "plugin/auth" | "plugin/pagination",
  "data": {
    "request": {                          // Immutable view of pending request or last response
      "method": "GET",
      "url": "https://api.example.com/items",
      "headers": {
        "Accept": "application/json",
        "User-Agent": "agentctl/1.0"
      },
      "body": null                        // Raw string for signing; omitted for large bodies
    },
    "context": {
      "security_scheme": {                // From OpenAPI securitySchemes (auth only)
        "type": "apiKey",
        "name": "X-API-Key",
        "in": "header"
      },
      "credentials": {                    // Redacted in logs
        "api_key": "secret_key_value"
      },
      "spec_hints": {                     // x-agentctl extensions copied verbatim
        "signing_algorithm": "HMAC-SHA256"
      },
      "paging_state": {                   // Only for pagination plugins
        "cursor": "previous_cursor_value",
        "page": 2
      },
      "response": {                       // Only for pagination plugins
        "status": 200,
        "headers": {
          "Content-Type": "application/json",
          "Link": "<https://...>; rel=\"next\""
        },
        "body": {                         // Parsed JSON or raw string
          "items": [...],
          "next_cursor": "abc123"
        }
      }
    },
    "limits": {                           // Enforcement limits
      "cpu_ms": 200,
      "wall_ms": 500,
      "max_out_kb": 32
    }
  }
}
```

### 3.2 Field Descriptions

#### `data.request` (object)
Snapshot of the HTTP request being prepared (auth) or last request made (pagination):
- `method`: HTTP method (GET, POST, PUT, DELETE, etc.)
- `url`: Full URL with query parameters
- `headers`: Current headers (before auth modification)
- `body`: Request body (null for GET/HEAD, omitted if >128KB)

#### `data.context` (object)
Contextual information for plugin processing:
- **Auth plugins**:
  - `security_scheme`: OpenAPI security scheme definition
  - `credentials`: Secret values (API keys, tokens, etc.) - **redacted in logs**
  - `spec_hints`: `x-agentctl` vendor extensions from spec
- **Pagination plugins**:
  - `paging_state`: Accumulated state from previous pages
  - `response`: Last HTTP response (status, headers, body)

#### `data.limits` (object)
Resource limits enforced by parent:
- `cpu_ms`: Maximum CPU time in milliseconds
- `wall_ms`: Maximum wall clock time in milliseconds
- `max_out_kb`: Maximum stdout output in kilobytes

---

## 4. Plugin Response Format

### 4.1 Success Response (Plugin → Parent)

```jsonc
{
  "version": 1,
  "status": "ok",
  "command": "plugin/auth" | "plugin/pagination",
  "data": {
    // Auth response fields:
    "headers": {                          // Modified/added headers
      "Authorization": "Bearer token...",
      "X-Signature": "hmac_signature..."
    },
    "body": null,                         // Optional body transform

    // Pagination response fields:
    "continue": true,                     // Whether to fetch next page
    "next_url": "https://api.../page2",   // Exclusive with next_query
    "next_query": {                       // Query params for next request
      "page_token": "abc123",
      "page": 3
    },
    "next_cursor": "abc123",              // Stored in paging_state for next iteration
    "items_in_page": 50                   // Optional: items in current page
  },
  "meta": {
    "ts": "2025-11-15T12:34:56Z",
    "duration_ms": 45
  },
  "error": {
    "code": null,
    "message": null
  }
}
```

### 4.2 Error Response (Plugin → Parent)

```jsonc
{
  "version": 1,
  "status": "error",
  "command": "plugin/auth",
  "data": {
    "hint": "Missing required credential 'secret_key'"
  },
  "meta": {
    "ts": "2025-11-15T12:34:56Z",
    "duration_ms": 12
  },
  "error": {
    "code": "EAUTH",                      // Use standard error codes
    "message": "Plugin error: missing secret_key",
    "details": {
      "missing_credentials": ["secret_key"]
    }
  }
}
```

---

## 5. Auth Plugin

### 5.1 Purpose

Auth plugins modify HTTP requests to add authentication:
- Inject headers (e.g., `Authorization`, `X-API-Key`, `X-Signature`)
- Sign request body (e.g., AWS SigV4, HMAC)
- Transform URLs (e.g., add query-based auth tokens)

### 5.2 Input (Parent → Auth Plugin)

```json
{
  "version": 1,
  "command": "plugin/auth",
  "data": {
    "request": {
      "method": "POST",
      "url": "https://api.example.com/v1/items",
      "headers": {
        "Content-Type": "application/json",
        "Accept": "application/json"
      },
      "body": "{\"name\":\"test\"}"
    },
    "context": {
      "security_scheme": {
        "type": "apiKey",
        "name": "X-API-Key",
        "in": "header",
        "x-agentctl": {
          "signing": "HMAC-SHA256"
        }
      },
      "credentials": {
        "api_key": "your_api_key_here",
        "secret_key": "your_secret_key_here"
      },
      "spec_hints": {
        "signing_algorithm": "HMAC-SHA256",
        "timestamp_header": "X-Timestamp"
      }
    },
    "limits": {
      "cpu_ms": 200,
      "wall_ms": 500,
      "max_out_kb": 32
    }
  }
}
```

### 5.3 Output (Auth Plugin → Parent)

```json
{
  "version": 1,
  "status": "ok",
  "command": "plugin/auth",
  "data": {
    "headers": {
      "Authorization": "Bearer generated_token",
      "X-API-Key": "your_api_key_here",
      "X-Signature": "hmac_sha256_signature",
      "X-Timestamp": "2025-11-15T12:34:56Z"
    },
    "body": null
  },
  "meta": {
    "ts": "2025-11-15T12:34:56Z",
    "duration_ms": 23
  },
  "error": {
    "code": null,
    "message": null
  }
}
```

### 5.4 Common Auth Patterns

| Pattern | Implementation |
|---------|----------------|
| **AWS SigV4** | Plugin calculates signature, adds `Authorization` header |
| **HMAC** | Plugin signs request body, adds `X-Signature` header |
| **Custom OAuth** | Plugin exchanges credentials for token, returns `Authorization: Bearer` |
| **mTLS** | Plugin returns certificate/key paths in custom fields |

---

## 6. Pagination Plugin

### 6.1 Purpose

Pagination plugins determine:
- Whether to fetch the next page
- How to construct the next request (URL, query params, cursor)
- When pagination is complete

### 6.2 Input (Parent → Pagination Plugin)

```json
{
  "version": 1,
  "command": "plugin/pagination",
  "data": {
    "request": {
      "method": "GET",
      "url": "https://api.example.com/items?page=1",
      "headers": {
        "Accept": "application/json"
      },
      "body": null
    },
    "context": {
      "paging_state": {
        "page": 1,
        "total_items": 100
      },
      "response": {
        "status": 200,
        "headers": {
          "Content-Type": "application/json",
          "X-Total-Count": "247"
        },
        "body": {
          "items": [
            {"id": 1, "name": "item1"},
            {"id": 2, "name": "item2"}
          ],
          "meta": {
            "next_cursor": "cursor_abc123",
            "has_more": true
          }
        }
      },
      "spec_hints": {
        "cursor_field": "meta.next_cursor",
        "max_items": 500
      }
    },
    "limits": {
      "cpu_ms": 200,
      "wall_ms": 500,
      "max_out_kb": 32
    }
  }
}
```

### 6.3 Output (Pagination Plugin → Parent)

```json
{
  "version": 1,
  "status": "ok",
  "command": "plugin/pagination",
  "data": {
    "continue": true,
    "next_url": null,
    "next_query": {
      "cursor": "cursor_abc123"
    },
    "next_cursor": "cursor_abc123",
    "items_in_page": 100
  },
  "meta": {
    "ts": "2025-11-15T12:34:56Z",
    "duration_ms": 18
  },
  "error": {
    "code": null,
    "message": null
  }
}
```

### 6.4 Pagination Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `continue` | boolean | **Required**. Whether to fetch next page |
| `next_url` | string | Complete URL for next request (exclusive with `next_query`) |
| `next_query` | object | Query parameters to merge into next request |
| `next_cursor` | string | Cursor value to store in `paging_state` |
| `items_in_page` | integer | Number of items in current page (optional) |

### 6.5 Termination Conditions

Pagination stops when:
1. Plugin returns `continue: false`
2. `max_pages` limit reached (configured by caller)
3. `max_items` limit reached (configured by caller)
4. Response is empty or has fewer items than page size

---

## 7. Discovery & Environment

### 7.1 Plugin Discovery

Plugins are discovered via:
1. **Environment variable**: `AGENTCTL_PLUGIN_PATH` (colon-separated paths)
2. **Default path**: `~/.agentctl/plugins`
3. **Explicit path**: `--auth.scheme=plugin:/absolute/path/to/plugin`

### 7.2 Naming Convention

Plugins MUST be named: `agentctl-plugin-<name>`

Examples:
- `agentctl-plugin-aws-sigv4`
- `agentctl-plugin-stripe-pagination`
- `agentctl-plugin-hmac-auth`

### 7.3 Plugin Invocation

Plugins are referenced by name:
```bash
# Auth plugin
agentctl run http/openapi \
  --spec memory:api \
  --operationId createItem \
  --auth.scheme=plugin:aws-sigv4

# Pagination plugin
agentctl run http/openapi \
  --spec memory:api \
  --operationId listItems \
  --paging.strategy=plugin:custom-cursor
```

### 7.4 Handshake (Capability Discovery)

When invoked with `--handshake` flag, plugins MUST output capabilities:

```bash
$ agentctl-plugin-aws-sigv4 --handshake
```

Output:
```json
{
  "name": "aws-sigv4",
  "version": "1.0.0",
  "commands": ["plugin/auth"],
  "protocols": ["core/v1"],
  "limits": {
    "max_in_kb": 128,
    "max_out_kb": 32,
    "cpu_ms": 200,
    "wall_ms": 500
  },
  "description": "AWS Signature Version 4 authentication"
}
```

**Note**: Plugin self-reported limits are advisory. Parent always enforces its own hard limits.

---

## 8. Security & Limits

### 8.1 Hard Limits (Parent-Enforced)

The parent process MUST enforce these limits regardless of plugin manifest:

| Limit | Default | Max |
|-------|---------|-----|
| Wall time | 500ms | 5s |
| CPU time | 200ms | 2s |
| Memory | 64MB | 256MB |
| Input size | 128KB | 1MB |
| Output size | 32KB | 128KB |

### 8.2 Network Isolation

Plugins SHOULD NOT make network calls. If network access is required:
1. Plugin MUST document this in manifest
2. Parent MAY enforce network isolation via sandbox (future)
3. Plugin SHOULD use localhost or well-known endpoints only

### 8.3 Credential Handling

**Secrets** (in `context.credentials`):
- Parent MUST redact credentials in all logs
- Parent MUST NOT write credentials to disk
- Plugins MUST NOT log credentials
- Plugins MUST clear sensitive data from memory after use

### 8.4 Filesystem Access

Plugins SHOULD NOT write to filesystem except:
- Temporary directories (e.g., `/tmp`, `$TMPDIR`)
- Temp files MUST be cleaned up on exit
- Parent MAY enforce read-only filesystem (future)

### 8.5 Resource Cleanup

Parent MUST:
- Kill plugin process if timeout exceeded
- Clean up stdin/stdout pipes
- Release resources on plugin exit
- Log resource usage for debugging

---

## 9. Error Handling

### 9.1 Plugin Error Codes

Plugins SHOULD use standard error codes from the Protocol v1 catalog:

| Code | Use Case |
|------|----------|
| `EAUTH` | Missing/invalid credentials |
| `EARG` | Invalid request format or missing required fields |
| `EPAGINATION` | Cannot determine next page |
| `ETIMEOUT` | Plugin internal timeout |
| `ERUNTIME` | Plugin logic error |

### 9.2 Parent Error Handling

If plugin fails, parent MUST:
1. Capture stderr output
2. Return error envelope with:
   - `error.code`: Appropriate code (e.g., `EAUTH`, `EPAGINATION`)
   - `error.message`: Error from plugin or timeout message
   - `error.details.plugin_stderr`: Captured stderr (bounded)
   - `data.hint`: Actionable remediation

Example:
```json
{
  "version": 1,
  "status": "error",
  "command": "http/openapi",
  "data": {
    "hint": "Auth plugin 'aws-sigv4' failed. Check credentials and plugin logs."
  },
  "meta": {
    "ts": "2025-11-15T12:34:56Z",
    "duration_ms": 523
  },
  "error": {
    "code": "EAUTH",
    "message": "Plugin error: missing AWS credentials",
    "details": {
      "plugin_name": "aws-sigv4",
      "plugin_stderr": "Error: AWS_ACCESS_KEY_ID not found",
      "exit_code": 1
    }
  }
}
```

### 9.3 Timeout Handling

If plugin exceeds `wall_ms`:
- Parent MUST kill plugin process (SIGTERM, then SIGKILL)
- Return error with `code: "ETIMEOUT"`
- Include partial output if any (up to `max_out_kb`)

---

## 10. Examples

### 10.1 AWS SigV4 Auth Plugin (Python)

```python
#!/usr/bin/env python3
import sys
import json
import hashlib
import hmac
from datetime import datetime

def sign_request(request, credentials):
    """AWS Signature Version 4 signing"""
    timestamp = datetime.utcnow().strftime('%Y%m%dT%H%M%SZ')
    date_stamp = timestamp[:8]

    # Canonical request
    canonical_uri = request['url'].split('?')[0]
    canonical_headers = f"host:{request['url'].split('/')[2]}\n"
    signed_headers = "host"
    payload_hash = hashlib.sha256(request.get('body', '').encode()).hexdigest()

    canonical_request = f"{request['method']}\n{canonical_uri}\n\n{canonical_headers}\n{signed_headers}\n{payload_hash}"

    # String to sign
    algorithm = 'AWS4-HMAC-SHA256'
    credential_scope = f"{date_stamp}/us-east-1/execute-api/aws4_request"
    string_to_sign = f"{algorithm}\n{timestamp}\n{credential_scope}\n{hashlib.sha256(canonical_request.encode()).hexdigest()}"

    # Signing key
    k_secret = ('AWS4' + credentials['secret_access_key']).encode()
    k_date = hmac.new(k_secret, date_stamp.encode(), hashlib.sha256).digest()
    k_region = hmac.new(k_date, 'us-east-1'.encode(), hashlib.sha256).digest()
    k_service = hmac.new(k_region, 'execute-api'.encode(), hashlib.sha256).digest()
    signing_key = hmac.new(k_service, 'aws4_request'.encode(), hashlib.sha256).digest()

    # Signature
    signature = hmac.new(signing_key, string_to_sign.encode(), hashlib.sha256).hexdigest()

    # Authorization header
    authorization = f"{algorithm} Credential={credentials['access_key_id']}/{credential_scope}, SignedHeaders={signed_headers}, Signature={signature}"

    return {
        "Authorization": authorization,
        "X-Amz-Date": timestamp
    }

def main():
    # Read request from stdin
    request_env = json.load(sys.stdin)

    try:
        request = request_env['data']['request']
        credentials = request_env['data']['context']['credentials']

        # Generate signature
        auth_headers = sign_request(request, credentials)

        # Return success response
        response = {
            "version": 1,
            "status": "ok",
            "command": "plugin/auth",
            "data": {
                "headers": auth_headers,
                "body": null
            },
            "meta": {
                "ts": datetime.utcnow().isoformat() + "Z",
                "duration_ms": 0
            },
            "error": {
                "code": null,
                "message": null
            }
        }

        json.dump(response, sys.stdout, indent=2)
        sys.exit(0)

    except KeyError as e:
        # Return error response
        response = {
            "version": 1,
            "status": "error",
            "command": "plugin/auth",
            "data": {
                "hint": f"Missing required field: {e}"
            },
            "meta": {
                "ts": datetime.utcnow().isoformat() + "Z",
                "duration_ms": 0
            },
            "error": {
                "code": "EAUTH",
                "message": f"Plugin error: missing {e}",
                "details": {}
            }
        }

        json.dump(response, sys.stdout, indent=2)
        sys.exit(1)

if __name__ == '__main__':
    main()
```

### 10.2 Cursor Pagination Plugin (Go)

```go
package main

import (
    "encoding/json"
    "fmt"
    "os"
    "time"
)

type Request struct {
    Version int    `json:"version"`
    Command string `json:"command"`
    Data    struct {
        Context struct {
            Response struct {
                Body map[string]interface{} `json:"body"`
            } `json:"response"`
            SpecHints struct {
                CursorField string `json:"cursor_field"`
                MaxItems    int    `json:"max_items"`
            } `json:"spec_hints"`
            PagingState struct {
                TotalItems int `json:"total_items"`
            } `json:"paging_state"`
        } `json:"context"`
    } `json:"data"`
}

type Response struct {
    Version int    `json:"version"`
    Status  string `json:"status"`
    Command string `json:"command"`
    Data    struct {
        Continue     bool              `json:"continue"`
        NextURL      interface{}       `json:"next_url"`
        NextQuery    map[string]string `json:"next_query,omitempty"`
        NextCursor   string            `json:"next_cursor,omitempty"`
        ItemsInPage  int               `json:"items_in_page,omitempty"`
    } `json:"data"`
    Meta struct {
        TS         string `json:"ts"`
        DurationMS int    `json:"duration_ms"`
    } `json:"meta"`
    Error struct {
        Code    interface{} `json:"code"`
        Message interface{} `json:"message"`
    } `json:"error"`
}

func main() {
    var req Request
    if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
        fmt.Fprintf(os.Stderr, "Failed to decode request: %v\n", err)
        os.Exit(1)
    }

    resp := Response{
        Version: 1,
        Status:  "ok",
        Command: "plugin/pagination",
    }
    resp.Meta.TS = time.Now().UTC().Format(time.RFC3339)

    // Extract cursor from response body
    body := req.Data.Context.Response.Body
    cursorField := req.Data.Context.SpecHints.CursorField
    maxItems := req.Data.Context.SpecHints.MaxItems

    // Navigate nested cursor field (e.g., "meta.next_cursor")
    cursor := ""
    if meta, ok := body["meta"].(map[string]interface{}); ok {
        if c, ok := meta["next_cursor"].(string); ok {
            cursor = c
        }
    }

    // Check if we should continue
    hasMore := cursor != ""
    totalItems := req.Data.Context.PagingState.TotalItems

    if items, ok := body["items"].([]interface{}); ok {
        totalItems += len(items)
        resp.Data.ItemsInPage = len(items)
    }

    if maxItems > 0 && totalItems >= maxItems {
        hasMore = false
    }

    resp.Data.Continue = hasMore
    if hasMore {
        resp.Data.NextQuery = map[string]string{"cursor": cursor}
        resp.Data.NextCursor = cursor
    }

    if err := json.NewEncoder(os.Stdout).Encode(&resp); err != nil {
        fmt.Fprintf(os.Stderr, "Failed to encode response: %v\n", err)
        os.Exit(1)
    }
}
```

---

## Appendix A: Plugin Manifest (Future)

In future versions, plugins MAY include a manifest file:

```yaml
# agentctl-plugin-aws-sigv4.yaml
name: aws-sigv4
version: 1.0.0
commands:
  - plugin/auth
protocols:
  - core/v1
limits:
  max_in_kb: 128
  max_out_kb: 32
  cpu_ms: 200
  wall_ms: 500
description: AWS Signature Version 4 authentication
author: example@example.com
license: Apache-2.0
dependencies:
  - python3
  - python3-boto3
```

---

## Appendix B: Testing Plugins

```bash
# Test auth plugin with sample request
echo '{
  "version": 1,
  "command": "plugin/auth",
  "data": {
    "request": {
      "method": "GET",
      "url": "https://api.example.com/items",
      "headers": {"Accept": "application/json"},
      "body": null
    },
    "context": {
      "credentials": {"api_key": "test_key"},
      "security_scheme": {"type": "apiKey", "in": "header", "name": "X-API-Key"}
    },
    "limits": {"cpu_ms": 200, "wall_ms": 500, "max_out_kb": 32}
  }
}' | agentctl-plugin-custom-auth | jq

# Test pagination plugin
echo '{
  "version": 1,
  "command": "plugin/pagination",
  "data": {
    "context": {
      "response": {
        "status": 200,
        "body": {"items": [1, 2, 3], "meta": {"next_cursor": "abc123"}}
      },
      "spec_hints": {"cursor_field": "meta.next_cursor", "max_items": 100}
    },
    "limits": {"cpu_ms": 200, "wall_ms": 500, "max_out_kb": 32}
  }
}' | agentctl-plugin-custom-pagination | jq
```

---

**Document Status**: Final Draft
**Related Specs**: Protocol v1, OpenAPI Skill, Core Profile v1
