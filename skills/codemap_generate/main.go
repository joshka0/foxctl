// Package main implements the codemap/generate skill.
// It generates semantic codemaps using the LLM chat codemap agent.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/intelligence/codemap"
	"github.com/joshka0/foxctl/internal/platform/config"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/storage/graph"
	"github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/rs/zerolog"
)

const command = "codemap/generate"

// input is the expected JSON input for codemap/generate operations.
type input struct {
	Query     string `json:"query"`
	Workspace string `json:"workspace"`
	Depth     int    `json:"depth"`
}

// main is the skill entry point for codemap/generate.
func main() {
	skillmain.Main(command, skillmain.Chain(
		run,
		skillmain.WithRecover[input](),
	))
}

// run orchestrates codemap generation using the codemap agent with optional embedding storage.
//
// Index:
//
//	Purpose: Generate semantic codemaps using AI agent with natural language queries
//	Flow: validate input → resolve workspace → open stores → create agent → generate codemap → store with embeddings
//	SideEffects: database operations; graph store queries; AI agent execution; embedding generation; memory storage
//	FailureModes: invalid queries, workspace validation errors, agent creation failures, generation errors, storage errors
//	Observability: emits generated codemap with traces, files, and metadata
//	Related: storeCodemapWithEmbedding, buildCodemapSummary
//	Keywords: codemap/generate, agent, embeddings, semantic, traces
//
// [[domain:semantic-codemap-generation]]
// [[protocol:codemap-embedding-storage]]
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	start := time.Now()
	if in.Query == "" {
		return skillerr.Arg("query is required", skillerr.WithHint("Provide a natural language query to generate a codemap."))
	}

	cfg := rc.Config

	// Resolve workspace
	workspace := in.Workspace
	if workspace == "" {
		workspace = rc.PathValidator.Workspace()
	} else {
		validated, err := skillmain.ValidatePath(
			rc,
			workspace,
			skillmain.WithPathMessage("workspace validation failed"),
			skillmain.WithPathHint("Provide a workspace within the allowed roots."),
		)
		if err != nil {
			return err
		}
		workspace = validated
	}

	// Default depth
	depth := in.Depth
	if depth < 1 {
		depth = 2
	}
	if depth > 5 {
		depth = 5
	}

	// Open graph store
	var graphStore graph.Store
	gs, err := graph.Open(ctx, cfg.Storage.Root)
	if err == nil {
		graphStore = gs
		defer errs.Ignore(gs.Close(), "graph store close")
	}

	// Create codemap agent
	agentOpts := []codemap.AgentOption{
		codemap.WithWorkspace(workspace),
		codemap.WithSkillResolver(skill.NewResolver()),
	}
	if graphStore != nil {
		agentOpts = append(agentOpts, codemap.WithGraphStore(graphStore))
	}

	agent, err := codemap.NewAgent(agentOpts...)
	if err != nil {
		return skillerr.WrapRuntime("create agent", err)
	}

	// Generate codemap
	result, err := agent.Generate(ctx, codemap.GenerateOptions{
		Query:     in.Query,
		Workspace: workspace,
		Depth:     depth,
	})
	if err != nil {
		emitCodemapError(ctx, rc, in.Query, workspace, depth, err, time.Since(start))
		return skillerr.WrapRuntime("generate codemap", err)
	}

	// Store codemap with embedding for semantic search
	if err := storeCodemapWithEmbedding(ctx, rc.Logger, &cfg, result, workspace); err != nil {
		// Log error but don't fail - codemap was generated successfully
		rc.Logger.Warn().Err(err).Msg("codemap: failed to store embedding")
	}

	return skillout.Emit(rc, command, result)
}

func emitCodemapError(ctx context.Context, rc *skillmain.RunContext, query, workspace string, depth int, err error, duration time.Duration) {
	if rc == nil {
		return
	}
	rc.Logger.Warn().Msg("codemap: emitting error event")
	if os.Getenv(observability.EnvObsDir) == "" && rc.Config.Paths.Observability != "" {
		observability.SetObsDirForTesting(rc.Config.Paths.Observability)
		_ = os.Setenv(observability.EnvObsDir, rc.Config.Paths.Observability)
	}
	builder := observability.NewEvent("codemap.generate").
		WithComponent("skill").
		WithCommand(command).
		WithSubtype("generate").
		WithWorkspace(workspace).
		WithData("query_hash", observability.HashQuestion(query)).
		WithData("depth", depth)

	if rc.ShouldStoreCAS() && err != nil {
		buf := bytes.NewBufferString(err.Error())
		artifact, _, persistErr := skillout.PersistBufferWithHint(
			ctx,
			rc,
			buf,
			"text/plain",
			"codemap_generate_error",
			skillout.DefaultCASHintLines,
		)
		if persistErr == nil {
			builder = builder.WithStderrArtifact(artifact.Digest)
		} else {
			rc.Logger.Warn().Err(persistErr).Msg("codemap: failed to persist error details")
		}
	}

	if emitErr := observability.EmitSync(ctx, builder.Error(err, duration)); emitErr != nil {
		rc.Logger.Warn().Err(emitErr).Msg("codemap: failed to emit error event")
	}
}

// storeCodemapWithEmbedding saves the codemap to memory store with chunked embeddings for semantic search.
func storeCodemapWithEmbedding(ctx context.Context, logger zerolog.Logger, cfg *config.Config, cm *codemap.Codemap, workspace string) error {
	// Open memory store
	memStore, err := memory.OpenWithConfig(ctx, *cfg)
	if err != nil {
		return skillerr.WrapIO("open memory store", err)
	}
	defer func() { errs.Ignore(memStore.Close(), "close memory store") }()

	// Serialize codemap result
	resultJSON, err := json.Marshal(cm)
	if err != nil {
		return skillerr.WrapRuntime("marshal codemap", err)
	}

	// Create named entry with type="codemap"
	entry := memory.NamedEntry{
		Name:      fmt.Sprintf("codemap://%s", cm.ID),
		Type:      "codemap",
		Summary:   buildCodemapSummary(cm.Title, cm.Description, cm.ID),
		Result:    resultJSON,
		Workspace: workspace,
	}

	// Save entry first
	saved, err := memStore.Save(ctx, entry)
	if err != nil {
		return skillerr.WrapIO("save codemap entry", err)
	}

	// Store chunked embeddings for codemap search
	plan := codemap.BuildEmbeddingPlan(cm)
	if _, err := codemap.StoreEmbeddingPlan(ctx, memStore, *cfg, workspace, saved.Name, plan); err != nil {
		return skillerr.WrapIO("store codemap embeddings", err)
	}

	logger.Info().Str("codemap_entry", saved.Name).Msg("Stored codemap with embeddings")
	return nil
}

// buildCodemapSummary creates a summary string from title, description, and fallback.
func buildCodemapSummary(title, description, fallback string) string {
	if title == "" && description == "" {
		return fallback
	}
	if title == "" {
		return description
	}
	if description == "" {
		return title
	}
	return fmt.Sprintf("%s - %s", title, description)
}
