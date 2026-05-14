// Package main implements the hooks/task_guard skill.
// This skill enforces the task-centric model by ensuring, requiring, or
// proposing an active task before allowing write operations.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/hookutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/context/sessionkit"
	"github.com/joshka0/foxctl/internal/runtime/hooks"
	"github.com/joshka0/foxctl/internal/runtime/hooks/pathutil"
	"github.com/joshka0/foxctl/internal/runtime/hooks/toolutil"
	"github.com/joshka0/foxctl/internal/storage/graph"
)

// Mode controls task_guard behavior.
type Mode string

const (
	// ModeAuto auto-creates tasks when none exist.
	ModeAuto Mode = "auto"
	// ModeStrict blocks operations when no active task exists.
	ModeStrict Mode = "strict"
	// ModeProposal records control-plane task proposals when no active task exists.
	ModeProposal Mode = "proposal"
)

// main is the skill entry point for hooks/task_guard.
func main() {
	skillmain.Main("hooks/task_guard", run)
}

// run enforces governed task context for write operations.
//
// Index:
//
//	Purpose: Govern write operations by auto-creating, requiring, or proposing task context
//	Keywords: hooks/task_guard, task_centric, write_operations, task_creation, task_proposal
//	Related: deriveTaskTitle, createModifiedEdge, toolutil.IsWriteOperation
//	Flow: validate write operation → determine mode → resolve workspace → validate scope → ensure/require/record proposal → dirty existing task → emit result
//	Resources: task store; graph store; contextplane control proposals
//	Events: task-auto-created, task-dirtied, task-proposal-recorded, file-modified-edge-created
//	OutputFields: task_id, task_title, task_status, proposal_id, proposal_count, created, dirtied
//
// [[invariant:proposal-mode-does-not-create-task]]
// [[domain:work-item-centric-model]]
func run(ctx context.Context, rc *skillmain.RunContext, in hooks.Input) error {
	paths := sessionkit.ResolvePaths(rc.Config)

	// Skip non-write operations using cross-platform detection
	// Supports CC tools (Edit, Write, etc.), canonical tools (edit.*, fs.write_*), and explicit tool_kind
	if !toolutil.IsWriteOperation(in.ToolName, in.ToolCanonical, string(in.ToolKind)) {
		return hookutil.EmitOutput(rc, "hooks/task_guard", hooks.NewApprove("non-write operation", nil), nil)
	}

	mode := taskGuardModeFromEnv()
	workspaceRoot := hookutil.ResolveWorkspaceRoot(in, in.Cwd)
	workspaceID := hookutil.ResolveWorkspaceID(in, workspaceRoot)
	if mode == ModeProposal && !hasExplicitWorkspaceContext(in) {
		return fmt.Errorf("proposal mode requires workspace_root, cwd, FOXCTL_WORKSPACE, or CLAUDE_PROJECT_DIR")
	}
	if workspaceID == "" {
		return fmt.Errorf("failed to determine workspace_id; provide workspace_id or workspace_root")
	}

	// Open task store
	store, err := rc.Stores.Tasks(ctx)
	if err != nil {
		return fmt.Errorf("open task store: %w", err)
	}

	// Generate default title from tool + file path
	defaultTitle := deriveTaskTitle(in, workspaceRoot)

	// Get scope path from tool input using cross-platform path extraction
	scopePath := pathutil.ExtractPath(in.ToolInput)
	normalizedScopePath, scopeSafe := normalizeScopePath(scopePath, workspaceRoot)

	var output hooks.Output

	switch mode {
	case ModeAuto:
		// Auto-create task if none exists
		task, created, err := store.EnsureActive(ctx, workspaceID, defaultTitle, scopePath)
		if err != nil {
			return fmt.Errorf("ensure active task: %w", err)
		}

		// Check if task needs dirtying (ready_for_review or completed -> in_progress)
		dirtied := false
		if !created {
			task, dirtied, err = store.DirtyIfReviewed(ctx, task.ID)
			if err != nil {
				return fmt.Errorf("dirty task: %w", err)
			}
		}

		reason := "task ensured"
		if created {
			reason = fmt.Sprintf("auto-created task: %s", task.Title)
		} else if dirtied {
			reason = fmt.Sprintf("task dirtied (demoted to in_progress): %s", task.Title)
		}

		// Create graph edge: task → file (modified)
		if scopePath != "" {
			createModifiedEdge(ctx, paths.StorageRoot, workspaceID, task.ID, scopePath)
		}

		output = hooks.NewApprove(reason, map[string]any{
			"task_id":      task.ID,
			"task_title":   task.Title,
			"task_status":  task.Status,
			"workspace_id": workspaceID,
			"created":      created,
			"dirtied":      dirtied,
		})

	case ModeStrict:
		// Check for existing active task
		task, found, err := store.GetActive(ctx, workspaceID)
		if err != nil {
			return fmt.Errorf("get active task: %w", err)
		}

		if !found {
			output = hooks.NewBlock(
				"No active task. Create one with: foxctl todo add --title \"<task>\" or use /start-task",
			)
		} else {
			// Check if task needs dirtying (ready_for_review or completed -> in_progress)
			task, dirtied, err := store.DirtyIfReviewed(ctx, task.ID)
			if err != nil {
				return fmt.Errorf("dirty task: %w", err)
			}

			reason := "active task exists"
			if dirtied {
				reason = fmt.Sprintf("task dirtied (demoted to in_progress): %s", task.Title)
			}

			// Create graph edge: task → file (modified)
			if scopePath != "" {
				createModifiedEdge(ctx, paths.StorageRoot, workspaceID, task.ID, scopePath)
			}

			output = hooks.NewApprove(reason, map[string]any{
				"task_id":      task.ID,
				"task_title":   task.Title,
				"task_status":  task.Status,
				"workspace_id": workspaceID,
				"dirtied":      dirtied,
			})
		}

	case ModeProposal:
		// Proposal mode records a coordinator proposal instead of creating a task.
		if scopePath != "" && !scopeSafe {
			output = hooks.NewBlock("scope path is outside workspace; proposal mode blocks unsafe write scope")
			output.Meta = map[string]any{
				"workspace_id": workspaceID,
				"workspace":    workspaceRoot,
				"scope_path":   scopePath,
			}
			return hookutil.EmitOutput(rc, "hooks/task_guard", output, nil)
		}

		task, found, err := store.GetActive(ctx, workspaceID)
		if err != nil {
			return fmt.Errorf("get active task: %w", err)
		}

		if found {
			task, dirtied, err := store.DirtyIfReviewed(ctx, task.ID)
			if err != nil {
				return fmt.Errorf("dirty task: %w", err)
			}

			reason := "active task exists"
			if dirtied {
				reason = fmt.Sprintf("task dirtied (demoted to in_progress): %s", task.Title)
			}
			if scopePath != "" {
				createModifiedEdge(ctx, paths.StorageRoot, workspaceID, task.ID, scopePath)
			}

			output = hooks.NewApprove(reason, map[string]any{
				"task_id":             task.ID,
				"task_title":          task.Title,
				"task_status":         task.Status,
				"workspace_id":        workspaceID,
				"dirtied":             dirtied,
				"proposal_recorded":   false,
				"proposal_mode":       true,
				"proposal_scope_path": normalizedScopePath,
			})
			break
		}

		sourceTool := strings.TrimSpace(in.ToolCanonical)
		if sourceTool == "" {
			sourceTool = strings.TrimSpace(in.ToolName)
		}
		sourceEvent := strings.TrimSpace(string(in.Event))
		title := normalizeText(defaultTitle)
		toolKind := effectiveToolKind(in)
		intent := normalizeText(defaultProposalIntent(in, normalizedScopePath))
		dedupeKey := buildProposalDedupeKey(workspaceID, in.SessionID, in.Event, toolKind, normalizedScopePath, intent)
		sourceRefs, evidenceRefs := buildProposalRefs(workspaceID, in, normalizedScopePath, sourceEvent, sourceTool)
		controlStore := contextplane.NewWorkspaceStore(workspaceRoot)

		proposal, err := controlStore.RecordControlProposal(ctx, contextplane.ControlProposal{
			DedupeKey:      dedupeKey,
			Kind:           contextplane.ProposalKindTaskProposal,
			WorkspaceID:    workspaceID,
			SessionID:      strings.TrimSpace(in.SessionID),
			AgentID:        hookutil.ResolveActorID(in),
			Summary:        fmt.Sprintf("task proposal: %s", title),
			SourceRefs:     sourceRefs,
			EvidenceRefs:   evidenceRefs,
			ReviewRequired: true,
			Payload: map[string]any{
				"title":          title,
				"scope_path":     normalizedScopePath,
				"tool_name":      strings.TrimSpace(in.ToolName),
				"tool_canonical": strings.TrimSpace(in.ToolCanonical),
				"tool_kind":      string(toolKind),
				"event":          sourceEvent,
				"source_tool":    sourceTool,
				"source_event":   sourceEvent,
				"workspace_root": workspaceRoot,
				"intent":         intent,
			},
		})
		if err != nil {
			return fmt.Errorf("record task proposal: %w", err)
		}

		output = hooks.NewApprove("recorded task proposal", map[string]any{
			"workspace_id":      workspaceID,
			"proposal_mode":     true,
			"proposal_recorded": true,
			"proposal_id":       proposal.ID,
			"proposal_count":    proposal.Count,
			"proposal_status":   proposal.Status,
			"proposal_kind":     proposal.Kind,
			"dedupe_key":        proposal.DedupeKey,
			"title":             title,
			"scope_path":        normalizedScopePath,
		})
	}

	return hookutil.EmitOutput(rc, "hooks/task_guard", output, nil)
}

func taskGuardModeFromEnv() Mode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("FOXCTL_TASK_GUARD_MODE"))) {
	case string(ModeStrict):
		return ModeStrict
	case string(ModeProposal):
		return ModeProposal
	default:
		return ModeAuto
	}
}

func hasExplicitWorkspaceContext(in hooks.Input) bool {
	return strings.TrimSpace(in.WorkspaceRoot) != "" ||
		strings.TrimSpace(in.Cwd) != "" ||
		strings.TrimSpace(os.Getenv("FOXCTL_WORKSPACE")) != "" ||
		strings.TrimSpace(os.Getenv("CLAUDE_PROJECT_DIR")) != ""
}

func normalizeScopePath(scopePath, workspaceRoot string) (string, bool) {
	scopePath = strings.TrimSpace(scopePath)
	if scopePath == "" {
		return "", true
	}
	normalized := pathutil.NormalizePath(scopePath, workspaceRoot)
	if workspaceRoot == "" {
		return normalized, false
	}
	if !pathutil.IsUnderWorkspace(normalized, workspaceRoot) {
		return normalized, false
	}
	return pathutil.RelativePath(normalized, workspaceRoot), true
}

func normalizeText(in string) string {
	parts := strings.Fields(strings.TrimSpace(in))
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func defaultProposalIntent(in hooks.Input, scopePath string) string {
	toolKind := strings.TrimSpace(string(effectiveToolKind(in)))
	event := strings.TrimSpace(string(in.Event))
	scope := strings.TrimSpace(scopePath)
	if scope == "" {
		scope = "."
	}
	return fmt.Sprintf("%s %s %s", event, toolKind, scope)
}

func effectiveToolKind(in hooks.Input) hooks.ToolKind {
	if in.ToolKind != "" && in.ToolKind != hooks.ToolKindAny {
		return in.ToolKind
	}
	return hooks.ToolKind(toolutil.ClassifyTool(in.ToolName, in.ToolCanonical, ""))
}

// buildProposalDedupeKey keeps repeated hook signals pinned to one proposal row.
func buildProposalDedupeKey(workspaceID, sessionID string, event hooks.Event, toolKind hooks.ToolKind, scopePath, intent string) string {
	parts := []string{
		"hooks_task_guard:task_proposal:v1",
		strings.ToLower(normalizeText(workspaceID)),
		strings.ToLower(normalizeText(sessionID)),
		strings.ToLower(normalizeText(string(event))),
		strings.ToLower(normalizeText(string(toolKind))),
		strings.ToLower(normalizeText(scopePath)),
		strings.ToLower(normalizeText(intent)),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return "task_proposal:" + hex.EncodeToString(sum[:16])
}

func buildProposalRefs(workspaceID string, in hooks.Input, scopePath, sourceEvent, sourceTool string) ([]contextengine.EvidenceRef, []contextengine.EvidenceRef) {
	sourceRefs := make([]contextengine.EvidenceRef, 0, 3)
	evidenceRefs := make([]contextengine.EvidenceRef, 0, 4)

	if sourceEvent != "" {
		ref := contextengine.EvidenceRef{Type: contextengine.RefTypeEvent, Ref: sourceEvent, WorkspaceID: workspaceID}
		sourceRefs = append(sourceRefs, ref)
		evidenceRefs = append(evidenceRefs, ref)
	}
	if sid := strings.TrimSpace(in.SessionID); sid != "" {
		ref := contextengine.EvidenceRef{Type: contextengine.RefTypeSession, Ref: sid, WorkspaceID: workspaceID}
		sourceRefs = append(sourceRefs, ref)
		evidenceRefs = append(evidenceRefs, ref)
	}
	toolRef := strings.TrimSpace(in.ToolUseID)
	if toolRef == "" {
		toolRef = normalizeText(sourceTool)
	}
	if toolRef != "" {
		ref := contextengine.EvidenceRef{Type: contextengine.RefTypeToolCall, Ref: toolRef, WorkspaceID: workspaceID}
		sourceRefs = append(sourceRefs, ref)
		evidenceRefs = append(evidenceRefs, ref)
	}
	if scope := strings.TrimSpace(scopePath); scope != "" {
		evidenceRefs = append(evidenceRefs, contextengine.EvidenceRef{
			Type:        contextengine.RefTypePath,
			Ref:         scope,
			WorkspaceID: workspaceID,
		})
	}

	return sourceRefs, evidenceRefs
}

// deriveTaskTitle generates a task title from the hook input.
// Format: "<tool> <relative/path>" or "<tool> operation" if no path.
func deriveTaskTitle(in hooks.Input, workspaceRoot string) string {
	filePath := pathutil.ExtractPath(in.ToolInput)
	if filePath == "" {
		toolName := in.ToolName
		if toolName == "" {
			toolName = in.ToolCanonical
		}
		if toolName == "" {
			toolName = "tool"
		}
		return fmt.Sprintf("%s operation", toolName)
	}

	// Make path relative to workspace using pathutil
	filePath = pathutil.RelativePath(filePath, workspaceRoot)

	toolName := in.ToolName
	if toolName == "" {
		toolName = in.ToolCanonical
	}
	if toolName == "" {
		toolName = "tool"
	}
	return fmt.Sprintf("%s %s", toolName, filePath)
}

// createModifiedEdge creates a graph edge from task to file when modified.
// This enables PageRank to flow from tasks to the files they touch.
func createModifiedEdge(ctx context.Context, storagePath, workspaceID, taskID, filePath string) {
	// Early exit if no file path - avoids unnecessary graph.Open() overhead
	if filePath == "" {
		return
	}

	// Open graph store (fail silently - graph is optional)
	graphStore, err := graph.Open(ctx, storagePath)
	if err != nil {
		return
	}
	defer func() { _ = graphStore.Close() }()

	// Ensure task node exists
	taskNodeID := graph.TaskNodeID(taskID)
	taskNode := graph.Node{
		Workspace:   workspaceID,
		NodeID:      taskNodeID,
		NodeType:    graph.NodeTypeTask,
		Title:       taskID, // Will be updated by ingestion
		CurrentPath: "",
		LastSeen:    time.Now().UTC(),
	}
	_ = graphStore.UpsertNode(ctx, taskNode)

	// Ensure file node exists
	fileNodeID := graph.FileNodeID(filePath)
	fileNode := graph.Node{
		Workspace:   workspaceID,
		NodeID:      fileNodeID,
		NodeType:    graph.NodeTypeFile,
		Title:       filepath.Base(filePath),
		CurrentPath: filePath,
		LastSeen:    time.Now().UTC(),
	}
	_ = graphStore.UpsertNode(ctx, fileNode)

	// Create edge: task → file (modified)
	edge := graph.Edge{
		Workspace: workspaceID,
		FromID:    taskNodeID,
		FromType:  graph.NodeTypeTask,
		ToID:      fileNodeID,
		ToType:    graph.NodeTypeFile,
		EdgeType:  graph.EdgeTypeModified,
		Weight:    1.0,
		TTLDays:   intPtr(90), // 90 day TTL for task edges
		CreatedAt: time.Now().UTC(),
	}
	_ = graphStore.UpsertEdge(ctx, edge)
}

// intPtr returns a pointer to an integer.
func intPtr(i int) *int {
	return &i
}
