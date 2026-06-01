package console

import (
	"errors"
	"testing"
	"testing/quick"
)

func TestCorrelationTrackerCompleteIsTerminalAndFreesCapacity(t *testing.T) {
	tracker := NewCorrelationTracker(1)

	correlationID, err := tracker.NewCorrelation("console-1", "actor-1", "first ask")
	if err != nil {
		t.Fatalf("NewCorrelation: %v", err)
	}
	if !tracker.IsFull() || tracker.Count() != 1 {
		t.Fatalf("tracker should be full after one in-flight ask: count=%d full=%v", tracker.Count(), tracker.IsFull())
	}

	if _, err := tracker.NewCorrelation("console-1", "actor-1", "second ask"); !errors.Is(err, ErrMaxInFlightReached) {
		t.Fatalf("second in-flight ask error = %v, want ErrMaxInFlightReached", err)
	}

	tracker.Cancel(correlationID)
	tracker.AppendStreamed(correlationID, "chunk-1")
	tracker.AppendStreamed(correlationID, " chunk-2")
	pending := tracker.GetPending(correlationID)
	if pending == nil {
		t.Fatal("cancelled correlation should remain pending until completion")
	}
	if !pending.Cancelled {
		t.Fatal("Cancel should mark the pending correlation as cancelled")
	}
	if pending.StreamedContent != "chunk-1 chunk-2" {
		t.Fatalf("StreamedContent = %q, want accumulated chunks", pending.StreamedContent)
	}
	if _, err := tracker.NewCorrelation("console-1", "actor-1", "blocked ask"); !errors.Is(err, ErrMaxInFlightReached) {
		t.Fatalf("cancelled-but-pending ask should still enforce backpressure, got error %v", err)
	}

	tracker.Complete(correlationID)
	if tracker.Count() != 0 || tracker.IsFull() {
		t.Fatalf("completed correlation should leave tracker empty: count=%d full=%v", tracker.Count(), tracker.IsFull())
	}
	if got := tracker.GetPending(correlationID); got != nil {
		t.Fatalf("completed correlation still pending: %+v", got)
	}
	if _, err := tracker.NewCorrelation("console-1", "actor-1", "next ask"); err != nil {
		t.Fatalf("completion should free capacity for the next ask: %v", err)
	}
}

func TestCorrelationTrackerGeneratedSingleFlightStateMachine(t *testing.T) {
	prop := func(actions []byte) bool {
		if len(actions) > 64 {
			actions = actions[:64]
		}

		tracker := NewCorrelationTracker(1)
		var activeID string
		modelCount := 0

		for _, action := range actions {
			switch action % 6 {
			case 0:
				id, err := tracker.NewCorrelation("console-1", "actor-1", "ask")
				if modelCount == 0 {
					if err != nil || id == "" {
						return false
					}
					activeID = id
					modelCount = 1
				} else if !errors.Is(err, ErrMaxInFlightReached) || id != "" {
					return false
				}
			case 1:
				tracker.Cancel(activeID)
			case 2:
				tracker.AppendStreamed(activeID, "x")
			case 3:
				tracker.Complete(activeID)
				activeID = ""
				modelCount = 0
			case 4:
				tracker.Complete("missing-correlation")
			case 5:
				tracker.Clear()
				activeID = ""
				modelCount = 0
			}

			if tracker.Count() != modelCount {
				return false
			}
			if tracker.IsFull() != (modelCount == tracker.MaxInFlight()) {
				return false
			}
			if modelCount == 0 && tracker.GetActive() != nil {
				return false
			}
			if modelCount == 1 && tracker.GetPending(activeID) == nil {
				return false
			}
		}
		return true
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatal(err)
	}
}
