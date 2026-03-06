package updater

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/planning/llm"
)

// Analyzer uses a cheap LLM to analyze conversations and extract context needs.
type Analyzer struct {
	planner  *llm.OpenAIPlanner
	timeout  time.Duration
	provider string
}

// NewAnalyzer creates a new conversation analyzer.
// It uses the provided LLM planner (should be configured for cheap models).
func NewAnalyzer(planner *llm.OpenAIPlanner, timeout time.Duration) *Analyzer {
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	provider := ""
	if planner != nil {
		provider = planner.Provider()
	}
	return &Analyzer{
		planner:  planner,
		timeout:  timeout,
		provider: provider,
	}
}

// NewAnalyzerFromConfig creates an analyzer using auto-detected LLM provider.
func NewAnalyzerFromConfig(ctx context.Context, cfg llm.ProviderConfig, timeout time.Duration) *Analyzer {
	if ctx == nil {
		ctx = context.Background()
	}
	planner := llm.AutoPlannerFromConfig(ctx, cfg)
	return NewAnalyzer(planner, timeout)
}

// Available returns true if an LLM backend is configured.
func (a *Analyzer) Available() bool {
	return a.planner != nil && a.planner.Available()
}

// Provider returns the LLM provider name.
func (a *Analyzer) Provider() string {
	return a.provider
}

// Turn represents a conversation turn for analysis.
type Turn struct {
	Role      string   // "user", "assistant", "tool"
	Content   string   // The message content
	ToolName  string   // Tool name if this is a tool call/result
	FilePaths []string // Any file paths mentioned or modified
}

// Analyze examines recent conversation turns and extracts structured context needs.
func (a *Analyzer) Analyze(ctx context.Context, turns []Turn, previousAnalysis *AnalysisResult) (*AnalysisResult, error) {
	if !a.Available() {
		return nil, fmt.Errorf("no LLM backend available")
	}

	// Build the prompt
	prompt := buildAnalysisPrompt(turns, previousAnalysis)

	// Create a child context with timeout
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	// Call the LLM using the raw chat completion
	// We need to use the underlying HTTP client since OpenAIPlanner.Plan() is for task planning
	content, err := a.callLLM(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	// Parse the response
	result, err := parseAnalysisResponse(content)
	if err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// Detect drift compared to previous analysis
	if previousAnalysis != nil {
		result.DriftDetected = detectDrift(previousAnalysis.Topics, result.Topics)
	}

	return result, nil
}

// callLLM makes a raw chat completion call to the LLM.
// We use this instead of planner.Plan() because we need a different prompt format.
func (a *Analyzer) callLLM(ctx context.Context, prompt string) (string, error) {
	// Build request using the same format as OpenAI planner
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type request struct {
		Model       string    `json:"model"`
		Messages    []message `json:"messages"`
		Temperature float64   `json:"temperature"`
		MaxTokens   int       `json:"max_tokens"`
	}

	// Get base URL and model from provider
	baseURL, model := a.getProviderConfig()

	reqBody, err := json.Marshal(request{
		Model: model,
		Messages: []message{
			{Role: "user", Content: prompt},
		},
		Temperature: 0.1, // Low temperature for structured output
		MaxTokens:   1024,
	})
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	// We need to get the API key - this is a bit hacky but necessary
	// The planner has it stored internally
	if a.planner != nil {
		httpReq.Header.Set("Authorization", "Bearer "+a.getAPIKey())
	}

	client := &http.Client{Timeout: a.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response
	var respData struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &respData); err != nil {
		return "", err
	}

	if len(respData.Choices) == 0 {
		return "", fmt.Errorf("no completion returned")
	}

	return respData.Choices[0].Message.Content, nil
}

// getProviderConfig returns the base URL and model for the current provider.
func (a *Analyzer) getProviderConfig() (baseURL, model string) {
	switch a.provider {
	case "cerebras":
		return "https://api.cerebras.ai/v1", "llama3.1-8b"
	case "groq":
		return "https://api.groq.com/openai/v1", "llama-3.1-8b-instant"
	case "openrouter":
		return "https://openrouter.ai/api/v1", "meta-llama/llama-3.1-8b-instruct"
	case "lmstudio":
		return "http://localhost:1234/v1", "zai-org/glm-4.7-flash"
	default:
		return "https://api.openai.com/v1", "gpt-4o-mini"
	}
}

// getAPIKey extracts the API key from the planner.
// This is a workaround since we can't access it directly.
func (a *Analyzer) getAPIKey() string {
	// The planner stores the API key in its config, but it's not exported.
	// We'll need to pass it through the config chain.
	// For now, return empty and let the caller set it.
	return ""
}

// AnalyzerWithAPIKey is an Analyzer that has direct access to the API key.
type AnalyzerWithAPIKey struct {
	*Analyzer
	apiKey string
}

// NewAnalyzerWithAPIKey creates an analyzer with direct API key access.
func NewAnalyzerWithAPIKey(ctx context.Context, provider, apiKey, model string, timeout time.Duration) *AnalyzerWithAPIKey {
	if ctx == nil {
		ctx = context.Background()
	}
	planner := llm.NewOpenAIPlanner(ctx, llm.OpenAIConfig{
		APIKey:   apiKey,
		Provider: provider,
		Model:    model,
	})
	return &AnalyzerWithAPIKey{
		Analyzer: NewAnalyzer(planner, timeout),
		apiKey:   apiKey,
	}
}

// Analyze uses the stored API key for the LLM call.
func (a *AnalyzerWithAPIKey) Analyze(ctx context.Context, turns []Turn, previousAnalysis *AnalysisResult) (*AnalysisResult, error) {
	if a.apiKey == "" {
		return nil, fmt.Errorf("no API key configured")
	}

	prompt := buildAnalysisPrompt(turns, previousAnalysis)

	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	content, err := a.callLLMWithKey(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	result, err := parseAnalysisResponse(content)
	if err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if previousAnalysis != nil {
		result.DriftDetected = detectDrift(previousAnalysis.Topics, result.Topics)
	}

	return result, nil
}

func (a *AnalyzerWithAPIKey) callLLMWithKey(ctx context.Context, prompt string) (string, error) {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type request struct {
		Model       string    `json:"model"`
		Messages    []message `json:"messages"`
		Temperature float64   `json:"temperature"`
		MaxTokens   int       `json:"max_tokens"`
	}

	baseURL, model := a.getProviderConfig()

	reqBody, err := json.Marshal(request{
		Model: model,
		Messages: []message{
			{Role: "user", Content: prompt},
		},
		Temperature: 0.1,
		MaxTokens:   1024,
	})
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)

	client := &http.Client{Timeout: a.timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var respData struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &respData); err != nil {
		return "", err
	}

	if len(respData.Choices) == 0 {
		return "", fmt.Errorf("no completion returned")
	}

	return respData.Choices[0].Message.Content, nil
}

const analysisPromptTemplate = `Analyze this coding conversation to identify what context would be helpful.

Recent conversation:
%s

%sRespond with JSON only (no markdown):
{
  "topics": ["topic1", "topic2"],
  "intent": "what the user is trying to accomplish",
  "files_active": ["path/to/file.go"],
  "search_queries": ["suggested search 1", "suggested search 2"],
  "confidence": 0.85
}

Rules:
- topics: Extract 1-3 key technical topics (e.g., "authentication", "database", "testing")
- intent: One sentence describing the current goal
- files_active: File paths mentioned or being modified (max 5)
- search_queries: 1-3 queries to find relevant gotchas, patterns, or past work
- confidence: 0.0-1.0 how confident you are in this analysis`

func buildAnalysisPrompt(turns []Turn, previous *AnalysisResult) string {
	var sb strings.Builder

	// Format recent turns
	for _, turn := range turns {
		switch turn.Role {
		case "user":
			sb.WriteString("User: ")
		case "assistant":
			sb.WriteString("Assistant: ")
		case "tool":
			sb.WriteString(fmt.Sprintf("Tool[%s]: ", turn.ToolName))
		}

		// Truncate long content
		content := turn.Content
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		sb.WriteString(content)
		sb.WriteString("\n")

		// Include file paths if any
		if len(turn.FilePaths) > 0 {
			sb.WriteString(fmt.Sprintf("  Files: %s\n", strings.Join(turn.FilePaths, ", ")))
		}
	}

	// Include previous analysis for drift detection
	previousContext := ""
	if previous != nil {
		previousContext = fmt.Sprintf("Previous topics were: %s\n\n", strings.Join(previous.Topics, ", "))
	}

	return fmt.Sprintf(analysisPromptTemplate, sb.String(), previousContext)
}

func parseAnalysisResponse(content string) (*AnalysisResult, error) {
	// Strip markdown code blocks if present
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		re := regexp.MustCompile("(?s)```(?:json)?\\s*(.+?)\\s*```")
		matches := re.FindStringSubmatch(content)
		if len(matches) > 1 {
			content = matches[1]
		}
	}

	var result AnalysisResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Validate result
	if len(result.Topics) == 0 {
		result.Topics = []string{"general"}
	}
	if result.Confidence < 0 || result.Confidence > 1 {
		result.Confidence = 0.5
	}

	return &result, nil
}

// detectDrift checks if topics have changed significantly.
func detectDrift(previous, current []string) bool {
	if len(previous) == 0 {
		return false
	}

	// Build set of previous topics
	prevSet := make(map[string]struct{})
	for _, t := range previous {
		prevSet[strings.ToLower(t)] = struct{}{}
	}

	// Count how many current topics are new
	newCount := 0
	for _, t := range current {
		if _, ok := prevSet[strings.ToLower(t)]; !ok {
			newCount++
		}
	}

	// Drift if >50% of current topics are new
	return float32(newCount)/float32(len(current)) > 0.5
}
