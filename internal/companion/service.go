package companion

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/engine"
	"github.com/jkatigb/agentctl/internal/hooks"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/contextvar"
)

// EngineType selects the execution engine for the companion service.
type EngineType string

const (
	// EngineTypeLLMChat uses LLMChatEngine for OpenAI-compatible execution.
	EngineTypeLLMChat EngineType = "llmchat"
)

const defaultMaxHistoryTurns = 50

// Service is the companion chat service.
type Service struct {
	contextStore contextvar.Store
	memory       *ConversationMemory
	config       ServiceConfig
	logger       zerolog.Logger

	autoCompressMu       sync.Mutex
	autoCompressInFlight map[string]struct{} // keyed by conversation ID
}

// ServiceConfig configures the companion service.
type ServiceConfig struct {
	// LLMProvider is the LLM provider: "openrouter", "groq", "openai", "cerebras"
	LLMProvider string

	// LLMAPIKey is the API key for the LLM provider.
	LLMAPIKey string

	// LLMModel is the model to use.
	LLMModel string

	// StoryGatherModel overrides LLMModel for story gather stage (optional).
	StoryGatherModel string

	// StoryDialogueModel overrides LLMModel for story dialogue stage (optional).
	StoryDialogueModel string

	// DefaultPersonality is the default system prompt personality.
	DefaultPersonality string

	// RequireContextQuery enforces context querying before responses.
	RequireContextQuery bool

	// MaxIterations limits the tool call loop.
	MaxIterations int

	// Timeout is the request timeout.
	Timeout time.Duration

	// ExecMode is the default execution mode: "reactive", "autonomous", or "proactive".
	// - reactive: Direct LLM response with tool calls as needed (default)
	// - autonomous: Multi-turn context gathering before responding
	// - proactive: Like autonomous but can initiate work on its own
	// - story: Two-stage gather + dialogue pipeline with structured outputs
	ExecMode agent.ExecutionMode

	// EngineType selects the execution engine (llmchat only).
	// - llmchat: Uses LLMChatEngine for OpenAI-compatible streaming
	EngineType EngineType

	// MemoryDB is the database for conversation memory (optional).
	// If nil, memory features are disabled.
	MemoryDB *sql.DB

	// MemoryConfig configures conversation memory behavior.
	MemoryConfig *MemoryConfig

	// MemoryStore is the named memory store for semantic search integration (optional).
	// When set, companion summaries will be stored in named_memory for semantic search.
	MemoryStore storage.MemoryStore

	// MemoryWorkspace is the workspace ID for semantic search scoping.
	MemoryWorkspace string

	// Config is the platform config for embedder creation (optional).
	// When set along with MemoryStore, an embedder will be created automatically
	// using the configured embedding provider (VOYAGE_API_KEY from .env).
	Config *config.Config

	// HookDispatcher for pre/post tool use hooks (optional).
	// When set, hooks will be invoked around tool execution.
	HookDispatcher hooks.Dispatcher

	// ActionExecutor processes hook output actions (optional).
	// When set, hook actions (inject_context, enqueue_context, etc.) will be processed.
	ActionExecutor hooks.ActionExecutor

	// HookContext provides context for hook dispatch.
	HookContext HookContext

	// ExtraToolExecutor adds extra tools to the companion toolset (optional).
	ExtraToolExecutor engine.ToolExecutor

	// ExtraToolsOnly restricts tools to ExtraToolExecutor when true.
	ExtraToolsOnly bool

	// ToolsAllow restricts the toolset to this allowlist when non-empty.
	// Names are engine tool names (ToolDef.Name).
	ToolsAllow []string

	// Logger for structured logging.
	Logger zerolog.Logger

	// PresenceConfig configures multimodal presence generation (optional).
	PresenceConfig *PresenceConfig

	// SkillRunner executes skills for presence generation (optional).
	// Required when PresenceConfig.Enabled is true.
	SkillRunner SkillRunner
}

// SkillRunner executes skills by name.
type SkillRunner interface {
	Run(ctx context.Context, skillName string, input map[string]any) (*SkillRunResult, error)
}

// SkillRunResult contains the result of a skill execution.
type SkillRunResult struct {
	Success bool            `json:"success"`
	Output  json.RawMessage `json:"output,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// HookContext provides context for hook dispatch in companion sessions.
type HookContext struct {
	// SessionID is the session identifier for the chat.
	SessionID string

	// ActorID is the agent/actor identifier.
	ActorID string

	// WorkspaceID is the workspace identifier.
	WorkspaceID string

	// WorkspaceRoot is the filesystem path to the workspace.
	WorkspaceRoot string
}

// DefaultServiceConfig returns sensible defaults.
func DefaultServiceConfig() ServiceConfig {
	return ServiceConfig{
		DefaultPersonality:  DefaultRLMPersonality,
		RequireContextQuery: true,
		MaxIterations:       20,
		Timeout:             60 * time.Second,
	}
}

// NewService creates a new companion service.
// NewService initializes the companion service with optional memory and embeddings.
//
// Index:
// - Purpose: Configure companion service defaults and optional conversation memory
// - Flow: normalize config → init memory/summarizer → return service
// - SideEffects: initializes memory store; may create embedder
// - FailureModes: memory initialization errors logged as warnings
// - Related: NewConversationMemory, NewLLMSummarizer
// - Keywords: companion_service, memory, summarizer, embedder, config
func NewService(store contextvar.Store, cfg ServiceConfig) *Service {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 20
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.DefaultPersonality == "" {
		cfg.DefaultPersonality = DefaultRLMPersonality
	}

	svc := &Service{
		contextStore: store,
		config:       cfg,
		logger:       cfg.Logger,

		autoCompressInFlight: make(map[string]struct{}),
	}

	// Initialize conversation memory if DB provided
	cfg.Logger.Debug().Bool("memorydb_provided", cfg.MemoryDB != nil).Msg("Checking MemoryDB")
	if cfg.MemoryDB != nil {
		cfg.Logger.Debug().Msg("MemoryDB is set, initializing conversation memory")
		var opts []MemoryOption
		if cfg.MemoryConfig != nil {
			opts = append(opts, WithMemoryConfig(*cfg.MemoryConfig))
		}

		// Create LLM summarizer only if credentials are available
		if cfg.LLMProvider != "" && cfg.LLMAPIKey != "" {
			summarizer := NewLLMSummarizer(LLMSummarizerConfig{
				Provider: cfg.LLMProvider,
				APIKey:   cfg.LLMAPIKey,
				Model:    cfg.LLMModel,
			})
			opts = append(opts, WithSummarizer(summarizer))
		} else {
			cfg.Logger.Warn().Msg("LLM credentials not configured - summarization disabled")
		}

		// Add memory store for semantic search if configured
		if cfg.MemoryStore != nil && cfg.MemoryWorkspace != "" {
			opts = append(opts, WithMemoryStore(cfg.MemoryStore, cfg.MemoryWorkspace))

			// Create embedder from config for vector search (requires VOYAGE_API_KEY in .env)
			if cfg.Config != nil {
				embedder, embErr := semantic.NewEmbedderFromConfig(semantic.ScopeMemory, *cfg.Config)
				if embErr != nil {
					cfg.Logger.Warn().Err(embErr).Msg("Could not create embedder - companion memories won't be vector searchable")
				} else {
					opts = append(opts, WithEmbedder(embedder))
					cfg.Logger.Debug().Msg("Embedder created - companion memories will be vector searchable")
				}
			}
		}

		memory, err := NewConversationMemory(cfg.MemoryDB, opts...)
		if err != nil {
			cfg.Logger.Warn().Err(err).Msg("Failed to initialize conversation memory")
		} else {
			svc.memory = memory
			cfg.Logger.Debug().Msg("Conversation memory initialized successfully")
		}
	}

	cfg.Logger.Debug().Bool("memory_enabled", svc.memory != nil).Msg("Service created")
	return svc
}

// ChatRequest is a chat request from the mobile app.
type ChatRequest struct {
	// ConversationID is the client-generated conversation identifier.
	ConversationID string `json:"conversation_id"`

	// Message is the user's message.
	Message string `json:"message"`

	// LLMProvider overrides the service default LLM provider for this request (optional).
	LLMProvider string `json:"llm_provider,omitempty"`

	// LLMModel overrides the service default model for this request (optional).
	LLMModel string `json:"llm_model,omitempty"`

	// Context is additional request context for this turn.
	Context map[string]any `json:"context,omitempty"`

	// Personality overrides the default system prompt.
	Personality string `json:"personality,omitempty"`

	// RequireContextQuery overrides the service default.
	RequireContextQuery *bool `json:"require_context_query,omitempty"`

	// ExecMode overrides the service default execution mode.
	// Use "autonomous" for multi-turn context gathering before responding.
	// Use "story" for gather + dialogue with structured outputs.
	ExecMode agent.ExecutionMode `json:"exec_mode,omitempty"`

	// EngineType overrides the service default engine type (llmchat only).
	EngineType EngineType `json:"engine_type,omitempty"`

	// StoryGatherModel overrides ServiceConfig.StoryGatherModel for story mode (optional).
	StoryGatherModel string `json:"story_gather_model,omitempty"`

	// StoryDialogueModel overrides ServiceConfig.StoryDialogueModel for story mode (optional).
	StoryDialogueModel string `json:"story_dialogue_model,omitempty"`

	// MaxHistoryTurns overrides how many prior conversation turns (messages) to include as
	// message history. This is best-effort: when memory is disabled or empty, no history
	// is injected.
	//
	// Semantics:
	// - 0: use default (currently 50)
	// - -1: disable history injection
	MaxHistoryTurns int `json:"max_history_turns,omitempty"`
}

// ChatTone captures structured emotion metadata for responses.
type ChatTone struct {
	Emotion   string  `json:"emotion,omitempty"`   // neutral|joy|sadness|anger|fear|surprise|disgust|playful
	Intensity float64 `json:"intensity,omitempty"` // 0..1
	Voice     string  `json:"voice,omitempty"`     // optional: warm, witty, calm, etc.
}

// ChatAction captures structured UI/scene directives for clients.
type ChatAction struct {
	BackgroundKey string `json:"background_key,omitempty"`
	ImagePrompt   string `json:"image_prompt,omitempty"`
	Scene         string `json:"scene,omitempty"`
}

// PresenceConfig configures multimodal presence generation.
type PresenceConfig struct {
	// Enabled turns on presence generation for responses.
	Enabled bool `json:"enabled"`

	// VoiceEnabled generates voice audio via ElevenLabs.
	VoiceEnabled bool `json:"voice_enabled"`

	// BackgroundEnabled generates mood backgrounds.
	BackgroundEnabled bool `json:"background_enabled"`

	// CharacterID is the default character for overlay selection.
	CharacterID string `json:"character_id,omitempty"`

	// Style is the art style for backgrounds: anime|realistic|watercolor|minimalist
	Style string `json:"style,omitempty"`

	// VoiceID is the ElevenLabs voice ID override.
	VoiceID string `json:"voice_id,omitempty"`
}

// PresenceBundle contains multimodal presence assets for a response.
type PresenceBundle struct {
	// Emotion detected from text
	Emotion   string  `json:"emotion"`
	Intensity float64 `json:"intensity"`

	// DisplayText is the response text with markers stripped
	DisplayText string `json:"display_text"`

	// Markers detected in text
	Markers       []string `json:"markers,omitempty"`
	DetectedEmoji []string `json:"detected_emoji,omitempty"`

	// Asset digests (CAS references)
	BackgroundDigest string `json:"background_digest,omitempty"`
	OverlayDigest    string `json:"overlay_digest,omitempty"`
	AudioDigest      string `json:"audio_digest,omitempty"`

	// Audio metadata
	AudioDurationMS int `json:"audio_duration_ms,omitempty"`

	// Generation metadata
	CacheHits   int      `json:"cache_hits"`
	CacheMisses int      `json:"cache_misses"`
	Errors      []string `json:"errors,omitempty"`
}

// ToolCallDetail contains detailed information about a tool call.
type ToolCallDetail struct {
	// ID is the unique identifier for this tool call.
	ID string `json:"id"`

	// Name is the canonical tool name.
	Name string `json:"name"`

	// Arguments is the JSON arguments for the tool.
	Arguments json.RawMessage `json:"arguments,omitempty"`

	// Output is the result from executing the tool.
	Output string `json:"output,omitempty"`
}

// InjectedContextDetail contains context injected by hooks.
type InjectedContextDetail struct {
	// ToolCallID is the ID of the associated tool call.
	ToolCallID string `json:"tool_call_id"`

	// Source is the hook/action that injected the context.
	Source string `json:"source,omitempty"`

	// Content is the injected context text.
	Content string `json:"content"`
}

// ChatResponse is the response from the companion.
type ChatResponse struct {
	// Response is the assistant's response text.
	Response string `json:"response"`

	// Tone captures structured emotion metadata for clients.
	Tone *ChatTone `json:"tone,omitempty"`

	// Action captures structured UI/scene directives for clients.
	Action *ChatAction `json:"action,omitempty"`

	// Presence contains multimodal presence assets (voice, background, overlay).
	// Only populated when PresenceConfig.Enabled is true.
	Presence *PresenceBundle `json:"presence,omitempty"`

	// ConversationID is the conversation this response belongs to.
	ConversationID string `json:"conversation_id"`

	// ContextQueries is how many times context was queried this turn.
	ContextQueries int `json:"context_queries"`

	// ToolsUsed lists the names of tools that were called.
	ToolsUsed []string `json:"tools_used"`

	// ToolCalls contains detailed information about each tool call.
	ToolCalls []ToolCallDetail `json:"tool_calls,omitempty"`

	// InjectedContexts contains context injected by hooks during tool execution.
	InjectedContexts []InjectedContextDetail `json:"injected_contexts,omitempty"`

	// TokenUsage tracks token consumption.
	TokenUsage TokenUsage `json:"token_usage"`

	// DurationMS is the request duration in milliseconds.
	DurationMS int64 `json:"duration_ms"`

	// Error contains error details if the request failed.
	Error string `json:"error,omitempty"`
}

// TokenUsage tracks token consumption.
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// Chat executes a single companion chat request.
//
// Index:
// - Purpose: Execute a companion chat turn and return structured response
// - Flow: validate request → resolve exec mode/engine → build prompt → run engine → store turns → generate presence
// - SideEffects: LLM calls; memory reads/writes; optional presence generation
// - FailureModes: validation errors, engine errors, memory errors
// - Related: chatWithLLMChat, chatWithStoryLoop, storeConversationTurns
// - Keywords: companion_chat, conversation_id, exec_mode, engine_type, presence, tools_used
func (s *Service) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	start := time.Now()

	if req.ConversationID == "" {
		return nil, fmt.Errorf("conversation_id is required")
	}
	if req.Message == "" {
		return nil, fmt.Errorf("message is required")
	}

	// Determine effective exec mode and engine type
	execMode := s.config.ExecMode
	if req.ExecMode != "" {
		execMode = req.ExecMode
	}
	if execMode == "" {
		execMode = agent.ModeReactive
	}

	engineType := s.config.EngineType
	if req.EngineType != "" {
		engineType = req.EngineType
	}
	if engineType == "" {
		engineType = EngineTypeLLMChat
	}
	if engineType != EngineTypeLLMChat {
		return nil, fmt.Errorf("unsupported engine_type %q (only %q is supported)", engineType, EngineTypeLLMChat)
	}

	s.logger.Debug().
		Str("exec_mode", string(execMode)).
		Str("engine_type", string(engineType)).
		Str("conversation_id", req.ConversationID).
		Msg("Processing chat request")

	// Create RLM tool executor
	rlmExecutor := engine.NewRLMToolExecutor(s.contextStore, req.ConversationID)

	// Enable semantic search over companion memories if memory store is configured
	if s.config.MemoryStore != nil && s.config.MemoryWorkspace != "" {
		rlmExecutor.SetMemoryStore(s.config.MemoryStore, s.config.MemoryWorkspace)

		// Enable vector search for semantic_query when embedding config is available.
		if s.config.Config != nil {
			provider, err := semantic.NewProviderForScope(semantic.ScopeMemory, *s.config.Config)
			if err != nil {
				s.logger.Warn().Err(err).Msg("Could not create embedding provider for semantic_query; falling back to text search")
			} else {
				rlmExecutor.SetEmbedProvider(provider)
			}
		}
	}

	// Build system prompt
	systemPrompt, err := s.buildSystemPrompt(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("build system prompt: %w", err)
	}

	// For autonomous/proactive modes, add thinking phase instructions
	if execMode == agent.ModeAutonomous || execMode == agent.ModeProactive {
		systemPrompt = s.addAutonomousInstructions(systemPrompt, execMode)
	}

	var resp *ChatResponse

	// Route to appropriate engine
	if execMode == agent.ModeStory {
		resp, err = s.chatWithStoryLoop(ctx, req, rlmExecutor, systemPrompt, start)
	} else {
		resp, err = s.chatWithLLMChat(ctx, req, rlmExecutor, systemPrompt, start)
	}

	if err != nil {
		return nil, err
	}

	// Store conversation turns in memory
	s.storeConversationTurns(ctx, req, resp, start)

	// Generate presence assets (voice, background, overlay) if enabled
	if s.presenceEnabled() {
		presenceCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.generatePresence(presenceCtx, req, resp)
	}

	return resp, nil
}

// chatWithLLMChat executes using the LLMChatEngine.
// The systemPrompt parameter is the full system prompt for this turn.
//
// Index:
// - Purpose: Execute the LLMChatEngine path for a companion chat turn
// - Flow: build engine config → set hook/tool context → run engine → assemble response/tool details
// - SideEffects: LLM calls; tool execution; hook dispatch
// - FailureModes: engine init errors, engine run errors
// - Related: engine.NewLLMChatEngine, engine.LLMChatEngine.Run, buildTooling
// - Keywords: llmchat, provider, model, tool_calls, conversation_id, tools_used
func (s *Service) chatWithLLMChat(ctx context.Context, req ChatRequest, rlmExecutor *engine.RLMToolExecutor, systemPrompt string, start time.Time) (*ChatResponse, error) {
	messages, hasHistory := s.buildChatMessages(ctx, req)

	// Create LLM engine in stateless mode
	engineCfg := engine.LLMChatConfig{
		Provider:              s.config.LLMProvider,
		APIKey:                s.config.LLMAPIKey,
		Model:                 s.config.LLMModel,
		MaxIterations:         s.config.MaxIterations,
		Timeout:               s.config.Timeout,
		StatelessMode:         true,
		RLMSystemPromptSuffix: RLMContextInstructions,
		RequireContextQuery:   s.config.RequireContextQuery,
		HookDispatcher:        s.config.HookDispatcher,
		ActionExecutor:        s.config.ActionExecutor,
	}

	// Override RequireContextQuery if specified in request
	if req.RequireContextQuery != nil {
		engineCfg.RequireContextQuery = *req.RequireContextQuery
	}
	if s.config.ExtraToolsOnly && s.config.ExtraToolExecutor != nil {
		engineCfg.RequireContextQuery = false
		engineCfg.RLMSystemPromptSuffix = ""
	}

	// If we have meaningful conversation history, don't force a context query —
	// the model can see the conversation. Context tools remain available for
	// long-term memory/preferences but aren't mandatory.
	if hasHistory && req.RequireContextQuery == nil {
		engineCfg.RequireContextQuery = false
	}

	// If the conversation tool allowlist excludes rlm_context_query, we must disable
	// RequireContextQuery and remove RLM instructions. Otherwise the engine will
	// endlessly nudge for a tool call it cannot make.
	if len(s.config.ToolsAllow) > 0 && !containsString(s.config.ToolsAllow, "rlm_context_query") {
		engineCfg.RequireContextQuery = false
		engineCfg.RLMSystemPromptSuffix = ""
	}

	llmEngine, err := engine.NewLLMChatEngine(engineCfg)
	if err != nil {
		return nil, fmt.Errorf("create engine: %w", err)
	}

	// Set hook context for dispatch (uses conversation ID as session if not set)
	hookCtx := engine.HookContext{
		SessionID:     s.config.HookContext.SessionID,
		ActorID:       s.config.HookContext.ActorID,
		WorkspaceID:   s.config.HookContext.WorkspaceID,
		WorkspaceRoot: s.config.HookContext.WorkspaceRoot,
	}
	if hookCtx.SessionID == "" {
		hookCtx.SessionID = req.ConversationID
	}
	llmEngine.SetHookContext(hookCtx)

	toolExecutor, toolDefs, usesRLM := s.buildTooling(rlmExecutor)

	toolRunner := engine.NewToolRunner(toolExecutor, nil, engine.DefaultToolRunnerConfig())
	llmEngine.SetToolRunner(toolRunner)
	if usesRLM {
		llmEngine.SetRLMExecutor(rlmExecutor)
	}

	input := engine.EngineInput{
		SystemPrompt: systemPrompt,
		Messages:     messages,
		Tools:        toolDefs,
	}

	// Execute
	output, err := llmEngine.Run(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("engine run: %w", err)
	}

	// Strip <think>...</think> blocks from reasoning models (e.g., GLM, DeepSeek)
	responseText := stripThinkTags(output.AssistantText)

	// Build response
	resp := &ChatResponse{
		ConversationID: req.ConversationID,
		Response:       responseText,
		ContextQueries: rlmExecutor.QueryCount(),
		DurationMS:     time.Since(start).Milliseconds(),
		TokenUsage: TokenUsage{
			InputTokens:  output.Tokens.InputTokens,
			OutputTokens: output.Tokens.OutputTokens,
			TotalTokens:  output.Tokens.TotalTokens,
		},
	}

	// Collect tools used (names only for backwards compatibility)
	toolNames := make(map[string]struct{})
	for _, tc := range output.ToolCalls {
		toolNames[tc.Name] = struct{}{}
	}
	for name := range toolNames {
		resp.ToolsUsed = append(resp.ToolsUsed, name)
	}
	sort.Strings(resp.ToolsUsed)

	// Build map of tool results by call ID for lookup
	toolResults := make(map[string]string)
	for _, tr := range output.ToolResults {
		toolResults[tr.ToolCallID] = tr.Content
	}
	// Add detailed tool call information with outputs
	for _, tc := range output.ToolCalls {
		resp.ToolCalls = append(resp.ToolCalls, ToolCallDetail{
			ID:        tc.ID,
			Name:      tc.Name,
			Arguments: tc.Arguments,
			Output:    toolResults[tc.ID],
		})
	}

	// Add injected context information
	for _, ic := range output.InjectedContexts {
		resp.InjectedContexts = append(resp.InjectedContexts, InjectedContextDetail{
			ToolCallID: ic.ToolCallID,
			Source:     ic.Source,
			Content:    ic.Content,
		})
	}

	// Check for errors
	if output.StopReason == engine.StopReasonError {
		resp.Error = output.Error
	}

	// Note: Conversation turns are stored by the Chat() entry point via storeConversationTurns()
	// to avoid duplicate entries.

	return resp, nil
}

// buildChatMessages constructs the message list for a turn by injecting recent conversation
// history from companion memory (when enabled), followed by the current user message.
//
// Index:
// - Purpose: Provide the LLM with L0 conversation continuity via real message history
// - Flow: resolve history limit → load turns → map to engine messages → append current user msg
// - SideEffects: queries the companion memory database
// - FailureModes: memory query errors or scan issues yield best-effort/no history injection
// - Related: ConversationMemory.GetConversationMessages
// - Keywords: history_injection, max_history_turns, companion_turns
func (s *Service) buildChatMessages(ctx context.Context, req ChatRequest) ([]engine.Message, bool) {
	historyLimit := req.MaxHistoryTurns
	switch {
	case historyLimit == 0:
		historyLimit = defaultMaxHistoryTurns
	case historyLimit < 0:
		historyLimit = 0
	}

	var messages []engine.Message
	hasHistory := false

	if s.memory != nil && historyLimit > 0 {
		turns, err := s.memory.GetConversationMessages(ctx, req.ConversationID, historyLimit)
		if err != nil {
			s.logger.Debug().
				Err(err).
				Str("conversation_id", req.ConversationID).
				Msg("Conversation history injection skipped")
		} else if len(turns) > 0 {
			for _, t := range turns {
				switch t.Role {
				case "user":
					messages = append(messages, engine.NewUserMessage(t.Content))
				case "assistant":
					messages = append(messages, engine.NewAssistantMessage(t.Content))
				}
			}
			hasHistory = len(turns) >= 2
			s.logger.Debug().
				Str("conversation_id", req.ConversationID).
				Int("history_turns", len(turns)).
				Int("history_limit", historyLimit).
				Msg("Injected conversation history")
		}
	}

	messages = append(messages, engine.NewUserMessage(req.Message))
	return messages, hasHistory
}

func (s *Service) buildTooling(rlmExecutor *engine.RLMToolExecutor) (engine.ToolExecutor, []engine.ToolDef, bool) {
	toolExecutor := engine.ToolExecutor(rlmExecutor)
	toolDefs := rlmExecutor.List()
	usesRLM := true
	if s.config.ExtraToolExecutor != nil {
		if s.config.ExtraToolsOnly {
			toolExecutor = s.config.ExtraToolExecutor
			toolDefs = s.config.ExtraToolExecutor.List()
			usesRLM = false
		} else {
			toolExecutor = engine.NewCompositeToolExecutor(rlmExecutor, s.config.ExtraToolExecutor)
			toolDefs = toolExecutor.List()
		}
	}

	// Apply conversation-level tool allowlist (normalize once for consistent matching).
	if len(s.config.ToolsAllow) > 0 {
		allowed := make([]string, 0, len(s.config.ToolsAllow))
		for _, t := range s.config.ToolsAllow {
			if v := strings.TrimSpace(t); v != "" {
				allowed = append(allowed, v)
			}
		}
		if len(allowed) > 0 {
			toolExecutor = engine.NewFilteredToolExecutor(toolExecutor, allowed)
			toolDefs = engine.FilterToolDefs(toolDefs, allowed)
		}
	}

	return toolExecutor, toolDefs, usesRLM
}

type storyContextBundle struct {
	ContextSummary   string         `json:"context_summary"`
	Facts            []string       `json:"facts"`
	RecalledMemories []string       `json:"recalled_memories"`
	UserState        map[string]any `json:"user_state"`
	SuggestedTone    *ChatTone      `json:"suggested_tone,omitempty"`
	SuggestedAction  *ChatAction    `json:"suggested_action,omitempty"`
}

type dialogueEnvelope struct {
	Text   string      `json:"text"`
	Tone   *ChatTone   `json:"tone,omitempty"`
	Action *ChatAction `json:"action,omitempty"`
}

// chatWithStoryLoop runs the story-mode gather + dialogue loop.
//
// Index:
// - Purpose: Produce story-mode responses by gathering context then generating dialogue
// - Flow: resolve models → run gather pass → parse context → run dialogue pass → assemble response
// - SideEffects: LLM calls; tool execution during gather phase
// - FailureModes: gather/dialogue errors, response format parsing failures
// - Related: runLLMChatWithResponseFormatFallback, parseStoryContextBundle, parseDialogueEnvelope
// - Keywords: story_mode, gather_model, dialogue_model, response_format, tools_used, conversation_id
func (s *Service) chatWithStoryLoop(ctx context.Context, req ChatRequest, rlmExecutor *engine.RLMToolExecutor, systemPrompt string, start time.Time) (*ChatResponse, error) {
	messages, hasHistory := s.buildChatMessages(ctx, req)

	gatherModel := s.config.StoryGatherModel
	if req.StoryGatherModel != "" {
		gatherModel = req.StoryGatherModel
	}
	if gatherModel == "" {
		gatherModel = s.config.LLMModel
	}

	dialogueModel := s.config.StoryDialogueModel
	if req.StoryDialogueModel != "" {
		dialogueModel = req.StoryDialogueModel
	}
	if dialogueModel == "" {
		dialogueModel = s.config.LLMModel
	}

	toolExecutor, toolDefs, usesRLM := s.buildTooling(rlmExecutor)

	gatherCfg := engine.LLMChatConfig{
		Provider:              s.config.LLMProvider,
		APIKey:                s.config.LLMAPIKey,
		Model:                 gatherModel,
		MaxIterations:         s.config.MaxIterations,
		Timeout:               s.config.Timeout,
		StatelessMode:         true,
		RLMSystemPromptSuffix: RLMContextInstructions,
		RequireContextQuery:   s.config.RequireContextQuery,
		HookDispatcher:        s.config.HookDispatcher,
		ActionExecutor:        s.config.ActionExecutor,
		ResponseFormat:        storyGatherResponseFormat(),
	}
	if req.RequireContextQuery != nil {
		gatherCfg.RequireContextQuery = *req.RequireContextQuery
	}
	if !usesRLM {
		gatherCfg.RequireContextQuery = false
		gatherCfg.RLMSystemPromptSuffix = ""
	}
	if hasHistory && req.RequireContextQuery == nil {
		gatherCfg.RequireContextQuery = false
	}
	if len(s.config.ToolsAllow) > 0 && !containsString(s.config.ToolsAllow, "rlm_context_query") {
		gatherCfg.RequireContextQuery = false
		gatherCfg.RLMSystemPromptSuffix = ""
	}

	gatherInput := engine.EngineInput{
		SystemPrompt: buildStoryGatherPrompt(systemPrompt),
		Messages:     messages,
	}

	gatherOutput, err := s.runLLMChatWithResponseFormatFallback(
		ctx,
		req,
		gatherCfg,
		gatherInput,
		toolExecutor,
		toolDefs,
		rlmExecutor,
		usesRLM,
	)
	if err != nil {
		return nil, err
	}
	if gatherOutput.StopReason == engine.StopReasonError {
		return nil, fmt.Errorf("story gather failed: %s", gatherOutput.Error)
	}

	contextQueries := rlmExecutor.QueryCount()

	storyContext, ok := parseStoryContextBundle(gatherOutput.AssistantText)
	storyContextJSON := "{}"
	if ok {
		if payload, err := json.Marshal(storyContext); err == nil {
			storyContextJSON = string(payload)
		}
	}

	dialogueCfg := engine.LLMChatConfig{
		Provider:              s.config.LLMProvider,
		APIKey:                s.config.LLMAPIKey,
		Model:                 dialogueModel,
		MaxIterations:         s.config.MaxIterations,
		Timeout:               s.config.Timeout,
		StatelessMode:         true,
		RLMSystemPromptSuffix: "",
		RequireContextQuery:   false,
		HookDispatcher:        s.config.HookDispatcher,
		ActionExecutor:        s.config.ActionExecutor,
		ResponseFormat:        storyDialogueResponseFormat(),
	}

	dialogueInput := engine.EngineInput{
		SystemPrompt: buildStoryDialoguePrompt(systemPrompt, storyContextJSON),
		Messages:     messages,
	}

	dialogueOutput, err := s.runLLMChatWithResponseFormatFallback(
		ctx,
		req,
		dialogueCfg,
		dialogueInput,
		nil,
		nil,
		nil,
		false,
	)
	if err != nil {
		return nil, err
	}

	envelope, parsed := parseDialogueEnvelope(dialogueOutput.AssistantText)
	responseText := stripThinkTags(dialogueOutput.AssistantText)
	var tone *ChatTone
	var action *ChatAction
	if parsed && envelope.Text != "" {
		responseText = envelope.Text
		tone = envelope.Tone
		action = envelope.Action
	}

	resp := &ChatResponse{
		ConversationID: req.ConversationID,
		Response:       responseText,
		Tone:           tone,
		Action:         action,
		ContextQueries: contextQueries,
		DurationMS:     time.Since(start).Milliseconds(),
		TokenUsage: TokenUsage{
			InputTokens:  gatherOutput.Tokens.InputTokens + dialogueOutput.Tokens.InputTokens,
			OutputTokens: gatherOutput.Tokens.OutputTokens + dialogueOutput.Tokens.OutputTokens,
			TotalTokens:  gatherOutput.Tokens.TotalTokens + dialogueOutput.Tokens.TotalTokens,
		},
	}

	toolNames := make(map[string]struct{})
	for _, tc := range gatherOutput.ToolCalls {
		toolNames[tc.Name] = struct{}{}
	}
	for name := range toolNames {
		resp.ToolsUsed = append(resp.ToolsUsed, name)
	}
	sort.Strings(resp.ToolsUsed)

	// Add detailed tool call information from gather phase
	for _, tc := range gatherOutput.ToolCalls {
		resp.ToolCalls = append(resp.ToolCalls, ToolCallDetail{
			ID:        tc.ID,
			Name:      tc.Name,
			Arguments: tc.Arguments,
		})
	}

	// Add injected context information from gather phase
	for _, ic := range gatherOutput.InjectedContexts {
		resp.InjectedContexts = append(resp.InjectedContexts, InjectedContextDetail{
			ToolCallID: ic.ToolCallID,
			Source:     ic.Source,
			Content:    ic.Content,
		})
	}

	if dialogueOutput.StopReason == engine.StopReasonError {
		resp.Error = dialogueOutput.Error
	}

	return resp, nil
}

func (s *Service) runLLMChatWithResponseFormatFallback(
	ctx context.Context,
	req ChatRequest,
	cfg engine.LLMChatConfig,
	input engine.EngineInput,
	toolExecutor engine.ToolExecutor,
	toolDefs []engine.ToolDef,
	rlmExecutor *engine.RLMToolExecutor,
	usesRLM bool,
) (engine.EngineOutput, error) {
	output, err := s.runLLMChat(ctx, req, cfg, input, toolExecutor, toolDefs, rlmExecutor, usesRLM)
	if err == nil || len(cfg.ResponseFormat) == 0 || !isResponseFormatError(err) {
		return output, err
	}

	if usesRLM || toolExecutor != nil || len(toolDefs) > 0 {
		s.logger.Warn().Err(err).Msg("Response format not supported with tools; skipping retry")
		return output, err
	}

	s.logger.Warn().Err(err).Msg("Response format not supported, retrying without response_format")
	cfg.ResponseFormat = nil
	return s.runLLMChat(ctx, req, cfg, input, toolExecutor, toolDefs, rlmExecutor, usesRLM)
}

func (s *Service) runLLMChat(
	ctx context.Context,
	req ChatRequest,
	engineCfg engine.LLMChatConfig,
	input engine.EngineInput,
	toolExecutor engine.ToolExecutor,
	toolDefs []engine.ToolDef,
	rlmExecutor *engine.RLMToolExecutor,
	usesRLM bool,
) (engine.EngineOutput, error) {
	llmEngine, err := engine.NewLLMChatEngine(engineCfg)
	if err != nil {
		return engine.EngineOutput{}, fmt.Errorf("create engine: %w", err)
	}

	hookCtx := engine.HookContext{
		SessionID:     s.config.HookContext.SessionID,
		ActorID:       s.config.HookContext.ActorID,
		WorkspaceID:   s.config.HookContext.WorkspaceID,
		WorkspaceRoot: s.config.HookContext.WorkspaceRoot,
	}
	if hookCtx.SessionID == "" {
		hookCtx.SessionID = req.ConversationID
	}
	llmEngine.SetHookContext(hookCtx)

	if toolExecutor != nil && len(toolDefs) > 0 {
		toolRunner := engine.NewToolRunner(toolExecutor, nil, engine.DefaultToolRunnerConfig())
		llmEngine.SetToolRunner(toolRunner)
		input.Tools = toolDefs
	}
	if usesRLM && rlmExecutor != nil {
		llmEngine.SetRLMExecutor(rlmExecutor)
	}

	output, err := llmEngine.Run(ctx, input)
	if err != nil {
		return engine.EngineOutput{}, fmt.Errorf("engine run: %w", err)
	}
	return output, nil
}

func buildStoryGatherPrompt(systemPrompt string) string {
	return strings.TrimSpace(systemPrompt) + `

# Story Context Gathering

You are running a context-gathering pass. Use tools to query or store relevant context.
Do not answer the user directly. Return JSON only that matches the schema.
Include:
- context_summary: short summary for the dialogue stage
- facts: list of relevant facts
- recalled_memories: list of recalled memories
- user_state: key/value state from context
- suggested_tone: emotion guidance for the reply
- suggested_action: UI/scene guidance for the reply`
}

func buildStoryDialoguePrompt(systemPrompt, storyContextJSON string) string {
	return strings.TrimSpace(systemPrompt) + "\n\n# Story Context Bundle\n" + storyContextJSON + `

# Story Dialogue

Use the story context bundle to craft the user-facing response.
Return JSON only that matches the schema with fields: text, tone, action.`
}

func parseStoryContextBundle(raw string) (storyContextBundle, bool) {
	var bundle storyContextBundle
	if err := json.Unmarshal([]byte(raw), &bundle); err == nil {
		normalizeChatTone(bundle.SuggestedTone)
		return bundle, true
	}
	if extracted, ok := extractJSONObject(raw); ok {
		if err := json.Unmarshal([]byte(extracted), &bundle); err == nil {
			normalizeChatTone(bundle.SuggestedTone)
			return bundle, true
		}
	}
	return storyContextBundle{}, false
}

// stripThinkTags removes <think>...</think> blocks from reasoning model output.
// Handles both closed tags and unclosed tags (truncated reasoning).
func stripThinkTags(s string) string {
	// Handle case where model outputs reasoning without opening <think> tag
	// (common with GLM and some reasoning models via LM Studio).
	// e.g., "reasoning here</think>actual response"
	if !strings.Contains(s, "<think>") && strings.Contains(s, "</think>") {
		idx := strings.Index(s, "</think>")
		s = strings.TrimSpace(s[idx+len("</think>"):])
		return s
	}

	for {
		start := strings.Index(s, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(s[start:], "</think>")
		if end == -1 {
			// Unclosed <think> tag — strip from <think> to end of string
			s = strings.TrimSpace(s[:start])
			break
		}
		// Remove the <think>...</think> block
		s = s[:start] + s[start+end+len("</think>"):]
	}
	return strings.TrimSpace(s)
}

func containsString(list []string, needle string) bool {
	needle = strings.TrimSpace(needle)
	for _, v := range list {
		if strings.TrimSpace(v) == needle {
			return true
		}
	}
	return false
}

func parseDialogueEnvelope(raw string) (dialogueEnvelope, bool) {
	var envelope dialogueEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err == nil {
		normalizeChatTone(envelope.Tone)
		return envelope, true
	}
	if extracted, ok := extractJSONObject(raw); ok {
		if err := json.Unmarshal([]byte(extracted), &envelope); err == nil {
			normalizeChatTone(envelope.Tone)
			return envelope, true
		}
	}
	return dialogueEnvelope{}, false
}

func normalizeChatTone(tone *ChatTone) {
	if tone == nil {
		return
	}
	if tone.Intensity < 0 {
		tone.Intensity = 0
	}
	if tone.Intensity > 1 {
		tone.Intensity = 1
	}
}

func extractJSONObject(raw string) (string, bool) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start == -1 || end <= start {
		return "", false
	}
	return raw[start : end+1], true
}

func isResponseFormatError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "response_format") ||
		strings.Contains(msg, "json_schema") ||
		strings.Contains(msg, "structured output") ||
		strings.Contains(msg, "structured outputs")
}

const storyGatherResponseFormatJSON = `{
  "type": "json_schema",
  "json_schema": {
    "name": "story_context_bundle",
    "strict": true,
    "schema": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "context_summary": { "type": "string" },
        "facts": { "type": "array", "items": { "type": "string" } },
        "recalled_memories": { "type": "array", "items": { "type": "string" } },
        "user_state": { "type": "object", "additionalProperties": true },
        "suggested_tone": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "emotion": {
              "type": "string",
              "enum": ["neutral","joy","sadness","anger","fear","surprise","disgust","playful"]
            },
            "intensity": { "type": "number" },
            "voice": { "type": "string" }
          },
          "required": ["emotion", "intensity"]
        },
        "suggested_action": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "background_key": { "type": "string" },
            "image_prompt": { "type": "string" },
            "scene": { "type": "string" }
          }
        }
      },
      "required": ["context_summary", "facts", "recalled_memories", "user_state", "suggested_tone", "suggested_action"]
    }
  }
}`

const storyDialogueResponseFormatJSON = `{
  "type": "json_schema",
  "json_schema": {
    "name": "story_dialogue_envelope",
    "strict": true,
    "schema": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "text": { "type": "string" },
        "tone": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "emotion": {
              "type": "string",
              "enum": ["neutral","joy","sadness","anger","fear","surprise","disgust","playful"]
            },
            "intensity": { "type": "number" },
            "voice": { "type": "string" }
          },
          "required": ["emotion", "intensity"]
        },
        "action": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "background_key": { "type": "string" },
            "image_prompt": { "type": "string" },
            "scene": { "type": "string" }
          }
        }
      },
      "required": ["text", "tone", "action"]
    }
  }
}`

func storyGatherResponseFormat() json.RawMessage {
	return json.RawMessage(storyGatherResponseFormatJSON)
}

func storyDialogueResponseFormat() json.RawMessage {
	return json.RawMessage(storyDialogueResponseFormatJSON)
}

// buildSystemPrompt constructs the system prompt with personality, memory context, and agent identity.
//
// Index:
// - Purpose: Assemble system prompt from personality + memory + request context
// - Flow: load personality → build evolving prompt → inject memory context → inject request context via formatRequestContext
// - SideEffects: reads contextvar store and conversation memory
// - FailureModes: personality/memory errors yield partial prompt with defaults
// - Related: EvolvingPersonality.BuildSystemPrompt, ConversationMemory.GetContext, formatRequestContext
// - Keywords: system_prompt, personality, memory_context, request_context, agent_identity
func (s *Service) buildSystemPrompt(ctx context.Context, req ChatRequest) (string, error) {
	basePersonality := req.Personality
	if basePersonality == "" {
		// Try to load stored base personality for this conversation
		if stored, err := s.contextStore.GetByKey(ctx, req.ConversationID, contextvar.ScopeGlobal, "personality/base"); err == nil {
			var personality string
			if err := json.Unmarshal(stored.ValueJSON, &personality); err == nil && personality != "" {
				basePersonality = personality
				s.logger.Debug().Str("personality", basePersonality[:min(50, len(basePersonality))]).Msg("Using stored base personality")
			}
		}
	}
	if basePersonality == "" {
		basePersonality = s.config.DefaultPersonality
	}

	// Use evolving personality to build dynamic system prompt
	evolvingPersonality := NewEvolvingPersonality(s.contextStore, req.ConversationID)
	systemPrompt, err := evolvingPersonality.BuildSystemPrompt(ctx, basePersonality)
	if err != nil {
		s.logger.Warn().Err(err).Msg("Failed to build evolving system prompt, using base")
		systemPrompt = basePersonality
	}

	// Include conversation memory context if available
	s.logger.Debug().Bool("memory_available", s.memory != nil).Str("conversation_id", req.ConversationID).Msg("Checking memory for chat")
	if s.memory != nil {
		memoryContext, err := s.memory.GetContext(ctx, req.ConversationID)
		if err != nil {
			s.logger.Warn().Err(err).Msg("Failed to get memory context")
		} else if memoryContext != "" {
			s.logger.Debug().Int("context_len", len(memoryContext)).Msg("Memory context retrieved, injecting into prompt")
			systemPrompt = systemPrompt + "\n\n# Conversation Memory\n" + memoryContext
		} else {
			s.logger.Debug().Msg("Memory context is empty")
		}
	} else {
		s.logger.Debug().Msg("Memory is nil, skipping context injection")
	}

	if len(req.Context) > 0 {
		systemPrompt = systemPrompt + "\n\n" + formatRequestContext(req.Context)
	}

	return systemPrompt, nil
}

// formatRequestContext formats the request context map as a human-readable prompt section.
// When agent identity fields are present, they are rendered as a dedicated "Your Identity"
// section so the companion knows who it is. Remaining fields appear as general context.
func formatRequestContext(ctx map[string]any) string {
	var identity []string
	var runtime []string
	var general []string

	// Known agent identity keys
	agentKeys := map[string]string{
		"agent_name":      "Name",
		"agent_role":      "Role",
		"agent_state":     "State",
		"agent_exec_mode": "Execution Mode",
		"agent_workspace": "Workspace",
		"agent_model":     "Model",
		"agent_id":        "ID",
	}

	// Common per-turn runtime keys (usually set by clients).
	runtimeKeys := map[string]string{
		"chat_llm_provider": "Chat Provider",
		"chat_llm_model":    "Chat Model",
	}

	keys := make([]string, 0, len(ctx))
	for k := range ctx {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := ctx[k]
		if v == nil {
			continue
		}
		str := fmt.Sprintf("%v", v)
		if str == "" {
			continue
		}
		if label, ok := agentKeys[k]; ok {
			identity = append(identity, fmt.Sprintf("- %s: %s", label, str))
		} else if label, ok := runtimeKeys[k]; ok {
			runtime = append(runtime, fmt.Sprintf("- %s: %s", label, str))
		} else {
			general = append(general, fmt.Sprintf("- %s: %s", k, str))
		}
	}

	var sections []string
	if len(identity) > 0 {
		sections = append(sections, "# Your Identity\nYou are the following agent. Use this name and role when introducing yourself or when asked who you are.\n"+strings.Join(identity, "\n"))
	}
	if len(runtime) > 0 {
		sections = append(sections, "# Runtime\n"+strings.Join(runtime, "\n"))
	}
	if len(general) > 0 {
		sections = append(sections, "# Request Context\n"+strings.Join(general, "\n"))
	}

	return strings.Join(sections, "\n\n")
}

// addAutonomousInstructions adds thinking phase instructions for autonomous/proactive modes.
func (s *Service) addAutonomousInstructions(systemPrompt string, execMode agent.ExecutionMode) string {
	var instructions string

	switch execMode {
	case agent.ModeAutonomous:
		instructions = `

## Autonomous Mode Instructions

Before responding to the user, you should:
1. **Gather Context**: Use available tools to query relevant information (memories, context variables, semantic search)
2. **Think Through**: Consider what information would help you give a better response
3. **Then Respond**: Only after gathering necessary context, provide your response

Take your time to think and gather information. The user expects a thoughtful, well-informed response.`

	case agent.ModeProactive:
		instructions = `

## Proactive Mode Instructions

You operate in proactive mode, which means:
1. **Anticipate Needs**: Think about what the user might need beyond their explicit request
2. **Gather Context**: Proactively query memories and context to understand the full picture
3. **Suggest Actions**: If you notice relevant information or opportunities, bring them up
4. **Think Ahead**: Consider follow-up questions or related topics the user might benefit from

Be thorough in your information gathering and don't hesitate to make helpful suggestions.`
	}

	return systemPrompt + instructions
}

// storeConversationTurns saves the conversation to memory.
func (s *Service) storeConversationTurns(ctx context.Context, req ChatRequest, resp *ChatResponse, start time.Time) {
	if s.memory == nil || resp.Error != "" {
		return
	}

	// Store user turn
	userTurn := ConversationTurn{
		ConversationID: req.ConversationID,
		Role:           "user",
		Content:        req.Message,
		CreatedAt:      start,
	}
	if err := s.memory.AppendTurn(ctx, userTurn); err != nil {
		s.logger.Warn().Err(err).Msg("Failed to store user turn")
	}

	// Store assistant turn with tool calls
	assistantTurn := ConversationTurn{
		ConversationID: req.ConversationID,
		Role:           "assistant",
		Content:        resp.Response,
		CreatedAt:      time.Now(),
	}

	// Serialize tool calls if present
	if len(resp.ToolCalls) > 0 {
		if toolCallsJSON, err := json.Marshal(resp.ToolCalls); err == nil {
			assistantTurn.ToolCalls = toolCallsJSON
		}
	}

	if err := s.memory.AppendTurn(ctx, assistantTurn); err != nil {
		s.logger.Warn().Err(err).Msg("Failed to store assistant turn")
	}

	// Auto-trigger L1/L2 compression in background if needed
	go s.autoCompress(ctx, req.ConversationID)
}

// autoCompress checks if L1 (daily summaries) or L2 (weekly distillation) need
// to run for a conversation and triggers them in-process. This ensures memory
// layers stay populated without relying on the external compression daemon.
//
// Index:
// - Purpose: Keep L1/L2 memory layers populated after each companion chat turn
// - Flow: de-dupe per conversation → derive bounded context → run pending L1 compression → run L2 weekly distillation
// - SideEffects: may invoke summarizer/LLM and write summary rows to SQLite
// - FailureModes: context cancellation/timeouts; missing summarizer; DB/LLM errors (logged, best-effort)
// - Related: ConversationMemory.RunPendingDailyCompression, ConversationMemory.RunWeeklyDistillation
// - Keywords: companion_memory, auto_compress, L1, L2, summaries, distillation
func (s *Service) autoCompress(ctx context.Context, conversationID string) {
	if s.memory == nil {
		return
	}
	if conversationID == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	s.autoCompressMu.Lock()
	if _, ok := s.autoCompressInFlight[conversationID]; ok {
		s.autoCompressMu.Unlock()
		s.logger.Debug().Str("conversation_id", conversationID).Msg("Auto compression already in flight")
		return
	}
	s.autoCompressInFlight[conversationID] = struct{}{}
	s.autoCompressMu.Unlock()
	defer func() {
		s.autoCompressMu.Lock()
		delete(s.autoCompressInFlight, conversationID)
		s.autoCompressMu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// L1: summarize the next pending day (catch-up/backfill)
	if err := s.memory.RunPendingDailyCompression(ctx, conversationID); err != nil {
		// "no summarizer configured" is expected when LLM is not set up for summarization
		if err.Error() != "no summarizer configured" {
			s.logger.Debug().Err(err).Str("conversation_id", conversationID).Msg("Auto L1 compression skipped")
		}
	} else {
		s.logger.Debug().Str("conversation_id", conversationID).Msg("Auto L1 compression completed")
	}

	// L2: distill old summaries into history
	if err := s.memory.RunWeeklyDistillation(ctx, conversationID); err != nil {
		if err.Error() != "no summarizer configured" {
			s.logger.Debug().Err(err).Str("conversation_id", conversationID).Msg("Auto L2 distillation skipped")
		}
	} else {
		s.logger.Debug().Str("conversation_id", conversationID).Msg("Auto L2 distillation completed")
	}
}

// SetContext stores a context variable for a conversation.
func (s *Service) SetContext(ctx context.Context, req ContextSetRequest) (*ContextSetResponse, error) {
	if req.ConversationID == "" {
		return nil, fmt.Errorf("conversation_id is required")
	}
	if req.Key == "" {
		return nil, fmt.Errorf("key is required")
	}

	scope, err := parseContextScope(req.Scope)
	if err != nil {
		return nil, err
	}

	params := contextvar.PutParams{
		ConversationID: req.ConversationID,
		Scope:          scope,
		Key:            req.Key,
		Value:          req.Value,
		Source:         "api",
		Upsert:         true,
	}

	if req.TTLSeconds > 0 {
		params.TTL = time.Duration(req.TTLSeconds) * time.Second
	}

	v, err := s.contextStore.Put(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("store context: %w", err)
	}

	return &ContextSetResponse{
		ID:    v.ID,
		Key:   v.Key,
		Scope: string(v.Scope),
	}, nil
}

// GetContext retrieves all context variables for a conversation.
func (s *Service) GetContext(ctx context.Context, conversationID string) (*ContextGetResponse, error) {
	if conversationID == "" {
		return nil, fmt.Errorf("conversation_id is required")
	}

	result, err := s.contextStore.Query(ctx, contextvar.QueryParams{
		ConversationID: conversationID,
		Limit:          100,
		OrderBy:        "key",
	})
	if err != nil {
		return nil, fmt.Errorf("query context: %w", err)
	}

	// Convert to response format
	variables := make([]ContextVariable, len(result.Variables))
	for i, v := range result.Variables {
		// Unmarshal JSON value for cleaner display
		var value interface{}
		if len(v.ValueJSON) > 0 {
			_ = json.Unmarshal(v.ValueJSON, &value)
		}
		variables[i] = ContextVariable{
			ID:          v.ID,
			Key:         v.Key,
			Value:       value,
			Scope:       string(v.Scope),
			CreatedAt:   v.CreatedAt,
			UpdatedAt:   v.UpdatedAt,
			AccessCount: v.AccessCount,
		}
	}

	return &ContextGetResponse{
		ConversationID: conversationID,
		Variables:      variables,
		TotalCount:     result.TotalCount,
	}, nil
}

// DeleteContext removes a context variable.
func (s *Service) DeleteContext(ctx context.Context, conversationID, key, scope string) error {
	if conversationID == "" {
		return fmt.Errorf("conversation_id is required")
	}
	if key == "" {
		return fmt.Errorf("key is required")
	}

	s_, err := parseContextScope(scope)
	if err != nil {
		return err
	}

	return s.contextStore.DeleteByKey(ctx, conversationID, s_, key)
}

// ClearConversation removes all context for a conversation.
func (s *Service) ClearConversation(ctx context.Context, conversationID string) (int, error) {
	if conversationID == "" {
		return 0, fmt.Errorf("conversation_id is required")
	}
	return s.contextStore.DeleteByConversation(ctx, conversationID)
}

// ContextSetRequest sets a context variable.
type ContextSetRequest struct {
	ConversationID string      `json:"conversation_id"`
	Key            string      `json:"key"`
	Value          interface{} `json:"value"`
	Scope          string      `json:"scope,omitempty"`
	TTLSeconds     int         `json:"ttl_seconds,omitempty"`
}

// ContextSetResponse is the result of setting context.
type ContextSetResponse struct {
	ID    string `json:"id"`
	Key   string `json:"key"`
	Scope string `json:"scope"`
}

// ContextGetResponse is the context for a conversation.
type ContextGetResponse struct {
	ConversationID string            `json:"conversation_id"`
	Variables      []ContextVariable `json:"variables"`
	TotalCount     int               `json:"total_count"`
}

// ContextVariable is a context variable in API responses.
type ContextVariable struct {
	ID          string      `json:"id"`
	Key         string      `json:"key"`
	Value       interface{} `json:"value"`
	Scope       string      `json:"scope"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	AccessCount int         `json:"access_count"`
}

func parseContextScope(scope string) (contextvar.Scope, error) {
	if scope == "" || scope == "conversation" {
		return contextvar.ScopeConversation, nil
	}
	switch scope {
	case "global":
		return contextvar.ScopeGlobal, nil
	case "turn":
		return contextvar.ScopeTurn, nil
	default:
		return "", fmt.Errorf("invalid scope %q", scope)
	}
}

// Memory returns the conversation memory store.
// Returns nil if memory features are disabled.
func (s *Service) Memory() *ConversationMemory {
	return s.memory
}

// GetMemoryStats returns memory statistics for a conversation.
func (s *Service) GetMemoryStats(ctx context.Context, conversationID string) (*MemoryStats, error) {
	if s.memory == nil {
		return nil, fmt.Errorf("memory features not enabled")
	}
	return s.memory.GetStats(ctx, conversationID)
}

// GetMemoryContext returns the formatted memory context for a conversation.
// This is what gets injected into the system prompt.
func (s *Service) GetMemoryContext(ctx context.Context, conversationID string) (string, error) {
	if s.memory == nil {
		return "", fmt.Errorf("memory features not enabled")
	}
	return s.memory.GetContext(ctx, conversationID)
}

// ExportMemory exports all memory state for debugging/inspection.
func (s *Service) ExportMemory(ctx context.Context, conversationID string) (json.RawMessage, error) {
	if s.memory == nil {
		return nil, fmt.Errorf("memory features not enabled")
	}
	return s.memory.Export(ctx, conversationID)
}

// ClearMemory removes all conversation memory for a conversation.
func (s *Service) ClearMemory(ctx context.Context, conversationID string) error {
	if s.memory == nil {
		return fmt.Errorf("memory features not enabled")
	}
	return s.memory.Clear(ctx, conversationID)
}

// ListConversations returns all conversations.
func (s *Service) ListConversations(ctx context.Context, limit int) ([]ConversationSummary, error) {
	if s.memory == nil {
		return nil, fmt.Errorf("memory features not enabled")
	}
	return s.memory.ListConversations(ctx, limit)
}

// GetConversationMessages returns messages for a specific conversation.
func (s *Service) GetConversationMessages(ctx context.Context, conversationID string, limit int) ([]ConversationTurn, error) {
	if s.memory == nil {
		return nil, fmt.Errorf("memory features not enabled")
	}
	return s.memory.GetConversationMessages(ctx, conversationID, limit)
}

// DeleteMessage removes a single message from a conversation.
func (s *Service) DeleteMessage(ctx context.Context, conversationID, messageID string) error {
	if s.memory == nil {
		return fmt.Errorf("memory features not enabled")
	}
	return s.memory.DeleteMessage(ctx, conversationID, messageID)
}

// SoftDeleteConversation marks a conversation as deleted without removing data.
func (s *Service) SoftDeleteConversation(ctx context.Context, conversationID string) error {
	if s.memory == nil {
		return fmt.Errorf("memory features not enabled")
	}
	return s.memory.SoftDeleteConversation(ctx, conversationID)
}

// RenameConversation sets or updates the custom title for a conversation.
func (s *Service) RenameConversation(ctx context.Context, conversationID, title string) error {
	if s.memory == nil {
		return fmt.Errorf("memory features not enabled")
	}
	return s.memory.RenameConversation(ctx, conversationID, title)
}

// LinkConversationAgent associates a conversation with an agent ID.
func (s *Service) LinkConversationAgent(ctx context.Context, conversationID, agentID string) error {
	if s.memory == nil {
		return fmt.Errorf("memory features not enabled")
	}
	return s.memory.LinkConversationAgent(ctx, conversationID, agentID)
}

// PersonalityInfo contains the full personality state and built system prompt.
type PersonalityInfo struct {
	// Profile contains the raw personality dimensions and learned preferences.
	Profile *PersonalityProfile `json:"profile"`

	// SystemPrompt is the built prompt including personality adjustments.
	SystemPrompt string `json:"system_prompt"`

	// MemoryContext is the conversation memory summary if available.
	MemoryContext string `json:"memory_context,omitempty"`
}

// GetPersonalityInfo returns the personality profile and built system prompt for a conversation.
func (s *Service) GetPersonalityInfo(ctx context.Context, conversationID string) (*PersonalityInfo, error) {
	// Get base personality
	basePersonality := s.config.DefaultPersonality
	if stored, err := s.contextStore.GetByKey(ctx, conversationID, contextvar.ScopeGlobal, "personality/base"); err == nil {
		var personality string
		if err := json.Unmarshal(stored.ValueJSON, &personality); err == nil && personality != "" {
			basePersonality = personality
		}
	}

	// Create evolving personality manager
	evolvingPersonality := NewEvolvingPersonality(s.contextStore, conversationID)

	// Get the profile
	profile, err := evolvingPersonality.GetProfile(ctx)
	if err != nil {
		return nil, fmt.Errorf("get personality profile: %w", err)
	}

	// Build the full system prompt
	systemPrompt, err := evolvingPersonality.BuildSystemPrompt(ctx, basePersonality)
	if err != nil {
		s.logger.Warn().Err(err).Msg("Failed to build system prompt, using base")
		systemPrompt = basePersonality
	}

	// Get memory context if available
	var memoryContext string
	if s.memory != nil {
		memoryContext, _ = s.memory.GetContext(ctx, conversationID)
	}

	return &PersonalityInfo{
		Profile:       profile,
		SystemPrompt:  systemPrompt,
		MemoryContext: memoryContext,
	}, nil
}

// UpdatePersonalityDimension updates a single personality dimension value.
func (s *Service) UpdatePersonalityDimension(ctx context.Context, conversationID, dimensionName string, value float64) error {
	// Clamp value to valid range
	if value < 0 {
		value = 0
	}
	if value > 1 {
		value = 1
	}

	evolvingPersonality := NewEvolvingPersonality(s.contextStore, conversationID)

	profile, err := evolvingPersonality.GetProfile(ctx)
	if err != nil {
		return fmt.Errorf("get personality profile: %w", err)
	}

	// Find and update the dimension
	found := false
	for i := range profile.Dimensions {
		if profile.Dimensions[i].Name == dimensionName {
			profile.Dimensions[i].Value = value
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("dimension %q not found", dimensionName)
	}

	return evolvingPersonality.SaveProfile(ctx, profile)
}

// presenceEnabled returns true if presence generation is configured and enabled.
func (s *Service) presenceEnabled() bool {
	return s.config.PresenceConfig != nil && s.config.PresenceConfig.Enabled && s.config.SkillRunner != nil
}

// generatePresence runs presence/orchestrate and executes sub-skills in parallel.
// It updates the response with presence assets.
func (s *Service) generatePresence(ctx context.Context, req ChatRequest, resp *ChatResponse) {
	if !s.presenceEnabled() {
		return
	}

	presenceCfg := s.config.PresenceConfig

	// Build orchestrate input
	input := map[string]any{
		"text":                resp.Response,
		"conversation_id":     req.ConversationID,
		"generate_voice":      presenceCfg.VoiceEnabled,
		"generate_background": presenceCfg.BackgroundEnabled,
	}
	if presenceCfg.CharacterID != "" {
		input["character_id"] = presenceCfg.CharacterID
	}
	if presenceCfg.Style != "" {
		input["style"] = presenceCfg.Style
	}
	if presenceCfg.VoiceID != "" {
		input["voice_id"] = presenceCfg.VoiceID
	}
	// Add scene from ChatAction if present
	if resp.Action != nil && resp.Action.Scene != "" {
		input["scene"] = resp.Action.Scene
	}

	// Run presence/orchestrate to parse emotions and get sub-skill params
	result, err := s.config.SkillRunner.Run(ctx, "presence/orchestrate", input)
	if err != nil {
		s.logger.Warn().Err(err).Msg("presence/orchestrate failed")
		return
	}
	if !result.Success {
		s.logger.Warn().Str("error", result.Error).Msg("presence/orchestrate returned error")
		return
	}

	// Parse orchestrate output
	var orchOutput struct {
		Emotion          string         `json:"emotion"`
		Intensity        float64        `json:"intensity"`
		DisplayText      string         `json:"display_text"`
		Markers          []string       `json:"markers"`
		DetectedEmoji    []string       `json:"detected_emoji"`
		BackgroundParams map[string]any `json:"background_params"`
		CharacterParams  map[string]any `json:"character_params"`
		VoiceParams      map[string]any `json:"voice_params"`
	}
	if err := json.Unmarshal(result.Output, &orchOutput); err != nil {
		s.logger.Warn().Err(err).Msg("failed to parse presence/orchestrate output")
		return
	}

	// Initialize presence bundle with parsed emotion data
	bundle := &PresenceBundle{
		Emotion:       orchOutput.Emotion,
		Intensity:     orchOutput.Intensity,
		DisplayText:   orchOutput.DisplayText,
		Markers:       orchOutput.Markers,
		DetectedEmoji: orchOutput.DetectedEmoji,
	}

	// Run sub-skills in parallel using goroutines
	type subSkillResult struct {
		skill  string
		output json.RawMessage
		err    error
	}
	resultCh := make(chan subSkillResult, 3)
	running := 0

	// Background generation
	if orchOutput.BackgroundParams != nil {
		running++
		go func() {
			res, err := s.config.SkillRunner.Run(ctx, "presence/background", orchOutput.BackgroundParams)
			if err != nil {
				resultCh <- subSkillResult{skill: "background", err: err}
				return
			}
			resultCh <- subSkillResult{skill: "background", output: res.Output}
		}()
	}

	// Character overlay selection
	if orchOutput.CharacterParams != nil {
		running++
		go func() {
			res, err := s.config.SkillRunner.Run(ctx, "presence/character", orchOutput.CharacterParams)
			if err != nil {
				resultCh <- subSkillResult{skill: "character", err: err}
				return
			}
			resultCh <- subSkillResult{skill: "character", output: res.Output}
		}()
	}

	// Voice generation
	if orchOutput.VoiceParams != nil {
		running++
		go func() {
			res, err := s.config.SkillRunner.Run(ctx, "presence/voice", orchOutput.VoiceParams)
			if err != nil {
				resultCh <- subSkillResult{skill: "voice", err: err}
				return
			}
			resultCh <- subSkillResult{skill: "voice", output: res.Output}
		}()
	}

	// Collect results with context cancellation support
	for i := 0; i < running; i++ {
		var res subSkillResult
		select {
		case res = <-resultCh:
			// Got result, process below
		case <-ctx.Done():
			// Context cancelled, stop waiting for remaining results
			s.logger.Warn().Err(ctx.Err()).Int("remaining", running-i).Msg("presence bundle collection cancelled")
			bundle.Errors = append(bundle.Errors, fmt.Sprintf("cancelled: %v", ctx.Err()))
			resp.Presence = bundle
			return
		}
		if res.err != nil {
			s.logger.Warn().Err(res.err).Str("skill", res.skill).Msg("presence sub-skill failed")
			bundle.Errors = append(bundle.Errors, fmt.Sprintf("%s: %v", res.skill, res.err))
			bundle.CacheMisses++
			continue
		}

		switch res.skill {
		case "background":
			var bgOut struct {
				ImageDigest string `json:"image_digest"`
				Cached      bool   `json:"cached"`
			}
			if err := json.Unmarshal(res.output, &bgOut); err == nil {
				bundle.BackgroundDigest = bgOut.ImageDigest
				if bgOut.Cached {
					bundle.CacheHits++
				} else {
					bundle.CacheMisses++
				}
			}

		case "character":
			var charOut struct {
				Overlay *struct {
					OverlayDigest string `json:"overlay_digest"`
				} `json:"overlay"`
			}
			if err := json.Unmarshal(res.output, &charOut); err == nil && charOut.Overlay != nil {
				bundle.OverlayDigest = charOut.Overlay.OverlayDigest
			}

		case "voice":
			var voiceOut struct {
				AudioDigest string `json:"audio_digest"`
				DurationMS  int    `json:"duration_ms"`
				Cached      bool   `json:"cached"`
			}
			if err := json.Unmarshal(res.output, &voiceOut); err == nil {
				bundle.AudioDigest = voiceOut.AudioDigest
				bundle.AudioDurationMS = voiceOut.DurationMS
				if voiceOut.Cached {
					bundle.CacheHits++
				} else {
					bundle.CacheMisses++
				}
			}
		}
	}

	resp.Presence = bundle

	// Also update Tone from presence if not already set
	if resp.Tone == nil && bundle.Emotion != "" {
		resp.Tone = &ChatTone{
			Emotion:   bundle.Emotion,
			Intensity: bundle.Intensity,
		}
	}
}
