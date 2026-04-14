// Package main implements the todo/continuation skill for analyzing task dependencies and generating continuation prompts.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/mathutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/sliceutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/stringutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/workspaceutil"
	"github.com/jkatigb/agentctl/internal/context/sessionkit"
	"github.com/jkatigb/agentctl/internal/intelligence/analysis/tasksgraph"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
)

// input defines the skill input parameters for todo continuation analysis with workspace and session targeting.
type input struct {
	WorkspaceID           string `json:"workspace_id"`
	SessionID             string `json:"session_id"`
	TopN                  int    `json:"top_n"`
	MinPending            int    `json:"min_pending"`
	AnchorGoal            string `json:"anchor_goal"`
	AnchorPending         string `json:"anchor_pending"`
	IncludeExecutionOrder *bool  `json:"include_execution_order"`
}

// output defines the skill output with comprehensive task analysis and continuation guidance.
type output struct {
	ShouldContinue          bool       `json:"should_continue"`
	Prompt                  string     `json:"prompt"`
	SessionID               string     `json:"session_id,omitempty"`
	IncompleteCount         int        `json:"incomplete_count"`
	UnscopedIncompleteCount int        `json:"unscoped_incomplete_count"`
	ReadyCount              int        `json:"ready_count"`
	BlockedCount            int        `json:"blocked_count"`
	InProgressCount         int        `json:"in_progress_count"`
	CycleCount              int        `json:"cycle_count"`
	Cycles                  [][]string `json:"cycles"`
	TopologicalOrder        []string   `json:"topological_order"`
	Summary                 string     `json:"summary"`
}

// blocked represents a task with its blocking dependencies for analysis and reporting.
type blocked struct {
	Task     tasks.Task
	Blockers []string
}

// cachedInsights represents cached task graph insights with hash validation for performance optimization.
type cachedInsights struct {
	Hash     string              `json:"hash"`
	Insights tasksgraph.Insights `json:"insights"`
}

const command = "todo/continuation"

// main is the skill entry point for todo/continuation with task dependency analysis capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates todo continuation analysis with dependency graph computation and prompt generation.
//
// Index:
// - Purpose: Analyze task dependencies and generate continuation prompts with cycle detection and execution ordering
// - Flow: validate input → open task store → analyze incomplete tasks → compute insights → generate prompt → emit results
// - SideEffects: reads task store; computes dependency graphs; caches insights; generates continuation prompts
// - FailureModes: task store access failures, dependency analysis errors, cache I/O failures, prompt generation errors
// - Observability: emits task counts, dependency cycles, execution order, ready tasks, and comprehensive continuation guidance
// - Related: runContinuation, buildPrompt, loadOrComputeInsights, computeTasksHash
// - Keywords: todo/continuation, task_dependencies, cycle_detection, execution_order, continuation_prompt
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Default workspace
	in.WorkspaceID = workspaceutil.Resolve(in.WorkspaceID, "", rc.Workspace)

	// Resolve session ID
	in.SessionID = sessionkit.ResolveSessionID(in.WorkspaceID, in.SessionID)

	// Apply defaults
	if in.TopN <= 0 {
		in.TopN = 5
	}
	if in.MinPending <= 0 {
		in.MinPending = 1
	}
	includeExecutionOrder := true
	if in.IncludeExecutionOrder != nil {
		includeExecutionOrder = *in.IncludeExecutionOrder
	}

	// Get paths from sessionkit
	paths := sessionkit.ResolvePaths(rc.Config)

	// Open task store
	store, err := rc.Stores.Tasks(ctx)
	if err != nil {
		return skillerr.IO("open task store", skillerr.WithCause(err))
	}

	out, err := runContinuation(ctx, paths.CachePath, store, in, includeExecutionOrder)
	if err != nil {
		return skillerr.Runtime("continuation analysis", skillerr.WithCause(err))
	}

	return skillout.Emit(rc, command, out)
}

// runContinuation performs detailed task analysis with dependency resolution and prompt generation.
func runContinuation(ctx context.Context, cachePath string, store tasks.Store, in input, includeExecutionOrder bool) (output, error) {
	allTasks, err := store.ListByWorkspace(ctx, in.WorkspaceID)
	if err != nil {
		return output{}, err
	}

	sid := strings.TrimSpace(in.SessionID)

	completed := make(map[string]bool)
	incomplete := make([]tasks.Task, 0, len(allTasks))
	idToTitle := make(map[string]string, len(allTasks))
	unscopedIncompleteCount := 0
	for _, t := range allTasks {
		idToTitle[t.ID] = t.Title
		if t.Status == tasks.StatusCompleted {
			completed[t.ID] = true
			continue
		}

		isOpen := t.Status == tasks.StatusPending || t.Status == tasks.StatusInProgress || t.Status == tasks.StatusBlocked
		if !isOpen {
			continue
		}

		if sid != "" {
			taskSID := strings.TrimSpace(t.SessionID)
			if taskSID == "" {
				unscopedIncompleteCount++
				continue
			}
			if taskSID != sid {
				continue
			}
		}

		incomplete = append(incomplete, t)
	}

	pending := make([]tasks.Task, 0, len(incomplete))
	inProgress := make([]tasks.Task, 0, len(incomplete))
	blockedStatus := make([]tasks.Task, 0, len(incomplete))
	for _, t := range incomplete {
		switch t.Status {
		case tasks.StatusPending:
			pending = append(pending, t)
		case tasks.StatusInProgress:
			inProgress = append(inProgress, t)
		case tasks.StatusBlocked:
			blockedStatus = append(blockedStatus, t)
		}
	}

	trimmedPending := strings.TrimSpace(in.AnchorPending)
	shouldContinue := len(incomplete) >= in.MinPending || trimmedPending != ""
	if !shouldContinue {
		readyCount := 0
		blockedCount := len(blockedStatus)
		for _, t := range pending {
			hasBlocker := false
			for _, dep := range t.DependsOn {
				if completed[dep] {
					continue
				}
				hasBlocker = true
				break
			}
			if hasBlocker {
				blockedCount++
				continue
			}
			readyCount++
		}

		return output{
			ShouldContinue:          false,
			Prompt:                  "",
			SessionID:               sid,
			IncompleteCount:         len(incomplete),
			UnscopedIncompleteCount: unscopedIncompleteCount,
			ReadyCount:              readyCount,
			BlockedCount:            blockedCount,
			InProgressCount:         len(inProgress),
			CycleCount:              0,
			Cycles:                  [][]string{},
			TopologicalOrder:        []string{},
			Summary:                 fmt.Sprintf("incomplete=%d ready=%d blocked=%d in_progress=%d unscoped=%d", len(incomplete), readyCount, blockedCount, len(inProgress), unscopedIncompleteCount),
		}, nil
	}

	insights, err := loadOrComputeInsights(cachePath, in.WorkspaceID, sid, incomplete)
	if err != nil {
		return output{}, err
	}

	metricsByID := make(map[string]tasksgraph.NodeMetrics, len(insights.Nodes))
	for _, n := range insights.Nodes {
		metricsByID[n.TaskID] = n
	}

	ready := make([]tasks.Task, 0, len(pending))
	blockedTasks := make([]blocked, 0, len(pending)+len(blockedStatus))
	for _, t := range pending {
		blockerIDs := make([]string, 0, len(t.DependsOn))
		blockerTitles := make([]string, 0, len(t.DependsOn))
		for _, dep := range t.DependsOn {
			if completed[dep] {
				continue
			}
			blockerIDs = append(blockerIDs, dep)
			if title := strings.TrimSpace(idToTitle[dep]); title != "" {
				blockerTitles = append(blockerTitles, title)
			} else {
				blockerTitles = append(blockerTitles, dep)
			}
		}
		if len(blockerIDs) == 0 {
			ready = append(ready, t)
		} else {
			blockedTasks = append(blockedTasks, blocked{Task: t, Blockers: blockerTitles})
		}
	}
	for _, t := range blockedStatus {
		blockerTitles := make([]string, 0, len(t.DependsOn))
		for _, dep := range t.DependsOn {
			if completed[dep] {
				continue
			}
			if title := strings.TrimSpace(idToTitle[dep]); title != "" {
				blockerTitles = append(blockerTitles, title)
			} else {
				blockerTitles = append(blockerTitles, dep)
			}
		}
		if len(blockerTitles) == 0 {
			blockerTitles = []string{"(unspecified)"}
		}
		blockedTasks = append(blockedTasks, blocked{Task: t, Blockers: blockerTitles})
	}

	sort.Slice(ready, func(i, j int) bool {
		return metricsByID[ready[i].ID].PageRank > metricsByID[ready[j].ID].PageRank
	})
	sort.Slice(blockedTasks, func(i, j int) bool {
		return metricsByID[blockedTasks[i].Task.ID].PageRank > metricsByID[blockedTasks[j].Task.ID].PageRank
	})
	sort.Slice(inProgress, func(i, j int) bool {
		return metricsByID[inProgress[i].ID].PageRank > metricsByID[inProgress[j].ID].PageRank
	})

	prompt := buildPrompt(promptInput{
		AnchorGoal:              strings.TrimSpace(in.AnchorGoal),
		AnchorPending:           trimmedPending,
		SessionID:               sid,
		UnscopedIncompleteCount: unscopedIncompleteCount,
		IncompleteCount:         len(incomplete),
		Ready:                   ready,
		Blocked:                 blockedTasks,
		InProgress:              inProgress,
		TopN:                    in.TopN,
		Insights:                insights,
		MetricsByID:             metricsByID,
		IDToTitle:               idToTitle,
		IncludeExecutionOrd:     includeExecutionOrder,
	})

	out := output{
		ShouldContinue:          shouldContinue,
		Prompt:                  prompt,
		SessionID:               sid,
		IncompleteCount:         len(incomplete),
		UnscopedIncompleteCount: unscopedIncompleteCount,
		ReadyCount:              len(ready),
		BlockedCount:            len(blockedTasks),
		InProgressCount:         len(inProgress),
		CycleCount:              len(insights.Cycles),
		Cycles:                  ensure2D(insights.Cycles),
		TopologicalOrder:        sliceutil.Clone(insights.TopologicalOrder),
		Summary:                 fmt.Sprintf("incomplete=%d ready=%d blocked=%d in_progress=%d unscoped=%d", len(incomplete), len(ready), len(blockedTasks), len(inProgress), unscopedIncompleteCount),
	}
	return out, nil
}

// promptInput encapsulates all data needed for building the continuation prompt with task metrics.
type promptInput struct {
	AnchorGoal              string
	AnchorPending           string
	SessionID               string
	UnscopedIncompleteCount int
	IncompleteCount         int
	Ready                   []tasks.Task
	Blocked                 []blocked
	InProgress              []tasks.Task
	TopN                    int
	Insights                tasksgraph.Insights
	MetricsByID             map[string]tasksgraph.NodeMetrics
	IDToTitle               map[string]string
	IncludeExecutionOrd     bool
}

// buildPrompt generates a comprehensive continuation prompt with task prioritization and dependency information.
func buildPrompt(in promptInput) string {
	type taskGroup struct {
		Task     tasks.Task
		Count    int
		PageRank float64
		InDegree int
	}
	type blockedGroup struct {
		Task     tasks.Task
		Count    int
		PageRank float64
		Blockers []string
	}

	groupTasks := func(items []tasks.Task) []taskGroup {
		groups := make(map[string]*taskGroup)
		for _, t := range items {
			title := strings.TrimSpace(t.Title)
			key := strings.ToLower(title)
			if key == "" {
				key = t.ID
			}
			m := in.MetricsByID[t.ID]
			g := groups[key]
			if g == nil {
				groups[key] = &taskGroup{Task: t, Count: 1, PageRank: m.PageRank, InDegree: m.InDegree}
				continue
			}
			g.Count++
			if m.PageRank > g.PageRank {
				g.Task = t
				g.PageRank = m.PageRank
				g.InDegree = m.InDegree
			}
		}

		out := make([]taskGroup, 0, len(groups))
		for _, g := range groups {
			out = append(out, *g)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].PageRank > out[j].PageRank })
		return out
	}

	groupBlocked := func(items []blocked) []blockedGroup {
		groups := make(map[string]*blockedGroup)
		for _, b := range items {
			title := strings.TrimSpace(b.Task.Title)
			key := strings.ToLower(title)
			if key == "" {
				key = b.Task.ID
			}
			m := in.MetricsByID[b.Task.ID]
			g := groups[key]
			if g == nil {
				groups[key] = &blockedGroup{Task: b.Task, Count: 1, PageRank: m.PageRank, Blockers: stringutil.NormalizeStrings(b.Blockers)}
				continue
			}
			g.Count++
			g.Blockers = append(g.Blockers, stringutil.NormalizeStrings(b.Blockers)...)
			if m.PageRank > g.PageRank {
				g.Task = b.Task
				g.PageRank = m.PageRank
			}
		}

		out := make([]blockedGroup, 0, len(groups))
		for _, g := range groups {
			seen := make(map[string]bool)
			unique := make([]string, 0, len(g.Blockers))
			for _, s := range g.Blockers {
				if s == "" {
					continue
				}
				if seen[s] {
					continue
				}
				seen[s] = true
				unique = append(unique, s)
			}
			sort.Strings(unique)
			g.Blockers = unique
			out = append(out, *g)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].PageRank > out[j].PageRank })
		return out
	}

	readyGroups := groupTasks(in.Ready)
	blockedGroups := groupBlocked(in.Blocked)
	inProgressGroups := groupTasks(in.InProgress)

	prompt := fmt.Sprintf(
		"[SYSTEM REMINDER - TODO CONTINUATION]\n\nIncomplete tasks: %d (%d ready/%d unique, %d blocked/%d unique, %d in progress/%d unique)",
		in.IncompleteCount,
		len(in.Ready),
		len(readyGroups),
		len(in.Blocked),
		len(blockedGroups),
		len(in.InProgress),
		len(inProgressGroups),
	)

	if strings.TrimSpace(in.SessionID) != "" {
		prompt += "\n\nSession ID: " + strings.TrimSpace(in.SessionID)
	}
	if strings.TrimSpace(in.SessionID) != "" && in.UnscopedIncompleteCount > 0 {
		prompt += fmt.Sprintf("\n\nWARNING: %d incomplete tasks in this workspace have no session_id (ignored for this session).", in.UnscopedIncompleteCount)
	}

	if len(in.Insights.Cycles) > 0 {
		cycle := in.Insights.Cycles[0]
		if len(cycle) > 0 {
			prompt += "\n\n**CYCLE DETECTED**: " + strings.Join(cycle, " -> ") + "\nThis circular dependency must be resolved before continuing."
		}
	}

	if in.AnchorGoal != "" {
		prompt += "\n\n**Goal:** " + in.AnchorGoal
	}
	if in.AnchorPending != "" {
		prompt += "\n\n**Pending:** " + in.AnchorPending
	}

	if len(readyGroups) > 0 {
		limit := mathutil.MinInt(in.TopN, len(readyGroups))
		lines := make([]string, 0, limit)
		for i := 0; i < limit; i++ {
			g := readyGroups[i]
			title := strings.TrimSpace(g.Task.Title)
			if title == "" {
				title = g.Task.ID
			}
			suffix := ""
			if g.Count > 1 {
				suffix = fmt.Sprintf(" (x%d)", g.Count)
			}
			lines = append(lines, fmt.Sprintf("  %d. %s%s\n     pagerank=%.4f | unblocks=%d tasks", i+1, title, suffix, g.PageRank, g.InDegree))
		}
		prompt += "\n\n**READY TO START** (sorted by impact):\n" + strings.Join(lines, "\n")
	}

	if len(blockedGroups) > 0 {
		limit := 3
		if len(blockedGroups) < limit {
			limit = len(blockedGroups)
		}
		lines := make([]string, 0, limit)
		for i := 0; i < limit; i++ {
			g := blockedGroups[i]
			title := strings.TrimSpace(g.Task.Title)
			if title == "" {
				title = g.Task.ID
			}
			suffix := ""
			if g.Count > 1 {
				suffix = fmt.Sprintf(" (x%d)", g.Count)
			}
			blockers := strings.Join(g.Blockers, ", ")
			if blockers == "" {
				blockers = "(unknown)"
			}
			lines = append(lines, fmt.Sprintf("  - %s%s\n    blocked by: %s", title, suffix, blockers))
		}
		prompt += "\n\n**BLOCKED** (waiting on dependencies):\n" + strings.Join(lines, "\n")
	}

	if len(inProgressGroups) > 0 {
		limit := 3
		if len(inProgressGroups) < limit {
			limit = len(inProgressGroups)
		}
		lines := make([]string, 0, limit)
		for i := 0; i < limit; i++ {
			g := inProgressGroups[i]
			title := strings.TrimSpace(g.Task.Title)
			if title == "" {
				title = g.Task.ID
			}
			suffix := ""
			if g.Count > 1 {
				suffix = fmt.Sprintf(" (x%d)", g.Count)
			}
			lines = append(lines, "  - "+title+suffix)
		}
		prompt += "\n\n**IN PROGRESS** (complete these first):\n" + strings.Join(lines, "\n")
	}

	if in.IncludeExecutionOrd && len(in.Insights.TopologicalOrder) > 0 {
		order := in.Insights.TopologicalOrder
		max := 50
		if len(order) > max {
			order = append(order[:max], "...")
		}
		mapped := make([]string, 0, len(order))
		for _, id := range order {
			mapped = append(mapped, strings.TrimSpace(in.IDToTitle[id]))
			if mapped[len(mapped)-1] == "" {
				mapped[len(mapped)-1] = id
			}
		}
		prompt += "\n\n**Execution Order**: " + strings.Join(mapped, " -> ")
	}

	prompt += "\n\nContinue with ready tasks first."
	prompt += "\n- Proceed without asking for permission"
	prompt += "\n- Mark each task complete when finished"
	prompt += "\n- Do not stop until all tasks are done"
	return prompt
}

// ensure2D ensures a 2D string slice is properly initialized with defensive copying.
func ensure2D(in [][]string) [][]string {
	if in == nil {
		return [][]string{}
	}
	out := make([][]string, 0, len(in))
	for _, row := range in {
		out = append(out, sliceutil.Clone(row))
	}
	return out
}

// loadOrComputeInsights loads cached task graph insights or computes them fresh with hash validation.
func loadOrComputeInsights(cacheDir string, workspaceID, sessionID string, taskList []tasks.Task) (tasksgraph.Insights, error) {
	if len(taskList) == 0 {
		return tasksgraph.NewAnalyzer().Analyze(taskList, workspaceID)
	}

	hash := computeTasksHash(taskList)
	if hash == "" {
		return tasksgraph.NewAnalyzer().Analyze(taskList, workspaceID)
	}

	cachePath := cacheInsightsPath(cacheDir, workspaceID, sessionID)
	if cachePath != "" {
		if cached, ok := readInsightsCache(cachePath, hash); ok {
			return cached, nil
		}
	}

	insights, err := tasksgraph.NewAnalyzer().Analyze(taskList, workspaceID)
	if err != nil {
		return tasksgraph.Insights{}, err
	}

	if cachePath != "" {
		writeInsightsCache(cachePath, hash, insights)
	}

	return insights, nil
}

// cacheInsightsPath generates the cache file path for task insights based on workspace and session.
func cacheInsightsPath(cacheDir string, workspaceID, sessionID string) string {
	if strings.TrimSpace(cacheDir) == "" {
		return ""
	}
	workspaceID = strings.TrimSpace(workspaceID)
	sessionID = strings.TrimSpace(sessionID)
	key := fmt.Sprintf("%s|%s", workspaceID, sessionID)
	sum := sha256.Sum256([]byte(key))
	dir := filepath.Join(cacheDir, "todo-continuation")
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".json")
}

// readInsightsCache reads and validates cached task insights with hash verification.
// It returns the cached insights and a boolean indicating whether the cache was valid.
// If the cache file does not exist or the hash does not match, it returns an empty insights object and false.
func readInsightsCache(path, hash string) (tasksgraph.Insights, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return tasksgraph.Insights{}, false
	}
	var cached cachedInsights
	if err := json.Unmarshal(data, &cached); err != nil {
		return tasksgraph.Insights{}, false
	}
	if cached.Hash != hash {
		return tasksgraph.Insights{}, false
	}
	return cached.Insights, true
}

// writeInsightsCache writes task insights to cache with hash validation for future retrieval.
// It writes the insights to the cache file and returns without error.
// If the cache directory does not exist, it creates it with the correct permissions.
func writeInsightsCache(path, hash string, insights tasksgraph.Insights) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	payload := cachedInsights{Hash: hash, Insights: insights}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// computeTasksHash generates a deterministic hash of task list for cache validation and change detection.
func computeTasksHash(taskList []tasks.Task) string {
	if len(taskList) == 0 {
		return ""
	}
	ordered := make([]tasks.Task, len(taskList))
	copy(ordered, taskList)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].ID < ordered[j].ID
	})
	hasher := sha256.New()
	for _, t := range ordered {
		deps := append([]string(nil), t.DependsOn...)
		sort.Strings(deps)
		hasher.Write([]byte(t.ID))
		hasher.Write([]byte{0})
		hasher.Write([]byte(t.Status))
		hasher.Write([]byte{0})
		hasher.Write([]byte(t.Title))
		hasher.Write([]byte{0})
		hasher.Write([]byte(t.SessionID))
		hasher.Write([]byte{0})
		for _, dep := range deps {
			hasher.Write([]byte(dep))
			hasher.Write([]byte{0})
		}
		hasher.Write([]byte{'\n'})
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil))
}
