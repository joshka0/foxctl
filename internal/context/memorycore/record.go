package memorycore

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/storage"
)

type Kind string

const (
	KindWorkingContext  Kind = "working_context"
	KindEpisodicTrace   Kind = "episodic_trace"
	KindSemanticFact    Kind = "semantic_fact"
	KindDecision        Kind = "decision"
	KindProceduralSkill Kind = "procedural_skill"
	KindPolicyRule      Kind = "policy_rule"
	KindReflection      Kind = "reflection"
	KindEvalResult      Kind = "eval_result"
	KindAdapterExample  Kind = "adapter_example"
)

func (k Kind) IsValid() bool {
	switch k {
	case KindWorkingContext, KindEpisodicTrace, KindSemanticFact, KindDecision,
		KindProceduralSkill, KindPolicyRule, KindReflection, KindEvalResult,
		KindAdapterExample:
		return true
	default:
		return false
	}
}

type SourceLane string

const (
	SourceLaneNamedMemory        SourceLane = "named_memory"
	SourceLaneContextClaim       SourceLane = "context_claim"
	SourceLaneCompanionEvent     SourceLane = "companion_event"
	SourceLaneCompanionHardState SourceLane = "companion_hard_state"
	SourceLaneCompanionEvidence  SourceLane = "companion_evidence"
	SourceLaneTranscriptClaim    SourceLane = "transcript_claim"
	SourceLaneContextProposal    SourceLane = "context_proposal"
)

type LifecycleState string

const (
	LifecycleStateCandidate   LifecycleState = "candidate"
	LifecycleStateActive      LifecycleState = "active"
	LifecycleStateStale       LifecycleState = "stale"
	LifecycleStateArchived    LifecycleState = "archived"
	LifecycleStateDeprecated  LifecycleState = "deprecated"
	LifecycleStateQuarantined LifecycleState = "quarantined"
)

func (s LifecycleState) IsValid() bool {
	switch s {
	case LifecycleStateCandidate, LifecycleStateActive, LifecycleStateStale,
		LifecycleStateArchived, LifecycleStateDeprecated, LifecycleStateQuarantined:
		return true
	default:
		return false
	}
}

type ReviewStatus string

const (
	ReviewStatusUnreviewed       ReviewStatus = "unreviewed"
	ReviewStatusNeedsReview      ReviewStatus = "needs_review"
	ReviewStatusReviewed         ReviewStatus = "reviewed"
	ReviewStatusValidated        ReviewStatus = "validated"
	ReviewStatusFailedValidation ReviewStatus = "failed_validation"
)

type TemporalEnvelope struct {
	ObservedAt         string `json:"observed_at,omitempty"`
	IngestedAt         string `json:"ingested_at,omitempty"`
	EventAt            string `json:"event_at,omitempty"`
	ValidFrom          string `json:"valid_from,omitempty"`
	ValidUntil         string `json:"valid_until,omitempty"`
	LastAccessedAt     string `json:"last_accessed_at,omitempty"`
	LastUsedAt         string `json:"last_used_at,omitempty"`
	LastValidatedAt    string `json:"last_validated_at,omitempty"`
	LastPatchedAt      string `json:"last_patched_at,omitempty"`
	TemporalScope      string `json:"temporal_scope"`
	TTLSeconds         int64  `json:"ttl_seconds,omitempty"`
	RequiresValidation bool   `json:"revalidation_required,omitempty"`
}

type Provenance struct {
	SourceType      string   `json:"source_type"`
	SessionID       string   `json:"session_id,omitempty"`
	AgentID         string   `json:"agent_id,omitempty"`
	ToolCallID      string   `json:"tool_call_id,omitempty"`
	Commit          string   `json:"commit,omitempty"`
	FileRefs        []string `json:"file_refs,omitempty"`
	ParentMemoryIDs []string `json:"parent_memory_ids,omitempty"`
	CreatedBy       string   `json:"created_by"`
}

type TrustEnvelope struct {
	SourceTrust  string   `json:"source_trust"`
	Confidence   float64  `json:"confidence"`
	Authority    float64  `json:"authority"`
	Tainted      bool     `json:"tainted"`
	TaintReasons []string `json:"taint_reasons,omitempty"`
}

type LifecycleEnvelope struct {
	State        LifecycleState `json:"state"`
	Pinned       bool           `json:"pinned"`
	ReviewStatus ReviewStatus   `json:"review_status"`
	Supersedes   []string       `json:"supersedes,omitempty"`
	SupersededBy string         `json:"superseded_by,omitempty"`
	ReviewNotes  string         `json:"review_notes,omitempty"`
}

type TelemetryEnvelope struct {
	ViewCount       int    `json:"view_count"`
	SelectedCount   int    `json:"selected_count"`
	UseCount        int    `json:"use_count"`
	SuccessCount    int    `json:"success_count"`
	FailureCount    int    `json:"failure_count"`
	PatchCount      int    `json:"patch_count"`
	RestoreCount    int    `json:"restore_count"`
	LastViewedAt    string `json:"last_viewed_at,omitempty"`
	LastSelectedAt  string `json:"last_selected_at,omitempty"`
	LastUsedAt      string `json:"last_used_at,omitempty"`
	LastSucceededAt string `json:"last_succeeded_at,omitempty"`
	LastFailedAt    string `json:"last_failed_at,omitempty"`
}

type UsageEnvelope struct {
	InstructionEligible bool   `json:"instruction_eligible"`
	EvidenceOnly        bool   `json:"evidence_only"`
	Reason              string `json:"reason,omitempty"`
}

type Links struct {
	FileRefs []string `json:"file_refs,omitempty"`
}

type Record struct {
	ID         string            `json:"id"`
	Kind       Kind              `json:"kind"`
	SourceLane SourceLane        `json:"source_lane"`
	SourceID   string            `json:"source_id"`
	Summary    string            `json:"summary,omitempty"`
	Content    string            `json:"content,omitempty"`
	Score      float64           `json:"score,omitempty"`
	Temporal   TemporalEnvelope  `json:"temporal"`
	Provenance Provenance        `json:"provenance"`
	Trust      TrustEnvelope     `json:"trust"`
	Lifecycle  LifecycleEnvelope `json:"lifecycle"`
	Telemetry  TelemetryEnvelope `json:"telemetry"`
	Usage      UsageEnvelope     `json:"usage"`
	Links      Links             `json:"links,omitempty"`
	Tags       []string          `json:"tags,omitempty"`
}

type NamedEntryOptions struct {
	Score          float64
	Summary        string
	FileRefs       []string
	IncludeContent bool
}

func RecordFromNamedEntry(entry storage.NamedEntry, opts NamedEntryOptions) Record {
	kind := KindForNamedType(entry.Type)
	lifecycleState := LifecycleState(strings.TrimSpace(entry.LifecycleState))
	if !lifecycleState.IsValid() {
		lifecycleState = LifecycleStateActive
	}
	reviewStatus := ReviewStatus(strings.TrimSpace(entry.ReviewStatus))
	if reviewStatus == "" {
		reviewStatus = ReviewStatusUnreviewed
	}
	fileRefs := dedupeStrings(opts.FileRefs)
	summary := strings.TrimSpace(opts.Summary)
	if summary == "" {
		summary = strings.TrimSpace(entry.Summary)
	}
	record := Record{
		ID:         strings.TrimSpace(entry.ID),
		Kind:       kind,
		SourceLane: SourceLaneNamedMemory,
		SourceID:   strings.TrimSpace(entry.Name),
		Summary:    summary,
		Score:      opts.Score,
		Temporal: TemporalEnvelope{
			ObservedAt:      formatTime(entry.CreatedAt),
			IngestedAt:      formatTime(entry.CreatedAt),
			LastAccessedAt:  formatTime(entry.LastAccess),
			LastUsedAt:      formatTime(entry.LastUsedAt),
			LastValidatedAt: formatTime(entry.LastValidatedAt),
			LastPatchedAt:   formatTime(entry.LastPatchedAt),
			TemporalScope:   "unknown",
		},
		Provenance: Provenance{
			SourceType: "agent",
			SessionID:  strings.TrimSpace(entry.SessionID),
			FileRefs:   fileRefs,
			CreatedBy:  "foxctl.memory",
		},
		Trust: TrustEnvelope{
			SourceTrust: "agent_generated",
			Confidence:  0.55,
			Authority:   authorityForKind(kind),
			Tainted:     false,
		},
		Lifecycle: LifecycleEnvelope{
			State:        lifecycleState,
			Pinned:       entry.Pinned,
			ReviewStatus: reviewStatus,
			SupersededBy: strings.TrimSpace(entry.SupersededBy),
			ReviewNotes:  strings.TrimSpace(entry.ReviewNotes),
		},
		Telemetry: TelemetryEnvelope{
			ViewCount:       entry.AccessCount,
			SelectedCount:   entry.SelectedCount,
			UseCount:        entry.UseCount,
			SuccessCount:    entry.SuccessCount,
			FailureCount:    entry.FailureCount,
			PatchCount:      entry.PatchCount,
			RestoreCount:    entry.RestoreCount,
			LastViewedAt:    formatTime(entry.LastAccess),
			LastSelectedAt:  formatTime(entry.LastSelectedAt),
			LastUsedAt:      formatTime(entry.LastUsedAt),
			LastSucceededAt: formatTime(entry.LastSucceededAt),
			LastFailedAt:    formatTime(entry.LastFailedAt),
		},
		Usage: UsageEnvelope{
			InstructionEligible: false,
			EvidenceOnly:        true,
			Reason:              "named memory records are evidence unless promoted as validated policy or skill",
		},
		Links: Links{
			FileRefs: fileRefs,
		},
	}
	if record.ID == "" {
		record.ID = strings.TrimSpace(entry.Name)
	}
	if opts.IncludeContent && len(entry.Result) > 0 {
		record.Content = compactJSONOrString(entry.Result)
	}
	return record
}

func KindForNamedType(entryType string) Kind {
	switch strings.TrimSpace(entryType) {
	case "context", "working_context":
		return KindWorkingContext
	case "decision":
		return KindDecision
	case "pattern", "skill":
		return KindProceduralSkill
	case "preference", "user_pref", "policy":
		return KindPolicyRule
	case "edit", "session", "turn", "tool_result":
		return KindEpisodicTrace
	case "reflection":
		return KindReflection
	case "eval", "eval_result":
		return KindEvalResult
	case "curator_report":
		return KindEvalResult
	case "adapter_example":
		return KindAdapterExample
	default:
		return KindSemanticFact
	}
}

func ParseKinds(raw string) ([]Kind, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]Kind, 0, len(parts))
	for _, part := range parts {
		kind := Kind(strings.TrimSpace(part))
		if kind == "" {
			continue
		}
		if !kind.IsValid() {
			return nil, ErrInvalidKind{Kind: string(kind)}
		}
		out = append(out, kind)
	}
	return out, nil
}

func KindAllowed(kind Kind, allowed []Kind) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if kind == candidate {
			return true
		}
	}
	return false
}

func ParseLifecycleStates(raw string) ([]LifecycleState, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]LifecycleState, 0, len(parts))
	for _, part := range parts {
		state := LifecycleState(strings.TrimSpace(part))
		if state == "" {
			continue
		}
		if !state.IsValid() {
			return nil, ErrInvalidLifecycleState{State: string(state)}
		}
		out = append(out, state)
	}
	return out, nil
}

func LifecycleStateAllowed(state LifecycleState, allowed []LifecycleState) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if state == candidate {
			return true
		}
	}
	return false
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func authorityForKind(kind Kind) float64 {
	switch kind {
	case KindPolicyRule:
		return 0.45
	case KindProceduralSkill:
		return 0.4
	case KindDecision:
		return 0.35
	default:
		return 0.25
	}
}

func compactJSONOrString(raw []byte) string {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return strings.TrimSpace(string(raw))
	}
	out, err := json.Marshal(value)
	if err != nil {
		return strings.TrimSpace(string(raw))
	}
	return string(out)
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
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
