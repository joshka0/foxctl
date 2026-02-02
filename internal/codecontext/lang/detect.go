package lang

import (
	"github.com/jkatigb/agentctl/internal/platform/fsutil"
)

// DetectLanguage returns the programming language based on file extension.
// Returns "text" for unknown extensions.
//
// This is a re-export of fsutil.DetectLanguage for convenience.
// All language detection should go through this function (or fsutil.DetectLanguage)
// to ensure consistency across the codebase.
func DetectLanguage(path string) string {
	return fsutil.DetectLanguage(path)
}

// DetectLanguageWithHint returns the language based on hint or extension.
// If hint is not "auto" or empty, returns the hint directly.
// Otherwise falls back to extension-based detection.
//
// This is a re-export of fsutil.DetectLanguageWithHint.
func DetectLanguageWithHint(hint, path string) string {
	return fsutil.DetectLanguageWithHint(hint, path)
}

// IsBinaryFile returns true if the file extension suggests binary content.
// Used to skip binary files during code extraction.
//
// This is a re-export of fsutil.IsBinaryFile.
func IsBinaryFile(path string) bool {
	return fsutil.IsBinaryFile(path)
}

// IsTestFile returns true if the filename indicates a test file.
//
// This is a re-export of fsutil.IsTestFile.
func IsTestFile(name string) bool {
	return fsutil.IsTestFile(name)
}

// IsCommonExclude returns true if the directory name should be excluded
// from file traversal (e.g., .git, node_modules, vendor).
//
// This is a re-export of fsutil.IsCommonExclude.
func IsCommonExclude(name string) bool {
	return fsutil.IsCommonExclude(name)
}

// CommentMarkers contains language-specific comment syntax.
type CommentMarkers struct {
	// Line is the single-line comment prefix (e.g., "//", "#").
	Line string

	// BlockStart is the multi-line comment start (e.g., "/*").
	BlockStart string

	// BlockEnd is the multi-line comment end (e.g., "*/").
	BlockEnd string
}

// GetCommentMarkers returns the comment syntax for a language.
// Returns C-style markers ("//", "/*", "*/") as default.
func GetCommentMarkers(language string) CommentMarkers {
	switch language {
	case "python", "ruby", "shell", "yaml", "makefile", "r", "perl":
		return CommentMarkers{Line: "#"}
	case "lua":
		return CommentMarkers{Line: "--", BlockStart: "--[[", BlockEnd: "]]"}
	case "sql":
		return CommentMarkers{Line: "--", BlockStart: "/*", BlockEnd: "*/"}
	case "html", "xml":
		return CommentMarkers{BlockStart: "<!--", BlockEnd: "-->"}
	case "css", "scss":
		return CommentMarkers{BlockStart: "/*", BlockEnd: "*/"}
	case "haskell":
		return CommentMarkers{Line: "--", BlockStart: "{-", BlockEnd: "-}"}
	case "ocaml":
		return CommentMarkers{BlockStart: "(*", BlockEnd: "*)"}
	case "erlang", "elixir":
		return CommentMarkers{Line: "%"}
	case "vim":
		return CommentMarkers{Line: "\""}
	case "clojure":
		return CommentMarkers{Line: ";"}
	default:
		// C-style: Go, JavaScript, TypeScript, Java, C, C++, Rust, Swift, Kotlin, etc.
		return CommentMarkers{Line: "//", BlockStart: "/*", BlockEnd: "*/"}
	}
}

// SupportedLanguages returns the list of languages with specialized support.
// Languages not in this list fall back to generic handling.
var SupportedLanguages = []string{
	"go",
	"python",
	"javascript",
	"typescript",
	"java",
	"c",
	"cpp",
	"rust",
	"ruby",
	"php",
	"swift",
	"kotlin",
	"csharp",
	"scala",
	"gdscript",
}
