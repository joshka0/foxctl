package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/jobs"
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

// JobDetailHandler returns a handler for GET /api/jobs/{id}.
func JobDetailHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

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
			httpError(w, http.StatusNotFound, "not found")
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
