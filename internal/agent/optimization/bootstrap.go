package optimization

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

// BootstrapConfig configures the bootstrap few-shot optimizer.
type BootstrapConfig struct {
	// MinSuccessRate is the minimum success rate for including an example (default: 0.8).
	MinSuccessRate float64

	// MaxExamples is the maximum number of examples to include (default: 5).
	MaxExamples int

	// DiversityWeight weights diversity vs recency in example selection (0.0-1.0).
	// Higher values favor more diverse examples, lower values favor recent ones.
	DiversityWeight float64

	// MinHumanRating filters examples by minimum human rating (optional).
	MinHumanRating *int

	// LookbackDays limits how far back to look for examples (default: 30).
	LookbackDays int
}

// DefaultBootstrapConfig returns a default configuration.
func DefaultBootstrapConfig() BootstrapConfig {
	return BootstrapConfig{
		MinSuccessRate:  0.8,
		MaxExamples:     5,
		DiversityWeight: 0.5,
		LookbackDays:    30,
	}
}

// Example represents a few-shot example extracted from trajectory data.
type Example struct {
	// Input is the task or question given.
	Input string `json:"input"`

	// Output is the successful response or result.
	Output string `json:"output"`

	// Tools lists the tools used (if available).
	Tools []string `json:"tools,omitempty"`

	// Rating is the human rating (if available).
	Rating *int `json:"rating,omitempty"`

	// Diversity score (internal use).
	diversityScore float64
}

// BootstrapOptimizer generates few-shot examples from successful trajectories.
type BootstrapOptimizer struct {
	trajStore    trajectory.Store
	patternStore PatternStore
	config       BootstrapConfig
}

// NewBootstrapOptimizer creates a new bootstrap optimizer.
func NewBootstrapOptimizer(trajStore trajectory.Store, patternStore PatternStore, config BootstrapConfig) *BootstrapOptimizer {
	if config.MinSuccessRate <= 0 {
		config.MinSuccessRate = 0.8
	}
	if config.MaxExamples <= 0 {
		config.MaxExamples = 5
	}
	if config.LookbackDays <= 0 {
		config.LookbackDays = 30
	}
	return &BootstrapOptimizer{
		trajStore:    trajStore,
		patternStore: patternStore,
		config:       config,
	}
}

// GenerateExamples extracts few-shot examples for an agent role.
func (b *BootstrapOptimizer) GenerateExamples(ctx context.Context, workspaceID, agentRole string) ([]Example, error) {
	// Query successful trajectories
	success := true
	since := time.Now().AddDate(0, 0, -b.config.LookbackDays)

	filter := trajectory.OutcomeFilter{
		WorkspaceID: workspaceID,
		AgentRole:   agentRole,
		Success:     &success,
		Since:       since,
		Limit:       100, // Get more than we need for diversity selection
	}

	if b.config.MinHumanRating != nil {
		filter.MinRating = b.config.MinHumanRating
	}

	trajs, err := b.trajStore.ListByOutcome(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: list trajectories: %w", err)
	}

	// Convert to examples
	examples := make([]Example, 0, len(trajs))
	for _, traj := range trajs {
		example, err := b.extractExample(ctx, traj)
		if err != nil {
			continue // Skip failed extractions
		}
		if example != nil {
			examples = append(examples, *example)
		}
	}

	if len(examples) == 0 {
		return examples, nil
	}

	// Select diverse examples
	selected := b.selectDiverse(examples)

	// Limit to max examples
	if len(selected) > b.config.MaxExamples {
		selected = selected[:b.config.MaxExamples]
	}

	return selected, nil
}

// extractExample extracts an example from a trajectory.
func (b *BootstrapOptimizer) extractExample(ctx context.Context, traj trajectory.Trajectory) (*Example, error) {
	// Get events for this trajectory
	events, err := b.trajStore.ListEvents(ctx, trajectory.EventFilter{
		TrajectoryID: traj.ID,
		Limit:        1000,
	})
	if err != nil {
		return nil, err
	}

	var input, output string
	var tools []string

	for _, event := range events {
		switch event.Kind {
		case trajectory.EventKindUserRequest:
			// Extract the original request/question
			if event.DataInline != nil {
				if text, ok := event.DataInline["text"].(string); ok {
					input = text
				}
			}
		case trajectory.EventKindToolCall:
			// Extract tool names used
			if event.DataInline != nil {
				if toolName, ok := event.DataInline["tool"].(string); ok {
					tools = append(tools, toolName)
				}
			}
		case trajectory.EventKindAgentThought:
			// Look for final output/result
			if event.DataInline != nil {
				if result, ok := event.DataInline["result"].(string); ok && result != "" {
					output = result
				} else if thought, ok := event.DataInline["thought"].(string); ok && thought != "" {
					// Use thought as fallback if no explicit result
					output = thought
				}
			}
		}
	}

	// Need both input and output for a valid example
	if input == "" || output == "" {
		return nil, nil
	}

	example := &Example{
		Input:  input,
		Output: output,
		Tools:  tools,
	}

	if traj.Outcome != nil && traj.Outcome.HumanRating != nil {
		example.Rating = traj.Outcome.HumanRating
	}

	return example, nil
}

// selectDiverse selects a diverse set of examples using MMR (Maximal Marginal Relevance).
func (b *BootstrapOptimizer) selectDiverse(examples []Example) []Example {
	if len(examples) <= b.config.MaxExamples {
		return examples
	}

	// Score each example for diversity (based on input uniqueness)
	for i := range examples {
		examples[i].diversityScore = b.computeDiversityScore(examples[i], examples)
	}

	// Sort by combined score (diversity + quality indicators)
	sort.Slice(examples, func(i, j int) bool {
		scoreI := b.combinedScore(examples[i])
		scoreJ := b.combinedScore(examples[j])
		return scoreI > scoreJ
	})

	// Use MMR-style selection: iteratively pick examples that are different from selected ones
	selected := make([]Example, 0, b.config.MaxExamples)
	remaining := make([]Example, len(examples))
	copy(remaining, examples)

	for len(selected) < b.config.MaxExamples && len(remaining) > 0 {
		// Find best candidate considering diversity from already selected
		bestIdx := 0
		bestScore := -1.0

		for i, candidate := range remaining {
			// Compute MMR score: relevance - lambda * max_similarity
			relevance := b.combinedScore(candidate)
			maxSim := 0.0
			for _, s := range selected {
				sim := b.similarity(candidate.Input, s.Input)
				if sim > maxSim {
					maxSim = sim
				}
			}
			mmrScore := relevance - b.config.DiversityWeight*maxSim

			if mmrScore > bestScore {
				bestScore = mmrScore
				bestIdx = i
			}
		}

		// Add best candidate to selected
		selected = append(selected, remaining[bestIdx])
		// Remove from remaining
		remaining = append(remaining[:bestIdx], remaining[bestIdx+1:]...)
	}

	return selected
}

// computeDiversityScore computes how different this example is from others.
func (b *BootstrapOptimizer) computeDiversityScore(example Example, all []Example) float64 {
	if len(all) <= 1 {
		return 1.0
	}

	totalSim := 0.0
	count := 0
	for _, other := range all {
		if other.Input != example.Input {
			totalSim += b.similarity(example.Input, other.Input)
			count++
		}
	}

	if count == 0 {
		return 1.0
	}

	avgSim := totalSim / float64(count)
	return 1.0 - avgSim // Higher diversity = lower average similarity
}

// combinedScore combines quality indicators into a single score.
func (b *BootstrapOptimizer) combinedScore(example Example) float64 {
	score := 0.5 // Base score

	// Rating bonus
	if example.Rating != nil {
		score += float64(*example.Rating) / 10.0 // Rating 1-5 -> 0.1-0.5
	}

	// Diversity bonus
	score += example.diversityScore * b.config.DiversityWeight

	// Tool usage bonus (examples with tool usage are often more instructive)
	if len(example.Tools) > 0 {
		score += 0.1
	}

	return score
}

// similarity computes simple text similarity between two strings.
func (b *BootstrapOptimizer) similarity(textA, textB string) float64 {
	// Simple Jaccard similarity on words
	wordsA := strings.Fields(strings.ToLower(textA))
	wordsB := strings.Fields(strings.ToLower(textB))

	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0.0
	}

	setA := make(map[string]bool)
	for _, w := range wordsA {
		setA[w] = true
	}

	setB := make(map[string]bool)
	for _, w := range wordsB {
		setB[w] = true
	}

	intersection := 0
	for w := range setA {
		if setB[w] {
			intersection++
		}
	}

	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// FormatExamplesForPrompt formats examples as a string for prompt injection.
func (b *BootstrapOptimizer) FormatExamplesForPrompt(examples []Example) string {
	if len(examples) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n## Few-Shot Examples\n\n")
	sb.WriteString("Here are examples of successful task completions:\n\n")

	for i, example := range examples {
		sb.WriteString(fmt.Sprintf("### Example %d\n", i+1))
		sb.WriteString(fmt.Sprintf("**Input:** %s\n", truncate(example.Input, 200)))
		sb.WriteString(fmt.Sprintf("**Output:** %s\n", truncate(example.Output, 500)))
		if len(example.Tools) > 0 {
			sb.WriteString(fmt.Sprintf("**Tools used:** %s\n", strings.Join(example.Tools, ", ")))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// truncate truncates a string to maxLen runes (not bytes).
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// GetExampleStats returns statistics about available examples.
func (b *BootstrapOptimizer) GetExampleStats(ctx context.Context, workspaceID, agentRole string) (*ExampleStats, error) {
	examples, err := b.GenerateExamples(ctx, workspaceID, agentRole)
	if err != nil {
		return nil, err
	}

	stats := &ExampleStats{
		TotalAvailable:     len(examples),
		AvgToolsPerExample: 0,
		HasRatings:         0,
	}

	if len(examples) == 0 {
		return stats, nil
	}

	totalTools := 0
	totalRating := 0
	ratedCount := 0

	for _, ex := range examples {
		totalTools += len(ex.Tools)
		if ex.Rating != nil {
			stats.HasRatings++
			totalRating += *ex.Rating
			ratedCount++
		}
	}

	stats.AvgToolsPerExample = float64(totalTools) / float64(len(examples))
	if ratedCount > 0 {
		avgRating := float64(totalRating) / float64(ratedCount)
		stats.AvgRating = &avgRating
	}

	return stats, nil
}

// ExampleStats provides statistics about available examples.
type ExampleStats struct {
	// TotalAvailable is the total number of examples available.
	TotalAvailable int `json:"total_available"`

	// AvgToolsPerExample is the average number of tools per example.
	AvgToolsPerExample float64 `json:"avg_tools_per_example"`

	// HasRatings is the number of examples with human ratings.
	HasRatings int `json:"has_ratings"`

	// AvgRating is the average rating (if any are rated).
	AvgRating *float64 `json:"avg_rating,omitempty"`
}
