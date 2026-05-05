package observability

import "time"

type testEventOpt func(*Event)

func testEvent(ts time.Time, operation string, status Status, opts ...testEventOpt) Event {
	event := Event{
		Timestamp: ts,
		Operation: operation,
		Status:    status,
		Data: map[string]any{
			"service": "foxctl",
		},
	}
	for _, opt := range opts {
		opt(&event)
	}
	return event
}

func testTrace(id string) testEventOpt {
	return func(event *Event) {
		event.TraceID = id
	}
}

func testSpan(id string) testEventOpt {
	return func(event *Event) {
		event.SpanID = id
	}
}

func testComponent(component string) testEventOpt {
	return func(event *Event) {
		event.Data["component"] = component
	}
}

func testSession(id string) testEventOpt {
	return func(event *Event) {
		event.Data["session_id"] = id
	}
}

func testWorkspace(id string) testEventOpt {
	return func(event *Event) {
		event.Data["workspace_id"] = id
	}
}

func testError(code, message string) testEventOpt {
	return func(event *Event) {
		event.ErrorCode = code
		event.ErrorMessage = message
	}
}

func testData(key string, value any) testEventOpt {
	return func(event *Event) {
		event.Data[key] = value
	}
}
