// Package main implements the lsp/pylsp skill.
// It provides Python language server operations via python-lsp-server (pylsp).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	lsphelpers "github.com/jkatigb/agentctl/internal/adapters/skillslib/lsp"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/oputil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/sliceutil"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/lsp/jsonrpc"
)

// defaultTimeout is the maximum time to wait for LSP operations.
const defaultTimeout = 30 * time.Second

const command = "lsp/pylsp"

var allowedOps = []string{"definition", "references", "symbols", "workspace_symbol", "hover", "diagnostics"}

type input struct {
	Operation  string `json:"operation"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Column     int    `json:"column"`
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
	Timeout    int    `json:"timeout"` // timeout in seconds, defaults to 30
}

type Diagnostic struct {
	Range    lsphelpers.Range `json:"range"`
	Severity int              `json:"severity"`
	Message  string           `json:"message"`
	Source   string           `json:"source,omitempty"`
}

// Symbol represents a document symbol with name, kind, and location.
type Symbol struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type Reference struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type Definition struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Text   string `json:"text,omitempty"`
}

type DiagnosticOutput struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source,omitempty"`
}

type output struct {
	Operation   string             `json:"operation"`
	Definition  *Definition        `json:"definition,omitempty"`
	References  []Reference        `json:"references,omitempty"`
	Symbols     []Symbol           `json:"symbols,omitempty"`
	Diagnostics []DiagnosticOutput `json:"diagnostics,omitempty"`
	Hover       string             `json:"hover,omitempty"`
	Count       int                `json:"count"`
}

// LSPClient manages the Python language server lifecycle
type LSPClient struct {
	cmd *exec.Cmd
	rpc *jsonrpc.Client
}

func main() {
	skillmain.Main(command, skillmain.Chain(run,
		skillmain.WithDynamicTimeout[input](func(in input) time.Duration {
			if in.Timeout > 0 {
				return time.Duration(in.Timeout) * time.Second
			}
			return defaultTimeout
		}),
		skillmain.WithRecover[input](),
	))
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Apply defaults
	if in.MaxResults <= 0 {
		in.MaxResults = 50
	}
	if in.Column <= 0 {
		in.Column = 1
	}

	op := oputil.Op(in.Operation)
	opHint := fmt.Sprintf("Use one of: %s.", strings.Join(allowedOps, ", "))
	if op == "" {
		return skillerr.Arg("operation is required", skillerr.WithHint(opHint))
	}
	if err := oputil.Validate(op, allowedOps...); err != nil {
		return skillerr.Arg(err.Error(), skillerr.WithHint(opHint))
	}

	// Check pylsp availability - try multiple common names
	serverPath, err := findPylsp()
	if err != nil {
		return err
	}

	workspace := rc.Workspace
	out := output{Operation: op}

	// Create and initialize LSP client
	client, err := newLSPClient(ctx, serverPath, workspace)
	if err != nil {
		return skillerr.WrapRuntime("failed to start language server", err)
	}
	defer func() { errs.Ignore(client.Close(), "close LSP client") }()

	// Open the file if needed
	if in.File != "" {
		filePath := lsphelpers.ResolvePath(workspace, in.File)
		if err := client.openFile(ctx, filePath); err != nil {
			return err
		}
	}

	switch op {
	case "definition":
		def, err := client.definition(ctx, workspace, in)
		if err != nil {
			return err
		}
		out.Definition = def
		out.Count = 1

	case "references":
		refs, err := client.references(ctx, workspace, in)
		if err != nil {
			return err
		}
		refs = sliceutil.Limit(refs, in.MaxResults)
		out.References = refs
		out.Count = len(refs)

	case "symbols":
		syms, err := client.documentSymbols(ctx, workspace, in)
		if err != nil {
			return err
		}
		syms = sliceutil.Limit(syms, in.MaxResults)
		out.Symbols = syms
		out.Count = len(syms)

	case "workspace_symbol":
		syms, err := client.workspaceSymbols(ctx, workspace, in)
		if err != nil {
			return err
		}
		syms = sliceutil.Limit(syms, in.MaxResults)
		out.Symbols = syms
		out.Count = len(syms)

	case "hover":
		// Hover returns markdown documentation
		hover, err := client.hover(ctx, workspace, in)
		if err != nil {
			return err
		}
		// Return hover content in the dedicated Hover field
		out.Hover = hover
		if hover != "" {
			out.Count = 1
		} else {
			out.Count = 0
		}

	case "diagnostics":
		diags, err := client.diagnostics(ctx, workspace, in) //nolint:staticcheck // SA4023: diagnostics is a stub returning error
		if err != nil {                                      //nolint:staticcheck // SA4023: Intentional for future implementation
			return err
		}
		out.Diagnostics = diags
		out.Count = len(diags)

	default:
		return skillerr.Arg("invalid operation", skillerr.WithHint(opHint))
	}

	return writeOutput(rc, out)
}

func findPylsp() (string, error) {
	// Try common names for the Python language server
	names := []string{"pylsp", "pyls", "python-lsp-server"}
	path, err := executil.RequireAny(names, "install python-lsp-server: pip install python-lsp-server")
	if err != nil {
		return "", skillerr.Runtime(
			"python-lsp-server not found in PATH",
			skillerr.WithCause(err),
			skillerr.WithHint("Install with: pip install python-lsp-server"),
		)
	}
	return path, nil
}

func newLSPClient(ctx context.Context, serverPath, workspace string) (*LSPClient, error) {
	cmd := exec.CommandContext(ctx, serverPath)
	cmd.Dir = workspace

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	// Suppress stderr to avoid noise
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	client := &LSPClient{
		cmd: cmd,
		rpc: jsonrpc.NewClient(stdin, stdout),
	}

	// Initialize the server
	if err := client.initialize(ctx, workspace); err != nil {
		errs.Ignore(client.Close(), "close LSP client on init error")
		return nil, err
	}

	return client, nil
}

func (c *LSPClient) Close() error {
	// Send shutdown request
	_, _ = c.rpc.Call(context.Background(), "shutdown", nil)
	_ = c.rpc.Notify("exit", nil)
	errs.Ignore(c.rpc.Close(), "close LSP RPC client")
	return c.cmd.Wait()
}

func (c *LSPClient) initialize(ctx context.Context, workspace string) error {
	params := map[string]any{
		"processId": os.Getpid(),
		"rootUri":   "file://" + workspace,
		"rootPath":  workspace,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"synchronization": map[string]any{
					"didSave":   true,
					"didOpen":   true,
					"didClose":  true,
					"didChange": true,
				},
				"definition":         map[string]any{},
				"references":         map[string]any{},
				"documentSymbol":     map[string]any{},
				"hover":              map[string]any{},
				"publishDiagnostics": map[string]any{},
			},
			"workspace": map[string]any{
				"symbol": map[string]any{},
			},
		},
	}

	_, err := c.rpc.Call(ctx, "initialize", params)
	if err != nil {
		return err
	}

	// Send initialized notification
	if err := c.rpc.Notify("initialized", map[string]any{}); err != nil {
		return err
	}

	// Give server time to initialize
	time.Sleep(200 * time.Millisecond)
	return nil
}

func (c *LSPClient) openFile(_ context.Context, filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return skillerr.WrapIO("open file", err)
	}

	params := map[string]any{
		"textDocument": map[string]any{
			"uri":        "file://" + filePath,
			"languageId": "python",
			"version":    1,
			"text":       string(content),
		},
	}

	if err := c.rpc.Notify("textDocument/didOpen", params); err != nil {
		return skillerr.WrapRuntime("notify didOpen", err)
	}

	// Give the server time to analyze
	time.Sleep(300 * time.Millisecond)
	return nil
}

func (c *LSPClient) definition(ctx context.Context, workspace string, in input) (*Definition, error) {
	if in.File == "" || in.Line <= 0 {
		return nil, skillerr.Arg("definition requires file and line")
	}

	filePath := lsphelpers.ResolvePath(workspace, in.File)
	params := map[string]any{
		"textDocument": map[string]any{
			"uri": "file://" + filePath,
		},
		"position": map[string]any{
			"line":      in.Line - 1, // LSP is 0-based
			"character": in.Column - 1,
		},
	}

	result, err := c.rpc.Call(ctx, "textDocument/definition", params)
	if err != nil {
		return nil, err
	}

	if result == nil || string(result) == "null" {
		return nil, skillerr.NotFound("no definition found")
	}

	// Parse result - can be Location or []Location
	var locations []lsphelpers.Location
	if err := json.Unmarshal(result, &locations); err != nil {
		var loc lsphelpers.Location
		if err := json.Unmarshal(result, &loc); err != nil {
			return nil, skillerr.WrapParse("failed to parse definition result", err)
		}
		locations = []lsphelpers.Location{loc}
	}

	if len(locations) == 0 {
		return nil, skillerr.NotFound("no definition found")
	}

	loc := locations[0]
	return &Definition{
		File:   lsphelpers.URIToPath(loc.URI, workspace),
		Line:   loc.Range.Start.Line + 1,
		Column: loc.Range.Start.Character + 1,
	}, nil
}

func (c *LSPClient) references(ctx context.Context, workspace string, in input) ([]Reference, error) {
	if in.File == "" || in.Line <= 0 {
		return nil, skillerr.Arg("references requires file and line")
	}

	filePath := lsphelpers.ResolvePath(workspace, in.File)
	params := map[string]any{
		"textDocument": map[string]any{
			"uri": "file://" + filePath,
		},
		"position": map[string]any{
			"line":      in.Line - 1,
			"character": in.Column - 1,
		},
		"context": map[string]any{
			"includeDeclaration": true,
		},
	}

	result, err := c.rpc.Call(ctx, "textDocument/references", params)
	if err != nil {
		return nil, err
	}

	if result == nil || string(result) == "null" {
		return []Reference{}, nil
	}

	var locations []lsphelpers.Location
	if err := json.Unmarshal(result, &locations); err != nil {
		return nil, skillerr.WrapParse("failed to parse references result", err)
	}

	refs := make([]Reference, 0, len(locations))
	for _, loc := range locations {
		refs = append(refs, Reference{
			File:   lsphelpers.URIToPath(loc.URI, workspace),
			Line:   loc.Range.Start.Line + 1,
			Column: loc.Range.Start.Character + 1,
		})
	}

	return refs, nil
}

func (c *LSPClient) documentSymbols(ctx context.Context, workspace string, in input) ([]Symbol, error) {
	if in.File == "" {
		return nil, skillerr.Arg("symbols requires file")
	}

	filePath := lsphelpers.ResolvePath(workspace, in.File)
	params := map[string]any{
		"textDocument": map[string]any{
			"uri": "file://" + filePath,
		},
	}

	result, err := c.rpc.Call(ctx, "textDocument/documentSymbol", params)
	if err != nil {
		return nil, err
	}

	if result == nil || string(result) == "null" {
		return []Symbol{}, nil
	}

	// Can be DocumentSymbol[] or SymbolInformation[]
	var docSymbols []lsphelpers.DocumentSymbol
	if err := json.Unmarshal(result, &docSymbols); err == nil && len(docSymbols) > 0 {
		flat := lsphelpers.FlattenDocumentSymbols(docSymbols)
		syms := make([]Symbol, 0, len(flat))
		for _, s := range flat {
			syms = append(syms, Symbol{
				Name:   s.Name,
				Kind:   lsphelpers.SymbolKindToString(s.Kind),
				File:   in.File,
				Line:   s.Range.Start.Line + 1,
				Column: s.Range.Start.Character + 1,
			})
		}
		return syms, nil
	}

	var symInfos []lsphelpers.SymbolInformation
	if err := json.Unmarshal(result, &symInfos); err != nil {
		return nil, skillerr.WrapParse("failed to parse symbols result", err)
	}

	syms := make([]Symbol, 0, len(symInfos))
	for _, si := range symInfos {
		syms = append(syms, Symbol{
			Name:   si.Name,
			Kind:   lsphelpers.SymbolKindToString(si.Kind),
			File:   lsphelpers.URIToPath(si.Location.URI, workspace),
			Line:   si.Location.Range.Start.Line + 1,
			Column: si.Location.Range.Start.Character + 1,
		})
	}

	return syms, nil
}

func (c *LSPClient) workspaceSymbols(ctx context.Context, workspace string, in input) ([]Symbol, error) {
	if in.Query == "" {
		return nil, skillerr.Arg("workspace_symbol requires query")
	}

	params := map[string]any{
		"query": in.Query,
	}

	result, err := c.rpc.Call(ctx, "workspace/symbol", params)
	if err != nil {
		return nil, err
	}

	if result == nil || string(result) == "null" {
		return []Symbol{}, nil
	}

	var symInfos []lsphelpers.SymbolInformation
	if err := json.Unmarshal(result, &symInfos); err != nil {
		return nil, skillerr.WrapParse("failed to parse workspace symbols result", err)
	}

	syms := make([]Symbol, 0, len(symInfos))
	for _, si := range symInfos {
		syms = append(syms, Symbol{
			Name:   si.Name,
			Kind:   lsphelpers.SymbolKindToString(si.Kind),
			File:   lsphelpers.URIToPath(si.Location.URI, workspace),
			Line:   si.Location.Range.Start.Line + 1,
			Column: si.Location.Range.Start.Character + 1,
		})
	}

	return syms, nil
}

func (c *LSPClient) hover(ctx context.Context, workspace string, in input) (string, error) {
	if in.File == "" || in.Line <= 0 {
		return "", skillerr.Arg("hover requires file and line")
	}

	filePath := lsphelpers.ResolvePath(workspace, in.File)
	params := map[string]any{
		"textDocument": map[string]any{
			"uri": "file://" + filePath,
		},
		"position": map[string]any{
			"line":      in.Line - 1,
			"character": in.Column - 1,
		},
	}

	result, err := c.rpc.Call(ctx, "textDocument/hover", params)
	if err != nil {
		return "", err
	}

	if result == nil || string(result) == "null" {
		return "", nil
	}

	var hover struct {
		Contents any `json:"contents"`
	}
	if err := json.Unmarshal(result, &hover); err != nil {
		return "", nil
	}

	// Contents can be string, MarkupContent, or array
	switch v := hover.Contents.(type) {
	case string:
		return v, nil
	case map[string]any:
		if val, ok := v["value"].(string); ok {
			return val, nil
		}
	}

	return "", nil
}

//nolint:staticcheck // SA4023: Stub method that always returns error - will be implemented when notification handling is added
func (c *LSPClient) diagnostics(_ context.Context, _ string, in input) ([]DiagnosticOutput, error) {
	if in.File == "" {
		return []DiagnosticOutput{}, skillerr.Arg("diagnostics requires file")
	}

	// pylsp sends diagnostics via textDocument/publishDiagnostics notifications,
	// not via request/response. This skill does not currently implement notification
	// handling to collect and cache diagnostic notifications.
	return []DiagnosticOutput{}, skillerr.Arg(
		"diagnostics not supported: pylsp sends diagnostics via notifications",
		skillerr.WithHint("Enable notification handling to collect them, or use an IDE/editor that handles LSP notifications."),
	)
}

func writeOutput(rc *skillmain.RunContext, out output) error {
	return skillout.Emit(rc, command, out)
}
