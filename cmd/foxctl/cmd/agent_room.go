package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	agentdomain "github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/platform/config"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
)

func init() {
	agentCmd.AddCommand(newAgentRoomCommand())
}

func newAgentRoomCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "room",
		Short: "Inspect or update an agent control room",
	}
	cmd.AddCommand(
		newAgentRoomInfoCommand(),
		newAgentRoomPolicyCommand(),
	)
	return cmd
}

func newAgentRoomInfoCommand() *cobra.Command {
	var workspaceID string
	var roomID string
	cmd := &cobra.Command{
		Use:   "info <agent-ref>",
		Short: "Show the control-room metadata for an agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg := config.MustFromContext(ctx)

			agentRecord, cleanup, err := resolveAgentForMemory(ctx, cfg, args[0])
			if err != nil {
				return agentMemoryError(cmd, "agent/room/info", err)
			}
			defer cleanup()

			store, err := blackboard.OpenBoardStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeErrorEnvelope(cmd, "agent/room/info", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open board store: %v", err))
			}
			defer func() { errs.Ignore(store.Close(), "close board store") }()

			resolvedWorkspaceID := resolveAgentRoomWorkspace(workspaceID, agentRecord)
			resolvedRoomID := resolveAgentRoomID(roomID, agentRecord.ID)
			room, err := store.GetRoom(ctx, resolvedWorkspaceID, resolvedRoomID, "")
			if err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "not found") {
					return writeErrorEnvelope(cmd, "agent/room/info", string(protocol.ErrorCodeENotFound), fmt.Sprintf("room not found: %v", err))
				}
				return writeErrorEnvelope(cmd, "agent/room/info", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to get room: %v", err))
			}

			return writeOK(cmd, "agent/room/info", map[string]any{
				"agent_id":           agentRecord.ID,
				"workspace_id":       resolvedWorkspaceID,
				"room":               room,
				"dispatch_policy":    room.DispatchPolicy,
				"dispatch_agent_ids": room.DispatchAgentIDs,
			}, "run", nil)
		},
	}
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "Workspace path for the room lookup (defaults to the agent namespace or cwd)")
	cmd.Flags().StringVar(&roomID, "room-id", "", "Override the default control-room id")
	return cmd
}

func newAgentRoomPolicyCommand() *cobra.Command {
	var workspaceID string
	var roomID string
	var dispatchPolicy string
	var dispatchAgentIDs []string
	cmd := &cobra.Command{
		Use:   "policy <agent-ref>",
		Short: "Create or update an agent control-room dispatch policy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("dispatch-policy") && !cmd.Flags().Changed("dispatch-agent") {
				return writeErrorEnvelope(cmd, "agent/room/policy", string(protocol.ErrorCodeEARG), "at least one of --dispatch-policy or --dispatch-agent is required")
			}

			ctx := cmd.Context()
			cfg := config.MustFromContext(ctx)

			agentRecord, cleanup, err := resolveAgentForMemory(ctx, cfg, args[0])
			if err != nil {
				return agentMemoryError(cmd, "agent/room/policy", err)
			}
			defer cleanup()

			store, err := blackboard.OpenBoardStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeErrorEnvelope(cmd, "agent/room/policy", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open board store: %v", err))
			}
			defer func() { errs.Ignore(store.Close(), "close board store") }()

			resolvedWorkspaceID := resolveAgentRoomWorkspace(workspaceID, agentRecord)
			resolvedRoomID := resolveAgentRoomID(roomID, agentRecord.ID)
			current, err := store.GetRoom(ctx, resolvedWorkspaceID, resolvedRoomID, "")
			if err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
				return writeErrorEnvelope(cmd, "agent/room/policy", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to load room: %v", err))
			}

			nextPolicy := strings.TrimSpace(current.DispatchPolicy)
			if !cmd.Flags().Changed("dispatch-policy") || strings.TrimSpace(dispatchPolicy) == "" {
				if nextPolicy == "" {
					nextPolicy = "all_subtree"
				}
			} else {
				nextPolicy = normalizeRoomDispatchPolicyCLI(dispatchPolicy)
			}

			nextDispatchAgentIDs := append([]string(nil), current.DispatchAgentIDs...)
			if cmd.Flags().Changed("dispatch-agent") {
				nextDispatchAgentIDs = normalizeRoomDispatchAgentIDsCLI(dispatchAgentIDs)
			}

			title := strings.TrimSpace(current.Title)
			if title == "" {
				title = defaultAgentRoomTitle(agentRecord)
			}
			description := strings.TrimSpace(current.Description)

			room, err := store.UpsertRoom(ctx, agentdomain.Room{
				ID:               resolvedRoomID,
				WorkspaceID:      resolvedWorkspaceID,
				Stream:           agentdomain.RoomStreamName(resolvedRoomID),
				Title:            title,
				Description:      description,
				DispatchPolicy:   nextPolicy,
				DispatchAgentIDs: nextDispatchAgentIDs,
			})
			if err != nil {
				return writeErrorEnvelope(cmd, "agent/room/policy", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to persist room policy: %v", err))
			}

			return writeOK(cmd, "agent/room/policy", map[string]any{
				"agent_id":           agentRecord.ID,
				"workspace_id":       resolvedWorkspaceID,
				"room_id":            room.ID,
				"dispatch_policy":    room.DispatchPolicy,
				"dispatch_agent_ids": room.DispatchAgentIDs,
			}, "run", nil)
		},
	}
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "Workspace path for the room lookup (defaults to the agent namespace or cwd)")
	cmd.Flags().StringVar(&roomID, "room-id", "", "Override the default control-room id")
	cmd.Flags().StringVar(&dispatchPolicy, "dispatch-policy", "", "Dispatch policy (all_subtree|children_only|lead_only|selected)")
	cmd.Flags().StringArrayVar(&dispatchAgentIDs, "dispatch-agent", nil, "Explicit dispatch target id (repeatable)")
	return cmd
}

func resolveAgentRoomWorkspace(explicitWorkspace string, agentRecord agentdomain.Agent) string {
	if trimmed := strings.TrimSpace(explicitWorkspace); trimmed != "" {
		return workspace.Normalize(trimmed)
	}
	if trimmed := strings.TrimSpace(agentRecord.Namespace); strings.HasPrefix(trimmed, "/") {
		return workspace.Normalize(trimmed)
	}
	if cwd, err := os.Getwd(); err == nil {
		return workspace.Normalize(cwd)
	}
	return strings.TrimSpace(agentRecord.Namespace)
}

func resolveAgentRoomID(explicitRoomID, agentID string) string {
	if trimmed := strings.TrimSpace(explicitRoomID); trimmed != "" {
		return trimmed
	}
	return fmt.Sprintf("agent-%s", strings.TrimSpace(agentID))
}

func defaultAgentRoomTitle(agentRecord agentdomain.Agent) string {
	name := strings.TrimSpace(agentRecord.Name)
	if name == "" {
		name = strings.TrimSpace(agentRecord.Slug)
	}
	if name == "" {
		name = strings.TrimSpace(agentRecord.ID)
	}
	return fmt.Sprintf("%s Control Room", name)
}

func normalizeRoomDispatchPolicyCLI(raw string) string {
	switch strings.TrimSpace(raw) {
	case "children_only", "lead_only", "selected":
		return strings.TrimSpace(raw)
	default:
		return "all_subtree"
	}
}

func normalizeRoomDispatchAgentIDsCLI(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
