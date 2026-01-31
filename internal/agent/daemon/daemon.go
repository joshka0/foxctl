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

	"github.com/XiaoConstantine/dspy-go/pkg/agents"
	"github.com/XiaoConstantine/dspy-go/pkg/agents/react"
	"github.com/XiaoConstantine/dspy-go/pkg/core"
	"github.com/XiaoConstantine/dspy-go/pkg/llms"
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

	// 4. Initialize agent engine
	// - Default (reactive mode): LLMChatEngine via companion.Service
	// - Autonomous/Proactive modes: DSPy ReAct (needs tool loop)
	traceID := ulid.Make().String()
	recorder := &noopRecorder{}

	var dspyAgent agents.Agent
	var companionSvc *companion.Service

	useDSPy := agentRecord.ExecMode == agent.ModeAutonomous || agentRecord.ExecMode == agent.ModeProactive

	if useDSPy {
		// Autonomous/proactive agents use DSPy ReAct for tool calling
		log.Info().Str("exec_mode", string(agentRecord.ExecMode)).Msg("using DSPy ReAct agent")

		toolsCfg := buildToolsConfig(opts, agentRecord, traceID, stores)
		toolsCfg.FilesystemPolicy = mapFilesystemPolicy(agentRecord.Policy)

		registry, err := tools.NewRegistry(toolsCfg, recorder)
		if err != nil {
			return fmt.Errorf("create tools registry: %w", err)
		}

		if opts.AgentFactory != nil {
			dspyAgent, err = opts.AgentFactory(ctx, agentRecord, registry)
		} else {
			dspyAgent, err = createAgent(ctx, agentRecord, registry, opts)
		}
		if err != nil {
			return fmt.Errorf("create dspy agent: %w", err)
		}
	} else {
		// Default: use LLMChatEngine via companion.Service
		log.Info().Msg("using LLMChatEngine via companion service")

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

		var extraToolExecutor engine.ToolExecutor
		extraToolsOnly := false
		if repoIndexOnlyAllowlist(agentRecord.SkillsAllow) {
			store, err := repoindex.Open(ctx, opts.StorageRoot, opts.WorkspaceRoot)
			if err != nil {
				return fmt.Errorf("open repoindex store: %w", err)
			}
			stores.repoIndexStore = store
			extraToolExecutor = engine.NewRepoIndexToolExecutor(store)
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
		opts:            opts,
		logger:          logger,
		agentRecord:     agentRecord,
		agentStore:      stores.agentStore,
		mailboxStore:    stores.mailboxStore,
		dedupeStore:     dedupeStore,
		dspyAgent:       dspyAgent,
		companionSvc:    companionSvc,
		cancelCtx:       cancelCtx,
		optCtx:          optCtx,
		companionMemory: stores.companionMemory,
	})
}

type pollDeps struct {
	opts            Options
	logger          zerolog.Logger
	agentRecord     agent.Agent
	agentStore      storagents.Store
	mailboxStore    mailbox.Store
	dedupeStore     DedupeStore
	dspyAgent       agents.Agent       // nil for companion agents
	companionSvc    *companion.Service // non-nil for companion agents
	cancelCtx       *CancelContext
	optCtx          *OptimizationContext
	companionMemory *companion.ConversationMemory
}

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
		repoindex.ToolSearch: {},
		repoindex.ToolExpand: {},
		repoindex.ToolOpen:   {},
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

		procErr := processMessage(ctx, deps.logger, msg, deps.dspyAgent, deps.companionSvc, deps.mailboxStore, currentAgent.Policy, deps.optCtx, deps.cancelCtx, deps.companionMemory, deps.opts.AgentID)
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
func runAutonomousContinuation(ctx context.Context, deps pollDeps, lastMsg agent.Message) {
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

		input := map[string]any{
			"task": continuePrompt,
		}

		timeout := 10 * time.Minute
		turnCtx, cancel := context.WithTimeout(ctx, timeout)
		resultMap, err := deps.dspyAgent.Execute(turnCtx, input)
		cancel()

		if err != nil {
			deps.logger.Debug().Err(err).Msg("autonomous continuation failed, stopping")
			break
		}

		// Check if agent signaled completion
		result := extractResult(resultMap)
		if containsTaskComplete(result) {
			deps.logger.Debug().Msg("agent signaled task complete, stopping continuation")
			break
		}
	}
}

// runProactiveThink runs a periodic "think" cycle for proactive agents.
// This allows the agent to check if there's work to do and initiate it.
func runProactiveThink(ctx context.Context, deps pollDeps) error {
	deps.logger.Debug().Msg("running proactive think cycle")

	// Proactive prompt: ask agent to check for work
	thinkPrompt := `You are in proactive mode. Check if there is any work that needs to be done:
1. Review any pending tasks or todos
2. Check for any issues that need attention
3. Look for opportunities to help

If there is work to do, start working on the highest priority item.
If there is nothing to do, respond with 'NO_WORK_NEEDED'.`

	input := map[string]any{
		"task": thinkPrompt,
	}

	timeout := 5 * time.Minute
	turnCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resultMap, err := deps.dspyAgent.Execute(turnCtx, input)
	if err != nil {
		return fmt.Errorf("proactive think execution: %w", err)
	}

	result := extractResult(resultMap)
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
	dspyAgent agents.Agent,
	companionSvc *companion.Service,
	mailboxStore mailbox.Store,
	policy agent.Policy,
	optCtx *OptimizationContext,
	cancelCtx *CancelContext,
	companionMemory *companion.ConversationMemory,
	agentID string, // needed for conversation scoping
) error {
	switch msg.Type {
	case agent.MessageTypeAsk:
		return handleAsk(ctx, logger, msg, dspyAgent, companionSvc, mailboxStore, policy, optCtx, companionMemory, agentID)
	case agent.MessageTypeCmd:
		return handleCmd(ctx, logger, msg, dspyAgent, companionSvc, policy, optCtx, agentID)
	case agent.MessageTypeEvent:
		return handleEvent(ctx, logger, msg)
	case agent.MessageTypeReply:
		logger.Info().Str("msg_id", msg.ID).Msg("received reply")
	case agent.MessageTypeConsoleAsk:
		return handleConsoleAsk(ctx, logger, msg, dspyAgent, companionSvc, mailboxStore, policy, optCtx, cancelCtx, companionMemory, agentID)
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

func createAgent(ctx context.Context, agentRecord agent.Agent, registry *tools.Registry, daemonOpts Options) (agents.Agent, error) {
	// Resolve max iterations: agent record takes precedence, else default to 10
	maxIterations := agentRecord.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 10
	}

	// Create options
	opts := []react.Option{
		react.WithMaxIterations(maxIterations),
		react.WithTimeout(10 * time.Minute),
	}

	agentID := fmt.Sprintf("%s:%s", agentRecord.Role, agentRecord.ID)
	dspyAgent := react.NewReActAgent(agentID, agentRecord.Role, opts...)

	// Initialize LLM
	llms.EnsureFactory()

	// Resolve LLM configuration: per-agent settings override daemon config defaults
	provider := agentRecord.LLMProvider
	if provider == "" {
		provider = daemonOpts.LLMProvider
	}
	if provider == "" {
		provider = "gemini"
	}

	model := agentRecord.LLMModel
	if model == "" {
		model = daemonOpts.LLMModel
	}
	if model == "" {
		switch provider {
		case "groq":
			model = "qwen/qwen3-32b"
		case "openrouter":
			model = "minimax/minimax-m2.1"
		case "anthropic":
			model = "claude-haiku-4-5"
		case "openai":
			model = "gpt-5.2"
		default:
			model = "gemini-3.0-flash"
		}
	}

	apiKey := agentRecord.LLMAPIKey
	if apiKey == "" {
		apiKey = daemonOpts.LLMAPIKey
	}

	if apiKey == "" {
		return nil, fmt.Errorf("LLM API key not set (use --llm-api-key or AGENTCTL_LLM_API_KEY)")
	}

	var llm core.LLM
	var err error
	switch provider {
	case "gemini":
		llm, err = llms.NewGeminiLLM(apiKey, core.ModelID(model))
	case "openai":
		llm, err = llms.NewOpenAILLM(core.ModelID(model), llms.WithAPIKey(apiKey))
	case "anthropic":
		// For Claude models via Anthropic API
		config := core.ProviderConfig{Name: "anthropic", APIKey: apiKey}
		llm, err = llms.NewAnthropicLLMFromConfig(ctx, config, core.ModelID(model))
	case "groq":
		// GROQ uses OpenAI-compatible API (dspy-go appends /v1/chat/completions)
		llm, err = llms.NewOpenAICompatible("groq", core.ModelID(model),
			"https://api.groq.com/openai", llms.WithAPIKey(apiKey))
	case "openrouter":
		// OpenRouter provides access to multiple models via OpenAI-compatible API
		llm, err = llms.NewOpenAICompatible("openrouter", core.ModelID(model),
			"https://openrouter.ai/api", llms.WithAPIKey(apiKey))
	case "cerebras":
		// Cerebras uses OpenAI-compatible API (dspy-go appends /v1/chat/completions)
		llm, err = llms.NewOpenAICompatible("cerebras", core.ModelID(model),
			"https://api.cerebras.ai", llms.WithAPIKey(apiKey))
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %q (supported: gemini, openai, anthropic, groq, openrouter, cerebras)", provider)
	}
	if err != nil {
		return nil, fmt.Errorf("create LLM: %w", err)
	}

	// Register tools BEFORE Initialize() so the signature includes tool descriptions.
	// The ReAct module builds its action description during Initialize(), so tools
	// must be registered first for the LLM to know they're available.
	for _, tool := range registry.List() {
		if err := dspyAgent.RegisterTool(tool); err != nil {
			return nil, fmt.Errorf("register tool %s: %w", tool.Name(), err)
		}
	}

	// Build signature
	sig := buildSignature(agentRecord)

	if err := dspyAgent.Initialize(llm, *sig); err != nil {
		return nil, fmt.Errorf("initialize agent: %w", err)
	}

	return dspyAgent, nil
}

func buildSignature(a agent.Agent) *core.Signature {
	instruction := fmt.Sprintf("You are a %s agent. %s", a.Role, a.Prompt)
	sig := core.NewSignature(
		[]core.InputField{
			{Field: core.NewField("task", core.WithDescription("The task to be completed by the agent"))},
		},
		[]core.OutputField{
			{Field: core.NewField("result", core.WithDescription("The final result or answer from completing the task"))},
		},
	).WithInstruction(instruction)
	return &sig
}