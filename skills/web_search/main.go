// Package main implements the web/search skill for searching the web with Exa/Tavily.
package main

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

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
)

// Input defines the web search parameters with provider and extraction options.
type Input struct {
	Query        string `json:"query"`
	MaxResults   int    `json:"max_results"`
	Extract      bool   `json:"extract"`
	ExtractQuery string `json:"extract_query"`
	ExtractLimit int    `json:"extract_limit"`
	Provider     string `json:"provider"`
	Topic        string `json:"topic"`
}

// SearchResult represents a single search result with title, URL, snippet, and relevance score.
type SearchResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score,omitempty"`
}

// Extraction represents extracted content from a URL with contextual information.
type Extraction struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Query   string `json:"query,omitempty"`
}

// Output defines the skill output with search results, extractions, and metadata.
type Output struct {
	Results     []SearchResult `json:"results"`
	Extractions []Extraction   `json:"extractions,omitempty"`
	Provider    string         `json:"provider"`
	Query       string         `json:"query"`
	Truncated   bool           `json:"truncated,omitempty"`
	Artifact    string         `json:"artifact,omitempty"`
}

const command = "web/search"

// main is the skill entry point for web/search.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates web searches using Exa or Tavily providers with optional content extraction.
//
// Index:
//   Purpose: Search the web using Exa/Tavily APIs with optional content extraction from top results
//   Keywords: web/search, web_searching, exa_api, tavily_api, content_extraction, html_parsing
//   Related: searchExa, searchTavily, extractContent, extractTextFromHTML, removeTagContent
//   Flow: validate input → determine provider → execute search → extract content if requested → emit results
//   Resources: Exa API, Tavily API, HTTP client, CAS store
//   Events: none
//   OutputFields: results, extractions, provider, query, truncated, artifact
//
// [[domain:web_search]]
// [[risk:api_key_missing]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Validate input
	if strings.TrimSpace(in.Query) == "" {
		return skillerr.Arg("query is required")
	}

	// Set defaults
	if in.MaxResults <= 0 {
		in.MaxResults = 10
	}
	if in.MaxResults > 20 {
		in.MaxResults = 20
	}
	if in.ExtractLimit <= 0 {
		in.ExtractLimit = 3
	}
	if in.ExtractQuery == "" {
		in.ExtractQuery = in.Query
	}

	// Determine provider
	provider := in.Provider
	if provider == "" {
		provider = os.Getenv("FOXCTL_SEARCH_PROVIDER")
	}
	if provider == "" {
		// Check which API keys are available from config
		if rc.Config.Search.ExaAPIKey != "" {
			provider = "exa"
		} else if rc.Config.Search.TavilyAPIKey != "" {
			provider = "tavily"
		} else {
			return skillerr.Arg(
				"no search provider configured",
				skillerr.WithHint("Set EXA_API_KEY or TAVILY_API_KEY environment variable."),
			)
		}
	}

	var results []SearchResult
	var err error

	switch provider {
	case "exa":
		err = skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
			var e error
			results, e = searchExa(ctx, in, rc.Config.Search.ExaAPIKey)
			return e
		})
	case "tavily":
		err = skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
			var e error
			results, e = searchTavily(ctx, in, rc.Config.Search.TavilyAPIKey)
			return e
		})
	default:
		return skillerr.Arg(
			fmt.Sprintf("unknown provider: %s", provider),
			skillerr.WithHint("Use 'exa' or 'tavily'."),
		)
	}

	if err != nil {
		return err
	}

	output := Output{
		Results:  results,
		Provider: provider,
		Query:    in.Query,
	}

	// Extract content from top results if requested
	if in.Extract && len(results) > 0 {
		limit := in.ExtractLimit
		if limit > len(results) {
			limit = len(results)
		}

		extractions := make([]Extraction, 0, limit)
		for i := 0; i < limit; i++ {
			content, err := extractContent(ctx, results[i].URL, in.ExtractQuery)
			if err != nil {
				// Skip failed extractions but continue
				continue
			}
			extractions = append(extractions, Extraction{
				URL:     results[i].URL,
				Title:   results[i].Title,
				Content: content,
				Query:   in.ExtractQuery,
			})
		}
		output.Extractions = extractions
	}

	// Use CAS-aware emit for potentially large results
	return skillout.EmitWithCAS(ctx, rc, command, output)
}

// searchExa performs a search using Exa API with neural search and autoprompt.
func searchExa(ctx context.Context, in Input, apiKey string) ([]SearchResult, error) {
	if apiKey == "" {
		return nil, skillerr.Arg(
			"EXA_API_KEY not set",
			skillerr.WithHint("Set the EXA_API_KEY environment variable."),
		)
	}

	reqBody := map[string]any{
		"query":         in.Query,
		"numResults":    in.MaxResults,
		"type":          "neural",
		"useAutoprompt": true,
		"contents": map[string]any{
			"text": true,
		},
	}

	if in.Topic == "news" {
		reqBody["category"] = "news"
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, skillerr.WrapRuntime("marshal exa request", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.exa.ai/search", bytes.NewReader(body))
	if err != nil {
		return nil, skillerr.WrapRuntime("create exa request", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, skillerr.WrapIO("exa request failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, skillerr.Runtime(
			fmt.Sprintf("exa returned %d: %s", resp.StatusCode, string(respBody)),
		)
	}

	var exaResp struct {
		Results []struct {
			Title string  `json:"title"`
			URL   string  `json:"url"`
			Text  string  `json:"text"`
			Score float64 `json:"score"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&exaResp); err != nil {
		return nil, skillerr.WrapRuntime("decode exa response", err)
	}

	results := make([]SearchResult, len(exaResp.Results))
	for i, r := range exaResp.Results {
		snippet := r.Text
		if len(snippet) > 500 {
			snippet = snippet[:500] + "..."
		}
		results[i] = SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: snippet,
			Score:   r.Score,
		}
	}

	return results, nil
}

// searchTavily performs a search using Tavily API with advanced search depth.
func searchTavily(ctx context.Context, in Input, apiKey string) ([]SearchResult, error) {
	if apiKey == "" {
		return nil, skillerr.Arg(
			"TAVILY_API_KEY not set",
			skillerr.WithHint("Set the TAVILY_API_KEY environment variable."),
		)
	}

	reqBody := map[string]any{
		"api_key":        apiKey,
		"query":          in.Query,
		"max_results":    in.MaxResults,
		"search_depth":   "advanced",
		"include_answer": false,
	}

	if in.Topic == "news" {
		reqBody["topic"] = "news"
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, skillerr.WrapRuntime("marshal tavily request", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.tavily.com/search", bytes.NewReader(body))
	if err != nil {
		return nil, skillerr.WrapRuntime("create tavily request", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, skillerr.WrapIO("tavily request failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, skillerr.Runtime(
			fmt.Sprintf("tavily returned %d: %s", resp.StatusCode, string(respBody)),
		)
	}

	var tavilyResp struct {
		Results []struct {
			Title   string  `json:"title"`
			URL     string  `json:"url"`
			Content string  `json:"content"`
			Score   float64 `json:"score"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tavilyResp); err != nil {
		return nil, skillerr.WrapRuntime("decode tavily response", err)
	}

	results := make([]SearchResult, len(tavilyResp.Results))
	for i, r := range tavilyResp.Results {
		snippet := r.Content
		if len(snippet) > 500 {
			snippet = snippet[:500] + "..."
		}
		results[i] = SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: snippet,
			Score:   r.Score,
		}
	}

	return results, nil
}

// extractContent fetches URL content and extracts relevant snippets with size limits.
func extractContent(ctx context.Context, url string, query string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; foxctl/1.0)")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http %d", resp.StatusCode)
	}

	// Read limited content
	limitedReader := io.LimitReader(resp.Body, 100*1024) // 100KB limit
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return "", err
	}

	// Convert HTML to plain text (basic extraction)
	content := extractTextFromHTML(string(body))

	// Truncate to reasonable size
	if len(content) > 5000 {
		content = content[:5000] + "..."
	}

	return content, nil
}

// extractTextFromHTML performs basic HTML to text conversion with tag removal.
func extractTextFromHTML(html string) string {
	// Remove script and style tags
	html = removeTagContent(html, "script")
	html = removeTagContent(html, "style")
	html = removeTagContent(html, "noscript")

	// Remove HTML tags
	var result strings.Builder
	inTag := false
	for _, r := range html {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			result.WriteRune(' ')
		case !inTag:
			result.WriteRune(r)
		}
	}

	// Clean up whitespace
	text := result.String()
	lines := strings.Split(text, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}

	return strings.Join(cleaned, "\n")
}

// removeTagContent removes a tag and its content from HTML with proper nesting handling.
func removeTagContent(html, tag string) string {
	lower := strings.ToLower(html)
	result := html

	for {
		start := strings.Index(strings.ToLower(result), "<"+tag)
		if start == -1 {
			break
		}
		end := strings.Index(lower[start:], "</"+tag+">")
		if end == -1 {
			// Self-closing or malformed - just remove the opening tag
			tagEnd := strings.Index(result[start:], ">")
			if tagEnd != -1 {
				result = result[:start] + result[start+tagEnd+1:]
				lower = strings.ToLower(result)
			} else {
				break
			}
		} else {
			result = result[:start] + result[start+end+len("</"+tag+">"):]
			lower = strings.ToLower(result)
		}
	}

	return result
}
