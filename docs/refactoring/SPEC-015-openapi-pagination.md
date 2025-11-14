# SPEC-015: OpenAPI Pagination Support

## Status
**Not Started** | Priority: High | Complexity: Medium

## Problem Statement

Many APIs return paginated results. The OpenAPI skill needs automatic pagination with strategies:
- **Link headers** (RFC 5988): `Link: <url>; rel="next"`
- **Cursor-based**: `{"next_cursor": "abc123", "data": [...]}`
- **Offset/limit**: `?offset=100&limit=50` with total count
- **Custom**: Plugin-based for vendor-specific formats

## Proposed Solution

```go
// internal/openapi/pagination/paginator.go
package pagination

type Strategy string

const (
    StrategyNone   Strategy = "none"
    StrategyLink   Strategy = "link"       // RFC 5988 Link headers
    StrategyCursor Strategy = "cursor"     // next_cursor/prev_cursor fields
    StrategyOffset Strategy = "offset"     // offset/limit with total
    StrategyPlugin Strategy = "plugin"     // Custom plugin
)

type Config struct {
    Strategy      Strategy `json:"strategy"`
    MaxPages      int      `json:"max_pages"`       // Default: 10
    MaxRecords    int      `json:"max_records"`     // Default: 1000
    CursorField   string   `json:"cursor_field"`    // For cursor strategy
    OffsetParam   string   `json:"offset_param"`    // Default: "offset"
    LimitParam    string   `json:"limit_param"`     // Default: "limit"
    TotalField    string   `json:"total_field"`     // For offset strategy
}

type Paginator struct {
    config    Config
    collected int
    pages     int
}

func (p *Paginator) ShouldContinue(resp *Response) (nextReq *http.Request, done bool) {
    // Implementation...
}
```

## Implementation Plan

1. **Link header parser** (2h)
2. **Cursor extractor** (2h)
3. **Offset calculator** (2h)
4. **Aggregation logic** (2h)
5. **Tests** (2h)

## Effort Estimate
**Total: 10 hours**

## Dependencies
- **Depends on:** SPEC-014 (HTTP Client)
- **Required by:** Complete OpenAPI skill
