package handlers

import (
	"fmt"
	"strings"
)

// ScriptsHandler handles script actions: script_create, script_read, script_edit.
type ScriptsHandler struct{}

func init() {
	h := &ScriptsHandler{}
	Register(ActionScriptCreate, h)
	Register(ActionScriptRead, h)
	Register(ActionScriptEdit, h)
}

func (h *ScriptsHandler) Validate(in Input) error {
	switch in.Action {
	case ActionScriptCreate:
		if strings.TrimSpace(in.ScriptPath) == "" {
			return fmt.Errorf("script_path is required for action %q", in.Action)
		}
	case ActionScriptRead:
		if strings.TrimSpace(in.ScriptPath) == "" {
			return fmt.Errorf("script_path is required for action %q", in.Action)
		}
	case ActionScriptEdit:
		if strings.TrimSpace(in.ScriptPath) == "" {
			return fmt.Errorf("script_path is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.Content) == "" {
			return fmt.Errorf("content is required for action %q", in.Action)
		}
	}
	return nil
}

func (h *ScriptsHandler) BuildParams(in Input) map[string]any {
	params := make(map[string]any)

	switch in.Action {
	case ActionScriptCreate:
		params["path"] = in.ScriptPath
		if in.ExtendsClass != "" {
			params["extends"] = in.ExtendsClass
		}
		if len(in.Exports) > 0 {
			params["exports"] = in.Exports
		}
		if len(in.Methods) > 0 {
			params["methods"] = in.Methods
		}
		if len(in.Signals) > 0 {
			params["signals"] = in.Signals
		}
		params["overwrite"] = in.Overwrite
	case ActionScriptRead:
		params["path"] = in.ScriptPath
	case ActionScriptEdit:
		params["path"] = in.ScriptPath
		params["content"] = in.Content
		if in.StartLine > 0 {
			params["start_line"] = in.StartLine
		}
		if in.EndLine > 0 {
			params["end_line"] = in.EndLine
		}
	}

	return params
}

func (h *ScriptsHandler) GenerateSummary(action string, data any) string {
	m, _ := data.(map[string]any)

	switch action {
	case ActionScriptCreate:
		if m != nil {
			path, _ := m["path"].(string)
			return fmt.Sprintf("Created script %s", path)
		}
		return "Created script"

	case ActionScriptRead:
		if m != nil {
			path, _ := m["path"].(string)
			lineCount, _ := m["line_count"].(float64)
			return fmt.Sprintf("Read script %s (%d lines)", path, int(lineCount))
		}
		return "Read script"

	case ActionScriptEdit:
		if m != nil {
			path, _ := m["path"].(string)
			return fmt.Sprintf("Edited script %s", path)
		}
		return "Edited script"

	default:
		return fmt.Sprintf("Completed action: %s", action)
	}
}
