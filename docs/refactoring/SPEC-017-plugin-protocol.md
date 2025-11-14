# SPEC-017: Plugin Protocol Implementation

## Status
**Not Started** | Priority: Medium | Complexity: High

## Problem Statement

The plugin protocol spec is a 14-line stub. Need full implementation for auth and pagination plugins.

## Proposed Solution

### Plugin Envelope Format

```json
{
  "version": 1,
  "command": "plugin/auth",
  "data": {
    "scheme": "hmac",
    "config": {...},
    "request": {
      "method": "GET",
      "url": "https://api.example.com/data",
      "headers": {}
    }
  }
}
```

### Plugin Response

```json
{
  "version": 1,
  "command": "plugin/auth",
  "status": "ok",
  "data": {
    "headers": {
      "Authorization": "HMAC ...",
      "X-Signature": "..."
    }
  }
}
```

## Implementation Plan

1. **Protocol definition** (3h)
2. **Plugin discovery** (2h)
3. **Subprocess executor** (4h)
4. **Timeout/cancellation** (2h)
5. **Example plugins** (5h)
   - auth-hmac
   - paging-custom
6. **Tests** (4h)

## Effort Estimate
**Total: 20 hours**

## Dependencies
- **Depends on:** SPEC-012, 013, 014 (OpenAPI core)
- **Optional for:** v1.0 (can defer to v1.1)
