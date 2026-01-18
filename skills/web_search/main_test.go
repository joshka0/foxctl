package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "web/search", command)
}

// Tests for Input structure

func TestInput_AllFields(t *testing.T) {
	in := Input{
		Query:        "golang concurrency",
		MaxResults:   15,
		Extract:      true,
		ExtractQuery: "goroutines",
		ExtractLimit: 5,
		Provider:     "exa",
		Topic:        "news",
	}

	assert.Equal(t, "golang concurrency", in.Query)
	assert.Equal(t, 15, in.MaxResults)
	assert.True(t, in.Extract)
	assert.Equal(t, "goroutines", in.ExtractQuery)
	assert.Equal(t, 5, in.ExtractLimit)
	assert.Equal(t, "exa", in.Provider)
	assert.Equal(t, "news", in.Topic)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := Input{
		Query:      "test query",
		MaxResults: 10,
		Extract:    true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Query, decoded.Query)
	assert.Equal(t, in.MaxResults, decoded.MaxResults)
	assert.Equal(t, in.Extract, decoded.Extract)
}

func TestInput_EmptyFields(t *testing.T) {
	in := Input{}

	assert.Empty(t, in.Query)
	assert.Zero(t, in.MaxResults)
	assert.False(t, in.Extract)
	assert.Empty(t, in.ExtractQuery)
	assert.Zero(t, in.ExtractLimit)
	assert.Empty(t, in.Provider)
	assert.Empty(t, in.Topic)
}

func TestInput_ProviderValues(t *testing.T) {
	providers := []string{"exa", "tavily"}

	for _, provider := range providers {
		in := Input{Provider: provider}
		assert.Equal(t, provider, in.Provider)
	}
}

func TestInput_TopicValues(t *testing.T) {
	topics := []string{"general", "news"}

	for _, topic := range topics {
		in := Input{Topic: topic}
		assert.Equal(t, topic, in.Topic)
	}
}

// Tests for SearchResult structure

func TestSearchResult_AllFields(t *testing.T) {
	result := SearchResult{
		Title:   "Go Concurrency Guide",
		URL:     "https://example.com/go-concurrency",
		Snippet: "Learn about goroutines and channels...",
		Score:   0.95,
	}

	assert.Equal(t, "Go Concurrency Guide", result.Title)
	assert.Equal(t, "https://example.com/go-concurrency", result.URL)
	assert.Equal(t, "Learn about goroutines and channels...", result.Snippet)
	assert.Equal(t, 0.95, result.Score)
}

func TestSearchResult_JSONSerialization(t *testing.T) {
	result := SearchResult{
		Title: "Test Title",
		URL:   "https://test.com",
		Score: 0.8,
	}

	data, err := json.Marshal(result)
	assert.NoError(t, err)

	var decoded SearchResult
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, result.Title, decoded.Title)
	assert.Equal(t, result.URL, decoded.URL)
	assert.Equal(t, result.Score, decoded.Score)
}

func TestSearchResult_EmptyFields(t *testing.T) {
	result := SearchResult{}

	assert.Empty(t, result.Title)
	assert.Empty(t, result.URL)
	assert.Empty(t, result.Snippet)
	assert.Zero(t, result.Score)
}

func TestSearchResult_OmitEmptyScore(t *testing.T) {
	result := SearchResult{
		Title:   "Test",
		URL:     "https://test.com",
		Snippet: "snippet",
		// Score is 0
	}

	data, err := json.Marshal(result)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "score")
}

// Tests for Extraction structure

func TestExtraction_AllFields(t *testing.T) {
	extraction := Extraction{
		URL:     "https://example.com/article",
		Title:   "Article Title",
		Content: "This is the extracted content...",
		Query:   "search query",
	}

	assert.Equal(t, "https://example.com/article", extraction.URL)
	assert.Equal(t, "Article Title", extraction.Title)
	assert.Equal(t, "This is the extracted content...", extraction.Content)
	assert.Equal(t, "search query", extraction.Query)
}

func TestExtraction_JSONSerialization(t *testing.T) {
	extraction := Extraction{
		URL:     "https://test.com",
		Title:   "Test",
		Content: "content",
	}

	data, err := json.Marshal(extraction)
	assert.NoError(t, err)

	var decoded Extraction
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, extraction.URL, decoded.URL)
	assert.Equal(t, extraction.Title, decoded.Title)
	assert.Equal(t, extraction.Content, decoded.Content)
}

func TestExtraction_EmptyFields(t *testing.T) {
	extraction := Extraction{}

	assert.Empty(t, extraction.URL)
	assert.Empty(t, extraction.Title)
	assert.Empty(t, extraction.Content)
	assert.Empty(t, extraction.Query)
}

func TestExtraction_OmitEmptyQuery(t *testing.T) {
	extraction := Extraction{
		URL:     "https://test.com",
		Title:   "Test",
		Content: "content",
		// Query is empty
	}

	data, err := json.Marshal(extraction)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "query")
}

// Tests for Output structure

func TestOutput_AllFields(t *testing.T) {
	output := Output{
		Results: []SearchResult{
			{Title: "Result 1", URL: "https://r1.com"},
		},
		Extractions: []Extraction{
			{URL: "https://r1.com", Content: "extracted"},
		},
		Provider:  "exa",
		Query:     "test query",
		Truncated: true,
		Artifact:  "sha256:abc123",
	}

	assert.Len(t, output.Results, 1)
	assert.Len(t, output.Extractions, 1)
	assert.Equal(t, "exa", output.Provider)
	assert.Equal(t, "test query", output.Query)
	assert.True(t, output.Truncated)
	assert.Equal(t, "sha256:abc123", output.Artifact)
}

func TestOutput_JSONSerialization(t *testing.T) {
	output := Output{
		Results: []SearchResult{
			{Title: "Test", URL: "https://test.com"},
		},
		Provider: "tavily",
		Query:    "query",
	}

	data, err := json.Marshal(output)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, output.Provider, decoded.Provider)
	assert.Equal(t, output.Query, decoded.Query)
	assert.Len(t, decoded.Results, 1)
}

func TestOutput_EmptyResults(t *testing.T) {
	output := Output{
		Results:  []SearchResult{},
		Provider: "exa",
		Query:    "no results query",
	}

	assert.Empty(t, output.Results)
	assert.Equal(t, "exa", output.Provider)
}

func TestOutput_OmitEmptyFields(t *testing.T) {
	output := Output{
		Results:  []SearchResult{},
		Provider: "exa",
		Query:    "test",
		// Extractions, Truncated, Artifact are zero values
	}

	data, err := json.Marshal(output)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "extractions")
	assert.NotContains(t, jsonStr, "truncated")
	assert.NotContains(t, jsonStr, "artifact")
}

// Tests for extractTextFromHTML helper

func TestExtractTextFromHTML_Simple(t *testing.T) {
	html := "<p>Hello World</p>"
	result := extractTextFromHTML(html)
	assert.Contains(t, result, "Hello World")
}

func TestExtractTextFromHTML_RemovesScriptTags(t *testing.T) {
	html := "<script>alert('xss');</script><p>Safe content</p>"
	result := extractTextFromHTML(html)
	assert.NotContains(t, result, "alert")
	assert.NotContains(t, result, "xss")
	assert.Contains(t, result, "Safe content")
}

func TestExtractTextFromHTML_RemovesStyleTags(t *testing.T) {
	html := "<style>.class { color: red; }</style><p>Visible text</p>"
	result := extractTextFromHTML(html)
	assert.NotContains(t, result, "color")
	assert.NotContains(t, result, "red")
	assert.Contains(t, result, "Visible text")
}

func TestExtractTextFromHTML_RemovesNoscriptTags(t *testing.T) {
	html := "<noscript>Enable JavaScript</noscript><p>Main content</p>"
	result := extractTextFromHTML(html)
	assert.NotContains(t, result, "Enable JavaScript")
	assert.Contains(t, result, "Main content")
}

func TestExtractTextFromHTML_StripsTags(t *testing.T) {
	html := "<div><span>Nested</span> <strong>text</strong></div>"
	result := extractTextFromHTML(html)
	assert.Contains(t, result, "Nested")
	assert.Contains(t, result, "text")
	assert.NotContains(t, result, "<div>")
	assert.NotContains(t, result, "<span>")
}

func TestExtractTextFromHTML_HandlesEmptyLines(t *testing.T) {
	html := "<p>Line 1</p>\n\n\n<p>Line 2</p>"
	result := extractTextFromHTML(html)
	assert.Contains(t, result, "Line 1")
	assert.Contains(t, result, "Line 2")
}

func TestExtractTextFromHTML_EmptyInput(t *testing.T) {
	result := extractTextFromHTML("")
	assert.Empty(t, result)
}

func TestExtractTextFromHTML_PlainText(t *testing.T) {
	result := extractTextFromHTML("Just plain text")
	assert.Equal(t, "Just plain text", result)
}

// Tests for removeTagContent helper

func TestRemoveTagContent_RemovesTag(t *testing.T) {
	html := "<div>Keep</div><script>Remove</script><div>Also keep</div>"
	result := removeTagContent(html, "script")
	assert.Contains(t, result, "Keep")
	assert.Contains(t, result, "Also keep")
	assert.NotContains(t, result, "Remove")
}

func TestRemoveTagContent_CaseInsensitive(t *testing.T) {
	html := "<SCRIPT>Remove this</SCRIPT><p>Keep</p>"
	result := removeTagContent(html, "script")
	assert.NotContains(t, result, "Remove this")
	assert.Contains(t, result, "Keep")
}

func TestRemoveTagContent_MultipleTags(t *testing.T) {
	html := "<style>first</style><p>middle</p><style>second</style>"
	result := removeTagContent(html, "style")
	assert.NotContains(t, result, "first")
	assert.NotContains(t, result, "second")
	assert.Contains(t, result, "middle")
}

func TestRemoveTagContent_NestedContent(t *testing.T) {
	html := "<script><script>nested</script></script><p>visible</p>"
	result := removeTagContent(html, "script")
	assert.Contains(t, result, "visible")
}

func TestRemoveTagContent_TagWithAttributes(t *testing.T) {
	html := `<script type="text/javascript" src="app.js">code</script><p>text</p>`
	result := removeTagContent(html, "script")
	assert.NotContains(t, result, "code")
	assert.Contains(t, result, "text")
}

func TestRemoveTagContent_NoMatchingTag(t *testing.T) {
	html := "<div>content</div>"
	result := removeTagContent(html, "script")
	assert.Equal(t, html, result)
}

func TestRemoveTagContent_Empty(t *testing.T) {
	result := removeTagContent("", "script")
	assert.Empty(t, result)
}

// Tests for default logic

func TestInput_MaxResultsDefault(t *testing.T) {
	in := Input{}

	maxResults := in.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}

	assert.Equal(t, 10, maxResults)
}

func TestInput_MaxResultsPositive(t *testing.T) {
	in := Input{MaxResults: 15}

	maxResults := in.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}

	assert.Equal(t, 15, maxResults)
}

func TestInput_MaxResultsCapped(t *testing.T) {
	in := Input{MaxResults: 50}

	maxResults := in.MaxResults
	if maxResults > 20 {
		maxResults = 20
	}

	assert.Equal(t, 20, maxResults)
}

func TestInput_ExtractLimitDefault(t *testing.T) {
	in := Input{}

	extractLimit := in.ExtractLimit
	if extractLimit <= 0 {
		extractLimit = 3
	}

	assert.Equal(t, 3, extractLimit)
}

func TestInput_ExtractQueryDefault(t *testing.T) {
	in := Input{Query: "main query"}

	extractQuery := in.ExtractQuery
	if extractQuery == "" {
		extractQuery = in.Query
	}

	assert.Equal(t, "main query", extractQuery)
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := Input{
		Query:        "full query",
		MaxResults:   15,
		Extract:      true,
		ExtractQuery: "extract this",
		ExtractLimit: 5,
		Provider:     "tavily",
		Topic:        "news",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Query, decoded.Query)
	assert.Equal(t, in.MaxResults, decoded.MaxResults)
	assert.Equal(t, in.Extract, decoded.Extract)
	assert.Equal(t, in.ExtractQuery, decoded.ExtractQuery)
	assert.Equal(t, in.ExtractLimit, decoded.ExtractLimit)
	assert.Equal(t, in.Provider, decoded.Provider)
	assert.Equal(t, in.Topic, decoded.Topic)
}

func TestOutput_MultipleResults(t *testing.T) {
	output := Output{
		Results: []SearchResult{
			{Title: "Result 1", URL: "https://r1.com", Score: 0.9},
			{Title: "Result 2", URL: "https://r2.com", Score: 0.8},
			{Title: "Result 3", URL: "https://r3.com", Score: 0.7},
		},
		Provider: "exa",
		Query:    "test",
	}

	assert.Len(t, output.Results, 3)
	assert.Equal(t, 0.9, output.Results[0].Score)
	assert.Equal(t, "Result 3", output.Results[2].Title)
}

func TestOutput_WithExtractions(t *testing.T) {
	output := Output{
		Results: []SearchResult{
			{Title: "Result", URL: "https://r.com"},
		},
		Extractions: []Extraction{
			{URL: "https://r.com", Title: "Result", Content: "Full content here"},
		},
		Provider: "exa",
		Query:    "test",
	}

	assert.Len(t, output.Extractions, 1)
	assert.Equal(t, "Full content here", output.Extractions[0].Content)
}

func TestInput_JSONFieldNames(t *testing.T) {
	in := Input{
		Query:        "q",
		MaxResults:   1,
		Extract:      true,
		ExtractQuery: "eq",
		ExtractLimit: 2,
		Provider:     "p",
		Topic:        "t",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "query")
	assert.Contains(t, jsonStr, "max_results")
	assert.Contains(t, jsonStr, "extract")
	assert.Contains(t, jsonStr, "extract_query")
	assert.Contains(t, jsonStr, "extract_limit")
	assert.Contains(t, jsonStr, "provider")
	assert.Contains(t, jsonStr, "topic")
}

func TestExtractTextFromHTML_ComplexHTML(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<head>
<title>Test Page</title>
<script>var x = 1;</script>
<style>body { margin: 0; }</style>
</head>
<body>
<div class="content">
<p>First paragraph with <strong>bold</strong> text.</p>
<p>Second paragraph.</p>
</div>
<noscript>Please enable JavaScript</noscript>
</body>
</html>`

	result := extractTextFromHTML(html)
	assert.Contains(t, result, "First paragraph")
	assert.Contains(t, result, "bold")
	assert.Contains(t, result, "Second paragraph")
	assert.NotContains(t, result, "var x = 1")
	assert.NotContains(t, result, "margin: 0")
	assert.NotContains(t, result, "enable JavaScript")
}
