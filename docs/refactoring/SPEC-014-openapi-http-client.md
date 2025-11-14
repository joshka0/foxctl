# SPEC-014: OpenAPI HTTP Client & Response Processing

## Status
**Not Started** | Priority: Critical | Complexity: High

## Problem Statement

Execute HTTP requests built by SPEC-013 and process responses according to the Core Profile:
- Small responses (<inline_output_kb) → inline in envelope
- Large responses → CAS with summary
- Include status code, headers, timing
- Generate preview for large responses
- Error handling (4xx, 5xx)

## Proposed Solution

### HTTP Client with Response Processing

```go
// internal/openapi/client/client.go
package client

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"

    "github.com/jkatigb/agentctl/internal/storage"
    "github.com/jkatigb/agentctl/internal/platform/config"
)

type Client struct {
    http        *http.Client
    cas         storage.CASStore
    inlineLimit int // KB threshold for inline vs CAS
}

type Response struct {
    StatusCode int               `json:"status_code"`
    Headers    map[string]string `json:"headers"`
    Body       interface{}       `json:"body,omitempty"`
    Digest     string            `json:"digest,omitempty"`
    Preview    string            `json:"preview,omitempty"`
    RecordCount int              `json:"record_count,omitempty"`
    Size       int64             `json:"size"`
    Timing     Timing            `json:"timing"`
}

type Timing struct {
    DNS      time.Duration `json:"dns_ms"`
    Connect  time.Duration `json:"connect_ms"`
    TLS      time.Duration `json:"tls_ms"`
    Total    time.Duration `json:"total_ms"`
}

func NewClient(cfg *config.Config, cas storage.CASStore) *Client {
    return &Client{
        http: &http.Client{
            Timeout: 30 * time.Second,
            Transport: &http.Transport{
                MaxIdleConns:    10,
                IdleConnTimeout: 90 * time.Second,
            },
        },
        cas:         cas,
        inlineLimit: cfg.InlineOutputKB,
    }
}

func (c *Client) Execute(ctx context.Context, req *http.Request) (*Response, error) {
    start := time.Now()

    resp, err := c.http.Do(req.WithContext(ctx))
    if err != nil {
        return nil, fmt.Errorf("ERUNTIME: %w", err)
    }
    defer resp.Body.Close()

    timing := Timing{Total: time.Since(start)}

    // Read response body
    bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, int64(cfg.MaxCaptureKB)*1024))
    if err != nil {
        return nil, fmt.Errorf("read response: %w", err)
    }

    // Process response based on size
    response := &Response{
        StatusCode: resp.StatusCode,
        Headers:    sanitizeHeaders(resp.Header),
        Size:       int64(len(bodyBytes)),
        Timing:     timing,
    }

    // For 4xx errors, always inline (bounded by max_capture_kb)
    if resp.StatusCode >= 400 && resp.StatusCode < 500 {
        var errBody interface{}
        if json.Valid(bodyBytes) {
            json.Unmarshal(bodyBytes, &errBody)
        } else {
            errBody = string(bodyBytes)
        }
        response.Body = errBody
        return response, nil
    }

    // For large 2xx/3xx responses, use CAS
    if len(bodyBytes) > c.inlineLimit*1024 {
        digest, size, err := c.cas.Put(ctx, bytes.NewReader(bodyBytes), resp.Header.Get("Content-Type"), nil)
        if err != nil {
            return nil, fmt.Errorf("store response in CAS: %w", err)
        }

        response.Digest = digest
        response.Size = size
        response.Preview = generatePreview(bodyBytes, 5)
        response.RecordCount = countRecords(bodyBytes)
        return response, nil
    }

    // Small response - inline
    var body interface{}
    if json.Valid(bodyBytes) {
        json.Unmarshal(bodyBytes, &body)
    } else {
        body = string(bodyBytes)
    }
    response.Body = body

    return response, nil
}

func sanitizeHeaders(h http.Header) map[string]string {
    result := make(map[string]string)
    for k, v := range h {
        if !isSensitiveHeader(k) {
            result[k] = strings.Join(v, ", ")
        }
    }
    return result
}

func generatePreview(data []byte, maxRecords int) string {
    // Parse JSON array and return first N records
    var arr []interface{}
    if err := json.Unmarshal(data, &arr); err == nil {
        if len(arr) > maxRecords {
            arr = arr[:maxRecords]
        }
        preview, _ := json.Marshal(arr)
        return string(preview) + "..."
    }
    // For non-array, truncate to 500 chars
    if len(data) > 500 {
        return string(data[:500]) + "..."
    }
    return string(data)
}
```

## Implementation Plan

### Step 1: HTTP Client (6h)
- Configure client (timeouts, pooling)
- Execute requests with context
- Collect timing metrics
- Handle redirects

### Step 2: Response Processing (5h)
- Size-based routing (inline vs CAS)
- Preview generation for arrays/objects
- Record counting heuristics
- Header sanitization

### Step 3: Error Handling (2h)
- 4xx errors (inline, actionable)
- 5xx errors (ERUNTIME with retry hint)
- Network errors (ECONNECTION)
- Timeout errors (ETIMEOUT)

### Step 4: Integration Tests (2h)
- End-to-end API calls
- Response size scenarios
- Error scenarios

## Effort Estimate
**Total: 15 hours**

## Dependencies
- **Depends on:** SPEC-012 (Loader), SPEC-013 (Request Builder)
- **Required by:** SPEC-015 (Pagination), SPEC-016 (Retry)

## Success Criteria
- ✅ Execute real HTTP requests
- ✅ Small responses inline
- ✅ Large responses → CAS with preview
- ✅ 4xx errors inline
- ✅ Timing metrics collected
- ✅ Headers sanitized
