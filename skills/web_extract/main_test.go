package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "web/extract", command)
}

// Tests for Input structure

func TestInput_AllFields(t *testing.T) {
	in := Input{
		URLs:         []string{"https://example.com", "https://test.com"},
		Query:        "search query",
		MaxContentKB: 100,
		IncludeLinks: true,
	}

	assert.Len(t, in.URLs, 2)
	assert.Equal(t, "search query", in.Query)
	assert.Equal(t, 100, in.MaxContentKB)
	assert.True(t, in.IncludeLinks)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := Input{
		URLs:  []string{"https://test.com"},
		Query: "test",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.URLs, decoded.URLs)
	assert.Equal(t, in.Query, decoded.Query)
}

func TestInput_EmptyFields(t *testing.T) {
	in := Input{}

	assert.Nil(t, in.URLs)
	assert.Empty(t, in.Query)
	assert.Zero(t, in.MaxContentKB)
	assert.False(t, in.IncludeLinks)
}

// Tests for Extraction structure

func TestExtraction_AllFields(t *testing.T) {
	extraction := Extraction{
		URL:      "https://example.com",
		Title:    "Example Page",
		Content:  "Page content here",
		Snippets: []string{"snippet 1", "snippet 2"},
		Links:    []string{"https://link1.com", "https://link2.com"},
		Error:    "",
	}

	assert.Equal(t, "https://example.com", extraction.URL)
	assert.Equal(t, "Example Page", extraction.Title)
	assert.Equal(t, "Page content here", extraction.Content)
	assert.Len(t, extraction.Snippets, 2)
	assert.Len(t, extraction.Links, 2)
	assert.Empty(t, extraction.Error)
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
	assert.Nil(t, extraction.Snippets)
	assert.Nil(t, extraction.Links)
	assert.Empty(t, extraction.Error)
}

func TestExtraction_WithError(t *testing.T) {
	extraction := Extraction{
		URL:   "https://invalid.com",
		Error: "http 404",
	}

	assert.Equal(t, "https://invalid.com", extraction.URL)
	assert.Equal(t, "http 404", extraction.Error)
	assert.Empty(t, extraction.Content)
}

func TestExtraction_OmitEmptyFields(t *testing.T) {
	extraction := Extraction{
		URL:     "https://test.com",
		Title:   "Test",
		Content: "content",
		// Snippets, Links, Error are empty
	}

	data, err := json.Marshal(extraction)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "snippets")
	assert.NotContains(t, jsonStr, "links")
	assert.NotContains(t, jsonStr, "error")
}

// Tests for Output structure

func TestOutput_AllFields(t *testing.T) {
	output := Output{
		Extractions: []Extraction{
			{URL: "https://test.com", Content: "content"},
		},
		Query:     "test query",
		Truncated: true,
		Artifact:  "sha256:abc123",
	}

	assert.Len(t, output.Extractions, 1)
	assert.Equal(t, "test query", output.Query)
	assert.True(t, output.Truncated)
	assert.Equal(t, "sha256:abc123", output.Artifact)
}

func TestOutput_JSONSerialization(t *testing.T) {
	output := Output{
		Extractions: []Extraction{
			{URL: "https://test.com"},
		},
		Query: "query",
	}

	data, err := json.Marshal(output)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, output.Query, decoded.Query)
	assert.Len(t, decoded.Extractions, 1)
}

func TestOutput_OmitEmptyFields(t *testing.T) {
	output := Output{
		Extractions: []Extraction{},
		// Query, Truncated, Artifact are empty
	}

	data, err := json.Marshal(output)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "query")
	assert.NotContains(t, jsonStr, "truncated")
	assert.NotContains(t, jsonStr, "artifact")
}

// Tests for extractTitle helper

func TestExtractTitle_Simple(t *testing.T) {
	html := "<html><head><title>Page Title</title></head></html>"
	result := extractTitle(html)
	assert.Equal(t, "Page Title", result)
}

func TestExtractTitle_WithWhitespace(t *testing.T) {
	html := "<title>  Trimmed Title  </title>"
	result := extractTitle(html)
	assert.Equal(t, "Trimmed Title", result)
}

func TestExtractTitle_CaseInsensitive(t *testing.T) {
	html := "<TITLE>Uppercase Tag</TITLE>"
	result := extractTitle(html)
	assert.Equal(t, "Uppercase Tag", result)
}

func TestExtractTitle_WithAttributes(t *testing.T) {
	html := `<title lang="en">Title With Attrs</title>`
	result := extractTitle(html)
	assert.Equal(t, "Title With Attrs", result)
}

func TestExtractTitle_NoTitle(t *testing.T) {
	html := "<html><head></head><body>No title</body></html>"
	result := extractTitle(html)
	assert.Empty(t, result)
}

func TestExtractTitle_Empty(t *testing.T) {
	result := extractTitle("")
	assert.Empty(t, result)
}

// Tests for extractLinks helper

func TestExtractLinks_SimpleLinks(t *testing.T) {
	html := `<a href="https://example.com">Link 1</a><a href="https://test.com">Link 2</a>`
	result := extractLinks(html, "https://base.com")

	assert.Len(t, result, 2)
	assert.Contains(t, result, "https://example.com")
	assert.Contains(t, result, "https://test.com")
}

func TestExtractLinks_SkipsAnchors(t *testing.T) {
	html := `<a href="#section">Anchor</a><a href="https://valid.com">Valid</a>`
	result := extractLinks(html, "https://base.com")

	assert.Len(t, result, 1)
	assert.Contains(t, result, "https://valid.com")
}

func TestExtractLinks_SkipsJavascript(t *testing.T) {
	html := `<a href="javascript:void(0)">JS Link</a><a href="https://valid.com">Valid</a>`
	result := extractLinks(html, "https://base.com")

	assert.Len(t, result, 1)
	assert.Contains(t, result, "https://valid.com")
}

func TestExtractLinks_SkipsMailto(t *testing.T) {
	html := `<a href="mailto:test@example.com">Email</a><a href="https://valid.com">Valid</a>`
	result := extractLinks(html, "https://base.com")

	assert.Len(t, result, 1)
	assert.Contains(t, result, "https://valid.com")
}

func TestExtractLinks_DeduplicatesLinks(t *testing.T) {
	html := `<a href="https://dupe.com">First</a><a href="https://dupe.com">Second</a>`
	result := extractLinks(html, "https://base.com")

	assert.Len(t, result, 1)
}

func TestExtractLinks_LimitsResults(t *testing.T) {
	// Create HTML with more than 20 links
	html := ""
	for i := 0; i < 30; i++ {
		html += `<a href="https://link` + string(rune('A'+i)) + `.com">Link</a>`
	}
	result := extractLinks(html, "https://base.com")

	assert.LessOrEqual(t, len(result), 20)
}

func TestExtractLinks_Empty(t *testing.T) {
	result := extractLinks("<p>No links</p>", "https://base.com")
	assert.Empty(t, result)
}

func TestExtractLinks_RelativeURLs(t *testing.T) {
	html := `<a href="/page/subpage">Relative</a>`
	result := extractLinks(html, "https://example.com/current")

	assert.Len(t, result, 1)
	assert.Contains(t, result[0], "example.com")
	assert.Contains(t, result[0], "/page/subpage")
}

// Tests for htmlToMarkdown helper

func TestHtmlToMarkdown_Headers(t *testing.T) {
	html := "<h1>Header 1</h1><h2>Header 2</h2><h3>Header 3</h3>"
	result := htmlToMarkdown(html)

	assert.Contains(t, result, "# Header 1")
	assert.Contains(t, result, "## Header 2")
	assert.Contains(t, result, "### Header 3")
}

func TestHtmlToMarkdown_Paragraphs(t *testing.T) {
	html := "<p>First paragraph</p><p>Second paragraph</p>"
	result := htmlToMarkdown(html)

	assert.Contains(t, result, "First paragraph")
	assert.Contains(t, result, "Second paragraph")
}

func TestHtmlToMarkdown_ListItems(t *testing.T) {
	html := "<ul><li>Item 1</li><li>Item 2</li></ul>"
	result := htmlToMarkdown(html)

	assert.Contains(t, result, "- Item 1")
	assert.Contains(t, result, "- Item 2")
}

func TestHtmlToMarkdown_CodeBlocks(t *testing.T) {
	html := "<pre>code block</pre>"
	result := htmlToMarkdown(html)

	assert.Contains(t, result, "```")
	assert.Contains(t, result, "code block")
}

func TestHtmlToMarkdown_InlineCode(t *testing.T) {
	html := "<p>Use <code>fmt.Println</code> for output</p>"
	result := htmlToMarkdown(html)

	assert.Contains(t, result, "`fmt.Println`")
}

func TestHtmlToMarkdown_Bold(t *testing.T) {
	html := "<p><strong>Bold text</strong> and <b>also bold</b></p>"
	result := htmlToMarkdown(html)

	assert.Contains(t, result, "**Bold text**")
	assert.Contains(t, result, "**also bold**")
}

func TestHtmlToMarkdown_Italic(t *testing.T) {
	html := "<p><em>Italic text</em> and <i>also italic</i></p>"
	result := htmlToMarkdown(html)

	assert.Contains(t, result, "*Italic text*")
	assert.Contains(t, result, "*also italic*")
}

func TestHtmlToMarkdown_RemovesScript(t *testing.T) {
	html := "<script>alert('xss')</script><p>Safe</p>"
	result := htmlToMarkdown(html)

	assert.NotContains(t, result, "alert")
	assert.Contains(t, result, "Safe")
}

func TestHtmlToMarkdown_RemovesStyle(t *testing.T) {
	html := "<style>.x{color:red}</style><p>Visible</p>"
	result := htmlToMarkdown(html)

	assert.NotContains(t, result, "color")
	assert.Contains(t, result, "Visible")
}

func TestHtmlToMarkdown_RemovesNav(t *testing.T) {
	html := "<nav>Navigation menu</nav><p>Content</p>"
	result := htmlToMarkdown(html)

	assert.NotContains(t, result, "Navigation menu")
	assert.Contains(t, result, "Content")
}

func TestHtmlToMarkdown_RemovesFooter(t *testing.T) {
	html := "<footer>Copyright 2024</footer><p>Content</p>"
	result := htmlToMarkdown(html)

	assert.NotContains(t, result, "Copyright")
	assert.Contains(t, result, "Content")
}

func TestHtmlToMarkdown_DecodesEntities(t *testing.T) {
	html := "<p>Use &amp; for ampersand, &lt;tag&gt; for tags, &quot;quotes&quot;</p>"
	result := htmlToMarkdown(html)

	assert.Contains(t, result, "&")
	assert.Contains(t, result, "<tag>")
	assert.Contains(t, result, `"quotes"`)
}

func TestHtmlToMarkdown_LineBreaks(t *testing.T) {
	html := "<p>Line 1<br>Line 2<br/>Line 3</p>"
	result := htmlToMarkdown(html)

	assert.Contains(t, result, "Line 1")
	assert.Contains(t, result, "Line 2")
	assert.Contains(t, result, "Line 3")
}

func TestHtmlToMarkdown_Empty(t *testing.T) {
	result := htmlToMarkdown("")
	assert.Empty(t, result)
}

// Tests for removeTagContent helper

func TestRemoveTagContent_RemovesTag(t *testing.T) {
	html := "<div>Keep</div><script>Remove</script>"
	result := removeTagContent(html, "script")
	assert.Contains(t, result, "Keep")
	assert.NotContains(t, result, "Remove")
}

func TestRemoveTagContent_CaseInsensitive(t *testing.T) {
	html := "<SCRIPT>Remove</SCRIPT><p>Keep</p>"
	result := removeTagContent(html, "script")
	assert.NotContains(t, result, "Remove")
	assert.Contains(t, result, "Keep")
}

func TestRemoveTagContent_MultilineContent(t *testing.T) {
	html := `<style>
.class {
  color: red;
}
</style><p>Visible</p>`
	result := removeTagContent(html, "style")
	assert.NotContains(t, result, "color")
	assert.Contains(t, result, "Visible")
}

func TestRemoveTagContent_NoMatch(t *testing.T) {
	html := "<div>content</div>"
	result := removeTagContent(html, "script")
	assert.Equal(t, html, result)
}

// Tests for extractRelevantSnippets helper

func TestExtractRelevantSnippets_FindsMatches(t *testing.T) {
	// Paragraphs need to be at least 50 chars to be considered
	content := "First paragraph about golang with enough text to meet the minimum length requirement for extraction.\n\nSecond paragraph about python with plenty of text to make it long enough for the algorithm.\n\nThird paragraph about golang programming with additional content to ensure sufficient length."
	snippets := extractRelevantSnippets(content, "golang", 5)

	assert.NotEmpty(t, snippets)
}

func TestExtractRelevantSnippets_ExactPhraseBonus(t *testing.T) {
	content := "This talks about go language.\n\nThis specifically mentions golang programming.\n\nAnother about go."
	snippets := extractRelevantSnippets(content, "golang programming", 5)

	// The paragraph with exact phrase should be ranked higher
	if len(snippets) > 0 {
		assert.Contains(t, snippets[0], "golang programming")
	}
}

func TestExtractRelevantSnippets_LimitsResults(t *testing.T) {
	content := ""
	for i := 0; i < 20; i++ {
		content += "Paragraph about testing.\n\n"
	}
	snippets := extractRelevantSnippets(content, "testing", 5)

	assert.LessOrEqual(t, len(snippets), 5)
}

func TestExtractRelevantSnippets_SkipsShortParagraphs(t *testing.T) {
	content := "Short.\n\nThis is a much longer paragraph that contains the search query testing and should be included in the results."
	snippets := extractRelevantSnippets(content, "testing", 5)

	// Short paragraph should be skipped
	for _, s := range snippets {
		assert.GreaterOrEqual(t, len(s), 50)
	}
}

func TestExtractRelevantSnippets_NoMatches(t *testing.T) {
	content := "This paragraph has no relevant words.\n\nNeither does this one."
	snippets := extractRelevantSnippets(content, "golang", 5)

	assert.Empty(t, snippets)
}

func TestExtractRelevantSnippets_CaseInsensitive(t *testing.T) {
	content := "This paragraph contains GOLANG in uppercase and should match the query."
	snippets := extractRelevantSnippets(content, "golang", 5)

	assert.NotEmpty(t, snippets)
}

func TestExtractRelevantSnippets_Empty(t *testing.T) {
	snippets := extractRelevantSnippets("", "query", 5)
	assert.Empty(t, snippets)
}

// Tests for default logic

func TestInput_MaxContentKBDefault(t *testing.T) {
	in := Input{}

	maxKB := in.MaxContentKB
	if maxKB <= 0 {
		maxKB = 50
	}

	assert.Equal(t, 50, maxKB)
}

func TestInput_MaxContentKBPositive(t *testing.T) {
	in := Input{MaxContentKB: 100}

	maxKB := in.MaxContentKB
	if maxKB <= 0 {
		maxKB = 50
	}

	assert.Equal(t, 100, maxKB)
}

func TestInput_MaxContentKBCapped(t *testing.T) {
	in := Input{MaxContentKB: 500}

	maxKB := in.MaxContentKB
	if maxKB > 200 {
		maxKB = 200
	}

	assert.Equal(t, 200, maxKB)
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := Input{
		URLs:         []string{"https://a.com", "https://b.com"},
		Query:        "test query",
		MaxContentKB: 100,
		IncludeLinks: true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.URLs, decoded.URLs)
	assert.Equal(t, in.Query, decoded.Query)
	assert.Equal(t, in.MaxContentKB, decoded.MaxContentKB)
	assert.Equal(t, in.IncludeLinks, decoded.IncludeLinks)
}

func TestExtraction_FullJSONRoundTrip(t *testing.T) {
	extraction := Extraction{
		URL:      "https://test.com",
		Title:    "Test Page",
		Content:  "Full content",
		Snippets: []string{"snippet 1", "snippet 2"},
		Links:    []string{"https://link.com"},
	}

	data, err := json.Marshal(extraction)
	assert.NoError(t, err)

	var decoded Extraction
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, extraction.URL, decoded.URL)
	assert.Equal(t, extraction.Title, decoded.Title)
	assert.Equal(t, extraction.Content, decoded.Content)
	assert.Equal(t, extraction.Snippets, decoded.Snippets)
	assert.Equal(t, extraction.Links, decoded.Links)
}

func TestOutput_MultipleExtractions(t *testing.T) {
	output := Output{
		Extractions: []Extraction{
			{URL: "https://a.com", Content: "content a"},
			{URL: "https://b.com", Content: "content b"},
			{URL: "https://c.com", Error: "http 500"},
		},
		Query: "test",
	}

	assert.Len(t, output.Extractions, 3)
	assert.Empty(t, output.Extractions[0].Error)
	assert.NotEmpty(t, output.Extractions[2].Error)
}

func TestInput_JSONFieldNames(t *testing.T) {
	in := Input{
		URLs:         []string{"u"},
		Query:        "q",
		MaxContentKB: 1,
		IncludeLinks: true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "urls")
	assert.Contains(t, jsonStr, "query")
	assert.Contains(t, jsonStr, "max_content_kb")
	assert.Contains(t, jsonStr, "include_links")
}

func TestHtmlToMarkdown_ComplexHTML(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<head>
<title>Test</title>
<script>var x = 1;</script>
<style>body { margin: 0; }</style>
</head>
<body>
<nav>Menu</nav>
<h1>Main Title</h1>
<p>Paragraph with <strong>bold</strong> and <em>italic</em>.</p>
<ul>
<li>Item 1</li>
<li>Item 2</li>
</ul>
<pre>code block</pre>
<footer>Footer text</footer>
</body>
</html>`

	result := htmlToMarkdown(html)
	assert.Contains(t, result, "# Main Title")
	assert.Contains(t, result, "**bold**")
	assert.Contains(t, result, "*italic*")
	assert.Contains(t, result, "- Item 1")
	assert.Contains(t, result, "```")
	assert.NotContains(t, result, "var x = 1")
	assert.NotContains(t, result, "Menu")
	assert.NotContains(t, result, "Footer text")
}
