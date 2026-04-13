package actor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/actor/memory"
	agenttools "github.com/jkatigb/agentctl/internal/agent/tools"
	agenttypes "github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/runtime/agentprompt"
	agentdomain "github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/runtime/engine"
	"github.com/jkatigb/agentctl/internal/runtime/hooks"
	"github.com/jkatigb/agentctl/internal/storage"
)

// AgentActor wraps an LLMChatEngine as a reactive actor.
//
// It provides message-driven execution where each agent.ask or agent.cmd
// message triggers a single LLMChat turn. The actor integrates with
// ShortTermMemory for progressive context management.
//
// See docs/designs/reactive-actor-system.md for the full design.
type AgentActor struct {
	*BaseActor

	// agentEngine is the execution engine for this actor.
	agentEngine engine.AgentEngine

	// toolRunner is the tool runner for the llmchat engine (stored for SessionID updates)
	toolRunner *engine.ToolRunner

	// toolsRegistry holds available tools
	toolsRegistry *agenttools.Registry

	// agentConfig holds agent-specific configuration
	agentConfig agenttypes.AgentConfig

	// shortTermMem manages progressive context (always enabled)
	shortTermMem *memory.ShortTermMemory

	// hooks dispatches hook events at canonical points
	hooks hooks.Dispatcher

	// workspaceRoot for hook context
	workspaceRoot string

	// sessionID for hook context (set in onStart)
	sessionID string

	// logger for structured logging
	logger zerolog.Logger

	// replySender is injected by supervisor for sending replies
	sendReply func(ctx context.Context, msg *Message) error

	// cancelMu protects cancelFuncs map
	cancelMu sync.Mutex
	// cancelFuncs maps askID to cancel function for in-flight console asks
	cancelFuncs map[string]context.CancelFunc

	// clock provides current time (injectable for testing/determinism)
	clock func() time.Time
}

// AgentActorConfig holds configuration for creating a AgentActor.
type AgentActorConfig struct {
	// ActorConfig is the base actor configuration
	ActorConfig Config

	// AgentConfig is the agent configuration
	AgentConfig agenttypes.AgentConfig

	// LLMProvider is the LLM provider (gemini, openai, anthropic, groq, openrouter)
	LLMProvider string

	// LLMModel is the model name
	LLMModel string

	// LLMAPIKey is the API key for the LLM provider
	LLMAPIKey string

	// WorkspaceRoot is the workspace directory
	WorkspaceRoot string

	// ShortTermMemory provides progressive context management (optional)
	// FC/IS: Caller constructs memory at boundary, actor uses abstraction.
	ShortTermMemory *memory.ShortTermMemory

	// OpenMemoryStore provides access to named memory for retrieval tools
	OpenMemoryStore func(context.Context) (storage.MemoryStore, error)

	// TrajectoryStorageRoot enables agent tool call capture when set
	TrajectoryStorageRoot string

	// Hooks is the dispatcher for hook events (optional)
	Hooks hooks.Dispatcher

	// Logger for structured logging
	Logger zerolog.Logger
}

// AgentActorOption configures a AgentActor.
type AgentActorOption func(*AgentActor)

// WithAgentLogger sets the logger for the AgentActor.
func WithAgentLogger(logger zerolog.Logger) AgentActorOption {
	return func(a *AgentActor) {
		a.logger = logger
	}
}

// WithAgentClock sets the clock function for the AgentActor (for testing/determinism).
func WithAgentClock(clock func() time.Time) AgentActorOption {
	return func(a *AgentActor) {
		a.clock = clock
	}
}

// NewAgentActor creates a new AgentActor with the given configuration.
func NewAgentActor(cfg AgentActorConfig, opts ...AgentActorOption) (*AgentActor, error) {
	// Create base actor with lifecycle hooks
	baseActor := NewBaseActor(cfg.ActorConfig)

	actor := &AgentActor{
		BaseActor:     baseActor,
		agentConfig:   cfg.AgentConfig,
		hooks:         cfg.Hooks,
		workspaceRoot: cfg.WorkspaceRoot,
		logger:        cfg.Logger,
		cancelFuncs:   make(map[string]context.CancelFunc),
		clock:         func() time.Time { return time.Now() },
	}

	// Apply options
	for _, opt := range opts {
		opt(actor)
	}

	// Set up lifecycle hooks
	baseActor.onStart = actor.onStart
	baseActor.onStop = actor.onStop

	// Initialize tools first (needed by both engines)
	if err := actor.initializeTools(cfg); err != nil {
		return nil, fmt.Errorf("initialize tools: %w", err)
	}

	// Initialize engine
	if err := actor.initializeLLMChatEngine(cfg); err != nil {
		return nil, fmt.Errorf("initialize llmchat engine: %w", err)
	}

	// Use pre-constructed short-term memory if provided
	if cfg.ShortTermMemory != nil {
		actor.shortTermMem = cfg.ShortTermMemory
	}

	// Register message handlers
	actor.RegisterHandler("agent.ask", actor.handleAsk)
	actor.RegisterHandler("agent.cmd", actor.handleCmd)
	actor.RegisterHandler("agent.event", actor.handleEvent)

	// Register console handlers
	actor.RegisterHandler("console.ask", actor.handleConsoleAsk)
	actor.RegisterHandler("console.cmd", actor.handleConsoleCmd)

	return actor, nil
}

// initializeTools sets up the tools registry.
func (a *AgentActor) initializeTools(cfg AgentActorConfig) error {
	toolsCfg := agenttools.Config{
		WorkspaceRoot:         cfg.WorkspaceRoot,
		WorkspaceID:           cfg.AgentConfig.WorkspaceID,
		ActorID:               cfg.AgentConfig.ActorID,
		TaskID:                cfg.AgentConfig.TaskID,
		EpicID:                cfg.AgentConfig.EpicID,
		Depth:                 cfg.AgentConfig.Depth,
		MaxDepth:              cfg.AgentConfig.MaxDepth,
		LocalMaxDepth:         cfg.AgentConfig.LocalMaxDepth,
		AgentRole:             string(cfg.AgentConfig.Role),
		TrajectoryStorageRoot: cfg.TrajectoryStorageRoot,
		HookDispatcher:        cfg.Hooks,
	}

	if cfg.OpenMemoryStore != nil {
		toolsCfg.OpenMemoryStore = cfg.OpenMemoryStore
	}

	registry, err := agenttools.NewRegistry(toolsCfg, nil)
	if err != nil {
		return fmt.Errorf("create tools registry: %w", err)
	}

	a.toolsRegistry = registry
	return nil
}

// initializeLLMChatEngine sets up the LLMChatEngine for agent execution.
func (a *AgentActor) initializeLLMChatEngine(cfg AgentActorConfig) error {
	// Create LLMChatEngine config
	engineCfg := engine.DefaultLLMChatConfig()
	engineCfg.Provider = cfg.LLMProvider
	engineCfg.APIKey = cfg.LLMAPIKey
	engineCfg.Model = cfg.LLMModel
	engineCfg.HookDispatcher = cfg.Hooks

	// Set max iterations from agent config
	if cfg.AgentConfig.MaxIterations > 0 {
		engineCfg.MaxIterations = cfg.AgentConfig.MaxIterations
	}

	// Set timeout
	if cfg.AgentConfig.Timeout > 0 {
		engineCfg.Timeout = cfg.AgentConfig.Timeout
	}

	// Create the engine
	llmEngine, err := engine.NewLLMChatEngine(engineCfg)
	if err != nil {
		return fmt.Errorf("create llmchat engine: %w", err)
	}

	// Create tool executor adapter from registry
	toolExecutor := agenttools.NewRegistryToolExecutor(a.toolsRegistry)

	// Create tool runner with hook integration
	// SessionID is set in onStart when the session is generated
	toolRunner := engine.NewToolRunner(toolExecutor, cfg.Hooks, engine.ToolRunnerConfig{
		Workspace:   cfg.WorkspaceRoot,
		WorkspaceID: cfg.AgentConfig.WorkspaceID,
		SessionID:   "", // Set in onStart when sessionID is generated
		ActorID:     cfg.ActorConfig.ID,
	})

	// Store tool runner for later SessionID update
	a.toolRunner = toolRunner

	// Wire tool runner into engine
	llmEngine.SetToolRunner(toolRunner)

	a.agentEngine = llmEngine
	return nil
}

// onStart is called when the actor starts.
func (a *AgentActor) onStart(ctx context.Context) error {
	// Generate session ID for this actor session
	a.sessionID = ulid.Make().String()

	// Update tool runner with session ID (if using llmchat engine)
	if a.toolRunner != nil {
		a.toolRunner.SetSessionID(a.sessionID)
	}
	if a.toolsRegistry != nil {
		a.toolsRegistry.SetSessionID(a.sessionID)
	}

	// Initialize memory state for this actor
	if a.shortTermMem != nil {
		if err := a.shortTermMem.InitState(ctx, a.ID(), a.sessionID); err != nil {
			a.logger.Warn().Err(err).Msg("failed to initialize memory state")
		}
	}

	// Dispatch SessionStart hook
	a.dispatchHook(ctx, hooks.EventSessionStart, nil)

	a.logger.Info().
		Str("actor_id", a.ID()).
		Str("namespace", a.Namespace()).
		Str("session_id", a.sessionID).
		Str("role", string(a.agentConfig.Role)).
		Msg("AgentActor started")

	return nil
}

// onStop is called when the actor stops.
func (a *AgentActor) onStop(ctx context.Context) error {
	// Dispatch SessionEnd hook
	a.dispatchHook(ctx, hooks.EventSessionEnd, nil)

	a.logger.Info().
		Str("actor_id", a.ID()).
		Str("session_id", a.sessionID).
		Msg("AgentActor stopped")
	return nil
}

// handleAsk processes an agent.ask message.
func (a *AgentActor) handleAsk(ctx context.Context, msg *Message) (*Message, error) {
	// Parse payload envelope
	var env struct {
		Data agentdomain.AskData `json:"data"`
	}
	if err := json.Unmarshal(msg.Body, &env); err != nil {
		return nil, fmt.Errorf("unmarshal ask payload: %w", err)
	}
	askData := env.Data

	// Dispatch MessageReceived hook
	msgResult := a.dispatchHook(ctx, hooks.EventMessageReceived, map[string]any{
		"correlation_id": askData.AskID,
		"mailbox_message": &hooks.MailboxMessage{
			ID:      msg.ID,
			FromNS:  msg.FromNS,
			ToNS:    msg.ToNS,
			Type:    msg.Subject,
			Payload: msg.Body,
		},
	})
	if msgResult.Blocked {
		return nil, fmt.Errorf("blocked by hook: %s", msgResult.Output.Reason)
	}

	// Build prompt from ask
	prompt := fmt.Sprintf("Question: %s\nContext: %v", askData.Question, askData.Context)

	// Add memory context if available
	if a.shortTermMem != nil {
		memContext, err := a.shortTermMem.GetContext(ctx, a.ID())
		if err == nil && memContext != "" {
			prompt = memContext + "\n\n---\n\n" + prompt
		}
	}
	basePrompt := prompt

	// Apply timeout
	timeout := 10 * time.Minute
	if a.agentConfig.Timeout > 0 {
		timeout = a.agentConfig.Timeout
	}
	turnCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var result string
	continuation := ""

	for {
		prompt := basePrompt
		if continuation != "" {
			prompt += "\n\n---\n\n" + continuation
		}

		// Generate turn ID for correlation
		turnID := ulid.Make().String()

		// Dispatch LLMRequest hook
		llmReqResult := a.dispatchHook(ctx, hooks.EventLLMRequest, map[string]any{
			"turn_id":        turnID,
			"correlation_id": askData.AskID,
			"prompt":         prompt,
		})
		if llmReqResult.Blocked {
			return nil, fmt.Errorf("blocked by LLMRequest hook: %s", llmReqResult.Output.Reason)
		}

		// Execute turn using the LLMChatEngine
		var execErr error
		result = ""

		if a.agentEngine == nil {
			execErr = fmt.Errorf("agent engine not configured")
		} else {
			engineInput := engine.EngineInput{
				Messages:     []engine.Message{engine.NewUserMessage(prompt)},
				Tools:        a.getEngineTools(),
				SystemPrompt: buildSystemPromptString(a.agentConfig.Role),
				Workspace:    a.workspaceRoot,
				SessionID:    a.sessionID,
				ActorID:      a.ID(),
				TurnID:       turnID,
			}
			output, err := a.agentEngine.Run(turnCtx, engineInput)
			if err != nil {
				execErr = err
			} else if output.StopReason == engine.StopReasonError {
				execErr = fmt.Errorf("engine error: %s", output.Error)
			} else {
				result = output.AssistantText
			}
		}

		// Dispatch LLMResponse hook (even on error)
		a.dispatchHook(ctx, hooks.EventLLMResponse, map[string]any{
			"turn_id":        turnID,
			"correlation_id": askData.AskID,
			"prompt":         prompt,
			"assistant_text": result,
		})

		if execErr != nil {
			return nil, fmt.Errorf("execution failed: %w", execErr)
		}

		stopResult := a.dispatchHook(ctx, hooks.EventStopRequested, map[string]any{
			"turn_id":        turnID,
			"correlation_id": askData.AskID,
			"prompt":         prompt,
			"assistant_text": result,
		})
		if stopResult.Blocked {
			continuation = buildStopContinuation(result, stopResult.Output.Context)
			if continuation == "" {
				return nil, fmt.Errorf("stop blocked without continuation context: %s", stopResult.Output.Reason)
			}
			continue
		}

		// Record turn in memory
		if a.shortTermMem != nil {
			turn := memory.Turn{
				Role:      "assistant",
				Content:   result,
				Timestamp: time.Now(),
			}
			if err := a.shortTermMem.AppendTurn(ctx, a.ID(), turn); err != nil {
				a.logger.Warn().Err(err).Msg("failed to append turn to memory")
			}
		}

		// Dispatch PostAgentTurn hook
		turnResult := a.dispatchHook(ctx, hooks.EventPostAgentTurn, map[string]any{
			"turn_id":        turnID,
			"correlation_id": askData.AskID,
			"assistant_text": result,
		})
		// Apply updated assistant text if hook modified it
		if turnResult.Output.UpdatedAssistantText != "" {
			result = turnResult.Output.UpdatedAssistantText
		}

		break
	}

	// Build reply
	replyData := agentdomain.ReplyData{
		AskID:  askData.AskID,
		Answer: map[string]any{"response": result},
	}
	replyEnv := envelope.OK("agent.reply", replyData)
	replyPayload, err := json.Marshal(replyEnv)
	if err != nil {
		return nil, fmt.Errorf("marshal reply envelope: %w", err)
	}

	return &Message{
		ID:        ulid.Make().String(),
		Subject:   "agent.reply",
		Body:      replyPayload,
		CreatedAt: a.clock(),
	}, nil
}

// handleCmd processes an agent.cmd message.
func (a *AgentActor) handleCmd(ctx context.Context, msg *Message) (*Message, error) {
	var env struct {
		Data agentdomain.CmdData `json:"data"`
	}
	if err := json.Unmarshal(msg.Body, &env); err != nil {
		return nil, fmt.Errorf("unmarshal cmd payload: %w", err)
	}
	cmdData := env.Data

	switch cmdData.Action {
	case "run_skill":
		return nil, fmt.Errorf("run_skill not yet implemented")
	case "run_turn", "do_work":
		// Execute a single LLMChat turn with cmdData.Args as context
		prompt := fmt.Sprintf("Command: %s\nArgs: %v", cmdData.Action, cmdData.Args)

		timeout := 10 * time.Minute
		if a.agentConfig.Timeout > 0 {
			timeout = a.agentConfig.Timeout
		}
		turnCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		if a.agentEngine == nil {
			return nil, fmt.Errorf("agent engine not configured")
		}

		engineInput := engine.EngineInput{
			Messages:     []engine.Message{engine.NewUserMessage(prompt)},
			Tools:        a.getEngineTools(),
			SystemPrompt: buildSystemPromptString(a.agentConfig.Role),
			Workspace:    a.workspaceRoot,
			SessionID:    a.sessionID,
			ActorID:      a.ID(),
			TurnID:       ulid.Make().String(),
		}
		output, err := a.agentEngine.Run(turnCtx, engineInput)
		if err != nil {
			return nil, fmt.Errorf("engine execution failed: %w", err)
		}
		if output.StopReason == engine.StopReasonError {
			return nil, fmt.Errorf("engine error: %s", output.Error)
		}

		return nil, nil // Commands are fire-and-forget
	default:
		return nil, fmt.Errorf("unknown action: %s", cmdData.Action)
	}
}

// handleEvent processes an agent.event message.
func (a *AgentActor) handleEvent(ctx context.Context, msg *Message) (*Message, error) {
	var env struct {
		Data agentdomain.EventData `json:"data"`
	}
	if err := json.Unmarshal(msg.Body, &env); err != nil {
		return nil, fmt.Errorf("unmarshal event payload: %w", err)
	}
	eventData := env.Data

	// MVP: just log it
	a.logger.Info().
		Str("event_id", eventData.EventID).
		Str("kind", eventData.Kind).
		Int("job_count", eventData.JobCount).
		Msg("received agent event")

	return nil, nil
}

// handleConsoleAsk processes a console.ask message from TUI/API.
func (a *AgentActor) handleConsoleAsk(ctx context.Context, msg *Message) (*Message, error) {
	// Parse envelope
	var env struct {
		Data agentdomain.ConsoleAskData `json:"data"`
	}
	if err := json.Unmarshal(msg.Body, &env); err != nil {
		return nil, fmt.Errorf("unmarshal console ask: %w", err)
	}
	askData := env.Data

	// Get correlation ID from header or use ask ID
	correlID := msg.Headers["correlation"]
	if correlID == "" {
		correlID = askData.AskID
	}

	// Dispatch MessageReceived hook
	msgResult := a.dispatchHook(ctx, hooks.EventMessageReceived, map[string]any{
		"correlation_id": correlID,
		"mailbox_message": &hooks.MailboxMessage{
			ID:      msg.ID,
			FromNS:  msg.FromNS,
			ToNS:    msg.ToNS,
			Type:    msg.Subject,
			Payload: msg.Body,
		},
	})
	if msgResult.Blocked {
		return nil, fmt.Errorf("blocked by hook: %s", msgResult.Output.Reason)
	}

	// Create cancellable context with timeout
	timeout := 10 * time.Minute
	if a.agentConfig.Timeout > 0 {
		timeout = a.agentConfig.Timeout
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)

	// Track cancel function for this ask
	a.cancelMu.Lock()
	a.cancelFuncs[askData.AskID] = cancel
	a.cancelMu.Unlock()

	defer func() {
		a.cancelMu.Lock()
		delete(a.cancelFuncs, askData.AskID)
		a.cancelMu.Unlock()
		cancel()
	}()

	startTime := time.Now()

	// Build prompt with memory context
	prompt := askData.Prompt
	if a.shortTermMem != nil {
		if memCtx, err := a.shortTermMem.GetContext(ctx, a.ID()); err == nil && memCtx != "" {
			prompt = memCtx + "\n\n---\n\n" + prompt
		}
	}

	// Emit start event
	a.emitConsoleEvent(msg.FromNS, askData.AskID, correlID, 1, 0, "progress", "Starting execution...")

	// Generate turn ID for correlation
	turnID := ulid.Make().String()

	// Dispatch LLMRequest hook
	llmReqResult := a.dispatchHook(ctx, hooks.EventLLMRequest, map[string]any{
		"turn_id":        turnID,
		"correlation_id": correlID,
		"prompt":         prompt,
	})
	if llmReqResult.Blocked {
		return nil, fmt.Errorf("blocked by LLMRequest hook: %s", llmReqResult.Output.Reason)
	}

	// Execute turn using configured engine
	var response string
	var status string
	var execErr error

	if a.agentEngine == nil {
		execErr = fmt.Errorf("agent engine not configured")
	} else {
		engineInput := engine.EngineInput{
			Messages:     []engine.Message{engine.NewUserMessage(prompt)},
			Tools:        a.getEngineTools(),
			SystemPrompt: buildSystemPromptString(a.agentConfig.Role),
			Workspace:    a.workspaceRoot,
			SessionID:    a.sessionID,
			ActorID:      a.ID(),
			TurnID:       turnID,
		}
		output, err := a.agentEngine.Run(execCtx, engineInput)
		if err != nil {
			execErr = err
		} else if output.StopReason == engine.StopReasonError {
			execErr = fmt.Errorf("engine error: %s", output.Error)
		} else {
			response = output.AssistantText
			status = "ok"
		}
	}

	// Handle errors
	if execErr != nil {
		if errors.Is(execErr, context.Canceled) {
			response = "Cancelled by user"
			status = "cancelled"
		} else if errors.Is(execErr, context.DeadlineExceeded) {
			response = "Execution timed out"
			status = "error"
		} else {
			response = fmt.Sprintf("Error: %v", execErr)
			status = "error"
		}
	}

	// Dispatch LLMResponse hook (even on error)
	a.dispatchHook(ctx, hooks.EventLLMResponse, map[string]any{
		"turn_id":        turnID,
		"correlation_id": correlID,
		"prompt":         prompt,
		"assistant_text": response,
	})

	// Emit completion event
	a.emitConsoleEvent(msg.FromNS, askData.AskID, correlID, 2, 0, "progress",
		fmt.Sprintf("Completed in %v", time.Since(startTime).Round(time.Millisecond)))

	// Record in memory
	if a.shortTermMem != nil && status == "ok" {
		turn := memory.Turn{Role: "assistant", Content: response, Timestamp: time.Now()}
		if err := a.shortTermMem.AppendTurn(ctx, a.ID(), turn); err != nil {
			a.logger.Warn().Err(err).Msg("failed to append turn to memory")
		}
	}

	// Dispatch PostAgentTurn hook (only on success)
	if status == "ok" {
		turnResult := a.dispatchHook(ctx, hooks.EventPostAgentTurn, map[string]any{
			"turn_id":        turnID,
			"correlation_id": correlID,
			"assistant_text": response,
		})
		// Apply updated assistant text if hook modified it
		if turnResult.Output.UpdatedAssistantText != "" {
			response = turnResult.Output.UpdatedAssistantText
		}
	}

	// Build console.reply
	replyData := agentdomain.ConsoleReplyData{
		AskID:    askData.AskID,
		Response: response,
		Status:   status,
		Metrics: map[string]any{
			"duration_ms": time.Since(startTime).Milliseconds(),
		},
	}
	replyEnv := envelope.OK("console.reply", replyData)
	replyPayload, err := json.Marshal(replyEnv)
	if err != nil {
		return nil, fmt.Errorf("marshal console reply: %w", err)
	}

	return &Message{
		ID:        ulid.Make().String(),
		Subject:   "console.reply",
		Body:      replyPayload,
		CreatedAt: time.Now(),
		Headers:   map[string]string{"correlation": correlID},
	}, nil
}

// handleConsoleCmd processes a console.cmd message (e.g., cancel).
func (a *AgentActor) handleConsoleCmd(ctx context.Context, msg *Message) (*Message, error) {
	var env struct {
		Data agentdomain.ConsoleCmdData `json:"data"`
	}
	if err := json.Unmarshal(msg.Body, &env); err != nil {
		return nil, fmt.Errorf("unmarshal console cmd: %w", err)
	}
	cmdData := env.Data

	switch cmdData.Action {
	case "cancel":
		// Find and cancel the in-flight ask
		askID := cmdData.AskID
		if askID == "" {
			askID = msg.Headers["ask_id"]
		}
		if askID == "" {
			a.logger.Warn().Msg("cancel command missing ask_id")
			return nil, nil
		}

		a.cancelMu.Lock()
		if cancel, ok := a.cancelFuncs[askID]; ok {
			cancel()
			a.logger.Info().Str("ask_id", askID).Msg("cancelled console ask")
		} else {
			a.logger.Warn().Str("ask_id", askID).Msg("no in-flight ask to cancel")
		}
		a.cancelMu.Unlock()

	default:
		a.logger.Warn().Str("action", cmdData.Action).Msg("unknown console command")
	}

	return nil, nil // Commands are fire-and-forget
}

// emitConsoleEvent sends a console.event message to the caller.
func (a *AgentActor) emitConsoleEvent(toNS, askID, correlID string, seq, iteration int, kind, content string) {
	if a.sendReply == nil {
		return
	}

	eventData := agentdomain.ConsoleEventData{
		AskID:     askID,
		Kind:      kind,
		Content:   content,
		Seq:       seq,
		Iteration: iteration,
	}
	eventEnv := envelope.OK("console.event", eventData)
	payload, err := json.Marshal(eventEnv)
	if err != nil {
		a.logger.Warn().Err(err).Msg("failed to marshal console event")
		return
	}

	eventMsg := &Message{
		ID:        ulid.Make().String(),
		Subject:   "console.event",
		Body:      payload,
		ToNS:      toNS,
		FromNS:    a.Namespace(),
		CreatedAt: time.Now(),
		Headers:   map[string]string{"correlation": correlID, "ask_id": askID},
	}

	if err := a.sendReply(context.Background(), eventMsg); err != nil {
		a.logger.Warn().Err(err).Msg("failed to send console event")
	}
}

func buildStopContinuation(result string, context string) string {
	result = strings.TrimSpace(result)
	context = strings.TrimSpace(context)

	if result == "" && context == "" {
		return ""
	}
	if result == "" {
		return context
	}
	if context == "" {
		return fmt.Sprintf("Previous response:\n%s", result)
	}
	return fmt.Sprintf("Previous response:\n%s\n\n%s", result, context)
}

// SetReplySender sets the function used to send reply messages.
// Called by the supervisor when registering the actor.
func (a *AgentActor) SetReplySender(fn func(ctx context.Context, msg *Message) error) {
	a.sendReply = fn
	// Also set on base actor
	a.replySender = fn
}

// ToolsRegistry returns the tools registry for testing.
func (a *AgentActor) ToolsRegistry() *agenttools.Registry {
	return a.toolsRegistry
}

// ShortTermMemory returns the short-term memory for testing.
func (a *AgentActor) ShortTermMemory() *memory.ShortTermMemory {
	return a.shortTermMem
}

// dispatchHook dispatches a hook event if a dispatcher is configured.
// Returns the hook result for inspection by the caller.
// The opts map can contain event-specific fields to set on the input.
func (a *AgentActor) dispatchHook(ctx context.Context, event hooks.Event, opts map[string]any) hooks.Result {
	if a.hooks == nil {
		return hooks.Result{Output: hooks.NewApprove("no dispatcher", nil)}
	}

	input := a.buildHookInput(event, opts)

	result, err := a.hooks.Dispatch(ctx, input)
	if err != nil {
		a.logger.Warn().
			Err(err).
			Str("event", string(event)).
			Msg("hook dispatch error")
		return hooks.Result{Output: hooks.NewApprove("dispatch error", nil)}
	}

	// Log if blocked
	if result.Blocked {
		a.logger.Info().
			Str("event", string(event)).
			Str("blocked_by", result.BlockedBy).
			Str("reason", result.Output.Reason).
			Msg("hook blocked operation")
	}

	return result
}

// buildHookInput creates a hooks.Input with common fields populated.
// The opts map can contain event-specific overrides.
func (a *AgentActor) buildHookInput(event hooks.Event, opts map[string]any) hooks.Input {
	input := hooks.Input{
		Event:         event,
		ActorID:       a.ID(),
		WorkspaceID:   a.agentConfig.WorkspaceID,
		WorkspaceRoot: a.workspaceRoot,
		SessionID:     a.sessionID,
	}

	if opts == nil {
		return input
	}

	// Apply event-specific fields from opts
	if v, ok := opts["turn_id"].(string); ok {
		input.TurnID = v
	}
	if v, ok := opts["correlation_id"].(string); ok {
		input.CorrelationID = v
	}
	if v, ok := opts["prompt"].(string); ok {
		input.Prompt = v
	}
	if v, ok := opts["assistant_text"].(string); ok {
		input.AssistantText = v
	}
	if v, ok := opts["tool_name"].(string); ok {
		input.ToolName = v
	}
	if v, ok := opts["tool_input"].(json.RawMessage); ok {
		input.ToolInput = v
	}
	if v, ok := opts["tool_observation"].(json.RawMessage); ok {
		input.ToolObservation = v
	}
	if v, ok := opts["tool_error"].(string); ok {
		input.ToolError = v
	}
	if v, ok := opts["tool_duration_ms"].(int64); ok {
		input.ToolDurationMS = v
	}
	if v, ok := opts["mailbox_message"].(*hooks.MailboxMessage); ok {
		input.MailboxMessage = v
	}

	return input
}

// getEngineTools returns the tools as engine.ToolDef slice for the LLMChatEngine.
func (a *AgentActor) getEngineTools() []engine.ToolDef {
	if a.toolsRegistry == nil {
		return nil
	}
	tools := a.toolsRegistry.List()
	defs := make([]engine.ToolDef, len(tools))
	for i, t := range tools {
		schema, _ := json.Marshal(t.InputSchema())
		defs[i] = engine.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  schema,
		}
	}
	return defs
}

// buildSystemPromptString returns the system prompt for a given role as a plain string.
// This is used by the LLMChatEngine path.
func buildSystemPromptString(role agenttypes.AgentRole) string {
	return agentprompt.Instruction(role)
}

// Engine returns the underlying AgentEngine for testing.
func (a *AgentActor) Engine() engine.AgentEngine {
	return a.agentEngine
}

// Ensure AgentActor implements Actor interface.
var _ Actor = (*AgentActor)(nil)
