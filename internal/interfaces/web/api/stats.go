package api

import (
	"net/http"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/intelligence/analysis/tasksgraph"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/jobs"
	jobtypes "github.com/jkatigb/agentctl/internal/storage/jobs/types"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
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

		workspace := r.URL.Query().Get("workspace")
		if workspace == "" {
			workspace = "."
		}

		// Open tasks store
		store, err := tasks.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open tasks store")
			httpError(w, http.StatusInternalServerError, "failed to open tasks store")
			return
		}
		defer store.Close()

		// List all tasks for workspace
		taskList, err := store.ListWithOptions(r.Context(), workspace, tasks.ListOptions{
			Limit: 1000,
		})
		if err != nil {
			log.Error().Err(err).Msg("failed to list tasks")
			httpError(w, http.StatusInternalServerError, "failed to list tasks")
			return
		}

		// Run graph analysis
		analyzer := tasksgraph.NewAnalyzer()
		insights, err := analyzer.Analyze(taskList, workspace)
		if err != nil {
			log.Error().Err(err).Msg("failed to analyze task graph")
			httpError(w, http.StatusInternalServerError, "failed to analyze task graph")
			return
		}

		// Return in the format expected by the GUI
		writeJSON(w, http.StatusOK, map[string]any{
			"nodes":             insights.Nodes,
			"cycles":            insights.Cycles,
			"topological_order": insights.TopologicalOrder,
			"workspace_id":      insights.WorkspaceID,
			"generated_at":      insights.GeneratedAt,
		})
	}
}
