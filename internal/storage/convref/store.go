package convref

import "context"

// Ref stores a stable reference to a chat conversation.
//
// Times are milliseconds since Unix epoch.
type Ref struct {
	ConversationKey   string
	Platform          string
	TenantID          string
	RawConversationID string
	ServiceURL        string
	LastActivityID    string
	BotID             string
	CreatedAtMS       int64
	UpdatedAtMS       int64
}

// Store persists conversation references.
type Store interface {
	// Close releases any resources held by the store implementation.
	Close() error
	// Upsert inserts or updates a conversation reference.
	Upsert(ctx context.Context, ref Ref) error
	// Get retrieves a reference by conversation key. It returns (nil, nil) if missing.
	Get(ctx context.Context, conversationKey string) (*Ref, error)
	// Delete removes a reference by conversation key.
	Delete(ctx context.Context, conversationKey string) error

	// DeleteStale deletes refs whose UpdatedAtMS is older than olderThanMS (ms since epoch).
	// It returns the number of rows deleted.
	DeleteStale(ctx context.Context, olderThanMS int64) (int64, error)
}
