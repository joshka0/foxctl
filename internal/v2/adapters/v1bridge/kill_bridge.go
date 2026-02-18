package v1bridge

import (
	"context"
	stderrors "errors"
	"strings"

	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
)

// LegacyKiller is the minimal legacy kill contract.
type LegacyKiller interface {
	Kill(sessionID string) error
}

// KillBridge adapts a legacy killer to the v2 kill dependency shape.
type KillBridge struct {
	legacy LegacyKiller
}

// NewKillBridge creates a legacy kill bridge.
func NewKillBridge(legacy LegacyKiller) *KillBridge {
	return &KillBridge{legacy: legacy}
}

// Kill invokes the legacy killer after basic v2 validation.
func (b *KillBridge) Kill(ctx context.Context, runID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b == nil || b.legacy == nil {
		return &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "legacy killer is not configured",
			Fatal:   true,
		}
	}
	target := strings.TrimSpace(runID)
	if target == "" {
		return &v2errors.V2Error{
			Kind:    v2errors.ErrValidation,
			Message: "run_id is required",
			Fatal:   true,
		}
	}
	if err := b.legacy.Kill(target); err != nil {
		var verr *v2errors.V2Error
		if stderrors.As(err, &verr) {
			return verr
		}
		return &v2errors.V2Error{
			Kind:      v2errors.ErrDependency,
			Message:   "legacy kill failed",
			Cause:     err,
			Fatal:     true,
			Retryable: true,
		}
	}
	return nil
}
