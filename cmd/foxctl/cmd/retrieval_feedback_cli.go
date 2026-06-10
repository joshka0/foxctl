package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
)

type retrievalFeedbackCLIInput struct {
	WorkspacePath  string
	ID             string
	EpisodeID      string
	Kind           string
	Query          string
	UsedRefs       []string
	UsedRefFlag    string
	GapStmt        string
	CorrectionStmt string
	DryRun         bool
}

type retrievalFeedbackCLIRecord struct {
	WorkspacePath string                          `json:"workspace_path"`
	WorkspaceID   string                          `json:"workspace_id"`
	Feedback      contextengine.RetrievalFeedback `json:"feedback"`
	Applied       bool                            `json:"applied"`
	DryRun        bool                            `json:"dry_run"`
}

func recordRetrievalFeedbackCLI(ctx context.Context, store contextengine.RetrievalFeedbackEffectStore, input retrievalFeedbackCLIInput) (retrievalFeedbackCLIRecord, error) {
	record, err := buildRetrievalFeedbackCLI(input)
	if err != nil {
		return retrievalFeedbackCLIRecord{}, err
	}
	if input.DryRun {
		return record, nil
	}
	if store == nil {
		return retrievalFeedbackCLIRecord{}, fmt.Errorf("contextengine store is unavailable")
	}
	recorded, err := contextengine.RecordRetrievalFeedbackWithEffects(ctx, store, record.Feedback)
	if err != nil {
		return retrievalFeedbackCLIRecord{}, err
	}
	record.Feedback = recorded
	record.Applied = true
	return record, nil
}

func buildRetrievalFeedbackCLI(input retrievalFeedbackCLIInput) (retrievalFeedbackCLIRecord, error) {
	if strings.TrimSpace(input.EpisodeID) == "" {
		return retrievalFeedbackCLIRecord{}, fmt.Errorf("--episode-id is required")
	}
	if strings.TrimSpace(input.Query) == "" {
		return retrievalFeedbackCLIRecord{}, fmt.Errorf("--query is required")
	}
	feedbackKind := contextengine.RetrievalFeedbackKind(strings.TrimSpace(input.Kind))
	if !feedbackKind.IsValid() {
		return retrievalFeedbackCLIRecord{}, fmt.Errorf("--kind must be a valid retrieval feedback kind")
	}

	usedRefFlag := strings.TrimSpace(input.UsedRefFlag)
	if usedRefFlag == "" {
		usedRefFlag = "used-ref"
	}
	parsedUsedRefs, err := parseEvidenceRefsStrict(usedRefFlag, input.UsedRefs)
	if err != nil {
		return retrievalFeedbackCLIRecord{}, err
	}
	parsedUsedRefs = sortEvidenceRefsByIdentity(parsedUsedRefs)

	target := resolveContextWorkspace(input.WorkspacePath)
	workspaceID := ws.CanonicalID(target)
	feedback := contextengine.RetrievalFeedback{
		ID:             strings.TrimSpace(input.ID),
		WorkspaceID:    workspaceID,
		EpisodeID:      strings.TrimSpace(input.EpisodeID),
		Kind:           feedbackKind,
		Query:          strings.TrimSpace(input.Query),
		UsedRefs:       parsedUsedRefs,
		GapStmt:        strings.TrimSpace(input.GapStmt),
		CorrectionStmt: strings.TrimSpace(input.CorrectionStmt),
	}
	if feedback.ID == "" {
		feedback.ID = deterministicRetrievalFeedbackID(feedback)
	}
	if err := feedback.Validate(); err != nil {
		return retrievalFeedbackCLIRecord{}, err
	}
	return retrievalFeedbackCLIRecord{
		WorkspacePath: target,
		WorkspaceID:   workspaceID,
		Feedback:      feedback,
		DryRun:        input.DryRun,
	}, nil
}
