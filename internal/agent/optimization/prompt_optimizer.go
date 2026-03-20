package optimization

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/trajectory"
	"github.com/jkatigb/agentctl/internal/verification"
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

	judgeOutputFunc func(ctx context.Context, systemPrompt, userPrompt string) (string, error)
	judgeScoreCache map[string]float64
	judgeCaseCache  map[string]promptJudgeCaseEvaluation
	cacheMu         sync.RWMutex

	transcriptExamples []TranscriptTrainingExample
	preferenceExamples []PromptPreferenceExample
}

type promptJudgeCaseEvaluation struct {
	Output   string
	Result   PromptJudgeResult
	Feedback string
}

// PromptOptimizerConfig configures the prompt optimization process.
type PromptOptimizerConfig struct {
	// Mode selects the optimization strategy.
	// Options: "copro" (coordinate-based), "mipro-light", "mipro-medium",
	// "mipro-heavy", "gepa" (reflective GEPA-style prompt evolution)
	Mode string

	// Backend selects the optimizer backend. Options: "auto", "agentctl", "dspy-go".
	Backend string

	// BreadthCandidates is the number of candidates in breadth iteration.
	BreadthCandidates int

	// DepthIterations is the number of depth refinement iterations.
	DepthIterations int

	// MinImprovement is the minimum score improvement to accept a change.
	MinImprovement float64

	// LookbackDays limits how far back to look for training data.
	LookbackDays int

	// PrimaryLLM configures the primary model used by backends that require live LLM calls.
	PrimaryLLM *PromptOptimizerLLMTarget

	// FallbackLLM configures an optional fallback model for live-call backends.
	FallbackLLM *PromptOptimizerLLMTarget

	// TargetProfile describes the prompt family this optimization is targeting.
	TargetProfile string
}

// PromptOptimizerLLMTarget describes one OpenAI-compatible model target.
type PromptOptimizerLLMTarget struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url,omitempty"`
	APIKey   string `json:"-"`
	Model    string `json:"model"`
}

func (t PromptOptimizerLLMTarget) String() string {
	return fmt.Sprintf("PromptOptimizerLLMTarget{provider=%q, model=%q}", strings.TrimSpace(t.Provider), strings.TrimSpace(t.Model))
}

// DefaultPromptOptimizerConfig returns a default configuration.
func DefaultPromptOptimizerConfig() PromptOptimizerConfig {
	return PromptOptimizerConfig{
		Mode:              "copro",
		Backend:           "agentctl",
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
		trajStore:       trajStore,
		patternStore:    patternStore,
		config:          config,
		judgeScoreCache: make(map[string]float64),
		judgeCaseCache:  make(map[string]promptJudgeCaseEvaluation),
	}
}

func (p *PromptOptimizer) resolvedBackend() string {
	if p == nil {
		return "agentctl"
	}
	backend := strings.ToLower(strings.TrimSpace(p.config.Backend))
	switch backend {
	case "", "auto":
		if p.isGEPAMode() {
			return "dspy-go"
		}
		return "agentctl"
	case "dspy-go", "agentctl":
		return backend
	default:
		return "agentctl"
	}
}

// SetEvalFunc sets a custom evaluation function for testing.
func (p *PromptOptimizer) SetEvalFunc(fn func(ctx context.Context, prompt string) (float64, error)) {
	p.evalFunc = fn
}

// SetJudgeOutputFunc injects a custom prompt->output evaluator for testing.
func (p *PromptOptimizer) SetJudgeOutputFunc(fn func(ctx context.Context, systemPrompt, userPrompt string) (string, error)) {
	p.judgeOutputFunc = fn
	p.cacheMu.Lock()
	p.judgeScoreCache = make(map[string]float64)
	p.judgeCaseCache = make(map[string]promptJudgeCaseEvaluation)
	p.cacheMu.Unlock()
}

// SetTranscriptExamples injects transcript-derived dataset rows into optimization.
func (p *PromptOptimizer) SetTranscriptExamples(examples []TranscriptTrainingExample) {
	p.transcriptExamples = append([]TranscriptTrainingExample(nil), examples...)
	p.cacheMu.Lock()
	p.judgeScoreCache = make(map[string]float64)
	p.judgeCaseCache = make(map[string]promptJudgeCaseEvaluation)
	p.cacheMu.Unlock()
}

// SetPreferenceExamples injects ranked preference dataset rows into optimization.
func (p *PromptOptimizer) SetPreferenceExamples(examples []PromptPreferenceExample) {
	p.preferenceExamples = append([]PromptPreferenceExample(nil), examples...)
	p.cacheMu.Lock()
	p.judgeScoreCache = make(map[string]float64)
	p.judgeCaseCache = make(map[string]promptJudgeCaseEvaluation)
	p.cacheMu.Unlock()
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
	if p.resolvedBackend() == "dspy-go" && p.isGEPAMode() {
		return p.optimizeInstructionDSPyGo(ctx, workspaceID, agentRole, currentPrompt)
	}

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

// ProposeCandidates generates multiple scored prompt candidates without selecting a single winner.
func (p *PromptOptimizer) ProposeCandidates(ctx context.Context, workspaceID, agentRole, basePrompt string, count int) ([]ScoredPrompt, error) {
	if p.resolvedBackend() == "dspy-go" && p.isGEPAMode() {
		return p.proposeCandidatesDSPyGo(ctx, workspaceID, agentRole, basePrompt, count)
	}

	if strings.TrimSpace(basePrompt) == "" {
		return nil, fmt.Errorf("prompt optimizer: base prompt is required")
	}
	if count <= 0 {
		count = p.config.BreadthCandidates
	}

	insights, err := p.gatherInsights(ctx, workspaceID, agentRole)
	if err != nil {
		insights = &promptInsights{}
	}

	strategies := p.breadthStrategies()
	if len(strategies) == 0 {
		return nil, fmt.Errorf("prompt optimizer: no candidate strategies configured")
	}

	candidates := make([]ScoredPrompt, 0, count)
	seen := map[string]struct{}{}
	appendCandidate := func(prompt string, improvements []string, generation int) error {
		prompt = strings.TrimSpace(prompt)
		if prompt == "" {
			return nil
		}
		if _, ok := seen[prompt]; ok {
			return nil
		}
		score, err := p.evaluate(ctx, workspaceID, agentRole, prompt)
		if err != nil {
			return err
		}
		seen[prompt] = struct{}{}
		candidates = append(candidates, ScoredPrompt{
			Prompt:       prompt,
			Score:        score,
			Improvements: append([]string(nil), improvements...),
			Generation:   generation,
		})
		return nil
	}

	for _, strategy := range strategies {
		if len(candidates) >= count {
			break
		}
		if err := appendCandidate(strategy.generate(basePrompt, insights), []string{strategy.name}, 1); err != nil {
			continue
		}
	}

	for iteration := 0; len(candidates) < count && len(candidates) > 0; iteration++ {
		seed := candidates[iteration%len(candidates)]
		refined := p.refinePrompt(seed.Prompt, insights, iteration)
		improvements := append(append([]string(nil), seed.Improvements...), fmt.Sprintf("refine_%d", iteration+1))
		if err := appendCandidate(refined, improvements, seed.Generation+1); err != nil {
			continue
		}
		if iteration > count*3 {
			break
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].Prompt < candidates[j].Prompt
		}
		return candidates[i].Score > candidates[j].Score
	})

	if len(candidates) > count {
		candidates = candidates[:count]
	}
	return candidates, nil
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
	strategies := p.breadthStrategies()

	for i := 0; i < p.config.BreadthCandidates && i < len(strategies); i++ {
		strategy := strategies[i%len(strategies)]
		candidatePrompt := strategy.generate(basePrompt, insights)
		if strings.TrimSpace(candidatePrompt) == "" {
			continue
		}

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

func (p *PromptOptimizer) breadthStrategies() []promptMutationStrategy {
	if p.isGEPAMode() {
		return []promptMutationStrategy{
			{name: "gepa_address_weaknesses", generate: p.addressWeaknesses},
			{name: "gepa_apply_improvements", generate: p.applyImprovementActions},
			{name: "gepa_reinforce_strengths", generate: p.reinforceStrengths},
			{name: "gepa_use_transcript_examples", generate: p.addTranscriptExamples},
			{name: "gepa_use_preference_winners", generate: p.applyPreferredPromptLines},
			{name: "add_specificity", generate: p.addSpecificity},
		}
	}

	return []promptMutationStrategy{
		{name: "add_specificity", generate: p.addSpecificity},
		{name: "add_examples", generate: p.addExamples},
		{name: "add_constraints", generate: p.addConstraints},
		{name: "simplify", generate: p.simplify},
		{name: "restructure", generate: p.restructure},
	}
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

	// CommonStrengths are frequently observed strengths across reflections.
	CommonStrengths []string

	// CommonWeaknesses are frequently observed weaknesses across reflections.
	CommonWeaknesses []string

	// ImprovementActions are high-impact actions extracted from reflections.
	ImprovementActions []string

	// ExamplePairs are compact transcript-grounded examples to inject into prompts.
	ExamplePairs []string

	// PreferredPromptLines are high-signal prompt directives extracted from winning variants.
	PreferredPromptLines []string
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

	maxRating := 2
	lowRated, err := p.trajStore.ListByOutcome(ctx, trajectory.OutcomeFilter{
		WorkspaceID: workspaceID,
		AgentRole:   agentRole,
		MaxRating:   &maxRating,
		Limit:       20,
	})
	if err == nil {
		for _, t := range lowRated {
			if t.Outcome != nil && t.Outcome.Feedback != "" {
				insights.CommonErrors = append(insights.CommonErrors,
					extractKeyPhrases(t.Outcome.Feedback)...)
			}
		}
	}

	// GEPA-style prompt evolution benefits from the same trajectory reflections
	// the rest of the optimization stack already computes.
	reflectionConfig := DefaultReflectionConfig()
	reflectionConfig.LookbackDays = p.config.LookbackDays
	reflectionConfig.MinEventsForReflection = 3
	reflectionConfig.MinTrajectoriesForImprovement = 2
	reflectionEngine := NewReflectionEngine(p.trajStore, p.patternStore, reflectionConfig)

	if summary, err := reflectionEngine.GenerateSummary(ctx, workspaceID, agentRole); err == nil && summary != nil {
		insights.CommonStrengths = topPatternTexts(summary.CommonStrengths, 3)
		insights.CommonWeaknesses = topPatternTexts(summary.CommonWeaknesses, 3)
	}
	if improvements, err := reflectionEngine.GenerateImprovements(ctx, workspaceID, agentRole); err == nil {
		insights.ImprovementActions = topImprovementActions(improvements, 3)
	}

	p.mergeTranscriptDatasetInsights(insights)
	p.mergePreferenceDatasetInsights(insights)

	return insights, nil
}

func (p *PromptOptimizer) mergeTranscriptDatasetInsights(insights *promptInsights) {
	if p == nil || insights == nil || len(p.transcriptExamples) == 0 {
		return
	}
	for _, example := range p.transcriptExamples {
		rating := example.Metadata.Rating
		outcome := strings.ToLower(strings.TrimSpace(example.Metadata.Outcome))
		successLike := rating >= 4 || outcome == "success"
		failureLike := (rating > 0 && rating <= 2) || outcome == "failure"

		if successLike {
			insights.EffectivePhrases = append(insights.EffectivePhrases,
				extractKeyPhrases(example.Output.Response)...)
			insights.ToolPreferences = append(insights.ToolPreferences, example.Output.ToolsUsed...)
			pair := compactTranscriptPair(example)
			if pair != "" {
				insights.ExamplePairs = append(insights.ExamplePairs, pair)
			}
		}
		if failureLike {
			if strings.TrimSpace(example.Metadata.Notes) != "" {
				insights.CommonErrors = append(insights.CommonErrors,
					extractKeyPhrases(example.Metadata.Notes)...)
			}
			insights.CommonErrors = append(insights.CommonErrors,
				extractKeyPhrases(example.Output.Response)...)
		}
	}
	insights.EffectivePhrases = uniqueStrings(insights.EffectivePhrases)
	insights.ToolPreferences = uniqueStrings(insights.ToolPreferences)
	insights.CommonErrors = uniqueStrings(insights.CommonErrors)
	insights.ExamplePairs = uniqueStrings(insights.ExamplePairs)
}

func compactTranscriptPair(example TranscriptTrainingExample) string {
	request := strings.TrimSpace(example.Input.UserRequest)
	response := strings.TrimSpace(example.Output.Response)
	if request == "" || response == "" {
		return ""
	}
	request = truncateRunesForPromptOptimizer(request, 80)
	response = truncateRunesForPromptOptimizer(response, 120)
	return fmt.Sprintf("User: %s | Assistant: %s", request, response)
}

func (p *PromptOptimizer) mergePreferenceDatasetInsights(insights *promptInsights) {
	if p == nil || insights == nil || len(p.preferenceExamples) == 0 {
		return
	}
	for _, example := range p.preferenceExamples {
		if strings.TrimSpace(example.Chosen.Prompt) != "" {
			insights.PreferredPromptLines = append(insights.PreferredPromptLines,
				extractPromptDirectives(example.Chosen.Prompt)...)
		}
		if strings.TrimSpace(example.Rejected.Prompt) != "" {
			insights.CommonWeaknesses = append(insights.CommonWeaknesses,
				extractPromptWeaknesses(example.Rejected.Prompt, example.Chosen.Prompt)...)
		}
	}
	insights.PreferredPromptLines = uniqueStrings(insights.PreferredPromptLines)
	insights.CommonWeaknesses = uniqueStrings(insights.CommonWeaknesses)
}

func extractPromptDirectives(prompt string) []string {
	lines := strings.Split(prompt, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if len(line) < 12 {
			continue
		}
		out = append(out, truncateRunesForPromptOptimizer(line, 120))
		if len(out) >= 4 {
			break
		}
	}
	if len(out) == 0 {
		return extractKeyPhrases(prompt)
	}
	return out
}

func extractPromptWeaknesses(rejected, chosen string) []string {
	rejectedLines := extractPromptDirectives(rejected)
	chosenSet := map[string]struct{}{}
	for _, line := range extractPromptDirectives(chosen) {
		chosenSet[strings.ToLower(strings.TrimSpace(line))] = struct{}{}
	}
	out := make([]string, 0, len(rejectedLines))
	for _, line := range rejectedLines {
		if _, ok := chosenSet[strings.ToLower(strings.TrimSpace(line))]; ok {
			continue
		}
		out = append(out, line)
		if len(out) >= 4 {
			break
		}
	}
	if len(out) == 0 {
		return extractKeyPhrases(rejected)
	}
	return out
}

func topPatternTexts(freqs []PatternFrequency, limit int) []string {
	if len(freqs) == 0 || limit <= 0 {
		return nil
	}

	copyFreqs := append([]PatternFrequency(nil), freqs...)
	sort.Slice(copyFreqs, func(i, j int) bool {
		if copyFreqs[i].Count == copyFreqs[j].Count {
			return copyFreqs[i].Pattern < copyFreqs[j].Pattern
		}
		return copyFreqs[i].Count > copyFreqs[j].Count
	})

	out := make([]string, 0, min(limit, len(copyFreqs)))
	for _, freq := range copyFreqs {
		pattern := strings.TrimSpace(freq.Pattern)
		if pattern == "" {
			continue
		}
		out = append(out, pattern)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func topImprovementActions(improvements []Improvement, limit int) []string {
	if len(improvements) == 0 || limit <= 0 {
		return nil
	}

	copyImprovements := append([]Improvement(nil), improvements...)
	sort.Slice(copyImprovements, func(i, j int) bool {
		if copyImprovements[i].Impact == copyImprovements[j].Impact {
			return copyImprovements[i].Action < copyImprovements[j].Action
		}
		return copyImprovements[i].Impact > copyImprovements[j].Impact
	})

	out := make([]string, 0, min(limit, len(copyImprovements)))
	for _, improvement := range copyImprovements {
		action := strings.TrimSpace(improvement.Action)
		if action == "" {
			action = strings.TrimSpace(improvement.Description)
		}
		if action == "" {
			continue
		}
		out = append(out, action)
		if len(out) >= limit {
			break
		}
	}
	return out
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

type promptMutationStrategy struct {
	name     string
	generate func(string, *promptInsights) string
}

// evaluate scores a prompt candidate.
func (p *PromptOptimizer) evaluate(ctx context.Context, workspaceID, agentRole, prompt string) (float64, error) {
	// Use custom eval function if provided (for testing)
	if p.evalFunc != nil {
		return p.evalFunc(ctx, prompt)
	}
	cacheKey := strings.TrimSpace(workspaceID) + "::" + strings.TrimSpace(agentRole) + "::" + strings.TrimSpace(prompt)
	p.cacheMu.RLock()
	score, ok := p.judgeScoreCache[cacheKey]
	p.cacheMu.RUnlock()
	if ok {
		return score, nil
	}

	if score, ok, err := p.evaluateWithPromptJudge(ctx, prompt); err == nil && ok {
		p.cacheMu.Lock()
		p.judgeScoreCache[cacheKey] = score
		p.cacheMu.Unlock()
		return score, nil
	}

	// Default evaluation based on trajectory data correlation
	// Score prompts based on similarity to successful execution patterns
	score = 0.5 // Base score

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

	p.cacheMu.Lock()
	p.judgeScoreCache[cacheKey] = score
	p.cacheMu.Unlock()
	return score, nil
}

func (p *PromptOptimizer) evaluateWithPromptJudge(ctx context.Context, prompt string) (float64, bool, error) {
	examples := p.buildPromptJudgeExamples()
	if len(examples) == 0 {
		return 0, false, nil
	}
	if p.config.PrimaryLLM == nil || strings.TrimSpace(p.config.PrimaryLLM.Model) == "" {
		return 0, false, nil
	}

	judge := DefaultPromptJudge()
	total := 0.0
	count := 0
	for _, example := range examples {
		caseEval, err := p.evaluatePromptJudgeCase(ctx, prompt, example.Question, example.Context, example.TargetResponse)
		if err != nil {
			return 0, false, err
		}
		total += judge.Score(PromptJudgeInput{
			Question:       example.Question,
			Context:        example.Context,
			TargetResponse: example.TargetResponse,
			Output:         caseEval.Output,
		})
		count++
	}
	if count == 0 {
		return 0, false, nil
	}
	return total / float64(count), true, nil
}

type promptJudgeExample struct {
	Question       string
	Context        string
	TargetResponse string
}

func (p *PromptOptimizer) buildPromptJudgeExamples() []promptJudgeExample {
	seen := map[string]struct{}{}
	out := make([]promptJudgeExample, 0, 6)

	for _, example := range p.preferenceExamples {
		question := strings.TrimSpace(example.Input.Question)
		contextText := strings.TrimSpace(example.Input.Context)
		target := strings.TrimSpace(example.Input.TargetResponse)
		if question == "" || target == "" {
			continue
		}
		key := question + "\n" + contextText + "\n" + target
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, promptJudgeExample{
			Question:       question,
			Context:        contextText,
			TargetResponse: target,
		})
		if len(out) >= 6 {
			return out
		}
	}

	for _, example := range p.transcriptExamples {
		question := strings.TrimSpace(example.Input.UserRequest)
		contextText := strings.TrimSpace(example.Input.Context)
		target := strings.TrimSpace(example.Output.Response)
		if question == "" || target == "" {
			continue
		}
		key := question + "\n" + contextText + "\n" + target
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, promptJudgeExample{
			Question:       question,
			Context:        contextText,
			TargetResponse: target,
		})
		if len(out) >= 6 {
			return out
		}
	}

	return out
}

func buildPromptJudgeUserPrompt(question, contextText string) string {
	question = strings.TrimSpace(question)
	contextText = strings.TrimSpace(contextText)
	if contextText == "" {
		return question
	}
	return "Context:\n" + contextText + "\n\nQuestion:\n" + question
}

func (p *PromptOptimizer) evaluatePromptJudgeCase(ctx context.Context, systemPrompt, question, contextText, targetResponse string) (promptJudgeCaseEvaluation, error) {
	key := strings.TrimSpace(systemPrompt) + "::" + strings.TrimSpace(question) + "::" + strings.TrimSpace(contextText) + "::" + strings.TrimSpace(targetResponse)
	p.cacheMu.RLock()
	eval, ok := p.judgeCaseCache[key]
	p.cacheMu.RUnlock()
	if ok {
		return eval, nil
	}
	output, err := p.generateJudgeOutput(ctx, systemPrompt, buildPromptJudgeUserPrompt(question, contextText))
	if err != nil {
		return promptJudgeCaseEvaluation{}, err
	}
	judge := DefaultPromptJudge()
	result := judge.Evaluate(PromptJudgeInput{
		Question:       question,
		Context:        contextText,
		TargetResponse: targetResponse,
		Output:         output,
	})
	eval = promptJudgeCaseEvaluation{
		Output:   output,
		Result:   result,
		Feedback: buildPromptJudgeFeedback(question, contextText, targetResponse, output, result),
	}
	p.cacheMu.Lock()
	p.judgeCaseCache[key] = eval
	p.cacheMu.Unlock()
	return eval, nil
}

func buildPromptJudgeFeedback(question, contextText, targetResponse, output string, result PromptJudgeResult) string {
	issues := make([]string, 0, 4)
	if targetResponse != "" && result.TargetSimilarity < 0.45 {
		issues = append(issues, "misses important target-response details")
	}
	if result.QuerySimilarity < 0.35 {
		issues = append(issues, "drifts from the question/context")
	}
	if result.GenericPenalty < 0.8 {
		issues = append(issues, "is too generic or asks for more input instead of acting")
	}
	if result.LengthQuality < 0.7 {
		issues = append(issues, "uses a poor length/verbosity fit for concise coding help")
	}
	if len(issues) == 0 {
		issues = append(issues, "response is broadly aligned but can still be made sharper and more direct")
	}
	parts := []string{
		fmt.Sprintf("Judge summary: score=%.3f target=%.3f query=%.3f length=%.3f penalty=%.3f.", result.Score, result.TargetSimilarity, result.QuerySimilarity, result.LengthQuality, result.GenericPenalty),
		"Issues: " + strings.Join(issues, "; ") + ".",
	}
	if strings.TrimSpace(targetResponse) != "" {
		parts = append(parts, "Target response: "+strings.TrimSpace(targetResponse))
	}
	if strings.TrimSpace(output) != "" {
		parts = append(parts, "Observed output: "+strings.TrimSpace(output))
	}
	return strings.Join(parts, "\n")
}

func (p *PromptOptimizer) generateJudgeOutput(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if p.judgeOutputFunc != nil {
		return p.judgeOutputFunc(ctx, systemPrompt, userPrompt)
	}
	primary := p.config.PrimaryLLM
	if primary == nil {
		return "", fmt.Errorf("prompt optimizer: no primary llm configured for judge evaluation")
	}
	client, err := verification.NewOpenAIClient(verification.OpenAIConfig{
		Provider: primary.Provider,
		BaseURL:  primary.BaseURL,
		APIKey:   primary.APIKey,
		Model:    primary.Model,
		Timeout:  60 * time.Second,
	})
	if err != nil {
		return "", err
	}
	output, callErr := client.Chat(ctx, systemPrompt, userPrompt, verification.LLMCallOptions{
		MaxTokens:   256,
		Temperature: 0,
	})
	if callErr == nil {
		return output, nil
	}
	if p.config.FallbackLLM == nil || strings.TrimSpace(p.config.FallbackLLM.Model) == "" {
		return "", callErr
	}
	fallback := p.config.FallbackLLM
	fallbackClient, err := verification.NewOpenAIClient(verification.OpenAIConfig{
		Provider: fallback.Provider,
		BaseURL:  fallback.BaseURL,
		APIKey:   fallback.APIKey,
		Model:    fallback.Model,
		Timeout:  60 * time.Second,
	})
	if err != nil {
		return "", err
	}
	return fallbackClient.Chat(ctx, systemPrompt, userPrompt, verification.LLMCallOptions{
		MaxTokens:   256,
		Temperature: 0,
	})
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
	if p.isGEPAMode() {
		switch iteration % 3 {
		case 0:
			return p.addressWeaknesses(prompt, insights)
		case 1:
			return p.applyImprovementActions(prompt, insights)
		case 2:
			return p.reinforceStrengths(prompt, insights)
		}
	}

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

func (p *PromptOptimizer) isGEPAMode() bool {
	mode := strings.ToLower(strings.TrimSpace(p.config.Mode))
	return mode == "gepa" || strings.HasPrefix(mode, "gepa-")
}

func (p *PromptOptimizer) addressWeaknesses(prompt string, insights *promptInsights) string {
	if len(insights.CommonWeaknesses) == 0 {
		return p.addConstraints(prompt, insights)
	}

	weaknesses := insights.CommonWeaknesses
	if len(weaknesses) > 3 {
		weaknesses = weaknesses[:3]
	}
	return prompt + "\n\nAddress recurring failure modes observed in past trajectories:\n- " +
		strings.Join(weaknesses, "\n- ")
}

func (p *PromptOptimizer) applyImprovementActions(prompt string, insights *promptInsights) string {
	if len(insights.ImprovementActions) == 0 {
		return p.addSpecificity(prompt, insights)
	}

	actions := insights.ImprovementActions
	if len(actions) > 3 {
		actions = actions[:3]
	}
	return prompt + "\n\nReflection-driven improvements to apply:\n- " +
		strings.Join(actions, "\n- ")
}

func (p *PromptOptimizer) reinforceStrengths(prompt string, insights *promptInsights) string {
	if len(insights.CommonStrengths) == 0 {
		return p.addExamples(prompt, insights)
	}

	strengths := insights.CommonStrengths
	if len(strengths) > 3 {
		strengths = strengths[:3]
	}
	return prompt + "\n\nPreserve these high-confidence strengths from successful runs:\n- " +
		strings.Join(strengths, "\n- ")
}

func (p *PromptOptimizer) addTranscriptExamples(prompt string, insights *promptInsights) string {
	if len(insights.ExamplePairs) == 0 {
		return p.addExamples(prompt, insights)
	}
	examples := insights.ExamplePairs
	if len(examples) > 2 {
		examples = examples[:2]
	}
	return prompt + "\n\nHigh-rated transcript examples to emulate:\n- " + strings.Join(examples, "\n- ")
}

func (p *PromptOptimizer) applyPreferredPromptLines(prompt string, insights *promptInsights) string {
	if len(insights.PreferredPromptLines) == 0 {
		return p.addSpecificity(prompt, insights)
	}
	lines := insights.PreferredPromptLines
	if len(lines) > 4 {
		lines = lines[:4]
	}
	return prompt + "\n\nPreferred directives from winning prompt variants:\n- " + strings.Join(lines, "\n- ")
}

func truncateRunesForPromptOptimizer(text string, maxLen int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= maxLen {
		return string(runes)
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// min returns the smaller of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
