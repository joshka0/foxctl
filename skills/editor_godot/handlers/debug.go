package handlers

import "fmt"

// DebugHandler handles debug actions: debug_draw_enable, debug_draw_disable.
type DebugHandler struct{}

func init() {
	h := &DebugHandler{}
	Register(ActionDebugDrawEnable, h)
	Register(ActionDebugDrawDisable, h)
}

func (h *DebugHandler) Validate(in Input) error {
	// No required fields - debug_mode defaults to "wireframe"
	return nil
}

func (h *DebugHandler) BuildParams(in Input) map[string]any {
	params := make(map[string]any)

	if in.Action == ActionDebugDrawEnable {
		mode := in.DebugMode
		if mode == "" {
			mode = "wireframe"
		}
		params["mode"] = mode
	}

	return params
}

func (h *DebugHandler) GenerateSummary(action string, data any) string {
	m, _ := data.(map[string]any)

	switch action {
	case ActionDebugDrawEnable:
		if m != nil {
			mode, _ := m["mode"].(string)
			return fmt.Sprintf("Enabled debug draw mode: %s", mode)
		}
		return "Enabled debug draw"

	case ActionDebugDrawDisable:
		return "Disabled debug draw"

	default:
		return fmt.Sprintf("Completed action: %s", action)
	}
}
