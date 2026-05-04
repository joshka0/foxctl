package tui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNormalizeBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{
			name: "trims trailing slash",
			raw:  "http://localhost:8090/",
			want: "http://localhost:8090",
		},
		{
			name: "adds http scheme when missing",
			raw:  "localhost:8090/",
			want: "http://localhost:8090",
		},
		{
			name: "normalizes base path",
			raw:  "https://example.com//api//",
			want: "https://example.com/api",
		},
		{
			name:    "rejects empty value",
			raw:     "   ",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeBaseURL(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizeBaseURL(%q) error = nil, want error", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeBaseURL(%q) error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeBaseURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestAPIClientRequestJSONUsesMethodPathAndBody(t *testing.T) {
	t.Parallel()

	type requestShape struct {
		Query string `json:"query"`
	}
	type responseShape struct {
		OK bool `json:"ok"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/prefix/api/console/sessions" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/prefix/api/console/sessions")
		}
		if r.URL.RawQuery != "format=payload" {
			t.Fatalf("raw query = %q, want %q", r.URL.RawQuery, "format=payload")
		}

		var got requestShape
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if got.Query != "hello" {
			t.Fatalf("request body query = %q, want %q", got.Query, "hello")
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(responseShape{OK: true})
	}))
	defer srv.Close()

	client, err := NewAPIClient(srv.URL+"/prefix/", srv.Client())
	if err != nil {
		t.Fatalf("NewAPIClient error: %v", err)
	}

	var response responseShape
	err = client.RequestJSON(
		context.Background(),
		http.MethodPost,
		"/api/console/sessions?format=payload",
		requestShape{Query: "hello"},
		&response,
	)
	if err != nil {
		t.Fatalf("RequestJSON error: %v", err)
	}
	if !response.OK {
		t.Fatalf("response.OK = %v, want true", response.OK)
	}
}

func TestAPIClientRequestJSONReturnsHTTPStatusError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client, err := NewAPIClient(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("NewAPIClient error: %v", err)
	}

	err = client.RequestJSON(context.Background(), http.MethodGet, "/api/agents", nil, nil)
	if err == nil {
		t.Fatal("RequestJSON error = nil, want HTTPStatusError")
	}

	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error type = %T, want *HTTPStatusError", err)
	}
	if statusErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("StatusCode = %d, want %d", statusErr.StatusCode, http.StatusServiceUnavailable)
	}
	if statusErr.Method != http.MethodGet {
		t.Fatalf("Method = %q, want %q", statusErr.Method, http.MethodGet)
	}
	if statusErr.Body == "" {
		t.Fatal("Body is empty, want server error detail")
	}
}

func TestAPIClientRequestJSONPropagatesContextCancellation(t *testing.T) {
	t.Parallel()

	canceled := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		canceled <- struct{}{}
		w.WriteHeader(http.StatusRequestTimeout)
	}))
	defer srv.Close()

	client, err := NewAPIClient(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("NewAPIClient error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err = client.RequestJSON(ctx, http.MethodGet, "/api/agents", nil, nil)
	if err == nil {
		t.Fatal("RequestJSON error = nil, want context cancellation error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}

	select {
	case <-canceled:
	case <-time.After(1 * time.Second):
		t.Fatal("handler context was not canceled")
	}
}
