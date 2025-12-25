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
// Only accepts POST requests and validates CSRF token for security.
func Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate CSRF token from header or form
	csrfToken := r.Header.Get("X-CSRF-Token")
	if csrfToken == "" {
		_ = r.ParseForm()
		csrfToken = r.FormValue("csrf_token")
	}
	if csrfToken == "" || csrfToken != serverCSRFToken {
		http.Error(w, "invalid or missing CSRF token", http.StatusForbidden)
		return
	}

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
// NOTE: These are intentionally simple test stubs. In production:
// - validateCredentials must verify against a secure credential store
// - createSession must generate cryptographically secure session tokens
// - getSession must validate session integrity and expiration
// - destroySession must invalidate all session data
// - createUser must hash passwords and validate input

// Test credentials for deterministic testing.
const (
	testUsername    = "testuser"
	testPassword    = "testpass123"
	serverCSRFToken = "test-csrf-token-12345"
)

// testSession represents an in-memory session for testing.
type testSession struct {
	Username  string
	CSRFToken string
}

// In-memory stores for testing.
var (
	sessions = make(map[string]*testSession)
	users    = make(map[string]string) // username -> email
)

// validateCredentials validates credentials against test user or in-memory store.
// Returns true for the hard-coded test user or users created via createUser.
func validateCredentials(ctx context.Context, username, password string) bool {
	// Accept hard-coded test credentials for deterministic testing
	if username == testUsername && password == testPassword {
		return true
	}
	// Check in-memory user store (password not stored for simplicity)
	_, exists := users[username]
	return exists
}

// createSession creates a test session and sets a cookie.
func createSession(ctx context.Context, w http.ResponseWriter, username string) {
	sessionID := "session-" + username
	sessions[sessionID] = &testSession{
		Username:  username,
		CSRFToken: serverCSRFToken,
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
	})
}

// getSession retrieves a session from the cookie.
func getSession(ctx context.Context, r *http.Request) any {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		return nil
	}
	session, exists := sessions[cookie.Value]
	if !exists {
		return nil
	}
	return session
}

// destroySession removes the session from the in-memory store.
func destroySession(ctx context.Context, w http.ResponseWriter, session any) {
	if s, ok := session.(*testSession); ok {
		sessionID := "session-" + s.Username
		delete(sessions, sessionID)
	}
	// Clear the cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}

// createUser adds a user to the in-memory store.
func createUser(ctx context.Context, username, email, password string) error {
	users[username] = email
	return nil
}

// getSessionCSRFToken retrieves the CSRF token for a session.
func getSessionCSRFToken(ctx context.Context, session any) string {
	if s, ok := session.(*testSession); ok {
		return s.CSRFToken
	}
	return ""
}
