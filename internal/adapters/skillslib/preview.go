// Package skillslib provides utilities shared across skill binaries.
package skillslib

// PreparePreview returns a truncated slice when the collection exceeds limit.
func PreparePreview[T any](items []T, limit int) ([]T, bool) {
	if limit <= 0 || len(items) <= limit {
		return items, false
	}
	return items[:limit], true
}
