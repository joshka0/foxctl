package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

// WorkspaceInfo represents workspace information.
type WorkspaceInfo struct {
	Path         string `json:"path"`
	Name         string `json:"name"`
	SessionCount int    `json:"session_count"`
	LastActive   string `json:"last_active,omitempty"`
	IsActive     bool   `json:"is_active"`
}

// WorkspacesHandler returns a handler for GET /api/workspaces.
// Lists known workspaces from sessions database.
func WorkspacesHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Open sessions store
		store, err := sessions.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open sessions store")
			httpError(w, http.StatusInternalServerError, "failed to open sessions store")
			return
		}
		defer store.Close()

		// Query unique workspaces with counts
		workspaces, current, err := listWorkspaces(r.Context(), store, cfg)
		if err != nil {
			log.Error().Err(err).Msg("failed to list workspaces")
			httpError(w, http.StatusInternalServerError, "failed to list workspaces")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"workspaces": workspaces,
			"count":      len(workspaces),
			"current":    current,
		})
	}
}

// listWorkspaces queries unique workspace paths from sessions.
func listWorkspaces(ctx context.Context, store *sessions.Store, cfg config.Config) ([]WorkspaceInfo, string, error) {
	// Get all sessions (with a reasonable limit)
	sessionList, err := store.List(ctx, sessions.ListOptions{Limit: 1000})
	if err != nil {
		return nil, "", err
	}

	// Aggregate by workspace path
	workspaceMap := make(map[string]*WorkspaceInfo)
	for _, s := range sessionList {
		if s.WorkspacePath == "" {
			continue
		}

		info, ok := workspaceMap[s.WorkspacePath]
		if !ok {
			info = &WorkspaceInfo{
				Path: s.WorkspacePath,
				Name: filepath.Base(s.WorkspacePath),
			}
			workspaceMap[s.WorkspacePath] = info
		}
		info.SessionCount++

		// Track most recent activity
		if !s.StartedAt.IsZero() {
			ts := s.StartedAt.Format("2006-01-02T15:04:05Z07:00")
			if info.LastActive == "" || ts > info.LastActive {
				info.LastActive = ts
			}
		}
	}

	// Determine current workspace
	currentWorkspace := ""
	if pwd, err := os.Getwd(); err == nil {
		currentWorkspace = pwd
	}

	// Convert to slice
	workspaces := make([]WorkspaceInfo, 0, len(workspaceMap))
	for _, info := range workspaceMap {
		info.IsActive = info.Path == currentWorkspace
		workspaces = append(workspaces, *info)
	}

	return workspaces, currentWorkspace, nil
}

// WorkspaceSwitchHandler returns a handler for POST /api/workspaces/switch.
// Switch the active workspace context.
func WorkspaceSwitchHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		path := r.URL.Query().Get("workspace")
		if path == "" {
			path = r.URL.Query().Get("path")
		}

		if path == "" {
			var req struct {
				Path string `json:"path"`
			}
			if err := readJSON(r, &req); err != nil {
				httpError(w, http.StatusBadRequest, "invalid json")
				return
			}
			path = req.Path
		}

		if path == "" {
			httpError(w, http.StatusBadRequest, "path required")
			return
		}

		// Clean and validate the path to prevent path traversal
		cleanPath := filepath.Clean(path)
		if !filepath.IsAbs(cleanPath) {
			httpError(w, http.StatusBadRequest, "workspace path must be absolute")
			return
		}
		if strings.Contains(cleanPath, "..") {
			httpError(w, http.StatusBadRequest, "invalid workspace path")
			return
		}
		path = cleanPath

		// Validate the path exists
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				httpError(w, http.StatusBadRequest, "workspace path does not exist")
				return
			}
			log.Error().Err(err).Str("path", path).Msg("failed to stat workspace path")
			httpError(w, http.StatusInternalServerError, "failed to validate workspace path")
			return
		}

		if !info.IsDir() {
			httpError(w, http.StatusBadRequest, "workspace path must be a directory")
			return
		}

		// Note: In a web server context, we can't actually change the working directory
		// for the server process. This endpoint is informational - the client should
		// use this to filter/scope API calls by workspace.
		log.Info().Str("workspace", path).Msg("workspace switch requested")

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        true,
			"workspace": path,
			"name":      filepath.Base(path),
		})
	}
}
