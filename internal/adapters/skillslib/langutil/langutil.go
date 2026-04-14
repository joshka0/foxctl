package langutil

import "github.com/joshka0/foxctl/internal/adapters/skillslib/fsutil"

// CommonCodeLanguages covers Go, Python, JS, TS, Elixir, and Rust.
var CommonCodeLanguages = map[string]struct{}{
	"go":         {},
	"python":     {},
	"javascript": {},
	"typescript": {},
	"elixir":     {},
	"rust":       {},
}

// SnippetLanguages covers languages supported by code_snippet_extract.
var SnippetLanguages = map[string]struct{}{
	"go":         {},
	"python":     {},
	"javascript": {},
	"typescript": {},
	"gdscript":   {},
	"rust":       {},
	"java":       {},
	"c":          {},
	"cpp":        {},
}

// DetectAllowed returns a detected language if it is in the allowed set.
func DetectAllowed(path string, allowed map[string]struct{}) string {
	lang := fsutil.DetectLanguage(path)
	if lang == "text" {
		return ""
	}
	if len(allowed) == 0 {
		return lang
	}
	if _, ok := allowed[lang]; ok {
		return lang
	}
	return ""
}

// DetectAllowedWithHint returns a language hint when provided, otherwise detects and filters.
func DetectAllowedWithHint(hint, path string, allowed map[string]struct{}) string {
	if hint != "" && hint != "auto" {
		detected := DetectAllowed(path, allowed)
		if detected == hint {
			return hint
		}
		return ""
	}
	return DetectAllowed(path, allowed)
}
