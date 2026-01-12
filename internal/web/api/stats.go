package api

import (
	"net/http"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/jobs"
	jobtypes "github.com/jkatigb/agentctl/internal/storage/jobs/types"
)

// StatsHandler returns a handler for GET /api/stats.
func StatsHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Open jobs store
		store, err := jobs.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open jobs store")
			httpError(w, http.StatusInternalServerError, "failed to open jobs store")
			return
		}
		defer store.Close()

		// List all jobs and compute stats
		jobList, err := store.List(r.Context(), 1000)
		if err != nil {
			log.Error().Err(err).Msg("failed to list jobs")
			httpError(w, http.StatusInternalServerError, "failed to list jobs")
			return
		}

		// Compute stats from job list
		var total, queued, running, completed, failed int
		for _, j := range jobList {
			total++
			switch j.State {
			case jobtypes.StateQueued:
				queued++
			case jobtypes.StateRunning:
				running++
			case jobtypes.StateOK:
				completed++
			case jobtypes.StateError:
				failed++
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true,
			"data": map[string]any{
				"total":       total,
				"pending":     queued,
				"running":     running,
				"completed":   completed,
				"failed":      failed,
				"canceled":    0,
				"avg_latency": 0,
				"avg_runtime": 0,
			},
		})
	}
}

// InsightsHandler returns a handler for GET /api/insights.
// Provides task graph analysis including PageRank priorities.
func InsightsHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// For now, return basic insights structure
		// Full implementation would analyze task graph
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true,
			"data": map[string]any{
				"priorities":    []any{},
				"critical_path": []any{},
				"cycles":        []any{},
				"stats": map[string]any{
					"total_tasks":     0,
					"completed_tasks": 0,
					"blocked_tasks":   0,
				},
			},
		})
	}
}
