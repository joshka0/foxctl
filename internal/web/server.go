package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/consoleapp"
	"github.com/jkatigb/agentctl/internal/engine"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/web/api"
	"github.com/jkatigb/agentctl/internal/web/consolews"
	"github.com/jkatigb/agentctl/internal/web/sse"
)

// Server is the agentctl web server.
type Server struct {
	opts       Options
	cfg        config.Config
	log        zerolog.Logger
	sseHub     *sse.Hub
	consoleHub *consolews.Hub
}

// NewServer creates and returns a configured Server for the web layer.
// It initializes the SSE hub and the console websocket hub, configures console
// session persistence and the console runner factory, and wires observability
// events to the SSE publisher.
// The provided ctx is used for console hub persistence goroutines and should be
// tied to the application's lifecycle.
//
// Index:
// - Purpose: Initialize the web server and real-time hubs
// - Flow: create hubs → wire persistence/runner factory → connect SSE publisher → return server
// - SideEffects: sets global SSE publisher; starts hub dependencies
// - FailureModes: NewServer does not fail on createConsoleRunnerFactory; runner errors surface when the console hub invokes the per-session factory
// - Related: createConsoleRunnerFactory, sse.NewHub, consolews.NewHub
// - Keywords: web_server, sse_hub, consolews, runner_factory
func NewServer(ctx context.Context, opts Options, cfg config.Config, log zerolog.Logger) (*Server, error) {
	sseHub := sse.NewHub()
	consoleHub := consolews.NewHub(ctx)

	// Wire up observability events to SSE for real-time activity streaming
	observability.SetSSEPublisher(sseHub)

	// Set up persistence adapter for console sessions
	persistence := consolews.NewPersistenceAdapter(cfg.Storage.Root)
	consoleHub.SetPersistence(persistence)

	// Set up runner factory for console sessions (LLM integration)
	runnerFactory := createConsoleRunnerFactory(cfg)
	consoleHub.SetRunnerFactory(runnerFactory)

	s := &Server{
		opts:       opts,
		cfg:        cfg,
		log:        log,
		sseHub:     sseHub,
		consoleHub: consoleHub,
	}

	return s, nil
}

// createConsoleRunnerFactory creates a factory function that creates LLM runners for console sessions.
//
// Index:
// - Purpose: Build console runner factory with LLMChat engine defaults
// - Flow: load env → build engine config → construct engine → wrap runner
// - SideEffects: reads env vars; may emit failure events
// - FailureModes: missing API keys, engine construction errors
// - Observability: emits web.console_engine_failed
// - Related: engine.NewLLMChatEngine, consoleapp.NewRunner
// - Keywords: console_runner, llmchat, api_key, web.console_engine_failed
func createConsoleRunnerFactory(cfg config.Config) consolews.RunnerFactory {
	// Load .env file to get API keys
	config.LoadDotEnv()

	return func(session *consolews.Session) consolews.Runner {
		// Create LLM engine config
		baseCfg := engine.LLMChatConfig{
			MaxIterations: 20,
			Temperature:   0.0,
			MaxTokens:     4096,
		}

		// Validate that we have some provider configured (auto-detects API keys).
		// Per-turn overrides are applied inside the runner.
		_, err := engine.NewLLMChatEngine(baseCfg)
		if err != nil {
			observability.Emit(context.Background(), observability.NewEvent("web.console_engine_failed").
				WithComponent("web").
				WithSession(session.ID(), "").
				Error(err, 0))
			return nil
		}

		// Create console runner
		runner := consoleapp.NewRunner(consoleapp.RunnerConfig{
			BaseConfig: baseCfg,
			Tools:      nil, // No tools for now - pure chat
		})

		return runner
	}
}

// Run starts the SSE hub event loop. Call in a goroutine.
func (s *Server) Run(ctx context.Context) {
	s.sseHub.Run(ctx)
}

// SSEHub returns the SSE hub for publishing events.
func (s *Server) SSEHub() *sse.Hub {
	return s.sseHub
}

// ConsoleHub returns the console WebSocket hub.
func (s *Server) ConsoleHub() *consolews.Hub {
	return s.consoleHub
}

// Handler returns the HTTP handler for the server.
//
// Index:
// - Purpose: Build the HTTP mux and wire API routes
// - Flow: register routes → return mux
// - SideEffects: registers handlers
// - FailureModes: none (handler construction)
// - Related: api.* handlers, sse.Handler
// - Keywords: api_routes, http_mux, handlers
func (s *Server) Handler() http.Handler {
	apiMux := http.NewServeMux()

	// --- Health ---
	apiMux.HandleFunc("/api/health", api.StatusHandler(s.cfg, s.log))

	// --- SSE Events ---
	apiMux.HandleFunc("/api/events", sse.Handler(s.sseHub))

	// --- Jobs (Phase 2) ---
	apiMux.HandleFunc("/api/jobs", api.JobsListHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/jobs/", api.JobDetailHandler(s.cfg, s.log))

	// --- CAS (Phase 2) ---
	apiMux.HandleFunc("/api/cas/", api.CASHandler(s.cfg, s.log))

	// --- Workspaces ---
	apiMux.HandleFunc("/api/workspaces", api.WorkspacesHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/workspaces/switch", api.WorkspaceSwitchHandler(s.cfg, s.log))

	// --- OpenAPI / Swagger ---
	apiMux.HandleFunc("/api/openapi.json", api.OpenAPIHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/swagger", api.SwaggerUIHandler())
	apiMux.HandleFunc("/api/swagger/", api.SwaggerUIHandler())

	// --- Skills (Phase 4) ---
	apiMux.HandleFunc("/api/skills", api.SkillsListHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/skills/schema", api.SkillsSchemaHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/skills/run", api.SkillsRunHandler(s.cfg, s.log))
	// Skill CRUD routes - supports all HTTP methods:
	//   GET    /api/skills/todo/manage        → list
	//   GET    /api/skills/todo/manage/{id}   → get
	//   POST   /api/skills/todo/manage        → add
	//   PUT    /api/skills/todo/manage/{id}   → update
	//   DELETE /api/skills/todo/manage/{id}   → delete
	apiMux.HandleFunc("/api/skills/", api.SkillsCRUDHandler(s.cfg, s.log))

	// --- Console (Phase 6-8) ---
	apiMux.HandleFunc("/api/console/sessions", api.ConsoleSessionsHandler(s.consoleHub, s.cfg, s.log))
	apiMux.HandleFunc("/api/console/sessions/", api.ConsoleSessionDetailHandler(s.consoleHub, s.cfg, s.log))

	// --- Tasks (Phase 11) ---
	apiMux.HandleFunc("/api/tasks", api.TasksListHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/tasks/", api.TaskDetailHandler(s.cfg, s.log))

	// --- Sessions (Phase 11) ---
	apiMux.HandleFunc("/api/sessions", api.SessionsListHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/sessions/", api.SessionDetailHandler(s.cfg, s.log))

	// --- Agents (Phase 11) ---
	apiMux.HandleFunc("/api/agents", api.AgentsListHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/agents/", api.AgentDetailHandler(s.cfg, s.log))

	// --- Stats & Insights (Phase 11) ---
	apiMux.HandleFunc("/api/stats", api.StatsHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/insights", api.InsightsHandler(s.cfg, s.log))

	// --- Mailbox (Phase 11) ---
	apiMux.HandleFunc("/api/mailbox", api.MailboxListHandler(s.cfg, s.log))

	// --- Reservations (Phase 11) ---
	apiMux.HandleFunc("/api/reservations", api.ReservationsListHandler(s.cfg, s.log))

	// --- Blackboard (Phase 11) ---
	apiMux.HandleFunc("/api/blackboard", api.BlackboardListHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/blackboard/", api.BlackboardDetailHandler(s.cfg, s.log))

	// --- Logs (Phase 12 - GUI) ---
	apiMux.HandleFunc("/api/logs", api.LogsHandler(s.cfg, s.log))

	// --- SQLite Browser (Phase 11) ---
	apiMux.HandleFunc("/api/sqlite", api.SQLiteHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/sqlite/", api.SQLiteHandler(s.cfg, s.log))

	// --- Search (Phase 11) ---
	apiMux.HandleFunc("/api/search", api.SearchHandler(s.cfg, s.log))

	// --- Codemaps (Phase 11) ---
	apiMux.HandleFunc("/api/codemaps", api.CodemapsListHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/codemaps/", api.CodemapDetailHandler(s.cfg, s.log))

	// --- Companion (RLM Mobile Backend) ---
	apiMux.HandleFunc("/api/companion/providers", api.CompanionProvidersHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/companion/chat", api.CompanionChatHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/companion/conversations", api.CompanionConversationsHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/companion/conversations/", func(w http.ResponseWriter, r *http.Request) {
		// Route based on sub-path: /api/companion/conversations/:id/(messages|personality)
		// or DELETE /api/companion/conversations/:id for soft delete
		path := strings.TrimPrefix(r.URL.Path, "/api/companion/conversations/")
		parts := strings.Split(path, "/")
		if len(parts) >= 2 && parts[1] != "" {
			switch parts[1] {
			case "messages":
				if r.Method == http.MethodDelete {
					api.CompanionMessageDeleteHandler(s.cfg, s.log).ServeHTTP(w, r)
				} else {
					api.CompanionConversationMessagesHandler(s.cfg, s.log).ServeHTTP(w, r)
				}
			case "compress":
				api.CompanionConversationCompressHandler(s.cfg, s.log).ServeHTTP(w, r)
			case "personality":
				// Check for sub-path: /api/companion/conversations/:id/personality/dimension
				if len(parts) >= 3 && parts[2] == "dimension" {
					api.CompanionPersonalityDimensionPatchHandler(s.cfg, s.log).ServeHTTP(w, r)
				} else {
					api.CompanionPersonalityHandler(s.cfg, s.log).ServeHTTP(w, r)
				}
			case "settings":
				api.CompanionConversationSettingsHandler(s.cfg, s.log).ServeHTTP(w, r)
			default:
				http.Error(w, "unknown conversation endpoint", http.StatusNotFound)
			}
		} else if len(parts) >= 1 && parts[0] != "" {
			// /api/companion/conversations/:id - DELETE for soft delete, PATCH for rename
			switch r.Method {
			case http.MethodDelete:
				api.CompanionConversationDeleteHandler(s.cfg, s.log).ServeHTTP(w, r)
			case http.MethodPatch:
				api.CompanionConversationRenameHandler(s.cfg, s.log).ServeHTTP(w, r)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		} else {
			http.Error(w, "invalid conversation path", http.StatusBadRequest)
		}
	})
	apiMux.HandleFunc("/api/companion/context", api.CompanionContextSetHandler(s.cfg, s.log))
	// Context routes with path params (GET, DELETE for specific conversation/key)
	apiMux.HandleFunc("/api/companion/context/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			api.CompanionContextGetHandler(s.cfg, s.log).ServeHTTP(w, r)
		case http.MethodDelete:
			// Check if this is a clear (no key) or delete specific key
			path := strings.TrimPrefix(r.URL.Path, "/api/companion/context/")
			parts := strings.Split(path, "/")
			if len(parts) == 1 || (len(parts) > 1 && parts[1] == "") {
				// Clear conversation: DELETE /api/companion/context/:id?clear=true
				api.CompanionContextClearHandler(s.cfg, s.log).ServeHTTP(w, r)
			} else {
				// Delete key: DELETE /api/companion/context/:id/:key
				api.CompanionContextDeleteHandler(s.cfg, s.log).ServeHTTP(w, r)
			}
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	// Character routes (presence)
	skillRunner := api.NewSkillRunner(s.cfg)
	apiMux.HandleFunc("/api/companion/characters", api.CompanionCharacterCreateHandler(s.cfg, s.log, skillRunner))
	apiMux.HandleFunc("/api/companion/characters/", func(w http.ResponseWriter, r *http.Request) {
		// Route based on path and method
		// GET  /api/companion/characters/:conversation_id           → list characters
		// GET  /api/companion/characters/:conversation_id/:id       → get character
		// POST /api/companion/characters/:conversation_id/:id/overlays → add overlay
		path := strings.TrimPrefix(r.URL.Path, "/api/companion/characters/")
		parts := strings.Split(path, "/")

		switch r.Method {
		case http.MethodGet:
			if len(parts) >= 2 && parts[1] != "" {
				// GET /:conversation_id/:character_id
				api.CompanionCharacterGetHandler(s.cfg, s.log, skillRunner).ServeHTTP(w, r)
			} else if len(parts) >= 1 && parts[0] != "" {
				// GET /:conversation_id
				api.CompanionCharactersListHandler(s.cfg, s.log, skillRunner).ServeHTTP(w, r)
			} else {
				http.Error(w, "conversation_id required", http.StatusBadRequest)
			}
		case http.MethodPost:
			if len(parts) >= 3 && parts[2] == "overlays" {
				// POST /:conversation_id/:character_id/overlays
				api.CompanionCharacterOverlayHandler(s.cfg, s.log, skillRunner).ServeHTTP(w, r)
			} else {
				http.Error(w, "invalid path for POST", http.StatusBadRequest)
			}
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Memory routes with path params
	apiMux.HandleFunc("/api/companion/memory/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/companion/memory/")
		parts := strings.Split(path, "/")
		if len(parts) < 1 || parts[0] == "" {
			http.Error(w, "conversation_id required", http.StatusBadRequest)
			return
		}

		switch r.Method {
		case http.MethodGet:
			if len(parts) >= 2 {
				switch parts[1] {
				case "stats":
					api.CompanionMemoryStatsHandler(s.cfg, s.log).ServeHTTP(w, r)
				case "context":
					api.CompanionMemoryContextHandler(s.cfg, s.log).ServeHTTP(w, r)
				default:
					http.Error(w, "unknown memory endpoint", http.StatusNotFound)
				}
			} else {
				http.Error(w, "invalid memory path", http.StatusBadRequest)
			}
		case http.MethodDelete:
			// DELETE requires exactly /api/companion/memory/{conversation_id}
			if len(parts) != 1 {
				http.Error(w, "DELETE only supports /api/companion/memory/{conversation_id}", http.StatusBadRequest)
				return
			}
			api.CompanionMemoryClearHandler(s.cfg, s.log).ServeHTTP(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Route legacy /api and wrapped /api/v1 to the same handlers.
	mux := http.NewServeMux()
	mux.Handle("/api/", apiMux)
	mux.Handle("/api/v1/", wrapV1(apiMux))
	mux.HandleFunc("/ws/console/", consolews.HandleWebSocket(s.consoleHub))

	// --- Static UI (optional) ---
	if s.opts.UIDir != "" {
		fs := http.FileServer(http.Dir(s.opts.UIDir))
		mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Don't serve static files for API routes
			if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
				http.NotFound(w, r)
				return
			}
			fs.ServeHTTP(w, r)
		}))
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("agentctl_web running. In dev, run Vite at :5173.\n"))
		})
	}

	// Apply middleware
	h := withCORS(s.opts.DevCORS, mux)
	h = withRequestLogging(s.log, h)
	return h
}

// withCORS adds CORS headers for local development.
func withCORS(dev bool, next http.Handler) http.Handler {
	if !dev {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "http://localhost:5173"
		}

		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// withRequestLogging is a middleware placeholder for HTTP request logging.
//
// Currently a pass-through (no logging) for two reasons:
//  1. Debug-level request logging adds overhead and noise for local API server
//  2. Wide events are better suited for operation-level telemetry (errors, latency)
//     rather than per-request debug traces
//
// TODO: Consider adding observability events for:
//   - Slow requests (latency > threshold)
//   - Error responses (4xx, 5xx)
//   - Specific endpoints that benefit from audit trails
func withRequestLogging(_ zerolog.Logger, next http.Handler) http.Handler {
	return next
}
