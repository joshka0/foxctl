package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/jobs"
)

// JobResponse represents a job in API responses.
type JobResponse struct {
	ID         string `json:"id"`
	Command    string `json:"command"`
	Type       string `json:"type,omitempty"`
	Category   string `json:"category,omitempty"`
	Skill      string `json:"skill,omitempty"`
	State      string `json:"state"`
	Error      string `json:"error,omitempty"`
	ResultPath string `json:"result_path,omitempty"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// JobDetailResponse represents a detailed job response.
type JobDetailResponse struct {
	JobResponse
	Args       any      `json:"args,omitempty"`
	ResultData any      `json:"result_data,omitempty"`
	Stderr     string   `json:"stderr,omitempty"`
	Artifacts  []string `json:"artifacts,omitempty"`
}

// JobsListHandler returns a handler for GET /api/jobs.
func JobsListHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Parse optional limit parameter
		limit := 100
		if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
			if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 1000 {
				limit = n
			}
		}
		stateFilter := normalizeJobState(r.URL.Query().Get("state"))

		// Open job store
		store, err := jobs.Open(r.Context(), cfg.Paths.Jobs)
		if err != nil {
			log.Error().Err(err).Msg("failed to open jobs store")
			httpError(w, http.StatusInternalServerError, "failed to open jobs store")
			return
		}
		defer store.Close()

		// List jobs
		jobList, err := store.List(r.Context(), limit)
		if err != nil {
			log.Error().Err(err).Msg("failed to list jobs")
			httpError(w, http.StatusInternalServerError, "failed to list jobs")
			return
		}

		// Convert to response format
		resp := make([]JobResponse, 0, len(jobList))
		for _, j := range jobList {
			if stateFilter != "" && string(j.State) != stateFilter {
				continue
			}
			jobType, category, skill := parseJobCommand(j.Command)
			resp = append(resp, JobResponse{
				ID:         j.ID,
				Command:    j.Command,
				Type:       jobType,
				Category:   category,
				Skill:      skill,
				State:      string(j.State),
				Error:      j.Error,
				ResultPath: j.ResultPath,
				CreatedAt:  j.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
				UpdatedAt:  j.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
			})
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"jobs":  resp,
			"count": len(resp),
		})
	}
}

// JobDetailHandler returns a handler for /api/jobs/{id} and subroutes.
func JobDetailHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract job ID from path: /api/jobs/{id}
		jobID := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
		if jobID == "" {
			httpError(w, http.StatusBadRequest, "missing job id")
			return
		}

		// Handle sub-routes
		if strings.Contains(jobID, "/") {
			parts := strings.SplitN(jobID, "/", 2)
			jobID = parts[0]
			subRoute := parts[1]

			if subRoute == "result" {
				handleJobResult(w, r, cfg, log, jobID)
				return
			}
			if subRoute == "progress" {
				handleJobProgress(w, r, cfg, log, jobID)
				return
			}
			if subRoute == "events" {
				handleJobEvents(w, r, cfg, log, jobID)
				return
			}
			if subRoute == "cancel" {
				handleJobCancel(w, r, cfg, log, jobID)
				return
			}
			if subRoute == "wait" {
				handleJobWait(w, r, cfg, log, jobID)
				return
			}
			httpError(w, http.StatusNotFound, "not found")
			return
		}

		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Open job store
		store, err := jobs.Open(r.Context(), cfg.Paths.Jobs)
		if err != nil {
			log.Error().Err(err).Msg("failed to open jobs store")
			httpError(w, http.StatusInternalServerError, "failed to open jobs store")
			return
		}
		defer store.Close()

		// Get job
		job, err := store.Get(r.Context(), jobID)
		if err != nil {
			if errors.Is(err, jobs.ErrNotFound) {
				httpError(w, http.StatusNotFound, "job not found")
				return
			}
			log.Error().Err(err).Str("job_id", jobID).Msg("failed to get job")
			httpError(w, http.StatusInternalServerError, "failed to get job")
			return
		}

		// Parse args JSON for response
		var args any
		if job.ArgsJSON != "" {
			_ = json.Unmarshal([]byte(job.ArgsJSON), &args)
		}

		jobType, category, skill := parseJobCommand(job.Command)
		resultData := loadJobResult(r.Context(), store, jobID)
		stderr := readJobStderr(cfg.Paths.Jobs, jobID)
		artifacts := readJobArtifacts(cfg.Paths.Jobs, jobID)

		resp := JobDetailResponse{
			JobResponse: JobResponse{
				ID:         job.ID,
				Command:    job.Command,
				Type:       jobType,
				Category:   category,
				Skill:      skill,
				State:      string(job.State),
				Error:      job.Error,
				ResultPath: job.ResultPath,
				CreatedAt:  job.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
				UpdatedAt:  job.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
			},
			Args:       args,
			ResultData: resultData,
			Stderr:     stderr,
			Artifacts:  artifacts,
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// handleJobResult handles GET /api/jobs/{id}/result.
func handleJobResult(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, jobID string) {
	store, err := jobs.Open(r.Context(), cfg.Paths.Jobs)
	if err != nil {
		log.Error().Err(err).Msg("failed to open jobs store")
		httpError(w, http.StatusInternalServerError, "failed to open jobs store")
		return
	}
	defer store.Close()

	result, err := store.Result(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, jobs.ErrNotFound) {
			httpError(w, http.StatusNotFound, "job not found")
			return
		}
		log.Error().Err(err).Str("job_id", jobID).Msg("failed to get job result")
		httpError(w, http.StatusInternalServerError, "failed to get job result")
		return
	}

	// Return raw result JSON
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result)
}

// handleJobProgress handles GET /api/jobs/{id}/progress.
func handleJobProgress(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, jobID string) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	store, err := jobs.Open(r.Context(), cfg.Paths.Jobs)
	if err != nil {
		log.Error().Err(err).Msg("failed to open jobs store")
		httpError(w, http.StatusInternalServerError, "failed to open jobs store")
		return
	}
	defer store.Close()

	job, err := store.Get(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, jobs.ErrNotFound) {
			httpError(w, http.StatusNotFound, "job not found")
			return
		}
		log.Error().Err(err).Str("job_id", jobID).Msg("failed to get job")
		httpError(w, http.StatusInternalServerError, "failed to get job")
		return
	}

	limit := 200
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if n, parseErr := strconv.Atoi(limitStr); parseErr == nil {
			switch {
			case n <= 0:
				limit = 1
			case n > 2000:
				limit = 2000
			default:
				limit = n
			}
		}
	}

	events, err := loadJobProgressEvents(cfg.Paths.Jobs, jobID, limit)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusOK, map[string]any{
				"job_id": jobID,
				"state":  string(job.State),
				"events": []jobs.ProgressEvent{},
				"count":  0,
			})
			return
		}
		log.Error().Err(err).Str("job_id", jobID).Msg("failed to scan job progress")
		httpError(w, http.StatusInternalServerError, "failed to read job progress")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"job_id": jobID,
		"state":  string(job.State),
		"events": events,
		"count":  len(events),
	})
}

func handleJobEvents(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, jobID string) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	store, err := jobs.Open(r.Context(), cfg.Paths.Jobs)
	if err != nil {
		log.Error().Err(err).Msg("failed to open jobs store")
		httpError(w, http.StatusInternalServerError, "failed to open jobs store")
		return
	}
	defer store.Close()

	job, err := store.Get(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, jobs.ErrNotFound) {
			httpError(w, http.StatusNotFound, "job not found")
			return
		}
		log.Error().Err(err).Str("job_id", jobID).Msg("failed to get job")
		httpError(w, http.StatusInternalServerError, "failed to get job")
		return
	}

	limit := 200
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if n, parseErr := strconv.Atoi(limitStr); parseErr == nil {
			switch {
			case n <= 0:
				limit = 1
			case n > 2000:
				limit = 2000
			default:
				limit = n
			}
		}
	}
	events, err := loadJobProgressEvents(cfg.Paths.Jobs, jobID, limit)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Error().Err(err).Str("job_id", jobID).Msg("failed to scan job progress events")
		httpError(w, http.StatusInternalServerError, "failed to read job progress")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)
	writeSSEJSON(w, "job.snapshot", map[string]any{
		"job_id": jobID,
		"state":  string(job.State),
	})
	for _, event := range events {
		writeSSEJSON(w, "job.progress", event)
	}
	writeSSEJSON(w, "job.state", map[string]any{
		"job_id": jobID,
		"state":  string(job.State),
	})
	if flusher != nil {
		flusher.Flush()
	}
}

func handleJobCancel(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, jobID string) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	store, err := jobs.Open(r.Context(), cfg.Paths.Jobs)
	if err != nil {
		log.Error().Err(err).Msg("failed to open jobs store")
		httpError(w, http.StatusInternalServerError, "failed to open jobs store")
		return
	}
	defer store.Close()

	if err := store.Cancel(r.Context(), jobID); err != nil {
		switch {
		case errors.Is(err, jobs.ErrNotFound):
			httpError(w, http.StatusNotFound, "job not found")
		case errors.Is(err, jobs.ErrInvalidState):
			httpError(w, http.StatusConflict, err.Error())
		default:
			log.Error().Err(err).Str("job_id", jobID).Msg("failed to cancel job")
			httpError(w, http.StatusInternalServerError, "failed to cancel job")
		}
		return
	}
	job, err := store.Get(r.Context(), jobID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"job_id": jobID,
			"status": "canceled",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"job":    convertJobResponse(job),
		"job_id": jobID,
		"status": "canceled",
	})
}

func handleJobWait(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, jobID string) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	timeout := 30 * time.Second
	if raw := strings.TrimSpace(r.URL.Query().Get("timeout_ms")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			httpError(w, http.StatusBadRequest, "timeout_ms must be a non-negative integer")
			return
		}
		timeout = time.Duration(n) * time.Millisecond
	}
	if timeout > 5*time.Minute {
		timeout = 5 * time.Minute
	}
	poll := 500 * time.Millisecond
	if raw := strings.TrimSpace(r.URL.Query().Get("poll_ms")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			httpError(w, http.StatusBadRequest, "poll_ms must be a positive integer")
			return
		}
		poll = time.Duration(n) * time.Millisecond
	}

	store, err := jobs.Open(r.Context(), cfg.Paths.Jobs)
	if err != nil {
		log.Error().Err(err).Msg("failed to open jobs store")
		httpError(w, http.StatusInternalServerError, "failed to open jobs store")
		return
	}
	defer store.Close()

	ctx := r.Context()
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(r.Context(), timeout)
		defer cancel()
	}
	job, err := store.WaitForCompletion(ctx, jobID, poll)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			httpError(w, http.StatusGatewayTimeout, "job wait timed out")
			return
		}
		if errors.Is(err, jobs.ErrNotFound) {
			httpError(w, http.StatusNotFound, "job not found")
			return
		}
		log.Error().Err(err).Str("job_id", jobID).Msg("failed while waiting for job")
		httpError(w, http.StatusInternalServerError, "failed to wait for job")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"job":    convertJobResponse(job),
		"job_id": jobID,
		"state":  string(job.State),
	})
}

func loadJobProgressEvents(jobsRoot, jobID string, limit int) ([]jobs.ProgressEvent, error) {
	path := validateJobPath(jobsRoot, jobID, "progress.ndjson")
	if path == "" {
		return nil, errors.New("invalid job id")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	events := make([]jobs.ProgressEvent, 0, limit)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event jobs.ProgressEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}
	return events, nil
}

func writeSSEJSON(w http.ResponseWriter, event string, data any) {
	body, err := json.Marshal(data)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\n", event)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", body)
}

func convertJobResponse(j jobs.Job) JobResponse {
	jobType, category, skill := parseJobCommand(j.Command)
	return JobResponse{
		ID:         j.ID,
		Command:    j.Command,
		Type:       jobType,
		Category:   category,
		Skill:      skill,
		State:      string(j.State),
		Error:      j.Error,
		ResultPath: j.ResultPath,
		CreatedAt:  j.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:  j.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func parseJobCommand(command string) (jobType, category, skill string) {
	if command == "" {
		return "job", "", ""
	}
	if strings.HasPrefix(command, "skill:") {
		jobType = "skill"
		skill = strings.TrimPrefix(command, "skill:")
	} else if strings.Contains(command, ":") {
		parts := strings.SplitN(command, ":", 2)
		jobType = parts[0]
		skill = parts[1]
	} else {
		jobType = "job"
		skill = command
	}

	if skill == "" {
		return jobType, "", ""
	}
	if parts := strings.SplitN(skill, "/", 2); len(parts) == 2 {
		category = parts[0]
		skill = parts[1]
	}
	return jobType, category, skill
}

func normalizeJobState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "completed":
		return "ok"
	case "pending":
		return "queued"
	case "failed":
		return "error"
	case "cancelled":
		return "canceled"
	default:
		return strings.ToLower(strings.TrimSpace(state))
	}
}

func loadJobResult(ctx context.Context, store *jobs.Store, jobID string) any {
	result, err := store.Result(ctx, jobID)
	if err != nil {
		return nil
	}
	var parsed any
	if err := json.Unmarshal(result, &parsed); err == nil {
		return parsed
	}
	return string(result)
}

// validateJobPath validates and returns a safe path within the jobs directory.
// Returns empty string if the path would escape the root directory.
func validateJobPath(jobsRoot, jobID, filename string) string {
	// Clean the job ID to prevent directory traversal
	cleanID := filepath.Clean(jobID)
	if cleanID == "." || cleanID == ".." || strings.HasPrefix(cleanID, ".."+string(filepath.Separator)) {
		return ""
	}

	// Build the path and resolve it
	path := filepath.Join(jobsRoot, cleanID, filename)
	absPath, err := filepath.Abs(path)
	if err != nil {
		return ""
	}

	absRoot, err := filepath.Abs(jobsRoot)
	if err != nil {
		return ""
	}

	// Ensure the resolved path is within the jobs root
	if !strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) {
		return ""
	}

	return absPath
}

func readJobArtifacts(jobsRoot, jobID string) []string {
	path := validateJobPath(jobsRoot, jobID, "artifacts.json")
	if path == "" {
		return []string{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{}
	}
	var meta struct {
		Digests []string `json:"digests"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return []string{}
	}
	if meta.Digests == nil {
		return []string{}
	}
	return meta.Digests
}

func readJobStderr(jobsRoot, jobID string) string {
	path := validateJobPath(jobsRoot, jobID, "stderr.log")
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
