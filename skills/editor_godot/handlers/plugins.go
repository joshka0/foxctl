package handlers

import (
	"fmt"
	"strings"
)

// PluginsHandler handles plugin actions: plugin_list, plugin_enable, plugin_disable.
type PluginsHandler struct{}

func init() {
	h := &PluginsHandler{}
	Register(ActionPluginList, h)
	Register(ActionPluginEnable, h)
	Register(ActionPluginDisable, h)
}

func (h *PluginsHandler) Validate(in Input) error {
	switch in.Action {
	case ActionPluginList:
		// No required fields
	case ActionPluginEnable, ActionPluginDisable:
		if strings.TrimSpace(in.PluginName) == "" {
			return fmt.Errorf("plugin_name is required for action %q", in.Action)
		}
	}
	return nil
}

func (h *PluginsHandler) BuildParams(in Input) map[string]any {
	params := make(map[string]any)

	switch in.Action {
	case ActionPluginEnable, ActionPluginDisable:
		params["name"] = in.PluginName
	}

	return params
}

func (h *PluginsHandler) GenerateSummary(action string, data any) string {
	m, _ := data.(map[string]any)

	switch action {
	case ActionPluginList:
		if m != nil {
			if plugins, ok := m["plugins"].([]any); ok {
				return fmt.Sprintf("Found %d plugin(s)", len(plugins))
			}
		}
		return "Listed plugins"

	case ActionPluginEnable:
		if m != nil {
			name, _ := m["name"].(string)
			return fmt.Sprintf("Enabled plugin '%s'", name)
		}
		return "Enabled plugin"

	case ActionPluginDisable:
		if m != nil {
			name, _ := m["name"].(string)
			return fmt.Sprintf("Disabled plugin '%s'", name)
		}
		return "Disabled plugin"

	default:
		return fmt.Sprintf("Completed action: %s", action)
	}
}
