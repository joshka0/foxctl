package gateway

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/joshka0/foxctl/internal/domain/identity"
	"github.com/joshka0/foxctl/internal/interfaces/gateway/sshterm"
	"tailscale.com/tsnet"
)

type contextKey string

const identityContextKey contextKey = "gateway_identity"

const (
	headerGatewayTenantID      = "X-Foxctl-Tenant-ID"
	headerGatewayWorkspaceID   = "X-Foxctl-Workspace-ID"
	headerGatewayWorkspaceRoot = "X-Foxctl-Workspace-Root"
	headerGatewaySessionID     = "X-Foxctl-Session-ID"
)

var (
	ErrPrincipalTenantRequired    = errors.New("tenant id is required")
	ErrPrincipalWorkspaceMismatch = errors.New("workspace id mismatch")
)

type PrincipalRequestOptions struct {
	RequireTenant       bool
	ExpectedWorkspaceID string
}

// IdentityFromRequest extracts the Tailscale identity from the request context.
// Returns nil if no identity is available (e.g., dev mode).
func IdentityFromRequest(r *http.Request) *sshterm.IdentityInfo {
	info, _ := r.Context().Value(identityContextKey).(*sshterm.IdentityInfo)
	return info
}

func PrincipalFromRequest(r *http.Request, opts PrincipalRequestOptions) (identity.Principal, error) {
	tenantID := strings.TrimSpace(r.Header.Get(headerGatewayTenantID))
	if opts.RequireTenant && tenantID == "" {
		return identity.Principal{}, ErrPrincipalTenantRequired
	}

	workspaceID := strings.TrimSpace(r.Header.Get(headerGatewayWorkspaceID))
	expectedWorkspaceID := strings.TrimSpace(opts.ExpectedWorkspaceID)
	if expectedWorkspaceID != "" {
		if workspaceID != "" && workspaceID != expectedWorkspaceID {
			return identity.Principal{}, ErrPrincipalWorkspaceMismatch
		}
		workspaceID = expectedWorkspaceID
	}

	principal := identity.Principal{
		TenantID:      tenantID,
		Platform:      "tailscale",
		WorkspaceID:   workspaceID,
		WorkspaceRoot: strings.TrimSpace(r.Header.Get(headerGatewayWorkspaceRoot)),
		SessionID:     strings.TrimSpace(r.Header.Get(headerGatewaySessionID)),
	}
	if info := IdentityFromRequest(r); info != nil {
		principal.UserID = strings.TrimSpace(firstNonEmpty(info.UserID, info.UserLogin))
		principal.Username = strings.TrimSpace(firstNonEmpty(info.UserName, info.UserLogin, info.UserID))
	}
	return principal, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// WhoIsMiddleware extracts the Tailscale identity for each HTTPS request and
// injects it into the request context. When running in tsnet mode, this enables
// identity-aware authorization for agent API routes. In dev mode (ts == nil),
// the middleware is a no-op pass-through.
func WhoIsMiddleware(ts *tsnet.Server, next http.Handler) http.Handler {
	if ts == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Derive the remote address for WhoIs lookup.
		// tsnet connections expose the peer address through RemoteAddr.
		addr := r.RemoteAddr
		if addr != "" {
			lc, err := ts.LocalClient()
			if err == nil {
				who, err := lc.WhoIs(r.Context(), addr)
				if err == nil && who != nil {
					info := &sshterm.IdentityInfo{
						NodeName: who.Node.Name,
						NodeID:   string(who.Node.StableID),
					}
					if who.UserProfile != nil {
						info.UserID = who.UserProfile.LoginName
						info.UserLogin = who.UserProfile.LoginName
						info.UserName = who.UserProfile.DisplayName
					}
					ctx := context.WithValue(r.Context(), identityContextKey, info)
					r = r.WithContext(ctx)
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
