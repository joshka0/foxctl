package api

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/joshka0/foxctl/internal/platform/config"
)

// LivenessHandler returns 200 if the process is alive.
// Used by Kubernetes liveness probes: GET /healthz
// Response is intentionally minimal (no envelope) for probe compatibility.
func LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	}
}

// ReadinessHandler returns 200 when the service can accept traffic.
// Checks that the storage root directory exists and is writable.
// Used by Kubernetes readiness probes: GET /readyz
func ReadinessHandler(cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		checks := map[string]string{}

		// Check storage accessibility and writability
		if err := probeStorageWritable(cfg.Storage.Root); err != nil {
			checks["storage"] = err.Error()
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "not_ready",
				"checks": checks,
				"hint":   "Ensure the storage directory exists and is writable: " + cfg.Storage.Root,
			})
			return
		}
		checks["storage"] = "ok"

		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"checks": checks,
		})
	}
}

// probeStorageWritable checks that the directory exists and is writable
// by creating and immediately removing a temporary file.
func probeStorageWritable(dir string) error {
	if _, err := os.Stat(dir); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".healthprobe-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(filepath.Clean(name))
}
