package contextplane

import (
	"context"
	"fmt"
	"sort"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	taskstore "github.com/joshka0/foxctl/internal/storage/tasks"
)

// SelectNextTask chooses the highest-priority currently eligible task from the workspace task store.
func SelectNextTask(ctx context.Context, tasks taskstore.Store, workspaceID string) (TaskCandidate, bool, error) {
	if tasks == nil {
		return TaskCandidate{}, false, fmt.Errorf("task store is required")
	}
	if active, ok, err := tasks.GetActive(ctx, workspaceID); err != nil {
		return TaskCandidate{}, false, err
	} else if ok {
		return toTaskCandidate(active), true, nil
	}
	items, err := tasks.ListWithOptions(ctx, workspaceID, taskstore.ListOptions{
		Statuses: []string{
			taskstore.StatusInProgress,
			taskstore.StatusReadyForReview,
			taskstore.StatusPending,
			taskstore.StatusBlocked,
		},
		Limit: 50,
	})
	if err != nil {
		return TaskCandidate{}, false, err
	}
	if len(items) == 0 {
		return TaskCandidate{}, false, nil
	}
	sort.SliceStable(items, func(i, j int) bool {
		return taskRank(items[i]) < taskRank(items[j])
	})
	return toTaskCandidate(items[0]), true, nil
}

// BuildTaskPacket creates a bounded dispatch packet for a selected task.
func (s *WorkspaceStore) BuildTaskPacket(ctx context.Context, tasks taskstore.Store, workspaceID, taskID string) (TaskPacket, error) {
	top, err := s.LoadTopOfMind()
	if err != nil {
		return TaskPacket{}, err
	}
	var candidate taskstore.Task
	if taskID != "" {
		candidate, err = tasks.Get(ctx, taskID)
		if err != nil {
			return TaskPacket{}, err
		}
	} else {
		next, ok, err := SelectNextTask(ctx, tasks, workspaceID)
		if err != nil {
			return TaskPacket{}, err
		}
		if !ok {
			return TaskPacket{}, fmt.Errorf("no eligible task found")
		}
		candidate, err = tasks.Get(ctx, next.ID)
		if err != nil {
			return TaskPacket{}, err
		}
	}
	handoffs, err := s.ListHandoffs(1)
	if err != nil {
		return TaskPacket{}, err
	}
	packet := TaskPacket{
		WorkspaceID:     workspaceID,
		Task:            toTaskCandidate(candidate),
		Objective:       top.Objective,
		Phase:           top.Phase,
		HardConstraints: append([]string(nil), top.HardConstraints...),
		Blockers:        append([]string(nil), top.Blockers...),
		RecentDecisions: append([]RecentDecision(nil), top.RecentDecisions...),
		NextActions:     append([]string(nil), top.NextActions...),
		RelevantRefs:    append([]contextengine.EvidenceRef(nil), top.RelevantRefs...),
		GeneratedAt:     top.UpdatedAt,
	}
	if len(handoffs) > 0 {
		packet.LatestHandoff = &handoffs[0]
		packet.RelevantRefs = uniqueEvidenceRefs(append(packet.RelevantRefs, handoffs[0].Handoff.EvidenceRefs...))
	}
	if candidate.ScopePath != "" {
		packet.RelevantRefs = uniqueEvidenceRefs(append(packet.RelevantRefs, contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: candidate.ScopePath}))
	}
	if candidate.PlanFile != "" {
		packet.RelevantRefs = uniqueEvidenceRefs(append(packet.RelevantRefs, contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: candidate.PlanFile}))
	}
	if len(packet.RelevantRefs) > 8 {
		packet.RelevantRefs = packet.RelevantRefs[:8]
	}
	return packet, nil
}

func toTaskCandidate(task taskstore.Task) TaskCandidate {
	return TaskCandidate{
		ID:          task.ID,
		Title:       task.Title,
		Description: task.Description,
		Status:      task.Status,
		ScopePath:   task.ScopePath,
		DependsOn:   append([]string(nil), task.DependsOn...),
		PlanFile:    task.PlanFile,
		PlanSection: task.PlanSection,
		SessionID:   task.SessionID,
	}
}

func taskRank(task taskstore.Task) int {
	switch task.Status {
	case taskstore.StatusInProgress:
		return 0
	case taskstore.StatusReadyForReview:
		return 1
	case taskstore.StatusPending:
		return 2
	case taskstore.StatusBlocked:
		return 3
	default:
		return 99
	}
}
