package authbroker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// TokenRow represents a stored token.
type TokenRow struct {
	ID           string
	TenantID     string
	Subject      string
	Provider     Provider
	ScopesHash   string
	TokenJSONEnc []byte
	CreatedAtMS  int64
	UpdatedAtMS  int64
	ExpiresAtMS  int64
}

// AuthRequestRow represents a pending auth request.
type AuthRequestRow struct {
	ID             string
	TenantID       string
	Subject        string
	Provider       Provider
	Scopes         string
	StateNonce     string
	ConversationID string
	ReplyContext   string
	CreatedAtMS    int64
	ExpiresAtMS    int64
	CompletedAtMS  *int64
}

// Store persists OAuth tokens and auth requests.
type Store interface {
	Close() error

	// Token operations.
	UpsertToken(ctx context.Context, row TokenRow) error
	GetToken(ctx context.Context, tenantID, subject string, provider Provider, scopesHash string) (*TokenRow, error)
	DeleteToken(ctx context.Context, tenantID, subject string, provider Provider) error
	DeleteExpiredTokens(ctx context.Context, beforeMS int64) (int64, error)

	// Auth request operations.
	CreateAuthRequest(ctx context.Context, row AuthRequestRow) error
	GetAuthRequest(ctx context.Context, id string) (*AuthRequestRow, error)
	GetAuthRequestByNonce(ctx context.Context, stateNonce string) (*AuthRequestRow, error)
	CompleteAuthRequest(ctx context.Context, id string, completedAtMS int64) error
	DeleteExpiredAuthRequests(ctx context.Context, beforeMS int64) (int64, error)
}

// Clock abstracts time for deterministic tests.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// ScopesHash computes a stable hash of sorted scopes + optional audience.
func ScopesHash(scopes []string, audience string) string {
	sorted := make([]string, len(scopes))
	copy(sorted, scopes)
	sort.Strings(sorted)
	h := sha256.New()
	h.Write([]byte(strings.Join(sorted, " ")))
	if audience != "" {
		h.Write([]byte("|" + audience))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func validateAuthRequestForCreate(row AuthRequestRow) error {
	if strings.TrimSpace(row.ID) == "" {
		return fmt.Errorf("auth request id is required")
	}
	if strings.TrimSpace(row.TenantID) == "" {
		return fmt.Errorf("auth request tenant_id is required")
	}
	if strings.TrimSpace(row.Subject) == "" {
		return fmt.Errorf("auth request subject is required")
	}
	if !isKnownProvider(row.Provider) {
		return fmt.Errorf("auth request provider %q is invalid", row.Provider)
	}
	if strings.TrimSpace(row.StateNonce) == "" {
		return fmt.Errorf("auth request state nonce is required")
	}
	if strings.TrimSpace(row.ConversationID) == "" {
		return fmt.Errorf("auth request conversation_id is required")
	}
	if row.ExpiresAtMS <= row.CreatedAtMS {
		return fmt.Errorf("auth request expires_at_ms must be after created_at_ms")
	}
	if row.CompletedAtMS != nil {
		return fmt.Errorf("auth request must be pending when created")
	}
	return nil
}

func isKnownProvider(provider Provider) bool {
	switch provider {
	case ProviderMicrosoftGraph, ProviderGoogle, ProviderGitHub:
		return true
	default:
		return false
	}
}
