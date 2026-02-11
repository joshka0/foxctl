package identity

import "context"

type contextKey struct{}

// WithPrincipal returns a new context with the given principal.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}

// FromContext extracts the principal from context. Returns zero Principal if absent.
func FromContext(ctx context.Context) Principal {
	p, _ := ctx.Value(contextKey{}).(Principal)
	return p
}
