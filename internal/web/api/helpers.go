// Package api provides HTTP handlers for the agentctl web API.
package api

import (
	"encoding/json"
	"net/http"
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

// httpError writes a JSON error response.
func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
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