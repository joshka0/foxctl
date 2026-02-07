package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/contextvar"
)

// RLMToolExecutor implements ToolExecutor for RLM context operations.
// These tools allow the LLM to query and store context in stateless mode.
type RLMToolExecutor struct {
	store          contextvar.Store
	conversationID string
	queryCount     int // Tracks queries per turn for validation

	// Optional: for semantic search over named memories
	memoryStore   storage.MemoryStore
	workspace     string
	embedProvider semantic.EmbeddingProvider
}

// NewRLMToolExecutor creates a new RLM tool executor.
//
// Index:
// - Purpose: Initialize RLM context tool execution with per-conversation state
// - Flow: store context store and conversation ID → return executor
// - Related: RLMToolExecutor.Execute, RLMToolDefs
// - Keywords: rlm_context, tool_executor, conversation_id, context_store
func NewRLMToolExecutor(store contextvar.Store, conversationID string) *RLMToolExecutor {
	return &RLMToolExecutor{
		store:          store,
		conversationID: conversationID,
	}
}

// ResetQueryCount resets the query counter (call at start of each turn).
func (e *RLMToolExecutor) ResetQueryCount() {
	e.queryCount = 0
}

// SetMemoryStore configures the memory store for semantic search.
// Call this to enable semantic_query in rlm_context_query.
func (e *RLMToolExecutor) SetMemoryStore(store storage.MemoryStore, workspace string) {
	e.memoryStore = store
	e.workspace = workspace
}

// SetEmbedProvider configures the embedding provider for semantic search.
// Required for vector-based semantic search; falls back to text search if nil.
func (e *RLMToolExecutor) SetEmbedProvider(provider semantic.EmbeddingProvider) {
	e.embedProvider = provider
}

// QueryCount returns the number of context queries this turn.
func (e *RLMToolExecutor) QueryCount() int {
	return e.queryCount
}

// Execute implements ToolExecutor.
//
// Index:
// - Purpose: Dispatch RLM context tool calls by name
// - Flow: switch on tool name → execute handler → return JSON output
// - SideEffects: context store reads/writes; query count increments
// - FailureModes: unknown tool, handler errors
// - Related: executePut, executeQuery, executeList, executePersonalityAdjust
// - Keywords: rlm_context_put, rlm_context_query, rlm_context_list, rlm_personality_adjust
func (e *RLMToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	switch name {
	case "rlm_context_put":
		return e.executePut(ctx, args)
	case "rlm_context_query":
		return e.executeQuery(ctx, args)
	case "rlm_context_list":
		return e.executeList(ctx, args)
	case "rlm_personality_adjust":
		return e.executePersonalityAdjust(ctx, args)
	default:
		return "", fmt.Errorf("unknown RLM tool: %s", name)
	}
}

// List implements ToolExecutor.
func (e *RLMToolExecutor) List() []ToolDef {
	return RLMToolDefs()
}

// RLMToolDefs returns the tool definitions for RLM context operations.
//
// Index:
// - Purpose: Describe available RLM context tools and schemas
// - Flow: build tool definitions → return list
// - Related: RLMToolExecutor.Execute
// - Keywords: rlm_context_put, rlm_context_query, rlm_context_list, rlm_personality_adjust
func RLMToolDefs() []ToolDef {
	return []ToolDef{
		{
			Name:        "rlm_context_put",
			Description: "Store a context variable for later retrieval. Use this to save important information about the conversation or user that should persist.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"key": {
						"type": "string",
						"description": "The key name for the variable (e.g., 'user_name', 'current_topic', 'preferences/theme')"
					},
					"value": {
						"description": "The value to store (any JSON-serializable value)"
					},
					"scope": {
						"type": "string",
						"enum": ["global", "conversation", "turn"],
						"description": "Persistence scope: 'global' (all conversations), 'conversation' (this conversation only), 'turn' (ephemeral, current turn only). Defaults to 'conversation'."
					},
					"ttl_seconds": {
						"type": "integer",
						"description": "Optional time-to-live in seconds. Variable expires after this duration."
					}
				},
				"required": ["key", "value"]
			}`),
		},
		{
			Name:        "rlm_context_query",
			Description: "Retrieve context variables. Use this to recall information about the user or conversation. IMPORTANT: Always query context before making assumptions about the user or conversation history.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"key": {
						"type": "string",
						"description": "Exact key to retrieve (e.g., 'user_name')"
					},
					"key_pattern": {
						"type": "string",
						"description": "Glob pattern to match keys (e.g., 'preferences/*', 'memory_*')"
					},
					"semantic_query": {
						"type": "string",
						"description": "Natural language query to search your conversation memories. Use this to find past topics, preferences, or experiences that may be relevant. Example: 'hobbies they mentioned' or 'previous technical discussions'."
					},
					"scope": {
						"type": "string",
						"enum": ["global", "conversation", "turn", ""],
						"description": "Filter by scope. Empty for all scopes."
					},
					"limit": {
						"type": "integer",
						"description": "Maximum results to return (default: 20)"
					}
				}
			}`),
		},
		{
			Name:        "rlm_context_list",
			Description: "List all available context keys for this conversation. Use this to discover what context is available before querying specific keys.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"scope": {
						"type": "string",
						"enum": ["global", "conversation", "turn", ""],
						"description": "Filter by scope. Empty for all scopes."
					}
				}
			}`),
		},
		{
			Name:        "rlm_personality_adjust",
			Description: "Adjust your communication style based on user feedback or observed preferences. Use when the user expresses preferences about how you communicate (e.g., 'be more concise', 'I like your enthusiasm'), or when you notice patterns in what they respond well to.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"dimension": {
						"type": "string",
						"enum": ["formality", "verbosity", "enthusiasm", "humor", "empathy", "proactivity"],
						"description": "Which personality dimension to adjust: formality (formal↔casual), verbosity (brief↔detailed), enthusiasm (calm↔energetic), humor (serious↔playful), empathy (task-focused↔supportive), proactivity (responsive↔suggests)"
					},
					"direction": {
						"type": "string",
						"enum": ["increase", "decrease", "note"],
						"description": "Direction of adjustment, or 'note' to record a learned preference without changing dimensions"
					},
					"amount": {
						"type": "number",
						"description": "Adjustment strength: 0.1=subtle, 0.2=moderate, 0.3=significant. Default 0.1"
					},
					"note": {
						"type": "string",
						"description": "Learned preference to remember (e.g., 'prefers technical depth', 'enjoys analogies', 'likes bullet points')"
					},
					"reason": {
						"type": "string",
						"description": "Why this adjustment - helps track what triggered the change"
					}
				},
				"required": ["direction"]
			}`),
		},
	}
}

// ContextPutInput is the input for rlm_context_put.
type ContextPutInput struct {
	Key        string      `json:"key"`
	Value      interface{} `json:"value"`
	Scope      string      `json:"scope,omitempty"`
	TTLSeconds int         `json:"ttl_seconds,omitempty"`
}

// ContextQueryInput is the input for rlm_context_query.
type ContextQueryInput struct {
	Key           string `json:"key,omitempty"`
	KeyPattern    string `json:"key_pattern,omitempty"`
	SemanticQuery string `json:"semantic_query,omitempty"`
	Scope         string `json:"scope,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

// ContextListInput is the input for rlm_context_list.
type ContextListInput struct {
	Scope string `json:"scope,omitempty"`
}

// PersonalityAdjustInput is the input for rlm_personality_adjust.
type PersonalityAdjustInput struct {
	Dimension string  `json:"dimension,omitempty"`
	Direction string  `json:"direction"`
	Amount    float64 `json:"amount,omitempty"`
	Note      string  `json:"note,omitempty"`
	Reason    string  `json:"reason,omitempty"`
}

// PersonalityDimension represents an adjustable personality dimension.
type PersonalityDimension struct {
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Value       float64 `json:"value"`
	MinLabel    string  `json:"min_label"`
	MaxLabel    string  `json:"max_label"`
}

// PersonalityProfile is stored in context for persistence.
type PersonalityProfile struct {
	Dimensions    []PersonalityDimension `json:"dimensions"`
	LearnedTraits []string               `json:"learned_traits"`
}

// executePut stores a context variable via rlm_context_put.
//
// Index:
// - Purpose: Persist a context variable with scope and optional TTL
// - Flow: parse input → map scope → build params → store → marshal result
// - SideEffects: writes to context store
// - FailureModes: invalid input, invalid scope, store errors
// - Related: contextvar.Store.Put
// - Keywords: rlm_context_put, scope, ttl_seconds, context_store, upsert
func (e *RLMToolExecutor) executePut(ctx context.Context, args json.RawMessage) (string, error) {
	var input ContextPutInput
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if input.Key == "" {
		return "", errors.New("key is required")
	}

	// Map scope string to type
	var scope contextvar.Scope
	switch input.Scope {
	case "global":
		scope = contextvar.ScopeGlobal
	case "turn":
		scope = contextvar.ScopeTurn
	case "conversation", "":
		scope = contextvar.ScopeConversation
	default:
		return "", fmt.Errorf("invalid scope: %s", input.Scope)
	}

	// Build params
	params := contextvar.PutParams{
		ConversationID: e.conversationID,
		Scope:          scope,
		Key:            input.Key,
		Value:          input.Value,
		Source:         "rlm_context_put",
		Upsert:         true, // Always upsert for RLM tools
	}

	if input.TTLSeconds > 0 {
		params.TTL = time.Duration(input.TTLSeconds) * time.Second
	}

	v, err := e.store.Put(ctx, params)
	if err != nil {
		return "", fmt.Errorf("failed to store context: %w", err)
	}

	result := map[string]interface{}{
		"success": true,
		"key":     v.Key,
		"scope":   string(v.Scope),
		"id":      v.ID,
	}

	b, _ := json.Marshal(result)
	return string(b), nil
}

// executeQuery retrieves context variables via rlm_context_query.
//
// Index:
// - Purpose: Query context variables by key, pattern, or semantic search
// - Flow: parse input → increment count → run semantic or store query → format result
// - SideEffects: reads context store; increments access counts
// - FailureModes: invalid input, store errors
// - Related: executeSemanticQuery, contextvar.Store.Query
// - Keywords: rlm_context_query, key_pattern, semantic_query, access_count, context_store
func (e *RLMToolExecutor) executeQuery(ctx context.Context, args json.RawMessage) (string, error) {
	var input ContextQueryInput
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Track query
	e.queryCount++

	// Handle semantic query over named memories
	if input.SemanticQuery != "" {
		return e.executeSemanticQuery(ctx, input)
	}

	// Build query params
	limit := 20
	if input.Limit > 0 {
		limit = input.Limit
	}

	params := contextvar.QueryParams{
		ConversationID: e.conversationID,
		Key:            input.Key,
		KeyPattern:     input.KeyPattern,
		Limit:          limit,
	}

	// Map scope
	switch input.Scope {
	case "global":
		params.Scope = contextvar.ScopeGlobal
	case "conversation":
		params.Scope = contextvar.ScopeConversation
	case "turn":
		params.Scope = contextvar.ScopeTurn
	}

	// For exact key match, try GetByKey first
	if input.Key != "" && input.KeyPattern == "" {
		// Try each scope if not specified
		if params.Scope != "" {
			v, err := e.store.GetByKey(ctx, e.conversationID, params.Scope, input.Key)
			if err != nil && !errors.Is(err, contextvar.ErrNotFound) {
				return "", fmt.Errorf("query failed: %w", err)
			}
			if v != nil {
				// Increment access count
				_ = e.store.IncrementAccess(ctx, v.ID)
				return formatQueryResult([]contextvar.Variable{*v})
			}
		} else {
			// Try conversation scope first, then global
			for _, scope := range []contextvar.Scope{contextvar.ScopeConversation, contextvar.ScopeGlobal, contextvar.ScopeTurn} {
				v, err := e.store.GetByKey(ctx, e.conversationID, scope, input.Key)
				if err != nil && !errors.Is(err, contextvar.ErrNotFound) {
					return "", fmt.Errorf("query failed: %w", err)
				}
				if v != nil {
					_ = e.store.IncrementAccess(ctx, v.ID)
					return formatQueryResult([]contextvar.Variable{*v})
				}
			}
		}

		// Not found
		return `{"variables": [], "found": false}`, nil
	}

	// Pattern or multi-result query
	result, err := e.store.Query(ctx, params)
	if err != nil {
		return "", fmt.Errorf("query failed: %w", err)
	}

	// Increment access counts
	for _, v := range result.Variables {
		_ = e.store.IncrementAccess(ctx, v.ID)
	}

	return formatQueryResult(result.Variables)
}

// executeSemanticQuery searches named memories using semantic similarity.
// Falls back to text search if embeddings are not available.
//
// Index:
// - Purpose: Search companion memories by semantic similarity or text
// - Flow: validate memory store → run vector search → fallback text search → format results
// - SideEffects: reads memory store; may call embedding provider
// - FailureModes: memory store errors, embedding errors
// - Related: storage.MemoryStore.SearchSimilarByType, storage.MemoryStore.Search
// - Keywords: semantic_query, memory_store, embeddings, companion_history, scored_entry
func (e *RLMToolExecutor) executeSemanticQuery(ctx context.Context, input ContextQueryInput) (string, error) {
	// Check if memory store is configured
	if e.memoryStore == nil {
		return `{"memories": [], "message": "Memory store not configured for semantic search"}`, nil
	}

	const (
		defaultLimit    = 10
		maxLimit        = 20
		defaultMaxChars = 6000
		maxSummaryChars = 1200
	)

	limit := defaultLimit
	if input.Limit > 0 {
		limit = input.Limit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	// Filter by conversation (SessionID) to scope to this companion's memories
	filter := storage.MemoryListFilter{
		SessionID: e.conversationID,
		Types:     []string{"companion_summary", "companion_history"},
	}

	seen := make(map[string]struct{}, limit)
	var l2 []storage.ScoredEntry
	var l1 []storage.ScoredEntry
	method := "none"
	truncated := false

	add := func(dst *[]storage.ScoredEntry, m storage.ScoredEntry) {
		if m.Entry.Name == "" {
			return
		}
		if m.Entry.SessionID != e.conversationID {
			return
		}
		if m.Entry.Type != "companion_summary" && m.Entry.Type != "companion_history" {
			return
		}
		if _, ok := seen[m.Entry.Name]; ok {
			return
		}
		seen[m.Entry.Name] = struct{}{}
		*dst = append(*dst, m)
	}

	// L2 fast-path: deterministic lookup for distilled history when conversationID is known.
	// This avoids missing L2 due to post-filtering on shared workspaces.
	if strings.TrimSpace(e.conversationID) != "" {
		if entry, err := e.memoryStore.Get(ctx, fmt.Sprintf("companion:history:%s", e.conversationID), e.workspace); err == nil {
			add(&l2, storage.ScoredEntry{Entry: entry, Score: 1.0})
		}
	}

	// Try vector search if embedding provider is configured.
	if e.embedProvider != nil {
		embedding, err := e.embedProvider.Embed(ctx, input.SemanticQuery)
		if err == nil && len(embedding) > 0 {
			method = "vector_by_type"

			// Prefer returning L2 (history) first, then L1 (day summaries).
			if len(l2) == 0 {
				results, err := e.memoryStore.SearchSimilarByType(ctx, e.workspace, "companion_history", embedding, 50)
				if err == nil {
					for _, r := range results {
						add(&l2, r)
						if len(l2) >= 1 {
							break
						}
					}
				}
			}

			targetL1 := limit - len(l2)
			if targetL1 > 0 {
				// Over-fetch to compensate for SessionID post-filtering on shared workspaces.
				overfetch := targetL1 * 200
				if overfetch < 200 {
					overfetch = 200
				}
				if overfetch > 1000 {
					overfetch = 1000
				}

				results, err := e.memoryStore.SearchSimilarByType(ctx, e.workspace, "companion_summary", embedding, overfetch)
				if err == nil {
					for _, r := range results {
						add(&l1, r)
						if len(l1) >= targetL1 {
							break
						}
					}
				}
			}
		}
	}

	// Fallback to text search if vector search didn't find results.
	if len(l2)+len(l1) == 0 {
		method = "text"
		results, err := e.memoryStore.Search(ctx, e.workspace, input.SemanticQuery, limit*20)
		if err == nil {
			for _, r := range results {
				if r.Entry.Type == "companion_history" && len(l2) < 1 {
					add(&l2, r)
				} else {
					add(&l1, r)
				}
				if len(l2)+len(l1) >= limit {
					break
				}
			}
		}
	}

	// Also try listing filtered memories if we still didn't fill the requested limit.
	if len(l2)+len(l1) < limit {
		if method == "none" {
			method = "list_filtered"
		}
		entries, _, err := e.memoryStore.ListFiltered(ctx, e.workspace, filter, limit*2, 0)
		if err == nil && len(entries) > 0 {
			for _, entry := range entries {
				if entry.Type == "companion_history" && len(l2) < 1 {
					add(&l2, storage.ScoredEntry{Entry: entry, Score: 0.5})
				} else {
					add(&l1, storage.ScoredEntry{Entry: entry, Score: 0.5})
				}
				if len(l2)+len(l1) >= limit {
					break
				}
			}
		}
	}

	memories := append(append([]storage.ScoredEntry{}, l2...), l1...)

	// Format results for LLM consumption.
	results := make([]map[string]interface{}, 0, len(memories))
	for _, m := range memories {
		summary := strings.TrimSpace(m.Entry.Summary)
		if len(summary) > maxSummaryChars {
			summary = summary[:maxSummaryChars] + "..."
			truncated = true
		}

		result := map[string]interface{}{
			"name":       m.Entry.Name,
			"type":       m.Entry.Type,
			"layer":      memoryLayer(m.Entry.Type),
			"summary":    summary,
			"score":      m.Score,
			"created_at": m.Entry.CreatedAt.Format(time.RFC3339),
		}

		// Provide date as a first-class field for L1 summaries when name matches pattern.
		if m.Entry.Type == "companion_summary" {
			if date := memoryDateFromName(m.Entry.Name); date != "" {
				result["date"] = date
			}
		}

		results = append(results, result)
	}

	output := map[string]interface{}{
		"memories": results,
		"found":    len(results) > 0,
		"count":    len(results),
		"query":    input.SemanticQuery,
		"stats": map[string]interface{}{
			"method":    method,
			"max_chars": defaultMaxChars,
			"truncated": truncated,
		},
	}

	b, _ := json.Marshal(output)
	if len(b) > defaultMaxChars {
		// Best-effort: drop summaries to fit within a predictable bound.
		for i := range results {
			results[i]["summary"] = ""
		}
		output["stats"].(map[string]interface{})["truncated"] = true
		b, _ = json.Marshal(output)
		if len(b) > defaultMaxChars && len(results) > 0 {
			// Last resort: return the first entry only.
			output["memories"] = results[:1]
			output["count"] = 1
			b, _ = json.Marshal(output)
		}
	}

	return string(b), nil
}

func memoryLayer(entryType string) string {
	switch entryType {
	case "companion_history":
		return "L2"
	case "companion_summary":
		return "L1"
	default:
		return ""
	}
}

func memoryDateFromName(name string) string {
	// Expected: companion:summary:<conversationID>:<YYYY-MM-DD>
	parts := strings.Split(name, ":")
	if len(parts) < 4 {
		return ""
	}
	date := parts[len(parts)-1]
	if len(date) != len("2006-01-02") {
		return ""
	}
	// Loose validation: ensure it looks like YYYY-MM-DD.
	if date[4] != '-' || date[7] != '-' {
		return ""
	}
	return date
}

// executeList lists context keys via rlm_context_list.
//
// Index:
// - Purpose: List context keys filtered by scope
// - Flow: parse input → map scope → list keys → marshal response
// - SideEffects: reads context store
// - FailureModes: invalid input, store errors
// - Related: contextvar.Store.ListKeys
// - Keywords: rlm_context_list, scope, keys, context_store
func (e *RLMToolExecutor) executeList(ctx context.Context, args json.RawMessage) (string, error) {
	var input ContextListInput
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Map scope
	var scope contextvar.Scope
	switch input.Scope {
	case "global":
		scope = contextvar.ScopeGlobal
	case "conversation":
		scope = contextvar.ScopeConversation
	case "turn":
		scope = contextvar.ScopeTurn
	}

	result, err := e.store.ListKeys(ctx, e.conversationID, scope)
	if err != nil {
		return "", fmt.Errorf("list failed: %w", err)
	}

	// Format output
	output := map[string]interface{}{
		"keys":        result.Keys,
		"total_count": result.TotalCount,
	}

	b, _ := json.Marshal(output)
	return string(b), nil
}

// executePersonalityAdjust updates the personality profile via rlm_personality_adjust.
//
// Index:
// - Purpose: Adjust stored personality dimensions and learned traits
// - Flow: parse input → load profile → apply adjustment → save profile → marshal response
// - SideEffects: reads/writes context store
// - FailureModes: invalid input, load/save errors
// - Related: loadPersonalityProfile, savePersonalityProfile
// - Keywords: rlm_personality_adjust, personality_profile, dimensions, learned_traits
func (e *RLMToolExecutor) executePersonalityAdjust(ctx context.Context, args json.RawMessage) (string, error) {
	var input PersonalityAdjustInput
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Load current personality profile from global context
	profile, extras, err := e.loadPersonalityProfile(ctx)
	if err != nil {
		return "", fmt.Errorf("load profile: %w", err)
	}

	// Apply dimension adjustment if specified
	if input.Dimension != "" && input.Direction != "note" {
		delta := input.Amount
		if delta == 0 {
			delta = 0.1 // Default subtle adjustment
		}
		if input.Direction == "decrease" {
			delta = -delta
		}

		for i := range profile.Dimensions {
			if profile.Dimensions[i].Name == input.Dimension {
				profile.Dimensions[i].Value = clamp(profile.Dimensions[i].Value+delta, 0, 1)
				break
			}
		}
	}

	// Add note to learned traits if provided
	if input.Note != "" {
		profile.LearnedTraits = appendUnique(profile.LearnedTraits, input.Note)
		// Keep only last 10 traits
		if len(profile.LearnedTraits) > 10 {
			profile.LearnedTraits = profile.LearnedTraits[len(profile.LearnedTraits)-10:]
		}
	}

	// Save updated profile
	if err := e.savePersonalityProfile(ctx, profile, extras); err != nil {
		return "", fmt.Errorf("save profile: %w", err)
	}

	// Build response
	result := map[string]interface{}{
		"success":   true,
		"dimension": input.Dimension,
		"direction": input.Direction,
		"note":      input.Note,
		"profile": map[string]interface{}{
			"dimensions":     profile.Dimensions,
			"learned_traits": profile.LearnedTraits,
		},
	}

	b, _ := json.Marshal(result)
	return string(b), nil
}

// loadPersonalityProfile loads the personality profile from global context.
func (e *RLMToolExecutor) loadPersonalityProfile(ctx context.Context) (*PersonalityProfile, map[string]any, error) {
	v, err := e.store.GetByKey(ctx, e.conversationID, contextvar.ScopeGlobal, "personality/profile")
	if err != nil {
		if errors.Is(err, contextvar.ErrNotFound) {
			// Return default profile
			return &PersonalityProfile{
				Dimensions: defaultPersonalityDimensions(),
			}, map[string]any{}, nil
		}
		return nil, nil, err
	}

	var profile PersonalityProfile
	if err := json.Unmarshal(v.ValueJSON, &profile); err != nil {
		return nil, nil, fmt.Errorf("unmarshal profile: %w", err)
	}

	// Ensure all dimensions exist (in case new ones were added)
	profile.Dimensions = mergeDimensions(profile.Dimensions, defaultPersonalityDimensions())

	extras := map[string]any{}
	if err := json.Unmarshal(v.ValueJSON, &extras); err != nil {
		extras = map[string]any{}
	}
	return &profile, extras, nil
}

// savePersonalityProfile persists the personality profile to global context.
func (e *RLMToolExecutor) savePersonalityProfile(ctx context.Context, profile *PersonalityProfile, extras map[string]any) error {
	if extras == nil {
		extras = map[string]any{}
	}
	extras["dimensions"] = profile.Dimensions
	extras["learned_traits"] = profile.LearnedTraits

	_, err := e.store.Put(ctx, contextvar.PutParams{
		ConversationID: e.conversationID,
		Scope:          contextvar.ScopeGlobal,
		Key:            "personality/profile",
		Value:          extras,
		Source:         "rlm_personality_adjust",
		Upsert:         true,
	})
	return err
}

// defaultPersonalityDimensions returns the default personality configuration.
func defaultPersonalityDimensions() []PersonalityDimension {
	return []PersonalityDimension{
		{
			Name:        "formality",
			Description: "How formal vs casual the responses are",
			Value:       0.5,
			MinLabel:    "formal and professional",
			MaxLabel:    "casual and friendly",
		},
		{
			Name:        "verbosity",
			Description: "How detailed vs concise the responses are",
			Value:       0.5,
			MinLabel:    "brief and to-the-point",
			MaxLabel:    "detailed and thorough",
		},
		{
			Name:        "enthusiasm",
			Description: "Energy level in responses",
			Value:       0.6,
			MinLabel:    "calm and measured",
			MaxLabel:    "enthusiastic and energetic",
		},
		{
			Name:        "humor",
			Description: "Use of humor and playfulness",
			Value:       0.3,
			MinLabel:    "serious and straightforward",
			MaxLabel:    "playful and witty",
		},
		{
			Name:        "empathy",
			Description: "Emotional attunement and support",
			Value:       0.7,
			MinLabel:    "task-focused",
			MaxLabel:    "emotionally supportive",
		},
		{
			Name:        "proactivity",
			Description: "How much to offer suggestions and follow-ups",
			Value:       0.5,
			MinLabel:    "responsive only",
			MaxLabel:    "proactive with suggestions",
		},
	}
}

// mergeDimensions ensures all default dimensions exist in the profile.
func mergeDimensions(existing, defaults []PersonalityDimension) []PersonalityDimension {
	byName := make(map[string]PersonalityDimension, len(existing))
	for _, d := range existing {
		byName[d.Name] = d
	}

	result := make([]PersonalityDimension, 0, len(defaults)+len(existing))
	for _, d := range defaults {
		if existingDim, ok := byName[d.Name]; ok {
			if existingDim.Description == "" {
				existingDim.Description = d.Description
			}
			if existingDim.MinLabel == "" {
				existingDim.MinLabel = d.MinLabel
			}
			if existingDim.MaxLabel == "" {
				existingDim.MaxLabel = d.MaxLabel
			}
			result = append(result, existingDim)
			delete(byName, d.Name)
		} else {
			result = append(result, d)
		}
	}
	for _, d := range existing {
		if _, ok := byName[d.Name]; ok {
			result = append(result, d)
			delete(byName, d.Name)
		}
	}
	return result
}

// clamp restricts a value to a range.
func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// appendUnique adds an item to a slice if not already present.
func appendUnique(slice []string, item string) []string {
	for _, existing := range slice {
		if existing == item {
			return slice
		}
	}
	return append(slice, item)
}

// formatQueryResult formats query results for the LLM.
func formatQueryResult(variables []contextvar.Variable) (string, error) {
	// Convert to a format suitable for LLM consumption
	results := make([]map[string]interface{}, len(variables))
	for i, v := range variables {
		// Unmarshal the JSON value for cleaner output
		var value interface{}
		if len(v.ValueJSON) > 0 {
			_ = json.Unmarshal(v.ValueJSON, &value)
		}

		results[i] = map[string]interface{}{
			"key":          v.Key,
			"value":        value,
			"scope":        string(v.Scope),
			"created_at":   v.CreatedAt.Format(time.RFC3339),
			"updated_at":   v.UpdatedAt.Format(time.RFC3339),
			"source":       v.Source,
			"access_count": v.AccessCount,
		}

		if v.ExpiresAt != nil {
			results[i]["expires_at"] = v.ExpiresAt.Format(time.RFC3339)
		}
	}

	output := map[string]interface{}{
		"variables": results,
		"found":     len(results) > 0,
		"count":     len(results),
	}

	b, err := json.Marshal(output)
	return string(b), err
}

// CompositeToolExecutor combines multiple ToolExecutors into one.
// This allows mixing RLM tools with other tool sets.
type CompositeToolExecutor struct {
	executors []ToolExecutor
	toolIndex map[string]ToolExecutor
}

// NewCompositeToolExecutor creates a composite executor.
//
// Index:
// - Purpose: Merge multiple tool executors into a single dispatcher
// - Flow: store executors → build tool index → return composite
// - Related: CompositeToolExecutor.Execute, ToolExecutor.List
// - Keywords: composite_executor, tool_index, tool_defs, dispatcher
func NewCompositeToolExecutor(executors ...ToolExecutor) *CompositeToolExecutor {
	c := &CompositeToolExecutor{
		executors: executors,
		toolIndex: make(map[string]ToolExecutor),
	}

	// Build index
	for _, exec := range executors {
		for _, tool := range exec.List() {
			c.toolIndex[tool.Name] = exec
		}
	}

	return c
}

// Execute implements ToolExecutor.
func (c *CompositeToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	exec, ok := c.toolIndex[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	return exec.Execute(ctx, name, args)
}

// List implements ToolExecutor.
func (c *CompositeToolExecutor) List() []ToolDef {
	var tools []ToolDef
	for _, exec := range c.executors {
		tools = append(tools, exec.List()...)
	}
	return tools
}
