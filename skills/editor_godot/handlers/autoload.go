package handlers

import (
	"fmt"
	"strings"
)

// AutoloadHandler handles autoload actions: autoload_list, autoload_add, autoload_remove.
type AutoloadHandler struct{}

func init() {
	h := &AutoloadHandler{}
	Register(ActionAutoloadList, h)
	Register(ActionAutoloadAdd, h)
	Register(ActionAutoloadRemove, h)
}

func (h *AutoloadHandler) Validate(in Input) error {
	switch in.Action {
	case ActionAutoloadList:
		// No required fields
	case ActionAutoloadAdd:
		if strings.TrimSpace(in.AutoloadName) == "" {
			return fmt.Errorf("autoload_name is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.ScriptPath) == "" {
			return fmt.Errorf("script_path is required for action %q", in.Action)
		}
	case ActionAutoloadRemove:
		if strings.TrimSpace(in.AutoloadName) == "" {
			return fmt.Errorf("autoload_name is required for action %q", in.Action)
		}
	}
	return nil
}

func (h *AutoloadHandler) BuildParams(in Input) map[string]any {
	params := make(map[string]any)

	switch in.Action {
	case ActionAutoloadAdd:
		params["name"] = in.AutoloadName
		params["path"] = in.ScriptPath
		params["enabled"] = in.Enabled
	case ActionAutoloadRemove:
		params["name"] = in.AutoloadName
	}

	return params
}

func (h *AutoloadHandler) GenerateSummary(action string, data any) string {
	m, _ := data.(map[string]any)

	switch action {
	case ActionAutoloadList:
		if m != nil {
			if autoloads, ok := m["autoloads"].([]any); ok {
				return fmt.Sprintf("Found %d autoload(s)", len(autoloads))
			}
		}
		return "Listed autoloads"

	case ActionAutoloadAdd:
		if m != nil {
			name, _ := m["name"].(string)
			return fmt.Sprintf("Added autoload '%s'", name)
		}
		return "Added autoload"

	case ActionAutoloadRemove:
		if m != nil {
			name, _ := m["name"].(string)
			return fmt.Sprintf("Removed autoload '%s'", name)
		}
		return "Removed autoload"

	default:
		return fmt.Sprintf("Completed action: %s", action)
	}
}
