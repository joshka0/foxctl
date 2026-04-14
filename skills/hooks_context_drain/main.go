// Package main implements the hooks/context_drain skill.
// This skill drains the context buffer for injection into AI turns.
// Used by inject-capable events (UserPromptSubmit, system.transform, etc.).
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/obs"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/contextbuffer"
)

const skillName = "hooks/context_drain"

// logger is the package-level observability logger.
var logger *obs.Logger

// Input for the context_drain skill.
// Input defines the input parameters for context_drain operations.
type Input struct {
	WorkspaceID string   `json:"workspace_id"`
	SessionID   string   `json:"session_id"`
	AgentID     string   `json:"agent_id,omitempty"`
	Sources     []string `json:"sources,omitempty"`      // Filter by source
	MinPriority int      `json:"min_priority,omitempty"` // 1=high priority only
	Limit       int      `json:"limit,omitempty"`        // Max entries (default 50)
	Peek        bool     `json:"peek,omitempty"`         // Don't consume
	Format      string   `json:"format,omitempty"`       // "markdown" (default) or "json"
	Prune       bool     `json:"prune,omitempty"`        // Also prune expired entries
}

// main is the skill entry point for hooks/context_drain.
func main() {
	config.LoadDotEnv()
	skillmain.Main(skillName, run)
}

// run orchestrates context buffer draining with filtering, peeking, and pruning capabilities.
//
// Index:
// - Purpose: Drain the context buffer for injection into AI turns with flexible filtering and formatting
// - Flow: validate input → open store → build drain params → drain/peek → optionally prune → emit results
// - SideEffects: context consumption; expired entry pruning; source filtering
// - FailureModes: invalid input, store access failures, drain operation errors
// - Observability: emits drain counts, pending totals, timing metrics, and source summaries
// - Related: contextbuffer.Drain, contextbuffer.Peek, contextbuffer.PruneExpired
// - Keywords: hooks/context_drain, context_buffer, context_injection, peek_operations, pruning
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Initialize package logger
	logger = obs.NewLogger(obs.WithLogCommand(skillName))

	start := time.Now()

	// Validate required fields
	if in.WorkspaceID == "" {
		return fmt.Errorf("workspace_id is required")
	}
	if in.SessionID == "" {
		return fmt.Errorf("session_id is required")
	}

	// Defaults
	if in.Limit <= 0 {
		in.Limit = 50
	}
	if in.Format == "" {
		in.Format = "markdown"
	}

	// Open context buffer store
	store, err := contextbuffer.Open(ctx, rc.Config.Storage.Root)
	if err != nil {
		return fmt.Errorf("open context buffer: %w", err)
	}
	defer store.Close()

	// Build drain params
	params := contextbuffer.DrainParams{
		WorkspaceID:  in.WorkspaceID,
		SessionID:    in.SessionID,
		AgentID:      in.AgentID,
		Sources:      in.Sources,
		MinPriority:  in.MinPriority,
		Limit:        in.Limit,
		MarkConsumed: !in.Peek,
	}

	// Drain or peek
	var result *contextbuffer.DrainResult
	if in.Peek {
		result, err = store.Peek(ctx, params)
	} else {
		result, err = store.Drain(ctx, params)
	}
	if err != nil {
		return fmt.Errorf("drain context: %w", err)
	}

	// Optionally prune expired entries
	var pruned int
	if in.Prune {
		pruned, err = store.PruneExpired(ctx, 24*time.Hour)
		if err != nil {
			// Non-fatal, just log
			logger.Warn("prune failed", obs.Err(err))
		}
	}

	// Build output
	data := map[string]any{
		"count":         len(result.Entries),
		"total_pending": result.TotalPending,
		"duration_ms":   time.Since(start).Milliseconds(),
	}

	if in.Format == "json" {
		data["entries"] = result.Entries
	} else {
		data["markdown"] = result.Markdown
	}

	if in.Prune {
		data["pruned"] = pruned
	}

	// Include sources summary
	sources := make(map[string]int)
	for _, e := range result.Entries {
		sources[e.Source]++
	}
	data["sources"] = sources

	return skillout.Emit(rc, skillName, data)
}
