package web

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/consoleapp"
	"github.com/jkatigb/agentctl/internal/engine"
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

// NewServer creates a new web server.
// The ctx is used for console hub persistence goroutines - pass the application lifecycle context.
func NewServer(ctx context.Context, opts Options, cfg config.Config, log zerolog.Logger) (*Server, error) {
	sseHub := sse.NewHub(log.With().Str("component", "sse").Logger())
	consoleHub := consolews.NewHub(ctx, log.With().Str("component", "console").Logger())

	// Set up persistence adapter for console sessions
	persistence := consolews.NewPersistenceAdapter(cfg.Storage.Root, log)
	consoleHub.SetPersistence(persistence)

	// Set up runner factory for console sessions (LLM integration)
	runnerFactory := createConsoleRunnerFactory(cfg, log)
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
func createConsoleRunnerFactory(cfg config.Config, log zerolog.Logger) consolews.RunnerFactory {
	// Load .env file to get API keys
	config.LoadDotEnv()

	// Debug: check if API key is available
	groqKey := os.Getenv("GROQ_API_KEY")
	orKey := os.Getenv("OPENROUTER_API_KEY")
	log.Info().
		Bool("groq_key_present", groqKey != "").
		Bool("openrouter_key_present", orKey != "").
		Msg("console runner factory initialized, .env loaded")

	return func(session *consolews.Session) consolews.Runner {
		log.Info().Str("session", session.ID()).Msg("creating console runner for session")

		// Create LLM engine config
		engineCfg := engine.LLMChatConfig{
			MaxIterations: 20,
			Temperature:   0.0,
			MaxTokens:     4096,
			Logger:        log.With().Str("session", session.ID()).Logger(),
		}

		// Create the engine (auto-detects API keys)
		llmEngine, err := engine.NewLLMChatEngine(engineCfg)
		if err != nil {
			log.Warn().Err(err).Msg("failed to create LLM engine for console - no API key?")
			return nil
		}

		log.Info().Str("session", session.ID()).Msg("LLM engine created for console session")

		// Create console runner
		runner := consoleapp.NewRunner(consoleapp.RunnerConfig{
			Engine: llmEngine,
			Tools:  nil, // No tools for now - pure chat
			Logger: log.With().Str("session", session.ID()).Logger(),
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
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// --- Health ---
	mux.HandleFunc("/api/health", api.StatusHandler(s.cfg, s.log))

	// --- SSE Events ---
	mux.HandleFunc("/api/events", sse.Handler(s.sseHub, s.log))

	// --- Jobs (Phase 2) ---
	mux.HandleFunc("/api/jobs", api.JobsListHandler(s.cfg, s.log))
	mux.HandleFunc("/api/jobs/", api.JobDetailHandler(s.cfg, s.log))

	// --- CAS (Phase 2) ---
	mux.HandleFunc("/api/cas/", api.CASHandler(s.cfg, s.log))

	// --- Workspaces ---
	mux.HandleFunc("/api/workspaces", api.WorkspacesHandler(s.cfg, s.log))
	mux.HandleFunc("/api/workspaces/switch", api.WorkspaceSwitchHandler(s.cfg, s.log))

	// --- Skills (Phase 4) ---
	mux.HandleFunc("/api/skills", api.SkillsListHandler(s.cfg, s.log))
	mux.HandleFunc("/api/skills/schema", api.SkillsSchemaHandler(s.cfg, s.log))
	mux.HandleFunc("/api/skills/run", api.SkillsRunHandler(s.cfg, s.log))
	// Skill detail: /api/skills/{category}/{name} - must come after specific routes
	mux.HandleFunc("/api/skills/", api.SkillDetailHandler(s.cfg, s.log))

	// --- Console (Phase 6-8) ---
	mux.HandleFunc("/api/console/sessions", api.ConsoleSessionsHandler(s.consoleHub, s.cfg, s.log))
	mux.HandleFunc("/api/console/sessions/", api.ConsoleSessionDetailHandler(s.consoleHub, s.cfg, s.log))
	mux.HandleFunc("/ws/console/", consolews.HandleWebSocket(s.consoleHub, s.log))

	// --- Tasks (Phase 11) ---
	mux.HandleFunc("/api/tasks", api.TasksListHandler(s.cfg, s.log))
	mux.HandleFunc("/api/tasks/", api.TaskDetailHandler(s.cfg, s.log))

	// --- Sessions (Phase 11) ---
	mux.HandleFunc("/api/sessions", api.SessionsListHandler(s.cfg, s.log))
	mux.HandleFunc("/api/sessions/", api.SessionDetailHandler(s.cfg, s.log))

	// --- Agents (Phase 11) ---
	mux.HandleFunc("/api/agents", api.AgentsListHandler(s.cfg, s.log))
	mux.HandleFunc("/api/agents/", api.AgentDetailHandler(s.cfg, s.log))

	// --- Stats & Insights (Phase 11) ---
	mux.HandleFunc("/api/stats", api.StatsHandler(s.cfg, s.log))
	mux.HandleFunc("/api/insights", api.InsightsHandler(s.cfg, s.log))

	// --- Mailbox (Phase 11) ---
	mux.HandleFunc("/api/mailbox", api.MailboxListHandler(s.cfg, s.log))

	// --- Reservations (Phase 11) ---
	mux.HandleFunc("/api/reservations", api.ReservationsListHandler(s.cfg, s.log))

	// --- Blackboard (Phase 11) ---
	mux.HandleFunc("/api/blackboard", api.BlackboardListHandler(s.cfg, s.log))

	// --- SQLite Browser (Phase 11) ---
	mux.HandleFunc("/api/sqlite", api.SQLiteHandler(s.cfg, s.log))
	mux.HandleFunc("/api/sqlite/", api.SQLiteHandler(s.cfg, s.log))

	// --- Search (Phase 11) ---
	mux.HandleFunc("/api/search", api.SearchHandler(s.cfg, s.log))

	// --- Codemaps (Phase 11) ---
	mux.HandleFunc("/api/codemaps", api.CodemapsListHandler(s.cfg, s.log))
	mux.HandleFunc("/api/codemaps/", api.CodemapDetailHandler(s.cfg, s.log))

	// --- Companion (RLM Mobile Backend) ---
	mux.HandleFunc("/api/companion/chat", api.CompanionChatHandler(s.cfg, s.log))
	mux.HandleFunc("/api/companion/conversations", api.CompanionConversationsHandler(s.cfg, s.log))
	mux.HandleFunc("/api/companion/context", api.CompanionContextSetHandler(s.cfg, s.log))
	// Context routes with path params (GET, DELETE for specific conversation/key)
	mux.HandleFunc("/api/companion/context/", func(w http.ResponseWriter, r *http.Request) {
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
	// Memory routes with path params
	mux.HandleFunc("/api/companion/memory/", func(w http.ResponseWriter, r *http.Request) {
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
			api.CompanionMemoryClearHandler(s.cfg, s.log).ServeHTTP(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

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

// withRequestLogging logs HTTP requests.
func withRequestLogging(log zerolog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Debug().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Str("remote", r.RemoteAddr).
			Msg("http request")
		next.ServeHTTP(w, r)
	})
}
