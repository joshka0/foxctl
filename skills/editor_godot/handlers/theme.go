package handlers

import (
	"fmt"
	"strings"
)

// ThemeHandler handles theme actions: theme_get, theme_set.
type ThemeHandler struct{}

func init() {
	h := &ThemeHandler{}
	Register(ActionThemeGet, h)
	Register(ActionThemeSet, h)
}

func (h *ThemeHandler) Validate(in Input) error {
	switch in.Action {
	case ActionThemeGet:
		if strings.TrimSpace(in.NodePath) == "" {
			return fmt.Errorf("node_path is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.ThemeName) == "" {
			return fmt.Errorf("theme_name (item name) is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.ThemeType) == "" {
			return fmt.Errorf("theme_type is required for action %q", in.Action)
		}
	case ActionThemeSet:
		if strings.TrimSpace(in.NodePath) == "" {
			return fmt.Errorf("node_path is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.ThemeName) == "" {
			return fmt.Errorf("theme_name (item name) is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.ThemeType) == "" {
			return fmt.Errorf("theme_type is required for action %q", in.Action)
		}
	}
	return nil
}

func (h *ThemeHandler) BuildParams(in Input) map[string]any {
	params := make(map[string]any)

	params["node_path"] = in.NodePath
	params["name"] = in.ThemeName
	params["type"] = in.ThemeType

	if in.Action == ActionThemeSet {
		params["value"] = in.ThemeValue
	}

	return params
}

func (h *ThemeHandler) GenerateSummary(action string, data any) string {
	m, _ := data.(map[string]any)

	switch action {
	case ActionThemeGet:
		if m != nil {
			name, _ := m["name"].(string)
			themeType, _ := m["type"].(string)
			return fmt.Sprintf("Retrieved theme %s '%s'", themeType, name)
		}
		return "Retrieved theme value"

	case ActionThemeSet:
		if m != nil {
			name, _ := m["name"].(string)
			themeType, _ := m["type"].(string)
			return fmt.Sprintf("Set theme %s '%s'", themeType, name)
		}
		return "Set theme value"

	default:
		return fmt.Sprintf("Completed action: %s", action)
	}
}
