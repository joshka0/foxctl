// Package maputil provides small helpers for working with map[string]any values.
package maputil

// AsStringMap returns the map if the value is a map[string]any.
func AsStringMap(value any) (map[string]any, bool) {
	m, ok := value.(map[string]any)
	return m, ok
}

// MapOrEmpty returns the map if the value is a map[string]any, or an empty map otherwise.
func MapOrEmpty(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
