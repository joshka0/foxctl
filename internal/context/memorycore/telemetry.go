package memorycore

import (
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/storage"
)

type TelemetryAction string

const (
	TelemetryActionViewed    TelemetryAction = "viewed"
	TelemetryActionSelected  TelemetryAction = "selected"
	TelemetryActionUsed      TelemetryAction = "used"
	TelemetryActionSucceeded TelemetryAction = "succeeded"
	TelemetryActionFailed    TelemetryAction = "failed"
	TelemetryActionRestored  TelemetryAction = "restored"
	TelemetryActionPatched   TelemetryAction = "patched"
)

func (a TelemetryAction) IsValid() bool {
	switch a {
	case TelemetryActionViewed, TelemetryActionSelected, TelemetryActionUsed,
		TelemetryActionSucceeded, TelemetryActionFailed, TelemetryActionRestored,
		TelemetryActionPatched:
		return true
	default:
		return false
	}
}

func ParseTelemetryAction(raw string) (TelemetryAction, error) {
	action := TelemetryAction(strings.TrimSpace(raw))
	if !action.IsValid() {
		return "", fmt.Errorf("invalid memory telemetry action %q", raw)
	}
	return action, nil
}

func TelemetryUpdate(action TelemetryAction) (storage.MemoryTelemetryUpdate, error) {
	if !action.IsValid() {
		return storage.MemoryTelemetryUpdate{}, fmt.Errorf("invalid memory telemetry action %q", action)
	}
	return storage.MemoryTelemetryUpdate{Action: string(action)}, nil
}
