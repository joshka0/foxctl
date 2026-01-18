// Package env provides small helpers for environment variable access.
package env

import (
	"fmt"
	"os"
	"strings"
)

// GetString returns the trimmed value for the env var.
func GetString(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

// GetRequiredString returns the trimmed env var or an error if unset/empty.
func GetRequiredString(key string) (string, error) {
	value := GetString(key)
	if value == "" {
		return "", fmt.Errorf("missing required env var %s", key)
	}
	return value, nil
}
