// Package main implements the editor/godot skill for interacting with the Godot Editor.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/skills/editor_godot/handlers"
)

// PluginRequest is sent to the GodotAIBridge plugin.
type PluginRequest struct {
	WorkspaceRoot string         `json:"workspace_root"`
	Action        string         `json:"action"`
	Params        map[string]any `json:"params"`
}

// PluginResponse is received from the GodotAIBridge plugin.
type PluginResponse struct {
	Status string       `json:"status"`
	Data   any          `json:"data"`
	Error  *PluginError `json:"error"`
}

// PluginError represents an error from the plugin.
type PluginError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Hint    string         `json:"hint"`
	Details map[string]any `json:"details"`
}

const skillName = "editor/godot"

func main() {
	skillmain.Main(skillName, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in handlers.Input) error {
	// Validate
	if strings.TrimSpace(in.Action) == "" {
		return fmt.Errorf("action is required")
	}
	// Apply defaults
	if in.Host == "" {
		in.Host = "127.0.0.1"
	}
	if in.Port == 0 {
		in.Port = 7777
	}
	if in.TimeoutMS == 0 {
		in.TimeoutMS = 10000
	}
	if in.MaxDepth == 0 {
		in.MaxDepth = 10
	}
	if in.MaxNodes == 0 {
		in.MaxNodes = 500
	}
	if in.ErrorLimit == 0 {
		in.ErrorLimit = 50
	}

	// Get handler for action
	handler, ok := handlers.GetHandler(in.Action)
	if !ok {
		actions := handlers.AllActions()
		sort.Strings(actions)
		return fmt.Errorf("unknown action: %q (valid: %s)", in.Action, strings.Join(actions, ", "))
	}

	// Validate action-specific required fields
	if err := handler.Validate(in); err != nil {
		return err
	}

	// Get workspace root for plugin handshake
	workspace := rc.PathValidator.Workspace()

	// Build plugin request using handler
	req := PluginRequest{
		WorkspaceRoot: workspace,
		Action:        in.Action,
		Params:        handler.BuildParams(in),
	}

	// Call plugin
	timeout := time.Duration(in.TimeoutMS) * time.Millisecond
	resp, err := callPlugin(ctx, in.Host, in.Port, timeout, req)
	if err != nil {
		return err
	}

	// Handle plugin error
	if resp.Status == "error" && resp.Error != nil {
		return emitPluginError(rc, resp.Error)
	}

	// Build and emit success envelope using handler for summary
	return emitSuccess(ctx, rc, in.Action, resp.Data, handler)
}

func callPlugin(ctx context.Context, host string, port int, timeout time.Duration, req PluginRequest) (*PluginResponse, error) {
	url := fmt.Sprintf("http://%s:%d/", host, port)

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(httpCtx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		// Provide actionable error for connection failures
		return nil, &bridgeError{
			code:    "EBRIDGE_UNAVAILABLE",
			message: fmt.Sprintf("cannot connect to GodotAIBridge at %s:%d", host, port),
			hint:    "Ensure Godot Editor is running with the GodotAIBridge plugin enabled. The plugin should be listening on the specified host:port.",
			cause:   err,
		}
	}
	defer func() {
		errs.Ignore(httpResp.Body.Close(), "close plugin response body")
	}()

	if httpResp.StatusCode != http.StatusOK {
		// Error body read; error is not actionable in error path.
		body, _ := io.ReadAll(io.LimitReader(httpResp.Body, 1024)) //nolint:errcheck
		return nil, &bridgeError{
			code:    "EBRIDGE_HTTP",
			message: fmt.Sprintf("plugin returned HTTP %d", httpResp.StatusCode),
			hint:    fmt.Sprintf("Response: %s", string(body)),
		}
	}

	var resp PluginResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode plugin response: %w", err)
	}

	return &resp, nil
}

// bridgeError represents a connection or protocol error with the plugin.
type bridgeError struct {
	code    string
	message string
	hint    string
	cause   error
}

func (e *bridgeError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.message, e.cause)
	}
	return e.message
}

func emitPluginError(rc *skillmain.RunContext, pe *PluginError) error {
	data := map[string]any{
		"hint":    pe.Hint,
		"code":    pe.Code,
		"message": pe.Message,
	}
	if pe.Details != nil {
		data["details"] = pe.Details
	}
	// Return error to let skillmain emit error envelope
	return fmt.Errorf("%s: %s", pe.Code, pe.Message)
}

func emitSuccess(ctx context.Context, rc *skillmain.RunContext, action string, data any, handler handlers.Handler) error {
	// Serialize data to check size
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal data: %w", err)
	}

	result := map[string]any{
		"action": action,
		"result": data,
	}

	// Generate summary using handler
	summary := handler.GenerateSummary(action, data)
	result["summary"] = summary

	// Check if we need to use CAS
	inlineLimit := rc.InlineKB * 1024
	if inlineLimit <= 0 {
		inlineLimit = 32 * 1024
	}

	if len(dataBytes) > inlineLimit {
		// Store in CAS
		obj, err := rc.CASStore.Put(ctx, bytes.NewReader(dataBytes), "application/json", []string{"godot", action})
		if err != nil {
			return fmt.Errorf("cas put: %w", err)
		}

		result["artifact"] = obj.Digest
		result["artifact_size_bytes"] = obj.Size

		// Remove full result, keep only summary
		delete(result, "result")
		result["hint"] = "Full result stored in CAS; fetch via: agentctl cas get " + obj.Digest
	}

	return skillout.Emit(rc, skillName, result)
}

