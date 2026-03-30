package transcriptpipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	actormemory "github.com/jkatigb/agentctl/internal/actor/memory"
	"github.com/jkatigb/agentctl/internal/engine"
)

// Stage identifies the transcript pipeline stage being executed.
type Stage string

const (
	StageCompress Stage = "compress"
	StageDerive   Stage = "derive"
	StageBridge   Stage = "bridge"
	StageDistill  Stage = "distill"
	StagePolish   Stage = "polish"
	StageClassify Stage = "classify"
	StageReview   Stage = "review"
	StageAlign    Stage = "align"
)

// WorkerConfig configures a small-model worker for one pipeline task.
type WorkerConfig struct {
	Provider         string
	APIKey           string
	AuthMode         string
	AuthHeader       string
	AuthPrefix       string
	Model            string
	BaseURL          string
	MaxContextTokens int
	Timeout          time.Duration
}

// Task describes one bounded transcript-pipeline transform.
type Task struct {
	Stage         Stage
	InputKind     string
	PromptVersion string
	SystemPrompt  string
	ArtifactText  string
	MaxTokens     int
}

// Result captures one completed worker transform.
type Result struct {
	Stage         Stage
	InputKind     string
	PromptVersion string
	ModelID       string
	OutputText    string
}

// RunLLMTask executes one bounded small-model transcript-pipeline task.
func RunLLMTask(ctx context.Context, cfg WorkerConfig, task Task) (Result, error) {
	if strings.TrimSpace(task.SystemPrompt) == "" {
		return Result{}, fmt.Errorf("transcriptpipeline: system prompt is required")
	}
	if strings.TrimSpace(task.ArtifactText) == "" {
		return Result{}, fmt.Errorf("transcriptpipeline: artifact text is required")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	maxContextTokens := cfg.MaxContextTokens
	if maxContextTokens <= 0 {
		maxContextTokens = 100000
	}
	maxTokens := task.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 180
	}

	engineCfg := engine.LLMChatConfig{
		Provider:         strings.TrimSpace(cfg.Provider),
		APIKey:           strings.TrimSpace(cfg.APIKey),
		BaseURL:          strings.TrimSpace(cfg.BaseURL),
		AuthMode:         strings.TrimSpace(cfg.AuthMode),
		AuthHeader:       strings.TrimSpace(cfg.AuthHeader),
		AuthPrefix:       cfg.AuthPrefix,
		Model:            strings.TrimSpace(cfg.Model),
		MaxIterations:    1,
		MaxContextTokens: maxContextTokens,
		Timeout:          timeout,
		Temperature:      0.0,
		MaxTokens:        maxTokens,
	}
	llm, err := engine.NewLLMChatEngine(engineCfg)
	if err != nil {
		return Result{}, fmt.Errorf("transcriptpipeline: init worker engine: %w", err)
	}

	boundedText := BoundArtifactText(task.ArtifactText, maxContextTokens)
	input := engine.EngineInput{
		SystemPrompt: task.SystemPrompt,
		Messages: []engine.Message{
			engine.NewUserMessage("Artifact content:\n\n" + boundedText),
		},
		MaxTokens:   maxTokens,
		Temperature: 0.0,
	}
	output, err := llm.Run(ctx, input)
	if err != nil {
		return Result{}, fmt.Errorf("transcriptpipeline: run worker task: %w", err)
	}
	if output.StopReason == engine.StopReasonError {
		return Result{}, fmt.Errorf("transcriptpipeline: worker stop error: %s", output.Error)
	}
	out := strings.TrimSpace(output.AssistantText)
	if out == "" {
		return Result{}, fmt.Errorf("transcriptpipeline: empty worker output")
	}
	return Result{
		Stage:         task.Stage,
		InputKind:     strings.TrimSpace(task.InputKind),
		PromptVersion: strings.TrimSpace(task.PromptVersion),
		ModelID:       strings.TrimSpace(cfg.Provider) + ":" + strings.TrimSpace(cfg.Model),
		OutputText:    out,
	}, nil
}

type llmTaskRunner func(ctx context.Context, cfg WorkerConfig, task Task) (Result, error)

// RunLLMTaskWithFallbackModel retries a stage task on the main worker config when the
// stage-specific config fails or returns output rejected by the accept function.
func RunLLMTaskWithFallbackModel(ctx context.Context, primary WorkerConfig, fallback WorkerConfig, task Task, accept func(Result) bool, run llmTaskRunner) (Result, bool) {
	if run == nil {
		run = RunLLMTask
	}
	try := []WorkerConfig{primary}
	if !sameWorkerTarget(primary, fallback) {
		try = append(try, fallback)
	}
	for _, cfg := range try {
		result, err := run(ctx, cfg, task)
		if err != nil {
			continue
		}
		if accept == nil || accept(result) {
			return result, true
		}
	}
	return Result{}, false
}

// BoundArtifactText clips artifact text to fit safely inside one worker call.
func BoundArtifactText(text string, maxContextTokens int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if maxContextTokens <= 0 {
		maxContextTokens = 100000
	}
	boundedText, _ := actormemory.TruncateToFitWithMargin(text, maxContextTokens, 1.0, false)
	if strings.TrimSpace(boundedText) == "" {
		return truncateInline(text, 4000)
	}
	return boundedText
}

func truncateInline(text string, max int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if max <= 0 || len(text) <= max {
		return text
	}
	if max <= 1 {
		return text[:max]
	}
	return text[:max-1] + "…"
}

func sameWorkerTarget(a, b WorkerConfig) bool {
	return strings.EqualFold(strings.TrimSpace(a.Provider), strings.TrimSpace(b.Provider)) &&
		strings.EqualFold(strings.TrimSpace(a.Model), strings.TrimSpace(b.Model)) &&
		strings.TrimSpace(a.BaseURL) == strings.TrimSpace(b.BaseURL)
}
