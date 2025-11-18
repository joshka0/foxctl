// Package auth provides authentication strategies for OpenAPI HTTP requests.
package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Strategy defines how to authenticate an HTTP request.
type Strategy interface {
	Apply(req *http.Request, config Config) error
}

// Config holds authentication parameters for different auth types.
type Config struct {
	Type         string `json:"type"`
	Token        string `json:"token"`
	APIKey       string `json:"api_key"`
	Header       string `json:"header"`
	Query        string `json:"query"`
	User         string `json:"user"`
	Pass         string `json:"pass"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	TokenURL     string `json:"token_url"`
	Scopes       string `json:"scopes"`
}

// Bearer implements Authorization: Bearer <token> authentication.
type Bearer struct{}

// Apply adds a Bearer token to the Authorization header.
func (b Bearer) Apply(req *http.Request, cfg Config) error {
	if cfg.Token == "" {
		return fmt.Errorf("bearer: missing token")
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	return nil
}

// APIKey implements custom header or query parameter authentication.
type APIKey struct{}

// Apply adds an API key to either a header or query parameter.
func (a APIKey) Apply(req *http.Request, cfg Config) error {
	if cfg.APIKey == "" {
		return fmt.Errorf("apiKey: missing api_key")
	}
	if cfg.Header != "" {
		req.Header.Set(cfg.Header, cfg.APIKey)
		return nil
	}
	if cfg.Query != "" {
		q := req.URL.Query()
		q.Set(cfg.Query, cfg.APIKey)
		req.URL.RawQuery = q.Encode()
		return nil
	}
	return fmt.Errorf("apiKey: must specify header or query field")
}

// Basic implements HTTP basic authentication.
type Basic struct{}

// Apply adds HTTP Basic auth credentials to the Authorization header.
func (b Basic) Apply(req *http.Request, cfg Config) error {
	if cfg.User == "" {
		return fmt.Errorf("basic: missing user")
	}
	creds := cfg.User + ":" + cfg.Pass
	encoded := base64.StdEncoding.EncodeToString([]byte(creds))
	req.Header.Set("Authorization", "Basic "+encoded)
	return nil
}

// OAuth2 implements OAuth2 client credentials flow.
type OAuth2 struct {
	mu          sync.RWMutex
	cachedToken string
	expiresAt   time.Time
	httpClient  *http.Client
}

// tokenResponse represents the OAuth2 token endpoint response.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

// NewOAuth2 creates a new OAuth2 strategy with optional HTTP client.
func NewOAuth2(client *http.Client) *OAuth2 {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &OAuth2{httpClient: client}
}

// Apply obtains an access token and adds it to the request.
func (o *OAuth2) Apply(req *http.Request, cfg Config) error {
	if cfg.ClientID == "" {
		return fmt.Errorf("oauth2: missing client_id")
	}
	if cfg.ClientSecret == "" {
		return fmt.Errorf("oauth2: missing client_secret")
	}
	if cfg.TokenURL == "" {
		return fmt.Errorf("oauth2: missing token_url")
	}

	// Check cached token
	o.mu.RLock()
	if o.cachedToken != "" && time.Now().Before(o.expiresAt) {
		token := o.cachedToken
		o.mu.RUnlock()
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
	o.mu.RUnlock()

	// Exchange credentials for token
	token, expiresIn, err := o.exchangeCredentials(cfg)
	if err != nil {
		return fmt.Errorf("oauth2: token exchange failed: %w", err)
	}

	// Cache token with 30 second buffer before expiry
	o.mu.Lock()
	o.cachedToken = token
	if expiresIn > 30 {
		o.expiresAt = time.Now().Add(time.Duration(expiresIn-30) * time.Second)
	} else {
		o.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	}
	o.mu.Unlock()

	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

func (o *OAuth2) exchangeCredentials(cfg Config) (string, int, error) {
	// Prepare token request
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", cfg.ClientID)
	data.Set("client_secret", cfg.ClientSecret)
	if cfg.Scopes != "" {
		data.Set("scope", cfg.Scopes)
	}

	// Create HTTP request
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tokenReq, err := http.NewRequestWithContext(ctx, "POST", cfg.TokenURL, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("create request: %w", err)
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Execute request
	resp, err := o.httpClient.Do(tokenReq)
	if err != nil {
		return "", 0, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}

	// Parse response
	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", 0, fmt.Errorf("decode response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return "", 0, fmt.Errorf("empty access_token in response")
	}

	return tokenResp.AccessToken, tokenResp.ExpiresIn, nil
}

// NewStrategy returns the appropriate authentication strategy for the given type.
func NewStrategy(authType string) (Strategy, error) {
	switch authType {
	case "bearer":
		return Bearer{}, nil
	case "apiKey":
		return APIKey{}, nil
	case "basic":
		return Basic{}, nil
	case "oauth2":
		return NewOAuth2(nil), nil
	case "":
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown auth type: %s", authType)
	}
}

// Apply is a convenience helper that creates a strategy and applies it.
func Apply(req *http.Request, cfg Config) error {
	if cfg.Type == "" {
		return nil
	}
	strat, err := NewStrategy(cfg.Type)
	if err != nil {
		return err
	}
	if strat == nil {
		return nil
	}
	return strat.Apply(req, cfg)
}
