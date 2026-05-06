// Package main implements the hooks/context_enqueue skill.
// This skill enqueues context to the buffer for later injection.
// Used by hooks that run on events where injection isn't possible.
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/contextbuffer"
)

const skillName = "hooks/context_enqueue"

// Input for the context_enqueue skill.
// Input defines the input parameters for context_enqueue operations.
type Input struct {
	WorkspaceID string         `json:"workspace_id"`
	SessionID   string         `json:"session_id"`
	AgentID     string         `json:"agent_id,omitempty"`
	Source      string         `json:"source"`             // Required: identifies the hook origin
	Text        string         `json:"text"`               // Required: markdown content
	Priority    int            `json:"priority,omitempty"` // 1=high, 2=normal (default), 3=low
	TTLSeconds  int            `json:"ttl_seconds,omitempty"`
	Dedupe      bool           `json:"dedupe,omitempty"` // Skip if same source+text exists
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// main is the skill entry point for hooks/context_enqueue.
func main() {
	config.LoadDotEnv()
	skillmain.Main(skillName, run)
}

// run orchestrates context enqueuing to buffer for later injection with validation and deduplication.
//
// Index:
//
//	Purpose: Enqueue context to buffer for later injection when direct injection isn't possible
//	Keywords: hooks/context_enqueue, context_buffer, delayed_injection, ttl_management
//	Related: contextbuffer.Enqueue, contextbuffer.Count
//	Flow: validate input → open store → enqueue with TTL → get pending count → emit results
//	Resources: context buffer store (SQLite)
//	Events: context-enqueued
//	OutputFields: id, source, priority, expires_at, total_pending
//
// [[domain:context-buffer]]
// [[protocol:context-injection]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	start := time.Now()

	// Validate required fields
	if in.WorkspaceID == "" {
		return fmt.Errorf("workspace_id is required")
	}
	if in.SessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	if in.Source == "" {
		return fmt.Errorf("source is required")
	}
	if in.Text == "" {
		return fmt.Errorf("text is required")
	}

	// Defaults
	ttl := 60 * time.Second
	if in.TTLSeconds > 0 {
		ttl = time.Duration(in.TTLSeconds) * time.Second
	}

	// Open context buffer store
	store, err := contextbuffer.Open(ctx, rc.Config.Storage.Root)
	if err != nil {
		return fmt.Errorf("open context buffer: %w", err)
	}
	defer store.Close()

	// Build enqueue params
	params := contextbuffer.EnqueueParams{
		WorkspaceID: in.WorkspaceID,
		SessionID:   in.SessionID,
		AgentID:     in.AgentID,
		Source:      in.Source,
		Text:        in.Text,
		Priority:    in.Priority,
		TTL:         ttl,
		Dedupe:      in.Dedupe,
		Metadata:    in.Metadata,
	}

	// Enqueue
	entry, err := store.Enqueue(ctx, params)
	if err != nil {
		return fmt.Errorf("enqueue context: %w", err)
	}

	// Get pending count
	pending, err := store.Count(ctx, in.WorkspaceID, in.SessionID)
	if err != nil {
		// Non-fatal
		pending = -1
	}

	// Build output
	data := map[string]any{
		"id":            entry.ID,
		"source":        entry.Source,
		"priority":      entry.Priority,
		"expires_at":    entry.ExpiresAt.Format(time.RFC3339),
		"total_pending": pending,
		"duration_ms":   time.Since(start).Milliseconds(),
	}

	return skillout.Emit(rc, skillName, data)
}
