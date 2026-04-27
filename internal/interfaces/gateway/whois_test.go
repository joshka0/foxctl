package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/joshka0/foxctl/internal/interfaces/gateway/sshterm"
)

func TestWhoIsMiddleware_NilTsnet(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		// Verify no identity was injected
		info := IdentityFromRequest(r)
		assert.Nil(t, info, "IdentityFromRequest should return nil when tsnet is nil")
		w.WriteHeader(http.StatusOK)
	})

	middleware := WhoIsMiddleware(nil, next)
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	assert.True(t, called, "handler should be called for nil tsnet")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIdentityFromRequest_NoIdentity(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	info := IdentityFromRequest(req)
	assert.Nil(t, info, "IdentityFromRequest should return nil when no identity is in context")
}

func TestIdentityFromRequest_WithIdentity(t *testing.T) {
	expected := &sshterm.IdentityInfo{
		NodeName:  "test-node",
		NodeID:    "node-123",
		UserID:    "user-456",
		UserLogin: "test@example.com",
		UserName:  "Test User",
	}

	ctx := withIdentityContext(context.Background(), expected)
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil).WithContext(ctx)

	info := IdentityFromRequest(req)
	assert.NotNil(t, info)
	assert.Equal(t, expected.NodeName, info.NodeName)
	assert.Equal(t, expected.NodeID, info.NodeID)
	assert.Equal(t, expected.UserID, info.UserID)
	assert.Equal(t, expected.UserLogin, info.UserLogin)
	assert.Equal(t, expected.UserName, info.UserName)
}

func TestIdentityFromRequest_TypeAssertion(t *testing.T) {
	// Ensure IdentityFromRequest handles wrong type gracefully
	ctx := context.WithValue(context.Background(), identityContextKey, "not-an-identity-info")
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil).WithContext(ctx)

	info := IdentityFromRequest(req)
	assert.Nil(t, info, "IdentityFromRequest should return nil for wrong type")
}

// withIdentityContext is a test helper to inject identity into context directly.
func withIdentityContext(ctx context.Context, info *sshterm.IdentityInfo) context.Context {
	return context.WithValue(ctx, identityContextKey, info)
}
