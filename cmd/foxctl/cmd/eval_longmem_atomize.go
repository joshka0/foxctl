package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// newLongmemAtomizer creates an AtomizeFn that calls an OpenRouter-compatible
// LLM to extract atomic facts from a session transcript. The facts are merged
// into entities and keywords at ingest time, boosting BM25 signal density for
// buried facts (e.g. "grandma gave me silver necklace on my 18th birthday"
// buried in a 15K-char jewelry organization conversation).
func newLongmemAtomizer(model, baseURL, apiKey string) func(ctx context.Context, sessionText string) ([]string, error) {
	return func(ctx context.Context, sessionText string) ([]string, error) {
		// Truncate session text to avoid token limits on free-tier models.
		if len(sessionText) > 8000 {
			sessionText = sessionText[:8000]
		}

		prompt := `Extract ALL factual statements from this conversation transcript. Focus on:
- Personal facts: names, ages, dates, locations, relationships
- Preferences: likes, dislikes, recommendations, choices
- Numbers: counts, prices, measurements, durations
- Events: what happened, when, where
- Possessions: items owned, lost, given, received

Output one fact per line. Each fact should be a concise standalone statement.
Do NOT include opinions, filler, or meta-commentary. ONLY facts.

Transcript:
` + sessionText

		reqBody := struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
			Messages  []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}{
			Model:     model,
			MaxTokens: 512,
			Messages: []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}{
				{Role: "user", Content: prompt},
			},
		}

		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("marshal atomize request: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("create atomize request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("atomize request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("atomize HTTP %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
		}

		var result struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode atomize response: %w", err)
		}

		if len(result.Choices) == 0 {
			return nil, fmt.Errorf("atomize: no choices in response")
		}

		content := strings.TrimSpace(result.Choices[0].Message.Content)
		if content == "" {
			return nil, nil
		}

		// Split into lines, trim each, filter empties.
		var facts []string
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			// Strip leading bullets/numbering.
			line = strings.TrimLeft(line, "-*0123456789. ")
			if line == "" || len(line) < 3 {
				continue
			}
			facts = append(facts, line)
		}
		return facts, nil
	}
}
