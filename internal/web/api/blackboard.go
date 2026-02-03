package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
)

// BlackboardRecordResponse represents a blackboard record in API responses.
type BlackboardRecordResponse struct {
	ID       string `json:"id"`
	NS       string `json:"ns"`
	Topic    string `json:"topic"`
	TS       int64  `json:"ts"`
	TTLSec   int    `json:"ttl_sec"`
	Payload  string `json:"payload"`
	CASRef   string `json:"cas_ref,omitempty"`
	LeaseBy  string `json:"lease_by,omitempty"`
	LeaseExp int64  `json:"lease_exp,omitempty"`
}

// BlackboardPostRequest is the request body for posting to the blackboard.
type BlackboardPostRequest struct {
	NS      string `json:"ns"`
	Topic   string `json:"topic"`
	Payload string `json:"payload"`
	TTLSec  int    `json:"ttl_sec,omitempty"`
	CASRef  string `json:"cas_ref,omitempty"`
}

// BlackboardPostResponse is the response for posting to the blackboard.
type BlackboardPostResponse struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// BlackboardListHandler returns an HTTP handler that serves GET and POST requests for the /api/blackboard endpoint.
// GET lists blackboard records as JSON (response contains a "records" array) and accepts query parameters `limit`, `ns`, `topic`, and `all` with wildcard and default-namespace semantics; POST creates a new blackboard record (delegated to the post handler) and returns the created record metadata or appropriate HTTP errors.
func BlackboardListHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			handleBlackboardPost(w, r, cfg, log)
			return
		}
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

		ns := r.URL.Query().Get("ns")
		topic := r.URL.Query().Get("topic")
		all := parseBool(r.URL.Query().Get("all"))
		if ns == "*" || topic == "*" {
			all = true
		}

		if ns == "*" {
			ns = ""
		}
		if topic == "*" {
			topic = ""
		}

		if !all && ns == "" {
			ns = "default"
		}

		// Open blackboard store
		store, err := blackboard.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open blackboard store")
			httpError(w, http.StatusInternalServerError, "failed to open blackboard store")
			return
		}
		defer store.Close()

		// Use Search for wildcard queries, ListByTopic for specific topic
		var records []agent.BlackboardRecord
		if topic != "" && ns != "" {
			records, err = store.ListByTopic(r.Context(), ns, topic, limit)
		} else {
			// Search handles wildcard cases (empty ns and/or topic)
			records, err = store.Search(r.Context(), ns, topic, limit)
		}
		if err != nil {
			log.Error().Err(err).Msg("failed to list blackboard records")
			httpError(w, http.StatusInternalServerError, "failed to list blackboard records")
			return
		}

		// Convert to response format
		resp := make([]BlackboardRecordResponse, 0, len(records))
		for _, rec := range records {
			br := BlackboardRecordResponse{
				ID:      rec.ID,
				NS:      rec.NS,
				Topic:   rec.Topic,
				TS:      rec.TS,
				TTLSec:  rec.TTLSec,
				Payload: string(rec.Payload),
				CASRef:  rec.CASRef,
			}
			if rec.Lease != nil {
				br.LeaseBy = rec.Lease.Holder
				br.LeaseExp = rec.Lease.Until
			}
			resp = append(resp, br)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"records": resp,
		})
	}
}

// BlackboardDetailHandler returns a handler for /api/blackboard/{id} routes.
// Routes:
//   - GET /api/blackboard/{id} - Get record details
//
// BlackboardDetailHandler returns an HTTP handler that serves GET and DELETE for a single
// blackboard record identified by the URL path suffix (/api/blackboard/{id} or
// /api/v1/blackboard/{id}).
//
// GET responds with JSON containing the record under the "record" key when the record
// exists. DELETE removes the record and responds with a JSON status/message pair when
// deletion succeeds.
//
// The handler responds with 400 when the id is missing, 404 when a requested record is
// not found, 405 for unsupported methods, and 500 for storage-related errors.
func BlackboardDetailHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract record ID from path: /api/blackboard/{id}
		path := r.URL.Path
		prefixes := []string{"/api/v1/blackboard/", "/api/blackboard/"}
		var id string
		for _, prefix := range prefixes {
			if strings.HasPrefix(path, prefix) {
				id = strings.TrimPrefix(path, prefix)
				break
			}
		}

		if id == "" {
			httpError(w, http.StatusBadRequest, "missing record id")
			return
		}

		// Open blackboard store
		store, err := blackboard.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open blackboard store")
			httpError(w, http.StatusInternalServerError, "failed to open blackboard store")
			return
		}
		defer store.Close()

		switch r.Method {
		case http.MethodGet:
			// Get record
			rec, err := store.Get(r.Context(), id)
			if err != nil {
				log.Error().Err(err).Str("id", id).Msg("failed to get blackboard record")
				httpError(w, http.StatusNotFound, "record not found")
				return
			}

			br := BlackboardRecordResponse{
				ID:      rec.ID,
				NS:      rec.NS,
				Topic:   rec.Topic,
				TS:      rec.TS,
				TTLSec:  rec.TTLSec,
				Payload: string(rec.Payload),
				CASRef:  rec.CASRef,
			}
			if rec.Lease != nil {
				br.LeaseBy = rec.Lease.Holder
				br.LeaseExp = rec.Lease.Until
			}

			writeJSON(w, http.StatusOK, map[string]any{
				"record": br,
			})

		case http.MethodDelete:
			// Delete record
			if err := store.Delete(r.Context(), id); err != nil {
				log.Error().Err(err).Str("id", id).Msg("failed to delete blackboard record")
				httpError(w, http.StatusInternalServerError, "failed to delete record: "+err.Error())
				return
			}

			writeJSON(w, http.StatusOK, map[string]any{
				"status":  "deleted",
				"message": "Record deleted successfully",
			})

		default:
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

// handleBlackboardPost handles an HTTP request to create a new blackboard record.
//
// It decodes the request body as a BlackboardPostRequest, defaults the namespace to
// "default" when omitted, and requires a non-empty Topic. It opens the configured
// blackboard store, creates a record with a generated ID and current timestamp,
// and posts it to storage. On success it responds with HTTP 201 and a
// BlackboardPostResponse containing the new record ID. It responds with HTTP 400
// for invalid input and HTTP 500 for storage errors.
func handleBlackboardPost(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger) {
	// Parse request body
	var req BlackboardPostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.NS == "" {
		req.NS = "default"
	}
	if req.Topic == "" {
		httpError(w, http.StatusBadRequest, "topic is required")
		return
	}

	// Open blackboard store
	store, err := blackboard.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open blackboard store")
		httpError(w, http.StatusInternalServerError, "failed to open blackboard store")
		return
	}
	defer store.Close()

	// Create the record
	record := agent.BlackboardRecord{
		ID:      uuid.New().String(),
		NS:      req.NS,
		Topic:   req.Topic,
		TS:      time.Now().UnixMilli(),
		TTLSec:  req.TTLSec,
		Payload: json.RawMessage(req.Payload),
		CASRef:  req.CASRef,
	}

	// Post the record
	if err := store.Post(r.Context(), record); err != nil {
		log.Error().Err(err).Msg("failed to post blackboard record")
		httpError(w, http.StatusInternalServerError, "failed to post record: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, BlackboardPostResponse{
		ID:      record.ID,
		Status:  "created",
		Message: "Record created successfully",
	})
}
