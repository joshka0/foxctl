package auth

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestBearerApply(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := Apply(req, Config{Type: "bearer", Token: "secret"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer secret" {
		t.Fatalf("unexpected header: %s", got)
	}
}

func TestAPIKeyHeader(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	cfg := Config{Type: "apiKey", APIKey: "abc", Header: "X-Key"}
	if err := Apply(req, cfg); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := req.Header.Get("X-Key"); got != "abc" {
		t.Fatalf("expected key header, got %s", got)
	}
}

func TestAPIKeyQuery(t *testing.T) {
	u, err := url.Parse("https://example.com")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	req := &http.Request{URL: u, Header: http.Header{}}
	cfg := Config{Type: "apiKey", APIKey: "abc", Query: "token"}
	if err := Apply(req, cfg); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if req.URL.Query().Get("token") != "abc" {
		t.Fatalf("expected query param set")
	}
}

func TestBasicApply(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	cfg := Config{Type: "basic", User: "user", Pass: "pass"}
	if err := Apply(req, cfg); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if req.Header.Get("Authorization") == "" {
		t.Fatalf("expected authorization header")
	}
}

func TestOAuth2CacheIsScopedToCredentials(t *testing.T) {
	var calls int
	oauth := NewOAuth2(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		body := fmt.Sprintf(`{"access_token":"token-%d","expires_in":3600}`, calls)
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})})

	cfg := Config{
		Type:         "oauth2",
		ClientID:     "client-one",
		ClientSecret: "secret-one",
		TokenURL:     "https://auth.example.com/token",
	}
	req1, err := http.NewRequest(http.MethodGet, "https://example.com/one", nil)
	if err != nil {
		t.Fatalf("new request one: %v", err)
	}
	if err := oauth.Apply(req1, cfg); err != nil {
		t.Fatalf("apply first credentials: %v", err)
	}
	if got := req1.Header.Get("Authorization"); got != "Bearer token-1" {
		t.Fatalf("first authorization=%q", got)
	}

	req1Cached, err := http.NewRequest(http.MethodGet, "https://example.com/one-again", nil)
	if err != nil {
		t.Fatalf("new cached request: %v", err)
	}
	if err := oauth.Apply(req1Cached, cfg); err != nil {
		t.Fatalf("apply cached credentials: %v", err)
	}
	if got := req1Cached.Header.Get("Authorization"); got != "Bearer token-1" {
		t.Fatalf("cached authorization=%q", got)
	}
	if calls != 1 {
		t.Fatalf("expected same credentials to reuse cached token, calls=%d", calls)
	}

	cfg.ClientID = "client-two"
	cfg.ClientSecret = "secret-two"
	req2, err := http.NewRequest(http.MethodGet, "https://example.com/two", nil)
	if err != nil {
		t.Fatalf("new request two: %v", err)
	}
	if err := oauth.Apply(req2, cfg); err != nil {
		t.Fatalf("apply second credentials: %v", err)
	}
	if got := req2.Header.Get("Authorization"); got != "Bearer token-2" {
		t.Fatalf("second authorization=%q", got)
	}
	if calls != 2 {
		t.Fatalf("expected second credentials to fetch a distinct token, calls=%d", calls)
	}
}

func TestUnknownStrategy(t *testing.T) {
	if err := Apply(&http.Request{Header: http.Header{}}, Config{Type: "unknown"}); err == nil {
		t.Fatalf("expected error for unknown auth type")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
