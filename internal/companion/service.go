// Package companion provides the RLM-based companion service for mobile apps.
//
// The companion service operates in stateless mode where each turn is independent,
// with context queried via tools rather than accumulated in the conversation.
package companion

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/XiaoConstantine/dspy-go/pkg/agents/react"
	"github.com/XiaoConstantine/dspy-go/pkg/core"
	"github.com/XiaoConstantine/dspy-go/pkg/llms"
	models "github.com/XiaoConstantine/mcp-go/pkg/model"
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

	// EngineTypeDSPy uses DSPy ReAct agent for structured reasoning.
	EngineTypeDSPy EngineType = "dspy"
)

// Service is the companion chat service.
type Service struct {
	contextStore contextvar.Store
	memory       *ConversationMemory
	config       ServiceConfig
	logger       zerolog.Logger
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

	// EngineType selects the execution engine: "llmchat" (default) or "dspy".
	// - llmchat: Uses LLMChatEngine for OpenAI-compatible streaming
	// - dspy: Uses DSPy ReAct agent for structured reasoning
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

	// Logger for structured logging.
	Logger zerolog.Logger
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
		RequireContextQuery: false,
		MaxIterations:       20,
		Timeout:             60 * time.Second,
	}
}

// NewService creates a new companion service.
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

	// EngineType overrides the service default engine type.
	// Use "dspy" for DSPy ReAct agent execution.
	EngineType EngineType `json:"engine_type,omitempty"`

	// StoryGatherModel overrides ServiceConfig.StoryGatherModel for story mode (optional).
	StoryGatherModel string `json:"story_gather_model,omitempty"`

	// StoryDialogueModel overrides ServiceConfig.StoryDialogueModel for story mode (optional).
	StoryDialogueModel string `json:"story_dialogue_model,omitempty"`
}

// ChatResponse is the response from the companion.
type ChatTone struct {
	Emotion   string  `json:"emotion,omitempty"`   // neutral|joy|sadness|anger|fear|surprise|disgust|playful
	Intensity float64 `json:"intensity,omitempty"` // 0..1
	Voice     string  `json:"voice,omitempty"`     // optional: warm, witty, calm, etc.
}

type ChatAction struct {
	BackgroundKey string `json:"background_key,omitempty"`
	ImagePrompt   string `json:"image_prompt,omitempty"`
	Scene         string `json:"scene,omitempty"`
}

type ChatResponse struct {
	// Response is the assistant's response text.
	Response string `json:"response"`

	// Tone captures structured emotion metadata for clients.
	Tone *ChatTone `json:"tone,omitempty"`

	// Action captures structured UI/scene directives for clients.
	Action *ChatAction `json:"action,omitempty"`

	// ConversationID is the conversation this response belongs to.
	ConversationID string `json:"conversation_id"`

	// ContextQueries is how many times context was queried this turn.
	ContextQueries int `json:"context_queries"`

	// ToolsUsed lists the tools that were called.
	ToolsUsed []string `json:"tools_used"`

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

// Chat handles a chat request.
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

	if execMode == agent.ModeStory && engineType == EngineTypeDSPy {
		return nil, fmt.Errorf("story mode requires llmchat engine")
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
	switch engineType {
	case EngineTypeDSPy:
		resp, err = s.chatWithDSPy(ctx, req, rlmExecutor, systemPrompt, execMode, start)
	default:
		if execMode == agent.ModeStory {
			resp, err = s.chatWithStoryLoop(ctx, req, rlmExecutor, systemPrompt, start)
		} else {
			resp, err = s.chatWithLLMChat(ctx, req, rlmExecutor, systemPrompt, start)
		}
	}

	if err != nil {
		return nil, err
	}

	// Store conversation turns in memory
	s.storeConversationTurns(ctx, req, resp, start)

	return resp, nil
}

// chatWithLLMChat executes using the LLMChatEngine.
// The systemPrompt parameter is the full system prompt for this turn.
func (s *Service) chatWithLLMChat(ctx context.Context, req ChatRequest, rlmExecutor *engine.RLMToolExecutor, systemPrompt string, start time.Time) (*ChatResponse, error) {
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
		Messages: []engine.Message{
			engine.NewUserMessage(req.Message),
		},
		Tools: toolDefs,
	}

	// Execute
	output, err := llmEngine.Run(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("engine run: %w", err)
	}

	// Build response
	resp := &ChatResponse{
		ConversationID: req.ConversationID,
		Response:       output.AssistantText,
		ContextQueries: rlmExecutor.QueryCount(),
		DurationMS:     time.Since(start).Milliseconds(),
		TokenUsage: TokenUsage{
			InputTokens:  output.Tokens.InputTokens,
			OutputTokens: output.Tokens.OutputTokens,
			TotalTokens:  output.Tokens.TotalTokens,
		},
	}

	// Collect tools used
	toolNames := make(map[string]struct{})
	for _, tc := range output.ToolCalls {
		toolNames[tc.Name] = struct{}{}
	}
	for name := range toolNames {
		resp.ToolsUsed = append(resp.ToolsUsed, name)
	}
	sort.Strings(resp.ToolsUsed)

	// Check for errors
	if output.StopReason == engine.StopReasonError {
		resp.Error = output.Error
	}

	// Note: Conversation turns are stored by the Chat() entry point via storeConversationTurns()
	// to avoid duplicate entries.

	return resp, nil
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

func (s *Service) chatWithStoryLoop(ctx context.Context, req ChatRequest, rlmExecutor *engine.RLMToolExecutor, systemPrompt string, start time.Time) (*ChatResponse, error) {
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

	gatherInput := engine.EngineInput{
		SystemPrompt: buildStoryGatherPrompt(systemPrompt),
		Messages: []engine.Message{
			engine.NewUserMessage(req.Message),
		},
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
		Messages: []engine.Message{
			engine.NewUserMessage(req.Message),
		},
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
	responseText := dialogueOutput.AssistantText
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

// chatWithDSPy executes using the DSPy ReAct agent.
func (s *Service) chatWithDSPy(ctx context.Context, req ChatRequest, rlmExecutor *engine.RLMToolExecutor, systemPrompt string, execMode agent.ExecutionMode, start time.Time) (*ChatResponse, error) {
	// Initialize LLM factory
	llms.EnsureFactory()

	// Create LLM based on provider
	llm, err := s.createDSPyLLM(ctx)
	if err != nil {
		return nil, fmt.Errorf("create dspy llm: %w", err)
	}

	// Create DSPy ReAct agent
	agentID := fmt.Sprintf("companion:%s", req.ConversationID)
	maxIterations := s.config.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 10
	}

	dspyAgent := react.NewReActAgent(agentID, "companion",
		react.WithMaxIterations(maxIterations),
		react.WithTimeout(s.config.Timeout),
	)

	// Convert RLM tools to dspy-go tools
	for _, toolDef := range rlmExecutor.List() {
		tool := &dspyRLMTool{
			def:      toolDef,
			executor: rlmExecutor,
		}
		if err := dspyAgent.RegisterTool(tool); err != nil {
			s.logger.Warn().Err(err).Str("tool", toolDef.Name).Msg("Failed to register tool")
		}
	}

	// Build signature with system prompt as instruction
	sig := core.NewSignature(
		[]core.InputField{
			{Field: core.NewField("task", core.WithDescription("The user's message to respond to"))},
		},
		[]core.OutputField{
			{Field: core.NewField("result", core.WithDescription("Your response to the user"))},
		},
	).WithInstruction(systemPrompt)

	if err := dspyAgent.Initialize(llm, sig); err != nil {
		return nil, fmt.Errorf("initialize dspy agent: %w", err)
	}

	// Execute
	input := map[string]any{
		"task": req.Message,
	}

	resultMap, err := dspyAgent.Execute(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("dspy execute: %w", err)
	}

	// Extract result
	result := extractDSPyResult(resultMap)

	// Build response
	resp := &ChatResponse{
		ConversationID: req.ConversationID,
		Response:       result,
		ContextQueries: rlmExecutor.QueryCount(),
		DurationMS:     time.Since(start).Milliseconds(),
	}

	// Note: DSPy doesn't provide detailed token usage, but we could add it later
	return resp, nil
}

// createDSPyLLM creates a dspy-go LLM based on the configured provider.
// Accepts a context for proper cancellation and timeout propagation.
func (s *Service) createDSPyLLM(ctx context.Context) (core.LLM, error) {
	provider := s.config.LLMProvider
	model := s.config.LLMModel
	apiKey := s.config.LLMAPIKey

	switch provider {
	case "gemini":
		return llms.NewGeminiLLM(apiKey, core.ModelID(model))
	case "openai":
		return llms.NewOpenAILLM(core.ModelID(model), llms.WithAPIKey(apiKey))
	case "anthropic":
		config := core.ProviderConfig{Name: "anthropic", APIKey: apiKey}
		return llms.NewAnthropicLLMFromConfig(ctx, config, core.ModelID(model))
	case "groq":
		return llms.NewOpenAICompatible("groq", core.ModelID(model),
			"https://api.groq.com/openai", llms.WithAPIKey(apiKey))
	case "openrouter":
		return llms.NewOpenAICompatible("openrouter", core.ModelID(model),
			"https://openrouter.ai/api", llms.WithAPIKey(apiKey))
	case "cerebras":
		// Note: Use base URL without /v1 - dspy-go appends /v1/chat/completions
		return llms.NewOpenAICompatible("cerebras", core.ModelID(model),
			"https://api.cerebras.ai", llms.WithAPIKey(apiKey))
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %q", provider)
	}
}

// buildSystemPrompt constructs the system prompt with personality and memory context.
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
		ctxJSON, err := json.Marshal(req.Context)
		if err != nil {
			s.logger.Warn().Err(err).Msg("Failed to marshal request context")
		} else if len(ctxJSON) > 0 {
			systemPrompt = systemPrompt + "\n\n# Request Context\n" + string(ctxJSON)
		}
	}

	return systemPrompt, nil
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

	// Store assistant turn
	assistantTurn := ConversationTurn{
		ConversationID: req.ConversationID,
		Role:           "assistant",
		Content:        resp.Response,
		CreatedAt:      time.Now(),
	}
	if err := s.memory.AppendTurn(ctx, assistantTurn); err != nil {
		s.logger.Warn().Err(err).Msg("Failed to store assistant turn")
	}
}

// dspyRLMTool wraps an RLM tool for use with dspy-go.
// Implements core.Tool interface from dspy-go.
type dspyRLMTool struct {
	def      engine.ToolDef
	executor *engine.RLMToolExecutor
}

func (t *dspyRLMTool) Name() string {
	return t.def.Name
}

func (t *dspyRLMTool) Description() string {
	return t.def.Description
}

func (t *dspyRLMTool) Metadata() *core.ToolMetadata {
	return &core.ToolMetadata{
		Name:        t.def.Name,
		Description: t.def.Description,
		InputSchema: t.InputSchema(),
	}
}

func (t *dspyRLMTool) CanHandle(_ context.Context, intent string) bool {
	// RLM tools can handle any intent that mentions their name
	return intent == t.def.Name
}

func (t *dspyRLMTool) Execute(ctx context.Context, params map[string]interface{}) (core.ToolResult, error) {
	args, err := json.Marshal(params)
	if err != nil {
		return core.ToolResult{}, fmt.Errorf("marshal args: %w", err)
	}
	result, err := t.executor.Execute(ctx, t.def.Name, args)
	if err != nil {
		return core.ToolResult{}, err
	}
	return core.ToolResult{
		Data: result,
	}, nil
}

func (t *dspyRLMTool) Validate(_ map[string]interface{}) error {
	// RLM tools handle validation internally
	return nil
}

func (t *dspyRLMTool) InputSchema() models.InputSchema {
	var schema map[string]interface{}
	if err := json.Unmarshal(t.def.Parameters, &schema); err != nil {
		return models.InputSchema{
			Type: "object",
		}
	}

	// Build a set of required field names
	requiredSet := make(map[string]bool)
	if req, ok := schema["required"].([]interface{}); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				requiredSet[s] = true
			}
		}
	}

	// Extract properties if present
	properties := make(map[string]models.ParameterSchema)
	if props, ok := schema["properties"].(map[string]interface{}); ok {
		for name, prop := range props {
			if propMap, ok := prop.(map[string]interface{}); ok {
				param := models.ParameterSchema{
					Required: requiredSet[name],
				}
				if typ, ok := propMap["type"].(string); ok {
					param.Type = typ
				}
				if d, ok := propMap["description"].(string); ok {
					param.Description = d
				}
				properties[name] = param
			}
		}
	}

	return models.InputSchema{
		Type:       "object",
		Properties: properties,
	}
}

// extractDSPyResult extracts the response from DSPy execution output.
func extractDSPyResult(resultMap map[string]any) string {
	if resultMap == nil {
		return "Task completed"
	}

	// Priority order: result, answer, output, thought
	for _, key := range []string{"result", "answer", "output", "thought"} {
		if r, ok := resultMap[key].(string); ok && r != "" {
			return r
		}
	}

	// Last resort: format the map but exclude internal ReAct fields
	cleaned := make(map[string]any)
	for k, v := range resultMap {
		if k == "action" || k == "observation" || k == "conversation_context" {
			continue
		}
		cleaned[k] = v
	}
	if len(cleaned) > 0 {
		return fmt.Sprintf("%v", cleaned)
	}
	return "Task completed"
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
