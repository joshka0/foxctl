package sliceutil

// Limit returns the slice truncated to limit elements.
// If limit <= 0 or within bounds, returns the original slice.
func Limit[T any](items []T, limit int) []T {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

// LimitWithTruncated returns the truncated slice and whether truncation occurred.
func LimitWithTruncated[T any](items []T, limit int) ([]T, bool) {
	if limit <= 0 || len(items) <= limit {
		return items, false
	}
	return items[:limit], true
}

// Clone returns a copy of the slice, or an empty slice when nil.
func Clone[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	out := make([]T, len(items))
	copy(out, items)
	return out
}
