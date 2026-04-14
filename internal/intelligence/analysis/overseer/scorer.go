package overseer

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/analysis/tasksgraph"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
	"github.com/joshka0/foxctl/internal/storage/tasks"
)

// Weights for the scoring formula.
const (
	WeightCriticalPath = 0.30 // α
	WeightPageRank     = 0.20 // β
	WeightAdminMail    = 0.25 // γ
	WeightOverseerMail = 0.15 // δ
	WeightRecency      = 0.10 // ε
)

// TaskScore represents a scored task with breakdown.
type TaskScore struct {
	TaskID            string  `json:"task_id"`
	Title             string  `json:"title"`
	Score             float64 `json:"score"`
	CriticalPathScore float64 `json:"critical_path_score"`
	PageRank          float64 `json:"pagerank"`
	UnreadAdmin       int     `json:"unread_admin"`
	UnreadOverseer    int     `json:"unread_overseer"`
	UnreadTotal       int     `json:"unread_total"`
	RecencyFactor     float64 `json:"recency_factor"`
	DaysSinceUpdate   float64 `json:"days_since_update"`
}

// Recommendation contains scored tasks with summary.
type Recommendation struct {
	GeneratedAt    time.Time   `json:"generated_at"`
	WorkspaceID    string      `json:"workspace_id"`
	TotalPending   int         `json:"total_pending"`
	HasCycles      bool        `json:"has_cycles"`
	Tasks          []TaskScore `json:"tasks"`
	TopRecommended *TaskScore  `json:"top_recommended,omitempty"`
}

// Scorer computes task recommendations.
type Scorer struct {
	taskStore  tasks.Store
	boardStore blackboard.BoardStore
	clock      func() time.Time
}

// NewScorer constructs a task recommendation scorer.
func NewScorer(taskStore tasks.Store, boardStore blackboard.BoardStore) *Scorer {
	return &Scorer{
		taskStore:  taskStore,
		boardStore: boardStore,
		clock:      func() time.Time { return time.Now().UTC() },
	}
}

// ScorerOption configures a Scorer.
type ScorerOption func(*Scorer)

// WithClock sets the clock function for the scorer (for testing/determinism).
func WithClock(clock func() time.Time) ScorerOption {
	return func(s *Scorer) {
		s.clock = clock
	}
}

// Recommend scores pending tasks and returns a ranked recommendation list.
//
// Index:
// - Purpose: Rank pending tasks using graph insights and mailbox signals
// - Flow: list tasks -> filter pending -> analyze graph -> count mail -> score/normalize -> sort -> limit -> return
// - SideEffects: reads task store and mailbox
// - FailureModes: task listing errors, graph analysis errors
// - Related: tasks.Store.ListByWorkspace, tasksgraph.NewAnalyzer, blackboard.BoardStore
// - Keywords: recommend, critical_path_score, pagerank, unread_admin, unread_overseer, tasksgraph.NewAnalyzer
func (s *Scorer) Recommend(ctx context.Context, workspaceID string, limit int) (*Recommendation, error) {
	// Get all tasks
	allTasks, err := s.taskStore.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	// Filter to pending tasks
	var pending []tasks.Task
	for _, t := range allTasks {
		if t.Status != "completed" && t.Status != "cancelled" {
			pending = append(pending, t)
		}
	}

	if len(pending) == 0 {
		return &Recommendation{
			GeneratedAt:  s.clock(),
			WorkspaceID:  workspaceID,
			TotalPending: 0,
			Tasks:        []TaskScore{},
		}, nil
	}

	// Get graph insights using the existing analyzer
	analyzer := tasksgraph.NewAnalyzer()
	insights, err := analyzer.Analyze(pending, workspaceID)
	if err != nil {
		return nil, err
	}

	// Build lookup maps
	nodeByID := make(map[string]tasksgraph.NodeMetrics)
	for _, n := range insights.Nodes {
		nodeByID[n.TaskID] = n
	}

	// Get mailbox message counts per task
	taskMailCounts := s.getTaskMailCounts(ctx, workspaceID, pending)

	// Normalize scores
	maxCritPath := 0.0
	maxPageRank := 0.0
	for _, n := range insights.Nodes {
		if float64(n.CriticalPathScore) > maxCritPath {
			maxCritPath = float64(n.CriticalPathScore)
		}
		if n.PageRank > maxPageRank {
			maxPageRank = n.PageRank
		}
	}

	// Find max mail counts for normalization (avoids unbounded score inflation)
	maxAdmin, maxOverseer := 0, 0
	for _, mc := range taskMailCounts {
		if mc.admin > maxAdmin {
			maxAdmin = mc.admin
		}
		if mc.overseer > maxOverseer {
			maxOverseer = mc.overseer
		}
	}

	// Compute scores for each task
	scores := make([]TaskScore, 0, len(pending))
	now := s.clock()

	for _, t := range pending {
		node, hasNode := nodeByID[t.ID]
		mailCount := taskMailCounts[t.ID]

		// Normalize critical path (0-1)
		var normCritPath float64
		if hasNode && maxCritPath > 0 {
			normCritPath = float64(node.CriticalPathScore) / maxCritPath
		}

		// Normalize PageRank (0-1)
		var normPageRank float64
		if hasNode && maxPageRank > 0 {
			normPageRank = node.PageRank / maxPageRank
		}

		// Recency factor: 1 / (1 + days_since_last_update)
		lastUpdate := t.CreatedAt
		if t.CompletedAt != nil {
			lastUpdate = *t.CompletedAt
		}
		daysSince := now.Sub(lastUpdate).Hours() / 24.0
		recencyFactor := 1.0 / (1.0 + daysSince)

		// Normalize mail counts (0-1) to avoid unbounded score inflation
		var normAdmin, normOverseer float64
		if maxAdmin > 0 {
			normAdmin = float64(mailCount.admin) / float64(maxAdmin)
		}
		if maxOverseer > 0 {
			normOverseer = float64(mailCount.overseer) / float64(maxOverseer)
		}

		// Compute weighted score (all components are now 0-1 normalized)
		score := WeightCriticalPath*normCritPath +
			WeightPageRank*normPageRank +
			WeightAdminMail*normAdmin +
			WeightOverseerMail*normOverseer +
			WeightRecency*recencyFactor

		// Cap at 1.0 as a safety measure (should be rare with normalized inputs)
		score = math.Min(score, 1.0)

		scores = append(scores, TaskScore{
			TaskID:            t.ID,
			Title:             t.Title,
			Score:             score,
			CriticalPathScore: normCritPath,
			PageRank:          normPageRank,
			UnreadAdmin:       mailCount.admin,
			UnreadOverseer:    mailCount.overseer,
			UnreadTotal:       mailCount.total,
			RecencyFactor:     recencyFactor,
			DaysSinceUpdate:   daysSince,
		})
	}

	// Sort by score descending
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score
	})

	// Apply limit
	if limit > 0 && len(scores) > limit {
		scores = scores[:limit]
	}

	rec := &Recommendation{
		GeneratedAt:  now,
		WorkspaceID:  workspaceID,
		TotalPending: len(pending),
		HasCycles:    len(insights.Cycles) > 0,
		Tasks:        scores,
	}

	if len(scores) > 0 {
		rec.TopRecommended = &scores[0]
	}

	return rec, nil
}

type mailCount struct {
	admin    int
	overseer int
	total    int
}

// getTaskMailCounts retrieves unread message counts per task from admin/overseer.
func (s *Scorer) getTaskMailCounts(ctx context.Context, workspaceID string, taskList []tasks.Task) map[string]mailCount {
	counts := make(map[string]mailCount)

	if s.boardStore == nil {
		return counts
	}

	// For each task, count unread messages by sender type
	for _, t := range taskList {
		admin, overseerCnt, total, err := s.boardStore.CountMessagesByTask(ctx, workspaceID, t.ID)
		if err != nil {
			continue
		}
		counts[t.ID] = mailCount{
			admin:    admin,
			overseer: overseerCnt,
			total:    total,
		}
	}

	return counts
}
