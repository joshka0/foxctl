// Package main implements the code/stats skill.
package main

import (
	"bufio"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/fsutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/pathutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
)

const command = "code/stats"

// input is the expected JSON input for code/stats operations.
type input struct {
	Path         string `json:"path"`
	BreakdownBy  string `json:"breakdown_by"`
	IncludeTests bool   `json:"include_tests"`
	MaxDepth     int    `json:"max_depth"`
}

// stats contains aggregated code statistics with breakdown and language information.
type stats struct {
	TotalFiles      int             `json:"total_files"`
	TotalLines      int             `json:"total_lines"`
	TotalCodeLines  int             `json:"total_code_lines"`
	TotalBlankLines int             `json:"total_blank_lines"`
	TotalComments   int             `json:"total_comments"`
	TotalBytes      int64           `json:"total_bytes"`
	Breakdown       []breakdownItem `json:"breakdown"`
	Languages       map[string]int  `json:"languages"`
	TopFiles        []fileStats     `json:"top_files,omitempty"`
}

// breakdownItem represents statistics for a single breakdown category.
type breakdownItem struct {
	Name       string  `json:"name"`
	FileCount  int     `json:"file_count"`
	Lines      int     `json:"lines"`
	CodeLines  int     `json:"code_lines"`
	BlankLines int     `json:"blank_lines"`
	Comments   int     `json:"comments"`
	Bytes      int64   `json:"bytes"`
	Percentage float64 `json:"percentage"`
}

// fileStats represents statistics for a single file.
type fileStats struct {
	Path      string `json:"path"`
	Lines     int    `json:"lines"`
	CodeLines int    `json:"code_lines"`
	Language  string `json:"language"`
}

// main is the skill entry point for code/stats.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates code statistics collection with configurable breakdown options.
//
// Index:
// - Purpose: Collect and analyze code statistics including lines, files, languages, and breakdown metrics
// - Flow: resolve path → walk directory tree → count lines per file → aggregate by breakdown category → calculate percentages → persist if needed
// - SideEffects: file system reads; directory traversal; artifact persistence; CAS hint generation
// - FailureModes: invalid paths, file read errors, directory traversal errors, large file skips
// - Observability: emits statistics/breakdown_by/cas_hint/artifact with detailed metrics
// - Related: detectLanguage, countLines, preparePreview
// - Keywords: code/stats, statistics, lines, languages, breakdown, aggregation
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Apply defaults
	if in.BreakdownBy == "" {
		in.BreakdownBy = "language"
	}

	// Resolve workspace and search path
	workspace, searchPath, err := skillmain.ResolvePath(rc, in.Path)
	if err != nil {
		return err
	}

	// Collect statistics
	breakdown := make(map[string]*breakdownItem)
	languages := make(map[string]int)
	var allFiles []fileStats
	totalStats := &stats{
		Languages: languages,
	}

	err = filepath.WalkDir(searchPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}

		// Check depth
		if in.MaxDepth > 0 {
			depth := strings.Count(pathutil.RelTo(searchPath, path), "/")
			if depth > in.MaxDepth {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Skip hidden and common excludes
		if fsutil.ShouldSkipHiddenOrCommon(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip non-files
		if d.IsDir() {
			return nil
		}

		// Skip test files if requested
		if !in.IncludeTests && fsutil.IsTestFile(d.Name()) {
			return nil
		}

		// Get file info
		info, err := d.Info()
		if err != nil {
			return nil
		}

		// Skip large files (>5MB)
		if info.Size() > 5*1024*1024 {
			return nil
		}

		// Detect language
		ext := filepath.Ext(d.Name())
		lang := detectLanguage(ext, d.Name())
		if lang == "unknown" {
			return nil // Skip unknown file types
		}

		// Count lines
		lineCount, codeLines, blankLines, comments, err := countLines(path, lang)
		if err != nil {
			return nil
		}

		// Update totals
		totalStats.TotalFiles++
		totalStats.TotalLines += lineCount
		totalStats.TotalCodeLines += codeLines
		totalStats.TotalBlankLines += blankLines
		totalStats.TotalComments += comments
		totalStats.TotalBytes += info.Size()

		// Track by breakdown category
		var key string
		switch in.BreakdownBy {
		case "language":
			key = lang
		case "directory":
			key = filepath.Dir(pathutil.RelTo(searchPath, path))
			if key == "." {
				key = "(root)"
			}
		case "extension":
			key = ext
			if key == "" {
				key = "(no extension)"
			}
		}

		if breakdown[key] == nil {
			breakdown[key] = &breakdownItem{Name: key}
		}
		breakdown[key].FileCount++
		breakdown[key].Lines += lineCount
		breakdown[key].CodeLines += codeLines
		breakdown[key].BlankLines += blankLines
		breakdown[key].Comments += comments
		breakdown[key].Bytes += info.Size()

		// Track language
		languages[lang]++

		// Track top files
		allFiles = append(allFiles, fileStats{
			Path:      pathutil.RelTo(workspace, path),
			Lines:     lineCount,
			CodeLines: codeLines,
			Language:  lang,
		})

		return nil
	})
	if err != nil {
		return skillerr.WrapIO("directory walk failed", err)
	}

	// Convert breakdown to array and calculate percentages
	for _, item := range breakdown {
		if totalStats.TotalLines > 0 {
			item.Percentage = float64(item.Lines) / float64(totalStats.TotalLines) * 100
		}
		totalStats.Breakdown = append(totalStats.Breakdown, *item)
	}

	// Sort breakdown by lines descending
	sort.Slice(totalStats.Breakdown, func(i, j int) bool {
		return totalStats.Breakdown[i].Lines > totalStats.Breakdown[j].Lines
	})

	// Get top 10 files by lines
	sort.Slice(allFiles, func(i, j int) bool {
		return allFiles[i].Lines > allFiles[j].Lines
	})
	if len(allFiles) > 10 {
		allFiles = allFiles[:10]
	}
	totalStats.TopFiles = allFiles

	// Prepare response
	preview, truncated := preparePreview(totalStats, rc.MaxPreview)
	var artifact skillmain.Artifact
	var casHint *envelope.CASHint
	if truncated {
		artifact, casHint, err = skillout.PersistJSONWithHint(ctx, rc, totalStats, "code_stats", skillout.DefaultCASHintLines)
		if err != nil {
			return skillerr.WrapIO("persist stats artifact", err)
		}
	}

	data := map[string]any{
		"statistics":   preview,
		"breakdown_by": in.BreakdownBy,
	}
	skillout.AddArtifact(data, &artifact)
	if casHint != nil {
		data["cas_hint"] = casHint
	}

	return skillout.Emit(rc, command, data)
}

// detectLanguage determines the programming language from file extension and name.
func detectLanguage(ext, name string) string {
	langMap := map[string]string{
		".go":    "Go",
		".py":    "Python",
		".js":    "JavaScript",
		".ts":    "TypeScript",
		".jsx":   "JavaScript",
		".tsx":   "TypeScript",
		".java":  "Java",
		".c":     "C",
		".cpp":   "C++",
		".cc":    "C++",
		".cxx":   "C++",
		".h":     "C/C++ Header",
		".hpp":   "C++ Header",
		".rs":    "Rust",
		".rb":    "Ruby",
		".php":   "PHP",
		".sh":    "Shell",
		".bash":  "Shell",
		".zsh":   "Shell",
		".yaml":  "YAML",
		".yml":   "YAML",
		".json":  "JSON",
		".xml":   "XML",
		".html":  "HTML",
		".css":   "CSS",
		".scss":  "SCSS",
		".md":    "Markdown",
		".sql":   "SQL",
		".proto": "Protobuf",
		".toml":  "TOML",
	}

	if lang, ok := langMap[ext]; ok {
		return lang
	}

	// Check for special filenames
	switch name {
	case "Makefile", "Dockerfile", "Vagrantfile":
		return "Configuration"
	}

	return "unknown"
}

// countLines counts total, code, blank, and comment lines in a file.
func countLines(path, lang string) (total, code, blank, comments int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	defer func() {
		errs.Ignore(f.Close(), "close file for line counting")
	}()

	scanner := bufio.NewScanner(f)
	inBlockComment := false
	commentPrefixes := getCommentPrefixes(lang)

	for scanner.Scan() {
		total++
		line := strings.TrimSpace(scanner.Text())

		// Blank line
		if line == "" {
			blank++
			continue
		}

		// Block comments
		if commentPrefixes.blockStart != "" && commentPrefixes.blockEnd != "" {
			if strings.Contains(line, commentPrefixes.blockStart) {
				inBlockComment = true
				comments++
				continue
			}
			if inBlockComment {
				comments++
				if strings.Contains(line, commentPrefixes.blockEnd) {
					inBlockComment = false
				}
				continue
			}
		}

		// Line comments
		isComment := false
		for _, prefix := range commentPrefixes.line {
			if strings.HasPrefix(line, prefix) {
				isComment = true
				break
			}
		}

		if isComment {
			comments++
		} else {
			code++
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, 0, 0, 0, err
	}

	return total, code, blank, comments, nil
}

// commentStyle defines comment prefixes and block delimiters for a language.
type commentStyle struct {
	line       []string
	blockStart string
	blockEnd   string
}

// getCommentPrefixes returns comment style configuration for a given language.
func getCommentPrefixes(lang string) commentStyle {
	switch lang {
	case "Go", "C", "C++", "C/C++ Header", "C++ Header", "Java", "JavaScript", "TypeScript", "Rust", "PHP":
		return commentStyle{line: []string{"//"}, blockStart: "/*", blockEnd: "*/"}
	case "Python", "Ruby", "Shell", "YAML", "TOML":
		return commentStyle{line: []string{"#"}}
	case "HTML", "XML":
		return commentStyle{blockStart: "<!--", blockEnd: "-->"}
	case "SQL":
		return commentStyle{line: []string{"--", "#"}, blockStart: "/*", blockEnd: "*/"}
	default:
		return commentStyle{line: []string{"#"}}
	}
}

// preparePreview creates a truncated preview of stats for inline output.
func preparePreview(s *stats, limit int) (*stats, bool) {
	// For stats, keep the full struct; return a truncated view for preview only.
	if limit <= 0 || len(s.Breakdown) <= limit {
		return s, false
	}
	cp := *s
	cp.Breakdown = make([]breakdownItem, limit)
	copy(cp.Breakdown, s.Breakdown[:limit])
	return &cp, true
}
