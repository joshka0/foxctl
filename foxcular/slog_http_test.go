package foxcular_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxcular"
)

// ---------------------------------------------------------------------------
// Slog Integration Tests: VAL-SLOG-001 through VAL-SLOG-003
// ---------------------------------------------------------------------------

// newSlogTestClient creates a client suitable for slog tests.
func newSlogTestClient() (*foxcular.Client, *captureDrain, *stubClock, *stubIDs) {
	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	clock := &stubClock{times: []time.Time{
		ts,                             // span start
		ts.Add(50 * time.Millisecond),  // builder timestamp
		ts.Add(100 * time.Millisecond), // span end
		ts.Add(150 * time.Millisecond), // extra
	}}
	ids := &stubIDs{ids: []string{
		"trace-001", "span-001", "span-002", "span-003",
		"trace-002", "span-child-001", "span-slog-001", "span-slog-002",
	}}
	drain := &captureDrain{}
	client := foxcular.NewClient(
		drain,
		foxcular.WithClock(clock),
		foxcular.WithIDGenerator(ids),
	)
	return client, drain, clock, ids
}

// VAL-SLOG-001: slog records attach to active span
func TestSlogRecordsAttachToActiveSpan(t *testing.T) {
	client, drain, _, _ := newSlogTestClient()

	// Start a span.
	ctx, span := client.StartSpan(context.Background(), "test.op")

	// Create a slog logger with the foxcular handler.
	handler := foxcular.NewSlogHandler(client, nil)
	logger := slog.New(handler)

	// Log within the span context.
	logger.InfoContext(ctx, "test message", slog.String("key", "value"))

	// End the span.
	_ = span.End(nil)

	events := drain.Events()

	// Should have 2 events: one from slog, one from span end.
	// Find the slog event (operation "slog").
	var slogEvent *foxcular.Event
	for _, e := range events {
		if e.Operation == "slog" {
			slogEvent = e
			break
		}
	}
	if slogEvent == nil {
		t.Fatalf("no slog event found; got %d events with operations: %v",
			len(events), eventOps(events))
	}

	// Verify trace ID matches the span's trace ID.
	if slogEvent.TraceID != span.TraceID() {
		t.Errorf("slog event trace_id = %q, want span trace_id %q",
			slogEvent.TraceID, span.TraceID())
	}

	// Verify parent ID is the span's ID (slog record correlates with active span).
	if slogEvent.ParentID != span.SpanID() {
		t.Errorf("slog event parent_id = %q, want span span_id %q",
			slogEvent.ParentID, span.SpanID())
	}

	// Verify message is preserved.
	if slogEvent.Message != "test message" {
		t.Errorf("slog event message = %q, want %q", slogEvent.Message, "test message")
	}

	// Verify level is captured.
	if slogEvent.Data["slog.level"] != "INFO" {
		t.Errorf("slog.level = %v, want INFO", slogEvent.Data["slog.level"])
	}

	// Verify attr is preserved.
	if slogEvent.Data["key"] != "value" {
		t.Errorf("data[key] = %v, want value", slogEvent.Data["key"])
	}

	// Verify status is OK (info level).
	if slogEvent.Status != foxcular.StatusOK {
		t.Errorf("status = %q, want ok", slogEvent.Status)
	}
}

// VAL-SLOG-002: slog records without span emit standalone events
func TestSlogRecordsWithoutSpanEmitStandaloneEvents(t *testing.T) {
	client, drain, _, _ := newSlogTestClient()

	handler := foxcular.NewSlogHandler(client, nil)
	logger := slog.New(handler)

	// Log without any span context.
	logger.Info("standalone message", slog.Int("count", 42))

	events := drain.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 standalone event, got %d", len(events))
	}

	e := events[0]

	// Verify it's a valid event.
	if e.Operation != "slog" {
		t.Errorf("operation = %q, want slog", e.Operation)
	}
	if e.Message != "standalone message" {
		t.Errorf("message = %q, want standalone message", e.Message)
	}
	if e.SpanID == "" {
		t.Error("span ID should be auto-generated for standalone event")
	}
	if e.TraceID == "" {
		t.Error("trace ID should be auto-generated for standalone event")
	}

	// Verify attrs.
	if e.Data["count"] != int64(42) {
		t.Errorf("data[count] = %v (%T), want 42", e.Data["count"], e.Data["count"])
	}
	if e.Data["slog.level"] != "INFO" {
		t.Errorf("slog.level = %v, want INFO", e.Data["slog.level"])
	}
}

// VAL-SLOG-003: slog attributes and groups preserve meaning with redaction
func TestSlogAttributesAndGroupsPreserveMeaningWithRedaction(t *testing.T) {
	client, drain, _, _ := newSlogTestClient()

	// Create handler with redaction enabled.
	handler := foxcular.NewSlogHandler(client, nil)
	logger := slog.New(handler)

	// Log with grouped attrs and sensitive values.
	logger.WithGroup("request").With(
		slog.String("password", "secret123"),
		slog.String("user_agent", "test-client"),
	).Info("grouped message", slog.String("token", "bearer abc123"))

	events := drain.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]

	// Verify group prefix is applied.
	if e.Data["request.user_agent"] != "test-client" {
		t.Errorf("grouped attr request.user_agent = %v, want test-client", e.Data["request.user_agent"])
	}

	// Verify sensitive key is redacted.
	if e.Data["request.password"] != "[REDACTED]" {
		t.Errorf("sensitive key request.password not redacted: %v", e.Data["request.password"])
	}

	// Verify record-level sensitive key is also redacted (within the group).
	if e.Data["request.token"] != "[REDACTED]" {
		t.Errorf("sensitive key request.token not redacted: %v", e.Data["request.token"])
	}

	// Verify message is preserved.
	if e.Message != "grouped message" {
		t.Errorf("message = %q, want grouped message", e.Message)
	}
}

// VAL-SLOG-003: error-level records
func TestSlogErrorLevelRecordsStatus(t *testing.T) {
	client, drain, _, _ := newSlogTestClient()

	handler := foxcular.NewSlogHandler(client, nil)
	logger := slog.New(handler)

	logger.Error("something went wrong", slog.String("detail", "timeout"))

	events := drain.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]

	// Error level should produce error status.
	if e.Status != foxcular.StatusError {
		t.Errorf("status = %q, want error", e.Status)
	}
	if e.ErrorMessage != "something went wrong" {
		t.Errorf("error_message = %q, want 'something went wrong'", e.ErrorMessage)
	}
	if e.Data["detail"] != "timeout" {
		t.Errorf("data[detail] = %v, want timeout", e.Data["detail"])
	}
}

// VAL-SLOG-003: group attrs within group values (nested groups)
func TestSlogGroupValuePreservesStructure(t *testing.T) {
	client, drain, _, _ := newSlogTestClient()

	handler := foxcular.NewSlogHandler(client, nil)
	logger := slog.New(handler)

	// Log with a group value (slog.Group creates a group attribute).
	logger.Info(
		"nested groups",
		slog.Group(
			"request",
			slog.String("method", "GET"),
			slog.String("path", "/api/users"),
			slog.String("authorization", "bearer tok_secret"),
		),
		slog.String("safe_key", "preserved"),
	)

	events := drain.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]

	// Group value should be a map.
	req, ok := e.Data["request"].(map[string]any)
	if !ok {
		t.Fatalf("request is not a map: %T", e.Data["request"])
	}

	if req["method"] != "GET" {
		t.Errorf("request.method = %v, want GET", req["method"])
	}
	if req["path"] != "/api/users" {
		t.Errorf("request.path = %v, want /api/users", req["path"])
	}

	// Sensitive key within group should be redacted.
	if req["authorization"] != "[REDACTED]" {
		t.Errorf("request.authorization not redacted: %v", req["authorization"])
	}

	// Safe key should be preserved.
	if e.Data["safe_key"] != "preserved" {
		t.Errorf("safe_key = %v, want preserved", e.Data["safe_key"])
	}
}

// ---------------------------------------------------------------------------
// HTTP Middleware Tests: VAL-HTTP-001 through VAL-HTTP-004
// ---------------------------------------------------------------------------

// newHTTPTestClient creates a client suitable for HTTP middleware tests.
func newHTTPTestClient() (*foxcular.Client, *captureDrain, *stubClock, *stubIDs) {
	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	clock := &stubClock{times: []time.Time{
		ts,                             // middleware span start
		ts.Add(50 * time.Millisecond),  // builder timestamp for span event
		ts.Add(100 * time.Millisecond), // extra builder timestamp
		ts.Add(150 * time.Millisecond), // extra builder timestamp
		ts.Add(200 * time.Millisecond), // extra
		ts.Add(250 * time.Millisecond), // extra
		ts.Add(300 * time.Millisecond), // extra
		ts.Add(350 * time.Millisecond), // extra
	}}
	ids := &stubIDs{ids: []string{
		"trace-001", "span-001", "span-002", "span-003",
		"trace-002", "span-child-001", "trace-003", "span-003",
		"trace-004", "span-004", "trace-005", "span-005",
		"trace-006", "span-006", "trace-007", "span-007",
	}}
	drain := &captureDrain{}
	client := foxcular.NewClient(
		drain,
		foxcular.WithClock(clock),
		foxcular.WithIDGenerator(ids),
	)
	return client, drain, clock, ids
}

// VAL-HTTP-001: Middleware emits request lifecycle event
func TestMiddlewareEmitsRequestLifecycleEvent(t *testing.T) {
	client, drain, _, _ := newHTTPTestClient()

	middleware := foxcular.HTTPMiddleware(client, nil)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/users?page=1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	events := drain.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]

	// Verify operation.
	if e.Operation != "http.request" {
		t.Errorf("operation = %q, want http.request", e.Operation)
	}

	// Verify method.
	if e.Data["http.method"] != "GET" {
		t.Errorf("http.method = %v, want GET", e.Data["http.method"])
	}

	// Verify path.
	if e.Data["http.path"] != "/api/users?page=1" {
		t.Errorf("http.path = %v, want /api/users?page=1", e.Data["http.path"])
	}

	// Verify status code.
	if e.Data["http.status"] != http.StatusOK {
		t.Errorf("http.status = %v, want %d", e.Data["http.status"], http.StatusOK)
	}

	// Verify duration was captured.
	if _, ok := e.Data["http.duration_ms"]; !ok {
		t.Error("http.duration_ms not captured")
	}

	// Verify trace ID and span ID are set.
	if e.TraceID == "" {
		t.Error("trace ID should be set")
	}
	if e.SpanID == "" {
		t.Error("span ID should be set")
	}

	// Verify name contains method and path.
	if !strings.Contains(e.Name, "GET") {
		t.Errorf("name should contain method: %q", e.Name)
	}
}

// VAL-HTTP-002: Middleware captures actual response status
func TestMiddlewareCapturesActualResponseStatus(t *testing.T) {
	client, drain, _, _ := newHTTPTestClient()

	middleware := foxcular.HTTPMiddleware(client, nil)

	tests := []struct {
		name           string
		handler        http.HandlerFunc
		expectedStatus int
	}{
		{
			name: "implicit_200",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("ok")) // implicit 200
			},
			expectedStatus: 200,
		},
		{
			name: "explicit_201",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte("created"))
			},
			expectedStatus: 201,
		},
		{
			name: "explicit_404",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			expectedStatus: 404,
		},
		{
			name: "explicit_500",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectedStatus: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			drain.events = nil

			handler := middleware(tt.handler)
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			events := drain.Events()
			if len(events) != 1 {
				t.Fatalf("expected 1 event, got %d", len(events))
			}
			e := events[0]

			if e.Data["http.status"] != tt.expectedStatus {
				t.Errorf("http.status = %v, want %d", e.Data["http.status"], tt.expectedStatus)
			}
		})
	}
}

// VAL-HTTP-003: Middleware observes panic/error outcomes
func TestMiddlewareObservesPanicOutcomes(t *testing.T) {
	client, drain, _, _ := newHTTPTestClient()

	middleware := foxcular.HTTPMiddleware(client, nil)

	// Test panicking handler.
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something terrible happened")
	}))

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rec := httptest.NewRecorder()

	// The middleware should recover from the panic.
	handler.ServeHTTP(rec, req)

	events := drain.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]

	// Panic should result in error status.
	if e.Status != foxcular.StatusError {
		t.Errorf("status = %q, want error for panicking handler", e.Status)
	}
	if e.ErrorMessage == "" {
		t.Error("error message should be populated for panic")
	}
}

// VAL-HTTP-003: Middleware observes error status codes
func TestMiddlewareObservesErrorStatusCodes(t *testing.T) {
	client, drain, _, _ := newHTTPTestClient()

	middleware := foxcular.HTTPMiddleware(client, nil)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	req := httptest.NewRequest(http.MethodGet, "/error", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	events := drain.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]

	// 5xx status should produce error.
	if e.Status != foxcular.StatusError {
		t.Errorf("status = %q, want error for 500 status", e.Status)
	}
	if e.ErrorMessage == "" {
		t.Error("error message should be populated for 500 status")
	}
}

// VAL-HTTP-003: Successful requests are not marked as errors
func TestMiddlewareSuccessNotError(t *testing.T) {
	client, drain, _, _ := newHTTPTestClient()

	middleware := foxcular.HTTPMiddleware(client, nil)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	e := drain.Events()[0]
	if e.Status != foxcular.StatusOK {
		t.Errorf("status = %q, want ok for successful request", e.Status)
	}
}

// VAL-HTTP-003: 4xx is not an error from server perspective
func TestMiddlewareClientErrorNotServerError(t *testing.T) {
	client, drain, _, _ := newHTTPTestClient()

	middleware := foxcular.HTTPMiddleware(client, nil)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))

	req := httptest.NewRequest(http.MethodGet, "/bad-request", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	e := drain.Events()[0]
	// 4xx should be OK from server perspective (client error, not server error).
	if e.Status != foxcular.StatusOK {
		t.Errorf("status = %q, want ok for 4xx (client error is not server error)", e.Status)
	}
}

// VAL-HTTP-004: Middleware propagates context to handler
func TestMiddlewarePropagatesContextToHandler(t *testing.T) {
	client, drain, _, _ := newHTTPTestClient()

	middleware := foxcular.HTTPMiddleware(client, nil)

	var handlerTraceID, handlerSpanID string
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The handler should be able to retrieve span info from context.
		handlerTraceID = foxcular.TraceIDFromContext(r.Context())
		handlerSpanID = foxcular.SpanIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify the handler had access to trace/span IDs.
	if handlerTraceID == "" {
		t.Error("handler should have access to trace ID from request context")
	}
	if handlerSpanID == "" {
		t.Error("handler should have access to span ID from request context")
	}

	// Verify the request event shares the same trace ID.
	events := drain.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]

	if e.TraceID != handlerTraceID {
		t.Errorf("request event trace_id = %q, handler saw trace_id = %q; should match",
			e.TraceID, handlerTraceID)
	}
}

// VAL-HTTP-004: Handler logging through request context correlates with request span
func TestMiddlewareHandlerLoggingCorrelates(t *testing.T) {
	client, drain, _, _ := newHTTPTestClient()

	middleware := foxcular.HTTPMiddleware(client, nil)
	slogHandler := foxcular.NewSlogHandler(client, nil)
	logger := slog.New(slogHandler)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Log using the request context (which has span info).
		logger.InfoContext(r.Context(), "handler log message", slog.String("detail", "processing"))
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	events := drain.Events()
	// Should have 2 events: 1 from slog inside handler, 1 from middleware span end.
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(events))
	}

	// Find the slog event and the request event.
	var slogEvent, requestEvent *foxcular.Event
	for _, e := range events {
		switch e.Operation {
		case "slog":
			slogEvent = e
		case "http.request":
			requestEvent = e
		}
	}

	if slogEvent == nil {
		t.Fatal("no slog event found")
	}
	if requestEvent == nil {
		t.Fatal("no http.request event found")
	}

	// Both should share the same trace ID.
	if slogEvent.TraceID != requestEvent.TraceID {
		t.Errorf("slog event trace_id = %q, request event trace_id = %q; should match",
			slogEvent.TraceID, requestEvent.TraceID)
	}

	// Slog event should reference the request span as parent.
	if slogEvent.ParentID != requestEvent.SpanID {
		t.Errorf("slog event parent_id = %q, request span_id = %q; slog should be child of request span",
			slogEvent.ParentID, requestEvent.SpanID)
	}
}

// ---------------------------------------------------------------------------
// Custom operation name for HTTP middleware
// ---------------------------------------------------------------------------

func TestHTTPMiddlewareCustomOperation(t *testing.T) {
	client, drain, _, _ := newHTTPTestClient()

	middleware := foxcular.HTTPMiddleware(client, &foxcular.HTTPMiddlewareOptions{
		Operation: "myapp.request",
	})

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	e := drain.Events()[0]
	if e.Operation != "myapp.request" {
		t.Errorf("operation = %q, want myapp.request", e.Operation)
	}
}

// ---------------------------------------------------------------------------
// Slog custom operation name
// ---------------------------------------------------------------------------

func TestSlogCustomOperation(t *testing.T) {
	client, drain, _, _ := newSlogTestClient()

	handler := foxcular.NewSlogHandler(client, &foxcular.SlogHandlerOptions{
		Operation: "myapp.log",
	})
	logger := slog.New(handler)

	logger.Info("test")

	e := drain.Events()[0]
	if e.Operation != "myapp.log" {
		t.Errorf("operation = %q, want myapp.log", e.Operation)
	}
}

// ---------------------------------------------------------------------------
// Slog level filtering
// ---------------------------------------------------------------------------

func TestSlogLevelFiltering(t *testing.T) {
	client, drain, _, _ := newSlogTestClient()

	handler := foxcular.NewSlogHandler(client, &foxcular.SlogHandlerOptions{
		Level: slog.LevelError,
	})
	logger := slog.New(handler)

	logger.Info("should be filtered")
	logger.Warn("should also be filtered")
	logger.Error("should pass")

	events := drain.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event (error only), got %d", len(events))
	}
	if events[0].Message != "should pass" {
		t.Errorf("message = %q, want 'should pass'", events[0].Message)
	}
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

func eventOps(events []*foxcular.Event) []string {
	ops := make([]string, len(events))
	for i, e := range events {
		ops[i] = e.Operation
	}
	return ops
}
