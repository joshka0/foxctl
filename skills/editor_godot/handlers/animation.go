package handlers

import (
	"fmt"
	"strings"
)

// AnimationHandler handles animation actions: animation_list, animation_play, animation_stop.
type AnimationHandler struct{}

func init() {
	h := &AnimationHandler{}
	Register(ActionAnimationList, h)
	Register(ActionAnimationPlay, h)
	Register(ActionAnimationStop, h)
}

func (h *AnimationHandler) Validate(in Input) error {
	switch in.Action {
	case ActionAnimationList:
		if strings.TrimSpace(in.NodePath) == "" {
			return fmt.Errorf("node_path is required for action %q", in.Action)
		}
	case ActionAnimationPlay:
		if strings.TrimSpace(in.NodePath) == "" {
			return fmt.Errorf("node_path is required for action %q", in.Action)
		}
		if strings.TrimSpace(in.AnimationName) == "" {
			return fmt.Errorf("animation_name is required for action %q", in.Action)
		}
	case ActionAnimationStop:
		if strings.TrimSpace(in.NodePath) == "" {
			return fmt.Errorf("node_path is required for action %q", in.Action)
		}
	}
	return nil
}

func (h *AnimationHandler) BuildParams(in Input) map[string]any {
	params := make(map[string]any)

	params["node_path"] = in.NodePath

	switch in.Action {
	case ActionAnimationPlay:
		params["animation_name"] = in.AnimationName
		if in.Loop {
			params["loop"] = true
		}
		if in.FromPosition > 0 {
			params["from_position"] = in.FromPosition
		}
	}

	return params
}

func (h *AnimationHandler) GenerateSummary(action string, data any) string {
	m, _ := data.(map[string]any)

	switch action {
	case ActionAnimationList:
		if m != nil {
			if animations, ok := m["animations"].([]any); ok {
				return fmt.Sprintf("Found %d animation(s)", len(animations))
			}
		}
		return "Listed animations"

	case ActionAnimationPlay:
		if m != nil {
			animName, _ := m["animation_name"].(string)
			return fmt.Sprintf("Playing animation '%s'", animName)
		}
		return "Animation playing"

	case ActionAnimationStop:
		return "Animation stopped"

	default:
		return fmt.Sprintf("Completed action: %s", action)
	}
}
