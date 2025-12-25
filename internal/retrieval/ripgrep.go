package retrieval

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
)

// ripgrepFallback uses ripgrep to find files containing query keywords.
// This is a fallback when the symbol/semantic indexes are sparse.
func (g *Generator) ripgrepFallback(ctx context.Context, question string, limit int) ([]Candidate, error) {
	if g.workspaceRoot == "" {
		return nil, nil
	}

	// Extract keywords from the question
	keywords := extractKeywords(question)
	if len(keywords) == 0 {
		return nil, nil
	}

	// Build the pattern (OR of keywords)
	pattern := buildRipgrepPattern(keywords)
	if pattern == "" {
		return nil, nil
	}

	// Run ripgrep in files-with-matches mode
	args := []string{
		"--files-with-matches", // Only list file paths
		"--ignore-case",        // Case insensitive
		"--max-count", "1",     // Stop after first match per file
		"--no-heading",
		"--type", "go",
		"--type", "py",
		"--type", "ts",
		"--type", "js",
		"-g", "!vendor/**",
		"-g", "!node_modules/**",
		"-g", "!.git/**",
		"-g", "!dist/**",
		"-g", "!build/**",
		pattern,
		g.workspaceRoot,
	}

	cmd := exec.CommandContext(ctx, "rg", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// Exit code 1 = no matches (not an error)
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		// Check if ripgrep is not installed
		if strings.Contains(stderr.String(), "not found") || strings.Contains(err.Error(), "executable file not found") {
			g.logger.Debug().Msg("ripgrep not installed, skipping fallback")
			return nil, nil
		}
		g.logger.Debug().Err(err).Str("stderr", stderr.String()).Msg("ripgrep failed")
		return nil, nil
	}

	// Parse file paths from output
	candidates := make([]Candidate, 0, limit)
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(&stdout)
	rank := 0
	for scanner.Scan() && len(candidates) < limit {
		absPath := strings.TrimSpace(scanner.Text())
		if absPath == "" {
			continue
		}

		// Make path relative to workspace
		relPath, err := filepath.Rel(g.workspaceRoot, absPath)
		if err != nil {
			relPath = absPath
		}

		// Clean up path
		relPath = filepath.Clean(relPath)
		if relPath == "." || relPath == "" {
			continue
		}

		// Deduplicate
		if seen[relPath] {
			continue
		}
		seen[relPath] = true

		// Score based on rank (earlier = higher score)
		rank++
		score := 1.0 - (float64(rank) / float64(limit+1))
		if score < 0.1 {
			score = 0.1
		}

		candidates = append(candidates, Candidate{
			Path:     relPath,
			Score:    score,
			RawScore: float64(limit - rank + 1),
			Source:   SourceRipgrep,
		})
	}

	return candidates, nil
}

// extractKeywords extracts significant keywords from a question for ripgrep.
func extractKeywords(question string) []string {
	tokens := tokenize(question)
	if len(tokens) == 0 {
		return nil
	}

	// Filter to more significant tokens (longer = likely more specific)
	var keywords []string
	for _, tok := range tokens {
		if len(tok) >= 4 { // Only tokens with 4+ chars
			keywords = append(keywords, tok)
		}
	}

	// Limit keywords to avoid overly broad searches
	if len(keywords) > 5 {
		keywords = keywords[:5]
	}

	return keywords
}

// buildRipgrepPattern builds a regex pattern from keywords.
// Uses OR logic so any keyword match counts.
func buildRipgrepPattern(keywords []string) string {
	if len(keywords) == 0 {
		return ""
	}

	// Escape special regex characters and join with |
	var escaped []string
	for _, kw := range keywords {
		escaped = append(escaped, escapeRegex(kw))
	}

	return strings.Join(escaped, "|")
}

// escapeRegex escapes special regex characters.
func escapeRegex(s string) string {
	special := `\.+*?^$()[]{}|`
	var result strings.Builder
	for _, r := range s {
		if strings.ContainsRune(special, r) {
			result.WriteByte('\\')
		}
		result.WriteRune(r)
	}
	return result.String()
}
