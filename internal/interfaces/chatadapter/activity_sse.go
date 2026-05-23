package chatadapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/joshka0/foxctl/internal/interfaces/web/sse"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

const (
	activitySSEDecodeStageEvent       = "event"
	activitySSEDecodeStageMarshalData = "marshal_data"
	activitySSEDecodeStageActivity    = "activity"
)

// activitySSEDecodeError records which decode stage failed so adapters can
// preserve platform-specific logging behavior without owning decode details.
type activitySSEDecodeError struct {
	Stage string
	Err   error
}

func (e *activitySSEDecodeError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("decode activity SSE %s: %v", e.Stage, e.Err)
}

func (e *activitySSEDecodeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ActivitySSEDecodeStage returns the stage attached to an activity SSE decode
// error, or an empty string for unrelated errors.
func ActivitySSEDecodeStage(err error) string {
	var decodeErr *activitySSEDecodeError
	if errors.As(err, &decodeErr) {
		return decodeErr.Stage
	}
	return ""
}

// DecodeActivitySSEMessage decodes a raw SSE hub message into an ActivityEvent.
// ok=false with nil error means the message is valid SSE for a different event
// type and should be ignored by activity listeners.
func DecodeActivitySSEMessage(raw []byte) (observability.ActivityEvent, bool, error) {
	const prefix = "data: "
	data := raw
	if len(data) > len(prefix) && bytes.HasPrefix(data, []byte(prefix)) {
		data = data[len(prefix):]
	}
	for len(data) > 0 && (data[len(data)-1] == '\n' || data[len(data)-1] == '\r') {
		data = data[:len(data)-1]
	}

	var event sse.Event
	if err := json.Unmarshal(data, &event); err != nil {
		return observability.ActivityEvent{}, false, &activitySSEDecodeError{Stage: activitySSEDecodeStageEvent, Err: err}
	}
	if event.Type != "activity" {
		return observability.ActivityEvent{}, false, nil
	}

	dataBytes, err := json.Marshal(event.Data)
	if err != nil {
		return observability.ActivityEvent{}, false, &activitySSEDecodeError{Stage: activitySSEDecodeStageMarshalData, Err: err}
	}

	var activity observability.ActivityEvent
	if err := json.Unmarshal(dataBytes, &activity); err != nil {
		return observability.ActivityEvent{}, false, &activitySSEDecodeError{Stage: activitySSEDecodeStageActivity, Err: err}
	}

	return activity, true, nil
}
