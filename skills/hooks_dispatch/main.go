// Package main implements the hooks/dispatch skill.
// This is the central dispatcher that loads hooks.yaml, matches hooks,
// runs them, and merges outputs. CC/OC adapters call this skill.
//
// Context Buffer Integration:
// - Processes enqueue_context actions by writing to the context buffer
// - When provider.can_inject_context is true, drains buffer and merges into output
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/hookutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/hooks"
	"github.com/jkatigb/agentctl/internal/storage/contextbuffer"
)

const skillName = "hooks/dispatch"

func main() {
	skillmain.Main(skillName, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in hooks.Input) error {
	start := time.Now()

	// Validate event
	if in.Event == "" {
		return fmt.Errorf("event is required")
	}
	if !in.Event.IsValid() {
		return fmt.Errorf("invalid event: %s", in.Event)
	}

	// Resolve workspace root
	workspaceRoot := hookutil.ResolveWorkspaceRoot(in, "")
	if workspaceRoot == "" {
		return fmt.Errorf("failed to determine workspace")
	}
	in.WorkspaceRoot = workspaceRoot

	// Auto-populate workspace_id if not provided
	workspaceID := hookutil.ResolveWorkspaceID(in, workspaceRoot)

	// Auto-populate tool_kind if not provided
	if in.ToolKind == "" && (in.ToolName != "" || in.ToolCanonical != "") {
		in.ToolKind = hooks.ClassifyToolKind(in.ToolName, in.ToolCanonical)
	}

	// Load hooks.yaml configuration
	hooksCfg, err := hooks.LoadConfigWithDefaults(workspaceRoot)
	if err != nil {
		return fmt.Errorf("load hooks config: %w", err)
	}

	// Get skills directory
	skillsDir := rc.Config.Paths.Skills
	if skillsDir == "" {
		home, _ := os.UserHomeDir()
		skillsDir = filepath.Join(home, ".agentctl", "skills")
	}

	// Create dispatcher with registry
	dispatcher := hooks.NewDispatcherWithRegistry(hooksCfg, skillsDir)

	// Dispatch hooks
	result, err := dispatcher.Dispatch(ctx, in)
	if err != nil {
		return fmt.Errorf("dispatch hooks: %w", err)
	}

	// Context Buffer integration
	var bufferStats map[string]any
	canInject := in.Provider != nil && in.Provider.CanInjectContext

	// Process enqueue_context actions and drain buffer if provider can inject
	if in.SessionID != "" {
		bufferStats, err = processContextBuffer(ctx, rc, workspaceID, in.SessionID, &result.Output, canInject)
		if err != nil {
			// Log but don't fail - context buffer is non-critical
			bufferStats = map[string]any{"error": err.Error()}
		}
	}

	// Build response data
	data := map[string]any{
		"hook_output":   result.Output,
		"matched_hooks": buildMatchedHooksInfo(hooksCfg, in),
		"steps":         buildStepsInfo(result.HookResults),
		"config_files":  existingConfigFiles(workspaceRoot),
		"blocked":       result.Blocked,
		"blocked_by":    result.BlockedBy,
		"hooks_run":     result.HooksRun,
		"duration_ms":   time.Since(start).Milliseconds(),
	}

	if bufferStats != nil {
		data["context_buffer"] = bufferStats
	}

	return skillout.Emit(rc, skillName, data)
}

// processContextBuffer handles enqueue_context actions and drains buffer when possible.
// If canInject is true, drains the buffer and merges context into output.
// Returns stats about buffer operations.
func processContextBuffer(ctx context.Context, rc *skillmain.RunContext, workspaceID, sessionID string, output *hooks.Output, canInject bool) (map[string]any, error) {
	stats := map[string]any{
		"can_inject": canInject,
	}

	// Open context buffer store
	store, err := contextbuffer.Open(ctx, rc.Config.Storage.Root)
	if err != nil {
		return nil, fmt.Errorf("open context buffer: %w", err)
	}
	defer store.Close()

	// Process enqueue_context actions from hook outputs
	var enqueued int
	for i := range output.Actions {
		action := &output.Actions[i]
		if action.Type != hooks.ActionEnqueueContext {
			continue
		}

		// Enqueue to buffer
		ttl := 60 * time.Second
		if action.TTLSeconds > 0 {
			ttl = time.Duration(action.TTLSeconds) * time.Second
		}

		params := contextbuffer.EnqueueParams{
			WorkspaceID: workspaceID,
			SessionID:   sessionID,
			Source:      action.Source,
			Text:        action.Text,
			Priority:    action.Priority,
			TTL:         ttl,
			Dedupe:      action.Dedupe,
		}

		_, err := store.Enqueue(ctx, params)
		if err != nil {
			// Log but continue
			continue
		}
		enqueued++
	}
	stats["enqueued"] = enqueued

	// Remove processed enqueue_context actions from output
	// (they've been written to buffer, no need to return to adapter)
	if enqueued > 0 {
		filtered := make([]hooks.Action, 0, len(output.Actions))
		for _, action := range output.Actions {
			if action.Type != hooks.ActionEnqueueContext {
				filtered = append(filtered, action)
			}
		}
		output.Actions = filtered
	}

	// If provider can inject context, drain the buffer
	if canInject {
		drainParams := contextbuffer.DrainParams{
			WorkspaceID:  workspaceID,
			SessionID:    sessionID,
			Limit:        50,
			MarkConsumed: true,
		}

		result, err := store.Drain(ctx, drainParams)
		if err != nil {
			stats["drain_error"] = err.Error()
		} else {
			stats["drained"] = len(result.Entries)
			stats["pending"] = result.TotalPending

			// Merge drained context into output
			if result.Markdown != "" {
				if output.Context == "" {
					output.Context = result.Markdown
				} else {
					// Prepend buffer context (higher priority than hook context)
					output.Context = result.Markdown + "\n\n---\n\n" + output.Context
				}
			}

			// Also convert drained entries to inject_context actions for compatibility
			for _, entry := range result.Entries {
				output.Actions = append(output.Actions, hooks.Action{
					Type:     hooks.ActionInjectContext,
					Text:     entry.Text,
					Priority: entry.Priority,
					Source:   entry.Source,
				})
			}
		}
	} else {
		// Report pending count even if we can't inject
		count, err := store.Count(ctx, workspaceID, sessionID)
		if err == nil {
			stats["pending"] = count
		}
	}

	return stats, nil
}

// buildMatchedHooksInfo returns info about which hooks matched.
func buildMatchedHooksInfo(cfg *hooks.Config, in hooks.Input) []map[string]any {
	matched := cfg.HooksForEvent(in.Event)
	result := make([]map[string]any, 0, len(matched))
	for _, h := range matched {
		if !hooks.MatchesInput(h, in) {
			continue
		}
		info := map[string]any{
			"id":       h.ID,
			"event":    string(h.Event),
			"priority": h.Priority,
		}
		if h.Match != nil {
			info["match"] = h.Match
		}
		info["run"] = h.Run
		result = append(result, info)
	}
	return result
}

// buildStepsInfo converts HookResults to the steps format from dispatcher-hooks.md.
func buildStepsInfo(results []hooks.HookResult) []map[string]any {
	steps := make([]map[string]any, 0, len(results))
	for i, r := range results {
		step := map[string]any{
			"idx":         i + 1,
			"hook_id":     r.HookID,
			"skill":       r.Skill,
			"decision":    string(r.Output.Decision),
			"duration_ms": r.Duration.Milliseconds(),
		}
		if r.Error != nil {
			step["error"] = r.Error.Error()
		}
		if r.FailOpen {
			step["fail_open"] = true
		}
		// Include raw hook_output for debugging
		step["hook_output"] = r.Output
		steps = append(steps, step)
	}
	return steps
}

func existingConfigFiles(workspaceRoot string) []string {
	paths := hooks.DefaultConfigPaths(workspaceRoot)
	existing := make([]string, 0, len(paths))
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			existing = append(existing, p)
		}
	}
	return existing
}
