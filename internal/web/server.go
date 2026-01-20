package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/rs/zerolog"

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

	s := &Server{
		opts:       opts,
		cfg:        cfg,
		log:        log,
		sseHub:     sseHub,
		consoleHub: consoleHub,
	}

	return s, nil
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
