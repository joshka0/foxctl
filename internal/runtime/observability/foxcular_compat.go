package observability

import (
	"context"
	"time"

	"github.com/joshka0/foxcular"
)

// foxcularBridge adapts foxctl's NDJSON/SSE infrastructure as a foxcular Drain.
// This allows foxctl to delegate core event delivery to foxcular while keeping
// foxctl-specific persistence (NDJSON files, SQLite, SSE bridge) intact.
type foxcularBridge struct{}

// Send converts a foxcular Event to a foxctl WideEvent and routes it through
// foxctl's existing persistence and SSE publishing infrastructure.
func (f *foxcularBridge) Send(ctx context.Context, event *foxcular.Event) error {
	if event == nil {
		return nil
	}
	we := foxcularEventToWideEvent(event)
	publishToSSE(we)

	dir := getObsDir()
	if dir == "" {
		return nil
	}
	return WriteEvent(ctx, WideEventFileName, we)
}

// Flush is a no-op for the bridge since writes are immediate.
func (f *foxcularBridge) Flush(_ context.Context) error {
	return nil
}

// Close is a no-op for the bridge.
func (f *foxcularBridge) Close() error {
	return nil
}

// foxcularEventToWideEvent maps a foxcular Event into a foxctl WideEvent.
// Foxcular fields are mapped to the corresponding foxctl fields; foxctl-specific
// fields like Service, Version, Component are populated from foxcular Data when
// available or set to foxctl defaults.
func foxcularEventToWideEvent(e *foxcular.Event) *WideEvent {
	we := &WideEvent{
		Ts:        e.Timestamp,
		TraceID:   e.TraceID,
		SpanID:    e.SpanID,
		ParentID:  e.ParentID,
		Operation: e.Operation,
		Status:    foxcularStatusToFoxctl(e.Status),
		DurationMS: func() int64 {
			if e.Duration == 0 && e.Data != nil {
				if ms, ok := e.Data["duration_ms"]; ok {
					switch v := ms.(type) {
					case int64:
						return v
					case float64:
						return int64(v)
					case int:
						return int64(v)
					}
				}
				return 0
			}
			return e.Duration.Milliseconds()
		}(),
		ErrorType:    e.ErrorType,
		ErrorCode:    e.ErrorCode,
		ErrorMessage: e.ErrorMessage,
		Service:      "foxctl",
		Data:         make(map[string]any),
	}

	// Map foxcular's Name field to foxctl's Command.
	if e.Name != "" {
		we.Command = e.Name
	}

	// Copy data, extracting foxctl-specific structured fields when present.
	if e.Data != nil {
		for k, v := range e.Data {
			switch k {
			case "service":
				if s, ok := v.(string); ok && s != "" {
					we.Service = s
				}
			case "version":
				if s, ok := v.(string); ok {
					we.Version = s
				}
			case "component":
				if s, ok := v.(string); ok {
					we.Component = s
				}
			case "command":
				if s, ok := v.(string); ok && s != "" {
					we.Command = s
				}
			case "name":
				// Only use data["name"] if Name field was empty and Command is still empty
				if we.Command == "" {
					if s, ok := v.(string); ok {
						we.Command = s
					}
				}
			case "subtype":
				if s, ok := v.(string); ok {
					we.Subtype = s
				}
			case "session_id":
				if s, ok := v.(string); ok {
					we.SessionID = s
				}
			case "agent_id":
				if s, ok := v.(string); ok {
					we.AgentID = s
				}
			case "workspace_id":
				if s, ok := v.(string); ok {
					we.WorkspaceID = s
				}
			case "job_id":
				if s, ok := v.(string); ok {
					we.JobID = s
				}
			case "retriable":
				if b, ok := v.(bool); ok {
					we.Retriable = &b
				}
			case "duration_ms":
				// Already handled above
			default:
				we.Data[k] = v
			}
		}
	}

	return we
}

// foxcularStatusToFoxctl maps a foxcular Status to a foxctl Status.
func foxcularStatusToFoxctl(s foxcular.Status) Status {
	switch s {
	case foxcular.StatusOK:
		return StatusOK
	case foxcular.StatusError:
		return StatusError
	case foxcular.StatusCanceled:
		return StatusCanceled
	default:
		return StatusOK
	}
}

// foxctlStatusToFoxcular maps a foxctl Status to a foxcular Status.
func foxctlStatusToFoxcular(s Status) foxcular.Status {
	switch s {
	case StatusOK:
		return foxcular.StatusOK
	case StatusError:
		return foxcular.StatusError
	case StatusCanceled:
		return foxcular.StatusCanceled
	default:
		return foxcular.StatusOK
	}
}

// wideEventToFoxcularEvent maps a foxctl WideEvent into a foxcular Event.
// This enables foxctl events to be delivered through a foxcular client pipeline.
func wideEventToFoxcularEvent(we *WideEvent) *foxcular.Event {
	data := make(map[string]any, len(we.Data)+10)

	// Copy existing data
	for k, v := range we.Data {
		data[k] = v
	}

	// Promote foxctl-specific fields into foxcular Data for round-trip fidelity.
	data["service"] = we.Service
	data["version"] = we.Version
	if we.Component != "" {
		data["component"] = we.Component
	}
	if we.Command != "" {
		data["command"] = we.Command
	}
	if we.Subtype != "" {
		data["subtype"] = we.Subtype
	}
	if we.SessionID != "" {
		data["session_id"] = we.SessionID
	}
	if we.AgentID != "" {
		data["agent_id"] = we.AgentID
	}
	if we.WorkspaceID != "" {
		data["workspace_id"] = we.WorkspaceID
	}
	if we.JobID != "" {
		data["job_id"] = we.JobID
	}
	if we.Retriable != nil {
		data["retriable"] = *we.Retriable
	}

	e := &foxcular.Event{
		Timestamp:    we.Ts,
		TraceID:      we.TraceID,
		SpanID:       we.SpanID,
		ParentID:     we.ParentID,
		Operation:    we.Operation,
		Name:         we.Command,
		Status:       foxctlStatusToFoxcular(we.Status),
		Duration:     time.Duration(we.DurationMS) * time.Millisecond,
		ErrorType:    we.ErrorType,
		ErrorCode:    we.ErrorCode,
		ErrorMessage: we.ErrorMessage,
		Data:         data,
	}

	return e
}
