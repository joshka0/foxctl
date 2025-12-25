package handlers

import (
	"fmt"
	"strings"
)

// BuildHandler handles build actions: build.
type BuildHandler struct{}

func init() {
	h := &BuildHandler{}
	Register(ActionBuild, h)
}

func (h *BuildHandler) Validate(in Input) error {
	switch in.Action {
	case ActionBuild:
		if strings.TrimSpace(in.PresetName) == "" {
			return fmt.Errorf("preset_name is required for action %q", in.Action)
		}
	}
	return nil
}

func (h *BuildHandler) BuildParams(in Input) map[string]any {
	params := make(map[string]any)

	params["preset"] = in.PresetName
	if in.OutputPath != "" {
		params["output_path"] = in.OutputPath
	}
	if in.DryRun {
		params["dry_run"] = true
	}

	return params
}

func (h *BuildHandler) GenerateSummary(action string, data any) string {
	m, _ := data.(map[string]any)

	if m != nil {
		preset, _ := m["preset"].(string)
		outputPath, _ := m["output_path"].(string)
		if outputPath != "" {
			return fmt.Sprintf("Built project with preset '%s' to %s", preset, outputPath)
		}
		return fmt.Sprintf("Built project with preset '%s'", preset)
	}
	return "Project built"
}
