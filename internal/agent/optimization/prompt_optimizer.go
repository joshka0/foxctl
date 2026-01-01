package optimization

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

// PromptOptimizer optimizes agent prompts based on execution outcomes.
// It implements concepts from COPRO (Collaborative Prompt Optimization) and
// MIPRO (Multi-stage Instruction Prompt Optimization).
type PromptOptimizer struct {
	trajStore    trajectory.Store
	patternStore PatternStore
	config       PromptOptimizerConfig

	// evalFunc evaluates a prompt candidate (pluggable for testing)
	evalFunc func(ctx context.Context, prompt string) (float64, error)
}

// PromptOptimizerConfig configures the prompt optimization process.
type PromptOptimizerConfig struct {
	// Mode selects the optimization strategy.
	// Options: "copro" (coordinate-based), "mipro-light", "mipro-medium", "mipro-heavy"
	Mode string

	// BreadthCandidates is the number of candidates in breadth iteration.
	BreadthCandidates int

	// DepthIterations is the number of depth refinement iterations.
	DepthIterations int

	// MinImprovement is the minimum score improvement to accept a change.
	MinImprovement float64

	// LookbackDays limits how far back to look for training data.
	LookbackDays int
}

// DefaultPromptOptimizerConfig returns a default configuration.
func DefaultPromptOptimizerConfig() PromptOptimizerConfig {
	return PromptOptimizerConfig{
		Mode:              "copro",
		BreadthCandidates: 5,
		DepthIterations:   3,
		MinImprovement:    0.05,
		LookbackDays:      30,
	}
}

// NewPromptOptimizer creates a new prompt optimizer.
func NewPromptOptimizer(trajStore trajectory.Store, patternStore PatternStore, config PromptOptimizerConfig) *PromptOptimizer {
	if config.BreadthCandidates <= 0 {
		config.BreadthCandidates = 5
	}
	if config.DepthIterations <= 0 {
		config.DepthIterations = 3
	}
	if config.LookbackDays <= 0 {
		config.LookbackDays = 30
	}
	return &PromptOptimizer{
		trajStore:    trajStore,
		patternStore: patternStore,
		config:       config,
	}
}

// SetEvalFunc sets a custom evaluation function for testing.
func (p *PromptOptimizer) SetEvalFunc(fn func(ctx context.Context, prompt string) (float64, error)) {
	p.evalFunc = fn
}

// ScoredPrompt represents a prompt candidate with its score.
type ScoredPrompt struct {
	// Prompt is the instruction text.
	Prompt string `json:"prompt"`

	// Score is the evaluation score (higher is better).
	Score float64 `json:"score"`

	// Improvements lists specific improvements made.
	Improvements []string `json:"improvements,omitempty"`

	// Generation tracks which iteration produced this prompt.
	Generation int `json:"generation"`
}

// OptimizationResult contains the result of a prompt optimization run.
type OptimizationResult struct {
	// OriginalPrompt is the starting prompt.
	OriginalPrompt string `json:"original_prompt"`

	// OriginalScore is the starting score.
	OriginalScore float64 `json:"original_score"`

	// OptimizedPrompt is the best prompt found.
	OptimizedPrompt string `json:"optimized_prompt"`

	// OptimizedScore is the best score achieved.
	OptimizedScore float64 `json:"optimized_score"`

	// Improvement is the relative improvement (0.0-1.0).
	Improvement float64 `json:"improvement"`

	// Candidates lists all candidates evaluated.
	Candidates []ScoredPrompt `json:"candidates,omitempty"`

	// Duration is how long optimization took.
	Duration time.Duration `json:"duration,format:units"`

	// Mode is the optimization mode used.
	Mode string `json:"mode"`
}

// OptimizeInstruction runs prompt optimization for an agent role.
func (p *PromptOptimizer) OptimizeInstruction(ctx context.Context, workspaceID, agentRole, currentPrompt string) (*OptimizationResult, error) {
	startTime := time.Now()

	// Evaluate current prompt
	currentScore, err := p.evaluate(ctx, workspaceID, agentRole, currentPrompt)
	if err != nil {
		return nil, fmt.Errorf("prompt optimizer: evaluate current: %w", err)
	}

	result := &OptimizationResult{
		OriginalPrompt:  currentPrompt,
		OriginalScore:   currentScore,
		OptimizedPrompt: currentPrompt,
		OptimizedScore:  currentScore,
		Mode:            p.config.Mode,
	}

	// Run breadth phase: generate and evaluate multiple candidates
	candidates, err := p.runBreadthPhase(ctx, workspaceID, agentRole, currentPrompt)
	if err != nil {
		return nil, fmt.Errorf("prompt optimizer: breadth phase: %w", err)
	}

	result.Candidates = candidates

	// Find best candidate
	bestCandidate := p.findBest(candidates)
	if bestCandidate != nil && bestCandidate.Score > currentScore+p.config.MinImprovement {
		result.OptimizedPrompt = bestCandidate.Prompt
		result.OptimizedScore = bestCandidate.Score
	}

	// Run depth phase: refine the best candidate
	if p.config.DepthIterations > 0 && result.OptimizedScore > currentScore {
		refined, err := p.runDepthPhase(ctx, workspaceID, agentRole, result.OptimizedPrompt)
		if err == nil && refined.Score > result.OptimizedScore+p.config.MinImprovement {
			result.OptimizedPrompt = refined.Prompt
			result.OptimizedScore = refined.Score
			result.Candidates = append(result.Candidates, *refined)
		}
	}

	// Compute improvement
	if result.OriginalScore > 0 {
		result.Improvement = (result.OptimizedScore - result.OriginalScore) / result.OriginalScore
	}

	result.Duration = time.Since(startTime)
	return result, nil
}

// runBreadthPhase generates and evaluates multiple prompt candidates.
func (p *PromptOptimizer) runBreadthPhase(ctx context.Context, workspaceID, agentRole, basePrompt string) ([]ScoredPrompt, error) {
	// Get insights from successful trajectories
	insights, err := p.gatherInsights(ctx, workspaceID, agentRole)
	if err != nil {
		insights = &promptInsights{} // Continue without insights
	}

	// Generate candidates with different strategies
	candidates := make([]ScoredPrompt, 0, p.config.BreadthCandidates)

	strategies := []struct {
		name     string
		generate func(string, *promptInsights) string
	}{
		{"add_specificity", p.addSpecificity},
		{"add_examples", p.addExamples},
		{"add_constraints", p.addConstraints},
		{"simplify", p.simplify},
		{"restructure", p.restructure},
	}

	for i := 0; i < p.config.BreadthCandidates && i < len(strategies); i++ {
		strategy := strategies[i%len(strategies)]
		candidatePrompt := strategy.generate(basePrompt, insights)

		score, err := p.evaluate(ctx, workspaceID, agentRole, candidatePrompt)
		if err != nil {
			continue
		}

		candidates = append(candidates, ScoredPrompt{
			Prompt:       candidatePrompt,
			Score:        score,
			Improvements: []string{strategy.name},
			Generation:   1,
		})
	}

	// Sort by score descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	return candidates, nil
}

// runDepthPhase refines the best candidate iteratively.
func (p *PromptOptimizer) runDepthPhase(ctx context.Context, workspaceID, agentRole, bestPrompt string) (*ScoredPrompt, error) {
	current := bestPrompt
	currentScore, err := p.evaluate(ctx, workspaceID, agentRole, current)
	if err != nil {
		currentScore = 0.0 // Start with zero if evaluation fails
	}

	insights, err := p.gatherInsights(ctx, workspaceID, agentRole)
	if err != nil {
		insights = &promptInsights{} // Continue with empty insights if gathering fails
	}

	for i := 0; i < p.config.DepthIterations; i++ {
		// Try small refinements
		refined := p.refinePrompt(current, insights, i)

		score, err := p.evaluate(ctx, workspaceID, agentRole, refined)
		if err != nil {
			continue
		}

		if score > currentScore+p.config.MinImprovement {
			current = refined
			currentScore = score
		}
	}

	return &ScoredPrompt{
		Prompt:     current,
		Score:      currentScore,
		Generation: p.config.DepthIterations + 1,
	}, nil
}

// promptInsights holds insights extracted from trajectory data.
type promptInsights struct {
	// SuccessfulPatterns are common patterns in successful executions.
	SuccessfulPatterns []string

	// CommonErrors are frequent error types.
	CommonErrors []string

	// ToolPreferences are tools that lead to success.
	ToolPreferences []string

	// EffectivePhrases are phrases from high-rated responses.
	EffectivePhrases []string
}

// gatherInsights extracts insights from trajectory data.
func (p *PromptOptimizer) gatherInsights(ctx context.Context, workspaceID, agentRole string) (*promptInsights, error) {
	insights := &promptInsights{}

	// Get successful patterns
	patterns, err := p.patternStore.GetTopPatterns(ctx, agentRole, 10)
	if err == nil {
		for _, pat := range patterns {
			if pat.SuccessRate() > 0.7 && len(pat.ToolSequence) > 0 {
				insights.ToolPreferences = append(insights.ToolPreferences, pat.ToolSequence...)
			}
		}
	}

	// Get high-rated trajectories for effective phrases
	minRating := 4
	trajs, err := p.trajStore.ListByOutcome(ctx, trajectory.OutcomeFilter{
		WorkspaceID: workspaceID,
		AgentRole:   agentRole,
		MinRating:   &minRating,
		Limit:       20,
	})
	if err == nil {
		for _, t := range trajs {
			if t.Outcome != nil && t.Outcome.Feedback != "" {
				// Extract positive feedback phrases
				insights.EffectivePhrases = append(insights.EffectivePhrases,
					extractKeyPhrases(t.Outcome.Feedback)...)
			}
		}
	}

	return insights, nil
}

// extractKeyPhrases extracts key phrases from text.
func extractKeyPhrases(text string) []string {
	// Split on common delimiters using FieldsFunc for efficiency
	isDelimiter := func(r rune) bool {
		return r == '.' || r == ',' || r == '!' || r == '?'
	}

	parts := strings.FieldsFunc(text, isDelimiter)

	// Deduplicate using a map
	seen := make(map[string]struct{})
	phrases := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len(part) > 10 && len(part) < 100 {
			if _, exists := seen[part]; !exists {
				seen[part] = struct{}{}
				phrases = append(phrases, part)
			}
		}
	}
	return phrases
}

// evaluate scores a prompt candidate.
func (p *PromptOptimizer) evaluate(ctx context.Context, workspaceID, agentRole, prompt string) (float64, error) {
	// Use custom eval function if provided (for testing)
	if p.evalFunc != nil {
		return p.evalFunc(ctx, prompt)
	}

	// Default evaluation based on trajectory data correlation
	// Score prompts based on similarity to successful execution patterns
	score := 0.5 // Base score

	// Get successful patterns
	patterns, err := p.patternStore.GetTopPatterns(ctx, agentRole, 10)
	if err != nil {
		return score, nil
	}

	// Score based on pattern coverage
	promptLower := strings.ToLower(prompt)
	for _, pat := range patterns {
		if pat.SuccessRate() > 0.7 {
			// Check if prompt mentions tools that lead to success
			for _, tool := range pat.ToolSequence {
				if strings.Contains(promptLower, strings.ToLower(tool)) {
					score += 0.05 * pat.SuccessRate()
				}
			}
			// Check context similarity
			if pat.Context != "" && strings.Contains(promptLower, strings.ToLower(pat.Context)) {
				score += 0.03 * pat.SuccessRate()
			}
		}
	}

	// Cap score at 1.0
	if score > 1.0 {
		score = 1.0
	}

	return score, nil
}

// findBest returns the highest-scoring candidate.
func (p *PromptOptimizer) findBest(candidates []ScoredPrompt) *ScoredPrompt {
	if len(candidates) == 0 {
		return nil
	}

	best := &candidates[0]
	for i := 1; i < len(candidates); i++ {
		if candidates[i].Score > best.Score {
			best = &candidates[i]
		}
	}
	return best
}

// Prompt modification strategies

func (p *PromptOptimizer) addSpecificity(prompt string, insights *promptInsights) string {
	// Add specific guidance based on successful patterns
	additions := []string{}

	if len(insights.ToolPreferences) > 0 {
		// Deduplicate
		seen := make(map[string]bool)
		var tools []string
		for _, t := range insights.ToolPreferences {
			if !seen[t] {
				seen[t] = true
				tools = append(tools, t)
			}
		}
		if len(tools) > 3 {
			tools = tools[:3]
		}
		additions = append(additions,
			fmt.Sprintf("Consider using these tools when appropriate: %s.", strings.Join(tools, ", ")))
	}

	if len(additions) > 0 {
		return prompt + "\n\n" + strings.Join(additions, " ")
	}
	return prompt
}

func (p *PromptOptimizer) addExamples(prompt string, insights *promptInsights) string {
	if len(insights.EffectivePhrases) == 0 {
		return prompt
	}

	// Add example phrases from successful executions
	examples := insights.EffectivePhrases
	if len(examples) > 2 {
		examples = examples[:2]
	}

	return prompt + "\n\nFor reference, here are patterns from successful executions:\n- " +
		strings.Join(examples, "\n- ")
}

func (p *PromptOptimizer) addConstraints(prompt string, insights *promptInsights) string {
	// Add constraints based on common errors
	constraints := []string{
		"Be thorough but efficient - avoid unnecessary steps.",
		"Verify your work before reporting completion.",
	}

	if len(insights.CommonErrors) > 0 {
		constraints = append(constraints,
			fmt.Sprintf("Avoid common pitfalls: %s", strings.Join(insights.CommonErrors[:min(2, len(insights.CommonErrors))], ", ")))
	}

	return prompt + "\n\nImportant guidelines:\n- " + strings.Join(constraints, "\n- ")
}

func (p *PromptOptimizer) simplify(prompt string, _ *promptInsights) string {
	// Remove redundant phrases
	redundant := []string{
		"Please note that",
		"It is important to",
		"You should always",
		"Make sure to",
		"Don't forget to",
	}

	result := prompt
	for _, r := range redundant {
		result = strings.ReplaceAll(result, r, "")
	}

	// Clean up double spaces
	for strings.Contains(result, "  ") {
		result = strings.ReplaceAll(result, "  ", " ")
	}

	return strings.TrimSpace(result)
}

func (p *PromptOptimizer) restructure(prompt string, _ *promptInsights) string {
	// Split into sections if long
	if len(prompt) < 200 {
		return prompt
	}

	// Try to identify and separate main instruction from details
	sentences := strings.Split(prompt, ". ")
	if len(sentences) < 3 {
		return prompt
	}

	// First sentence as main instruction, rest as details
	main := sentences[0]
	details := sentences[1:]

	return main + ".\n\nDetails:\n- " + strings.Join(details, "\n- ")
}

func (p *PromptOptimizer) refinePrompt(prompt string, insights *promptInsights, iteration int) string {
	// Apply different refinements based on iteration
	switch iteration % 3 {
	case 0:
		// Tighten language
		return strings.ReplaceAll(prompt, "try to", "")
	case 1:
		// Add clarity markers
		if !strings.Contains(prompt, "Step ") && len(prompt) > 100 {
			parts := strings.Split(prompt, ". ")
			var numbered []string
			for i, part := range parts {
				if len(part) > 20 {
					numbered = append(numbered, fmt.Sprintf("Step %d: %s", i+1, part))
				} else {
					numbered = append(numbered, part)
				}
			}
			return strings.Join(numbered, ". ")
		}
		return prompt
	case 2:
		// Emphasize key points
		if len(insights.ToolPreferences) > 0 {
			return prompt + "\n\nKey: " + strings.Join(insights.ToolPreferences[:min(3, len(insights.ToolPreferences))], ", ")
		}
		return prompt
	}
	return prompt
}

// min returns the smaller of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
