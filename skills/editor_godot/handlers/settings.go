package handlers

import (
	"fmt"
	"strings"
)

// SettingsHandler handles settings actions: project_setting_get, project_setting_set.
type SettingsHandler struct{}

func init() {
	h := &SettingsHandler{}
	Register(ActionProjectSettingGet, h)
	Register(ActionProjectSettingSet, h)
}

func (h *SettingsHandler) Validate(in Input) error {
	switch in.Action {
	case ActionProjectSettingGet:
		if strings.TrimSpace(in.SettingName) == "" {
			return fmt.Errorf("setting_name is required for action %q", in.Action)
		}
	case ActionProjectSettingSet:
		if strings.TrimSpace(in.SettingName) == "" {
			return fmt.Errorf("setting_name is required for action %q", in.Action)
		}
		// setting_value can be nil/null to clear a setting
	}
	return nil
}

func (h *SettingsHandler) BuildParams(in Input) map[string]any {
	params := make(map[string]any)

	switch in.Action {
	case ActionProjectSettingGet:
		params["name"] = in.SettingName
	case ActionProjectSettingSet:
		params["name"] = in.SettingName
		params["value"] = in.SettingValue
	}

	return params
}

func (h *SettingsHandler) GenerateSummary(action string, data any) string {
	m, _ := data.(map[string]any)

	switch action {
	case ActionProjectSettingGet:
		if m != nil {
			name, _ := m["name"].(string)
			return fmt.Sprintf("Retrieved setting '%s'", name)
		}
		return "Retrieved project setting"

	case ActionProjectSettingSet:
		if m != nil {
			name, _ := m["name"].(string)
			return fmt.Sprintf("Set setting '%s'", name)
		}
		return "Set project setting"

	default:
		return fmt.Sprintf("Completed action: %s", action)
	}
}
