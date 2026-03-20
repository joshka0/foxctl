package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

// LogEntry represents a log entry in API responses.
type LogEntry = observability.EventRecord

// LogsHandler returns a handler for GET /api/logs.
//
// Index:
// - Purpose: Serve filtered observability events via HTTP
// - Flow: validate method → parse query → resolve obs dir → read entries → respond
// - SideEffects: reads NDJSON log files
// - FailureModes: method not allowed, read errors, missing observability directory
// - Related: readLogEntries, readLogFileTail, readLogFile
// - Keywords: logs, limit, since, component, operation, workspace, errors_only
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

		obsDir := observability.ResolveObsDir()
		if obsDir == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"entries": []LogEntry{},
				"message": "Observability directory not found",
			})
			return
		}

		entries, err := observability.QueryEventRecords(r.Context(), observability.EventQueryOptions{
			ObsDir:          obsDir,
			Limit:           limit,
			Since:           sinceTime,
			Component:       componentFilter,
			OperationPrefix: operationFilter,
			WorkspaceID:     workspaceFilter,
			ErrorsOnly:      errorsOnly,
		})
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
