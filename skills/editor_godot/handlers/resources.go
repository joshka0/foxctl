package handlers

import (
	"fmt"
	"strings"
)

// ResourcesHandler handles resource actions: scene_*, resource_*, camera_*.
type ResourcesHandler struct{}

func init() {
	h := &ResourcesHandler{}
	Register(ActionSceneSave, h)
	Register(ActionSceneList, h)
	Register(ActionSceneOpen, h)
	Register(ActionSceneInstance, h)
	Register(ActionResourceList, h)
	Register(ActionSearchResources, h)
	Register(ActionResourceReferences, h)
	Register(ActionCameraSave, h)
	Register(ActionCameraRestore, h)
	Register(ActionCameraList, h)
}

func (h *ResourcesHandler) Validate(in Input) error {
	switch in.Action {
	case ActionSceneSave, ActionSceneList, ActionCameraList:
		// No additional required fields
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
	case ActionResourceList:
		// Optional path, pattern, max_results
	case ActionSearchResources:
		if strings.TrimSpace(in.ResourceType) == "" {
			return fmt.Errorf("resource_type is required for action %q", in.Action)
		}
	case ActionResourceReferences:
		if strings.TrimSpace(in.ResourcePath) == "" {
			return fmt.Errorf("resource_path is required for action %q", in.Action)
		}
	case ActionCameraSave, ActionCameraRestore:
		if strings.TrimSpace(in.BookmarkName) == "" {
			return fmt.Errorf("bookmark_name is required for action %q", in.Action)
		}
	}
	return nil
}

func (h *ResourcesHandler) BuildParams(in Input) map[string]any {
	params := make(map[string]any)

	switch in.Action {
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
		if in.DryRun {
			params["dry_run"] = true
		}
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
	case ActionSearchResources:
		params["type"] = in.ResourceType
		if in.ResourcePath != "" {
			params["path"] = in.ResourcePath
		}
		if in.SearchName != "" {
			params["name"] = in.SearchName
		}
		if in.MaxResults > 0 {
			params["max_results"] = in.MaxResults
		}
	case ActionResourceReferences:
		params["path"] = in.ResourcePath
		if in.MaxResults > 0 {
			params["max_results"] = in.MaxResults
		}
	case ActionCameraSave, ActionCameraRestore:
		params["name"] = in.BookmarkName
	}

	return params
}

func (h *ResourcesHandler) GenerateSummary(action string, data any) string {
	m, _ := data.(map[string]any)

	switch action {
	case ActionSceneSave:
		if m != nil {
			scenePath, _ := m["scene_path"].(string)
			return fmt.Sprintf("Saved scene to %s", scenePath)
		}
		return "Scene saved"

	case ActionSceneList:
		if m != nil {
			count, _ := m["count"].(float64)
			return fmt.Sprintf("Found %d scene(s)", int(count))
		}
		return "Listed scenes"

	case ActionSceneOpen:
		if m != nil {
			scenePath, _ := m["scene_path"].(string)
			return fmt.Sprintf("Opened scene %s", scenePath)
		}
		return "Opened scene"

	case ActionSceneInstance:
		if m != nil {
			instancePath, _ := m["instance_path"].(string)
			return fmt.Sprintf("Instanced scene at %s", instancePath)
		}
		return "Instanced scene"

	case ActionResourceList:
		if m != nil {
			files, _ := m["files"].([]any)
			dirs, _ := m["directories"].([]any)
			return fmt.Sprintf("Found %d files, %d directories", len(files), len(dirs))
		}
		return "Listed resources"

	case ActionSearchResources:
		if m != nil {
			count, _ := m["count"].(float64)
			return fmt.Sprintf("Found %d resource(s)", int(count))
		}
		return "Searched resources"

	case ActionResourceReferences:
		if m != nil {
			count, _ := m["count"].(float64)
			return fmt.Sprintf("Found %d reference(s)", int(count))
		}
		return "Found references"

	case ActionCameraSave:
		if m != nil {
			name, _ := m["name"].(string)
			return fmt.Sprintf("Saved camera bookmark '%s'", name)
		}
		return "Saved camera bookmark"

	case ActionCameraRestore:
		if m != nil {
			name, _ := m["name"].(string)
			return fmt.Sprintf("Restored camera bookmark '%s'", name)
		}
		return "Restored camera bookmark"

	case ActionCameraList:
		if m != nil {
			count, _ := m["count"].(float64)
			return fmt.Sprintf("%d camera bookmark(s)", int(count))
		}
		return "Listed camera bookmarks"

	default:
		return fmt.Sprintf("Completed action: %s", action)
	}
}
