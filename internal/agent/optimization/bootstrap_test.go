package optimization_test

import (
	"context"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/agent/optimization"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

func TestBootstrapOptimizer_GenerateExamples(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	// Create successful trajectories with events
	for i := 0; i < 3; i++ {
		traj := trajectory.Trajectory{
			WorkspaceID: "ws-test",
			AgentRole:   "coder",
			Status:      trajectory.StatusOK,
			Outcome: &trajectory.Outcome{
				Success:       true,
				ToolCallCount: 3,
				RecordedAt:    time.Now(),
			},
		}
		inserted, err := trajStore.InsertTrajectory(ctx, traj)
		if err != nil {
			t.Fatalf("insert trajectory %d: %v", i, err)
		}

		// Add user request event
		userEvent := trajectory.Event{
			TrajectoryID: inserted.ID,
			Kind:         trajectory.EventKindUserRequest,
			DataInline:   map[string]any{"text": "Fix the authentication bug"},
		}
		if _, err := trajStore.InsertEvent(ctx, userEvent); err != nil {
			t.Fatalf("insert user event %d: %v", i, err)
		}

		// Add thought event with result
		thoughtEvent := trajectory.Event{
			TrajectoryID: inserted.ID,
			Kind:         trajectory.EventKindAgentThought,
			DataInline:   map[string]any{"result": "Fixed the authentication bug by updating the token validation."},
		}
		if _, err := trajStore.InsertEvent(ctx, thoughtEvent); err != nil {
			t.Fatalf("insert thought event %d: %v", i, err)
		}

		// Add tool call events
		toolEvent := trajectory.Event{
			TrajectoryID: inserted.ID,
			Kind:         trajectory.EventKindToolCall,
			DataInline:   map[string]any{"tool": "edit"},
		}
		if _, err := trajStore.InsertEvent(ctx, toolEvent); err != nil {
			t.Fatalf("insert tool event %d: %v", i, err)
		}
	}

	config := optimization.DefaultBootstrapConfig()
	optimizer := optimization.NewBootstrapOptimizer(trajStore, patternStore, config)

	examples, err := optimizer.GenerateExamples(ctx, "ws-test", "coder")
	if err != nil {
		t.Fatalf("generate examples: %v", err)
	}

	if len(examples) == 0 {
		t.Error("expected at least one example")
	}

	// Verify example structure
	for _, ex := range examples {
		if ex.Input == "" {
			t.Error("example input should not be empty")
		}
		if ex.Output == "" {
			t.Error("example output should not be empty")
		}
	}
}

func TestBootstrapOptimizer_GenerateExamplesEmpty(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	config := optimization.DefaultBootstrapConfig()
	optimizer := optimization.NewBootstrapOptimizer(trajStore, patternStore, config)

	// No trajectories - should return empty
	examples, err := optimizer.GenerateExamples(ctx, "ws-test", "coder")
	if err != nil {
		t.Fatalf("generate examples: %v", err)
	}

	if len(examples) != 0 {
		t.Errorf("expected empty examples, got %d", len(examples))
	}
}

func TestBootstrapOptimizer_MaxExamples(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	// Create many successful trajectories
	for i := 0; i < 20; i++ {
		traj := trajectory.Trajectory{
			WorkspaceID: "ws-test",
			AgentRole:   "coder",
			Status:      trajectory.StatusOK,
			Outcome: &trajectory.Outcome{
				Success: true,
			},
		}
		inserted, err := trajStore.InsertTrajectory(ctx, traj)
		if err != nil {
			t.Fatalf("insert trajectory %d: %v", i, err)
		}

		// Add events
		userEvent := trajectory.Event{
			TrajectoryID: inserted.ID,
			Kind:         trajectory.EventKindUserRequest,
			DataInline:   map[string]any{"text": "Task " + string(rune('A'+i))},
		}
		if _, err := trajStore.InsertEvent(ctx, userEvent); err != nil {
			t.Fatalf("insert user event %d: %v", i, err)
		}

		thoughtEvent := trajectory.Event{
			TrajectoryID: inserted.ID,
			Kind:         trajectory.EventKindAgentThought,
			DataInline:   map[string]any{"result": "Result " + string(rune('A'+i))},
		}
		if _, err := trajStore.InsertEvent(ctx, thoughtEvent); err != nil {
			t.Fatalf("insert thought event %d: %v", i, err)
		}
	}

	config := optimization.BootstrapConfig{
		MaxExamples:    5,
		MinSuccessRate: 0.5,
		LookbackDays:   30,
	}
	optimizer := optimization.NewBootstrapOptimizer(trajStore, patternStore, config)

	examples, err := optimizer.GenerateExamples(ctx, "ws-test", "coder")
	if err != nil {
		t.Fatalf("generate examples: %v", err)
	}

	if len(examples) > 5 {
		t.Errorf("expected at most 5 examples, got %d", len(examples))
	}
}

func TestBootstrapOptimizer_FormatExamplesForPrompt(t *testing.T) {
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	config := optimization.DefaultBootstrapConfig()
	optimizer := optimization.NewBootstrapOptimizer(trajStore, patternStore, config)

	// Create example manually
	examples := []optimization.Example{
		{
			Input:  "How do I fix the login bug?",
			Output: "I'll update the authentication token validation.",
			Tools:  []string{"grep", "edit"},
		},
		{
			Input:  "Add a new feature",
			Output: "I'll create the new feature.",
			Tools:  []string{"write"},
		},
	}

	formatted := optimizer.FormatExamplesForPrompt(examples)

	if formatted == "" {
		t.Error("expected formatted output, got empty string")
	}

	// Should contain example markers
	if len(formatted) < 50 {
		t.Errorf("formatted output too short: %q", formatted)
	}
}

func TestBootstrapOptimizer_FormatExamplesEmpty(t *testing.T) {
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	config := optimization.DefaultBootstrapConfig()
	optimizer := optimization.NewBootstrapOptimizer(trajStore, patternStore, config)

	formatted := optimizer.FormatExamplesForPrompt([]optimization.Example{})

	if formatted != "" {
		t.Errorf("expected empty string for no examples, got %q", formatted)
	}
}

func TestBootstrapOptimizer_GetExampleStats(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	// Create trajectories with ratings
	rating := 5
	for i := 0; i < 3; i++ {
		traj := trajectory.Trajectory{
			WorkspaceID: "ws-test",
			AgentRole:   "coder",
			Status:      trajectory.StatusOK,
			Outcome: &trajectory.Outcome{
				Success:     true,
				HumanRating: &rating,
				RecordedAt:  time.Now(),
			},
		}
		inserted, err := trajStore.InsertTrajectory(ctx, traj)
		if err != nil {
			t.Fatalf("insert trajectory: %v", err)
		}

		// Add events
		userEvent := trajectory.Event{
			TrajectoryID: inserted.ID,
			Kind:         trajectory.EventKindUserRequest,
			DataInline:   map[string]any{"text": "Task"},
		}
		if _, err := trajStore.InsertEvent(ctx, userEvent); err != nil {
			t.Fatalf("insert user event: %v", err)
		}

		thoughtEvent := trajectory.Event{
			TrajectoryID: inserted.ID,
			Kind:         trajectory.EventKindAgentThought,
			DataInline:   map[string]any{"result": "Done"},
		}
		if _, err := trajStore.InsertEvent(ctx, thoughtEvent); err != nil {
			t.Fatalf("insert thought event: %v", err)
		}

		// Add tool call
		toolEvent := trajectory.Event{
			TrajectoryID: inserted.ID,
			Kind:         trajectory.EventKindToolCall,
			DataInline:   map[string]any{"tool": "edit"},
		}
		if _, err := trajStore.InsertEvent(ctx, toolEvent); err != nil {
			t.Fatalf("insert tool event: %v", err)
		}
	}

	config := optimization.DefaultBootstrapConfig()
	optimizer := optimization.NewBootstrapOptimizer(trajStore, patternStore, config)

	stats, err := optimizer.GetExampleStats(ctx, "ws-test", "coder")
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}

	if stats.TotalAvailable == 0 {
		t.Error("expected some examples available")
	}
}

func TestBootstrapOptimizer_DiverseSelection(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	// Create similar trajectories with same input
	for i := 0; i < 5; i++ {
		traj := trajectory.Trajectory{
			WorkspaceID: "ws-test",
			AgentRole:   "coder",
			Status:      trajectory.StatusOK,
			Outcome:     &trajectory.Outcome{Success: true},
		}
		inserted, err := trajStore.InsertTrajectory(ctx, traj)
		if err != nil {
			t.Fatalf("insert similar: %v", err)
		}

		userEvent := trajectory.Event{
			TrajectoryID: inserted.ID,
			Kind:         trajectory.EventKindUserRequest,
			DataInline:   map[string]any{"text": "Fix the authentication bug"},
		}
		if _, err := trajStore.InsertEvent(ctx, userEvent); err != nil {
			t.Fatalf("insert user event: %v", err)
		}

		thoughtEvent := trajectory.Event{
			TrajectoryID: inserted.ID,
			Kind:         trajectory.EventKindAgentThought,
			DataInline:   map[string]any{"result": "Fixed it"},
		}
		if _, err := trajStore.InsertEvent(ctx, thoughtEvent); err != nil {
			t.Fatalf("insert thought event: %v", err)
		}
	}

	// Create diverse trajectories
	diverseInputs := []string{
		"Add a new database table",
		"Refactor the payment module",
		"Write unit tests for auth",
	}
	for _, input := range diverseInputs {
		traj := trajectory.Trajectory{
			WorkspaceID: "ws-test",
			AgentRole:   "coder",
			Status:      trajectory.StatusOK,
			Outcome:     &trajectory.Outcome{Success: true},
		}
		inserted, err := trajStore.InsertTrajectory(ctx, traj)
		if err != nil {
			t.Fatalf("insert diverse: %v", err)
		}

		userEvent := trajectory.Event{
			TrajectoryID: inserted.ID,
			Kind:         trajectory.EventKindUserRequest,
			DataInline:   map[string]any{"text": input},
		}
		if _, err := trajStore.InsertEvent(ctx, userEvent); err != nil {
			t.Fatalf("insert user event: %v", err)
		}

		thoughtEvent := trajectory.Event{
			TrajectoryID: inserted.ID,
			Kind:         trajectory.EventKindAgentThought,
			DataInline:   map[string]any{"result": "Completed: " + input},
		}
		if _, err := trajStore.InsertEvent(ctx, thoughtEvent); err != nil {
			t.Fatalf("insert thought event: %v", err)
		}
	}

	config := optimization.BootstrapConfig{
		MaxExamples:     3,
		MinSuccessRate:  0.5,
		DiversityWeight: 0.8, // High diversity weight
		LookbackDays:    30,
	}
	optimizer := optimization.NewBootstrapOptimizer(trajStore, patternStore, config)

	examples, err := optimizer.GenerateExamples(ctx, "ws-test", "coder")
	if err != nil {
		t.Fatalf("generate examples: %v", err)
	}

	// With high diversity weight, should not have all same-input examples
	sameInputCount := 0
	for _, ex := range examples {
		if ex.Input == "Fix the authentication bug" {
			sameInputCount++
		}
	}

	// Should have diverse examples, not all the same
	if sameInputCount == len(examples) && len(examples) > 1 {
		t.Error("expected diverse examples, but all have the same input")
	}
}

func TestDefaultBootstrapConfig(t *testing.T) {
	config := optimization.DefaultBootstrapConfig()

	if config.MinSuccessRate <= 0 || config.MinSuccessRate > 1 {
		t.Errorf("invalid min success rate: %f", config.MinSuccessRate)
	}
	if config.MaxExamples <= 0 {
		t.Errorf("invalid max examples: %d", config.MaxExamples)
	}
	if config.DiversityWeight < 0 || config.DiversityWeight > 1 {
		t.Errorf("invalid diversity weight: %f", config.DiversityWeight)
	}
	if config.LookbackDays <= 0 {
		t.Errorf("invalid lookback days: %d", config.LookbackDays)
	}
}
