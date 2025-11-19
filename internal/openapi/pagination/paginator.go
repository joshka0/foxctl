// Package pagination implements pagination logic for OpenAPI HTTP requests.
package pagination

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// Strategy identifies the pagination strategy in use.
type Strategy string

const (
	// StrategyAuto attempts to detect pagination strategy automatically.
	StrategyAuto Strategy = "auto"
	// StrategyNone disables pagination entirely.
	StrategyNone Strategy = "none"
	// StrategyLink uses RFC 5988 Link headers to locate the next page.
	StrategyLink Strategy = "link"
	// StrategyCursor reads a cursor/token value from the response body.
	StrategyCursor Strategy = "cursor"
	// StrategyOffset increments numeric paging parameters such as offset/page.
	StrategyOffset Strategy = "offset"
	// StrategyPlugin delegates pagination to an out-of-process plugin.
	StrategyPlugin Strategy = "plugin"
)

var errMissingRequest = errors.New("pagination: response request is required")

// Config captures pagination configuration provided by the user or spec hints.
type Config struct {
	Strategy     Strategy `json:"strategy"`
	MaxPages     int      `json:"max_pages"`
	MaxRecords   int      `json:"max_records"`
	CursorField  string   `json:"cursor_field"`
	CursorParam  string   `json:"cursor_param"`
	OffsetParam  string   `json:"offset_param"`
	LimitParam   string   `json:"limit_param"`
	PageParam    string   `json:"page_param"`
	PerPageParam string   `json:"per_page_param"`
	TotalField   string   `json:"total_field"`
}

// Summary provides pagination statistics for the collected pages.
type Summary struct {
	Strategy    Strategy
	TotalPages  int
	TotalItems  int
	HasMore     bool
	Truncated   bool
	CursorFinal string
}

// Response represents the HTTP response for a single page.
type Response struct {
	Request *http.Request
	Headers http.Header
	Body    []byte

	// ItemCount, when non-negative, is treated as authoritative.
	ItemCount int

	jsonOnce     sync.Once
	jsonValue    any
	jsonErr      error
	hasItemCount bool
}

// Items returns the number of items contained in the page, inferring from the
// response body when necessary.
func (r *Response) Items() int {
	if r == nil {
		return 0
	}
	if r.hasItemCount {
		if r.ItemCount < 0 {
			return 0
		}
		return r.ItemCount
	}
	count := inferItemCount(r.json())
	r.ItemCount = count
	r.hasItemCount = true
	return count
}

// json returns a cached JSON representation of the response body.
func (r *Response) json() any {
	if r == nil {
		return nil
	}
	r.jsonOnce.Do(func() {
		if len(r.Body) == 0 {
			r.jsonValue = nil
			return
		}
		dec := json.NewDecoder(bytes.NewReader(r.Body))
		dec.UseNumber()
		if err := dec.Decode(&r.jsonValue); err != nil {
			r.jsonErr = err
			r.jsonValue = nil
		}
	})
	return r.jsonValue
}

// HasMoreFlag attempts to find an explicit has-more indicator in the response
// JSON payload. It returns the flag value and whether the flag was present.
func (r *Response) HasMoreFlag() (bool, bool) {
	data := r.json()
	if data == nil {
		return false, false
	}
	switch v := data.(type) {
	case map[string]any:
		if val, ok := extractBool(v, []string{
			"has_more", "hasMore", "has_more_results", "hasMoreResults",
			"has_next", "hasNext", "hasNextPage",
		}); ok {
			return val, true
		}
	}
	return false, false
}

// TotalCount attempts to resolve a total count from the response JSON using
// either the configured total field or common heuristics.
func (r *Response) TotalCount(totalField string) (int, bool) {
	data := r.json()
	if data == nil {
		return 0, false
	}
	if totalField != "" {
		if val, ok := extractInt(data, strings.Split(totalField, ".")); ok {
			return val, true
		}
	}
	switch v := data.(type) {
	case map[string]any:
		if val, ok := extractIntFromMap(v, []string{"total", "total_count", "totalCount", "count"}); ok {
			return val, true
		}
	}
	return 0, false
}

// Paginator orchestrates pagination over HTTP responses.
type Paginator struct {
	config   Config
	strategy Strategy

	pages      int
	collected  int
	hasMore    bool
	truncated  bool
	lastCursor string

	offsetParam  string
	limitParam   string
	pageParam    string
	perPageParam string
	totalField   string

	pageSize      int
	pageNumber    int
	currentOffset int
	totalRecords  int
}

// New creates a new paginator using the provided configuration.
func New(cfg Config) (*Paginator, error) {
	cfg = applyDefaults(cfg)
	if cfg.MaxPages < 0 {
		return nil, fmt.Errorf("pagination: max_pages cannot be negative")
	}
	if cfg.MaxRecords < 0 {
		return nil, fmt.Errorf("pagination: max_records cannot be negative")
	}
	p := &Paginator{
		config:       cfg,
		totalField:   cfg.TotalField,
		offsetParam:  cfg.OffsetParam,
		limitParam:   cfg.LimitParam,
		pageParam:    cfg.PageParam,
		perPageParam: cfg.PerPageParam,
	}
	if cfg.Strategy == StrategyNone {
		p.strategy = StrategyNone
	}
	return p, nil
}

func applyDefaults(cfg Config) Config {
	if cfg.Strategy == "" {
		cfg.Strategy = StrategyAuto
	}
	return cfg
}

// ShouldContinue processes the provided response and determines whether another
// page should be fetched. When the returned done flag is true the caller should
// stop requesting more pages.
func (p *Paginator) ShouldContinue(resp *Response) (*http.Request, bool, error) {
	if resp == nil {
		return nil, true, errors.New("pagination: response is nil")
	}
	if resp.Request == nil && p.config.Strategy != StrategyNone {
		return nil, true, errMissingRequest
	}

	items := resp.Items()
	p.pages++
	p.collected += items

	if p.config.MaxPages > 0 && p.pages >= p.config.MaxPages {
		p.truncated = p.pages == p.config.MaxPages && items > 0
		p.hasMore = false
		return nil, true, nil
	}

	if p.config.MaxRecords > 0 && p.collected >= p.config.MaxRecords {
		if p.collected > p.config.MaxRecords {
			p.collected = p.config.MaxRecords
		}
		p.truncated = true
		p.hasMore = false
		return nil, true, nil
	}

	if items == 0 {
		p.hasMore = false
		return nil, true, nil
	}

	if p.strategy == "" {
		strat, err := p.selectStrategy(resp)
		if err != nil {
			return nil, true, err
		}
		p.strategy = strat
	}

	// Apply strategy-specific pagination logic
	return p.applyStrategy(resp)
}

// applyStrategy applies the selected pagination strategy and returns the next request
func (p *Paginator) applyStrategy(resp *Response) (*http.Request, bool, error) {
	switch p.strategy {
	case StrategyNone, StrategyPlugin:
		// No pagination or plugin-based pagination (handled externally)
		p.hasMore = false
		return nil, true, nil

	case StrategyLink:
		return p.applyLinkStrategy(resp)

	case StrategyCursor:
		return p.applyCursorStrategy(resp)

	case StrategyOffset:
		return p.applyOffsetStrategy(resp)

	default:
		p.hasMore = false
		return nil, true, nil
	}
}

// applyLinkStrategy handles link header-based pagination
func (p *Paginator) applyLinkStrategy(resp *Response) (*http.Request, bool, error) {
	req, cont, err := p.advanceLink(resp)
	if err != nil {
		return nil, true, err
	}
	if !cont {
		p.hasMore = false
		return nil, true, nil
	}
	p.hasMore = true
	return req, false, nil
}

// applyCursorStrategy handles cursor-based pagination
func (p *Paginator) applyCursorStrategy(resp *Response) (*http.Request, bool, error) {
	req, cont, err := p.advanceCursor(resp)
	if err != nil {
		return nil, true, err
	}
	if !cont {
		p.hasMore = false
		return nil, true, nil
	}
	p.hasMore = true
	return req, false, nil
}

// applyOffsetStrategy handles offset/limit-based pagination
func (p *Paginator) applyOffsetStrategy(resp *Response) (*http.Request, bool, error) {
	req, cont, err := p.advanceOffset(resp)
	if err != nil {
		return nil, true, err
	}
	if !cont {
		p.hasMore = false
		return nil, true, nil
	}
	p.hasMore = true
	return req, false, nil
}

// Summary returns pagination statistics accumulated so far.
func (p *Paginator) Summary() Summary {
	strat := p.strategy
	if strat == "" {
		strat = p.config.Strategy
	}
	return Summary{
		Strategy:    strat,
		TotalPages:  p.pages,
		TotalItems:  p.collected,
		HasMore:     p.hasMore,
		Truncated:   p.truncated,
		CursorFinal: p.lastCursor,
	}
}

func (p *Paginator) selectStrategy(resp *Response) (Strategy, error) {
	switch p.config.Strategy {
	case StrategyLink, StrategyCursor, StrategyOffset, StrategyNone, StrategyPlugin:
		return p.config.Strategy, nil
	}

	// Auto-detection: link header → cursor → offset
	if resp.Headers != nil {
		if link := resp.Headers.Get("Link"); link != "" {
			if _, ok := parseNextLink(link); ok {
				return StrategyLink, nil
			}
		}
	}

	if cursor, ok := p.extractCursor(resp); ok && cursor != "" {
		return StrategyCursor, nil
	}

	if off, lim, page, per := detectOffsetParams(resp.Request, p.config); off != "" || page != "" {
		p.offsetParam = off
		p.limitParam = lim
		p.pageParam = page
		p.perPageParam = per
		return StrategyOffset, nil
	}

	return StrategyNone, nil
}

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

func (p *Paginator) advanceOffset(resp *Response) (*http.Request, bool, error) {
	req := cloneRequest(resp.Request)
	if req == nil {
		return nil, false, errMissingRequest
	}

	if p.offsetParam == "" && p.pageParam == "" {
		off, lim, page, per := detectOffsetParams(req, p.config)
		p.offsetParam = off
		p.limitParam = lim
		p.pageParam = page
		p.perPageParam = per
	}

	q := req.URL.Query()
	items := resp.Items()

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
		if items < p.pageSize {
			hasMore, ok := resp.HasMoreFlag()
			if !ok || !hasMore {
				return nil, false, nil
			}
			// continue despite shorter page
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
		// continue
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
	if val, ok := extractString(data, []string{"next", "cursor"}); ok {
		return val, true
	}
	if val, ok := extractString(data, []string{"next_page_token"}); ok {
		return val, true
	}
	for _, candidate := range []string{"nextToken", "pagination.next", "pagination.next_cursor", "pagination.nextToken"} {
		if val, ok := extractString(data, strings.Split(candidate, ".")); ok {
			return val, true
		}
	}
	return "", false
}

func parseNextLink(header string) (string, bool) {
	if header == "" {
		return "", false
	}
	parts := splitLinkHeader(header)
	for _, part := range parts {
		urlPart, rel := parseLinkPart(part)
		if rel == "next" && urlPart != "" {
			return urlPart, true
		}
	}
	return "", false
}

func splitLinkHeader(header string) []string {
	var parts []string
	var current strings.Builder
	depth := 0
	for _, r := range header {
		switch r {
		case '<':
			depth++
			appendRune(&current, r)
		case '>':
			depth--
			appendRune(&current, r)
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(current.String()))
				current.Reset()
				continue
			}
			appendRune(&current, r)
		default:
			appendRune(&current, r)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, strings.TrimSpace(current.String()))
	}
	return parts
}

func appendRune(b *strings.Builder, r rune) {
	// WriteRune on strings.Builder only fails in catastrophic memory scenarios
	// In practice, this should never happen, so we ignore the error
	// rather than panicking which would crash the entire application
	_, _ = b.WriteRune(r)
}

func parseLinkPart(part string) (string, string) {
	part = strings.TrimSpace(part)
	if !strings.HasPrefix(part, "<") {
		return "", ""
	}
	end := strings.Index(part, ">")
	if end == -1 {
		return "", ""
	}
	urlPart := strings.TrimSpace(part[1:end])
	params := part[end+1:]
	for _, segment := range strings.Split(params, ";") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		pieces := strings.SplitN(segment, "=", 2)
		if len(pieces) != 2 {
			continue
		}
		key := strings.TrimSpace(pieces[0])
		val := strings.Trim(strings.TrimSpace(pieces[1]), "\"")
		if strings.EqualFold(key, "rel") && val == "next" {
			return urlPart, "next"
		}
	}
	return "", ""
}

func detectOffsetParams(req *http.Request, cfg Config) (offsetParam, limitParam, pageParam, perPageParam string) {
	if req == nil || req.URL == nil {
		return cfg.OffsetParam, cfg.LimitParam, cfg.PageParam, cfg.PerPageParam
	}
	q := req.URL.Query()
	if cfg.OffsetParam != "" {
		offsetParam = cfg.OffsetParam
	}
	if cfg.LimitParam != "" {
		limitParam = cfg.LimitParam
	}
	if cfg.PageParam != "" {
		pageParam = cfg.PageParam
	}
	if cfg.PerPageParam != "" {
		perPageParam = cfg.PerPageParam
	}

	if offsetParam == "" {
		for _, name := range []string{"offset", "start", "skip"} {
			if q.Has(name) {
				offsetParam = name
				break
			}
		}
	}
	if limitParam == "" {
		for _, name := range []string{"limit", "per_page", "page_size", "pageSize", "max_results", "count", "top", "take"} {
			if q.Has(name) {
				limitParam = name
				break
			}
		}
	}
	if pageParam == "" {
		for _, name := range []string{"page", "p"} {
			if q.Has(name) {
				pageParam = name
				break
			}
		}
	}
	if perPageParam == "" {
		for _, name := range []string{"per_page", "page_size", "pageSize", "limit", "max_results", "count"} {
			if q.Has(name) {
				perPageParam = name
				break
			}
		}
	}

	if perPageParam == "" {
		perPageParam = limitParam
	}
	if limitParam == "" {
		limitParam = perPageParam
	}
	return
}

func detectCursorParam(q url.Values) string {
	if q == nil {
		return ""
	}
	candidates := []string{"cursor", "page_token", "pageToken", "next_cursor", "nextCursor", "next_page_token", "page"}
	for _, name := range candidates {
		if q.Has(name) {
			return name
		}
	}
	return ""
}

func cloneRequest(r *http.Request) *http.Request {
	if r == nil {
		return nil
	}
	clone := r.Clone(r.Context())
	if r.GetBody != nil {
		body, err := r.GetBody()
		if err == nil {
			clone.Body = body
		} else {
			clone.Body = io.NopCloser(bytes.NewReader(nil))
		}
		clone.GetBody = r.GetBody
	} else {
		clone.Body = nil
		clone.GetBody = nil
	}
	return clone
}

func inferItemCount(data any) int {
	switch v := data.(type) {
	case []any:
		return len(v)
	case map[string]any:
		if count, ok := extractArrayLen(v, []string{"items", "data", "results", "value", "values", "records"}); ok {
			return count
		}
	}
	return 0
}

func extractArrayLen(m map[string]any, keys []string) (int, bool) {
	for _, key := range keys {
		if val, ok := m[key]; ok {
			if arr, ok := val.([]any); ok {
				return len(arr), true
			}
		}
	}
	return 0, false
}

func extractBool(m map[string]any, keys []string) (bool, bool) {
	for _, key := range keys {
		if val, ok := m[key]; ok {
			switch b := val.(type) {
			case bool:
				return b, true
			case string:
				lower := strings.ToLower(b)
				if lower == "true" {
					return true, true
				}
				if lower == "false" {
					return false, true
				}
			case json.Number:
				if b == "1" {
					return true, true
				}
				if b == "0" {
					return false, true
				}
			case float64:
				return b != 0, true
			}
		}
	}
	return false, false
}

func extractInt(data any, path []string) (int, bool) {
	if len(path) == 0 {
		return 0, false
	}
	current := data
	for _, segment := range path {
		switch v := current.(type) {
		case map[string]any:
			next, ok := v[segment]
			if !ok {
				return 0, false
			}
			current = next
		default:
			return 0, false
		}
	}
	switch v := current.(type) {
	case json.Number:
		iv, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(iv), true
	case float64:
		return int(v), true
	case string:
		iv, err := strconv.Atoi(v)
		if err != nil {
			return 0, false
		}
		return iv, true
	}
	return 0, false
}

func extractString(data any, path []string) (string, bool) {
	if len(path) == 0 {
		return "", false
	}
	current := data
	for _, segment := range path {
		switch v := current.(type) {
		case map[string]any:
			next, ok := v[segment]
			if !ok {
				return "", false
			}
			current = next
		case map[string]string:
			next, ok := v[segment]
			if !ok {
				return "", false
			}
			current = next
		case map[string]json.RawMessage:
			next, ok := v[segment]
			if !ok {
				return "", false
			}
			current = next
		default:
			return "", false
		}
	}
	switch v := current.(type) {
	case string:
		return v, v != ""
	case json.Number:
		return v.String(), true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	}
	return "", false
}

func extractIntFromMap(m map[string]any, keys []string) (int, bool) {
	for _, key := range keys {
		if val, ok := m[key]; ok {
			switch v := val.(type) {
			case json.Number:
				iv, err := v.Int64()
				if err != nil {
					continue
				}
				return int(iv), true
			case float64:
				return int(v), true
			case string:
				iv, err := strconv.Atoi(v)
				if err != nil {
					continue
				}
				return iv, true
			}
		}
	}
	return 0, false
}

func pageSizeFromQuery(q url.Values, name string, fallback int) int {
	if q == nil {
		return fallback
	}
	if name != "" && q.Has(name) {
		if v := parsePositiveInt(q.Get(name)); v > 0 {
			return v
		}
	}
	if fallback > 0 {
		return fallback
	}
	return 0
}

func parsePositiveInt(val string) int {
	if val == "" {
		return 0
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0
	}
	if n < 0 {
		return 0
	}
	return n
}

func chooseParam(names ...string) string {
	for _, name := range names {
		if name != "" {
			return name
		}
	}
	return ""
}
