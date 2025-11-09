package config

import "context"

type ctxKey struct{}

// WithContext stores cfg in the provided context for downstream commands.
func WithContext(ctx context.Context, cfg Config) context.Context {
	return context.WithValue(ctx, ctxKey{}, cfg)
}

// FromContext retrieves the configuration if present.
func FromContext(ctx context.Context) (Config, bool) {
	cfg, ok := ctx.Value(ctxKey{}).(Config)
	return cfg, ok
}
