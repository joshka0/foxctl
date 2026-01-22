package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

type v1Envelope struct {
	OK    bool     `json:"ok"`
	Data  any      `json:"data"`
	Error *v1Error `json:"error,omitempty"`
	Meta  v1Meta   `json:"meta"`
}

type v1Error struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Hint    string `json:"hint,omitempty"`
}

type v1Meta struct {
	RequestID  string `json:"request_id"`
	TS         string `json:"ts"`
	DurationMS int64  `json:"duration_ms"`
}

func wrapV1(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		meta := v1Meta{
			RequestID: requestIDFrom(r),
			TS:        time.Now().UTC().Format(time.RFC3339),
		}

		rewritten := cloneRequest(r)
		rewritten.URL.Path = strings.Replace(rewritten.URL.Path, "/api/v1/", "/api/", 1)

		if shouldBypassV1(rewritten) {
			next.ServeHTTP(w, rewritten)
			return
		}

		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, rewritten)

		status := rec.Code
		body := rec.Body.Bytes()
		meta.DurationMS = time.Since(start).Milliseconds()

		env := v1Envelope{
			OK:   status < http.StatusBadRequest,
			Meta: meta,
		}

		if status < http.StatusBadRequest {
			if isSkillRunPath(rewritten.URL.Path) {
				env.Data = map[string]any{"envelope": parseBody(body)}
			} else {
				env.Data = parseBody(body)
			}
		} else {
			env.Data = map[string]any{}
			env.Error = &v1Error{
				Code:    errorCodeForStatus(status),
				Message: errorMessage(body, status),
			}
		}

		copyResponseHeaders(w.Header(), rec.Header())
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if meta.RequestID != "" {
			w.Header().Set("X-Request-Id", meta.RequestID)
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(env)
	})
}

func cloneRequest(r *http.Request) *http.Request {
	clone := r.Clone(r.Context())
	if r.URL != nil {
		urlCopy := *r.URL
		clone.URL = &urlCopy
	}
	return clone
}

func copyResponseHeaders(dst, src http.Header) {
	for key, values := range src {
		if strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Content-Type") {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

// shouldBypassV1 skips envelope wrapping for streaming or binary responses.
func shouldBypassV1(r *http.Request) bool {
	path := r.URL.Path
	if strings.HasPrefix(path, "/api/events") {
		return true
	}
	if strings.HasPrefix(path, "/api/console/sessions/") && strings.HasSuffix(path, "/events") {
		return true
	}
	if strings.HasPrefix(path, "/api/cas/") && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		return true
	}
	if strings.HasPrefix(path, "/api/sqlite") {
		return true
	}
	return false
}

func isSkillRunPath(path string) bool {
	if path == "/api/skills/run" {
		return true
	}
	return strings.HasPrefix(path, "/api/skills/") && strings.HasSuffix(path, "/run")
}

func parseBody(body []byte) any {
	if len(body) == 0 {
		return map[string]any{}
	}

	var data any
	if err := json.Unmarshal(body, &data); err == nil {
		if data == nil {
			return map[string]any{}
		}
		return data
	}

	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return map[string]any{}
	}
	return map[string]any{"raw": trimmed}
}

func errorMessage(body []byte, status int) string {
	if len(body) > 0 {
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err == nil {
			if msg, ok := payload["error"].(string); ok && msg != "" {
				return msg
			}
			if msg, ok := payload["message"].(string); ok && msg != "" {
				return msg
			}
		}
		trimmed := strings.TrimSpace(string(body))
		if trimmed != "" {
			return trimmed
		}
	}

	if statusText := http.StatusText(status); statusText != "" {
		return statusText
	}
	return "request failed"
}

func errorCodeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "EARG"
	case http.StatusUnauthorized, http.StatusForbidden:
		return "EPOLICY"
	case http.StatusNotFound:
		return "ENOTFOUND"
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return "ETIMEOUT"
	default:
		return "ERUNTIME"
	}
}

func requestIDFrom(r *http.Request) string {
	if r == nil {
		return ""
	}

	reqID := r.Header.Get("X-Request-Id")
	if reqID == "" {
		reqID = r.Header.Get("X-Request-ID")
	}
	if reqID == "" {
		reqID = fmt.Sprintf("req_%d", time.Now().UnixNano())
	}
	return reqID
}
