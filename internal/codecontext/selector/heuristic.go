package selector

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"github.com/jkatigb/agentctl/internal/codecontext/expander"
	"github.com/jkatigb/agentctl/internal/codecontext/files"
)

// HeuristicSelector selects code spans using keyword matching.
// It extracts keywords from the query, finds matching lines,
// and groups them into contiguous blocks with context.
type HeuristicSelector struct {
	opts HeuristicOpts
}

// HeuristicOpts configures the heuristic selector.
type HeuristicOpts struct {
	// ContextLines is the number of lines to include before/after matches.
	// Default: 3
	ContextLines int

	// MinKeywordLen is the minimum length for a keyword to be considered.
	// Default: 3
	MinKeywordLen int
}

// NewHeuristic creates a new heuristic selector.
func NewHeuristic(opts HeuristicOpts) *HeuristicSelector {
	if opts.ContextLines <= 0 {
		opts.ContextLines = DefaultContextLines
	}
	if opts.MinKeywordLen <= 0 {
		opts.MinKeywordLen = 3
	}
	return &HeuristicSelector{opts: opts}
}

func (s *HeuristicSelector) Name() string {
	return "heuristic"
}

func (s *HeuristicSelector) Select(ctx context.Context, query string, content *files.FileContent, hints Hints) ([]Span, error) {
	hints.ApplyDefaults()

	// Check context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	lines := content.Lines
	if len(lines) == 0 {
		return nil, nil
	}

	// Strategy 1: If we have a specific line hint, select around it
	if hints.LineHint > 0 {
		return s.selectAroundLine(content, hints)
	}

	// Strategy 2: If we have a symbol ID, try to find the symbol
	if hints.SymbolID != "" {
		spans, err := s.selectBySymbol(content, hints)
		if err == nil && len(spans) > 0 {
			return spans, nil
		}
		// Fall through to keyword search if symbol not found
	}

	// Strategy 3: Extract and search for keywords
	keywords := hints.Keywords
	if len(keywords) == 0 {
		keywords = ExtractKeywords(query, s.opts.MinKeywordLen)
	}

	if len(keywords) > 0 {
		spans := s.selectByKeywords(content, keywords, hints)
		if len(spans) > 0 {
			return spans, nil
		}
	}

	// Strategy 4: Fallback - return the beginning of the file
	return s.selectFallback(content, hints), nil
}

// selectAroundLine selects a span centered on a specific line.
func (s *HeuristicSelector) selectAroundLine(content *files.FileContent, hints Hints) ([]Span, error) {
	totalLines := content.LineCount()
	if hints.LineHint < 1 || hints.LineHint > totalLines {
		return nil, &SelectorError{
			Selector: "heuristic",
			Message:  "line hint out of range",
		}
	}

	startLine := hints.LineHint - s.opts.ContextLines
	if startLine < 1 {
		startLine = 1
	}

	endLine := hints.LineHint + s.opts.ContextLines
	if endLine > totalLines {
		endLine = totalLines
	}

	// Optionally expand to full block
	if hints.ExpandToBlock && hints.Language != "" {
		if exp := expander.GetOrGeneric(hints.Language); exp != nil {
			if blockStart, blockEnd, _, err := exp.FindBlock(content, hints.LineHint); err == nil {
				startLine = blockStart
				endLine = blockEnd
			}
		}
	}

	// Cap at max lines
	if endLine-startLine+1 > hints.MaxLinesPerSpan {
		endLine = startLine + hints.MaxLinesPerSpan - 1
	}

	return []Span{{
		StartLine: startLine,
		EndLine:   endLine,
		Reason:    "line_hint",
		Priority:  1.0,
	}}, nil
}

// selectBySymbol finds a named symbol in the file.
func (s *HeuristicSelector) selectBySymbol(content *files.FileContent, hints Hints) ([]Span, error) {
	if hints.Language == "" {
		hints.Language = content.Language
	}

	exp := expander.GetOrGeneric(hints.Language)
	if exp == nil {
		return nil, nil
	}

	symbolName := extractSymbolName(hints.SymbolID)
	if symbolName == "" {
		return nil, nil
	}

	startLine, endLine, err := exp.ExpandToSymbol(content, symbolName)
	if err != nil {
		return nil, nil // Symbol not found, fall through to keywords
	}

	// Cap at max lines
	if endLine-startLine+1 > hints.MaxLinesPerSpan {
		endLine = startLine + hints.MaxLinesPerSpan - 1
	}

	return []Span{{
		StartLine: startLine,
		EndLine:   endLine,
		Reason:    "symbol_match",
		Priority:  1.0,
		SymbolID:  hints.SymbolID,
	}}, nil
}

// selectByKeywords finds lines matching keywords and groups them into spans.
func (s *HeuristicSelector) selectByKeywords(content *files.FileContent, keywords []string, hints Hints) []Span {
	lines := content.Lines

	// Find matching lines (0-indexed)
	matchingLines := findMatchingLines(lines, keywords)
	if len(matchingLines) == 0 {
		return nil
	}

	// Group into blocks
	blocks := groupIntoBlocks(matchingLines, len(lines), s.opts.ContextLines, hints.MaxLinesPerSpan)

	// Convert blocks to spans
	spans := make([]Span, 0, len(blocks))
	for _, block := range blocks {
		span := Span{
			StartLine:  block.start + 1, // Convert to 1-indexed
			EndLine:    block.end + 1,
			Reason:     "keyword_match",
			MatchCount: block.matchCount,
			Priority:   float64(block.matchCount) / float64(len(keywords)),
		}

		// Optionally expand to block boundaries
		if hints.ExpandToBlock && hints.Language != "" {
			if exp := expander.GetOrGeneric(hints.Language); exp != nil {
				// Find enclosing block for the first matched line
				midLine := (block.start + block.end) / 2
				if blockStart, blockEnd, symbol, err := exp.FindBlock(content, midLine+1); err == nil {
					span.StartLine = blockStart
					span.EndLine = blockEnd
					span.SymbolID = symbol
					// Cap at max lines
					if span.EndLine-span.StartLine+1 > hints.MaxLinesPerSpan {
						span.EndLine = span.StartLine + hints.MaxLinesPerSpan - 1
					}
				}
			}
		}

		spans = append(spans, span)
	}

	// Sort by priority (highest first)
	sort.Slice(spans, func(i, j int) bool {
		return spans[i].Priority > spans[j].Priority
	})

	// Limit to max spans
	if len(spans) > hints.MaxSpans {
		spans = spans[:hints.MaxSpans]
	}

	return spans
}

// selectFallback returns the beginning of the file when no matches are found.
func (s *HeuristicSelector) selectFallback(content *files.FileContent, hints Hints) []Span {
	totalLines := content.LineCount()
	if totalLines == 0 {
		return nil
	}

	endLine := hints.MaxLinesPerSpan
	if endLine > totalLines {
		endLine = totalLines
	}

	return []Span{{
		StartLine: 1,
		EndLine:   endLine,
		Reason:    "fallback",
		Priority:  0.1,
	}}
}

// lineBlock represents a contiguous range of lines.
type lineBlock struct {
	start      int // 0-indexed
	end        int // 0-indexed, inclusive
	matchCount int
}

// findMatchingLines returns 0-indexed line numbers that contain any keyword.
func findMatchingLines(lines []string, keywords []string) []int {
	if len(keywords) == 0 {
		return nil
	}

	matching := make([]int, 0)
	for i, line := range lines {
		lower := strings.ToLower(line)
		for _, kw := range keywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				matching = append(matching, i)
				break
			}
		}
	}
	return matching
}

// groupIntoBlocks groups matching line indices into contiguous blocks with context.
func groupIntoBlocks(matchingLines []int, totalLines, contextLines, maxLinesPerSpan int) []lineBlock {
	if len(matchingLines) == 0 {
		return nil
	}

	blocks := make([]lineBlock, 0)
	var current *lineBlock

	for _, lineIdx := range matchingLines {
		// Calculate block boundaries with context
		start := lineIdx - contextLines
		if start < 0 {
			start = 0
		}
		end := lineIdx + contextLines
		if end >= totalLines {
			end = totalLines - 1
		}

		if current == nil {
			current = &lineBlock{start: start, end: end, matchCount: 1}
		} else if start <= current.end+1 {
			// Merge overlapping or adjacent blocks
			if end > current.end {
				current.end = end
			}
			current.matchCount++

			// Cap at maxLinesPerSpan and finalize
			if current.end-current.start+1 > maxLinesPerSpan {
				current.end = current.start + maxLinesPerSpan - 1
				blocks = append(blocks, *current)
				current = nil
			}
		} else {
			// Start a new block
			blocks = append(blocks, *current)
			current = &lineBlock{start: start, end: end, matchCount: 1}
		}
	}

	if current != nil {
		blocks = append(blocks, *current)
	}

	return blocks
}

// ExtractKeywords extracts meaningful keywords from a query.
// It lowercases, splits on non-alphanumeric, and filters short/stop words.
func ExtractKeywords(query string, minLen int) []string {
	if minLen <= 0 {
		minLen = 3
	}

	// Common stop words to filter out
	stopWords := map[string]bool{
		"the": true, "and": true, "for": true, "are": true, "but": true,
		"not": true, "you": true, "all": true, "can": true, "had": true,
		"her": true, "was": true, "one": true, "our": true, "out": true,
		"has": true, "have": true, "been": true, "this": true, "that": true,
		"what": true, "when": true, "where": true, "which": true, "who": true,
		"how": true, "does": true, "from": true, "with": true, "into": true,
		"about": true, "would": true, "could": true, "should": true,
		"there": true, "their": true, "they": true, "will": true, "just": true,
		"also": true, "than": true, "then": true, "only": true, "more": true,
		"some": true, "such": true, "like": true, "very": true, "most": true,
	}

	lower := strings.ToLower(query)

	// Split on non-alphanumeric characters
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})

	keywords := make([]string, 0, len(words))
	seen := make(map[string]bool)

	for _, w := range words {
		if len(w) < minLen {
			continue
		}
		if stopWords[w] {
			continue
		}
		if seen[w] {
			continue
		}
		seen[w] = true
		keywords = append(keywords, w)
	}

	return keywords
}

// extractSymbolName extracts the symbol name from a SymbolID.
// Format is typically "file.go:FunctionName" or just "FunctionName".
func extractSymbolName(symbolID string) string {
	if symbolID == "" {
		return ""
	}
	if idx := strings.LastIndex(symbolID, ":"); idx >= 0 {
		return symbolID[idx+1:]
	}
	return symbolID
}
