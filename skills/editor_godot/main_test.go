package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skilltest"
	"github.com/joshka0/foxctl/skills/editor_godot/handlers"
)

// applyDefaultsAndValidate applies defaults and validates required fields (mirrors run function).
func applyDefaultsAndValidate(in *handlers.Input) error {
	// Validate required field
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
	return nil
}

// parseInput is a test helper that parses JSON, applies defaults, and validates.
func parseInput(r io.Reader) (handlers.Input, error) {
	in, err := skilltest.ParseInput[handlers.Input](r)
	if err != nil {
		return in, err
	}
	if err := applyDefaultsAndValidate(&in); err != nil {
		return in, err
	}
	return in, nil
}

func TestParseInput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, in handlers.Input)
	}{
		{
			name:    "valid ping action",
			input:   `{"action": "ping"}`,
			wantErr: false,
			check: func(t *testing.T, in handlers.Input) {
				if in.Action != "ping" {
					t.Errorf("expected action=ping, got %s", in.Action)
				}
				if in.Host != "127.0.0.1" {
					t.Errorf("expected default host=127.0.0.1, got %s", in.Host)
				}
				if in.Port != 7777 {
					t.Errorf("expected default port=7777, got %d", in.Port)
				}
			},
		},
		{
			name:    "valid scene_tree with options",
			input:   `{"action": "scene_tree", "max_depth": 5, "max_nodes": 100}`,
			wantErr: false,
			check: func(t *testing.T, in handlers.Input) {
				if in.Action != "scene_tree" {
					t.Errorf("expected action=scene_tree, got %s", in.Action)
				}
				if in.MaxDepth != 5 {
					t.Errorf("expected max_depth=5, got %d", in.MaxDepth)
				}
				if in.MaxNodes != 100 {
					t.Errorf("expected max_nodes=100, got %d", in.MaxNodes)
				}
			},
		},
		{
			name:    "valid node_inspect",
			input:   `{"action": "node_inspect", "node_path": "/root/Main/Player"}`,
			wantErr: false,
			check: func(t *testing.T, in handlers.Input) {
				if in.NodePath != "/root/Main/Player" {
					t.Errorf("expected node_path=/root/Main/Player, got %s", in.NodePath)
				}
			},
		},
		{
			name:    "custom host and port",
			input:   `{"action": "ping", "host": "localhost", "port": 8888}`,
			wantErr: false,
			check: func(t *testing.T, in handlers.Input) {
				if in.Host != "localhost" {
					t.Errorf("expected host=localhost, got %s", in.Host)
				}
				if in.Port != 8888 {
					t.Errorf("expected port=8888, got %d", in.Port)
				}
			},
		},
		{
			name:    "missing action",
			input:   `{"node_path": "/root/Main"}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			input:   `{invalid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, err := parseInput(strings.NewReader(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseInput() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(t, in)
			}
		})
	}
}

func TestValidateInput(t *testing.T) {
	tests := []struct {
		name    string
		input   handlers.Input
		wantErr bool
		errMsg  string
	}{
		{
			name:    "ping requires nothing",
			input:   handlers.Input{Action: "ping"},
			wantErr: false,
		},
		{
			name:    "scene_tree requires nothing",
			input:   handlers.Input{Action: "scene_tree"},
			wantErr: false,
		},
		{
			name:    "node_inspect requires node_path",
			input:   handlers.Input{Action: "node_inspect"},
			wantErr: true,
			errMsg:  "node_path is required",
		},
		{
			name:    "node_inspect with node_path",
			input:   handlers.Input{Action: "node_inspect", NodePath: "/root/Main"},
			wantErr: false,
		},
		{
			name:    "node_create requires parent_path",
			input:   handlers.Input{Action: "node_create"},
			wantErr: true,
			errMsg:  "parent_path is required",
		},
		{
			name:    "node_create requires node_type",
			input:   handlers.Input{Action: "node_create", ParentPath: "/root"},
			wantErr: true,
			errMsg:  "node_type is required",
		},
		{
			name:    "node_create requires node_name",
			input:   handlers.Input{Action: "node_create", ParentPath: "/root", NodeType: "Node2D"},
			wantErr: true,
			errMsg:  "node_name is required",
		},
		{
			name:    "node_create with all fields",
			input:   handlers.Input{Action: "node_create", ParentPath: "/root", NodeType: "Node2D", NodeName: "Enemy"},
			wantErr: false,
		},
		{
			name:    "node_set_prop requires node_path",
			input:   handlers.Input{Action: "node_set_prop"},
			wantErr: true,
			errMsg:  "node_path is required",
		},
		{
			name:    "node_set_prop requires property",
			input:   handlers.Input{Action: "node_set_prop", NodePath: "/root/Main"},
			wantErr: true,
			errMsg:  "property is required",
		},
		{
			name:    "node_set_prop with all fields",
			input:   handlers.Input{Action: "node_set_prop", NodePath: "/root/Main", Property: "position", Value: "Vector2(0, 0)"},
			wantErr: false,
		},
		{
			name:    "signal_connect requires all fields",
			input:   handlers.Input{Action: "signal_connect", NodePath: "/root/A", SignalName: "pressed"},
			wantErr: true,
			errMsg:  "target_path is required",
		},
		{
			name:    "unknown action",
			input:   handlers.Input{Action: "unknown_action"},
			wantErr: true,
			errMsg:  "unknown action",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, ok := handlers.GetHandler(tt.input.Action)
			if !ok {
				if tt.wantErr && tt.errMsg == "unknown action" {
					return // Expected: unknown action
				}
				t.Fatalf("no handler for action %q", tt.input.Action)
			}
			err := handler.Validate(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			}
		})
	}
}

func TestBuildParams(t *testing.T) {
	tests := []struct {
		name   string
		input  handlers.Input
		expect map[string]any
	}{
		{
			name:   "ping has empty params",
			input:  handlers.Input{Action: "ping"},
			expect: map[string]any{},
		},
		{
			name:  "scene_tree includes max_depth and max_nodes",
			input: handlers.Input{Action: "scene_tree", MaxDepth: 5, MaxNodes: 100},
			expect: map[string]any{
				"max_depth": 5,
				"max_nodes": 100,
			},
		},
		{
			name:  "node_inspect includes node_path",
			input: handlers.Input{Action: "node_inspect", NodePath: "/root/Main"},
			expect: map[string]any{
				"node_path": "/root/Main",
			},
		},
		{
			name:  "node_create includes parent_path, type, name",
			input: handlers.Input{Action: "node_create", ParentPath: "/root", NodeType: "Node2D", NodeName: "Enemy"},
			expect: map[string]any{
				"parent_path": "/root",
				"type":        "Node2D",
				"name":        "Enemy",
			},
		},
		{
			name:  "node_set_prop includes node_path, property, value",
			input: handlers.Input{Action: "node_set_prop", NodePath: "/root/Main", Property: "position", Value: "Vector2(0, 0)"},
			expect: map[string]any{
				"node_path": "/root/Main",
				"property":  "position",
				"value":     "Vector2(0, 0)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, ok := handlers.GetHandler(tt.input.Action)
			if !ok {
				t.Fatalf("no handler for action %q", tt.input.Action)
			}
			params := handler.BuildParams(tt.input)
			for k, v := range tt.expect {
				if params[k] != v {
					t.Errorf("expected params[%s]=%v, got %v", k, v, params[k])
				}
			}
		})
	}
}

func TestGenerateSummary(t *testing.T) {
	tests := []struct {
		name    string
		action  string
		data    any
		contain string
	}{
		{
			name:    "ping with project_root",
			action:  "ping",
			data:    map[string]any{"project_root": "/path/to/project"},
			contain: "/path/to/project",
		},
		{
			name:    "scene_tree with node_count",
			action:  "scene_tree",
			data:    map[string]any{"node_count": float64(42)},
			contain: "42 nodes",
		},
		{
			name:    "node_inspect with name and type",
			action:  "node_inspect",
			data:    map[string]any{"name": "Player", "type": "CharacterBody2D"},
			contain: "Player",
		},
		{
			name:    "node_create with path",
			action:  "node_create",
			data:    map[string]any{"created_path": "/root/Main/Enemy"},
			contain: "/root/Main/Enemy",
		},
		{
			name:    "errors with entries",
			action:  "errors",
			data:    map[string]any{"entries": []any{map[string]any{}, map[string]any{}, map[string]any{}}},
			contain: "3 error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, ok := handlers.GetHandler(tt.action)
			if !ok {
				t.Fatalf("no handler for action %q", tt.action)
			}
			summary := handler.GenerateSummary(tt.action, tt.data)
			if !strings.Contains(summary, tt.contain) {
				t.Errorf("expected summary to contain %q, got %q", tt.contain, summary)
			}
		})
	}
}

func TestPluginRequestJSON(t *testing.T) {
	req := PluginRequest{
		WorkspaceRoot: "/path/to/workspace",
		Action:        "scene_tree",
		Params: map[string]any{
			"max_depth": 10,
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded["workspace_root"] != "/path/to/workspace" {
		t.Errorf("expected workspace_root=/path/to/workspace")
	}
	if decoded["action"] != "scene_tree" {
		t.Errorf("expected action=scene_tree")
	}
}

func TestPluginResponseParsing(t *testing.T) {
	successJSON := `{"status": "success", "data": {"pong": true}, "error": null}`
	errorJSON := `{"status": "error", "data": null, "error": {"code": "ETEST", "message": "test error", "hint": "fix it"}}`

	var success PluginResponse
	if err := json.NewDecoder(bytes.NewReader([]byte(successJSON))).Decode(&success); err != nil {
		t.Fatalf("failed to decode success: %v", err)
	}
	if success.Status != "success" {
		t.Errorf("expected status=success")
	}

	var errResp PluginResponse
	if err := json.NewDecoder(bytes.NewReader([]byte(errorJSON))).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error: %v", err)
	}
	if errResp.Status != "error" {
		t.Errorf("expected status=error")
	}
	if errResp.Error == nil {
		t.Fatal("expected error field to be non-nil")
	}
	if errResp.Error.Code != "ETEST" {
		t.Errorf("expected error code=ETEST, got %s", errResp.Error.Code)
	}
}
