// Package contextvar implements a SQLite-backed store for RLM context variables.
//
// In RLM (Recursive Language Model) architecture, context is stored externally
// and queried on-demand via tools, rather than accumulated in the LLM context window.
// This enables stateless per-turn operation with active context navigation.
package contextvar

import (
	"encoding/json"
	"time"
)

// Scope defines the persistence scope of a context variable.
type Scope string

const (
	// ScopeGlobal persists across all conversations (user profile, preferences).
	ScopeGlobal Scope = "global"

	// ScopeConversation persists for a specific conversation only.
	ScopeConversation Scope = "conversation"

	// ScopeTurn is ephemeral, only for the current turn.
	ScopeTurn Scope = "turn"
)

// Variable represents a context variable stored externally.
type Variable struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"` // Groups variables for a user/conversation
	Scope          Scope  `json:"scope"`           // global|conversation|turn
	Key            string `json:"key"`             // Variable name/path

	// Content - either inline JSON or CAS reference for large values
	ValueJSON   json.RawMessage `json:"value_json,omitempty"`
	ValueCAS    string          `json:"value_cas,omitempty"` // CAS digest for >64KB values
	ContentType string          `json:"content_type"`        // json|markdown|code|text

	// Ordering for range queries
	SequenceNum int `json:"sequence_num"`

	// Metadata
	Source      string     `json:"source"` // Producer (tool, skill, or "user")
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	AccessCount int        `json:"access_count"`
	LastAccess  *time.Time `json:"last_access,omitempty"`

	// Semantic search support
	Embedding      []float32 `json:"embedding,omitempty"`
	EmbeddingModel string    `json:"embedding_model,omitempty"`
}

// PutParams for storing a context variable.
type PutParams struct {
	ConversationID string        // Required for conversation/turn scope; empty for global
	Scope          Scope         // Required: global|conversation|turn
	Key            string        // Required: variable name/path
	Value          interface{}   // Required: will be JSON-marshaled
	ContentType    string        // Optional: defaults to "json"
	Source         string        // Optional: identifies the producer
	TTL            time.Duration // Optional: auto-expire after duration
	Upsert         bool          // If true, update existing key instead of creating new
}

// QueryParams for retrieving context variables.
type QueryParams struct {
	ConversationID string // Filter by conversation (empty = global only)
	Scope          Scope  // Filter by scope (empty = all scopes)
	Key            string // Exact key match
	KeyPattern     string // Glob pattern (e.g., "memories/*", "user.*")
	KeyPrefix      string // Key prefix match
	SemanticQuery  string // Natural language query (requires embedding)
	SequenceRange  *Range // Sequence number range
	IncludeExpired bool   // Include expired variables (default: exclude)
	Limit          int    // Max results (default: 50)
	Offset         int    // Pagination offset
	OrderBy        string // "created_at", "updated_at", "sequence_num", "access_count"
	OrderDesc      bool   // Descending order
}

// Range specifies a sequence number range for range queries.
type Range struct {
	Start int // Inclusive start
	End   int // Inclusive end (-1 for no upper bound)
}

// QueryResult from a query operation.
type QueryResult struct {
	Variables  []Variable `json:"variables"`
	TotalCount int        `json:"total_count"` // Total matching (before limit)
	HasMore    bool       `json:"has_more"`    // More results available
}

// ScoredVariable includes relevance score from semantic search.
type ScoredVariable struct {
	Variable
	Score float32 `json:"score"` // Relevance score (0-1)
}

// SemanticResult from a semantic query.
type SemanticResult struct {
	Variables  []ScoredVariable `json:"variables"`
	TotalCount int              `json:"total_count"`
}

// Stats provides storage statistics.
type Stats struct {
	TotalVariables    int       `json:"total_variables"`
	GlobalVariables   int       `json:"global_variables"`
	ConversationCount int       `json:"conversation_count"`
	ExpiredCount      int       `json:"expired_count"`
	TotalSizeBytes    int64     `json:"total_size_bytes"`
	OldestCreatedAt   time.Time `json:"oldest_created_at"`
	NewestCreatedAt   time.Time `json:"newest_created_at"`
}

// ListKeysResult from listing keys.
type ListKeysResult struct {
	Keys       []KeyInfo `json:"keys"`
	TotalCount int       `json:"total_count"`
}

// KeyInfo provides metadata about a key.
type KeyInfo struct {
	Key         string    `json:"key"`
	Scope       Scope     `json:"scope"`
	ContentType string    `json:"content_type"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	AccessCount int       `json:"access_count"`
	SizeBytes   int       `json:"size_bytes"`
}
