package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIdentity_IsAuthenticated(t *testing.T) {
	tests := []struct {
		name     string
		identity *Identity
		want     bool
	}{
		{
			name:     "nil identity",
			identity: nil,
			want:     false,
		},
		{
			name:     "empty user id",
			identity: &Identity{UserID: "   "},
			want:     false,
		},
		{
			name:     "valid tailscale identity",
			identity: &Identity{UserID: "user@example.com", Source: "tailscale"},
			want:     true,
		},
		{
			name:     "valid betterauth identity",
			identity: &Identity{UserID: "admin@example.com", Source: "betterauth"},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.identity.IsAuthenticated())
		})
	}
}

func TestIdentity_SourceChecks(t *testing.T) {
	ts := &Identity{UserID: "u", Source: "tailscale"}
	ba := &Identity{UserID: "u", Source: "betterauth"}

	assert.True(t, ts.IsTailscale())
	assert.False(t, ts.IsBetterAuth())
	assert.True(t, ba.IsBetterAuth())
	assert.False(t, ba.IsTailscale())

	var nilID *Identity
	assert.False(t, nilID.IsTailscale())
	assert.False(t, nilID.IsBetterAuth())
}

func TestIdentityFromRequest_TailscaleHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.Header.Set("X-Tailscale-User", "user@example.com")
	req.Header.Set("X-Tailscale-User-Name", "Test User")
	req.Header.Set("X-Tailscale-Node", "test-node")
	req.Header.Set("X-Tailscale-Node-ID", "node-123")

	id := IdentityFromRequest(req)
	assert.NotNil(t, id)
	assert.Equal(t, "user@example.com", id.UserID)
	assert.Equal(t, "user@example.com", id.UserLogin)
	assert.Equal(t, "Test User", id.UserName)
	assert.Equal(t, "test-node", id.NodeName)
	assert.Equal(t, "node-123", id.NodeID)
	assert.Equal(t, "tailscale", id.Source)
}

func TestIdentityFromRequest_TailscaleHeaders_Minimal(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.Header.Set("X-Tailscale-User", "admin@tailscale.com")

	id := IdentityFromRequest(req)
	assert.NotNil(t, id)
	assert.Equal(t, "admin@tailscale.com", id.UserID)
	assert.Equal(t, "tailscale", id.Source)
}

func TestIdentityFromRequest_BetterAuthHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.Header.Set("X-BetterAuth-Email", "admin@example.com")
	req.Header.Set("X-BetterAuth-User-Name", "Admin User")

	id := IdentityFromRequest(req)
	assert.NotNil(t, id)
	assert.Equal(t, "admin@example.com", id.UserID)
	assert.Equal(t, "admin@example.com", id.UserLogin)
	assert.Equal(t, "Admin User", id.UserName)
	assert.Equal(t, "betterauth", id.Source)
	assert.Empty(t, id.NodeName)
	assert.Empty(t, id.NodeID)
}

func TestIdentityFromRequest_BetterAuthHeaders_Minimal(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.Header.Set("X-BetterAuth-Email", "user@example.com")

	id := IdentityFromRequest(req)
	assert.NotNil(t, id)
	assert.Equal(t, "user@example.com", id.UserID)
	assert.Equal(t, "betterauth", id.Source)
}

func TestIdentityFromRequest_NoHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	id := IdentityFromRequest(req)
	assert.Nil(t, id)
}

func TestIdentityFromRequest_TailscaleTakesPriority(t *testing.T) {
	// When both Tailscale and Better Auth headers are present,
	// Tailscale should win because it's checked first.
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.Header.Set("X-Tailscale-User", "ts-user@example.com")
	req.Header.Set("X-BetterAuth-Email", "ba-user@example.com")

	id := IdentityFromRequest(req)
	assert.NotNil(t, id)
	assert.Equal(t, "ts-user@example.com", id.UserID)
	assert.Equal(t, "tailscale", id.Source)
}

func TestIdentityFromRequest_WhitespaceTrimmed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.Header.Set("X-Tailscale-User", "  user@example.com  ")

	id := IdentityFromRequest(req)
	assert.NotNil(t, id)
	assert.Equal(t, "user@example.com", id.UserID)
}

func TestRequireIdentity_AllowsAuthenticated(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		id := IdentityFromRequest(r)
		assert.NotNil(t, id)
		assert.Equal(t, "user@example.com", id.UserID)
		w.WriteHeader(http.StatusOK)
	})

	middleware := RequireIdentity(next)
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.Header.Set("X-Tailscale-User", "user@example.com")
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)
	assert.True(t, nextCalled)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequireIdentity_RejectsAnonymous(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	middleware := RequireIdentity(next)
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)
	assert.False(t, nextCalled)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestOptionalIdentity_PassesAnonymous(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		id := IdentityFromRequest(r)
		assert.Nil(t, id)
		w.WriteHeader(http.StatusOK)
	})

	middleware := OptionalIdentity(next)
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)
	assert.True(t, nextCalled)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestOptionalIdentity_InjectsIdentity(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		id := IdentityFromRequest(r)
		assert.NotNil(t, id)
		assert.Equal(t, "admin@example.com", id.UserID)
		w.WriteHeader(http.StatusOK)
	})

	middleware := OptionalIdentity(next)
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.Header.Set("X-BetterAuth-Email", "admin@example.com")
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)
	assert.True(t, nextCalled)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestWithIdentity(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	id := &Identity{UserID: "test", Source: "tailscale"}

	newReq := WithIdentity(req, id)
	extracted := IdentityFromRequest(newReq)
	assert.NotNil(t, extracted)
	assert.Equal(t, "test", extracted.UserID)
}
