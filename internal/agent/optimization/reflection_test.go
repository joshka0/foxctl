package optimization_test

import (
	"context"
	"testing"

	"github.com/jkatigb/agentctl/internal/agent/optimization"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

func TestDefaultReflectionConfig(t *testing.T) {
	config := optimization.DefaultReflectionConfig()

	if config.MinEventsForReflection <= 0 {
		t.Error("MinEventsForReflection should be positive")
	}
	if config.LookbackDays <= 0 {
		t.Error("LookbackDays should be positive")
	}
	if config.MinTrajectoriesForImprovement <= 0 {
		t.Error("MinTrajectoriesForImprovement should be positive")
	}
}

func TestReflectionEngine_ReflectOnTrajectory(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	// Create trajectory with events
	traj := trajectory.Trajectory{
		WorkspaceID: "ws-test",
		AgentRole:   "coder",
		Status:      trajectory.StatusOK,
		Outcome: &trajectory.Outcome{
			Success:       true,
			ToolCallCount: 5,
			DurationMS:    30000,
		},
	}
	inserted, err := trajStore.InsertTrajectory(ctx, traj)
	if err != nil {
		t.Fatalf("insert trajectory: %v", err)
	}

	// Add events
	events := []trajectory.Event{
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindUserRequest, DataInline: map[string]any{"text": "Fix the bug"}},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindAgentThought, DataInline: map[string]any{"thought": "Let me analyze"}},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolCall, DataInline: map[string]any{"tool": "grep"}},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolResult, DataInline: map[string]any{"result": "found match"}},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolCall, DataInline: map[string]any{"tool": "edit"}},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolResult, DataInline: map[string]any{"result": "file updated"}},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindAgentThought, DataInline: map[string]any{"result": "Bug fixed"}},
	}
	for i, event := range events {
		if _, err := trajStore.InsertEvent(ctx, event); err != nil {
			t.Fatalf("insert event %d: %v", i, err)
		}
	}

	config := optimization.ReflectionConfig{
		MinEventsForReflection: 3,
		AnalyzeToolUsage:       true,
		LookbackDays:           30,
	}
	engine := optimization.NewReflectionEngine(trajStore, patternStore, config)

	reflection, err := engine.ReflectOnTrajectory(ctx, "ws-test", inserted.ID)
	if err != nil {
		t.Fatalf("reflect: %v", err)
	}

	if reflection == nil {
		t.Fatal("expected reflection, got nil")
		return
	}

	if reflection.TrajectoryID != inserted.ID {
		t.Error("trajectory ID mismatch")
	}
	if reflection.AgentRole != "coder" {
		t.Error("agent role mismatch")
	}
	if reflection.Confidence <= 0 {
		t.Error("confidence should be positive")
	}
}

func TestReflectionEngine_ReflectWithErrors(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	// Create trajectory with errors
	traj := trajectory.Trajectory{
		WorkspaceID: "ws-test",
		AgentRole:   "coder",
		Status:      trajectory.StatusOK,
		Outcome: &trajectory.Outcome{
			Success:    false,
			ErrorCount: 3,
		},
	}
	inserted, err := trajStore.InsertTrajectory(ctx, traj)
	if err != nil {
		t.Fatalf("insert trajectory: %v", err)
	}

	// Add events including errors
	events := []trajectory.Event{
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindUserRequest, DataInline: map[string]any{"text": "Fix bug"}},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolCall, DataInline: map[string]any{"tool": "edit"}},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolResult, DataInline: map[string]any{"error": "file not found"}},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolCall, DataInline: map[string]any{"tool": "edit", "error": "permission denied"}},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolResult, DataInline: map[string]any{"error": "syntax error", "is_retry": true}},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindAgentThought, DataInline: map[string]any{"thought": "Failed"}},
	}
	for i, event := range events {
		if _, err := trajStore.InsertEvent(ctx, event); err != nil {
			t.Fatalf("insert event %d: %v", i, err)
		}
	}

	config := optimization.ReflectionConfig{
		MinEventsForReflection: 3,
		AnalyzeToolUsage:       true,
		LookbackDays:           30,
	}
	engine := optimization.NewReflectionEngine(trajStore, patternStore, config)

	reflection, err := engine.ReflectOnTrajectory(ctx, "ws-test", inserted.ID)
	if err != nil {
		t.Fatalf("reflect: %v", err)
	}

	// Should have identified weaknesses
	if len(reflection.Weaknesses) == 0 {
		t.Error("expected weaknesses to be identified")
	}

	// Should have suggestions
	if len(reflection.Suggestions) == 0 {
		t.Error("expected suggestions for improvement")
	}
}

func TestReflectionEngine_ReflectInsufficientEvents(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	// Create trajectory with few events
	traj := trajectory.Trajectory{
		WorkspaceID: "ws-test",
		AgentRole:   "coder",
		Status:      trajectory.StatusOK,
	}
	inserted, err := trajStore.InsertTrajectory(ctx, traj)
	if err != nil {
		t.Fatalf("insert trajectory: %v", err)
	}

	// Add only 2 events
	event1 := trajectory.Event{TrajectoryID: inserted.ID, Kind: trajectory.EventKindUserRequest}
	event2 := trajectory.Event{TrajectoryID: inserted.ID, Kind: trajectory.EventKindAgentThought}
	if _, err := trajStore.InsertEvent(ctx, event1); err != nil {
		t.Fatalf("insert event 1: %v", err)
	}
	if _, err := trajStore.InsertEvent(ctx, event2); err != nil {
		t.Fatalf("insert event 2: %v", err)
	}

	config := optimization.ReflectionConfig{
		MinEventsForReflection: 10, // High threshold
		LookbackDays:           30,
	}
	engine := optimization.NewReflectionEngine(trajStore, patternStore, config)

	_, err = engine.ReflectOnTrajectory(ctx, "ws-test", inserted.ID)
	if err == nil {
		t.Error("expected error for insufficient events")
	}
}

func TestReflectionEngine_ToolAnalysis(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	traj := trajectory.Trajectory{
		WorkspaceID: "ws-test",
		AgentRole:   "coder",
		Status:      trajectory.StatusOK,
		Outcome:     &trajectory.Outcome{Success: true},
	}
	inserted, err := trajStore.InsertTrajectory(ctx, traj)
	if err != nil {
		t.Fatalf("insert trajectory: %v", err)
	}

	// Add tool call events
	events := []trajectory.Event{
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindUserRequest, DataInline: map[string]any{"text": "Task"}},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolCall, DataInline: map[string]any{"tool": "grep", "duration_ms": 100.0}},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolCall, DataInline: map[string]any{"tool": "grep", "duration_ms": 150.0}},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolCall, DataInline: map[string]any{"tool": "edit", "duration_ms": 200.0}},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindAgentThought, DataInline: map[string]any{"result": "Done"}},
	}
	for i, event := range events {
		if _, err := trajStore.InsertEvent(ctx, event); err != nil {
			t.Fatalf("insert event %d: %v", i, err)
		}
	}

	config := optimization.ReflectionConfig{
		MinEventsForReflection: 3,
		AnalyzeToolUsage:       true,
		LookbackDays:           30,
	}
	engine := optimization.NewReflectionEngine(trajStore, patternStore, config)

	reflection, err := engine.ReflectOnTrajectory(ctx, "ws-test", inserted.ID)
	if err != nil {
		t.Fatalf("reflect: %v", err)
	}

	// Should have tool analysis
	if len(reflection.ToolAnalysis) == 0 {
		t.Error("expected tool analysis")
	}

	// Should include grep (used twice) and edit
	foundGrep := false
	foundEdit := false
	for _, ta := range reflection.ToolAnalysis {
		if ta.ToolName == "grep" {
			foundGrep = true
			if ta.UsageCount != 2 {
				t.Errorf("grep usage count: got %d, want 2", ta.UsageCount)
			}
		}
		if ta.ToolName == "edit" {
			foundEdit = true
			if ta.UsageCount != 1 {
				t.Errorf("edit usage count: got %d, want 1", ta.UsageCount)
			}
		}
	}
	if !foundGrep {
		t.Error("expected grep in tool analysis")
	}
	if !foundEdit {
		t.Error("expected edit in tool analysis")
	}
}

func TestReflectionEngine_GenerateImprovements(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	// Create multiple trajectories with common weaknesses
	for i := 0; i < 10; i++ {
		traj := trajectory.Trajectory{
			WorkspaceID: "ws-test",
			AgentRole:   "coder",
			Status:      trajectory.StatusOK,
			Outcome: &trajectory.Outcome{
				Success:       i%2 == 0, // 50% success
				ErrorCount:    i % 3,
				ToolCallCount: 15, // High tool call count (weakness)
			},
		}
		inserted, err := trajStore.InsertTrajectory(ctx, traj)
		if err != nil {
			t.Fatalf("insert trajectory %d: %v", i, err)
		}

		// Add enough events
		events := []trajectory.Event{
			{TrajectoryID: inserted.ID, Kind: trajectory.EventKindUserRequest, DataInline: map[string]any{"text": "Task"}},
			{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolCall, DataInline: map[string]any{"tool": "grep"}},
			{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolCall, DataInline: map[string]any{"tool": "edit"}},
			{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolCall, DataInline: map[string]any{"tool": "grep"}},
			{TrajectoryID: inserted.ID, Kind: trajectory.EventKindAgentThought, DataInline: map[string]any{"thought": "thinking"}},
		}
		for _, event := range events {
			if _, err := trajStore.InsertEvent(ctx, event); err != nil {
				t.Fatalf("insert event: %v", err)
			}
		}
	}

	config := optimization.ReflectionConfig{
		MinEventsForReflection:        3,
		AnalyzeToolUsage:              true,
		LookbackDays:                  30,
		MinTrajectoriesForImprovement: 5,
	}
	engine := optimization.NewReflectionEngine(trajStore, patternStore, config)

	improvements, err := engine.GenerateImprovements(ctx, "ws-test", "coder")
	if err != nil {
		t.Fatalf("generate improvements: %v", err)
	}

	// Should have generated some improvements
	if len(improvements) == 0 {
		t.Error("expected improvements to be generated")
	}

	// Each improvement should have required fields
	for _, imp := range improvements {
		if imp.ID == "" {
			t.Error("improvement should have ID")
		}
		if imp.Category == "" {
			t.Error("improvement should have category")
		}
		if imp.Description == "" {
			t.Error("improvement should have description")
		}
		if imp.Priority == "" {
			t.Error("improvement should have priority")
		}
	}
}

func TestReflectionEngine_GenerateImprovementsInsufficientData(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	config := optimization.ReflectionConfig{
		MinEventsForReflection:        3,
		LookbackDays:                  30,
		MinTrajectoriesForImprovement: 100, // High threshold
	}
	engine := optimization.NewReflectionEngine(trajStore, patternStore, config)

	_, err := engine.GenerateImprovements(ctx, "ws-test", "coder")
	if err == nil {
		t.Error("expected error for insufficient data")
	}
}

func TestReflectionEngine_GenerateSummary(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	// Create trajectories
	for i := 0; i < 10; i++ {
		traj := trajectory.Trajectory{
			WorkspaceID: "ws-test",
			AgentRole:   "coder",
			Status:      trajectory.StatusOK,
			Outcome:     &trajectory.Outcome{Success: i < 7}, // 70% success
		}
		inserted, err := trajStore.InsertTrajectory(ctx, traj)
		if err != nil {
			t.Fatalf("insert trajectory: %v", err)
		}

		// Add events
		events := []trajectory.Event{
			{TrajectoryID: inserted.ID, Kind: trajectory.EventKindUserRequest, DataInline: map[string]any{"text": "Task"}},
			{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolCall, DataInline: map[string]any{"tool": "grep"}},
			{TrajectoryID: inserted.ID, Kind: trajectory.EventKindAgentThought, DataInline: map[string]any{"result": "Done"}},
		}
		for _, event := range events {
			if _, err := trajStore.InsertEvent(ctx, event); err != nil {
				t.Fatalf("insert event: %v", err)
			}
		}
	}

	config := optimization.ReflectionConfig{
		MinEventsForReflection:        2,
		AnalyzeToolUsage:              true,
		LookbackDays:                  30,
		MinTrajectoriesForImprovement: 3,
	}
	engine := optimization.NewReflectionEngine(trajStore, patternStore, config)

	summary, err := engine.GenerateSummary(ctx, "ws-test", "coder")
	if err != nil {
		t.Fatalf("generate summary: %v", err)
	}

	if summary == nil {
		t.Fatal("expected summary, got nil")
		return
	}

	if summary.TotalTrajectories == 0 {
		t.Error("total trajectories should be positive")
	}

	if summary.SuccessfulTrajectories > summary.TotalTrajectories {
		t.Error("successful cannot exceed total")
	}
}

func TestImprovement(t *testing.T) {
	improvement := optimization.Improvement{
		ID:          "imp-1",
		Category:    "tool_selection",
		Description: "High tool call count",
		Priority:    "high",
		Evidence:    []string{"traj-1", "traj-2"},
		Action:      "Optimize tool usage",
		Impact:      0.75,
	}

	if improvement.ID != "imp-1" {
		t.Error("ID mismatch")
	}
	if improvement.Category != "tool_selection" {
		t.Error("category mismatch")
	}
	if improvement.Priority != "high" {
		t.Error("priority mismatch")
	}
	if len(improvement.Evidence) != 2 {
		t.Error("evidence mismatch")
	}
	if improvement.Impact != 0.75 {
		t.Error("impact mismatch")
	}
}

func TestToolReflection(t *testing.T) {
	tr := optimization.ToolReflection{
		ToolName:    "grep",
		UsageCount:  5,
		SuccessRate: 0.8,
		Notes:       []string{"frequently used"},
	}

	if tr.ToolName != "grep" {
		t.Error("tool name mismatch")
	}
	if tr.UsageCount != 5 {
		t.Error("usage count mismatch")
	}
	if tr.SuccessRate != 0.8 {
		t.Error("success rate mismatch")
	}
}
