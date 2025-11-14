// Package auth provides authentication strategies for OpenAPI HTTP requests.
package auth

import (
	"encoding/base64"
	"fmt"
	"net/http"
)

// Strategy defines how to authenticate an HTTP request.
type Strategy interface {
	Apply(req *http.Request, config Config) error
}

// Config holds authentication parameters for different auth types.
type Config struct {
	Type   string `json:"type"`
	Token  string `json:"token"`
	APIKey string `json:"api_key"`
	Header string `json:"header"`
	Query  string `json:"query"`
	User   string `json:"user"`
	Pass   string `json:"pass"`
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

// NewStrategy returns the appropriate authentication strategy for the given type.
func NewStrategy(authType string) (Strategy, error) {
	switch authType {
	case "bearer":
		return Bearer{}, nil
	case "apiKey":
		return APIKey{}, nil
	case "basic":
		return Basic{}, nil
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
