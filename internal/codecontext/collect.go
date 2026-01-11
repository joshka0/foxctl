package codecontext

import (
	"context"
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/codecontext/files"
)

// CollectOpts configures the evidence collection process.
type CollectOpts struct {
	// Candidates are the files/symbols to process.
	Candidates []Candidate

	// Query is the question or search context (used for relevance ranking).
	Query string

	// PathValidator validates file paths against workspace boundaries.
	// Required - collection will fail without a validator.
	PathValidator files.PathValidator

	// MaxFiles limits the number of files to process.
	// Default: 50
	MaxFiles int

	// MaxSnippets limits the total number of snippets to extract.
	// Default: 100
	MaxSnippets int

	// MaxBytesPerFile limits bytes read per file.
	// Default: 64KB
	MaxBytesPerFile int

	// ContextLines is the number of lines to include before/after matches.
	// Default: 3
	ContextLines int

	// Mode determines how snippets are extracted.
	// Default: ModeSnippets
	Mode RenderMode
}

// Collect gathers code evidence from the given candidates.
//
// This is the main entry point for code context extraction. It:
//  1. Validates and reads files safely (TOCTOU-protected)
//  2. Extracts relevant snippets based on the query and mode
//  3. Returns structured evidence for downstream processing
//
// Skills should use this function rather than implementing their own
// file reading and extraction logic.
func Collect(ctx context.Context, opts CollectOpts) (*Evidence, error) {
	// Apply defaults
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = DefaultMaxFiles
	}
	if opts.MaxSnippets <= 0 {
		opts.MaxSnippets = DefaultMaxSnippets
	}
	if opts.MaxBytesPerFile <= 0 {
		opts.MaxBytesPerFile = DefaultMaxBytesPerFile
	}
	if opts.ContextLines <= 0 {
		opts.ContextLines = DefaultContextLines
	}
	if opts.Mode == "" {
		opts.Mode = ModeSnippets
	}

	// Validate we have a path validator
	if opts.PathValidator == nil {
		return nil, &CollectError{Message: "path validator is required"}
	}

	// Sort candidates by priority (highest first)
	sortedCandidates := make([]Candidate, len(opts.Candidates))
	copy(sortedCandidates, opts.Candidates)
	sort.Slice(sortedCandidates, func(i, j int) bool {
		return sortedCandidates[i].Priority > sortedCandidates[j].Priority
	})

	// Create safe reader
	reader := files.NewSafeReader(opts.PathValidator, opts.MaxBytesPerFile)

	// Process files and collect evidence
	evidence := &Evidence{
		Query:    opts.Query,
		Snippets: make([]Snippet, 0),
		Stats: EvidenceStats{
			FileErrors: make([]FileError, 0),
		},
	}

	// Deduplicate paths
	seenPaths := make(map[string]bool)
	filesProcessed := 0

	for _, candidate := range sortedCandidates {
		// Check context cancellation
		if ctx.Err() != nil {
			break
		}

		// Skip empty paths
		if candidate.Path == "" {
			continue
		}

		// Skip duplicates
		if seenPaths[candidate.Path] {
			continue
		}
		seenPaths[candidate.Path] = true

		// Respect file limit
		if filesProcessed >= opts.MaxFiles {
			evidence.Truncated = true
			break
		}

		// Read file
		content, err := reader.Read(ctx, candidate.Path)
		if err != nil {
			if readErr, ok := err.(*files.ReadError); ok {
				evidence.Stats.FileErrors = append(evidence.Stats.FileErrors, FileError{
					Path:    candidate.Path,
					Code:    readErr.Code,
					Message: readErr.Message,
				})
			} else {
				evidence.Stats.FileErrors = append(evidence.Stats.FileErrors, FileError{
					Path:    candidate.Path,
					Code:    "EIO",
					Message: err.Error(),
				})
			}
			evidence.Stats.FilesSkipped++
			continue
		}

		filesProcessed++
		evidence.Stats.FilesProcessed++
		evidence.Stats.TotalBytes += int64(len(content.Content))

		// Extract snippets based on mode
		snippets := extractSnippets(content, candidate, opts)

		// Add snippets up to limit
		for _, snippet := range snippets {
			if len(evidence.Snippets) >= opts.MaxSnippets {
				evidence.Truncated = true
				break
			}
			evidence.Snippets = append(evidence.Snippets, snippet)
		}

		if len(evidence.Snippets) >= opts.MaxSnippets {
			evidence.Truncated = true
			break
		}
	}

	evidence.Stats.SnippetsExtracted = len(evidence.Snippets)

	return evidence, nil
}

// extractSnippets extracts relevant code snippets from file content.
// The extraction strategy depends on the candidate hints and query.
func extractSnippets(content *files.FileContent, candidate Candidate, opts CollectOpts) []Snippet {
	snippets := make([]Snippet, 0)

	// Strategy 1: If we have a specific line hint, extract around it
	if candidate.LineHint > 0 {
		snippet := extractAroundLine(content, candidate.LineHint, opts.ContextLines)
		snippet.Priority = candidate.Priority
		snippet.SymbolID = candidate.SymbolID
		snippet.Reason = "line_hint"
		snippets = append(snippets, snippet)
		return snippets
	}

	// Strategy 2: If we have a symbol ID, try to find it
	if candidate.SymbolID != "" {
		// For now, do a simple text search for the symbol name
		// TODO: Use proper symbol extraction (expander package in Phase 2)
		symbolName := extractSymbolName(candidate.SymbolID)
		if symbolName != "" {
			found := findSymbolSnippets(content, symbolName, opts.ContextLines)
			for i := range found {
				found[i].Priority = candidate.Priority
				found[i].SymbolID = candidate.SymbolID
				found[i].Reason = "symbol_match"
			}
			snippets = append(snippets, found...)
		}
		if len(snippets) > 0 {
			return snippets
		}
	}

	// Strategy 3: If we have a query, find keyword matches
	if opts.Query != "" {
		keywords := extractKeywords(opts.Query)
		if len(keywords) > 0 {
			found := findKeywordSnippets(content, keywords, opts.ContextLines)
			for i := range found {
				found[i].Priority = candidate.Priority
				found[i].Reason = "keyword_match"
			}
			snippets = append(snippets, found...)
		}
		if len(snippets) > 0 {
			return snippets
		}
	}

	// Strategy 4: Fallback - return the beginning of the file
	snippet := extractAroundLine(content, 1, opts.ContextLines*3)
	snippet.Priority = candidate.Priority
	snippet.Reason = "file_start"
	snippets = append(snippets, snippet)

	return snippets
}

// extractAroundLine extracts a snippet centered around a line number.
func extractAroundLine(content *files.FileContent, lineNum, contextLines int) Snippet {
	totalLines := content.LineCount()
	if totalLines == 0 {
		return Snippet{
			File:      content.Path,
			StartLine: 1,
			EndLine:   1,
			Text:      "",
			Language:  content.Language,
		}
	}

	startLine := lineNum - contextLines
	if startLine < 1 {
		startLine = 1
	}

	endLine := lineNum + contextLines
	if endLine > totalLines {
		endLine = totalLines
	}

	lines := content.GetLines(startLine, endLine)
	text := strings.Join(lines, "\n")

	return Snippet{
		File:      content.Path,
		StartLine: startLine,
		EndLine:   endLine,
		Text:      text,
		Language:  content.Language,
	}
}

// extractSymbolName extracts the symbol name from a SymbolID.
// Format is typically "file.go:FunctionName" or just "FunctionName".
func extractSymbolName(symbolID string) string {
	if idx := strings.LastIndex(symbolID, ":"); idx >= 0 {
		return symbolID[idx+1:]
	}
	return symbolID
}

// findSymbolSnippets finds snippets containing a symbol definition.
func findSymbolSnippets(content *files.FileContent, symbolName string, contextLines int) []Snippet {
	var snippets []Snippet

	for lineIdx, line := range content.Lines {
		lineNum := lineIdx + 1

		// Simple heuristic: look for function/type definitions containing the symbol name
		// This is a placeholder - Phase 2 will add proper AST-based extraction
		if containsDefinition(line, symbolName) {
			snippet := extractAroundLine(content, lineNum, contextLines)
			snippets = append(snippets, snippet)
		}
	}

	return snippets
}

// containsDefinition checks if a line contains a definition of the symbol.
func containsDefinition(line, symbolName string) bool {
	// Simple check for common definition patterns
	// TODO: Use language-specific patterns from expander package
	patterns := []string{
		"func " + symbolName,
		"func (" + symbolName, // method receiver
		"type " + symbolName,
		"const " + symbolName,
		"var " + symbolName,
		"def " + symbolName,    // Python
		"class " + symbolName,  // Python/JS
		"function " + symbolName, // JS
	}

	for _, pattern := range patterns {
		if strings.Contains(line, pattern) {
			return true
		}
	}

	return false
}

// findKeywordSnippets finds snippets containing query keywords.
func findKeywordSnippets(content *files.FileContent, keywords []string, contextLines int) []Snippet {
	var snippets []Snippet
	matchedLines := make(map[int]bool)

	// Find lines containing keywords
	for lineIdx, line := range content.Lines {
		lineLower := strings.ToLower(line)
		for _, keyword := range keywords {
			if strings.Contains(lineLower, strings.ToLower(keyword)) {
				matchedLines[lineIdx+1] = true
				break
			}
		}
	}

	// Group adjacent matches into blocks
	sortedLines := make([]int, 0, len(matchedLines))
	for lineNum := range matchedLines {
		sortedLines = append(sortedLines, lineNum)
	}
	sort.Ints(sortedLines)

	// Merge nearby matches
	blocks := mergeIntoBlocks(sortedLines, contextLines)

	// Create snippets from blocks
	for _, block := range blocks {
		startLine := block[0] - contextLines
		if startLine < 1 {
			startLine = 1
		}
		endLine := block[len(block)-1] + contextLines
		if endLine > content.LineCount() {
			endLine = content.LineCount()
		}

		lines := content.GetLines(startLine, endLine)
		snippets = append(snippets, Snippet{
			File:      content.Path,
			StartLine: startLine,
			EndLine:   endLine,
			Text:      strings.Join(lines, "\n"),
			Language:  content.Language,
		})
	}

	return snippets
}

// mergeIntoBlocks groups line numbers into contiguous blocks.
func mergeIntoBlocks(lines []int, gapThreshold int) [][]int {
	if len(lines) == 0 {
		return nil
	}

	var blocks [][]int
	currentBlock := []int{lines[0]}

	for i := 1; i < len(lines); i++ {
		if lines[i]-currentBlock[len(currentBlock)-1] <= gapThreshold*2 {
			currentBlock = append(currentBlock, lines[i])
		} else {
			blocks = append(blocks, currentBlock)
			currentBlock = []int{lines[i]}
		}
	}
	blocks = append(blocks, currentBlock)

	return blocks
}

// extractKeywords extracts searchable keywords from a query.
func extractKeywords(query string) []string {
	// Simple tokenization - split on whitespace and filter
	words := strings.Fields(query)
	keywords := make([]string, 0, len(words))

	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "must": true, "shall": true,
		"to": true, "of": true, "in": true, "for": true, "on": true,
		"with": true, "at": true, "by": true, "from": true, "as": true,
		"into": true, "through": true, "during": true, "before": true, "after": true,
		"above": true, "below": true, "between": true, "under": true,
		"again": true, "further": true, "then": true, "once": true,
		"here": true, "there": true, "when": true, "where": true, "why": true,
		"how": true, "all": true, "each": true, "few": true, "more": true,
		"most": true, "other": true, "some": true, "such": true, "no": true,
		"nor": true, "not": true, "only": true, "own": true, "same": true,
		"so": true, "than": true, "too": true, "very": true, "can": true,
		"just": true, "what": true, "which": true, "who": true, "this": true,
		"that": true, "these": true, "those": true, "and": true, "but": true,
		"if": true, "or": true, "because": true, "until": true, "while": true,
	}

	for _, word := range words {
		// Clean the word
		word = strings.ToLower(word)
		word = strings.Trim(word, ".,!?;:'\"()[]{}") // Remove punctuation

		// Skip short words and stop words
		if len(word) < 3 {
			continue
		}
		if stopWords[word] {
			continue
		}

		keywords = append(keywords, word)
	}

	return keywords
}

// CollectError represents an error during evidence collection.
type CollectError struct {
	Message string
}

func (e *CollectError) Error() string {
	return e.Message
}
