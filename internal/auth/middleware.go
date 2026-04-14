package auth

import (
	"net/http"
	"strings"

	"github.com/casbin/casbin/v2"
	"github.com/joshka0/foxctl/internal/domain/identity"
)

// Middleware returns an HTTP middleware that authorizes requests with Casbin.
func Middleware(enforcer *casbin.Enforcer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if enforcer == nil {
				next.ServeHTTP(w, r)
				return
			}

			principal := identity.FromContext(r.Context())
			if principal.IsAnonymous() {
				next.ServeHTTP(w, r)
				return
			}

			action := methodAction(r.Method)
			resource := "api:" + r.URL.Path
			allowed, err := Enforce(enforcer, principal, resource, action)
			if err != nil || !allowed {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"forbidden","data":{"hint":"check that your user has the required role for this resource and action"}}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func methodAction(method string) string {
	switch strings.ToUpper(method) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return "write"
	default:
		return "read"
	}
}
