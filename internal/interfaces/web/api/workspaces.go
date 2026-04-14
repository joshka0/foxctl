package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/sessions"
)

// workspaceState tracks the currently selected workspace for the web server.
var (
	currentWorkspaceMu sync.RWMutex
	currentWorkspace   string
)

// GetCurrentWorkspace returns the currently selected workspace, falling back to cwd.
func GetCurrentWorkspace() string {
	currentWorkspaceMu.RLock()
	ws := currentWorkspace
	currentWorkspaceMu.RUnlock()

	if ws != "" {
		return ws
	}
	if pwd, err := os.Getwd(); err == nil {
		return pwd
	}
	return ""
}

// SetCurrentWorkspace sets the current workspace.
func SetCurrentWorkspace(path string) {
	currentWorkspaceMu.Lock()
	currentWorkspace = path
	currentWorkspaceMu.Unlock()
}

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

	// Determine current workspace (use selected workspace if set)
	activeWorkspace := GetCurrentWorkspace()

	// Convert to slice
	workspaces := make([]WorkspaceInfo, 0, len(workspaceMap))
	for _, info := range workspaceMap {
		info.IsActive = info.Path == activeWorkspace
		workspaces = append(workspaces, *info)
	}

	return workspaces, activeWorkspace, nil
}

// WorkspaceSwitchHandler returns a handler for POST /api/workspaces/switch.
// WorkspaceSwitchHandler returns an HTTP handler that switches the server's active workspace.
//
// The handler accepts a POST request and determines the target workspace path from the
// "workspace" or "path" query parameters, or from a JSON body { "path": "<workspace>" }.
// It validates that the path is absolute, does not contain `..`, exists on disk, and is a directory.
// On success it sets the server-side current workspace, logs the change, and responds with JSON
// containing the selected path and its base name. On method mismatch it responds 405; for
// missing/invalid input or validation failures it responds 400; for filesystem errors it responds 500.
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
			if err := readJSON(w, r, &req); err != nil {
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

		// Store the workspace selection in server-side state
		SetCurrentWorkspace(path)
		log.Info().Str("workspace", path).Msg("workspace switched")

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        true,
			"workspace": path,
			"name":      filepath.Base(path),
		})
	}
}
