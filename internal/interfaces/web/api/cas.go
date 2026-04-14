package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/cas"
)

// CASHandler returns a handler for GET /api/cas/{digest}.
// Supports:
//   - GET /api/cas/{digest} - Get content by digest
//   - GET /api/cas/{digest}?meta=1 - Get metadata only
//   - HEAD /api/cas/{digest} - Check existence / get metadata headers
func CASHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract digest from path: /api/cas/{digest}
		digest := strings.TrimPrefix(r.URL.Path, "/api/cas/")
		if digest == "" {
			httpError(w, http.StatusBadRequest, "missing digest")
			return
		}

		// Normalize digest format (accept bare hex or sha256:hex)
		if !strings.HasPrefix(digest, "sha256:") {
			digest = "sha256:" + digest
		}

		// Open CAS store
		store, err := cas.NewStore(cfg.Paths.CAS)
		if err != nil {
			log.Error().Err(err).Msg("failed to open cas store")
			httpError(w, http.StatusInternalServerError, "failed to open cas store")
			return
		}
		defer store.Close()

		switch r.Method {
		case http.MethodHead:
			handleCASHead(w, r, store, log, digest)
		case http.MethodGet:
			if r.URL.Query().Get("meta") == "1" {
				handleCASMeta(w, r, store, log, digest)
			} else {
				handleCASGet(w, r, store, log, digest)
			}
		default:
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

// handleCASHead returns metadata as headers.
func handleCASHead(w http.ResponseWriter, r *http.Request, store *cas.Store, log zerolog.Logger, digest string) {
	obj, err := store.Head(r.Context(), digest)
	if err != nil {
		if errors.Is(err, cas.ErrNotFound) {
			httpError(w, http.StatusNotFound, "not found")
			return
		}
		log.Error().Err(err).Str("digest", digest).Msg("cas head failed")
		httpError(w, http.StatusInternalServerError, "cas lookup failed")
		return
	}

	w.Header().Set("Content-Type", obj.Kind)
	w.Header().Set("X-CAS-Digest", obj.Digest)
	w.Header().Set("X-CAS-Size", formatInt(obj.Size))
	w.Header().Set("X-CAS-Pinned", formatBool(obj.Pinned))
	w.WriteHeader(http.StatusOK)
}

// handleCASMeta returns metadata as JSON.
func handleCASMeta(w http.ResponseWriter, r *http.Request, store *cas.Store, log zerolog.Logger, digest string) {
	obj, err := store.Head(r.Context(), digest)
	if err != nil {
		if errors.Is(err, cas.ErrNotFound) {
			httpError(w, http.StatusNotFound, "not found")
			return
		}
		log.Error().Err(err).Str("digest", digest).Msg("cas head failed")
		httpError(w, http.StatusInternalServerError, "cas lookup failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"digest":     obj.Digest,
		"size":       obj.Size,
		"kind":       obj.Kind,
		"tags":       obj.Tags,
		"pinned":     obj.Pinned,
		"created_at": obj.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

// handleCASGet streams the content.
func handleCASGet(w http.ResponseWriter, r *http.Request, store *cas.Store, log zerolog.Logger, digest string) {
	reader, meta, err := store.Get(r.Context(), digest)
	if err != nil {
		if errors.Is(err, cas.ErrNotFound) {
			httpError(w, http.StatusNotFound, "not found")
			return
		}
		log.Error().Err(err).Str("digest", digest).Msg("cas get failed")
		httpError(w, http.StatusInternalServerError, "cas read failed")
		return
	}
	defer reader.Close()

	// Set content type from metadata
	contentType := meta.Kind
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-CAS-Digest", meta.Digest)
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, reader); err != nil {
		log.Debug().Err(err).Str("digest", digest).Msg("cas stream interrupted")
	}
}

func formatInt(n int64) string {
	return strconv.FormatInt(n, 10)
}

func formatBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
