package pagination

import (
	"net/http"
	"net/url"
	"testing"
)

func TestLinkPagination(t *testing.T) {
	cfg := Config{}
	paginator, err := New(cfg)
	if err != nil {
		t.Fatalf("new paginator: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.example.com/repos?per_page=2", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp := &Response{
		Request: req,
		Headers: http.Header{
			"Link": []string{"<https://api.example.com/repos?page=2>; rel=\"next\", <https://api.example.com/repos?page=10>; rel=\"last\""},
		},
		Body:         []byte(`[1,2]`),
		ItemCount:    2,
		hasItemCount: true,
	}

	nextReq, done, err := paginator.ShouldContinue(resp)
	if err != nil {
		t.Fatalf("should continue: %v", err)
	}
	if done {
		t.Fatalf("expected more pages")
	}
	if nextReq.URL.String() != "https://api.example.com/repos?page=2" {
		t.Fatalf("unexpected next URL: %s", nextReq.URL.String())
	}
	if nextReq.Method != http.MethodGet {
		t.Fatalf("expected method preserved, got %s", nextReq.Method)
	}
}

func TestCursorPaginationAutoDetection(t *testing.T) {
	cfg := Config{}
	paginator, err := New(cfg)
	if err != nil {
		t.Fatalf("new paginator: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.example.com/items?page_token=first&limit=2", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp := &Response{
		Request: req,
		Headers: make(http.Header),
		Body:    []byte(`{"data":[1,2],"next_page_token":"abc123"}`),
	}

	nextReq, done, err := paginator.ShouldContinue(resp)
	if err != nil {
		t.Fatalf("should continue: %v", err)
	}
	if done {
		t.Fatalf("expected to continue for cursor pagination")
	}
	q := nextReq.URL.Query()
	if q.Get("page_token") != "abc123" {
		t.Fatalf("expected next page token set, got %q", q.Get("page_token"))
	}
}

func TestCursorPaginationCustomFields(t *testing.T) {
	cfg := Config{Strategy: StrategyCursor, CursorField: "meta.next", CursorParam: "cursor"}
	paginator, err := New(cfg)
	if err != nil {
		t.Fatalf("new paginator: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.example.com/search?cursor=", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp := &Response{
		Request: req,
		Headers: make(http.Header),
		Body:    []byte(`{"results":[{"id":1}],"meta":{"next":"token"}}`),
	}

	nextReq, done, err := paginator.ShouldContinue(resp)
	if err != nil {
		t.Fatalf("should continue: %v", err)
	}
	if done {
		t.Fatalf("expected to continue")
	}
	if got := nextReq.URL.Query().Get("cursor"); got != "token" {
		t.Fatalf("expected cursor set, got %q", got)
	}
}

func TestCursorPaginationNestedPaginationField(t *testing.T) {
	cfg := Config{}
	paginator, err := New(cfg)
	if err != nil {
		t.Fatalf("new paginator: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.example.com/items?cursor=", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp := &Response{
		Request: req,
		Headers: make(http.Header),
		Body:    []byte(`{"items":[1],"pagination":{"next":"nested-token"}}`),
	}

	nextReq, done, err := paginator.ShouldContinue(resp)
	if err != nil {
		t.Fatalf("should continue: %v", err)
	}
	if done {
		t.Fatalf("expected to continue for nested pagination")
	}
	if nextReq == nil {
		t.Fatal("expected next request")
		return
	}
	if got := nextReq.URL.Query().Get("cursor"); got != "nested-token" {
		t.Fatalf("expected nested token, got %q", got)
	}
}

func TestOffsetPagination(t *testing.T) {
	cfg := Config{}
	paginator, err := New(cfg)
	if err != nil {
		t.Fatalf("new paginator: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.example.com/items?offset=0&limit=2", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp1 := &Response{
		Request:      req,
		Headers:      make(http.Header),
		Body:         []byte(`[1,2]`),
		ItemCount:    2,
		hasItemCount: true,
	}

	nextReq, done, err := paginator.ShouldContinue(resp1)
	if err != nil {
		t.Fatalf("should continue: %v", err)
	}
	if done {
		t.Fatalf("expected another page")
	}
	if got := nextReq.URL.Query().Get("offset"); got != "2" {
		t.Fatalf("expected offset 2, got %q", got)
	}

	resp2 := &Response{
		Request:      nextReq,
		Headers:      make(http.Header),
		Body:         []byte(`[3]`),
		ItemCount:    1,
		hasItemCount: true,
	}

	_, done, err = paginator.ShouldContinue(resp2)
	if err != nil {
		t.Fatalf("should continue second page: %v", err)
	}
	if !done {
		t.Fatalf("expected pagination to finish")
	}
}

func TestPagePerPagePagination(t *testing.T) {
	cfg := Config{}
	paginator, err := New(cfg)
	if err != nil {
		t.Fatalf("new paginator: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.example.com/items?page=1&per_page=2", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp := &Response{
		Request:      req,
		Headers:      make(http.Header),
		Body:         []byte(`[1,2]`),
		ItemCount:    2,
		hasItemCount: true,
	}

	nextReq, done, err := paginator.ShouldContinue(resp)
	if err != nil {
		t.Fatalf("should continue: %v", err)
	}
	if done {
		t.Fatalf("expected another page")
	}
	values := nextReq.URL.Query()
	if got := values.Get("page"); got != "2" {
		t.Fatalf("expected page=2, got %q", got)
	}
	if got := values.Get("per_page"); got != "2" {
		t.Fatalf("expected per_page preserved, got %q", got)
	}
}

func TestMaxRecordsStopsPagination(t *testing.T) {
	cfg := Config{MaxRecords: 3}
	paginator, err := New(cfg)
	if err != nil {
		t.Fatalf("new paginator: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.example.com/items?per_page=5", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp := &Response{
		Request:      req,
		Headers:      make(http.Header),
		Body:         []byte(`[1,2,3,4,5]`),
		ItemCount:    5,
		hasItemCount: true,
	}

	_, done, err := paginator.ShouldContinue(resp)
	if err != nil {
		t.Fatalf("should continue: %v", err)
	}
	if !done {
		t.Fatalf("expected stop due to max records")
	}
	summary := paginator.Summary()
	if summary.TotalItems != 3 {
		t.Fatalf("expected total items clipped to max records, got %d", summary.TotalItems)
	}
	if !summary.Truncated {
		t.Fatalf("expected truncated flag due to max records")
	}
}

func TestMaxPagesStopsPagination(t *testing.T) {
	cfg := Config{MaxPages: 1}
	paginator, err := New(cfg)
	if err != nil {
		t.Fatalf("new paginator: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.example.com/items?per_page=2", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp := &Response{
		Request:      req,
		Headers:      make(http.Header),
		Body:         []byte(`[1,2]`),
		ItemCount:    2,
		hasItemCount: true,
	}

	_, done, err := paginator.ShouldContinue(resp)
	if err != nil {
		t.Fatalf("should continue: %v", err)
	}
	if !done {
		t.Fatalf("expected stop due to max pages")
	}
	summary := paginator.Summary()
	if summary.TotalPages != 1 {
		t.Fatalf("expected one page counted, got %d", summary.TotalPages)
	}
	if !summary.Truncated {
		t.Fatalf("expected truncated flag when max pages hit")
	}
}

func TestSummaryTracking(t *testing.T) {
	cfg := Config{}
	paginator, err := New(cfg)
	if err != nil {
		t.Fatalf("new paginator: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.example.com/repos?per_page=2", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp1 := &Response{
		Request:      req,
		Headers:      http.Header{"Link": []string{"<https://api.example.com/repos?page=2>; rel=\"next\""}},
		Body:         []byte(`[1,2]`),
		ItemCount:    2,
		hasItemCount: true,
	}
	nextReq, done, err := paginator.ShouldContinue(resp1)
	if err != nil || done {
		t.Fatalf("first page: err=%v done=%v", err, done)
	}

	resp2 := &Response{
		Request:      nextReq,
		Headers:      make(http.Header),
		Body:         []byte(`[3]`),
		ItemCount:    1,
		hasItemCount: true,
	}
	_, done, err = paginator.ShouldContinue(resp2)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if !done {
		t.Fatalf("expected completion after second page")
	}

	summary := paginator.Summary()
	if summary.TotalPages != 2 {
		t.Fatalf("expected total pages 2, got %d", summary.TotalPages)
	}
	if summary.TotalItems != 3 {
		t.Fatalf("expected total items 3, got %d", summary.TotalItems)
	}
	if summary.Strategy != StrategyLink {
		t.Fatalf("expected link strategy, got %s", summary.Strategy)
	}
}

func TestDetectOffsetParamsFallback(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://api.example.com/items?skip=0&top=10", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	off, lim, page, per := detectOffsetParams(req, Config{})
	if off != "skip" {
		t.Fatalf("expected skip offset, got %q", off)
	}
	if lim != "top" {
		t.Fatalf("expected top limit, got %q", lim)
	}
	if page != "" || per != "top" {
		t.Fatalf("unexpected page/per page detection: page=%q per=%q", page, per)
	}
}

func TestParseNextLink(t *testing.T) {
	header := "<https://api.example.com?page=2>; rel=\"next\", <https://api.example.com?page=5>; rel=\"last\""
	next, ok := parseNextLink(header)
	if !ok {
		t.Fatalf("expected next link detected")
	}
	if next != "https://api.example.com?page=2" {
		t.Fatalf("unexpected next link %q", next)
	}
}

func TestCloneRequestPreservesQuery(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://api.example.com/items?limit=2", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	clone := cloneRequest(req)
	if clone.Method != http.MethodPost {
		t.Fatalf("expected method preserved")
	}
	if clone.URL.Query().Get("limit") != "2" {
		t.Fatalf("expected query preserved")
	}
}

func TestApplyRecordLimit(t *testing.T) {
	cfg := Config{MaxRecords: 3}
	paginator, err := New(cfg)
	if err != nil {
		t.Fatalf("new paginator: %v", err)
	}
	paginator.limitParam = "limit"
	paginator.perPageParam = "per_page"
	paginator.collected = 2
	q := url.Values{"limit": []string{"5"}, "per_page": []string{"5"}}
	paginator.applyRecordLimit(&q)
	if q.Get("limit") != "1" {
		t.Fatalf("expected limit adjusted to remaining, got %q", q.Get("limit"))
	}
	if q.Get("per_page") != "1" {
		t.Fatalf("expected per_page adjusted to remaining, got %q", q.Get("per_page"))
	}
}
