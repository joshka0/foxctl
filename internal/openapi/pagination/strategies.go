package pagination

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// advanceLink implements link header-based pagination (RFC 8288).
// Extracts the "next" URL from Link header and creates the next request.
func (p *Paginator) advanceLink(resp *Response) (*http.Request, bool, error) {
	if resp.Headers == nil {
		return nil, false, nil
	}
	nextURL, ok := parseNextLink(resp.Headers.Get("Link"))
	if !ok || nextURL == "" {
		return nil, false, nil
	}
	req := cloneRequest(resp.Request)
	if req == nil {
		return nil, false, errMissingRequest
	}
	parsed, err := url.Parse(nextURL)
	if err != nil {
		return nil, false, err
	}
	if !parsed.IsAbs() {
		parsed = req.URL.ResolveReference(parsed)
	}
	req.URL = parsed
	req.Host = parsed.Host
	req.RequestURI = ""
	return req, true, nil
}

// advanceCursor implements cursor/token-based pagination.
// Extracts cursor from response and adds it to the next request's query parameters.
func (p *Paginator) advanceCursor(resp *Response) (*http.Request, bool, error) {
	cursor, ok := p.extractCursor(resp)
	if !ok || cursor == "" {
		return nil, false, nil
	}

	if hasMore, ok := resp.HasMoreFlag(); ok && !hasMore {
		return nil, false, nil
	}

	req := cloneRequest(resp.Request)
	if req == nil {
		return nil, false, errMissingRequest
	}

	param := p.config.CursorParam
	if param == "" {
		param = detectCursorParam(req.URL.Query())
	}
	if param == "" {
		param = "cursor"
	}

	q := req.URL.Query()
	q.Set(param, cursor)
	p.applyRecordLimit(&q)
	req.URL.RawQuery = q.Encode()
	p.lastCursor = cursor
	return req, true, nil
}

// advanceOffset implements offset/limit or page/per_page pagination.
// Increments the offset or page number for the next request.
func (p *Paginator) advanceOffset(resp *Response) (*http.Request, bool, error) {
	req := cloneRequest(resp.Request)
	if req == nil {
		return nil, false, errMissingRequest
	}

	// Auto-detect pagination parameters if not configured
	if p.offsetParam == "" && p.pageParam == "" {
		off, lim, page, per := detectOffsetParams(req, p.config)
		p.offsetParam = off
		p.limitParam = lim
		p.pageParam = page
		p.perPageParam = per
	}

	q := req.URL.Query()
	items := resp.Items()

	// Handle page/per_page style pagination
	if p.pageParam != "" {
		if p.pageNumber == 0 {
			p.pageNumber = parsePositiveInt(q.Get(p.pageParam))
			if p.pageNumber == 0 {
				p.pageNumber = 1
			}
		}
		if p.pageSize == 0 {
			p.pageSize = pageSizeFromQuery(q, p.perPageParam, items)
		}
		if p.pageSize == 0 {
			return nil, false, nil
		}
		// Check if this is the last page (fewer items than page size)
		if items < p.pageSize {
			hasMore, ok := resp.HasMoreFlag()
			if !ok || !hasMore {
				return nil, false, nil
			}
			// Continue despite shorter page if has_more is true
		}
		p.pageNumber++
		q.Set(p.pageParam, strconv.Itoa(p.pageNumber))
		if p.perPageParam != "" {
			limit := p.pageSize
			if remaining := p.remainingRecords(); remaining > 0 && remaining < limit {
				limit = remaining
			}
			if limit > 0 {
				q.Set(p.perPageParam, strconv.Itoa(limit))
			}
		}
		req.URL.RawQuery = q.Encode()
		return req, true, nil
	}

	// Handle offset/limit style pagination
	if p.offsetParam == "" {
		return nil, false, nil
	}

	if p.pageSize == 0 {
		p.pageSize = pageSizeFromQuery(q, chooseParam(p.limitParam, p.perPageParam), items)
	}
	if p.pageSize == 0 {
		return nil, false, nil
	}

	if p.currentOffset == 0 {
		p.currentOffset = parsePositiveInt(q.Get(p.offsetParam))
	}
	p.currentOffset += p.pageSize

	// Check if we've reached the end
	if items < p.pageSize {
		total, ok := resp.TotalCount(p.totalField)
		if ok {
			p.totalRecords = total
		}
		if p.totalRecords > 0 && p.currentOffset >= p.totalRecords {
			return nil, false, nil
		}
		hasMore, ok := resp.HasMoreFlag()
		if !ok || !hasMore {
			return nil, false, nil
		}
		// Continue if has_more flag indicates more data
	}

	q.Set(p.offsetParam, strconv.Itoa(p.currentOffset))
	if p.limitParam != "" {
		limit := p.pageSize
		if remaining := p.remainingRecords(); remaining > 0 && remaining < limit {
			limit = remaining
		}
		if limit > 0 {
			q.Set(p.limitParam, strconv.Itoa(limit))
		}
	}
	req.URL.RawQuery = q.Encode()
	return req, true, nil
}

// remainingRecords calculates how many more records we can collect based on MaxRecords limit.
func (p *Paginator) remainingRecords() int {
	if p.config.MaxRecords <= 0 {
		return 0
	}
	remaining := p.config.MaxRecords - p.collected
	if remaining < 0 {
		remaining = 0
	}
	return remaining
}

// applyRecordLimit adjusts the limit parameter in the query to respect MaxRecords.
func (p *Paginator) applyRecordLimit(q *url.Values) {
	if q == nil || p.config.MaxRecords <= 0 {
		return
	}
	remaining := p.config.MaxRecords - p.collected
	if remaining <= 0 {
		return
	}
	for _, name := range []string{p.limitParam, p.perPageParam} {
		if name == "" {
			continue
		}
		if q.Has(name) {
			current := parsePositiveInt(q.Get(name))
			if current == 0 || remaining < current {
				q.Set(name, strconv.Itoa(remaining))
			}
		}
	}
}

// extractCursor attempts to extract a cursor/token from the response.
func (p *Paginator) extractCursor(resp *Response) (string, bool) {
	data := resp.json()
	if data == nil {
		return "", false
	}
	if p.config.CursorField != "" {
		if val, ok := extractString(data, strings.Split(p.config.CursorField, ".")); ok {
			return val, true
		}
	}
	if val, ok := extractString(data, []string{"next_cursor"}); ok {
		return val, true
	}
	if val, ok := extractString(data, []string{"next_page_token"}); ok {
		return val, true
	}
	// Try nested cursor field names
	for _, candidate := range []string{"nextToken", "pagination.next", "pagination.next_cursor", "pagination.nextToken"} {
		if val, ok := extractString(data, strings.Split(candidate, ".")); ok {
			return val, true
		}
	}
	return "", false
}
