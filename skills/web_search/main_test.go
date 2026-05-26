package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

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

func TestOutput_EmptyResults(t *testing.T) {
	output := Output{
		Results:  []SearchResult{},
		Provider: "exa",
		Query:    "no results query",
	}

	assert.Empty(t, output.Results)
	assert.Equal(t, "exa", output.Provider)
}

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
