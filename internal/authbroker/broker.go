package authbroker

import (
	"context"

	"github.com/jkatigb/agentctl/internal/domain/identity"
)

type Provider string

const (
	ProviderMicrosoftGraph Provider = "microsoft_graph"
	ProviderGoogle         Provider = "google"
	ProviderGitHub         Provider = "github"
)

type Token struct {
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token,omitempty"`
	TokenType    string   `json:"token_type"`
	ExpiresAt    int64    `json:"expires_at"` // Unix seconds.
	Scopes       []string `json:"scopes"`
}

type GetTokenOpts struct {
	Scopes   []string
	Audience string
	Prompt   string // "consent", "login"
}

type AuthRequired struct {
	Provider      Provider       `json:"provider"`
	Scopes        []string       `json:"scopes"`
	AuthRequestID string         `json:"auth_request_id"`
	Message       string         `json:"message"`
	SignInURL     string         `json:"sign_in_url"`
	Meta          map[string]any `json:"meta,omitempty"`
}

type Broker interface {
	GetToken(ctx context.Context, p identity.Principal, provider Provider, opts GetTokenOpts) (*Token, *AuthRequired, error)
	CompleteAuth(ctx context.Context, authRequestID string, payload any) error
	Revoke(ctx context.Context, p identity.Principal, provider Provider) error
}
