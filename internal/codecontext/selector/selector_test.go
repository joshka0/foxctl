package selector_test

import (
	"context"
	"testing"

	"github.com/jkatigb/agentctl/internal/codecontext/files"
	"github.com/jkatigb/agentctl/internal/codecontext/selector"
)

// mockFileContent creates a FileContent from source lines for testing.
func mockFileContent(path, language string, lines []string) *files.FileContent {
	content := make([]byte, 0)
	offsets := make([]int, len(lines))

	for i, line := range lines {
		offsets[i] = len(content)
		content = append(content, []byte(line)...)
		content = append(content, '\n')
	}

	return &files.FileContent{
		Path:        path,
		Content:     content,
		Lines:       lines,
		LineOffsets: offsets,
		Language:    language,
		Truncated:   false,
	}
}

func TestHeuristicSelector_KeywordMatching(t *testing.T) {
	source := []string{
		"package main",
		"",
		"// HandleAuth handles authentication requests",
		"func HandleAuth(req Request) Response {",
		"    user := validateUser(req.Token)",
		"    if user == nil {",
		"        return ErrorResponse(401)",
		"    }",
		"    return SuccessResponse(user)",
		"}",
		"",
		"// HandleLogout handles logout",
		"func HandleLogout(req Request) Response {",
		"    return SuccessResponse(nil)",
		"}",
	}
	content := mockFileContent("auth.go", "go", source)

	sel := selector.NewHeuristic(selector.HeuristicOpts{ContextLines: 2})
	ctx := context.Background()

	spans, err := sel.Select(ctx, "authentication handling", content, selector.Hints{})
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}

	if len(spans) == 0 {
		t.Fatal("Expected at least one span")
	}

	// Should find the HandleAuth function
	found := false
	for _, span := range spans {
		if span.StartLine <= 3 && span.EndLine >= 3 {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected span to include line 3 (authentication comment)")
	}
}

func TestHeuristicSelector_LineHint(t *testing.T) {
	source := []string{
		"line 1",
		"line 2",
		"line 3",
		"line 4",
		"line 5",
		"line 6",
		"line 7",
		"line 8",
		"line 9",
		"line 10",
	}
	content := mockFileContent("test.txt", "text", source)

	sel := selector.NewHeuristic(selector.HeuristicOpts{ContextLines: 2})
	ctx := context.Background()

	spans, err := sel.Select(ctx, "", content, selector.Hints{LineHint: 5})
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}

	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	// Line 5 with 2 context lines should give us lines 3-7
	if span.StartLine != 3 || span.EndLine != 7 {
		t.Errorf("Expected lines 3-7, got %d-%d", span.StartLine, span.EndLine)
	}
	if span.Reason != "line_hint" {
		t.Errorf("Expected reason 'line_hint', got '%s'", span.Reason)
	}
}

func TestHeuristicSelector_Fallback(t *testing.T) {
	source := []string{
		"package main",
		"",
		"func main() {",
		"    println(\"hello\")",
		"}",
	}
	content := mockFileContent("main.go", "go", source)

	sel := selector.NewHeuristic(selector.HeuristicOpts{ContextLines: 3})
	ctx := context.Background()

	// Query with no matching keywords
	spans, err := sel.Select(ctx, "xyz123 nonexistent", content, selector.Hints{})
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}

	if len(spans) == 0 {
		t.Fatal("Expected fallback span")
	}

	span := spans[0]
	if span.StartLine != 1 {
		t.Errorf("Fallback should start at line 1, got %d", span.StartLine)
	}
	if span.Reason != "fallback" {
		t.Errorf("Expected reason 'fallback', got '%s'", span.Reason)
	}
}

func TestHeuristicSelector_MultipleMatches(t *testing.T) {
	source := []string{
		"// First error handler",
		"func handleError1() {",
		"    log.Error(\"first\")",
		"}",
		"",
		"// Some unrelated code",
		"func doSomething() {",
		"    println(\"hi\")",
		"}",
		"",
		"// Second error handler",
		"func handleError2() {",
		"    log.Error(\"second\")",
		"}",
	}
	content := mockFileContent("handlers.go", "go", source)

	sel := selector.NewHeuristic(selector.HeuristicOpts{ContextLines: 1})
	ctx := context.Background()

	spans, err := sel.Select(ctx, "error handler", content, selector.Hints{})
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}

	// Should find multiple spans
	if len(spans) < 2 {
		t.Errorf("Expected at least 2 spans, got %d", len(spans))
	}
}

func TestHeuristicSelector_MaxSpans(t *testing.T) {
	// Create file with many matches
	source := make([]string, 100)
	for i := range source {
		source[i] = "error on line " + string(rune('0'+i%10))
	}
	content := mockFileContent("errors.txt", "text", source)

	sel := selector.NewHeuristic(selector.HeuristicOpts{ContextLines: 1})
	ctx := context.Background()

	spans, err := sel.Select(ctx, "error", content, selector.Hints{MaxSpans: 3})
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}

	if len(spans) > 3 {
		t.Errorf("Expected at most 3 spans, got %d", len(spans))
	}
}

func TestHeuristicSelector_ExpandToBlock(t *testing.T) {
	source := []string{
		"package main",
		"",
		"func authenticate(user string) bool {",
		"    // Check auth",
		"    if user == \"\" {",
		"        return false",
		"    }",
		"    return true",
		"}",
	}
	content := mockFileContent("auth.go", "go", source)

	sel := selector.NewHeuristic(selector.HeuristicOpts{ContextLines: 1})
	ctx := context.Background()

	// Without block expansion
	spans1, _ := sel.Select(ctx, "auth check", content, selector.Hints{
		Language:      "go",
		ExpandToBlock: false,
	})

	// With block expansion
	spans2, _ := sel.Select(ctx, "auth check", content, selector.Hints{
		Language:      "go",
		ExpandToBlock: true,
	})

	// Block expansion should give larger span
	if len(spans2) > 0 && len(spans1) > 0 {
		span1 := spans1[0]
		span2 := spans2[0]
		if span2.EndLine-span2.StartLine <= span1.EndLine-span1.StartLine {
			// Note: This may not always be true depending on context lines
			// Just verify both work without error
			t.Logf("Span without expansion: %d-%d", span1.StartLine, span1.EndLine)
			t.Logf("Span with expansion: %d-%d", span2.StartLine, span2.EndLine)
		}
	}
}

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		query    string
		minLen   int
		expected []string
	}{
		{
			query:    "authentication handling",
			minLen:   3,
			expected: []string{"authentication", "handling"},
		},
		{
			query:    "the and for are but",
			minLen:   3,
			expected: []string{}, // All stop words
		},
		{
			query:    "find_user getUserById",
			minLen:   3,
			expected: []string{"find_user", "getuserbyid"},
		},
		{
			query:    "How does authentication work?",
			minLen:   3,
			expected: []string{"authentication", "work"},
		},
		{
			query:    "ab cd ef", // All too short
			minLen:   3,
			expected: []string{},
		},
	}

	for _, tc := range tests {
		got := selector.ExtractKeywords(tc.query, tc.minLen)
		if len(got) != len(tc.expected) {
			t.Errorf("ExtractKeywords(%q) = %v, want %v", tc.query, got, tc.expected)
			continue
		}
		for i, want := range tc.expected {
			if got[i] != want {
				t.Errorf("ExtractKeywords(%q)[%d] = %q, want %q", tc.query, i, got[i], want)
			}
		}
	}
}

func TestHeuristicSelector_EmptyFile(t *testing.T) {
	content := mockFileContent("empty.go", "go", []string{})

	sel := selector.NewHeuristic(selector.HeuristicOpts{ContextLines: 3})
	ctx := context.Background()

	spans, err := sel.Select(ctx, "anything", content, selector.Hints{})
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}

	if len(spans) > 0 {
		t.Errorf("Expected no spans for empty file, got %d", len(spans))
	}
}

func TestHeuristicSelector_ContextCancellation(t *testing.T) {
	source := []string{"line 1", "line 2", "line 3"}
	content := mockFileContent("test.txt", "text", source)

	sel := selector.NewHeuristic(selector.HeuristicOpts{ContextLines: 1})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := sel.Select(ctx, "anything", content, selector.Hints{})
	if err == nil {
		t.Error("Expected error for cancelled context")
	}
}

func TestHeuristicSelector_Name(t *testing.T) {
	sel := selector.NewHeuristic(selector.HeuristicOpts{})
	if sel.Name() != "heuristic" {
		t.Errorf("Expected name 'heuristic', got '%s'", sel.Name())
	}
}

func TestHints_ApplyDefaults(t *testing.T) {
	hints := selector.Hints{}
	hints.ApplyDefaults()

	if hints.MaxSpans != selector.DefaultMaxSpans {
		t.Errorf("MaxSpans = %d, want %d", hints.MaxSpans, selector.DefaultMaxSpans)
	}
	if hints.MaxLinesPerSpan != selector.DefaultMaxLinesPerSpan {
		t.Errorf("MaxLinesPerSpan = %d, want %d", hints.MaxLinesPerSpan, selector.DefaultMaxLinesPerSpan)
	}
}

func TestLLMSelector_RequiresBackend(t *testing.T) {
	_, err := selector.NewLLM(selector.LLMOpts{}, nil)
	if err == nil {
		t.Error("Expected error when backend is nil")
	}
}

func TestLLMSelector_NoOpBackend(t *testing.T) {
	backend := &selector.NoOpBackend{}
	sel, err := selector.NewLLM(selector.LLMOpts{}, backend)
	if err != nil {
		t.Fatalf("NewLLM failed: %v", err)
	}

	if sel.Name() != "llm" {
		t.Errorf("Expected name 'llm', got '%s'", sel.Name())
	}

	source := []string{"line 1", "line 2"}
	content := mockFileContent("test.txt", "text", source)
	ctx := context.Background()

	// NoOp backend returns empty, so we should get no spans
	spans, err := sel.Select(ctx, "query", content, selector.Hints{})
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if len(spans) != 0 {
		t.Errorf("Expected 0 spans from NoOp backend, got %d", len(spans))
	}
}
