package handlers

import (
	"fmt"
	"strings"
)

// GroupsHandler handles group actions: group_add, group_remove, group_list.
type GroupsHandler struct{}

func init() {
	h := &GroupsHandler{}
	Register(ActionGroupAdd, h)
	Register(ActionGroupRemove, h)
	Register(ActionGroupList, h)
}

func (h *GroupsHandler) Validate(in Input) error {
	switch in.Action {
	case ActionGroupAdd, ActionGroupRemove:
		if strings.TrimSpace(in.NodePath) == "" {
			return fmt.Errorf("node_path is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.GroupName) == "" {
			return fmt.Errorf("group_name is required for action %q", in.Action)
		}
	case ActionGroupList:
		// node_path is optional - if provided, lists groups for that node
		// if not provided, lists all groups in the scene
	}
	return nil
}

func (h *GroupsHandler) BuildParams(in Input) map[string]any {
	params := make(map[string]any)

	switch in.Action {
	case ActionGroupAdd, ActionGroupRemove:
		params["node_path"] = in.NodePath
		params["group_name"] = in.GroupName
	case ActionGroupList:
		if in.NodePath != "" {
			params["node_path"] = in.NodePath
		}
	}

	return params
}

func (h *GroupsHandler) GenerateSummary(action string, data any) string {
	m, _ := data.(map[string]any)

	switch action {
	case ActionGroupAdd:
		if m != nil {
			groupName, _ := m["group_name"].(string)
			nodePath, _ := m["node_path"].(string)
			return fmt.Sprintf("Added node %s to group '%s'", nodePath, groupName)
		}
		return "Added node to group"

	case ActionGroupRemove:
		if m != nil {
			groupName, _ := m["group_name"].(string)
			nodePath, _ := m["node_path"].(string)
			return fmt.Sprintf("Removed node %s from group '%s'", nodePath, groupName)
		}
		return "Removed node from group"

	case ActionGroupList:
		if m != nil {
			if groups, ok := m["groups"].([]any); ok {
				return fmt.Sprintf("Found %d group(s)", len(groups))
			}
		}
		return "Listed groups"

	default:
		return fmt.Sprintf("Completed action: %s", action)
	}
}
