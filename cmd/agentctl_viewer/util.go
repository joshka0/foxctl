package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ulidRegex validates ULID format: 26 characters of Crockford's Base32
var ulidRegex = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

// validateJobID ensures jobID is a valid ULID to prevent path traversal attacks.
// Returns error if jobID contains path separators, dots, or doesn't match ULID format.
func validateJobID(jobID string) error {
	if jobID == "" {
		return fmt.Errorf("job ID cannot be empty")
	}
	if strings.ContainsAny(jobID, "/\\") {
		return fmt.Errorf("job ID contains path separators")
	}
	if strings.Contains(jobID, "..") {
		return fmt.Errorf("job ID contains path traversal sequence")
	}
	if !ulidRegex.MatchString(strings.ToUpper(jobID)) {
		return fmt.Errorf("job ID is not a valid ULID format")
	}
	return nil
}

// getJobDir returns the validated job directory path.
// Returns error if jobID validation fails or the path escapes the jobs root.
func getJobDir(jobID string) (string, error) {
	if err := validateJobID(jobID); err != nil {
		return "", err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot get home directory: %w", err)
	}

	jobsRoot := filepath.Join(home, ".agentctl", "jobs")
	jobDir := filepath.Join(jobsRoot, jobID)

	// Clean and verify the path doesn't escape jobs root
	cleanPath := filepath.Clean(jobDir)
	if !strings.HasPrefix(cleanPath, jobsRoot) {
		return "", fmt.Errorf("job path escapes jobs directory")
	}

	return cleanPath, nil
}

// getWorkspace returns the workspace path, defaulting to cwd if empty.
func getWorkspace(workspace string) string {
	if workspace == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "."
		}
		return cwd
	}
	return workspace
}

// marshalSkillInput safely marshals skill input to JSON.
func marshalSkillInput(input skillInput) (string, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("failed to marshal skill input: %w", err)
	}
	return string(data), nil
}

// writeJSON encodes value as indented JSON to stdout.
func writeJSON(v any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(v) //nolint:errcheck
}

// stateColor returns a lipgloss style for the given job state.
func stateColor(state string) lipgloss.Style {
	switch state {
	case "ok":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	case "error":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	case "running":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	case "queued":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	case "canceled":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	default:
		return lipgloss.NewStyle()
	}
}

// truncate shortens a string to max runes with ellipsis.
// Handles UTF-8 safely by working with runes, not bytes.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if max <= 3 {
		return strings.Repeat(".", max)
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-3]) + "..."
}

// safeSlice returns the first n characters of s, or s if shorter.
// Safe for use with potentially short strings like job IDs.
func safeSlice(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// parseScopes parses comma-separated scope list, returning defaults if empty.
func parseScopes(scopeStr string) []string {
	if scopeStr == "" {
		return nil // let the handler use defaults
	}
	parts := strings.Split(scopeStr, ",")
	scopes := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			scopes = append(scopes, p)
		}
	}
	if len(scopes) == 0 {
		return nil
	}
	return scopes
}

// titleCase returns the string with the first letter capitalized.
// This is a simple replacement for the deprecated strings.Title for ASCII strings.
func titleCase(s string) string {
	if len(s) == 0 {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}

// sourceColor returns a lipgloss style for the given search source.
func sourceColor(source string) lipgloss.Style {
	switch source {
	case "symbol", "symbols":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("39")) // blue
	case "session", "sessions":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("135")) // purple
	case "memory", "memories":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("220")) // yellow
	case "task", "tasks":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("82")) // green
	default:
		return lipgloss.NewStyle()
	}
}
