//nolint:forbidigo // The web server still owns a zerolog seam until the wider observability migration is completed.
package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	consolepkg "github.com/joshka0/foxctl/internal/console"
	"github.com/joshka0/foxctl/internal/console/app"
	"github.com/joshka0/foxctl/internal/context/companion"
	"github.com/joshka0/foxctl/internal/interfaces/chatadapter"
	"github.com/joshka0/foxctl/internal/interfaces/chatadapter/discord"
	"github.com/joshka0/foxctl/internal/interfaces/chatadapter/teams"
	"github.com/joshka0/foxctl/internal/interfaces/chatadapter/telegram"
	"github.com/joshka0/foxctl/internal/interfaces/web/api"
	"github.com/joshka0/foxctl/internal/interfaces/web/consolews"
	"github.com/joshka0/foxctl/internal/interfaces/web/sse"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/storage/convref"
)

// Server is the foxctl web server.
type Server struct {
	opts             Options
	cfg              config.Config
	log              zerolog.Logger
	sseHub           *sse.Hub
	consoleTransport *consolews.Hub
	consoleSessions  consolepkg.SessionManager
	chatAdapter      chatadapter.ChatAdapter
	turnLock         companion.Locker
	convRefStore     convref.Store
	orchRuntime      api.OrchestrationRuntimeHost
}

// NewServer creates and returns a configured Server for the web layer.
// It initializes the SSE hub and console websocket transport, configures
// console session persistence and the console runner factory, and wires
// observability events to the SSE publisher.
// The provided ctx is used for console transport persistence goroutines and
// should be tied to the application's lifecycle.
//
// Index:
// - Purpose: Initialize the web server and real-time hubs
// - Flow: create hubs → wire persistence/runner factory → connect SSE publisher → return server
// - SideEffects: sets global SSE publisher; starts hub dependencies
// - FailureModes: NewServer does not fail on consoleapp.NewDefaultRunnerFactory; runner errors surface when the console transport invokes the per-session factory
// - Related: consoleapp.NewDefaultRunnerFactory, sse.NewHub, consolews.NewHub
// - Keywords: web_server, sse_hub, consolews, console_runner_factory
func NewServer(ctx context.Context, opts Options, cfg config.Config, log zerolog.Logger) (*Server, error) {
	sseHub := sse.NewHub()
	consoleTransport := consolews.NewHub(ctx)

	// Wire up observability events to SSE for real-time activity streaming
	observability.SetSSEPublisher(sseHub)

	// Set up persistence adapter for console sessions
	persistence := consolepkg.NewSessionPersistence(cfg.Storage.Root)
	consoleTransport.SetPersistence(persistence)

	// Set up runner factory for console sessions (LLM integration)
	runnerFactory := consoleapp.NewDefaultRunnerFactory(ctx)
	consoleTransport.SetRunnerFactory(runnerFactory)

	s := &Server{
		opts:             opts,
		cfg:              cfg,
		log:              log,
		sseHub:           sseHub,
		consoleTransport: consoleTransport,
		consoleSessions:  consoleTransport,
		turnLock:         buildCompanionLocker(ctx, cfg),
	}

	orchRuntime, err := api.NewOrchestrationRuntimeHost(ctx, cfg, log)
	if err != nil {
		return nil, fmt.Errorf("configure orchestration runtime host: %w", err)
	}
	s.orchRuntime = orchRuntime

	// Start chat adapter if configured
	switch opts.ChatAdapter {
	case "":
		// no-op
	case "discord":
		if cfg.Discord.BotToken == "" {
			log.Warn().Msg("--chat discord requires DISCORD_BOT_TOKEN; skipping adapter")
			break
		}
		if err := s.startChatAdapter(ctx); err != nil {
			log.Warn().Err(err).Msg("failed to start chat adapter (continuing without it)")
		}
	case "telegram":
		if cfg.Telegram.BotToken == "" {
			log.Warn().Msg("--chat telegram requires TELEGRAM_BOT_TOKEN; skipping adapter")
			break
		}
		if err := s.startChatAdapter(ctx); err != nil {
			log.Warn().Err(err).Msg("failed to start chat adapter (continuing without it)")
		}
	case "teams":
		if strings.TrimSpace(cfg.Teams.TenantID) == "" || strings.TrimSpace(cfg.Teams.ClientID) == "" || strings.TrimSpace(cfg.Teams.ClientSecret) == "" {
			log.Warn().Msg("--chat teams requires TEAMS_TENANT_ID, TEAMS_CLIENT_ID, TEAMS_CLIENT_SECRET; skipping adapter")
			break
		}
		if cfg.Teams.SkipJWTVerify && !opts.DevCORS {
			log.Warn().Msg("--chat teams with TEAMS_SKIP_JWT_VERIFY=true requires --dev-cors; skipping adapter")
			break
		}
		if err := s.startChatAdapter(ctx); err != nil {
			log.Warn().Err(err).Msg("failed to start chat adapter (continuing without it)")
		}
	default:
		log.Warn().Str("chat_adapter", opts.ChatAdapter).Msg("unknown chat adapter; skipping")
	}

	return s, nil
}

// WaitConsoleTransport drains background console transport goroutines such as
// session persistence workers during shutdown.
func (s *Server) WaitConsoleTransport() {
	if s == nil || s.consoleTransport == nil {
		return
	}
	s.consoleTransport.Wait()
}

func buildCompanionLocker(ctx context.Context, cfg config.Config) companion.Locker {
	if ctx == nil {
		ctx = context.Background()
	}

	if strings.EqualFold(cfg.Database.Driver, "postgres") && strings.TrimSpace(cfg.Database.Postgres.DSN) != "" {
		poolCfg, err := pgxpool.ParseConfig(cfg.Database.Postgres.DSN)
		if err != nil {
			observability.Emit(ctx, observability.NewEvent("web.companion_turnlock_init").
				WithComponent("web").
				WithData("mode", "in-memory").
				WithData("reason", "invalid_postgres_dsn").
				Error(err, 0))
			return companion.NewTurnLock()
		}
		if cfg.Database.Postgres.MaxOpenConns > 0 {
			poolCfg.MaxConns = int32(cfg.Database.Postgres.MaxOpenConns)
		}

		pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
		if err != nil {
			observability.Emit(ctx, observability.NewEvent("web.companion_turnlock_init").
				WithComponent("web").
				WithData("mode", "in-memory").
				WithData("reason", "postgres_pool_init_failed").
				Error(err, 0))
			return companion.NewTurnLock()
		}

		observability.Emit(ctx, observability.NewEvent("web.companion_turnlock_init").
			WithComponent("web").
			WithData("mode", "postgres").
			Success(0))
		return companion.NewPgTurnLock(pool)
	}

	observability.Emit(ctx, observability.NewEvent("web.companion_turnlock_init").
		WithComponent("web").
		WithData("mode", "in-memory").
		Success(0))
	return companion.NewTurnLock()
}

func (s *Server) buildConvRefStore(ctx context.Context) convref.Store {
	if strings.EqualFold(s.cfg.Database.Driver, "postgres") && strings.TrimSpace(s.cfg.Database.Postgres.DSN) != "" {
		store, err := convref.OpenPostgres(ctx, s.cfg.Database.Postgres.DSN)
		if err != nil {
			s.log.Warn().Err(err).Msg("failed to open postgres convref store; conversation refs will not be persisted")
			return nil
		}
		return store
	}

	// SQLite fallback for local/dev setups.
	dbPath := filepath.Join(s.cfg.Storage.Root, "storage", "convref.db")
	store, err := convref.OpenSQLite(ctx, dbPath)
	if err != nil {
		s.log.Warn().Err(err).Msg("failed to open sqlite convref store; conversation refs will not be persisted")
		return nil
	}
	return store
}

// Run starts the SSE hub event loop. Call in a goroutine.
func (s *Server) Run(ctx context.Context) {
	if s.orchRuntime != nil {
		go func() {
			if err := s.orchRuntime.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.log.Error().Err(err).Msg("orchestration runtime host failed")
			}
		}()
	}
	s.sseHub.Run(ctx)
	if s.orchRuntime != nil {
		if err := s.orchRuntime.Close(); err != nil {
			s.log.Warn().Err(err).Msg("failed to close orchestration runtime host")
		}
	}
}

// SSEHub returns the SSE hub for publishing events.
func (s *Server) SSEHub() *sse.Hub {
	return s.sseHub
}

// startChatAdapter initializes and connects the chat adapter.
func (s *Server) startChatAdapter(ctx context.Context) error {
	// Derive daemon URL from the server's listen address
	daemonURL := ""
	if strings.TrimSpace(s.opts.Addr) != "" {
		addr := strings.TrimSpace(s.opts.Addr)
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			// Handle bare port formats like ":8090" and "8090".
			if strings.HasPrefix(addr, ":") {
				host = "localhost"
				port = strings.TrimPrefix(addr, ":")
			} else {
				digitsOnly := true
				for _, r := range addr {
					if r < '0' || r > '9' {
						digitsOnly = false
						break
					}
				}
				if digitsOnly {
					host = "localhost"
					port = addr
				}
			}
		} else if host == "" || host == "0.0.0.0" {
			host = "localhost"
		}
		if port != "" {
			daemonURL = "http://" + net.JoinHostPort(host, port)
		}
	}

	bridge := chatadapter.NewDefaultBridge(s.cfg, daemonURL)

	switch s.opts.ChatAdapter {
	case "discord":
		adapter := discord.New(s.cfg.Discord, daemonURL)
		adapter.SetSSEHub(s.sseHub)

		adapter.OnCommand(bridge.HandleCommand)
		adapter.OnInteraction(adapter.HandleInteraction)

		// Phase 3: Wire console sessions for natural language messaging
		sessionBridge := discord.NewSessionBridge(s.consoleSessions, adapter, s.cfg.Discord, s.turnLock)
		adapter.OnMessage(sessionBridge.HandleMessage)

		if err := adapter.Connect(ctx); err != nil {
			return err
		}
		if err := adapter.RegisterCommands(ctx, discord.MVPCommands()); err != nil {
			_ = adapter.Disconnect(ctx)
			return err
		}

		s.chatAdapter = adapter

	case "telegram":
		adapter := telegram.New(s.cfg.Telegram, daemonURL, nil)
		adapter.SetSSEHub(s.sseHub)

		adapter.OnCommand(bridge.HandleCommand)
		adapter.OnInteraction(adapter.HandleInteraction)

		sessionBridge := telegram.NewSessionBridge(s.consoleSessions, adapter, s.cfg.Telegram, s.turnLock)
		adapter.OnMessage(sessionBridge.HandleMessage)

		if err := adapter.Connect(ctx); err != nil {
			return err
		}
		if err := adapter.RegisterCommands(ctx, telegram.MVPCommands()); err != nil {
			_ = adapter.Disconnect(ctx)
			return err
		}

		s.chatAdapter = adapter

	case "teams":
		if s.cfg.Teams.SkipJWTVerify && !s.opts.DevCORS {
			observability.Emit(ctx, observability.NewEvent("teams.skip_jwt_verify_blocked").
				WithComponent("web").
				WithData("warning", "TEAMS_SKIP_JWT_VERIFY requires --dev-cors; refusing to start Teams adapter").
				Error(nil, 0))
			return fmt.Errorf("teams adapter refused: TEAMS_SKIP_JWT_VERIFY requires --dev-cors")
		}

		adapter := teams.New(s.cfg.Teams, daemonURL, nil)
		adapter.SetSSEHub(s.sseHub)
		if convRefStore := s.buildConvRefStore(ctx); convRefStore != nil {
			adapter.SetConvRefStore(convRefStore)
			s.convRefStore = convRefStore
		}

		adapter.OnCommand(bridge.HandleCommand)
		adapter.OnInteraction(adapter.HandleInteraction)

		sessionBridge := chatadapter.NewSessionBridge(s.consoleSessions, adapter, chatadapter.SessionBridgeConfig{
			PlatformName:     "teams",
			MaxMessageLen:    4000,
			EditIntervalMS:   s.cfg.Teams.EditIntervalMS,
			ChatProfile:      s.cfg.Teams.ChatProfile,
			ChatSystemPrompt: s.cfg.Teams.ChatSystemPrompt,
		}, s.turnLock)
		adapter.OnMessage(sessionBridge.HandleMessage)

		if err := adapter.Connect(ctx); err != nil {
			if s.convRefStore != nil {
				_ = s.convRefStore.Close()
				s.convRefStore = nil
			}
			return err
		}
		if err := adapter.RegisterCommands(ctx, nil); err != nil {
			_ = adapter.Disconnect(ctx)
			if s.convRefStore != nil {
				_ = s.convRefStore.Close()
				s.convRefStore = nil
			}
			return err
		}

		s.chatAdapter = adapter

	default:
		return nil
	}

	if s.chatAdapter != nil {
		s.log.Info().Str("adapter", s.chatAdapter.Name()).Msg("chat adapter started")
	}
	return nil
}

// StopChatAdapter gracefully disconnects the chat adapter.
func (s *Server) StopChatAdapter(ctx context.Context) {
	if s.chatAdapter != nil {
		if err := s.chatAdapter.Disconnect(ctx); err != nil {
			s.log.Warn().Err(err).Msg("chat adapter disconnect error")
		}
	}
	if s.convRefStore != nil {
		if err := s.convRefStore.Close(); err != nil {
			s.log.Warn().Err(err).Msg("convref store close error")
		}
		s.convRefStore = nil
	}
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
	apiMux.HandleFunc("/api/logs/cleanup", api.LogCleanupHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/v2/events/stream", api.V2EventsStreamHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/v2/events", api.V2EventsHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/v2/model", api.V2ModelHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/v2/runs", api.V2RunsHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/v2/runs/", api.V2RunDetailHandler(s.cfg, s.log, s.orchRuntime))

	// --- OAuth AuthBroker Callback ---
	apiMux.HandleFunc("/api/oauth/callback", api.OAuthCallbackHandler(s.cfg, s.log))

	// --- Microsoft Teams Webhook (Chat Adapter) ---
	apiMux.HandleFunc("/api/teams/messages", func(w http.ResponseWriter, r *http.Request) {
		ta, ok := s.chatAdapter.(*teams.Adapter)
		if !ok || ta == nil {
			http.Error(w, "teams adapter inactive", http.StatusServiceUnavailable)
			return
		}
		ta.HTTPHandler()(w, r)
	})

	// --- Jobs (Phase 2) ---
	apiMux.HandleFunc("/api/jobs", api.JobsListHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/jobs/", api.JobDetailHandler(s.cfg, s.log))

	// --- CAS (Phase 2) ---
	apiMux.HandleFunc("/api/cas", api.CASHandler(s.cfg, s.log))
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
	apiMux.HandleFunc("/api/skills/manifest/", api.SkillDetailHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/skills/schema", api.SkillsSchemaHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/skills/run", api.SkillsRunHandler(s.cfg, s.log))
	// Skill CRUD routes - supports all HTTP methods:
	//   GET    /api/skills/todo/manage        → list
	//   GET    /api/skills/todo/manage/{id}   → get
	//   POST   /api/skills/todo/manage        → add
	//   PUT    /api/skills/todo/manage/{id}   → update
	//   DELETE /api/skills/todo/manage/{id}   → delete
	apiMux.HandleFunc("/api/skills/", api.SkillsCRUDHandler(s.cfg, s.log))

	// --- MCP Facade (read-only) ---
	apiMux.HandleFunc("/api/mcp/status", api.MCPStatusHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/mcp/tools", api.MCPToolsHandler(s.cfg, s.log))

	// --- Console (Phase 6-8) ---
	apiMux.HandleFunc("/api/console/sessions", api.ConsoleSessionsHandler(s.consoleSessions, s.cfg, s.log))
	apiMux.HandleFunc("/api/console/sessions/", api.ConsoleSessionDetailHandler(s.consoleSessions, s.cfg, s.log))

	// --- Tasks (Phase 11) ---
	apiMux.HandleFunc("/api/tasks", api.TasksListHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/tasks/", api.TaskDetailHandler(s.cfg, s.log))

	// --- Sessions (Phase 11) ---
	apiMux.HandleFunc("/api/sessions", api.SessionsListHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/sessions/", api.SessionDetailHandler(s.cfg, s.log))

	// --- Agents (Phase 11) ---
	apiMux.HandleFunc("/api/agents", api.AgentsListHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/agents/", api.AgentDetailHandlerWithRuntime(s.cfg, s.log, s.sseHub, s.orchRuntime))
	apiMux.HandleFunc("/api/atcp", api.ATCPHandler())
	apiMux.HandleFunc("/api/atcp/", api.ATCPHandler())

	// --- Stats & Insights (Phase 11) ---
	apiMux.HandleFunc("/api/stats", api.StatsHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/insights", api.InsightsHandler(s.cfg, s.log))

	// --- Mailbox (Phase 11) ---
	apiMux.HandleFunc("/api/mailbox", api.MailboxListHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/mux/panes", api.MuxPanesHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/mux/read", api.MuxReadHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/rooms", api.RoomsListHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/rooms/", api.RoomDetailHandler(s.cfg, s.log, s.sseHub))

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

	// --- ACA Context / Proposal Merge ---
	apiMux.HandleFunc("/api/context/overview", api.ContextOverviewHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/context/next-proposal-merge", api.ContextNextProposalMergeHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/context/next-proposal-merge/claim", api.ContextNextProposalMergeHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/context/proposals/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/release-merge"):
			api.ContextProposalReleaseMergeHandler(s.cfg, s.log).ServeHTTP(w, r)
		case strings.HasSuffix(r.URL.Path, "/merge"):
			api.ContextProposalMergeHandler(s.cfg, s.log).ServeHTTP(w, r)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	})

	// --- Codemaps (Phase 11) ---
	apiMux.HandleFunc("/api/codemaps", api.CodemapsListHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/codemaps/", api.CodemapDetailHandler(s.cfg, s.log))

	// --- Orchestration (Wave 7 / PR-46) ---
	apiMux.HandleFunc("/api/orchestration/dispatch-issue", api.OrchestrationDispatchIssueHandlerWithRuntime(s.cfg, s.log, s.orchRuntime))
	apiMux.HandleFunc("/api/orchestration/card-action", api.OrchestrationCardActionHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/orchestration/board-get", api.OrchestrationBoardGetHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/orchestration/board-card-get", api.OrchestrationBoardCardGetHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/orchestration/board-card-runtime-get", api.OrchestrationBoardCardRuntimeGetHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/orchestration/refresh", api.OrchestrationRefreshHandlerWithRuntime(s.cfg, s.log, s.orchRuntime))
	apiMux.HandleFunc("/api/orchestration/seed-cards", api.OrchestrationSeedCardsHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/orchestration/cleanup-cards", api.OrchestrationCleanupCardsHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/orchestration/archive-cards", api.OrchestrationArchiveCardsHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/orchestration/restore-cards", api.OrchestrationRestoreCardsHandler(s.cfg, s.log))

	// --- Companion (RLM Mobile Backend) ---
	// Shared Locker ensures per-conversation mutual exclusion across all
	// HTTP requests. Without this, each per-request Service would have its own
	// lock instance providing zero mutual exclusion.
	skillRunner := api.NewSkillRunner(s.cfg)
	apiMux.HandleFunc("/api/companion/providers", api.CompanionProvidersHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/companion/cochange", api.CompanionCoChangeHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/companion/chat", api.CompanionChatHandler(s.cfg, s.log, s.turnLock, skillRunner))
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
				case "export":
					api.CompanionMemoryExportHandler(s.cfg, s.log).ServeHTTP(w, r)
				case "search":
					api.CompanionMemorySearchHandler(s.cfg, s.log).ServeHTTP(w, r)
				default:
					http.Error(w, "unknown memory endpoint", http.StatusNotFound)
				}
			} else {
				http.Error(w, "invalid memory path", http.StatusBadRequest)
			}
		case http.MethodPost:
			if len(parts) == 2 && parts[1] == "import" {
				api.CompanionMemoryImportHandler(s.cfg, s.log).ServeHTTP(w, r)
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

	// Route canonical API handlers.
	mux := http.NewServeMux()
	mux.Handle("/api/", apiMux)
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusGone)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    "EDEPRECATED",
				"message": "legacy /api/v1 routes were removed; use /api routes",
				"hint":    "replace /api/v1/... with /api/...",
			},
		})
	})
	mux.HandleFunc("/ws/console/", consolews.HandleWebSocket(s.consoleTransport))

	// --- Kubernetes Probes (root path, not under /api/) ---
	mux.HandleFunc("/healthz", api.LivenessHandler())
	mux.HandleFunc("/readyz", api.ReadinessHandler(s.cfg))

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
			_, _ = w.Write([]byte("foxctl_web running. In dev, run Vite at :5173.\n"))
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
