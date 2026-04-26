package gateway

import (
	"context"
	"net/http"

	"github.com/joshka0/foxctl/internal/interfaces/gateway/sshterm"
	"tailscale.com/tsnet"
)

type contextKey string

const identityContextKey contextKey = "gateway_identity"

// IdentityFromRequest extracts the Tailscale identity from the request context.
// Returns nil if no identity is available (e.g., dev mode).
func IdentityFromRequest(r *http.Request) *sshterm.IdentityInfo {
	info, _ := r.Context().Value(identityContextKey).(*sshterm.IdentityInfo)
	return info
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
