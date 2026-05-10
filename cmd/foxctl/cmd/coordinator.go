package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/orchestration/roomruntime"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
	taskstore "github.com/joshka0/foxctl/internal/storage/tasks"
	"github.com/spf13/cobra"
)

const coordinatorActorID = "actor:system:coordinator"

type coordinatorProcessItem struct {
	ProposalID      string `json:"proposal_id"`
	DecisionID      string `json:"decision_id,omitempty"`
	ApplyID         string `json:"apply_id,omitempty"`
	TaskID          string `json:"task_id,omitempty"`
	RoomID          string `json:"room_id,omitempty"`
	RoomMessageID   string `json:"room_message_id,omitempty"`
	Status          string `json:"status"`
	Reason          string `json:"reason,omitempty"`
	RoomSendFailure string `json:"room_send_failure,omitempty"`
}

type coordinatorProcessResult struct {
	ProcessedCount   int                      `json:"processed_count"`
	DecisionCount    int                      `json:"decision_count"`
	ApplyCount       int                      `json:"apply_count"`
	SkippedCount     int                      `json:"skipped_count"`
	EscalatedCount   int                      `json:"escalated_count"`
	RoomMessageCount int                      `json:"room_message_count"`
	RoomFailureCount int                      `json:"room_failure_count"`
	ProposalOutcomes []coordinatorProcessItem `json:"proposal_outcomes"`
}

type coordinatorProcessInput struct {
	WorkspacePath string
	Limit         int
	TaskStore     taskstore.Store
	ControlStore  *contextplane.WorkspaceStore
	StorageRoot   string
}

// coordinatorProcessAdapter owns the CLI's one-shot coordinator boundary.
type coordinatorProcessAdapter interface {
	Process(ctx context.Context, input coordinatorProcessInput) (coordinatorProcessResult, error)
}

func newCoordinatorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "coordinator",
		Short: "Run coordinator control-plane workflows",
	}
	cmd.AddCommand(newCoordinatorProcessCommand())
	return cmd
}

func newCoordinatorProcessCommand() *cobra.Command {
	var workspacePath string
	var limit int

	cmd := &cobra.Command{
		Use:   "process",
		Short: "Process coordinator task proposals in one bounded pass",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("workspace") || strings.TrimSpace(workspacePath) == "" {
				return fmt.Errorf("--workspace is required and must not be blank")
			}
			target, err := filepath.Abs(strings.TrimSpace(workspacePath))
			if err != nil {
				return fmt.Errorf("resolve workspace: %w", err)
			}
			if limit <= 0 {
				return fmt.Errorf("--limit must be greater than 0")
			}

			cfg, err := loadConfig(cmd.Context())
			if err != nil {
				return err
			}
			tasks, err := taskstore.Open(cmd.Context(), cfg.Storage.Root)
			if err != nil {
				return err
			}
			defer func() { _ = tasks.Close() }()

			result, err := newCoordinatorProcessAdapter().Process(cmd.Context(), coordinatorProcessInput{
				WorkspacePath: target,
				Limit:         limit,
				TaskStore:     tasks,
				ControlStore:  contextplane.NewWorkspaceStore(target),
				StorageRoot:   cfg.Storage.Root,
			})
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("coordinator/process", map[string]any{
				"workspace_path":     target,
				"workspace_id":       workspace.ID(target),
				"limit":              limit,
				"processed_count":    result.ProcessedCount,
				"decisions":          result.DecisionCount,
				"applies":            result.ApplyCount,
				"skipped_count":      result.SkippedCount,
				"escalated_count":    result.EscalatedCount,
				"room_message_count": result.RoomMessageCount,
				"room_failure_count": result.RoomFailureCount,
				"proposal_outcomes":  result.ProposalOutcomes,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (required; no cwd default)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of proposals to process")
	return cmd
}

func newCoordinatorProcessAdapter() coordinatorProcessAdapter {
	return coordinatorProcessMVPAdapter{}
}

type coordinatorProcessMVPAdapter struct{}

// Process applies the one-shot coordinator policy to open control proposals.
//
// [[protocol:coordinator-one-shot-process]]
// [[test:cmd/foxctl/cmd/coordinator_test.go#TestCoordinatorProcessSeedsLowRiskTaskProposal]]
func (coordinatorProcessMVPAdapter) Process(ctx context.Context, input coordinatorProcessInput) (coordinatorProcessResult, error) {
	core, err := input.ControlStore.ProcessControlProposals(ctx, contextplane.TaskProposalControlProcessInput{
		TaskStore: input.TaskStore,
		Limit:     input.Limit,
	})
	if err != nil {
		return coordinatorProcessResult{}, err
	}

	var (
		result     coordinatorProcessResult
		boardStore blackboard.BoardStore
	)
	defer func() {
		if boardStore != nil {
			_ = boardStore.Close()
		}
	}()

	openBoardStore := func() (blackboard.BoardStore, error) {
		if boardStore != nil {
			return boardStore, nil
		}
		store, openErr := blackboard.OpenBoardStore(ctx, input.StorageRoot)
		if openErr != nil {
			return nil, openErr
		}
		boardStore = store
		return boardStore, nil
	}

	for _, coreItem := range core.Items {
		item := coordinatorProcessItem{
			ProposalID: coreItem.ProposalID,
			RoomID:     strings.TrimSpace(coreItem.Proposal.RoomID),
			Status:     "processed",
		}
		result.ProcessedCount++
		if coreItem.Decision != nil {
			item.DecisionID = coreItem.Decision.ID
			item.Reason = strings.TrimSpace(coreItem.Decision.Reason)
			if coreItem.DecisionRecorded {
				result.DecisionCount++
			}
		}
		if coreItem.Apply != nil {
			item.ApplyID = coreItem.Apply.ID
			item.TaskID = strings.TrimSpace(coreItem.Apply.TargetID)
		}
		if coreItem.Task != nil {
			item.TaskID = coreItem.Task.ID
		}

		switch {
		case coreItem.ApplyRecorded:
			item.Status = "applied"
			result.ApplyCount++
			maybeSendCoordinatorRoomMessage(ctx, input.WorkspacePath, openBoardStore, coreItem, &item, &result)
		case coreItem.Apply != nil:
			item.Status = "skipped"
			item.Reason = firstNonEmpty(strings.TrimSpace(item.Reason), "proposal already has apply result")
			result.SkippedCount++
		case coreItem.Decision != nil && coreItem.Decision.Decision != contextplane.DecisionKindApprove:
			item.Status = string(coreItem.Decision.StatusAfter)
			result.EscalatedCount++
		case coreItem.Decision != nil:
			item.Status = string(coreItem.Decision.StatusAfter)
		}

		result.ProposalOutcomes = append(result.ProposalOutcomes, item)
	}

	return result, nil
}

func maybeSendCoordinatorRoomMessage(
	ctx context.Context,
	workspacePath string,
	openBoardStore func() (blackboard.BoardStore, error),
	coreItem contextplane.TaskProposalControlProcessItem,
	item *coordinatorProcessItem,
	result *coordinatorProcessResult,
) {
	roomID := strings.TrimSpace(coreItem.Proposal.RoomID)
	if roomID == "" || coreItem.Task == nil {
		return
	}
	boardStore, err := openBoardStore()
	if err != nil {
		item.RoomSendFailure = err.Error()
		result.RoomFailureCount++
		return
	}
	roomResult, err := roomruntime.SendMessage(ctx, boardStore, roomruntime.SendMessageInput{
		WorkspaceID: workspacePath,
		RoomID:      roomID,
		RoomTitle:   roomID,
		Sender:      coordinatorActorID,
		Recipient:   agent.BroadcastRecipient,
		TaskID:      coreItem.Task.ID,
		Kind:        agent.BoardMessageKindTaskUpdate,
		Subject:     "Coordinator applied task proposal",
		Body:        fmt.Sprintf("Applied proposal %s to task %s (%s).", coreItem.ProposalID, coreItem.Task.ID, coreItem.Task.Title),
		EnsureRoom:  true,
	})
	if err != nil {
		item.RoomSendFailure = err.Error()
		result.RoomFailureCount++
		return
	}
	if roomResult.Message != nil {
		item.RoomMessageID = roomResult.Message.ID
		result.RoomMessageCount++
	}
}

func init() {
	rootCmd.AddCommand(newCoordinatorCommand())
}
