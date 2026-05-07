package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/joshka0/foxctl/internal/domain/identity"
)

const (
	headerBetterAuthUserID    = "X-BetterAuth-User-ID"
	headerBetterAuthEmail     = "X-BetterAuth-Email"
	headerBetterAuthUserName  = "X-BetterAuth-User-Name"
	headerTailscaleUser       = "X-Tailscale-User"
	headerTailscaleUserName   = "X-Tailscale-User-Name"
	headerTailscaleNode       = "X-Tailscale-Node"
	headerTailscaleNodeID     = "X-Tailscale-Node-ID"
	headerFoxctlTenantID      = "X-Foxctl-Tenant-ID"
	headerFoxctlWorkspaceID   = "X-Foxctl-Workspace-ID"
	headerFoxctlWorkspaceRoot = "X-Foxctl-Workspace-Root"
	headerFoxctlSessionID     = "X-Foxctl-Session-ID"
)

var (
	ErrPrincipalConflict          = errors.New("conflicting identity headers")
	ErrPrincipalTenantRequired    = errors.New("tenant id is required")
	ErrPrincipalWorkspaceMismatch = errors.New("workspace id mismatch")
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

type PrincipalRequestOptions struct {
	RequireTenant       bool
	ExpectedWorkspaceID string
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
	if tsUser := strings.TrimSpace(r.Header.Get(headerTailscaleUser)); tsUser != "" {
		return &Identity{
			UserID:    tsUser,
			UserLogin: tsUser,
			UserName:  strings.TrimSpace(r.Header.Get(headerTailscaleUserName)),
			NodeName:  strings.TrimSpace(r.Header.Get(headerTailscaleNode)),
			NodeID:    strings.TrimSpace(r.Header.Get(headerTailscaleNodeID)),
			Source:    "tailscale",
		}
	}

	// 3. Better Auth headers from gui-auth-gateway
	baEmail := strings.TrimSpace(r.Header.Get(headerBetterAuthEmail))
	baUserID := strings.TrimSpace(r.Header.Get(headerBetterAuthUserID))
	if baEmail != "" || baUserID != "" {
		userID := baUserID
		if userID == "" {
			userID = baEmail
		}
		return &Identity{
			UserID:    userID,
			UserLogin: baEmail,
			UserName:  strings.TrimSpace(r.Header.Get(headerBetterAuthUserName)),
			Source:    "betterauth",
		}
	}

	return nil
}

func PrincipalFromRequest(r *http.Request, opts PrincipalRequestOptions) (identity.Principal, error) {
	if hasTailscaleIdentityHeaders(r) && hasBetterAuthIdentityHeaders(r) {
		return identity.Principal{}, ErrPrincipalConflict
	}

	tenantID := strings.TrimSpace(r.Header.Get(headerFoxctlTenantID))
	if opts.RequireTenant && tenantID == "" {
		return identity.Principal{}, ErrPrincipalTenantRequired
	}

	workspaceID := strings.TrimSpace(r.Header.Get(headerFoxctlWorkspaceID))
	expectedWorkspaceID := strings.TrimSpace(opts.ExpectedWorkspaceID)
	if expectedWorkspaceID != "" {
		if workspaceID != "" && workspaceID != expectedWorkspaceID {
			return identity.Principal{}, ErrPrincipalWorkspaceMismatch
		}
		workspaceID = expectedWorkspaceID
	}

	id := IdentityFromRequest(r)
	principal := identity.Principal{
		TenantID:      tenantID,
		WorkspaceID:   workspaceID,
		WorkspaceRoot: strings.TrimSpace(r.Header.Get(headerFoxctlWorkspaceRoot)),
		SessionID:     strings.TrimSpace(r.Header.Get(headerFoxctlSessionID)),
	}
	if id != nil {
		principal.Platform = principalPlatform(id)
		principal.UserID = strings.TrimSpace(id.UserID)
		principal.Username = strings.TrimSpace(firstNonEmpty(id.UserName, id.UserLogin))
	} else {
		principal.Platform = "web"
	}
	return principal, nil
}

func principalPlatform(id *Identity) string {
	if id != nil && id.Source == "tailscale" {
		return "tailscale"
	}
	return "web"
}

func hasTailscaleIdentityHeaders(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get(headerTailscaleUser)) != ""
}

func hasBetterAuthIdentityHeaders(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get(headerBetterAuthEmail)) != "" ||
		strings.TrimSpace(r.Header.Get(headerBetterAuthUserID)) != ""
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
