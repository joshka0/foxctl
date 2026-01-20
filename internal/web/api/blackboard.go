package api

import (
	"net/http"
	"strconv"

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

// BlackboardListHandler returns a handler for GET /api/blackboard.
func BlackboardListHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
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
