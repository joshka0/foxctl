package teams

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestTokenManager_CachesAndRefreshes(t *testing.T) {
	t.Parallel()

	var reqCount atomic.Int64

	client := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			n := reqCount.Add(1)
			buf := bytes.NewBuffer(nil)
			_ = json.NewEncoder(buf).Encode(map[string]any{
				"access_token": "tok-" + strconv.FormatInt(n, 10),
				"expires_in":   3600,
				"token_type":   "Bearer",
			})
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

	now := time.Date(2026, 2, 9, 12, 0, 0, 0, time.UTC)
	m := newTokenManager("cid", "secret", client)
	m.tokenURL = "https://token.invalid"
	m.now = func() time.Time { return now }

	tok1, err := m.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() err: %v", err)
	}
	if tok1 == "" {
		t.Fatalf("Token() returned empty token")
	}
	if got := reqCount.Load(); got != 1 {
		t.Fatalf("expected 1 token request, got %d", got)
	}

	tok2, err := m.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() err: %v", err)
	}
	if tok2 != tok1 {
		t.Fatalf("expected cached token, got %q != %q", tok2, tok1)
	}
	if got := reqCount.Load(); got != 1 {
		t.Fatalf("expected 1 token request after cache hit, got %d", got)
	}

	// Advance into the refresh window (<5m to expiry) so we fetch a new token.
	now = now.Add(56 * time.Minute)
	_, err = m.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() err: %v", err)
	}
	if got := reqCount.Load(); got != 2 {
		t.Fatalf("expected refresh token request, got %d", got)
	}
}
