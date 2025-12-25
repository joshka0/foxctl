package optimization

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

// ToolHint represents a hint for tool selection based on learned patterns.
type ToolHint struct {
	// ToolName is the recommended tool.
	ToolName string `json:"tool_name"`

	// Confidence is the confidence score (0.0 to 1.0).
	Confidence float64 `json:"confidence"`

	// Reason explains why this tool is recommended.
	Reason string `json:"reason,omitempty"`

	// Sequence is the full tool sequence this came from.
	Sequence []string `json:"sequence,omitempty"`
}

// MCPPatternCollector collects and uses patterns for tool selection optimization.
type MCPPatternCollector struct {
	store     PatternStore
	trajStore trajectory.Store
}

// NewMCPPatternCollector creates a new MCP pattern collector.
func NewMCPPatternCollector(store PatternStore, trajStore trajectory.Store) *MCPPatternCollector {
	return &MCPPatternCollector{
		store:     store,
		trajStore: trajStore,
	}
}

// CollectFromTrajectory extracts patterns from a completed trajectory.
func (c *MCPPatternCollector) CollectFromTrajectory(ctx context.Context, traj trajectory.Trajectory) error {
	// Get events for this trajectory
	events, err := c.trajStore.ListEvents(ctx, trajectory.EventFilter{
		TrajectoryID: traj.ID,
		Limit:        1000,
	})
	if err != nil {
		return err
	}

	// Extract tool calls in order
	var toolSequence []string
	var contextText string
	var totalDuration int64

	for _, event := range events {
		switch event.Kind {
		case trajectory.EventKindUserRequest:
			// Extract context from the user request
			if event.DataInline != nil {
				if text, ok := event.DataInline["text"].(string); ok {
					contextText = extractContext(text)
				}
			}
		case trajectory.EventKindToolCall:
			// Extract tool name
			if event.DataInline != nil {
				if toolName, ok := event.DataInline["tool"].(string); ok {
					toolSequence = append(toolSequence, toolName)
				}
			}
		case trajectory.EventKindToolResult:
			// Extract duration if available
			if event.DataInline != nil {
				if dur, ok := event.DataInline["duration_ms"].(float64); ok {
					totalDuration += int64(dur)
				}
			}
		}
	}

	// Only record patterns with at least one tool call
	if len(toolSequence) == 0 {
		return nil
	}

	// Determine outcome
	outcome := "partial"
	switch traj.Status {
	case trajectory.StatusOK:
		outcome = "success"
	case trajectory.StatusError:
		outcome = "failure"
	}

	// If we have outcome data, use it for more accurate success determination
	if traj.Outcome != nil && traj.Outcome.Success {
		outcome = "success"
	}

	// Average duration per tool call
	avgDuration := int64(0)
	if len(toolSequence) > 0 {
		avgDuration = totalDuration / int64(len(toolSequence))
	}

	// Record the pattern
	pattern := Pattern{
		AgentRole:     traj.AgentRole,
		Context:       contextText,
		ToolSequence:  toolSequence,
		Outcome:       outcome,
		AvgDurationMS: avgDuration,
	}

	return c.store.Record(ctx, pattern)
}

// GetHints returns tool hints for a given task description.
func (c *MCPPatternCollector) GetHints(ctx context.Context, agentRole, taskDescription string) ([]ToolHint, error) {
	// Find similar patterns
	patterns, err := c.store.FindSimilar(ctx, agentRole, taskDescription, 0.3)
	if err != nil {
		return nil, err
	}

	// Also get top patterns for this agent role
	topPatterns, err := c.store.GetTopPatterns(ctx, agentRole, 5)
	if err == nil {
		// Merge, avoiding duplicates
		seen := make(map[string]bool)
		for _, p := range patterns {
			seen[p.ID] = true
		}
		for _, p := range topPatterns {
			if !seen[p.ID] {
				patterns = append(patterns, p)
			}
		}
	}

	// Convert to hints
	hints := make([]ToolHint, 0)
	toolScores := make(map[string]float64)
	toolReasons := make(map[string]string)
	toolSequences := make(map[string][]string)

	for _, p := range patterns {
		if len(p.ToolSequence) == 0 {
			continue
		}

		// Weight by success rate and count
		weight := p.SuccessRate() * (1.0 + float64(p.Count)/100.0)

		// First tool gets highest weight
		firstTool := p.ToolSequence[0]
		toolScores[firstTool] += weight
		if toolSequences[firstTool] == nil {
			toolSequences[firstTool] = p.ToolSequence
			toolReasons[firstTool] = formatReason(p)
		}

		// Subsequent tools get decreasing weight
		for i := 1; i < len(p.ToolSequence); i++ {
			tool := p.ToolSequence[i]
			toolScores[tool] += weight * (1.0 - float64(i)*0.2)
		}
	}

	// Normalize and create hints
	maxScore := 0.0
	for _, score := range toolScores {
		if score > maxScore {
			maxScore = score
		}
	}

	if maxScore > 0 {
		for tool, score := range toolScores {
			confidence := score / maxScore
			if confidence >= 0.3 { // Only include confident hints
				hints = append(hints, ToolHint{
					ToolName:   tool,
					Confidence: confidence,
					Reason:     toolReasons[tool],
					Sequence:   toolSequences[tool],
				})
			}
		}
	}

	// Sort by confidence descending
	for i := 0; i < len(hints)-1; i++ {
		for j := i + 1; j < len(hints); j++ {
			if hints[j].Confidence > hints[i].Confidence {
				hints[i], hints[j] = hints[j], hints[i]
			}
		}
	}

	// Limit to top 5 hints
	if len(hints) > 5 {
		hints = hints[:5]
	}

	return hints, nil
}

// FormatHintsForPrompt formats tool hints as a string to inject into agent prompts.
func (c *MCPPatternCollector) FormatHintsForPrompt(hints []ToolHint) string {
	if len(hints) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n## Tool Selection Hints (from learned patterns)\n\n")
	sb.WriteString("Based on similar tasks, consider using these tools:\n")

	for i, hint := range hints {
		sb.WriteString("\n")
		sb.WriteString(strings.Repeat(" ", i) + "- **" + hint.ToolName + "**")
		if hint.Confidence >= 0.8 {
			sb.WriteString(" (highly recommended)")
		} else if hint.Confidence >= 0.5 {
			sb.WriteString(" (recommended)")
		}
		if hint.Reason != "" {
			sb.WriteString(": " + hint.Reason)
		}
	}

	sb.WriteString("\n\nThese are suggestions based on past successful executions.\n")
	return sb.String()
}

// RecordToolCall records a single tool call for pattern learning.
func (c *MCPPatternCollector) RecordToolCall(ctx context.Context, agentRole, context, toolName string, success bool, durationMS int64) error {
	outcome := "failure"
	if success {
		outcome = "success"
	}

	pattern := Pattern{
		AgentRole:     agentRole,
		Context:       extractContext(context),
		ToolSequence:  []string{toolName},
		Outcome:       outcome,
		AvgDurationMS: durationMS,
	}

	return c.store.Record(ctx, pattern)
}

// extractContext extracts a simplified context from text for pattern matching.
func extractContext(text string) string {
	// Simplify the context for better matching
	// This is a basic implementation - could use NLP/embeddings for better results
	text = strings.TrimSpace(text)

	// Truncate if too long
	if len(text) > 200 {
		text = text[:200]
	}

	// Convert to lowercase for matching
	text = strings.ToLower(text)

	return text
}

// formatReason creates a human-readable reason for a pattern.
func formatReason(p Pattern) string {
	if p.Count < 3 {
		return ""
	}

	successRate := p.SuccessRate() * 100
	if successRate >= 80 {
		return "Based on " + strconv.Itoa(p.Count) + " successful uses"
	}
	return ""
}

// GetPatternStore returns the underlying pattern store.
func (c *MCPPatternCollector) GetPatternStore() PatternStore {
	return c.store
}

// CollectFromRecentTrajectories processes recent trajectories to build patterns.
func (c *MCPPatternCollector) CollectFromRecentTrajectories(ctx context.Context, workspaceID string, since time.Time) (int, error) {
	trajs, err := c.trajStore.ListTrajectories(ctx, trajectory.ListFilter{
		WorkspaceID: workspaceID,
		Since:       since,
		Limit:       100,
	})
	if err != nil {
		return 0, err
	}

	collected := 0
	for _, traj := range trajs {
		if err := c.CollectFromTrajectory(ctx, traj); err == nil {
			collected++
		}
	}

	return collected, nil
}
