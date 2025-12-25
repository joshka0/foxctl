package plans

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

// headingRegex matches markdown headings (# to ######).
var headingRegex = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)

// stepPrefixRegex matches common step patterns like "Step 1:", "1.", "1)", "1.1 " etc.
var stepPrefixRegex = regexp.MustCompile(`^(?:Step\s+)?(\d+(?:\.\d+)?)(?:[.:)]|\s)\s*(.+)`)

// Parser handles parsing of Claude Code plan files.
type Parser struct {
	opts ParseOptions
}

// NewParser creates a parser with the given options.
func NewParser(opts ParseOptions) *Parser {
	if opts.MaxSectionDepth <= 0 {
		opts.MaxSectionDepth = 4
	}
	return &Parser{opts: opts}
}

// ParseFile reads and parses a plan file from disk.
func (p *Parser) ParseFile(path string) (*PlanInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("plans: open file: %w", err)
	}
	defer func() { errs.Ignore(f.Close(), "close plan file") }()

	// Get file info for mod time
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("plans: stat file: %w", err)
	}

	// Read content for hashing
	content, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("plans: read file: %w", err)
	}

	// Compute hash
	hash := sha256.Sum256(content)
	hashStr := "sha256:" + hex.EncodeToString(hash[:])

	// Parse the content
	plan, err := p.Parse(string(content))
	if err != nil {
		return nil, err
	}

	// Fill in file metadata
	plan.FilePath = path
	plan.FileName = filepath.Base(path)
	plan.ContentHash = hashStr
	plan.ModTime = info.ModTime()
	plan.Status = StatusActive

	return plan, nil
}

// Parse parses plan content from a string.
func (p *Parser) Parse(content string) (*PlanInfo, error) {
	plan := &PlanInfo{
		Sections: []Section{},
		Status:   StatusActive,
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNum := 0

	var currentSection *Section
	var sectionStack []*Section
	var contentBuilder strings.Builder

	flushContent := func() {
		if currentSection != nil && p.opts.IncludeContent {
			currentSection.Content = strings.TrimSpace(contentBuilder.String())
		}
		contentBuilder.Reset()
	}

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Check for heading
		matches := headingRegex.FindStringSubmatch(line)
		if matches != nil {
			flushContent()

			level := len(matches[1])
			title := strings.TrimSpace(matches[2])

			// First # heading becomes the plan title
			if level == 1 && plan.Title == "" {
				plan.Title = title
				continue
			}

			// Create new section
			section := Section{
				Level:      level,
				Title:      title,
				LineNumber: lineNum,
				Children:   []Section{},
			}

			// Only track sections up to max depth
			if level > p.opts.MaxSectionDepth+1 {
				// Treat deep headings as content
				contentBuilder.WriteString(line)
				contentBuilder.WriteString("\n")
				continue
			}

			// Find parent section
			for len(sectionStack) > 0 && sectionStack[len(sectionStack)-1].Level >= level {
				sectionStack = sectionStack[:len(sectionStack)-1]
			}

			if len(sectionStack) == 0 {
				// Top-level section
				plan.Sections = append(plan.Sections, section)
				currentSection = &plan.Sections[len(plan.Sections)-1]
			} else {
				// Nested section
				parent := sectionStack[len(sectionStack)-1]
				parent.Children = append(parent.Children, section)
				currentSection = &parent.Children[len(parent.Children)-1]
			}

			sectionStack = append(sectionStack, currentSection)
			continue
		}

		// Accumulate content
		if currentSection != nil {
			contentBuilder.WriteString(line)
			contentBuilder.WriteString("\n")
		}
	}

	// Flush final content
	flushContent()

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("plans: scan error: %w", err)
	}

	return plan, nil
}

// ExtractSteps attempts to identify actionable steps from a parsed plan.
// It looks for sections with "Step", numbered patterns, or "Phase" headings.
func (p *Parser) ExtractSteps(plan *PlanInfo) []Step {
	var steps []Step
	order := 0

	var walkSections func(sections []Section, path []string)
	walkSections = func(sections []Section, path []string) {
		for _, sec := range sections {
			// Create a new slice to avoid aliasing issues with append
			newPath := append([]string(nil), path...)
			newPath = append(newPath, sec.Title)

			// Check if this looks like a step
			if isStepSection(sec.Title) {
				order++
				steps = append(steps, Step{
					Title:       cleanStepTitle(sec.Title),
					Description: sec.Content,
					SectionPath: newPath,
					Order:       order,
				})
			}

			// Recurse into children
			walkSections(sec.Children, newPath)
		}
	}

	walkSections(plan.Sections, nil)

	// Try to infer dependencies from ordering
	// Simple heuristic: steps in the same phase depend on previous steps
	for i := 1; i < len(steps); i++ {
		if len(steps[i].SectionPath) > 1 && len(steps[i-1].SectionPath) > 1 {
			// Same parent section
			if steps[i].SectionPath[0] == steps[i-1].SectionPath[0] {
				steps[i].DependsOn = []string{steps[i-1].Title}
			}
		}
	}

	return steps
}

// isStepSection checks if a section title looks like an actionable step.
func isStepSection(title string) bool {
	lower := strings.ToLower(title)

	// Explicit step patterns
	if strings.HasPrefix(lower, "step ") {
		return true
	}

	// Numbered patterns like "1.", "1.1", "1)"
	if stepPrefixRegex.MatchString(title) {
		return true
	}

	// Task-like patterns
	taskKeywords := []string{"implement", "create", "add", "update", "fix", "remove", "refactor", "test", "configure"}
	for _, kw := range taskKeywords {
		if strings.HasPrefix(lower, kw+" ") || strings.HasPrefix(lower, kw+":") {
			return true
		}
	}

	return false
}

// cleanStepTitle removes step prefixes to get the actual task title.
func cleanStepTitle(title string) string {
	// Remove "Step N:" prefix
	if strings.HasPrefix(strings.ToLower(title), "step ") {
		if idx := strings.Index(title, ":"); idx != -1 {
			return strings.TrimSpace(title[idx+1:])
		}
		// "Step N Title" format
		parts := strings.SplitN(title, " ", 3)
		if len(parts) >= 3 {
			return strings.TrimSpace(parts[2])
		}
	}

	// Remove numbered prefix like "1. ", "1.1 ", "1) "
	matches := stepPrefixRegex.FindStringSubmatch(title)
	if len(matches) >= 3 {
		return strings.TrimSpace(matches[2])
	}

	return title
}

// Detector handles scanning for plan files.
type Detector struct {
	plansDir string
}

// NewDetector creates a detector for the given plans directory.
func NewDetector(plansDir string) *Detector {
	if plansDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		plansDir = filepath.Join(home, ".claude", "plans")
	}
	return &Detector{plansDir: plansDir}
}

// Detect finds plan files matching the given options.
func (d *Detector) Detect(opts DetectOptions) ([]PlanInfo, error) {
	// Use local variable to avoid mutating receiver state
	plansDir := d.plansDir
	if opts.PlansDir != "" {
		plansDir = opts.PlansDir
	}

	// Check if plans directory exists
	if _, err := os.Stat(plansDir); os.IsNotExist(err) {
		return []PlanInfo{}, nil
	}

	var plans []PlanInfo
	parser := NewParser(DefaultParseOptions())

	err := filepath.WalkDir(plansDir, func(path string, info os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors, continue walking
		}

		// Skip directories
		if info.IsDir() {
			// Skip archived unless requested
			if info.Name() == "archived" && !opts.IncludeArchived {
				return filepath.SkipDir
			}
			return nil
		}

		// Only process .md files
		if !strings.HasSuffix(strings.ToLower(info.Name()), ".md") {
			return nil
		}

		// Get file info for mod time check
		fileInfo, err := info.Info()
		if err != nil {
			return nil
		}

		// Filter by modification time
		if !opts.Since.IsZero() && fileInfo.ModTime().Before(opts.Since) {
			return nil
		}

		// Parse the plan
		plan, err := parser.ParseFile(path)
		if err != nil {
			return nil // Skip unparseable files
		}

		// Check if archived
		if strings.Contains(path, "/archived/") {
			plan.Status = StatusArchived
		}

		plans = append(plans, *plan)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("plans: walk dir: %w", err)
	}

	// Sort by modification time (most recent first)
	sort.Slice(plans, func(i, j int) bool {
		return plans[i].ModTime.After(plans[j].ModTime)
	})

	// Apply limit after sorting
	if opts.Limit > 0 && len(plans) > opts.Limit {
		plans = plans[:opts.Limit]
	}

	return plans, nil
}

// DetectMostRecent returns the most recently modified plan.
func (d *Detector) DetectMostRecent() (*PlanInfo, error) {
	plans, err := d.Detect(DetectOptions{Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(plans) == 0 {
		return nil, nil
	}
	return &plans[0], nil
}

// DetectModifiedSince returns plans modified after the given time.
func (d *Detector) DetectModifiedSince(since time.Time) ([]PlanInfo, error) {
	return d.Detect(DetectOptions{Since: since})
}

// GetPlansDir returns the configured plans directory.
func (d *Detector) GetPlansDir() string {
	return d.plansDir
}
