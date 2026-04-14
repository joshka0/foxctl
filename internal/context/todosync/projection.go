package todosync

import (
	"fmt"
	"regexp"
	"strings"
)

// Status glyphs for visual indication
const (
	GlyphInProgress = "▶"
	GlyphPending    = "□"
	GlyphCompleted  = "✓"
	GlyphBlocked    = "⛔"
	GlyphCanceled   = "✕"
)

// Status constants matching agentctl task status values
const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusBlocked    = "blocked"
	StatusCanceled   = "canceled"
)

// ProjectionConfig controls how todos are formatted for output
type ProjectionConfig struct {
	IncludeGlyphs    bool // Add status glyphs (▶, □, ✓)
	IncludeDepHints  bool // Add dependency hints (⛓n)
	MaxContentLength int  // Max content length before tag (0 = no limit)
}

// DefaultProjectionConfig returns the recommended projection settings
func DefaultProjectionConfig() ProjectionConfig {
	return ProjectionConfig{
		IncludeGlyphs:    true,
		IncludeDepHints:  true,
		MaxContentLength: 80,
	}
}

// StatusGlyph returns the glyph for a given status
func StatusGlyph(status string) string {
	switch status {
	case StatusInProgress:
		return GlyphInProgress
	case StatusCompleted:
		return GlyphCompleted
	case StatusBlocked:
		return GlyphBlocked
	case StatusCanceled:
		return GlyphCanceled
	case StatusPending:
		return GlyphPending
	default:
		return GlyphPending
	}
}

// FormatContent formats task content for Claude Code projection.
// It adds status glyph, dependency hints, and task ID tag.
func FormatContent(title, status, taskID string, unresolvedDeps int, cfg ProjectionConfig) string {
	var parts []string

	// Status glyph prefix
	if cfg.IncludeGlyphs {
		parts = append(parts, StatusGlyph(status))
	}

	// Main title content (truncate if needed)
	content := title
	if cfg.MaxContentLength > 0 && len(content) > cfg.MaxContentLength {
		content = content[:cfg.MaxContentLength-3] + "..."
	}
	parts = append(parts, content)

	// Dependency hint suffix
	if cfg.IncludeDepHints && unresolvedDeps > 0 {
		parts = append(parts, fmt.Sprintf("⛓%d", unresolvedDeps))
	}

	// Join parts and append task ID tag
	result := strings.Join(parts, " ")
	return AppendTaskID(result, taskID)
}

// ParseProjectedContent extracts the original title from projected content.
// It removes glyphs, dependency hints, and task ID tag.
func ParseProjectedContent(content string) string {
	// Remove task ID tag first
	content = StripTaskID(content)

	// Remove dependency hints (⛓n pattern)
	depHintRe := regexp.MustCompile(`\s*⛓\d+\s*`)
	content = depHintRe.ReplaceAllString(content, "")

	// Remove status glyphs
	for _, glyph := range []string{GlyphInProgress, GlyphPending, GlyphCompleted, GlyphBlocked, GlyphCanceled} {
		content = strings.TrimPrefix(content, glyph+" ")
		content = strings.TrimPrefix(content, glyph)
	}

	return strings.TrimSpace(content)
}

// ClaudeTodo represents a todo item as stored in Claude Code's JSON file
type ClaudeTodo struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"activeForm"`
}

// MapClaudeStatus maps Claude Code status to agentctl task status
func MapClaudeStatus(claudeStatus string) string {
	switch claudeStatus {
	case "in_progress":
		return StatusInProgress
	case "completed":
		return StatusCompleted
	case "pending":
		return StatusPending
	default:
		return StatusPending
	}
}

// MapAgentctlStatus maps agentctl task status to Claude Code status
func MapAgentctlStatus(agentctlStatus string) string {
	switch agentctlStatus {
	case StatusInProgress:
		return "in_progress"
	case StatusCompleted:
		return "completed"
	case StatusBlocked:
		return "pending" // Claude doesn't have blocked, map to pending
	case StatusCanceled:
		return "completed" // Claude doesn't have canceled, map to completed (task is done)
	case StatusPending:
		return "pending"
	default:
		return "pending"
	}
}

// GenerateActiveForm creates the activeForm text from title
// Claude Code uses this for the "ing" form of the task
func GenerateActiveForm(title string) string {
	// Simple heuristic: if title starts with a verb, convert to -ing form
	// Otherwise, just use the title as-is
	words := strings.Fields(title)
	if len(words) == 0 {
		return title
	}

	// Convert common verb prefixes to -ing form
	first := strings.ToLower(words[0])
	switch first {
	case "run":
		words[0] = "Running"
	case "fix":
		words[0] = "Fixing"
	case "add":
		words[0] = "Adding"
	case "update":
		words[0] = "Updating"
	case "create":
		words[0] = "Creating"
	case "implement":
		words[0] = "Implementing"
	case "remove":
		words[0] = "Removing"
	case "refactor":
		words[0] = "Refactoring"
	case "test":
		words[0] = "Testing"
	case "review":
		words[0] = "Reviewing"
	case "check":
		words[0] = "Checking"
	case "verify":
		words[0] = "Verifying"
	case "build":
		words[0] = "Building"
	case "write":
		words[0] = "Writing"
	case "read":
		words[0] = "Reading"
	}

	return strings.Join(words, " ")
}
