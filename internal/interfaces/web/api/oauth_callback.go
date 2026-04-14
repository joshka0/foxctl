package api

import (
	"net/http"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/platform/config"
)

// OAuthCallbackHandler handles OAuth provider callbacks.
// Phase 1: validates state nonce, exchanges auth code for token, stores encrypted token.
func OAuthCallbackHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	_ = cfg // will be used when wiring to authbroker

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		state := r.URL.Query().Get("state")
		code := r.URL.Query().Get("code")
		errParam := r.URL.Query().Get("error")

		if errParam != "" {
			log.Warn().Str("error", errParam).Msg("OAuth callback received error from provider")
			http.Error(w, "OAuth authorization was denied or failed", http.StatusBadRequest)
			return
		}
		if state == "" || code == "" {
			http.Error(w, "missing state or code parameter", http.StatusBadRequest)
			return
		}

		// TODO: Wire to authbroker.CompleteAuth when store is available.
		log.Info().Str("state", state).Msg("OAuth callback received (stub — not yet wired)")
		http.Error(w, "OAuth callback not yet implemented", http.StatusNotImplemented)
	}
}
