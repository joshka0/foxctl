package longcoteval

import (
	"encoding/json"
	"time"
)

// ConditionID identifies one LongCoT evaluation arm. Keep these values stable;
// reports and experiment logs use them as durable join keys.
type ConditionID string

const (
	ConditionBaselineNoToolsOfficial  ConditionID = "baseline_no_tools_official_prompt"
	ConditionRLMNoToolsSingle         ConditionID = "rlm_no_tools_single"
	ConditionRLMReplNoSubcalls        ConditionID = "rlm_repl_no_subcalls"
	ConditionRLMReplRecursive         ConditionID = "rlm_repl_recursive"
	ConditionRLMLambdaReplSingle      ConditionID = "rlm_lambda_repl_single"
	ConditionRLMLambdaAdaptiveSingle  ConditionID = "rlm_lambda_adaptive_single"
	ConditionRLMLambdaThenBraidSingle ConditionID = "rlm_lambda_then_braid_single"
	ConditionRLMBraidSingle           ConditionID = "rlm_braid_single"
	ConditionRLMNoToolsStaged         ConditionID = "rlm_no_tools_staged"
	ConditionRLMNoModelToolsSingle    ConditionID = "rlm_no_model_tools_single"
	ConditionRLMNoModelToolsStaged    ConditionID = "rlm_no_model_tools_staged"
)

// ConditionKind groups conditions by execution family.
type ConditionKind string

const (
	ConditionKindBaseline ConditionKind = "baseline"
	ConditionKindRLM      ConditionKind = "rlm"
)

// AttemptStatus records the execution outcome before/around verifier import.
type AttemptStatus string

const (
	AttemptStatusOK         AttemptStatus = "ok"
	AttemptStatusError      AttemptStatus = "error"
	AttemptStatusCanceled   AttemptStatus = "canceled"
	AttemptStatusLeaked     AttemptStatus = "leaked"
	AttemptStatusUnverified AttemptStatus = "unverified"
)

const (
	VerifierStatusCorrect         = "correct"
	VerifierStatusIncorrect       = "incorrect"
	VerifierStatusFailed          = "failed"
	VerifierStatusWrongFormatting = "wrong_formatting"
)

// Question is the normalized metadata foxctl needs from the official LongCoT
// dataset shape and verifier bridge.
type Question struct {
	ID                    string                `json:"question_id"`
	Domain                string                `json:"domain,omitempty"`
	Split                 string                `json:"split,omitempty"` // legacy fixture compatibility
	Difficulty            string                `json:"difficulty,omitempty"`
	Template              string                `json:"template,omitempty"`
	PromptText            string                `json:"prompt,omitempty"`
	Answer                string                `json:"answer,omitempty"`
	Canary                string                `json:"canary,omitempty"`
	QuestionHash          string                `json:"question_hash,omitempty"`
	AllowOptionalSubcalls bool                  `json:"allow_optional_subcalls,omitempty"`
	RLMReview             bool                  `json:"rlm_review,omitempty"`
	RLMReviewRecursive    bool                  `json:"rlm_review_recursive,omitempty"`
	RequiredSubcallRules  []RequiredSubcallRule `json:"required_subcall_rules,omitempty"`
}

// RequiredSubcallRule declares a runtime-owned recursive-shape requirement.
type RequiredSubcallRule struct {
	Child            int `json:"child"`
	RequiredSubcalls int `json:"required_subcalls"`
}

// Condition describes one configured benchmark arm.
type Condition struct {
	ID                    ConditionID   `json:"condition_id"`
	Kind                  ConditionKind `json:"condition_kind"`
	PromptTemplateVersion string        `json:"prompt_template_version,omitempty"`
	RLMRouteProfile       string        `json:"rlm_route_profile,omitempty"`
	RLMPlanMode           string        `json:"rlm_plan_mode,omitempty"`
	RLMToolProfile        string        `json:"rlm_tool_profile,omitempty"`
	AllowedTools          []string      `json:"allowed_tools,omitempty"`
	MaxTokens             int           `json:"max_tokens,omitempty"`
	MaxDepth              int           `json:"max_depth,omitempty"`
	MaxIterations         int           `json:"max_iterations,omitempty"`
	MaxSubcalls           int           `json:"max_subcalls,omitempty"`
	TimeoutMS             int64         `json:"timeout_ms,omitempty"`
	Temperature           float64       `json:"temperature,omitempty"`
	Seed                  int64         `json:"seed,omitempty"`
}

// Usage records model accounting as reported or estimated for one attempt.
type Usage struct {
	InputTokens       int             `json:"input_tokens,omitempty"`
	OutputTokens      int             `json:"output_tokens,omitempty"`
	TotalTokens       int             `json:"total_tokens,omitempty"`
	ReasoningTokens   int             `json:"reasoning_tokens,omitempty"`
	CachedInputTokens int             `json:"cached_input_tokens,omitempty"`
	InputCostUSD      float64         `json:"input_cost_usd,omitempty"`
	OutputCostUSD     float64         `json:"output_cost_usd,omitempty"`
	TotalCostUSD      float64         `json:"total_cost_usd,omitempty"`
	RawProviderUsage  json.RawMessage `json:"raw_provider_usage,omitempty"`
}

// ToolEvent records one exposed tool call. The first slice only uses this shape
// for typed reporting; real runner integration can fill it later.
type ToolEvent struct {
	CallID                     string  `json:"call_id,omitempty"`
	Name                       string  `json:"name"`
	Status                     string  `json:"status,omitempty"`
	DurationMS                 int64   `json:"duration_ms,omitempty"`
	InputBytes                 int     `json:"input_bytes,omitempty"`
	OutputBytes                int     `json:"output_bytes,omitempty"`
	InputTokenEstimate         int     `json:"input_token_estimate,omitempty"`
	OutputTokenEstimate        int     `json:"output_token_estimate,omitempty"`
	RawOutputBytes             int     `json:"raw_output_bytes,omitempty"`
	ReducedOutputBytes         int     `json:"reduced_output_bytes,omitempty"`
	RawOutputTokenEstimate     int     `json:"raw_output_token_estimate,omitempty"`
	ReducedOutputTokenEstimate int     `json:"reduced_output_token_estimate,omitempty"`
	ReductionRatio             float64 `json:"reduction_ratio,omitempty"`
	CASDigest                  string  `json:"cas_digest,omitempty"`
	Error                      string  `json:"error,omitempty"`
}

// LeakageFlags make benchmark-contamination state explicit. These fields must
// be driven by exact config/tool data, not prompt keyword heuristics.
type LeakageFlags struct {
	FilesystemEnabled             bool     `json:"filesystem_enabled"`
	NetworkEnabled                bool     `json:"network_enabled"`
	RepoSearchEnabled             bool     `json:"repo_search_enabled"`
	MemoryEnabled                 bool     `json:"memory_enabled"`
	VaultEnabled                  bool     `json:"vault_enabled"`
	ArtifactEnabled               bool     `json:"artifact_enabled"`
	ShellEnabled                  bool     `json:"shell_enabled"`
	SubcallEnabled                bool     `json:"subcall_enabled"`
	SubcallAllowed                bool     `json:"subcall_allowed,omitempty"`
	VerifierAccessibleDuringSolve bool     `json:"verifier_accessible_during_solve"`
	DatasetAccessibleDuringSolve  bool     `json:"dataset_accessible_during_solve"`
	AnswerAccessibleDuringSolve   bool     `json:"answer_accessible_during_solve"`
	ForbiddenToolNames            []string `json:"forbidden_tool_names,omitempty"`
}

// Leaked reports whether any leakage flag invalidates primary benchmark use.
func (f LeakageFlags) Leaked() bool {
	return f.FilesystemEnabled || f.NetworkEnabled || f.RepoSearchEnabled || f.MemoryEnabled || f.VaultEnabled || f.ArtifactEnabled || f.ShellEnabled || (f.SubcallEnabled && !f.SubcallAllowed) || f.VerifierAccessibleDuringSolve || f.DatasetAccessibleDuringSolve || f.AnswerAccessibleDuringSolve || len(f.ForbiddenToolNames) > 0
}

// RLMAttemptMeta stores RLM-specific telemetry without forcing baseline arms to
// carry irrelevant fields.
type RLMAttemptMeta struct {
	RouteProfile         string         `json:"route_profile,omitempty"`
	PlanMode             string         `json:"plan_mode,omitempty"`
	ToolProfile          string         `json:"tool_profile,omitempty"`
	MaxDepth             int            `json:"max_depth,omitempty"`
	MaxIterations        int            `json:"max_iterations,omitempty"`
	MaxSubcalls          int            `json:"max_subcalls,omitempty"`
	Iterations           int            `json:"iterations,omitempty"`
	Subcalls             int            `json:"subcalls,omitempty"`
	EvidenceRefs         []string       `json:"evidence_refs,omitempty"`
	RetrievedPaths       []string       `json:"retrieved_paths,omitempty"`
	Metadata             map[string]any `json:"metadata,omitempty"`
	Phases               []RLMPhaseMeta `json:"phases,omitempty"`
	ParentInputTokens    int            `json:"parent_input_tokens,omitempty"`
	ParentOutputTokens   int            `json:"parent_output_tokens,omitempty"`
	ParentTotalTokens    int            `json:"parent_total_tokens,omitempty"`
	ParentIterationCount int            `json:"parent_iteration_count,omitempty"`
	ParentToolUsage      map[string]any `json:"parent_tool_usage,omitempty"`
}

type RLMPhaseMeta struct {
	Name               string   `json:"name"`
	AllowedTools       []string `json:"allowed_tools,omitempty"`
	RequiredTools      []string `json:"required_tools,omitempty"`
	ToolNames          []string `json:"tool_names,omitempty"`
	ParentInputTokens  int      `json:"parent_input_tokens,omitempty"`
	ParentOutputTokens int      `json:"parent_output_tokens,omitempty"`
	ParentTotalTokens  int      `json:"parent_total_tokens,omitempty"`
	AnswerExcerpt      string   `json:"answer_excerpt,omitempty"`
}

// Attempt is one condition's answer for one question.
type Attempt struct {
	RunID         string        `json:"run_id"`
	PairID        string        `json:"pair_id"`
	AttemptID     string        `json:"attempt_id"`
	QuestionID    string        `json:"question_id"`
	ConditionID   ConditionID   `json:"condition_id"`
	ConditionKind ConditionKind `json:"condition_kind"`
	Status        AttemptStatus `json:"status"`

	ResponseText  string `json:"response_text,omitempty"`
	ReasoningText string `json:"reasoning_text,omitempty"`

	Successful        bool   `json:"successful,omitempty"`
	Correct           bool   `json:"correct,omitempty"`
	VerifierStatus    string `json:"verifier_status,omitempty"`
	WrongFormatting   bool   `json:"wrong_formatting,omitempty"`
	VerificationError string `json:"verification_error,omitempty"`
	NormalizedAnswer  string `json:"normalized_answer,omitempty"`

	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Runner   string `json:"runner,omitempty"`

	Usage        Usage           `json:"usage"`
	ToolEvents   []ToolEvent     `json:"tool_events,omitempty"`
	LeakageFlags LeakageFlags    `json:"leakage_flags"`
	RLM          *RLMAttemptMeta `json:"rlm,omitempty"`

	TrajectoryID string `json:"trajectory_id,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	DurationMS   int64  `json:"duration_ms,omitempty"`
	Error        string `json:"error,omitempty"`
}

func (a Attempt) IsTerminal() bool {
	switch a.Status {
	case AttemptStatusOK, AttemptStatusError, AttemptStatusCanceled, AttemptStatusLeaked, AttemptStatusUnverified:
		return true
	default:
		return false
	}
}

func (a Attempt) IsCorrect() bool {
	return a.Correct || a.VerifierStatus == VerifierStatusCorrect
}

// SavedArtifact points to files emitted by an eval run.
type SavedArtifact struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

// RunResult is the benchmark-native output for one LongCoT foxctl run.
type RunResult struct {
	RunID       string          `json:"run_id"`
	GeneratedAt time.Time       `json:"generated_at"`
	Config      map[string]any  `json:"config,omitempty"`
	Questions   []Question      `json:"questions,omitempty"`
	Attempts    []Attempt       `json:"attempts,omitempty"`
	Summary     Summary         `json:"summary"`
	Artifacts   []SavedArtifact `json:"artifacts,omitempty"`
}
