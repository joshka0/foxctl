package workspace

import "context"

type ctxKey struct{}

var workspaceKey ctxKey

// WithContext attaches a workspace path to the context for downstream runners.
func WithContext(ctx context.Context, path string) context.Context {
	if ctx == nil || path == "" {
		return ctx
	}
	return context.WithValue(ctx, workspaceKey, path)
}

// FromContext extracts the workspace path annotated on the context, if any.
func FromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	val, ok := ctx.Value(workspaceKey).(string)
	if !ok || val == "" {
		return "", false
	}
	return val, true
}
