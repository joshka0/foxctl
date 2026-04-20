package tui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

const (
	maxSSELineBytes      = 1024 * 1024
	maxSSEEventDataBytes = 1024 * 1024
)

// ConsoleEventPayload is the decoded console event body from wrapped or payload SSE data.
type ConsoleEventPayload struct {
	Type          string          `json:"type"`
	ConsoleID     string          `json:"console_id,omitempty"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	Content       string          `json:"content,omitempty"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
}

// ConsoleStreamEvent is a normalized SSE event for console session streams.
type ConsoleStreamEvent struct {
	Event       string               `json:"event,omitempty"`
	Type        string               `json:"type,omitempty"`
	TimestampMS int64                `json:"timestamp_ms,omitempty"`
	Data        json.RawMessage      `json:"data,omitempty"`
	Payload     *ConsoleEventPayload `json:"payload,omitempty"`
}

// ConsoleSessionEventsEndpoint builds the console SSE endpoint path.
func ConsoleSessionEventsEndpoint(sessionID string, payloadFormat bool) (string, error) {
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		return "", errors.New("session id is required")
	}

	endpoint := "/api/console/sessions/" + url.PathEscape(trimmed) + "/events"
	if payloadFormat {
		endpoint += "?format=payload"
	}
	return endpoint, nil
}

// ParseConsoleEventStream reads an SSE stream and emits normalized console events.
func ParseConsoleEventStream(r io.Reader, onEvent func(ConsoleStreamEvent) error) error {
	if r == nil {
		return errors.New("sse reader is required")
	}
	if onEvent == nil {
		onEvent = func(ConsoleStreamEvent) error { return nil }
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineBytes)

	var eventName string
	dataLines := make([]string, 0, 4)
	dataBytes := 0

	flushEvent := func() error {
		if strings.TrimSpace(eventName) == "" && len(dataLines) == 0 {
			return nil
		}

		rawData := strings.Join(dataLines, "\n")
		parsed, err := decodeConsoleStreamEvent(eventName, rawData)
		if err != nil {
			return err
		}
		if err := onEvent(parsed); err != nil {
			return err
		}

		eventName = ""
		dataLines = dataLines[:0]
		dataBytes = 0
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flushEvent(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			value := strings.TrimPrefix(line, "data:")
			value = strings.TrimPrefix(value, " ")
			dataBytes += len(value)
			if dataBytes > maxSSEEventDataBytes {
				return fmt.Errorf("sse event data exceeds %d bytes", maxSSEEventDataBytes)
			}
			dataLines = append(dataLines, value)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read sse stream: %w", err)
	}
	if err := flushEvent(); err != nil {
		return err
	}
	return nil
}

// CollectConsoleStreamEvents parses all events from r and returns them as a slice.
func CollectConsoleStreamEvents(r io.Reader) ([]ConsoleStreamEvent, error) {
	events := make([]ConsoleStreamEvent, 0, 16)
	if err := ParseConsoleEventStream(r, func(event ConsoleStreamEvent) error {
		events = append(events, event)
		return nil
	}); err != nil {
		return nil, err
	}
	return events, nil
}

// MapConsoleStreamEventToTranscriptEntry converts one stream event to a transcript row when meaningful.
func MapConsoleStreamEventToTranscriptEntry(event ConsoleStreamEvent) (TranscriptEntry, bool) {
	if event.Payload != nil {
		kind := normalizeStreamType(event.Payload.Type)
		if kind == "" {
			kind = normalizeStreamType(firstNonEmpty(event.Type, event.Event))
		}
		if kind == "" {
			kind = "console"
		}

		text := payloadTranscriptText(*event.Payload, kind)
		if text == "" {
			return TranscriptEntry{}, false
		}

		return TranscriptEntry{
			Speaker:       payloadSpeaker(kind),
			Kind:          kind,
			Text:          text,
			CorrelationID: strings.TrimSpace(event.Payload.CorrelationID),
		}, true
	}

	kind := normalizeStreamType(firstNonEmpty(event.Type, event.Event))
	switch kind {
	case "":
		return TranscriptEntry{}, false
	case "heartbeat":
		return TranscriptEntry{}, false
	case "connected":
		sessionID := jsonFieldString(event.Data, "session_id")
		text := "console stream connected"
		if sessionID != "" {
			text += " (" + sessionID + ")"
		}
		return TranscriptEntry{
			Speaker: "system",
			Kind:    kind,
			Text:    text,
		}, true
	default:
		text := strings.TrimSpace(jsonFieldString(event.Data, "content"))
		if text == "" {
			text = kind + " event"
		}
		return TranscriptEntry{
			Speaker: "system",
			Kind:    kind,
			Text:    text,
		}, true
	}
}

// MapConsoleStreamEventsToTranscript maps stream events into transcript rows.
func MapConsoleStreamEventsToTranscript(events []ConsoleStreamEvent) []TranscriptEntry {
	entries := make([]TranscriptEntry, 0, len(events))
	for _, event := range events {
		entry, ok := MapConsoleStreamEventToTranscriptEntry(event)
		if !ok {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

type wrappedConsoleStreamEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
	TS   json.Number     `json:"ts"`
}

func decodeConsoleStreamEvent(eventName string, rawData string) (ConsoleStreamEvent, error) {
	eventName = strings.TrimSpace(eventName)
	trimmedData := strings.TrimSpace(rawData)

	streamEvent := ConsoleStreamEvent{
		Event: eventName,
		Type:  normalizeStreamType(eventName),
	}

	if trimmedData == "" {
		return streamEvent, nil
	}

	dataBytes := []byte(trimmedData)
	if !json.Valid(dataBytes) {
		quoted, _ := json.Marshal(trimmedData)
		dataBytes = quoted
	}

	if wrapped, ok, err := decodeWrappedStreamPayload(dataBytes); err != nil {
		return ConsoleStreamEvent{}, fmt.Errorf("decode wrapped stream event: %w", err)
	} else if ok {
		wrapped.Event = firstNonEmpty(eventName, wrapped.Type)
		return wrapped, nil
	}

	if payload, ok, err := decodeConsolePayload(dataBytes); err != nil {
		return ConsoleStreamEvent{}, fmt.Errorf("decode payload stream event: %w", err)
	} else if ok {
		streamEvent.Type = normalizeStreamType(firstNonEmpty(payload.Type, streamEvent.Type))
		streamEvent.Data = cloneRawMessage(dataBytes)
		streamEvent.Payload = &payload
		streamEvent.TimestampMS = extractTimestampMS(dataBytes)
		return streamEvent, nil
	}

	streamEvent.Data = cloneRawMessage(dataBytes)
	streamEvent.TimestampMS = extractTimestampMS(dataBytes)
	if parsedType := normalizeStreamType(jsonFieldString(dataBytes, "type")); parsedType != "" {
		streamEvent.Type = parsedType
	}
	return streamEvent, nil
}

func decodeWrappedStreamPayload(data []byte) (ConsoleStreamEvent, bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var wrapped wrappedConsoleStreamEvent
	if err := decoder.Decode(&wrapped); err != nil {
		return ConsoleStreamEvent{}, false, nil
	}

	wrappedType := normalizeStreamType(wrapped.Type)
	if wrappedType == "" || len(bytes.TrimSpace(wrapped.Data)) == 0 {
		return ConsoleStreamEvent{}, false, nil
	}

	event := ConsoleStreamEvent{
		Type: wrappedType,
		Data: cloneRawMessage(wrapped.Data),
	}
	event.TimestampMS = parseJSONNumberInt64(wrapped.TS)

	payload, ok, err := decodeConsolePayload(wrapped.Data)
	if err != nil {
		return ConsoleStreamEvent{}, false, err
	}
	if ok {
		event.Payload = &payload
	}

	return event, true, nil
}

func decodeConsolePayload(data []byte) (ConsoleEventPayload, bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var payload ConsoleEventPayload
	if err := decoder.Decode(&payload); err != nil {
		return ConsoleEventPayload{}, false, nil
	}

	payloadType := normalizeStreamType(payload.Type)
	switch payloadType {
	case "ask", "reply", "event", "cmd":
		payload.Type = payloadType
		payload.Metadata = cloneRawMessage(payload.Metadata)
		return payload, true, nil
	default:
		return ConsoleEventPayload{}, false, nil
	}
}

func payloadSpeaker(kind string) string {
	switch normalizeStreamType(kind) {
	case "ask":
		return "you"
	case "reply":
		return "assistant"
	case "event":
		return "worker"
	case "cmd":
		return "system"
	default:
		return "session"
	}
}

func payloadTranscriptText(payload ConsoleEventPayload, kind string) string {
	base := strings.TrimSpace(payload.Content)
	if base == "" {
		base = metadataTranscriptText(payload.Metadata)
	}
	if base == "" {
		switch normalizeStreamType(kind) {
		case "ask":
			base = "ask"
		case "reply":
			base = "reply"
		case "event":
			base = "event update"
		case "cmd":
			base = "command"
		default:
			base = "console event"
		}
	}

	correlationID := strings.TrimSpace(payload.CorrelationID)
	if correlationID != "" {
		return "[" + correlationID + "] " + base
	}
	return base
}

func metadataTranscriptText(raw json.RawMessage) string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return ""
	}
	for _, key := range []string{"error", "message", "status"} {
		value := strings.TrimSpace(jsonFieldString(raw, key))
		if value != "" {
			return value
		}
	}
	return ""
}

func jsonFieldString(data []byte, field string) string {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return ""
	}

	raw, ok := payload[field]
	if !ok {
		return ""
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.TrimSpace(asString)
	}
	return ""
}

func extractTimestampMS(data []byte) int64 {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return 0
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return 0
	}

	for _, key := range []string{"timestamp", "ts"} {
		raw, ok := payload[key]
		if !ok {
			continue
		}
		if ts, ok := parseTimestampRaw(raw); ok {
			return ts
		}
	}
	return 0
}

func parseTimestampRaw(data []byte) (int64, bool) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return 0, false
	}

	var number json.Number
	if err := json.Unmarshal(data, &number); err == nil {
		if number == "" {
			return 0, false
		}
		if value, err := number.Int64(); err == nil {
			return value, true
		}
		if value, err := strconv.ParseFloat(number.String(), 64); err == nil {
			return int64(value), true
		}
	}

	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		asString = strings.TrimSpace(asString)
		if asString == "" {
			return 0, false
		}
		value, err := strconv.ParseInt(asString, 10, 64)
		if err == nil {
			return value, true
		}
	}

	return 0, false
}

func parseJSONNumberInt64(number json.Number) int64 {
	if number == "" {
		return 0
	}
	if value, err := number.Int64(); err == nil {
		return value
	}
	if value, err := strconv.ParseFloat(number.String(), 64); err == nil {
		return int64(value)
	}
	return 0
}

func normalizeStreamType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func cloneRawMessage(data []byte) json.RawMessage {
	if len(data) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), data...)
}
