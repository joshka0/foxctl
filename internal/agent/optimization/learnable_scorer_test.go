package optimization_test

import (
	"context"
	"math"
	"testing"
	"testing/quick"
	"time"

	"github.com/joshka0/foxctl/internal/agent/optimization"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

func TestDefaultScorerWeights(t *testing.T) {
	weights := optimization.DefaultScorerWeights()

	// Verify weights sum to 1.0
	sum := weights.CriticalPath + weights.PageRank + weights.AdminMail +
		weights.OverseerMail + weights.Recency

	if math.Abs(sum-1.0) > 0.01 {
		t.Errorf("weights should sum to 1.0, got %.3f", sum)
	}

	// Verify all weights are positive
	if weights.CriticalPath <= 0 {
		t.Error("CriticalPath weight should be positive")
	}
	if weights.PageRank <= 0 {
		t.Error("PageRank weight should be positive")
	}
	if weights.AdminMail <= 0 {
		t.Error("AdminMail weight should be positive")
	}
	if weights.OverseerMail <= 0 {
		t.Error("OverseerMail weight should be positive")
	}
	if weights.Recency <= 0 {
		t.Error("Recency weight should be positive")
	}
}

func TestScorerWeights_Validate(t *testing.T) {
	tests := []struct {
		name    string
		weights optimization.ScorerWeights
		wantErr bool
	}{
		{
			name:    "valid default weights",
			weights: optimization.DefaultScorerWeights(),
			wantErr: false,
		},
		{
			name: "valid custom weights",
			weights: optimization.ScorerWeights{
				CriticalPath: 0.25,
				PageRank:     0.25,
				AdminMail:    0.25,
				OverseerMail: 0.15,
				Recency:      0.10,
			},
			wantErr: false,
		},
		{
			name: "negative weight",
			weights: optimization.ScorerWeights{
				CriticalPath: -0.1,
				PageRank:     0.5,
				AdminMail:    0.3,
				OverseerMail: 0.2,
				Recency:      0.1,
			},
			wantErr: true,
		},
		{
			name: "nan critical path weight",
			weights: optimization.ScorerWeights{
				CriticalPath: math.NaN(),
				PageRank:     0.20,
				AdminMail:    0.25,
				OverseerMail: 0.15,
				Recency:      0.10,
			},
			wantErr: true,
		},
		{
			name: "infinite pagerank weight",
			weights: optimization.ScorerWeights{
				CriticalPath: 0.30,
				PageRank:     math.Inf(1),
				AdminMail:    0.25,
				OverseerMail: 0.15,
				Recency:      0.10,
			},
			wantErr: true,
		},
		{
			name: "sum not 1.0",
			weights: optimization.ScorerWeights{
				CriticalPath: 0.5,
				PageRank:     0.5,
				AdminMail:    0.5,
				OverseerMail: 0.5,
				Recency:      0.5,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.weights.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestScorerWeights_Normalize(t *testing.T) {
	weights := optimization.ScorerWeights{
		CriticalPath: 2.0,
		PageRank:     2.0,
		AdminMail:    2.0,
		OverseerMail: 2.0,
		Recency:      2.0,
	}

	weights.Normalize()

	sum := weights.CriticalPath + weights.PageRank + weights.AdminMail +
		weights.OverseerMail + weights.Recency

	if math.Abs(sum-1.0) > 0.01 {
		t.Errorf("after normalize, weights should sum to 1.0, got %.3f", sum)
	}

	// Each should be 0.2 (2.0 / 10.0)
	if math.Abs(weights.CriticalPath-0.2) > 0.01 {
		t.Errorf("CriticalPath should be 0.2, got %.3f", weights.CriticalPath)
	}
}

func TestScorerWeightsNormalizePropertyProducesValidWeights(t *testing.T) {
	lastUpdated := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)

	property := func(criticalPath, pageRank, adminMail, overseerMail, recency uint16) bool {
		weights := optimization.ScorerWeights{
			CriticalPath: float64(criticalPath) + 1,
			PageRank:     float64(pageRank) + 1,
			AdminMail:    float64(adminMail) + 1,
			OverseerMail: float64(overseerMail) + 1,
			Recency:      float64(recency) + 1,
			LastUpdated:  lastUpdated,
			Version:      42,
		}

		weights.Normalize()

		sum := weights.CriticalPath + weights.PageRank + weights.AdminMail +
			weights.OverseerMail + weights.Recency

		return weights.Validate() == nil &&
			math.Abs(sum-1.0) <= 1e-12 &&
			finiteNonNegative(weights.CriticalPath) &&
			finiteNonNegative(weights.PageRank) &&
			finiteNonNegative(weights.AdminMail) &&
			finiteNonNegative(weights.OverseerMail) &&
			finiteNonNegative(weights.Recency) &&
			weights.LastUpdated.Equal(lastUpdated) &&
			weights.Version == 42
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatalf("Normalize property failed: %v", err)
	}
}

func finiteNonNegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func TestInMemoryWeightStore(t *testing.T) {
	ctx := context.Background()
	store := optimization.NewInMemoryWeightStore()

	weights := optimization.DefaultScorerWeights()

	// Save weights
	if err := store.SaveWeights(ctx, "ws-test", weights); err != nil {
		t.Fatalf("save weights: %v", err)
	}

	// Get weights
	got, err := store.GetWeights(ctx, "ws-test")
	if err != nil {
		t.Fatalf("get weights: %v", err)
	}

	if got.CriticalPath != weights.CriticalPath {
		t.Errorf("CriticalPath: got %.3f, want %.3f", got.CriticalPath, weights.CriticalPath)
	}

	// Get non-existent
	_, err = store.GetWeights(ctx, "non-existent")
	if err == nil {
		t.Error("expected error for non-existent workspace")
	}
}

func TestInMemoryWeightStore_History(t *testing.T) {
	ctx := context.Background()
	store := optimization.NewInMemoryWeightStore()

	// Save history updates
	for i := 0; i < 5; i++ {
		update := optimization.WeightUpdate{
			PreviousWeights: optimization.DefaultScorerWeights(),
			NewWeights:      optimization.DefaultScorerWeights(),
			Reason:          "test update",
			SampleSize:      10,
		}
		if err := store.SaveHistory(ctx, "ws-test", update); err != nil {
			t.Fatalf("save history %d: %v", i, err)
		}
	}

	// Get all history
	history, err := store.GetHistory(ctx, "ws-test", 0)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}

	if len(history) != 5 {
		t.Errorf("expected 5 history entries, got %d", len(history))
	}

	// Get limited history
	limitedHistory, err := store.GetHistory(ctx, "ws-test", 2)
	if err != nil {
		t.Fatalf("get limited history: %v", err)
	}

	if len(limitedHistory) != 2 {
		t.Errorf("expected 2 history entries, got %d", len(limitedHistory))
	}
}

func TestLearnableScorer_Score(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	weightStore := optimization.NewInMemoryWeightStore()

	// Save default weights
	weights := optimization.DefaultScorerWeights()
	if err := weightStore.SaveWeights(ctx, "ws-test", weights); err != nil {
		t.Fatalf("save weights: %v", err)
	}

	config := optimization.DefaultLearnerConfig()
	scorer := optimization.NewLearnableScorer(trajStore, weightStore, config)

	factors := optimization.TaskFactors{
		CriticalPathScore: 0.8,
		PageRank:          0.5,
		AdminMailScore:    0.3,
		OverseerMailScore: 0.2,
		RecencyFactor:     0.9,
	}

	score := scorer.Score(ctx, "ws-test", factors)

	// Score should be between 0 and 1 for normalized factors
	if score < 0 || score > 1 {
		t.Errorf("score should be between 0 and 1, got %.3f", score)
	}

	// Verify calculation:
	// 0.30*0.8 + 0.20*0.5 + 0.25*0.3 + 0.15*0.2 + 0.10*0.9
	// = 0.24 + 0.10 + 0.075 + 0.03 + 0.09 = 0.535
	expected := weights.CriticalPath*factors.CriticalPathScore +
		weights.PageRank*factors.PageRank +
		weights.AdminMail*factors.AdminMailScore +
		weights.OverseerMail*factors.OverseerMailScore +
		weights.Recency*factors.RecencyFactor

	if math.Abs(score-expected) > 0.01 {
		t.Errorf("score calculation: got %.3f, want %.3f", score, expected)
	}
}

func TestLearnableScorer_GetCurrentWeights(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	weightStore := optimization.NewInMemoryWeightStore()

	// Set custom weights
	customWeights := optimization.ScorerWeights{
		CriticalPath: 0.40,
		PageRank:     0.20,
		AdminMail:    0.20,
		OverseerMail: 0.10,
		Recency:      0.10,
	}
	if err := weightStore.SaveWeights(ctx, "ws-test", customWeights); err != nil {
		t.Fatalf("save weights: %v", err)
	}

	config := optimization.DefaultLearnerConfig()
	scorer := optimization.NewLearnableScorer(trajStore, weightStore, config)

	got, err := scorer.GetCurrentWeights(ctx, "ws-test")
	if err != nil {
		t.Fatalf("get weights: %v", err)
	}

	if got.CriticalPath != 0.40 {
		t.Errorf("CriticalPath: got %.2f, want 0.40", got.CriticalPath)
	}
}

func TestLearnableScorer_LearnFromOutcomes(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	weightStore := optimization.NewInMemoryWeightStore()

	// Save initial weights
	if err := weightStore.SaveWeights(ctx, "ws-test", optimization.DefaultScorerWeights()); err != nil {
		t.Fatalf("save initial weights: %v", err)
	}

	// Create trajectories with outcomes for learning
	for i := 0; i < 20; i++ {
		success := i%3 != 0 // 2/3 success rate
		outcome := trajectory.Outcome{
			Success:       success,
			ToolCallCount: 5 - (i % 3), // Successful ones have fewer tool calls
			DurationMS:    int64(1000 - (i%3)*200),
		}

		traj := trajectory.Trajectory{
			WorkspaceID: "ws-test",
			AgentRole:   "coder",
			Status:      trajectory.StatusOK,
			Outcome:     &outcome,
		}
		if _, err := trajStore.InsertTrajectory(ctx, traj); err != nil {
			t.Fatalf("insert trajectory %d: %v", i, err)
		}
	}

	config := optimization.LearnerConfig{
		LearningRate:    0.1,
		MinSamples:      10,
		MaxWeightChange: 0.05,
		LookbackDays:    30,
	}
	scorer := optimization.NewLearnableScorer(trajStore, weightStore, config)

	// Learn from outcomes
	update, err := scorer.LearnFromOutcomes(ctx, "ws-test")
	if err != nil {
		t.Fatalf("learn from outcomes: %v", err)
	}

	if update == nil {
		t.Fatal("expected weight update, got nil")
		return
	}

	if update.SampleSize < 10 {
		t.Errorf("sample size: got %d, want >= 10", update.SampleSize)
	}

	// Verify new weights are normalized
	sum := update.NewWeights.CriticalPath + update.NewWeights.PageRank +
		update.NewWeights.AdminMail + update.NewWeights.OverseerMail +
		update.NewWeights.Recency

	if math.Abs(sum-1.0) > 0.01 {
		t.Errorf("new weights should sum to 1.0, got %.3f", sum)
	}

	// Version should increment
	if update.NewWeights.Version != update.PreviousWeights.Version+1 {
		t.Errorf("version should increment")
	}
}

func TestLearnableScorer_LearnInsufficientSamples(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	weightStore := optimization.NewInMemoryWeightStore()

	config := optimization.LearnerConfig{
		LearningRate:    0.1,
		MinSamples:      100, // High threshold
		MaxWeightChange: 0.05,
		LookbackDays:    30,
	}
	scorer := optimization.NewLearnableScorer(trajStore, weightStore, config)

	// Only create a few trajectories
	for i := 0; i < 5; i++ {
		traj := trajectory.Trajectory{
			WorkspaceID: "ws-test",
			AgentRole:   "coder",
			Status:      trajectory.StatusOK,
			Outcome:     &trajectory.Outcome{Success: true},
		}
		if _, err := trajStore.InsertTrajectory(ctx, traj); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	_, err := scorer.LearnFromOutcomes(ctx, "ws-test")
	if err == nil {
		t.Error("expected error for insufficient samples")
	}
}

func TestDefaultLearnerConfig(t *testing.T) {
	config := optimization.DefaultLearnerConfig()

	if config.LearningRate <= 0 || config.LearningRate > 1 {
		t.Errorf("invalid learning rate: %.2f", config.LearningRate)
	}
	if config.MinSamples <= 0 {
		t.Errorf("invalid min samples: %d", config.MinSamples)
	}
	if config.MaxWeightChange <= 0 || config.MaxWeightChange > 1 {
		t.Errorf("invalid max weight change: %.2f", config.MaxWeightChange)
	}
	if config.LookbackDays <= 0 {
		t.Errorf("invalid lookback days: %d", config.LookbackDays)
	}
}

func TestExportImportWeightsJSON(t *testing.T) {
	weights := optimization.DefaultScorerWeights()

	// Export
	data, err := optimization.ExportWeightsJSON(weights)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	if len(data) == 0 {
		t.Error("exported data should not be empty")
	}

	// Import
	imported, err := optimization.ImportWeightsJSON(data)
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	if imported.CriticalPath != weights.CriticalPath {
		t.Errorf("CriticalPath mismatch after import")
	}
	if imported.PageRank != weights.PageRank {
		t.Errorf("PageRank mismatch after import")
	}
}

func TestImportWeightsJSON_Invalid(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{
			name: "invalid json",
			data: "not json",
		},
		{
			name: "invalid weights sum",
			data: `{"critical_path": 0.5, "pagerank": 0.5, "admin_mail": 0.5, "overseer_mail": 0.5, "recency": 0.5}`,
		},
		{
			name: "negative weight",
			data: `{"critical_path": -0.1, "pagerank": 0.3, "admin_mail": 0.3, "overseer_mail": 0.3, "recency": 0.2}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := optimization.ImportWeightsJSON([]byte(tt.data))
			if err == nil {
				t.Error("expected error for invalid data")
			}
		})
	}
}
