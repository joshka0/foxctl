package client_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/openapi/client"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/cas"
)

func TestExecuteInlineJSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Api-Key", "secret")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "name": "alpha"},
			{"id": 2, "name": "beta"},
		})
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{InlineOutputKB: 64, MaxCaptureKB: 1024}
	casStore := newTestCAS(t)
	c := client.New(cfg, casStore)

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, execErr := c.Execute(context.Background(), req)
	if execErr != nil {
		t.Fatalf("execute: %v", execErr)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Digest != "" {
		t.Fatalf("expected inline response, got digest %q", resp.Digest)
	}
	if resp.RecordCount != 2 {
		t.Fatalf("expected record count 2, got %d", resp.RecordCount)
	}
	if resp.Headers["x-api-key"] != "***" {
		t.Fatalf("expected header redacted, got %q", resp.Headers["x-api-key"])
	}

	body, ok := resp.Body.([]any)
	if !ok {
		t.Fatalf("expected []any body, got %T", resp.Body)
	}
	if len(body) != 2 {
		t.Fatalf("expected 2 body entries, got %d", len(body))
	}
	if len(resp.Preview.FirstKeys) == 0 {
		t.Fatalf("expected preview keys, got none")
	}
}

func TestExecuteStoresLargeBodyInCAS(t *testing.T) {
	t.Parallel()

	payload := buildLargeJSONArray(200)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{InlineOutputKB: 1, MaxCaptureKB: 2048}
	casStore := newTestCAS(t)
	c := client.New(cfg, casStore)

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, execErr := c.Execute(context.Background(), req)
	if execErr != nil {
		t.Fatalf("execute: %v", execErr)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Digest == "" {
		t.Fatalf("expected CAS digest")
	}
	if resp.RecordCount != 200 {
		t.Fatalf("expected record count 200, got %d", resp.RecordCount)
	}
	if resp.Artifact == nil {
		t.Fatalf("expected artifact metadata")
	}
	if resp.Artifact.Size <= 0 {
		t.Fatalf("expected artifact size, got %d", resp.Artifact.Size)
	}
	if resp.Body != nil {
		t.Fatalf("expected no inline body for CAS response")
	}
}

func TestExecuteReturnsHTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid"})
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{InlineOutputKB: 64, MaxCaptureKB: 1024}
	casStore := newTestCAS(t)
	c := client.New(cfg, casStore)

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, execErr := c.Execute(context.Background(), req)
	if execErr == nil {
		t.Fatalf("expected error for status 422")
	}
	clientErr, ok := execErr.(*client.Error)
	if !ok {
		t.Fatalf("expected *client.Error, got %T", execErr)
	}
	if clientErr.Code != "EARG" {
		t.Fatalf("expected code EARG, got %s", clientErr.Code)
	}
	if clientErr.Response == nil {
		t.Fatalf("expected response attached to error")
	}
	if resp != clientErr.Response {
		t.Fatalf("expected response to match error response")
	}
	body, ok := resp.Body.(map[string]any)
	if !ok {
		t.Fatalf("expected map body, got %T", resp.Body)
	}
	if body["error"] != "invalid" {
		t.Fatalf("unexpected body: %#v", body)
	}
}

func TestExecuteTimeout(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{InlineOutputKB: 64, MaxCaptureKB: 1024}
	casStore := newTestCAS(t)
	httpClient := &http.Client{Timeout: 50 * time.Millisecond}
	c := client.New(cfg, casStore, client.WithHTTPClient(httpClient))

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, execErr := c.Execute(context.Background(), req)
	if execErr == nil {
		t.Fatalf("expected timeout error")
	}
	if resp != nil {
		t.Fatalf("expected nil response on timeout")
	}
	clientErr, ok := execErr.(*client.Error)
	if !ok {
		t.Fatalf("expected *client.Error, got %T", execErr)
	}
	if clientErr.Code != "ETIMEOUT" {
		t.Fatalf("expected code ETIMEOUT, got %s", clientErr.Code)
	}
}

func newTestCAS(t *testing.T) *cas.Store {
	t.Helper()
	store, err := cas.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new cas store: %v", err)
	}
	return store
}

func buildLargeJSONArray(n int) []byte {
	records := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		records = append(records, map[string]any{"id": i, "name": "item"})
	}
	buf := &bytes.Buffer{}
	_ = json.NewEncoder(buf).Encode(records)
	return buf.Bytes()
}
