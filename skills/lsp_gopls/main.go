// Package main implements the lsp/gopls skill.
// It provides Go language server operations via a persistent gopls daemon.
// The daemon is spawned on first use and reused for subsequent requests,
// providing ~100x faster response times compared to CLI mode.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/lsp/gopls"
)

// defaultTimeout is the maximum time to wait for gopls operations.
// Increased from 30s to 60s to accommodate cold-start and large codebases.
const defaultTimeout = 60 * time.Second

// useDaemon controls whether to use the persistent daemon or CLI mode.
// Daemon mode is faster but may have issues with some operations.
var useDaemon = os.Getenv("AGENTCTL_GOPLS_CLI") != "1"

// Input defines the input parameters for lsp/gopls.
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

type textDocumentParam struct {
	URI string `json:"uri"`
}

type positionParam struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Result types for different operations
type Location struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	End    *Pos   `json:"end,omitempty"`
}

type Pos struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type Symbol struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	EndLine int    `json:"end_line,omitempty"`
	EndCol  int    `json:"end_column,omitempty"`
}

type Definition struct {
	Location Location `json:"location"`
	Text     string   `json:"text,omitempty"`
}

type Reference struct {
	Location Location `json:"location"`
}

type CallHierarchyItem struct {
	Function string `json:"function"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
}

type CallHierarchy struct {
	Identifier string              `json:"identifier"`
	Callers    []CallHierarchyItem `json:"callers"`
	Callees    []CallHierarchyItem `json:"callees"`
}

type Diagnostic struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type output struct {
	Operation     string         `json:"operation"`
	Definition    *Definition    `json:"definition,omitempty"`
	References    []Reference    `json:"references,omitempty"`
	Symbols       []Symbol       `json:"symbols,omitempty"`
	CallHierarchy *CallHierarchy `json:"call_hierarchy,omitempty"`
	Diagnostics   []Diagnostic   `json:"diagnostics,omitempty"`
	Count         int            `json:"count"`
}

func main() {
	skillmain.Main("lsp/gopls", run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Apply defaults and normalize LSP-style parameters
	normalizeInput(&in)

	// Apply timeout to context
	timeout := defaultTimeout
	if in.Timeout > 0 {
		timeout = time.Duration(in.Timeout) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	workspace := rc.PathValidator.Workspace()

	// Validate file path if provided to prevent path traversal attacks
	if in.File != "" {
		if _, err := rc.PathValidator.ValidatePath(in.File); err != nil {
			return fmt.Errorf("invalid file path: %w", err)
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

// normalizeInput applies defaults and LSP-style parameter normalization.
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

// runWithDaemon uses the persistent gopls daemon.
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

// runWithCLI uses the traditional gopls CLI (slower but supports all operations).
func runWithCLI(ctx context.Context, rc *skillmain.RunContext, workspace string, in Input) error {
	// Check gopls availability
	goplsPath, err := exec.LookPath("gopls")
	if err != nil {
		return fmt.Errorf("gopls not found in PATH: %w", err)
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
		if len(refs) > in.MaxResults {
			refs = refs[:in.MaxResults]
		}
		out.References = refs
		out.Count = len(refs)

	case "symbols":
		syms, err := runSymbols(ctx, goplsPath, workspace, in)
		if err != nil {
			return err
		}
		if len(syms) > in.MaxResults {
			syms = syms[:in.MaxResults]
		}
		out.Symbols = syms
		out.Count = len(syms)

	case "workspace_symbol":
		syms, err := runWorkspaceSymbol(ctx, goplsPath, workspace, in)
		if err != nil {
			return err
		}
		if len(syms) > in.MaxResults {
			syms = syms[:in.MaxResults]
		}
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
		if len(refs) > in.MaxResults {
			refs = refs[:in.MaxResults]
		}
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

func runDefinition(ctx context.Context, goplsPath, workspace string, in Input) (*Definition, error) {
	if in.File == "" || in.Line <= 0 {
		return nil, fmt.Errorf("definition requires file and line")
	}

	filePath := resolvePath(workspace, in.File)
	pos := fmt.Sprintf("%s:%d:%d", filePath, in.Line, in.Column)

	cmd := exec.CommandContext(ctx, goplsPath, "definition", pos)
	cmd.Dir = workspace
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gopls definition failed: %w", err)
	}

	return parseDefinition(string(out))
}

func runReferences(ctx context.Context, goplsPath, workspace string, in Input) ([]Reference, error) {
	if in.File == "" || in.Line <= 0 {
		return nil, fmt.Errorf("references requires file and line")
	}

	filePath := resolvePath(workspace, in.File)
	pos := fmt.Sprintf("%s:%d:%d", filePath, in.Line, in.Column)

	cmd := exec.CommandContext(ctx, goplsPath, "references", pos)
	cmd.Dir = workspace
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gopls references failed: %w", err)
	}

	return parseReferences(string(out), workspace)
}

func runSymbols(ctx context.Context, goplsPath, workspace string, in Input) ([]Symbol, error) {
	if in.File == "" {
		return nil, fmt.Errorf("symbols requires file")
	}

	filePath := resolvePath(workspace, in.File)

	cmd := exec.CommandContext(ctx, goplsPath, "symbols", filePath)
	cmd.Dir = workspace
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gopls symbols failed: %w", err)
	}

	return parseSymbols(string(out), workspace)
}

func runWorkspaceSymbol(ctx context.Context, goplsPath, workspace string, in Input) ([]Symbol, error) {
	if in.Query == "" {
		return nil, fmt.Errorf("workspace_symbol requires query")
	}

	cmd := exec.CommandContext(ctx, goplsPath, "workspace_symbol", in.Query)
	cmd.Dir = workspace
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gopls workspace_symbol failed: %w", err)
	}

	return parseWorkspaceSymbols(string(out), workspace)
}

func runCallHierarchy(ctx context.Context, goplsPath, workspace string, in Input) (*CallHierarchy, error) {
	if in.File == "" || in.Line <= 0 {
		return nil, fmt.Errorf("call_hierarchy requires file and line")
	}

	filePath := resolvePath(workspace, in.File)
	pos := fmt.Sprintf("%s:%d:%d", filePath, in.Line, in.Column)

	cmd := exec.CommandContext(ctx, goplsPath, "call_hierarchy", pos)
	cmd.Dir = workspace
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gopls call_hierarchy failed: %w", err)
	}

	return parseCallHierarchy(string(out), workspace)
}

func runImplementation(ctx context.Context, goplsPath, workspace string, in Input) ([]Reference, error) {
	if in.File == "" || in.Line <= 0 {
		return nil, fmt.Errorf("implementation requires file and line")
	}

	filePath := resolvePath(workspace, in.File)
	pos := fmt.Sprintf("%s:%d:%d", filePath, in.Line, in.Column)

	cmd := exec.CommandContext(ctx, goplsPath, "implementation", pos)
	cmd.Dir = workspace
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gopls implementation failed: %w", err)
	}

	return parseReferences(string(out), workspace)
}

func runCheck(ctx context.Context, goplsPath, workspace string, in Input) ([]Diagnostic, error) {
	if in.File == "" {
		return nil, fmt.Errorf("check requires file")
	}

	filePath := resolvePath(workspace, in.File)

	cmd := exec.CommandContext(ctx, goplsPath, "check", filePath)
	cmd.Dir = workspace
	out, _ := cmd.Output() // gopls check may exit non-zero on errors

	return parseDiagnostics(string(out), workspace)
}

// Parsing functions

func parseDefinition(out string) (*Definition, error) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("no definition found")
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
		return diag, fmt.Errorf("malformed diagnostic")
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

func parseLocation(s string) (Location, error) {
	loc := Location{}

	// Handle range format: /path:line:col-col or /path:line:col-line:col
	s = strings.TrimSpace(s)

	// Find last colon-separated numbers
	parts := strings.Split(s, ":")
	if len(parts) < 3 {
		return loc, fmt.Errorf("invalid location format: %s", s)
	}

	// Reconstruct file path (may contain colons on Windows)
	loc.File = strings.Join(parts[:len(parts)-2], ":")

	line, err := strconv.Atoi(parts[len(parts)-2])
	if err != nil {
		return loc, fmt.Errorf("invalid line number: %s", parts[len(parts)-2])
	}
	loc.Line = line

	// Column may have range (col-col or col-line:col)
	colPart := parts[len(parts)-1]
	if dashIdx := strings.Index(colPart, "-"); dashIdx >= 0 {
		colPart = colPart[:dashIdx]
	}

	col, err := strconv.Atoi(colPart)
	if err != nil {
		return loc, fmt.Errorf("invalid column number: %s", colPart)
	}
	loc.Column = col

	return loc, nil
}

func parseSymbolLine(line, workspace string) (Symbol, error) {
	sym := Symbol{}

	// Format: Name Kind Line:Col-Line:Col
	// Example: Resolver Struct 12:6-12:14
	parts := strings.Fields(line)
	if len(parts) < 3 {
		return sym, fmt.Errorf("invalid symbol line: %s", line)
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

func parseWorkspaceSymbolLine(line, workspace string) (Symbol, error) {
	sym := Symbol{}

	// Format: /path/to/file.go:line:col-col Name Kind
	// Find the last space-separated parts for name and kind
	parts := strings.Fields(line)
	if len(parts) < 3 {
		return sym, fmt.Errorf("invalid workspace symbol line: %s", line)
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

func resolvePath(workspace, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workspace, path)
}

func makeRelative(path, workspace string) string {
	rel, err := filepath.Rel(workspace, path)
	if err != nil {
		return path
	}
	return rel
}
