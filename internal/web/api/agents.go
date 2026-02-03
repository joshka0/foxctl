package api

import (
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/companion"
	"github.com/jkatigb/agentctl/internal/daemon"
	agenttypes "github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/contextvar"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
)

// Agent name generation word lists for memorable random names
var agentAdjectives = []string{
	"swift", "bright", "clever", "noble", "quiet", "bold", "calm", "keen",
	"wise", "brave", "kind", "fair", "true", "warm", "sharp", "clear",
	"deep", "free", "wild", "soft", "strong", "gentle", "nimble", "steady",
}

var agentNouns = []string{
	"atlas", "nova", "echo", "iris", "luna", "orion", "sage", "phoenix",
	"zephyr", "ember", "cedar", "river", "cliff", "dawn", "frost", "grove",
	"harbor", "jade", "maple", "oak", "peak", "rain", "sky", "tide",
}

// generateAgentName generates a memorable hyphenated name composed of a random adjective and noun (for example, "swift-atlas").
func generateAgentName() string {
	adj := agentAdjectives[rand.Intn(len(agentAdjectives))]
	noun := agentNouns[rand.Intn(len(agentNouns))]
	return adj + "-" + noun
}

// AgentResponse represents an agent in API responses.
type AgentResponse struct {
	ID             string   `json:"id"`
	ParentID       string   `json:"parent_id,omitempty"`
	Namespace      string   `json:"ns"`
	Name           string   `json:"name,omitempty"` // Human name (e.g., "Luna", "Atlas")
	Slug           string   `json:"slug,omitempty"` // Human-readable handle (e.g., "researcher")
	Role           string   `json:"role,omitempty"`
	SkillsAllow    []string `json:"skills_allow"`
	ShareBB        string   `json:"share_bb"`
	State          string   `json:"state"`
	CreatedAt      string   `json:"created_at"`
	HeartbeatAt    string   `json:"heartbeat_at,omitempty"`
	LLMProvider    string   `json:"llm_provider,omitempty"`
	LLMModel       string   `json:"llm_model,omitempty"`
	ConversationID string   `json:"conversation_id,omitempty"` // Linked companion conversation ID
}

// AgentSpawnRequest is the request body for spawning a new agent.
type AgentSpawnRequest struct {
	Role        string   `json:"role"`
	Prompt      string   `json:"prompt"`
	WorkspaceID string   `json:"workspace_id,omitempty"`
	SkillsAllow []string `json:"skills_allow,omitempty"`

	// Agent metadata
	Name string `json:"name,omitempty"` // Human name (auto-generated if empty)
	Slug string `json:"slug,omitempty"` // Human-readable handle

	// Execution config
	ExecMode         string `json:"exec_mode,omitempty"`          // "reactive", "autonomous", "proactive", "story"
	MaxIterations    int    `json:"max_iterations,omitempty"`     // Max tool calls per turn
	MaxContextTokens int    `json:"max_context_tokens,omitempty"` // Context budget (0=no limit)
	MaxAutoTurns     int    `json:"max_auto_turns,omitempty"`     // Max autonomous continuations

	// LLM override
	LLMProvider string `json:"llm_provider,omitempty"`
	LLMModel    string `json:"llm_model,omitempty"`
}

// AgentSpawnResponse is the response for spawning a new agent.
type AgentSpawnResponse struct {
	SessionID string `json:"session_id"`
	ActorID   string `json:"actor_id"`
	Status    string `json:"status"`
	Name      string `json:"name,omitempty"` // Generated or provided name
}

// AgentsListHandler provides an HTTP handler for listing agents at GET /api/agents.
//
// The handler accepts an optional `limit` query parameter (1–500, default 100) to cap
// the number of returned agents. It opens the agents store, retrieves up to `limit`
// agents, converts them into API-friendly records (including human-facing Name, Slug,
// ConversationID and RFC3339-formatted CreatedAt/HeartbeatAt timestamps), and responds
// with a JSON object containing "agents" and "total". The handler responds with
// 405 if the request method is not GET and 500 on store or listing errors.
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
				ID:             a.ID,
				ParentID:       a.ParentID,
				Namespace:      a.Namespace,
				Name:           a.Name,
				Slug:           a.Slug,
				Role:           a.Role,
				SkillsAllow:    a.SkillsAllow,
				ShareBB:        a.ShareBB,
				State:          string(a.State),
				LLMProvider:    a.LLMProvider,
				LLMModel:       a.LLMModel,
				ConversationID: a.ConversationID,
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
//   - POST /api/agents/spawn - Spawn new agent (no existing config required)
//   - GET /api/agents/{id} - Get agent details
//   - DELETE /api/agents/{id} - Soft delete (trash) a stopped agent
//   - POST /api/agents/{id}/daemon/start - Start agent via daemon
//
// AgentDetailHandler returns an HTTP handler that routes requests for agent detail and related actions.
//
// The handler supports:
//   - POST /api/agents/spawn                             : spawn a new agent
//   - /api/agents/{id}/daemon/{start|sessions|kill}     : daemon actions for a specific agent
//   - POST /api/agents/{id}/ask                          : send a message to a running agent
//   - DELETE /api/agents/{id}                            : soft-delete (trash) an agent
//   - PATCH /api/agents/{id}                             : update agent fields (e.g., conversation_id)
//   - GET /api/agents/{id}                               : fetch agent details
//
// The handler also accepts the legacy /api/v1/agents/ prefix and returns standard HTTP errors
// for unknown daemon actions, missing agent IDs, unsupported methods, and internal failures.
func AgentDetailHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract agent ID and action from path: /api/agents/{id}[/daemon/action]
		path := r.URL.Path

		// Handle spawn endpoint specially
		if strings.HasSuffix(path, "/agents/spawn") || strings.HasSuffix(path, "/agents/spawn/") {
			handleAgentSpawn(w, r, cfg, log)
			return
		}

		const prefix = "/api/agents/"
		if !strings.HasPrefix(path, prefix) {
			// Also try v1 prefix
			const v1prefix = "/api/v1/agents/"
			if !strings.HasPrefix(path, v1prefix) {
				httpError(w, http.StatusNotFound, "not found")
				return
			}
			path = strings.Replace(path, v1prefix, prefix, 1)
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
			case "kill":
				handleAgentDaemonKill(w, r, cfg, log, agentID)
			default:
				httpError(w, http.StatusNotFound, "unknown daemon action")
			}
			return
		}

		// Handle POST /api/agents/{id}/ask - send message to running daemon agent
		if len(parts) >= 2 && parts[1] == "ask" {
			handleAgentAsk(w, r, cfg, log, agentID)
			return
		}

		// Handle DELETE for soft delete (trash)
		if r.Method == http.MethodDelete {
			handleAgentTrash(w, r, cfg, log, agentID)
			return
		}

		// Handle PATCH for updating agent fields (e.g., conversation_id)
		if r.Method == http.MethodPatch {
			handleAgentPatch(w, r, cfg, log, agentID)
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
			ID:             agent.ID,
			ParentID:       agent.ParentID,
			Namespace:      agent.Namespace,
			Name:           agent.Name,
			Slug:           agent.Slug,
			Role:           agent.Role,
			SkillsAllow:    agent.SkillsAllow,
			ShareBB:        agent.ShareBB,
			State:          string(agent.State),
			LLMProvider:    agent.LLMProvider,
			LLMModel:       agent.LLMModel,
			ConversationID: agent.ConversationID,
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

// handleAgentDaemonStart starts an agent via the daemon using the agent's stored configuration
// and an optional request body.
//
// It ensures the daemon is running, spawns a daemon session using the workspace and prompt
// from the request (falling back to the agent's namespace and prompt), updates the agent's
// state to running in the store, and writes an AgentDaemonStartResponse on success or an
// appropriate HTTP error on failure.
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

	// Connect to daemon, auto-start if not running
	client := daemon.NewClient()
	if err := client.EnsureRunning(); err != nil {
		log.Error().Err(err).Msg("failed to start daemon")
		httpError(w, http.StatusServiceUnavailable, "failed to start daemon: "+err.Error())
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
		log.Error().Err(err).Str("agent_id", agentID).Msg("failed to update agent state")
		httpError(w, http.StatusInternalServerError, "failed to update agent state: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, AgentDaemonStartResponse{
		SessionID: result.SessionID,
		ActorID:   result.ActorID,
		Status:    result.Status,
	})
}

// handleAgentDaemonSessions handles GET requests to list active daemon sessions for the specified agentID.
// It ensures the daemon is running, filters daemon sessions by ActorID equal to agentID, and writes a JSON
// response with "sessions" (slice of session info) and "count" (number of sessions). It responds with 405 on
// non-GET methods, 503 if the daemon cannot be started, and 500 on internal listing errors.
func handleAgentDaemonSessions(w http.ResponseWriter, r *http.Request, log zerolog.Logger, agentID string) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Connect to daemon, auto-start if not running
	client := daemon.NewClient()
	if err := client.EnsureRunning(); err != nil {
		log.Error().Err(err).Msg("failed to start daemon")
		httpError(w, http.StatusServiceUnavailable, "failed to start daemon: "+err.Error())
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

// handleAgentDaemonKill terminates a running daemon session for the given agent and ensures the agent's stored state is set to stopped.
//
// It handles POST requests and writes JSON HTTP responses describing the outcome. If the daemon is not running or no matching session is found,
// the handler updates the agent state to "stopped" and returns an OK payload indicating no active session; on successful termination it returns
// the killed session ID and status. Errors are reported via appropriate HTTP error responses.
func handleAgentDaemonKill(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, agentID string) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Connect to daemon
	client := daemon.NewClient()
	if !client.IsRunning() {
		// Daemon not running - just update agent state to stopped
		store, err := agents.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open agents store")
			httpError(w, http.StatusInternalServerError, "failed to open agents store")
			return
		}
		defer store.Close()
		if err := store.UpdateState(r.Context(), agentID, agenttypes.StateStopped); err != nil {
			log.Error().Err(err).Str("agent_id", agentID).Msg("failed to update agent state")
			httpError(w, http.StatusInternalServerError, "failed to update agent state")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			"session_id": "",
			"status":     "stopped",
			"message":    "agent state updated (daemon not running)",
		})
		return
	}

	// First, find the session ID for this agent
	listResult, err := client.AgentList()
	if err != nil {
		log.Error().Err(err).Msg("failed to list agent sessions")
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Find the session matching this agent ID
	var sessionID string
	for _, session := range listResult.Sessions {
		if session.ActorID == agentID {
			sessionID = session.SessionID
			break
		}
	}

	if sessionID == "" {
		// No active daemon session found - agent may have stopped without state update
		// Just update the agent state to stopped
		store, err := agents.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			log.Error().Err(err).Msg("failed to open agents store")
			httpError(w, http.StatusInternalServerError, "failed to open agents store")
			return
		}
		defer store.Close()
		if err := store.UpdateState(r.Context(), agentID, agenttypes.StateStopped); err != nil {
			log.Error().Err(err).Str("agent_id", agentID).Msg("failed to update agent state")
			httpError(w, http.StatusInternalServerError, "failed to update agent state")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			"session_id": "",
			"status":     "stopped",
			"message":    "agent state updated (no active session found)",
		})
		return
	}

	// Kill the session
	result, err := client.AgentKill(sessionID)
	if err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Str("session_id", sessionID).Msg("failed to kill agent session")
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Update agent state to stopped in store
	store, err := agents.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open agents store after kill")
		httpError(w, http.StatusInternalServerError, "agent killed but failed to open store for state update")
		return
	}
	defer store.Close()
	if err := store.UpdateState(r.Context(), agentID, agenttypes.StateStopped); err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Msg("failed to update agent state after kill")
		httpError(w, http.StatusInternalServerError, "agent killed but failed to update state")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"session_id": sessionID,
		"status":     result.Status,
		"message":    "agent session killed",
	})
}

// handleAgentSpawn creates a new agent record, launches it via the daemon, and responds with the spawn result.
//
// It only accepts POST requests with a JSON AgentSpawnRequest containing at minimum `role` and `prompt`.
// If no name is provided one is generated; a new ULID is assigned as the agent ID. The workspace is normalized
// (defaults to "default") and the execution mode is validated (must be reactive, autonomous, proactive, or story).
// The handler ensures the daemon is running, persists an agent record in the agents store with an initial state,
// requests the daemon to spawn the agent, updates the agent state to running on success, and returns an
// AgentSpawnResponse containing the session ID, actor ID (the persisted agent ID), status, and name.
// On validation, store, daemon, or spawn failures it writes an appropriate HTTP error response.
func handleAgentSpawn(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Parse request body
	var req AgentSpawnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Role == "" {
		httpError(w, http.StatusBadRequest, "role is required")
		return
	}
	if req.Prompt == "" {
		httpError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	// Connect to daemon, auto-start if not running
	client := daemon.NewClient()
	if err := client.EnsureRunning(); err != nil {
		log.Error().Err(err).Msg("failed to start daemon")
		httpError(w, http.StatusServiceUnavailable, "failed to start daemon: "+err.Error())
		return
	}

	// Generate name if not provided
	name := req.Name
	if name == "" {
		name = generateAgentName()
	}

	// Generate agent ID
	agentID := ulid.Make().String()

	// Determine workspace/namespace (normalize path)
	namespace := req.WorkspaceID
	if namespace == "" {
		namespace = "default"
	} else {
		namespace = workspace.Normalize(namespace)
	}

	// Determine and validate exec mode
	execMode := agenttypes.ExecutionMode(req.ExecMode)
	if execMode == "" {
		execMode = agenttypes.ModeReactive
	} else {
		switch execMode {
		case agenttypes.ModeReactive, agenttypes.ModeAutonomous, agenttypes.ModeProactive, agenttypes.ModeStory:
			// valid
		default:
			httpError(w, http.StatusBadRequest, "invalid exec_mode: must be reactive, autonomous, proactive, or story")
			return
		}
	}

	// Create agent record in store
	store, err := agents.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open agents store")
		httpError(w, http.StatusInternalServerError, "failed to open agents store")
		return
	}
	defer store.Close()

	agent := agenttypes.Agent{
		ID:            agentID,
		Namespace:     namespace,
		Name:          name,
		Slug:          req.Slug,
		Role:          req.Role,
		Prompt:        req.Prompt,
		SkillsAllow:   req.SkillsAllow,
		Policy:        agenttypes.Policy{},
		ShareBB:       "scoped",
		State:         agenttypes.StateStarting,
		CreatedAt:     time.Now().UTC(),
		LLMProvider:   req.LLMProvider,
		LLMModel:      req.LLMModel,
		ExecMode:      execMode,
		MaxIterations: req.MaxIterations,
		MaxAutoTurns:  req.MaxAutoTurns,
	}

	if agent.SkillsAllow == nil {
		agent.SkillsAllow = []string{}
	}

	if err := store.Create(r.Context(), agent); err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Msg("failed to create agent record")
		httpError(w, http.StatusInternalServerError, "failed to create agent record: "+err.Error())
		return
	}

	// Spawn agent via daemon - use normalized/validated values
	params := daemon.AgentSpawnParams{
		Role:             req.Role,
		AgentID:          agentID,   // Pass agent ID for linking
		WorkspaceID:      namespace, // Use normalized workspace
		Prompt:           req.Prompt,
		SkillsAllow:      req.SkillsAllow,
		Name:             name,
		Slug:             req.Slug,
		ExecMode:         string(execMode), // Use validated exec mode
		MaxIterations:    req.MaxIterations,
		MaxContextTokens: req.MaxContextTokens,
		MaxAutoTurns:     req.MaxAutoTurns,
		LLMProvider:      req.LLMProvider,
		LLMModel:         req.LLMModel,
	}

	result, err := client.AgentSpawn(params)
	if err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Msg("failed to spawn agent")
		// Update agent state to error since spawn failed
		_ = store.UpdateState(r.Context(), agentID, agenttypes.StateError)
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Update agent state to running
	if err := store.UpdateState(r.Context(), agentID, agenttypes.StateRunning); err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Msg("failed to update agent state to running")
		httpError(w, http.StatusInternalServerError, "failed to update agent state: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, AgentSpawnResponse{
		SessionID: result.SessionID,
		ActorID:   agentID, // Return our persisted agent ID, not the daemon's actor ID
		Status:    result.Status,
		Name:      name,
	})
}

// AgentAskRequest is the request body for POST /api/agents/{id}/ask.
type AgentAskRequest struct {
	Message string `json:"message"`
}

// AgentAskResponse is the response for POST /api/agents/{id}/ask.
type AgentAskResponse struct {
	Reply          string `json:"reply"`
	ConversationID string `json:"conversation_id"`
}

// handleAgentAsk sends a message to an agent via companion chat.
// handleAgentAsk handles POST requests to send a message to a running agent's companion chat and return the assistant's reply.
// It validates the request, uses the agent's stored ConversationID (falling back to the agent ID), forwards the message to the companion service, and writes an AgentAskResponse containing the reply and the conversation ID; on failures it returns the appropriate HTTP error.
func handleAgentAsk(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, agentID string) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Parse request body
	var req AgentAskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Message == "" {
		httpError(w, http.StatusBadRequest, "message is required")
		return
	}

	// Get agent to retrieve stored conversation_id
	agentStore, err := agents.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open agents store")
		httpError(w, http.StatusInternalServerError, "failed to open agents store")
		return
	}
	defer agentStore.Close()

	agent, err := agentStore.Get(r.Context(), agentID)
	if err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Msg("agent not found")
		httpError(w, http.StatusNotFound, "agent not found")
		return
	}

	// Use stored conversation_id if set, otherwise use agent ID
	conversationID := agent.ConversationID
	if conversationID == "" {
		conversationID = agentID
	}

	// Open context store
	store, err := contextvar.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open context store")
		httpError(w, http.StatusInternalServerError, "failed to open context store")
		return
	}
	defer store.Close()

	// Open companion memory database
	dbPath := filepath.Join(cfg.Storage.Root, "companion.db")
	memoryDB, closeFn, err := sqliteutil.OpenDBShared(r.Context(), dbPath, nil)
	if err != nil {
		log.Error().Err(err).Msg("failed to open companion memory database")
		httpError(w, http.StatusInternalServerError, "failed to open memory database")
		return
	}
	defer func() { _ = closeFn() }()

	// Create companion service with memory enabled
	svc := companion.NewService(store, companion.ServiceConfig{
		Logger:   log,
		MemoryDB: memoryDB,
	})

	// Use stored conversation_id if set, otherwise agent ID - this is where the daemon agent reads from
	resp, err := svc.Chat(r.Context(), companion.ChatRequest{
		ConversationID: conversationID,
		Message:        req.Message,
	})
	if err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Msg("companion chat failed")
		httpError(w, http.StatusInternalServerError, "chat failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, AgentAskResponse{
		Reply:          resp.Response,
		ConversationID: conversationID,
	})
}

// handleAgentTrash soft-deletes the agent identified by agentID if it is stopped.
//
// On success it responds with HTTP 200 and a JSON payload containing "status":"trashed"
// and "agent_id". It responds with HTTP 404 if the agent does not exist, HTTP 400
// if the agent is not stopped, and HTTP 500 for internal errors while accessing
// or modifying the agents store.
func handleAgentTrash(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, agentID string) {
	// Open agents store
	store, err := agents.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open agents store")
		httpError(w, http.StatusInternalServerError, "failed to open agents store")
		return
	}
	defer store.Close()

	// Attempt to trash the agent (only works for stopped agents)
	err = store.Trash(r.Context(), agentID)
	if err != nil {
		if errors.Is(err, agents.ErrNotFound) {
			httpError(w, http.StatusNotFound, "agent not found")
			return
		}
		if errors.Is(err, agents.ErrNotStopped) {
			httpError(w, http.StatusBadRequest, "agent must be stopped before it can be trashed")
			return
		}
		log.Error().Err(err).Str("agent_id", agentID).Msg("failed to trash agent")
		httpError(w, http.StatusInternalServerError, "failed to trash agent")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":   "trashed",
		"agent_id": agentID,
	})
}

// AgentPatchRequest is the request body for PATCH /api/agents/{id}.
type AgentPatchRequest struct {
	ConversationID *string `json:"conversation_id,omitempty"` // Pointer to distinguish null/missing
}

// handleAgentPatch handles PATCH requests that update an agent's mutable fields.
// It decodes an AgentPatchRequest and, if ConversationID is provided, updates the agent's conversation_id in storage,
// then responds with the updated AgentResponse JSON.
// Returns HTTP 400 for invalid request bodies, 404 if the agent does not exist, and 500 for storage or update failures.
func handleAgentPatch(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, agentID string) {
	// Parse request body
	var req AgentPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body")
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

	// Get agent first to verify it exists
	agent, err := store.Get(r.Context(), agentID)
	if err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Msg("agent not found")
		httpError(w, http.StatusNotFound, "agent not found")
		return
	}

	// Update conversation_id if provided
	if req.ConversationID != nil {
		if err := store.UpdateConversationID(r.Context(), agentID, *req.ConversationID); err != nil {
			log.Error().Err(err).Str("agent_id", agentID).Msg("failed to update conversation_id")
			httpError(w, http.StatusInternalServerError, "failed to update agent")
			return
		}
		agent.ConversationID = *req.ConversationID
	}

	// Return updated agent
	ar := AgentResponse{
		ID:             agent.ID,
		ParentID:       agent.ParentID,
		Namespace:      agent.Namespace,
		Name:           agent.Name,
		Slug:           agent.Slug,
		Role:           agent.Role,
		SkillsAllow:    agent.SkillsAllow,
		ShareBB:        agent.ShareBB,
		State:          string(agent.State),
		LLMProvider:    agent.LLMProvider,
		LLMModel:       agent.LLMModel,
		ConversationID: agent.ConversationID,
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
