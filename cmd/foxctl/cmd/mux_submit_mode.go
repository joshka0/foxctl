package cmd

import (
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/runtime/terminal/tmuxbridge"
)

// parseMuxSubmitModeString maps CLI values to tmuxbridge/zellijbridge submit modes.
func parseMuxSubmitModeString(raw string) (string, error) {
	s := strings.TrimSpace(strings.ToLower(strings.ReplaceAll(raw, "_", "-")))
	switch s {
	case "", "escape-enter":
		return tmuxbridge.SubmitModeEscapeEnter, nil
	case "enter-only":
		return tmuxbridge.SubmitModeEnterOnly, nil
	default:
		return "", fmt.Errorf("unsupported submit mode %q", raw)
	}
}
