package ports

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	portconfig "github.com/jkatigb/agentctl/internal/v2/ports/config"
)

// Decision identifies which command path handled a routed request.
type Decision string

const (
	DecisionV1 Decision = "v1"
	DecisionV2 Decision = "v2"
)

// Observer receives per-dispatch routing metadata.
type Observer func(command string, decision Decision, correlationID string)

// ShadowReport captures parity information for a shadow execution.
type ShadowReport struct {
	Command         string
	CorrelationID   string
	PrimaryDecision Decision
	ShadowDecision  Decision
	Match           bool
	Reason          string
	PrimaryError    string
	ShadowError     string
	DurationMS      int64
}

// ShadowObserver receives shadow execution parity results.
type ShadowObserver func(report ShadowReport)

// Runner executes a command and returns one typed response.
type Runner[T any] func(ctx context.Context) (T, error)

// ShadowComparator compares primary and shadow outcomes.
type ShadowComparator[T any] func(primary T, primaryErr error, shadow T, shadowErr error) (match bool, reason string)

// DispatchOptions captures execution and shadow-routing inputs.
type DispatchOptions[T any] struct {
	Flags         portconfig.V2Flags
	ShadowFlags   portconfig.V2Flags
	Command       string
	CorrelationID string
	V1            Runner[T]
	V2            Runner[T]
	Observe       Observer
	ShadowObserve ShadowObserver
	Compare       ShadowComparator[T]
	ShadowTimeout time.Duration
}

// Dispatch selects v1/v2 execution based on flags and command.
func Dispatch[T any](
	ctx context.Context,
	flags portconfig.V2Flags,
	command string,
	correlationID string,
	v1 Runner[T],
	v2 Runner[T],
	observe Observer,
) (T, Decision, error) {
	return DispatchWithShadow(ctx, DispatchOptions[T]{
		Flags:         flags,
		Command:       command,
		CorrelationID: correlationID,
		V1:            v1,
		V2:            v2,
		Observe:       observe,
	})
}

// DispatchWithShadow selects v1/v2 execution and optionally runs a non-blocking shadow path.
func DispatchWithShadow[T any](ctx context.Context, opts DispatchOptions[T]) (T, Decision, error) {
	var zero T
	cmd := strings.ToLower(strings.TrimSpace(opts.Command))
	if cmd == "" {
		return zero, DecisionV1, &v2errors.V2Error{
			Kind:    v2errors.ErrValidation,
			Message: "command is required",
			Fatal:   true,
		}
	}

	decision := DecisionV1
	exec := opts.V1
	if opts.Flags.Enabled(cmd) {
		decision = DecisionV2
		exec = opts.V2
	}

	if opts.Observe != nil {
		opts.Observe(cmd, decision, strings.TrimSpace(opts.CorrelationID))
	}
	if exec == nil {
		return zero, decision, &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: fmt.Sprintf("%s command path is not configured for %s", decision, cmd),
			Fatal:   true,
			Details: map[string]any{
				"command":  cmd,
				"decision": decision,
			},
		}
	}
	out, err := exec(ctx)

	launchShadowIfEnabled(ctx, cmd, decision, out, err, opts)
	return out, decision, err
}

func launchShadowIfEnabled[T any](parentCtx context.Context, cmd string, primaryDecision Decision, primaryOut T, primaryErr error, opts DispatchOptions[T]) {
	if primaryDecision != DecisionV1 {
		return
	}
	if !opts.ShadowFlags.Enabled(cmd) {
		return
	}
	if opts.V2 == nil {
		return
	}

	timeout := opts.ShadowTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	correlationID := strings.TrimSpace(opts.CorrelationID)
	compare := opts.Compare
	observe := opts.ShadowObserve

	go func() {
		start := time.Now()
		baseCtx := parentCtx
		if baseCtx == nil {
			baseCtx = context.Background()
		}
		shadowCtx, cancel := context.WithTimeout(baseCtx, timeout)
		defer cancel()

		shadowOut, shadowErr := opts.V2(shadowCtx)
		match, reason := compareShadow(primaryOut, primaryErr, shadowOut, shadowErr, compare)
		if observe != nil {
			observe(ShadowReport{
				Command:         cmd,
				CorrelationID:   correlationID,
				PrimaryDecision: primaryDecision,
				ShadowDecision:  DecisionV2,
				Match:           match,
				Reason:          reason,
				PrimaryError:    errString(primaryErr),
				ShadowError:     errString(shadowErr),
				DurationMS:      time.Since(start).Milliseconds(),
			})
		}
	}()
}

func compareShadow[T any](primary T, primaryErr error, shadow T, shadowErr error, compare ShadowComparator[T]) (bool, string) {
	if compare != nil {
		return compare(primary, primaryErr, shadow, shadowErr)
	}

	if primaryErr != nil || shadowErr != nil {
		if primaryErr == nil || shadowErr == nil {
			return false, "only one path returned an error"
		}
		if primaryErr.Error() != shadowErr.Error() {
			return false, "error mismatch"
		}
		return true, ""
	}

	if reflect.DeepEqual(primary, shadow) {
		return true, ""
	}
	return false, "result mismatch"
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}
