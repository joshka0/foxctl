package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/jkatigb/agentctl/internal/agent/optimization"
	"github.com/jkatigb/agentctl/internal/agent/tools"
	"github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/companion"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/engine"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/storage"
	storagents "github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/jkatigb/agentctl/internal/storage/contextvar"
	"github.com/jkatigb/agentctl/internal/storage/mailbox"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

// messageLeaseDuration is the default lease timeout for message polling.
// This duration should align with the visibility timeout used for Nack operations
// to ensure consistent message handling semantics.
const messageLeaseDuration = 30 * time.Second

type daemonStores struct {
	agentStore           storagents.Store
	mailboxStore         mailbox.Store
	tasksStore           tasks.Store
	boardStore           blackboard.BoardStore
	bbStore              blackboard.Store
	contextvarStore      contextvar.Store              // for companion service RLM context
	companionMemory      *companion.ConversationMemory // nil if disabled
	companionMemoryDB    *sql.DB                       // need to close this too
	companionMemoryClose func() error
	compressionDaemon    *companion.CompressionDaemon // nil if disabled
	repoIndexStore       *repoindex.Store
}

// openDaemonStores opens and initializes all persistent stores used by the daemon under the provided workspace root
// and returns a daemonStores containing the opened resources.
//
// The function opens agent, mailbox, tasks, board, blackboard, and contextvar stores. If Options.EnableCompanionMemory
// is true it also opens a companion memory DB, constructs a ConversationMemory (with roleplay or standard memory
// configuration based on Options.CompanionMode), optionally attaches an LLM-based summarizer, and may start a background
// compression daemon. The returned daemonStores includes any cleanup callback for the companion DB.
//
// On any error the function closes any already-opened resources before returning the error.
//
// Index:
// - Purpose: Initialize daemon storage dependencies and optional companion services
// - Flow: open stores → configure companion memory → start compression daemon → return handles
// - SideEffects: opens databases; starts compression daemon
// - FailureModes: store open errors, companion initialization failures
// - Related: daemonStores.Close, initOptimization
// - Keywords: daemon_stores, companion_memory, contextvar, mailbox, blackboard
func openDaemonStores(ctx context.Context, root string, opts Options) (*daemonStores, error) {
	stores := &daemonStores{}

	agentStore, err := storagents.Open(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("open agent store: %w", err)
	}
	stores.agentStore = agentStore

	mailboxStore, err := mailbox.Open(ctx, root)
	if err != nil {
		stores.Close()
		return nil, fmt.Errorf("open mailbox store: %w", err)
	}
	stores.mailboxStore = mailboxStore

	tasksStore, err := tasks.Open(ctx, root)
	if err != nil {
		stores.Close()
		return nil, fmt.Errorf("open tasks store: %w", err)
	}
	stores.tasksStore = tasksStore

	boardStore, err := blackboard.OpenBoardStore(ctx, root)
	if err != nil {
		stores.Close()
		return nil, fmt.Errorf("open board store: %w", err)
	}
	stores.boardStore = boardStore

	bbStore, err := blackboard.Open(ctx, root)
	if err != nil {
		stores.Close()
		return nil, fmt.Errorf("open blackboard store: %w", err)
	}
	stores.bbStore = bbStore

	// Open contextvar store for companion service RLM context
	contextvarStore, err := contextvar.Open(ctx, root)
	if err != nil {
		stores.Close()
		return nil, fmt.Errorf("open contextvar store: %w", err)
	}
	stores.contextvarStore = contextvarStore

	// Open companion memory if enabled
	if opts.EnableCompanionMemory {
		dbPath := filepath.Join(root, "companion.db")
		db, closeFn, err := sqliteutil.OpenDBShared(ctx, dbPath, nil) // schema managed by NewConversationMemory
		if err != nil {
			stores.Close()
			return nil, fmt.Errorf("open companion db: %w", err)
		}
		stores.companionMemoryDB = db
		stores.companionMemoryClose = closeFn

		// Select memory configuration based on companion mode
		var memCfg companion.MemoryConfig
		switch opts.CompanionMode {
		case "roleplay":
			// Extended memory for roleplay/chat companions
			// - 48h vivid window for extended conversation memory
			// - 100 turn limit for active sessions
			// - 50K total tokens for rich context
			memCfg = companion.RoleplayMemoryConfig()
			log.Info().Msg("companion memory enabled (roleplay mode)")
		default:
			// Standard companion memory config
			memCfg = companion.DefaultMemoryConfig()
			log.Info().Msg("companion memory enabled (standard mode)")
		}

		// Create memory options
		memOpts := []companion.MemoryOption{companion.WithMemoryConfig(memCfg)}

		// Create summarizer if LLM is configured (enables L1/L2 compression)
		enableCompression := false
		if opts.LLMAPIKey != "" && opts.LLMProvider != "" {
			summarizer := companion.NewLLMSummarizer(companion.LLMSummarizerConfig{
				Provider: opts.LLMProvider,
				APIKey:   opts.LLMAPIKey,
				Model:    opts.LLMModel,
				Logger:   log.Logger,
			})
			memOpts = append(memOpts, companion.WithSummarizer(summarizer))
			enableCompression = true
			log.Info().Str("provider", opts.LLMProvider).Msg("companion memory summarizer enabled")
		}

		companionMem, err := companion.NewConversationMemory(db, memOpts...)
		if err != nil {
			stores.Close()
			return nil, fmt.Errorf("create companion memory: %w", err)
		}
		stores.companionMemory = companionMem

		// Start compression daemon for L0→L1→L2 compression
		if enableCompression {
			stores.compressionDaemon = companion.NewCompressionDaemon(companion.DaemonConfig{
				Memory:         companionMem,
				DB:             db,
				DailyInterval:  1 * time.Hour, // Check for daily compression every hour
				WeeklyInterval: 6 * time.Hour, // Check for weekly distillation every 6 hours
				Logger:         log.Logger,
			})
			stores.compressionDaemon.Start(ctx)
			log.Info().Msg("companion compression daemon started")
		} else {
			log.Info().Msg("companion memory summarizer not configured; compression daemon disabled")
		}
	}

	return stores, nil
}

func (s *daemonStores) Close() {
	if s == nil {
		return
	}
	// Stop compression daemon first (it uses memory and db)
	if s.compressionDaemon != nil {
		s.compressionDaemon.Stop()
	}
	if s.companionMemoryDB != nil {
		if s.companionMemoryClose != nil {
			_ = s.companionMemoryClose()
		} else {
			_ = s.companionMemoryDB.Close()
		}
	}
	if s.repoIndexStore != nil {
		_ = s.repoIndexStore.Close()
	}
	if s.contextvarStore != nil {
		_ = s.contextvarStore.Close()
	}
	if s.bbStore != nil {
		_ = s.bbStore.Close()
	}
	if s.boardStore != nil {
		_ = s.boardStore.Close()
	}
	if s.tasksStore != nil {
		_ = s.tasksStore.Close()
	}
	if s.mailboxStore != nil {
		_ = s.mailboxStore.Close()
	}
	if s.agentStore != nil {
		_ = s.agentStore.Close()
	}
}

// Run starts the agent daemon.
//
// Index:
// - Purpose: Initialize daemon dependencies and run the polling loop
// - Flow: resolve workspace → open stores → init optimization → load agent → init engine → start heartbeat → poll loop
// - SideEffects: opens databases; starts goroutines; makes LLM calls; processes mailbox messages
// - FailureModes: config errors, store errors, engine init failures, polling errors
// - Related: openDaemonStores, runPollLoop, initOptimization
// - Keywords: agent_daemon, poll_loop, heartbeat, mailbox, companion
func Run(ctx context.Context, opts Options) error {
	workspaceRoot := strings.TrimSpace(opts.WorkspaceRoot)
	if workspaceRoot == "" {
		workspaceRoot = "."
	}
	absWorkspace, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return fmt.Errorf("resolve workspace root: %w", err)
	}
	opts.WorkspaceRoot = absWorkspace

	// 1. Open stores
	stores, err := openDaemonStores(ctx, opts.StorageRoot, opts)
	if err != nil {
		return err
	}
	defer stores.Close()

	// Open optimization stores if enabled
	optCtx, optCleanup := initOptimization(ctx, opts)
	defer optCleanup()

	// 2. Load and validate agent
	agentRecord, err := loadAgentRecord(ctx, stores.agentStore, opts.AgentID)
	if err != nil {
		return err
	}

	// Populate optimization context with agent info
	if optCtx != nil {
		optCtx.AgentRole = agentRecord.Role
		optCtx.WorkspaceID = agentRecord.Namespace
	}

	// 3. Transition to running
	if err := stores.agentStore.UpdateState(ctx, opts.AgentID, agent.StateRunning); err != nil {
		return fmt.Errorf("update agent state: %w", err)
	}

	// 4. Initialize agent engine (LLMChatEngine via companion.Service)
	traceID := ulid.Make().String()
	recorder := &noopRecorder{}
	var companionSvc ChatService
	log.Info().Msg("using LLMChatEngine via companion service")

	if opts.CompanionService != nil {
		companionSvc = opts.CompanionService
	} else {
		// Resolve LLM configuration - default to cerebras
		provider := agentRecord.LLMProvider
		if provider == "" {
			provider = opts.LLMProvider
		}
		if provider == "" {
			provider = "cerebras" // Default to cerebras for companion agents
		}

		model := agentRecord.LLMModel
		if model == "" {
			model = opts.LLMModel
		}
		if model == "" && provider == "cerebras" {
			model = "llama3.1-8b" // Default cerebras model
		}

		apiKey := agentRecord.LLMAPIKey
		if apiKey == "" {
			apiKey = opts.LLMAPIKey
		}
		if apiKey == "" {
			return fmt.Errorf("LLM API key required for companion service (set AGENTCTL_LLM_API_KEY or provider-specific key)")
		}

		log.Info().
			Str("provider", provider).
			Str("model", model).
			Msg("companion service LLM config")

		toolsCfg := buildToolsConfig(opts, agentRecord, traceID, stores)
		toolsCfg.FilesystemPolicy = mapFilesystemPolicy(agentRecord.Policy)

		registry, err := tools.NewRegistry(toolsCfg, recorder)
		if err != nil {
			return fmt.Errorf("create tools registry: %w", err)
		}

		extraToolExecutor := engine.ToolExecutor(tools.NewRegistryToolExecutor(registry))
		extraToolsOnly := false
		if repoIndexOnlyAllowlist(agentRecord.SkillsAllow) {
			extraToolsOnly = true
			log.Info().Msg("companion service configured for repo index tools only")
		}

		companionSvc = companion.NewService(stores.contextvarStore, companion.ServiceConfig{
			LLMProvider:        provider,
			LLMAPIKey:          apiKey,
			LLMModel:           model,
			DefaultPersonality: agentRecord.Prompt,
			MaxIterations:      agentRecord.MaxIterations,
			Timeout:            90 * time.Second,
			ExecMode:           agentRecord.ExecMode,
			Logger:             log.Logger,
			MemoryDB:           stores.companionMemoryDB,
			ExtraToolExecutor:  extraToolExecutor,
			ExtraToolsOnly:     extraToolsOnly,
		})
	}

	// 5. Start heartbeat ticker
	stopHeartbeat := startHeartbeat(ctx, stores.agentStore, opts.AgentID, opts.HeartbeatInterval)
	defer stopHeartbeat()

	// Dedupe store
	dedupeStore, dedupeCleanup, err := initDedupeStore(ctx, opts)
	if err != nil {
		return err
	}
	defer dedupeCleanup()

	// 6. Initialize cancel context for console message handling
	cancelCtx := NewCancelContext()

	// Logging setup
	logger := zerolog.New(os.Stderr).With().
		Str("agent_id", opts.AgentID).
		Str("trace_id", traceID).
		Logger()

	// 7. Enter poll loop
	return runPollLoop(ctx, pollDeps{
		opts:         opts,
		logger:       logger,
		agentRecord:  agentRecord,
		agentStore:   stores.agentStore,
		mailboxStore: stores.mailboxStore,
		dedupeStore:  dedupeStore,
		companionSvc: companionSvc,
		cancelCtx:    cancelCtx,
		optCtx:       optCtx,
	})
}

type pollDeps struct {
	opts         Options
	logger       zerolog.Logger
	agentRecord  agent.Agent
	agentStore   storagents.Store
	mailboxStore mailbox.Store
	dedupeStore  DedupeStore
	companionSvc ChatService // non-nil for companion agents
	cancelCtx    *CancelContext
	optCtx       *OptimizationContext
}

// initOptimization initializes optimization stores and pattern collector if enabled.
//
// Index:
// - Purpose: Enable pattern learning for tool selection hints
// - Flow: open trajectory store → open pattern store → create collector → return context
// - SideEffects: opens stores; logs warnings on failures
// - FailureModes: store open errors disable optimization
// - Related: optimization.NewMCPPatternCollector
// - Keywords: optimization, pattern_store, trajectory_store, collector
func initOptimization(ctx context.Context, opts Options) (*OptimizationContext, func()) {
	if !opts.EnableOptimization {
		return nil, func() {}
	}

	trajStore, err := trajectory.Open(ctx, opts.StorageRoot)
	if err != nil {
		log.Warn().Err(err).Msg("failed to open trajectory store for optimization, disabling")
		return nil, func() {}
	}

	patternStore, err := optimization.OpenPatternStore(ctx, opts.StorageRoot)
	if err != nil {
		log.Warn().Err(err).Msg("failed to open pattern store for optimization, disabling")
		_ = trajStore.Close()
		return nil, func() {}
	}

	collector := optimization.NewMCPPatternCollector(patternStore, trajStore)
	optCtx := &OptimizationContext{
		Collector: collector,
		Enabled:   true,
	}
	log.Info().Msg("optimization enabled: pattern learning active")

	cleanup := func() {
		_ = patternStore.Close()
		_ = trajStore.Close()
	}
	return optCtx, cleanup
}

func loadAgentRecord(ctx context.Context, store storagents.Store, agentID string) (agent.Agent, error) {
	agentRecord, err := store.Get(ctx, agentID)
	if err != nil {
		return agent.Agent{}, fmt.Errorf("get agent %s: %w", agentID, err)
	}
	if agentRecord.State == agent.StateStopped {
		return agent.Agent{}, errors.New("agent is stopped")
	}
	return agentRecord, nil
}

func buildToolsConfig(opts Options, agentRecord agent.Agent, traceID string, stores *daemonStores) tools.Config {
	workspaceRoot := strings.TrimSpace(opts.WorkspaceRoot)
	if workspaceRoot == "" {
		workspaceRoot = "."
	}
	return tools.Config{
		WorkspaceRoot: workspaceRoot,
		WorkspaceID:   agentRecord.Namespace,
		ActorID:       "actor:agent:" + opts.AgentID,
		TraceID:       traceID,
		Allowlist:     agentRecord.SkillsAllow,
		MaxOutputSize: agentRecord.Policy.MaxOutputKB * 1024,
		OpenTasksStore: func(ctx context.Context) (tasks.Store, error) {
			return stores.tasksStore, nil
		},
		OpenBoardStore: func(ctx context.Context) (blackboard.BoardStore, error) {
			return stores.boardStore, nil
		},
		OpenBlackboardStore: func(ctx context.Context) (blackboard.Store, error) {
			return stores.bbStore, nil
		},
		OpenCASStore: func(ctx context.Context) (storage.CASStore, error) {
			return cas.OpenDefault(ctx, opts.StorageRoot)
		},
		OpenMailboxStore: func(ctx context.Context) (mailbox.Store, error) {
			return mailbox.Open(ctx, opts.StorageRoot)
		},
		OpenAgentsStore: func(ctx context.Context) (storagents.Store, error) {
			return storagents.Open(ctx, opts.StorageRoot)
		},
		OpenRepoIndexStore: func(ctx context.Context) (*repoindex.Store, error) {
			return repoindex.Open(ctx, opts.StorageRoot, workspaceRoot)
		},
	}
}

func mapFilesystemPolicy(policy agent.Policy) string {
	fsPolicy := "workspace"
	for _, fs := range policy.Filesystem {
		switch fs.Type {
		case "home":
			if fsPolicy == "tmp" {
				fsPolicy = "all"
			} else if fsPolicy != "all" {
				fsPolicy = "home"
			}
		case "tmp":
			if fsPolicy == "home" {
				fsPolicy = "all"
			} else if fsPolicy != "all" {
				fsPolicy = "tmp"
			}
		}
	}
	return fsPolicy
}

func repoIndexOnlyAllowlist(allowlist []string) bool {
	if len(allowlist) == 0 {
		return false
	}

	allowed := map[string]struct{}{
		repoindex.ToolSearch:        {},
		repoindex.ToolExpand:        {},
		repoindex.ToolOpen:          {},
		repoindex.ToolDAGGrep:       {},
		repoindex.ToolSearchLegacy:  {},
		repoindex.ToolExpandLegacy:  {},
		repoindex.ToolOpenLegacy:    {},
		repoindex.ToolDAGGrepLegacy: {},
	}

	validCount := 0
	for _, entry := range allowlist {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			continue
		}
		if _, ok := allowed[trimmed]; !ok {
			return false
		}
		validCount++
	}

	return validCount > 0
}

func startHeartbeat(ctx context.Context, store storagents.Store, agentID string, interval time.Duration) func() {
	heartbeatTicker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-heartbeatTicker.C:
				if err := store.UpdateHeartbeat(ctx, agentID); err != nil {
					log.Error().Err(err).Msg("heartbeat update failed")
				}
			}
		}
	}()
	return heartbeatTicker.Stop
}

func initDedupeStore(ctx context.Context, opts Options) (DedupeStore, func(), error) {
	if opts.UseMemoryDedupe {
		return NewMemoryDedupeStore(), func() {}, nil
	}

	sqliteStore, err := OpenSQLiteDedupeStore(ctx, opts.StorageRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("open dedupe store: %w", err)
	}

	cleanup := func() {
		_ = sqliteStore.Close()
	}

	if cleaned, err := sqliteStore.Cleanup(ctx, 7*24*time.Hour); err != nil {
		log.Warn().Err(err).Msg("dedupe cleanup failed")
	} else if cleaned > 0 {
		log.Info().Int64("cleaned", cleaned).Msg("dedupe cleanup completed")
	}

	return sqliteStore, cleanup, nil
}

// runPollLoop polls mailbox messages and dispatches processing based on agent mode.
//
// Index:
// - Purpose: Drive daemon message processing with polling and proactive ticks
// - Flow: start poll ticker → optional think ticker → process poll ticks → handle proactive think
// - SideEffects: polls mailbox; updates agent state; invokes message handlers
// - Concurrency: runs ticker loop with non-blocking think checks
// - FailureModes: poll errors, agent stop signals, message processing errors
// - Related: processPollTick, runProactiveThink
// - Keywords: poll_loop, mailbox, proactive, poll_interval, think_interval
func runPollLoop(ctx context.Context, deps pollDeps) error {
	pollTicker := time.NewTicker(deps.opts.PollInterval)
	defer pollTicker.Stop()

	// Setup proactive think ticker if in proactive mode
	var thinkTicker *time.Ticker
	if deps.agentRecord.ExecMode == agent.ModeProactive {
		interval := time.Duration(deps.agentRecord.ThinkInterval) * time.Second
		if interval <= 0 {
			interval = 60 * time.Second
		}
		thinkTicker = time.NewTicker(interval)
		defer thinkTicker.Stop()
		deps.logger.Info().Dur("think_interval", interval).Msg("proactive mode enabled")
	}

	deps.logger.Info().
		Str("exec_mode", string(deps.agentRecord.ExecMode)).
		Int("max_iterations", deps.agentRecord.MaxIterations).
		Int("max_auto_turns", deps.agentRecord.MaxAutoTurns).
		Msg("daemon started")

	for {
		// Build select cases dynamically based on mode
		select {
		case <-ctx.Done():
			_ = deps.agentStore.UpdateState(context.Background(), deps.opts.AgentID, agent.StateStopped) //nolint:errcheck
			return ctx.Err()

		case <-pollTicker.C:
			if err := processPollTick(ctx, deps); err != nil {
				if err == errAgentStopped {
					return nil
				}
				// Continue on other errors
			}
		}

		// Check proactive tick separately (Go doesn't allow dynamic select)
		if thinkTicker != nil {
			select {
			case <-thinkTicker.C:
				if err := runProactiveThink(ctx, deps); err != nil {
					deps.logger.Warn().Err(err).Msg("proactive think failed")
				}
			default:
				// Non-blocking check
			}
		}
	}
}

var errAgentStopped = errors.New("agent stopped")

// processPollTick handles a single poll tick by fetching and processing messages.
//
// Index:
// - Purpose: Fetch mailbox messages and process them with dedupe handling
// - Flow: load agent state → poll messages → dedupe check → process message → ack/nack
// - SideEffects: mailbox ack/nack; dedupe store writes; message processing
// - FailureModes: poll errors, dedupe errors, processing failures
// - Related: processMessage, runAutonomousContinuation
// - Keywords: poll_tick, mailbox, dedupe, ack, nack
func processPollTick(ctx context.Context, deps pollDeps) error {
	currentAgent, err := deps.agentStore.Get(ctx, deps.opts.AgentID)
	if err != nil {
		deps.logger.Error().Err(err).Msg("failed to get agent state")
		return err
	}
	if currentAgent.State == agent.StateStopped {
		deps.logger.Info().Msg("agent state is stopped, exiting daemon")
		return errAgentStopped
	}

	pollTypes := []agent.MessageType{
		agent.MessageTypeAsk,
		agent.MessageTypeCmd,
		agent.MessageTypeEvent,
		agent.MessageTypeReply,
		agent.MessageTypeConsoleAsk,
		agent.MessageTypeConsoleCmd,
		agent.MessageTypeConsoleReply,
		agent.MessageTypeConsoleEvent,
	}
	messages, err := deps.mailboxStore.PollByTypes(ctx, deps.agentRecord.Namespace, messageLeaseDuration, deps.opts.MaxPollMessages, pollTypes)
	if err != nil {
		deps.logger.Error().Err(err).Msg("poll failed")
		return err
	}

	for _, msg := range messages {
		processed, err := deps.dedupeStore.IsProcessed(ctx, deps.opts.AgentID, msg.ID)
		if err != nil {
			deps.logger.Warn().Err(err).Str("msg_id", msg.ID).Msg("dedupe check failed, nacking for retry")
			if nackErr := deps.mailboxStore.Nack(ctx, msg.ID, messageLeaseDuration); nackErr != nil {
				deps.logger.Error().Err(nackErr).Str("msg_id", msg.ID).Msg("failed to nack message")
			}
			continue
		}
		if processed {
			deps.logger.Debug().Str("msg_id", msg.ID).Msg("duplicate message, acking")
			_ = deps.mailboxStore.Ack(ctx, msg.ID) //nolint:errcheck
			continue
		}

		if isMessageExpired(msg) {
			deps.logger.Warn().Str("msg_id", msg.ID).Msg("message expired, acking without processing")
			_ = deps.mailboxStore.Ack(ctx, msg.ID) //nolint:errcheck
			continue
		}

		procErr := processMessage(ctx, deps.logger, msg, deps.companionSvc, deps.mailboxStore, currentAgent.Policy, deps.optCtx, deps.cancelCtx, deps.opts.AgentID)
		if procErr != nil {
			deps.logger.Error().Err(procErr).Str("msg_id", msg.ID).Msg("processing failed")
			_ = deps.mailboxStore.Nack(ctx, msg.ID, backoffDuration(msg.Attempt)) //nolint:errcheck
		} else {
			if err := deps.dedupeStore.MarkProcessed(ctx, deps.opts.AgentID, msg.ID); err != nil {
				deps.logger.Warn().Err(err).Str("msg_id", msg.ID).Msg("failed to mark message processed")
			}
			_ = deps.mailboxStore.Ack(ctx, msg.ID) //nolint:errcheck

			// For autonomous mode: check if agent wants to continue working
			// This runs after successful message processing
			if deps.agentRecord.ExecMode == agent.ModeAutonomous || deps.agentRecord.ExecMode == agent.ModeProactive {
				runAutonomousContinuation(ctx, deps, msg)
			}
		}
	}

	return nil
}

// runAutonomousContinuation allows the agent to continue working across multiple turns
// without needing a new external message. Used for autonomous and proactive modes.
//
// Index:
// - Purpose: Execute follow-on LLMChat turns for autonomous/proactive agents
// - Flow: check max turns → run continuation prompt loop → stop on completion signal
// - SideEffects: LLM calls via companion service
// - FailureModes: execution errors stop continuation
// - Related: runProactiveThink
// - Keywords: autonomous, continuation, max_auto_turns, llmchat
func runAutonomousContinuation(ctx context.Context, deps pollDeps, lastMsg agent.Message) {
	if deps.companionSvc == nil {
		deps.logger.Warn().Msg("autonomous continuation skipped: companion service not configured")
		return
	}
	maxTurns := deps.agentRecord.MaxAutoTurns
	if maxTurns <= 1 {
		return // Autonomous continuation disabled
	}

	// For now, we implement a simple continuation strategy:
	// Send a self-continuation message asking the agent to continue if there's more work
	for turn := 1; turn < maxTurns; turn++ {
		deps.logger.Debug().Int("turn", turn+1).Int("max_turns", maxTurns).Msg("autonomous continuation")

		// Check if we should continue (agent can signal via blackboard or task completion)
		// For MVP: run a "continue" prompt and let the agent decide if there's more work
		continuePrompt := "Continue working on the previous task if there is more to do. If the task is complete, respond with 'TASK_COMPLETE'. Do not repeat already completed work."

		timeout := 10 * time.Minute
		turnCtx, cancel := context.WithTimeout(ctx, timeout)
		resp, err := deps.companionSvc.Chat(turnCtx, companion.ChatRequest{
			ConversationID: deps.opts.AgentID,
			Message:        continuePrompt,
			ExecMode:       deps.agentRecord.ExecMode,
		})
		cancel()

		if err != nil {
			deps.logger.Debug().Err(err).Msg("autonomous continuation failed, stopping")
			break
		}

		// Check if agent signaled completion
		result := resp.Response
		if containsTaskComplete(result) {
			deps.logger.Debug().Msg("agent signaled task complete, stopping continuation")
			break
		}
	}
}

// runProactiveThink runs a periodic "think" cycle for proactive agents.
// This allows the agent to check if there's work to do and initiate it.
//
// Index:
// - Purpose: Execute proactive think cycle to initiate work
// - Flow: build prompt → execute LLMChat → evaluate result → run continuation
// - SideEffects: LLM calls via companion service; logs activity
// - FailureModes: execution errors propagated
// - Related: runAutonomousContinuation
// - Keywords: proactive_think, llmchat, think_prompt, continuation
func runProactiveThink(ctx context.Context, deps pollDeps) error {
	deps.logger.Debug().Msg("running proactive think cycle")
	if deps.companionSvc == nil {
		return fmt.Errorf("companion service not configured")
	}

	// Proactive prompt: ask agent to check for work
	thinkPrompt := `You are in proactive mode. Check if there is any work that needs to be done:
1. Review any pending tasks or todos
2. Check for any issues that need attention
3. Look for opportunities to help

If there is work to do, start working on the highest priority item.
If there is nothing to do, respond with 'NO_WORK_NEEDED'.`

	timeout := 5 * time.Minute
	turnCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := deps.companionSvc.Chat(turnCtx, companion.ChatRequest{
		ConversationID: deps.opts.AgentID,
		Message:        thinkPrompt,
		ExecMode:       deps.agentRecord.ExecMode,
	})
	if err != nil {
		return fmt.Errorf("proactive think execution: %w", err)
	}

	result := resp.Response
	if containsNoWork(result) {
		deps.logger.Debug().Msg("proactive think: no work needed")
		return nil
	}

	deps.logger.Info().Msg("proactive think: initiated work")

	// If work was started, allow autonomous continuation
	if deps.agentRecord.MaxAutoTurns > 1 {
		// Create a synthetic message for continuation tracking
		syntheticMsg := agent.Message{
			ID:     ulid.Make().String(),
			FromNS: deps.agentRecord.Namespace,
			ToNS:   deps.agentRecord.Namespace,
			Type:   agent.MessageTypeCmd,
		}
		runAutonomousContinuation(ctx, deps, syntheticMsg)
	}

	return nil
}

// containsTaskComplete checks if the agent signaled task completion.
func containsTaskComplete(result string) bool {
	return len(result) > 0 && (result == "TASK_COMPLETE" ||
		len(result) < 100 && (result == "Task completed" || result == "Done" || result == "Complete"))
}

// containsNoWork checks if the proactive agent found no work to do.
func containsNoWork(result string) bool {
	return result == "NO_WORK_NEEDED" || result == "No work needed"
}

func processMessage(
	ctx context.Context,
	logger zerolog.Logger,
	msg agent.Message,
	companionSvc ChatService,
	mailboxStore mailbox.Store,
	policy agent.Policy,
	optCtx *OptimizationContext,
	cancelCtx *CancelContext,
	agentID string, // needed for conversation scoping
) error {
	switch msg.Type {
	case agent.MessageTypeAsk:
		return handleAsk(ctx, logger, msg, companionSvc, mailboxStore, policy, optCtx, agentID)
	case agent.MessageTypeCmd:
		return handleCmd(ctx, logger, msg, companionSvc, policy, optCtx, agentID)
	case agent.MessageTypeEvent:
		return handleEvent(ctx, logger, msg)
	case agent.MessageTypeReply:
		logger.Info().Str("msg_id", msg.ID).Msg("received reply")
	case agent.MessageTypeConsoleAsk:
		return handleConsoleAsk(ctx, logger, msg, companionSvc, mailboxStore, policy, optCtx, cancelCtx, agentID)
	case agent.MessageTypeConsoleCmd:
		return handleConsoleCmd(ctx, logger, msg, cancelCtx)
	case agent.MessageTypeConsoleReply, agent.MessageTypeConsoleEvent:
		logger.Debug().Str("msg_id", msg.ID).Str("type", string(msg.Type)).Msg("received console response message")
	default:
		logger.Warn().Str("type", string(msg.Type)).Msg("unknown message type")
	}
	return nil
}

func isMessageExpired(msg agent.Message) bool {
	if msg.TTLMS <= 0 {
		return false
	}
	expiresAtMS := msg.Timestamp*1000 + msg.TTLMS
	return time.Now().UnixMilli() > expiresAtMS
}

type noopRecorder struct{}

func (r *noopRecorder) RecordToolCall(call types.ToolCall) {}
