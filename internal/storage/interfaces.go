// Package storage defines shared interfaces and data structures for agentctl stores.
package storage

import (
	"context"
	"io"
	"time"

	jobtypes "github.com/jkatigb/agentctl/internal/storage/jobs/types"
)

// Store is the minimal interface implemented by all storage providers.
type Store interface {
	Close() error
}

// CacheEntry represents the persisted data for a cache item.
type CacheEntry struct {
	CacheKey     string
	SkillName    string
	SkillVersion string
	Workspace    string
	Result       []byte
	Digests      []string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	LastAccessed time.Time
	HitCount     int
}

// CacheStats captures high-level cache metadata.
type CacheStats struct {
	Entries int64
	Path    string
	TTL     time.Duration
}

// CacheStore manages cached run results.
type CacheStore interface {
	Store
	Get(ctx context.Context, key string) (CacheEntry, bool, error)
	Put(ctx context.Context, entry CacheEntry) error
	Recent(ctx context.Context, workspace string, limit int) ([]CacheEntry, error)
	Delete(ctx context.Context, key string) error
	Stats(ctx context.Context) (CacheStats, error)
}

// Job is an alias to the canonical job metadata type.
type Job = jobtypes.Job

// JobState aliases the canonical job state enumeration.
type JobState = jobtypes.State

const (
	// JobStateQueued mirrors jobtypes.StateQueued.
	JobStateQueued = jobtypes.StateQueued
	// JobStateRunning mirrors jobtypes.StateRunning.
	JobStateRunning = jobtypes.StateRunning
	// JobStateOK mirrors jobtypes.StateOK.
	JobStateOK = jobtypes.StateOK
	// JobStateError mirrors jobtypes.StateError.
	JobStateError = jobtypes.StateError
	// JobStateCanceled mirrors jobtypes.StateCanceled.
	JobStateCanceled = jobtypes.StateCanceled
)

// JobStore exposes the public job management surface.
type JobStore interface {
	Store
	SubmitEcho(ctx context.Context, message string) (Job, error)
	List(ctx context.Context, limit int) ([]Job, error)
	Get(ctx context.Context, id string) (Job, error)
	Result(ctx context.Context, id string) ([]byte, error)
	Cancel(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
	FindOrPrepareSkillJob(ctx context.Context, name string, input []byte, dedupe bool) (Job, bool, error)
	SetWorkspace(ctx context.Context, jobID, workspacePath string) error
	WaitForCompletion(ctx context.Context, jobID string, pollInterval time.Duration) (Job, error)
	TailProgress(ctx context.Context, jobID string, follow bool, w io.Writer) error
	ExecutePreparedSkill(ctx context.Context, jobID, manifestPath, artifactPath string) ([]byte, error)
}

// CASMetadata describes a stored CAS object.
type CASMetadata struct {
	Digest    string    `json:"digest"`
	Size      int64     `json:"size_bytes"`
	Kind      string    `json:"kind,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Metadata provides a compatibility alias for embedded object metadata.
type Metadata = CASMetadata

// CASObject augments metadata with pin state.
type CASObject struct {
	Metadata
	Pinned bool `json:"pinned"`
}

// CASGCOptions controls garbage collection behavior.
type CASGCOptions struct {
	DryRun     bool
	OlderThan  time.Duration
	KeepPinned bool
	MaxDelete  int
}

// CASGCResult summarizes the outcome of a GC run.
type CASGCResult struct {
	ObjectsDeleted int   `json:"objects_deleted"`
	ObjectsSkipped int   `json:"objects_skipped"`
	BytesFreed     int64 `json:"bytes_freed"`
	Errors         int   `json:"errors"`
}

// CASStore defines the CAS operations used by the CLI.
type CASStore interface {
	Store
	Put(ctx context.Context, r io.Reader, kind string, tags []string) (CASObject, error)
	Get(ctx context.Context, digest string) (io.ReadCloser, CASMetadata, error)
	Head(ctx context.Context, digest string) (CASObject, error)
	List(ctx context.Context) ([]CASObject, error)
	Remove(ctx context.Context, digest string) error
	Pin(ctx context.Context, digest string) error
	Unpin(ctx context.Context, digest string) error
	AddTags(ctx context.Context, digest string, tags []string) error
	GC(ctx context.Context, opts CASGCOptions) (CASGCResult, error)
}

// NamedEntry captures a stored named memory entry.
type NamedEntry struct {
	ID          string
	Name        string
	Type        string
	Workspace   string
	Summary     string
	Result      []byte
	Digests     []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	LastAccess  time.Time
	AccessCount int
	SessionID   string // AI coding tool session ID (Claude Code, OpenCode, Cursor, etc.)
}

// ScoredEntry couples a named entry with a relevance score.
type ScoredEntry struct {
	Entry NamedEntry
	Score float64
}

// MemoryStats summarizes named memory metadata.
type MemoryStats struct {
	Named int64
	Path  string
}

// MemoryStore persists named memories and auto-cache entries.
type MemoryStore interface {
	Store
	Save(ctx context.Context, entry NamedEntry) (NamedEntry, error)
	SaveFromResult(ctx context.Context, name, typ, workspace, summary string, result []byte) (NamedEntry, error)
	Get(ctx context.Context, name, workspace string) (NamedEntry, error)
	List(ctx context.Context, workspace string, limit int) ([]NamedEntry, error)
	Delete(ctx context.Context, name, workspace string) error
	DeleteByNamePrefix(ctx context.Context, workspace, namePrefix string) (int, error)
	Search(ctx context.Context, workspace, query string, limit int) ([]ScoredEntry, error)
	Update(ctx context.Context, name, workspace string, summary, typ *string) (NamedEntry, error)
	Relevant(ctx context.Context, workspace string, limit int) ([]ScoredEntry, error)
	Stats(ctx context.Context) (MemoryStats, error)
	// UpdateEmbedding stores an embedding vector for a named memory entry.
	UpdateEmbedding(ctx context.Context, name, workspace string, embedding []float32) error
	// SearchSimilar finds entries similar to the given embedding using vector similarity.
	SearchSimilar(ctx context.Context, workspace string, embedding []float32, limit int) ([]ScoredEntry, error)
}

// Session represents a captured Claude Code conversation session.
type Session struct {
	ID              string    `json:"id"`
	WorkspacePath   string    `json:"workspace_path"`
	ProjectName     string    `json:"project_name"`
	GitBranch       string    `json:"git_branch"`
	ClaudeVersion   string    `json:"claude_version"`
	StartedAt       time.Time `json:"started_at"`
	EndedAt         time.Time `json:"ended_at"`
	Summary         string    `json:"summary"`
	Accomplished    []string  `json:"accomplished"`
	Decisions       []string  `json:"decisions"`
	Gotchas         []string  `json:"gotchas"`
	UserInsights    []string  `json:"user_insights,omitempty"`
	Tags            []string  `json:"tags"`
	KeyFiles        []string  `json:"key_files"`
	ToolsPattern    string    `json:"tools_pattern"`
	MessageCount    int       `json:"message_count"`
	UserTurns       int       `json:"user_turns"`
	ToolInvocations int       `json:"tool_invocations"`
	TotalTokens     int       `json:"total_tokens"`
	RawJSONLPath    string    `json:"raw_jsonl_path"`
	Embedding       []byte    `json:"embedding,omitempty"`
	EmbeddingModel  string    `json:"embedding_model,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	// Lineage fields for session tracking
	ParentSessionID string `json:"parent_session_id,omitempty"`
	AgentID         string `json:"agent_id,omitempty"` // AI agent identifier (default: "agentctl")
	Status          string `json:"status,omitempty"`   // ok, error, canceled
}

// Session status constants
const (
	SessionStatusRunning  = "running"  // Session is active
	SessionStatusOK       = "ok"       // Session completed successfully
	SessionStatusError    = "error"    // Session ended with error
	SessionStatusCanceled = "canceled" // Session was canceled
)

// IsTerminalStatus returns true if the status indicates the session has ended.
func IsTerminalStatus(status string) bool {
	return status == SessionStatusOK || status == SessionStatusError || status == SessionStatusCanceled
}

// SessionEdge represents a relationship between two sessions.
type SessionEdge struct {
	ID          string         `json:"id"`
	Workspace   string         `json:"workspace"`
	FromSession string         `json:"from_session"`
	ToSession   string         `json:"to_session"`
	EdgeType    string         `json:"edge_type"` // continues, forked_from, relates_to
	CreatedAt   time.Time      `json:"created_at"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// Session edge type constants
const (
	SessionEdgeContinues  = "continues"
	SessionEdgeForkedFrom = "forked_from"
	SessionEdgeRelatesTo  = "relates_to"
)

// SessionStats summarizes session store metadata.
type SessionStats struct {
	Count int64
	Path  string
}

// SessionListOptions configures session listing.
type SessionListOptions struct {
	WorkspacePath string
	ProjectName   string
	Tags          []string
	Limit         int
	Offset        int
}

// SimilarSession represents a session with similarity score.
type SimilarSession struct {
	Session    Session `json:"session"`
	Similarity float64 `json:"similarity"`
}

// SessionTurn represents a single turn in a conversation session.
type SessionTurn struct {
	ID             string     `json:"id"`
	SessionID      string     `json:"session_id"`
	TurnIndex      int        `json:"turn_index"`
	Role           string     `json:"role"` // 'user', 'assistant', 'system'
	ContentPreview string     `json:"content_preview,omitempty"`
	ToolCalls      []ToolCall `json:"tool_calls,omitempty"`
	FilesTouched   []string   `json:"files_touched,omitempty"`
	HasError       bool       `json:"has_error"`
	ErrorType      string     `json:"error_type,omitempty"`
	ErrorMessage   string     `json:"error_message,omitempty"`
	Resolution     string     `json:"resolution,omitempty"`
	TokensUsed     int        `json:"tokens_used"`
	Timestamp      time.Time  `json:"timestamp"`
	CreatedAt      time.Time  `json:"created_at"`
}

// ToolCall represents a tool invocation within a turn.
type ToolCall struct {
	Name    string `json:"name"`
	Success bool   `json:"success"`
}

// SessionTurnListOptions configures turn listing.
type SessionTurnListOptions struct {
	SessionID  string
	ErrorsOnly bool
	Role       string
	Limit      int
	Offset     int
}

// SessionStore persists Claude Code conversation sessions.
type SessionStore interface {
	Store
	Save(ctx context.Context, session Session) (Session, error)
	Get(ctx context.Context, id string) (Session, error)
	List(ctx context.Context, opts SessionListOptions) ([]Session, error)
	Delete(ctx context.Context, id string) error
	Search(ctx context.Context, query string, limit int) ([]Session, error)
	SearchSimilar(ctx context.Context, embedding []float32, limit int) ([]SimilarSession, error)
	UpdateSummary(ctx context.Context, id string, summary string, accomplished, decisions, gotchas, userInsights, tags, keyFiles []string, toolsPattern string) error
	SetEmbedding(ctx context.Context, id string, embedding []byte, model string) error
	Stats(ctx context.Context) (SessionStats, error)

	// Lineage operations
	GetActive(ctx context.Context, workspace, agentID string) (*Session, error)
	SetStatus(ctx context.Context, id, status string) error
	FindLastSession(ctx context.Context, workspace, agentID string, statuses []string) (*Session, error)
	SaveEdge(ctx context.Context, edge SessionEdge) error
	GetAncestorChain(ctx context.Context, sessionID string, maxDepth int) ([]Session, error)
	GetEdges(ctx context.Context, sessionID string) ([]SessionEdge, error)

	// Turn operations
	SaveTurn(ctx context.Context, turn SessionTurn) (SessionTurn, error)
	SaveTurns(ctx context.Context, turns []SessionTurn) error
	GetTurns(ctx context.Context, sessionID string, opts SessionTurnListOptions) ([]SessionTurn, error)
	GetTurnsWithErrors(ctx context.Context, sessionID string) ([]SessionTurn, error)
	SearchTurns(ctx context.Context, query string, limit int) ([]SessionTurn, error)
	DeleteTurns(ctx context.Context, sessionID string) error

	// Chunk operations (for JSONL archive deep retrieval)
	SaveChunk(ctx context.Context, chunk SessionChunk) (SessionChunk, error)
	SaveChunks(ctx context.Context, chunks []SessionChunk) error
	GetChunks(ctx context.Context, sessionID string, limit int) ([]SessionChunk, error)
	GetChunk(ctx context.Context, sessionID string, chunkIndex int) (SessionChunk, error)
	SearchChunks(ctx context.Context, embedding []float32, limit int) ([]ScoredChunk, error)
	DeleteChunks(ctx context.Context, sessionID string) error
	SetArchivePath(ctx context.Context, sessionID, archivePath string) error
	GetArchivePath(ctx context.Context, sessionID string) (string, error)
}

// SessionChunk represents a chunk of a session for deep retrieval.
type SessionChunk struct {
	ID             string    `json:"id"`
	SessionID      string    `json:"session_id"`
	ChunkIndex     int       `json:"chunk_index"`
	ChunkType      string    `json:"chunk_type"` // 'user_request', 'assistant_response', 'tool_output', 'error'
	ContentHash    string    `json:"content_hash"`
	ContentPreview string    `json:"content_preview,omitempty"`
	ByteOffset     int64     `json:"byte_offset"`
	ByteLength     int64     `json:"byte_length"`
	ToolsUsed      []string  `json:"tools_used,omitempty"`
	FilesTouched   []string  `json:"files_touched,omitempty"`
	HasError       bool      `json:"has_error"`
	ErrorType      string    `json:"error_type,omitempty"`
	Embedding      []byte    `json:"embedding,omitempty"`
	EmbeddingModel string    `json:"embedding_model,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// ScoredChunk couples a chunk with a relevance score.
type ScoredChunk struct {
	Chunk      SessionChunk `json:"chunk"`
	Similarity float64      `json:"similarity"`
}

// ChunkListOptions configures chunk listing.
type ChunkListOptions struct {
	SessionID  string
	ChunkType  string
	ErrorsOnly bool
	Limit      int
	Offset     int
}
