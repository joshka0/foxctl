package handlers

import (
	"fmt"
	"strings"
)

// ShaderHandler handles shader actions: shader_create, shader_edit.
type ShaderHandler struct{}

func init() {
	h := &ShaderHandler{}
	Register(ActionShaderCreate, h)
	Register(ActionShaderEdit, h)
}

func (h *ShaderHandler) Validate(in Input) error {
	switch in.Action {
	case ActionShaderCreate:
		if strings.TrimSpace(in.ScriptPath) == "" {
			return fmt.Errorf("script_path (shader path) is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.ShaderType) == "" {
			return fmt.Errorf("shader_type is required for action %q", in.Action)
		}
	case ActionShaderEdit:
		if strings.TrimSpace(in.ScriptPath) == "" {
			return fmt.Errorf("script_path (shader path) is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.ShaderCode) == "" {
			return fmt.Errorf("shader_code is required for action %q", in.Action)
		}
	}
	return nil
}

func (h *ShaderHandler) BuildParams(in Input) map[string]any {
	params := make(map[string]any)

	params["path"] = in.ScriptPath

	switch in.Action {
	case ActionShaderCreate:
		params["shader_type"] = in.ShaderType
		params["overwrite"] = in.Overwrite
	case ActionShaderEdit:
		params["code"] = in.ShaderCode
	}

	return params
}

func (h *ShaderHandler) GenerateSummary(action string, data any) string {
	m, _ := data.(map[string]any)

	switch action {
	case ActionShaderCreate:
		if m != nil {
			path, _ := m["path"].(string)
			shaderType, _ := m["shader_type"].(string)
			return fmt.Sprintf("Created %s shader at %s", shaderType, path)
		}
		return "Created shader"

	case ActionShaderEdit:
		if m != nil {
			path, _ := m["path"].(string)
			return fmt.Sprintf("Edited shader %s", path)
		}
		return "Edited shader"

	default:
		return fmt.Sprintf("Completed action: %s", action)
	}
}
