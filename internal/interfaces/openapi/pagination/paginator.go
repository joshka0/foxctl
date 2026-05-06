package pagination

import (
	"errors"
	"fmt"
	"net/http"
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
//
// Index:
//
//	Purpose: Decide whether to fetch the next page and build its request
//	Flow: count items → enforce page/record limits → select strategy → apply strategy
//	Related: Paginator.selectStrategy, Paginator.applyStrategy
//	Keywords: pagination, max_pages, max_records, strategy, cursor, offset, has_more
//
// [[protocol:openapi-pagination]]
// [[domain:pagination-state-machine]]
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

// applyStrategy applies the selected pagination strategy and returns the next request.
// This method delegates to strategy-specific handlers to reduce complexity.
// Returns: (nextRequest, isDone, error)
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

// applyLinkStrategy handles RFC 8288 Link header-based pagination (e.g., GitHub API).
// Extracts "next" link from Link header: Link: <url>; rel="next"
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

// applyCursorStrategy handles cursor-based pagination (e.g., Stripe API).
// Looks for cursor tokens like "next_cursor" or "pagination.nextToken" in response body.
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

// applyOffsetStrategy handles offset/limit-based pagination (e.g., REST APIs).
// Uses query parameters like "offset", "limit", "page", or "per_page".
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
