package contextengine

import (
	"fmt"
	"time"
)

// RetrievalFeedbackKind classifies the kind of retrieval feedback.
type RetrievalFeedbackKind string

const (
	RetrievalFeedbackKindEvidenceUsed        RetrievalFeedbackKind = "evidence_used"
	RetrievalFeedbackKindAnswerAccepted     RetrievalFeedbackKind = "answer_accepted"
	RetrievalFeedbackKindAnswerCorrected    RetrievalFeedbackKind = "answer_corrected"
	RetrievalFeedbackKindRetrievalMissed    RetrievalFeedbackKind = "retrieval_missed"
	RetrievalFeedbackKindWrongFileRetrieved RetrievalFeedbackKind = "wrong_file_retrieved"
	RetrievalFeedbackKindStaleContextUsed   RetrievalFeedbackKind = "stale_context_used"
	RetrievalFeedbackKindGapCreated         RetrievalFeedbackKind = "gap_created"
)

// IsValid reports whether k is a known RetrievalFeedbackKind.
func (k RetrievalFeedbackKind) IsValid() bool {
	switch k {
	case RetrievalFeedbackKindEvidenceUsed, RetrievalFeedbackKindAnswerAccepted,
		RetrievalFeedbackKindAnswerCorrected, RetrievalFeedbackKindRetrievalMissed,
		RetrievalFeedbackKindWrongFileRetrieved, RetrievalFeedbackKindStaleContextUsed,
		RetrievalFeedbackKindGapCreated:
		return true
	default:
		return false
	}
}

// RetrievalEpisode records one retrieval operation.
type RetrievalEpisode struct {
	// ID is the unique episode identifier.
	ID string `json:"id"`
	// WorkspaceID is the owning workspace.
	WorkspaceID string `json:"workspace_id"`
	// Query is the retrieval query.
	Query string `json:"query"`
	// Lane is which retrieval lane was used.
	Lane EvidenceLane `json:"lane"`
	// PackID is the resulting evidence pack ID.
	PackID string `json:"pack_id,omitempty"`
	// DurationMs is how long the retrieval took.
	DurationMs int64 `json:"duration_ms,omitempty"`
	// TokensUsed is approximate tokens consumed.
	TokensUsed int `json:"tokens_used,omitempty"`
	// HitCount is how many hits were returned.
	HitCount int `json:"hit_count,omitempty"`
	// SubEpisodeIDs are child episode IDs for mixed lane retrievals.
	SubEpisodeIDs []string `json:"sub_episode_ids,omitempty"`
	// CreatedAt is when the episode was created.
	CreatedAt time.Time `json:"created_at"`
}

// Validate checks that the episode has required fields.
func (e RetrievalEpisode) Validate() error {
	if e.ID == "" {
		return fmt.Errorf("retrieval episode: missing id")
	}
	if e.WorkspaceID == "" {
		return fmt.Errorf("retrieval episode: missing workspace_id")
	}
	if e.Query == "" {
		return fmt.Errorf("retrieval episode: missing query")
	}
	if !e.Lane.IsValid() {
		return fmt.Errorf("retrieval episode: unknown lane %q", e.Lane)
	}
	for i, subID := range e.SubEpisodeIDs {
		if subID == "" {
			return fmt.Errorf("retrieval episode: sub_episode_ids[%d] is empty", i)
		}
	}
	return nil
}

// RetrievalFeedback records user or system feedback on retrieval results.
type RetrievalFeedback struct {
	// ID is the unique feedback identifier.
	ID string `json:"id"`
	// WorkspaceID is the owning workspace.
	WorkspaceID string `json:"workspace_id"`
	// EpisodeID is the retrieval episode this feedback responds to.
	EpisodeID string `json:"episode_id"`
	// Kind classifies the feedback.
	Kind RetrievalFeedbackKind `json:"kind"`
	// Query is the original query.
	Query string `json:"query"`
	// UsedRefs are the evidence refs that were actually used.
	UsedRefs []EvidenceRef `json:"used_refs,omitempty"`
	// GapStmt is an extracted gap statement, if kind is gap_created.
	GapStmt string `json:"gap_stmt,omitempty"`
	// CorrectionStmt is a user correction, if kind is answer_corrected.
	CorrectionStmt string `json:"correction_stmt,omitempty"`
	// CreatedAt is when the feedback was created.
	CreatedAt time.Time `json:"created_at"`
}

// Validate checks that the feedback has required fields.
func (f RetrievalFeedback) Validate() error {
	if f.ID == "" {
		return fmt.Errorf("retrieval feedback: missing id")
	}
	if f.WorkspaceID == "" {
		return fmt.Errorf("retrieval feedback: missing workspace_id")
	}
	if f.EpisodeID == "" {
		return fmt.Errorf("retrieval feedback: missing episode_id")
	}
	if !f.Kind.IsValid() {
		return fmt.Errorf("retrieval feedback: unknown kind %q", f.Kind)
	}
	if f.Query == "" {
		return fmt.Errorf("retrieval feedback: missing query")
	}
	for i, ref := range f.UsedRefs {
		if err := ValidateEvidenceRef(ref); err != nil {
			return fmt.Errorf("retrieval feedback: used_ref[%d]: %w", i, err)
		}
	}
	return nil
}
