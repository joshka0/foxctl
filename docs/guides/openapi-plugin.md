# OpenAPI Plugin System Guide

**Version:** 1.0.0
**Last Updated:** 2025-11-18

This guide explains how to extend the OpenAPI skill with custom authentication and pagination strategies using the plugin system.

## Overview

The OpenAPI skill supports plugins for:
- **Custom Authentication** - Implement non-standard auth schemes (AWS SigV4, HMAC, etc.)
- **Custom Pagination** - Handle vendor-specific pagination logic

Plugins communicate via JSON envelopes on stdin/stdout.

## Plugin Discovery

Plugins are discovered from:
1. `AGENTCTL_PLUGIN_PATH` environment variable (colon-separated paths)
2. `~/.agentctl/plugins` (default)

**Naming Convention:** `agentctl-plugin-<name>`

Example:
```
~/.agentctl/plugins/
├── agentctl-plugin-aws-sigv4
├── agentctl-plugin-hmac-auth
└── agentctl-plugin-custom-paging
```

## Authentication Plugins

### Interface

**Command:** `plugin/auth`

**Input Envelope:**
```json
{
  "version": 1,
  "command": "plugin/auth",
  "data": {
    "request": {
      "method": "GET",
      "url": "https://api.example.com/resource",
      "headers": {"Host": "api.example.com"},
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
        "x-agentctl": {
          "auth": "plugin:aws-sigv4",
          "auth_config": {
            "service": "s3",
            "region": "us-east-1"
          }
        }
      }
    }
  }
}
```

**Output Envelope (Success):**
```json
{
  "version": 1,
  "status": "ok",
  "command": "plugin/auth",
  "data": {
    "headers": {
      "Authorization": "AWS4-HMAC-SHA256 Credential=...",
      "X-Amz-Date": "20251118T103000Z"
    },
    "body": null
  },
  "error": {"code": null, "message": null}
}
```

**Output Envelope (Error):**
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

### Example: HMAC Authentication Plugin

**File:** `~/.agentctl/plugins/agentctl-plugin-hmac-auth`

```python
#!/usr/bin/env python3
import json
import sys
import hmac
import hashlib
import base64
from datetime import datetime

def sign_hmac(request, api_key, secret_key):
    """Sign request with HMAC-SHA256."""
    method = request["method"]
    url = request["url"]
    timestamp = datetime.utcnow().strftime('%Y-%m-%dT%H:%M:%SZ')

    # Create string to sign
    string_to_sign = f"{method}\n{url}\n{timestamp}"

    # Generate signature
    signature = hmac.new(
        secret_key.encode(),
        string_to_sign.encode(),
        hashlib.sha256
    ).hexdigest()

    # Return headers
    return {
        "X-API-Key": api_key,
        "X-Timestamp": timestamp,
        "X-Signature": signature
    }

def main():
    try:
        # Read input envelope
        input_data = json.load(sys.stdin)
        request = input_data["data"]["request"]
        context = input_data["data"]["context"]
        credentials = context["credentials"]

        # Extract credentials
        api_key = credentials.get("api_key", "")
        secret_key = credentials.get("secret_key", "")

        if not api_key or not secret_key:
            raise ValueError("Missing api_key or secret_key")

        # Sign request
        signed_headers = sign_hmac(request, api_key, secret_key)

        # Return success
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
        # Return error
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

**Make executable:**
```bash
chmod +x ~/.agentctl/plugins/agentctl-plugin-hmac-auth
```

### Usage

**OpenAPI Spec Hint:**
```yaml
x-agentctl:
  auth: plugin:hmac-auth
  auth_config:
    # Plugin-specific configuration
```

**Skill Input:**
```json
{
  "spec": "memory:api",
  "operationId": "getData",
  "auth": {
    "type": "plugin:hmac-auth",
    "api_key": "key123",
    "secret_key": "secret456"
  }
}
```

**Environment Variables:**
```bash
export AGENTCTL_PLUGIN_PATH=~/.agentctl/plugins
export AGENTCTL_API_KEY="key123"
export AGENTCTL_SECRET_KEY="secret456"
```

## Pagination Plugins

### Interface

**Command:** `plugin/pagination`

**Input Envelope:**
```json
{
  "version": 1,
  "command": "plugin/pagination",
  "data": {
    "last_response": {
      "status": 200,
      "headers": {"content-type": "application/json"},
      "body": {
        "results": [...],
        "meta": {"next_token": "eyJwYWdlIjoy..."}
      }
    },
    "requested_max_items": 1000,
    "items_fetched_so_far": 250
  }
}
```

**Output Envelope (Continue):**
```json
{
  "version": 1,
  "status": "ok",
  "command": "plugin/pagination",
  "data": {
    "continue": true,
    "next_url": null,
    "next_query": {"page_token": "eyJwYWdlIjoy..."},
    "next_cursor": "eyJwYWdlIjoy..."
  },
  "error": {"code": null, "message": null}
}
```

**Output Envelope (Stop):**
```json
{
  "version": 1,
  "status": "ok",
  "command": "plugin/pagination",
  "data": {"continue": false},
  "error": {"code": null, "message": null}
}
```

### Example: Custom Cursor Pagination

**File:** `~/.agentctl/plugins/agentctl-plugin-custom-paging`

```python
#!/usr/bin/env python3
import json
import sys

def extract_cursor(response_body):
    """Extract pagination cursor from custom response format."""
    if not isinstance(response_body, dict):
        return None

    # Custom API might have deeply nested cursor
    metadata = response_body.get("_metadata", {})
    pagination = metadata.get("pagination", {})
    return pagination.get("next_cursor")

def main():
    try:
        input_data = json.load(sys.stdin)
        last_response = input_data["data"]["last_response"]
        max_items = input_data["data"].get("requested_max_items", 0)
        fetched = input_data["data"].get("items_fetched_so_far", 0)

        body = last_response.get("body", {})
        cursor = extract_cursor(body)

        # Check if we should continue
        if not cursor:
            # No more pages
            output_data = {"continue": False}
        elif max_items > 0 and fetched >= max_items:
            # Reached limit
            output_data = {"continue": False}
        else:
            # Continue with next cursor
            output_data = {
                "continue": True,
                "next_query": {"cursor": cursor},
                "next_cursor": cursor
            }

        output = {
            "version": 1,
            "status": "ok",
            "command": "plugin/pagination",
            "data": output_data,
            "error": {"code": None, "message": None}
        }
        json.dump(output, sys.stdout)
        sys.exit(0)

    except Exception as e:
        error_output = {
            "version": 1,
            "status": "error",
            "command": "plugin/pagination",
            "data": {"hint": str(e)},
            "error": {"code": "EPAGINATION", "message": str(e)}
        }
        json.dump(error_output, sys.stdout)
        sys.exit(1)

if __name__ == "__main__":
    main()
```

### Usage

**Skill Input:**
```json
{
  "spec": "memory:api",
  "operationId": "listItems",
  "paging": {
    "strategy": "plugin:custom-paging",
    "max_items": 1000
  }
}
```

## Plugin Security

### Sandboxing

Plugins run with limited environment:
- Read-only access to spec and request data
- No network access to arbitrary hosts
- Limited CPU and wall time (configurable)
- Output size limits

### Runtime Limits

Default limits:
- **Wall timeout:** 30 seconds
- **CPU timeout:** 10 seconds
- **Output size:** 1 MB

Configure via environment:
```bash
export AGENTCTL_PLUGIN_TIMEOUT_WALL=60
export AGENTCTL_PLUGIN_TIMEOUT_CPU=20
export AGENTCTL_PLUGIN_MAX_OUTPUT=2097152
```

### Credential Handling

**Best Practices:**
- ✅ Read credentials from environment variables
- ✅ Never log credentials
- ✅ Use secure credential storage (e.g., system keychain)
- ❌ Don't hardcode credentials in plugins
- ❌ Don't write credentials to disk

## Testing Plugins

### Manual Test

```bash
# Test auth plugin
echo '{
  "version": 1,
  "command": "plugin/auth",
  "data": {
    "request": {"method": "GET", "url": "https://api.example.com/data"},
    "context": {
      "credentials": {"api_key": "test", "secret_key": "test123"}
    }
  }
}' | ~/.agentctl/plugins/agentctl-plugin-hmac-auth | jq
```

### Integration Test

```bash
# Import spec with plugin hint
agentctl openapi import api-with-hmac.yaml --as hmac-api

# Test with plugin
agentctl run http/openapi \
  --spec memory:hmac-api \
  --operationId getData \
  --auth.type plugin:hmac-auth \
  --dry_run
```

## Troubleshooting

### Plugin Not Found

**Error:** `plugin not found: hmac-auth`

**Solution:**
1. Check plugin is in search path: `ls $AGENTCTL_PLUGIN_PATH`
2. Verify naming: Must be `agentctl-plugin-<name>`
3. Check executable permission: `chmod +x plugin-file`

### Plugin Timeout

**Error:** `plugin timeout after 30s`

**Solution:**
1. Optimize plugin performance
2. Increase timeout: `export AGENTCTL_PLUGIN_TIMEOUT_WALL=60`
3. Check for infinite loops

### JSON Parse Error

**Error:** `failed to parse plugin output`

**Solution:**
1. Ensure plugin outputs valid JSON
2. Test plugin manually with echo
3. Check for debug print statements (use stderr, not stdout)

## Best Practices

### Plugin Development

1. **Error Handling:** Always catch exceptions and return proper error envelopes
2. **Logging:** Write debug logs to stderr, not stdout
3. **Performance:** Minimize external dependencies and I/O
4. **Testing:** Test with various inputs and edge cases
5. **Documentation:** Include usage examples in plugin header

### Plugin Distribution

1. **Versioning:** Include version in plugin output metadata
2. **Dependencies:** Document all required dependencies
3. **Installation:** Provide installation script
4. **Updates:** Check for updates on startup

## Examples Repository

More plugin examples available at:
- `plugins/auth-hmac/` - HMAC authentication
- `plugins/paging-custom/` - Custom pagination
- `docs/spec/v1/plugin_protocol.md` - Full plugin protocol spec

## Reference

- [OpenAPI Skill Specification](../spec/openapi_skill.md) - Skill behavior and plugin integration
- [Plugin Protocol](../spec/v1/plugin_protocol.md) - Plugin wire contract
- [Plugin Manager Source](../../internal/openapi/plugin/manager.go)
- [Example Plugins](../../plugins/)
