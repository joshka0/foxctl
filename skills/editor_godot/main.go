// Package main implements the editor/godot skill for interacting with the Godot Editor.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

// Supported actions.
const (
	ActionPing             = "ping"
	ActionSceneTree        = "scene_tree"
	ActionNodeInspect      = "node_inspect"
	ActionNodeCreate       = "node_create"
	ActionNodeDelete       = "node_delete"
	ActionNodeRename       = "node_rename"
	ActionNodeReparent     = "node_reparent"
	ActionNodeSetProp      = "node_set_prop"
	ActionNodeAttachScript = "node_attach_script"
	ActionSignalConnect    = "signal_connect"
	ActionClassInfo        = "class_info"
	ActionEnsureNode       = "ensure_node"
	ActionSceneSave        = "scene_save"
	ActionSceneList        = "scene_list"
	ActionSceneOpen        = "scene_open"
	ActionSceneInstance    = "scene_instance"
	ActionSearchNodes      = "search_nodes"
	ActionFocusNode        = "focus_node"
	ActionResourceList     = "resource_list"
	ActionRunGame          = "run_game"
	ActionStopGame         = "stop_game"
	ActionErrors           = "errors"
)

// Input represents the skill input parameters.
type Input struct {
	Action        string            `json:"action"`
	Host          string            `json:"host"`
	Port          int               `json:"port"`
	TimeoutMS     int               `json:"timeout_ms"`
	NodePath      string            `json:"node_path"`
	ParentPath    string            `json:"parent_path"`
	NewParentPath string            `json:"new_parent_path"`
	NodeType      string            `json:"node_type"`
	NodeName      string            `json:"node_name"`
	NewName       string            `json:"new_name"`
	Property      string            `json:"property"`
	Value         string            `json:"value"`
	ScriptPath    string            `json:"script_path"`
	SignalName    string            `json:"signal_name"`
	TargetPath    string            `json:"target_path"`
	MethodName    string            `json:"method_name"`
	ClassName     string            `json:"class_name"`
	ResourcePath  string            `json:"resource_path"`
	Pattern       string            `json:"pattern"`
	MaxDepth      int               `json:"max_depth"`
	MaxNodes      int               `json:"max_nodes"`
	MaxResults    int               `json:"max_results"`
	ErrorLimit    int               `json:"error_limit"`
	Props         map[string]string `json:"props"`
	IfExists      string            `json:"if_exists"`
	ScenePath     string            `json:"scene_path"`
	Recursive     bool              `json:"recursive"`
	InstanceName  string            `json:"instance_name"`
	SearchName    string            `json:"search_name"`
	SearchType    string            `json:"search_type"`
	Frame         bool              `json:"frame"`
}

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
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("ECONFIG", err)
	}

	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("EARG", err)
	}

	if err := run(ctx, rc, in); err != nil {
		fail("ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, in Input) error {
	// Validate action-specific required fields
	if err := validateInput(in); err != nil {
		return err
	}

	// Get workspace root for plugin handshake
	workspace := rc.PathValidator.Workspace()

	// Build plugin request
	req := PluginRequest{
		WorkspaceRoot: workspace,
		Action:        in.Action,
		Params:        buildParams(in),
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

	// Build and emit success envelope
	return emitSuccess(ctx, rc, in.Action, resp.Data)
}

func parseInput(r io.Reader) (Input, error) {
	var in Input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return Input{}, fmt.Errorf("decode input: %w", err)
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

	// Validate action
	if strings.TrimSpace(in.Action) == "" {
		return Input{}, fmt.Errorf("action is required")
	}

	return in, nil
}

func validateInput(in Input) error {
	switch in.Action {
	case ActionPing, ActionSceneTree, ActionRunGame:
		// No additional required fields
	case ActionErrors:
		// Optional error_limit already has default
	case ActionNodeInspect:
		if strings.TrimSpace(in.NodePath) == "" {
			return fmt.Errorf("node_path is required for action %q", in.Action)
		}
	case ActionNodeCreate:
		if strings.TrimSpace(in.ParentPath) == "" {
			return fmt.Errorf("parent_path is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.NodeType) == "" {
			return fmt.Errorf("node_type is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.NodeName) == "" {
			return fmt.Errorf("node_name is required for action %q", in.Action)
		}
	case ActionNodeSetProp:
		if strings.TrimSpace(in.NodePath) == "" {
			return fmt.Errorf("node_path is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.Property) == "" {
			return fmt.Errorf("property is required for action %q", in.Action)
		}
		// value can be empty string (valid for some properties)
	case ActionNodeAttachScript:
		if strings.TrimSpace(in.NodePath) == "" {
			return fmt.Errorf("node_path is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.ScriptPath) == "" {
			return fmt.Errorf("script_path is required for action %q", in.Action)
		}
	case ActionSignalConnect:
		if strings.TrimSpace(in.NodePath) == "" {
			return fmt.Errorf("node_path (source) is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.SignalName) == "" {
			return fmt.Errorf("signal_name is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.TargetPath) == "" {
			return fmt.Errorf("target_path is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.MethodName) == "" {
			return fmt.Errorf("method_name is required for action %q", in.Action)
		}
	case ActionNodeDelete:
		if strings.TrimSpace(in.NodePath) == "" {
			return fmt.Errorf("node_path is required for action %q", in.Action)
		}
	case ActionNodeRename:
		if strings.TrimSpace(in.NodePath) == "" {
			return fmt.Errorf("node_path is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.NewName) == "" {
			return fmt.Errorf("new_name is required for action %q", in.Action)
		}
	case ActionNodeReparent:
		if strings.TrimSpace(in.NodePath) == "" {
			return fmt.Errorf("node_path is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.NewParentPath) == "" {
			return fmt.Errorf("new_parent_path is required for action %q", in.Action)
		}
	case ActionClassInfo:
		if strings.TrimSpace(in.ClassName) == "" {
			return fmt.Errorf("class_name is required for action %q", in.Action)
		}
	case ActionEnsureNode:
		if strings.TrimSpace(in.NodePath) == "" {
			return fmt.Errorf("node_path is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.NodeType) == "" {
			return fmt.Errorf("node_type is required for action %q", in.Action)
		}
	case ActionSceneSave, ActionStopGame:
		// No additional required fields
	case ActionSceneList:
		// Optional path, max_results, recursive
	case ActionSceneOpen:
		if strings.TrimSpace(in.ScenePath) == "" {
			return fmt.Errorf("scene_path is required for action %q", in.Action)
		}
	case ActionSceneInstance:
		if strings.TrimSpace(in.ScenePath) == "" {
			return fmt.Errorf("scene_path is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.ParentPath) == "" {
			return fmt.Errorf("parent_path is required for action %q", in.Action)
		}
	case ActionSearchNodes:
		// All filters are optional
	case ActionFocusNode:
		if strings.TrimSpace(in.NodePath) == "" {
			return fmt.Errorf("node_path is required for action %q", in.Action)
		}
	case ActionResourceList:
		// Optional path, pattern, max_results
	default:
		return fmt.Errorf("unknown action: %q (valid: ping, scene_tree, node_inspect, node_create, node_delete, node_rename, node_reparent, node_set_prop, node_attach_script, signal_connect, class_info, ensure_node, scene_save, scene_list, scene_open, scene_instance, search_nodes, focus_node, resource_list, run_game, stop_game, errors)", in.Action)
	}
	return nil
}

func buildParams(in Input) map[string]any {
	params := make(map[string]any)

	switch in.Action {
	case ActionSceneTree:
		params["max_depth"] = in.MaxDepth
		params["max_nodes"] = in.MaxNodes
	case ActionNodeInspect:
		params["node_path"] = in.NodePath
	case ActionNodeCreate:
		params["parent_path"] = in.ParentPath
		params["type"] = in.NodeType
		params["name"] = in.NodeName
	case ActionNodeDelete:
		params["node_path"] = in.NodePath
	case ActionNodeRename:
		params["node_path"] = in.NodePath
		params["new_name"] = in.NewName
	case ActionNodeReparent:
		params["node_path"] = in.NodePath
		params["new_parent_path"] = in.NewParentPath
	case ActionNodeSetProp:
		params["node_path"] = in.NodePath
		params["property"] = in.Property
		params["value"] = in.Value
	case ActionNodeAttachScript:
		params["node_path"] = in.NodePath
		params["script_path"] = in.ScriptPath
	case ActionSignalConnect:
		params["source_path"] = in.NodePath
		params["signal_name"] = in.SignalName
		params["target_path"] = in.TargetPath
		params["method_name"] = in.MethodName
	case ActionClassInfo:
		params["class_name"] = in.ClassName
	case ActionEnsureNode:
		params["path"] = in.NodePath
		params["type"] = in.NodeType
		if len(in.Props) > 0 {
			params["props"] = in.Props
		}
		if in.IfExists != "" {
			params["if_exists"] = in.IfExists
		}
	case ActionSceneList:
		if in.ResourcePath != "" {
			params["path"] = in.ResourcePath
		}
		if in.MaxResults > 0 {
			params["max_results"] = in.MaxResults
		}
		params["recursive"] = in.Recursive
	case ActionSceneOpen:
		params["path"] = in.ScenePath
	case ActionSceneInstance:
		params["scene_path"] = in.ScenePath
		params["parent_path"] = in.ParentPath
		if in.InstanceName != "" {
			params["name"] = in.InstanceName
		}
	case ActionSearchNodes:
		if in.SearchName != "" {
			params["name"] = in.SearchName
		}
		if in.SearchType != "" {
			params["type"] = in.SearchType
		}
		if in.Property != "" {
			params["property"] = in.Property
		}
		if in.Value != "" {
			params["value"] = in.Value
		}
		if in.MaxResults > 0 {
			params["max_results"] = in.MaxResults
		}
	case ActionFocusNode:
		params["node_path"] = in.NodePath
		params["frame"] = in.Frame
	case ActionResourceList:
		if in.ResourcePath != "" {
			params["path"] = in.ResourcePath
		}
		if in.Pattern != "" {
			params["pattern"] = in.Pattern
		}
		if in.MaxResults > 0 {
			params["max_results"] = in.MaxResults
		}
	case ActionErrors:
		params["limit"] = in.ErrorLimit
	}

	return params
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
		body, _ := io.ReadAll(io.LimitReader(httpResp.Body, 1024))
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

func emitPluginError(rc *runner.RunnerContext, pe *PluginError) error {
	data := map[string]any{
		"hint": pe.Hint,
	}
	if pe.Details != nil {
		data["details"] = pe.Details
	}

	env := envelope.Envelope{
		Version: 1,
		Status:  "error",
		Command: skillName,
		Data:    data,
		Meta: envelope.Meta{
			TS:        time.Now().UTC().Format(time.RFC3339),
			Source:    "run",
			Runner:    "exec",
			Workspace: rc.PathValidator.Workspace(),
		},
		Error: envelope.ErrorFields{
			Code:    pe.Code,
			Message: pe.Message,
		},
	}

	return envelope.Write(rc.Stdout, env)
}

func emitSuccess(ctx context.Context, rc *runner.RunnerContext, action string, data any) error {
	// Serialize data to check size
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal data: %w", err)
	}

	result := map[string]any{
		"action": action,
		"result": data,
	}

	// Generate summary based on action
	summary := generateSummary(action, data)
	result["summary"] = summary

	// Check if we need to use CAS
	inlineLimit := rc.InlineKB * 1024
	if inlineLimit <= 0 {
		inlineLimit = 32 * 1024
	}

	var meta envelope.Meta
	meta.TS = time.Now().UTC().Format(time.RFC3339)
	meta.Source = "run"
	meta.Runner = "exec"
	meta.Workspace = rc.PathValidator.Workspace()

	if len(dataBytes) > inlineLimit {
		// Store in CAS
		obj, err := rc.CASStore.Put(ctx, bytes.NewReader(dataBytes), "application/json", []string{"godot", action})
		if err != nil {
			return fmt.Errorf("cas put: %w", err)
		}

		result["artifact"] = obj.Digest
		result["artifact_size_bytes"] = obj.Size
		meta.CASDigest = obj.Digest

		// Remove full result, keep only summary
		delete(result, "result")
		result["hint"] = "Full result stored in CAS; fetch via: agentctl cas get " + obj.Digest
	}

	return rc.Emit(skillName, result, "application/json", meta)
}

func generateSummary(action string, data any) string {
	switch action {
	case ActionPing:
		if m, ok := data.(map[string]any); ok {
			if root, ok := m["project_root"].(string); ok {
				return fmt.Sprintf("Connected to Godot project at %s", root)
			}
		}
		return "Connected to GodotAIBridge"

	case ActionSceneTree:
		if m, ok := data.(map[string]any); ok {
			if count, ok := m["node_count"].(float64); ok {
				return fmt.Sprintf("Scene tree with %d nodes", int(count))
			}
		}
		return "Retrieved scene tree"

	case ActionNodeInspect:
		if m, ok := data.(map[string]any); ok {
			name, _ := m["name"].(string)
			nodeType, _ := m["type"].(string)
			return fmt.Sprintf("Node %q (%s)", name, nodeType)
		}
		return "Retrieved node info"

	case ActionNodeCreate:
		if m, ok := data.(map[string]any); ok {
			path, _ := m["created_path"].(string)
			return fmt.Sprintf("Created node at %s", path)
		}
		return "Created node"

	case ActionNodeDelete:
		if m, ok := data.(map[string]any); ok {
			name, _ := m["name"].(string)
			return fmt.Sprintf("Deleted node %q", name)
		}
		return "Deleted node"

	case ActionNodeRename:
		if m, ok := data.(map[string]any); ok {
			oldName, _ := m["old_name"].(string)
			newName, _ := m["new_name"].(string)
			return fmt.Sprintf("Renamed %q to %q", oldName, newName)
		}
		return "Renamed node"

	case ActionNodeReparent:
		if m, ok := data.(map[string]any); ok {
			newPath, _ := m["new_path"].(string)
			return fmt.Sprintf("Reparented node to %s", newPath)
		}
		return "Reparented node"

	case ActionNodeSetProp:
		return "Property updated"

	case ActionNodeAttachScript:
		return "Script attached"

	case ActionSignalConnect:
		return "Signal connected"

	case ActionClassInfo:
		if m, ok := data.(map[string]any); ok {
			className, _ := m["class_name"].(string)
			return fmt.Sprintf("Class info for %s", className)
		}
		return "Retrieved class info"

	case ActionEnsureNode:
		if m, ok := data.(map[string]any); ok {
			status, _ := m["status"].(string)
			path, _ := m["path"].(string)
			created, _ := m["created"].(bool)
			if created {
				return fmt.Sprintf("Created node at %s", path)
			}
			return fmt.Sprintf("Node exists at %s (status: %s)", path, status)
		}
		return "Ensured node"

	case ActionSceneSave:
		if m, ok := data.(map[string]any); ok {
			scenePath, _ := m["scene_path"].(string)
			return fmt.Sprintf("Saved scene to %s", scenePath)
		}
		return "Scene saved"

	case ActionSceneList:
		if m, ok := data.(map[string]any); ok {
			count, _ := m["count"].(float64)
			return fmt.Sprintf("Found %d scene(s)", int(count))
		}
		return "Listed scenes"

	case ActionSceneOpen:
		if m, ok := data.(map[string]any); ok {
			scenePath, _ := m["scene_path"].(string)
			return fmt.Sprintf("Opened scene %s", scenePath)
		}
		return "Opened scene"

	case ActionSceneInstance:
		if m, ok := data.(map[string]any); ok {
			instancePath, _ := m["instance_path"].(string)
			return fmt.Sprintf("Instanced scene at %s", instancePath)
		}
		return "Instanced scene"

	case ActionSearchNodes:
		if m, ok := data.(map[string]any); ok {
			count, _ := m["count"].(float64)
			return fmt.Sprintf("Found %d matching node(s)", int(count))
		}
		return "Searched nodes"

	case ActionFocusNode:
		if m, ok := data.(map[string]any); ok {
			selectedPath, _ := m["selected_path"].(string)
			return fmt.Sprintf("Focused on %s", selectedPath)
		}
		return "Focused node"

	case ActionResourceList:
		if m, ok := data.(map[string]any); ok {
			files, _ := m["files"].([]any)
			dirs, _ := m["directories"].([]any)
			return fmt.Sprintf("Found %d files, %d directories", len(files), len(dirs))
		}
		return "Listed resources"

	case ActionRunGame:
		return "Game started"

	case ActionStopGame:
		return "Game stopped"

	case ActionErrors:
		if m, ok := data.(map[string]any); ok {
			if entries, ok := m["entries"].([]any); ok {
				return fmt.Sprintf("Retrieved %d error(s)", len(entries))
			}
		}
		return "Retrieved errors"

	default:
		return fmt.Sprintf("Completed action: %s", action)
	}
}

func fail(code string, err error) {
	env := envelope.Error(skillName, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit failure envelope")
	os.Exit(1)
}
