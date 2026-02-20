package sourceimport

import (
	"context"
	"time"

	"github.com/jkatigb/agentctl/internal/todosync"
	"github.com/jkatigb/agentctl/internal/v2/adapters/libsql/turns"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
)

// Provider identifies the source session format.
type Provider string

const (
	ProviderAuto   Provider = "auto"
	ProviderClaude Provider = "claude"
	ProviderCodex  Provider = "codex"
)

// ParsedSession is one normalized source conversation.
type ParsedSession struct {
	Provider      Provider
	SessionID     string
	SourcePath    string
	WorkspacePath string
	Turns         []run.TurnRecord
}

// ToolUseInput is one parsed tool invocation from source logs.
type ToolUseInput struct {
	CallID string
	Name   string
	Args   []byte
}

// ToolResultInput is one parsed tool result from source logs.
type ToolResultInput struct {
	CallID  string
	IsError bool
	Content string
}

// TodoStats is todo-derived metadata used during artifact synthesis.
type TodoStats struct {
	Total      int
	Pending    int
	InProgress int
	Completed  int
}

// ArtifactBuildOptions controls deterministic artifact derivation.
type ArtifactBuildOptions struct {
	IncludeEmbedding bool
	Embedder         Embedder
	Todos            []todosync.ClaudeTodo
	Now              func() time.Time
}

// ArtifactBuildResult contains derived artifacts and non-fatal warnings.
type ArtifactBuildResult struct {
	Artifacts []turns.Artifact
	Warnings  []string
	TodoStats TodoStats
}

// EmbeddingResult is one embedding vector with model metadata.
type EmbeddingResult struct {
	Vector []float32
	Model  string
}

// Embedder produces deterministic embeddings for source-derived artifacts.
type Embedder interface {
	Embed(ctx context.Context, text string) (EmbeddingResult, error)
}
