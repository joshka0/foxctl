package generalsolver

import (
	"fmt"
	"sort"
)

const defaultMaxSchedulerIterations = 128

type Scheduler struct {
	state *SolverState
}

func NewScheduler(state *SolverState) *Scheduler {
	return &Scheduler{state: state}
}

func (s *Scheduler) State() *SolverState {
	return s.state
}

func (s *Scheduler) PickNext() (WorkItem, bool) {
	if s.state == nil {
		return WorkItem{}, false
	}
	queue := ComputeReadyQueue(s.state)
	if len(queue) == 0 {
		return WorkItem{}, false
	}
	id := queue[0]
	item, exists := s.state.Items[id]
	if !exists {
		return WorkItem{}, false
	}
	item.Status = StatusSolving
	s.state.Items[id] = item
	return item, true
}

func (s *Scheduler) Commit(workItemID string, artifact WorkArtifact) error {
	return CommitArtifact(s.state, workItemID, artifact)
}

func (s *Scheduler) RecordFailure(workItemID string, reason string, feedback map[string]any) error {
	return RecordFailure(s.state, workItemID, reason, feedback)
}

func (s *Scheduler) IsComplete() bool {
	if s.state == nil {
		return true
	}
	for _, item := range s.state.Items {
		switch item.Status {
		case StatusPending, StatusReady, StatusSolving:
			return false
		}
	}
	return len(s.state.Items) > 0
}

func (s *Scheduler) HasBlockedOrFailed() bool {
	if s.state == nil {
		return false
	}
	for _, item := range s.state.Items {
		if item.Status == StatusBlocked || item.Status == StatusFailed {
			return true
		}
	}
	return false
}

func (s *Scheduler) RunToCompletion(fn func(item WorkItem) (WorkArtifact, WorkVerdict, error)) (*SchedulerReport, error) {
	if s.state == nil {
		return nil, fmt.Errorf("generalsolver: scheduler has no state")
	}
	report := &SchedulerReport{}
	for i := 0; i < defaultMaxSchedulerIterations; i++ {
		if s.IsComplete() {
			break
		}
		item, ok := s.PickNext()
		if !ok {
			break
		}
		report.ItemsProcessed++
		artifact, verdict, err := fn(item)
		if err != nil {
			report.Errors = append(report.Errors, SchedulerError{
				WorkItemID: item.ID,
				Attempt:    item.Attempts,
				Error:      err.Error(),
			})
			_ = s.RecordFailure(item.ID, err.Error(), nil)
			continue
		}
		if verdict.Accept {
			report.Committed++
			if cErr := s.Commit(item.ID, artifact); cErr != nil {
				return report, fmt.Errorf("generalsolver: commit failed for %q: %w", item.ID, cErr)
			}
		} else if verdict.Repairable {
			report.Repairs++
			_ = s.RecordFailure(item.ID, "verdict: not accepted, repairable", verdict.Feedback)
		} else {
			report.Rejections++
			_ = s.RecordFailure(item.ID, "verdict: not accepted, not repairable", verdict.Feedback)
		}
	}
	return report, nil
}

type SchedulerReport struct {
	ItemsProcessed int             `json:"items_processed"`
	Committed      int             `json:"committed"`
	Repairs        int             `json:"repairs"`
	Rejections     int             `json:"rejections"`
	Errors         []SchedulerError `json:"errors,omitempty"`
}

type SchedulerError struct {
	WorkItemID string `json:"work_item_id"`
	Attempt    int    `json:"attempt"`
	Error      string `json:"error"`
}

func ValidateSolverState(state *SolverState) error {
	if state == nil {
		return fmt.Errorf("generalsolver: state is nil")
	}
	ids := make(map[string]bool, len(state.Items))
	for id, item := range state.Items {
		if id != item.ID {
			return fmt.Errorf("generalsolver: map key %q does not match item id %q", id, item.ID)
		}
		if ids[id] {
			return fmt.Errorf("generalsolver: duplicate id %q", id)
		}
		ids[id] = true
		for _, depID := range item.DependsOn {
			if !ids[depID] && state.Items[depID].ID == "" {
				return fmt.Errorf("generalsolver: work item %q depends on unknown id %q", id, depID)
			}
		}
	}
	if err := validateNoCycles(state); err != nil {
		return err
	}
	return nil
}

func validateNoCycles(state *SolverState) error {
	visitState := make(map[string]int, len(state.Items))
	var visit func(string) error
	visit = func(id string) error {
		switch visitState[id] {
		case 1:
			return fmt.Errorf("generalsolver: cycle detected at work item %q", id)
		case 2:
			return nil
		}
		visitState[id] = 1
		item, exists := state.Items[id]
		if !exists {
			return nil
		}
		for _, depID := range item.DependsOn {
			if err := visit(depID); err != nil {
				return err
			}
		}
		visitState[id] = 2
		return nil
	}
	ids := make([]string, 0, len(state.Items))
	for id := range state.Items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}
