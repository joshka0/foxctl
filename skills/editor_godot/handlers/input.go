package handlers

import (
	"fmt"
	"strings"
)

// InputHandler handles input actions: input_action_list, input_action_add, input_action_remove.
type InputHandler struct{}

func init() {
	h := &InputHandler{}
	Register(ActionInputActionList, h)
	Register(ActionInputActionAdd, h)
	Register(ActionInputActionRemove, h)
}

func (h *InputHandler) Validate(in Input) error {
	switch in.Action {
	case ActionInputActionList:
		// No required fields
	case ActionInputActionAdd:
		if strings.TrimSpace(in.SearchName) == "" {
			return fmt.Errorf("name (action name) is required for action %q", in.Action)
		}
		if in.InputEvent == nil {
			return fmt.Errorf("input_event is required for action %q", in.Action)
		}
	case ActionInputActionRemove:
		if strings.TrimSpace(in.SearchName) == "" {
			return fmt.Errorf("name (action name) is required for action %q", in.Action)
		}
	}
	return nil
}

func (h *InputHandler) BuildParams(in Input) map[string]any {
	params := make(map[string]any)

	switch in.Action {
	case ActionInputActionAdd:
		params["name"] = in.SearchName
		params["event"] = in.InputEvent
	case ActionInputActionRemove:
		params["name"] = in.SearchName
	}

	return params
}

func (h *InputHandler) GenerateSummary(action string, data any) string {
	m, _ := data.(map[string]any)

	switch action {
	case ActionInputActionList:
		if m != nil {
			if actions, ok := m["actions"].([]any); ok {
				return fmt.Sprintf("Found %d input action(s)", len(actions))
			}
		}
		return "Listed input actions"

	case ActionInputActionAdd:
		if m != nil {
			name, _ := m["name"].(string)
			return fmt.Sprintf("Added input action '%s'", name)
		}
		return "Added input action"

	case ActionInputActionRemove:
		if m != nil {
			name, _ := m["name"].(string)
			return fmt.Sprintf("Removed input action '%s'", name)
		}
		return "Removed input action"

	default:
		return fmt.Sprintf("Completed action: %s", action)
	}
}
