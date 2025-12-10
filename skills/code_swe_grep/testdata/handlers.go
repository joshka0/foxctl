package handlers

import (
	"context"
	"net/http"
)

// Login handles user authentication.
// It validates credentials and creates a session.
// Only accepts POST requests for security.
func Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		http.Error(w, "missing credentials", http.StatusBadRequest)
		return
	}

	// Validate credentials against database
	if !validateCredentials(r.Context(), username, password) {
		http.Error(w, "invalid login", http.StatusUnauthorized)
		return
	}

	// Create session for authenticated user
	createSession(r.Context(), w, username)
	w.WriteHeader(http.StatusOK)
}

// Logout terminates the user session.
// Only accepts POST requests and validates CSRF token for security.
func Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	session := getSession(r.Context(), r)
	if session == nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	// Validate CSRF token from header or form
	csrfToken := r.Header.Get("X-CSRF-Token")
	if csrfToken == "" {
		_ = r.ParseForm()
		csrfToken = r.FormValue("csrf_token")
	}
	expectedToken := getSessionCSRFToken(r.Context(), session)
	if csrfToken == "" || csrfToken != expectedToken {
		http.Error(w, "invalid or missing CSRF token", http.StatusForbidden)
		return
	}

	destroySession(r.Context(), w, session)
	http.Redirect(w, r, "/", http.StatusFound)
}

// Register creates a new user account.
func Register(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	email := r.FormValue("email")
	password := r.FormValue("password")

	if username == "" || email == "" || password == "" {
		http.Error(w, "missing required fields", http.StatusBadRequest)
		return
	}

	// Create the new user
	if err := createUser(r.Context(), username, email, password); err != nil {
		http.Error(w, "registration failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// Helper functions (stubs for testing only)
//
// TODO: These are intentionally insecure test stubs. In production:
// - validateCredentials must verify against a secure credential store
// - createSession must generate cryptographically secure session tokens
// - getSession must validate session integrity and expiration
// - destroySession must invalidate all session data
// - createUser must hash passwords and validate input

// validateCredentials is a TEST STUB that always returns false.
// SECURITY: Must be replaced with real credential validation in production.
func validateCredentials(ctx context.Context, username, password string) bool {
	// TODO: Replace with real credential validation (e.g., bcrypt hash comparison)
	return false // Explicitly insecure: rejects all logins in test mode
}

func createSession(ctx context.Context, w http.ResponseWriter, username string)      {}
func getSession(ctx context.Context, r *http.Request) interface{}                    { return nil }
func destroySession(ctx context.Context, w http.ResponseWriter, session interface{}) {}
func createUser(ctx context.Context, username, email, password string) error         { return nil }

// getSessionCSRFToken retrieves the CSRF token for a session.
// TODO: Replace with real CSRF token retrieval from session store.
func getSessionCSRFToken(ctx context.Context, session interface{}) string {
	return "" // Stub: no CSRF token in test mode
}
