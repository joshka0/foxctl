// Package main implements the web/extract skill for extracting content from URLs with HTML parsing and query-based snippet extraction.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
)

// Input defines the extraction parameters with URL list, query filtering, and content options.
type Input struct {
	URLs         []string `json:"urls"`
	Query        string   `json:"query"`
	MaxContentKB int      `json:"max_content_kb"`
	IncludeLinks bool     `json:"include_links"`
}

// Extraction represents extracted content from a URL with title, content, snippets, and links.
type Extraction struct {
	URL      string   `json:"url"`
	Title    string   `json:"title"`
	Content  string   `json:"content"`
	Snippets []string `json:"snippets,omitempty"`
	Links    []string `json:"links,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// Output defines the skill output with extraction results, query information, and artifact handling.
type Output struct {
	Extractions []Extraction `json:"extractions"`
	Query       string       `json:"query,omitempty"`
	Truncated   bool         `json:"truncated,omitempty"`
	Artifact    string       `json:"artifact,omitempty"`
}

const command = "web/extract"

// main is the skill entry point for web/extract with comprehensive URL content extraction capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates web content extraction with validation, HTTP requests, HTML parsing, and query-based snippet generation.
//
// Index:
// - Purpose: Extract content from multiple URLs with HTML-to-markdown conversion, query-based snippet extraction, and link harvesting
// - Flow: validate URLs → configure limits → create HTTP client → extract each URL → parse HTML → generate snippets → emit with CAS
// - SideEffects: makes HTTP requests; parses HTML content; extracts links; generates snippets; manages content limits
// - FailureModes: invalid URLs, HTTP errors, content size limits, HTML parsing failures, network timeouts
// - Observability: emits extraction results with titles, content, snippets, links, error information, and artifact references
// - Related: extractURL, extractTitle, extractLinks, htmlToMarkdown, extractRelevantSnippets
// - Keywords: web/extract, html_parsing, content_extraction, snippet_generation, link_extraction, markdown_conversion
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Validate input
	if len(in.URLs) == 0 {
		return skillerr.Arg("urls is required")
	}
	if len(in.URLs) > 10 {
		return skillerr.Arg(
			"too many URLs",
			skillerr.WithHint("Maximum 10 URLs per request."),
		)
	}

	// Set defaults
	if in.MaxContentKB <= 0 {
		in.MaxContentKB = 50
	}
	if in.MaxContentKB > 200 {
		in.MaxContentKB = 200
	}

	maxBytes := in.MaxContentKB * 1024

	extractions := make([]Extraction, 0, len(in.URLs))

	client := &http.Client{Timeout: 20 * time.Second}

	for _, url := range in.URLs {
		var extraction Extraction
		if err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
			extraction = extractURL(ctx, client, url, maxBytes, in.Query, in.IncludeLinks)
			if extraction.Error != "" {
				return fmt.Errorf("%s", extraction.Error)
			}
			return nil
		}); err != nil && extraction.Error == "" {
			extraction.Error = fmt.Sprintf("extract %s: %v", url, err)
		}
		extractions = append(extractions, extraction)
	}

	output := Output{
		Extractions: extractions,
		Query:       in.Query,
	}

	// Use CAS-aware emit for potentially large results
	return skillout.EmitWithCAS(ctx, rc, command, output)
}

// extractURL fetches and extracts content from a single URL with HTML parsing and snippet generation.
func extractURL(ctx context.Context, client *http.Client, url string, maxBytes int, query string, includeLinks bool) Extraction {
	extraction := Extraction{URL: url}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		extraction.Error = fmt.Sprintf("create request: %v", err)
		return extraction
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; foxctl/1.0; +https://github.com/joshka0/foxctl)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		extraction.Error = fmt.Sprintf("fetch: %v", err)
		return extraction
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		extraction.Error = fmt.Sprintf("http %d", resp.StatusCode)
		return extraction
	}

	// Read limited content
	limitedReader := io.LimitReader(resp.Body, int64(maxBytes))
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		extraction.Error = fmt.Sprintf("read: %v", err)
		return extraction
	}

	html := string(body)

	// Extract title
	extraction.Title = extractTitle(html)

	// Extract links if requested
	if includeLinks {
		extraction.Links = extractLinks(html, url)
	}

	// Convert to text
	content := htmlToMarkdown(html)

	// If query provided, extract relevant snippets
	if query != "" {
		snippets := extractRelevantSnippets(content, query, 5)
		extraction.Snippets = snippets
		// Also include condensed content
		if len(content) > 3000 {
			content = content[:3000] + "\n\n[content truncated - use snippets for relevant sections]"
		}
	}

	extraction.Content = content

	return extraction
}

// extractTitle extracts the page title from HTML using regex pattern matching.
func extractTitle(html string) string {
	re := regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)
	matches := re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// extractLinks extracts links from HTML with URL normalization and duplicate removal.
func extractLinks(html, baseURL string) []string {
	re := regexp.MustCompile(`(?i)<a[^>]+href=["']([^"']+)["']`)
	matches := re.FindAllStringSubmatch(html, -1)

	seen := make(map[string]bool)
	var links []string

	for _, match := range matches {
		if len(match) > 1 {
			link := match[1]
			// Skip anchors, javascript, mailto
			if strings.HasPrefix(link, "#") || strings.HasPrefix(link, "javascript:") || strings.HasPrefix(link, "mailto:") {
				continue
			}
			// Make relative URLs absolute (simple version)
			if strings.HasPrefix(link, "/") && !strings.HasPrefix(link, "//") {
				// Extract base domain
				if idx := strings.Index(baseURL[8:], "/"); idx > 0 {
					link = baseURL[:8+idx] + link
				} else {
					link = baseURL + link
				}
			}
			if !seen[link] {
				seen[link] = true
				links = append(links, link)
			}
		}
	}

	// Limit links
	if len(links) > 20 {
		links = links[:20]
	}

	return links
}

// htmlToMarkdown converts HTML to markdown-like text with tag removal and entity decoding.
func htmlToMarkdown(html string) string {
	// Remove script, style, nav, footer tags
	html = removeTagContent(html, "script")
	html = removeTagContent(html, "style")
	html = removeTagContent(html, "noscript")
	html = removeTagContent(html, "nav")
	html = removeTagContent(html, "footer")
	html = removeTagContent(html, "header")

	// Convert headers
	for i := 6; i >= 1; i-- {
		tag := fmt.Sprintf("h%d", i)
		prefix := strings.Repeat("#", i) + " "
		re := regexp.MustCompile(fmt.Sprintf(`(?i)<%s[^>]*>([^<]*)</%s>`, tag, tag))
		html = re.ReplaceAllString(html, "\n"+prefix+"$1\n")
	}

	// Convert paragraphs
	html = regexp.MustCompile(`(?i)<p[^>]*>`).ReplaceAllString(html, "\n")
	html = regexp.MustCompile(`(?i)</p>`).ReplaceAllString(html, "\n")

	// Convert line breaks
	html = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(html, "\n")

	// Convert list items
	html = regexp.MustCompile(`(?i)<li[^>]*>`).ReplaceAllString(html, "\n- ")
	html = regexp.MustCompile(`(?i)</li>`).ReplaceAllString(html, "")

	// Convert code blocks
	html = regexp.MustCompile(`(?i)<pre[^>]*>`).ReplaceAllString(html, "\n```\n")
	html = regexp.MustCompile(`(?i)</pre>`).ReplaceAllString(html, "\n```\n")
	html = regexp.MustCompile(`(?i)<code[^>]*>`).ReplaceAllString(html, "`")
	html = regexp.MustCompile(`(?i)</code>`).ReplaceAllString(html, "`")

	// Convert bold/strong
	html = regexp.MustCompile(`(?i)<(strong|b)[^>]*>`).ReplaceAllString(html, "**")
	html = regexp.MustCompile(`(?i)</(strong|b)>`).ReplaceAllString(html, "**")

	// Convert italic/em
	html = regexp.MustCompile(`(?i)<(em|i)[^>]*>`).ReplaceAllString(html, "*")
	html = regexp.MustCompile(`(?i)</(em|i)>`).ReplaceAllString(html, "*")

	// Remove remaining HTML tags
	html = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(html, "")

	// Decode common HTML entities
	html = strings.ReplaceAll(html, "&nbsp;", " ")
	html = strings.ReplaceAll(html, "&amp;", "&")
	html = strings.ReplaceAll(html, "&lt;", "<")
	html = strings.ReplaceAll(html, "&gt;", ">")
	html = strings.ReplaceAll(html, "&quot;", "\"")
	html = strings.ReplaceAll(html, "&#39;", "'")

	// Clean up whitespace
	lines := strings.Split(html, "\n")
	var cleaned []string
	prevEmpty := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if !prevEmpty {
				cleaned = append(cleaned, "")
				prevEmpty = true
			}
		} else {
			cleaned = append(cleaned, line)
			prevEmpty = false
		}
	}

	return strings.Join(cleaned, "\n")
}

// removeTagContent removes a tag and its content from HTML using regex pattern matching.
func removeTagContent(html, tag string) string {
	re := regexp.MustCompile(fmt.Sprintf(`(?is)<%s[^>]*>.*?</%s>`, tag, tag))
	return re.ReplaceAllString(html, "")
}

// extractRelevantSnippets finds paragraphs/sections relevant to the query with scoring and ranking.
func extractRelevantSnippets(content, query string, maxSnippets int) []string {
	queryLower := strings.ToLower(query)
	queryWords := strings.Fields(queryLower)

	// Split content into paragraphs
	paragraphs := strings.Split(content, "\n\n")

	type scored struct {
		text  string
		score int
	}

	var candidates []scored

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if len(para) < 50 || len(para) > 2000 {
			continue
		}

		paraLower := strings.ToLower(para)
		score := 0

		// Score based on query word matches
		for _, word := range queryWords {
			if strings.Contains(paraLower, word) {
				score += 10
				// Bonus for word appearing multiple times
				score += strings.Count(paraLower, word) * 2
			}
		}

		// Bonus for exact phrase match
		if strings.Contains(paraLower, queryLower) {
			score += 50
		}

		if score > 0 {
			candidates = append(candidates, scored{para, score})
		}
	}

	// Sort by score (simple bubble sort for small lists)
	for i := 0; i < len(candidates)-1; i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].score > candidates[i].score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	// Take top snippets
	var snippets []string
	for i := 0; i < len(candidates) && i < maxSnippets; i++ {
		snippets = append(snippets, candidates[i].text)
	}

	return snippets
}
