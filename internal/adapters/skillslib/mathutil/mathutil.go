package mathutil

// MinInt returns the smaller of two ints.
func MinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// DefaultPositiveInt returns def if value is <= 0.
func DefaultPositiveInt(value, def int) int {
	if value <= 0 {
		return def
	}
	return value
}

// DefaultPositiveFloat returns def if value is <= 0.
func DefaultPositiveFloat(value, def float64) float64 {
	if value <= 0 {
		return def
	}
	return value
}
