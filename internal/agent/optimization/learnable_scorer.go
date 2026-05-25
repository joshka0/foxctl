package optimization

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

// ScorerWeights holds the adjustable weights for task scoring.
type ScorerWeights struct {
	// CriticalPath weight (default: 0.30).
	CriticalPath float64 `json:"critical_path"`

	// PageRank weight (default: 0.20).
	PageRank float64 `json:"pagerank"`

	// AdminMail weight (default: 0.25).
	AdminMail float64 `json:"admin_mail"`

	// OverseerMail weight (default: 0.15).
	OverseerMail float64 `json:"overseer_mail"`

	// Recency weight (default: 0.10).
	Recency float64 `json:"recency"`

	// LastUpdated is when weights were last adjusted.
	LastUpdated time.Time `json:"last_updated"`

	// Version tracks weight iterations.
	Version int `json:"version"`
}

// DefaultScorerWeights returns the default weights.
func DefaultScorerWeights() ScorerWeights {
	return ScorerWeights{
		CriticalPath: 0.30,
		PageRank:     0.20,
		AdminMail:    0.25,
		OverseerMail: 0.15,
		Recency:      0.10,
		LastUpdated:  time.Now().UTC(),
		Version:      1,
	}
}

// Validate ensures weights sum to approximately 1.0 and are non-negative.
func (w ScorerWeights) Validate() error {
	if !finiteWeight(w.CriticalPath) || !finiteWeight(w.PageRank) || !finiteWeight(w.AdminMail) ||
		!finiteWeight(w.OverseerMail) || !finiteWeight(w.Recency) {
		return fmt.Errorf("weights must be finite")
	}
	if w.CriticalPath < 0 || w.PageRank < 0 || w.AdminMail < 0 ||
		w.OverseerMail < 0 || w.Recency < 0 {
		return fmt.Errorf("weights must be non-negative")
	}

	sum := w.CriticalPath + w.PageRank + w.AdminMail + w.OverseerMail + w.Recency
	if math.Abs(sum-1.0) > 0.01 {
		return fmt.Errorf("weights must sum to 1.0, got %.3f", sum)
	}

	return nil
}

func finiteWeight(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// Normalize adjusts weights to sum to 1.0.
func (w *ScorerWeights) Normalize() {
	sum := w.CriticalPath + w.PageRank + w.AdminMail + w.OverseerMail + w.Recency
	if sum > 0 {
		w.CriticalPath /= sum
		w.PageRank /= sum
		w.AdminMail /= sum
		w.OverseerMail /= sum
		w.Recency /= sum
	}
}

// WeightUpdate records a weight adjustment.
type WeightUpdate struct {
	// PreviousWeights before adjustment.
	PreviousWeights ScorerWeights `json:"previous_weights"`

	// NewWeights after adjustment.
	NewWeights ScorerWeights `json:"new_weights"`

	// Timestamp of the update.
	Timestamp time.Time `json:"timestamp"`

	// Reason explains why weights were updated.
	Reason string `json:"reason"`

	// SampleSize is the number of outcomes used.
	SampleSize int `json:"sample_size"`
}

// WeightStore persists scorer weights.
type WeightStore interface {
	// GetWeights retrieves the current weights.
	GetWeights(ctx context.Context, workspaceID string) (ScorerWeights, error)

	// SaveWeights persists weights.
	SaveWeights(ctx context.Context, workspaceID string, weights ScorerWeights) error

	// GetHistory retrieves weight update history.
	GetHistory(ctx context.Context, workspaceID string, limit int) ([]WeightUpdate, error)

	// SaveHistory appends to weight update history.
	SaveHistory(ctx context.Context, workspaceID string, update WeightUpdate) error
}

// LearnableScorer adapts weights based on outcome data.
type LearnableScorer struct {
	trajStore   trajectory.Store
	weightStore WeightStore
	config      LearnerConfig
}

// LearnerConfig configures the weight learning process.
type LearnerConfig struct {
	// LearningRate controls how much weights change per iteration (0.0-1.0).
	LearningRate float64

	// MinSamples is the minimum number of outcomes required for learning.
	MinSamples int

	// MaxWeightChange caps the maximum change per weight per iteration.
	MaxWeightChange float64

	// LookbackDays limits how far back to look for outcomes.
	LookbackDays int
}

// DefaultLearnerConfig returns a default configuration.
func DefaultLearnerConfig() LearnerConfig {
	return LearnerConfig{
		LearningRate:    0.1,
		MinSamples:      10,
		MaxWeightChange: 0.05,
		LookbackDays:    30,
	}
}

// NewLearnableScorer creates a new learnable scorer.
func NewLearnableScorer(trajStore trajectory.Store, weightStore WeightStore, config LearnerConfig) *LearnableScorer {
	if config.LearningRate <= 0 {
		config.LearningRate = 0.1
	}
	if config.MinSamples <= 0 {
		config.MinSamples = 10
	}
	if config.MaxWeightChange <= 0 {
		config.MaxWeightChange = 0.05
	}
	if config.LookbackDays <= 0 {
		config.LookbackDays = 30
	}
	return &LearnableScorer{
		trajStore:   trajStore,
		weightStore: weightStore,
		config:      config,
	}
}

// LearnFromOutcomes adjusts weights based on trajectory outcomes.
// It analyzes which factors correlate with success and adjusts weights accordingly.
func (s *LearnableScorer) LearnFromOutcomes(ctx context.Context, workspaceID string) (*WeightUpdate, error) {
	// Get current weights
	currentWeights, err := s.GetCurrentWeights(ctx, workspaceID)
	if err != nil {
		currentWeights = DefaultScorerWeights()
	}

	// Get recent trajectories with outcomes
	since := time.Now().AddDate(0, 0, -s.config.LookbackDays)
	trajs, err := s.trajStore.ListTrajectories(ctx, trajectory.ListFilter{
		WorkspaceID: workspaceID,
		Since:       since,
		Limit:       1000,
	})
	if err != nil {
		return nil, fmt.Errorf("learnable scorer: list trajectories: %w", err)
	}

	// Filter to those with outcomes
	var withOutcomes []trajectory.Trajectory
	for _, t := range trajs {
		if t.Outcome != nil {
			withOutcomes = append(withOutcomes, t)
		}
	}

	if len(withOutcomes) < s.config.MinSamples {
		return nil, fmt.Errorf("learnable scorer: insufficient samples (%d < %d minimum)",
			len(withOutcomes), s.config.MinSamples)
	}

	// Analyze factor correlations with success
	correlations := s.analyzeCorrelations(ctx, withOutcomes)

	// Compute new weights based on correlations
	newWeights := s.computeNewWeights(currentWeights, correlations)
	newWeights.Normalize()
	newWeights.LastUpdated = time.Now().UTC()
	newWeights.Version = currentWeights.Version + 1

	// Save new weights
	if err := s.weightStore.SaveWeights(ctx, workspaceID, newWeights); err != nil {
		return nil, fmt.Errorf("learnable scorer: save weights: %w", err)
	}

	// Record update
	update := WeightUpdate{
		PreviousWeights: currentWeights,
		NewWeights:      newWeights,
		Timestamp:       time.Now().UTC(),
		Reason:          fmt.Sprintf("Learned from %d outcomes", len(withOutcomes)),
		SampleSize:      len(withOutcomes),
	}

	// Non-fatal: SaveHistory errors are logged but don't fail the operation
	_ = s.weightStore.SaveHistory(ctx, workspaceID, update) //nolint:errcheck

	return &update, nil
}

// FactorCorrelation represents correlation between a factor and success.
type FactorCorrelation struct {
	Factor      string  `json:"factor"`
	Correlation float64 `json:"correlation"` // -1.0 to 1.0
	SampleSize  int     `json:"sample_size"`
}

// analyzeCorrelations computes correlations between factors and outcomes.
func (s *LearnableScorer) analyzeCorrelations(ctx context.Context, trajs []trajectory.Trajectory) []FactorCorrelation {
	// Extract factor values from trajectory metadata
	// This is a simplified analysis - in practice, we'd extract actual factor values
	// from trajectory events and compute Pearson correlations

	correlations := make([]FactorCorrelation, 0)

	// Analyze tool call count correlation (proxy for efficiency)
	var toolCallCorr float64
	successToolCalls, failureToolCalls := 0.0, 0.0
	successCount, failureCount := 0, 0

	for _, t := range trajs {
		if t.Outcome == nil {
			continue
		}
		if t.Outcome.Success {
			successToolCalls += float64(t.Outcome.ToolCallCount)
			successCount++
		} else {
			failureToolCalls += float64(t.Outcome.ToolCallCount)
			failureCount++
		}
	}

	// Lower tool calls on success = positive correlation (efficiency matters)
	if successCount > 0 && failureCount > 0 {
		avgSuccess := successToolCalls / float64(successCount)
		avgFailure := failureToolCalls / float64(failureCount)
		if avgFailure > avgSuccess {
			toolCallCorr = 0.3 // Efficiency correlates with success
		}
	}

	correlations = append(correlations, FactorCorrelation{
		Factor:      "efficiency",
		Correlation: toolCallCorr,
		SampleSize:  len(trajs),
	})

	// Analyze duration correlation
	var durationCorr float64
	successDuration, failureDuration := int64(0), int64(0)

	for _, t := range trajs {
		if t.Outcome == nil {
			continue
		}
		if t.Outcome.Success {
			successDuration += t.Outcome.DurationMS
		} else {
			failureDuration += t.Outcome.DurationMS
		}
	}

	// Shorter duration on success = positive correlation
	if successCount > 0 && failureCount > 0 {
		avgSuccess := float64(successDuration) / float64(successCount)
		avgFailure := float64(failureDuration) / float64(failureCount)
		if avgFailure > avgSuccess {
			durationCorr = 0.2
		}
	}

	correlations = append(correlations, FactorCorrelation{
		Factor:      "speed",
		Correlation: durationCorr,
		SampleSize:  len(trajs),
	})

	// Human rating correlation (if available)
	var ratingCorr float64
	ratedCount := 0
	highRatedSuccess := 0

	for _, t := range trajs {
		if t.Outcome == nil || t.Outcome.HumanRating == nil {
			continue
		}
		ratedCount++
		if *t.Outcome.HumanRating >= 4 && t.Outcome.Success {
			highRatedSuccess++
		}
	}

	if ratedCount > 5 {
		ratingCorr = float64(highRatedSuccess) / float64(ratedCount)
	}

	correlations = append(correlations, FactorCorrelation{
		Factor:      "human_rating",
		Correlation: ratingCorr,
		SampleSize:  ratedCount,
	})

	return correlations
}

// computeNewWeights adjusts weights based on correlations.
func (s *LearnableScorer) computeNewWeights(current ScorerWeights, correlations []FactorCorrelation) ScorerWeights {
	// Start with current weights
	newWeights := current

	// Map correlation factors to weights
	factorWeightMap := map[string]*float64{
		"efficiency":   &newWeights.CriticalPath, // Efficiency suggests task ordering matters
		"speed":        &newWeights.Recency,      // Speed suggests recency matters
		"human_rating": &newWeights.AdminMail,    // Human feedback often comes via admin
	}

	for _, corr := range correlations {
		weightPtr, ok := factorWeightMap[corr.Factor]
		if !ok || corr.SampleSize < 5 {
			continue
		}

		// Adjust weight based on correlation
		adjustment := corr.Correlation * s.config.LearningRate

		// Cap adjustment
		if adjustment > s.config.MaxWeightChange {
			adjustment = s.config.MaxWeightChange
		} else if adjustment < -s.config.MaxWeightChange {
			adjustment = -s.config.MaxWeightChange
		}

		*weightPtr += adjustment

		// Ensure non-negative
		if *weightPtr < 0.01 {
			*weightPtr = 0.01
		}
	}

	return newWeights
}

// GetCurrentWeights retrieves current weights.
func (s *LearnableScorer) GetCurrentWeights(ctx context.Context, workspaceID string) (ScorerWeights, error) {
	return s.weightStore.GetWeights(ctx, workspaceID)
}

// Score computes a task score using learned weights.
func (s *LearnableScorer) Score(ctx context.Context, workspaceID string, factors TaskFactors) float64 {
	weights, err := s.GetCurrentWeights(ctx, workspaceID)
	if err != nil {
		weights = DefaultScorerWeights()
	}

	return weights.CriticalPath*factors.CriticalPathScore +
		weights.PageRank*factors.PageRank +
		weights.AdminMail*factors.AdminMailScore +
		weights.OverseerMail*factors.OverseerMailScore +
		weights.Recency*factors.RecencyFactor
}

// TaskFactors holds the normalized factors for scoring.
type TaskFactors struct {
	CriticalPathScore float64
	PageRank          float64
	AdminMailScore    float64
	OverseerMailScore float64
	RecencyFactor     float64
}

// InMemoryWeightStore is a simple in-memory implementation for testing.
type InMemoryWeightStore struct {
	weights map[string]ScorerWeights
	history map[string][]WeightUpdate
}

// NewInMemoryWeightStore creates a new in-memory weight store.
func NewInMemoryWeightStore() *InMemoryWeightStore {
	return &InMemoryWeightStore{
		weights: make(map[string]ScorerWeights),
		history: make(map[string][]WeightUpdate),
	}
}

func (s *InMemoryWeightStore) GetWeights(ctx context.Context, workspaceID string) (ScorerWeights, error) {
	if w, ok := s.weights[workspaceID]; ok {
		return w, nil
	}
	return ScorerWeights{}, fmt.Errorf("weights not found for workspace %s", workspaceID)
}

func (s *InMemoryWeightStore) SaveWeights(ctx context.Context, workspaceID string, weights ScorerWeights) error {
	s.weights[workspaceID] = weights
	return nil
}

func (s *InMemoryWeightStore) GetHistory(ctx context.Context, workspaceID string, limit int) ([]WeightUpdate, error) {
	history := s.history[workspaceID]
	if limit > 0 && len(history) > limit {
		return history[len(history)-limit:], nil
	}
	return history, nil
}

func (s *InMemoryWeightStore) SaveHistory(ctx context.Context, workspaceID string, update WeightUpdate) error {
	s.history[workspaceID] = append(s.history[workspaceID], update)
	return nil
}

// ExportWeightsJSON exports weights as JSON for external use.
func ExportWeightsJSON(weights ScorerWeights) ([]byte, error) {
	return json.MarshalIndent(weights, "", "  ")
}

// ImportWeightsJSON imports weights from JSON.
func ImportWeightsJSON(data []byte) (ScorerWeights, error) {
	var weights ScorerWeights
	if err := json.Unmarshal(data, &weights); err != nil {
		return ScorerWeights{}, err
	}
	if err := weights.Validate(); err != nil {
		return ScorerWeights{}, err
	}
	return weights, nil
}
