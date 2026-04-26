package api

import (
	"context"
	"net/http"
	"strings"
)

// Identity represents an authenticated user, extracted from either Better Auth
// session headers (via gui-auth-gateway) or Tailscale identity headers (via
// foxctl gateway --with-web).
type Identity struct {
	// UserID is the stable user identifier (email for Better Auth, login name
	// for Tailscale).
	UserID string

	// UserLogin is the login handle (email or Tailscale login name).
	UserLogin string

	// UserName is the human-readable display name.
	UserName string

	// NodeName is the Tailscale node name (empty for Better Auth sessions).
	NodeName string

	// NodeID is the Tailscale node stable ID (empty for Better Auth sessions).
	NodeID string

	// Source indicates how the identity was derived: "tailscale" or "betterauth".
	Source string
}

// IsAuthenticated reports whether the identity represents an authenticated
// user (as opposed to an anonymous request).
func (i *Identity) IsAuthenticated() bool {
	return i != nil && strings.TrimSpace(i.UserID) != ""
}

// IsTailscale reports whether the identity came from the Tailscale gateway.
func (i *Identity) IsTailscale() bool {
	return i != nil && i.Source == "tailscale"
}

// IsBetterAuth reports whether the identity came from the Better Auth gateway.
func (i *Identity) IsBetterAuth() bool {
	return i != nil && i.Source == "betterauth"
}

// identityContextKey is the context key for storing *Identity.
type identityContextKey struct{}

// IdentityFromRequest extracts the authenticated identity from the request.
// It checks, in order:
//
//  1. Request context (set by RequireIdentity or OptionalIdentity middleware).
//
//  2. Tailscale identity headers (X-Tailscale-User, X-Tailscale-Node) set by
//     the gateway reverse proxy.
//
//  3. Better Auth session headers (X-BetterAuth-User, X-BetterAuth-Email) set
//     by gui-auth-gateway.
//
// Returns nil if no identity is available.
func IdentityFromRequest(r *http.Request) *Identity {
	// 1. Context (set by middleware)
	if id, ok := r.Context().Value(identityContextKey{}).(*Identity); ok && id != nil {
		return id
	}

	// 2. Tailscale headers from gateway reverse proxy
	if tsUser := strings.TrimSpace(r.Header.Get("X-Tailscale-User")); tsUser != "" {
		return &Identity{
			UserID:    tsUser,
			UserLogin: tsUser,
			UserName:  strings.TrimSpace(r.Header.Get("X-Tailscale-User-Name")),
			NodeName:  strings.TrimSpace(r.Header.Get("X-Tailscale-Node")),
			NodeID:    strings.TrimSpace(r.Header.Get("X-Tailscale-Node-ID")),
			Source:    "tailscale",
		}
	}

	// 3. Better Auth headers from gui-auth-gateway
	if baEmail := strings.TrimSpace(r.Header.Get("X-BetterAuth-Email")); baEmail != "" {
		return &Identity{
			UserID:    baEmail,
			UserLogin: baEmail,
			UserName:  strings.TrimSpace(r.Header.Get("X-BetterAuth-User-Name")),
			Source:    "betterauth",
		}
	}

	return nil
}

// WithIdentity stores the given identity in the request context.
func WithIdentity(r *http.Request, id *Identity) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), identityContextKey{}, id))
}

// RequireIdentity is middleware that ensures every request has an authenticated
// identity. Requests without identity receive 401 Unauthorized.
//
// Endpoints that should remain public (health, readiness, openapi) must be
// registered before this middleware or handled separately.
func RequireIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := IdentityFromRequest(r)
		if !id.IsAuthenticated() {
			httpError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, WithIdentity(r, id))
	})
}

// OptionalIdentity is middleware that extracts identity when present but does
// not reject anonymous requests. Handlers can check IdentityFromRequest(r) to
// enforce their own access control.
func OptionalIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := IdentityFromRequest(r)
		if id != nil {
			r = WithIdentity(r, id)
		}
		next.ServeHTTP(w, r)
	})
}
