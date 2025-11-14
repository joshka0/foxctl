// Package skillslib provides utilities shared across skill binaries.
package skillslib

// PreparePreview returns a truncated slice when the collection exceeds max.
func PreparePreview[T any](items []T, max int) ([]T, bool) {
	if max <= 0 || len(items) <= max {
		return items, false
	}
	return items[:max], true
}
