package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"

	agentprompts "github.com/joshka0/foxctl/internal/agent/prompts"
	"github.com/joshka0/foxctl/internal/context/companion"
	agenttypes "github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	llmproviders "github.com/joshka0/foxctl/internal/providers/llm"
	"github.com/joshka0/foxctl/internal/runtime/daemon"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/agents"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
	"github.com/joshka0/foxctl/internal/storage/contextvar"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	memorystore "github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/joshka0/foxctl/internal/storage/sessions"
	coreworker "github.com/joshka0/foxctl/internal/v2/core/worker"
)

// AgentResponse represents an agent in API responses.
type AgentResponse struct {
	ID              string   `json:"id"`
	ParentID        string   `json:"parent_id,omitempty"`
	Namespace       string   `json:"ns"`
	WorkspaceRoot   string   `json:"workspace_root,omitempty"`
	WorkspaceSource string   `json:"workspace_source,omitempty"`
	Name            string   `json:"name,omitempty"` // Human name (e.g., "Luna", "Atlas")
	Slug            string   `json:"slug,omitempty"` // Human-readable handle (e.g., "researcher")
	Role            string   `json:"role,omitempty"`
	PromptSummary   string   `json:"prompt_summary,omitempty"`
	SkillsAllow     []string `json:"skills_allow"`
	ShareBB         string   `json:"share_bb"`
	State           string   `json:"state"`
	CreatedAt       string   `json:"created_at"`
	HeartbeatAt     string   `json:"heartbeat_at,omitempty"`
	LLMProvider     string   `json:"llm_provider,omitempty"`
	LLMModel        string   `json:"llm_model,omitempty"`
	LLMBaseURL      string   `json:"llm_base_url,omitempty"`
	LLMAuthMode     string   `json:"llm_auth_mode,omitempty"`
	LLMAuthHeader   string   `json:"llm_auth_header,omitempty"`
	LLMAuthPrefix   string   `json:"llm_auth_prefix,omitempty"`
	ExecMode        string   `json:"exec_mode,omitempty"`        // reactive|autonomous|proactive|tick|story
	ThinkInterval   int      `json:"think_interval,omitempty"`   // Seconds between proactive/tick cycles
	ConversationID  string   `json:"conversation_id,omitempty"`  // Linked companion conversation ID
	MemoryScope     string   `json:"memory_scope,omitempty"`     // agent|session
	MemoryRetention string   `json:"memory_retention,omitempty"` // companion|durable|task|ephemeral
	SandboxProvider string   `json:"sandbox_provider,omitempty"`
	SandboxID       string   `json:"sandbox_id,omitempty"`
	RepoURL         string   `json:"repo_url,omitempty"`
	RepoRef         string   `json:"repo_ref,omitempty"`
}

// AgentSpawnRequest is the request body for spawning a new agent.
type AgentSpawnRequest struct {
	Role            string   `json:"role"`
	Prompt          string   `json:"prompt"`
	WorkspaceID     string   `json:"workspace_id,omitempty"`
	WorkspaceRoot   string   `json:"workspace_root,omitempty"`
	WorkspaceSource string   `json:"workspace_source,omitempty"`
	SandboxProvider string   `json:"sandbox_provider,omitempty"`
	SandboxID       string   `json:"sandbox_id,omitempty"`
	RepoURL         string   `json:"repo_url,omitempty"`
	RepoRef         string   `json:"repo_ref,omitempty"`
	SandboxImage    string   `json:"sandbox_image,omitempty"`
	SandboxTimeoutS int      `json:"sandbox_timeout_s,omitempty"`
	AllowEgress     []string `json:"allow_egress,omitempty"`
	SkillsAllow     []string `json:"skills_allow,omitempty"`
	ParentID        string   `json:"parent_id,omitempty"`
	MemoryScope     string   `json:"memory_scope,omitempty"`
	MemoryRetention string   `json:"memory_retention,omitempty"`
	RoomID          string   `json:"room_id,omitempty"`
	RoomRole        string   `json:"room_role,omitempty"`

	// Agent metadata
	Name string `json:"name,omitempty"` // Human name (auto-generated if empty)
	Slug string `json:"slug,omitempty"` // Human-readable handle

	// Execution config
	ExecMode         string `json:"exec_mode,omitempty"`          // "reactive", "autonomous", "proactive", "tick", "story"
	ThinkInterval    int    `json:"think_interval,omitempty"`     // Seconds between proactive/tick cycles
	MaxIterations    int    `json:"max_iterations,omitempty"`     // Max tool calls per turn
	MaxContextTokens int    `json:"max_context_tokens,omitempty"` // Context budget (0=no limit)
	MaxAutoTurns     int    `json:"max_auto_turns,omitempty"`     // Max autonomous continuations

	// LLM override
	LLMProvider   string `json:"llm_provider,omitempty"`
	LLMModel      string `json:"llm_model,omitempty"`
	LLMBaseURL    string `json:"llm_base_url,omitempty"`
	LLMAuthMode   string `json:"llm_auth_mode,omitempty"`
	LLMAuthHeader string `json:"llm_auth_header,omitempty"`
	LLMAuthPrefix string `json:"llm_auth_prefix,omitempty"`
}

// AgentSpawnResponse is the response for spawning a new agent.
type AgentSpawnResponse struct {
	SessionID       string `json:"session_id"`
	ActorID         string `json:"actor_id"`
	Status          string `json:"status"`
	Name            string `json:"name,omitempty"` // Generated or provided name
	WorkspaceID     string `json:"workspace_id,omitempty"`
	WorkspaceRoot   string `json:"workspace_root,omitempty"`
	WorkspaceSource string `json:"workspace_source,omitempty"`
	SandboxProvider string `json:"sandbox_provider,omitempty"`
	SandboxID       string `json:"sandbox_id,omitempty"`
	RepoURL         string `json:"repo_url,omitempty"`
	RepoRef         string `json:"repo_ref,omitempty"`
}

type agentEventPublisher interface {
	Publish(eventType string, data any)
}

type AgentAskStreamRequest struct {
	Message        string          `json:"message"`
	CorrelationID  string          `json:"correlation_id,omitempty"`
	ConversationID string          `json:"conversation_id,omitempty"`
	Context        map[string]any  `json:"context,omitempty"`
	ResponseSchema json.RawMessage `json:"response_schema,omitempty"`
	ResponseKeys   []string        `json:"response_keys,omitempty"`
}

type AgentAskStreamResponse struct {
	Accepted       bool   `json:"accepted"`
	AgentID        string `json:"agent_id"`
	CorrelationID  string `json:"correlation_id"`
	ConversationID string `json:"conversation_id"`
}

type AgentAskStreamCancelRequest struct {
	CorrelationID string `json:"correlation_id,omitempty"`
}

type AgentAskStreamCancelResponse struct {
	OK            bool   `json:"ok"`
	AgentID       string `json:"agent_id"`
	CorrelationID string `json:"correlation_id,omitempty"`
	Cancelled     int    `json:"cancelled"`
}

type agentChatEvent struct {
	AgentID        string         `json:"agent_id"`
	ConversationID string         `json:"conversation_id"`
	CorrelationID  string         `json:"correlation_id"`
	Phase          string         `json:"phase"`
	Content        string         `json:"content,omitempty"`
	ContentDelta   string         `json:"content_delta,omitempty"`
	ToolName       string         `json:"tool_name,omitempty"`
	ToolCallID     string         `json:"tool_call_id,omitempty"`
	ToolArguments  any            `json:"tool_arguments,omitempty"`
	ToolOutput     string         `json:"tool_output,omitempty"`
	ContextQueries int            `json:"context_queries,omitempty"`
	Error          string         `json:"error,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type agentRuntimeTreeData struct {
	Enabled bool                  `json:"enabled"`
	AgentID string                `json:"agent_id,omitempty"`
	Depth   int                   `json:"depth"`
	Root    *agentRuntimeTreeNode `json:"root,omitempty"`
	Error   string                `json:"error,omitempty"`
}

type agentRuntimeTreeNode struct {
	Tag      string                  `json:"tag,omitempty"`
	AgentID  string                  `json:"agent_id,omitempty"`
	PID      string                  `json:"pid,omitempty"`
	Metadata map[string]any          `json:"metadata,omitempty"`
	Status   string                  `json:"status,omitempty"`
	State    any                     `json:"state,omitempty"`
	Error    string                  `json:"error,omitempty"`
	Children []*agentRuntimeTreeNode `json:"children,omitempty"`
}

type agentStreamRegistry struct {
	mu       sync.Mutex
	inflight map[string]map[string]context.CancelFunc
}

func newAgentStreamRegistry() *agentStreamRegistry {
	return &agentStreamRegistry{inflight: make(map[string]map[string]context.CancelFunc)}
}

type preparedSandboxSpawn struct {
	workspaceID     string
	workspaceRoot   string
	workspaceSource string
	sandboxProvider string
	sandboxID       string
	repoURL         string
	repoRef         string
	cleanup         func(context.Context)
	release         func()
}

func (r *agentStreamRegistry) Put(agentID, correlationID string, cancel context.CancelFunc) {
	if cancel == nil || strings.TrimSpace(agentID) == "" || strings.TrimSpace(correlationID) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inflight[agentID] == nil {
		r.inflight[agentID] = make(map[string]context.CancelFunc)
	}
	r.inflight[agentID][correlationID] = cancel
}

func (r *agentStreamRegistry) Delete(agentID, correlationID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	children, ok := r.inflight[agentID]
	if !ok {
		return
	}
	delete(children, correlationID)
	if len(children) == 0 {
		delete(r.inflight, agentID)
	}
}

func (r *agentStreamRegistry) Cancel(agentID, correlationID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	children, ok := r.inflight[agentID]
	if !ok {
		return 0
	}
	cancelled := 0
	if strings.TrimSpace(correlationID) != "" {
		if cancel, ok := children[correlationID]; ok {
			cancel()
			delete(children, correlationID)
			cancelled = 1
		}
	} else {
		for key, cancel := range children {
			cancel()
			delete(children, key)
			cancelled++
		}
	}
	if len(children) == 0 {
		delete(r.inflight, agentID)
	}
	return cancelled
}

var activeAgentStreams = newAgentStreamRegistry()

func summarizePrompt(prompt string, maxLen int) string {
	prompt = strings.TrimSpace(prompt)
	if maxLen <= 0 || prompt == "" {
		return ""
	}
	runes := []rune(prompt)
	if len(runes) <= maxLen {
		return prompt
	}
	return string(runes[:maxLen]) + "..."
}

func normalizeSpawnMemoryScope(req AgentSpawnRequest) agenttypes.MemoryScope {
	scope := agenttypes.NormalizeMemoryScope(agenttypes.MemoryScope(strings.TrimSpace(req.MemoryScope)))
	if strings.TrimSpace(req.MemoryScope) != "" {
		return scope
	}
	retention := strings.TrimSpace(req.MemoryRetention)
	if retention != "" {
		return agenttypes.RecommendedMemoryScopeForRetention(agenttypes.MemoryRetention(retention))
	}
	if strings.EqualFold(strings.TrimSpace(req.Role), "companion") {
		return agenttypes.MemoryScopeAgent
	}
	if strings.TrimSpace(req.ParentID) != "" {
		return agenttypes.MemoryScopeSession
	}
	return scope
}

func normalizeSpawnMemoryRetention(req AgentSpawnRequest) agenttypes.MemoryRetention {
	if trimmed := strings.TrimSpace(req.MemoryRetention); trimmed != "" {
		return agenttypes.NormalizeMemoryRetention(agenttypes.MemoryRetention(trimmed))
	}
	if strings.EqualFold(strings.TrimSpace(req.Role), "companion") {
		return agenttypes.MemoryRetentionCompanion
	}
	scope := normalizeSpawnMemoryScope(req)
	return agenttypes.DefaultMemoryRetentionForScope(scope)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
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
				ID:              a.ID,
				ParentID:        a.ParentID,
				Namespace:       a.Namespace,
				WorkspaceRoot:   a.WorkspaceRoot,
				WorkspaceSource: a.WorkspaceSource,
				Name:            a.Name,
				Slug:            a.Slug,
				Role:            a.Role,
				PromptSummary:   summarizePrompt(a.Prompt, 100),
				SkillsAllow:     a.SkillsAllow,
				ShareBB:         a.ShareBB,
				State:           string(a.State),
				LLMProvider:     a.LLMProvider,
				LLMModel:        a.LLMModel,
				LLMBaseURL:      a.LLMBaseURL,
				LLMAuthMode:     a.LLMAuthMode,
				LLMAuthHeader:   a.LLMAuthHeader,
				LLMAuthPrefix:   a.LLMAuthPrefix,
				ExecMode:        string(a.ExecMode),
				ThinkInterval:   a.ThinkInterval,
				ConversationID:  a.ConversationID,
				MemoryScope:     string(agenttypes.NormalizeMemoryScope(a.MemoryScope)),
				MemoryRetention: string(agenttypes.NormalizeMemoryRetention(a.MemoryRetention)),
				SandboxProvider: a.SandboxProvider,
				SandboxID:       a.SandboxID,
				RepoURL:         a.RepoURL,
				RepoRef:         a.RepoRef,
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
// Returns standard HTTP errors for unknown daemon actions, missing agent IDs,
// unsupported methods, and internal failures.
func AgentDetailHandler(cfg config.Config, log zerolog.Logger, events agentEventPublisher) http.HandlerFunc {
	return AgentDetailHandlerWithRuntime(cfg, log, events, nil)
}

func AgentDetailHandlerWithRuntime(cfg config.Config, log zerolog.Logger, events agentEventPublisher, runtimeHost OrchestrationRuntimeHost) http.HandlerFunc {
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
			httpError(w, http.StatusNotFound, "not found")
			return
		}

		remaining := strings.TrimPrefix(path, prefix)
		parts := strings.SplitN(remaining, "/", 4)
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
				handleAgentDaemonKillWithRuntime(w, r, cfg, log, agentID, runtimeHost)
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

		if len(parts) >= 3 && parts[1] == "ask-stream" && parts[2] == "cancel" {
			handleAgentAskStreamCancel(w, r, log, agentID)
			return
		}

		if len(parts) >= 2 && parts[1] == "ask-stream" {
			handleAgentAskStream(w, r, cfg, log, events, agentID)
			return
		}

		if len(parts) >= 2 && parts[1] == "runtime" {
			if len(parts) >= 4 && parts[2] == "logs" && parts[3] == "stream" {
				handleAgentRuntimeLogsStream(w, r, cfg, log, agentID)
				return
			}
			if len(parts) >= 3 && parts[2] == "logs" {
				handleAgentRuntimeLogsGet(w, r, cfg, log, agentID)
				return
			}
			handleAgentRuntimeGet(w, r, cfg, log, agentID)
			return
		}

		if len(parts) >= 3 && parts[1] == "memory" {
			switch parts[2] {
			case "compress":
				handleAgentMemoryCompress(w, r, cfg, log, agentID)
			case "stats":
				handleAgentMemoryStats(w, r, cfg, log, agentID)
			case "context":
				handleAgentMemoryContext(w, r, cfg, log, agentID)
			case "search":
				handleAgentMemorySearch(w, r, cfg, log, agentID)
			default:
				httpError(w, http.StatusNotFound, "unknown agent memory action")
			}
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
			ID:              agent.ID,
			ParentID:        agent.ParentID,
			Namespace:       agent.Namespace,
			WorkspaceRoot:   agent.WorkspaceRoot,
			WorkspaceSource: agent.WorkspaceSource,
			Name:            agent.Name,
			Slug:            agent.Slug,
			Role:            agent.Role,
			PromptSummary:   summarizePrompt(agent.Prompt, 100),
			SkillsAllow:     agent.SkillsAllow,
			ShareBB:         agent.ShareBB,
			State:           string(agent.State),
			LLMProvider:     agent.LLMProvider,
			LLMModel:        agent.LLMModel,
			LLMBaseURL:      agent.LLMBaseURL,
			LLMAuthMode:     agent.LLMAuthMode,
			LLMAuthHeader:   agent.LLMAuthHeader,
			LLMAuthPrefix:   agent.LLMAuthPrefix,
			ExecMode:        string(agent.ExecMode),
			ThinkInterval:   agent.ThinkInterval,
			ConversationID:  agent.ConversationID,
			MemoryScope:     string(agenttypes.NormalizeMemoryScope(agent.MemoryScope)),
			MemoryRetention: string(agenttypes.NormalizeMemoryRetention(agent.MemoryRetention)),
			SandboxProvider: agent.SandboxProvider,
			SandboxID:       agent.SandboxID,
			RepoURL:         agent.RepoURL,
			RepoRef:         agent.RepoRef,
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

func handleAgentDaemonStart(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, agentID string) {
	handleAgentDaemonStartWithRoute(w, r, cfg, log, agentID)
}

// handleAgentDaemonStartLegacy starts an agent via the daemon using the agent's stored configuration
// and an optional request body.
//
// It ensures the daemon is running, spawns a daemon session using the workspace and prompt
// from the request (falling back to the agent's namespace and prompt), updates the agent's
// state to running in the store, and writes an AgentDaemonStartResponse on success or an
// appropriate HTTP error on failure.
func handleAgentDaemonStartWithRoute(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, agentID string) {
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
	ctrl := agentControl()
	if err := ctrl.EnsureRunning(); err != nil {
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
		Role:            agent.Role,
		AgentID:         agentID, // Pass agent config ID for session filtering
		WorkspaceID:     workspace,
		Prompt:          prompt,
		SkillsAllow:     agent.SkillsAllow,
		MemoryScope:     string(agenttypes.NormalizeMemoryScope(agent.MemoryScope)),
		MemoryRetention: string(normalizeAgentMemoryRetention(agent)),
		Name:            agent.Name,
		Slug:            agent.Slug,
		ExecMode:        string(agent.ExecMode),
		ThinkInterval:   agent.ThinkInterval,
		MaxIterations:   agent.MaxIterations,
		MaxAutoTurns:    agent.MaxAutoTurns,
		LLMProvider:     agent.LLMProvider,
		LLMModel:        agent.LLMModel,
		LLMAPIKey:       agent.LLMAPIKey,
	}

	result, err := ctrl.Spawn(params)
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

func handleAgentDaemonSessions(w http.ResponseWriter, r *http.Request, log zerolog.Logger, agentID string) {
	handleAgentDaemonSessionsWithRoute(w, r, log, agentID)
}

// handleAgentDaemonSessionsLegacy handles GET requests to list active daemon sessions for the specified agentID.
// It ensures the daemon is running, filters daemon sessions by ActorID equal to agentID, and writes a JSON
// response with "sessions" (slice of session info) and "count" (number of sessions). It responds with 405 on
// non-GET methods, 503 if the daemon cannot be started, and 500 on internal listing errors.
func handleAgentDaemonSessionsWithRoute(w http.ResponseWriter, r *http.Request, log zerolog.Logger, agentID string) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Connect to daemon, auto-start if not running
	ctrl := agentControl()
	if err := ctrl.EnsureRunning(); err != nil {
		log.Error().Err(err).Msg("failed to start daemon")
		httpError(w, http.StatusServiceUnavailable, "failed to start daemon: "+err.Error())
		return
	}

	result, err := ctrl.List()
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

func handleAgentDaemonKillWithRuntime(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, agentID string, runtimeHost OrchestrationRuntimeHost) {
	handleAgentDaemonKillWithRoute(w, r, cfg, log, agentID, runtimeHost)
}

// handleAgentDaemonKillLegacy terminates a running daemon session for the given agent and ensures the agent's stored state is set to stopped.
//
// It handles POST requests and writes JSON HTTP responses describing the outcome. If the daemon is not running or no matching session is found,
// the handler updates the agent state to "stopped" and returns an OK payload indicating no active session; on successful termination it returns
// the killed session ID and status. Errors are reported via appropriate HTTP error responses.
func handleAgentDaemonKillWithRoute(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, agentID string, runtimeHost OrchestrationRuntimeHost) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	store, err := agents.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open agents store")
		httpError(w, http.StatusInternalServerError, "failed to open agents store")
		return
	}
	defer store.Close()

	if _, err := store.Get(r.Context(), agentID); err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Msg("agent not found")
		httpError(w, http.StatusNotFound, "agent not found")
		return
	}

	if runtimeHost != nil && strings.EqualFold(ResolveOrchestrationRuntimeBackend(), orchestrationRuntimeBackendGoruntimeAPI) {
		signalResp, signalErr := runtimeHost.Signal(r.Context(), coreworker.SignalRequest{
			AgentID:   agentID,
			RequestID: "kill:" + agentID,
			Signal:    "terminate",
			Reason:    "foxctl web kill",
		})
		if signalErr == nil {
			if err := store.UpdateState(r.Context(), agentID, agenttypes.StateStopped); err != nil {
				log.Error().Err(err).Str("agent_id", agentID).Msg("failed to update agent state after runtime signal")
				httpError(w, http.StatusInternalServerError, "agent signaled but failed to update state")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":         true,
				"session_id": "",
				"status":     string(signalResp.Status),
				"message":    "agent runtime signaled",
			})
			return
		}
	}

	// Connect to daemon
	ctrl := agentControl()
	if !ctrl.IsRunning() {
		// Daemon not running - just update agent state to stopped
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
	listResult, err := ctrl.List()
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
	result, err := ctrl.Kill(sessionID)
	if err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Str("session_id", sessionID).Msg("failed to kill agent session")
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Update agent state to stopped in store
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

func handleAgentSpawn(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger) {
	handleAgentSpawnWithRoute(w, r, cfg, log)
}

// handleAgentSpawnLegacy creates a new agent record, launches it via the daemon, and responds with the spawn result.
//
// It only accepts POST requests with a JSON AgentSpawnRequest containing at minimum `role` and `prompt`.
// If no name is provided one is generated; a new ULID is assigned as the agent ID. The workspace is normalized
// (defaults to "default") and the execution mode is validated (must be reactive, autonomous, proactive, or story).
// The handler ensures the daemon is running, persists an agent record in the agents store with an initial state,
// requests the daemon to spawn the agent, updates the agent state to running on success, and returns an
// AgentSpawnResponse containing the session ID, actor ID (the persisted agent ID), status, and name.
// On validation, store, daemon, or spawn failures it writes an appropriate HTTP error response.
func handleAgentSpawnWithRoute(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger) {
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

	workspaceRoot := strings.TrimSpace(req.WorkspaceRoot)
	if workspaceRoot != "" {
		cleanRoot := filepath.Clean(workspaceRoot)
		if !filepath.IsAbs(cleanRoot) {
			httpError(w, http.StatusBadRequest, "workspace_root must be absolute")
			return
		}
		workspaceRoot = cleanRoot
	}
	workspaceSource := strings.TrimSpace(req.WorkspaceSource)

	// Connect to daemon, auto-start if not running
	ctrl := agentControl()
	if err := ctrl.EnsureRunning(); err != nil {
		log.Error().Err(err).Msg("failed to start daemon")
		httpError(w, http.StatusServiceUnavailable, "failed to start daemon: "+err.Error())
		return
	}

	preparedSandbox, err := prepareSandboxBackedSpawn(r.Context(), req)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if preparedSandbox != nil {
		defer preparedSandbox.cleanup(context.Background())
		if strings.TrimSpace(req.WorkspaceID) == "" {
			req.WorkspaceID = preparedSandbox.workspaceID
		}
		workspaceRoot = preparedSandbox.workspaceRoot
		workspaceSource = preparedSandbox.workspaceSource
		req.SandboxProvider = preparedSandbox.sandboxProvider
		req.SandboxID = preparedSandbox.sandboxID
		req.RepoURL = preparedSandbox.repoURL
		req.RepoRef = preparedSandbox.repoRef
	}

	namespace := strings.TrimSpace(req.WorkspaceID)
	if namespace == "" {
		if workspaceRoot != "" {
			namespace = workspace.CanonicalID(workspaceRoot)
		} else {
			namespace = "default"
		}
	} else {
		namespace = workspace.Normalize(namespace)
	}

	resolvedPrompt := resolveAgentSpawnPrompt(req, namespace)
	if resolvedPrompt == "" {
		httpError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	// Generate name if not provided
	name := req.Name
	if name == "" {
		name = agenttypes.GenerateAgentName(rand.New(rand.NewSource(time.Now().UnixNano())))
	}

	// Generate agent ID
	agentID := ulid.Make().String()

	// Determine and validate exec mode
	execMode := agenttypes.ExecutionMode(req.ExecMode)
	if execMode == "" {
		execMode = agenttypes.ModeReactive
	} else {
		switch execMode {
		case agenttypes.ModeReactive, agenttypes.ModeAutonomous, agenttypes.ModeProactive, agenttypes.ModeTick, agenttypes.ModeStory:
			// valid
		default:
			httpError(w, http.StatusBadRequest, "invalid exec_mode: must be reactive, autonomous, proactive, tick, or story")
			return
		}
	}
	if execMode == agenttypes.ModeTick {
		req.LLMProvider = "lmstudio"
		if strings.TrimSpace(req.LLMModel) == "" {
			req.LLMModel = llmproviders.DefaultModelForProvider("lmstudio")
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
		ID:              agentID,
		ParentID:        strings.TrimSpace(req.ParentID),
		Namespace:       namespace,
		WorkspaceRoot:   workspaceRoot,
		WorkspaceSource: workspaceSource,
		Name:            name,
		Slug:            req.Slug,
		Role:            req.Role,
		Prompt:          resolvedPrompt,
		SkillsAllow:     req.SkillsAllow,
		Policy:          agenttypes.Policy{},
		ShareBB:         "scoped",
		State:           agenttypes.StateStarting,
		CreatedAt:       time.Now().UTC(),
		LLMProvider:     req.LLMProvider,
		LLMModel:        req.LLMModel,
		LLMBaseURL:      req.LLMBaseURL,
		LLMAuthMode:     req.LLMAuthMode,
		LLMAuthHeader:   req.LLMAuthHeader,
		LLMAuthPrefix:   req.LLMAuthPrefix,
		ExecMode:        execMode,
		ThinkInterval:   req.ThinkInterval,
		MaxIterations:   req.MaxIterations,
		MaxAutoTurns:    req.MaxAutoTurns,
		MemoryScope:     normalizeSpawnMemoryScope(req),
		MemoryRetention: normalizeSpawnMemoryRetention(req),
		SandboxProvider: strings.TrimSpace(req.SandboxProvider),
		SandboxID:       strings.TrimSpace(req.SandboxID),
		RepoURL:         strings.TrimSpace(req.RepoURL),
		RepoRef:         strings.TrimSpace(req.RepoRef),
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
		Role:        req.Role,
		AgentID:     agentID,   // Pass agent ID for linking
		WorkspaceID: namespace, // Use normalized workspace
		WorkspaceRoot: func() string {
			// Sandbox-backed agents currently persist their sandbox workspace
			// metadata but still execute in the local runtime until the
			// runtime-in-sandbox path is wired.
			if workspaceSource == "sandbox" {
				return ""
			}
			return workspaceRoot
		}(),
		Prompt:           resolvedPrompt,
		SkillsAllow:      req.SkillsAllow,
		MemoryScope:      string(normalizeSpawnMemoryScope(req)),
		MemoryRetention:  string(normalizeSpawnMemoryRetention(req)),
		Name:             name,
		Slug:             req.Slug,
		ExecMode:         string(execMode), // Use validated exec mode
		ThinkInterval:    req.ThinkInterval,
		MaxIterations:    req.MaxIterations,
		MaxContextTokens: req.MaxContextTokens,
		MaxAutoTurns:     req.MaxAutoTurns,
		LLMProvider:      req.LLMProvider,
		LLMModel:         req.LLMModel,
		LLMBaseURL:       req.LLMBaseURL,
		LLMAuthMode:      req.LLMAuthMode,
		LLMAuthHeader:    req.LLMAuthHeader,
		LLMAuthPrefix:    req.LLMAuthPrefix,
	}

	result, err := ctrl.Spawn(params)
	if err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Msg("failed to spawn agent")
		// Update agent state to error since spawn failed
		_ = store.UpdateState(r.Context(), agentID, agenttypes.StateError)
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Sync persisted state after a successful daemon spawn. This is best-effort:
	// the spawn already succeeded, so transient SQLite lock contention should not
	// turn a live agent into an HTTP 500 for the caller.
	if err := updateAgentStateAfterSpawn(r.Context(), store, agentID); err != nil {
		log.Warn().Err(err).Str("agent_id", agentID).Msg("failed to update agent state to running after spawn")
	}

	if roomID := strings.TrimSpace(req.RoomID); roomID != "" {
		if err := attachSpawnedAgentToRoom(r.Context(), cfg, namespace, roomID, agentID, strings.TrimSpace(req.Role), strings.TrimSpace(req.RoomRole)); err != nil {
			log.Warn().Err(err).Str("agent_id", agentID).Str("room_id", roomID).Msg("failed to attach spawned agent to room")
		}
	}

	writeJSON(w, http.StatusOK, AgentSpawnResponse{
		SessionID:       result.SessionID,
		ActorID:         agentID, // Return our persisted agent ID, not the daemon's actor ID
		Status:          result.Status,
		Name:            name,
		WorkspaceID:     namespace,
		WorkspaceRoot:   workspaceRoot,
		WorkspaceSource: workspaceSource,
		SandboxProvider: strings.TrimSpace(req.SandboxProvider),
		SandboxID:       strings.TrimSpace(req.SandboxID),
		RepoURL:         strings.TrimSpace(req.RepoURL),
		RepoRef:         strings.TrimSpace(req.RepoRef),
	})
	if preparedSandbox != nil && preparedSandbox.release != nil {
		preparedSandbox.release()
	}
}

func prepareSandboxBackedSpawn(ctx context.Context, req AgentSpawnRequest) (*preparedSandboxSpawn, error) {
	provider := strings.TrimSpace(req.SandboxProvider)
	if provider == "" {
		return nil, nil
	}
	return nil, fmt.Errorf("sandbox_provider %q is temporarily disabled", provider)
}

func updateAgentStateAfterSpawn(ctx context.Context, store agents.Store, agentID string) error {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		lastErr = store.UpdateState(ctx, agentID, agenttypes.StateRunning)
		if lastErr == nil {
			return nil
		}
		if !isSQLiteBusyError(lastErr) {
			return lastErr
		}
		if attempt < 4 {
			time.Sleep(100 * time.Millisecond)
		}
	}
	return lastErr
}

func isSQLiteBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "sqlite_busy")
}

func resolveAgentSpawnPrompt(req AgentSpawnRequest, workspaceID string) string {
	roleForPrompt := chooseNonEmpty(strings.TrimSpace(req.RoomRole), strings.TrimSpace(req.Role))
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		if defaultPrompt, ok := agentprompts.DefaultPrompt(roleForPrompt); ok {
			prompt = defaultPrompt
		}
	}
	if prompt == "" && !strings.EqualFold(roleForPrompt, strings.TrimSpace(req.Role)) {
		if defaultPrompt, ok := agentprompts.DefaultPrompt(strings.TrimSpace(req.Role)); ok {
			prompt = defaultPrompt
		}
	}
	return agentprompts.ComposeRoomAwarePrompt(prompt, agentprompts.RoomOnboardingOptions{
		RoomID:      strings.TrimSpace(req.RoomID),
		WorkspaceID: strings.TrimSpace(workspaceID),
		Role:        strings.TrimSpace(req.Role),
		RoomRole:    strings.TrimSpace(req.RoomRole),
	})
}

func attachSpawnedAgentToRoom(ctx context.Context, cfg config.Config, workspaceID, roomID, agentID, role, roomRole string) error {
	store, err := blackboard.OpenBoardStore(ctx, cfg.Storage.Root)
	if err != nil {
		return fmt.Errorf("open board store: %w", err)
	}
	defer store.Close()

	if _, err := store.EnsureRoom(ctx, workspaceID, roomID, roomID); err != nil {
		return fmt.Errorf("ensure room: %w", err)
	}
	room, err := store.GetRoom(ctx, workspaceID, roomID, "")
	if err != nil {
		return fmt.Errorf("get room: %w", err)
	}

	nextMembers := make([]agenttypes.RoomMember, 0, len(room.Members)+1)
	seen := make(map[string]struct{}, len(room.Members)+1)
	for _, member := range room.Members {
		if member.ActorID == "" {
			continue
		}
		if _, ok := seen[member.ActorID]; ok {
			continue
		}
		seen[member.ActorID] = struct{}{}
		if member.ActorID == agentID {
			member.Role = chooseNonEmpty(strings.TrimSpace(roomRole), member.Role)
		}
		nextMembers = append(nextMembers, member)
	}
	if _, ok := seen[agentID]; !ok {
		nextMembers = append(nextMembers, agenttypes.RoomMember{
			ActorID: agentID,
			Role:    chooseNonEmpty(strings.TrimSpace(roomRole), strings.TrimSpace(role)),
		})
	}

	if _, err := store.ReplaceRoomMembers(ctx, workspaceID, roomID, nextMembers); err != nil {
		return fmt.Errorf("replace room members: %w", err)
	}
	subject, body := agentprompts.RoomOnboardingMessage(agentprompts.RoomOnboardingOptions{
		RoomID:      roomID,
		WorkspaceID: workspaceID,
		Role:        role,
		RoomRole:    roomRole,
	})
	if strings.TrimSpace(body) != "" {
		msg := &agenttypes.BoardMessage{
			WorkspaceID: workspaceID,
			Stream:      agenttypes.RoomStreamName(roomID),
			Sender:      "actor:system:room:" + roomID,
			Recipient:   agentID,
			Subject:     subject,
			Body:        body,
			Kind:        "info",
			Priority:    agenttypes.DefaultPriority,
			Status:      agenttypes.BoardMessageStatusUnread,
			CreatedAt:   time.Now().UTC(),
		}
		if err := store.SendMessage(ctx, msg); err != nil {
			return fmt.Errorf("send onboarding message: %w", err)
		}
	}
	return nil
}

// AgentAskRequest is the request body for POST /api/agents/{id}/ask.
type AgentAskRequest struct {
	Message        string          `json:"message"`
	ResponseSchema json.RawMessage `json:"response_schema,omitempty"`
	ResponseKeys   []string        `json:"response_keys,omitempty"`
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

	if isSandboxBackedAgent(agent) {
		httpError(w, http.StatusServiceUnavailable, "sandbox-backed agent execution is temporarily disabled")
		return
	}

	conversationID := resolveAgentConversationID(agent, "")
	svc, cleanup, err := buildAgentCompanionService(r.Context(), cfg, log, agent)
	if err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Msg("failed to initialize companion service")
		httpError(w, http.StatusInternalServerError, "failed to initialize companion service")
		return
	}
	defer cleanup()

	// Use stored conversation_id if set, otherwise agent ID - this is where the daemon agent reads from
	resp, err := svc.Chat(r.Context(), companion.ChatRequest{
		ConversationID: conversationID,
		Message:        req.Message,
		ResponseSchema: req.ResponseSchema,
		ResponseKeys:   req.ResponseKeys,
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

func handleAgentAskStream(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, events agentEventPublisher, agentID string) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if events == nil {
		httpError(w, http.StatusServiceUnavailable, "streaming events not configured")
		return
	}

	var req AgentAskStreamRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		httpError(w, http.StatusBadRequest, "message is required")
		return
	}

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

	correlationID := strings.TrimSpace(req.CorrelationID)
	if correlationID == "" {
		correlationID = ulid.Make().String()
	}
	conversationID := resolveAgentConversationID(agent, req.ConversationID)
	timeout := agentAskStreamTimeout(agent)

	if isSandboxBackedAgent(agent) {
		httpError(w, http.StatusServiceUnavailable, "sandbox-backed agent execution is temporarily disabled")
		return
	}

	svc, cleanup, err := buildAgentCompanionService(r.Context(), cfg, log, agent)
	if err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Msg("failed to initialize companion service")
		httpError(w, http.StatusInternalServerError, "failed to initialize companion service")
		return
	}

	writeJSON(w, http.StatusAccepted, AgentAskStreamResponse{
		Accepted:       true,
		AgentID:        agentID,
		CorrelationID:  correlationID,
		ConversationID: conversationID,
	})

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	activeAgentStreams.Put(agentID, correlationID, cancel)

	publishAgentChatEvent(events, agentChatEvent{
		AgentID:        agentID,
		ConversationID: conversationID,
		CorrelationID:  correlationID,
		Phase:          "started",
		Metadata: map[string]any{
			"memory_scope":     string(agenttypes.NormalizeMemoryScope(agent.MemoryScope)),
			"memory_retention": string(agenttypes.NormalizeMemoryRetention(agent.MemoryRetention)),
		},
	})

	go func() {
		defer cleanup()
		defer activeAgentStreams.Delete(agentID, correlationID)
		defer cancel()

		resp, err := svc.ChatStreaming(ctx, companion.ChatRequest{
			ConversationID: conversationID,
			Message:        req.Message,
			Context:        req.Context,
			ResponseSchema: req.ResponseSchema,
			ResponseKeys:   req.ResponseKeys,
		}, companion.ChatStreamCallbacks{
			OnDelta: func(delta companion.ChatStreamDelta) {
				if strings.TrimSpace(delta.ContentDelta) == "" {
					return
				}
				publishAgentChatEvent(events, agentChatEvent{
					AgentID:        agentID,
					ConversationID: conversationID,
					CorrelationID:  correlationID,
					Phase:          "delta",
					ContentDelta:   delta.ContentDelta,
				})
			},
			OnToolCall: func(call companion.ChatToolCallEvent) {
				publishAgentChatEvent(events, agentChatEvent{
					AgentID:        agentID,
					ConversationID: conversationID,
					CorrelationID:  correlationID,
					Phase:          "tool_call",
					ToolCallID:     call.ID,
					ToolName:       call.Name,
					ToolArguments:  call.Arguments,
				})
			},
			OnToolResult: func(result companion.ChatToolResultEvent) {
				publishAgentChatEvent(events, agentChatEvent{
					AgentID:        agentID,
					ConversationID: conversationID,
					CorrelationID:  correlationID,
					Phase:          "tool_result",
					ToolCallID:     result.ToolCallID,
					ToolName:       result.Name,
					ToolOutput:     truncateAgentChatPayload(result.Content, 2048),
					Metadata: map[string]any{
						"is_error": result.IsError,
					},
				})
			},
		})
		if err != nil {
			if errors.Is(err, context.Canceled) {
				publishAgentChatEvent(events, agentChatEvent{
					AgentID:        agentID,
					ConversationID: conversationID,
					CorrelationID:  correlationID,
					Phase:          "cancelled",
					Error:          "cancelled",
				})
				return
			}
			publishAgentChatEvent(events, agentChatEvent{
				AgentID:        agentID,
				ConversationID: conversationID,
				CorrelationID:  correlationID,
				Phase:          "error",
				Error:          err.Error(),
			})
			return
		}

		publishAgentChatEvent(events, agentChatEvent{
			AgentID:        agentID,
			ConversationID: conversationID,
			CorrelationID:  correlationID,
			Phase:          "completed",
			Content:        resp.Response,
			ContextQueries: resp.ContextQueries,
		})
	}()
}

func agentAskStreamTimeout(agent agenttypes.Agent) time.Duration {
	timeout := 30 * time.Minute
	if agent.Policy.Timeout != "" {
		if d, err := time.ParseDuration(agent.Policy.Timeout); err == nil && d > 0 {
			timeout = d
		}
	}
	return timeout
}

func handleAgentAskStreamCancel(w http.ResponseWriter, r *http.Request, log zerolog.Logger, agentID string) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req AgentAskStreamCancelRequest
	_ = readJSON(w, r, &req)
	cancelled := activeAgentStreams.Cancel(agentID, strings.TrimSpace(req.CorrelationID))

	log.Info().
		Str("agent_id", agentID).
		Str("correlation_id", strings.TrimSpace(req.CorrelationID)).
		Int("cancelled", cancelled).
		Msg("agent stream cancel received via REST")

	writeJSON(w, http.StatusOK, AgentAskStreamCancelResponse{
		OK:            true,
		AgentID:       agentID,
		CorrelationID: strings.TrimSpace(req.CorrelationID),
		Cancelled:     cancelled,
	})
}

func handleAgentRuntimeGet(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, agentID string) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	depth := defaultRuntimeTreeDepth
	if rawDepth := strings.TrimSpace(r.URL.Query().Get("depth")); rawDepth != "" {
		parsed, err := strconv.Atoi(rawDepth)
		if err != nil || parsed < 0 {
			httpError(w, http.StatusBadRequest, "depth must be a non-negative integer")
			return
		}
		depth = parsed
	}
	if depth > maxRuntimeTreeDepth {
		depth = maxRuntimeTreeDepth
	}

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

	writeJSON(w, http.StatusOK, map[string]any{
		"runtime": loadAgentRuntimeTree(r.Context(), cfg, log, agent, depth),
	})
}

func handleAgentRuntimeLogsGet(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, agentID string) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	store, err := agents.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open agents store")
		httpError(w, http.StatusInternalServerError, "failed to open agents store")
		return
	}
	defer store.Close()

	if _, err := store.Get(r.Context(), agentID); err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Msg("agent not found")
		httpError(w, http.StatusNotFound, "agent not found")
		return
	}

	entries, found, err := loadAgentRuntimeLogEntries(r.Context(), cfg, agentID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		httpError(w, http.StatusNotFound, "runtime worker not found")
		return
	}
	limit := len(entries)
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		parsed, parseErr := strconv.Atoi(rawLimit)
		if parseErr != nil || parsed < 0 {
			httpError(w, http.StatusBadRequest, "limit must be a non-negative integer")
			return
		}
		if parsed > 0 && parsed < limit {
			entries = entries[len(entries)-parsed:]
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id": agentID,
		"entries":  entries,
		"count":    len(entries),
	})
}

func handleAgentRuntimeLogsStream(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, agentID string) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	store, err := agents.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open agents store")
		httpError(w, http.StatusInternalServerError, "failed to open agents store")
		return
	}
	defer store.Close()

	if _, err := store.Get(r.Context(), agentID); err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Msg("agent not found")
		httpError(w, http.StatusNotFound, "agent not found")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	format := strings.TrimSpace(r.URL.Query().Get("format"))
	poll := 250 * time.Millisecond
	if rawPoll := strings.TrimSpace(r.URL.Query().Get("poll_ms")); rawPoll != "" {
		if parsed, err := strconv.Atoi(rawPoll); err == nil && parsed >= 25 && parsed <= 5000 {
			poll = time.Duration(parsed) * time.Millisecond
		}
	}

	writeSSEEvent(w, "connected", map[string]any{
		"agent_id":  agentID,
		"timestamp": time.Now().UnixMilli(),
	}, format)
	flusher.Flush()

	lastSent := 0
	sendEntries := func(entries []map[string]any) {
		if len(entries) == 0 {
			return
		}
		writeSSEEvent(w, "runtime.logs", map[string]any{
			"agent_id": agentID,
			"entries":  entries,
			"count":    len(entries),
		}, format)
		flusher.Flush()
	}

	if entries, found, err := loadAgentRuntimeLogEntries(r.Context(), cfg, agentID); err == nil && found {
		sendEntries(entries)
		lastSent = len(entries)
	}

	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			writeSSEEvent(w, "heartbeat", map[string]any{
				"timestamp": time.Now().UnixMilli(),
			}, format)
			flusher.Flush()
		case <-ticker.C:
			entries, found, err := loadAgentRuntimeLogEntries(r.Context(), cfg, agentID)
			if err != nil || !found {
				continue
			}
			switch {
			case len(entries) > lastSent:
				sendEntries(entries[lastSent:])
			case len(entries) < lastSent:
				sendEntries(entries)
			default:
				continue
			}
			lastSent = len(entries)
		}
	}
}

func loadAgentRuntimeLogEntries(ctx context.Context, cfg config.Config, agentID string) ([]map[string]any, bool, error) {
	reader, closeFn, available, err := loadOptionalRuntimeStateReader(ctx, cfg)
	if err != nil {
		return nil, false, err
	}
	if closeFn != nil {
		defer func() { _ = closeFn() }()
	}
	if !available {
		return nil, true, nil
	}
	record, err := reader.Worker(ctx, coreworker.LookupRequest{AgentID: agentID})
	if err != nil {
		return nil, false, nil
	}
	return runtimeRecentLogs(record.RawState), true, nil
}

func buildAgentCompanionService(ctx context.Context, cfg config.Config, log zerolog.Logger, agent agenttypes.Agent) (*companion.Service, func(), error) {
	store, err := contextvar.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return nil, nil, fmt.Errorf("open context store: %w", err)
	}

	dbPath := filepath.Join(cfg.Storage.Root, "companion.db")
	memoryDB, closeFn, err := dbutil.OpenStoreDB(ctx, cfg.Storage.Root, "COMPANION", filepath.Base(dbPath), nil)
	if err != nil {
		_ = store.Close()
		return nil, nil, fmt.Errorf("open companion memory database: %w", err)
	}

	llmProvider := strings.TrimSpace(agent.LLMProvider)
	if llmProvider == "" {
		llmProvider = cfg.LLM.Provider
	}
	if agent.ExecMode == agenttypes.ModeTick {
		llmProvider = "lmstudio"
	}
	llmAPIKey := strings.TrimSpace(agent.LLMAPIKey)
	if llmAPIKey == "" {
		llmAPIKey = cfg.LLM.ResolveAPIKey(llmProvider)
	}
	llmModel := strings.TrimSpace(agent.LLMModel)
	if llmModel == "" {
		llmModel = cfg.LLM.ResolveModel(llmProvider)
	}
	if agent.ExecMode == agenttypes.ModeTick && llmModel == "" {
		llmModel = llmproviders.DefaultModelForProvider("lmstudio")
	}

	var memStore storage.MemoryStore
	var sessionStore *sessions.Store
	cleanup := func() {
		if sessionStore != nil {
			_ = sessionStore.Close()
		}
		if memStore != nil {
			_ = memStore.Close()
		}
		_ = closeFn()
		_ = store.Close()
	}

	memoryCfg := agentMemoryConfig(agent)
	memoryBehavior := companion.MemoryBehaviorForRetention(normalizeAgentMemoryRetention(agent))
	if openedStore, memErr := memorystore.OpenFromConfig(ctx, cfg); memErr != nil {
		log.Warn().Err(memErr).Msg("open memory store for agent companion recall failed; continuing without semantic recall")
	} else {
		memStore = openedStore
	}
	var sessionRecallProvider companion.SessionRecallProvider
	if openedStore, sessionErr := sessions.OpenFromConfig(ctx, cfg); sessionErr != nil {
		log.Warn().Err(sessionErr).Msg("open sessions store for agent companion recall failed; continuing without session recall")
	} else {
		sessionStore = openedStore
		var embedder *semantic.Embedder
		if created, embedErr := semantic.NewEmbedderFromConfig(semantic.ScopeSessions, cfg); embedErr == nil {
			embedder = created
		} else {
			log.Debug().Err(embedErr).Msg("session embedding provider unavailable; session recall will use BM25 fallback")
		}
		sessionRecallProvider = &companion.SessionStoreRecallProvider{
			Store:       sessionStore,
			Embedder:    embedder,
			MemoryStore: memStore,
			Workspace:   workspace.CanonicalID(cfg.Storage.Root),
		}
	}

	svc := companion.NewService(store, companion.ServiceConfig{
		Logger:                log,
		MemoryDB:              memoryDB,
		MemoryConfig:          &memoryCfg,
		MemoryBehavior:        memoryBehavior,
		RequireContextQuery:   false,
		LLMProvider:           llmProvider,
		LLMAPIKey:             llmAPIKey,
		LLMModel:              llmModel,
		LLMBaseURL:            firstNonEmpty(agent.LLMBaseURL, cfg.LLM.ResolveBaseURL(llmProvider)),
		LLMAuthMode:           firstNonEmpty(agent.LLMAuthMode, cfg.LLM.ResolveAuthMode(llmProvider)),
		LLMAuthHeader:         firstNonEmpty(agent.LLMAuthHeader, cfg.LLM.ResolveAuthHeader(llmProvider)),
		LLMAuthPrefix:         firstNonEmpty(agent.LLMAuthPrefix, cfg.LLM.ResolveAuthPrefix(llmProvider)),
		MemoryStore:           memStore,
		MemoryWorkspace:       workspace.CanonicalID(cfg.Storage.Root),
		Config:                &cfg,
		SessionRecallProvider: sessionRecallProvider,
	}, nil)
	return svc, cleanup, nil
}

func buildAgentCompanionSearchService(ctx context.Context, cfg config.Config, log zerolog.Logger, agent agenttypes.Agent) (*companion.Service, func(), error) {
	store, err := contextvar.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return nil, nil, fmt.Errorf("open context store: %w", err)
	}

	dbPath := filepath.Join(cfg.Storage.Root, "companion.db")
	memoryDB, closeDB, err := dbutil.OpenStoreDB(ctx, cfg.Storage.Root, "COMPANION", filepath.Base(dbPath), nil)
	if err != nil {
		_ = store.Close()
		return nil, nil, fmt.Errorf("open companion memory database: %w", err)
	}

	memStore, err := memorystore.OpenFromConfig(ctx, cfg)
	if err != nil {
		_ = closeDB()
		_ = store.Close()
		return nil, nil, fmt.Errorf("open memory store: %w", err)
	}

	llmProvider := strings.TrimSpace(agent.LLMProvider)
	if llmProvider == "" {
		llmProvider = cfg.LLM.Provider
	}
	if agent.ExecMode == agenttypes.ModeTick {
		llmProvider = "lmstudio"
	}
	llmAPIKey := strings.TrimSpace(agent.LLMAPIKey)
	if llmAPIKey == "" {
		llmAPIKey = cfg.LLM.ResolveAPIKey(llmProvider)
	}
	llmModel := strings.TrimSpace(agent.LLMModel)
	if llmModel == "" {
		llmModel = cfg.LLM.ResolveModel(llmProvider)
	}
	if agent.ExecMode == agenttypes.ModeTick && llmModel == "" {
		llmModel = llmproviders.DefaultModelForProvider("lmstudio")
	}

	memoryCfg := agentMemoryConfig(agent)
	memoryBehavior := companion.MemoryBehaviorForRetention(normalizeAgentMemoryRetention(agent))
	workspaceID := workspace.CanonicalID(cfg.Storage.Root)
	var sessionStore *sessions.Store
	var sessionRecallProvider companion.SessionRecallProvider
	if openedStore, sessionErr := sessions.OpenFromConfig(ctx, cfg); sessionErr != nil {
		log.Warn().Err(sessionErr).Msg("open sessions store for agent companion search service failed; continuing without session recall")
	} else {
		sessionStore = openedStore
		var sessionEmbedder *semantic.Embedder
		if created, embedErr := semantic.NewEmbedderFromConfig(semantic.ScopeSessions, cfg); embedErr == nil {
			sessionEmbedder = created
		} else {
			log.Debug().Err(embedErr).Msg("session embedding provider unavailable; agent companion search service will use BM25 session recall")
		}
		sessionRecallProvider = &companion.SessionStoreRecallProvider{
			Store:       sessionStore,
			Embedder:    sessionEmbedder,
			MemoryStore: memStore,
			Workspace:   workspaceID,
		}
	}
	cleanup := func() {
		if sessionStore != nil {
			_ = sessionStore.Close()
		}
		_ = memStore.Close()
		_ = closeDB()
		_ = store.Close()
	}

	svc := companion.NewService(store, companion.ServiceConfig{
		Logger:                log,
		MemoryDB:              memoryDB,
		MemoryConfig:          &memoryCfg,
		MemoryBehavior:        memoryBehavior,
		RequireContextQuery:   false,
		LLMProvider:           llmProvider,
		LLMAPIKey:             llmAPIKey,
		LLMModel:              llmModel,
		LLMBaseURL:            firstNonEmpty(agent.LLMBaseURL, cfg.LLM.ResolveBaseURL(llmProvider)),
		LLMAuthMode:           firstNonEmpty(agent.LLMAuthMode, cfg.LLM.ResolveAuthMode(llmProvider)),
		LLMAuthHeader:         firstNonEmpty(agent.LLMAuthHeader, cfg.LLM.ResolveAuthHeader(llmProvider)),
		LLMAuthPrefix:         firstNonEmpty(agent.LLMAuthPrefix, cfg.LLM.ResolveAuthPrefix(llmProvider)),
		MemoryStore:           memStore,
		MemoryWorkspace:       workspaceID,
		Config:                &cfg,
		SessionRecallProvider: sessionRecallProvider,
	}, nil)
	return svc, cleanup, nil
}

func normalizeAgentMemoryRetention(agent agenttypes.Agent) agenttypes.MemoryRetention {
	if strings.TrimSpace(string(agent.MemoryRetention)) != "" {
		return agenttypes.NormalizeMemoryRetention(agent.MemoryRetention)
	}
	return agenttypes.DefaultMemoryRetentionForScope(agenttypes.NormalizeMemoryScope(agent.MemoryScope))
}

func agentMemoryConfig(agent agenttypes.Agent) companion.MemoryConfig {
	cfg := companion.DefaultMemoryConfig()
	switch normalizeAgentMemoryRetention(agent) {
	case agenttypes.MemoryRetentionCompanion:
		cfg.VividWindowHours = 72
		cfg.VividMaxTurns = 120
		cfg.VividTokenBudget = 30000
		cfg.RecentWindowDays = 21
		cfg.RecentTokenBudget = 12000
		cfg.HistoryTokenBudget = 10000
		cfg.TotalTokenBudget = 52000
	case agenttypes.MemoryRetentionTask:
		cfg.VividWindowHours = 12
		cfg.VividMaxTurns = 24
		cfg.VividTokenBudget = 12000
		cfg.RecentWindowDays = 3
		cfg.RecentTokenBudget = 4000
		cfg.HistoryTokenBudget = 2000
		cfg.TotalTokenBudget = 18000
	case agenttypes.MemoryRetentionEphemeral:
		cfg.VividWindowHours = 6
		cfg.VividMaxTurns = 12
		cfg.VividTokenBudget = 6000
		cfg.RecentWindowDays = 1
		cfg.RecentTokenBudget = 2000
		cfg.HistoryTokenBudget = 1000
		cfg.TotalTokenBudget = 9000
	case agenttypes.MemoryRetentionDurable:
		fallthrough
	default:
	}
	return cfg
}

func defaultDistillForAgent(agent agenttypes.Agent) bool {
	switch normalizeAgentMemoryRetention(agent) {
	case agenttypes.MemoryRetentionTask, agenttypes.MemoryRetentionEphemeral:
		return false
	default:
		return true
	}
}

func defaultMemorySearchLimitForAgent(agent agenttypes.Agent) int {
	switch normalizeAgentMemoryRetention(agent) {
	case agenttypes.MemoryRetentionCompanion:
		return 12
	case agenttypes.MemoryRetentionTask:
		return 5
	case agenttypes.MemoryRetentionEphemeral:
		return 3
	default:
		return 8
	}
}

func clampMemorySearchLimitForAgent(agent agenttypes.Agent, requested int) int {
	maxLimit := 12
	switch normalizeAgentMemoryRetention(agent) {
	case agenttypes.MemoryRetentionCompanion:
		maxLimit = 20
	case agenttypes.MemoryRetentionTask:
		maxLimit = 8
	case agenttypes.MemoryRetentionEphemeral:
		maxLimit = 5
	}
	if requested <= 0 {
		return defaultMemorySearchLimitForAgent(agent)
	}
	if requested > maxLimit {
		return maxLimit
	}
	return requested
}

func resolveAgentConversationID(agent agenttypes.Agent, requested string) string {
	if trimmed := strings.TrimSpace(requested); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(agent.ConversationID); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(agent.ID)
}

func isSandboxBackedAgent(agent agenttypes.Agent) bool {
	return strings.TrimSpace(agent.WorkspaceSource) == "sandbox" &&
		strings.TrimSpace(agent.SandboxProvider) == "opensandbox" &&
		strings.TrimSpace(agent.SandboxID) != ""
}

func handleAgentMemoryCompress(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, agentID string) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		ConversationID string `json:"conversation_id,omitempty"`
		Distill        *bool  `json:"distill,omitempty"`
	}
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

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

	svc, cleanup, err := buildAgentCompanionService(r.Context(), cfg, log, agent)
	if err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Msg("failed to build agent companion service")
		httpError(w, http.StatusInternalServerError, "failed to initialize agent memory")
		return
	}
	defer cleanup()

	if svc.Memory() == nil {
		httpError(w, http.StatusInternalServerError, "memory features not enabled")
		return
	}

	conversationID := resolveAgentConversationID(agent, req.ConversationID)
	distill := defaultDistillForAgent(agent)
	if req.Distill != nil {
		distill = *req.Distill
	}

	result, err := svc.Memory().CompressConversation(r.Context(), conversationID, companion.CompressionOptions{
		Distill: distill,
	})
	if err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Str("conversation_id", conversationID).Msg("agent memory compress failed")
		httpError(w, http.StatusInternalServerError, "agent memory compress failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"conversation_id": conversationID,
		"processed_dates": result.ProcessedDates,
		"summarized":      result.Summarized,
		"skipped":         result.Skipped,
		"distilled":       result.Distilled,
		"policy": map[string]any{
			"memory_scope":     string(agenttypes.NormalizeMemoryScope(agent.MemoryScope)),
			"memory_retention": string(normalizeAgentMemoryRetention(agent)),
			"default_distill":  defaultDistillForAgent(agent),
		},
	})
}

func loadAgentRecord(ctx context.Context, cfg config.Config, log zerolog.Logger, agentID string) (agenttypes.Agent, func(), error) {
	store, err := agents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open agents store")
		return agenttypes.Agent{}, nil, fmt.Errorf("failed to open agents store")
	}
	agent, err := store.Get(ctx, agentID)
	if err != nil {
		_ = store.Close()
		if errors.Is(err, agents.ErrNotFound) {
			return agenttypes.Agent{}, nil, fmt.Errorf("agent not found")
		}
		log.Error().Err(err).Str("agent_id", agentID).Msg("failed to load agent")
		return agenttypes.Agent{}, nil, fmt.Errorf("failed to load agent")
	}
	return agent, func() { _ = store.Close() }, nil
}

func handleAgentMemoryStats(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, agentID string) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	agent, closeStore, err := loadAgentRecord(r.Context(), cfg, log, agentID)
	if err != nil {
		if err.Error() == "agent not found" {
			httpError(w, http.StatusNotFound, "agent not found")
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer closeStore()

	svc, cleanup, err := buildAgentCompanionService(r.Context(), cfg, log, agent)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to initialize agent memory")
		return
	}
	defer cleanup()

	stats, err := svc.GetMemoryStats(r.Context(), resolveAgentConversationID(agent, ""))
	if err != nil {
		httpError(w, http.StatusInternalServerError, "get memory stats failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"stats": stats,
		"policy": map[string]any{
			"memory_scope":     string(agenttypes.NormalizeMemoryScope(agent.MemoryScope)),
			"memory_retention": string(normalizeAgentMemoryRetention(agent)),
			"default_distill":  defaultDistillForAgent(agent),
		},
	})
}

func handleAgentMemoryContext(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, agentID string) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	agent, closeStore, err := loadAgentRecord(r.Context(), cfg, log, agentID)
	if err != nil {
		if err.Error() == "agent not found" {
			httpError(w, http.StatusNotFound, "agent not found")
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer closeStore()

	svc, cleanup, err := buildAgentCompanionService(r.Context(), cfg, log, agent)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to initialize agent memory")
		return
	}
	defer cleanup()

	conversationID := resolveAgentConversationID(agent, strings.TrimSpace(r.URL.Query().Get("conversation_id")))
	contextText, err := svc.GetMemoryContext(r.Context(), conversationID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "get memory context failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"conversation_id": conversationID,
		"context":         contextText,
		"policy": map[string]any{
			"memory_scope":       string(agenttypes.NormalizeMemoryScope(agent.MemoryScope)),
			"memory_retention":   string(normalizeAgentMemoryRetention(agent)),
			"search_limit":       defaultMemorySearchLimitForAgent(agent),
			"default_distill":    defaultDistillForAgent(agent),
			"context_token_hint": agentMemoryConfig(agent).TotalTokenBudget,
		},
	})
}

func handleAgentMemorySearch(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, agentID string) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		httpError(w, http.StatusBadRequest, "query parameter q is required")
		return
	}
	agent, closeStore, err := loadAgentRecord(r.Context(), cfg, log, agentID)
	if err != nil {
		if err.Error() == "agent not found" {
			httpError(w, http.StatusNotFound, "agent not found")
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer closeStore()

	svc, cleanup, err := buildAgentCompanionSearchService(r.Context(), cfg, log, agent)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to initialize agent memory search")
		return
	}
	defer cleanup()

	requestedLimit := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			requestedLimit = parsed
		}
	}
	limit := clampMemorySearchLimitForAgent(agent, requestedLimit)
	conversationID := resolveAgentConversationID(agent, strings.TrimSpace(r.URL.Query().Get("conversation_id")))
	results, err := svc.SearchMemory(r.Context(), conversationID, query, limit)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "search memory failed: "+err.Error())
		return
	}

	type memorySearchResult struct {
		Name      string  `json:"name"`
		Type      string  `json:"type"`
		Score     float64 `json:"score"`
		Summary   string  `json:"summary"`
		SessionID string  `json:"session_id,omitempty"`
		UpdatedAt string  `json:"updated_at,omitempty"`
	}
	out := make([]memorySearchResult, 0, len(results))
	for _, result := range results {
		out = append(out, memorySearchResult{
			Name:      result.Entry.Name,
			Type:      result.Entry.Type,
			Score:     result.Score,
			Summary:   result.Entry.Summary,
			SessionID: result.Entry.SessionID,
			UpdatedAt: result.Entry.UpdatedAt.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"conversation_id": conversationID,
		"query":           query,
		"limit":           limit,
		"results":         out,
		"policy": map[string]any{
			"memory_scope":     string(agenttypes.NormalizeMemoryScope(agent.MemoryScope)),
			"memory_retention": string(normalizeAgentMemoryRetention(agent)),
			"default_limit":    defaultMemorySearchLimitForAgent(agent),
			"effective_limit":  limit,
		},
	})
}

func publishAgentChatEvent(events agentEventPublisher, event agentChatEvent) {
	if events == nil {
		return
	}
	events.Publish("agent.chat", event)
}

func truncateAgentChatPayload(content string, maxLen int) string {
	content = strings.TrimSpace(content)
	if maxLen <= 0 || len(content) <= maxLen {
		return content
	}
	return content[:maxLen] + "...(truncated)"
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
	ConversationID  *string `json:"conversation_id,omitempty"`  // Pointer to distinguish null/missing
	MemoryScope     *string `json:"memory_scope,omitempty"`     // agent|session
	MemoryRetention *string `json:"memory_retention,omitempty"` // companion|durable|task|ephemeral
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
	if req.MemoryScope != nil {
		scope := agenttypes.NormalizeMemoryScope(agenttypes.MemoryScope(strings.TrimSpace(*req.MemoryScope)))
		if err := store.UpdateMemoryScope(r.Context(), agentID, scope); err != nil {
			log.Error().Err(err).Str("agent_id", agentID).Msg("failed to update memory_scope")
			httpError(w, http.StatusInternalServerError, "failed to update agent")
			return
		}
		agent.MemoryScope = scope
	}
	if req.MemoryRetention != nil {
		retention := agenttypes.NormalizeMemoryRetention(agenttypes.MemoryRetention(strings.TrimSpace(*req.MemoryRetention)))
		if err := store.UpdateMemoryRetention(r.Context(), agentID, retention); err != nil {
			log.Error().Err(err).Str("agent_id", agentID).Msg("failed to update memory_retention")
			httpError(w, http.StatusInternalServerError, "failed to update agent")
			return
		}
		agent.MemoryRetention = retention
	}

	// Return updated agent
	ar := AgentResponse{
		ID:              agent.ID,
		ParentID:        agent.ParentID,
		Namespace:       agent.Namespace,
		WorkspaceRoot:   agent.WorkspaceRoot,
		WorkspaceSource: agent.WorkspaceSource,
		Name:            agent.Name,
		Slug:            agent.Slug,
		Role:            agent.Role,
		PromptSummary:   summarizePrompt(agent.Prompt, 100),
		SkillsAllow:     agent.SkillsAllow,
		ShareBB:         agent.ShareBB,
		State:           string(agent.State),
		LLMProvider:     agent.LLMProvider,
		LLMModel:        agent.LLMModel,
		ExecMode:        string(agent.ExecMode),
		ThinkInterval:   agent.ThinkInterval,
		ConversationID:  agent.ConversationID,
		MemoryScope:     string(agenttypes.NormalizeMemoryScope(agent.MemoryScope)),
		MemoryRetention: string(agenttypes.NormalizeMemoryRetention(agent.MemoryRetention)),
		SandboxProvider: agent.SandboxProvider,
		SandboxID:       agent.SandboxID,
		RepoURL:         agent.RepoURL,
		RepoRef:         agent.RepoRef,
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

func loadAgentRuntimeTree(ctx context.Context, cfg config.Config, log zerolog.Logger, agent agenttypes.Agent, depth int) *agentRuntimeTreeData {
	runtime := &agentRuntimeTreeData{
		Enabled: strings.TrimSpace(agent.ID) != "",
		AgentID: strings.TrimSpace(agent.ID),
		Depth:   depth,
	}
	if runtime.AgentID == "" {
		runtime.Error = "agent has no id"
		return runtime
	}
	if isSandboxBackedAgent(agent) {
		runtime.Root = loadSandboxRuntimeTree(agent)
		return runtime
	}

	reader, closeFn, available, err := loadOptionalRuntimeStateReader(ctx, cfg)
	if err != nil {
		runtime.Error = err.Error()
		return runtime
	}
	if !available {
		return runtime
	}
	defer func() {
		if closeFn != nil {
			_ = closeFn()
		}
	}()

	visited := map[string]struct{}{}
	root := loadAgentRuntimeTreeNode(ctx, cfg, log, reader, coreworker.Record{
		Tag:     runtime.AgentID,
		AgentID: runtime.AgentID,
		Metadata: map[string]any{
			"workspace_id": strings.TrimSpace(agent.Namespace),
			"role":         strings.TrimSpace(agent.Role),
			"name":         strings.TrimSpace(agent.Name),
			"slug":         strings.TrimSpace(agent.Slug),
		},
	}, depth, visited)
	runtime.Root = root
	if root != nil && strings.TrimSpace(root.Error) != "" {
		runtime.Error = root.Error
	}
	return runtime
}

func loadSandboxRuntimeTree(agent agenttypes.Agent) *agentRuntimeTreeNode {
	return &agentRuntimeTreeNode{
		Tag:     strings.TrimSpace(agent.ID),
		AgentID: strings.TrimSpace(agent.ID),
		Status:  "sandbox",
		Metadata: map[string]any{
			"workspace_id":     strings.TrimSpace(agent.Namespace),
			"workspace_root":   strings.TrimSpace(agent.WorkspaceRoot),
			"workspace_source": strings.TrimSpace(agent.WorkspaceSource),
			"role":             strings.TrimSpace(agent.Role),
			"name":             strings.TrimSpace(agent.Name),
			"slug":             strings.TrimSpace(agent.Slug),
			"sandbox_provider": strings.TrimSpace(agent.SandboxProvider),
			"sandbox_id":       strings.TrimSpace(agent.SandboxID),
			"repo_url":         strings.TrimSpace(agent.RepoURL),
			"repo_ref":         strings.TrimSpace(agent.RepoRef),
		},
		State: map[string]any{
			"profile": "sandbox",
			"foxctl": map[string]any{
				"status":           strings.TrimSpace(string(agent.State)),
				"workspace_source": strings.TrimSpace(agent.WorkspaceSource),
			},
			"sandbox": map[string]any{
				"provider":       strings.TrimSpace(agent.SandboxProvider),
				"id":             strings.TrimSpace(agent.SandboxID),
				"workspace_root": strings.TrimSpace(agent.WorkspaceRoot),
				"repo_url":       strings.TrimSpace(agent.RepoURL),
				"repo_ref":       strings.TrimSpace(agent.RepoRef),
			},
		},
	}
}

func loadAgentRuntimeTreeNode(
	ctx context.Context,
	cfg config.Config,
	log zerolog.Logger,
	reader coreworker.StateReader,
	seed coreworker.Record,
	depth int,
	visited map[string]struct{},
) *agentRuntimeTreeNode {
	agentID := strings.TrimSpace(seed.AgentID)
	node := &agentRuntimeTreeNode{
		Tag:      strings.TrimSpace(seed.Tag),
		AgentID:  agentID,
		PID:      strings.TrimSpace(seed.PID),
		Metadata: seed.Metadata,
	}
	if agentID == "" {
		node.Error = "runtime node has no agent_id"
		return node
	}
	if _, ok := visited[agentID]; ok {
		node.Error = "runtime subtree cycle detected"
		return node
	}
	visited[agentID] = struct{}{}
	defer delete(visited, agentID)

	record, err := reader.Worker(ctx, coreworker.LookupRequest{AgentID: agentID})
	if err != nil {
		node.Error = err.Error()
		return node
	}
	node.Tag = chooseNonEmpty(strings.TrimSpace(record.Tag), node.Tag)
	node.PID = chooseNonEmpty(strings.TrimSpace(record.PID), node.PID)
	node.Metadata = mergeRuntimeMetadata(node.Metadata, record.Metadata)
	node.Status = string(record.Status)
	node.State = decodeRuntimeWorkerState(ctx, cfg, log, agentID, record.RawState, "failed to decode agent runtime state; returning raw payload")
	if depth <= 0 {
		return node
	}

	children, err := reader.Children(ctx, coreworker.ChildrenRequest{ParentAgentID: agentID})
	if err != nil {
		node.Error = chooseNonEmpty(node.Error, err.Error())
		return node
	}
	for _, child := range children {
		node.Children = append(node.Children, loadAgentRuntimeTreeNode(ctx, cfg, log, reader, child, depth-1, visited))
	}
	return node
}
