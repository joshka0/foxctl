package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/storage"
	storagents "github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/jkatigb/agentctl/internal/storage/mailbox"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

// messageLeaseDuration is the default lease timeout for message polling.
// This duration should align with the visibility timeout used for Nack operations
// to ensure consistent message handling semantics.
const messageLeaseDuration = 30 * time.Second

type daemonStores struct {
	agentStore   storagents.Store
	mailboxStore mailbox.Store
	tasksStore   tasks.Store
	boardStore   blackboard.BoardStore
	bbStore      blackboard.Store
}

func openDaemonStores(ctx context.Context, root string) (*daemonStores, error) {
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

	return stores, nil
}

func (s *daemonStores) Close() {
	if s == nil {
		return
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
	// 1. Open stores
	stores, err := openDaemonStores(ctx, opts.StorageRoot)
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

	// 4. Initialize tool registry + DSPy runtime
	traceID := ulid.Make().String()
	recorder := &noopRecorder{}

	toolsCfg := buildToolsConfig(opts, agentRecord, traceID, stores)
	toolsCfg.FilesystemPolicy = mapFilesystemPolicy(agentRecord.Policy)

	registry, err := tools.NewRegistry(toolsCfg, recorder)
	if err != nil {
		return fmt.Errorf("create tools registry: %w", err)
	}

	// Create DSPy agent
	var dspyAgent agents.Agent
	if opts.AgentFactory != nil {
		dspyAgent, err = opts.AgentFactory(ctx, agentRecord, registry)
	} else {
		dspyAgent, err = createAgent(ctx, agentRecord, registry, opts)
	}
	if err != nil {
		return fmt.Errorf("create dspy agent: %w", err)
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
		dspyAgent:    dspyAgent,
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
	dspyAgent    agents.Agent
	cancelCtx    *CancelContext
	optCtx       *OptimizationContext
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
	return tools.Config{
		WorkspaceRoot: ".", // Default to current directory
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

	deps.logger.Info().Msg("daemon started")

	for {
		select {
		case <-ctx.Done():
			_ = deps.agentStore.UpdateState(context.Background(), deps.opts.AgentID, agent.StateStopped) //nolint:errcheck
			return ctx.Err()

		case <-pollTicker.C:
			currentAgent, err := deps.agentStore.Get(ctx, deps.opts.AgentID)
			if err != nil {
				deps.logger.Error().Err(err).Msg("failed to get agent state")
				continue
			}
			if currentAgent.State == agent.StateStopped {
				deps.logger.Info().Msg("agent state is stopped, exiting daemon")
				return nil
			}

			messages, err := deps.mailboxStore.Poll(ctx, deps.agentRecord.Namespace, messageLeaseDuration, deps.opts.MaxPollMessages)
			if err != nil {
				deps.logger.Error().Err(err).Msg("poll failed")
				continue
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

				procErr := processMessage(ctx, deps.logger, msg, deps.dspyAgent, deps.mailboxStore, currentAgent.Policy, deps.optCtx, deps.cancelCtx)
				if procErr != nil {
					deps.logger.Error().Err(procErr).Str("msg_id", msg.ID).Msg("processing failed")
					_ = deps.mailboxStore.Nack(ctx, msg.ID, backoffDuration(msg.Attempt)) //nolint:errcheck
				} else {
					_ = deps.dedupeStore.MarkProcessed(ctx, deps.opts.AgentID, msg.ID) //nolint:errcheck
					_ = deps.mailboxStore.Ack(ctx, msg.ID)                             //nolint:errcheck
				}
			}
		}
	}
}

func processMessage(
	ctx context.Context,
	logger zerolog.Logger,
	msg agent.Message,
	dspyAgent agents.Agent,
	mailboxStore mailbox.Store,
	policy agent.Policy,
	optCtx *OptimizationContext,
	cancelCtx *CancelContext,
) error {
	switch msg.Type {
	case agent.MessageTypeAsk:
		return handleAsk(ctx, logger, msg, dspyAgent, mailboxStore, policy, optCtx)
	case agent.MessageTypeCmd:
		return handleCmd(ctx, logger, msg, dspyAgent, policy, optCtx)
	case agent.MessageTypeEvent:
		return handleEvent(ctx, logger, msg)
	case agent.MessageTypeReply:
		logger.Info().Str("msg_id", msg.ID).Msg("received reply")
	case agent.MessageTypeConsoleAsk:
		return handleConsoleAsk(ctx, logger, msg, dspyAgent, mailboxStore, policy, optCtx, cancelCtx)
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
	// Create options
	opts := []react.Option{
		react.WithMaxIterations(10),
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
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %q (supported: gemini, openai, anthropic, groq, openrouter)", provider)
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
