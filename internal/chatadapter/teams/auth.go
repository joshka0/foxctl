package teams

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	botFrameworkTokenURL = "https://login.microsoftonline.com/botframework.com/oauth2/v2.0/token"
	botFrameworkScope    = "https://api.botframework.com/.default"
)

type tokenManager struct {
	clientID     string
	clientSecret string

	tokenURL string
	scope    string

	httpClient *http.Client
	now        func() time.Time

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

func newTokenManager(clientID, clientSecret string, httpClient *http.Client) *tokenManager {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &tokenManager{
		clientID:     strings.TrimSpace(clientID),
		clientSecret: strings.TrimSpace(clientSecret),
		tokenURL:     botFrameworkTokenURL,
		scope:        botFrameworkScope,
		httpClient:   httpClient,
		now:          time.Now,
	}
}

func (m *tokenManager) Token(ctx context.Context) (string, error) {
	if strings.TrimSpace(m.clientID) == "" {
		return "", fmt.Errorf("teams: missing client id")
	}
	if strings.TrimSpace(m.clientSecret) == "" {
		return "", fmt.Errorf("teams: missing client secret")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Refresh ~5 minutes before expiry.
	if m.token != "" && m.now().Before(m.expiresAt.Add(-5*time.Minute)) {
		return m.token, nil
	}

	tok, exp, err := m.exchange(ctx)
	if err != nil {
		return "", err
	}

	m.token = tok
	m.expiresAt = exp
	return m.token, nil
}

func (m *tokenManager) exchange(parent context.Context) (token string, expiresAt time.Time, _ error) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", m.clientID)
	form.Set("client_secret", m.clientSecret)
	form.Set("scope", m.scope)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.tokenURL, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("teams: create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("teams: token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, fmt.Errorf("teams: token endpoint returned HTTP %d", resp.StatusCode)
	}

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", time.Time{}, fmt.Errorf("teams: decode token response: %w", err)
	}
	if strings.TrimSpace(tr.AccessToken) == "" {
		return "", time.Time{}, fmt.Errorf("teams: empty access_token in token response")
	}
	if tr.ExpiresIn <= 0 {
		// Default to 1 hour if not provided (conservative).
		tr.ExpiresIn = int64((1 * time.Hour).Seconds())
	}

	return tr.AccessToken, m.now().Add(time.Duration(tr.ExpiresIn) * time.Second), nil
}
