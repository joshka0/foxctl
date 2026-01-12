// Package api provides HTTP handlers for the agentctl web API.
package api

import (
	"encoding/json"
	"net/http"
)

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// readJSON decodes JSON from the request body into the given value.
func readJSON(r *http.Request, out any) error {
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
