# SPEC-013: OpenAPI Request Builder

## Status
**Not Started** | Priority: Critical | Complexity: High

## Problem Statement

After loading an OpenAPI spec (SPEC-012), we need to build HTTP requests based on operation definitions. This involves:
- Resolving path parameters (`/users/{userId}`)
- Applying query parameters
- Setting headers (including auth)
- Serializing request body (JSON, form-data, etc.)
- Content-Type negotiation

### Current State
Skills/http_openapi/main.go only builds basic dry-run requests without parameter validation.

## Proposed Solution

### Architecture

```go
// internal/openapi/request/builder.go
package request

import (
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"
    "net/url"
    "strings"

    "github.com/jkatigb/agentctl/internal/openapi/loader"
)

type Builder struct {
    baseURL string
    spec    *loader.Spec
}

type Parameters struct {
    Path   map[string]interface{} `json:"path"`
    Query  map[string]interface{} `json:"query"`
    Header map[string]string      `json:"header"`
    Body   interface{}            `json:"body"`
}

func NewBuilder(baseURL string, spec *loader.Spec) *Builder {
    return &Builder{baseURL: baseURL, spec: spec}
}

func (b *Builder) Build(operationID string, params Parameters) (*http.Request, error) {
    op, err := b.spec.GetOperation(operationID)
    if err != nil {
        return nil, err
    }

    // 1. Validate parameters
    if err := b.validateParameters(op, params); err != nil {
        return nil, fmt.Errorf("EARG: %w", err)
    }

    // 2. Build URL with path params
    path, err := b.resolvePath(op.Path, params.Path)
    if err != nil {
        return nil, err
    }

    fullURL, err := url.Parse(b.baseURL)
    if err != nil {
        return nil, fmt.Errorf("invalid base URL: %w", err)
    }
    fullURL.Path = path

    // 3. Apply query parameters
    q := fullURL.Query()
    for k, v := range params.Query {
        q.Set(k, fmt.Sprint(v))
    }
    fullURL.RawQuery = q.Encode()

    // 4. Serialize body
    var bodyReader io.Reader
    contentType := "application/json"
    if params.Body != nil {
        bodyBytes, err := json.Marshal(params.Body)
        if err != nil {
            return nil, fmt.Errorf("serialize body: %w", err)
        }
        bodyReader = bytes.NewReader(bodyBytes)
    }

    // 5. Create request
    req, err := http.NewRequest(op.Method, fullURL.String(), bodyReader)
    if err != nil {
        return nil, err
    }

    // 6. Apply headers
    for k, v := range params.Header {
        req.Header.Set(k, v)
    }
    if params.Body != nil {
        req.Header.Set("Content-Type", contentType)
    }

    return req, nil
}

func (b *Builder) validateParameters(op *loader.Operation, params Parameters) error {
    // Validate required path parameters
    for _, paramRef := range op.Parameters {
        if paramRef.Value.In == "path" && paramRef.Value.Required {
            if _, ok := params.Path[paramRef.Value.Name]; !ok {
                return fmt.Errorf("missing required path parameter: %s", paramRef.Value.Name)
            }
        }
        if paramRef.Value.In == "query" && paramRef.Value.Required {
            if _, ok := params.Query[paramRef.Value.Name]; !ok {
                return fmt.Errorf("missing required query parameter: %s", paramRef.Value.Name)
            }
        }
    }

    // Validate request body if required
    if op.RequestBody != nil && op.RequestBody.Value.Required && params.Body == nil {
        return fmt.Errorf("missing required request body")
    }

    return nil
}

func (b *Builder) resolvePath(pathTemplate string, params map[string]interface{}) (string, error) {
    result := pathTemplate
    for k, v := range params {
        placeholder := fmt.Sprintf("{%s}", k)
        value := url.PathEscape(fmt.Sprint(v))
        result = strings.ReplaceAll(result, placeholder, value)
    }

    // Check for unresolved placeholders
    if strings.Contains(result, "{") {
        return "", fmt.Errorf("unresolved path parameters in: %s", result)
    }

    return result, nil
}
```

## Implementation Plan

### Step 1: Core Builder (5h)
- Parameter validation against OpenAPI schema
- Path template resolution
- Query parameter encoding
- Header application
- Body serialization (JSON, form-data, multipart)

### Step 2: Tests (3h)
- Parameter validation tests
- Path resolution tests
- Content-Type negotiation tests
- Error cases (missing required params)

### Step 3: Dry-run Integration (2h)
- Update http/openapi skill to use builder
- Enhance dry-run output with validated request

### Step 4: Documentation (2h)
- Parameter format examples
- Error message guide
- Schema validation details

## Effort Estimate
**Total: 12 hours**

## Dependencies
- **Depends on:** SPEC-012 (Spec Loader) ✅
- **Required by:** SPEC-014 (HTTP Client)

## Success Criteria
- ✅ Validate all parameter types (path, query, header, body)
- ✅ Resolve path templates correctly
- ✅ Handle content-type negotiation
- ✅ Actionable error messages (EARG with hints)
- ✅ 85%+ test coverage
