// Package main implements the lsp/tsserver skill.
// It provides TypeScript/JavaScript language server operations via typescript-language-server.
//
// Unlike gopls which has a convenient CLI mode, typescript-language-server uses
// JSON-RPC over stdio, requiring us to manage the LSP lifecycle.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	lsphelpers "github.com/jkatigb/agentctl/internal/adapters/skillslib/lsp"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/oputil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/sliceutil"
	"github.com/jkatigb/agentctl/internal/lsp/jsonrpc"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

// defaultTimeout is the maximum time to wait for LSP operations.
const defaultTimeout = 30 * time.Second

const command = "lsp/tsserver"

var allowedOps = []string{"definition", "references", "symbols", "workspace_symbol"}

type input struct {
	Operation  string `json:"operation"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Column     int    `json:"column"`
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
	Timeout    int    `json:"timeout"` // timeout in seconds, defaults to 30
}

// Output types
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

type output struct {
	Operation   string      `json:"operation"`
	Definition  *Definition `json:"definition,omitempty"`
	References  []Reference `json:"references,omitempty"`
	Symbols     []Symbol    `json:"symbols,omitempty"`
	Diagnostics []any       `json:"diagnostics,omitempty"`
	Count       int         `json:"count"`
}

// LSPClient manages the TypeScript language server lifecycle
type LSPClient struct {
	cmd *exec.Cmd
	rpc *jsonrpc.Client
}

func main() {
	skillmain.Main(command, run)
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

	// Check typescript-language-server availability
	serverPath, err := executil.RequireTool("typescript-language-server", "install with: npm i -g typescript-language-server")
	if err != nil {
		return skillerr.Runtime(
			"typescript-language-server not found in PATH",
			skillerr.WithCause(err),
			skillerr.WithHint("Install with: npm i -g typescript-language-server"),
		)
	}

	// Apply timeout to context
	timeout := defaultTimeout
	if in.Timeout > 0 {
		timeout = time.Duration(in.Timeout) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

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

	default:
		return skillerr.Arg("invalid operation", skillerr.WithHint(opHint))
	}

	return writeOutput(rc, out)
}

func newLSPClient(ctx context.Context, serverPath, workspace string) (*LSPClient, error) {
	cmd := exec.CommandContext(ctx, serverPath, "--stdio")
	cmd.Dir = workspace

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

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

// shutdownTimeout is the maximum time to wait for graceful shutdown.
const shutdownTimeout = 5 * time.Second

func (c *LSPClient) Close() error {
	// Send shutdown request with timeout
	done := make(chan error, 1)
	go func() {
		// Use background context for shutdown since we're in Close()
		_, _ = c.rpc.Call(context.Background(), "shutdown", nil)
		_ = c.rpc.Notify("exit", nil)
		errs.Ignore(c.rpc.Close(), "close LSP RPC client")
		done <- c.cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(shutdownTimeout):
		// Forcefully kill if graceful shutdown times out
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		return skillerr.Runtimef("LSP server shutdown timed out after %v", shutdownTimeout)
	}
}

func (c *LSPClient) initialize(ctx context.Context, workspace string) error {
	params := map[string]any{
		"processId": os.Getpid(),
		"rootUri":   "file://" + workspace,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"synchronization": map[string]any{
					"didSave": true,
				},
				"definition":         map[string]any{},
				"references":         map[string]any{},
				"documentSymbol":     map[string]any{},
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
	return c.rpc.Notify("initialized", map[string]any{})
}

func (c *LSPClient) openFile(_ context.Context, filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return skillerr.WrapIO("open file", err)
	}

	params := map[string]any{
		"textDocument": map[string]any{
			"uri":        "file://" + filePath,
			"languageId": detectLanguage(filePath),
			"version":    1,
			"text":       string(content),
		},
	}

	if err := c.rpc.Notify("textDocument/didOpen", params); err != nil {
		return skillerr.WrapRuntime("notify didOpen", err)
	}

	// Give the server a moment to process
	time.Sleep(100 * time.Millisecond)
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

func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".ts":
		return "typescript"
	case ".tsx":
		return "typescriptreact"
	case ".js":
		return "javascript"
	case ".jsx":
		return "javascriptreact"
	default:
		return "typescript"
	}
}

func writeOutput(rc *skillmain.RunContext, out output) error {
	return skillout.Emit(rc, command, out)
}
