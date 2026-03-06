package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

// SessionRecallRequest describes a semantic recall query over archived sessions.
type SessionRecallRequest struct {
	Query                 string
	Workspace             string
	Limit                 int
	MinSimilarity         float64
	IncludeTimeline       bool
	TimelineSummaryLimit  int
	TimelineLearningLimit int
}

// SessionRecallMatch is a compact session recall item suitable for prompt injection.
type SessionRecallMatch struct {
	SessionID            string
	ProjectName          string
	GitBranch            string
	Summary              string
	Accomplished         []string
	Decisions            []string
	Gotchas              []string
	KeyFiles             []string
	Similarity           float64
	StartedAt            time.Time
	TimelineSummaryLines []string
	TimelineTools        []string
	TimelineFiles        []string
	TimelineDecisions    []string
	TimelineGotchas      []string
	TimelinePreferences  []string
	TimelineAntiPatterns []string
	TimelineLearnings    []string
}

// SessionRecallProvider resolves relevant prior sessions for a query.
type SessionRecallProvider interface {
	RecallSessions(ctx context.Context, req SessionRecallRequest) ([]SessionRecallMatch, error)
}

// SessionStoreRecallProvider implements session recall using sessions.Store with
// query embeddings when available and BM25 fallback otherwise.
type SessionStoreRecallProvider struct {
	Store       *sessions.Store
	Embedder    *semantic.Embedder
	MemoryStore storage.MemoryStore
	Workspace   string
}

func (p *SessionStoreRecallProvider) RecallSessions(ctx context.Context, req SessionRecallRequest) ([]SessionRecallMatch, error) {
	if p == nil || p.Store == nil {
		return nil, nil
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, nil
	}

	workspace := strings.TrimSpace(req.Workspace)
	if workspace == "" {
		workspace = strings.TrimSpace(p.Workspace)
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}
	minSimilarity := req.MinSimilarity
	if minSimilarity <= 0 {
		minSimilarity = 0.3
	}

	if p.Embedder != nil {
		enrichedQuery := semantic.EnrichQuery(query)
		embedding, err := p.Embedder.EmbedQuery(ctx, enrichedQuery)
		if err == nil && len(embedding.Vec) > 0 {
			results, searchErr := p.Store.SearchSimilar(ctx, workspace, embedding.Vec, limit*3)
			if searchErr == nil {
				matches := sessionRecallMatchesFromSimilar(results, limit, minSimilarity)
				return p.enrichMatchesWithTimeline(ctx, workspace, matches, req), nil
			}
		}
	}

	results, err := p.Store.Search(ctx, query, limit*3)
	if err != nil {
		return nil, fmt.Errorf("search sessions: %w", err)
	}
	matches := sessionRecallMatchesFromSessions(results, workspace, limit)
	return p.enrichMatchesWithTimeline(ctx, workspace, matches, req), nil
}

func sessionRecallMatchesFromSimilar(results []storage.SimilarSession, limit int, minSimilarity float64) []SessionRecallMatch {
	matches := make([]SessionRecallMatch, 0, min(limit, len(results)))
	for _, result := range results {
		if result.Similarity < minSimilarity {
			continue
		}
		matches = append(matches, sessionRecallMatchFromSession(result.Session, result.Similarity))
		if len(matches) >= limit {
			break
		}
	}
	return matches
}

func sessionRecallMatchesFromSessions(results []sessions.Session, workspace string, limit int) []SessionRecallMatch {
	workspace = strings.TrimSpace(workspace)
	matches := make([]SessionRecallMatch, 0, min(limit, len(results)))
	for _, result := range results {
		if workspace != "" && workspace != result.WorkspaceID && workspace != result.WorkspacePath {
			continue
		}
		matches = append(matches, sessionRecallMatchFromSession(result, 0.35))
		if len(matches) >= limit {
			break
		}
	}
	return matches
}

func sessionRecallMatchFromSession(session sessions.Session, similarity float64) SessionRecallMatch {
	return SessionRecallMatch{
		SessionID:    session.ID,
		ProjectName:  session.ProjectName,
		GitBranch:    session.GitBranch,
		Summary:      strings.TrimSpace(session.Summary),
		Accomplished: append([]string(nil), session.Accomplished...),
		Decisions:    append([]string(nil), session.Decisions...),
		Gotchas:      append([]string(nil), session.Gotchas...),
		KeyFiles:     append([]string(nil), session.KeyFiles...),
		Similarity:   similarity,
		StartedAt:    session.StartedAt,
	}
}

func formatSessionRecallMatches(matches []SessionRecallMatch) string {
	if len(matches) == 0 {
		return ""
	}

	ordered := append([]SessionRecallMatch(nil), matches...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Similarity == ordered[j].Similarity {
			return ordered[i].SessionID < ordered[j].SessionID
		}
		return ordered[i].Similarity > ordered[j].Similarity
	})

	lines := make([]string, 0, len(ordered)*4)
	for _, match := range ordered {
		header := fmt.Sprintf("- [%s %.2f]", match.SessionID, match.Similarity)
		if project := strings.TrimSpace(match.ProjectName); project != "" {
			header += " " + truncateForPrompt(project, 48)
		}
		if startedAt := match.StartedAt; !startedAt.IsZero() {
			header += " (" + startedAt.UTC().Format("2006-01-02") + ")"
		}
		if summary := strings.TrimSpace(match.Summary); summary != "" {
			header += ": " + truncateForPrompt(strings.ReplaceAll(summary, "\n", " "), 220)
		}
		lines = append(lines, header)

		if len(match.Decisions) > 0 {
			lines = append(lines, "  decisions: "+truncateForPrompt(strings.Join(match.Decisions, "; "), 180))
		}
		if len(match.Gotchas) > 0 {
			lines = append(lines, "  gotchas: "+truncateForPrompt(strings.Join(match.Gotchas, "; "), 180))
		}
		if len(match.KeyFiles) > 0 {
			lines = append(lines, "  files: "+truncateForPrompt(strings.Join(match.KeyFiles, ", "), 160))
		}
		if len(match.TimelineSummaryLines) > 0 {
			lines = append(lines, "  recent_timeline: "+truncateInlineForPrompt(strings.Join(match.TimelineSummaryLines, " | "), 240))
		}
		if len(match.TimelineDecisions) > 0 {
			lines = append(lines, "  recent_decisions: "+truncateInlineForPrompt(strings.Join(match.TimelineDecisions, "; "), 180))
		}
		if len(match.TimelineGotchas) > 0 {
			lines = append(lines, "  recent_gotchas: "+truncateInlineForPrompt(strings.Join(match.TimelineGotchas, "; "), 180))
		}
		if len(match.TimelinePreferences) > 0 {
			lines = append(lines, "  recent_preferences: "+truncateInlineForPrompt(strings.Join(match.TimelinePreferences, "; "), 180))
		}
		if len(match.TimelineAntiPatterns) > 0 {
			lines = append(lines, "  recent_anti_patterns: "+truncateInlineForPrompt(strings.Join(match.TimelineAntiPatterns, "; "), 180))
		}
		if len(match.TimelineLearnings) > 0 {
			lines = append(lines, "  recent_learnings: "+truncateInlineForPrompt(strings.Join(match.TimelineLearnings, "; "), 180))
		}
		if len(match.TimelineTools) > 0 {
			lines = append(lines, "  recent_tools: "+truncateInlineForPrompt(strings.Join(match.TimelineTools, ", "), 140))
		}
		if len(match.TimelineFiles) > 0 {
			lines = append(lines, "  recent_files: "+truncateInlineForPrompt(strings.Join(match.TimelineFiles, ", "), 160))
		}
	}

	return strings.Join(lines, "\n")
}

func (p *SessionStoreRecallProvider) enrichMatchesWithTimeline(ctx context.Context, workspace string, matches []SessionRecallMatch, req SessionRecallRequest) []SessionRecallMatch {
	if len(matches) == 0 || !shouldIncludeSessionTimeline(req) {
		return matches
	}

	enriched := append([]SessionRecallMatch(nil), matches...)
	for i := range enriched {
		enriched[i] = p.enrichMatchWithTimeline(ctx, workspace, enriched[i], req)
	}
	return enriched
}

func shouldIncludeSessionTimeline(req SessionRecallRequest) bool {
	return req.IncludeTimeline && (req.TimelineSummaryLimit > 0 || req.TimelineLearningLimit > 0)
}

func (p *SessionStoreRecallProvider) enrichMatchWithTimeline(ctx context.Context, workspace string, match SessionRecallMatch, req SessionRecallRequest) SessionRecallMatch {
	summaryLines, tools, files := p.sessionTimelineSummaries(ctx, match.SessionID, req.TimelineSummaryLimit)
	match.TimelineSummaryLines = summaryLines
	match.TimelineTools = tools
	match.TimelineFiles = files

	decisions, gotchas, preferences, antiPatterns, learnings := p.sessionTimelineLearnings(ctx, workspace, match.SessionID, req.TimelineLearningLimit)
	match.TimelineDecisions = decisions
	match.TimelineGotchas = gotchas
	match.TimelinePreferences = preferences
	match.TimelineAntiPatterns = antiPatterns
	match.TimelineLearnings = learnings
	return match
}

func (p *SessionStoreRecallProvider) sessionTimelineSummaries(ctx context.Context, sessionID string, limit int) ([]string, []string, []string) {
	if p == nil || p.Store == nil || strings.TrimSpace(sessionID) == "" || limit <= 0 {
		return nil, nil, nil
	}

	windows, err := p.Store.GetContextWindows(ctx, sessionID)
	if err != nil || len(windows) == 0 {
		return nil, nil, nil
	}
	sort.SliceStable(windows, func(i, j int) bool {
		return windows[i].WindowIndex > windows[j].WindowIndex
	})

	lines := make([]string, 0, limit)
	toolSet := make(map[string]struct{})
	fileSet := make(map[string]struct{})

	for _, window := range windows {
		summaries, err := p.Store.GetChunkSummaries(ctx, sessionID, window.WindowIndex)
		if err != nil || len(summaries) == 0 {
			continue
		}
		sort.SliceStable(summaries, func(i, j int) bool {
			if summaries[i].ChunkIndexMax == summaries[j].ChunkIndexMax {
				return summaries[i].ChunkIndexMin > summaries[j].ChunkIndexMin
			}
			return summaries[i].ChunkIndexMax > summaries[j].ChunkIndexMax
		})

		for _, summary := range summaries {
			text := strings.TrimSpace(summary.Summary)
			if text == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf("W%d %s: %s", summary.WindowIndex, formatSessionChunkRange(summary.ChunkIndexMin, summary.ChunkIndexMax), truncateInlineForPrompt(strings.ReplaceAll(text, "\n", " "), 140)))
			for _, tool := range summary.Tools {
				tool = strings.TrimSpace(tool)
				if tool != "" {
					toolSet[tool] = struct{}{}
				}
			}
			for _, file := range summary.Files {
				file = strings.TrimSpace(file)
				if file != "" {
					fileSet[file] = struct{}{}
				}
			}
			if len(lines) >= limit {
				return lines, sortedRecallKeys(toolSet), sortedRecallKeys(fileSet)
			}
		}
	}

	return lines, sortedRecallKeys(toolSet), sortedRecallKeys(fileSet)
}

func (p *SessionStoreRecallProvider) sessionTimelineLearnings(ctx context.Context, workspace, sessionID string, limit int) ([]string, []string, []string, []string, []string) {
	if p == nil || p.MemoryStore == nil || strings.TrimSpace(sessionID) == "" || limit <= 0 {
		return nil, nil, nil, nil, nil
	}

	filter := storage.MemoryListFilter{
		Types:     []string{"decision", "gotcha", "preference", "anti_pattern", "learning"},
		SessionID: sessionID,
	}

	pageSize := limit * 4
	if pageSize < 20 {
		pageSize = 20
	}

	var offset int
	var decisions, gotchas, preferences, antiPatterns, learnings []string
	totalAdded := 0

	for {
		entries, total, err := p.MemoryStore.ListFiltered(ctx, workspace, filter, pageSize, offset)
		if err != nil || len(entries) == 0 {
			break
		}
		for _, entry := range entries {
			summary := sessionLearningSummary(entry)
			if summary == "" {
				continue
			}
			before := totalAdded
			switch strings.ToLower(strings.TrimSpace(entry.Type)) {
			case "decision":
				decisions = appendUnique(decisions, summary)
				totalAdded = len(decisions) + len(gotchas) + len(preferences) + len(antiPatterns) + len(learnings)
			case "gotcha":
				gotchas = appendUnique(gotchas, summary)
				totalAdded = len(decisions) + len(gotchas) + len(preferences) + len(antiPatterns) + len(learnings)
			case "preference":
				preferences = appendUnique(preferences, summary)
				totalAdded = len(decisions) + len(gotchas) + len(preferences) + len(antiPatterns) + len(learnings)
			case "anti_pattern":
				antiPatterns = appendUnique(antiPatterns, summary)
				totalAdded = len(decisions) + len(gotchas) + len(preferences) + len(antiPatterns) + len(learnings)
			case "learning":
				learnings = appendUnique(learnings, summary)
				totalAdded = len(decisions) + len(gotchas) + len(preferences) + len(antiPatterns) + len(learnings)
			}
			if totalAdded >= limit || (before == totalAdded && totalAdded >= limit) {
				return decisions, gotchas, preferences, antiPatterns, learnings
			}
		}
		offset += len(entries)
		if offset >= total || totalAdded >= limit {
			break
		}
	}

	return decisions, gotchas, preferences, antiPatterns, learnings
}

func sessionLearningSummary(entry storage.NamedEntry) string {
	if len(entry.Result) > 0 {
		var payload map[string]any
		if err := json.Unmarshal(entry.Result, &payload); err == nil {
			if summary := strings.TrimSpace(payloadString(payload["summary"])); summary != "" {
				return summary
			}
		}
	}
	return strings.TrimSpace(entry.Summary)
}

func payloadString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}

func truncateInlineForPrompt(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func formatSessionChunkRange(minIdx, maxIdx int) string {
	if minIdx == maxIdx {
		return fmt.Sprintf("[%d]", minIdx)
	}
	return fmt.Sprintf("[%d-%d]", minIdx, maxIdx)
}

func sortedRecallKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
