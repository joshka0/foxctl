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

// CASHandler routes CAS API requests.
// Supports:
//   - GET /api/cas - list CAS objects
//   - GET /api/cas/{digest}/read?page=&page_size= - paged preview
//   - POST /api/cas/{digest}/pin - pin object
//   - POST /api/cas/{digest}/unpin - unpin object
//   - GET /api/cas/{digest} - stream raw object
//   - GET /api/cas/{digest}?meta=1 - metadata only
//   - HEAD /api/cas/{digest} - metadata headers
func CASHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Open CAS store
		store, err := cas.NewStore(cfg.Paths.CAS)
		if err != nil {
			log.Error().Err(err).Msg("failed to open cas store")
			httpError(w, http.StatusInternalServerError, "failed to open cas store")
			return
		}
		defer store.Close()

		if r.URL.Path == "/api/cas" || r.URL.Path == "/api/cas/" {
			handleCASList(w, r, store, log)
			return
		}

		// Extract digest and optional sub-route from path: /api/cas/{digest}/{action}
		path := strings.TrimPrefix(r.URL.Path, "/api/cas/")
		if path == "" {
			httpError(w, http.StatusBadRequest, "missing digest")
			return
		}
		parts := strings.SplitN(path, "/", 2)
		digest := normalizeCASDigest(parts[0])
		if digest == "" {
			httpError(w, http.StatusBadRequest, "missing digest")
			return
		}

		action := ""
		if len(parts) == 2 {
			action = strings.TrimSpace(parts[1])
		}

		switch action {
		case "":
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
		case "read":
			handleCASRead(w, r, store, log, digest)
		case "pin":
			handleCASPin(w, r, store, log, digest)
		case "unpin":
			handleCASUnpin(w, r, store, log, digest)
		default:
			httpError(w, http.StatusNotFound, "not found")
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

func handleCASList(w http.ResponseWriter, r *http.Request, store *cas.Store, log zerolog.Logger) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	limit := 200
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		n, err := strconv.Atoi(rawLimit)
		if err == nil {
			switch {
			case n <= 0:
				limit = 1
			case n > 5000:
				limit = 5000
			default:
				limit = n
			}
		}
	}

	filterPinned := false
	pinnedOnly := false
	if rawPinned := strings.TrimSpace(r.URL.Query().Get("pinned")); rawPinned != "" {
		filterPinned = true
		pinnedOnly = strings.EqualFold(rawPinned, "true") || rawPinned == "1"
	}

	objects, err := store.List(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("cas list failed")
		httpError(w, http.StatusInternalServerError, "cas list failed")
		return
	}

	filtered := make([]cas.Object, 0, min(limit, len(objects)))
	for i := len(objects) - 1; i >= 0 && len(filtered) < limit; i-- {
		obj := objects[i]
		if filterPinned && obj.Pinned != pinnedOnly {
			continue
		}
		filtered = append(filtered, obj)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"objects": filtered,
		"count":   len(filtered),
	})
}

func handleCASRead(w http.ResponseWriter, r *http.Request, store *cas.Store, log zerolog.Logger, digest string) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	page := 1
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			page = n
		}
	}

	pageSize := 4096
	rawPageSize := strings.TrimSpace(r.URL.Query().Get("page_size"))
	if rawPageSize == "" {
		rawPageSize = strings.TrimSpace(r.URL.Query().Get("pageSize"))
	}
	if rawPageSize != "" {
		if n, err := strconv.Atoi(rawPageSize); err == nil {
			switch {
			case n <= 0:
				pageSize = 1
			case n > 256*1024:
				pageSize = 256 * 1024
			default:
				pageSize = n
			}
		}
	}

	reader, meta, err := store.Get(r.Context(), digest)
	if err != nil {
		if errors.Is(err, cas.ErrNotFound) {
			httpError(w, http.StatusNotFound, "not found")
			return
		}
		log.Error().Err(err).Str("digest", digest).Msg("cas read failed")
		httpError(w, http.StatusInternalServerError, "cas read failed")
		return
	}
	defer reader.Close()

	pageData, totalBytes, err := readCASPage(reader, page, pageSize)
	if err != nil {
		log.Error().Err(err).Str("digest", digest).Msg("cas read pagination failed")
		httpError(w, http.StatusInternalServerError, "cas read failed")
		return
	}

	totalPages := 0
	if totalBytes > 0 {
		totalPages = int((totalBytes + int64(pageSize) - 1) / int64(pageSize))
	}
	nextPage := 0
	prevPage := 0
	if totalPages > 0 && page < totalPages {
		nextPage = page + 1
	}
	if page > 1 && page <= totalPages {
		prevPage = page - 1
	}

	contentType := meta.Kind
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	resp := map[string]any{
		"digest":       meta.Digest,
		"content":      string(pageData),
		"page":         page,
		"total_pages":  totalPages,
		"page_size":    pageSize,
		"total_bytes":  totalBytes,
		"content_type": contentType,
	}
	if nextPage > 0 {
		resp["next_page"] = nextPage
	}
	if prevPage > 0 {
		resp["prev_page"] = prevPage
	}
	writeJSON(w, http.StatusOK, resp)
}

func handleCASPin(w http.ResponseWriter, r *http.Request, store *cas.Store, log zerolog.Logger, digest string) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := store.Pin(r.Context(), digest); err != nil {
		if errors.Is(err, cas.ErrNotFound) {
			httpError(w, http.StatusNotFound, "not found")
			return
		}
		log.Error().Err(err).Str("digest", digest).Msg("cas pin failed")
		httpError(w, http.StatusInternalServerError, "cas pin failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"digest": digest,
		"pinned": true,
	})
}

func handleCASUnpin(w http.ResponseWriter, r *http.Request, store *cas.Store, log zerolog.Logger, digest string) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := store.Unpin(r.Context(), digest); err != nil {
		if errors.Is(err, cas.ErrNotFound) {
			httpError(w, http.StatusNotFound, "not found")
			return
		}
		log.Error().Err(err).Str("digest", digest).Msg("cas unpin failed")
		httpError(w, http.StatusInternalServerError, "cas unpin failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"digest": digest,
		"pinned": false,
	})
}

func normalizeCASDigest(digest string) string {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return ""
	}
	if !strings.HasPrefix(digest, "sha256:") {
		digest = "sha256:" + digest
	}
	return digest
}

func readCASPage(reader io.Reader, page, pageSize int) ([]byte, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 1
	}
	start := int64((page - 1) * pageSize)
	end := start + int64(pageSize)

	buf := make([]byte, 32*1024)
	pageData := make([]byte, 0, pageSize)
	var totalBytes int64

	for {
		n, err := reader.Read(buf)
		if n > 0 {
			chunkStart := totalBytes
			chunkEnd := totalBytes + int64(n)
			if chunkEnd > start && chunkStart < end {
				from := 0
				to := n
				if start > chunkStart {
					from = int(start - chunkStart)
				}
				if end < chunkEnd {
					to = int(end - chunkStart)
				}
				if from < to {
					pageData = append(pageData, buf[from:to]...)
				}
			}
			totalBytes += int64(n)
		}

		if errors.Is(err, io.EOF) {
			return pageData, totalBytes, nil
		}
		if err != nil {
			return nil, totalBytes, err
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
