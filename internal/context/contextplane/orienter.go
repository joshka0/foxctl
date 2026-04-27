package contextplane

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/platform/timeutil"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/tasks"
)

// TaskProvider is the minimal task store contract required to compute orientation.
type TaskProvider interface {
	GetActive(ctx context.Context, workspaceID string) (tasks.Task, bool, error)
	ListWithOptions(ctx context.Context, workspaceID string, opts tasks.ListOptions) ([]tasks.Task, error)
}

// SessionProvider is the minimal session store contract required to compute orientation.
type SessionProvider interface {
	List(ctx context.Context, opts storage.SessionListOptions) ([]storage.Session, error)
}

// Orienter computes a bounded top-of-mind bundle from existing runtime state.
type Orienter struct {
	Tasks    TaskProvider
	Sessions SessionProvider
}

// NewOrienter creates an orientation service from task and session providers.
func NewOrienter(taskStore TaskProvider, sessionStore SessionProvider) *Orienter {
	return &Orienter{
		Tasks:    taskStore,
		Sessions: sessionStore,
	}
}

// Build computes top-of-mind for the detected or provided workspace root.
func (o *Orienter) Build(ctx context.Context, workspacePath string) (TopOfMind, error) {
	root := workspacePath
	if strings.TrimSpace(root) == "" {
		root = ws.Detect("")
	} else {
		root = ws.Detect(root)
	}
	root = ws.Normalize(root)
	if root == "" {
		return TopOfMind{}, fmt.Errorf("detect workspace")
	}

	workspaceID := ws.ID(root)
	top := TopOfMind{
		WorkspaceID: workspaceID,
		Objective:   "Orient workspace and establish next action",
		Phase:       "orient",
		UpdatedAt:   timeutil.NowUTC(),
	}

	var active tasks.Task
	var activeOK bool
	var err error
	if o.Tasks != nil {
		active, activeOK, err = o.Tasks.GetActive(ctx, workspaceID)
		if err != nil {
			return TopOfMind{}, fmt.Errorf("get active task: %w", err)
		}
	}

	taskList := make([]tasks.Task, 0)
	if o.Tasks != nil {
		taskList, err = o.Tasks.ListWithOptions(ctx, workspaceID, tasks.ListOptions{
			Statuses: []string{
				tasks.StatusInProgress,
				tasks.StatusPending,
				tasks.StatusBlocked,
				tasks.StatusReadyForReview,
			},
			Limit: 25,
		})
		if err != nil {
			return TopOfMind{}, fmt.Errorf("list tasks: %w", err)
		}
	}
	sortTasks(taskList)

	recentSessions := make([]storage.Session, 0)
	if o.Sessions != nil {
		recentSessions, err = o.Sessions.List(ctx, storage.SessionListOptions{
			WorkspaceID: workspaceID,
			Limit:       5,
		})
		if err != nil {
			return TopOfMind{}, fmt.Errorf("list sessions: %w", err)
		}
	}
	sortSessions(recentSessions)

	if activeOK {
		top.ActiveTaskIDs = []string{active.ID}
		top.Objective = firstNonEmpty(active.Title, active.Description, top.Objective)
		top.Phase = phaseFromTaskStatus(active.Status)
	} else if len(taskList) > 0 {
		top.ActiveTaskIDs = nonEmptyStrings(taskList[0].ID)
		top.Objective = firstNonEmpty(taskList[0].Title, taskList[0].Description, top.Objective)
		top.Phase = phaseFromTaskStatus(taskList[0].Status)
	} else if len(recentSessions) > 0 {
		top.Objective = firstNonEmpty(recentSessions[0].Summary, top.Objective)
		top.Phase = phaseFromSession(recentSessions[0].Status)
	}

	top.HardConstraints = uniqueLimit(append(splitNotes(active.Gotchas), collectSessionNotes(recentSessions, func(s storage.Session) []string {
		return s.Gotchas
	})...), 3)
	top.Blockers = collectBlockers(taskList, active.ID, 3)
	top.RecentDecisions = collectDecisions(recentSessions, 3)
	top.OpenLoops = uniqueLimit(append(
		collectSessionNotes(recentSessions, func(s storage.Session) []string { return s.KeyQuestions }),
		collectTaskTitles(taskList, active.ID, []string{tasks.StatusPending}, 3)...,
	), 3)
	top.NextActions = uniqueLimit(buildNextActions(active, activeOK, taskList), 3)
	top.RelevantRefs = uniqueEvidenceRefsLimit(buildRelevantRefs(active, activeOK, recentSessions), 4)

	if len(top.HardConstraints) == 0 {
		top.HardConstraints = []string{"Keep the control plane file-first and bounded."}
	}
	if len(top.NextActions) == 0 {
		top.NextActions = []string{"Capture the next concrete task before spawning work."}
	}
	if len(top.OpenLoops) == 0 && len(top.Blockers) > 0 {
		top.OpenLoops = append([]string{}, top.Blockers...)
	}
	return top, nil
}

func sortTasks(items []tasks.Task) {
	statusRank := map[string]int{
		tasks.StatusInProgress:     0,
		tasks.StatusPending:        1,
		tasks.StatusBlocked:        2,
		tasks.StatusReadyForReview: 3,
	}
	sort.SliceStable(items, func(i, j int) bool {
		ri, ok := statusRank[items[i].Status]
		if !ok {
			ri = 99
		}
		rj, ok := statusRank[items[j].Status]
		if !ok {
			rj = 99
		}
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(items[i].Title) < strings.ToLower(items[j].Title)
	})
}

func sortSessions(items []storage.Session) {
	sort.SliceStable(items, func(i, j int) bool {
		ti := items[i].EndedAt
		if ti.IsZero() {
			ti = items[i].UpdatedAt
		}
		if ti.IsZero() {
			ti = items[i].StartedAt
		}
		tj := items[j].EndedAt
		if tj.IsZero() {
			tj = items[j].UpdatedAt
		}
		if tj.IsZero() {
			tj = items[j].StartedAt
		}
		return tj.Before(ti)
	})
}

func phaseFromTaskStatus(status string) string {
	switch status {
	case tasks.StatusInProgress:
		return "execute"
	case tasks.StatusPending:
		return "select"
	case tasks.StatusBlocked:
		return "blocked"
	case tasks.StatusReadyForReview:
		return "verify"
	case tasks.StatusCompleted:
		return "complete"
	default:
		return "orient"
	}
}

func phaseFromSession(status string) string {
	switch status {
	case storage.SessionStatusRunning:
		return "resume"
	case storage.SessionStatusError:
		return "recover"
	default:
		return "orient"
	}
}

func collectBlockers(taskList []tasks.Task, activeTaskID string, limit int) []string {
	out := make([]string, 0, limit)
	for _, task := range taskList {
		if task.Status != tasks.StatusBlocked || task.ID == activeTaskID {
			continue
		}
		out = append(out, task.Title)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func collectDecisions(items []storage.Session, limit int) []RecentDecision {
	out := make([]RecentDecision, 0, limit)
	seen := make(map[string]struct{})
	for _, session := range items {
		for i, decision := range session.Decisions {
			decision = strings.TrimSpace(decision)
			if decision == "" {
				continue
			}
			key := strings.ToLower(decision)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, RecentDecision{
				ID:   fmt.Sprintf("%s:%d", session.ID, i+1),
				Text: decision,
				Ref:  "session:" + session.ID,
			})
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func collectSessionNotes(items []storage.Session, pick func(storage.Session) []string) []string {
	var out []string
	for _, session := range items {
		out = append(out, pick(session)...)
	}
	return out
}

func collectTaskTitles(taskList []tasks.Task, activeTaskID string, statuses []string, limit int) []string {
	allowed := make(map[string]struct{}, len(statuses))
	for _, status := range statuses {
		allowed[status] = struct{}{}
	}
	out := make([]string, 0, limit)
	for _, task := range taskList {
		if task.ID == activeTaskID {
			continue
		}
		if _, ok := allowed[task.Status]; !ok {
			continue
		}
		if title := strings.TrimSpace(task.Title); title != "" {
			out = append(out, title)
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}

func buildNextActions(active tasks.Task, activeOK bool, taskList []tasks.Task) []string {
	var out []string
	if activeOK && strings.TrimSpace(active.Title) != "" {
		out = append(out, "Continue "+strings.TrimSpace(active.Title))
	}
	out = append(out, collectTaskTitles(taskList, active.ID, []string{
		tasks.StatusPending,
		tasks.StatusReadyForReview,
	}, 3)...)
	return out
}

func buildRelevantRefs(active tasks.Task, activeOK bool, sessions []storage.Session) []contextengine.EvidenceRef {
	var refs []contextengine.EvidenceRef
	if activeOK {
		if strings.TrimSpace(active.PlanFile) != "" {
			refs = append(refs, contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: strings.TrimSpace(active.PlanFile)})
		}
		if strings.TrimSpace(active.ScopePath) != "" {
			refs = append(refs, contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: strings.TrimSpace(active.ScopePath)})
		}
	}
	for _, session := range sessions {
		for _, file := range session.KeyFiles {
			file = strings.TrimSpace(file)
			if file == "" {
				continue
			}
			refs = append(refs, contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: file})
		}
		if len(refs) >= 4 {
			break
		}
	}
	if len(refs) == 0 && len(sessions) > 0 {
		refs = append(refs, contextengine.EvidenceRef{Type: contextengine.RefTypeSession, Ref: sessions[0].ID})
	}
	return refs
}

func splitNotes(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	normalized := strings.NewReplacer("\r\n", "\n", ";", "\n").Replace(raw)
	lines := strings.Split(normalized, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimPrefix(line, "• ")
		out = append(out, line)
	}
	return uniqueLimit(out, len(out))
}

func uniqueLimit(items []string, limit int) []string {
	out := make([]string, 0, limit)
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func nonEmptyStrings(items ...string) []string {
	return uniqueLimit(items, len(items))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func uniqueEvidenceRefsLimit(refs []contextengine.EvidenceRef, limit int) []contextengine.EvidenceRef {
	if len(refs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(refs))
	out := make([]contextengine.EvidenceRef, 0, limit)
	for _, ref := range refs {
		key := string(ref.Type) + ":" + ref.Ref
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}
