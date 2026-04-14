package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/joshka0/foxctl/internal/domain/envelope"
)

// writeJSON writes v as JSON to w and sets the HTTP status code.
// It sets the Content-Type header to "application/json; charset=utf-8" and writes the provided status code before encoding v. Encoding errors are ignored.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// maxBodySize is the default maximum request body size (1MB).
const maxBodySize = 1 << 20 // 1MB

// readJSON decodes JSON from the request body into the given value.
// Limits request body to maxBodySize to prevent DOS attacks.
// The ResponseWriter is required so that http.MaxBytesReader can close the
// readJSON reads JSON from r into out after limiting the request body to maxBodySize.
// It wraps r.Body with http.MaxBytesReader using w so the server can close the connection if the body exceeds the limit.
// Returns any decode error, including errors caused by an oversized request body.
func readJSON(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	return json.NewDecoder(r.Body).Decode(out)
}

// httpError writes an error envelope response that matches the canonical wire format.
//
// Index:
// - Purpose: Provide consistent error payloads for HTTP API endpoints
// - Flow: derive stable error.code + user hint → build envelope.Error → write JSON with HTTP status
// - SideEffects: writes response headers/body
// - FailureModes: JSON encoding errors ignored (best-effort)
// - Related: envelope.Error, writeJSON
// - Keywords: http, api, error_envelope, status_code, remediation_hint
func httpError(w http.ResponseWriter, status int, msg string) {
	code := fmt.Sprintf("http_%d", status)
	if text := http.StatusText(status); text != "" {
		code = fmt.Sprintf("http_%d_%s", status, strings.ToLower(strings.ReplaceAll(text, " ", "_")))
	}
	if strings.TrimSpace(msg) == "" {
		msg = http.StatusText(status)
		if msg == "" {
			msg = "request failed"
		}
	}

	env := envelope.Error("http.error", code, msg, map[string]any{
		"hint": httpErrorHint(status),
	})
	writeJSON(w, status, env)
}

// httpErrorHint provides a user-facing remediation hint for common HTTP errors.
//
// Index:
// - Purpose: Give clients actionable remediation guidance without leaking internals
// - Flow: map HTTP status → hint string
// - SideEffects: none
// - FailureModes: none
// - Related: httpError
// - Keywords: hint, remediation, http_status
func httpErrorHint(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "Check the request parameters and try again."
	case http.StatusUnauthorized, http.StatusForbidden:
		return "Verify your credentials and permissions, then try again."
	case http.StatusNotFound:
		return "Verify the resource exists and the URL is correct."
	default:
		if status >= 500 {
			return "Retry in a moment. If the problem persists, check the server logs."
		}
		return "Check the request and try again."
	}
}

// ErrorResponse is a standard error response.
type ErrorResponse struct {
	Error string `json:"error"`
}

// SuccessResponse is a standard success response.
type SuccessResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}
