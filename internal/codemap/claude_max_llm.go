package codemap

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/XiaoConstantine/dspy-go/pkg/core"
)

const (
	anthropicAPIURL   = "https://api.anthropic.com/v1/messages"
	anthropicTokenURL = "https://console.anthropic.com/v1/oauth/token"
	anthropicClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
)

// malformedXMLClosingWithAttr matches Claude's malformed closing tags like </arg key="...">
// which should be </arg><arg key="...">
var malformedXMLClosingWithAttr = regexp.MustCompile(`</(\w+)\s+(key="[^"]*")>`)

// malformedXMLClosingSimple matches simpler malformed closing tags like </arg key>
var malformedXMLClosingSimple = regexp.MustCompile(`</(\w+)\s+\w+>`)

// xmlCommentPattern matches XML comments like <!-- ... -->
var xmlCommentPattern = regexp.MustCompile(`<!--[\s\S]*?-->`)

// fixMalformedXML corrects Claude's habit of producing invalid XML.
// Claude often outputs: <arg key="query">value</arg key="limit">10</arg>
// Which should be: <arg key="query">value</arg><arg key="limit">10</arg>
// Also strips XML comments which Claude Haiku often includes.
func fixMalformedXML(content string) string {
	// Strip XML comments (Claude Haiku includes <!-- thinking --> etc)
	content = xmlCommentPattern.ReplaceAllString(content, "")
	// First fix: </arg key="limit"> -> </arg><arg key="limit">
	content = malformedXMLClosingWithAttr.ReplaceAllString(content, "</$1><$1 $2>")
	// Second fix: </arg key> -> </arg>
	content = malformedXMLClosingSimple.ReplaceAllString(content, "</$1>")
	return content
}

// ClaudeMaxTokenStorage stores OAuth2 tokens for Claude Max subscription.
type ClaudeMaxTokenStorage struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	LastRefresh  string `json:"last_refresh"`
	Email        string `json:"email"`
	Type         string `json:"type"`
	Expire       string `json:"expired"`
}

// TokenStorage abstracts token persistence for FC/IS compliance.
// Callers can inject custom implementations for testing or alternative storage.
type TokenStorage interface {
	// Load reads token data from storage.
	Load() ([]byte, error)
	// Save writes token data to storage.
	Save(data []byte) error
}

// FileTokenStorage is the default file-based token storage.
type FileTokenStorage struct {
	Path string
}

// Load reads token data from the file.
func (f *FileTokenStorage) Load() ([]byte, error) {
	data, err := os.ReadFile(f.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("claude max tokens not found, run 'agentctl auth claude-login' first")
		}
		return nil, err
	}
	return data, nil
}

// Save writes token data to the file.
func (f *FileTokenStorage) Save(data []byte) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(f.Path, data, 0o600)
}

// ClaudeMaxLLM implements core.LLM using Claude Max subscription OAuth.
type ClaudeMaxLLM struct {
	model      string
	token      *ClaudeMaxTokenStorage
	storage    TokenStorage // FC/IS: injected storage abstraction
	httpClient *http.Client
}

// ClaudeMaxLLMOption configures ClaudeMaxLLM.
type ClaudeMaxLLMOption func(*ClaudeMaxLLM)

// WithTokenStorage sets a custom token storage implementation.
// FC/IS: allows injecting storage at the boundary for testing.
func WithTokenStorage(storage TokenStorage) ClaudeMaxLLMOption {
	return func(l *ClaudeMaxLLM) {
		l.storage = storage
	}
}

// NewClaudeMaxLLM creates a new Claude Max LLM client.
// By default, uses file-based token storage at ~/.agentctl/auth/claude_token.json.
func NewClaudeMaxLLM(model string, opts ...ClaudeMaxLLMOption) (*ClaudeMaxLLM, error) {
	llm := &ClaudeMaxLLM{
		model:      model,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}

	// Apply options
	for _, opt := range opts {
		opt(llm)
	}

	// Default to file storage if not set
	if llm.storage == nil {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("get home dir: %w", err)
		}
		llm.storage = &FileTokenStorage{
			Path: filepath.Join(home, ".agentctl", "auth", "claude_token.json"),
		}
	}

	// Load existing tokens
	if err := llm.loadTokens(); err != nil {
		return nil, fmt.Errorf("load tokens: %w", err)
	}

	return llm, nil
}

func (l *ClaudeMaxLLM) loadTokens() error {
	data, err := l.storage.Load()
	if err != nil {
		return err
	}

	var token ClaudeMaxTokenStorage
	if err := json.Unmarshal(data, &token); err != nil {
		return fmt.Errorf("parse tokens: %w", err)
	}

	l.token = &token
	return nil
}

func (l *ClaudeMaxLLM) saveTokens() error {
	data, err := json.MarshalIndent(l.token, "", "  ")
	if err != nil {
		return err
	}

	return l.storage.Save(data)
}

func (l *ClaudeMaxLLM) refreshTokens(ctx context.Context) error {
	if l.token == nil || l.token.RefreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	reqBody := map[string]interface{}{
		"client_id":     anthropicClientID,
		"grant_type":    "refresh_token",
		"refresh_token": l.token.RefreshToken,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", anthropicTokenURL, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := l.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token refresh failed: %s", string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Account      struct {
			EmailAddress string `json:"email_address"`
		} `json:"account"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return err
	}

	l.token.AccessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		l.token.RefreshToken = tokenResp.RefreshToken
	}
	l.token.Email = tokenResp.Account.EmailAddress
	l.token.LastRefresh = time.Now().Format(time.RFC3339)
	l.token.Expire = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).Format(time.RFC3339)

	return l.saveTokens()
}

func (l *ClaudeMaxLLM) doRequest(ctx context.Context, body []byte, stream bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", anthropicAPIURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	// Set OAuth headers (different from API key auth)
	req.Header.Set("Authorization", "Bearer "+l.token.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Anthropic-Beta", "oauth-2025-04-20,interleaved-thinking-2025-05-14")
	req.Header.Set("Anthropic-Version", "2023-06-01")
	req.Header.Set("Anthropic-Dangerous-Direct-Browser-Access", "true")

	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}

	resp, err := l.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	// If unauthorized, try refreshing token once
	if resp.StatusCode == 401 {
		resp.Body.Close()
		if err := l.refreshTokens(ctx); err != nil {
			return nil, fmt.Errorf("token refresh failed: %w", err)
		}
		// Retry with new token
		req.Header.Set("Authorization", "Bearer "+l.token.AccessToken)
		return l.httpClient.Do(req)
	}

	return resp, nil
}

// Generate implements core.LLM.
func (l *ClaudeMaxLLM) Generate(ctx context.Context, prompt string, options ...core.GenerateOption) (*core.LLMResponse, error) {
	opts := &core.GenerateOptions{}
	for _, opt := range options {
		opt(opts)
	}

	maxTokens := 4096
	if opts.MaxTokens > 0 {
		maxTokens = opts.MaxTokens
	}

	reqBody := map[string]interface{}{
		"model":      l.model,
		"max_tokens": maxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	resp, err := l.doRequest(ctx, jsonBody, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	// Debug: log raw response
	if os.Getenv("AGENTCTL_DEBUG_LLM") == "1" {
		fmt.Fprintf(os.Stderr, "[DEBUG LLM] Raw response: %s\n", string(body))
	}

	var apiResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, err
	}

	var content string
	for _, c := range apiResp.Content {
		if c.Type == "text" {
			content += c.Text
		}
	}

	// Fix Claude's malformed XML: </arg key> -> </arg>
	content = fixMalformedXML(content)

	return &core.LLMResponse{
		Content: content,
		Usage: &core.TokenInfo{
			PromptTokens:     apiResp.Usage.InputTokens,
			CompletionTokens: apiResp.Usage.OutputTokens,
			TotalTokens:      apiResp.Usage.InputTokens + apiResp.Usage.OutputTokens,
		},
	}, nil
}

// GenerateWithJSON implements core.LLM.
func (l *ClaudeMaxLLM) GenerateWithJSON(ctx context.Context, prompt string, options ...core.GenerateOption) (map[string]interface{}, error) {
	resp, err := l.Generate(ctx, prompt, options...)
	if err != nil {
		return nil, err
	}

	// Try to extract JSON from response
	content := resp.Content
	// Look for JSON in the response
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		content = content[start : end+1]
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("parse JSON response: %w", err)
	}

	return result, nil
}

// GenerateWithFunctions implements core.LLM for tool calling.
func (l *ClaudeMaxLLM) GenerateWithFunctions(ctx context.Context, prompt string, functions []map[string]interface{}, options ...core.GenerateOption) (map[string]interface{}, error) {
	opts := &core.GenerateOptions{}
	for _, opt := range options {
		opt(opts)
	}

	maxTokens := 4096
	if opts.MaxTokens > 0 {
		maxTokens = opts.MaxTokens
	}

	// Convert OpenAI-style functions to Anthropic tools format
	tools := make([]map[string]interface{}, 0, len(functions))
	for _, fn := range functions {
		tool := map[string]interface{}{
			"name":        fn["name"],
			"description": fn["description"],
		}
		if params, ok := fn["parameters"]; ok {
			tool["input_schema"] = params
		}
		tools = append(tools, tool)
	}

	reqBody := map[string]interface{}{
		"model":      l.model,
		"max_tokens": maxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"tools": tools,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	resp, err := l.doRequest(ctx, jsonBody, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Content []struct {
			Type  string                 `json:"type"`
			Text  string                 `json:"text,omitempty"`
			ID    string                 `json:"id,omitempty"`
			Name  string                 `json:"name,omitempty"`
			Input map[string]interface{} `json:"input,omitempty"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	result := make(map[string]interface{})
	result["stop_reason"] = apiResp.StopReason

	for _, c := range apiResp.Content {
		switch c.Type {
		case "text":
			result["content"] = c.Text
		case "tool_use":
			// dspy-go's function_calling interceptor expects "function_call" key
			result["function_call"] = map[string]interface{}{
				"name":      c.Name,
				"arguments": c.Input,
			}
		}
	}

	return result, nil
}

// CreateEmbedding implements core.LLM (not supported for Claude).
func (l *ClaudeMaxLLM) CreateEmbedding(ctx context.Context, input string, options ...core.EmbeddingOption) (*core.EmbeddingResult, error) {
	return nil, fmt.Errorf("embeddings not supported by Claude")
}

// CreateEmbeddings implements core.LLM (not supported for Claude).
func (l *ClaudeMaxLLM) CreateEmbeddings(ctx context.Context, inputs []string, options ...core.EmbeddingOption) (*core.BatchEmbeddingResult, error) {
	return nil, fmt.Errorf("embeddings not supported by Claude")
}

// StreamGenerate implements core.LLM.
func (l *ClaudeMaxLLM) StreamGenerate(ctx context.Context, prompt string, options ...core.GenerateOption) (*core.StreamResponse, error) {
	opts := &core.GenerateOptions{}
	for _, opt := range options {
		opt(opts)
	}

	maxTokens := 4096
	if opts.MaxTokens > 0 {
		maxTokens = opts.MaxTokens
	}

	reqBody := map[string]interface{}{
		"model":      l.model,
		"max_tokens": maxTokens,
		"stream":     true,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	resp, err := l.doRequest(ctx, jsonBody, true)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	_, cancel := context.WithCancel(ctx)
	chunkChan := make(chan core.StreamChunk)

	go func() {
		defer close(chunkChan)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				chunkChan <- core.StreamChunk{Done: true}
				return
			}

			var event struct {
				Type  string `json:"type"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}

			if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" {
				chunkChan <- core.StreamChunk{Content: event.Delta.Text}
			}
		}

		if err := scanner.Err(); err != nil {
			chunkChan <- core.StreamChunk{Error: err}
		}
	}()

	return &core.StreamResponse{
		ChunkChannel: chunkChan,
		Cancel:       cancel,
	}, nil
}

// GenerateWithContent implements core.LLM for multimodal.
func (l *ClaudeMaxLLM) GenerateWithContent(ctx context.Context, content []core.ContentBlock, options ...core.GenerateOption) (*core.LLMResponse, error) {
	// For now, just extract text and use Generate
	var textParts []string
	for _, c := range content {
		if c.Type == core.FieldTypeText {
			textParts = append(textParts, c.Text)
		}
	}
	return l.Generate(ctx, strings.Join(textParts, "\n"), options...)
}

// StreamGenerateWithContent implements core.LLM.
func (l *ClaudeMaxLLM) StreamGenerateWithContent(ctx context.Context, content []core.ContentBlock, options ...core.GenerateOption) (*core.StreamResponse, error) {
	var textParts []string
	for _, c := range content {
		if c.Type == core.FieldTypeText {
			textParts = append(textParts, c.Text)
		}
	}
	return l.StreamGenerate(ctx, strings.Join(textParts, "\n"), options...)
}

// ProviderName implements core.LLM.
func (l *ClaudeMaxLLM) ProviderName() string {
	return "claude-max"
}

// ModelID implements core.LLM.
func (l *ClaudeMaxLLM) ModelID() string {
	return l.model
}

// Capabilities implements core.LLM.
func (l *ClaudeMaxLLM) Capabilities() []core.Capability {
	return []core.Capability{
		core.CapabilityCompletion,
		core.CapabilityChat,
		core.CapabilityStreaming,
		core.CapabilityToolCalling,
		core.CapabilityJSON,
	}
}
