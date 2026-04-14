package atomic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/obs"
)

// LLMUsage tracks raw token counts from an LLM call.
// Use obs.CalculateTokenCost() to get pricing information.
type LLMUsage struct {
	Model        string `json:"model"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

// Processor transforms raw text into atomic facts using LLM.
type Processor struct {
	apiKey   string
	model    string
	endpoint string
}

// NewProcessor creates a new atomic processor.
// Uses OpenRouter with glm-4.7-flash by default.
func NewProcessor() (*Processor, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY not set")
	}

	model := strings.TrimSpace(os.Getenv("AGENTCTL_ATOMIC_MODEL"))
	if model == "" {
		model = DefaultAtomicModel
	}

	return &Processor{
		apiKey:   apiKey,
		model:    model,
		endpoint: DefaultAtomicEndpoint,
	}, nil
}

// DefaultAtomicEndpoint is the default endpoint for atomic processing (OpenRouter).
const DefaultAtomicEndpoint = "https://openrouter.ai/api/v1/chat/completions"

// DefaultAtomicModel is the default model for atomic processing.
const DefaultAtomicModel = "z-ai/glm-4.7-flash"

// NewProcessorWithConfig creates a processor with explicit configuration.
// apiKey is required. endpoint and model use defaults if empty.
func NewProcessorWithConfig(apiKey, endpoint, model string) (*Processor, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key required")
	}
	if endpoint == "" {
		endpoint = DefaultAtomicEndpoint
	}
	if model == "" {
		model = DefaultAtomicModel
	}
	return &Processor{
		apiKey:   apiKey,
		model:    model,
		endpoint: endpoint,
	}, nil
}

// ProcessContext provides additional context for atomic processing.
type ProcessContext struct {
	// Workspace is the current workspace path (for scoping).
	Workspace string

	// Timestamp is the reference time for resolving relative dates.
	// If zero, time.Now() is used.
	Timestamp time.Time

	// SessionTranscript provides conversation context for disambiguation.
	// Optional but improves coreference resolution.
	SessionTranscript string

	// FilesTouched lists files relevant to this content.
	FilesTouched []string

	// ErrorsSeen lists error messages encountered.
	ErrorsSeen []string

	// CurrentFile is the file being edited (if applicable).
	CurrentFile string
}

// AtomicFact represents a processed, self-contained fact.
type AtomicFact struct {
	// Original is the raw input text.
	Original string `json:"original"`

	// Atomic is the self-contained, disambiguated rewrite.
	// All coreferences resolved, timestamps absolute, context injected.
	Atomic string `json:"atomic"`

	// Entities are extracted named entities (files, functions, people, concepts).
	Entities []string `json:"entities"`

	// Timestamp is the absolute temporal anchor (if applicable).
	Timestamp *time.Time `json:"timestamp,omitempty"`

	// Keywords are BM25-optimized search terms.
	Keywords []string `json:"keywords"`

	// Category classifies the fact type.
	Category string `json:"category,omitempty"` // task, memory, plan, gotcha, decision
}

// ProcessResult contains the LLM response for atomic processing.
type ProcessResult struct {
	Facts []AtomicFact `json:"facts"`
}

// Process transforms raw text into atomic facts.
// It may return multiple facts if the input contains multiple distinct pieces of information.
// Returns token usage for cost tracking via observability.
//
// Index:
// - Purpose: Convert raw text into disambiguated atomic facts using LLM
// - Flow: build prompt → call LLM → parse facts → fallback to single fact on parse error
// - SideEffects: network call to LLM
// - FailureModes: missing input, LLM errors, parse errors (fallback)
// - Related: buildAtomicPrompt, parseAtomicResponse, Processor.callLLM
// - Keywords: atomic_facts, llm, entities, timestamp, disambiguation
func (p *Processor) Process(ctx context.Context, raw string, pctx ProcessContext) ([]AtomicFact, *obs.TokenUsage, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil, fmt.Errorf("empty input")
	}

	// Set default timestamp
	timestamp := pctx.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	prompt := buildAtomicPrompt(raw, timestamp, pctx)

	result, usage, err := p.callLLM(ctx, prompt)
	if err != nil {
		return nil, usage, fmt.Errorf("LLM call failed: %w", err)
	}

	// Parse response
	facts, err := parseAtomicResponse(result, raw)
	if err != nil {
		// Fallback: return original as single fact with basic processing
		return []AtomicFact{{
			Original: raw,
			Atomic:   raw,
			Keywords: extractBasicKeywords(raw),
		}}, usage, nil
	}

	// Ensure original is set on all facts
	for i := range facts {
		if facts[i].Original == "" {
			facts[i].Original = raw
		}
	}

	return facts, usage, nil
}

// ProcessSingle is a convenience method that returns a single atomic fact.
// If the input produces multiple facts, they are merged.
// Returns token usage for cost tracking via observability.
func (p *Processor) ProcessSingle(ctx context.Context, raw string, pctx ProcessContext) (AtomicFact, *obs.TokenUsage, error) {
	facts, usage, err := p.Process(ctx, raw, pctx)
	if err != nil {
		return AtomicFact{}, usage, err
	}

	if len(facts) == 0 {
		return AtomicFact{
			Original: raw,
			Atomic:   raw,
			Keywords: extractBasicKeywords(raw),
		}, usage, nil
	}

	if len(facts) == 1 {
		return facts[0], usage, nil
	}

	// Merge multiple facts
	merged := AtomicFact{
		Original: raw,
		Atomic:   mergeAtomicTexts(facts),
		Entities: mergeUnique(factsEntities(facts)),
		Keywords: mergeUnique(factsKeywords(facts)),
		Category: facts[0].Category,
	}

	return merged, usage, nil
}

func (p *Processor) callLLM(ctx context.Context, prompt string) (string, *obs.TokenUsage, error) {
	reqBody := map[string]any{
		"model":      p.model,
		"max_tokens": 1024,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("HTTP-Referer", "https://github.com/joshka0/foxctl")
	req.Header.Set("X-Title", "foxctl")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", nil, err
	}

	if len(result.Choices) == 0 {
		return "", nil, fmt.Errorf("empty response")
	}

	// Calculate token usage with costs via obs package
	usage := obs.CalculateTokenCost(p.model, result.Usage.PromptTokens, result.Usage.CompletionTokens)

	return result.Choices[0].Message.Content, &usage, nil
}

func buildAtomicPrompt(raw string, timestamp time.Time, pctx ProcessContext) string {
	dateStr := timestamp.Format("2006-01-02")
	timeStr := timestamp.Format("15:04")

	var contextParts []string
	if pctx.CurrentFile != "" {
		contextParts = append(contextParts, fmt.Sprintf("Current file: %s", pctx.CurrentFile))
	}
	if len(pctx.FilesTouched) > 0 {
		contextParts = append(contextParts, fmt.Sprintf("Related files: %s", strings.Join(pctx.FilesTouched, ", ")))
	}
	if len(pctx.ErrorsSeen) > 0 {
		contextParts = append(contextParts, fmt.Sprintf("Errors encountered: %s", strings.Join(pctx.ErrorsSeen, "; ")))
	}

	contextSection := ""
	if len(contextParts) > 0 {
		contextSection = fmt.Sprintf("\n\nAdditional context:\n%s", strings.Join(contextParts, "\n"))
	}

	transcriptSection := ""
	if pctx.SessionTranscript != "" {
		// Truncate to ~2000 chars for context
		transcript := pctx.SessionTranscript
		if len(transcript) > 2000 {
			transcript = transcript[len(transcript)-2000:]
		}
		transcriptSection = fmt.Sprintf("\n\nRecent session context (for disambiguation):\n%s", transcript)
	}

	return fmt.Sprintf(`Transform this text into atomic, self-contained facts for a knowledge base.

Current date: %s
Current time: %s
%s%s

Input text:
%s

Instructions:
1. RESOLVE COREFERENCES: Replace pronouns (he, she, it, this, that) with actual names/entities
2. ANCHOR TIMESTAMPS: Convert relative time ("yesterday", "tomorrow", "last week") to absolute dates
3. INJECT CONTEXT: Make each fact understandable without the original conversation
4. EXTRACT ENTITIES: Identify files, functions, people, concepts, error messages
5. GENERATE KEYWORDS: Create BM25-friendly search terms (technical terms, identifiers, error codes)

Return JSON:
{
  "facts": [
    {
      "atomic": "Self-contained rewrite with all references resolved",
      "entities": ["file.go", "functionName", "PersonName"],
      "keywords": ["keyword1", "keyword2", "error_code"],
      "category": "task|memory|plan|gotcha|decision"
    }
  ]
}

Guidelines:
- If input contains multiple distinct pieces of information, create multiple facts
- Each fact MUST be independently understandable
- Include file paths with line numbers when mentioned (e.g., "auth.go:142")
- Preserve technical precision (error messages, function signatures, etc.)
- Keywords should include: function names, file names, error codes, technical terms

Return ONLY valid JSON, no markdown fences.`, dateStr, timeStr, contextSection, transcriptSection, raw)
}

func parseAtomicResponse(content, original string) ([]AtomicFact, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var result ProcessResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	if len(result.Facts) == 0 {
		return nil, fmt.Errorf("no facts in response")
	}

	return result.Facts, nil
}

func extractBasicKeywords(text string) []string {
	// Simple keyword extraction as fallback
	words := strings.Fields(text)
	keywords := make([]string, 0, len(words))

	for _, word := range words {
		// Clean punctuation
		word = strings.Trim(word, ".,;:!?\"'()[]{}")
		word = strings.ToLower(word)

		// Skip short words and common stop words
		if len(word) < 3 {
			continue
		}
		if isStopWord(word) {
			continue
		}

		keywords = append(keywords, word)
	}

	return dedupe(keywords)
}

func isStopWord(word string) bool {
	stopWords := map[string]bool{
		"the": true, "and": true, "for": true, "are": true, "but": true,
		"not": true, "you": true, "all": true, "can": true, "had": true,
		"her": true, "was": true, "one": true, "our": true, "out": true,
		"has": true, "have": true, "been": true, "would": true, "could": true,
		"this": true, "that": true, "with": true, "from": true, "they": true,
		"will": true, "what": true, "when": true, "where": true, "which": true,
	}
	return stopWords[word]
}

func dedupe(items []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

func mergeAtomicTexts(facts []AtomicFact) string {
	parts := make([]string, len(facts))
	for i, f := range facts {
		parts[i] = f.Atomic
	}
	return strings.Join(parts, " | ")
}

func factsEntities(facts []AtomicFact) []string {
	var all []string
	for _, f := range facts {
		all = append(all, f.Entities...)
	}
	return all
}

func factsKeywords(facts []AtomicFact) []string {
	var all []string
	for _, f := range facts {
		all = append(all, f.Keywords...)
	}
	return all
}

func mergeUnique(items []string) []string {
	return dedupe(items)
}
