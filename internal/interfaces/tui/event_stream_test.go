package tui

import (
	"errors"
	"strings"
	"testing"
)

func TestConsoleSessionEventsEndpoint(t *testing.T) {
	t.Parallel()

	endpoint, err := ConsoleSessionEventsEndpoint("sess 1/2", true)
	if err != nil {
		t.Fatalf("ConsoleSessionEventsEndpoint error: %v", err)
	}

	const want = "/api/console/sessions/sess%201%2F2/events?format=payload"
	if endpoint != want {
		t.Fatalf("endpoint = %q, want %q", endpoint, want)
	}
}

func TestConsoleSessionEventsEndpointRejectsEmptySessionID(t *testing.T) {
	t.Parallel()

	_, err := ConsoleSessionEventsEndpoint("   ", false)
	if err == nil {
		t.Fatal("ConsoleSessionEventsEndpoint error = nil, want validation failure")
	}
}

func TestCollectConsoleStreamEventsParsesWrappedEvent(t *testing.T) {
	t.Parallel()

	stream := strings.NewReader(
		"event: reply\n" +
			`data: {"type":"reply","data":{"type":"reply","console_id":"sess-1","correlation_id":"corr-1","content":"done","metadata":{"partial":false}},"ts":1712000000123}` + "\n\n",
	)

	events, err := CollectConsoleStreamEvents(stream)
	if err != nil {
		t.Fatalf("CollectConsoleStreamEvents error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}

	event := events[0]
	if event.Event != "reply" {
		t.Fatalf("Event = %q, want %q", event.Event, "reply")
	}
	if event.Type != "reply" {
		t.Fatalf("Type = %q, want %q", event.Type, "reply")
	}
	if event.TimestampMS != 1712000000123 {
		t.Fatalf("TimestampMS = %d, want %d", event.TimestampMS, int64(1712000000123))
	}
	if event.Payload == nil {
		t.Fatal("Payload = nil, want decoded payload")
	}
	if event.Payload.CorrelationID != "corr-1" {
		t.Fatalf("Payload.CorrelationID = %q, want %q", event.Payload.CorrelationID, "corr-1")
	}
}

func TestCollectConsoleStreamEventsParsesPayloadEventWithoutEventLine(t *testing.T) {
	t.Parallel()

	stream := strings.NewReader(
		`data: {"type":"event","console_id":"sess-1","correlation_id":"corr-2","content":"chunk","metadata":{"partial":true}}` + "\n",
	)

	events, err := CollectConsoleStreamEvents(stream)
	if err != nil {
		t.Fatalf("CollectConsoleStreamEvents error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}

	event := events[0]
	if event.Event != "" {
		t.Fatalf("Event = %q, want empty event name in payload mode", event.Event)
	}
	if event.Type != "event" {
		t.Fatalf("Type = %q, want %q", event.Type, "event")
	}
	if event.Payload == nil {
		t.Fatal("Payload = nil, want decoded payload")
	}
	if event.Payload.Content != "chunk" {
		t.Fatalf("Payload.Content = %q, want %q", event.Payload.Content, "chunk")
	}
}

func TestCollectConsoleStreamEventsSupportsMultiLineDataAndComments(t *testing.T) {
	t.Parallel()

	stream := strings.NewReader(
		": first comment\n" +
			"event: connected\n" +
			`data: {"session_id":"sess-1",` + "\n" +
			`data: "timestamp":1712000000999}` + "\n\n" +
			": ignored heartbeat comment\n" +
			"event: heartbeat\n" +
			`data: {"timestamp":1712000001999}` + "\n\n",
	)

	events, err := CollectConsoleStreamEvents(stream)
	if err != nil {
		t.Fatalf("CollectConsoleStreamEvents error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}

	if events[0].Type != "connected" {
		t.Fatalf("events[0].Type = %q, want %q", events[0].Type, "connected")
	}
	if events[0].TimestampMS != 1712000000999 {
		t.Fatalf("events[0].TimestampMS = %d, want %d", events[0].TimestampMS, int64(1712000000999))
	}
	if events[1].Type != "heartbeat" {
		t.Fatalf("events[1].Type = %q, want %q", events[1].Type, "heartbeat")
	}
	if events[1].TimestampMS != 1712000001999 {
		t.Fatalf("events[1].TimestampMS = %d, want %d", events[1].TimestampMS, int64(1712000001999))
	}
}

func TestParseConsoleEventStreamPropagatesCallbackError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("stop parsing")
	stream := strings.NewReader("event: connected\ndata: {\"session_id\":\"s\"}\n\n")
	err := ParseConsoleEventStream(stream, func(ConsoleStreamEvent) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("ParseConsoleEventStream error = %v, want %v", err, wantErr)
	}
}

func TestMapConsoleStreamEventsToTranscriptPreservesKindsAndCorrelation(t *testing.T) {
	t.Parallel()

	events := []ConsoleStreamEvent{
		{
			Type: "ask",
			Payload: &ConsoleEventPayload{
				Type:          "ask",
				CorrelationID: "corr-ask",
				Content:       "start",
			},
		},
		{
			Type: "reply",
			Payload: &ConsoleEventPayload{
				Type:          "reply",
				CorrelationID: "corr-ask",
				Content:       "done",
			},
		},
		{
			Type: "event",
			Payload: &ConsoleEventPayload{
				Type:          "event",
				CorrelationID: "corr-ask",
				Content:       "working",
			},
		},
		{
			Type: "cmd",
			Payload: &ConsoleEventPayload{
				Type:          "cmd",
				CorrelationID: "corr-cmd",
			},
		},
	}

	got := MapConsoleStreamEventsToTranscript(events)
	want := []TranscriptEntry{
		{Speaker: "you", Kind: "ask", Text: "[corr-ask] start"},
		{Speaker: "assistant", Kind: "reply", Text: "[corr-ask] done"},
		{Speaker: "worker", Kind: "event", Text: "[corr-ask] working"},
		{Speaker: "system", Kind: "cmd", Text: "[corr-cmd] command"},
	}

	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestMapConsoleStreamEventToTranscriptEntryHandlesConnectedAndHeartbeat(t *testing.T) {
	t.Parallel()

	connected := ConsoleStreamEvent{
		Type: "connected",
		Data: []byte(`{"session_id":"sess-live","timestamp":1712000000}`),
	}

	entry, ok := MapConsoleStreamEventToTranscriptEntry(connected)
	if !ok {
		t.Fatal("connected event should map to transcript entry")
	}
	if entry.Kind != "connected" {
		t.Fatalf("entry.Kind = %q, want %q", entry.Kind, "connected")
	}
	if !strings.Contains(entry.Text, "sess-live") {
		t.Fatalf("entry.Text = %q, want session id mention", entry.Text)
	}

	heartbeat := ConsoleStreamEvent{
		Type: "heartbeat",
		Data: []byte(`{"timestamp":1712000001}`),
	}
	if _, ok := MapConsoleStreamEventToTranscriptEntry(heartbeat); ok {
		t.Fatal("heartbeat event should not map to transcript entry")
	}
}
