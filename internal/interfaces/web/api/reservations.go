package api

import (
	"net/http"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
)

// ReservationResponse represents a file reservation in API responses.
type ReservationResponse struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Holder    string `json:"holder"`
	Mode      string `json:"mode"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// ReservationsListHandler returns a handler for GET /api/reservations.
// Reservations are file locks held by agents during editing.
func ReservationsListHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		workspace := r.URL.Query().Get("workspace_id")
		if workspace == "" {
			workspace = r.URL.Query().Get("workspace")
		}
		if workspace == "" {
			httpError(w, http.StatusBadRequest, "workspace_id required")
			return
		}

		store, err := blackboard.OpenBoardStore(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open board store")
			httpError(w, http.StatusInternalServerError, "failed to open board store")
			return
		}
		defer store.Close()

		reservations, err := store.ListReservations(r.Context(), workspace)
		if err != nil {
			log.Error().Err(err).Msg("failed to list reservations")
			httpError(w, http.StatusInternalServerError, "failed to list reservations")
			return
		}

		resp := convertReservations(reservations)
		writeJSON(w, http.StatusOK, map[string]any{
			"reservations": resp,
		})
	}
}

func convertReservations(reservations []agent.FileReservation) []ReservationResponse {
	resp := make([]ReservationResponse, 0, len(reservations))
	for _, r := range reservations {
		resp = append(resp, ReservationResponse{
			ID:        r.ID,
			Path:      r.Path,
			Holder:    r.Holder,
			Mode:      string(r.Mode),
			ExpiresAt: r.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	return resp
}
