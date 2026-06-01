package client_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"testing/quick"
	"time"

	"github.com/joshka0/foxctl/internal/interfaces/openapi/client"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/cas"
)

func TestExecuteInlineJSON(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Api-Key", "secret")
		if err := json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "name": "alpha"},
			{"id": 2, "name": "beta"},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	})

	cfg := config.Config{InlineOutputKB: 64, MaxCaptureKB: 1024}
	casStore := newTestCAS(t)
	httpClient := &http.Client{Transport: &handlerTransport{handler: handler}}
	c := client.New(cfg, casStore, client.WithHTTPClient(httpClient))

	req, err := http.NewRequest(http.MethodGet, "http://mock", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, execErr := c.Execute(context.Background(), req)
	if execErr != nil {
		t.Fatalf("execute: %v", execErr)
	}
	if resp == nil {
		t.Fatal("expected response")
		return
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

	payload := buildLargeJSONArray(t, 200)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(payload); err != nil {
			t.Fatalf("write payload: %v", err)
		}
	})

	cfg := config.Config{InlineOutputKB: 1, MaxCaptureKB: 2048}
	casStore := newTestCAS(t)
	httpClient := &http.Client{Transport: &handlerTransport{handler: handler}}
	c := client.New(cfg, casStore, client.WithHTTPClient(httpClient))

	req, err := http.NewRequest(http.MethodGet, "http://mock", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, execErr := c.Execute(context.Background(), req)
	if execErr != nil {
		t.Fatalf("execute: %v", execErr)
	}
	if resp == nil {
		t.Fatal("expected response")
		return
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

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		if err := json.NewEncoder(w).Encode(map[string]any{"error": "invalid"}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	})

	cfg := config.Config{InlineOutputKB: 64, MaxCaptureKB: 1024}
	casStore := newTestCAS(t)
	httpClient := &http.Client{Transport: &handlerTransport{handler: handler}}
	c := client.New(cfg, casStore, client.WithHTTPClient(httpClient))

	req, err := http.NewRequest(http.MethodGet, "http://mock", nil)
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

func TestExecuteClassifiesHTTPStatusContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		code   string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, code: "EAUTH"},
		{name: "forbidden", status: http.StatusForbidden, code: "EAUTH"},
		{name: "not_found", status: http.StatusNotFound, code: "ENOTFOUND"},
		{name: "request_timeout", status: http.StatusRequestTimeout, code: "ETIMEOUT"},
		{name: "rate_limited", status: http.StatusTooManyRequests, code: "ERATELIMIT"},
		{name: "bad_request", status: http.StatusBadRequest, code: "EARG"},
		{name: "server_error", status: http.StatusInternalServerError, code: "ERUNTIME"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				if err := json.NewEncoder(w).Encode(map[string]any{"status": tt.status}); err != nil {
					t.Fatalf("encode response: %v", err)
				}
			})

			cfg := config.Config{InlineOutputKB: 64, MaxCaptureKB: 1024}
			casStore := newTestCAS(t)
			httpClient := &http.Client{Transport: &handlerTransport{handler: handler}}
			c := client.New(cfg, casStore, client.WithHTTPClient(httpClient))

			req, err := http.NewRequest(http.MethodGet, "http://mock", nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}

			resp, execErr := c.Execute(context.Background(), req)
			if execErr == nil {
				t.Fatalf("expected error for status %d", tt.status)
			}
			clientErr, ok := execErr.(*client.Error)
			if !ok {
				t.Fatalf("expected *client.Error, got %T", execErr)
			}
			if clientErr.Code != tt.code {
				t.Fatalf("status %d code=%s want %s", tt.status, clientErr.Code, tt.code)
			}
			if resp == nil || clientErr.Response != resp {
				t.Fatalf("expected returned response attached to error")
			}
			if resp.StatusCode != tt.status {
				t.Fatalf("status=%d want %d", resp.StatusCode, tt.status)
			}
		})
	}
}

func TestExecuteMaxCaptureBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		size    int
		wantErr string
	}{
		{name: "at_limit_allowed", size: 1024},
		{name: "over_limit_rejected", size: 1025, wantErr: "EOUTPUT_TOO_LARGE"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			payload := bytes.Repeat([]byte("x"), tt.size)
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				if _, err := w.Write(payload); err != nil {
					t.Fatalf("write payload: %v", err)
				}
			})

			cfg := config.Config{InlineOutputKB: 2, MaxCaptureKB: 1}
			casStore := newTestCAS(t)
			httpClient := &http.Client{Transport: &handlerTransport{handler: handler}}
			c := client.New(cfg, casStore, client.WithHTTPClient(httpClient))

			req, err := http.NewRequest(http.MethodGet, "http://mock", nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}

			resp, execErr := c.Execute(context.Background(), req)
			if tt.wantErr == "" {
				if execErr != nil {
					t.Fatalf("execute: %v", execErr)
				}
				body, ok := resp.Body.(string)
				if !ok {
					t.Fatalf("expected string body, got %T", resp.Body)
				}
				if len(body) != tt.size {
					t.Fatalf("body size=%d want %d", len(body), tt.size)
				}
				return
			}

			if execErr == nil {
				t.Fatalf("expected %s", tt.wantErr)
			}
			if resp != nil {
				t.Fatalf("expected no processed response on capture limit error")
			}
			clientErr, ok := execErr.(*client.Error)
			if !ok {
				t.Fatalf("expected *client.Error, got %T", execErr)
			}
			if clientErr.Code != tt.wantErr {
				t.Fatalf("code=%s want %s", clientErr.Code, tt.wantErr)
			}
		})
	}
}

func TestExecutePreviewFirstKeysPropertySortedAndLimited(t *testing.T) {
	t.Parallel()

	var payload []byte
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(payload); err != nil {
			t.Fatalf("write payload: %v", err)
		}
	})

	cfg := config.Config{InlineOutputKB: 64, MaxCaptureKB: 1024}
	casStore := newTestCAS(t)
	httpClient := &http.Client{Transport: &handlerTransport{handler: handler}}
	c := client.New(cfg, casStore, client.WithHTTPClient(httpClient))

	check := func(seed uint64) bool {
		record := make(map[string]any, 8)
		for i := 0; i < 8; i++ {
			part := byte(seed >> (i * 8))
			record[fmt.Sprintf("k%03d_%02d", part, i)] = i
		}
		var err error
		payload, err = json.Marshal(record)
		if err != nil {
			return false
		}

		req, err := http.NewRequest(http.MethodGet, "http://mock", nil)
		if err != nil {
			return false
		}
		resp, execErr := c.Execute(context.Background(), req)
		if execErr != nil || resp == nil {
			return false
		}

		keys := resp.Preview.FirstKeys
		if len(keys) > 5 || !sort.StringsAreSorted(keys) {
			return false
		}
		sample, ok := resp.Preview.SampleRecord.(map[string]any)
		if !ok || len(sample) != len(keys) {
			return false
		}
		for _, key := range keys {
			if _, ok := record[key]; !ok {
				return false
			}
			if _, ok := sample[key]; !ok {
				return false
			}
		}
		return true
	}

	if err := quick.Check(check, &quick.Config{MaxCount: 50}); err != nil {
		t.Fatalf("preview first keys property failed: %v", err)
	}
}

func TestExecuteTimeout(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	cfg := config.Config{InlineOutputKB: 64, MaxCaptureKB: 1024}
	casStore := newTestCAS(t)
	httpClient := &http.Client{
		Timeout:   50 * time.Millisecond,
		Transport: &handlerTransport{handler: handler},
	}
	c := client.New(cfg, casStore, client.WithHTTPClient(httpClient))

	req, err := http.NewRequest(http.MethodGet, "http://mock", nil)
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

type handlerTransport struct {
	handler http.Handler
}

func (t *handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	recorder := httptest.NewRecorder()
	t.handler.ServeHTTP(recorder, req)
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	return recorder.Result(), nil
}

func newTestCAS(t *testing.T) *cas.Store {
	t.Helper()
	store, err := cas.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("new cas store: %v", err)
	}
	return store
}

func buildLargeJSONArray(t *testing.T, n int) []byte {
	t.Helper()
	records := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		records = append(records, map[string]any{"id": i, "name": "item"})
	}
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(records); err != nil {
		t.Fatalf("encode array: %v", err)
	}
	return buf.Bytes()
}
