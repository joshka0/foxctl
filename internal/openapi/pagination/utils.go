package pagination

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// parseNextLink extracts the "next" URL from an RFC 8288 Link header.
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

// splitLinkHeader splits a Link header into individual link parts,
// respecting nested angle brackets.
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

// appendRune appends a rune to a strings.Builder.
func appendRune(b *strings.Builder, r rune) {
	// WriteRune on strings.Builder never returns an error
	b.WriteRune(r)
}

// parseLinkPart parses a single Link header part into URL and rel values.
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

// detectOffsetParams auto-detects pagination parameter names from the request.
func detectOffsetParams(req *http.Request, cfg Config) (offsetParam, limitParam, pageParam, perPageParam string) {
	if req == nil || req.URL == nil {
		return cfg.OffsetParam, cfg.LimitParam, cfg.PageParam, cfg.PerPageParam
	}
	q := req.URL.Query()

	// Use configured values if present
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

	// Auto-detect offset parameter
	if offsetParam == "" {
		for _, name := range []string{"offset", "start", "skip"} {
			if q.Has(name) {
				offsetParam = name
				break
			}
		}
	}

	// Auto-detect limit parameter
	if limitParam == "" {
		for _, name := range []string{"limit", "per_page", "page_size", "pageSize", "max_results", "count", "top", "take"} {
			if q.Has(name) {
				limitParam = name
				break
			}
		}
	}

	// Auto-detect page parameter
	if pageParam == "" {
		for _, name := range []string{"page", "p"} {
			if q.Has(name) {
				pageParam = name
				break
			}
		}
	}

	// Auto-detect per_page parameter
	if perPageParam == "" {
		for _, name := range []string{"per_page", "page_size", "pageSize", "limit", "max_results", "count"} {
			if q.Has(name) {
				perPageParam = name
				break
			}
		}
	}

	// Fallback: use limit and per_page interchangeably if one is missing
	if perPageParam == "" {
		perPageParam = limitParam
	}
	if limitParam == "" {
		limitParam = perPageParam
	}
	return
}

// detectCursorParam auto-detects cursor parameter name from query.
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

// cloneRequest creates a deep copy of an HTTP request.
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

// inferItemCount attempts to infer the number of items from response JSON.
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

// extractArrayLen extracts array length from a map using common field names.
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

// extractBool extracts a boolean value from a map using common field names.
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

// extractInt extracts an integer value from nested JSON using a path.
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

// extractString extracts a string value from nested JSON using a path.
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

// extractIntFromMap extracts an integer from a map using common field names.
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

// pageSizeFromQuery extracts page size from query or returns fallback.
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

// parsePositiveInt parses a string as a positive integer.
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

// chooseParam returns the first non-empty parameter name.
func chooseParam(names ...string) string {
	for _, name := range names {
		if name != "" {
			return name
		}
	}
	return ""
}
