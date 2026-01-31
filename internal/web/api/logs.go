package api

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

// LogEntry represents a log entry in API responses.
type LogEntry struct {
	Timestamp    string         `json:"ts"`
	Operation    string         `json:"operation"`
	Command      string         `json:"command,omitempty"` // Skill/hook name (e.g., "code/semantic_search")
	Status       string         `json:"status"`
	Component    string         `json:"component,omitempty"`
	TraceID      string         `json:"trace_id,omitempty"`
	SpanID       string         `json:"span_id,omitempty"`
	ParentID     string         `json:"parent_id,omitempty"`
	Service      string         `json:"service,omitempty"`
	Version      string         `json:"version,omitempty"`
	Subtype      string         `json:"subtype,omitempty"`
	SessionID    string         `json:"session_id,omitempty"`
	AgentID      string         `json:"agent_id,omitempty"`
	WorkspaceID  string         `json:"workspace_id,omitempty"`
	JobID        string         `json:"job_id,omitempty"`
	DurationMS   int64          `json:"duration_ms,omitempty"`
	ErrorType    string         `json:"error_type,omitempty"`
	ErrorCode    string         `json:"error_code,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
	Retriable    *bool          `json:"retriable,omitempty"`
	Data         map[string]any `json:"data,omitempty"`
}

// LogsHandler returns a handler for GET /api/logs.
// Query params:
//   - limit: max entries to return (default 100, max 1000)
//   - since: only entries after this duration (e.g., "1h", "30m")
//   - component: filter by component (agent, hook, skill, etc.)
//   - operation: filter by operation prefix (e.g., "agent.", "hook.")
// LogsHandler returns an HTTP handler for the GET /api/logs endpoint that serves filtered recent log entries.
// 
// The handler accepts query parameters to filter results: `limit` (1–1000, default 100), `since` (duration, e.g. "30m"),
// `component`, `operation`, `workspace`, and `errors_only` (boolean). It locates the observability directory from the
// AGENTCTL_OBS_DIR environment variable or defaults to ~/.agentctl/observability, reads events from the `events` subdirectory,
// and responds with a JSON object containing `entries` (slice of LogEntry) and `count`. Unsupported HTTP methods result in
// a method-not-allowed response and read failures result in an internal-server-error response.
func LogsHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Parse query params
		limit := 100
		if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
			if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 1000 {
				limit = n
			}
		}

		var sinceTime time.Time
		if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
			duration, err := time.ParseDuration(sinceStr)
			if err == nil {
				sinceTime = time.Now().Add(-duration)
			}
		}

		componentFilter := r.URL.Query().Get("component")
		operationFilter := r.URL.Query().Get("operation")
		workspaceFilter := r.URL.Query().Get("workspace")
		errorsOnly := parseBool(r.URL.Query().Get("errors_only"))

		// Get observability directory - try env var first, then default path
		obsDir := os.Getenv("AGENTCTL_OBS_DIR")
		if obsDir == "" {
			// Default to ~/.agentctl/observability
			homeDir, err := os.UserHomeDir()
			if err == nil {
				obsDir = filepath.Join(homeDir, ".agentctl", "observability")
			}
		}
		if obsDir == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"entries": []LogEntry{},
				"message": "Observability directory not found",
			})
			return
		}

		// Find and read log files
		eventsDir := filepath.Join(obsDir, "events")
		entries, err := readLogEntries(eventsDir, limit, sinceTime, componentFilter, operationFilter, workspaceFilter, errorsOnly)
		if err != nil {
			log.Error().Err(err).Msg("failed to read log entries")
			httpError(w, http.StatusInternalServerError, "failed to read logs: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"entries": entries,
			"count":   len(entries),
		})
	}
}

// readLogEntries reads log entries from the eventsDir, applies the provided filters, and returns up to limit entries.
//
// The function searches for files matching "*.ndjson*" under eventsDir, processes newer files first, and uses a larger internal read quota
// to account for filtering. For non-gzipped files it attempts an efficient tail-read; gzipped or fallback files are read fully.
// Filters applied: events newer than sinceTime, matching componentFilter, operationFilter (prefix match), workspaceFilter (prefix match),
// and an errorsOnly flag to include only error-status events. Files that cannot be read or parsed are skipped.
// The returned slice is sorted newest-first by timestamp and truncated to at most limit entries.
func readLogEntries(eventsDir string, limit int, sinceTime time.Time, componentFilter, operationFilter, workspaceFilter string, errorsOnly bool) ([]LogEntry, error) {
	// Find all NDJSON files
	files, err := filepath.Glob(filepath.Join(eventsDir, "*.ndjson*"))
	if err != nil {
		return nil, err
	}

	// Sort files by modification time (newest first)
	sort.Slice(files, func(i, j int) bool {
		infoI, _ := os.Stat(files[i])
		infoJ, _ := os.Stat(files[j])
		if infoI == nil || infoJ == nil {
			return false
		}
		return infoI.ModTime().After(infoJ.ModTime())
	})

	var entries []LogEntry

	// Read more entries than needed since we'll filter and sort
	// Use a higher internal limit to ensure we get enough recent entries
	internalLimit := limit * 20
	if internalLimit < 500 {
		internalLimit = 500
	}

	for _, file := range files {
		if len(entries) >= internalLimit {
			break
		}

		// For non-gzipped files, read from the end for efficiency
		if !strings.HasSuffix(file, ".gz") {
			fileEntries, err := readLogFileTail(file, internalLimit-len(entries), sinceTime, componentFilter, operationFilter, workspaceFilter, errorsOnly)
			if err == nil {
				entries = append(entries, fileEntries...)
				continue
			}
			// Fall back to regular reading if tail fails
		}

		fileEntries, err := readLogFile(file, internalLimit-len(entries), sinceTime, componentFilter, operationFilter, workspaceFilter, errorsOnly)
		if err != nil {
			continue // Skip files with errors
		}

		entries = append(entries, fileEntries...)
	}

	// Sort all entries by timestamp (newest first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp > entries[j].Timestamp
	})

	// Truncate to limit
	if len(entries) > limit {
		entries = entries[:limit]
	}

	return entries, nil
}

// readLogFileTail reads the last N entries from a log file (for non-gzipped files).
// readLogFileTail reads the most recent log events from a non-gzipped NDJSON file and returns filtered entries in newest-first order.
// It reads a chunk from the end of the file, parses each line as an observability.WideEvent, applies the provided filters
// (`sinceTime`, `componentFilter`, `operationFilter`, `workspaceFilter`, `errorsOnly`), and collects entries until `limit` is reached.
// Malformed JSON lines are skipped; IO or file-stat errors are returned.
func readLogFileTail(path string, limit int, sinceTime time.Time, componentFilter, operationFilter, workspaceFilter string, errorsOnly bool) ([]LogEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	// Read from end of file - start with last 512KB
	chunkSize := int64(512 * 1024)
	fileSize := stat.Size()

	if fileSize < chunkSize {
		chunkSize = fileSize
	}

	// Read the last chunk
	buf := make([]byte, chunkSize)
	_, err = file.ReadAt(buf, fileSize-chunkSize)
	if err != nil {
		return nil, err
	}

	// Split into lines and process from end
	lines := strings.Split(string(buf), "\n")
	var entries []LogEntry

	// Process lines in reverse order (newest first)
	for i := len(lines) - 1; i >= 0 && len(entries) < limit; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		var event observability.WideEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue // Skip malformed lines
		}

		// Apply filters
		if !sinceTime.IsZero() && event.Ts.Before(sinceTime) {
			continue
		}
		if componentFilter != "" && event.Component != componentFilter {
			continue
		}
		if operationFilter != "" && !strings.HasPrefix(event.Operation, operationFilter) {
			continue
		}
		if workspaceFilter != "" && event.WorkspaceID != workspaceFilter {
			continue
		}
		if errorsOnly && event.Status != observability.StatusError {
			continue
		}

		entry := LogEntry{
			Timestamp:    event.Ts.Format(time.RFC3339Nano),
			Operation:    event.Operation,
			Command:      event.Command,
			Status:       string(event.Status),
			Component:    event.Component,
			TraceID:      event.TraceID,
			SpanID:       event.SpanID,
			ParentID:     event.ParentID,
			Service:      event.Service,
			Version:      event.Version,
			Subtype:      event.Subtype,
			SessionID:    event.SessionID,
			AgentID:      event.AgentID,
			WorkspaceID:  event.WorkspaceID,
			JobID:        event.JobID,
			DurationMS:   event.DurationMS,
			ErrorType:    event.ErrorType,
			ErrorCode:    event.ErrorCode,
			ErrorMessage: event.ErrorMessage,
			Retriable:    event.Retriable,
			Data:         event.Data,
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// readLogFile reads log events from the file at path and returns up to limit entries that match the provided filters.
// It supports plain and gzipped (.gz) NDJSON files and skips malformed lines.
// Filters applied are: sinceTime (include events after this time), component (exact match), operation (prefix match),
// workspace (exact match), and errorsOnly (include only events with error status).
// The returned slice contains LogEntry values mapped from observability.WideEvent in the file's order; the error return
// reports any I/O or scanner error encountered during reading.
func readLogFile(path string, limit int, sinceTime time.Time, componentFilter, operationFilter, workspaceFilter string, errorsOnly bool) ([]LogEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var reader *bufio.Scanner

	// Handle gzipped files
	if strings.HasSuffix(path, ".gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return nil, err
		}
		defer gzReader.Close()
		reader = bufio.NewScanner(gzReader)
	} else {
		reader = bufio.NewScanner(file)
	}

	// Increase buffer size for long lines
	reader.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var entries []LogEntry

	for reader.Scan() {
		if len(entries) >= limit {
			break
		}

		line := reader.Bytes()
		if len(line) == 0 {
			continue
		}

		var event observability.WideEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue // Skip malformed lines
		}

		// Apply filters
		if !sinceTime.IsZero() && event.Ts.Before(sinceTime) {
			continue
		}
		if componentFilter != "" && event.Component != componentFilter {
			continue
		}
		if operationFilter != "" && !strings.HasPrefix(event.Operation, operationFilter) {
			continue
		}
		if workspaceFilter != "" && event.WorkspaceID != workspaceFilter {
			continue
		}
		if errorsOnly && event.Status != observability.StatusError {
			continue
		}

		entry := LogEntry{
			Timestamp:    event.Ts.Format(time.RFC3339Nano),
			Operation:    event.Operation,
			Command:      event.Command,
			Status:       string(event.Status),
			Component:    event.Component,
			TraceID:      event.TraceID,
			SpanID:       event.SpanID,
			ParentID:     event.ParentID,
			Service:      event.Service,
			Version:      event.Version,
			Subtype:      event.Subtype,
			SessionID:    event.SessionID,
			AgentID:      event.AgentID,
			WorkspaceID:  event.WorkspaceID,
			JobID:        event.JobID,
			DurationMS:   event.DurationMS,
			ErrorType:    event.ErrorType,
			ErrorCode:    event.ErrorCode,
			ErrorMessage: event.ErrorMessage,
			Retriable:    event.Retriable,
			Data:         event.Data,
		}

		entries = append(entries, entry)
	}

	return entries, reader.Err()
}