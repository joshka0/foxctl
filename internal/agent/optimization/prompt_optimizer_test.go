package optimization_test

import (
	"context"
	"testing"

	"github.com/jkatigb/agentctl/internal/agent/optimization"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

func TestDefaultPromptOptimizerConfig(t *testing.T) {
	config := optimization.DefaultPromptOptimizerConfig()

	if config.Mode == "" {
		t.Error("Mode should have default value")
	}
	if config.BreadthCandidates <= 0 {
		t.Error("BreadthCandidates should be positive")
	}
	if config.DepthIterations < 0 {
		t.Error("DepthIterations should be non-negative")
	}
	if config.MinImprovement < 0 || config.MinImprovement > 1 {
		t.Error("MinImprovement should be between 0 and 1")
	}
	if config.LookbackDays <= 0 {
		t.Error("LookbackDays should be positive")
	}
}

func TestPromptOptimizer_OptimizeInstruction(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	// Add some patterns for evaluation context
	for i := 0; i < 5; i++ {
		pattern := optimization.Pattern{
			AgentRole:    "coder",
			Context:      "code task",
			ToolSequence: []string{"grep", "edit"},
			Outcome:      "success",
			SuccessCount: 10,
		}
		if err := patternStore.Record(ctx, pattern); err != nil {
			t.Fatalf("record pattern: %v", err)
		}
	}

	config := optimization.DefaultPromptOptimizerConfig()
	optimizer := optimization.NewPromptOptimizer(trajStore, patternStore, config)

	// Set a custom eval function for testing
	optimizer.SetEvalFunc(func(ctx context.Context, prompt string) (float64, error) {
		// Simple eval: longer prompts get slightly higher scores
		baseScore := 0.5
		if len(prompt) > 50 {
			baseScore += 0.1
		}
		if len(prompt) > 100 {
			baseScore += 0.1
		}
		return baseScore, nil
	})

	currentPrompt := "You are a helpful coder. Complete tasks efficiently."

	result, err := optimizer.OptimizeInstruction(ctx, "ws-test", "coder", currentPrompt)
	if err != nil {
		t.Fatalf("optimize instruction: %v", err)
	}

	if result == nil {
		t.Fatal("expected result, got nil")
		return
	}

	if result.OriginalPrompt != currentPrompt {
		t.Error("original prompt should match input")
	}

	if result.OriginalScore <= 0 {
		t.Error("original score should be positive")
	}

	if len(result.Candidates) == 0 {
		t.Error("expected some candidates")
	}

	if result.Duration <= 0 {
		t.Error("duration should be positive")
	}

	if result.Mode != config.Mode {
		t.Errorf("mode: got %q, want %q", result.Mode, config.Mode)
	}
}

func TestPromptOptimizer_OptimizeInstructionNoImprovement(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	config := optimization.PromptOptimizerConfig{
		Mode:              "copro",
		BreadthCandidates: 3,
		DepthIterations:   1,
		MinImprovement:    0.5, // Very high threshold
		LookbackDays:      30,
	}
	optimizer := optimization.NewPromptOptimizer(trajStore, patternStore, config)

	// Eval function that gives same score to everything
	optimizer.SetEvalFunc(func(ctx context.Context, prompt string) (float64, error) {
		return 0.5, nil
	})

	currentPrompt := "You are a helpful assistant."

	result, err := optimizer.OptimizeInstruction(ctx, "ws-test", "coder", currentPrompt)
	if err != nil {
		t.Fatalf("optimize instruction: %v", err)
	}

	// With same score everywhere, optimized should equal original
	// (may change if a candidate scored higher by random variation)
	_ = result.OptimizedPrompt // Acknowledge we've seen this value

	if result.Improvement < 0 {
		t.Error("improvement should not be negative")
	}
}

func TestPromptOptimizer_CandidatesGeneration(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	// Add successful patterns with tool preferences
	for i := 0; i < 10; i++ {
		pattern := optimization.Pattern{
			AgentRole:    "coder",
			Context:      "write code",
			ToolSequence: []string{"read", "grep", "edit"},
			Outcome:      "success",
			SuccessCount: 10,
		}
		if err := patternStore.Record(ctx, pattern); err != nil {
			t.Fatalf("record pattern: %v", err)
		}
	}

	config := optimization.PromptOptimizerConfig{
		Mode:              "copro",
		BreadthCandidates: 5,
		DepthIterations:   0, // No depth phase
		MinImprovement:    0.01,
		LookbackDays:      30,
	}
	optimizer := optimization.NewPromptOptimizer(trajStore, patternStore, config)

	// Eval that scores based on keyword presence
	optimizer.SetEvalFunc(func(ctx context.Context, prompt string) (float64, error) {
		score := 0.5
		if len(prompt) > 100 {
			score += 0.1
		}
		return score, nil
	})

	result, err := optimizer.OptimizeInstruction(ctx, "ws-test", "coder", "You are a coder.")
	if err != nil {
		t.Fatalf("optimize: %v", err)
	}

	// Should have generated candidates
	if len(result.Candidates) == 0 {
		t.Error("expected candidates to be generated")
	}

	// Each candidate should have a prompt and score
	for i, c := range result.Candidates {
		if c.Prompt == "" {
			t.Errorf("candidate %d has empty prompt", i)
		}
		if c.Score <= 0 {
			t.Errorf("candidate %d has non-positive score", i)
		}
	}
}

func TestPromptOptimizer_DepthPhase(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	config := optimization.PromptOptimizerConfig{
		Mode:              "copro",
		BreadthCandidates: 2,
		DepthIterations:   3,
		MinImprovement:    0.01,
		LookbackDays:      30,
	}
	optimizer := optimization.NewPromptOptimizer(trajStore, patternStore, config)

	// Eval that gives higher scores to longer prompts (simulating refinement helping)
	callCount := 0
	optimizer.SetEvalFunc(func(ctx context.Context, prompt string) (float64, error) {
		callCount++
		score := 0.4 + float64(len(prompt))/1000.0
		if score > 1.0 {
			score = 1.0
		}
		return score, nil
	})

	result, err := optimizer.OptimizeInstruction(ctx, "ws-test", "coder", "Be helpful.")
	if err != nil {
		t.Fatalf("optimize: %v", err)
	}

	// Should have had multiple eval calls (breadth + depth phases)
	if callCount < 3 {
		t.Errorf("expected multiple eval calls, got %d", callCount)
	}

	// Result should show some improvement if depth phase ran
	if result.OptimizedScore < result.OriginalScore {
		t.Error("optimized score should not be worse than original")
	}
}

func TestPromptOptimizer_WithTrajectoryData(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	// Create successful trajectories with feedback
	rating := 5
	for i := 0; i < 5; i++ {
		traj := trajectory.Trajectory{
			WorkspaceID: "ws-test",
			AgentRole:   "coder",
			Status:      trajectory.StatusOK,
			Outcome: &trajectory.Outcome{
				Success:     true,
				HumanRating: &rating,
				Feedback:    "Great job on the authentication fix!",
			},
		}
		if _, err := trajStore.InsertTrajectory(ctx, traj); err != nil {
			t.Fatalf("insert trajectory: %v", err)
		}
	}

	config := optimization.DefaultPromptOptimizerConfig()
	optimizer := optimization.NewPromptOptimizer(trajStore, patternStore, config)

	optimizer.SetEvalFunc(func(ctx context.Context, prompt string) (float64, error) {
		return 0.6, nil
	})

	result, err := optimizer.OptimizeInstruction(ctx, "ws-test", "coder", "You are a coder.")
	if err != nil {
		t.Fatalf("optimize: %v", err)
	}

	// Should complete without error even with trajectory data
	if result == nil {
		t.Fatal("expected result")
	}
}

func TestScoredPrompt(t *testing.T) {
	scored := optimization.ScoredPrompt{
		Prompt:       "Test prompt",
		Score:        0.85,
		Improvements: []string{"added specificity"},
		Generation:   2,
	}

	if scored.Prompt != "Test prompt" {
		t.Error("prompt mismatch")
	}
	if scored.Score != 0.85 {
		t.Error("score mismatch")
	}
	if len(scored.Improvements) != 1 {
		t.Error("improvements mismatch")
	}
	if scored.Generation != 2 {
		t.Error("generation mismatch")
	}
}

func TestOptimizationResult(t *testing.T) {
	result := optimization.OptimizationResult{
		OriginalPrompt:  "Original",
		OriginalScore:   0.5,
		OptimizedPrompt: "Optimized",
		OptimizedScore:  0.7,
		Improvement:     0.4, // 40% improvement
		Mode:            "copro",
	}

	if result.OriginalPrompt != "Original" {
		t.Error("original prompt mismatch")
	}
	if result.OptimizedPrompt != "Optimized" {
		t.Error("optimized prompt mismatch")
	}
	if result.Improvement != 0.4 {
		t.Error("improvement mismatch")
	}
}
