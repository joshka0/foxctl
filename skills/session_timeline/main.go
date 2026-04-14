// Package main implements the session/timeline skill for timeline retrieval with comprehensive session analysis and learning extraction.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/workspaceutil"
	"github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/joshka0/foxctl/internal/storage/sessions"
)

const commandName = "session/timeline"

var defaultLearningTypes = []string{"decision", "gotcha", "preference", "anti_pattern", "learning"}

// Input defines the skill input parameters for timeline retrieval with flexible anchoring and filtering options.
type Input struct {
	SessionID             string   `json:"session_id"`
	SummaryID             string   `json:"summary_id,omitempty"`
	WindowIndex           *int     `json:"window_index,omitempty"`
	ChunkIndex            *int     `json:"chunk_index,omitempty"`
	Workspace             string   `json:"workspace,omitempty"`
	Types                 []string `json:"types,omitempty"`
	IncludeList           bool     `json:"include_list,omitempty"`
	IncludeRollup         bool     `json:"include_rollup,omitempty"`
	IncludeChunkSummaries bool     `json:"include_chunk_summaries,omitempty"`
	IncludeLearnings      bool     `json:"include_learnings,omitempty"`
	Limit                 int      `json:"limit,omitempty"`
	MaxWindows            int      `json:"max_windows,omitempty"` // Limit how many windows back from anchor (0 = all)
	Since                 string   `json:"since,omitempty"`       // Time filter: duration ("30m", "2h") or RFC3339 timestamp
	Until                 string   `json:"until,omitempty"`       // End of time range: duration or timestamp (default: now)
}

// Output defines the skill output with timeline data, anchor information, and comprehensive analysis results.
type Output struct {
	SessionID      string             `json:"session_id"`
	Anchor         Anchor             `json:"anchor"`
	ChunkSummaries []ChunkSummaryItem `json:"chunk_summaries,omitempty"`
	Learnings      []LearningItem     `json:"learnings,omitempty"`
	Rollup         *TimelineRollup    `json:"rollup,omitempty"`
	Truncated      bool               `json:"truncated,omitempty"`
	Status         string             `json:"status"`
	Message        string             `json:"message,omitempty"`
}

// Anchor identifies the matched summary boundary with detailed positioning and metadata.
type Anchor struct {
	SummaryID     string `json:"summary_id"`
	WindowIndex   int    `json:"window_index"`
	ChunkIndex    int    `json:"chunk_index"`
	ChunkIndexMin int    `json:"chunk_index_min"`
	ChunkIndexMax int    `json:"chunk_index_max"`
	ChunkIndices  []int  `json:"chunk_indices,omitempty"`
	Trigger       string `json:"trigger,omitempty"`
	Summary       string `json:"summary,omitempty"`
	SummaryModel  string `json:"summary_model,omitempty"`
}

// ChunkSummaryItem represents a timeline summary entry with tools, files, and error tracking.
type ChunkSummaryItem struct {
	SummaryID     string   `json:"summary_id"`
	WindowIndex   int      `json:"window_index"`
	ChunkIndexMin int      `json:"chunk_index_min"`
	ChunkIndexMax int      `json:"chunk_index_max"`
	ChunkIndices  []int    `json:"chunk_indices,omitempty"`
	Trigger       string   `json:"trigger,omitempty"`
	Summary       string   `json:"summary"`
	SummaryModel  string   `json:"summary_model,omitempty"`
	Tools         []string `json:"tools,omitempty"`
	Files         []string `json:"files,omitempty"`
	Errors        []string `json:"errors,omitempty"`
}

// LearningItem represents extracted learnings within the timeline with type classification and metadata.
type LearningItem struct {
	Type        string         `json:"type"`
	Summary     string         `json:"summary"`
	WindowIndex int            `json:"window_index"`
	ExtractedAt string         `json:"extracted_at,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// TimelineRollup aggregates metadata up to the anchor with comprehensive categorization and deduplication.
type TimelineRollup struct {
	SummaryLines []string `json:"summary_lines,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	Files        []string `json:"files,omitempty"`
	Errors       []string `json:"errors,omitempty"`
	Decisions    []string `json:"decisions,omitempty"`
	Gotchas      []string `json:"gotchas,omitempty"`
	Preferences  []string `json:"preferences,omitempty"`
	AntiPatterns []string `json:"anti_patterns,omitempty"`
	Learnings    []string `json:"learnings,omitempty"`
}

// parseSince parses a since string into a time.Time with flexible format support.
// Accepts durations ("30m", "2h", "7d") or RFC3339 timestamps.
// Returns zero time if empty or unparseable.
func parseSince(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	// Try duration first (e.g., "30m", "2h", "7d")
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d)
	}
	// Try day shorthand (e.g., "7d" -> 7 days)
	if strings.HasSuffix(s, "d") {
		if days, err := fmt.Sscanf(s, "%dd", new(int)); err == nil && days == 1 {
			var n int
			_, _ = fmt.Sscanf(s, "%dd", &n)
			return time.Now().Add(-time.Duration(n) * 24 * time.Hour)
		}
	}
	// Try RFC3339 timestamp
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	// Try date-only format
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t
	}
	return time.Time{}
}

// main is the skill entry point for session/timeline with comprehensive timeline retrieval capabilities.
func main() {
	skillmain.Main(commandName, run)
}

// run orchestrates timeline retrieval with anchor resolution, chunk collection, and learning aggregation.
//
// Index:
// - Purpose: Retrieve session timeline with flexible anchoring, chunk summaries, learnings, and rollup analysis
// - Flow: validate input → open session store → resolve anchor → collect chunk summaries → fetch learnings → build rollup → emit results
// - SideEffects: reads session metadata; accesses chunk summaries; queries memory store; aggregates timeline data
// - FailureModes: session not found, anchor resolution errors, store access failures, time parsing errors
// - Observability: emits timeline anchor, chunk summaries, learning items, rollup data, and truncation status
// - Related: resolveAnchor, collectChunkSummariesUpTo, fetchLearningItems, buildRollup
// - Keywords: session/timeline, anchor_resolution, chunk_summaries, learning_extraction, timeline_rollup
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	if strings.TrimSpace(in.SessionID) == "" {
		return skillerr.Arg("session_id is required")
	}
	if in.WindowIndex != nil && *in.WindowIndex < 0 {
		return skillerr.Arg("window_index must be >= 0")
	}
	if in.ChunkIndex != nil && *in.ChunkIndex < 0 {
		return skillerr.Arg("chunk_index must be >= 0")
	}
	includeList := in.IncludeList
	includeRollup := in.IncludeRollup
	if !includeList && !includeRollup {
		includeList = true
		includeRollup = true
	}
	includeChunkSummaries := in.IncludeChunkSummaries
	includeLearnings := in.IncludeLearnings
	if !includeChunkSummaries && !includeLearnings {
		includeChunkSummaries = true
		includeLearnings = true
	}

	sessionStore, err := rc.Stores.Sessions(ctx)
	if err != nil {
		return skillerr.IO("open sessions store", skillerr.WithCause(err))
	}

	sess, err := sessionStore.Get(ctx, in.SessionID)
	if err != nil {
		return skillerr.Arg("session not found", skillerr.WithCause(err))
	}

	anchorSummary, anchorChunkIndex, err := resolveAnchor(ctx, sessionStore, in, sess.ID)
	if err != nil {
		return err
	}

	anchor := Anchor{
		SummaryID:     anchorSummary.ID,
		WindowIndex:   anchorSummary.WindowIndex,
		ChunkIndex:    anchorChunkIndex,
		ChunkIndexMin: anchorSummary.ChunkIndexMin,
		ChunkIndexMax: anchorSummary.ChunkIndexMax,
		ChunkIndices:  anchorSummary.ChunkIndices,
		Trigger:       anchorSummary.Trigger,
		Summary:       anchorSummary.Summary,
		SummaryModel:  anchorSummary.SummaryModel,
	}

	output := Output{
		SessionID: sess.ID,
		Anchor:    anchor,
		Status:    "ok",
	}

	var chunkSummaries []sessions.SessionChunkSummary
	if includeChunkSummaries {
		sinceTime := parseSince(in.Since)
		untilTime := parseSince(in.Until)
		chunkSummaries, err = collectChunkSummariesUpTo(ctx, sessionStore, sess.ID, anchorSummary, in.MaxWindows, sinceTime, untilTime)
		if err != nil {
			return skillerr.IO("collect chunk summaries", skillerr.WithCause(err))
		}
	}

	var truncated bool
	if includeList && includeChunkSummaries {
		items := make([]ChunkSummaryItem, 0, len(chunkSummaries))
		for _, summary := range chunkSummaries {
			items = append(items, chunkSummaryItem(summary))
		}
		if in.Limit > 0 && len(items) > in.Limit {
			items = items[:in.Limit]
			truncated = true
		}
		output.ChunkSummaries = items
	}

	var learningItems []LearningItem
	if includeLearnings {
		workspace := workspaceutil.Resolve(in.Workspace, sess.WorkspacePath, rc.Workspace)
		memStore, err := rc.Stores.Memory(ctx)
		if err != nil {
			return skillerr.IO("open memory store", skillerr.WithCause(err))
		}

		learningItems, err = fetchLearningItems(ctx, memStore, workspace, sess.ID, normalizeTypes(in.Types), anchorSummary.WindowIndex)
		if err != nil {
			return skillerr.IO("load learnings", skillerr.WithCause(err))
		}
		if includeList {
			output.Learnings = learningItems
		}
	}

	if includeRollup {
		output.Rollup = buildRollup(chunkSummaries, learningItems)
	}

	output.Truncated = truncated
	if truncated {
		output.Message = fmt.Sprintf("timeline truncated to %d chunk summaries", in.Limit)
	}

	return skillout.Emit(rc, commandName, output)
}

// resolveAnchor finds the appropriate summary anchor based on input parameters with multiple resolution strategies.
func resolveAnchor(ctx context.Context, store *sessions.Store, in Input, sessionID string) (sessions.SessionChunkSummary, int, error) {
	if summaryID := strings.TrimSpace(in.SummaryID); summaryID != "" {
		summary, err := store.GetChunkSummary(ctx, summaryID)
		if err != nil {
			return sessions.SessionChunkSummary{}, 0, skillerr.Arg("anchor summary not found", skillerr.WithCause(err))
		}
		return summary, summary.ChunkIndexMax, nil
	}

	if in.WindowIndex != nil {
		windowIndex := *in.WindowIndex
		summaries, err := store.GetChunkSummaries(ctx, sessionID, windowIndex)
		if err != nil {
			return sessions.SessionChunkSummary{}, 0, skillerr.IO("load window summaries", skillerr.WithCause(err))
		}
		if len(summaries) == 0 {
			return sessions.SessionChunkSummary{}, 0, skillerr.Arg("no chunk summaries for window")
		}
		if in.ChunkIndex != nil {
			chunkIndex := *in.ChunkIndex
			if summary, ok := findSummaryForChunk(summaries, chunkIndex); ok {
				return summary, chunkIndex, nil
			}
			return sessions.SessionChunkSummary{}, 0, skillerr.Arg("chunk index not covered by summaries")
		}
		latest := latestSummaryInWindow(summaries)
		return latest, latest.ChunkIndexMax, nil
	}

	latest, err := latestSummaryInSession(ctx, store, sessionID)
	if err != nil {
		return sessions.SessionChunkSummary{}, 0, err
	}
	return latest, latest.ChunkIndexMax, nil
}

// latestSummaryInWindow finds the latest summary by chunk index within a specific window.
func latestSummaryInWindow(summaries []sessions.SessionChunkSummary) sessions.SessionChunkSummary {
	latest := summaries[0]
	for _, summary := range summaries[1:] {
		if summary.ChunkIndexMax > latest.ChunkIndexMax {
			latest = summary
		}
	}
	return latest
}

// latestSummaryInSession finds the latest summary across all windows in a session with fallback handling.
func latestSummaryInSession(ctx context.Context, store *sessions.Store, sessionID string) (sessions.SessionChunkSummary, error) {
	windows, err := store.GetContextWindows(ctx, sessionID)
	if err != nil {
		return sessions.SessionChunkSummary{}, skillerr.IO("load context windows", skillerr.WithCause(err))
	}
	if len(windows) == 0 {
		return sessions.SessionChunkSummary{}, skillerr.Arg("no context windows for session")
	}
	maxWindow := windows[0].WindowIndex
	for _, window := range windows[1:] {
		if window.WindowIndex > maxWindow {
			maxWindow = window.WindowIndex
		}
	}
	for windowIndex := maxWindow; windowIndex >= 0; windowIndex-- {
		summaries, err := store.GetChunkSummaries(ctx, sessionID, windowIndex)
		if err != nil || len(summaries) == 0 {
			continue
		}
		return latestSummaryInWindow(summaries), nil
	}
	return sessions.SessionChunkSummary{}, skillerr.Arg("no chunk summaries for session")
}

// collectChunkSummariesUpTo collects chunk summaries up to the anchor with time filtering and window limits.
func collectChunkSummariesUpTo(ctx context.Context, store *sessions.Store, sessionID string, anchor sessions.SessionChunkSummary, maxWindows int, sinceTime, untilTime time.Time) ([]sessions.SessionChunkSummary, error) {
	windows, err := store.GetContextWindows(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	sort.Slice(windows, func(i, j int) bool {
		return windows[i].WindowIndex < windows[j].WindowIndex
	})

	// Calculate minimum window index based on maxWindows limit
	minWindowIndex := 0
	if maxWindows > 0 {
		minWindowIndex = anchor.WindowIndex - maxWindows + 1
		if minWindowIndex < 0 {
			minWindowIndex = 0
		}
	}

	var out []sessions.SessionChunkSummary
	for _, window := range windows {
		if window.WindowIndex > anchor.WindowIndex {
			break
		}
		if window.WindowIndex < minWindowIndex {
			continue
		}
		// Filter by sinceTime if set
		if !sinceTime.IsZero() && window.StartedAt.Before(sinceTime) {
			continue
		}
		// Filter by untilTime if set (window must have started before until)
		if !untilTime.IsZero() && window.StartedAt.After(untilTime) {
			continue
		}
		summaries, err := store.GetChunkSummaries(ctx, sessionID, window.WindowIndex)
		if err != nil {
			continue
		}
		sort.Slice(summaries, func(i, j int) bool {
			if summaries[i].ChunkIndexMin == summaries[j].ChunkIndexMin {
				return summaries[i].ChunkIndexMax < summaries[j].ChunkIndexMax
			}
			return summaries[i].ChunkIndexMin < summaries[j].ChunkIndexMin
		})

		for _, summary := range summaries {
			if window.WindowIndex < anchor.WindowIndex {
				out = append(out, summary)
				continue
			}
			if summary.ID == anchor.ID {
				out = append(out, summary)
				return out, nil
			}
			if summary.ChunkIndexMax <= anchor.ChunkIndexMax {
				out = append(out, summary)
				continue
			}
			if summary.ChunkIndexMin <= anchor.ChunkIndexMax && summary.ChunkIndexMax >= anchor.ChunkIndexMin {
				out = append(out, summary)
			}
			return out, nil
		}
	}

	return out, nil
}

// chunkSummaryItem converts internal summary format to output format with field mapping.
func chunkSummaryItem(summary sessions.SessionChunkSummary) ChunkSummaryItem {
	return ChunkSummaryItem{
		SummaryID:     summary.ID,
		WindowIndex:   summary.WindowIndex,
		ChunkIndexMin: summary.ChunkIndexMin,
		ChunkIndexMax: summary.ChunkIndexMax,
		ChunkIndices:  summary.ChunkIndices,
		Trigger:       summary.Trigger,
		Summary:       summary.Summary,
		SummaryModel:  summary.SummaryModel,
		Tools:         summary.Tools,
		Files:         summary.Files,
		Errors:        summary.Errors,
	}
}

// findSummaryForChunk finds a summary that covers a specific chunk index with range and index checking.
func findSummaryForChunk(summaries []sessions.SessionChunkSummary, chunkIndex int) (sessions.SessionChunkSummary, bool) {
	for _, summary := range summaries {
		if summary.ChunkIndexMin <= chunkIndex && summary.ChunkIndexMax >= chunkIndex {
			return summary, true
		}
	}
	for _, summary := range summaries {
		for _, idx := range summary.ChunkIndices {
			if idx == chunkIndex {
				return summary, true
			}
		}
	}
	return sessions.SessionChunkSummary{}, false
}

// fetchLearningItems retrieves learning items from memory store with filtering and sorting.
func fetchLearningItems(ctx context.Context, store *memory.Store, workspace, sessionID string, types []string, maxWindow int) ([]LearningItem, error) {
	filter := memory.ListFilter{Types: types, SessionID: sessionID}
	const pageSize = 200
	var offset int
	var out []LearningItem
	for {
		entries, total, err := store.ListFiltered(ctx, workspace, filter, pageSize, offset)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			item, ok := learningItemFromEntry(entry)
			if !ok {
				continue
			}
			if item.WindowIndex > maxWindow {
				continue
			}
			out = append(out, item)
		}
		offset += len(entries)
		if offset >= total || len(entries) == 0 {
			break
		}
	}
	if len(out) == 0 {
		return out, nil
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].WindowIndex == out[j].WindowIndex {
			return out[i].Type < out[j].Type
		}
		return out[i].WindowIndex < out[j].WindowIndex
	})
	return out, nil
}

// learningItemFromEntry converts memory store entry to learning item with payload parsing.
func learningItemFromEntry(entry memory.NamedEntry) (LearningItem, bool) {
	if len(entry.Result) == 0 {
		return LearningItem{}, false
	}
	var payload map[string]any
	if err := json.Unmarshal(entry.Result, &payload); err != nil {
		return LearningItem{}, false
	}
	windowIndex, ok := intFromPayload(payload["window_index"])
	if !ok {
		return LearningItem{}, false
	}
	summary := stringFromPayload(payload["summary"])
	if summary == "" {
		summary = entry.Summary
	}
	extractedAt := stringFromPayload(payload["extracted_at"])

	return LearningItem{
		Type:        entry.Type,
		Summary:     summary,
		WindowIndex: windowIndex,
		ExtractedAt: extractedAt,
		Metadata:    payload,
	}, true
}

// intFromPayload extracts integer value from payload with type conversion support.
func intFromPayload(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i), true
		}
	}
	return 0, false
}

// stringFromPayload extracts string value from payload with type conversion and trimming.
func stringFromPayload(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	}
	return ""
}

// normalizeTypes normalizes and deduplicates learning types with default fallback.
func normalizeTypes(types []string) []string {
	if len(types) == 0 {
		return defaultLearningTypes
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, len(types))
	for _, t := range types {
		key := strings.ToLower(strings.TrimSpace(t))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	if len(out) == 0 {
		return defaultLearningTypes
	}
	return out
}

// buildRollup creates aggregated timeline summary from chunks and learnings with deduplication.
func buildRollup(summaries []sessions.SessionChunkSummary, learnings []LearningItem) *TimelineRollup {
	rollup := &TimelineRollup{}

	summaryLines := make([]string, 0, len(summaries))
	toolSet := make(map[string]struct{})
	fileSet := make(map[string]struct{})
	errorSet := make(map[string]struct{})

	for _, summary := range summaries {
		rangeLabel := formatChunkRange(summary.ChunkIndexMin, summary.ChunkIndexMax)
		line := fmt.Sprintf("W%d %s: %s", summary.WindowIndex, rangeLabel, summary.Summary)
		summaryLines = append(summaryLines, line)
		for _, tool := range summary.Tools {
			toolSet[tool] = struct{}{}
		}
		for _, file := range summary.Files {
			fileSet[file] = struct{}{}
		}
		for _, err := range summary.Errors {
			errorSet[err] = struct{}{}
		}
	}

	rollup.SummaryLines = summaryLines
	rollup.Tools = sortedKeys(toolSet)
	rollup.Files = sortedKeys(fileSet)
	rollup.Errors = sortedKeys(errorSet)

	for _, item := range learnings {
		switch item.Type {
		case "decision":
			rollup.Decisions = appendUnique(rollup.Decisions, item.Summary)
		case "gotcha":
			rollup.Gotchas = appendUnique(rollup.Gotchas, item.Summary)
		case "preference":
			rollup.Preferences = appendUnique(rollup.Preferences, item.Summary)
		case "anti_pattern":
			rollup.AntiPatterns = appendUnique(rollup.AntiPatterns, item.Summary)
		case "learning":
			rollup.Learnings = appendUnique(rollup.Learnings, item.Summary)
		}
	}

	return rollup
}

// appendUnique adds value to slice if not already present with trimming and validation.
func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// sortedKeys converts map keys to sorted slice with trimming and empty value filtering.
func sortedKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// formatChunkRange formats chunk index range for display with single and range support.
func formatChunkRange(minIndex, maxIndex int) string {
	if minIndex == maxIndex {
		return fmt.Sprintf("C%d", minIndex)
	}
	return fmt.Sprintf("C%d-%d", minIndex, maxIndex)
}
