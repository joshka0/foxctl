package companion

// ConversationEvent represents a row in companion_events.
type ConversationEvent struct {
	ID               int64  `json:"id"`
	ConversationID   string `json:"conversation_id"`
	EventType        string `json:"event_type"`
	TurnID           string `json:"turn_id,omitempty"`
	ToolName         string `json:"tool_name,omitempty"`
	ToolRunID        string `json:"tool_run_id,omitempty"`
	ParentToolCallID int64  `json:"parent_tool_call_id,omitempty"`
	PayloadJSON      string `json:"payload_json,omitempty"`
	PayloadRef       string `json:"payload_ref,omitempty"`
	TokenCount       int    `json:"token_count,omitempty"`
	ContentHash      string `json:"content_hash,omitempty"`
	Content          string `json:"content,omitempty"`
	CreatedAt        string `json:"created_at"`
}

// HardStateEntry represents a row in companion_hard_state_entries.
type HardStateEntry struct {
	ID           int64   `json:"id"`
	ConversationID string  `json:"conversation_id"`
	EntryType    string  `json:"entry_type"`
	Key          string  `json:"key"`
	ValueJSON    string  `json:"value_json"`
	Status       string  `json:"status"`
	SourceEventID int64  `json:"source_event_id"`
	Confidence   float64 `json:"confidence"`
	MetadataJSON *string `json:"metadata_json,omitempty"`
	Supersedes   *int64  `json:"supersedes,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

// SoftEpisode represents a row in companion_soft_episodes.
type SoftEpisode struct {
	ID             int64   `json:"id"`
	ConversationID string  `json:"conversation_id"`
	EpisodeType    string  `json:"episode_type"`
	StartEventID   int64   `json:"start_event_id"`
	EndEventID     int64   `json:"end_event_id"`
	Summary        string  `json:"summary"`
	NeedsSummary   int     `json:"needs_summary"`
	AssumptionIDs  string  `json:"assumption_ids"`
	TokenCount     int     `json:"token_count"`
	BoundaryHash   string  `json:"boundary_hash"`
	CreatedAt      string  `json:"created_at"`
	DeletedAt      *string `json:"deleted_at,omitempty"`
}

// EvidenceSnippet represents a row in companion_evidence_snippets.
type EvidenceSnippet struct {
	ID             int64   `json:"id"`
	ConversationID string  `json:"conversation_id"`
	SourceEventID  int64   `json:"source_event_id"`
	EventType      string  `json:"event_type"`
	FactText       string  `json:"fact_text"`
	ContentHash    string  `json:"content_hash"`
	Confidence     float64 `json:"confidence"`
	Bucket         string  `json:"bucket"`
	TTLDays        *int    `json:"ttl_days,omitempty"`
	CreatedAt      string  `json:"created_at"`
	ExpiresAt      *string `json:"expires_at,omitempty"`
	CanVerify      bool    `json:"can_verify,omitempty"`
}

// Assumption represents a row in companion_assumptions_ledger.
type Assumption struct {
	ID                  int64   `json:"id"`
	ConversationID      string  `json:"conversation_id"`
	Assumption          string  `json:"assumption"`
	Status              string  `json:"status"`
	Reason              *string `json:"reason,omitempty"`
	SourceEventID       int64   `json:"source_event_id"`
	Confidence          float64 `json:"confidence"`
	CreatedAt           string  `json:"created_at"`
	RetractedAt         *string `json:"retracted_at,omitempty"`
	RetractedByEventID  *int64  `json:"retracted_by_event_id,omitempty"`
	RetractionReason    *string `json:"retraction_reason,omitempty"`
}

// MemoryModeState represents a row in companion_memory_mode_state.
type MemoryModeState struct {
	ConversationID    string `json:"conversation_id"`
	Mode             string `json:"mode"`
	SchemaVersion    int    `json:"schema_version"`
	LastProcessedEvent int64  `json:"last_processed_event"`
	LastSoftEvent     int64  `json:"last_soft_event"`
	LastEvidenceEvent int64  `json:"last_evidence_event"`
	UpdatedAt         string `json:"updated_at"`
}

// OpenEpisodeState represents a row in companion_open_episode.
type OpenEpisodeState struct {
	ConversationID    string  `json:"conversation_id"`
	StartEventID      int64   `json:"start_event_id"`
	EpisodeType       string  `json:"episode_type"`
	EventCount        int     `json:"event_count"`
	TopicSig          *string `json:"topic_sig,omitempty"`
	PendingSealReason  *string `json:"pending_seal_reason,omitempty"`
	UpdatedAt         string  `json:"updated_at"`
}

// OpenToolRun represents a row in companion_open_tool_runs.
type OpenToolRun struct {
	ConversationID     string  `json:"conversation_id"`
	ToolRunID         string  `json:"tool_run_id"`
	StartEventID      int64   `json:"start_event_id"`
	ParentCallEventID *int64  `json:"parent_call_event_id,omitempty"`
	CreatedAt         string  `json:"created_at"`
}

// ExtractionStagingEntry represents a row in companion_extraction_staging.
type ExtractionStagingEntry struct {
	ID                int64   `json:"id"`
	ConversationID    string  `json:"conversation_id"`
	SourceEventID     int64   `json:"source_event_id"`
	ProposedEntryType string  `json:"proposed_entry_type"`
	RawText           string  `json:"raw_text"`
	Reason            string  `json:"reason"`
	AttemptCount      int     `json:"attempt_count"`
	CreatedAt         string  `json:"created_at"`
	ResolvedAt        *string `json:"resolved_at,omitempty"`
	DiscardedAt       *string `json:"discarded_at,omitempty"`
	DiscardReason     *string `json:"discard_reason,omitempty"`
}

// HardStateCache represents a row in companion_hard_state_cache.
type HardStateCache struct {
	ConversationID string `json:"conversation_id"`
	CompactJSON    string `json:"compact_json"`
	LastEntryID    int64  `json:"last_entry_id"`
	UpdatedAt      string `json:"updated_at"`
}

const (
	EventTypeUserMessage      = "user_message"
	EventTypeAssistantMessage = "assistant_message"
	EventTypeToolCall        = "tool_call"
	EventTypeToolResult      = "tool_result"
)

const (
	EntryTypePreference   = "preference"
	EntryTypeDecision     = "decision"
	EntryTypeGlossary     = "glossary"
	EntryTypeOpenQuestion = "open_question"
	EntryTypeGoal         = "goal"
	EntryTypePolicy       = "policy"
)

const (
	EntryStatusActive     = "active"
	EntryStatusSuperseded = "superseded"
	EntryStatusRetracted  = "retracted"
)

const (
	AssumptionStatusActive    = "active"
	AssumptionStatusRetracted = "retracted"
	AssumptionStatusPromoted  = "promoted"
)

// ExtractedEntry represents a deterministic extraction result from event content.
type ExtractedEntry struct {
	EntryType  string  `json:"entry_type"`
	Key        string  `json:"key,omitempty"`
	RawText    string  `json:"raw_text"`
	Value      string  `json:"value,omitempty"`
	Confidence float64 `json:"confidence"`
}

func strPtr(s string) *string   { return &s }
func int64Ptr(i int64) *int64    { return &i }

const (
	MemoryModeLegacy = "legacy"
	MemoryModeHybrid = "hybrid"
)
