package fsutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

var extensionLanguages = map[string]string{
	".go":         "go",
	".py":         "python",
	".pyw":        "python",
	".pyi":        "python",
	".js":         "javascript",
	".mjs":        "javascript",
	".cjs":        "javascript",
	".ts":         "typescript",
	".mts":        "typescript",
	".cts":        "typescript",
	".jsx":        "javascript",
	".tsx":        "typescript",
	".rs":         "rust",
	".java":       "java",
	".c":          "c",
	".h":          "c",
	".cpp":        "cpp",
	".cc":         "cpp",
	".hpp":        "cpp",
	".cxx":        "cpp",
	".rb":         "ruby",
	".php":        "php",
	".swift":      "swift",
	".kt":         "kotlin",
	".kts":        "kotlin",
	".cs":         "csharp",
	".scala":      "scala",
	".md":         "markdown",
	".markdown":   "markdown",
	".json":       "json",
	".yaml":       "yaml",
	".yml":        "yaml",
	".toml":       "toml",
	".xml":        "xml",
	".html":       "html",
	".htm":        "html",
	".css":        "css",
	".scss":       "scss",
	".sass":       "scss",
	".sql":        "sql",
	".sh":         "shell",
	".bash":       "shell",
	".zsh":        "shell",
	".ps1":        "powershell",
	".lua":        "lua",
	".gd":         "gdscript",
	".r":          "r",
	".ex":         "elixir",
	".exs":        "elixir",
	".erl":        "erlang",
	".hrl":        "erlang",
	".hs":         "haskell",
	".ml":         "ocaml",
	".mli":        "ocaml",
	".clj":        "clojure",
	".cljs":       "clojure",
	".vim":        "vim",
	".proto":      "protobuf",
	".graphql":    "graphql",
	".gql":        "graphql",
	".tf":         "terraform",
	".tfvars":     "terraform",
	".dockerfile": "dockerfile",
}

var specialFilenameLanguages = map[string]string{
	"dockerfile":  "dockerfile",
	"makefile":    "makefile",
	"gnumakefile": "makefile",
}

var commonExcludeNames = map[string]struct{}{
	".git":         {},
	".svn":         {},
	".hg":          {},
	"node_modules": {},
	"vendor":       {},
	"__pycache__":  {},
	".venv":        {},
	"venv":         {},
	"dist":         {},
	"build":        {},
	"target":       {},
	".idea":        {},
	".vscode":      {},
	".cache":       {},
	"coverage":     {},
	".next":        {},
	".nuxt":        {},
	"out":          {},
	".terraform":   {},
	".gradle":      {},
	"bin":          {},
	"obj":          {},
}

var binaryExtensions = map[string]struct{}{
	".exe":    {},
	".dll":    {},
	".so":     {},
	".dylib":  {},
	".a":      {},
	".o":      {},
	".obj":    {},
	".bin":    {},
	".png":    {},
	".jpg":    {},
	".jpeg":   {},
	".gif":    {},
	".ico":    {},
	".webp":   {},
	".bmp":    {},
	".tiff":   {},
	".svg":    {},
	".zip":    {},
	".tar":    {},
	".gz":     {},
	".bz2":    {},
	".xz":     {},
	".rar":    {},
	".7z":     {},
	".pdf":    {},
	".doc":    {},
	".docx":   {},
	".xls":    {},
	".xlsx":   {},
	".ppt":    {},
	".pptx":   {},
	".mp3":    {},
	".mp4":    {},
	".avi":    {},
	".mov":    {},
	".wav":    {},
	".woff":   {},
	".woff2":  {},
	".ttf":    {},
	".otf":    {},
	".eot":    {},
	".sqlite": {},
	".db":     {},
	".pyc":    {},
	".class":  {},
}

// DetectLanguage returns the language identifier based on file extension.
// Returns "text" for unknown extensions.
func DetectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if lang, ok := extensionLanguages[ext]; ok {
		return lang
	}

	name := strings.ToLower(filepath.Base(path))
	if lang, ok := specialFilenameLanguages[name]; ok {
		return lang
	}
	if strings.HasPrefix(name, "dockerfile.") {
		return "dockerfile"
	}
	return "text"
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
	_, ok := commonExcludeNames[name]
	return ok
}

// IsTestFile returns true if the filename indicates a test file.
func IsTestFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, "_test.go") ||
		strings.HasSuffix(lower, "_test.py") ||
		(strings.HasPrefix(lower, "test_") && strings.HasSuffix(lower, ".py")) ||
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
	_, ok := binaryExtensions[ext]
	return ok
}

// FindFilesMatchingGlob finds files in root matching a glob pattern, excluding files matching exclude patterns.
// Uses filepath.Walk for traversal and doublestar for pattern matching.
func FindFilesMatchingGlob(root, pattern string, excludePatterns []string) ([]string, error) {
	var files []string

	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("walk %s: %w", path, err)
		}
		if info.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("rel %s: %w", path, err)
		}

		relSlash := filepath.ToSlash(rel)

		// Check include pattern
		matched, err := doublestar.Match(pattern, relSlash)
		if err != nil {
			return fmt.Errorf("match pattern %q for %q: %w", pattern, rel, err)
		}
		if !matched {
			return nil
		}

		// Check exclude patterns
		for _, excl := range excludePatterns {
			excluded, err := doublestar.Match(excl, relSlash)
			if err != nil {
				return fmt.Errorf("match exclude pattern %q for %q: %w", excl, rel, err)
			}
			if excluded {
				return nil
			}
		}

		files = append(files, rel)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	return files, nil
}

// FindFilesRespectingGitignore finds files matching a glob pattern while respecting .gitignore.
// It uses `git ls-files` to get tracked files, then filters by the glob pattern.
// Falls back to FindFilesMatchingGlob if not in a git repository.
func FindFilesRespectingGitignore(root, pattern string, excludePatterns []string) ([]string, error) {
	// Check if we're in a git repo
	gitCmd := exec.Command("git", "-C", root, "rev-parse", "--git-dir")
	if err := gitCmd.Run(); err != nil {
		// Not a git repo, fall back to regular glob
		return FindFilesMatchingGlob(root, pattern, excludePatterns)
	}

	// Use git ls-files to get all tracked + untracked (but not ignored) files
	// --cached: tracked files
	// --others: untracked files
	// --exclude-standard: respect .gitignore
	cmd := exec.Command("git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard")
	output, err := cmd.Output()
	if err != nil {
		// Git command failed, fall back to regular glob
		return FindFilesMatchingGlob(root, pattern, excludePatterns)
	}

	var files []string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Normalize to forward slashes for pattern matching
		relSlash := filepath.ToSlash(line)

		// Check include pattern
		matched, err := doublestar.Match(pattern, relSlash)
		if err != nil {
			return nil, fmt.Errorf("match pattern %q for %q: %w", pattern, line, err)
		}
		if !matched {
			continue
		}

		// Check exclude patterns
		excluded := false
		for _, excl := range excludePatterns {
			ex, err := doublestar.Match(excl, relSlash)
			if err != nil {
				return nil, fmt.Errorf("match exclude pattern %q for %q: %w", excl, line, err)
			}
			if ex {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		// Normalize to OS-native path separators and canonical form
		normalized := filepath.Clean(filepath.FromSlash(line))
		files = append(files, normalized)
	}

	return files, nil
}
