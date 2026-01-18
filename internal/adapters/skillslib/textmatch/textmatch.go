package textmatch

import (
	"regexp"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
)

// Match represents a text search match with a preview snippet.
type Match struct {
	File    string `json:"file"`
	Line    int    `json:"line_no"`
	Text    string `json:"line"`
	Snippet string `json:"snippet"`
}

// RegexOptions configures regex compilation behavior.
type RegexOptions struct {
	CaseInsensitive bool
	WordBoundary    bool
	Multiline       bool
}

// CompileRegex builds and compiles a regex pattern with common flags.
func CompileRegex(pattern string, opts RegexOptions) (*regexp.Regexp, error) {
	// Add word boundaries if requested.
	if opts.WordBoundary && !strings.HasPrefix(pattern, `\b`) {
		pattern = `\b` + pattern
	}
	if opts.WordBoundary && !strings.HasSuffix(pattern, `\b`) {
		pattern = pattern + `\b`
	}

	// Add case-insensitive flag.
	if opts.CaseInsensitive && !strings.HasPrefix(pattern, "(?i)") {
		pattern = "(?i)" + pattern
	}

	// Add multiline/dotall flags.
	if opts.Multiline {
		if !strings.HasPrefix(pattern, "(?s)") && !strings.HasPrefix(pattern, "(?i)") {
			pattern = "(?s)" + pattern
		} else if strings.HasPrefix(pattern, "(?i)") && !strings.Contains(pattern, "(?s)") {
			pattern = strings.Replace(pattern, "(?i)", "(?is)", 1)
		}
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, skillerr.WrapValidation("invalid regex pattern", err)
	}
	return re, nil
}

// RequirePattern validates that a pattern string is present.
func RequirePattern(pattern string) error {
	if strings.TrimSpace(pattern) == "" {
		return skillerr.Arg("pattern is required", skillerr.WithHint("Provide a regex pattern to search for."))
	}
	return nil
}

// TrimLine trims a line to a maximum length, appending ellipsis if needed.
func TrimLine(line string, limit int) string {
	if len(line) <= limit {
		return line
	}
	return line[:limit] + "..."
}

// EmptySearchResult builds a standard empty search response payload.
func EmptySearchResult(pattern string, caseInsensitive bool, preview any) map[string]any {
	return map[string]any{
		"pattern":          pattern,
		"case_insensitive": caseInsensitive,
		"match_count":      0,
		"files_touched":    0,
		"preview":          preview,
		"top_files":        [][2]any{},
	}
}
