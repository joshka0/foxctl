package hashutil

import (
	"crypto/sha256"
	"fmt"
)

// ShortHash returns the first 16 hex chars of a SHA256 digest.
func ShortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum)[:16]
}

// FullHash returns the full 64-char hex SHA256 digest.
func FullHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum)
}
