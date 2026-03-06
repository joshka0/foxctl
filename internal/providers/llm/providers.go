package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Provider describes a single LLM backend configuration.
type Provider struct {
	Name      string
	Endpoint  string
	APIKey    string
	Model     string
	IsCLI     bool
	MaxTokens int
}

// SummarizationProviders returns providers for session summarization in priority order.
// Priority: Cerebras -> Groq -> OpenRouter -> CLI fallback (Gemini, Claude).
func SummarizationProviders() []Provider {
	var providers []Provider
	providers = appendAPIProvider(
		providers,
		"CEREBRAS_API_KEY",
		"cerebras",
		"https://api.cerebras.ai/v1/chat/completions",
		envOrDefault("CEREBRAS_MODEL", "llama-3.3-70b"),
		8000,
	)
	providers = appendAPIProvider(
		providers,
		"GROQ_API_KEY",
		"groq",
		"https://api.groq.com/openai/v1/chat/completions",
		envOrDefault("GROQ_MODEL", "llama-3.3-70b-versatile"),
		10000,
	)
	providers = appendOpenRouterProviders(
		providers,
		envOrDefault("OPENROUTER_MODELS", "google/gemini-3.1-flash-lite-preview"),
		24000,
	)
	providers = appendCLIProvider(
		providers,
		"gemini",
		"gemini-cli",
		envOrDefault("GEMINI_MODEL", "gemini-2.5-flash"),
		100000,
	)
	providers = appendCLIProvider(
		providers,
		"claude",
		"claude-cli",
		envOrDefault("CLAUDE_MODEL", "claude-haiku-4-5"),
		50000,
	)
	return providers
}

// ExtractionProviders returns providers for session learnings extraction.
// Priority: Cerebras -> Groq -> OpenRouter -> Gemini CLI.
func ExtractionProviders() []Provider {
	var providers []Provider
	providers = appendAPIProvider(
		providers,
		"CEREBRAS_API_KEY",
		"cerebras",
		"https://api.cerebras.ai/v1/chat/completions",
		"llama-3.3-70b",
		8000,
	)
	providers = appendAPIProvider(
		providers,
		"GROQ_API_KEY",
		"groq",
		"https://api.groq.com/openai/v1/chat/completions",
		envOrDefault("GROQ_MODEL", "llama-3.3-70b-versatile"),
		10000,
	)
	providers = appendOpenRouterProviders(
		providers,
		"google/gemini-3.1-flash-lite-preview",
		24000,
	)
	providers = appendCLIProvider(
		providers,
		"gemini",
		"gemini-cli",
		"gemini-2.5-flash",
		100000,
	)
	return providers
}

// SynthesisProviders returns providers for code semantic search synthesis.
// Priority: OpenRouter -> Groq -> Cerebras.
func SynthesisProviders(modelOverride string) []Provider {
	var providers []Provider
	openRouterModel := strings.TrimSpace(modelOverride)
	if openRouterModel == "" {
		openRouterModel = envOrDefault("OPENROUTER_MODELS", "google/gemini-3.1-flash-lite-preview")
	}
	providers = appendOpenRouterProviders(providers, openRouterModel, 0)

	groqModel := strings.TrimSpace(modelOverride)
	if groqModel == "" {
		groqModel = "llama-3.3-70b-versatile"
	}
	providers = appendAPIProvider(
		providers,
		"GROQ_API_KEY",
		"groq",
		"https://api.groq.com/openai/v1/chat/completions",
		groqModel,
		0,
	)

	cerebrasModel := strings.TrimSpace(modelOverride)
	if cerebrasModel == "" {
		cerebrasModel = "llama-3.3-70b"
	}
	providers = appendAPIProvider(
		providers,
		"CEREBRAS_API_KEY",
		"cerebras",
		"https://api.cerebras.ai/v1/chat/completions",
		cerebrasModel,
		0,
	)
	return providers
}

func appendAPIProvider(providers []Provider, keyEnv, name, endpoint, model string, maxTokens int) []Provider {
	key := strings.TrimSpace(os.Getenv(keyEnv))
	if key == "" {
		return providers
	}
	return append(providers, Provider{
		Name:      name,
		Endpoint:  endpoint,
		APIKey:    key,
		Model:     model,
		MaxTokens: maxTokens,
	})
}

func appendOpenRouterProviders(providers []Provider, models string, maxTokens int) []Provider {
	key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if key == "" {
		return providers
	}
	for _, model := range strings.Split(models, ",") {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		providers = append(providers, Provider{
			Name:      "openrouter:" + model,
			Endpoint:  "https://openrouter.ai/api/v1/chat/completions",
			APIKey:    key,
			Model:     model,
			MaxTokens: maxTokens,
		})
	}
	return providers
}

func appendCLIProvider(providers []Provider, toolName, name, model string, maxTokens int) []Provider {
	if !hasTool(toolName) {
		return providers
	}
	return append(providers, Provider{
		Name:      name,
		Model:     model,
		IsCLI:     true,
		MaxTokens: maxTokens,
	})
}

func hasTool(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func envOrDefault(key, fallback string) string {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		return val
	}
	return fallback
}

// FileSummaryProviders returns providers for fast file summary generation.
// Priority: Devstral (technical/direct) -> Cerebras -> Groq.
// Uses low temperature (0.0) for consistent, deterministic summaries.
func FileSummaryProviders() []Provider {
	var providers []Provider

	// Devstral is best for technical code summaries - direct and precise
	providers = appendOpenRouterProviders(
		providers,
		"google/gemini-3.1-flash-lite-preview",
		256,
	)

	// Cerebras as fast fallback
	providers = appendAPIProvider(
		providers,
		"CEREBRAS_API_KEY",
		"cerebras",
		"https://api.cerebras.ai/v1/chat/completions",
		envOrDefault("CEREBRAS_MODEL", "llama-3.3-70b"),
		256,
	)

	// Groq as another fallback
	providers = appendAPIProvider(
		providers,
		"GROQ_API_KEY",
		"groq",
		"https://api.groq.com/openai/v1/chat/completions",
		envOrDefault("GROQ_MODEL", "llama-3.3-70b-versatile"),
		256,
	)

	return providers
}

// FileSummaryTemperature is the temperature for file summary generation.
// Set to 0.0 for consistent, deterministic outputs.
const FileSummaryTemperature = 0.0

// SummaryLLM implements the retrieval.SummaryLLM interface for file summaries.
type SummaryLLM struct {
	provider Provider
}

// NewSummaryLLM creates a new SummaryLLM from a provider.
// Returns nil if the provider cannot be used for summaries.
func NewSummaryLLM(provider Provider) *SummaryLLM {
	if provider.IsCLI {
		// CLI providers not supported for file summaries (too slow)
		return nil
	}
	return &SummaryLLM{
		provider: provider,
	}
}

// GenerateSummary implements the retrieval.SummaryLLM interface.
func (s *SummaryLLM) GenerateSummary(ctx context.Context, prompt string) (string, error) {
	// Build request body
	reqBody := map[string]any{
		"model": s.provider.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"temperature": FileSummaryTemperature,
		"max_tokens":  256,
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", s.provider.Endpoint, bytes.NewReader(reqJSON))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.provider.APIKey)

	// OpenRouter-specific headers
	if strings.Contains(s.provider.Endpoint, "openrouter.ai") {
		req.Header.Set("HTTP-Referer", "https://github.com/jkatigb/agentctl")
		req.Header.Set("X-Title", "agentctl")
	}

	// Execute request with timeout
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
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
		return "", fmt.Errorf("parse response: %w", err)
	}

	if len(respData.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return strings.TrimSpace(respData.Choices[0].Message.Content), nil
}
