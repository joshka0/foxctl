package adapters

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/runtime/engine"
)

const RepoIndexAskPrompt = "You are a repo index assistant. Use repo_index_search to find multiple relevant nodes, repo_index_expand to map relationships, and repo_index_open for details. Edge types include structural edges (CONTAINS, IMPORTS, REFERS_TO, CALLS, IMPLEMENTS, EMBEDS, TESTS) and doc/comment edges (HAS_KEYWORD, HAS_OUTPUT_FIELD, TOUCHES_RESOURCE, EMITS_EVENT, DOC_RELATED, DOC_FLOW). When answering, list up to 5 relevant files or symbols with node IDs and file paths, plus a 1-2 sentence summary (use node summaries when available). If a tool call fails, retry with valid arguments; if unsure, say so."

type AskConfig struct {
	Store         *repoindex.Store
	Question      string
	Provider      string
	Model         string
	APIKey        string
	MaxIterations int
	Timeout       time.Duration
	SystemPrompt  string
}

type AskResult struct {
	Output engine.EngineOutput
}

func RunAsk(ctx context.Context, cfg AskConfig) (AskResult, error) {
	question := strings.TrimSpace(cfg.Question)
	if question == "" {
		return AskResult{}, errors.New("question is required")
	}
	if cfg.Store == nil {
		return AskResult{}, errors.New("repo index store is required")
	}
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 12
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	systemPrompt := strings.TrimSpace(cfg.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = RepoIndexAskPrompt
	}

	toolExecutor := engine.NewRepoIndexToolExecutor(cfg.Store)
	toolRunner := engine.NewToolRunner(toolExecutor, nil, engine.DefaultToolRunnerConfig())

	llmEngine, err := engine.NewLLMChatEngine(engine.LLMChatConfig{
		Provider:      cfg.Provider,
		APIKey:        cfg.APIKey,
		Model:         cfg.Model,
		MaxIterations: cfg.MaxIterations,
		Timeout:       cfg.Timeout,
		StatelessMode: true,
	})
	if err != nil {
		return AskResult{}, err
	}
	llmEngine.SetToolRunner(toolRunner)

	output, err := llmEngine.Run(ctx, engine.EngineInput{
		SystemPrompt: systemPrompt,
		Messages:     []engine.Message{engine.NewUserMessage(question)},
		Tools:        toolExecutor.List(),
	})
	if err != nil {
		return AskResult{}, err
	}
	return AskResult{Output: output}, nil
}
