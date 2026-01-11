// Package main implements the code/stats skill.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

const command = "code/stats"

type input struct {
	Path         string `json:"path"`
	BreakdownBy  string `json:"breakdown_by"`
	IncludeTests bool   `json:"include_tests"`
	MaxDepth     int    `json:"max_depth"`
}

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

type fileStats struct {
	Path      string `json:"path"`
	Lines     int    `json:"lines"`
	CodeLines int    `json:"code_lines"`
	Language  string `json:"language"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Apply defaults
	if in.BreakdownBy == "" {
		in.BreakdownBy = "language"
	}

	// Resolve workspace and search path
	workspace := rc.PathValidator.Workspace()
	searchPath := workspace
	if in.Path != "" {
		validated, err := rc.PathValidator.ValidatePath(in.Path)
		if err != nil {
			return fmt.Errorf("path validation failed: %w", err)
		}
		searchPath = validated
	}

	// Collect statistics
	breakdown := make(map[string]*breakdownItem)
	languages := make(map[string]int)
	var allFiles []fileStats
	totalStats := &stats{
		Languages: languages,
	}

	err := filepath.WalkDir(searchPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		// Check depth
		if in.MaxDepth > 0 {
			depth := strings.Count(relativeTo(searchPath, path), "/")
			if depth > in.MaxDepth {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Skip hidden and common excludes
		if strings.HasPrefix(d.Name(), ".") || isCommonExclude(d.Name()) {
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
		if !in.IncludeTests && isTestFile(d.Name()) {
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
			key = filepath.Dir(relativeTo(searchPath, path))
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
			Path:      relativeTo(workspace, path),
			Lines:     lineCount,
			CodeLines: codeLines,
			Language:  lang,
		})

		return nil
	})
	if err != nil {
		return fmt.Errorf("directory walk failed: %w", err)
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
	artifact, err := persistStatsArtifact(ctx, rc, totalStats, truncated)
	if err != nil {
		return err
	}

	data := map[string]any{
		"statistics":   preview,
		"breakdown_by": in.BreakdownBy,
	}
	if artifact.Digest != "" {
		data["artifact"] = artifact.Digest
	}

	return skillout.Emit(rc, command, data)
}


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

type commentStyle struct {
	line       []string
	blockStart string
	blockEnd   string
}

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

func isTestFile(name string) bool {
	name = strings.ToLower(name)
	return strings.HasSuffix(name, "_test.go") ||
		strings.HasSuffix(name, "_test.py") ||
		strings.HasSuffix(name, ".test.js") ||
		strings.HasSuffix(name, ".test.ts") ||
		strings.HasSuffix(name, ".spec.js") ||
		strings.HasSuffix(name, ".spec.ts") ||
		strings.Contains(name, "test_")
}

func isCommonExclude(name string) bool {
	excludes := []string{
		".git", ".svn", ".hg",
		"node_modules", "vendor", "__pycache__",
		".venv", "venv", ".tox",
		"dist", "build", "target",
	}
	for _, exclude := range excludes {
		if name == exclude {
			return true
		}
	}
	return false
}

func relativeTo(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return filepath.ToSlash(target)
	}
	if strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(target)
	}
	return filepath.ToSlash(rel)
}

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

func persistStatsArtifact(ctx context.Context, rc *skillmain.RunContext, s *stats, truncated bool) (skillmain.Artifact, error) {
	if !truncated {
		return skillmain.Artifact{}, nil
	}
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(s); err != nil {
		return skillmain.Artifact{}, fmt.Errorf("encode stats: %w", err)
	}
	return skillmain.PersistBuffer(ctx, rc, buf, "application/json", "code_stats")
}
