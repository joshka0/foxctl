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
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/engine"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/contextvar"
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
	// LLMProvider is the LLM provider: "openrouter", "groq", "openai"
	LLMProvider string

	// LLMAPIKey is the API key for the LLM provider.
	LLMAPIKey string

	// LLMModel is the model to use.
	LLMModel string

	// DefaultPersonality is the default system prompt personality.
	DefaultPersonality string

	// RequireContextQuery enforces context querying before responses.
	RequireContextQuery bool

	// MaxIterations limits the tool call loop.
	MaxIterations int

	// Timeout is the request timeout.
	Timeout time.Duration

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

	// Logger for structured logging.
	Logger zerolog.Logger
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
				Logger:   cfg.Logger,
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

	// Personality overrides the default system prompt.
	Personality string `json:"personality,omitempty"`

	// RequireContextQuery overrides the service default.
	RequireContextQuery *bool `json:"require_context_query,omitempty"`
}

// ChatResponse is the response from the companion.
type ChatResponse struct {
	// Response is the assistant's response text.
	Response string `json:"response"`

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

	// Create RLM tool executor
	rlmExecutor := engine.NewRLMToolExecutor(s.contextStore, req.ConversationID)

	// Enable semantic search over companion memories if memory store is configured
	if s.config.MemoryStore != nil && s.config.MemoryWorkspace != "" {
		rlmExecutor.SetMemoryStore(s.config.MemoryStore, s.config.MemoryWorkspace)
	}

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
		Logger:                s.logger,
	}

	// Override RequireContextQuery if specified in request
	if req.RequireContextQuery != nil {
		engineCfg.RequireContextQuery = *req.RequireContextQuery
	}

	llmEngine, err := engine.NewLLMChatEngine(engineCfg)
	if err != nil {
		return nil, fmt.Errorf("create engine: %w", err)
	}

	// Set up tool runner with RLM tools
	toolRunner := engine.NewToolRunner(rlmExecutor, nil, engine.DefaultToolRunnerConfig())
	llmEngine.SetToolRunner(toolRunner)
	llmEngine.SetRLMExecutor(rlmExecutor)

	// Build input with evolving personality
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

	input := engine.EngineInput{
		SystemPrompt: systemPrompt,
		Messages: []engine.Message{
			engine.NewUserMessage(req.Message),
		},
		Tools: rlmExecutor.List(),
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
	toolNames := make(map[string]bool)
	for _, tc := range output.ToolCalls {
		toolNames[tc.Name] = true
	}
	for name := range toolNames {
		resp.ToolsUsed = append(resp.ToolsUsed, name)
	}

	// Check for errors
	if output.StopReason == engine.StopReasonError {
		resp.Error = output.Error
	}

	// Store conversation turns in memory (synchronous to ensure DB isn't closed prematurely)
	if s.memory != nil && resp.Error == "" {
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
			Content:        output.AssistantText,
			CreatedAt:      time.Now(),
		}
		if err := s.memory.AppendTurn(ctx, assistantTurn); err != nil {
			s.logger.Warn().Err(err).Msg("Failed to store assistant turn")
		}
	}

	return resp, nil
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
