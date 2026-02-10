package teams

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNormalizeServiceURL(t *testing.T) {
	t.Parallel()

	got, err := normalizeServiceURL("https://smba.trafficmanager.net/amer/")
	if err != nil {
		t.Fatalf("normalizeServiceURL err: %v", err)
	}
	if got != "https://smba.trafficmanager.net/amer" {
		t.Fatalf("unexpected normalized url: %q", got)
	}

	if _, err := normalizeServiceURL("http://smba.trafficmanager.net/amer/"); err == nil {
		t.Fatalf("expected scheme error, got nil")
	}
	if _, err := normalizeServiceURL("https://evil.example/"); err == nil {
		t.Fatalf("expected host error, got nil")
	}
}

func TestBotClient_doJSON_SetsAuthHeader(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer abc" {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ResourceResponse{ID: "123"})
	}))
	t.Cleanup(srv.Close)

	m := newTokenManager("cid", "secret", srv.Client())
	m.token = "abc"
	m.expiresAt = time.Now().Add(1 * time.Hour)

	c := newBotClient(m, srv.Client())

	var out ResourceResponse
	if err := c.doJSON(context.Background(), http.MethodPost, srv.URL, map[string]string{"x": "y"}, &out); err != nil {
		t.Fatalf("doJSON err: %v", err)
	}
	if out.ID != "123" {
		t.Fatalf("unexpected response id: %q", out.ID)
	}
}
