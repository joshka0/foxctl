package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/daemon"
	agenttypes "github.com/jkatigb/agentctl/internal/domain/agent"
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

// AgentDetailHandler returns a handler for /api/agents/{id} routes.
// Routes:
//   - GET /api/agents/{id} - Get agent details
//   - POST /api/agents/{id}/daemon/start - Start agent via daemon
//   - GET /api/agents/{id}/daemon/sessions - List active daemon sessions for agent
func AgentDetailHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract agent ID and action from path: /api/agents/{id}[/daemon/action]
		path := r.URL.Path
		const prefix = "/api/agents/"
		if !strings.HasPrefix(path, prefix) {
			httpError(w, http.StatusNotFound, "not found")
			return
		}

		remaining := strings.TrimPrefix(path, prefix)
		parts := strings.SplitN(remaining, "/", 3)
		agentID := parts[0]

		if agentID == "" {
			httpError(w, http.StatusBadRequest, "missing agent id")
			return
		}

		// Route based on action
		if len(parts) >= 3 && parts[1] == "daemon" {
			action := parts[2]
			switch action {
			case "start":
				handleAgentDaemonStart(w, r, cfg, log, agentID)
			case "sessions":
				handleAgentDaemonSessions(w, r, log, agentID)
			default:
				httpError(w, http.StatusNotFound, "unknown daemon action")
			}
			return
		}

		// Default: GET agent details
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

// AgentDaemonStartRequest is the request body for starting an agent daemon.
type AgentDaemonStartRequest struct {
	Workspace string `json:"workspace,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
}

// AgentDaemonStartResponse is the response for starting an agent daemon.
type AgentDaemonStartResponse struct {
	SessionID string `json:"session_id"`
	ActorID   string `json:"actor_id"`
	Status    string `json:"status"`
}

// handleAgentDaemonStart starts an agent via the daemon.
func handleAgentDaemonStart(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, agentID string) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Parse request body
	var req AgentDaemonStartRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			httpError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	// Look up agent config from DB
	store, err := agents.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open agents store")
		httpError(w, http.StatusInternalServerError, "failed to open agents store")
		return
	}
	defer store.Close()

	agent, err := store.Get(r.Context(), agentID)
	if err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Msg("agent not found")
		httpError(w, http.StatusNotFound, "agent not found")
		return
	}

	// Connect to daemon
	client := daemon.NewClient()
	if !client.IsRunning() {
		httpError(w, http.StatusServiceUnavailable, "daemon not running")
		return
	}

	// Determine workspace - use request, then agent namespace, then default
	workspace := req.Workspace
	if workspace == "" {
		workspace = agent.Namespace
	}

	// Use prompt from request if provided, else from agent config
	prompt := req.Prompt
	if prompt == "" {
		prompt = agent.Prompt
	}

	// Spawn agent via daemon
	params := daemon.AgentSpawnParams{
		Role:        agent.Role,
		AgentID:     agentID, // Pass agent config ID for session filtering
		WorkspaceID: workspace,
		Prompt:      prompt,
		SkillsAllow: agent.SkillsAllow,
	}

	result, err := client.AgentSpawn(params)
	if err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Msg("failed to spawn agent")
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Update agent state in DB
	if err := store.UpdateState(r.Context(), agentID, agenttypes.StateRunning); err != nil {
		log.Warn().Err(err).Str("agent_id", agentID).Msg("failed to update agent state")
	}

	writeJSON(w, http.StatusOK, AgentDaemonStartResponse{
		SessionID: result.SessionID,
		ActorID:   result.ActorID,
		Status:    result.Status,
	})
}

// handleAgentDaemonSessions lists active daemon sessions for a specific agent.
func handleAgentDaemonSessions(w http.ResponseWriter, r *http.Request, log zerolog.Logger, agentID string) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Connect to daemon
	client := daemon.NewClient()
	if !client.IsRunning() {
		httpError(w, http.StatusServiceUnavailable, "daemon not running")
		return
	}

	result, err := client.AgentList()
	if err != nil {
		log.Error().Err(err).Msg("failed to list agent sessions")
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Filter sessions by agent ID (matches ActorID in session config)
	filtered := make([]daemon.AgentSessionInfo, 0)
	for _, session := range result.Sessions {
		if session.ActorID == agentID {
			filtered = append(filtered, session)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"sessions": filtered,
		"count":    len(filtered),
	})
}
