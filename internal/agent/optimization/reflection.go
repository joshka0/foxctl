package optimization

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

// Reflection represents an analyzed trajectory with insights.
type Reflection struct {
	// TrajectoryID is the trajectory that was analyzed.
	TrajectoryID string `json:"trajectory_id"`

	// AgentRole is the role of the agent.
	AgentRole string `json:"agent_role"`

	// Strengths lists positive aspects of the execution.
	Strengths []string `json:"strengths"`

	// Weaknesses lists areas for improvement.
	Weaknesses []string `json:"weaknesses"`

	// Suggestions lists actionable improvement suggestions.
	Suggestions []string `json:"suggestions"`

	// ToolAnalysis provides tool-specific insights.
	ToolAnalysis []ToolReflection `json:"tool_analysis,omitempty"`

	// Confidence is how confident the analysis is (0.0-1.0).
	Confidence float64 `json:"confidence"`

	// GeneratedAt is when this reflection was created.
	GeneratedAt time.Time `json:"generated_at"`
}

// ToolReflection provides reflection on a specific tool's usage.
type ToolReflection struct {
	// ToolName is the tool being analyzed.
	ToolName string `json:"tool_name"`

	// UsageCount is how many times the tool was called.
	UsageCount int `json:"usage_count"`

	// SuccessRate is the tool's success rate in this trajectory.
	SuccessRate float64 `json:"success_rate"`

	// AvgDuration is the average duration of tool calls.
	AvgDuration time.Duration `json:"avg_duration,format:units"`

	// Notes are observations about tool usage.
	Notes []string `json:"notes,omitempty"`
}

// Improvement represents a suggested improvement derived from reflections.
type Improvement struct {
	// ID is a unique identifier.
	ID string `json:"id"`

	// Category is the type of improvement (prompt, tool_selection, workflow).
	Category string `json:"category"`

	// Description explains the improvement.
	Description string `json:"description"`

	// Priority is the importance (high, medium, low).
	Priority string `json:"priority"`

	// Evidence lists trajectory IDs that support this improvement.
	Evidence []string `json:"evidence"`

	// Action describes what to do.
	Action string `json:"action"`

	// Impact is the expected impact (0.0-1.0).
	Impact float64 `json:"impact"`
}

// ReflectionConfig configures the reflection engine.
type ReflectionConfig struct {
	// MinEventsForReflection is the minimum events needed for meaningful reflection.
	MinEventsForReflection int

	// AnalyzeToolUsage enables tool-specific analysis.
	AnalyzeToolUsage bool

	// LookbackDays limits how far back to analyze.
	LookbackDays int

	// MinTrajectoriesForImprovement is the minimum reflections needed to generate improvements.
	MinTrajectoriesForImprovement int
}

// DefaultReflectionConfig returns a default configuration.
func DefaultReflectionConfig() ReflectionConfig {
	return ReflectionConfig{
		MinEventsForReflection:        5,
		AnalyzeToolUsage:              true,
		LookbackDays:                  30,
		MinTrajectoriesForImprovement: 5,
	}
}

// ReflectionEngine analyzes trajectories to generate insights and improvements.
// Implements concepts from GEPA (Generative Execution Pattern Analysis) and
// SIMBA (Self-Improving Model-Based Agents).
type ReflectionEngine struct {
	trajStore    trajectory.Store
	patternStore PatternStore
	config       ReflectionConfig
}

// NewReflectionEngine creates a new reflection engine.
func NewReflectionEngine(trajStore trajectory.Store, patternStore PatternStore, config ReflectionConfig) *ReflectionEngine {
	if config.MinEventsForReflection <= 0 {
		config.MinEventsForReflection = 5
	}
	if config.LookbackDays <= 0 {
		config.LookbackDays = 30
	}
	if config.MinTrajectoriesForImprovement <= 0 {
		config.MinTrajectoriesForImprovement = 5
	}
	return &ReflectionEngine{
		trajStore:    trajStore,
		patternStore: patternStore,
		config:       config,
	}
}

// ReflectOnTrajectory analyzes a single trajectory and generates insights.
func (r *ReflectionEngine) ReflectOnTrajectory(ctx context.Context, workspaceID, trajID string) (*Reflection, error) {
	// Get the trajectory directly by ID
	traj, err := r.trajStore.GetTrajectory(ctx, workspaceID, trajID)
	if err != nil {
		return nil, fmt.Errorf("reflection: get trajectory: %w", err)
	}

	// Get events for this trajectory
	events, err := r.trajStore.ListEvents(ctx, trajectory.EventFilter{
		TrajectoryID: trajID,
		Limit:        1000,
	})
	if err != nil {
		return nil, fmt.Errorf("reflection: list events: %w", err)
	}

	if len(events) < r.config.MinEventsForReflection {
		return nil, fmt.Errorf("reflection: insufficient events (%d < %d minimum)",
			len(events), r.config.MinEventsForReflection)
	}

	reflection := &Reflection{
		TrajectoryID: trajID,
		AgentRole:    traj.AgentRole,
		GeneratedAt:  time.Now().UTC(),
	}

	// Analyze execution flow
	r.analyzeExecutionFlow(events, reflection)

	// Analyze tool usage if enabled
	if r.config.AnalyzeToolUsage {
		reflection.ToolAnalysis = r.analyzeToolUsage(events)
	}

	// Generate insights based on outcome
	if traj.Outcome != nil {
		r.generateOutcomeInsights(traj, events, reflection)
	}

	// Compute confidence based on data quality
	reflection.Confidence = r.computeConfidence(events, traj)

	return reflection, nil
}

// analyzeExecutionFlow examines the flow of events for patterns.
func (r *ReflectionEngine) analyzeExecutionFlow(events []trajectory.Event, reflection *Reflection) {
	var (
		toolCalls      int
		errors         int
		retries        int
		thoughts       int
		hasUserInput   bool
		hasFinalResult bool
	)

	for _, event := range events {
		switch event.Kind {
		case trajectory.EventKindToolCall:
			toolCalls++
			// Check if this tool call had an error
			if event.DataInline != nil {
				if _, hasError := event.DataInline["error"]; hasError {
					errors++
				}
			}
		case trajectory.EventKindToolResult:
			// Check for errors in tool results
			if event.DataInline != nil {
				if _, hasError := event.DataInline["error"]; hasError {
					errors++
				}
				// Check for retry indication
				if isRetry, _ := event.DataInline["is_retry"].(bool); isRetry {
					retries++
				}
			}
		case trajectory.EventKindAgentThought:
			thoughts++
			// Check for result
			if event.DataInline != nil {
				if _, ok := event.DataInline["result"]; ok {
					hasFinalResult = true
				}
			}
		case trajectory.EventKindUserRequest:
			hasUserInput = true
		}
	}

	// Strengths
	if errors == 0 {
		reflection.Strengths = append(reflection.Strengths, "No errors during execution")
	}
	if retries == 0 {
		reflection.Strengths = append(reflection.Strengths, "No retries needed - efficient execution")
	}
	if hasFinalResult {
		reflection.Strengths = append(reflection.Strengths, "Produced a clear final result")
	}
	if toolCalls > 0 && toolCalls <= 5 {
		reflection.Strengths = append(reflection.Strengths, "Efficient tool usage (5 or fewer calls)")
	}
	if thoughts > 0 {
		reflection.Strengths = append(reflection.Strengths, "Demonstrated reasoning through thought events")
	}

	// Weaknesses
	if errors > 0 {
		reflection.Weaknesses = append(reflection.Weaknesses,
			fmt.Sprintf("Encountered %d error(s) during execution", errors))
	}
	if retries > 2 {
		reflection.Weaknesses = append(reflection.Weaknesses,
			fmt.Sprintf("Required %d retries - may indicate unclear instructions or tool issues", retries))
	}
	if toolCalls > 10 {
		reflection.Weaknesses = append(reflection.Weaknesses,
			fmt.Sprintf("High tool call count (%d) - may indicate inefficiency", toolCalls))
	}
	if !hasFinalResult {
		reflection.Weaknesses = append(reflection.Weaknesses,
			"No clear final result recorded")
	}
	if !hasUserInput {
		reflection.Weaknesses = append(reflection.Weaknesses,
			"Missing user request event - incomplete trajectory data")
	}

	// Suggestions based on patterns
	if errors > 0 && retries > 0 {
		reflection.Suggestions = append(reflection.Suggestions,
			"Consider adding error handling guidance to the prompt")
	}
	if toolCalls > 10 {
		reflection.Suggestions = append(reflection.Suggestions,
			"Consider breaking down complex tasks into smaller subtasks")
		reflection.Suggestions = append(reflection.Suggestions,
			"Review tool selection to ensure most appropriate tools are being used")
	}
	if thoughts == 0 {
		reflection.Suggestions = append(reflection.Suggestions,
			"Encourage more explicit reasoning through thought articulation")
	}
}

// analyzeToolUsage provides tool-specific analysis.
func (r *ReflectionEngine) analyzeToolUsage(events []trajectory.Event) []ToolReflection {
	toolStats := make(map[string]*struct {
		count      int
		successes  int
		totalDurMS int64
	})

	for _, event := range events {
		if event.Kind != trajectory.EventKindToolCall {
			continue
		}
		if event.DataInline == nil {
			continue
		}

		toolName, _ := event.DataInline["tool"].(string)
		if toolName == "" {
			continue
		}

		if toolStats[toolName] == nil {
			toolStats[toolName] = &struct {
				count      int
				successes  int
				totalDurMS int64
			}{}
		}

		toolStats[toolName].count++

		// Check for success (absence of error in result)
		if result, ok := event.DataInline["result"].(map[string]any); ok {
			if _, hasError := result["error"]; !hasError {
				toolStats[toolName].successes++
			}
		} else {
			// Assume success if no explicit error
			toolStats[toolName].successes++
		}

		// Duration if available
		if durMS, ok := event.DataInline["duration_ms"].(float64); ok {
			toolStats[toolName].totalDurMS += int64(durMS)
		}
	}

	reflections := make([]ToolReflection, 0, len(toolStats))
	for toolName, stats := range toolStats {
		tr := ToolReflection{
			ToolName:    toolName,
			UsageCount:  stats.count,
			SuccessRate: 1.0,
		}

		if stats.count > 0 {
			tr.SuccessRate = float64(stats.successes) / float64(stats.count)
			tr.AvgDuration = time.Duration(stats.totalDurMS/int64(stats.count)) * time.Millisecond
		}

		// Add notes based on analysis
		if tr.SuccessRate < 0.8 {
			tr.Notes = append(tr.Notes,
				fmt.Sprintf("Low success rate (%.1f%%) - may need better input validation", tr.SuccessRate*100))
		}
		if stats.count > 5 {
			tr.Notes = append(tr.Notes,
				fmt.Sprintf("Heavily used (%d calls) - ensure this is intentional", stats.count))
		}

		reflections = append(reflections, tr)
	}

	return reflections
}

// generateOutcomeInsights adds insights based on the trajectory outcome.
func (r *ReflectionEngine) generateOutcomeInsights(traj trajectory.Trajectory, events []trajectory.Event, reflection *Reflection) {
	outcome := traj.Outcome

	if outcome.Success {
		reflection.Strengths = append(reflection.Strengths, "Task completed successfully")

		if outcome.HumanRating != nil && *outcome.HumanRating >= 4 {
			reflection.Strengths = append(reflection.Strengths,
				fmt.Sprintf("High human rating (%d/5)", *outcome.HumanRating))
		}

		if outcome.ToolCallCount > 0 && outcome.ToolCallCount <= 3 {
			reflection.Strengths = append(reflection.Strengths,
				"Achieved goal with minimal tool calls")
		}
	} else {
		reflection.Weaknesses = append(reflection.Weaknesses, "Task did not complete successfully")

		if outcome.ErrorCount > 0 {
			reflection.Weaknesses = append(reflection.Weaknesses,
				fmt.Sprintf("Encountered %d errors", outcome.ErrorCount))
			reflection.Suggestions = append(reflection.Suggestions,
				"Analyze error patterns and add preventive guidance to prompt")
		}
	}

	if outcome.HumanRating != nil && *outcome.HumanRating < 3 {
		reflection.Weaknesses = append(reflection.Weaknesses,
			fmt.Sprintf("Low human rating (%d/5)", *outcome.HumanRating))
		if outcome.Feedback != "" {
			reflection.Suggestions = append(reflection.Suggestions,
				fmt.Sprintf("Address feedback: %s", truncateString(outcome.Feedback, 100)))
		}
	}

	// Efficiency analysis
	if outcome.DurationMS > 0 {
		duration := time.Duration(outcome.DurationMS) * time.Millisecond
		if duration > 5*time.Minute {
			reflection.Weaknesses = append(reflection.Weaknesses,
				fmt.Sprintf("Long execution time (%s)", duration.Round(time.Second)))
			reflection.Suggestions = append(reflection.Suggestions,
				"Consider optimizing for faster completion or breaking into smaller tasks")
		}
	}
}

// computeConfidence calculates confidence in the reflection.
func (r *ReflectionEngine) computeConfidence(events []trajectory.Event, traj trajectory.Trajectory) float64 {
	confidence := 0.5 // Base confidence

	// More events = more data = higher confidence
	if len(events) >= 10 {
		confidence += 0.1
	}
	if len(events) >= 20 {
		confidence += 0.1
	}

	// Outcome data increases confidence
	if traj.Outcome != nil {
		confidence += 0.1
		if traj.Outcome.HumanRating != nil {
			confidence += 0.1 // Human validation is valuable
		}
	}

	// Cap at 1.0
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// GenerateImprovements analyzes multiple reflections to generate actionable improvements.
func (r *ReflectionEngine) GenerateImprovements(ctx context.Context, workspaceID, agentRole string) ([]Improvement, error) {
	// Get recent trajectories with outcomes
	since := time.Now().AddDate(0, 0, -r.config.LookbackDays)
	trajs, err := r.trajStore.ListTrajectories(ctx, trajectory.ListFilter{
		WorkspaceID: workspaceID,
		AgentRole:   agentRole,
		Since:       since,
		Limit:       100,
	})
	if err != nil {
		return nil, fmt.Errorf("reflection: list trajectories: %w", err)
	}

	// Generate reflections
	var reflections []Reflection
	for _, traj := range trajs {
		if traj.Outcome == nil {
			continue
		}
		reflection, err := r.ReflectOnTrajectory(ctx, workspaceID, traj.ID)
		if err != nil {
			continue // Skip failed reflections
		}
		reflections = append(reflections, *reflection)
	}

	if len(reflections) < r.config.MinTrajectoriesForImprovement {
		return nil, fmt.Errorf("reflection: insufficient data (%d < %d minimum)",
			len(reflections), r.config.MinTrajectoriesForImprovement)
	}

	// Aggregate insights across reflections
	return r.aggregateImprovements(reflections), nil
}

// aggregateImprovements combines reflections into actionable improvements.
func (r *ReflectionEngine) aggregateImprovements(reflections []Reflection) []Improvement {
	improvements := make([]Improvement, 0)

	// Count recurring patterns
	weaknessCount := make(map[string]int)
	suggestionCount := make(map[string]int)
	evidenceMap := make(map[string][]string) // pattern -> trajectory IDs

	for _, ref := range reflections {
		for _, w := range ref.Weaknesses {
			weaknessCount[w]++
			evidenceMap[w] = append(evidenceMap[w], ref.TrajectoryID)
		}
		for _, s := range ref.Suggestions {
			suggestionCount[s]++
			evidenceMap[s] = append(evidenceMap[s], ref.TrajectoryID)
		}
	}

	// Generate improvements from recurring weaknesses
	for weakness, count := range weaknessCount {
		if count < 2 {
			continue // Need at least 2 occurrences
		}

		freq := float64(count) / float64(len(reflections))
		priority := "low"
		if freq > 0.5 {
			priority = "high"
		} else if freq > 0.25 {
			priority = "medium"
		}

		improvements = append(improvements, Improvement{
			ID:          fmt.Sprintf("weakness-%d", len(improvements)+1),
			Category:    categorizeWeakness(weakness),
			Description: weakness,
			Priority:    priority,
			Evidence:    evidenceMap[weakness],
			Action:      deriveAction(weakness),
			Impact:      freq,
		})
	}

	// Generate improvements from recurring suggestions
	for suggestion, count := range suggestionCount {
		if count < 2 {
			continue
		}

		freq := float64(count) / float64(len(reflections))
		priority := "low"
		if freq > 0.5 {
			priority = "high"
		} else if freq > 0.25 {
			priority = "medium"
		}

		improvements = append(improvements, Improvement{
			ID:          fmt.Sprintf("suggestion-%d", len(improvements)+1),
			Category:    categorizeSuggestion(suggestion),
			Description: suggestion,
			Priority:    priority,
			Evidence:    evidenceMap[suggestion],
			Action:      suggestion, // Suggestions are already actionable
			Impact:      freq,
		})
	}

	return improvements
}

// categorizeWeakness determines the category of a weakness.
func categorizeWeakness(weakness string) string {
	weaknessLower := strings.ToLower(weakness)
	switch {
	case strings.Contains(weaknessLower, "error"):
		return "error_handling"
	case strings.Contains(weaknessLower, "tool"):
		return "tool_selection"
	case strings.Contains(weaknessLower, "retry"):
		return "reliability"
	case strings.Contains(weaknessLower, "time") || strings.Contains(weaknessLower, "slow"):
		return "performance"
	case strings.Contains(weaknessLower, "result"):
		return "output_quality"
	default:
		return "general"
	}
}

// categorizeSuggestion determines the category of a suggestion.
func categorizeSuggestion(suggestion string) string {
	suggestionLower := strings.ToLower(suggestion)
	switch {
	case strings.Contains(suggestionLower, "prompt"):
		return "prompt"
	case strings.Contains(suggestionLower, "tool"):
		return "tool_selection"
	case strings.Contains(suggestionLower, "task") || strings.Contains(suggestionLower, "break"):
		return "workflow"
	case strings.Contains(suggestionLower, "error") || strings.Contains(suggestionLower, "handling"):
		return "error_handling"
	default:
		return "general"
	}
}

// deriveAction creates an actionable recommendation from a weakness.
func deriveAction(weakness string) string {
	weaknessLower := strings.ToLower(weakness)
	switch {
	case strings.Contains(weaknessLower, "error"):
		return "Add error handling guidance to agent prompt"
	case strings.Contains(weaknessLower, "tool call count"):
		return "Optimize tool selection in prompt or add hints"
	case strings.Contains(weaknessLower, "retry"):
		return "Improve instruction clarity to reduce retries"
	case strings.Contains(weaknessLower, "time") || strings.Contains(weaknessLower, "slow"):
		return "Break down complex tasks or optimize workflow"
	case strings.Contains(weaknessLower, "result"):
		return "Add explicit output format requirements to prompt"
	default:
		return "Review and address identified weakness"
	}
}

// truncateString truncates a string to maxLen runes (not bytes).
func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// ReflectionSummary provides an aggregate summary of reflections.
type ReflectionSummary struct {
	// TotalTrajectories analyzed.
	TotalTrajectories int `json:"total_trajectories"`

	// SuccessfulTrajectories count.
	SuccessfulTrajectories int `json:"successful_trajectories"`

	// CommonStrengths lists frequently observed strengths.
	CommonStrengths []PatternFrequency `json:"common_strengths"`

	// CommonWeaknesses lists frequently observed weaknesses.
	CommonWeaknesses []PatternFrequency `json:"common_weaknesses"`

	// TopImprovements lists the highest-priority improvements.
	TopImprovements []Improvement `json:"top_improvements"`

	// GeneratedAt is when this summary was created.
	GeneratedAt time.Time `json:"generated_at"`
}

// PatternFrequency tracks how often a pattern appears.
type PatternFrequency struct {
	Pattern   string  `json:"pattern"`
	Count     int     `json:"count"`
	Frequency float64 `json:"frequency"` // 0.0-1.0
}

// GenerateSummary creates an aggregate summary for an agent role.
func (r *ReflectionEngine) GenerateSummary(ctx context.Context, workspaceID, agentRole string) (*ReflectionSummary, error) {
	// Get recent trajectories
	since := time.Now().AddDate(0, 0, -r.config.LookbackDays)
	trajs, err := r.trajStore.ListTrajectories(ctx, trajectory.ListFilter{
		WorkspaceID: workspaceID,
		AgentRole:   agentRole,
		Since:       since,
		Limit:       100,
	})
	if err != nil {
		return nil, fmt.Errorf("reflection: list trajectories: %w", err)
	}

	summary := &ReflectionSummary{
		TotalTrajectories: len(trajs),
		GeneratedAt:       time.Now().UTC(),
	}

	strengthCount := make(map[string]int)
	weaknessCount := make(map[string]int)

	for _, traj := range trajs {
		if traj.Outcome != nil && traj.Outcome.Success {
			summary.SuccessfulTrajectories++
		}

		reflection, err := r.ReflectOnTrajectory(ctx, workspaceID, traj.ID)
		if err != nil {
			continue
		}

		for _, s := range reflection.Strengths {
			strengthCount[s]++
		}
		for _, w := range reflection.Weaknesses {
			weaknessCount[w]++
		}
	}

	// Convert to frequency lists (guard against division by zero)
	if summary.TotalTrajectories > 0 {
		for pattern, count := range strengthCount {
			summary.CommonStrengths = append(summary.CommonStrengths, PatternFrequency{
				Pattern:   pattern,
				Count:     count,
				Frequency: float64(count) / float64(summary.TotalTrajectories),
			})
		}
		for pattern, count := range weaknessCount {
			summary.CommonWeaknesses = append(summary.CommonWeaknesses, PatternFrequency{
				Pattern:   pattern,
				Count:     count,
				Frequency: float64(count) / float64(summary.TotalTrajectories),
			})
		}
	}

	// Get improvements
	improvements, err := r.GenerateImprovements(ctx, workspaceID, agentRole)
	if err == nil && len(improvements) > 0 {
		// Take top 5 by impact
		if len(improvements) > 5 {
			improvements = improvements[:5]
		}
		summary.TopImprovements = improvements
	}

	return summary, nil
}
