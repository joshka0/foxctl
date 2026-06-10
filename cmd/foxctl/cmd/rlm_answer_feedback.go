package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/rlm"
)

type rlmAnswerFeedbackOptions struct {
	ID             string
	EpisodeID      string
	Kind           string
	Query          string
	UsedRefs       []string
	UseAnswerRefs  bool
	GapStmt        string
	CorrectionStmt string
	DryRun         bool
}

type rlmAnswerFeedbackRecord struct {
	Enabled bool   `json:"enabled"`
	Applied bool   `json:"applied"`
	DryRun  bool   `json:"dry_run"`
	Reason  string `json:"reason,omitempty"`
	// Source names where Refs came from. answer_used_evidence_refs is only an
	// answer-citation signal; lifecycle feedback must use explicit refs.
	Source   string                           `json:"source,omitempty"`
	Refs     []string                         `json:"refs,omitempty"`
	Feedback *contextengine.RetrievalFeedback `json:"feedback,omitempty"`
}

func recordRLMAnswerFeedback(ctx context.Context, store contextengine.RetrievalFeedbackEffectStore, workspaceRoot string, task rlm.Task, result rlm.Result, opts rlmAnswerFeedbackOptions) (*rlmAnswerFeedbackRecord, error) {
	if !rlmAnswerFeedbackRequested(opts) {
		return nil, nil
	}
	if strings.TrimSpace(opts.Kind) == "" {
		return nil, fmt.Errorf("--answer-feedback-kind is required when answer feedback flags are set")
	}
	if strings.TrimSpace(opts.EpisodeID) == "" {
		return nil, fmt.Errorf("--answer-feedback-episode-id is required when answer feedback flags are set")
	}

	kind := contextengine.RetrievalFeedbackKind(strings.TrimSpace(opts.Kind))
	if !kind.IsValid() {
		return nil, fmt.Errorf("--answer-feedback-kind must be a valid retrieval feedback kind")
	}
	refs, source, err := rlmAnswerFeedbackRefs(kind, result, opts)
	if err != nil {
		return nil, err
	}
	record := &rlmAnswerFeedbackRecord{
		Enabled: true,
		DryRun:  opts.DryRun,
		Source:  source,
		Refs:    append([]string(nil), refs...),
	}
	if len(refs) == 0 {
		record.Reason = "no_answer_feedback_refs"
		return record, nil
	}

	query := strings.TrimSpace(opts.Query)
	if query == "" {
		query = strings.TrimSpace(task.Prompt)
	}
	feedbackRecord, err := recordRetrievalFeedbackCLI(ctx, store, retrievalFeedbackCLIInput{
		WorkspacePath:  workspaceRoot,
		ID:             opts.ID,
		EpisodeID:      opts.EpisodeID,
		Kind:           string(kind),
		Query:          query,
		UsedRefs:       refs,
		UsedRefFlag:    "answer-feedback-used-ref",
		GapStmt:        opts.GapStmt,
		CorrectionStmt: opts.CorrectionStmt,
		DryRun:         opts.DryRun,
	})
	if err != nil {
		return nil, fmt.Errorf("record answer feedback: %w", err)
	}
	record.Applied = feedbackRecord.Applied
	record.Feedback = &feedbackRecord.Feedback
	return record, nil
}

func rlmAnswerFeedbackRefs(kind contextengine.RetrievalFeedbackKind, result rlm.Result, opts rlmAnswerFeedbackOptions) ([]string, string, error) {
	explicitRefs := uniqueTrimmedStrings(opts.UsedRefs)
	if len(explicitRefs) > 0 {
		return explicitRefs, "answer_feedback_used_ref", nil
	}
	if !opts.UseAnswerRefs {
		return nil, "", fmt.Errorf("--answer-feedback-used-ref or --answer-feedback-use-answer-refs is required when answer feedback flags are set")
	}
	if rlmAnswerFeedbackNeedsExplicitUsedRefs(kind) {
		return nil, "", fmt.Errorf("--answer-feedback-used-ref is required for lifecycle-impacting answer feedback kind %q", kind)
	}
	return rlmMetadataStringSlice(result.Metadata["answer_used_evidence_refs"]), "answer_used_evidence_refs", nil
}

func rlmAnswerFeedbackNeedsExplicitUsedRefs(kind contextengine.RetrievalFeedbackKind) bool {
	return contextengine.RetrievalFeedbackKindHasLifecycleImpact(kind)
}

func rlmAnswerFeedbackRequested(opts rlmAnswerFeedbackOptions) bool {
	return strings.TrimSpace(opts.ID) != "" ||
		strings.TrimSpace(opts.EpisodeID) != "" ||
		strings.TrimSpace(opts.Kind) != "" ||
		strings.TrimSpace(opts.Query) != "" ||
		len(opts.UsedRefs) > 0 ||
		opts.UseAnswerRefs ||
		strings.TrimSpace(opts.GapStmt) != "" ||
		strings.TrimSpace(opts.CorrectionStmt) != "" ||
		opts.DryRun
}

func rlmMetadataStringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		return uniqueTrimmedStrings(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				continue
			}
			out = append(out, s)
		}
		return uniqueTrimmedStrings(out)
	default:
		return nil
	}
}

func uniqueTrimmedStrings(values []string) []string {
	seen := map[string]struct{}{}
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
