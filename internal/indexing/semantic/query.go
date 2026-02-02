package semantic

import (
	"fmt"
	"strings"
	"time"
)

// QueryEnricher adds temporal and type context to search queries.
// This enables natural language searches like "January gotchas" or "recent decisions"
// to match embeddings with date/type prefixes.
type QueryEnricher struct {
	now time.Time
}

// NewQueryEnricher creates a new enricher with current time.
func NewQueryEnricher() *QueryEnricher {
	return &QueryEnricher{now: time.Now()}
}

// NewQueryEnricherWithTime creates an enricher with a specific time (for testing).
func NewQueryEnricherWithTime(t time.Time) *QueryEnricher {
	return &QueryEnricher{now: t}
}

// Enrich expands natural language temporal patterns in queries.
// Examples:
//   - "January gotchas" -> "[Jan 2026] [gotcha] January gotchas"
//   - "recent decisions" -> "[Jan 2026] [decision] recent decisions"
//   - "last week debugging" -> "[Jan 2026] [debugging] last week debugging"
func (e *QueryEnricher) Enrich(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return query
	}

	lower := strings.ToLower(query)
	var prefixes []string

	// Detect month references
	months := map[string]time.Month{
		"january": time.January, "jan": time.January,
		"february": time.February, "feb": time.February,
		"march": time.March, "mar": time.March,
		"april": time.April, "apr": time.April,
		"may":  time.May,
		"june": time.June, "jun": time.June,
		"july": time.July, "jul": time.July,
		"august": time.August, "aug": time.August,
		"september": time.September, "sep": time.September, "sept": time.September,
		"october": time.October, "oct": time.October,
		"november": time.November, "nov": time.November,
		"december": time.December, "dec": time.December,
	}

	for name, month := range months {
		if strings.Contains(lower, name) {
			year := e.now.Year()
			// If month is ahead of current month, use previous year
			if month > e.now.Month() {
				year--
			}
			prefixes = append(prefixes, fmt.Sprintf("[%s %d]",
				month.String()[:3], year))
			break // Only add one month prefix
		}
	}

	// Detect relative time patterns
	if strings.Contains(lower, "last week") || strings.Contains(lower, "past week") {
		weekAgo := e.now.AddDate(0, 0, -7)
		prefixes = append(prefixes, fmt.Sprintf("[%s]", weekAgo.Format("Jan 2006")))
	} else if strings.Contains(lower, "recent") || strings.Contains(lower, "latest") || strings.Contains(lower, "this month") {
		prefixes = append(prefixes, fmt.Sprintf("[%s]", e.now.Format("Jan 2006")))
	} else if strings.Contains(lower, "last month") || strings.Contains(lower, "previous month") {
		lastMonth := e.now.AddDate(0, -1, 0)
		prefixes = append(prefixes, fmt.Sprintf("[%s]", lastMonth.Format("Jan 2006")))
	}

	// Detect year references (e.g., "2025 learnings", "2026 decisions")
	for year := e.now.Year() - 2; year <= e.now.Year()+1; year++ {
		yearStr := fmt.Sprintf("%d", year)
		if strings.Contains(lower, yearStr) {
			// Year is already in query, don't add prefix
			break
		}
	}

	// Detect memory type references
	typeMap := map[string]string{
		"gotcha": "gotcha", "gotchas": "gotcha",
		"decision": "decision", "decisions": "decision",
		"learning": "learning", "learnings": "learning",
		"preference": "preference", "preferences": "preference",
		"anti-pattern": "anti_pattern", "anti_pattern": "anti_pattern", "antipattern": "anti_pattern",
	}
	for keyword, typePrefix := range typeMap {
		if strings.Contains(lower, keyword) {
			prefixes = append(prefixes, fmt.Sprintf("[%s]", typePrefix))
			break // Only add one type prefix
		}
	}

	// Detect activity type references (for sessions)
	activityMap := map[string]string{
		"debugging": "debugging", "debug": "debugging",
		"bug fix": "bug-fix", "bugfix": "bug-fix", "fix": "bug-fix",
		"feature": "feature", "implement": "feature",
		"refactor": "refactoring", "refactoring": "refactoring",
		"testing": "testing", "test": "testing",
		"documentation": "documentation", "docs": "documentation", "doc": "documentation",
		"review": "code-review", "code review": "code-review",
		"setup": "setup", "config": "setup",
	}
	for keyword, activity := range activityMap {
		if strings.Contains(lower, keyword) {
			prefixes = append(prefixes, fmt.Sprintf("[%s]", activity))
			break // Only add one activity prefix
		}
	}

	// Detect task status references
	statusMap := map[string]string{
		"completed": "completed", "done": "completed", "finished": "completed",
		"pending": "pending", "todo": "pending",
		"in progress": "in_progress", "in_progress": "in_progress", "ongoing": "in_progress",
		"blocked": "blocked",
	}
	for keyword, status := range statusMap {
		if strings.Contains(lower, keyword) {
			prefixes = append(prefixes, fmt.Sprintf("[%s]", status))
			break // Only add one status prefix
		}
	}

	// Prepend detected prefixes to query for embedding similarity
	if len(prefixes) > 0 {
		return strings.Join(prefixes, " ") + " " + query
	}
	return query
}

// EnrichQuery is a convenience function that creates an enricher and enriches the query.
func EnrichQuery(query string) string {
	return NewQueryEnricher().Enrich(query)
}
