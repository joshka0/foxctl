package authbroker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
