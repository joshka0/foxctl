// Package main implements the context/filter skill.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

// httpClient is a shared HTTP client with timeout for LLM provider calls.
var httpClient = &http.Client{
	Timeout: 120 * time.Second, // LLM calls can be slow
}

type input struct {
	Prompt string      `json:"prompt"`
	Scope  string      `json:"scope"`
	Source sourceInput `json:"source"`
	Budget budgetInput `json:"budget"`
	LLM    llmInput    `json:"llm"`
}

type sourceInput struct {
	CASDigest string          `json:"cas_digest"`
	Text      string          `json:"text"`
	Chunks    []rawChunkInput `json:"chunks"`
}

type rawChunkInput struct {
	ID       string         `json:"id"`
	Text     string         `json:"text"`
	Metadata map[string]any `json:"metadata"`
}

type budgetInput struct {
	TargetTokens    int `json:"target_tokens"`
	MaxChunks       int `json:"max_chunks"`
	MaxSourceTokens int `json:"max_source_tokens"`
}

type llmInput struct {
	Provider        string  `json:"provider"`
	Model           string  `json:"model"`
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"max_output_tokens"`
}

type candidateChunk struct {
	ID       string
	Text     string
	Metadata map[string]any
}

type llmSelectionResponse struct {
	Chunks []struct {
		ID        string  `json:"id"`
		Score     float64 `json:"score"`
		Rationale string  `json:"rationale"`
	} `json:"chunks"`
	Summary      string `json:"summary"`
	ApproxTokens int    `json:"approx_tokens"`
}

type outputChunk struct {
	ID        string         `json:"id"`
	Text      string         `json:"text"`
	Score     float64        `json:"score"`
	Metadata  map[string]any `json:"metadata"`
	Rationale string         `json:"rationale"`
}

type outputEnvelopeData struct {
	Prompt       string         `json:"prompt"`
	Scope        string         `json:"scope"`
	Chunks       []outputChunk  `json:"chunks"`
	Summary      string         `json:"summary"`
	ApproxTokens int            `json:"approx_tokens"`
	LLMUsage     map[string]any `json:"llm_usage"`
}

var debugContextFilter = strings.TrimSpace(os.Getenv("CONTEXT_FILTER_DEBUG")) != ""

func debugf(format string, args ...any) {
	if !debugContextFilter {
		return
	}
	// Debug output to stderr; error is not actionable.
	_, _ = fmt.Fprintf(os.Stderr, "context/filter: "+format+"\n", args...) //nolint:errcheck
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("context/filter", "ECONFIG", err)
	}

	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("context/filter", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("context/filter", "EARG", err)
	}

	if err := run(ctx, rc, in); err != nil {
		fail("context/filter", "ERUNTIME", err)
	}
}

func parseInput(r io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}
	in.Prompt = strings.TrimSpace(in.Prompt)
	if in.Prompt == "" {
		return input{}, fmt.Errorf("prompt is required")
	}
	if in.Scope == "" {
		in.Scope = "auto"
	}
	// Normalize budget defaults
	if in.Budget.TargetTokens <= 0 {
		in.Budget.TargetTokens = 2000
	}
	if in.Budget.MaxChunks <= 0 || in.Budget.MaxChunks > 64 {
		in.Budget.MaxChunks = 16
	}
	if in.Budget.MaxSourceTokens <= 0 {
		in.Budget.MaxSourceTokens = 16000
	}
	// Normalize LLM defaults
	if in.LLM.Provider == "" {
		in.LLM.Provider = "openai"
	}
	if in.LLM.Model == "" {
		switch in.LLM.Provider {
		case "openai":
			in.LLM.Model = "gpt-4.1-mini"
		case "anthropic":
			in.LLM.Model = "claude-sonnet-4-20250514" // Claude Sonnet 4.5
		case "gemini":
			in.LLM.Model = "gemini-2.0-flash" // Gemini 2.0
		case "groq":
			in.LLM.Model = "llama-3.3-70b-versatile" // Llama 3.3
		case "openrouter":
			in.LLM.Model = "openrouter/auto"
		default:
			return input{}, fmt.Errorf("unsupported llm.provider %q", in.LLM.Provider)
		}
	}
	if in.LLM.MaxOutputTokens <= 0 {
		in.LLM.MaxOutputTokens = 512
	}
	if in.LLM.Temperature < 0 {
		in.LLM.Temperature = 0
	}
	// Ensure we have some source
	if strings.TrimSpace(in.Source.Text) == "" && in.Source.CASDigest == "" && len(in.Source.Chunks) == 0 {
		return input{}, fmt.Errorf("source is required (cas_digest, text, or chunks)")
	}
	return in, nil
}

func run(ctx context.Context, rc *runner.RunnerContext, in input) error {
	candidates, err := buildCandidates(ctx, rc, in.Source, in.Budget.MaxSourceTokens)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		// Nothing to select from; return empty success to keep agents simple
		data := outputEnvelopeData{
			Prompt:       in.Prompt,
			Scope:        in.Scope,
			Chunks:       nil,
			Summary:      "no candidate chunks provided",
			ApproxTokens: 0,
			LLMUsage:     map[string]any{},
		}
		return rc.Emit("context/filter", data, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
	}

	selection, usage, err := callLLMForSelection(ctx, in, candidates)
	if err != nil {
		return err
	}

	selected := applySelection(selection, candidates, in.Budget)
	approxTokens := estimateTokens(selected)

	out := outputEnvelopeData{
		Prompt:       in.Prompt,
		Scope:        in.Scope,
		Chunks:       selected,
		Summary:      selection.Summary,
		ApproxTokens: approxTokens,
		LLMUsage:     usage,
	}

	return rc.Emit("context/filter", out, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func buildCandidates(ctx context.Context, rc *runner.RunnerContext, src sourceInput, maxSourceTokens int) ([]candidateChunk, error) {
	var candidates []candidateChunk

	// Prefer explicit chunks if provided
	if len(src.Chunks) > 0 {
		for _, ch := range src.Chunks {
			text := strings.TrimSpace(ch.Text)
			if text == "" {
				continue
			}
			candidates = append(candidates, candidateChunk{
				ID:       ch.ID,
				Text:     text,
				Metadata: ch.Metadata,
			})
		}
	} else {
		text := src.Text
		if text == "" && src.CASDigest != "" {
			// Load from CAS; treat payload as text (or JSON-as-text)
			reader, meta, err := rc.CASStore.Get(ctx, src.CASDigest)
			if err != nil {
				return nil, fmt.Errorf("load source from cas %q: %w", src.CASDigest, err)
			}
			defer func() {
				if reader != nil {
					errs.Ignore(reader.Close(), "close cas reader")
				}
			}()

			// Read with a reasonable cap to avoid OOM
			limit := meta.Size
			if limit <= 0 || limit > 512*1024 {
				limit = 512 * 1024
			}
			buf := &bytes.Buffer{}
			if _, err := io.CopyN(buf, reader, limit); err != nil && !errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("read cas payload: %w", err)
			}
			text = buf.String()
		}
		text = strings.TrimSpace(text)
		if text != "" {
			candidates = chunkText(text)
		}
	}

	// Enforce max candidate count and approximate token budget
	if len(candidates) > 128 {
		candidates = candidates[:128]
	}

	// Crude source token estimate: 1 token ~= 4 chars
	var totalChars int
	for i, c := range candidates {
		if totalChars/4 > maxSourceTokens {
			candidates = candidates[:i]
			break
		}
		totalChars += len(c.Text)
	}
	return candidates, nil
}

func chunkText(text string) []candidateChunk {
	// Simple heuristic: split on double newlines, then trim and cap size.
	parts := strings.Split(text, "\n\n")
	var out []candidateChunk
	idCounter := 0
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		// Cap chunk length to avoid giant pieces
		if len(p) > 2000 {
			p = p[:2000]
		}
		idCounter++
		out = append(out, candidateChunk{
			ID:   fmt.Sprintf("chunk-%d", idCounter),
			Text: p,
			Metadata: map[string]any{
				"index": idCounter,
			},
		})
		if idCounter >= 128 {
			break
		}
	}
	return out
}

func callLLMForSelection(ctx context.Context, in input, candidates []candidateChunk) (llmSelectionResponse, map[string]any, error) {
	prompt := buildLLMPrompt(in.Prompt, in.Scope, candidates, in.Budget)

	start := time.Now()
	content, usage, err := callProvider(ctx, in.LLM, prompt)
	if err != nil {
		return llmSelectionResponse{}, nil, err
	}
	_ = start // placeholder if we later want latency metrics in usage

	var sel llmSelectionResponse
	if err := json.Unmarshal([]byte(content), &sel); err != nil {
		return llmSelectionResponse{}, usage, fmt.Errorf("parse LLM JSON: %w", err)
	}

	if len(sel.Chunks) == 0 {
		// Treat as error to avoid silent empty selections
		return llmSelectionResponse{}, usage, fmt.Errorf("LLM returned no chunks in selection response")
	}

	return sel, usage, nil
}

//nolint:revive // strings.Builder.Write/WriteString never returns an error for in-memory writes.
func buildLLMPrompt(userPrompt, scope string, candidates []candidateChunk, budget budgetInput) string {
	// Prepare a compact JSON description of candidates with truncated text
	type candidateView struct {
		ID       string         `json:"id"`
		Preview  string         `json:"preview"`
		Metadata map[string]any `json:"metadata,omitempty"`
	}
	views := make([]candidateView, 0, len(candidates))
	for _, c := range candidates {
		preview := c.Text
		if len(preview) > 280 {
			preview = preview[:280] + "…"
		}
		views = append(views, candidateView{
			ID:       c.ID,
			Preview:  preview,
			Metadata: c.Metadata,
		})
	}
	// Marshal error is nil for valid struct slices.
	candJSON, _ := json.Marshal(views) //nolint:errcheck

	var b strings.Builder
	b.WriteString("You are a retrieval assistant. Given a user prompt and a list of candidate text chunks, " +
		"you must select the chunks that are most relevant to answering the prompt.\n\n")
	b.WriteString("Always respond with STRICT JSON ONLY, no markdown, no comments, no extra text.\n\n")
	b.WriteString("User prompt:\n")
	b.WriteString(userPrompt)
	b.WriteString("\n\n")
	if scope != "" {
		b.WriteString("Scope hint: " + scope + "\n\n")
	}
	b.WriteString("Candidate chunks (JSON array):\n")
	b.Write(candJSON)
	b.WriteString("\n\n")
	b.WriteString("Your task:\n")
	b.WriteString("1. Choose the most relevant chunks for the prompt.\n")
	b.WriteString("2. Prefer chunks that directly answer the question or will be most useful as context.\n")
	b.WriteString("3. You may choose fewer than the maximum if some chunks are not helpful.\n\n")
	b.WriteString("Constraints:\n")
	b.WriteString("- Maximum chunks: " + strconv.Itoa(budget.MaxChunks) + "\n")
	b.WriteString("- Target total tokens (approximate): " + strconv.Itoa(budget.TargetTokens) + "\n\n")
	b.WriteString("Respond with EXACTLY ONE JSON object of this form:\n")
	b.WriteString("{\n")
	b.WriteString("  \"chunks\": [\n")
	b.WriteString("    { \"id\": \"<candidate-id>\", \"score\": <float 0-1>, \"rationale\": \"short reason\" }\n")
	b.WriteString("  ],\n")
	b.WriteString("  \"summary\": \"short natural language summary of the selected chunks\",\n")
	b.WriteString("  \"approx_tokens\": <integer approximate token count>\n")
	b.WriteString("}\n\n")
	b.WriteString("Do not include the full text of any chunk, only IDs, scores, and rationales.")
	return b.String()
}

func applySelection(sel llmSelectionResponse, candidates []candidateChunk, budget budgetInput) []outputChunk {
	// Index candidates by ID for fast lookup
	index := make(map[string]candidateChunk, len(candidates))
	for _, c := range candidates {
		index[c.ID] = c
	}

	var out []outputChunk
	for _, ch := range sel.Chunks {
		cand, ok := index[ch.ID]
		if !ok {
			continue
		}
		out = append(out, outputChunk{
			ID:        cand.ID,
			Text:      cand.Text,
			Score:     ch.Score,
			Metadata:  cand.Metadata,
			Rationale: ch.Rationale,
		})
	}

	// Enforce max_chunks and target_tokens greedily in order returned by the LLM
	if len(out) == 0 {
		return out
	}

	var (
		kept  []outputChunk
		soFar int
	)
	for _, c := range out {
		if len(kept) >= budget.MaxChunks {
			break
		}
		newTokens := soFar + estimateTokens([]outputChunk{c})
		if newTokens > budget.TargetTokens && len(kept) > 0 {
			// stop when we exceed budget and have at least one chunk
			break
		}
		kept = append(kept, c)
		soFar = newTokens
	}
	return kept
}

func estimateTokens(chunks []outputChunk) int {
	// Very rough: 1 token ~ 4 chars of text
	var chars int
	for _, c := range chunks {
		chars += len(c.Text)
	}
	if chars == 0 {
		return 0
	}
	return chars / 4
}

func callProvider(ctx context.Context, llm llmInput, prompt string) (string, map[string]any, error) {
	switch llm.Provider {
	case "openai", "groq", "openrouter":
		return callOpenAICompatible(ctx, llm, prompt)
	case "anthropic":
		return callAnthropic(ctx, llm, prompt)
	case "gemini":
		return callGemini(ctx, llm, prompt)
	default:
		return "", nil, fmt.Errorf("unsupported llm.provider %q", llm.Provider)
	}
}

func callOpenAICompatible(ctx context.Context, llm llmInput, prompt string) (string, map[string]any, error) {
	var baseURL, apiKeyEnv, path string
	path = "/v1/chat/completions"
	switch llm.Provider {
	case "openai":
		baseURL = "https://api.openai.com"
		apiKeyEnv = "OPENAI_API_KEY"
	case "groq":
		baseURL = "https://api.groq.com"
		apiKeyEnv = "GROQ_API_KEY"
		// Groq exposes an OpenAI-compatible API under /openai/v1
		path = "/openai/v1/chat/completions"
	case "openrouter":
		baseURL = "https://api.openrouter.ai"
		apiKeyEnv = "OPENROUTER_API_KEY"
	}

	apiKey := strings.TrimSpace(os.Getenv(apiKeyEnv))
	if apiKey == "" {
		return "", nil, fmt.Errorf("missing API key for provider %s (env %s)", llm.Provider, apiKeyEnv)
	}

	url := strings.TrimRight(baseURL, "/") + path
	debugf("calling provider=%s url=%s model=%s", llm.Provider, url, llm.Model)
	body := map[string]any{
		"model":       llm.Model,
		"temperature": llm.Temperature,
		"max_tokens":  llm.MaxOutputTokens,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a helpful assistant that MUST respond with strict JSON only."},
			{"role": "user", "content": prompt},
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("http request: %w", err)
	}
	defer func() {
		if resp != nil && resp.Body != nil {
			errs.Ignore(resp.Body.Close(), "close openai-compatible response body")
		}
	}()

	debugf("provider=%s status=%d", llm.Provider, resp.StatusCode)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Error body read; error is not actionable in error path.
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048)) //nolint:errcheck
		return "", nil, fmt.Errorf("provider %s returned status %d: %s", llm.Provider, resp.StatusCode, strings.TrimSpace(string(b)))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("read response body: %w", err)
	}

	var parsed struct {
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
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		return "", nil, fmt.Errorf("decode response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", nil, fmt.Errorf("no choices in response")
	}

	usage := map[string]any{
		"provider":          llm.Provider,
		"model":             llm.Model,
		"prompt_tokens":     parsed.Usage.PromptTokens,
		"completion_tokens": parsed.Usage.CompletionTokens,
	}

	return parsed.Choices[0].Message.Content, usage, nil
}

func callAnthropic(ctx context.Context, llm llmInput, prompt string) (string, map[string]any, error) {
	apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if apiKey == "" {
		return "", nil, fmt.Errorf("missing API key for provider anthropic (env ANTHROPIC_API_KEY)")
	}

	url := "https://api.anthropic.com/v1/messages"
	body := map[string]any{
		"model":       llm.Model,
		"max_tokens":  llm.MaxOutputTokens,
		"temperature": llm.Temperature,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]string{
					{"type": "text", "text": prompt},
				},
			},
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("http request: %w", err)
	}
	defer func() {
		if resp != nil && resp.Body != nil {
			errs.Ignore(resp.Body.Close(), "close anthropic response body")
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Error body read; error is not actionable in error path.
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048)) //nolint:errcheck
		return "", nil, fmt.Errorf("provider anthropic returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", nil, fmt.Errorf("decode response: %w", err)
	}
	if len(parsed.Content) == 0 {
		return "", nil, fmt.Errorf("no content in response")
	}

	var text string
	for _, c := range parsed.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	usage := map[string]any{
		"provider":          "anthropic",
		"model":             llm.Model,
		"prompt_tokens":     parsed.Usage.InputTokens,
		"completion_tokens": parsed.Usage.OutputTokens,
	}

	return text, usage, nil
}

func callGemini(ctx context.Context, llm llmInput, prompt string) (string, map[string]any, error) {
	apiKey := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if apiKey == "" {
		return "", nil, fmt.Errorf("missing API key for provider gemini (env GEMINI_API_KEY)")
	}

	// Gemini uses key as query parameter
	baseURL := "https://generativelanguage.googleapis.com"
	url := strings.TrimRight(baseURL, "/") + "/v1beta/models/" + llm.Model + ":generateContent?key=" + apiKey

	body := map[string]any{
		"contents": []map[string]any{
			{
				"role": "user",
				"parts": []map[string]string{
					{"text": prompt},
				},
			},
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("http request: %w", err)
	}
	defer func() {
		if resp != nil && resp.Body != nil {
			errs.Ignore(resp.Body.Close(), "close gemini response body")
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Error body read; error is not actionable in error path.
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048)) //nolint:errcheck
		return "", nil, fmt.Errorf("provider gemini returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", nil, fmt.Errorf("decode response: %w", err)
	}
	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return "", nil, fmt.Errorf("no candidates in response")
	}

	text := parsed.Candidates[0].Content.Parts[0].Text
	usage := map[string]any{
		"provider":          "gemini",
		"model":             llm.Model,
		"prompt_tokens":     nil,
		"completion_tokens": nil,
	}

	return text, usage, nil
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit context/filter failure")
	os.Exit(1)
}
