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

// Run starts the agent daemon.
func Run(ctx context.Context, opts Options) error {
	// 1. Open stores
	agentStore, err := storagents.Open(ctx, opts.StorageRoot)
	if err != nil {
		return fmt.Errorf("open agent store: %w", err)
	}
	defer agentStore.Close()

	mailboxStore, err := mailbox.Open(ctx, opts.StorageRoot)
	if err != nil {
		return fmt.Errorf("open mailbox store: %w", err)
	}
	defer mailboxStore.Close()

	tasksStore, err := tasks.Open(ctx, opts.StorageRoot)
	if err != nil {
		return fmt.Errorf("open tasks store: %w", err)
	}
	defer tasksStore.Close()

	boardStore, err := blackboard.OpenBoardStore(ctx, opts.StorageRoot)
	if err != nil {
		return fmt.Errorf("open board store: %w", err)
	}
	defer boardStore.Close()

	bbStore, err := blackboard.Open(ctx, opts.StorageRoot)
	if err != nil {
		return fmt.Errorf("open blackboard store: %w", err)
	}
	defer bbStore.Close()

	// Open optimization stores if enabled
	var optCtx *OptimizationContext
	if opts.EnableOptimization {
		trajStore, err := trajectory.Open(ctx, opts.StorageRoot)
		if err != nil {
			log.Warn().Err(err).Msg("failed to open trajectory store for optimization, disabling")
		} else {
			defer trajStore.Close()

			patternStore, err := optimization.OpenPatternStore(ctx, opts.StorageRoot)
			if err != nil {
				log.Warn().Err(err).Msg("failed to open pattern store for optimization, disabling")
			} else {
				defer patternStore.Close()

				collector := optimization.NewMCPPatternCollector(patternStore, trajStore)
				optCtx = &OptimizationContext{
					Collector: collector,
					Enabled:   true,
				}
				log.Info().Msg("optimization enabled: pattern learning active")
			}
		}
	}

	// 2. Load and validate agent
	agentRecord, err := agentStore.Get(ctx, opts.AgentID)
	if err != nil {
		return fmt.Errorf("get agent %s: %w", opts.AgentID, err)
	}
	if agentRecord.State == agent.StateStopped {
		return errors.New("agent is stopped")
	}

	// Populate optimization context with agent info
	if optCtx != nil {
		optCtx.AgentRole = agentRecord.Role
		optCtx.WorkspaceID = agentRecord.Namespace
	}

	// 3. Transition to running
	if err := agentStore.UpdateState(ctx, opts.AgentID, agent.StateRunning); err != nil {
		return fmt.Errorf("update agent state: %w", err)
	}

	// 4. Initialize tool registry + DSPy runtime
	traceID := ulid.Make().String()
	recorder := &noopRecorder{}

	toolsCfg := tools.Config{
		WorkspaceRoot: ".", // Default to current directory
		WorkspaceID:   agentRecord.Namespace,
		ActorID:       "actor:agent:" + opts.AgentID,
		TraceID:       traceID,
		Allowlist:     agentRecord.SkillsAllow,
		MaxOutputSize: agentRecord.Policy.MaxOutputKB * 1024,
		OpenTasksStore: func(ctx context.Context) (tasks.Store, error) {
			return tasksStore, nil
		},
		OpenBoardStore: func(ctx context.Context) (blackboard.BoardStore, error) {
			return boardStore, nil
		},
		OpenBlackboardStore: func(ctx context.Context) (blackboard.Store, error) {
			return bbStore, nil
		},
		OpenCASStore: func(ctx context.Context) (storage.CASStore, error) {
			return cas.OpenDefault(ctx, opts.StorageRoot)
		},
	}

	// Map filesystem policy
	toolsCfg.FilesystemPolicy = "workspace"
	for _, fs := range agentRecord.Policy.Filesystem {
		switch fs.Type {
		case "home":
			if toolsCfg.FilesystemPolicy == "tmp" {
				toolsCfg.FilesystemPolicy = "all"
			} else if toolsCfg.FilesystemPolicy != "all" {
				toolsCfg.FilesystemPolicy = "home"
			}
		case "tmp":
			if toolsCfg.FilesystemPolicy == "home" {
				toolsCfg.FilesystemPolicy = "all"
			} else if toolsCfg.FilesystemPolicy != "all" {
				toolsCfg.FilesystemPolicy = "tmp"
			}
		}
	}

	registry, err := tools.NewRegistry(toolsCfg, recorder)
	if err != nil {
		return fmt.Errorf("create tools registry: %w", err)
	}

	// Create DSPy agent
	var dspyAgent agents.Agent
	if opts.AgentFactory != nil {
		dspyAgent, err = opts.AgentFactory(ctx, agentRecord, registry)
	} else {
		dspyAgent, err = createAgent(ctx, agentRecord, registry)
	}
	if err != nil {
		return fmt.Errorf("create dspy agent: %w", err)
	}

	// 5. Start heartbeat ticker
	heartbeatTicker := time.NewTicker(opts.HeartbeatInterval)
	defer heartbeatTicker.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-heartbeatTicker.C:
				if err := agentStore.UpdateHeartbeat(ctx, opts.AgentID); err != nil {
					log.Error().Err(err).Msg("heartbeat update failed")
				}
			}
		}
	}()

	// Dedupe store
	var dedupeStore DedupeStore
	if opts.UseMemoryDedupe {
		dedupeStore = NewMemoryDedupeStore()
	} else {
		sqliteStore, err := OpenSQLiteDedupeStore(ctx, opts.StorageRoot)
		if err != nil {
			return fmt.Errorf("open dedupe store: %w", err)
		}
		defer sqliteStore.Close()
		dedupeStore = sqliteStore

		// Cleanup records older than 7 days on startup
		if cleaned, err := sqliteStore.Cleanup(ctx, 7*24*time.Hour); err != nil {
			log.Warn().Err(err).Msg("dedupe cleanup failed")
		} else if cleaned > 0 {
			log.Info().Int64("cleaned", cleaned).Msg("dedupe cleanup completed")
		}
	}

	// 6. Enter poll loop
	pollTicker := time.NewTicker(opts.PollInterval)
	defer pollTicker.Stop()

	// Logging setup
	logger := zerolog.New(os.Stderr).With().
		Str("agent_id", opts.AgentID).
		Str("trace_id", traceID).
		Logger()

	logger.Info().Msg("daemon started")

	for {
		select {
		case <-ctx.Done():
			// Update state on exit
			// Use background context for cleanup
			_ = agentStore.UpdateState(context.Background(), opts.AgentID, agent.StateStopped) //nolint:errcheck
			return ctx.Err()

		case <-pollTicker.C:
			// Check if stopped
			currentAgent, err := agentStore.Get(ctx, opts.AgentID)
			if err != nil {
				logger.Error().Err(err).Msg("failed to get agent state")
				continue
			}
			if currentAgent.State == agent.StateStopped {
				logger.Info().Msg("agent state is stopped, exiting daemon")
				return nil
			}

			messages, err := mailboxStore.Poll(ctx, agentRecord.Namespace, 0, opts.MaxPollMessages)
			if err != nil {
				logger.Error().Err(err).Msg("poll failed")
				continue
			}

			for _, msg := range messages {
				// Dedupe - on error, Nack so message can be retried rather than dropped
				processed, err := dedupeStore.IsProcessed(ctx, opts.AgentID, msg.ID)
				if err != nil {
					logger.Warn().Err(err).Str("msg_id", msg.ID).Msg("dedupe check failed, nacking for retry")
					if nackErr := mailboxStore.Nack(ctx, msg.ID, 30*time.Second); nackErr != nil {
						logger.Error().Err(nackErr).Str("msg_id", msg.ID).Msg("failed to nack message")
					}
					continue
				}
				if processed {
					logger.Debug().Str("msg_id", msg.ID).Msg("duplicate message, acking")
					_ = mailboxStore.Ack(ctx, msg.ID) //nolint:errcheck
					continue
				}

				// TTL check
				if msg.TTLMS > 0 {
					expiresAtMS := msg.Timestamp*1000 + msg.TTLMS
					if time.Now().UnixMilli() > expiresAtMS {
						logger.Warn().Str("msg_id", msg.ID).Msg("message expired, acking without processing")
						_ = mailboxStore.Ack(ctx, msg.ID) //nolint:errcheck
						continue
					}
				}

				// Process
				var procErr error
				switch msg.Type {
				case agent.MessageTypeAsk:
					procErr = handleAsk(ctx, logger, msg, dspyAgent, mailboxStore, currentAgent.Policy, optCtx)
				case agent.MessageTypeCmd:
					procErr = handleCmd(ctx, logger, msg, dspyAgent, currentAgent.Policy, optCtx)
				case agent.MessageTypeEvent:
					procErr = handleEvent(ctx, logger, msg)
				case agent.MessageTypeReply:
					logger.Info().Str("msg_id", msg.ID).Msg("received reply")
				default:
					logger.Warn().Str("type", string(msg.Type)).Msg("unknown message type")
				}

				if procErr != nil {
					logger.Error().Err(procErr).Str("msg_id", msg.ID).Msg("processing failed")
					_ = mailboxStore.Nack(ctx, msg.ID, backoffDuration(msg.Attempt)) //nolint:errcheck
				} else {
					_ = dedupeStore.MarkProcessed(ctx, opts.AgentID, msg.ID) //nolint:errcheck
					_ = mailboxStore.Ack(ctx, msg.ID)                        //nolint:errcheck
				}
			}
		}
	}
}

type noopRecorder struct{}

func (r *noopRecorder) RecordToolCall(call types.ToolCall) {}

func createAgent(ctx context.Context, agentRecord agent.Agent, registry *tools.Registry) (agents.Agent, error) {
	// Create options
	opts := []react.Option{
		react.WithMaxIterations(10),
		react.WithTimeout(10 * time.Minute),
	}

	agentID := fmt.Sprintf("%s:%s", agentRecord.Role, agentRecord.ID)
	dspyAgent := react.NewReActAgent(agentID, agentRecord.Role, opts...)

	// Initialize LLM
	llms.EnsureFactory()

	// Resolve LLM configuration: per-agent settings override environment defaults
	provider := agentRecord.LLMProvider
	if provider == "" {
		provider = os.Getenv("AGENTCTL_LLM_PROVIDER")
	}
	if provider == "" {
		provider = "gemini"
	}

	model := agentRecord.LLMModel
	if model == "" {
		model = os.Getenv("AGENTCTL_LLM_MODEL")
	}
	if model == "" {
		// Default to latest stable Gemini model
		model = "gemini-2.0-flash"
	}

	apiKey := agentRecord.LLMAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("AGENTCTL_LLM_API_KEY")
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
