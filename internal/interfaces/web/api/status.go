package api

import (
	"net/http"
	"runtime"
	"time"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/platform/config"
)

// StatusResponse is the response for GET /api/health.
type StatusResponse struct {
	OK             bool   `json:"ok"`
	Timestamp      string `json:"ts"`
	Version        string `json:"version"`
	GoVersion      string `json:"go_version"`
	Home           string `json:"home"`
	SkillsPath     string `json:"skills_path"`
	JobsPath       string `json:"jobs_path"`
	CASPath        string `json:"cas_path"`
	StorageRoot    string `json:"storage_root"`
	CacheRoot      string `json:"cache_root"`
	DatabaseDriver string `json:"database_driver"`
	VectorEnabled  bool   `json:"vector_enabled"`
}

// StatusHandler returns a handler for GET /api/health.
func StatusHandler(cfg config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		resp := StatusResponse{
			OK:             true,
			Timestamp:      time.Now().UTC().Format(time.RFC3339),
			Version:        "0.1.0", // TODO: inject from build
			GoVersion:      runtime.Version(),
			Home:           cfg.Home,
			SkillsPath:     cfg.Paths.Skills,
			JobsPath:       cfg.Paths.Jobs,
			CASPath:        cfg.Paths.CAS,
			StorageRoot:    cfg.Storage.Root,
			CacheRoot:      cfg.Paths.Cache,
			DatabaseDriver: cfg.Database.Driver,
			VectorEnabled:  cfg.Database.Vector.Enabled,
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":   true,
			"data": resp,
		})
	}
}
