// Package main implements the lsp/gopls skill for Go language server operations via persistent gopls daemon with LSP-style support.
//
// It provides Go language server operations via a persistent gopls daemon.
// The daemon is spawned on first use and reused for subsequent requests,
// providing ~100x faster response times compared to CLI mode.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/sliceutil"
	"github.com/joshka0/foxctl/internal/platform/lsp/gopls"
)

// defaultTimeout is the maximum time to wait for gopls operations.
// Increased from 30s to 60s to accommodate cold-start and large codebases.
const defaultTimeout = 60 * time.Second

// useDaemon controls whether to use the persistent daemon or CLI mode.
// Daemon mode is faster but may have issues with some operations.
var useDaemon = os.Getenv("FOXCTL_GOPLS_CLI") != "1"

// Input defines the input parameters for lsp/gopls with LSP-style nested parameters support and comprehensive operation options.
type Input struct {
	Operation  string `json:"operation" validate:"required,oneof=definition references symbols workspace_symbol call_hierarchy implementation check"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Column     int    `json:"column"`
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
	Timeout    int    `json:"timeout"` // timeout in seconds, defaults to 60

	// LSP-style nested parameters (alternative to flat file/line/column)
	TextDocument *textDocumentParam `json:"textDocument,omitempty"`
	Position     *positionParam     `json:"position,omitempty"`
}

// textDocumentParam represents LSP-style TextDocumentIdentifier for nested parameter support.
type textDocumentParam struct {
	URI string `json:"uri"`
}

// positionParam represents LSP-style Position for nested parameter support.
type positionParam struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Location represents a file location with line and column coordinates and optional end position for ranges.
type Location struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	End    *Pos   `json:"end,omitempty"`
}

// Pos represents a position with line and column coordinates for precise location tracking.
type Pos struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Symbol represents a code symbol with name, kind, and location information including range details.
type Symbol struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	EndLine int    `json:"end_line,omitempty"`
	EndCol  int    `json:"end_column,omitempty"`
}

// Definition represents a symbol definition with location and optional text for context.
type Definition struct {
	Location Location `json:"location"`
	Text     string   `json:"text,omitempty"`
}

// Reference represents a symbol reference with location information for navigation.
type Reference struct {
	Location Location `json:"location"`
}

// CallHierarchyItem represents an item in call hierarchy with function name and location details.
type CallHierarchyItem struct {
	Function string `json:"function"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
}

// CallHierarchy represents bidirectional call relationships with callers and callees for analysis.
type CallHierarchy struct {
	Identifier string              `json:"identifier"`
	Callers    []CallHierarchyItem `json:"callers"`
	Callees    []CallHierarchyItem `json:"callees"`
}

// Diagnostic represents a diagnostic message with location and severity information for code analysis.
type Diagnostic struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// output contains the skill result data with operation-specific results and counts for comprehensive LSP operations.
type output struct {
	Operation     string         `json:"operation"`
	Definition    *Definition    `json:"definition,omitempty"`
	References    []Reference    `json:"references,omitempty"`
	Symbols       []Symbol       `json:"symbols,omitempty"`
	CallHierarchy *CallHierarchy `json:"call_hierarchy,omitempty"`
	Diagnostics   []Diagnostic   `json:"diagnostics,omitempty"`
	Count         int            `json:"count"`
}

// main is the skill entry point for lsp/gopls with comprehensive Go language server capabilities.
func main() {
	skillmain.Main("lsp/gopls", skillmain.Chain(
		run,
		skillmain.WithDynamicTimeout[Input](func(in Input) time.Duration {
			if in.Timeout > 0 {
				return time.Duration(in.Timeout) * time.Second
			}
			return defaultTimeout
		}),
		skillmain.WithRecover[Input](),
	))
}

// run orchestrates Go language server operations using either persistent daemon or CLI mode with fallback.
//
// Index:
//
//	Purpose: Provide Go language server operations (definition, references, symbols, etc.) via gopls daemon or CLI
//	Keywords: lsp/gopls, language_server, go_tools, code_navigation, symbol_search, call_hierarchy, diagnostics
//	Related: runWithDaemon, runWithCLI, normalizeInput, parseDefinition, parseReferences
//	Flow: normalize input → apply timeout → validate paths → try daemon mode → fallback to CLI mode → emit results
//	Resources: gopls daemon; gopls CLI
//	Events: gopls-definition, gopls-references, gopls-symbols
//	OutputFields: operation, definition, references, symbols, diagnostics, count
//
// [[domain:lsp-operations]]
// [[protocol:gopls-integration]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Apply defaults and normalize LSP-style parameters
	normalizeInput(&in)

	workspace := rc.PathValidator.Workspace()

	// Validate file path if provided to prevent path traversal attacks
	if in.File != "" {
		if _, err := skillmain.ValidatePath(rc, in.File, skillmain.WithPathMessage("invalid file path")); err != nil {
			return err
		}
	}

	// Try daemon mode for supported operations
	if useDaemon {
		switch in.Operation {
		case "definition", "references", "implementation":
			return runWithDaemon(ctx, rc, workspace, in)
		}
	}

	// Fall back to CLI mode for other operations or if daemon disabled
	return runWithCLI(ctx, rc, workspace, in)
}

// normalizeInput applies defaults and LSP-style parameter normalization for flat and nested input formats.
func normalizeInput(in *Input) {
	// Support LSP-style nested parameters (textDocument/position) as alternative to flat file/line/column
	if in.TextDocument != nil && in.TextDocument.URI != "" && in.File == "" {
		in.File = in.TextDocument.URI
	}
	if in.Position != nil {
		if in.Line <= 0 {
			in.Line = in.Position.Line
		}
		if in.Column <= 0 {
			in.Column = in.Position.Character
		}
	}

	// Defaults
	if in.MaxResults <= 0 {
		in.MaxResults = 50
	}
	if in.Column <= 0 {
		in.Column = 1
	}
}

// runWithDaemon uses the persistent gopls daemon for fast LSP operations with connection reuse and fallback handling.
func runWithDaemon(ctx context.Context, rc *skillmain.RunContext, workspace string, in Input) error {
	daemon, err := gopls.GetDaemon(ctx, workspace)
	if err != nil {
		// Fall back to CLI mode if daemon fails to start
		return runWithCLI(ctx, rc, workspace, in)
	}

	out := output{Operation: in.Operation}

	switch in.Operation {
	case "definition":
		locs, err := daemon.Definition(ctx, in.File, in.Line, in.Column)
		if err != nil {
			return err
		}
		if len(locs) > 0 {
			out.Definition = &Definition{
				Location: Location{
					File:   locs[0].File,
					Line:   locs[0].Line,
					Column: locs[0].Column,
				},
			}
			out.Count = 1
		}

	case "references":
		locs, err := daemon.References(ctx, in.File, in.Line, in.Column)
		if err != nil {
			return err
		}
		refs := []Reference{}
		for _, loc := range locs {
			if len(refs) >= in.MaxResults {
				break
			}
			refs = append(refs, Reference{
				Location: Location{
					File:   loc.File,
					Line:   loc.Line,
					Column: loc.Column,
				},
			})
		}
		out.References = refs
		out.Count = len(refs)

	case "implementation":
		locs, err := daemon.Implementation(ctx, in.File, in.Line, in.Column)
		if err != nil {
			return err
		}
		refs := []Reference{}
		for _, loc := range locs {
			if len(refs) >= in.MaxResults {
				break
			}
			refs = append(refs, Reference{
				Location: Location{
					File:   loc.File,
					Line:   loc.Line,
					Column: loc.Column,
				},
			})
		}
		out.References = refs
		out.Count = len(refs)
	}

	return skillout.Emit(rc, "lsp/gopls", out)
}

// runWithCLI uses the traditional gopls CLI for all operations with slower but comprehensive support and fallback reliability.
func runWithCLI(ctx context.Context, rc *skillmain.RunContext, workspace string, in Input) error {
	// Check gopls availability
	goplsPath, err := executil.RequireTool("gopls", "install gopls (go install golang.org/x/tools/gopls@latest)")
	if err != nil {
		return skillerr.Runtime(
			"gopls not found in PATH",
			skillerr.WithCause(err),
			skillerr.WithHint("Install gopls (go install golang.org/x/tools/gopls@latest)."),
		)
	}

	out := output{Operation: in.Operation}

	switch in.Operation {
	case "definition":
		def, err := runDefinition(ctx, goplsPath, workspace, in)
		if err != nil {
			return err
		}
		out.Definition = def
		out.Count = 1

	case "references":
		refs, err := runReferences(ctx, goplsPath, workspace, in)
		if err != nil {
			return err
		}
		refs = sliceutil.Limit(refs, in.MaxResults)
		out.References = refs
		out.Count = len(refs)

	case "symbols":
		syms, err := runSymbols(ctx, goplsPath, workspace, in)
		if err != nil {
			return err
		}
		syms = sliceutil.Limit(syms, in.MaxResults)
		out.Symbols = syms
		out.Count = len(syms)

	case "workspace_symbol":
		syms, err := runWorkspaceSymbol(ctx, goplsPath, workspace, in)
		if err != nil {
			return err
		}
		syms = sliceutil.Limit(syms, in.MaxResults)
		out.Symbols = syms
		out.Count = len(syms)

	case "call_hierarchy":
		ch, err := runCallHierarchy(ctx, goplsPath, workspace, in)
		if err != nil {
			return err
		}
		out.CallHierarchy = ch
		out.Count = len(ch.Callers) + len(ch.Callees)

	case "implementation":
		refs, err := runImplementation(ctx, goplsPath, workspace, in)
		if err != nil {
			return err
		}
		refs = sliceutil.Limit(refs, in.MaxResults)
		out.References = refs
		out.Count = len(refs)

	case "check":
		diags, err := runCheck(ctx, goplsPath, workspace, in)
		if err != nil {
			return err
		}
		out.Diagnostics = diags
		out.Count = len(diags)
	}

	return skillout.Emit(rc, "lsp/gopls", out)
}

// runDefinition executes gopls definition command to find symbol definitions with location parsing and error handling.
func runDefinition(ctx context.Context, goplsPath, workspace string, in Input) (*Definition, error) {
	if in.File == "" || in.Line <= 0 {
		return nil, skillerr.Arg("definition requires file and line")
	}

	filePath := resolvePath(workspace, in.File)
	pos := fmt.Sprintf("%s:%d:%d", filePath, in.Line, in.Column)

	result := executil.Run(ctx, workspace, goplsPath, "definition", pos)
	if result.Err != nil {
		return nil, skillerr.Runtimef("gopls definition failed: %v\nstderr: %s", result.Err, string(result.Stderr))
	}

	return parseDefinition(string(result.Stdout))
}

// runReferences executes gopls references command to find all symbol references with result limiting and workspace path resolution.
func runReferences(ctx context.Context, goplsPath, workspace string, in Input) ([]Reference, error) {
	if in.File == "" || in.Line <= 0 {
		return nil, skillerr.Arg("references requires file and line")
	}

	filePath := resolvePath(workspace, in.File)
	pos := fmt.Sprintf("%s:%d:%d", filePath, in.Line, in.Column)

	result := executil.Run(ctx, workspace, goplsPath, "references", pos)
	if result.Err != nil {
		return nil, skillerr.Runtimef("gopls references failed: %v\nstderr: %s", result.Err, string(result.Stderr))
	}

	return parseReferences(string(result.Stdout), workspace)
}

// runSymbols executes gopls symbols command to list document symbols with kind and position parsing for code navigation.
func runSymbols(ctx context.Context, goplsPath, workspace string, in Input) ([]Symbol, error) {
	if in.File == "" {
		return nil, skillerr.Arg("symbols requires file")
	}

	filePath := resolvePath(workspace, in.File)

	result := executil.Run(ctx, workspace, goplsPath, "symbols", filePath)
	if result.Err != nil {
		return nil, skillerr.Runtimef("gopls symbols failed: %v\nstderr: %s", result.Err, string(result.Stderr))
	}

	return parseSymbols(string(result.Stdout), workspace)
}

// runWorkspaceSymbol executes gopls workspace_symbol command to search symbols across the workspace with query matching.
func runWorkspaceSymbol(ctx context.Context, goplsPath, workspace string, in Input) ([]Symbol, error) {
	if in.Query == "" {
		return nil, skillerr.Arg("workspace_symbol requires query")
	}

	result := executil.Run(ctx, workspace, goplsPath, "workspace_symbol", in.Query)
	if result.Err != nil {
		return nil, skillerr.Runtimef("gopls workspace_symbol failed: %v\nstderr: %s", result.Err, string(result.Stderr))
	}

	return parseWorkspaceSymbols(string(result.Stdout), workspace)
}

// runCallHierarchy executes gopls call_hierarchy command to analyze bidirectional call relationships for function analysis.
func runCallHierarchy(ctx context.Context, goplsPath, workspace string, in Input) (*CallHierarchy, error) {
	if in.File == "" || in.Line <= 0 {
		return nil, skillerr.Arg("call_hierarchy requires file and line")
	}

	filePath := resolvePath(workspace, in.File)
	pos := fmt.Sprintf("%s:%d:%d", filePath, in.Line, in.Column)

	result := executil.Run(ctx, workspace, goplsPath, "call_hierarchy", pos)
	if result.Err != nil {
		return nil, skillerr.Runtimef("gopls call_hierarchy failed: %v\nstderr: %s", result.Err, string(result.Stderr))
	}

	return parseCallHierarchy(string(result.Stdout), workspace)
}

// runImplementation executes gopls implementation command to find interface implementations with location resolution.
func runImplementation(ctx context.Context, goplsPath, workspace string, in Input) ([]Reference, error) {
	if in.File == "" || in.Line <= 0 {
		return nil, skillerr.Arg("implementation requires file and line")
	}

	filePath := resolvePath(workspace, in.File)
	pos := fmt.Sprintf("%s:%d:%d", filePath, in.Line, in.Column)

	result := executil.Run(ctx, workspace, goplsPath, "implementation", pos)
	if result.Err != nil {
		return nil, skillerr.Runtimef("gopls implementation failed: %v\nstderr: %s", result.Err, string(result.Stderr))
	}

	return parseReferences(string(result.Stdout), workspace)
}

// runCheck executes gopls check command to analyze code for diagnostic issues and errors with comprehensive reporting.
func runCheck(ctx context.Context, goplsPath, workspace string, in Input) ([]Diagnostic, error) {
	if in.File == "" {
		return nil, skillerr.Arg("check requires file")
	}

	filePath := resolvePath(workspace, in.File)

	result := executil.Run(ctx, workspace, goplsPath, "check", filePath) // gopls check may exit non-zero on errors

	return parseDiagnostics(string(result.Stdout), workspace)
}

// Parsing functions

// parseDefinition parses gopls definition output to extract location and definition text with error handling.
func parseDefinition(out string) (*Definition, error) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 {
		return nil, skillerr.NotFound("no definition found")
	}

	// Format: /path/to/file.go:line:col-col: defined here as <text>
	firstLine := lines[0]

	def := &Definition{}

	// Extract location
	if idx := strings.Index(firstLine, ": defined here as "); idx > 0 {
		def.Text = strings.TrimPrefix(firstLine[idx:], ": defined here as ")
		firstLine = firstLine[:idx]
	}

	loc, err := parseLocation(firstLine)
	if err != nil {
		return nil, err
	}
	def.Location = loc

	return def, nil
}

// parseReferences parses gopls references output to extract reference locations with workspace-relative path conversion.
func parseReferences(out, workspace string) ([]Reference, error) {
	refs := []Reference{}
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		loc, err := parseLocation(line)
		if err != nil {
			continue // Skip malformed lines
		}
		loc.File = makeRelative(loc.File, workspace)
		refs = append(refs, Reference{Location: loc})
	}
	return refs, nil
}

// parseSymbols parses gopls symbols output to extract document symbols with positions and kinds for code analysis.
func parseSymbols(out, workspace string) ([]Symbol, error) {
	syms := []Symbol{}
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Format: Name Kind Line:Col-Line:Col
		sym, err := parseSymbolLine(line, workspace)
		if err != nil {
			continue
		}
		syms = append(syms, sym)
	}
	return syms, nil
}

// parseWorkspaceSymbols parses workspace symbol output with location resolution and relative path conversion.
func parseWorkspaceSymbols(out, workspace string) ([]Symbol, error) {
	syms := []Symbol{}
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Format: /path/to/file.go:line:col-col Name Kind
		sym, err := parseWorkspaceSymbolLine(line, workspace)
		if err != nil {
			continue
		}
		syms = append(syms, sym)
	}
	return syms, nil
}

// parseCallHierarchy parses gopls call_hierarchy output to extract callers and callees with locations and function names.
func parseCallHierarchy(out, workspace string) (*CallHierarchy, error) {
	ch := &CallHierarchy{
		Callers: []CallHierarchyItem{},
		Callees: []CallHierarchyItem{},
	}
	scanner := bufio.NewScanner(strings.NewReader(out))

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "identifier:") {
			// identifier: function Name in /path/to/file.go:line:col
			parts := strings.SplitN(line, " in ", 2)
			if len(parts) >= 1 {
				ch.Identifier = strings.TrimPrefix(parts[0], "identifier: ")
			}
			continue
		}

		if strings.HasPrefix(line, "caller[") {
			item := parseCallHierarchyItem(line, workspace)
			if item != nil {
				ch.Callers = append(ch.Callers, *item)
			}
		} else if strings.HasPrefix(line, "callee[") {
			item := parseCallHierarchyItem(line, workspace)
			if item != nil {
				ch.Callees = append(ch.Callees, *item)
			}
		}
		// Note: Lines with "from/to function" are already parsed via parseCallHierarchyItem
	}

	return ch, nil
}

// parseCallHierarchyItem parses individual call hierarchy items with function names and locations for relationship mapping.
func parseCallHierarchyItem(line, workspace string) *CallHierarchyItem {
	// Format: caller[0]: ranges 128:15-26 in /path/file.go from/to function funcName in /path/file.go:line:col
	item := &CallHierarchyItem{}

	if idx := strings.Index(line, "from/to function "); idx >= 0 {
		rest := line[idx+len("from/to function "):]
		if inIdx := strings.Index(rest, " in "); inIdx >= 0 {
			item.Function = rest[:inIdx]
			locPart := rest[inIdx+4:]
			loc, err := parseLocation(locPart)
			if err == nil {
				item.File = makeRelative(loc.File, workspace)
				item.Line = loc.Line
				item.Column = loc.Column
			}
		}
	}

	if item.Function == "" {
		return nil
	}
	return item
}

// parseDiagnostics parses gopls check output to extract diagnostic messages with severity and locations for code analysis.
func parseDiagnostics(out, workspace string) ([]Diagnostic, error) {
	diags := []Diagnostic{}
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Format: /path/to/file.go:line:col-col: message
		diag, err := parseDiagnosticLine(line, workspace)
		if err != nil {
			continue
		}
		diags = append(diags, diag)
	}
	return diags, nil
}

// parseDiagnosticLine parses individual diagnostic lines to extract location, severity, and message for error reporting.
func parseDiagnosticLine(line, workspace string) (Diagnostic, error) {
	diag := Diagnostic{Severity: "error"}

	// Find message after location
	colonCount := 0
	msgStart := -1
	for i, c := range line {
		if c == ':' {
			colonCount++
			if colonCount >= 3 {
				// After file:line:col
				if i+2 < len(line) && line[i+1] == ' ' {
					msgStart = i + 2
					break
				}
			}
		}
	}

	if msgStart < 0 {
		return diag, skillerr.Parse("malformed diagnostic")
	}

	locPart := line[:msgStart-2]
	diag.Message = line[msgStart:]

	loc, err := parseLocation(locPart)
	if err != nil {
		return diag, err
	}

	diag.File = makeRelative(loc.File, workspace)
	diag.Line = loc.Line
	diag.Column = loc.Column

	return diag, nil
}

// parseLocation parses location strings with line/column coordinates and optional range information for navigation.
func parseLocation(s string) (Location, error) {
	loc := Location{}

	// Handle range format: /path:line:col-col or /path:line:col-line:col
	s = strings.TrimSpace(s)

	// Find last colon-separated numbers
	parts := strings.Split(s, ":")
	if len(parts) < 3 {
		return loc, skillerr.Parsef("invalid location format: %s", s)
	}

	// Reconstruct file path (may contain colons on Windows)
	loc.File = strings.Join(parts[:len(parts)-2], ":")

	line, err := strconv.Atoi(parts[len(parts)-2])
	if err != nil {
		return loc, skillerr.Parsef("invalid line number: %s", parts[len(parts)-2])
	}
	loc.Line = line

	// Column may have range (col-col or col-line:col)
	colPart := parts[len(parts)-1]
	if dashIdx := strings.Index(colPart, "-"); dashIdx >= 0 {
		colPart = colPart[:dashIdx]
	}

	col, err := strconv.Atoi(colPart)
	if err != nil {
		return loc, skillerr.Parsef("invalid column number: %s", colPart)
	}
	loc.Column = col

	return loc, nil
}

// parseSymbolLine parses symbol lines with name, kind, and position information for document symbol extraction.
func parseSymbolLine(line, workspace string) (Symbol, error) {
	sym := Symbol{}

	// Format: Name Kind Line:Col-Line:Col
	// Example: Resolver Struct 12:6-12:14
	parts := strings.Fields(line)
	if len(parts) < 3 {
		return sym, skillerr.Parsef("invalid symbol line: %s", line)
	}

	sym.Name = parts[0]
	sym.Kind = parts[1]

	// Parse position
	posParts := strings.Split(parts[2], "-")
	if len(posParts) >= 1 {
		startParts := strings.Split(posParts[0], ":")
		if len(startParts) >= 2 {
			sym.Line, _ = strconv.Atoi(startParts[0])
			sym.Column, _ = strconv.Atoi(startParts[1])
		}
	}
	if len(posParts) >= 2 {
		endParts := strings.Split(posParts[1], ":")
		if len(endParts) >= 2 {
			sym.EndLine, _ = strconv.Atoi(endParts[0])
			sym.EndCol, _ = strconv.Atoi(endParts[1])
		}
	}

	return sym, nil
}

// parseWorkspaceSymbolLine parses workspace symbol lines with location information and relative path conversion.
func parseWorkspaceSymbolLine(line, workspace string) (Symbol, error) {
	sym := Symbol{}

	// Format: /path/to/file.go:line:col-col Name Kind
	// Find the last space-separated parts for name and kind
	parts := strings.Fields(line)
	if len(parts) < 3 {
		return sym, skillerr.Parsef("invalid workspace symbol line: %s", line)
	}

	sym.Kind = parts[len(parts)-1]
	sym.Name = parts[len(parts)-2]
	locPart := strings.Join(parts[:len(parts)-2], " ")

	loc, err := parseLocation(locPart)
	if err != nil {
		return sym, err
	}

	sym.File = makeRelative(loc.File, workspace)
	sym.Line = loc.Line
	sym.Column = loc.Column

	return sym, nil
}

// resolvePath converts relative paths to absolute paths using the workspace as base for file operations.
func resolvePath(workspace, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workspace, path)
}

// makeRelative converts absolute paths to workspace-relative paths for cleaner output and better usability.
func makeRelative(path, workspace string) string {
	rel, err := filepath.Rel(workspace, path)
	if err != nil {
		return path
	}
	return rel
}
