// Package pagination implements pagination logic for OpenAPI HTTP requests.
package pagination

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

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
