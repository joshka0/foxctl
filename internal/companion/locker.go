package companion

import "context"

// Locker abstracts per-conversation locking for both in-memory and Postgres implementations.
// The returned unlock function is safe to call exactly once; callers must not call it again.
type Locker interface {
	// Lock acquires the lock for the given conversation, respecting context cancellation.
	Lock(ctx context.Context, conversationID string) (unlock func(), err error)
}
