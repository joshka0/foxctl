// Package main implements the hooks/impact_analysis skill.
// After editing a file, it shows what other code depends on the edited symbols
// using LSP servers for references and implementations.
//
// Features:
//   - Parallel LSP calls for faster analysis
//   - Debouncing to avoid spam on rapid edits
//   - Support for Go, Python, TypeScript/JavaScript
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/hookutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/hooks"
	"github.com/jkatigb/agentctl/internal/hooks/pathutil"
	"github.com/jkatigb/agentctl/internal/lsp/gopls"
)

const (
	defaultMaxSymbols = 3
	defaultMaxRefs    = 5
	defaultTimeout    = 45 * time.Second
	debounceCooldown  = 10 * time.Second
)

// Environment variable names for impact analysis configuration.
// FC/IS: Constants ensure consistency between LoadConfig and ConfigFromMap.
const (
	EnvImpactDisabled   = "AGENTCTL_IMPACT_DISABLED"
	EnvImpactMaxSymbols = "AGENTCTL_IMPACT_MAX_SYMBOLS"
	EnvImpactMaxRefs    = "AGENTCTL_IMPACT_MAX_REFS"
	EnvImpactTimeout    = "AGENTCTL_IMPACT_TIMEOUT"
	EnvAgentctlBin      = "AGENTCTL_BIN"
)

// Config holds impact analysis configuration.
type Config struct {
	MaxSymbols int           `json:"max_symbols"`
	MaxRefs    int           `json:"max_refs"`
	Timeout    time.Duration `json:"timeout,format:units"`
	Disabled   bool          `json:"disabled"`
}

// LoadConfig loads configuration from environment.
// FC/IS: Collects env values at boundary, delegates parsing to ConfigFromMap.
func LoadConfig() Config {
	envMap := map[string]string{
		EnvImpactDisabled:   os.Getenv(EnvImpactDisabled),
		EnvImpactMaxSymbols: os.Getenv(EnvImpactMaxSymbols),
		EnvImpactMaxRefs:    os.Getenv(EnvImpactMaxRefs),
		EnvImpactTimeout:    os.Getenv(EnvImpactTimeout),
	}
	return ConfigFromMap(envMap)
}

// ConfigFromMap creates a Config from a string map.
// FC/IS: Pure function for parsing - no os.Getenv calls.
// Tests can call this directly with controlled values.
func ConfigFromMap(envMap map[string]string) Config {
	cfg := Config{
		MaxSymbols: defaultMaxSymbols,
		MaxRefs:    defaultMaxRefs,
		Timeout:    defaultTimeout,
		Disabled:   envMap[EnvImpactDisabled] == "1",
	}

	if v := envMap[EnvImpactMaxSymbols]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxSymbols = n
		}
	}
	if v := envMap[EnvImpactMaxRefs]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxRefs = n
		}
	}
	if v := envMap[EnvImpactTimeout]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Timeout = time.Duration(n) * time.Second
		}
	}

	return cfg
}

// Symbol represents a code symbol.
type Symbol struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Line    int    `json:"line"`
	EndLine int    `json:"end_line,omitempty"`
}

// Impact represents the impact analysis result for a symbol.
type Impact struct {
	Symbol     string   `json:"symbol"`
	SymbolType string   `json:"symbol_type"`
	RefCount   int      `json:"ref_count"`
	RefFiles   []string `json:"ref_files"`
	ImplCount  int      `json:"impl_count,omitempty"`
	ImplFiles  []string `json:"impl_files,omitempty"`
}

// Language represents a supported language with its LSP skill.
type Language struct {
	Name     string
	Skill    string
	IsPublic func(name string) bool
}

var languages = map[string]Language{
	".go": {
		Name:  "go",
		Skill: "lsp/gopls",
		IsPublic: func(name string) bool {
			if len(name) == 0 {
				return false
			}
			return unicode.IsUpper(rune(name[0]))
		},
	},
	".py": {
		Name:  "python",
		Skill: "lsp/pylsp",
		IsPublic: func(name string) bool {
			return !strings.HasPrefix(name, "_")
		},
	},
	".ts": {
		Name:  "typescript",
		Skill: "lsp/tsserver",
		IsPublic: func(name string) bool {
			return !strings.HasPrefix(name, "_")
		},
	},
	".tsx": {
		Name:  "typescript",
		Skill: "lsp/tsserver",
		IsPublic: func(name string) bool {
			return !strings.HasPrefix(name, "_")
		},
	},
	".js": {
		Name:  "javascript",
		Skill: "lsp/tsserver",
		IsPublic: func(name string) bool {
			return !strings.HasPrefix(name, "_")
		},
	},
	".jsx": {
		Name:  "javascript",
		Skill: "lsp/tsserver",
		IsPublic: func(name string) bool {
			return !strings.HasPrefix(name, "_")
		},
	},
}

func main() {
	skillmain.Main("hooks/impact_analysis", run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in hooks.Input) error {
	cfg := LoadConfig()

	// Check if disabled
	if cfg.Disabled {
		return emitNone(rc, "disabled via AGENTCTL_IMPACT_DISABLED")
	}

	// Extract file path from tool input using cross-platform path extraction
	// Checks file_path, path, file, current_path fields
	filePath := pathutil.ExtractPath(in.ToolInput)
	if filePath == "" {
		return emitNone(rc, "no file path")
	}

	// Check language support
	ext := strings.ToLower(filepath.Ext(filePath))
	lang, ok := languages[ext]
	if !ok {
		return emitNone(rc, "unsupported language")
	}

	// Skip test files using cross-platform detection
	if pathutil.IsTestFile(filePath) {
		return emitNone(rc, "test file")
	}

	// Check if file exists
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return emitNone(rc, "invalid path")
	}
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return emitNone(rc, "file not found")
	}

	// Debounce check
	if shouldDebounce(absPath) {
		return emitNone(rc, "debounced")
	}

	// Get workspace root using detection chain (hook input takes priority)
	workspace := hookutil.ResolveWorkspaceRoot(in, absPath)

	// For Go files, skip if gopls isn't warm (avoid 30-40s cold start).
	// Users get impact analysis "for free" after using any LSP feature.
	if lang.Name == "go" && !gopls.IsDaemonReady(workspace) {
		return emitNone(rc, "gopls not warm")
	}

	// Get symbols from file
	symbols, err := getSymbols(ctx, absPath, cfg.MaxSymbols, workspace)
	if err != nil || len(symbols) == 0 {
		return emitNone(rc, "no symbols found")
	}

	// Filter to public symbols of analyzable types
	var publicSymbols []Symbol
	for _, s := range symbols {
		if !lang.IsPublic(s.Name) {
			continue
		}
		switch s.Type {
		case "function", "method", "type", "struct", "interface", "class":
			publicSymbols = append(publicSymbols, s)
		}
	}

	if len(publicSymbols) == 0 {
		return emitNone(rc, "no public symbols")
	}

	// Limit symbols
	if len(publicSymbols) > cfg.MaxSymbols {
		publicSymbols = publicSymbols[:cfg.MaxSymbols]
	}

	// Analyze impacts in parallel
	impacts := analyzeImpacts(ctx, absPath, publicSymbols, lang, cfg, workspace)

	// Filter to symbols with external references
	var significantImpacts []Impact
	for _, imp := range impacts {
		if imp.RefCount > 0 || imp.ImplCount > 0 {
			significantImpacts = append(significantImpacts, imp)
		}
	}

	if len(significantImpacts) == 0 {
		return emitNone(rc, "no external dependencies")
	}

	// Update debounce timestamp
	touchDebounce(absPath)

	// Build context message
	filename := filepath.Base(filePath)
	contextMsg := formatImpactContext(filename, significantImpacts)

	return emitApprove(rc, contextMsg, significantImpacts)
}

func getSymbols(ctx context.Context, filePath string, maxResults int, workspace string) ([]Symbol, error) {
	// Use agentctl run code/symbols
	input := map[string]any{
		"path":            filePath,
		"include_private": false,
		"max_results":     maxResults * 2, // Get extra to filter
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	extraArgs := workspaceArgs(workspace)
	var data struct {
		Preview []Symbol `json:"preview"`
	}
	_, err = executil.RunAgentctlSkillDecodeWithArgs(ctx, workspace, "code/symbols", inputJSON, extraArgs, &data)
	if err != nil {
		return nil, err
	}

	return data.Preview, nil
}

func analyzeImpacts(ctx context.Context, filePath string, symbols []Symbol, lang Language, cfg Config, workspace string) []Impact {
	var wg sync.WaitGroup
	results := make(chan Impact, len(symbols))

	for _, sym := range symbols {
		wg.Add(1)
		go func(s Symbol) {
			defer wg.Done()

			impact := Impact{
				Symbol:     s.Name,
				SymbolType: s.Type,
			}

			// Find column position for the symbol
			col := findSymbolColumn(filePath, s.Line, s.Name)

			// Get references
			refs := getLSPReferences(ctx, lang.Skill, filePath, s.Line, col, cfg.Timeout, workspace)

			// Filter to external references
			for _, ref := range refs {
				// Extract file path part (remove :line suffix)
				refFile := ref
				if idx := strings.LastIndex(ref, ":"); idx > 0 {
					refFile = ref[:idx]
				}

				// Skip self-references - LSP may return relative paths
				// So we compare:
				// 1. Direct match with original filePath
				// 2. Absolute path comparison
				// 3. Suffix match (relative path matching end of absolute)
				if isSameFile(filePath, refFile, workspace) {
					continue
				}

				// Make relative
				relPath := refFile
				if strings.HasPrefix(refFile, workspace+"/") {
					relPath = strings.TrimPrefix(refFile, workspace+"/")
				}

				// Dedupe
				found := false
				for _, existing := range impact.RefFiles {
					if existing == relPath {
						found = true
						break
					}
				}
				if !found && impact.RefCount < cfg.MaxRefs {
					impact.RefFiles = append(impact.RefFiles, relPath)
					impact.RefCount++
				}
			}

			// For interfaces (Go), also get implementations
			if lang.Name == "go" && s.Type == "interface" {
				impls := getLSPImplementations(ctx, lang.Skill, filePath, s.Line, col, cfg.Timeout, workspace)
				for _, impl := range impls {
					if impl != filePath {
						relPath := impl
						if strings.HasPrefix(impl, workspace) {
							relPath = strings.TrimPrefix(impl, workspace+"/")
						}
						if idx := strings.LastIndex(relPath, ":"); idx > 0 {
							relPath = relPath[:idx]
						}
						impact.ImplFiles = append(impact.ImplFiles, relPath)
						impact.ImplCount++
					}
				}
			}

			results <- impact
		}(sym)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var impacts []Impact
	for impact := range results {
		impacts = append(impacts, impact)
	}

	return impacts
}

func findSymbolColumn(filePath string, line int, symbolName string) int {
	f, err := os.Open(filePath)
	if err != nil {
		return 1
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	currentLine := 0
	for scanner.Scan() {
		currentLine++
		if currentLine == line {
			text := scanner.Text()
			idx := strings.Index(text, symbolName)
			if idx >= 0 {
				return idx + 1 // 1-based
			}
			return 1
		}
	}
	return 1
}

func getLSPReferences(ctx context.Context, skill, filePath string, line, col int, timeout time.Duration, workspace string) []string {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// For Go files, use the gopls daemon directly (bypasses agentctl run overhead)
	if skill == "lsp/gopls" {
		return getGoplsReferences(ctx, filePath, line, col, workspace)
	}

	// For other languages, fall back to agentctl run
	return getLSPReferencesViaAgentctl(ctx, skill, filePath, line, col, timeout, workspace)
}

// getGoplsReferences uses the gopls daemon directly for ~100x faster response times.
func getGoplsReferences(ctx context.Context, filePath string, line, col int, workspace string) []string {
	daemon, err := gopls.GetDaemon(ctx, workspace)
	if err != nil {
		return []string{}
	}

	locs, err := daemon.References(ctx, filePath, line, col)
	if err != nil {
		return []string{}
	}

	var refs []string
	for _, loc := range locs {
		refs = append(refs, fmt.Sprintf("%s:%d", loc.File, loc.Line))
	}
	return refs
}

// getLSPReferencesViaAgentctl falls back to spawning agentctl run for non-Go languages.
func getLSPReferencesViaAgentctl(ctx context.Context, skill, filePath string, line, col int, timeout time.Duration, workspace string) []string {
	// Pass timeout to skill (in seconds)
	timeoutSec := int(timeout.Seconds())
	if timeoutSec < 30 {
		timeoutSec = 30
	}

	input := map[string]any{
		"operation": "references",
		"file":      filePath,
		"line":      line,
		"column":    col,
		"timeout":   timeoutSec,
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return []string{}
	}

	extraArgs := workspaceArgs(workspace)
	var data struct {
		References []struct {
			Location struct {
				File string `json:"file"`
				Line int    `json:"line"`
			} `json:"location"`
		} `json:"references"`
	}
	_, err = executil.RunAgentctlSkillDecodeWithArgs(ctx, workspace, skill, inputJSON, extraArgs, &data)
	if err != nil {
		return []string{}
	}

	refs := make([]string, 0, len(data.References))
	for _, ref := range data.References {
		refs = append(refs, fmt.Sprintf("%s:%d", ref.Location.File, ref.Location.Line))
	}
	return refs
}

func getLSPImplementations(ctx context.Context, skill, filePath string, line, col int, timeout time.Duration, workspace string) []string {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// For Go files, use the gopls daemon directly (bypasses agentctl run overhead)
	if skill == "lsp/gopls" {
		return getGoplsImplementations(ctx, filePath, line, col, workspace)
	}

	// For other languages, fall back to agentctl run
	return getLSPImplementationsViaAgentctl(ctx, skill, filePath, line, col, timeout, workspace)
}

// getGoplsImplementations uses the gopls daemon directly for ~100x faster response times.
func getGoplsImplementations(ctx context.Context, filePath string, line, col int, workspace string) []string {
	daemon, err := gopls.GetDaemon(ctx, workspace)
	if err != nil {
		return []string{}
	}

	locs, err := daemon.Implementation(ctx, filePath, line, col)
	if err != nil {
		return []string{}
	}

	var impls []string
	for _, loc := range locs {
		impls = append(impls, fmt.Sprintf("%s:%d", loc.File, loc.Line))
	}
	return impls
}

// getLSPImplementationsViaAgentctl falls back to spawning agentctl run for non-Go languages.
func getLSPImplementationsViaAgentctl(ctx context.Context, skill, filePath string, line, col int, timeout time.Duration, workspace string) []string {
	// Pass timeout to skill (in seconds)
	timeoutSec := int(timeout.Seconds())
	if timeoutSec < 30 {
		timeoutSec = 30
	}

	input := map[string]any{
		"operation": "implementation",
		"file":      filePath,
		"line":      line,
		"column":    col,
		"timeout":   timeoutSec,
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return []string{}
	}

	extraArgs := workspaceArgs(workspace)
	var data struct {
		Implementations []struct {
			Location struct {
				File string `json:"file"`
				Line int    `json:"line"`
			} `json:"location"`
		} `json:"implementations"`
	}
	_, err = executil.RunAgentctlSkillDecodeWithArgs(ctx, workspace, skill, inputJSON, extraArgs, &data)
	if err != nil {
		return []string{}
	}

	impls := make([]string, 0, len(data.Implementations))
	for _, impl := range data.Implementations {
		impls = append(impls, fmt.Sprintf("%s:%d", impl.Location.File, impl.Location.Line))
	}
	return impls
}

func workspaceArgs(workspace string) []string {
	if workspace == "" {
		return nil
	}
	return []string{"--workspace", workspace}
}

// Debounce support using temp file timestamps.
var debounceDir = filepath.Join(os.TempDir(), "agentctl-impact-debounce")

func shouldDebounce(filePath string) bool {
	hash := hashPath(filePath)
	markerPath := filepath.Join(debounceDir, hash)

	info, err := os.Stat(markerPath)
	if err != nil {
		return false
	}

	return time.Since(info.ModTime()) < debounceCooldown
}

func touchDebounce(filePath string) {
	_ = os.MkdirAll(debounceDir, 0o755)
	hash := hashPath(filePath)
	markerPath := filepath.Join(debounceDir, hash)

	f, err := os.Create(markerPath)
	if err == nil {
		f.Close()
	}
}

func hashPath(path string) string {
	// Simple hash using path characters
	var h uint64
	for _, c := range path {
		h = h*31 + uint64(c)
	}
	return fmt.Sprintf("%x", h)
}

// isSameFile checks if refFile refers to the same file as filePath.
// Handles both absolute and relative paths from LSP.
func isSameFile(filePath, refFile, workspaceRoot string) bool {
	// Direct match
	if filePath == refFile {
		return true
	}

	// If refFile is relative, make it absolute
	if !filepath.IsAbs(refFile) {
		absRef := filepath.Join(workspaceRoot, refFile)
		if absRef == filePath {
			return true
		}
	}

	// If filePath ends with refFile (handles relative refs)
	if strings.HasSuffix(filePath, "/"+refFile) {
		return true
	}

	return false
}

func formatImpactContext(filename string, impacts []Impact) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Impact:** `%s` - external dependencies found:\n", filename))

	for _, imp := range impacts {
		sb.WriteString(fmt.Sprintf("- `%s` (%s): ", imp.Symbol, imp.SymbolType))

		var parts []string
		if imp.ImplCount > 0 {
			parts = append(parts, fmt.Sprintf("impls: %s", strings.Join(imp.ImplFiles, ", ")))
		}
		if imp.RefCount > 0 {
			parts = append(parts, fmt.Sprintf("%d refs in: %s", imp.RefCount, strings.Join(imp.RefFiles, ", ")))
		}
		sb.WriteString(strings.Join(parts, "; "))
		sb.WriteString("\n")
	}

	return sb.String()
}

func emitNone(rc *skillmain.RunContext, reason string) error {
	output := hooks.NewNone()
	output.Reason = reason
	return emitOutput(rc, output)
}

func emitApprove(rc *skillmain.RunContext, contextMsg string, impacts []Impact) error {
	output := hooks.Output{
		Decision: hooks.DecisionApprove,
		Context:  contextMsg,
		Meta: map[string]any{
			"impacts": impacts,
		},
	}
	return emitOutput(rc, output)
}

func emitOutput(rc *skillmain.RunContext, output hooks.Output) error {
	return hookutil.EmitOutput(rc, "hooks/impact_analysis", output, nil)
}
