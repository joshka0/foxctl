package orchestration

import (
	"context"
	"errors"
	"testing"
	"time"

	v2events "github.com/joshka0/foxctl/internal/v2/core/events"
)

func TestRetryDelay_DefaultsAndNormalizesAttempt(t *testing.T) {
	t.Parallel()

	delay := RetryDelay(0, 0, 0)
	if delay != time.Second {
		t.Fatalf("delay=%s want 1s", delay)
	}
}

func TestRetryDelay_CapsExponentialBackoff(t *testing.T) {
	t.Parallel()

	delay := RetryDelay(10, time.Second, 5*time.Second)
	if delay != 5*time.Second {
		t.Fatalf("delay=%s want 5s", delay)
	}
}

func TestAppendAndProject_AppendsWithoutProjector(t *testing.T) {
	t.Parallel()

	appender := &recordingEventAppender{}
	evt := v2events.Event{ID: "evt-1"}

	if err := AppendAndProject(context.Background(), appender, nil, evt); err != nil {
		t.Fatalf("AppendAndProject() error = %v", err)
	}
	if len(appender.events) != 1 {
		t.Fatalf("appended events=%d want 1", len(appender.events))
	}
	if appender.events[0].ID != "evt-1" {
		t.Fatalf("appended event id=%q want evt-1", appender.events[0].ID)
	}
}

func TestAppendAndProject_AppendFailureStopsProjection(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("append failed")
	appender := &recordingEventAppender{err: wantErr}
	projector := &recordingEventProjector{}

	err := AppendAndProject(context.Background(), appender, projector, v2events.Event{ID: "evt-1"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error=%v want %v", err, wantErr)
	}
	if projector.called {
		t.Fatal("projector should not be called after append failure")
	}
}

func TestAppendAndProject_ReturnsProjectionErrorAfterAppend(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("project failed")
	appender := &recordingEventAppender{}
	projector := &recordingEventProjector{err: wantErr}

	err := AppendAndProject(context.Background(), appender, projector, v2events.Event{ID: "evt-1"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error=%v want %v", err, wantErr)
	}
	if len(appender.events) != 1 {
		t.Fatalf("appended events=%d want 1", len(appender.events))
	}
	if !projector.called {
		t.Fatal("projector should be called after append succeeds")
	}
}

type recordingEventAppender struct {
	events []v2events.Event
	err    error
}

func (r *recordingEventAppender) Append(_ context.Context, event v2events.Event) error {
	if r.err != nil {
		return r.err
	}
	r.events = append(r.events, event)
	return nil
}

type recordingEventProjector struct {
	called bool
	err    error
}

func (r *recordingEventProjector) Apply(_ context.Context, _ v2events.Event) error {
	r.called = true
	return r.err
}
