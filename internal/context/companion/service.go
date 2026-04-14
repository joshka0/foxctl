package companion

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/runtime/engine"
	"github.com/jkatigb/agentctl/internal/runtime/hooks"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/contextvar"
	einoadapter "github.com/jkatigb/agentctl/internal/v2/adapters/eino"
	"github.com/jkatigb/agentctl/internal/v2/runtime/contextbuilder"
)

// EngineType selects the execution engine for the companion service.
type EngineType string

const (
	// EngineTypeLLMChat uses LLMChatEngine for OpenAI-compatible execution.
	EngineTypeLLMChat EngineType = "llmchat"

	// EngineTypeEino uses EinoEngineAdapter for graph-based tool execution.
	EngineTypeEino EngineType = "eino"
)

const (
	defaultMaxHistoryTurns   = 50
	DefaultSubcallWorkerRole = "subcall_worker"
)

// Service is the companion chat service.
type Service struct {
	contextStore contextvar.Store
	memory       *ConversationMemory
	memoryDB     *sql.DB
	layeredCtx   *contextbuilder.Builder
	config       ServiceConfig
	logger       zerolog.Logger

	turnLock Locker

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

	// LLMBaseURL overrides the API base URL for self-hosted or OpenAI-compatible backends.
	LLMBaseURL string

	// LLMAuthMode controls authentication for LLM requests: auto, none, bearer, header.
	LLMAuthMode string

	// LLMAuthHeader names the header when auth mode is header.
	LLMAuthHeader string

	// LLMAuthPrefix prefixes the API key for bearer/header auth.
	LLMAuthPrefix string

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

	// MemoryBehavior configures reply-time memory retrieval and compression policy.
	MemoryBehavior MemoryBehavior

	// MemoryStore is the named memory store for semantic search integration (optional).
	// When set, companion summaries will be stored in named_memory for semantic search.
	MemoryStore storage.MemoryStore

	// MemoryWorkspace is the workspace ID for semantic search scoping.
	MemoryWorkspace string

	// Config is the platform config for embedder creation (optional).
	// When set along with MemoryStore, an embedder will be created automatically
	// using the configured embedding provider (VOYAGE_API_KEY from .env).
	Config *config.Config

	// SessionRecallProvider injects related prior sessions into the prompt when configured.
	SessionRecallProvider SessionRecallProvider

	// TopOfMindProvider returns compact ACA top-of-mind context for the current workspace.
	TopOfMindProvider func(ctx context.Context, workspace string) (HarnessLayer, error)

	// TaskContinuityProvider returns compact task continuity context for the current workspace.
	TaskContinuityProvider func(ctx context.Context, workspace string) (HarnessLayer, error)

	// SubcallProvider executes one bounded recursive subcall, typically via Jido runtime children.
	SubcallProvider func(ctx context.Context, req CompanionSubcallRequest) (CompanionSubcallResult, error)

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

// NewService initializes the companion service with optional memory and embeddings.
//
// The turnLock parameter provides per-conversation mutual exclusion for turn
// processing. Pass a shared Locker across all requests to the same server so
// that concurrent requests for the same conversation are serialized. If nil, a
// new in-memory TurnLock is created (useful for tests but NOT for production where the
// Service is constructed per-request).
//
// Index:
// - Purpose: Configure companion service defaults and optional conversation memory
// - Flow: normalize config → init memory/summarizer → return service
// - SideEffects: initializes memory store; may create embedder
// - FailureModes: memory initialization errors logged as warnings
// - Related: NewConversationMemory, NewLLMSummarizer
// - Keywords: companion_service, memory, summarizer, embedder, config
func NewService(store contextvar.Store, cfg ServiceConfig, turnLock Locker) *Service {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 20
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.DefaultPersonality == "" {
		cfg.DefaultPersonality = DefaultRLMPersonality
	}
	cfg.MemoryBehavior = normalizeMemoryBehavior(cfg.MemoryBehavior)

	if turnLock == nil {
		turnLock = NewTurnLock()
	}

	svc := &Service{
		contextStore: store,
		config:       cfg,
		logger:       cfg.Logger,
		memoryDB:     cfg.MemoryDB,

		turnLock: turnLock,

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
		opts = append(opts, WithTokenCounter(NewTikTokenCounter(cfg.LLMModel)))

		// Create LLM summarizer only if credentials are available
		if cfg.LLMProvider != "" && cfg.LLMAPIKey != "" {
			summarizer := NewLLMSummarizer(LLMSummarizerConfig{
				Provider:   cfg.LLMProvider,
				APIKey:     cfg.LLMAPIKey,
				Model:      cfg.LLMModel,
				BaseURL:    cfg.LLMBaseURL,
				AuthMode:   cfg.LLMAuthMode,
				AuthHeader: cfg.LLMAuthHeader,
				AuthPrefix: cfg.LLMAuthPrefix,
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
			svc.layeredCtx = newCompanionContextBuilder(memory)
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

	// ResponseSchema is an optional JSON Schema describing the exact final user-facing structure.
	ResponseSchema json.RawMessage `json:"response_schema,omitempty"`

	// ResponseKeys is an optional ordered list of expected top-level JSON keys.
	ResponseKeys []string `json:"response_keys,omitempty"`
}

type memoryPromptMetadata struct {
	HasLayeredContext   bool
	ImplicitRecallCount int
	SessionRecallCount  int
	HasTopOfMind        bool
	HasTaskContinuity   bool
}

type HarnessLayer struct {
	Content     string   `json:"content,omitempty"`
	Refs        []string `json:"refs,omitempty"`
	ArtifactRef string   `json:"artifact_ref,omitempty"`
}

type conversationContextFrame struct {
	Messages          []engine.Message
	Turns             []ConversationTurn
	HistoryRecap      string
	HasHistory        bool
	ContinuationQuery string
	WorkspaceState    string
	ArtifactRefs      []string
}

type CompanionSubcallRequest struct {
	ParentAgentID  string
	Workspace      string
	ConversationID string
	Prompt         string
	HarnessState   string
	Role           string
	LLMProvider    string
	LLMModel       string
	MaxDepth       int
	MaxIterations  int
	MaxSubcalls    int
}

type CompanionSubcallResult struct {
	Summary        string         `json:"summary,omitempty"`
	EvidenceRefs   []string       `json:"evidence_refs,omitempty"`
	RetrievedPaths []string       `json:"retrieved_paths,omitempty"`
	ArtifactRef    string         `json:"artifact_ref,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

func resolveCompanionSubcallRole(role string) string {
	role = strings.TrimSpace(role)
	if role == "" {
		return DefaultSubcallWorkerRole
	}
	return role
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

	// TTSProvider selects the voice provider: elevenlabs|pocket.
	TTSProvider string `json:"tts_provider,omitempty"`

	// PocketBaseURL is the Pocket TTS server URL when TTSProvider is pocket.
	PocketBaseURL string `json:"pocket_base_url,omitempty"`

	// RewriteForTTS rewrites model output into concise speech-ready text.
	RewriteForTTS bool `json:"rewrite_for_tts,omitempty"`

	// RewriteModel is the OpenRouter model used for rewrite_for_tts.
	RewriteModel string `json:"rewrite_model,omitempty"`

	// RewriteMaxChars caps rewrite output length.
	RewriteMaxChars int `json:"rewrite_max_chars,omitempty"`
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

type ContinuityDetail struct {
	Source         string   `json:"source,omitempty"`
	VisibleSummary string   `json:"visible_summary,omitempty"`
	MemoryQuery    string   `json:"memory_query,omitempty"`
	SubcallPrompt  string   `json:"subcall_prompt,omitempty"`
	LayerHits      []string `json:"layer_hits,omitempty"`
	SubcallCount   int      `json:"subcall_count,omitempty"`
	ArtifactRefs   []string `json:"artifact_refs,omitempty"`
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

	// Continuity describes how the RLM-style controller decided to source this answer.
	Continuity *ContinuityDetail `json:"continuity,omitempty"`
}

// ChatStreamDelta is one streamed text/tool delta for a companion turn.
type ChatStreamDelta struct {
	ContentDelta string `json:"content_delta,omitempty"`
	FinishReason string `json:"finish_reason,omitempty"`
}

// ChatToolCallEvent describes one streamed tool call.
type ChatToolCallEvent struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ChatToolResultEvent describes one streamed tool result.
type ChatToolResultEvent struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Content    string `json:"content,omitempty"`
	IsError    bool   `json:"is_error,omitempty"`
}

// ChatStreamCallbacks receives streaming updates for one companion turn.
type ChatStreamCallbacks struct {
	OnDelta      func(ChatStreamDelta)
	OnToolCall   func(ChatToolCallEvent)
	OnToolResult func(ChatToolResultEvent)
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

	unlock, err := s.turnLock.Lock(ctx, req.ConversationID)
	if err != nil {
		return nil, fmt.Errorf("turn lock: %w", err)
	}
	defer unlock()

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
	if engineType != EngineTypeLLMChat && engineType != EngineTypeEino {
		return nil, fmt.Errorf("unsupported engine_type %q (only %q and %q are supported)", engineType, EngineTypeLLMChat, EngineTypeEino)
	}

	s.logger.Debug().
		Str("exec_mode", string(execMode)).
		Str("engine_type", string(engineType)).
		Str("conversation_id", req.ConversationID).
		Msg("Processing chat request")

	// Create RLM tool executor
	rlmExecutor := engine.NewRLMToolExecutor(s.contextStore, req.ConversationID)
	if s.memoryDB != nil {
		rlmExecutor.SetCompanionDB(s.memoryDB)
	}

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
	frame := s.buildConversationContextFrame(ctx, req)
	systemPrompt, promptMeta, err := s.buildSystemPromptWithFrame(ctx, req, frame)
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
		resp, err = s.chatWithStoryLoop(ctx, req, frame, rlmExecutor, systemPrompt, promptMeta, start)
	} else {
		resp, err = s.chatWithLLMChat(ctx, req, frame, rlmExecutor, systemPrompt, promptMeta, start, nil)
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

// ChatStreaming executes one companion turn and emits streamed deltas when supported.
func (s *Service) ChatStreaming(ctx context.Context, req ChatRequest, callbacks ChatStreamCallbacks) (*ChatResponse, error) {
	start := time.Now()

	if req.ConversationID == "" {
		return nil, fmt.Errorf("conversation_id is required")
	}
	if req.Message == "" {
		return nil, fmt.Errorf("message is required")
	}

	unlock, err := s.turnLock.Lock(ctx, req.ConversationID)
	if err != nil {
		return nil, fmt.Errorf("turn lock: %w", err)
	}
	defer unlock()

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
	if engineType != EngineTypeLLMChat && engineType != EngineTypeEino {
		return nil, fmt.Errorf("unsupported engine_type %q (only %q and %q are supported)", engineType, EngineTypeLLMChat, EngineTypeEino)
	}
	if execMode == agent.ModeStory {
		return s.Chat(ctx, req)
	}

	rlmExecutor := engine.NewRLMToolExecutor(s.contextStore, req.ConversationID)
	if s.memoryDB != nil {
		rlmExecutor.SetCompanionDB(s.memoryDB)
	}
	if s.config.MemoryStore != nil && s.config.MemoryWorkspace != "" {
		rlmExecutor.SetMemoryStore(s.config.MemoryStore, s.config.MemoryWorkspace)
		if s.config.Config != nil {
			provider, err := semantic.NewProviderForScope(semantic.ScopeMemory, *s.config.Config)
			if err != nil {
				s.logger.Warn().Err(err).Msg("Could not create embedding provider for semantic_query; falling back to text search")
			} else {
				rlmExecutor.SetEmbedProvider(provider)
			}
		}
	}

	frame := s.buildConversationContextFrame(ctx, req)
	systemPrompt, promptMeta, err := s.buildSystemPromptWithFrame(ctx, req, frame)
	if err != nil {
		return nil, fmt.Errorf("build system prompt: %w", err)
	}
	if execMode == agent.ModeAutonomous || execMode == agent.ModeProactive {
		systemPrompt = s.addAutonomousInstructions(systemPrompt, execMode)
	}

	resp, err := s.chatWithLLMChat(ctx, req, frame, rlmExecutor, systemPrompt, promptMeta, start, &callbacks)
	if err != nil {
		return nil, err
	}

	s.storeConversationTurns(ctx, req, resp, start)
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
func (s *Service) chatWithLLMChat(ctx context.Context, req ChatRequest, frame conversationContextFrame, rlmExecutor *engine.RLMToolExecutor, systemPrompt string, promptMeta memoryPromptMetadata, start time.Time, stream *ChatStreamCallbacks) (*ChatResponse, error) {
	systemPrompt, controllerPlan, controllerTokens, subcallResult := s.prepareCompanionSystemPrompt(ctx, req, frame, systemPrompt)
	eng, input, toolExecutor, toolDefs, usesRLM, engineCfg, err := s.buildCompanionExecutionEngine(ctx, req, frame, rlmExecutor, systemPrompt, promptMeta, controllerPlan)
	if err != nil {
		return nil, err
	}
	output, err := s.runCompanionAgentEngine(ctx, eng, input, stream)
	if err != nil {
		return nil, fmt.Errorf("engine run: %w", err)
	}
	if controllerTokens.InputTokens > 0 || controllerTokens.OutputTokens > 0 {
		output.Tokens.Add(controllerTokens.InputTokens, controllerTokens.OutputTokens)
	}

	responseText, diagnosticError, output := s.postProcessCompanionOutput(ctx, req, frame, rlmExecutor, systemPrompt, input, engineCfg, toolExecutor, toolDefs, usesRLM, output)
	resp := s.buildCompanionChatResponse(req, rlmExecutor, start, responseText, diagnosticError, output)
	if controllerPlan.Source != "" || controllerPlan.VisibleSummary != "" || controllerPlan.MemoryQuery != "" {
		resp.Continuity = &ContinuityDetail{
			Source:         controllerPlan.Source,
			VisibleSummary: controllerPlan.VisibleSummary,
			MemoryQuery:    controllerPlan.MemoryQuery,
			SubcallPrompt:  controllerPlan.SubcallPrompt,
			LayerHits:      continuityLayerHits(frame, promptMeta, rlmExecutor.QueryCount(), continuitySubcallCount(controllerPlan, subcallResult) > 0),
			SubcallCount:   continuitySubcallCount(controllerPlan, subcallResult),
			ArtifactRefs:   continuityArtifactRefs(frame, subcallResult),
		}
	} else {
		layerHits := continuityLayerHits(frame, promptMeta, rlmExecutor.QueryCount(), continuitySubcallCount(controllerPlan, subcallResult) > 0)
		if len(layerHits) > 0 || len(frame.ArtifactRefs) > 0 {
			resp.Continuity = &ContinuityDetail{
				Source:       "workspace_harness",
				LayerHits:    layerHits,
				SubcallCount: 0,
				ArtifactRefs: continuityArtifactRefs(frame, subcallResult),
			}
		}
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

func (s *Service) prepareCompanionSystemPrompt(ctx context.Context, req ChatRequest, frame conversationContextFrame, systemPrompt string) (string, continuityControllerPlan, engine.TokenUsage, CompanionSubcallResult) {
	if strings.TrimSpace(frame.HistoryRecap) != "" {
		systemPrompt += "\n\n# Conversation State (Machine Generated)\n" + buildStructuredConversationState(frame)
	}
	controllerPlan, controllerTokens := s.planConversationAnswerSource(ctx, req, frame)
	systemPrompt = appendContinuityControllerPrompt(systemPrompt, controllerPlan)
	subcallResult := s.maybeRunCompanionSubcall(ctx, req, frame, controllerPlan, &systemPrompt)
	return systemPrompt, controllerPlan, controllerTokens, subcallResult
}

func appendContinuityControllerPrompt(systemPrompt string, controllerPlan continuityControllerPlan) string {
	if strings.TrimSpace(controllerPlan.VisibleSummary) == "" {
		return systemPrompt
	}
	systemPrompt += "\n\n# Continuity Controller\nsource: " + controllerPlan.Source + "\nvisible_summary: " + controllerPlan.VisibleSummary
	if strings.TrimSpace(controllerPlan.MemoryQuery) != "" {
		systemPrompt += "\nmemory_query: " + controllerPlan.MemoryQuery
	}
	if strings.TrimSpace(controllerPlan.SubcallPrompt) != "" {
		systemPrompt += "\nsubcall_prompt: " + controllerPlan.SubcallPrompt
	}
	return systemPrompt
}

func (s *Service) maybeRunCompanionSubcall(ctx context.Context, req ChatRequest, frame conversationContextFrame, controllerPlan continuityControllerPlan, systemPrompt *string) CompanionSubcallResult {
	if controllerPlan.Source != "subcall" || s.config.SubcallProvider == nil {
		return CompanionSubcallResult{}
	}
	subcallResult, subcallErr := s.config.SubcallProvider(ctx, CompanionSubcallRequest{
		ParentAgentID:  resolveCompanionParentAgentID(req),
		Workspace:      resolveCompanionWorkspace(req, s.config.HookContext.WorkspaceRoot),
		ConversationID: req.ConversationID,
		Prompt:         chooseFirstNonEmpty(controllerPlan.SubcallPrompt, controllerPlan.MemoryQuery, req.Message),
		HarnessState:   frame.WorkspaceState,
		Role:           DefaultSubcallWorkerRole,
		LLMProvider:    s.config.LLMProvider,
		LLMModel:       s.config.LLMModel,
		MaxDepth:       1,
		MaxIterations:  4,
		MaxSubcalls:    0,
	})
	if subcallErr != nil {
		s.logger.Warn().Err(subcallErr).Str("conversation_id", req.ConversationID).Msg("companion subcall failed")
	}
	if strings.TrimSpace(subcallResult.Summary) != "" {
		*systemPrompt += "\n\n# Subcall Result\n" + strings.TrimSpace(subcallResult.Summary)
	}
	return subcallResult
}

func (s *Service) buildCompanionExecutionEngine(ctx context.Context, req ChatRequest, frame conversationContextFrame, rlmExecutor *engine.RLMToolExecutor, systemPrompt string, promptMeta memoryPromptMetadata, controllerPlan continuityControllerPlan) (engine.AgentEngine, engine.EngineInput, engine.ToolExecutor, []engine.ToolDef, bool, engine.LLMChatConfig, error) {
	engineCfg := s.newCompanionLLMChatConfig(req, frame, promptMeta)
	toolExecutor, toolDefs, usesRLM := s.buildTooling(rlmExecutor)
	if controllerPlan.Source == "visible_history" || controllerPlan.Source == "subcall" {
		toolExecutor = nil
		toolDefs = nil
		usesRLM = false
		engineCfg.RequireContextQuery = false
		engineCfg.RLMSystemPromptSuffix = ""
	}

	llmEngine, err := engine.NewLLMChatEngine(engineCfg)
	if err != nil {
		return nil, engine.EngineInput{}, nil, nil, false, engine.LLMChatConfig{}, fmt.Errorf("create engine: %w", err)
	}
	s.configureCompanionLLMEngine(llmEngine, req, toolExecutor, toolDefs, rlmExecutor, usesRLM)

	eng := engine.AgentEngine(llmEngine)
	if einoadapter.IsEinoEnabled() {
		einoEngine, err := einoadapter.ProvisionFromLLMConfig(llmEngine.Config(), toolExecutor, toolDefs)
		if err != nil {
			return nil, engine.EngineInput{}, nil, nil, false, engine.LLMChatConfig{}, fmt.Errorf("eino companion provisioning failed: %w", err)
		}
		eng = einoEngine
	}
	return eng, engine.EngineInput{SystemPrompt: systemPrompt, Messages: frame.Messages, Tools: toolDefs}, toolExecutor, toolDefs, usesRLM, engineCfg, nil
}

func (s *Service) newCompanionLLMChatConfig(req ChatRequest, frame conversationContextFrame, promptMeta memoryPromptMetadata) engine.LLMChatConfig {
	engineCfg := engine.LLMChatConfig{
		Provider:              s.config.LLMProvider,
		APIKey:                s.config.LLMAPIKey,
		BaseURL:               s.config.LLMBaseURL,
		AuthMode:              s.config.LLMAuthMode,
		AuthHeader:            s.config.LLMAuthHeader,
		AuthPrefix:            s.config.LLMAuthPrefix,
		Model:                 s.config.LLMModel,
		MaxIterations:         s.config.MaxIterations,
		Timeout:               s.config.Timeout,
		StatelessMode:         true,
		RLMSystemPromptSuffix: RLMContextInstructions,
		RequireContextQuery:   s.config.RequireContextQuery,
		HookDispatcher:        s.config.HookDispatcher,
		ActionExecutor:        s.config.ActionExecutor,
	}
	s.applyRetentionAwareContextPolicy(&engineCfg, req, frame.HasHistory, promptMeta, true)
	return engineCfg
}

func (s *Service) configureCompanionLLMEngine(llmEngine *engine.LLMChatEngine, req ChatRequest, toolExecutor engine.ToolExecutor, toolDefs []engine.ToolDef, rlmExecutor *engine.RLMToolExecutor, usesRLM bool) {
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
		llmEngine.SetToolRunner(engine.NewToolRunner(toolExecutor, nil, engine.DefaultToolRunnerConfig()))
	}
	if usesRLM {
		llmEngine.SetRLMExecutor(rlmExecutor)
	}
}

func (s *Service) runCompanionAgentEngine(ctx context.Context, eng engine.AgentEngine, input engine.EngineInput, stream *ChatStreamCallbacks) (engine.EngineOutput, error) {
	if stream == nil {
		return eng.Run(ctx, input)
	}
	if se, ok := eng.(interface {
		RunStreaming(ctx context.Context, input engine.EngineInput, streamCfg engine.StreamConfig) (engine.EngineOutput, error)
	}); ok {
		return se.RunStreaming(ctx, input, engine.StreamConfig{
			Stream: true,
			OnDelta: func(delta engine.StreamDelta) {
				if stream.OnDelta != nil {
					stream.OnDelta(ChatStreamDelta{ContentDelta: delta.ContentDelta, FinishReason: delta.FinishReason})
				}
			},
			OnToolCall: func(call engine.ToolCall) {
				if stream.OnToolCall != nil {
					stream.OnToolCall(ChatToolCallEvent{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
				}
			},
			OnToolResult: func(call engine.ToolCall, result engine.ToolResult) {
				if stream.OnToolResult != nil {
					stream.OnToolResult(ChatToolResultEvent{
						ToolCallID: result.ToolCallID,
						Name:       call.Name,
						Content:    result.Content,
						IsError:    result.IsError,
					})
				}
			},
		})
	}
	return eng.Run(ctx, input)
}

func (s *Service) postProcessCompanionOutput(ctx context.Context, req ChatRequest, frame conversationContextFrame, rlmExecutor *engine.RLMToolExecutor, systemPrompt string, input engine.EngineInput, engineCfg engine.LLMChatConfig, toolExecutor engine.ToolExecutor, toolDefs []engine.ToolDef, usesRLM bool, output engine.EngineOutput) (string, string, engine.EngineOutput) {
	enforceGrounded := req.RequireContextQuery != nil && *req.RequireContextQuery
	rawAssistantText := output.AssistantText
	responseText := strings.TrimSpace(stripThinkTags(rawAssistantText))
	output, rawAssistantText, responseText = s.maybeRetryGroundedCompanionTurn(ctx, req, rlmExecutor, input, engineCfg, toolExecutor, toolDefs, usesRLM, output, rawAssistantText, responseText, enforceGrounded)
	output, responseText = s.maybeRecoverCompanionResponse(ctx, req, frame, systemPrompt, engineCfg, toolExecutor, toolDefs, output, rawAssistantText, responseText, enforceGrounded)
	diagnosticError := ""
	if responseText == "" {
		diagnosticError = explainInvisibleResponse(output, rawAssistantText, enforceGrounded)
		responseText = "I couldn't generate a visible response. Please try again."
	}
	return responseText, diagnosticError, output
}

func (s *Service) maybeRetryGroundedCompanionTurn(ctx context.Context, req ChatRequest, rlmExecutor *engine.RLMToolExecutor, input engine.EngineInput, engineCfg engine.LLMChatConfig, toolExecutor engine.ToolExecutor, toolDefs []engine.ToolDef, usesRLM bool, output engine.EngineOutput, rawAssistantText, responseText string, enforceGrounded bool) (engine.EngineOutput, string, string) {
	if !shouldRetryGroundedTurn(enforceGrounded, output, responseText, rlmExecutor.QueryCount()) {
		return output, rawAssistantText, responseText
	}
	retryOutput, retryErr := s.retryGroundedTurn(ctx, req, engineCfg, input, toolExecutor, toolDefs, rlmExecutor, usesRLM)
	if retryErr != nil {
		s.logger.Warn().Err(retryErr).Str("conversation_id", req.ConversationID).Msg("grounded retry failed")
		return output, rawAssistantText, responseText
	}
	retryOutput.Tokens.Add(output.Tokens.InputTokens, output.Tokens.OutputTokens)
	output = retryOutput
	rawAssistantText = output.AssistantText
	responseText = strings.TrimSpace(stripThinkTags(rawAssistantText))
	s.logger.Debug().
		Str("conversation_id", req.ConversationID).
		Int("context_queries", rlmExecutor.QueryCount()).
		Int("tool_calls", len(output.ToolCalls)).
		Msg("grounded retry completed")
	return output, rawAssistantText, responseText
}

func (s *Service) maybeRecoverCompanionResponse(ctx context.Context, req ChatRequest, frame conversationContextFrame, systemPrompt string, engineCfg engine.LLMChatConfig, toolExecutor engine.ToolExecutor, toolDefs []engine.ToolDef, output engine.EngineOutput, rawAssistantText, responseText string, enforceGrounded bool) (engine.EngineOutput, string) {
	if responseText == "" && !enforceGrounded {
		recoveredText, recoveredTokens := s.recoverEmptyAssistantText(ctx, systemPrompt, frame.Messages, rawAssistantText)
		if recoveredText != "" {
			responseText = recoveredText
		}
		if recoveredTokens.InputTokens > 0 || recoveredTokens.OutputTokens > 0 {
			output.Tokens.Add(recoveredTokens.InputTokens, recoveredTokens.OutputTokens)
		}
	}
	if shouldRecoverContextToolLeak(responseText, rawAssistantText, output.ToolCalls) {
		recoveredText, recoveredTokens, recoverErr := s.synthesizeContextToolAnswer(ctx, req, engineCfg, systemPrompt, frame.Messages, rawAssistantText, output.ToolCalls, output.ToolResults)
		if recoverErr != nil {
			s.logger.Warn().Err(recoverErr).Str("conversation_id", req.ConversationID).Msg("context-tool synthesis recovery failed")
		} else if strings.TrimSpace(recoveredText) != "" {
			responseText = strings.TrimSpace(recoveredText)
			output.Tokens.Add(recoveredTokens.InputTokens, recoveredTokens.OutputTokens)
		}
	}
	if enforceGrounded && len(output.ToolCalls) == 0 {
		output, responseText = s.applyForcedGroundingFallback(ctx, req, systemPrompt, engineCfg, toolExecutor, toolDefs, output, responseText)
	}
	if enforceGrounded && len(output.ToolCalls) == 0 && strings.TrimSpace(responseText) == "" {
		responseText = "I could not complete a tool-grounded research pass in this turn. Retry with a narrower target (path/function) or use a model with stronger tool-calling support."
	}
	return output, responseText
}

func (s *Service) applyForcedGroundingFallback(ctx context.Context, req ChatRequest, systemPrompt string, engineCfg engine.LLMChatConfig, toolExecutor engine.ToolExecutor, toolDefs []engine.ToolDef, output engine.EngineOutput, responseText string) (engine.EngineOutput, string) {
	forcedCalls, forcedResults, forcedEvidence := s.collectForcedResearchEvidence(ctx, toolExecutor, toolDefs, req.Message)
	if len(forcedCalls) > 0 {
		output.ToolCalls = append(output.ToolCalls, forcedCalls...)
		output.ToolResults = append(output.ToolResults, forcedResults...)
	}
	if forcedEvidence != "" {
		synthText, synthTokens, synthErr := s.synthesizeForcedResearchAnswer(ctx, req, engineCfg, systemPrompt, req.Message, forcedEvidence)
		if synthErr != nil {
			s.logger.Warn().Err(synthErr).Str("conversation_id", req.ConversationID).Msg("forced grounded synthesis failed")
		} else if strings.TrimSpace(synthText) != "" {
			responseText = strings.TrimSpace(synthText)
			output.Tokens.Add(synthTokens.InputTokens, synthTokens.OutputTokens)
			s.logger.Debug().Str("conversation_id", req.ConversationID).Int("forced_tool_calls", len(forcedCalls)).Msg("forced grounded synthesis completed")
		}
	}
	if strings.TrimSpace(responseText) == "" && len(forcedCalls) > 0 {
		responseText = buildForcedResearchFallbackReport(forcedCalls, forcedResults)
	}
	return output, responseText
}

func (s *Service) buildCompanionChatResponse(req ChatRequest, rlmExecutor *engine.RLMToolExecutor, start time.Time, responseText, diagnosticError string, output engine.EngineOutput) *ChatResponse {
	return &ChatResponse{
		ConversationID: req.ConversationID,
		Response:       responseText,
		ContextQueries: rlmExecutor.QueryCount(),
		DurationMS:     time.Since(start).Milliseconds(),
		TokenUsage: TokenUsage{
			InputTokens:  output.Tokens.InputTokens,
			OutputTokens: output.Tokens.OutputTokens,
			TotalTokens:  output.Tokens.TotalTokens,
		},
		Error: diagnosticError,
	}
}

func explainInvisibleResponse(output engine.EngineOutput, rawAssistantText string, enforceGrounded bool) string {
	reasons := make([]string, 0, 5)
	if msg := strings.TrimSpace(output.Error); msg != "" {
		reasons = append(reasons, msg)
	}

	visibleAssistantText := strings.TrimSpace(stripThinkTags(rawAssistantText))
	switch {
	case strings.TrimSpace(rawAssistantText) == "":
		reasons = append(reasons, "model returned no assistant text")
	case visibleAssistantText == "":
		reasons = append(reasons, "model returned only hidden reasoning or non-visible content")
	}

	if toolIssue := summarizeInvisibleResponseToolIssues(output); toolIssue != "" {
		reasons = append(reasons, toolIssue)
	}
	if enforceGrounded && len(output.ToolCalls) == 0 {
		reasons = append(reasons, "no grounded tool call completed")
	}

	switch output.StopReason {
	case engine.StopReasonMaxIterations:
		reasons = append(reasons, "iteration budget was exhausted before a final answer")
	case engine.StopReasonContextBudget:
		reasons = append(reasons, "context budget was exceeded before a final answer")
	case engine.StopReasonMaxTokens:
		reasons = append(reasons, "token limit was reached before a final answer")
	case engine.StopReasonCancelled:
		reasons = append(reasons, "request was cancelled before a final answer")
	case engine.StopReasonError:
		reasons = append(reasons, "engine stopped with an error before a final answer")
	}

	seen := map[string]struct{}{}
	unique := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			continue
		}
		if _, ok := seen[reason]; ok {
			continue
		}
		seen[reason] = struct{}{}
		unique = append(unique, reason)
	}
	return strings.Join(unique, "; ")
}

func summarizeInvisibleResponseToolIssues(output engine.EngineOutput) string {
	if len(output.ToolCalls) == 0 {
		return ""
	}
	toolNames := make(map[string]string, len(output.ToolCalls))
	for _, call := range output.ToolCalls {
		toolNames[call.ID] = call.Name
	}

	failures := make([]string, 0, len(output.ToolResults))
	for _, result := range output.ToolResults {
		if !result.IsError {
			continue
		}
		name := strings.TrimSpace(toolNames[result.ToolCallID])
		if name == "" {
			name = "tool"
		}
		msg := strings.TrimSpace(result.Content)
		if msg == "" {
			failures = append(failures, name+" failed")
			continue
		}
		if len(msg) > 120 {
			msg = msg[:120] + "..."
		}
		failures = append(failures, fmt.Sprintf("%s failed: %s", name, msg))
	}
	if len(failures) > 0 {
		return strings.Join(failures, "; ")
	}
	return "tool calls completed but no final text answer was produced"
}

func shouldRetryGroundedTurn(enforceGrounded bool, output engine.EngineOutput, responseText string, contextQueries int) bool {
	if !enforceGrounded {
		return false
	}
	if output.StopReason == engine.StopReasonError {
		return true
	}
	if strings.TrimSpace(responseText) == "" {
		return true
	}
	if len(output.ToolCalls) == 0 && contextQueries == 0 {
		return true
	}
	return false
}

func (s *Service) retryGroundedTurn(
	ctx context.Context,
	req ChatRequest,
	engineCfg engine.LLMChatConfig,
	input engine.EngineInput,
	toolExecutor engine.ToolExecutor,
	toolDefs []engine.ToolDef,
	rlmExecutor *engine.RLMToolExecutor,
	usesRLM bool,
) (engine.EngineOutput, error) {
	retryCfg := engineCfg
	if retryCfg.MaxIterations > 8 {
		retryCfg.MaxIterations = 8
	}
	if retryCfg.MaxIterations < 3 {
		retryCfg.MaxIterations = 3
	}

	retryInput := input
	retryInput.Messages = append(append([]engine.Message{}, input.Messages...),
		engine.NewUserMessage("RETRY CONTRACT: Your previous turn did not produce a usable evidence-backed answer. You MUST use available context/code tools before final response. Then provide specific findings with file references in `path:line` format. If evidence is missing, say so explicitly."),
	)

	return s.runLLMChat(ctx, req, retryCfg, retryInput, toolExecutor, toolDefs, rlmExecutor, usesRLM)
}

func (s *Service) collectForcedResearchEvidence(
	ctx context.Context,
	toolExecutor engine.ToolExecutor,
	toolDefs []engine.ToolDef,
	query string,
) ([]engine.ToolCall, []engine.ToolResult, string) {
	if toolExecutor == nil || strings.TrimSpace(query) == "" {
		return nil, nil, ""
	}

	available := make(map[string]struct{}, len(toolDefs))
	for _, td := range toolDefs {
		available[td.Name] = struct{}{}
	}

	type forcedStep struct {
		name string
		args map[string]any
	}

	pattern := queryToSearchPattern(query)
	explicitPath := extractLikelyFilePath(query)

	var steps []forcedStep
	if explicitPath != "" {
		steps = append(steps,
			forcedStep{name: "fs.read_file", args: map[string]any{"path": explicitPath}},
			forcedStep{name: "fs_read_file", args: map[string]any{"path": explicitPath}},
		)
	}
	steps = append(steps,
		forcedStep{name: "context_search", args: map[string]any{"query": query, "limit": 12}},
		forcedStep{name: "smart_search", args: map[string]any{"question": query, "max_snippets": 8}},
		forcedStep{name: "repo_index_search", args: map[string]any{"query": query, "limit": 20}},
		forcedStep{name: "repo_index_dag_grep", args: map[string]any{"query": query, "render": "tree", "edge_sets": []string{"structural"}, "depth": 2, "budget": 80, "k": 5}},
		forcedStep{name: "code.search", args: map[string]any{"pattern": pattern, "max_results": 40}},
	)

	var calls []engine.ToolCall
	var results []engine.ToolResult
	var evidenceBlocks []string

	for _, step := range steps {
		if _, ok := available[step.name]; !ok {
			continue
		}

		argBytes, err := json.Marshal(step.args)
		if err != nil {
			continue
		}

		callID := fmt.Sprintf("forced_%s_%d", step.name, len(calls)+1)
		resultText, callErr := toolExecutor.Execute(ctx, step.name, argBytes)

		calls = append(calls, engine.ToolCall{
			ID:        callID,
			Name:      step.name,
			Arguments: json.RawMessage(argBytes),
		})

		content := strings.TrimSpace(resultText)
		isError := callErr != nil
		if callErr != nil {
			content = fmt.Sprintf("forced tool call failed: %v", callErr)
		}
		content = truncateForPrompt(content, 5000)
		results = append(results, engine.ToolResult{
			ToolCallID: callID,
			Content:    content,
			IsError:    isError,
		})

		if content != "" {
			if isError {
				evidenceBlocks = append(evidenceBlocks, "## "+step.name+"\nERROR: "+content)
			} else {
				evidenceBlocks = append(evidenceBlocks, "## "+step.name+"\n"+content)
			}
		}
	}

	if len(evidenceBlocks) == 0 {
		return calls, results, ""
	}
	return calls, results, strings.Join(evidenceBlocks, "\n\n")
}

func (s *Service) synthesizeForcedResearchAnswer(
	ctx context.Context,
	req ChatRequest,
	engineCfg engine.LLMChatConfig,
	systemPrompt string,
	question string,
	evidence string,
) (string, engine.TokenUsage, error) {
	synthCfg := engineCfg
	synthCfg.MaxIterations = 2
	synthCfg.RequireContextQuery = false
	synthCfg.RLMSystemPromptSuffix = ""
	synthCfg.ResponseFormat = nil

	synthEngine, err := engine.NewLLMChatEngine(synthCfg)
	if err != nil {
		return "", engine.TokenUsage{}, fmt.Errorf("create synth engine: %w", err)
	}

	synthInput := engine.EngineInput{
		SystemPrompt: strings.TrimSpace(systemPrompt) + "\n\nYou are synthesizing an answer from provided tool evidence. Do not claim facts that are not in the evidence.",
		Messages: []engine.Message{
			engine.NewUserMessage(
				"Question:\n" + strings.TrimSpace(question) + "\n\n" +
					"Tool Evidence:\n" + evidence + "\n\n" +
					"Write a concise answer with sections: Findings, Evidence, Gaps, Next Steps.\n" +
					"For Evidence, include concrete file references as `path:line` whenever present in the tool outputs.",
			),
		},
	}

	synthOutput, err := synthEngine.Run(ctx, synthInput)
	if err != nil {
		return "", engine.TokenUsage{}, fmt.Errorf("run synth engine: %w", err)
	}

	text := strings.TrimSpace(stripThinkTags(synthOutput.AssistantText))
	return text, synthOutput.Tokens, nil
}

func truncateForPrompt(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n...(truncated)"
}

func buildForcedResearchFallbackReport(calls []engine.ToolCall, results []engine.ToolResult) string {
	toolNameByCallID := make(map[string]string, len(calls))
	for _, call := range calls {
		toolNameByCallID[call.ID] = call.Name
	}

	var b strings.Builder
	b.WriteString("Findings\n")
	b.WriteString("The model did not complete a native tool-calling response, so a forced tool evidence pass was executed.\n\n")
	b.WriteString("Evidence\n")
	for _, result := range results {
		name := toolNameByCallID[result.ToolCallID]
		if name == "" {
			name = result.ToolCallID
		}
		line := strings.ReplaceAll(strings.TrimSpace(result.Content), "\n", " ")
		line = truncateForPrompt(line, 260)
		if result.IsError {
			b.WriteString("- " + name + ": ERROR: " + line + "\n")
		} else {
			b.WriteString("- " + name + ": " + line + "\n")
		}
	}
	b.WriteString("\nGaps\n")
	b.WriteString("- A native model tool-calling turn did not complete in this request.\n")
	b.WriteString("- If you need deeper source grounding, retry with a narrower query (specific file/function) or a stronger tool-calling model.\n")
	return b.String()
}

func queryToSearchPattern(query string) string {
	fields := strings.Fields(query)
	if len(fields) == 0 {
		return query
	}
	var terms []string
	for _, f := range fields {
		clean := strings.Trim(strings.TrimSpace(f), ".,:;!?()[]{}\"'")
		if len(clean) < 3 {
			continue
		}
		switch strings.ToLower(clean) {
		case "the", "and", "for", "with", "this", "that", "from", "into", "how", "what", "where", "when", "which", "does", "were", "have", "has", "our", "your":
			continue
		}
		terms = append(terms, clean)
		if len(terms) == 4 {
			break
		}
	}
	if len(terms) == 0 {
		return query
	}
	return strings.Join(terms, "|")
}

func extractLikelyFilePath(query string) string {
	for _, token := range strings.Fields(query) {
		clean := strings.Trim(strings.TrimSpace(token), ".,:;!?()[]{}\"'`")
		if strings.Contains(clean, "/") &&
			(strings.Contains(clean, ".go") || strings.Contains(clean, ".ts") || strings.Contains(clean, ".tsx") || strings.Contains(clean, ".js") || strings.Contains(clean, ".rs") || strings.Contains(clean, ".py") || strings.Contains(clean, ".md")) {
			return clean
		}
	}
	return ""
}

// recoverEmptyAssistantText attempts a one-shot recovery when a model returns a
// non-empty assistant payload that becomes empty after stripping <think> tags.
func (s *Service) recoverEmptyAssistantText(ctx context.Context, systemPrompt string, messages []engine.Message, rawAssistantText string) (string, engine.TokenUsage) {
	recoveryCfg := engine.LLMChatConfig{
		Provider:            s.config.LLMProvider,
		APIKey:              s.config.LLMAPIKey,
		BaseURL:             s.config.LLMBaseURL,
		AuthMode:            s.config.LLMAuthMode,
		AuthHeader:          s.config.LLMAuthHeader,
		AuthPrefix:          s.config.LLMAuthPrefix,
		Model:               s.config.LLMModel,
		MaxIterations:       2,
		Timeout:             s.config.Timeout,
		StatelessMode:       true,
		RequireContextQuery: false,
	}

	recoveryEngine, err := engine.NewLLMChatEngine(recoveryCfg)
	if err != nil {
		s.logger.Debug().Err(err).Msg("empty-response recovery disabled: create engine failed")
		return "", engine.TokenUsage{}
	}

	recoveryMessages := append([]engine.Message{}, messages...)
	if strings.TrimSpace(rawAssistantText) != "" {
		recoveryMessages = append(recoveryMessages, engine.NewAssistantMessage(rawAssistantText))
	}
	recoveryMessages = append(recoveryMessages, engine.NewUserMessage(
		"Your previous output did not contain a visible final answer. Return only the final user-facing answer now in plain text. Do not use <think> tags.",
	))

	recoveryInput := engine.EngineInput{
		SystemPrompt: strings.TrimSpace(systemPrompt) + "\n\nReturn only plain text for the user-facing answer.",
		Messages:     recoveryMessages,
	}

	recoveryOutput, err := recoveryEngine.Run(ctx, recoveryInput)
	if err != nil {
		s.logger.Debug().Err(err).Msg("empty-response recovery call failed")
		return "", engine.TokenUsage{}
	}

	return strings.TrimSpace(stripThinkTags(recoveryOutput.AssistantText)), recoveryOutput.Tokens
}

func shouldRecoverContextToolLeak(responseText string, rawAssistantText string, calls []engine.ToolCall) bool {
	responseText = strings.TrimSpace(responseText)
	rawAssistantText = strings.TrimSpace(rawAssistantText)
	hasContextCalls := hasContextToolCalls(calls)
	hasRawContextSyntax := strings.Contains(rawAssistantText, "rlm_context_") || strings.Contains(responseText, "rlm_context_")
	if !hasContextCalls && !hasRawContextSyntax && !looksLikeContextMutationJSON(responseText) {
		return false
	}
	if responseText == "" {
		return true
	}
	if strings.Contains(rawAssistantText, "<|tool_call_end|>") {
		return true
	}
	if strings.HasPrefix(responseText, "[rlm_context_") || strings.Contains(responseText, "rlm_context_") {
		return true
	}
	if looksLikeContextMutationJSON(responseText) {
		return true
	}
	return false
}

func hasContextToolCalls(calls []engine.ToolCall) bool {
	for _, call := range calls {
		if strings.HasPrefix(strings.TrimSpace(call.Name), "rlm_context_") {
			return true
		}
	}
	return false
}

func looksLikeContextMutationJSON(text string) bool {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	valid := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
			return false
		}
		if !strings.Contains(line, `"key"`) || !strings.Contains(line, `"value"`) {
			return false
		}
		valid++
	}
	return valid > 0
}

func (s *Service) synthesizeContextToolAnswer(
	ctx context.Context,
	req ChatRequest,
	engineCfg engine.LLMChatConfig,
	systemPrompt string,
	messages []engine.Message,
	rawAssistantText string,
	calls []engine.ToolCall,
	results []engine.ToolResult,
) (string, engine.TokenUsage, error) {
	synthCfg := engineCfg
	synthCfg.MaxIterations = 2
	synthCfg.RequireContextQuery = false
	synthCfg.RLMSystemPromptSuffix = ""
	synthCfg.ResponseFormat = nil

	synthEngine, err := engine.NewLLMChatEngine(synthCfg)
	if err != nil {
		return "", engine.TokenUsage{}, fmt.Errorf("create context synthesis engine: %w", err)
	}

	resultByCallID := make(map[string]engine.ToolResult, len(results))
	for _, result := range results {
		resultByCallID[result.ToolCallID] = result
	}

	var evidence strings.Builder
	for _, call := range calls {
		if !strings.HasPrefix(strings.TrimSpace(call.Name), "rlm_context_") {
			continue
		}
		evidence.WriteString("## ")
		evidence.WriteString(strings.TrimSpace(call.Name))
		evidence.WriteString("\n")
		if args := strings.TrimSpace(string(call.Arguments)); args != "" {
			evidence.WriteString("arguments: ")
			evidence.WriteString(truncateForPrompt(args, 800))
			evidence.WriteString("\n")
		}
		if result, ok := resultByCallID[call.ID]; ok {
			if result.IsError {
				evidence.WriteString("result: ERROR: ")
			} else {
				evidence.WriteString("result: ")
			}
			evidence.WriteString(truncateForPrompt(strings.TrimSpace(result.Content), 3000))
			evidence.WriteString("\n")
		}
		evidence.WriteString("\n")
	}

	evidenceText := strings.TrimSpace(evidence.String())
	recoveryMessages := append([]engine.Message{}, messages...)
	if strings.TrimSpace(rawAssistantText) != "" {
		recoveryMessages = append(recoveryMessages, engine.NewAssistantMessage(rawAssistantText))
	}
	formatInstruction, formatMode := requestedOutputFormat(req.Message)
	formatKeys := requestedResponseKeys(req)
	instruction := "The previous draft exposed internal context-tool artifacts instead of a user-facing answer.\n\n"
	if evidenceText != "" {
		instruction += "Internal context tool results:\n" + evidenceText + "\n\n"
	} else {
		instruction += "No structured tool results were captured. Use the conversation memory already present in the system prompt and the previous draft only as hints.\n\n"
	}
	instruction += "Now answer the user's last request cleanly.\n" +
		"- Do not mention tool names.\n" +
		"- Do not emit raw tool-call syntax.\n" +
		"- Match the user's requested output format exactly.\n"
	if formatInstruction != "" {
		instruction += "- " + formatInstruction + "\n"
	}
	if schemaHint := requestedResponseSchemaHint(req); schemaHint != "" {
		instruction += "- " + schemaHint + "\n"
	}
	if formatMode == requestedOutputFormatCompactJSON && len(formatKeys) > 0 {
		instruction += "- Use exactly these JSON keys when applicable: " + strings.Join(formatKeys, ", ") + ".\n"
	}
	if formatMode == requestedOutputFormatCompactJSON {
		instruction += "- Prefer the final_answer_json tool for the final response.\n"
	}
	recoveryMessages = append(recoveryMessages, engine.NewUserMessage(
		instruction,
	))

	recoveryInput := engine.EngineInput{
		SystemPrompt: strings.TrimSpace(systemPrompt) + "\n\nReturn only the final user-facing answer.",
		Messages:     recoveryMessages,
	}
	if formatMode == requestedOutputFormatCompactJSON {
		recoveryInput.Tools = []engine.ToolDef{engine.FinalAnswerJSONToolDef()}
	}

	recoveryOutput, err := synthEngine.Run(ctx, recoveryInput)
	if err != nil {
		return "", engine.TokenUsage{}, fmt.Errorf("run context synthesis engine: %w", err)
	}
	text := strings.TrimSpace(stripThinkTags(recoveryOutput.AssistantText))
	text = applyRequestedOutputFormat(text, formatMode)
	return text, recoveryOutput.Tokens, nil
}

type requestedOutputFormatMode int

const (
	requestedOutputFormatNone requestedOutputFormatMode = iota
	requestedOutputFormatCompactJSON
	requestedOutputFormatOnlyValue
)

var replyOnlyPattern = regexp.MustCompile(`(?i)\breply\s+(?:only\s+)?with\b`)

func requestedOutputFormat(question string) (string, requestedOutputFormatMode) {
	lower := strings.ToLower(strings.TrimSpace(question))
	switch {
	case strings.Contains(lower, "compact json"),
		strings.Contains(lower, "reply as json"),
		strings.Contains(lower, "reply with json"),
		strings.Contains(lower, "return json"),
		strings.Contains(lower, "json object"):
		return "Return a single compact JSON object and nothing else.", requestedOutputFormatCompactJSON
	case replyOnlyPattern.MatchString(lower),
		strings.Contains(lower, "reply with exactly"),
		strings.Contains(lower, "respond only with"),
		strings.Contains(lower, "answer only with"):
		return "Return only the requested value or token with no explanation, labels, markdown, or extra punctuation.", requestedOutputFormatOnlyValue
	default:
		return "", requestedOutputFormatNone
	}
}

func requestedResponseKeys(req ChatRequest) []string {
	keys := make([]string, 0, len(req.ResponseKeys)+4)
	add := func(key string) {
		key = strings.TrimSpace(key)
		if key == "" {
			return
		}
		for _, existing := range keys {
			if existing == key {
				return
			}
		}
		keys = append(keys, key)
	}
	for _, key := range req.ResponseKeys {
		add(key)
	}
	if len(keys) > 0 {
		return keys
	}
	for _, key := range schemaTopLevelKeys(req.ResponseSchema) {
		add(key)
	}
	return keys
}

func schemaTopLevelKeys(schema json.RawMessage) []string {
	schema = json.RawMessage(strings.TrimSpace(string(schema)))
	if len(schema) == 0 || string(schema) == "null" {
		return nil
	}

	var raw map[string]any
	if err := json.Unmarshal(schema, &raw); err != nil {
		return nil
	}
	properties, ok := raw["properties"].(map[string]any)
	if !ok || len(properties) == 0 {
		return nil
	}

	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func requestedResponseSchemaHint(req ChatRequest) string {
	schema := strings.TrimSpace(string(req.ResponseSchema))
	if schema == "" || schema == "null" {
		return ""
	}
	return "Return JSON that matches this schema exactly: " + truncateForPrompt(schema, 1200)
}

func applyRequestedOutputFormat(text string, mode requestedOutputFormatMode) string {
	text = strings.TrimSpace(text)
	switch mode {
	case requestedOutputFormatCompactJSON:
		if extracted, ok := extractJSONObject(text); ok {
			var payload any
			if err := json.Unmarshal([]byte(extracted), &payload); err == nil {
				if compact, err := json.Marshal(payload); err == nil {
					return string(compact)
				}
			}
			return strings.TrimSpace(extracted)
		}
		return text
	case requestedOutputFormatOnlyValue:
		text = strings.Trim(text, "`")
		text = strings.TrimSpace(text)
		if strings.Contains(text, "\n") {
			for _, line := range strings.Split(text, "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					return strings.Trim(line, `"'`)
				}
			}
		}
		return strings.Trim(text, `"'`)
	default:
		return text
	}
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
func (s *Service) buildChatMessages(ctx context.Context, req ChatRequest) ([]engine.Message, string, bool) {
	frame := s.buildConversationContextFrame(ctx, req)
	return frame.Messages, frame.HistoryRecap, frame.HasHistory
}

func (s *Service) buildConversationContextFrame(ctx context.Context, req ChatRequest) conversationContextFrame {
	historyLimit := req.MaxHistoryTurns
	switch {
	case historyLimit == 0:
		historyLimit = s.config.MemoryBehavior.HistoryTurnLimit
	case historyLimit < 0:
		historyLimit = 0
	}

	frame := conversationContextFrame{
		ContinuationQuery: strings.TrimSpace(req.Message),
	}

	if s.memory != nil && historyLimit > 0 {
		turns, err := s.memory.GetConversationMessages(ctx, req.ConversationID, historyLimit)
		if err != nil {
			s.logger.Debug().
				Err(err).
				Str("conversation_id", req.ConversationID).
				Msg("Conversation history injection skipped")
		} else if len(turns) > 0 {
			frame.Turns = append(frame.Turns, turns...)
			for _, t := range turns {
				switch t.Role {
				case "user":
					frame.Messages = append(frame.Messages, engine.NewUserMessage(t.Content))
				case "assistant":
					frame.Messages = append(frame.Messages, engine.NewAssistantMessage(t.Content))
				}
			}
			frame.HasHistory = len(turns) >= 2
			frame.HistoryRecap = buildRecentConversationRecap(turns)
			frame.ContinuationQuery = buildContinuationRecallQuery(req.Message, turns)
			s.logger.Debug().
				Str("conversation_id", req.ConversationID).
				Int("history_turns", len(turns)).
				Int("history_limit", historyLimit).
				Msg("Injected conversation history")
		}
	}

	if workspace := resolveCompanionWorkspace(req, s.config.HookContext.WorkspaceRoot); workspace != "" {
		var sections []string
		if topOfMind := s.getTopOfMindContext(ctx, workspace); strings.TrimSpace(topOfMind.Content) != "" {
			sections = append(sections, "# Top Of Mind\n"+topOfMind.Content)
			if strings.TrimSpace(topOfMind.ArtifactRef) != "" {
				frame.ArtifactRefs = append(frame.ArtifactRefs, strings.TrimSpace(topOfMind.ArtifactRef))
			}
		}
		if continuity := s.getTaskContinuityContext(ctx, workspace); strings.TrimSpace(continuity.Content) != "" {
			sections = append(sections, continuity.Content)
			if strings.TrimSpace(continuity.ArtifactRef) != "" {
				frame.ArtifactRefs = append(frame.ArtifactRefs, strings.TrimSpace(continuity.ArtifactRef))
			}
		}
		frame.WorkspaceState = strings.Join(sections, "\n\n")
	}

	frame.Messages = append(frame.Messages, engine.NewUserMessage(augmentChatInputWithConversationState(req.Message, frame)))
	if strings.TrimSpace(frame.ContinuationQuery) == "" {
		frame.ContinuationQuery = strings.TrimSpace(req.Message)
	}
	return frame
}

func buildRecentConversationRecap(turns []ConversationTurn) string {
	if len(turns) == 0 {
		return ""
	}
	const maxTurns = 6
	start := 0
	if len(turns) > maxTurns {
		start = len(turns) - maxTurns
	}

	lines := make([]string, 0, maxTurns+2)
	var lastUser, lastAssistant string
	for _, turn := range turns[start:] {
		content := strings.TrimSpace(turn.Content)
		if content == "" {
			continue
		}
		if len(content) > 180 {
			content = content[:180] + "..."
		}
		switch turn.Role {
		case "user":
			lastUser = content
			lines = append(lines, "- user: "+content)
		case "assistant":
			lastAssistant = content
			lines = append(lines, "- assistant: "+content)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	if lastUser != "" {
		lines = append(lines, "Most recent user ask: "+lastUser)
	}
	if lastAssistant != "" {
		lines = append(lines, "Most recent assistant reply: "+lastAssistant)
	}
	return strings.Join(lines, "\n")
}

func buildContinuationRecallQuery(currentMessage string, turns []ConversationTurn) string {
	currentMessage = strings.TrimSpace(currentMessage)
	if len(turns) == 0 {
		return currentMessage
	}

	currentTokens := tokenizeForGrounding(currentMessage)
	if len(currentTokens) >= 8 && len(strings.Fields(currentMessage)) >= 10 {
		return currentMessage
	}

	type recallSnippet struct {
		label   string
		content string
	}

	snippets := []recallSnippet{{label: "Current user ask", content: currentMessage}}
	appendSnippet := func(label, content string) {
		content = strings.TrimSpace(content)
		if content == "" {
			return
		}
		if len(content) > 220 {
			content = content[:220] + "..."
		}
		tokens := tokenizeForGrounding(content)
		newTokens := 0
		for token := range tokens {
			if _, ok := currentTokens[token]; !ok {
				newTokens++
			}
		}
		if newTokens < 2 && len(currentTokens) > 0 {
			return
		}
		snippets = append(snippets, recallSnippet{label: label, content: content})
	}

	for i := len(turns) - 1; i >= 0; i-- {
		turn := turns[i]
		switch turn.Role {
		case "user":
			appendSnippet("Previous user ask", turn.Content)
		case "assistant":
			appendSnippet("Previous assistant reply", turn.Content)
		}
		if len(snippets) >= 3 {
			break
		}
	}

	lines := make([]string, 0, len(snippets))
	for _, snippet := range snippets {
		lines = append(lines, snippet.label+": "+snippet.content)
	}
	return strings.Join(lines, "\n")
}

func buildStructuredConversationState(frame conversationContextFrame) string {
	type stateTurn struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if !frame.HasHistory || len(frame.Turns) == 0 {
		return ""
	}

	turns := make([]stateTurn, 0, min(len(frame.Turns), 6))
	start := 0
	if len(frame.Turns) > 6 {
		start = len(frame.Turns) - 6
	}

	var lastUser string
	var lastAssistant string
	for _, turn := range frame.Turns[start:] {
		content := strings.TrimSpace(turn.Content)
		if content == "" {
			continue
		}
		content = truncateInlineForPrompt(content, 220)
		turns = append(turns, stateTurn{Role: turn.Role, Content: content})
		switch turn.Role {
		case "user":
			lastUser = content
		case "assistant":
			lastAssistant = content
		}
	}

	payload := map[string]any{
		"ongoing_conversation": true,
		"recent_turns":         turns,
		"history_recap":        strings.TrimSpace(frame.HistoryRecap),
	}
	if lastUser != "" {
		payload["last_user_ask"] = lastUser
	}
	if lastAssistant != "" {
		payload["last_assistant_reply"] = lastAssistant
	}
	if strings.TrimSpace(frame.ContinuationQuery) != "" {
		payload["continuation_query"] = strings.TrimSpace(frame.ContinuationQuery)
	}

	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return strings.TrimSpace(frame.HistoryRecap)
	}
	return string(body)
}

func augmentChatInputWithConversationState(userMessage string, frame conversationContextFrame) string {
	userMessage = strings.TrimSpace(userMessage)
	sections := make([]string, 0, 2)
	if state := strings.TrimSpace(buildStructuredConversationState(frame)); state != "" {
		sections = append(sections, "[Machine-generated conversation state]\n"+state)
	}
	if workspaceState := strings.TrimSpace(frame.WorkspaceState); workspaceState != "" {
		sections = append(sections, "[Machine-generated harness state]\n"+workspaceState)
	}
	if len(sections) == 0 {
		return userMessage
	}
	return userMessage + "\n\n" + strings.Join(sections, "\n\n")
}

func continuityLayerHits(frame conversationContextFrame, meta memoryPromptMetadata, contextQueries int, subcallUsed bool) []string {
	hits := make([]string, 0, 6)
	add := func(hit string) {
		for _, existing := range hits {
			if existing == hit {
				return
			}
		}
		hits = append(hits, hit)
	}
	if frame.HasHistory {
		add("L0")
	}
	if strings.TrimSpace(frame.HistoryRecap) != "" {
		add("L1")
	}
	if meta.HasLayeredContext {
		add("L2")
	}
	if meta.HasTopOfMind {
		add("L3")
	}
	if meta.HasTaskContinuity {
		add("L4")
	}
	if meta.ImplicitRecallCount > 0 || meta.SessionRecallCount > 0 || contextQueries > 0 || subcallUsed {
		add("L5")
	}
	return hits
}

func continuitySubcallCount(plan continuityControllerPlan, result CompanionSubcallResult) int {
	if strings.TrimSpace(plan.Source) != "subcall" {
		return 0
	}
	if strings.TrimSpace(result.Summary) == "" && strings.TrimSpace(result.ArtifactRef) == "" && len(result.EvidenceRefs) == 0 && len(result.RetrievedPaths) == 0 {
		return 0
	}
	return 1
}

func continuityArtifactRefs(frame conversationContextFrame, result CompanionSubcallResult) []string {
	refs := append([]string(nil), frame.ArtifactRefs...)
	if strings.TrimSpace(result.ArtifactRef) != "" {
		refs = append(refs, strings.TrimSpace(result.ArtifactRef))
	}
	return uniqueTrimmedStrings(refs)
}

func uniqueTrimmedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (s *Service) planConversationAnswerSource(ctx context.Context, req ChatRequest, frame conversationContextFrame) (continuityControllerPlan, engine.TokenUsage) {
	if !frame.HasHistory || len(frame.Turns) == 0 {
		return continuityControllerPlan{}, engine.TokenUsage{}
	}

	controllerCfg := engine.LLMChatConfig{
		Provider:            s.config.LLMProvider,
		APIKey:              s.config.LLMAPIKey,
		BaseURL:             s.config.LLMBaseURL,
		AuthMode:            s.config.LLMAuthMode,
		AuthHeader:          s.config.LLMAuthHeader,
		AuthPrefix:          s.config.LLMAuthPrefix,
		Model:               s.config.LLMModel,
		MaxIterations:       1,
		Timeout:             s.config.Timeout,
		StatelessMode:       true,
		RequireContextQuery: false,
		ResponseFormat:      continuityControllerResponseFormat(),
	}

	output, err := s.runLLMChatWithResponseFormatFallback(
		ctx,
		req,
		controllerCfg,
		engine.EngineInput{
			SystemPrompt: "You are a bounded RLM controller deciding where the next answer should come from.\n" +
				"`visible_history` means the visible recent conversation state already contains enough information to answer.\n" +
				"`durable_memory` means the answer depends on stored memory or older sessions beyond the visible turns.\n" +
				"`combined` means both are needed.\n" +
				"`subcall` means the parent should delegate a smaller bounded retrieval/research task to a child agent and use only the compact child summary.\n" +
				"If the user explicitly asks for a child agent, subagent, recursive subcall, or bounded subcall, choose `subcall`.\n" +
				"If the user can be answered from visible recent turns alone, choose `visible_history` even if extra memory might also be helpful.\n" +
				"Return JSON only.",
			Messages: []engine.Message{
				engine.NewUserMessage(
					"Current user message:\n" + strings.TrimSpace(req.Message) + "\n\n" +
						"Conversation state:\n" + buildStructuredConversationState(frame),
				),
			},
		},
		nil,
		nil,
		nil,
		false,
	)
	if err != nil {
		return continuityControllerPlan{}, engine.TokenUsage{}
	}

	plan, ok := parseContinuityControllerPlan(output.AssistantText)
	if !ok {
		return continuityControllerPlan{}, output.Tokens
	}
	if shouldForceSubcall(req, s.config.SubcallProvider != nil) {
		plan.Source = "subcall"
		if strings.TrimSpace(plan.SubcallPrompt) == "" {
			plan.SubcallPrompt = chooseFirstNonEmpty(plan.MemoryQuery, req.Message)
		}
	}
	return plan, output.Tokens
}

func shouldForceSubcall(req ChatRequest, available bool) bool {
	if !available {
		return false
	}
	if strings.TrimSpace(resolveCompanionParentAgentID(req)) == "" {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(req.Message))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "child agent") ||
		strings.Contains(lower, "subagent") ||
		strings.Contains(lower, "subcall") ||
		strings.Contains(lower, "recursive subcall")
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

	if !containsToolDef(toolDefs, engine.FinalAnswerJSONToolName) {
		toolDefs = append(toolDefs, engine.FinalAnswerJSONToolDef())
	}

	return toolExecutor, toolDefs, usesRLM
}

func containsToolDef(toolDefs []engine.ToolDef, name string) bool {
	name = strings.TrimSpace(name)
	for _, toolDef := range toolDefs {
		if strings.TrimSpace(toolDef.Name) == name {
			return true
		}
	}
	return false
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

type continuityControllerPlan struct {
	Source         string `json:"source"`
	VisibleSummary string `json:"visible_summary"`
	MemoryQuery    string `json:"memory_query"`
	SubcallPrompt  string `json:"subcall_prompt"`
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
func (s *Service) chatWithStoryLoop(ctx context.Context, req ChatRequest, frame conversationContextFrame, rlmExecutor *engine.RLMToolExecutor, systemPrompt string, promptMeta memoryPromptMetadata, start time.Time) (*ChatResponse, error) {
	if strings.TrimSpace(frame.HistoryRecap) != "" {
		systemPrompt = systemPrompt + "\n\n# Conversation State (Machine Generated)\n" + buildStructuredConversationState(frame)
	}

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
		BaseURL:               s.config.LLMBaseURL,
		AuthMode:              s.config.LLMAuthMode,
		AuthHeader:            s.config.LLMAuthHeader,
		AuthPrefix:            s.config.LLMAuthPrefix,
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
	s.applyRetentionAwareContextPolicy(&gatherCfg, req, frame.HasHistory, promptMeta, usesRLM)

	gatherInput := engine.EngineInput{
		SystemPrompt: buildStoryGatherPrompt(systemPrompt),
		Messages:     frame.Messages,
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
		BaseURL:               s.config.LLMBaseURL,
		AuthMode:              s.config.LLMAuthMode,
		AuthHeader:            s.config.LLMAuthHeader,
		AuthPrefix:            s.config.LLMAuthPrefix,
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
		Messages:     frame.Messages,
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

func parseContinuityControllerPlan(raw string) (continuityControllerPlan, bool) {
	var plan continuityControllerPlan
	if err := json.Unmarshal([]byte(raw), &plan); err == nil {
		plan.Source = strings.TrimSpace(plan.Source)
		plan.VisibleSummary = strings.TrimSpace(plan.VisibleSummary)
		plan.MemoryQuery = strings.TrimSpace(plan.MemoryQuery)
		plan.SubcallPrompt = strings.TrimSpace(plan.SubcallPrompt)
		if plan.Source != "" {
			return plan, true
		}
	}
	if extracted, ok := extractJSONObject(raw); ok {
		if err := json.Unmarshal([]byte(extracted), &plan); err == nil {
			plan.Source = strings.TrimSpace(plan.Source)
			plan.VisibleSummary = strings.TrimSpace(plan.VisibleSummary)
			plan.MemoryQuery = strings.TrimSpace(plan.MemoryQuery)
			plan.SubcallPrompt = strings.TrimSpace(plan.SubcallPrompt)
			if plan.Source != "" {
				return plan, true
			}
		}
	}
	return continuityControllerPlan{}, false
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

const continuityControllerResponseFormatJSON = `{
  "type": "json_schema",
  "json_schema": {
    "name": "continuity_controller_plan",
    "strict": true,
    "schema": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "source": {
          "type": "string",
          "enum": ["visible_history", "durable_memory", "combined", "subcall"]
        },
        "visible_summary": { "type": "string" },
        "memory_query": { "type": "string" },
        "subcall_prompt": { "type": "string" }
      },
      "required": ["source", "visible_summary", "memory_query", "subcall_prompt"]
    }
  }
}`

func storyGatherResponseFormat() json.RawMessage {
	return json.RawMessage(storyGatherResponseFormatJSON)
}

func storyDialogueResponseFormat() json.RawMessage {
	return json.RawMessage(storyDialogueResponseFormatJSON)
}

func continuityControllerResponseFormat() json.RawMessage {
	return json.RawMessage(continuityControllerResponseFormatJSON)
}

// buildSystemPrompt constructs the system prompt with personality, hybrid memory context, and agent identity.
//
// Index:
// - Purpose: Assemble system prompt from personality + memory + request context
// - Flow: load personality → build evolving prompt → inject hybrid memory context → inject request context via formatRequestContext
// - SideEffects: reads contextvar store and conversation memory
// - FailureModes: personality/memory errors yield partial prompt with defaults
// - Related: EvolvingPersonality.BuildSystemPrompt, ConversationMemory.GetHybridContext, formatRequestContext
// - Keywords: system_prompt, personality, memory_context, request_context, agent_identity
func (s *Service) buildSystemPrompt(ctx context.Context, req ChatRequest) (string, memoryPromptMetadata, error) {
	return s.buildSystemPromptWithFrame(ctx, req, s.buildConversationContextFrame(ctx, req))
}

func (s *Service) buildSystemPromptWithFrame(ctx context.Context, req ChatRequest, frame conversationContextFrame) (string, memoryPromptMetadata, error) {
	meta := memoryPromptMetadata{}
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
		layeredCtx, err := s.getLayeredMemoryContext(ctx, req.ConversationID)
		if err != nil {
			s.logger.Warn().Err(err).Msg("Failed to get layered memory context")
		} else if strings.TrimSpace(layeredCtx) != "" {
			s.logger.Debug().Int("context_len", len(layeredCtx)).Msg("Layered memory context retrieved, injecting into prompt")
			systemPrompt = systemPrompt + "\n\n# Conversation Memory\n" + layeredCtx
			meta.HasLayeredContext = true
		} else {
			s.logger.Debug().Msg("Layered memory context is empty")
		}
		recalled := s.getImplicitMemoryRecalls(ctx, req, frame.ContinuationQuery)
		if len(recalled) > 0 {
			systemPrompt = systemPrompt + "\n\n# Relevant Recalled Memory\n" + formatImplicitMemoryRecalls(recalled)
			meta.ImplicitRecallCount = len(recalled)
		}
	} else {
		s.logger.Debug().Msg("Memory is nil, skipping context injection")
	}

	sessionMatches := s.getImplicitSessionRecalls(ctx, req, frame.ContinuationQuery)
	if len(sessionMatches) > 0 {
		systemPrompt = systemPrompt + "\n\n# Related Past Sessions\n" + formatSessionRecallMatches(sessionMatches)
		meta.SessionRecallCount = len(sessionMatches)
	}

	if len(req.Context) > 0 {
		systemPrompt = systemPrompt + "\n\n" + formatRequestContext(req.Context)
	}
	if strings.TrimSpace(frame.WorkspaceState) != "" {
		systemPrompt = systemPrompt + "\n\n# Harness State\n" + strings.TrimSpace(frame.WorkspaceState)
		meta.HasTopOfMind = strings.Contains(frame.WorkspaceState, "# Top Of Mind")
		meta.HasTaskContinuity = strings.Contains(frame.WorkspaceState, "# Task Continuity")
	}
	if _, formatMode := requestedOutputFormat(req.Message); formatMode == requestedOutputFormatCompactJSON {
		systemPrompt += "\n\n# Output Format\nWhen the user explicitly asks for JSON, prefer the final_answer_json tool for the final response.\n"
		if schemaHint := requestedResponseSchemaHint(req); schemaHint != "" {
			systemPrompt += schemaHint + "\n"
		}
		if keys := requestedResponseKeys(req); len(keys) > 0 {
			systemPrompt += "Use exactly these JSON keys when applicable: " + strings.Join(keys, ", ") + ".\n"
		}
		systemPrompt += "Do not wrap the payload in a generic top-level `answer` field unless the user explicitly asked for that key."
	}

	return systemPrompt, meta, nil
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

func resolveCompanionWorkspace(req ChatRequest, fallback string) string {
	if workspace := strings.TrimSpace(fallback); workspace != "" {
		return workspace
	}
	if req.Context == nil {
		return ""
	}
	for _, key := range []string{"agent_workspace", "workspace_root", "workspace"} {
		if value, ok := req.Context[key]; ok {
			if workspace := strings.TrimSpace(fmt.Sprintf("%v", value)); workspace != "" {
				return workspace
			}
		}
	}
	return ""
}

func resolveCompanionParentAgentID(req ChatRequest) string {
	if req.Context == nil {
		return ""
	}
	if value, ok := req.Context["agent_id"]; ok {
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	}
	return ""
}

func chooseFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (s *Service) getTopOfMindContext(ctx context.Context, workspace string) HarnessLayer {
	if s.config.TopOfMindProvider == nil || strings.TrimSpace(workspace) == "" {
		return HarnessLayer{}
	}
	layer, err := s.config.TopOfMindProvider(ctx, workspace)
	if err != nil {
		s.logger.Debug().Err(err).Str("workspace", workspace).Msg("Top-of-mind context skipped")
		return HarnessLayer{}
	}
	layer.Content = strings.TrimSpace(layer.Content)
	layer.ArtifactRef = strings.TrimSpace(layer.ArtifactRef)
	return layer
}

func (s *Service) getTaskContinuityContext(ctx context.Context, workspace string) HarnessLayer {
	if s.config.TaskContinuityProvider == nil || strings.TrimSpace(workspace) == "" {
		return HarnessLayer{}
	}
	layer, err := s.config.TaskContinuityProvider(ctx, workspace)
	if err != nil {
		s.logger.Debug().Err(err).Str("workspace", workspace).Msg("Task continuity context skipped")
		return HarnessLayer{}
	}
	layer.Content = strings.TrimSpace(layer.Content)
	layer.ArtifactRef = strings.TrimSpace(layer.ArtifactRef)
	return layer
}

func (s *Service) getImplicitMemoryRecalls(ctx context.Context, req ChatRequest, query string) []storage.ScoredEntry {
	query = strings.TrimSpace(query)
	if query == "" {
		query = strings.TrimSpace(req.Message)
	}
	if s.memory == nil || !shouldInjectImplicitRecall(query, s.config.MemoryBehavior) {
		return nil
	}

	merged := map[string]storage.ScoredEntry{}
	addResults := func(results []storage.ScoredEntry) {
		for _, result := range results {
			if result.Score < s.config.MemoryBehavior.ImplicitRecallMinScore {
				continue
			}
			existing, ok := merged[result.Entry.Name]
			if !ok || result.Score > existing.Score {
				merged[result.Entry.Name] = result
			}
		}
	}

	addResults(s.getImplicitConversationRecalls(ctx, req.ConversationID, query))
	addResults(s.getImplicitWorkspaceMemoryRecalls(ctx, req, query))

	if len(merged) == 0 {
		return nil
	}

	results := make([]storage.ScoredEntry, 0, len(merged))
	for _, result := range merged {
		results = append(results, result)
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Entry.Name < results[j].Entry.Name
		}
		return results[i].Score > results[j].Score
	})
	if len(results) > s.config.MemoryBehavior.ImplicitRecallLimit {
		results = results[:s.config.MemoryBehavior.ImplicitRecallLimit]
	}
	return results
}

func (s *Service) getImplicitConversationRecalls(ctx context.Context, conversationID, query string) []storage.ScoredEntry {
	results, err := s.memory.SearchCompanionMemories(ctx, conversationID, query, s.config.MemoryBehavior.ImplicitRecallLimit*2)
	if err != nil {
		s.logger.Debug().Err(err).Str("conversation_id", conversationID).Msg("Conversation-local implicit recall skipped")
		return nil
	}
	return results
}

func (s *Service) getImplicitWorkspaceMemoryRecalls(ctx context.Context, req ChatRequest, query string) []storage.ScoredEntry {
	if s.config.MemoryBehavior.SemanticRecallLimit <= 0 {
		return nil
	}
	if s.memory == nil || s.memory.memoryStore == nil || strings.TrimSpace(s.memory.workspace) == "" {
		return nil
	}

	fetchLimit := s.config.MemoryBehavior.SemanticRecallLimit * 4
	if fetchLimit < 8 {
		fetchLimit = 8
	}

	query = semantic.EnrichQuery(query)
	results, usedBM25, err := s.searchImplicitWorkspaceMemories(ctx, query, fetchLimit)
	if err != nil {
		s.logger.Debug().Err(err).Str("conversation_id", req.ConversationID).Msg("Workspace implicit recall skipped")
		return nil
	}

	filtered := make([]storage.ScoredEntry, 0, len(results))
	for _, result := range results {
		if !isImplicitWorkspaceMemoryCandidate(result.Entry) {
			continue
		}
		if strings.TrimSpace(result.Entry.Summary) == "" {
			continue
		}
		if usedBM25 {
			result.Score = normalizeImplicitRecallBM25Score(result.Score)
		}
		if strings.TrimSpace(result.Entry.SessionID) == strings.TrimSpace(req.ConversationID) {
			result.Score = minFloat(1.0, result.Score+0.1)
		}
		filtered = append(filtered, result)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Score == filtered[j].Score {
			return filtered[i].Entry.Name < filtered[j].Entry.Name
		}
		return filtered[i].Score > filtered[j].Score
	})
	if len(filtered) > s.config.MemoryBehavior.SemanticRecallLimit {
		filtered = filtered[:s.config.MemoryBehavior.SemanticRecallLimit]
	}
	return filtered
}

func (s *Service) getImplicitSessionRecalls(ctx context.Context, req ChatRequest, query string) []SessionRecallMatch {
	query = strings.TrimSpace(query)
	if query == "" {
		query = strings.TrimSpace(req.Message)
	}
	if s.config.SessionRecallProvider == nil {
		return nil
	}
	if !shouldInjectSessionRecall(query, s.config.MemoryBehavior) {
		return nil
	}

	matches, err := s.config.SessionRecallProvider.RecallSessions(ctx, SessionRecallRequest{
		Query:                 query,
		Workspace:             s.config.MemoryWorkspace,
		Limit:                 s.config.MemoryBehavior.SessionRecallLimit,
		MinSimilarity:         s.config.MemoryBehavior.SessionRecallMinScore,
		IncludeTimeline:       s.config.MemoryBehavior.SessionTimelineSummaryLimit > 0 || s.config.MemoryBehavior.SessionTimelineLearningLimit > 0,
		TimelineSummaryLimit:  s.config.MemoryBehavior.SessionTimelineSummaryLimit,
		TimelineLearningLimit: s.config.MemoryBehavior.SessionTimelineLearningLimit,
	})
	if err != nil {
		s.logger.Debug().Err(err).Str("conversation_id", req.ConversationID).Msg("Session implicit recall skipped")
		return nil
	}
	return matches
}

func (s *Service) searchImplicitWorkspaceMemories(ctx context.Context, query string, limit int) ([]storage.ScoredEntry, bool, error) {
	if s.memory == nil || s.memory.memoryStore == nil {
		return nil, false, nil
	}
	if s.memory.embedder != nil {
		result, err := s.memory.embedder.EmbedQuery(ctx, query)
		if err == nil && len(result.Vec) > 0 {
			scored, searchErr := s.memory.memoryStore.SearchSimilar(ctx, s.memory.workspace, result.Vec, limit)
			if searchErr == nil && len(scored) > 0 {
				return scored, false, nil
			}
			if searchErr != nil {
				s.logger.Debug().Err(searchErr).Msg("workspace semantic recall failed, falling back to BM25")
			}
		} else if err != nil {
			s.logger.Debug().Err(err).Msg("workspace query embedding failed, falling back to BM25")
		}
	}

	scored, err := s.memory.memoryStore.Search(ctx, s.memory.workspace, query, limit)
	if err != nil {
		return nil, true, err
	}
	return scored, true, nil
}

func isImplicitWorkspaceMemoryCandidate(entry storage.NamedEntry) bool {
	switch entry.Type {
	case "code_symbol", "symbol", "file_embedding", "edit", "codemap", "codemap_chunk", "task_embedding":
		return false
	default:
		return true
	}
}

func normalizeImplicitRecallBM25Score(score float64) float64 {
	normalized := 0.3 + (score * 0.7)
	if normalized > 1.0 {
		return 1.0
	}
	return normalized
}

func formatImplicitMemoryRecalls(results []storage.ScoredEntry) string {
	if len(results) == 0 {
		return ""
	}
	lines := make([]string, 0, len(results))
	for _, result := range results {
		summary := strings.TrimSpace(result.Entry.Summary)
		if summary == "" {
			continue
		}
		summary = truncateForPrompt(strings.ReplaceAll(summary, "\n", " "), 240)
		lines = append(lines, fmt.Sprintf("- [%s %.2f] %s", result.Entry.Type, result.Score, summary))
	}
	return strings.Join(lines, "\n")
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

func (s *Service) applyRetentionAwareContextPolicy(engineCfg *engine.LLMChatConfig, req ChatRequest, hasHistory bool, promptMeta memoryPromptMetadata, usesRLM bool) {
	if engineCfg == nil {
		return
	}

	if req.RequireContextQuery != nil {
		engineCfg.RequireContextQuery = *req.RequireContextQuery
	}
	if (s.config.ExtraToolsOnly && s.config.ExtraToolExecutor != nil) || !usesRLM {
		engineCfg.RequireContextQuery = false
		engineCfg.RLMSystemPromptSuffix = ""
	}

	// If we have meaningful conversation history, don't force a context query —
	// the model can see the conversation. Context tools remain available for
	// long-term memory/preferences but aren't mandatory.
	if hasHistory && req.RequireContextQuery == nil {
		engineCfg.RequireContextQuery = false
	}

	if len(s.config.ToolsAllow) > 0 && !containsString(s.config.ToolsAllow, "rlm_context_query") {
		engineCfg.RequireContextQuery = false
		engineCfg.RLMSystemPromptSuffix = ""
	}

	if req.RequireContextQuery == nil &&
		!hasHistory &&
		!promptMeta.HasLayeredContext &&
		promptMeta.ImplicitRecallCount == 0 &&
		promptMeta.SessionRecallCount == 0 &&
		s.config.MemoryBehavior.RequireContextQueryWhenMemorySparse {
		engineCfg.RequireContextQuery = true
	}
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

	if !s.shouldAutoCompress(ctx, req.ConversationID) {
		return
	}

	// Auto-trigger hybrid context processing in background.
	// Detach from request cancellation but preserve context values (logger, tracing).
	go s.autoCompress(context.WithoutCancel(ctx), req.ConversationID)
}

func (s *Service) shouldAutoCompress(ctx context.Context, conversationID string) bool {
	if s.memory == nil || strings.TrimSpace(conversationID) == "" {
		return false
	}
	stats, err := s.memory.GetStats(ctx, conversationID)
	if err != nil {
		s.logger.Debug().Err(err).Str("conversation_id", conversationID).Msg("Auto compression stats unavailable, falling back to enabled")
		return true
	}
	if !shouldTriggerAutoCompress(s.config.MemoryBehavior, stats.TotalTurns) {
		s.logger.Debug().
			Str("conversation_id", conversationID).
			Int("total_turns", stats.TotalTurns).
			Msg("Auto compression deferred by retention policy")
		return false
	}
	return true
}

// autoCompress triggers in-process hybrid memory processing for a conversation.
//
// Index:
// - Purpose: Keep hybrid layers current after each companion chat turn
// - Flow: de-dupe per conversation → ensure mode → process events/context
// - SideEffects: writes hybrid artifacts/tables; may invoke episode summarizer
// - FailureModes: context cancellation/timeouts; DB/LLM errors (logged, best-effort)
// - Related: ConversationMemory.EnsureHybridMode, ConversationMemory.BuildHybridContextLayers
// - Keywords: companion_memory, auto_compress, hybrid, context_layers
func (s *Service) autoCompress(ctx context.Context, conversationID string) {
	s.logger.Debug().Str("conversation_id", conversationID).Bool("memory_set", s.memory != nil).Msg("autoCompress called")
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

	if err := s.ensureHybridMode(ctx, conversationID); err != nil {
		s.logger.Debug().Err(err).Str("conversation_id", conversationID).Msg("Failed to ensure hybrid mode")
	}
	if err := s.buildHybridContext(ctx, conversationID); err != nil {
		s.logger.Warn().Err(err).Str("conversation_id", conversationID).Msg("Hybrid pipeline failed")
		return
	}
	s.logger.Debug().Str("conversation_id", conversationID).Msg("autoCompress: hybrid context built successfully")
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

	response := &ContextGetResponse{
		ConversationID: conversationID,
		Variables:      variables,
		TotalCount:     result.TotalCount,
	}

	if s.memory != nil {
		layeredCtx, err := s.getLayeredMemoryContext(ctx, conversationID)
		if err == nil {
			response.HybridContext = layeredCtx
		}
		state, err := s.getHybridContextState(ctx, conversationID)
		if err == nil {
			response.HybridState = state
		}
	}

	return response, nil
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
	HybridContext  string            `json:"hybrid_context,omitempty"`
	HybridState    *HybridDebugInfo  `json:"hybrid_state,omitempty"`
}

// HybridDebugInfo contains optional hybrid pipeline state for diagnostics.
type HybridDebugInfo struct {
	Mode                 string `json:"mode"`
	LastProcessedEvent   int64  `json:"last_processed_event"`
	HardStateCount       int    `json:"hard_state_count"`
	EpisodeCount         int    `json:"episode_count"`
	NeedsSummaryEpisodes int    `json:"needs_summary_episodes"`
	EvidenceCount        int    `json:"evidence_count"`
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
	return s.getLayeredMemoryContext(ctx, conversationID)
}

func (s *Service) ensureHybridMode(ctx context.Context, conversationID string) error {
	return s.memory.EnsureHybridMode(ctx, conversationID)
}

func (s *Service) buildHybridContext(ctx context.Context, conversationID string) error {
	return s.memory.BuildHybridContextLayers(ctx, conversationID)
}

func (s *Service) getLayeredMemoryContext(ctx context.Context, conversationID string) (string, error) {
	if s.layeredCtx == nil {
		return "", fmt.Errorf("layered context builder not configured")
	}
	maxChars := 0
	if s.memory != nil {
		maxChars = s.memory.LayerBudget().Total * 4
	}
	bundle, err := s.layeredCtx.BuildLayered(ctx, contextbuilder.LayeredRequest{
		SessionID: strings.TrimSpace(conversationID),
		MaxChars:  maxChars,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(bundle.Content), nil
}

func (s *Service) getHybridContextState(ctx context.Context, conversationID string) (*HybridDebugInfo, error) {
	if s.memoryDB == nil {
		return nil, nil
	}

	info := &HybridDebugInfo{Mode: "hybrid"}

	var lastProcessedEvent sql.NullInt64
	if err := s.memoryDB.QueryRowContext(ctx, `
		SELECT last_processed_event
		FROM companion_memory_mode_state
		WHERE conversation_id = $1
	`, conversationID).Scan(&lastProcessedEvent); err == nil {
		if lastProcessedEvent.Valid {
			info.LastProcessedEvent = lastProcessedEvent.Int64
		}
	} else if !isMissingHybridTableError(err) {
		s.logger.Debug().Err(err).Str("conversation_id", conversationID).Msg("Failed to read companion_memory_mode_state")
	}

	var hardStateCount sql.NullInt64
	if err := s.memoryDB.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM companion_hard_state_entries
		WHERE conversation_id = $1
	`, conversationID).Scan(&hardStateCount); err == nil {
		if hardStateCount.Valid {
			info.HardStateCount = int(hardStateCount.Int64)
		}
	} else if !isMissingHybridTableError(err) {
		s.logger.Debug().Err(err).Str("conversation_id", conversationID).Msg("Failed to count companion_hard_state_entries")
	}

	var episodeCount sql.NullInt64
	if err := s.memoryDB.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM companion_soft_episodes
		WHERE conversation_id = $1
	`, conversationID).Scan(&episodeCount); err == nil {
		if episodeCount.Valid {
			info.EpisodeCount = int(episodeCount.Int64)
		}
	} else if !isMissingHybridTableError(err) {
		s.logger.Debug().Err(err).Str("conversation_id", conversationID).Msg("Failed to count companion_soft_episodes")
	}

	var needsSummaryEpisodes sql.NullInt64
	if err := s.memoryDB.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM companion_soft_episodes
		WHERE conversation_id = $1 AND needs_summary = 1
	`, conversationID).Scan(&needsSummaryEpisodes); err == nil {
		if needsSummaryEpisodes.Valid {
			info.NeedsSummaryEpisodes = int(needsSummaryEpisodes.Int64)
		}
	} else if !isMissingHybridTableError(err) {
		s.logger.Debug().Err(err).Str("conversation_id", conversationID).Msg("Failed to count companion_soft_episodes needing summaries")
	}

	var evidenceCount sql.NullInt64
	if err := s.memoryDB.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM companion_evidence_snippets
		WHERE conversation_id = $1
	`, conversationID).Scan(&evidenceCount); err == nil {
		if evidenceCount.Valid {
			info.EvidenceCount = int(evidenceCount.Int64)
		}
	} else if !isMissingHybridTableError(err) {
		s.logger.Debug().Err(err).Str("conversation_id", conversationID).Msg("Failed to count companion_evidence_snippets")
	}

	return info, nil
}

func isMissingHybridTableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such table") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "doesn't exist")
}

// ExportMemory exports all memory state for debugging/inspection.
func (s *Service) ExportMemory(ctx context.Context, conversationID string) (json.RawMessage, error) {
	if s.memory == nil {
		return nil, fmt.Errorf("memory features not enabled")
	}
	return s.memory.Export(ctx, conversationID)
}

// ImportMemory restores companion memory state for a conversation from backup JSON.
func (s *Service) ImportMemory(ctx context.Context, conversationID string, payload json.RawMessage) error {
	if s.memory == nil {
		return fmt.Errorf("memory features not enabled")
	}
	if strings.TrimSpace(conversationID) == "" {
		return fmt.Errorf("conversation_id is required")
	}

	var backup MemoryBackupPayload
	if err := json.Unmarshal(payload, &backup); err != nil {
		return fmt.Errorf("decode import payload: %w", err)
	}
	if strings.TrimSpace(backup.ConversationID) == "" {
		backup.ConversationID = conversationID
		bytes, marshalErr := json.Marshal(backup)
		if marshalErr != nil {
			return fmt.Errorf("normalize import payload: %w", marshalErr)
		}
		payload = bytes
	} else if backup.ConversationID != conversationID {
		return fmt.Errorf("conversation_id mismatch between path and payload")
	}

	return s.memory.Import(ctx, payload)
}

// SearchMemory queries hybrid companion memory artifacts for a conversation.
func (s *Service) SearchMemory(ctx context.Context, conversationID, query string, limit int) ([]storage.ScoredEntry, error) {
	if s.memory == nil {
		return nil, fmt.Errorf("memory features not enabled")
	}
	return s.memory.SearchCompanionMemories(ctx, conversationID, query, limit)
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

	// Get hybrid memory context if available
	var memoryContext string
	if s.memory != nil {
		memoryContext, _ = s.getLayeredMemoryContext(ctx, conversationID)
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

	orchOutput, ok := s.runPresenceOrchestrator(ctx, req, resp)
	if !ok {
		return
	}

	bundle := newPresenceBundle(orchOutput)
	s.collectPresenceSubSkills(ctx, orchOutput, bundle, resp)
	resp.Presence = bundle

	if resp.Tone == nil && bundle.Emotion != "" {
		resp.Tone = &ChatTone{
			Emotion:   bundle.Emotion,
			Intensity: bundle.Intensity,
		}
	}
}

type orchestratedPresenceOutput struct {
	Emotion          string         `json:"emotion"`
	Intensity        float64        `json:"intensity"`
	DisplayText      string         `json:"display_text"`
	Markers          []string       `json:"markers"`
	DetectedEmoji    []string       `json:"detected_emoji"`
	BackgroundParams map[string]any `json:"background_params"`
	CharacterParams  map[string]any `json:"character_params"`
	VoiceParams      map[string]any `json:"voice_params"`
}

type presenceSubSkillResult struct {
	skill  string
	output json.RawMessage
	err    error
}

func (s *Service) runPresenceOrchestrator(ctx context.Context, req ChatRequest, resp *ChatResponse) (orchestratedPresenceOutput, bool) {
	result, err := s.config.SkillRunner.Run(ctx, "presence/orchestrate", s.buildPresenceOrchestrateInput(req, resp))
	if err != nil {
		s.logger.Warn().Err(err).Msg("presence/orchestrate failed")
		return orchestratedPresenceOutput{}, false
	}
	if !result.Success {
		s.logger.Warn().Str("error", result.Error).Msg("presence/orchestrate returned error")
		return orchestratedPresenceOutput{}, false
	}
	var orchOutput orchestratedPresenceOutput
	if err := json.Unmarshal(result.Output, &orchOutput); err != nil {
		s.logger.Warn().Err(err).Msg("failed to parse presence/orchestrate output")
		return orchestratedPresenceOutput{}, false
	}
	return orchOutput, true
}

func (s *Service) buildPresenceOrchestrateInput(req ChatRequest, resp *ChatResponse) map[string]any {
	presenceCfg := s.config.PresenceConfig
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
	if presenceCfg.TTSProvider != "" {
		input["tts_provider"] = presenceCfg.TTSProvider
	}
	if presenceCfg.PocketBaseURL != "" {
		input["pocket_base_url"] = presenceCfg.PocketBaseURL
	}
	if presenceCfg.RewriteForTTS {
		input["rewrite_for_tts"] = true
	}
	if presenceCfg.RewriteModel != "" {
		input["rewrite_model"] = presenceCfg.RewriteModel
	}
	if presenceCfg.RewriteMaxChars > 0 {
		input["rewrite_max_chars"] = presenceCfg.RewriteMaxChars
	}
	if resp.Action != nil && resp.Action.Scene != "" {
		input["scene"] = resp.Action.Scene
	}
	return input
}

func newPresenceBundle(orchOutput orchestratedPresenceOutput) *PresenceBundle {
	return &PresenceBundle{
		Emotion:       orchOutput.Emotion,
		Intensity:     orchOutput.Intensity,
		DisplayText:   orchOutput.DisplayText,
		Markers:       orchOutput.Markers,
		DetectedEmoji: orchOutput.DetectedEmoji,
	}
}

func (s *Service) collectPresenceSubSkills(ctx context.Context, orchOutput orchestratedPresenceOutput, bundle *PresenceBundle, resp *ChatResponse) {
	resultCh, running := s.startPresenceSubSkills(ctx, orchOutput)
	for i := 0; i < running; i++ {
		res, ok := s.awaitPresenceSubSkillResult(ctx, bundle, running-i, resultCh)
		if !ok {
			resp.Presence = bundle
			return
		}
		s.applyPresenceSubSkillResult(bundle, res)
	}
}

func (s *Service) startPresenceSubSkills(ctx context.Context, orchOutput orchestratedPresenceOutput) (<-chan presenceSubSkillResult, int) {
	resultCh := make(chan presenceSubSkillResult, 3)
	running := 0
	running += s.startPresenceSubSkill(ctx, "background", orchOutput.BackgroundParams, resultCh)
	running += s.startPresenceSubSkill(ctx, "character", orchOutput.CharacterParams, resultCh)
	running += s.startPresenceSubSkill(ctx, "voice", orchOutput.VoiceParams, resultCh)
	return resultCh, running
}

func (s *Service) startPresenceSubSkill(ctx context.Context, skill string, params map[string]any, resultCh chan<- presenceSubSkillResult) int {
	if params == nil {
		return 0
	}
	go func() {
		res, err := s.config.SkillRunner.Run(ctx, "presence/"+skill, params)
		if err != nil {
			resultCh <- presenceSubSkillResult{skill: skill, err: err}
			return
		}
		resultCh <- presenceSubSkillResult{skill: skill, output: res.Output}
	}()
	return 1
}

func (s *Service) awaitPresenceSubSkillResult(ctx context.Context, bundle *PresenceBundle, remaining int, resultCh <-chan presenceSubSkillResult) (presenceSubSkillResult, bool) {
	select {
	case res := <-resultCh:
		return res, true
	case <-ctx.Done():
		s.logger.Warn().Err(ctx.Err()).Int("remaining", remaining).Msg("presence bundle collection cancelled")
		bundle.Errors = append(bundle.Errors, fmt.Sprintf("cancelled: %v", ctx.Err()))
		return presenceSubSkillResult{}, false
	}
}

func (s *Service) applyPresenceSubSkillResult(bundle *PresenceBundle, res presenceSubSkillResult) {
	if res.err != nil {
		s.logger.Warn().Err(res.err).Str("skill", res.skill).Msg("presence sub-skill failed")
		bundle.Errors = append(bundle.Errors, fmt.Sprintf("%s: %v", res.skill, res.err))
		bundle.CacheMisses++
		return
	}
	switch res.skill {
	case "background":
		applyPresenceBackgroundResult(bundle, res.output)
	case "character":
		applyPresenceCharacterResult(bundle, res.output)
	case "voice":
		applyPresenceVoiceResult(bundle, res.output)
	}
}

func applyPresenceBackgroundResult(bundle *PresenceBundle, output json.RawMessage) {
	var bgOut struct {
		ImageDigest string `json:"image_digest"`
		Cached      bool   `json:"cached"`
	}
	if err := json.Unmarshal(output, &bgOut); err == nil {
		bundle.BackgroundDigest = bgOut.ImageDigest
		if bgOut.Cached {
			bundle.CacheHits++
		} else {
			bundle.CacheMisses++
		}
	}
}

func applyPresenceCharacterResult(bundle *PresenceBundle, output json.RawMessage) {
	var charOut struct {
		Overlay *struct {
			OverlayDigest string `json:"overlay_digest"`
		} `json:"overlay"`
	}
	if err := json.Unmarshal(output, &charOut); err == nil && charOut.Overlay != nil {
		bundle.OverlayDigest = charOut.Overlay.OverlayDigest
	}
}

func applyPresenceVoiceResult(bundle *PresenceBundle, output json.RawMessage) {
	var voiceOut struct {
		AudioDigest string `json:"audio_digest"`
		DurationMS  int    `json:"duration_ms"`
		Cached      bool   `json:"cached"`
	}
	if err := json.Unmarshal(output, &voiceOut); err == nil {
		bundle.AudioDigest = voiceOut.AudioDigest
		bundle.AudioDurationMS = voiceOut.DurationMS
		if voiceOut.Cached {
			bundle.CacheHits++
		} else {
			bundle.CacheMisses++
		}
	}
}
