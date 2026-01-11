// Package main implements the hooks/context_drain skill.
// This skill drains the context buffer for injection into AI turns.
// Used by inject-capable events (UserPromptSubmit, system.transform, etc.).
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/contextbuffer"
)

const skillName = "hooks/context_drain"

// Input for the context_drain skill.
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

func main() {
	config.LoadDotEnv()
	skillmain.Main(skillName, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
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
			fmt.Printf("warn: prune failed: %v\n", err)
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
