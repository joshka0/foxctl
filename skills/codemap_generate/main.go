// Package main implements the codemap/generate skill.
// It generates semantic codemaps using a dspy-go agent.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/XiaoConstantine/dspy-go/pkg/logging"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/codemap"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/graph"
	"github.com/jkatigb/agentctl/internal/storage/memory"
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
		return fmt.Errorf("query is required")
	}

	cfg := rc.Config

	// Resolve workspace
	workspace := in.Workspace
	if workspace == "" {
		workspace = rc.PathValidator.Workspace()
	} else {
		validated, err := rc.PathValidator.ValidatePath(workspace)
		if err != nil {
			return fmt.Errorf("workspace validation failed: %w", err)
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
		return fmt.Errorf("create agent: %w", err)
	}

	// Generate codemap
	result, err := agent.Generate(ctx, codemap.GenerateOptions{
		Query:     in.Query,
		Workspace: workspace,
		Depth:     depth,
	})
	if err != nil {
		return fmt.Errorf("generate codemap: %w", err)
	}

	// Store codemap with embedding for semantic search
	if err := storeCodemapWithEmbedding(ctx, &cfg, result, workspace); err != nil {
		// Log error but don't fail - codemap was generated successfully
		fmt.Fprintf(os.Stderr, "warning: failed to store codemap embedding: %v\n", err)
	}

	return skillout.Emit(rc, command, result)
}

// storeCodemapWithEmbedding saves the codemap to memory store with an embedding
// for semantic search. Uses voyage-3.5 model via ScopeModelRecommendation.
func storeCodemapWithEmbedding(ctx context.Context, cfg *config.Config, cm *codemap.Codemap, workspace string) error {
	// Check for Voyage API key
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	if voyageKey == "" {
		return fmt.Errorf("VOYAGE_API_KEY not set; embedding skipped")
	}

	// Create embedding provider using scope-based model recommendation
	model, _ := semantic.ScopeModelRecommendation(semantic.ScopeCodemaps)
	provider, err := semantic.NewVoyageProvider(semantic.VoyageConfig{
		APIKey: voyageKey,
		Model:  model,
	})
	if err != nil {
		return fmt.Errorf("create embedding provider: %w", err)
	}

	// Build embedding text from title, description, and query
	embeddingText := fmt.Sprintf("%s - %s\nQuery: %s", cm.Title, cm.Description, cm.Query)

	// Generate embedding
	embedding, err := provider.Embed(ctx, embeddingText)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}

	// Open memory store
	storageRoot := cfg.Storage.Root
	casRoot := filepath.Join(filepath.Dir(storageRoot), "cas")
	memStore, err := memory.Open(ctx, storageRoot, casRoot)
	if err != nil {
		return fmt.Errorf("open memory store: %w", err)
	}
	defer memStore.Close()

	// Serialize codemap result
	resultJSON, err := json.Marshal(cm)
	if err != nil {
		return fmt.Errorf("marshal codemap: %w", err)
	}

	// Create named entry with type="codemap"
	entry := memory.NamedEntry{
		Name:      fmt.Sprintf("codemap://%s", cm.ID),
		Type:      "codemap",
		Summary:   fmt.Sprintf("%s - %s", cm.Title, cm.Description),
		Result:    resultJSON,
		Workspace: workspace,
	}

	// Save entry first
	saved, err := memStore.Save(ctx, entry)
	if err != nil {
		return fmt.Errorf("save codemap entry: %w", err)
	}

	// Update with embedding
	if err := memStore.UpdateEmbedding(ctx, saved.Name, workspace, embedding); err != nil {
		return fmt.Errorf("update embedding: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Stored codemap with embedding: %s\n", saved.Name)
	return nil
}
