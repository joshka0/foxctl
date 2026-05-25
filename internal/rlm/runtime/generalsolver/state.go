package generalsolver

import (
	"fmt"
	"sort"
)

const (
	defaultMaxAttempts    = 5
	maxWorkItems          = 128
	maxDigests            = 64
	maxFailureEntries     = 256
	maxFailureDigestChars = 2000
)

type SolverState struct {
	Items       map[string]WorkItem
	ReverseDeps map[string][]string
	Artifacts   map[string]WorkArtifact
	FailureLog  []FailureEntry
	ReadyQueue  []string
	Digests     []string
}

type FailureEntry struct {
	WorkItemID string         `json:"work_item_id"`
	Attempt    int            `json:"attempt"`
	Reason     string         `json:"reason"`
	Feedback   map[string]any `json:"feedback,omitempty"`
}

func NewSolverState() *SolverState {
	return &SolverState{
		Items:       make(map[string]WorkItem),
		ReverseDeps: make(map[string][]string),
		Artifacts:   make(map[string]WorkArtifact),
		FailureLog:  make([]FailureEntry, 0),
		ReadyQueue:  make([]string, 0),
		Digests:     make([]string, 0),
	}
}

func AddWorkItem(state *SolverState, item WorkItem) error {
	if state == nil {
		return fmt.Errorf("generalsolver: state is nil")
	}
	if item.ID == "" {
		return fmt.Errorf("generalsolver: work item id is required")
	}
	if _, exists := state.Items[item.ID]; exists {
		return fmt.Errorf("generalsolver: duplicate work item id %q", item.ID)
	}
	if len(state.Items) >= maxWorkItems {
		return fmt.Errorf("generalsolver: work item count %d exceeds max %d", len(state.Items), maxWorkItems)
	}
	if !ValidProblemArchetype(item.Archetype) {
		return fmt.Errorf("generalsolver: invalid archetype %q for work item %q", item.Archetype, item.ID)
	}
	if item.MaxAttempts <= 0 {
		item.MaxAttempts = defaultMaxAttempts
	}
	if item.Status == "" {
		item.Status = StatusPending
	}
	if !ValidWorkItemStatus(item.Status) {
		return fmt.Errorf("generalsolver: invalid status %q for work item %q", item.Status, item.ID)
	}
	for _, depID := range item.DependsOn {
		if depID == "" {
			return fmt.Errorf("generalsolver: work item %q has empty dependency id", item.ID)
		}
		if depID == item.ID {
			return fmt.Errorf("generalsolver: work item %q depends on itself", item.ID)
		}
		state.ReverseDeps[depID] = append(state.ReverseDeps[depID], item.ID)
	}
	state.Items[item.ID] = item
	return nil
}

func MarkDependencySolved(state *SolverState, solvedID string) {
	if state == nil {
		return
	}
	for _, affectedID := range state.ReverseDeps[solvedID] {
		item, exists := state.Items[affectedID]
		if !exists || item.Status != StatusPending {
			continue
		}
		allSolved := true
		for _, depID := range item.DependsOn {
			dep, ok := state.Items[depID]
			if !ok || dep.Status != StatusSolved {
				allSolved = false
				break
			}
		}
		if allSolved {
			item.Status = StatusReady
			state.Items[affectedID] = item
		}
	}
}

func ComputeReadyQueue(state *SolverState) []string {
	if state == nil {
		return nil
	}
	queue := make([]string, 0)
	for id, item := range state.Items {
		switch item.Status {
		case StatusReady, StatusPending:
			if allDependenciesSolved(state, item) {
				queue = append(queue, id)
			}
		}
	}
	sort.Slice(queue, func(i, j int) bool {
		a, b := state.Items[queue[i]], state.Items[queue[j]]
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if a.Risk != b.Risk {
			return a.Risk > b.Risk
		}
		return queue[i] < queue[j]
	})
	state.ReadyQueue = queue
	return queue
}

func CommitArtifact(state *SolverState, workItemID string, artifact WorkArtifact) error {
	if state == nil {
		return fmt.Errorf("generalsolver: state is nil")
	}
	item, exists := state.Items[workItemID]
	if !exists {
		return fmt.Errorf("generalsolver: work item %q does not exist", workItemID)
	}
	if item.Status != StatusSolving {
		return fmt.Errorf("generalsolver: work item %q status is %q, expected %q", workItemID, item.Status, StatusSolving)
	}
	artifact.WorkItemID = workItemID
	state.Artifacts[workItemID] = artifact
	item.Status = StatusSolved
	item.Attempts++
	state.Items[workItemID] = item
	MarkDependencySolved(state, workItemID)
	return nil
}

func RecordFailure(state *SolverState, workItemID string, reason string, feedback map[string]any) error {
	if state == nil {
		return fmt.Errorf("generalsolver: state is nil")
	}
	item, exists := state.Items[workItemID]
	if !exists {
		return fmt.Errorf("generalsolver: work item %q does not exist", workItemID)
	}
	if len(state.FailureLog) >= maxFailureEntries {
		state.FailureLog = state.FailureLog[1:]
	}
	entry := FailureEntry{
		WorkItemID: workItemID,
		Attempt:    item.Attempts + 1,
		Reason:     reason,
		Feedback:   feedback,
	}
	state.FailureLog = append(state.FailureLog, entry)
	item.Attempts++
	if item.Attempts >= item.MaxAttempts {
		item.Status = StatusFailed
	} else {
		item.Status = StatusReady
	}
	state.Items[workItemID] = item
	return nil
}

func CompactFailureDigest(state *SolverState) string {
	if state == nil || len(state.FailureLog) == 0 {
		return ""
	}
	byItem := make(map[string][]FailureEntry)
	for _, entry := range state.FailureLog {
		byItem[entry.WorkItemID] = append(byItem[entry.WorkItemID], entry)
	}
	var digest string
	for itemID, entries := range byItem {
		if len(digest) > 0 {
			digest += "; "
		}
		digest += fmt.Sprintf("%s: %d failures", itemID, len(entries))
		last := entries[len(entries)-1]
		if last.Reason != "" {
			digest += fmt.Sprintf(" (last: %s)", truncateString(last.Reason, 80))
		}
	}
	digest = truncateString(digest, maxFailureDigestChars)
	state.Digests = append(state.Digests, digest)
	if len(state.Digests) > maxDigests {
		state.Digests = state.Digests[1:]
	}
	state.FailureLog = state.FailureLog[:0]
	return digest
}

func SummarizeState(state *SolverState) StateSummary {
	if state == nil {
		return StateSummary{}
	}
	summary := StateSummary{
		TotalItems:   len(state.Items),
		Artifacts:    len(state.Artifacts),
		FailureCount: len(state.FailureLog),
		DigestCount:  len(state.Digests),
		ByStatus:     make(map[WorkItemStatus]int),
		ByArchetype:  make(map[ProblemArchetype]int),
	}
	for _, item := range state.Items {
		summary.ByStatus[item.Status]++
		summary.ByArchetype[item.Archetype]++
		switch item.Status {
		case StatusSolved:
			summary.SolvedIDs = append(summary.SolvedIDs, item.ID)
		case StatusFailed, StatusBlocked:
			summary.BlockedIDs = append(summary.BlockedIDs, item.ID)
		}
	}
	ComputeReadyQueue(state)
	summary.ReadyCount = len(state.ReadyQueue)
	return summary
}

type StateSummary struct {
	TotalItems   int                      `json:"total_items"`
	SolvedIDs    []string                 `json:"solved_ids,omitempty"`
	BlockedIDs   []string                 `json:"blocked_ids,omitempty"`
	ReadyCount   int                      `json:"ready_count"`
	Artifacts    int                      `json:"artifacts"`
	FailureCount int                      `json:"failure_count"`
	DigestCount  int                      `json:"digest_count"`
	ByStatus     map[WorkItemStatus]int   `json:"by_status"`
	ByArchetype  map[ProblemArchetype]int `json:"by_archetype"`
}

func allDependenciesSolved(state *SolverState, item WorkItem) bool {
	for _, depID := range item.DependsOn {
		dep, ok := state.Items[depID]
		if !ok || dep.Status != StatusSolved {
			return false
		}
	}
	return true
}

func truncateString(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	if maxLen < 15 {
		return s[:maxLen]
	}
	return s[:maxLen-12] + "...[trunc]"
}
