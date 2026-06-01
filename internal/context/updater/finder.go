package updater

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const sourceIDLimit = 8

// MemorySearcher searches the memory store.
type MemorySearcher interface {
	// SearchByQuery searches memories by text query.
	// If workspace is empty, searches all workspaces.
	SearchByQuery(ctx context.Context, workspace, query string, limit int) ([]MemoryResult, error)
}

// MemoryResult represents a memory search result.
type MemoryResult struct {
	ID      string
	Type    string // gotcha, pattern, decision, context
	Summary string
	Score   float32
}

// SessionSearcher searches past sessions.
type SessionSearcher interface {
	// SearchSessions searches past session learnings.
	SearchSessions(ctx context.Context, query string, limit int) ([]SessionResult, error)
}

// SessionResult represents a session search result.
type SessionResult struct {
	SessionID string
	Content   string
	Type      string // learning, decision, gotcha
	Score     float32
}

// CodemapSearcher searches codemaps.
type CodemapSearcher interface {
	// SearchCodemaps searches code relationship maps.
	SearchCodemaps(ctx context.Context, query string, limit int) ([]CodemapResult, error)
}

// CodemapResult represents a codemap search result.
type CodemapResult struct {
	ID      string
	Query   string
	Summary string
	Score   float32
}

// Finder searches for relevant context across multiple sources.
type Finder struct {
	memory   MemorySearcher
	sessions SessionSearcher
	codemaps CodemapSearcher
	config   FinderConfig
}

// FinderConfig configures the context finder.
type FinderConfig struct {
	// MaxResultsPerSource limits results from each source.
	MaxResultsPerSource int

	// MinScore is the minimum score to include a result.
	MinScore float32

	// BoostGotchas increases the score for gotcha-type results.
	BoostGotchas float32

	// BoostFileSpecific increases score for file-specific results.
	BoostFileSpecific float32
}

// DefaultFinderConfig returns default finder configuration.
func DefaultFinderConfig() FinderConfig {
	return FinderConfig{
		MaxResultsPerSource: 5,
		MinScore:            0.5,
		BoostGotchas:        0.15,
		BoostFileSpecific:   0.1,
	}
}

// NewFinder creates a new context finder.
func NewFinder(memory MemorySearcher, sessions SessionSearcher, codemaps CodemapSearcher, config FinderConfig) *Finder {
	return &Finder{
		memory:   memory,
		sessions: sessions,
		codemaps: codemaps,
		config:   config,
	}
}

// FindContext searches for relevant context based on analysis results.
// The workspace parameter is used to scope memory searches to the session's workspace.
func (f *Finder) FindContext(ctx context.Context, analysis *AnalysisResult, sessionID, workspace string) ([]ContextCandidate, error) {
	if analysis == nil || len(analysis.SearchQueries) == 0 {
		return nil, nil
	}

	var candidates []ContextCandidate

	// Search each query across all sources
	for _, query := range analysis.SearchQueries {
		// Search memories
		if f.memory != nil {
			memResults, err := f.memory.SearchByQuery(ctx, workspace, query, f.config.MaxResultsPerSource)
			if err == nil {
				for _, r := range memResults {
					score := r.Score
					// Boost gotchas
					if r.Type == "gotcha" {
						score += f.config.BoostGotchas
					}
					// Boost file-specific if matches active files
					if matchesActiveFiles(r.Summary, analysis.FilesActive) {
						score += f.config.BoostFileSpecific
					}

					if score >= f.config.MinScore {
						candidates = append(candidates, ContextCandidate{
							ID:        r.ID,
							Type:      "memory",
							Content:   formatMemoryContent(r),
							Source:    fmt.Sprintf("memory:%s", r.Type),
							Score:     score,
							Query:     query,
							Timestamp: time.Now(),
						})
					}
				}
			}
		}

		// Search past sessions
		if f.sessions != nil {
			sessResults, err := f.sessions.SearchSessions(ctx, query, f.config.MaxResultsPerSource)
			if err == nil {
				for _, r := range sessResults {
					// Skip current session
					if r.SessionID == sessionID {
						continue
					}

					score := r.Score
					if r.Type == "gotcha" || r.Type == "learning" {
						score += f.config.BoostGotchas
					}

					if score >= f.config.MinScore {
						candidates = append(candidates, ContextCandidate{
							ID:        r.SessionID + ":" + truncate(r.Content, 20),
							Type:      "session",
							Content:   formatSessionContent(r),
							Source:    fmt.Sprintf("session:%s", shortSourceID(r.SessionID)),
							Score:     score,
							Query:     query,
							Timestamp: time.Now(),
						})
					}
				}
			}
		}

		// Search codemaps
		if f.codemaps != nil {
			cmResults, err := f.codemaps.SearchCodemaps(ctx, query, f.config.MaxResultsPerSource)
			if err == nil {
				for _, r := range cmResults {
					if r.Score >= f.config.MinScore {
						candidates = append(candidates, ContextCandidate{
							ID:        r.ID,
							Type:      "codemap",
							Content:   formatCodemapContent(r),
							Source:    fmt.Sprintf("codemap:%s", shortSourceID(r.ID)),
							Score:     r.Score,
							Query:     query,
							Timestamp: time.Now(),
						})
					}
				}
			}
		}
	}

	// Deduplicate by ID
	candidates = deduplicateCandidates(candidates)

	// Sort by score (descending)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	// Limit total results
	if len(candidates) > 10 {
		candidates = candidates[:10]
	}

	return candidates, nil
}

// matchesActiveFiles checks if content mentions any active files.
func matchesActiveFiles(content string, activeFiles []string) bool {
	contentLower := strings.ToLower(content)
	for _, file := range activeFiles {
		// Check filename (not full path)
		filename := baseName(file)
		if filename == "" {
			continue
		}
		if strings.Contains(contentLower, strings.ToLower(filename)) {
			return true
		}
	}
	return false
}

func baseName(path string) string {
	path = strings.TrimSpace(path)
	parts := strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// formatMemoryContent formats a memory result for injection.
func formatMemoryContent(r MemoryResult) string {
	prefix := ""
	switch r.Type {
	case "gotcha":
		prefix = "**Gotcha:** "
	case "pattern":
		prefix = "**Pattern:** "
	case "decision":
		prefix = "**Decision:** "
	default:
		prefix = "**Note:** "
	}
	return prefix + r.Summary
}

// formatSessionContent formats a session result for injection.
func formatSessionContent(r SessionResult) string {
	prefix := ""
	switch r.Type {
	case "learning":
		prefix = "**Previous learning:** "
	case "gotcha":
		prefix = "**Previous gotcha:** "
	case "decision":
		prefix = "**Previous decision:** "
	default:
		prefix = "**From past session:** "
	}
	return prefix + truncate(r.Content, 200)
}

// formatCodemapContent formats a codemap result for injection.
func formatCodemapContent(r CodemapResult) string {
	return fmt.Sprintf("**Code relationship (%s):** %s", r.Query, truncate(r.Summary, 200))
}

// deduplicateCandidates removes duplicate candidates by ID, keeping highest score.
func deduplicateCandidates(candidates []ContextCandidate) []ContextCandidate {
	seen := make(map[string]int) // ID -> index in result
	result := make([]ContextCandidate, 0, len(candidates))

	for _, c := range candidates {
		if idx, ok := seen[c.ID]; ok {
			// Keep the one with higher score
			if c.Score > result[idx].Score {
				result[idx] = c
			}
		} else {
			seen[c.ID] = len(result)
			result = append(result, c)
		}
	}

	return result
}

// truncate shortens a string to max length, adding ellipsis if needed.
func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

func shortSourceID(id string) string {
	if id == "" {
		return "unknown"
	}
	runes := []rune(id)
	if len(runes) <= sourceIDLimit {
		return id
	}
	return string(runes[:sourceIDLimit])
}
