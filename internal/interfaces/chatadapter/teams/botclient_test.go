package teams

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
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

	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if got := r.Header.Get("Authorization"); got != "Bearer abc" {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Status:     "401 Unauthorized",
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewReader([]byte("missing auth"))),
					Request:    r,
				}, nil
			}

			buf := bytes.NewBuffer(nil)
			_ = json.NewEncoder(buf).Encode(ResourceResponse{ID: "123"})
			h := make(http.Header)
			h.Set("Content-Type", "application/json")
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     h,
				Body:       io.NopCloser(bytes.NewReader(buf.Bytes())),
				Request:    r,
			}, nil
		}),
	}

	m := newTokenManager("cid", "secret", client)
	m.token = "abc"
	m.expiresAt = time.Now().Add(1 * time.Hour)

	c := newBotClient(m, client)

	var out ResourceResponse
	if err := c.doJSON(context.Background(), http.MethodPost, "https://bot.invalid", map[string]string{"x": "y"}, &out); err != nil {
		t.Fatalf("doJSON err: %v", err)
	}
	if out.ID != "123" {
		t.Fatalf("unexpected response id: %q", out.ID)
	}
}
