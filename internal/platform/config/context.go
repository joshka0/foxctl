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

// MustFromContext retrieves the configuration from context, panicking if not present.
func MustFromContext(ctx context.Context) Config {
	cfg, ok := FromContext(ctx)
	if !ok {
		panic("config: not found in context")
	}
	return cfg
}
