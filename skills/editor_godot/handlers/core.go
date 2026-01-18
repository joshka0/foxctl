package handlers

import (
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
)

// CoreHandler handles core actions: ping, scene_tree, node_*, signal_connect, etc.
type CoreHandler struct{}

func init() {
	h := &CoreHandler{}
	Register(ActionPing, h)
	Register(ActionSceneTree, h)
	Register(ActionNodeInspect, h)
	Register(ActionNodeCreate, h)
	Register(ActionNodeDelete, h)
	Register(ActionNodeRename, h)
	Register(ActionNodeReparent, h)
	Register(ActionNodeSetProp, h)
	Register(ActionNodeAttachScript, h)
	Register(ActionSignalConnect, h)
	Register(ActionClassInfo, h)
	Register(ActionEnsureNode, h)
	Register(ActionSearchNodes, h)
	Register(ActionFocusNode, h)
	Register(ActionSelectionState, h)
	Register(ActionRunGame, h)
	Register(ActionRunScene, h)
	Register(ActionStopGame, h)
	Register(ActionErrors, h)
}

func (h *CoreHandler) Validate(in Input) error {
	switch in.Action {
	case ActionPing, ActionSceneTree, ActionRunGame, ActionStopGame, ActionSelectionState:
		// No additional required fields
	case ActionErrors:
		// Optional error_limit already has default
	case ActionNodeInspect:
		if strings.TrimSpace(in.NodePath) == "" {
			return skillerr.Arg(fmt.Sprintf("node_path is required for action %q", in.Action))
		}
	case ActionNodeCreate:
		if strings.TrimSpace(in.ParentPath) == "" {
			return skillerr.Arg(fmt.Sprintf("parent_path is required for action %q", in.Action))
		}
		if strings.TrimSpace(in.NodeType) == "" {
			return skillerr.Arg(fmt.Sprintf("node_type is required for action %q", in.Action))
		}
		if strings.TrimSpace(in.NodeName) == "" {
			return skillerr.Arg(fmt.Sprintf("node_name is required for action %q", in.Action))
		}
	case ActionNodeSetProp:
		if strings.TrimSpace(in.NodePath) == "" {
			return skillerr.Arg(fmt.Sprintf("node_path is required for action %q", in.Action))
		}
		if strings.TrimSpace(in.Property) == "" {
			return skillerr.Arg(fmt.Sprintf("property is required for action %q", in.Action))
		}
	case ActionNodeAttachScript:
		if strings.TrimSpace(in.NodePath) == "" {
			return skillerr.Arg(fmt.Sprintf("node_path is required for action %q", in.Action))
		}
		if strings.TrimSpace(in.ScriptPath) == "" {
			return skillerr.Arg(fmt.Sprintf("script_path is required for action %q", in.Action))
		}
	case ActionSignalConnect:
		if strings.TrimSpace(in.NodePath) == "" {
			return skillerr.Arg(fmt.Sprintf("node_path (source) is required for action %q", in.Action))
		}
		if strings.TrimSpace(in.SignalName) == "" {
			return skillerr.Arg(fmt.Sprintf("signal_name is required for action %q", in.Action))
		}
		if strings.TrimSpace(in.TargetPath) == "" {
			return skillerr.Arg(fmt.Sprintf("target_path is required for action %q", in.Action))
		}
		if strings.TrimSpace(in.MethodName) == "" {
			return skillerr.Arg(fmt.Sprintf("method_name is required for action %q", in.Action))
		}
	case ActionNodeDelete:
		if strings.TrimSpace(in.NodePath) == "" {
			return skillerr.Arg(fmt.Sprintf("node_path is required for action %q", in.Action))
		}
	case ActionNodeRename:
		if strings.TrimSpace(in.NodePath) == "" {
			return skillerr.Arg(fmt.Sprintf("node_path is required for action %q", in.Action))
		}
		if strings.TrimSpace(in.NewName) == "" {
			return skillerr.Arg(fmt.Sprintf("new_name is required for action %q", in.Action))
		}
	case ActionNodeReparent:
		if strings.TrimSpace(in.NodePath) == "" {
			return skillerr.Arg(fmt.Sprintf("node_path is required for action %q", in.Action))
		}
		if strings.TrimSpace(in.NewParentPath) == "" {
			return skillerr.Arg(fmt.Sprintf("new_parent_path is required for action %q", in.Action))
		}
	case ActionClassInfo:
		if strings.TrimSpace(in.ClassName) == "" {
			return skillerr.Arg(fmt.Sprintf("class_name is required for action %q", in.Action))
		}
	case ActionEnsureNode:
		if strings.TrimSpace(in.NodePath) == "" {
			return skillerr.Arg(fmt.Sprintf("node_path is required for action %q", in.Action))
		}
		if strings.TrimSpace(in.NodeType) == "" {
			return skillerr.Arg(fmt.Sprintf("node_type is required for action %q", in.Action))
		}
	case ActionSearchNodes:
		// All filters are optional
	case ActionFocusNode:
		if strings.TrimSpace(in.NodePath) == "" {
			return skillerr.Arg(fmt.Sprintf("node_path is required for action %q", in.Action))
		}
	case ActionRunScene:
		if strings.TrimSpace(in.ScenePath) == "" {
			return skillerr.Arg(fmt.Sprintf("scene_path is required for action %q", in.Action))
		}
	}
	return nil
}

func (h *CoreHandler) BuildParams(in Input) map[string]any {
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
		if in.DryRun {
			params["dry_run"] = true
		}
	case ActionNodeDelete:
		params["node_path"] = in.NodePath
		if in.DryRun {
			params["dry_run"] = true
		}
	case ActionNodeRename:
		params["node_path"] = in.NodePath
		params["new_name"] = in.NewName
		if in.DryRun {
			params["dry_run"] = true
		}
	case ActionNodeReparent:
		params["node_path"] = in.NodePath
		params["new_parent_path"] = in.NewParentPath
		if in.DryRun {
			params["dry_run"] = true
		}
	case ActionNodeSetProp:
		params["node_path"] = in.NodePath
		params["property"] = in.Property
		params["value"] = in.Value
		if in.DryRun {
			params["dry_run"] = true
		}
	case ActionNodeAttachScript:
		params["node_path"] = in.NodePath
		params["script_path"] = in.ScriptPath
		if in.DryRun {
			params["dry_run"] = true
		}
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
		if in.DryRun {
			params["dry_run"] = true
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
	case ActionRunScene:
		params["path"] = in.ScenePath
	case ActionErrors:
		params["limit"] = in.ErrorLimit
	}

	return params
}

func (h *CoreHandler) GenerateSummary(action string, data any) string {
	m, _ := data.(map[string]any)

	switch action {
	case ActionPing:
		if m != nil {
			if root, ok := m["project_root"].(string); ok {
				return fmt.Sprintf("Connected to Godot project at %s", root)
			}
		}
		return "Connected to GodotAIBridge"

	case ActionSceneTree:
		if m != nil {
			if count, ok := m["node_count"].(float64); ok {
				return fmt.Sprintf("Scene tree with %d nodes", int(count))
			}
		}
		return "Retrieved scene tree"

	case ActionNodeInspect:
		if m != nil {
			name, _ := m["name"].(string)
			nodeType, _ := m["type"].(string)
			return fmt.Sprintf("Node %q (%s)", name, nodeType)
		}
		return "Retrieved node info"

	case ActionNodeCreate:
		if m != nil {
			path, _ := m["created_path"].(string)
			return fmt.Sprintf("Created node at %s", path)
		}
		return "Created node"

	case ActionNodeDelete:
		if m != nil {
			name, _ := m["name"].(string)
			return fmt.Sprintf("Deleted node %q", name)
		}
		return "Deleted node"

	case ActionNodeRename:
		if m != nil {
			oldName, _ := m["old_name"].(string)
			newName, _ := m["new_name"].(string)
			return fmt.Sprintf("Renamed %q to %q", oldName, newName)
		}
		return "Renamed node"

	case ActionNodeReparent:
		if m != nil {
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
		if m != nil {
			className, _ := m["class_name"].(string)
			return fmt.Sprintf("Class info for %s", className)
		}
		return "Retrieved class info"

	case ActionEnsureNode:
		if m != nil {
			status, _ := m["status"].(string)
			path, _ := m["path"].(string)
			created, _ := m["created"].(bool)
			if created {
				return fmt.Sprintf("Created node at %s", path)
			}
			return fmt.Sprintf("Node exists at %s (status: %s)", path, status)
		}
		return "Ensured node"

	case ActionSearchNodes:
		if m != nil {
			count, _ := m["count"].(float64)
			return fmt.Sprintf("Found %d matching node(s)", int(count))
		}
		return "Searched nodes"

	case ActionFocusNode:
		if m != nil {
			selectedPath, _ := m["selected_path"].(string)
			return fmt.Sprintf("Focused on %s", selectedPath)
		}
		return "Focused node"

	case ActionSelectionState:
		if m != nil {
			count, _ := m["selected_count"].(float64)
			return fmt.Sprintf("%d node(s) selected", int(count))
		}
		return "Retrieved selection state"

	case ActionRunGame:
		return "Game started"

	case ActionRunScene:
		if m != nil {
			scenePath, _ := m["scene_path"].(string)
			return fmt.Sprintf("Running scene %s", scenePath)
		}
		return "Scene started"

	case ActionStopGame:
		return "Game stopped"

	case ActionErrors:
		if m != nil {
			if entries, ok := m["entries"].([]any); ok {
				return fmt.Sprintf("Retrieved %d error(s)", len(entries))
			}
		}
		return "Retrieved errors"

	default:
		return fmt.Sprintf("Completed action: %s", action)
	}
}
