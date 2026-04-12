package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
)

// TaskResponse represents a task in API responses.
type TaskResponse struct {
	ID          string   `json:"id"`
	WorkspaceID string   `json:"workspace_id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	ScopePath   string   `json:"scope_path,omitempty"`
	ParentID    string   `json:"parent_id,omitempty"`
	Children    []string `json:"children,omitempty"`
	DependsOn   []string `json:"depends_on,omitempty"`
	Status      string   `json:"status"`
	CreatedAt   string   `json:"created_at"`
	CompletedAt string   `json:"completed_at,omitempty"`
	Notes       string   `json:"notes,omitempty"`
	Gotchas     string   `json:"gotchas,omitempty"`
	SessionID   string   `json:"session_id,omitempty"`
	PageRank    float64  `json:"pagerank"`
}

// TaskStats provides summary statistics for tasks.
type TaskStats struct {
	Total       int `json:"total"`
	Pending     int `json:"pending"`
	InProgress  int `json:"in_progress"`
	Completed   int `json:"completed"`
	Blocked     int `json:"blocked"`
	ReadyReview int `json:"ready_for_review"`
}

// TasksListHandler returns a handler for GET /api/tasks.
func TasksListHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
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

		status := r.URL.Query().Get("status")
		sessionID := r.URL.Query().Get("session_id")

		// Open task store
		store, err := tasks.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open tasks store")
			httpError(w, http.StatusInternalServerError, "failed to open tasks store")
			return
		}
		defer store.Close()

		// Build list options
		opts := tasks.ListOptions{
			Limit:     limit,
			SessionID: sessionID,
		}
		if status != "" {
			opts.Statuses = []string{status}
		}

		// List tasks for workspace (from query param or default to ".")
		workspace := r.URL.Query().Get("workspace")
		if workspace == "" {
			workspace = "."
		}
		taskList, err := store.ListWithOptions(r.Context(), workspace, opts)
		if err != nil {
			log.Error().Err(err).Msg("failed to list tasks")
			httpError(w, http.StatusInternalServerError, "failed to list tasks")
			return
		}

		// Convert to response format and compute stats
		resp := make([]TaskResponse, 0, len(taskList))
		stats := TaskStats{}

		for _, t := range taskList {
			tr := TaskResponse{
				ID:          t.ID,
				WorkspaceID: t.WorkspaceID,
				Title:       t.Title,
				Description: t.Description,
				ScopePath:   t.ScopePath,
				ParentID:    t.ParentID,
				Children:    t.Children,
				DependsOn:   t.DependsOn,
				Status:      t.Status,
				CreatedAt:   t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
				Notes:       t.Notes,
				Gotchas:     t.Gotchas,
				SessionID:   t.SessionID,
				PageRank:    t.PageRank,
			}
			if t.CompletedAt != nil {
				tr.CompletedAt = t.CompletedAt.Format("2006-01-02T15:04:05Z07:00")
			}
			resp = append(resp, tr)

			// Update stats
			stats.Total++
			switch t.Status {
			case tasks.StatusPending:
				stats.Pending++
			case tasks.StatusInProgress:
				stats.InProgress++
			case tasks.StatusCompleted:
				stats.Completed++
			case tasks.StatusBlocked:
				stats.Blocked++
			case tasks.StatusReadyForReview:
				stats.ReadyReview++
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"tasks": resp,
			"stats": stats,
		})
	}
}

// TaskDetailHandler returns a handler for GET /api/tasks/{id}.
func TaskDetailHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract task ID from path: /api/tasks/{id}
		path := r.URL.Path
		const prefix = "/api/tasks/"
		if !strings.HasPrefix(path, prefix) {
			httpError(w, http.StatusNotFound, "not found")
			return
		}

		remaining := strings.TrimPrefix(path, prefix)
		parts := strings.SplitN(remaining, "/", 2)
		taskID := parts[0]

		if taskID == "" {
			httpError(w, http.StatusBadRequest, "missing task id")
			return
		}

		// Handle sub-routes
		if len(parts) > 1 {
			switch parts[1] {
			case "complete":
				handleTaskComplete(w, r, cfg, log, taskID)
				return
			case "uncomplete":
				handleTaskUncomplete(w, r, cfg, log, taskID)
				return
			}
		}

		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Open task store
		store, err := tasks.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open tasks store")
			httpError(w, http.StatusInternalServerError, "failed to open tasks store")
			return
		}
		defer store.Close()

		// Get task
		task, err := store.Get(r.Context(), taskID)
		if err != nil {
			log.Error().Err(err).Str("task_id", taskID).Msg("failed to get task")
			httpError(w, http.StatusNotFound, "task not found")
			return
		}

		tr := TaskResponse{
			ID:          task.ID,
			WorkspaceID: task.WorkspaceID,
			Title:       task.Title,
			Description: task.Description,
			ScopePath:   task.ScopePath,
			ParentID:    task.ParentID,
			Children:    task.Children,
			DependsOn:   task.DependsOn,
			Status:      task.Status,
			CreatedAt:   task.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			Notes:       task.Notes,
			Gotchas:     task.Gotchas,
			SessionID:   task.SessionID,
			PageRank:    task.PageRank,
		}
		if task.CompletedAt != nil {
			tr.CompletedAt = task.CompletedAt.Format("2006-01-02T15:04:05Z07:00")
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"task": tr,
		})
	}
}

// handleTaskComplete handles POST /api/tasks/{id}/complete.
func handleTaskComplete(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, taskID string) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	store, err := tasks.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open tasks store")
		httpError(w, http.StatusInternalServerError, "failed to open tasks store")
		return
	}
	defer store.Close()

	task, err := store.Get(r.Context(), taskID)
	if err != nil {
		httpError(w, http.StatusNotFound, "task not found")
		return
	}

	task.Status = tasks.StatusCompleted
	now := time.Now().UTC()
	task.CompletedAt = &now
	task, err = store.Update(r.Context(), task)
	if err != nil {
		log.Error().Err(err).Str("task_id", taskID).Msg("failed to complete task")
		httpError(w, http.StatusInternalServerError, "failed to complete task")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"status": task.Status,
	})
}

// handleTaskUncomplete handles POST /api/tasks/{id}/uncomplete.
func handleTaskUncomplete(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, taskID string) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	store, err := tasks.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open tasks store")
		httpError(w, http.StatusInternalServerError, "failed to open tasks store")
		return
	}
	defer store.Close()

	task, err := store.Get(r.Context(), taskID)
	if err != nil {
		httpError(w, http.StatusNotFound, "task not found")
		return
	}

	task.Status = tasks.StatusInProgress
	task.CompletedAt = nil
	task, err = store.Update(r.Context(), task)
	if err != nil {
		log.Error().Err(err).Str("task_id", taskID).Msg("failed to uncomplete task")
		httpError(w, http.StatusInternalServerError, "failed to uncomplete task")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"status": task.Status,
	})
}
