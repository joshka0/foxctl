// Package fsutil provides file system utilities for skills and indexers.
// It consolidates common helpers like language detection and path manipulation.
package fsutil

import (
	"path/filepath"
	"strings"
)

// DetectLanguage returns the language identifier based on file extension.
// Returns "text" for unknown extensions.
func DetectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".jsx":
		return "javascript"
	case ".tsx":
		return "typescript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".hpp", ".cxx":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".swift":
		return "swift"
	case ".kt", ".kts":
		return "kotlin"
	case ".cs":
		return "csharp"
	case ".scala":
		return "scala"
	case ".md", ".markdown":
		return "markdown"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".xml":
		return "xml"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".scss", ".sass":
		return "scss"
	case ".sql":
		return "sql"
	case ".sh", ".bash", ".zsh":
		return "shell"
	case ".ps1":
		return "powershell"
	case ".lua":
		return "lua"
	case ".r":
		return "r"
	case ".ex", ".exs":
		return "elixir"
	case ".erl", ".hrl":
		return "erlang"
	case ".hs":
		return "haskell"
	case ".ml", ".mli":
		return "ocaml"
	case ".clj", ".cljs":
		return "clojure"
	case ".vim":
		return "vim"
	case ".proto":
		return "protobuf"
	case ".graphql", ".gql":
		return "graphql"
	case ".tf", ".tfvars":
		return "terraform"
	case ".dockerfile":
		return "dockerfile"
	default:
		// Check for special filenames
		name := strings.ToLower(filepath.Base(path))
		if name == "dockerfile" || strings.HasPrefix(name, "dockerfile.") {
			return "dockerfile"
		}
		if name == "makefile" || name == "gnumakefile" {
			return "makefile"
		}
		return "text"
	}
}

// DetectLanguageWithHint returns the language based on hint or extension.
// If hint is not "auto" or empty, returns the hint directly.
// Otherwise falls back to extension-based detection.
func DetectLanguageWithHint(hint, path string) string {
	if hint != "" && hint != "auto" {
		return hint
	}
	return DetectLanguage(path)
}

// RelativeTo returns the path relative to base, or the original path if
// it cannot be made relative (e.g., different drive on Windows).
// Always uses forward slashes for consistency.
func RelativeTo(base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// IsCommonExclude returns true if the directory name should be excluded
// from file traversal (e.g., .git, node_modules, vendor).
func IsCommonExclude(name string) bool {
	excludes := map[string]bool{
		".git":         true,
		".svn":         true,
		".hg":          true,
		"node_modules": true,
		"vendor":       true,
		"__pycache__":  true,
		".venv":        true,
		"venv":         true,
		"dist":         true,
		"build":        true,
		"target":       true,
		".idea":        true,
		".vscode":      true,
		".cache":       true,
		"coverage":     true,
		".next":        true,
		".nuxt":        true,
		"out":          true,
		".terraform":   true,
		".gradle":      true,
		"bin":          true,
		"obj":          true,
	}
	return excludes[name]
}

// IsTestFile returns true if the filename indicates a test file.
func IsTestFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, "_test.go") ||
		strings.HasSuffix(lower, "_test.py") ||
		strings.HasSuffix(lower, ".test.js") ||
		strings.HasSuffix(lower, ".test.ts") ||
		strings.HasSuffix(lower, ".test.jsx") ||
		strings.HasSuffix(lower, ".test.tsx") ||
		strings.HasSuffix(lower, ".spec.js") ||
		strings.HasSuffix(lower, ".spec.ts") ||
		strings.HasSuffix(lower, ".spec.jsx") ||
		strings.HasSuffix(lower, ".spec.tsx") ||
		strings.Contains(lower, "_test.") ||
		strings.Contains(lower, ".test.") ||
		strings.Contains(lower, ".spec.")
}

// IsBinaryFile returns true if the file extension suggests binary content.
func IsBinaryFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	binaries := map[string]bool{
		".exe": true, ".dll": true, ".so": true, ".dylib": true, ".a": true,
		".o": true, ".obj": true, ".bin": true,
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".ico": true,
		".webp": true, ".bmp": true, ".tiff": true, ".svg": true,
		".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true,
		".rar": true, ".7z": true,
		".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
		".ppt": true, ".pptx": true,
		".mp3": true, ".mp4": true, ".avi": true, ".mov": true, ".wav": true,
		".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true,
		".sqlite": true, ".db": true,
		".pyc": true, ".class": true,
	}
	return binaries[ext]
}
