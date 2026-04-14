package optimization_test

import (
	"context"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/agent/optimization"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
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

func TestPromptOptimizer_GEPAModeUsesReflectionSignals(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	insertTrajectory := func(rating int, feedback string, success bool, toolName string, toolError string) {
		t.Helper()

		outcome := &trajectory.Outcome{
			Success:  success,
			Feedback: feedback,
		}
		outcome.HumanRating = &rating

		traj, err := trajStore.InsertTrajectory(ctx, trajectory.Trajectory{
			WorkspaceID: "ws-test",
			AgentRole:   "coder",
			Status:      trajectory.StatusOK,
			Outcome:     outcome,
		})
		if err != nil {
			t.Fatalf("insert trajectory: %v", err)
		}

		events := []trajectory.Event{
			{TrajectoryID: traj.ID, Kind: trajectory.EventKindUserRequest, DataInline: map[string]any{"text": "Fix auth regression"}},
			{TrajectoryID: traj.ID, Kind: trajectory.EventKindAgentThought, DataInline: map[string]any{"thought": "Inspecting issue"}},
			{TrajectoryID: traj.ID, Kind: trajectory.EventKindToolCall, DataInline: map[string]any{"tool": toolName}},
			{TrajectoryID: traj.ID, Kind: trajectory.EventKindToolResult, DataInline: map[string]any{"error": toolError}},
			{TrajectoryID: traj.ID, Kind: trajectory.EventKindToolCall, DataInline: map[string]any{"tool": "edit"}},
			{TrajectoryID: traj.ID, Kind: trajectory.EventKindAgentThought, DataInline: map[string]any{"result": "done"}},
		}
		for idx, event := range events {
			if _, err := trajStore.InsertEvent(ctx, event); err != nil {
				t.Fatalf("insert event %d: %v", idx, err)
			}
		}
	}

	insertTrajectory(1, "Missed explicit output format and retried too often.", false, "read", "file not found")
	insertTrajectory(2, "Tool selection was noisy and result formatting was unclear.", false, "grep", "syntax error")
	insertTrajectory(5, "Clear final result and efficient workflow with strong verification.", true, "read", "")
	insertTrajectory(4, "Produced a clear final result and verified the fix quickly.", true, "edit", "")

	config := optimization.PromptOptimizerConfig{
		Mode:              "gepa",
		Backend:           "foxctl",
		BreadthCandidates: 4,
		DepthIterations:   1,
		MinImprovement:    0.01,
		LookbackDays:      30,
	}
	optimizer := optimization.NewPromptOptimizer(trajStore, patternStore, config)
	optimizer.SetEvalFunc(func(ctx context.Context, prompt string) (float64, error) {
		score := 0.5
		if strings.Contains(prompt, "Address recurring failure modes observed in past trajectories") {
			score += 0.2
		}
		if strings.Contains(prompt, "Reflection-driven improvements to apply") {
			score += 0.2
		}
		if strings.Contains(prompt, "Preserve these high-confidence strengths from successful runs") {
			score += 0.1
		}
		return score, nil
	})

	result, err := optimizer.OptimizeInstruction(ctx, "ws-test", "coder", "You are a careful coding assistant.")
	if err != nil {
		t.Fatalf("optimize instruction: %v", err)
	}

	if result == nil {
		t.Fatal("expected result")
		return
	}
	got := *result
	if got.Mode != "gepa" {
		t.Fatalf("mode=%q want gepa", got.Mode)
	}
	if len(got.Candidates) == 0 {
		t.Fatal("expected GEPA candidates")
	}
	if !strings.Contains(got.OptimizedPrompt, "Address recurring failure modes observed in past trajectories") &&
		!strings.Contains(got.OptimizedPrompt, "Reflection-driven improvements to apply") {
		t.Fatalf("optimized prompt missing GEPA reflection content: %q", got.OptimizedPrompt)
	}
}

func TestPromptOptimizer_GEPAModeUsesDatasetExamples(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	optimizer := optimization.NewPromptOptimizer(trajStore, patternStore, optimization.PromptOptimizerConfig{
		Mode:              "gepa",
		Backend:           "foxctl",
		BreadthCandidates: 5,
		DepthIterations:   1,
		MinImprovement:    0.01,
		LookbackDays:      30,
	})
	optimizer.SetTranscriptExamples([]optimization.TranscriptTrainingExample{
		{
			Input: optimization.TranscriptTrainingInput{
				UserRequest: "Investigate the failing integration test",
			},
			Output: optimization.TranscriptTrainingOutput{
				Response:  "I inspected the test output, isolated the regression, and verified the fix.",
				ToolsUsed: []string{"read", "edit"},
			},
			Metadata: optimization.TranscriptTrainingMetadata{
				Rating:  5,
				Outcome: "success",
				Notes:   "Strong structured debugging",
			},
		},
	})
	optimizer.SetPreferenceExamples([]optimization.PromptPreferenceExample{
		{
			RecordType: "prompt_preference",
			Input: optimization.PromptPreferenceInput{
				Question: "Fix the failing test",
			},
			Chosen: optimization.PromptPreferenceCandidate{
				VariantID: "chosen-1",
				AgentRole: "coder",
				Mode:      "gepa",
				Prompt:    "Always inspect failures first.\nVerify the fix before finishing.",
			},
			Rejected: optimization.PromptPreferenceCandidate{
				VariantID: "rejected-1",
				AgentRole: "coder",
				Mode:      "copro",
				Prompt:    "Try random fixes quickly.",
			},
		},
	})
	optimizer.SetEvalFunc(func(ctx context.Context, prompt string) (float64, error) {
		score := 0.5
		if strings.Contains(prompt, "High-rated transcript examples to emulate") {
			score += 0.2
		}
		if strings.Contains(prompt, "Preferred directives from winning prompt variants") {
			score += 0.2
		}
		return score, nil
	})

	result, err := optimizer.OptimizeInstruction(ctx, "ws-test", "coder", "You are a coding assistant.")
	if err != nil {
		t.Fatalf("OptimizeInstruction: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
		return
	}
	got := *result
	if !strings.Contains(got.OptimizedPrompt, "High-rated transcript examples to emulate") &&
		!strings.Contains(got.OptimizedPrompt, "Preferred directives from winning prompt variants") {
		t.Fatalf("optimized prompt missing dataset-driven content: %q", got.OptimizedPrompt)
	}
}

func TestPromptOptimizer_ProposeCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	optimizer := optimization.NewPromptOptimizer(trajStore, patternStore, optimization.PromptOptimizerConfig{
		Mode:              "gepa",
		Backend:           "foxctl",
		BreadthCandidates: 4,
		DepthIterations:   2,
		MinImprovement:    0.01,
		LookbackDays:      30,
	})
	optimizer.SetTranscriptExamples([]optimization.TranscriptTrainingExample{
		{
			Input: optimization.TranscriptTrainingInput{UserRequest: "Inspect the failing test"},
			Output: optimization.TranscriptTrainingOutput{
				Response: "I inspected the failure first and verified the fix.",
			},
			Metadata: optimization.TranscriptTrainingMetadata{
				Rating:  5,
				Outcome: "success",
			},
		},
	})
	optimizer.SetPreferenceExamples([]optimization.PromptPreferenceExample{
		{
			Chosen: optimization.PromptPreferenceCandidate{
				Prompt: "Inspect failures before editing.\nVerify the fix after changes.",
			},
			Rejected: optimization.PromptPreferenceCandidate{
				Prompt: "Guess and patch quickly.",
			},
		},
	})
	optimizer.SetEvalFunc(func(ctx context.Context, prompt string) (float64, error) {
		score := 0.2 + float64(len(prompt))/1000
		if strings.Contains(prompt, "High-rated transcript examples to emulate") {
			score += 0.2
		}
		if strings.Contains(prompt, "Preferred directives from winning prompt variants") {
			score += 0.2
		}
		return score, nil
	})

	candidates, err := optimizer.ProposeCandidates(ctx, "ws-test", "coder", "You are a coding assistant.", 3)
	if err != nil {
		t.Fatalf("ProposeCandidates: %v", err)
	}
	if len(candidates) != 3 {
		t.Fatalf("len(candidates)=%d want 3", len(candidates))
	}
	if candidates[0].Score < candidates[1].Score {
		t.Fatalf("expected sorted candidates by score: %+v", candidates)
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
