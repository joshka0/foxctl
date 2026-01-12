package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/agents"
)

// AgentResponse represents an agent in API responses.
type AgentResponse struct {
	ID          string   `json:"id"`
	ParentID    string   `json:"parent_id,omitempty"`
	Namespace   string   `json:"ns"`
	Role        string   `json:"role,omitempty"`
	SkillsAllow []string `json:"skills_allow"`
	ShareBB     string   `json:"share_bb"`
	State       string   `json:"state"`
	CreatedAt   string   `json:"created_at"`
	HeartbeatAt string   `json:"heartbeat_at,omitempty"`
	LLMProvider string   `json:"llm_provider,omitempty"`
	LLMModel    string   `json:"llm_model,omitempty"`
}

// AgentsListHandler returns a handler for GET /api/agents.
func AgentsListHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Parse query params
		limit := 100
		if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
			if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}

		// Open agents store
		store, err := agents.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open agents store")
			httpError(w, http.StatusInternalServerError, "failed to open agents store")
			return
		}
		defer store.Close()

		// List agents
		agentList, err := store.List(r.Context(), limit)
		if err != nil {
			log.Error().Err(err).Msg("failed to list agents")
			httpError(w, http.StatusInternalServerError, "failed to list agents")
			return
		}

		// Convert to response format
		resp := make([]AgentResponse, 0, len(agentList))
		for _, a := range agentList {
			ar := AgentResponse{
				ID:          a.ID,
				ParentID:    a.ParentID,
				Namespace:   a.Namespace,
				Role:        a.Role,
				SkillsAllow: a.SkillsAllow,
				ShareBB:     a.ShareBB,
				State:       string(a.State),
				LLMProvider: a.LLMProvider,
				LLMModel:    a.LLMModel,
			}
			if !a.CreatedAt.IsZero() {
				ar.CreatedAt = a.CreatedAt.Format("2006-01-02T15:04:05Z07:00")
			}
			if !a.HeartbeatAt.IsZero() {
				ar.HeartbeatAt = a.HeartbeatAt.Format("2006-01-02T15:04:05Z07:00")
			}
			resp = append(resp, ar)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"agents": resp,
			"total":  len(resp),
		})
	}
}

// AgentDetailHandler returns a handler for GET /api/agents/{id}.
func AgentDetailHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract agent ID from path: /api/agents/{id}
		path := r.URL.Path
		const prefix = "/api/agents/"
		if !strings.HasPrefix(path, prefix) {
			httpError(w, http.StatusNotFound, "not found")
			return
		}

		remaining := strings.TrimPrefix(path, prefix)
		parts := strings.SplitN(remaining, "/", 2)
		agentID := parts[0]

		if agentID == "" {
			httpError(w, http.StatusBadRequest, "missing agent id")
			return
		}

		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Open agents store
		store, err := agents.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open agents store")
			httpError(w, http.StatusInternalServerError, "failed to open agents store")
			return
		}
		defer store.Close()

		// Get agent
		agent, err := store.Get(r.Context(), agentID)
		if err != nil {
			log.Error().Err(err).Str("agent_id", agentID).Msg("failed to get agent")
			httpError(w, http.StatusNotFound, "agent not found")
			return
		}

		ar := AgentResponse{
			ID:          agent.ID,
			ParentID:    agent.ParentID,
			Namespace:   agent.Namespace,
			Role:        agent.Role,
			SkillsAllow: agent.SkillsAllow,
			ShareBB:     agent.ShareBB,
			State:       string(agent.State),
			LLMProvider: agent.LLMProvider,
			LLMModel:    agent.LLMModel,
		}
		if !agent.CreatedAt.IsZero() {
			ar.CreatedAt = agent.CreatedAt.Format("2006-01-02T15:04:05Z07:00")
		}
		if !agent.HeartbeatAt.IsZero() {
			ar.HeartbeatAt = agent.HeartbeatAt.Format("2006-01-02T15:04:05Z07:00")
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"agent": ar,
		})
	}
}
