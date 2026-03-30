package transcriptpipeline

import (
	"strings"
	"time"
)

const (
	DefaultWorkerProvider                  = "lmstudio"
	DefaultWorkerModel                     = "nvidia/nemotron-3-nano-4b"
	DefaultDoctrineBridgeModel             = "liquid/lfm2.5-1.2b"
	DefaultDoctrineDistillModel            = "liquid/lfm2.5-1.2b"
	DefaultReferencePromptVersion          = "reference_blob_summary_v1"
	DefaultToolOutputPromptVersion         = "tool_output_summary_v1"
	DefaultObjectivePromptVersion          = "session_objective_v5"
	DefaultDoctrineBridgePromptVersion     = "doctrine_bridge_v5"
	DefaultDoctrineDistillPromptVersion    = "doctrine_distill_v3"
	DefaultObjectiveAlignmentPromptVersion = "objective_alignment_v4"
	DefaultFrameSynopsisPromptVersion      = "frame_synopsis_v1"
	DefaultGroupToplinePromptVersion       = "group_topline_summary_v2"
	DefaultGroupClaimsPromptVersion        = "group_topline_claims_v3"
	DefaultClassifiedClaimsPromptVersion   = "classified_claims_v6"
	DefaultClaimReviewPromptVersion        = "classified_claim_review_v5"
	DefaultMaxContextTokens                = 100000
	DefaultToolOutputSummaryMinTokens      = 1200
	DefaultSynopsisWindowSize              = 5
)

// LocalModelRuntime captures the repeated local-model pipeline settings used by transcript commands.
type LocalModelRuntime struct {
	Mode                            string
	Provider                        string
	APIKey                          string
	AuthMode                        string
	AuthHeader                      string
	AuthPrefix                      string
	Model                           string
	DoctrineBridgeModel             string
	DoctrineDistillModel            string
	BaseURL                         string
	Timeout                         time.Duration
	MaxContextTokens                int
	ToolOutputSummaryMinTokens      int
	SynopsisWindowSize              int
	ReferencePromptVersion          string
	ToolOutputPromptVersion         string
	ObjectivePromptVersion          string
	DoctrineBridgePromptVersion     string
	DoctrineDistillPromptVersion    string
	ObjectiveAlignmentPromptVersion string
	FrameSynopsisPromptVersion      string
	GroupToplinePromptVersion       string
	GroupClaimsPromptVersion        string
	ClassificationPromptVersion     string
	ClaimReviewPromptVersion        string
}

// NewLocalModelRuntime returns a local-model runtime with transcript-pipeline defaults applied.
func NewLocalModelRuntime(mode, model, baseURL string, timeout time.Duration) LocalModelRuntime {
	return LocalModelRuntime{
		Mode:                            strings.TrimSpace(mode),
		Provider:                        DefaultWorkerProvider,
		Model:                           firstNonEmpty(strings.TrimSpace(model), DefaultWorkerModel),
		DoctrineBridgeModel:             DefaultDoctrineBridgeModel,
		DoctrineDistillModel:            DefaultDoctrineDistillModel,
		BaseURL:                         strings.TrimSpace(baseURL),
		Timeout:                         timeout,
		MaxContextTokens:                DefaultMaxContextTokens,
		ToolOutputSummaryMinTokens:      DefaultToolOutputSummaryMinTokens,
		SynopsisWindowSize:              DefaultSynopsisWindowSize,
		ReferencePromptVersion:          DefaultReferencePromptVersion,
		ToolOutputPromptVersion:         DefaultToolOutputPromptVersion,
		ObjectivePromptVersion:          DefaultObjectivePromptVersion,
		DoctrineBridgePromptVersion:     DefaultDoctrineBridgePromptVersion,
		DoctrineDistillPromptVersion:    DefaultDoctrineDistillPromptVersion,
		ObjectiveAlignmentPromptVersion: DefaultObjectiveAlignmentPromptVersion,
		FrameSynopsisPromptVersion:      DefaultFrameSynopsisPromptVersion,
		GroupToplinePromptVersion:       DefaultGroupToplinePromptVersion,
		GroupClaimsPromptVersion:        DefaultGroupClaimsPromptVersion,
		ClassificationPromptVersion:     DefaultClassifiedClaimsPromptVersion,
		ClaimReviewPromptVersion:        DefaultClaimReviewPromptVersion,
	}
}

// PreprocessOptions converts runtime settings into artifact-preprocessing settings.
func (r LocalModelRuntime) PreprocessOptions() PreprocessOptions {
	return PreprocessOptions{
		Mode:                       strings.TrimSpace(r.Mode),
		Model:                      firstNonEmpty(strings.TrimSpace(r.Model), DefaultWorkerModel),
		Provider:                   firstNonEmpty(strings.TrimSpace(r.Provider), DefaultWorkerProvider),
		BaseURL:                    strings.TrimSpace(r.BaseURL),
		ReferencePromptVersion:     firstNonEmpty(strings.TrimSpace(r.ReferencePromptVersion), DefaultReferencePromptVersion),
		ToolOutputPromptVersion:    firstNonEmpty(strings.TrimSpace(r.ToolOutputPromptVersion), DefaultToolOutputPromptVersion),
		Timeout:                    r.Timeout,
		MaxContextTokens:           positiveIntOr(r.MaxContextTokens, DefaultMaxContextTokens),
		ToolOutputSummaryMinTokens: positiveIntOr(r.ToolOutputSummaryMinTokens, DefaultToolOutputSummaryMinTokens),
	}
}

// WorkerConfig converts runtime settings into bounded small-model worker config.
func (r LocalModelRuntime) WorkerConfig() WorkerConfig {
	return WorkerConfig{
		Provider:         firstNonEmpty(strings.TrimSpace(r.Provider), DefaultWorkerProvider),
		APIKey:           strings.TrimSpace(r.APIKey),
		AuthMode:         strings.TrimSpace(r.AuthMode),
		AuthHeader:       strings.TrimSpace(r.AuthHeader),
		AuthPrefix:       r.AuthPrefix,
		Model:            firstNonEmpty(strings.TrimSpace(r.Model), DefaultWorkerModel),
		BaseURL:          strings.TrimSpace(r.BaseURL),
		MaxContextTokens: positiveIntOr(r.MaxContextTokens, DefaultMaxContextTokens),
		Timeout:          r.Timeout,
	}
}

func (r LocalModelRuntime) WorkerConfigForStage(stage Stage) WorkerConfig {
	cfg := r.WorkerConfig()
	switch stage {
	case StageBridge:
		cfg.Model = firstNonEmpty(strings.TrimSpace(r.DoctrineBridgeModel), DefaultDoctrineBridgeModel)
	case StageDistill:
		cfg.Model = firstNonEmpty(strings.TrimSpace(r.DoctrineDistillModel), DefaultDoctrineDistillModel)
	}
	return cfg
}

// TimeoutSeconds returns a stable helper for APIs that still accept integer timeout seconds.
func (r LocalModelRuntime) TimeoutSeconds() int {
	if r.Timeout <= 0 {
		return 45
	}
	return int(r.Timeout.Seconds())
}

func positiveIntOr(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
