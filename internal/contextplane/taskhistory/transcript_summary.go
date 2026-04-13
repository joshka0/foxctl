package taskhistory

import (
	"context"

	"github.com/jkatigb/agentctl/internal/transcriptpipeline"
)

type TranscriptSummaryWorker struct {
	Provider  string
	Model     string
	runConfig transcriptpipeline.WorkerConfig
}

type TranscriptSummaryRequest struct {
	InputKind     string
	PromptVersion string
	SystemPrompt  string
	ArtifactText  string
	MaxTokens     int
}

type TranscriptSummaryResponse struct {
	ModelID    string
	OutputText string
}

type TranscriptSummaryRunFunc func(context.Context, TranscriptSummaryWorker, TranscriptSummaryRequest) (TranscriptSummaryResponse, error)

func defaultTranscriptSummaryRun(ctx context.Context, worker TranscriptSummaryWorker, req TranscriptSummaryRequest) (TranscriptSummaryResponse, error) {
	result, err := transcriptpipeline.RunLLMTask(ctx, worker.runConfig, transcriptpipeline.Task{
		Stage:         transcriptpipeline.StageReview,
		InputKind:     req.InputKind,
		PromptVersion: req.PromptVersion,
		SystemPrompt:  req.SystemPrompt,
		ArtifactText:  req.ArtifactText,
		MaxTokens:     req.MaxTokens,
	})
	if err != nil {
		return TranscriptSummaryResponse{}, err
	}
	return TranscriptSummaryResponse{
		ModelID:    result.ModelID,
		OutputText: result.OutputText,
	}, nil
}
