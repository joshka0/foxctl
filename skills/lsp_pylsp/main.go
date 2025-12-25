// Package main implements the lsp/pylsp skill.
// It provides Python language server operations via python-lsp-server (pylsp).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

// defaultTimeout is the maximum time to wait for LSP operations.
const defaultTimeout = 30 * time.Second

type input struct {
	Operation  string `json:"operation"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Column     int    `json:"column"`
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
	Timeout    int    `json:"timeout"` // timeout in seconds, defaults to 30
}

// LSP JSON-RPC types
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// LSP types
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type SymbolInformation struct {
	Name     string   `json:"name"`
	Kind     int      `json:"kind"`
	Location Location `json:"location"`
}

type DocumentSymbol struct {
	Name           string           `json:"name"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source,omitempty"`
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
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
	nextID int
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("lsp/pylsp", "ERUNTIME", err)
	}

	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("lsp/pylsp", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("lsp/pylsp", "EARG", err)
	}

	if err := run(ctx, rc, in); err != nil {
		code := "ERUNTIME"
		var ae *argError
		if errors.As(err, &ae) {
			code = "EARG"
		}
		fail("lsp/pylsp", code, err)
	}
}

func parseInput(r io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return in, fmt.Errorf("failed to parse input: %w", err)
	}

	if in.MaxResults <= 0 {
		in.MaxResults = 50
	}
	if in.Column <= 0 {
		in.Column = 1
	}

	return in, nil
}

func run(ctx context.Context, rc *runner.RunnerContext, in input) error {
	// Check pylsp availability - try multiple common names
	serverPath, err := findPylsp()
	if err != nil {
		return err
	}

	// Apply timeout to context
	timeout := defaultTimeout
	if in.Timeout > 0 {
		timeout = time.Duration(in.Timeout) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	workspace := rc.PathValidator.Workspace()
	out := output{Operation: in.Operation}

	// Create and initialize LSP client
	client, err := newLSPClient(ctx, serverPath, workspace)
	if err != nil {
		return fmt.Errorf("failed to start language server: %w", err)
	}
	defer func() { errs.Ignore(client.Close(), "close LSP client") }()

	// Open the file if needed
	if in.File != "" {
		filePath := resolvePath(workspace, in.File)
		if err := client.openFile(ctx, filePath); err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}
	}

	switch in.Operation {
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
		if len(refs) > in.MaxResults {
			refs = refs[:in.MaxResults]
		}
		out.References = refs
		out.Count = len(refs)

	case "symbols":
		syms, err := client.documentSymbols(ctx, workspace, in)
		if err != nil {
			return err
		}
		if len(syms) > in.MaxResults {
			syms = syms[:in.MaxResults]
		}
		out.Symbols = syms
		out.Count = len(syms)

	case "workspace_symbol":
		syms, err := client.workspaceSymbols(ctx, workspace, in)
		if err != nil {
			return err
		}
		if len(syms) > in.MaxResults {
			syms = syms[:in.MaxResults]
		}
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
		diags, err := client.diagnostics(ctx, workspace, in)
		if err != nil {
			return err
		}
		out.Diagnostics = diags
		out.Count = len(diags)

	default:
		return fmt.Errorf("unknown operation: %s", in.Operation)
	}

	return writeOutput(rc, out)
}

func findPylsp() (string, error) {
	// Try common names for the Python language server
	names := []string{"pylsp", "pyls", "python-lsp-server"}
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("python-lsp-server not found in PATH (install: pip install python-lsp-server)")
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
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		nextID: 1,
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
	_, _ = c.sendRequest("shutdown", nil)
	c.sendNotification("exit", nil)
	errs.Ignore(c.stdin.Close(), "close stdin")
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

	_, err := c.sendRequest("initialize", params)
	if err != nil {
		return err
	}

	// Send initialized notification
	c.sendNotification("initialized", map[string]any{})

	// Give server time to initialize
	time.Sleep(200 * time.Millisecond)
	return nil
}

func (c *LSPClient) openFile(ctx context.Context, filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	params := map[string]any{
		"textDocument": map[string]any{
			"uri":        "file://" + filePath,
			"languageId": "python",
			"version":    1,
			"text":       string(content),
		},
	}

	c.sendNotification("textDocument/didOpen", params)

	// Give the server time to analyze
	time.Sleep(300 * time.Millisecond)
	return nil
}

func (c *LSPClient) definition(ctx context.Context, workspace string, in input) (*Definition, error) {
	if in.File == "" || in.Line <= 0 {
		return nil, fmt.Errorf("definition requires file and line")
	}

	filePath := resolvePath(workspace, in.File)
	params := map[string]any{
		"textDocument": map[string]any{
			"uri": "file://" + filePath,
		},
		"position": map[string]any{
			"line":      in.Line - 1, // LSP is 0-based
			"character": in.Column - 1,
		},
	}

	result, err := c.sendRequest("textDocument/definition", params)
	if err != nil {
		return nil, err
	}

	if result == nil || string(result) == "null" {
		return nil, fmt.Errorf("no definition found")
	}

	// Parse result - can be Location or []Location
	var locations []Location
	if err := json.Unmarshal(result, &locations); err != nil {
		var loc Location
		if err := json.Unmarshal(result, &loc); err != nil {
			return nil, fmt.Errorf("failed to parse definition result: %w", err)
		}
		locations = []Location{loc}
	}

	if len(locations) == 0 {
		return nil, fmt.Errorf("no definition found")
	}

	loc := locations[0]
	return &Definition{
		File:   uriToPath(loc.URI, workspace),
		Line:   loc.Range.Start.Line + 1,
		Column: loc.Range.Start.Character + 1,
	}, nil
}

func (c *LSPClient) references(ctx context.Context, workspace string, in input) ([]Reference, error) {
	if in.File == "" || in.Line <= 0 {
		return nil, fmt.Errorf("references requires file and line")
	}

	filePath := resolvePath(workspace, in.File)
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

	result, err := c.sendRequest("textDocument/references", params)
	if err != nil {
		return nil, err
	}

	if result == nil || string(result) == "null" {
		return []Reference{}, nil
	}

	var locations []Location
	if err := json.Unmarshal(result, &locations); err != nil {
		return nil, fmt.Errorf("failed to parse references result: %w", err)
	}

	refs := make([]Reference, 0, len(locations))
	for _, loc := range locations {
		refs = append(refs, Reference{
			File:   uriToPath(loc.URI, workspace),
			Line:   loc.Range.Start.Line + 1,
			Column: loc.Range.Start.Character + 1,
		})
	}

	return refs, nil
}

func (c *LSPClient) documentSymbols(ctx context.Context, workspace string, in input) ([]Symbol, error) {
	if in.File == "" {
		return nil, fmt.Errorf("symbols requires file")
	}

	filePath := resolvePath(workspace, in.File)
	params := map[string]any{
		"textDocument": map[string]any{
			"uri": "file://" + filePath,
		},
	}

	result, err := c.sendRequest("textDocument/documentSymbol", params)
	if err != nil {
		return nil, err
	}

	if result == nil || string(result) == "null" {
		return []Symbol{}, nil
	}

	// Can be DocumentSymbol[] or SymbolInformation[]
	var docSymbols []DocumentSymbol
	if err := json.Unmarshal(result, &docSymbols); err == nil && len(docSymbols) > 0 {
		return flattenDocSymbols(docSymbols, in.File), nil
	}

	var symInfos []SymbolInformation
	if err := json.Unmarshal(result, &symInfos); err != nil {
		return nil, fmt.Errorf("failed to parse symbols result: %w", err)
	}

	syms := make([]Symbol, 0, len(symInfos))
	for _, si := range symInfos {
		syms = append(syms, Symbol{
			Name:   si.Name,
			Kind:   symbolKindToString(si.Kind),
			File:   uriToPath(si.Location.URI, workspace),
			Line:   si.Location.Range.Start.Line + 1,
			Column: si.Location.Range.Start.Character + 1,
		})
	}

	return syms, nil
}

func (c *LSPClient) workspaceSymbols(ctx context.Context, workspace string, in input) ([]Symbol, error) {
	if in.Query == "" {
		return nil, fmt.Errorf("workspace_symbol requires query")
	}

	params := map[string]any{
		"query": in.Query,
	}

	result, err := c.sendRequest("workspace/symbol", params)
	if err != nil {
		return nil, err
	}

	if result == nil || string(result) == "null" {
		return []Symbol{}, nil
	}

	var symInfos []SymbolInformation
	if err := json.Unmarshal(result, &symInfos); err != nil {
		return nil, fmt.Errorf("failed to parse workspace symbols result: %w", err)
	}

	syms := make([]Symbol, 0, len(symInfos))
	for _, si := range symInfos {
		syms = append(syms, Symbol{
			Name:   si.Name,
			Kind:   symbolKindToString(si.Kind),
			File:   uriToPath(si.Location.URI, workspace),
			Line:   si.Location.Range.Start.Line + 1,
			Column: si.Location.Range.Start.Character + 1,
		})
	}

	return syms, nil
}

func (c *LSPClient) hover(ctx context.Context, workspace string, in input) (string, error) {
	if in.File == "" || in.Line <= 0 {
		return "", fmt.Errorf("hover requires file and line")
	}

	filePath := resolvePath(workspace, in.File)
	params := map[string]any{
		"textDocument": map[string]any{
			"uri": "file://" + filePath,
		},
		"position": map[string]any{
			"line":      in.Line - 1,
			"character": in.Column - 1,
		},
	}

	result, err := c.sendRequest("textDocument/hover", params)
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

// argError represents a validation/argument error (EARG).
type argError struct {
	msg string
}

func (e *argError) Error() string { return e.msg }

func (c *LSPClient) diagnostics(ctx context.Context, workspace string, in input) ([]DiagnosticOutput, error) {
	if in.File == "" {
		return []DiagnosticOutput{}, &argError{msg: "diagnostics requires file"}
	}

	// pylsp sends diagnostics via textDocument/publishDiagnostics notifications,
	// not via request/response. This skill does not currently implement notification
	// handling to collect and cache diagnostic notifications.
	return []DiagnosticOutput{}, &argError{msg: "diagnostics not supported: pylsp sends diagnostics via notifications; " +
		"enable notification handling to collect them, or use an IDE/editor that handles LSP notifications"}
}

func (c *LSPClient) sendRequest(method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	c.mu.Unlock()

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	// Write with Content-Length header
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(reqBytes))
	if _, err := c.stdin.Write([]byte(header)); err != nil {
		return nil, err
	}
	if _, err := c.stdin.Write(reqBytes); err != nil {
		return nil, err
	}

	// Read response with timeout
	return c.readResponse(id)
}

func (c *LSPClient) sendNotification(method string, params any) {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	reqBytes, _ := json.Marshal(req)
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(reqBytes))
	_, _ = c.stdin.Write([]byte(header))
	_, _ = c.stdin.Write(reqBytes)
}

func (c *LSPClient) readResponse(expectedID int) (json.RawMessage, error) {
	deadline := time.Now().Add(defaultTimeout)

	for time.Now().Before(deadline) {
		// Read headers
		var contentLength int
		for {
			line, err := c.stdout.ReadString('\n')
			if err != nil {
				return nil, fmt.Errorf("failed to read response header: %w", err)
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if strings.HasPrefix(line, "Content-Length:") {
				_, _ = fmt.Sscanf(line, "Content-Length: %d", &contentLength)
			}
		}

		if contentLength == 0 {
			continue
		}

		// Read body
		body := make([]byte, contentLength)
		if _, err := io.ReadFull(c.stdout, body); err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		// Check if this is a notification (no ID field) or a response (has ID field)
		// Note: ID=0 is a valid JSON-RPC request ID, so we need to distinguish
		// between a missing ID (notification) and ID=0 (response).
		var hasID struct {
			ID *int `json:"id"`
		}
		if err := json.Unmarshal(body, &hasID); err == nil && hasID.ID == nil {
			// This is a notification (no ID field), skip it
			continue
		}

		var resp jsonRPCResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			// Parse error, skip it
			continue
		}

		if resp.ID != expectedID {
			continue
		}

		if resp.Error != nil {
			return nil, fmt.Errorf("LSP error %d: %s", resp.Error.Code, resp.Error.Message)
		}

		return resp.Result, nil
	}

	return nil, fmt.Errorf("timeout waiting for response")
}

// Helper functions

func resolvePath(workspace, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workspace, path)
}

func uriToPath(uri, workspace string) string {
	path := strings.TrimPrefix(uri, "file://")
	rel, err := filepath.Rel(workspace, path)
	if err != nil {
		return path
	}
	return rel
}

func flattenDocSymbols(symbols []DocumentSymbol, file string) []Symbol {
	var result []Symbol
	var flatten func([]DocumentSymbol)
	flatten = func(syms []DocumentSymbol) {
		for _, s := range syms {
			result = append(result, Symbol{
				Name:   s.Name,
				Kind:   symbolKindToString(s.Kind),
				File:   file,
				Line:   s.Range.Start.Line + 1,
				Column: s.Range.Start.Character + 1,
			})
			if len(s.Children) > 0 {
				flatten(s.Children)
			}
		}
	}
	flatten(symbols)
	return result
}

func symbolKindToString(kind int) string {
	kinds := map[int]string{
		1:  "File",
		2:  "Module",
		3:  "Namespace",
		4:  "Package",
		5:  "Class",
		6:  "Method",
		7:  "Property",
		8:  "Field",
		9:  "Constructor",
		10: "Enum",
		11: "Interface",
		12: "Function",
		13: "Variable",
		14: "Constant",
		15: "String",
		16: "Number",
		17: "Boolean",
		18: "Array",
		19: "Object",
		20: "Key",
		21: "Null",
		22: "EnumMember",
		23: "Struct",
		24: "Event",
		25: "Operator",
		26: "TypeParameter",
	}
	if s, ok := kinds[kind]; ok {
		return s
	}
	return strconv.Itoa(kind)
}

func writeOutput(rc *runner.RunnerContext, out output) error {
	return rc.Emit("lsp/pylsp", out, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func fail(cmd, code string, err error) {
	env := envelope.Error(cmd, code, err.Error(), nil)
	_ = envelope.Write(os.Stdout, env)
	os.Exit(1)
}
