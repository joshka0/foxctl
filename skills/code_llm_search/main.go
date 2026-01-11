// Package main implements the code/llm_search skill.
//
// This skill sends candidates to multiple LLM providers for relevance
// ranking and combines the results into a unified ranking.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
)

// Command is the envelope command for this skill.
const Command = "code/llm_search"

// Error codes per Core Profile v1 §13.
const (
	ErrCodeArg     = "EARG"
	ErrCodeRuntime = "ERUNTIME"
	ErrCodeConfig  = "ERUNTIME"
)

// Provider names.
const (
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai"
	ProviderGemini    = "gemini"
	ProviderCerebras  = "cerebras"
)

// Default limits.
const (
	DefaultMaxCandidates = 50
	DefaultTimeout       = 30 * time.Second
)

// Input is the expected JSON input.
type Input struct {
	WorkspaceID string      `json:"workspace_id"`
	Question    string      `json:"question" validate:"required"`
	Candidates  []Candidate `json:"candidates" validate:"required,min=1"`
	Providers   []string    `json:"providers,omitempty"`
	Limits      Limits      `json:"limits,omitempty"`
}

// Candidate represents a file candidate with optional content.
type Candidate struct {
	Path     string  `json:"path"`
	SymbolID string  `json:"symbol_id,omitempty"`
	Snippet  string  `json:"snippet,omitempty"`
	Priority float64 `json:"priority,omitempty"`
}

// Limits controls the ranking process.
type Limits struct {
	MaxCandidates int           `json:"max_candidates,omitempty"`
	Timeout       time.Duration `json:"timeout_ms,omitempty,format:units"`
}

// Output is the skill output structure.
type Output struct {
	Summary          Summary                   `json:"summary"`
	RankedCandidates []RankedCandidate         `json:"ranked_candidates"`
	ProviderResults  map[string]ProviderResult `json:"provider_results"`
}

// Summary contains aggregated statistics.
type Summary struct {
	CandidatesRanked  int              `json:"candidates_ranked"`
	ProvidersUsed     []string         `json:"providers_used"`
	ProviderLatencies map[string]int64 `json:"provider_latencies_ms"`
	DurationMS        int64            `json:"duration_ms"`
}

// RankedCandidate is the output representation of a ranked candidate.
type RankedCandidate struct {
	Path        string             `json:"path"`
	SymbolID    string             `json:"symbol_id,omitempty"`
	Score       float64            `json:"score"`
	Rank        int                `json:"rank"`
	Explanation string             `json:"explanation,omitempty"`
	ByProvider  map[string]float64 `json:"by_provider"`
}

// ProviderResult contains the result from a single provider.
type ProviderResult struct {
	Provider   string          `json:"provider"`
	Status     string          `json:"status"` // "ok", "error", "timeout"
	Error      string          `json:"error,omitempty"`
	Rankings   []ProviderScore `json:"rankings,omitempty"`
	DurationMS int64           `json:"duration_ms"`
}

// ProviderScore is a single provider's score for a candidate.
type ProviderScore struct {
	Path        string  `json:"path"`
	Score       float64 `json:"score"`
	Explanation string  `json:"explanation,omitempty"`
}

// RankingProvider interface for LLM-based ranking.
type RankingProvider interface {
	// Name returns the provider name.
	Name() string

	// RankCandidates ranks candidates based on relevance to the question.
	// Returns scores between 0-1 for each candidate.
	RankCandidates(ctx context.Context, question string, candidates []Candidate) ([]ProviderScore, error)
}

func main() {
	skillmain.Main(Command, run)
}

// detectAvailableProviders returns providers that have API keys configured.
func detectAvailableProviders() []string {
	var providers []string

	// Cerebras first - fast inference, good for quick ranking
	if os.Getenv("CEREBRAS_API_KEY") != "" {
		providers = append(providers, ProviderCerebras)
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		providers = append(providers, ProviderAnthropic)
	}
	if os.Getenv("OPENAI_API_KEY") != "" {
		providers = append(providers, ProviderOpenAI)
	}
	if os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != "" {
		providers = append(providers, ProviderGemini)
	}

	// If no providers detected, use cerebras if available, else anthropic
	if len(providers) == 0 {
		if os.Getenv("CEREBRAS_API_KEY") != "" {
			providers = []string{ProviderCerebras}
		} else {
			providers = []string{ProviderAnthropic}
		}
	}

	return providers
}

// run is the main skill logic.
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Apply defaults
	if in.WorkspaceID == "" {
		in.WorkspaceID = "default"
	}
	if len(in.Providers) == 0 {
		in.Providers = detectAvailableProviders()
	}
	if in.Limits.MaxCandidates <= 0 {
		in.Limits.MaxCandidates = DefaultMaxCandidates
	}
	if in.Limits.Timeout <= 0 {
		in.Limits.Timeout = DefaultTimeout
	}
	// Limit candidates
	if len(in.Candidates) > in.Limits.MaxCandidates {
		in.Candidates = in.Candidates[:in.Limits.MaxCandidates]
	}

	start := time.Now()
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()

	// Create providers
	providers := make([]RankingProvider, 0, len(in.Providers))
	for _, name := range in.Providers {
		p, err := createProvider(name)
		if err != nil {
			logger.Warn().Err(err).Str("provider", name).Msg("failed to create provider")
			continue
		}
		providers = append(providers, p)
	}

	if len(providers) == 0 {
		return fmt.Errorf("no valid providers configured")
	}

	// Run providers in parallel
	ctx, cancel := context.WithTimeout(ctx, in.Limits.Timeout)
	defer cancel()

	var wg sync.WaitGroup
	providerResults := make(map[string]ProviderResult)
	var mu sync.Mutex

	for _, p := range providers {
		wg.Add(1)
		go func(provider RankingProvider) {
			defer wg.Done()

			providerStart := time.Now()
			result := ProviderResult{
				Provider: provider.Name(),
			}

			scores, err := provider.RankCandidates(ctx, in.Question, in.Candidates)
			result.DurationMS = time.Since(providerStart).Milliseconds()

			if err != nil {
				if ctx.Err() != nil {
					result.Status = "timeout"
					result.Error = "request timed out"
				} else {
					result.Status = "error"
					result.Error = err.Error()
				}
			} else {
				result.Status = "ok"
				result.Rankings = scores
			}

			mu.Lock()
			providerResults[provider.Name()] = result
			mu.Unlock()
		}(p)
	}

	wg.Wait()

	// Merge results from all providers
	rankedCandidates := mergeProviderResults(in.Candidates, providerResults)

	// Build summary
	providersUsed := make([]string, 0, len(providerResults))
	latencies := make(map[string]int64)
	for name, result := range providerResults {
		if result.Status == "ok" {
			providersUsed = append(providersUsed, name)
		}
		latencies[name] = result.DurationMS
	}
	sort.Strings(providersUsed)

	output := Output{
		Summary: Summary{
			CandidatesRanked:  len(rankedCandidates),
			ProvidersUsed:     providersUsed,
			ProviderLatencies: latencies,
			DurationMS:        time.Since(start).Milliseconds(),
		},
		RankedCandidates: rankedCandidates,
		ProviderResults:  providerResults,
	}

	logger.Info().
		Str("skill", Command).
		Int("candidates", len(rankedCandidates)).
		Strs("providers", providersUsed).
		Int64("duration_ms", output.Summary.DurationMS).
		Msg("llm_search_complete")

	return skillout.Emit(rc, Command, output)
}

// mergeProviderResults combines scores from all providers.
func mergeProviderResults(candidates []Candidate, results map[string]ProviderResult) []RankedCandidate {
	// Build a map of path -> scores by provider
	scoresByPath := make(map[string]map[string]float64)
	explanationsByPath := make(map[string]string)

	for providerName, result := range results {
		if result.Status != "ok" {
			continue
		}
		for _, score := range result.Rankings {
			if scoresByPath[score.Path] == nil {
				scoresByPath[score.Path] = make(map[string]float64)
			}
			scoresByPath[score.Path][providerName] = score.Score
			if score.Explanation != "" {
				explanationsByPath[score.Path] = score.Explanation
			}
		}
	}

	// Calculate combined scores (average across providers)
	ranked := make([]RankedCandidate, 0, len(candidates))
	for _, c := range candidates {
		byProvider := scoresByPath[c.Path]
		if byProvider == nil {
			byProvider = make(map[string]float64)
		}

		// Calculate average score
		var totalScore float64
		count := 0
		for _, score := range byProvider {
			totalScore += score
			count++
		}
		avgScore := 0.0
		if count > 0 {
			avgScore = totalScore / float64(count)
		} else {
			// Use original priority if no provider scored this candidate
			avgScore = c.Priority
		}

		ranked = append(ranked, RankedCandidate{
			Path:        c.Path,
			SymbolID:    c.SymbolID,
			Score:       avgScore,
			Explanation: explanationsByPath[c.Path],
			ByProvider:  byProvider,
		})
	}

	// Sort by score (descending)
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].Score > ranked[j].Score
	})

	// Assign ranks
	for i := range ranked {
		ranked[i].Rank = i + 1
	}

	return ranked
}

// createProvider creates a ranking provider by name.
func createProvider(name string) (RankingProvider, error) {
	switch name {
	case ProviderCerebras:
		apiKey := os.Getenv("CEREBRAS_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("CEREBRAS_API_KEY not set")
		}
		model := os.Getenv("CEREBRAS_MODEL")
		if model == "" {
			model = "llama-3.3-70b" // Default to Llama 3.3 70B
		}
		return NewCerebrasProvider(apiKey, model), nil

	case ProviderAnthropic:
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
		}
		return NewAnthropicProvider(apiKey), nil

	case ProviderOpenAI:
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY not set")
		}
		return NewOpenAIProvider(apiKey), nil

	case ProviderGemini:
		apiKey := os.Getenv("GEMINI_API_KEY")
		if apiKey == "" {
			apiKey = os.Getenv("GOOGLE_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("GEMINI_API_KEY not set")
		}
		return NewGeminiProvider(apiKey), nil

	default:
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
}

// CerebrasProvider implements RankingProvider for Cerebras (OpenAI-compatible API).
type CerebrasProvider struct {
	apiKey string
	model  string
	client *http.Client
}

// NewCerebrasProvider creates a new Cerebras provider.
func NewCerebrasProvider(apiKey, model string) *CerebrasProvider {
	return &CerebrasProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// Name returns the provider name.
func (p *CerebrasProvider) Name() string {
	return ProviderCerebras
}

// RankCandidates ranks candidates using Cerebras (OpenAI-compatible API).
func (p *CerebrasProvider) RankCandidates(ctx context.Context, question string, candidates []Candidate) ([]ProviderScore, error) {
	prompt := buildRankingPrompt(question, candidates)

	reqBody := map[string]any{
		"model":      p.model,
		"max_tokens": 2048,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.cerebras.ai/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	return parseRankingResponse(result.Choices[0].Message.Content, candidates)
}

// AnthropicProvider implements RankingProvider for Claude.
type AnthropicProvider struct {
	apiKey string
	client *http.Client
}

// NewAnthropicProvider creates a new Anthropic provider.
func NewAnthropicProvider(apiKey string) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey: apiKey,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// Name returns the provider name.
func (p *AnthropicProvider) Name() string {
	return ProviderAnthropic
}

// RankCandidates ranks candidates using Claude.
func (p *AnthropicProvider) RankCandidates(ctx context.Context, question string, candidates []Candidate) ([]ProviderScore, error) {
	prompt := buildRankingPrompt(question, candidates)

	// Build request
	reqBody := map[string]any{
		"model":      "claude-3-5-haiku-20241022",
		"max_tokens": 2048,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Content) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	return parseRankingResponse(result.Content[0].Text, candidates)
}

// OpenAIProvider implements RankingProvider for GPT.
type OpenAIProvider struct {
	apiKey string
	client *http.Client
}

// NewOpenAIProvider creates a new OpenAI provider.
func NewOpenAIProvider(apiKey string) *OpenAIProvider {
	return &OpenAIProvider{
		apiKey: apiKey,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// Name returns the provider name.
func (p *OpenAIProvider) Name() string {
	return ProviderOpenAI
}

// RankCandidates ranks candidates using GPT.
func (p *OpenAIProvider) RankCandidates(ctx context.Context, question string, candidates []Candidate) ([]ProviderScore, error) {
	prompt := buildRankingPrompt(question, candidates)

	reqBody := map[string]any{
		"model":      "gpt-4o-mini",
		"max_tokens": 2048,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	return parseRankingResponse(result.Choices[0].Message.Content, candidates)
}

// GeminiProvider implements RankingProvider for Gemini.
type GeminiProvider struct {
	apiKey string
	client *http.Client
}

// NewGeminiProvider creates a new Gemini provider.
func NewGeminiProvider(apiKey string) *GeminiProvider {
	return &GeminiProvider{
		apiKey: apiKey,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// Name returns the provider name.
func (p *GeminiProvider) Name() string {
	return ProviderGemini
}

// RankCandidates ranks candidates using Gemini.
func (p *GeminiProvider) RankCandidates(ctx context.Context, question string, candidates []Candidate) ([]ProviderScore, error) {
	prompt := buildRankingPrompt(question, candidates)

	reqBody := map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]string{
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]any{
			"maxOutputTokens": 2048,
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-1.5-flash:generateContent?key=%s", p.apiKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	return parseRankingResponse(result.Candidates[0].Content.Parts[0].Text, candidates)
}

// buildRankingPrompt creates a prompt for ranking candidates.
func buildRankingPrompt(question string, candidates []Candidate) string {
	var sb strings.Builder

	sb.WriteString("You are a code search relevance expert. Rate how relevant each file is to the given question.\n\n")
	sb.WriteString("Question: ")
	sb.WriteString(question)
	sb.WriteString("\n\nFiles to rank:\n")

	for i, c := range candidates {
		sb.WriteString(fmt.Sprintf("\n%d. %s", i+1, c.Path))
		if c.SymbolID != "" {
			sb.WriteString(fmt.Sprintf(" (symbol: %s)", c.SymbolID))
		}
		if c.Snippet != "" {
			sb.WriteString(fmt.Sprintf("\n   Snippet:\n   %s", strings.ReplaceAll(c.Snippet, "\n", "\n   ")))
		}
	}

	sb.WriteString("\n\nRespond with a JSON array of objects, each with 'path', 'score' (0-1), and 'explanation' fields.")
	sb.WriteString("\nExample: [{\"path\": \"file.go\", \"score\": 0.9, \"explanation\": \"Contains the main handler\"}]")
	sb.WriteString("\nReturn ONLY the JSON array, no other text.")

	return sb.String()
}

// parseRankingResponse parses the LLM response into scores.
func parseRankingResponse(response string, candidates []Candidate) ([]ProviderScore, error) {
	// Find JSON array in response
	response = strings.TrimSpace(response)

	// Remove markdown code blocks if present
	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		var jsonLines []string
		inBlock := false
		for _, line := range lines {
			if strings.HasPrefix(line, "```") {
				inBlock = !inBlock
				continue
			}
			if inBlock {
				jsonLines = append(jsonLines, line)
			}
		}
		response = strings.Join(jsonLines, "\n")
	}

	// Find array boundaries
	start := strings.Index(response, "[")
	end := strings.LastIndex(response, "]")
	if start == -1 || end == -1 || start >= end {
		return nil, fmt.Errorf("no JSON array found in response")
	}
	response = response[start : end+1]

	var scores []ProviderScore
	if err := json.Unmarshal([]byte(response), &scores); err != nil {
		return nil, fmt.Errorf("parse ranking JSON: %w", err)
	}

	// Validate scores are in 0-1 range and normalize
	for i := range scores {
		if scores[i].Score < 0 {
			scores[i].Score = 0
		}
		if scores[i].Score > 1 {
			scores[i].Score = 1
		}
	}

	return scores, nil
}

