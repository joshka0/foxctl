package auth

import (
	"net/http"
	"net/url"
	"testing"
)

func TestBearerApply(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err := Apply(req, Config{Type: "bearer", Token: "secret"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer secret" {
		t.Fatalf("unexpected header: %s", got)
	}
}

func TestAPIKeyHeader(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	cfg := Config{Type: "apiKey", APIKey: "abc", Header: "X-Key"}
	if err := Apply(req, cfg); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := req.Header.Get("X-Key"); got != "abc" {
		t.Fatalf("expected key header, got %s", got)
	}
}

func TestAPIKeyQuery(t *testing.T) {
	u, _ := url.Parse("https://example.com")
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
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	cfg := Config{Type: "basic", User: "user", Pass: "pass"}
	if err := Apply(req, cfg); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if req.Header.Get("Authorization") == "" {
		t.Fatalf("expected authorization header")
	}
}

func TestUnknownStrategy(t *testing.T) {
	if err := Apply(&http.Request{Header: http.Header{}}, Config{Type: "unknown"}); err == nil {
		t.Fatalf("expected error for unknown auth type")
	}
}
