package optimization

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

// AgentStats aggregates statistics for an agent role.
type AgentStats struct {
	// AgentRole identifies the agent type.
	AgentRole string `json:"agent_role"`

	// TotalTrajectories is the total number of trajectories.
	TotalTrajectories int `json:"total_trajectories"`

	// SuccessfulTrajectories is the count of successful executions.
	SuccessfulTrajectories int `json:"successful_trajectories"`

	// FailedTrajectories is the count of failed executions.
	FailedTrajectories int `json:"failed_trajectories"`

	// SuccessRate is the ratio of successful to total trajectories.
	SuccessRate float64 `json:"success_rate"`

	// AvgToolCalls is the average number of tool calls per trajectory.
	AvgToolCalls float64 `json:"avg_tool_calls"`

	// AvgDurationMS is the average execution time in milliseconds.
	AvgDurationMS float64 `json:"avg_duration_ms"`

	// AvgHumanRating is the average human rating (if available).
	AvgHumanRating *float64 `json:"avg_human_rating,omitempty"`

	// TopTools lists the most frequently used tools.
	TopTools []ToolUsage `json:"top_tools,omitempty"`

	// ErrorPatterns lists common error patterns.
	ErrorPatterns []ErrorPattern `json:"error_patterns,omitempty"`

	// Period describes the time period of the stats.
	Period StatsPeriod `json:"period"`
}

// ToolUsage tracks usage statistics for a single tool.
type ToolUsage struct {
	// ToolName is the name of the tool.
	ToolName string `json:"tool_name"`

	// Count is the total number of invocations.
	Count int `json:"count"`

	// SuccessCount is the number of successful invocations.
	SuccessCount int `json:"success_count"`

	// SuccessRate is the ratio of successful to total invocations.
	SuccessRate float64 `json:"success_rate"`

	// AvgDurationMS is the average execution time in milliseconds.
	AvgDurationMS float64 `json:"avg_duration_ms"`
}

// ErrorPattern represents a recurring error pattern.
type ErrorPattern struct {
	// Description describes the error pattern.
	Description string `json:"description"`

	// Count is the number of occurrences.
	Count int `json:"count"`

	// LastSeen is when the error was last observed.
	LastSeen time.Time `json:"last_seen"`
}

// StatsPeriod describes the time period for statistics.
type StatsPeriod struct {
	// Since is the start of the period.
	Since time.Time `json:"since"`

	// Until is the end of the period.
	Until time.Time `json:"until"`
}

// StatsAggregator computes aggregate statistics from trajectory data.
type StatsAggregator struct {
	trajStore    trajectory.Store
	patternStore PatternStore
}

// NewStatsAggregator creates a new statistics aggregator.
func NewStatsAggregator(trajStore trajectory.Store, patternStore PatternStore) *StatsAggregator {
	return &StatsAggregator{
		trajStore:    trajStore,
		patternStore: patternStore,
	}
}

// GetAgentStats computes aggregate statistics for an agent role.
func (s *StatsAggregator) GetAgentStats(ctx context.Context, workspaceID, agentRole string, since, until time.Time) (*AgentStats, error) {
	// Get all trajectories for this agent role in the time period
	trajs, err := s.trajStore.ListTrajectories(ctx, trajectory.ListFilter{
		WorkspaceID: workspaceID,
		AgentRole:   agentRole,
		Since:       since,
		Until:       until,
		Limit:       10000, // Get a large sample
	})
	if err != nil {
		return nil, fmt.Errorf("stats: list trajectories: %w", err)
	}

	stats := &AgentStats{
		AgentRole: agentRole,
		Period: StatsPeriod{
			Since: since,
			Until: until,
		},
	}

	if len(trajs) == 0 {
		return stats, nil
	}

	stats.TotalTrajectories = len(trajs)

	var totalToolCalls int
	var totalDurationMS int64
	var totalRating int
	var ratedCount int
	toolUsage := make(map[string]*ToolUsage)

	for _, traj := range trajs {
		// Count successes/failures based on outcome
		if traj.Outcome != nil {
			if traj.Outcome.Success {
				stats.SuccessfulTrajectories++
			} else {
				stats.FailedTrajectories++
			}
			totalToolCalls += traj.Outcome.ToolCallCount
			totalDurationMS += traj.Outcome.DurationMS

			if traj.Outcome.HumanRating != nil {
				totalRating += *traj.Outcome.HumanRating
				ratedCount++
			}
		} else {
			// Fallback to status-based counting
			switch traj.Status {
			case trajectory.StatusOK:
				stats.SuccessfulTrajectories++
			case trajectory.StatusError, trajectory.StatusAborted:
				stats.FailedTrajectories++
			}
		}
	}

	// Compute averages
	if stats.TotalTrajectories > 0 {
		stats.SuccessRate = float64(stats.SuccessfulTrajectories) / float64(stats.TotalTrajectories)
		stats.AvgToolCalls = float64(totalToolCalls) / float64(stats.TotalTrajectories)
		stats.AvgDurationMS = float64(totalDurationMS) / float64(stats.TotalTrajectories)
	}

	if ratedCount > 0 {
		avgRating := float64(totalRating) / float64(ratedCount)
		stats.AvgHumanRating = &avgRating
	}

	// Get tool usage from patterns
	patterns, err := s.patternStore.List(ctx, agentRole, 100)
	if err == nil && len(patterns) > 0 {
		for _, p := range patterns {
			for _, tool := range p.ToolSequence {
				if toolUsage[tool] == nil {
					toolUsage[tool] = &ToolUsage{ToolName: tool}
				}
				toolUsage[tool].Count += p.Count
				toolUsage[tool].SuccessCount += p.SuccessCount
			}
		}

		// Convert to slice and sort by count
		for _, tu := range toolUsage {
			if tu.Count > 0 {
				tu.SuccessRate = float64(tu.SuccessCount) / float64(tu.Count)
			}
			stats.TopTools = append(stats.TopTools, *tu)
		}
		sort.Slice(stats.TopTools, func(i, j int) bool {
			return stats.TopTools[i].Count > stats.TopTools[j].Count
		})

		// Keep only top 10 tools
		if len(stats.TopTools) > 10 {
			stats.TopTools = stats.TopTools[:10]
		}
	}

	return stats, nil
}

// GetToolStats returns detailed statistics for a specific tool.
func (s *StatsAggregator) GetToolStats(ctx context.Context, agentRole, toolName string) (*ToolUsage, error) {
	patterns, err := s.patternStore.List(ctx, agentRole, 1000)
	if err != nil {
		return nil, fmt.Errorf("stats: list patterns: %w", err)
	}

	tool := &ToolUsage{ToolName: toolName}
	var totalDuration int64
	var durationCount int

	for _, p := range patterns {
		for _, t := range p.ToolSequence {
			if t == toolName {
				tool.Count += p.Count
				tool.SuccessCount += p.SuccessCount
				if p.AvgDurationMS > 0 {
					totalDuration += p.AvgDurationMS * int64(p.Count)
					durationCount += p.Count
				}
				break // Count each pattern once per tool
			}
		}
	}

	if tool.Count > 0 {
		tool.SuccessRate = float64(tool.SuccessCount) / float64(tool.Count)
	}
	if durationCount > 0 {
		tool.AvgDurationMS = float64(totalDuration) / float64(durationCount)
	}

	return tool, nil
}

// CompareAgentRoles compares statistics across different agent roles.
func (s *StatsAggregator) CompareAgentRoles(ctx context.Context, workspaceID string, roles []string, since, until time.Time) (map[string]*AgentStats, error) {
	result := make(map[string]*AgentStats)

	for _, role := range roles {
		stats, err := s.GetAgentStats(ctx, workspaceID, role, since, until)
		if err != nil {
			return nil, fmt.Errorf("stats: get stats for %s: %w", role, err)
		}
		result[role] = stats
	}

	return result, nil
}

// IdentifyImprovementAreas analyzes statistics to identify areas for improvement.
func (s *StatsAggregator) IdentifyImprovementAreas(ctx context.Context, workspaceID, agentRole string) ([]ImprovementArea, error) {
	// Get recent stats (last 7 days)
	until := time.Now()
	since := until.AddDate(0, 0, -7)

	stats, err := s.GetAgentStats(ctx, workspaceID, agentRole, since, until)
	if err != nil {
		return nil, fmt.Errorf("stats: get agent stats: %w", err)
	}

	areas := make([]ImprovementArea, 0)

	// Low success rate
	if stats.TotalTrajectories >= 5 && stats.SuccessRate < 0.7 {
		areas = append(areas, ImprovementArea{
			Area:        "success_rate",
			Priority:    "high",
			Description: fmt.Sprintf("Success rate is %.1f%%, below target of 70%%", stats.SuccessRate*100),
			Metric:      stats.SuccessRate,
			Target:      0.7,
		})
	}

	// Low human rating
	if stats.AvgHumanRating != nil && *stats.AvgHumanRating < 3.5 {
		areas = append(areas, ImprovementArea{
			Area:        "human_rating",
			Priority:    "high",
			Description: fmt.Sprintf("Average human rating is %.1f, below target of 3.5", *stats.AvgHumanRating),
			Metric:      *stats.AvgHumanRating,
			Target:      3.5,
		})
	}

	// High tool call count (inefficiency)
	if stats.AvgToolCalls > 20 {
		areas = append(areas, ImprovementArea{
			Area:        "efficiency",
			Priority:    "medium",
			Description: fmt.Sprintf("Average of %.1f tool calls per task suggests inefficiency", stats.AvgToolCalls),
			Metric:      stats.AvgToolCalls,
			Target:      15,
		})
	}

	// Identify low-performing tools
	for _, tool := range stats.TopTools {
		if tool.Count >= 10 && tool.SuccessRate < 0.5 {
			areas = append(areas, ImprovementArea{
				Area:        fmt.Sprintf("tool_%s", tool.ToolName),
				Priority:    "medium",
				Description: fmt.Sprintf("Tool '%s' has %.1f%% success rate", tool.ToolName, tool.SuccessRate*100),
				Metric:      tool.SuccessRate,
				Target:      0.7,
			})
		}
	}

	return areas, nil
}

// ImprovementArea identifies an area that could be improved.
type ImprovementArea struct {
	// Area identifies what needs improvement.
	Area string `json:"area"`

	// Priority indicates urgency (high, medium, low).
	Priority string `json:"priority"`

	// Description explains the issue.
	Description string `json:"description"`

	// Metric is the current value.
	Metric float64 `json:"metric"`

	// Target is the target value.
	Target float64 `json:"target"`
}
