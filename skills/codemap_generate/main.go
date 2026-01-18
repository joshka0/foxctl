// Package main implements the codemap/generate skill.
// It generates semantic codemaps using a dspy-go agent.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/XiaoConstantine/dspy-go/pkg/logging"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/codemap"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/graph"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/rs/zerolog"
)

const command = "codemap/generate"

func init() {
	// Configure dspy-go to log to stderr instead of stdout
	// This prevents log output from corrupting the JSON envelope
	logger := logging.NewLogger(logging.Config{
		Severity: logging.INFO,
		Outputs:  []logging.Output{logging.NewConsoleOutput(true)}, // true = stderr
	})
	logging.SetLogger(logger)
}

type input struct {
	Query     string `json:"query"`
	Workspace string `json:"workspace"`
	Depth     int    `json:"depth"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
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
		return skillerr.WrapRuntime("generate codemap", err)
	}

	// Store codemap with embedding for semantic search
	if err := storeCodemapWithEmbedding(ctx, rc.Logger, &cfg, result, workspace); err != nil {
		// Log error but don't fail - codemap was generated successfully
		rc.Logger.Warn().Err(err).Msg("codemap: failed to store embedding")
	}

	return skillout.Emit(rc, command, result)
}

// storeCodemapWithEmbedding saves the codemap to memory store with chunked embeddings
// for semantic search.
func storeCodemapWithEmbedding(ctx context.Context, logger zerolog.Logger, cfg *config.Config, cm *codemap.Codemap, workspace string) error {
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")
	if voyageKey == "" && geminiKey == "" {
		return skillerr.Auth(
			"no embedding API key set; embedding skipped",
			skillerr.WithHint("Set VOYAGE_API_KEY or GEMINI_API_KEY to enable codemap embeddings."),
		)
	}

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
