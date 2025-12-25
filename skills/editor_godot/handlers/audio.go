package handlers

import (
	"fmt"
	"strings"
)

// AudioHandler handles audio actions: audio_play, audio_stop.
type AudioHandler struct{}

func init() {
	h := &AudioHandler{}
	Register(ActionAudioPlay, h)
	Register(ActionAudioStop, h)
}

func (h *AudioHandler) Validate(in Input) error {
	switch in.Action {
	case ActionAudioPlay, ActionAudioStop:
		if strings.TrimSpace(in.NodePath) == "" {
			return fmt.Errorf("node_path is required for action %q", in.Action)
		}
	}
	return nil
}

func (h *AudioHandler) BuildParams(in Input) map[string]any {
	params := make(map[string]any)

	params["node_path"] = in.NodePath

	switch in.Action {
	case ActionAudioPlay:
		if in.FromPosition > 0 {
			params["from_position"] = in.FromPosition
		}
	}

	return params
}

func (h *AudioHandler) GenerateSummary(action string, data any) string {
	m, _ := data.(map[string]any)

	switch action {
	case ActionAudioPlay:
		if m != nil {
			nodePath, _ := m["node_path"].(string)
			return fmt.Sprintf("Playing audio on %s", nodePath)
		}
		return "Audio playing"

	case ActionAudioStop:
		if m != nil {
			nodePath, _ := m["node_path"].(string)
			return fmt.Sprintf("Stopped audio on %s", nodePath)
		}
		return "Audio stopped"

	default:
		return fmt.Sprintf("Completed action: %s", action)
	}
}
