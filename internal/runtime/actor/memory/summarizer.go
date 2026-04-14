package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/template"
	"time"
)

// Default prompts for summarization.
const (
	// SummarizeTurnsPromptTemplate generates L1 summaries from L0 turns.
	SummarizeTurnsPromptTemplate = `You are summarizing a batch of conversation turns for an AI agent's memory.

TASK CONTEXT: {{.TaskContext}}

TURNS TO SUMMARIZE:
{{range .Turns}}[{{.Role}}] {{.Content}}
{{end}}

Create a concise summary that captures:
1. What was accomplished
2. Key decisions made and their rationale
3. Important information discovered
4. Current state/progress
5. Any blockers or open questions

IMPORTANT - PRUNE THE FOLLOWING:
- Off-topic tangents or distractions
- Verbose explanations that can be condensed
- Redundant information (said multiple ways)
- Failed attempts (unless the failure taught something)
- Back-and-forth clarifications (keep only the resolution)

IMPORTANT - PRESERVE THE FOLLOWING:
- Exact file paths, function names, error messages
- Technical decisions and why they were made
- Gotchas or learnings worth remembering
- Current state and next steps

Output a focused summary in 2-4 paragraphs. After the summary, output key points and decisions in the following JSON format:
{"key_points": ["point1", "point2"], "decisions": ["decision1", "decision2"]}`

	// DistillSummariesPromptTemplate compresses L1 summaries into L2.
	DistillSummariesPromptTemplate = `You are distilling multiple conversation summaries into compressed session history.

TASK CONTEXT: {{.TaskContext}}

SUMMARIES TO DISTILL:
{{range .Summaries}}---
[Turns {{.TurnRange.Start}}-{{.TurnRange.End}}]
{{.Content}}
{{end}}

Create a highly compressed history that captures:
1. The overall trajectory of the session
2. Major milestones and completions
3. Critical decisions that affect future work
4. Accumulated knowledge/gotchas
5. Current state summary

AGGRESSIVE PRUNING:
- Remove anything tried and abandoned (unless it taught something)
- Remove debugging back-and-forth (keep only: "fixed X by doing Y")
- Remove explanations the agent already knows
- Collapse "after N attempts, solved by X"
- Remove emotional/social content

MUST PRESERVE:
- Decisions and their rationale
- Learnings that prevent future mistakes
- Current state and blockers
- File/function/variable names that matter

Output 3-5 bullet points or 2-3 short paragraphs.`

	// FilterByRelevancePromptTemplate scores items by relevance.
	FilterByRelevancePromptTemplate = `Given the current task, score each piece of information (0-10).

CURRENT TASK: {{.Task}}

ITEMS TO SCORE:
{{range $i, $item := .Items}}{{$i}}. {{$item}}
{{end}}

For each item, output JSON:
{"scores": [{"index": 0, "score": 8, "keep": true}, {"index": 1, "score": 3, "keep": false}]}

Items scoring <5 should have keep=false.`
)

// GeminiSummarizer uses Gemini Flash for summarization.
type GeminiSummarizer struct {
	apiKey     string
	model      string
	httpClient *http.Client

	// Templates (cached)
	summarizeTpl *template.Template
	distillTpl   *template.Template
	filterTpl    *template.Template
}

// GeminiSummarizerOption configures the summarizer.
type GeminiSummarizerOption func(*GeminiSummarizer)

// WithModel sets the Gemini model to use.
func WithModel(model string) GeminiSummarizerOption {
	return func(s *GeminiSummarizer) {
		s.model = model
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) GeminiSummarizerOption {
	return func(s *GeminiSummarizer) {
		s.httpClient = client
	}
}

// NewGeminiSummarizer creates a new Gemini-based summarizer.
func NewGeminiSummarizer(opts ...GeminiSummarizerOption) (*GeminiSummarizer, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY environment variable not set")
	}

	s := &GeminiSummarizer{
		apiKey: apiKey,
		model:  "gemini-2.0-flash-exp", // Fast, cheap model for summarization
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}

	for _, opt := range opts {
		opt(s)
	}

	// Parse templates
	var err error
	s.summarizeTpl, err = template.New("summarize").Parse(SummarizeTurnsPromptTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse summarize template: %w", err)
	}

	s.distillTpl, err = template.New("distill").Parse(DistillSummariesPromptTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse distill template: %w", err)
	}

	s.filterTpl, err = template.New("filter").Parse(FilterByRelevancePromptTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse filter template: %w", err)
	}

	return s, nil
}

// SummarizeTurns creates a summary from raw turns.
func (s *GeminiSummarizer) SummarizeTurns(ctx context.Context, task string, turns []Turn) (*Summary, error) {
	// Build prompt
	data := struct {
		TaskContext string
		Turns       []Turn
	}{
		TaskContext: task,
		Turns:       turns,
	}

	var buf bytes.Buffer
	if err := s.summarizeTpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}

	// Call Gemini
	response, err := s.callGemini(ctx, buf.String())
	if err != nil {
		return nil, fmt.Errorf("call gemini: %w", err)
	}

	// Parse response - extract JSON metadata if present
	content, keyPoints, decisions := parseGeminiSummaryResponse(response)

	// Ensure non-nil slices for JSON serialization
	if keyPoints == nil {
		keyPoints = []string{}
	}
	if decisions == nil {
		decisions = []string{}
	}

	// Calculate turn range
	var start, end int
	if len(turns) > 0 {
		start = turns[0].Index
		end = turns[len(turns)-1].Index
	}

	return &Summary{
		TurnRange: TurnRange{
			Start: start,
			End:   end,
		},
		Content:    content,
		KeyPoints:  keyPoints,
		Decisions:  decisions,
		TokenCount: EstimateTokens(content),
		CreatedAt:  time.Now(),
	}, nil
}

// DistillSummaries compresses multiple summaries into one.
func (s *GeminiSummarizer) DistillSummaries(ctx context.Context, task string, summaries []Summary) (string, error) {
	// Build prompt
	data := struct {
		TaskContext string
		Summaries   []Summary
	}{
		TaskContext: task,
		Summaries:   summaries,
	}

	var buf bytes.Buffer
	if err := s.distillTpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	// Call Gemini
	response, err := s.callGemini(ctx, buf.String())
	if err != nil {
		return "", fmt.Errorf("call gemini: %w", err)
	}

	return strings.TrimSpace(response), nil
}

// FilterByRelevance scores and filters items by relevance.
func (s *GeminiSummarizer) FilterByRelevance(ctx context.Context, task string, items []string) ([]string, error) {
	if len(items) == 0 {
		return []string{}, nil
	}

	// Build prompt
	data := struct {
		Task  string
		Items []string
	}{
		Task:  task,
		Items: items,
	}

	var buf bytes.Buffer
	if err := s.filterTpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}

	// Call Gemini
	response, err := s.callGemini(ctx, buf.String())
	if err != nil {
		return nil, fmt.Errorf("call gemini: %w", err)
	}

	// Parse JSON response
	var result struct {
		Scores []struct {
			Index int  `json:"index"`
			Score int  `json:"score"`
			Keep  bool `json:"keep"`
		} `json:"scores"`
	}

	// Find JSON in response
	jsonStart := strings.Index(response, "{")
	jsonEnd := strings.LastIndex(response, "}")
	if jsonStart >= 0 && jsonEnd > jsonStart {
		jsonStr := response[jsonStart : jsonEnd+1]
		if err := json.Unmarshal([]byte(jsonStr), &result); err == nil {
			filtered := []string{}
			for _, score := range result.Scores {
				if score.Keep && score.Index >= 0 && score.Index < len(items) {
					filtered = append(filtered, items[score.Index])
				}
			}
			return filtered, nil
		}
	}

	// If parsing fails, return all items (fail open)
	return items, nil
}

// callGemini makes an API call to Gemini.
func (s *GeminiSummarizer) callGemini(ctx context.Context, prompt string) (string, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent",
		s.model)

	// Build request body
	reqBody := map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]any{
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]any{
			"temperature":     0.3, // Lower temperature for more consistent summaries
			"maxOutputTokens": 2048,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Goog-API-Key", s.apiKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini API error: %s: %s", resp.Status, string(respBody))
	}

	// Parse response
	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from Gemini")
	}

	return geminiResp.Candidates[0].Content.Parts[0].Text, nil
}

// parseGeminiSummaryResponse extracts content and structured data from response.
func parseGeminiSummaryResponse(response string) (content string, keyPoints, decisions []string) {
	// Try to find JSON at the end
	jsonStart := strings.LastIndex(response, "{")
	if jsonStart >= 0 {
		jsonStr := response[jsonStart:]

		var metadata struct {
			KeyPoints []string `json:"key_points"`
			Decisions []string `json:"decisions"`
		}

		if err := json.Unmarshal([]byte(jsonStr), &metadata); err == nil {
			content = strings.TrimSpace(response[:jsonStart])
			return content, metadata.KeyPoints, metadata.Decisions
		}
	}

	// No JSON found, return full response as content
	return strings.TrimSpace(response), []string{}, []string{}
}

// MockSummarizer is a test implementation of Summarizer.
type MockSummarizer struct {
	SummarizeFunc func(ctx context.Context, task string, turns []Turn) (*Summary, error)
	DistillFunc   func(ctx context.Context, task string, summaries []Summary) (string, error)
	FilterFunc    func(ctx context.Context, task string, items []string) ([]string, error)
}

// SummarizeTurns implements Summarizer.
func (m *MockSummarizer) SummarizeTurns(ctx context.Context, task string, turns []Turn) (*Summary, error) {
	if m.SummarizeFunc != nil {
		return m.SummarizeFunc(ctx, task, turns)
	}

	// Default: simple mock summary
	var start, end int
	if len(turns) > 0 {
		start = turns[0].Index
		end = turns[len(turns)-1].Index
	}

	return &Summary{
		TurnRange: TurnRange{
			Start: start,
			End:   end,
		},
		Content:    fmt.Sprintf("Mock summary of %d turns", len(turns)),
		KeyPoints:  []string{"mock key point"},
		Decisions:  []string{"mock decision"},
		TokenCount: 50,
		CreatedAt:  time.Now(),
	}, nil
}

// DistillSummaries implements Summarizer.
func (m *MockSummarizer) DistillSummaries(ctx context.Context, task string, summaries []Summary) (string, error) {
	if m.DistillFunc != nil {
		return m.DistillFunc(ctx, task, summaries)
	}

	// Default: simple mock distillation
	return fmt.Sprintf("Mock distillation of %d summaries", len(summaries)), nil
}

// FilterByRelevance implements Summarizer.
func (m *MockSummarizer) FilterByRelevance(ctx context.Context, task string, items []string) ([]string, error) {
	if m.FilterFunc != nil {
		return m.FilterFunc(ctx, task, items)
	}

	// Default: return all items
	return items, nil
}

// Ensure implementations satisfy interface.
var (
	_ Summarizer = (*GeminiSummarizer)(nil)
	_ Summarizer = (*MockSummarizer)(nil)
)
