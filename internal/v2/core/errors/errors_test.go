package errors

import (
	"encoding/json"
	stderrors "errors"
	"net/http"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/v2/core/events"
)

func TestV2Error_HTTPStatusAndToEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    *V2Error
		status int
	}{
		{name: "validation", err: &V2Error{Kind: ErrValidation}, status: http.StatusBadRequest},
		{name: "policy", err: &V2Error{Kind: ErrPolicyViolation}, status: http.StatusForbidden},
		{name: "not_found", err: &V2Error{Kind: ErrNotFound}, status: http.StatusNotFound},
		{name: "timeout", err: &V2Error{Kind: ErrTimeout}, status: http.StatusRequestTimeout},
		{name: "tool_failed", err: &V2Error{Kind: ErrToolFailed}, status: http.StatusBadGateway},
		{name: "stage_failed_fatal", err: &V2Error{Kind: ErrStageFailed, Fatal: true}, status: http.StatusInternalServerError},
		{name: "stage_failed_nonfatal", err: &V2Error{Kind: ErrStageFailed, Fatal: false}, status: http.StatusOK},
		{name: "internal", err: &V2Error{Kind: ErrInternal}, status: http.StatusInternalServerError},
		{name: "dependency", err: &V2Error{Kind: ErrDependency}, status: http.StatusInternalServerError},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.err.HTTPStatus(); got != tc.status {
				t.Fatalf("HTTPStatus()=%d want %d", got, tc.status)
			}
		})
	}

	t.Run("to_event_population", func(t *testing.T) {
		t.Parallel()

		verr := &V2Error{
			Kind:      ErrToolFailed,
			Message:   "tool execution failed",
			Cause:     stderrors.New("exit status 1"),
			Retryable: true,
			Details: map[string]any{
				"tool": "agent_spawn",
			},
		}
		ctx := EventContext{
			StreamID:      "run-001",
			StreamType:    events.StreamTypeRun,
			Command:       "spawn",
			CorrelationID: "corr-001",
			CausationID:   "cause-001",
			ActorID:       "actor-overseer",
			RequestID:     "req-001",
			Now: func() time.Time {
				return time.Date(2026, time.February, 18, 12, 34, 56, 0, time.UTC)
			},
		}

		evt := verr.ToEvent(ctx, events.EventRunFailed)
		if evt.ID == "" {
			t.Fatal("event id is empty")
		}
		if evt.StreamID != ctx.StreamID {
			t.Fatalf("stream_id=%q want %q", evt.StreamID, ctx.StreamID)
		}
		if evt.StreamType != ctx.StreamType {
			t.Fatalf("stream_type=%q want %q", evt.StreamType, ctx.StreamType)
		}
		if evt.EventType != events.EventRunFailed {
			t.Fatalf("event_type=%q want %q", evt.EventType, events.EventRunFailed)
		}
		if evt.Command != "spawn" {
			t.Fatalf("command=%q want spawn", evt.Command)
		}
		if evt.CorrelationID != "corr-001" {
			t.Fatalf("correlation_id=%q want corr-001", evt.CorrelationID)
		}
		if evt.RequestID != "req-001" {
			t.Fatalf("request_id=%q want req-001", evt.RequestID)
		}
		if got := evt.OccurredAt; !got.Equal(time.Date(2026, time.February, 18, 12, 34, 56, 0, time.UTC)) {
			t.Fatalf("occurred_at=%s want fixed timestamp", got)
		}

		var payload events.ErrorPayload
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload.Kind != string(ErrToolFailed) {
			t.Fatalf("payload.kind=%q want %q", payload.Kind, ErrToolFailed)
		}
		if payload.HTTPStatus != http.StatusBadGateway {
			t.Fatalf("payload.http_status=%d want %d", payload.HTTPStatus, http.StatusBadGateway)
		}
		if payload.EnvelopeCode != "ERUNTIME" {
			t.Fatalf("payload.envelope_code=%q want ERUNTIME", payload.EnvelopeCode)
		}
		if payload.Cause != "exit status 1" {
			t.Fatalf("payload.cause=%q want \"exit status 1\"", payload.Cause)
		}
		if got := payload.Details["tool"]; got != "agent_spawn" {
			t.Fatalf("payload.details.tool=%v want agent_spawn", got)
		}
	})
}

func TestEnvelopeContract_V2Output_V1Shape(t *testing.T) {
	t.Parallel()

	t.Run("ok_shape", func(t *testing.T) {
		t.Parallel()

		env := envelope.OK("v2.spawn", map[string]any{
			"run_id": "run-001",
		})

		if err := envelope.Validate(env); err != nil {
			t.Fatalf("validate ok envelope: %v", err)
		}
		if env.Version != envelope.Version {
			t.Fatalf("version=%d want %d", env.Version, envelope.Version)
		}
		if env.Status != envelope.StatusOK {
			t.Fatalf("status=%q want %q", env.Status, envelope.StatusOK)
		}
		if env.Meta.TS == "" {
			t.Fatal("meta.ts is empty")
		}
		if _, err := time.Parse(time.RFC3339, env.Meta.TS); err != nil {
			t.Fatalf("parse meta.ts: %v", err)
		}
		if env.Error.Code != "" || env.Error.Message != "" {
			t.Fatalf("unexpected error block for ok status: %+v", env.Error)
		}
	})

	t.Run("error_shape", func(t *testing.T) {
		t.Parallel()

		verr := &V2Error{
			Kind:    ErrPolicyViolation,
			Message: "command blocked by policy",
		}
		env := envelope.Error("v2.spawn", verr.EnvelopeCode(), verr.Error(), map[string]any{
			"kind": verr.Kind,
		})

		if err := envelope.Validate(env); err != nil {
			t.Fatalf("validate error envelope: %v", err)
		}
		if env.Version != envelope.Version {
			t.Fatalf("version=%d want %d", env.Version, envelope.Version)
		}
		if env.Status != envelope.StatusError {
			t.Fatalf("status=%q want %q", env.Status, envelope.StatusError)
		}
		if env.Meta.TS == "" {
			t.Fatal("meta.ts is empty")
		}
		if _, err := time.Parse(time.RFC3339, env.Meta.TS); err != nil {
			t.Fatalf("parse meta.ts: %v", err)
		}
		if env.Error.Code == "" {
			t.Fatal("error.code is empty")
		}
		if env.Error.Message == "" {
			t.Fatal("error.message is empty")
		}
	})
}
