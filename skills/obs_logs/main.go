// Package main implements the obs/logs skill for querying observability logs.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

const command = "obs/logs"

// input defines the parameters for observability log queries with filtering options.
type input struct {
	Limit      int    `json:"limit,omitempty"`
	Operation  string `json:"operation,omitempty"`
	Command    string `json:"command,omitempty"`
	Status     string `json:"status,omitempty"`
	Component  string `json:"component,omitempty"`
	Since      string `json:"since,omitempty"`
	Stats      bool   `json:"stats,omitempty"`
	ErrorsOnly bool   `json:"errors_only,omitempty"`
}

// eventSummary is a condensed view for output with essential fields.
type eventSummary struct {
	Timestamp  string `json:"ts"`
	TraceID    string `json:"trace_id"`
	Operation  string `json:"operation"`
	Command    string `json:"command,omitempty"`
	Component  string `json:"component"`
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	ErrorCode  string `json:"error_code,omitempty"`
	ErrorMsg   string `json:"error_message,omitempty"`
}

// stats holds aggregate statistics computed from filtered events.
type stats struct {
	TotalEvents    int            `json:"total_events"`
	ByStatus       map[string]int `json:"by_status"`
	ByOperation    map[string]int `json:"by_operation"`
	ByComponent    map[string]int `json:"by_component"`
	ByCommand      map[string]int `json:"by_command,omitempty"`
	AvgDurationMS  float64        `json:"avg_duration_ms"`
	MaxDurationMS  int64          `json:"max_duration_ms"`
	ErrorRate      float64        `json:"error_rate"`
	TimeRangeStart string         `json:"time_range_start,omitempty"`
	TimeRangeEnd   string         `json:"time_range_end,omitempty"`
}

// main is the skill entry point for obs/logs.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates observability log queries with filtering, sorting, and statistics.
//
// Index:
//
//	Purpose: Query observability logs from NDJSON files with filtering by time, status, operation, and component
//	Keywords: obs/logs, observability, log_querying, ndjson_parsing, event_filtering, statistics
//	Related: parseSince, readEvents, matchesFilters, computeStats, topN, buildSummary
//	Flow: parse input → find events file → read and filter events → sort by timestamp → compute stats → emit results
//	Resources: NDJSON log files, observability event store
//	Events: observability log query events
//	OutputFields: events, count, total_scanned, stats, summary
//
// [[domain:observability-log-query]]
// [[risk:missing-log-file-fallback]]
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Set defaults
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 500 {
		limit = 500
	}

	// Parse since duration/timestamp
	var sinceTime time.Time
	if in.Since != "" {
		sinceTime = parseSince(in.Since)
	}

	// Find the events file
	obsDir := rc.Config.Paths.Observability
	if obsDir == "" {
		obsDir = os.Getenv("FOXCTL_OBS_DIR")
	}
	if obsDir == "" {
		obsDir = filepath.Join(os.Getenv("HOME"), ".foxctl", "observability")
	}
	eventsFile := filepath.Join(obsDir, "events", observability.EventFileName+".ndjson")

	// Check if file exists
	if _, err := os.Stat(eventsFile); os.IsNotExist(err) {
		return skillout.Emit(rc, command, map[string]any{
			"events":        []eventSummary{},
			"count":         0,
			"total_scanned": 0,
			"summary":       fmt.Sprintf("No observability data found at %s", eventsFile),
		})
	}

	// Read and filter events
	events, totalScanned, err := readEvents(eventsFile, limit, sinceTime, in)
	if err != nil {
		return fmt.Errorf("read events: %w", err)
	}

	// Convert to summaries (most recent first)
	summaries := make([]eventSummary, len(events))
	for i, evt := range events {
		summaries[i] = eventSummary{
			Timestamp:  evt.Timestamp.Format(time.RFC3339),
			TraceID:    truncateID(evt.TraceID),
			Operation:  evt.Operation,
			Command:    evt.Name,
			Component:  observability.EventDataString(&evt, observability.DataKeyComponent),
			Status:     string(evt.Status),
			DurationMS: evt.Duration.Milliseconds(),
			ErrorCode:  evt.ErrorCode,
			ErrorMsg:   evt.ErrorMessage,
		}
	}

	// Build response
	data := map[string]any{
		"events":        summaries,
		"count":         len(summaries),
		"total_scanned": totalScanned,
	}

	// Add stats if requested
	if in.Stats && len(events) > 0 {
		data["stats"] = computeStats(events)
	}

	// Build summary
	summary := buildSummary(len(summaries), totalScanned, in)
	data["summary"] = summary

	return skillout.Emit(rc, command, data)
}

// parseSince converts duration strings or timestamps to time objects for filtering.
func parseSince(s string) time.Time {
	// Try parsing as duration (e.g., "1h", "30m", "2h30m")
	durationPattern := regexp.MustCompile(`^(\d+[hms])+$`)
	if durationPattern.MatchString(s) {
		d, err := time.ParseDuration(s)
		if err == nil {
			return time.Now().Add(-d)
		}
	}

	// Try parsing as RFC3339
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t
	}

	// Try parsing as date only
	t, err = time.Parse("2006-01-02", s)
	if err == nil {
		return t
	}

	return time.Time{}
}

// readEvents streams and filters NDJSON events with memory-efficient processing.
func readEvents(path string, limit int, sinceTime time.Time, in input) ([]observability.Event, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer func() { errs.Ignore(f.Close(), "close events file") }()

	var allEvents []observability.Event
	totalScanned := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		totalScanned++

		var evt observability.Event
		if err := json.Unmarshal(line, &evt); err != nil {
			continue
		}

		// Apply filters
		if !matchesFilters(evt, sinceTime, in) {
			continue
		}

		allEvents = append(allEvents, evt)
	}

	if err := scanner.Err(); err != nil {
		return nil, totalScanned, err
	}

	// Sort by timestamp descending (most recent first)
	sort.Slice(allEvents, func(i, j int) bool {
		return allEvents[i].Timestamp.After(allEvents[j].Timestamp)
	})

	// Apply limit
	if len(allEvents) > limit {
		allEvents = allEvents[:limit]
	}

	return allEvents, totalScanned, nil
}

// matchesFilters applies all configured filters to determine if an event should be included.
func matchesFilters(evt observability.Event, sinceTime time.Time, in input) bool {
	// Time filter
	if !sinceTime.IsZero() && evt.Timestamp.Before(sinceTime) {
		return false
	}

	// Errors only
	if in.ErrorsOnly && evt.Status != observability.StatusError {
		return false
	}

	// Operation filter (substring match)
	if in.Operation != "" && !strings.Contains(strings.ToLower(evt.Operation), strings.ToLower(in.Operation)) {
		return false
	}

	// Command filter (substring match)
	if in.Command != "" && !strings.Contains(strings.ToLower(evt.Name), strings.ToLower(in.Command)) {
		return false
	}

	// Status filter (exact match)
	if in.Status != "" && string(evt.Status) != in.Status {
		return false
	}

	// Component filter (exact match)
	if in.Component != "" && observability.EventDataString(&evt, observability.DataKeyComponent) != in.Component {
		return false
	}

	return true
}

// computeStats calculates aggregate statistics from filtered events.
func computeStats(events []observability.Event) stats {
	s := stats{
		TotalEvents: len(events),
		ByStatus:    make(map[string]int),
		ByOperation: make(map[string]int),
		ByComponent: make(map[string]int),
		ByCommand:   make(map[string]int),
	}

	var totalDuration int64
	errorCount := 0
	var minTime, maxTime time.Time

	for _, evt := range events {
		s.ByStatus[string(evt.Status)]++
		s.ByOperation[evt.Operation]++
		component := observability.EventDataString(&evt, observability.DataKeyComponent)
		s.ByComponent[component]++
		if evt.Name != "" {
			s.ByCommand[evt.Name]++
		}

		durationMS := evt.Duration.Milliseconds()
		totalDuration += durationMS
		if durationMS > s.MaxDurationMS {
			s.MaxDurationMS = durationMS
		}

		if evt.Status == observability.StatusError {
			errorCount++
		}

		if minTime.IsZero() || evt.Timestamp.Before(minTime) {
			minTime = evt.Timestamp
		}
		if maxTime.IsZero() || evt.Timestamp.After(maxTime) {
			maxTime = evt.Timestamp
		}
	}

	if len(events) > 0 {
		s.AvgDurationMS = float64(totalDuration) / float64(len(events))
		s.ErrorRate = float64(errorCount) / float64(len(events))
	}

	if !minTime.IsZero() {
		s.TimeRangeStart = minTime.Format(time.RFC3339)
	}
	if !maxTime.IsZero() {
		s.TimeRangeEnd = maxTime.Format(time.RFC3339)
	}

	// Trim to top 10 for each category to avoid huge output
	s.ByOperation = topN(s.ByOperation, 10)
	s.ByCommand = topN(s.ByCommand, 10)

	return s
}

// topN returns the top N items from a map sorted by value in descending order.
func topN(m map[string]int, n int) map[string]int {
	if len(m) <= n {
		return m
	}

	type kv struct {
		k string
		v int
	}
	var pairs []kv
	for k, v := range m {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].v > pairs[j].v
	})

	result := make(map[string]int)
	for i := 0; i < n && i < len(pairs); i++ {
		result[pairs[i].k] = pairs[i].v
	}
	return result
}

// buildSummary generates a human-readable summary of the query results and filters applied.
func buildSummary(count, total int, in input) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("Showing %d of %d events", count, total))

	var filters []string
	if in.ErrorsOnly {
		filters = append(filters, "errors only")
	}
	if in.Operation != "" {
		filters = append(filters, fmt.Sprintf("operation=%s", in.Operation))
	}
	if in.Command != "" {
		filters = append(filters, fmt.Sprintf("command=%s", in.Command))
	}
	if in.Status != "" {
		filters = append(filters, fmt.Sprintf("status=%s", in.Status))
	}
	if in.Component != "" {
		filters = append(filters, fmt.Sprintf("component=%s", in.Component))
	}
	if in.Since != "" {
		filters = append(filters, fmt.Sprintf("since=%s", in.Since))
	}

	if len(filters) > 0 {
		parts = append(parts, fmt.Sprintf("(filters: %s)", strings.Join(filters, ", ")))
	}

	return strings.Join(parts, " ")
}

// truncateID shortens trace and span IDs for display while preserving uniqueness.
func truncateID(id string) string {
	if len(id) > 12 {
		return id[:12] + "..."
	}
	return id
}
