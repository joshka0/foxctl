package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/identity"
)

func TestMiddlewareAuthenticatedRequestWithRolePasses(t *testing.T) {
	t.Parallel()

	enforcer := mustNewEnforcer(t, `
g, user:web:alice, viewer, tenant-a
p, viewer, tenant-a, api:/api/conversations, read, allow
`)

	handler := Middleware(enforcer)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/conversations", http.NoBody)
	req = req.WithContext(identity.WithPrincipal(req.Context(), identity.Principal{
		Platform: "web",
		UserID:   "alice",
		TenantID: "tenant-a",
	}))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want %d", rr.Code, http.StatusNoContent)
	}
}

func TestMiddlewareAuthenticatedRequestWithoutRoleGetsForbidden(t *testing.T) {
	t.Parallel()

	enforcer := mustNewEnforcer(t, `
g, user:web:alice, viewer, tenant-a
p, viewer, tenant-a, api:/api/conversations, read, allow
`)

	handler := Middleware(enforcer)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/conversations", http.NoBody)
	req = req.WithContext(identity.WithPrincipal(req.Context(), identity.Principal{
		Platform: "web",
		UserID:   "bob",
		TenantID: "tenant-a",
	}))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want %d", rr.Code, http.StatusForbidden)
	}
}

func TestMiddlewareAnonymousRequestPassesThrough(t *testing.T) {
	t.Parallel()

	enforcer := mustNewEnforcer(t, `
g, user:web:alice, viewer, tenant-a
p, viewer, tenant-a, api:/api/conversations, read, allow
`)

	handler := Middleware(enforcer)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/conversations", http.NoBody)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want %d", rr.Code, http.StatusNoContent)
	}
}

func TestMiddlewareMapsHTTPMethodsToReadWrite(t *testing.T) {
	t.Parallel()

	enforcer := mustNewEnforcer(t, `
g, user:web:alice, viewer, tenant-a
p, viewer, tenant-a, api:/api/conversations, read, allow
p, viewer, tenant-a, api:/api/conversations, write, deny
`)

	handler := Middleware(enforcer)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	principal := identity.Principal{Platform: "web", UserID: "alice", TenantID: "tenant-a"}

	getReq := httptest.NewRequest(http.MethodGet, "/api/conversations", http.NoBody)
	getReq = getReq.WithContext(identity.WithPrincipal(getReq.Context(), principal))
	getResp := httptest.NewRecorder()
	handler.ServeHTTP(getResp, getReq)
	if getResp.Code != http.StatusNoContent {
		t.Fatalf("GET status=%d, want %d", getResp.Code, http.StatusNoContent)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/api/conversations", http.NoBody)
	postReq = postReq.WithContext(identity.WithPrincipal(postReq.Context(), principal))
	postResp := httptest.NewRecorder()
	handler.ServeHTTP(postResp, postReq)
	if postResp.Code != http.StatusForbidden {
		t.Fatalf("POST status=%d, want %d", postResp.Code, http.StatusForbidden)
	}
}
