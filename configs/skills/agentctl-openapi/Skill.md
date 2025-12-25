---
name: agentctl OpenAPI
description: HTTP request planner using OpenAPI specs. Build and validate API requests with dry-run mode.
---

# OpenAPI Request Planner

Plan and validate HTTP requests against OpenAPI specifications.

## Usage

```bash
agentctl run http/openapi --input '{
  "base_url": "https://api.example.com",
  "path": "/users/123",
  "method": "GET"
}'
```

## Parameters

| Parameter  | Type    | Required | Default | Description                 |
| ---------- | ------- | -------- | ------- | --------------------------- |
| `base_url` | string  | Yes      | -       | API base URL                |
| `path`     | string  | Yes      | -       | API endpoint path           |
| `method`   | string  | No       | `GET`   | HTTP method                 |
| `query`    | object  | No       | -       | Query parameters            |
| `headers`  | object  | No       | -       | Request headers             |
| `body`     | object  | No       | -       | Request body (for POST/PUT) |
| `dry_run`  | boolean | No       | `true`  | Plan only, don't execute    |

## Examples

### GET Request with Query Params

```bash
agentctl run http/openapi --input '{
  "base_url": "https://api.github.com",
  "path": "/repos/owner/repo/issues",
  "method": "GET",
  "query": {
    "state": "open",
    "labels": "bug",
    "per_page": 10
  },
  "headers": {
    "Accept": "application/vnd.github.v3+json"
  }
}'
```

### POST Request with Body

```bash
agentctl run http/openapi --input '{
  "base_url": "https://api.example.com",
  "path": "/users",
  "method": "POST",
  "headers": {
    "Content-Type": "application/json"
  },
  "body": {
    "name": "John Doe",
    "email": "john@example.com"
  }
}'
```

### PUT/PATCH Request

```bash
agentctl run http/openapi --input '{
  "base_url": "https://api.example.com",
  "path": "/users/123",
  "method": "PATCH",
  "body": {
    "status": "active"
  }
}'
```

### DELETE Request

```bash
agentctl run http/openapi --input '{
  "base_url": "https://api.example.com",
  "path": "/users/123",
  "method": "DELETE"
}'
```

## Output

Returns a `request_plan` object containing:

- Fully constructed URL
- Method and headers
- Query string (if applicable)
- Request body (if applicable)
- Validation status

## Use Cases

- **API exploration**: Understand how to call an endpoint
- **Request validation**: Verify parameters before execution
- **Documentation**: Generate request examples
- **Testing**: Build test fixtures for API calls
