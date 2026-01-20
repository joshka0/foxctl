// Package calibration provides user calibration profile management.
// It analyzes sessions at the context window level to extract communication
// style, tone, working patterns, and deeper user understanding.
package calibration

import "time"

// Profile represents the holistic user calibration profile.
// Stored as a single NamedEntry per workspace with type "calibration_profile".
type Profile struct {
	ProfileID string    `json:"profile_id"`
	Workspace string    `json:"workspace"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Surface preferences
	Communication CommunicationStyle `json:"communication"`
	Tone          ToneProfile        `json:"tone"`
	WorkingStyle  WorkingPattern     `json:"working_style"`

	// Deeper inference
	Cognition CognitionProfile `json:"cognition"`
	Expertise ExpertiseMap     `json:"expertise"`
	Trust     TrustProfile     `json:"trust"`

	// Evolution tracking
	Timeline []Snapshot `json:"timeline"`

	// Idempotent analysis tracking
	WindowsAnalyzed []AnalyzedWindow `json:"windows_analyzed"`
}

// AnalyzedWindow tracks each window processed for idempotency.
// Windows are identified by session_id:window_index:content_hash.
type AnalyzedWindow struct {
	SessionID   string    `json:"session_id"`
	WindowIndex int       `json:"window_index"`
	ContentHash string    `json:"content_hash"` // Hash of window content
	AnalyzedAt  time.Time `json:"analyzed_at"`
	SignalCount int       `json:"signal_count"` // Number of signals extracted
}

// CommunicationStyle captures how the user prefers to receive information.
type CommunicationStyle struct {
	Verbosity        VerbosityLevel `json:"verbosity"`         // concise|moderate|detailed
	TechnicalDepth   DepthLevel     `json:"technical_depth"`   // high-level|moderate|deep-dive
	ExplanationStyle string         `json:"explanation_style"` // examples-first|theory-first|mixed
	CodePreference   CodeStyle      `json:"code_preference"`   // full-code|snippets|pseudocode
	Confidence       float32        `json:"confidence"`        // 0.0-1.0
	Signals          []Signal       `json:"signals,omitempty"`
}

// ToneProfile captures interaction tone preferences.
type ToneProfile struct {
	Formality     FormalityLevel `json:"formality"`     // formal|casual|adaptive
	Assertiveness string         `json:"assertiveness"` // directive|collaborative|deferential
	Patience      PatienceLevel  `json:"patience"`      // patient|moderate|impatient
	Confidence    float32        `json:"confidence"`
	Signals       []Signal       `json:"signals,omitempty"`
}

// WorkingPattern captures how the user approaches problems.
type WorkingPattern struct {
	ProblemApproach   string            `json:"problem_approach"`   // iterative|big-picture|test-driven|exploratory
	FeedbackStyle     string            `json:"feedback_style"`     // direct|diplomatic|detailed-critique
	CollaborationMode string            `json:"collaboration_mode"` // pair-programming|review-based|autonomous
	Confidence        float32           `json:"confidence"`
	Patterns          []ObservedPattern `json:"patterns,omitempty"`
	Signals           []Signal          `json:"signals,omitempty"`
}

// CognitionProfile captures how the user thinks and processes information.
type CognitionProfile struct {
	MentalModel     string   `json:"mental_model"`      // visual|sequential|hierarchical|associative
	LearningStyle   string   `json:"learning_style"`    // examples-first|theory-first|hands-on|reading
	ProblemApproach string   `json:"problem_approach"`  // top-down|bottom-up|middle-out
	DecisionStyle   string   `json:"decision_style"`    // analytical|intuitive|collaborative
	Motivations     []string `json:"motivations"`       // speed|correctness|elegance|learning|pragmatism
	Confidence      float32  `json:"confidence"`
	Signals         []Signal `json:"signals,omitempty"`
}

// ExpertiseMap tracks known domains and learning areas.
type ExpertiseMap struct {
	StrongDomains []Domain `json:"strong_domains"` // Areas of expertise
	LearningAreas []Domain `json:"learning_areas"` // Areas user is learning
	KnowledgeGaps []Domain `json:"knowledge_gaps"` // Identified gaps
	Confidence    float32  `json:"confidence"`
}

// Domain represents an area of expertise or learning.
type Domain struct {
	Name        string    `json:"name"`         // e.g., "Go concurrency", "React hooks"
	Level       string    `json:"level"`        // expert|proficient|familiar|learning|novice
	LastSeen    time.Time `json:"last_seen"`
	SignalCount int       `json:"signal_count"`
}

// TrustProfile captures autonomy and control preferences.
type TrustProfile struct {
	AutonomyLevel    string   `json:"autonomy_level"`    // high|moderate|low (how much to delegate)
	VerificationNeed string   `json:"verification_need"` // always|sometimes|rarely
	PushbackPattern  string   `json:"pushback_pattern"`  // frequent|selective|rare
	CorrectionsStyle string   `json:"corrections_style"` // direct|diplomatic|questioning
	Confidence       float32  `json:"confidence"`
	Signals          []Signal `json:"signals,omitempty"`
}

// Snapshot captures the state of preferences at a point in time.
// This enables timeline tracking of preference evolution.
type Snapshot struct {
	Timestamp time.Time          `json:"timestamp"`
	Trigger   string             `json:"trigger"` // session_end|manual|scheduled
	SessionID string             `json:"session_id,omitempty"`
	Changes   []PreferenceChange `json:"changes,omitempty"`
}

// PreferenceChange records a single preference change.
type PreferenceChange struct {
	Dimension     string `json:"dimension"`      // e.g., communication.verbosity
	PreviousValue string `json:"previous_value"`
	NewValue      string `json:"new_value"`
	Reason        string `json:"reason,omitempty"` // Evidence for the change
}

// Signal represents evidence from a session for a preference.
type Signal struct {
	SessionID   string    `json:"session_id"`
	WindowIndex int       `json:"window_index"`
	At          time.Time `json:"at"`
	Quote       string    `json:"quote"`      // Exact user text
	Dimension   string    `json:"dimension"`  // e.g., "communication.verbosity"
	Direction   string    `json:"direction"`  // increase|decrease|confirm
	Confidence  float32   `json:"confidence"` // 0.0-1.0
}

// ObservedPattern represents a recurring pattern in user behavior.
type ObservedPattern struct {
	Pattern   string    `json:"pattern"`
	Frequency int       `json:"frequency"`
	LastSeen  time.Time `json:"last_seen"`
	Examples  []string  `json:"examples,omitempty"`
}

// VerbosityLevel indicates preferred response length.
type VerbosityLevel string

const (
	VerbosityConcise  VerbosityLevel = "concise"
	VerbosityModerate VerbosityLevel = "moderate"
	VerbosityDetailed VerbosityLevel = "detailed"
)

// DepthLevel indicates preferred technical depth.
type DepthLevel string

const (
	DepthHighLevel DepthLevel = "high-level"
	DepthModerate  DepthLevel = "moderate"
	DepthDeepDive  DepthLevel = "deep-dive"
)

// CodeStyle indicates preferred code output format.
type CodeStyle string

const (
	CodeFull       CodeStyle = "full-code"
	CodeSnippets   CodeStyle = "snippets"
	CodePseudocode CodeStyle = "pseudocode"
)

// FormalityLevel indicates interaction formality preference.
type FormalityLevel string

const (
	FormalityFormal   FormalityLevel = "formal"
	FormalityCasual   FormalityLevel = "casual"
	FormalityAdaptive FormalityLevel = "adaptive"
)

// PatienceLevel indicates patience in interactions.
type PatienceLevel string

const (
	PatiencePatient   PatienceLevel = "patient"
	PatienceModerate  PatienceLevel = "moderate"
	PatienceImpatient PatienceLevel = "impatient"
)

// NewProfile creates a new empty profile for a workspace.
func NewProfile(workspace string) *Profile {
	now := time.Now().UTC()
	return &Profile{
		Workspace: workspace,
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
		Communication: CommunicationStyle{
			Verbosity:        VerbosityModerate,
			TechnicalDepth:   DepthModerate,
			ExplanationStyle: "mixed",
			CodePreference:   CodeSnippets,
		},
		Tone: ToneProfile{
			Formality:     FormalityAdaptive,
			Assertiveness: "collaborative",
			Patience:      PatienceModerate,
		},
		WorkingStyle: WorkingPattern{
			ProblemApproach:   "iterative",
			FeedbackStyle:     "direct",
			CollaborationMode: "pair-programming",
		},
		Cognition: CognitionProfile{
			MentalModel:     "hierarchical",
			LearningStyle:   "mixed",
			ProblemApproach: "top-down",
			DecisionStyle:   "analytical",
			Motivations:     []string{"correctness", "pragmatism"},
		},
		Expertise: ExpertiseMap{
			StrongDomains: []Domain{},
			LearningAreas: []Domain{},
			KnowledgeGaps: []Domain{},
		},
		Trust: TrustProfile{
			AutonomyLevel:    "moderate",
			VerificationNeed: "sometimes",
			PushbackPattern:  "selective",
			CorrectionsStyle: "direct",
		},
		Timeline:        []Snapshot{},
		WindowsAnalyzed: []AnalyzedWindow{},
	}
}

// IsWindowAnalyzed checks if a window has already been analyzed with the same content.
func (p *Profile) IsWindowAnalyzed(sessionID string, windowIndex int, contentHash string) bool {
	for _, w := range p.WindowsAnalyzed {
		if w.SessionID == sessionID && w.WindowIndex == windowIndex && w.ContentHash == contentHash {
			return true
		}
	}
	return false
}

// MarkWindowAnalyzed records that a window has been analyzed.
func (p *Profile) MarkWindowAnalyzed(sessionID string, windowIndex int, contentHash string, signalCount int) {
	// Remove any existing entry for this session/window (in case of content change)
	filtered := make([]AnalyzedWindow, 0, len(p.WindowsAnalyzed))
	for _, w := range p.WindowsAnalyzed {
		if !(w.SessionID == sessionID && w.WindowIndex == windowIndex) {
			filtered = append(filtered, w)
		}
	}
	p.WindowsAnalyzed = filtered

	// Add new entry
	p.WindowsAnalyzed = append(p.WindowsAnalyzed, AnalyzedWindow{
		SessionID:   sessionID,
		WindowIndex: windowIndex,
		ContentHash: contentHash,
		AnalyzedAt:  time.Now().UTC(),
		SignalCount: signalCount,
	})
}

// AddSnapshot adds a timeline snapshot.
func (p *Profile) AddSnapshot(trigger, sessionID string, changes []PreferenceChange) {
	p.Timeline = append(p.Timeline, Snapshot{
		Timestamp: time.Now().UTC(),
		Trigger:   trigger,
		SessionID: sessionID,
		Changes:   changes,
	})

	// Cap timeline to last 100 snapshots
	const maxSnapshots = 100
	if len(p.Timeline) > maxSnapshots {
		p.Timeline = p.Timeline[len(p.Timeline)-maxSnapshots:]
	}
}
