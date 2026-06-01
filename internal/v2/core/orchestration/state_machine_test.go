package orchestration

import (
	"testing"
	"testing/quick"
)

func TestTransition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current State
		event   Event
		want    State
	}{
		{name: "discovered -> unclaimed", current: StateReleased, event: EventIssueDiscovered, want: StateUnclaimed},
		{name: "claimed -> claimed", current: StateUnclaimed, event: EventIssueClaimed, want: StateClaimed},
		{name: "dispatched -> running", current: StateClaimed, event: EventIssueDispatched, want: StateRunning},
		{name: "retry queued -> retry", current: StateRunning, event: EventIssueRetryQueue, want: StateRetryQueue},
		{name: "released -> released", current: StateRetryQueue, event: EventIssueReleased, want: StateReleased},
		{name: "blocked -> released", current: StateRunning, event: EventIssueBlocked, want: StateReleased},
		{name: "handoff -> released", current: StateRunning, event: EventIssueHandoff, want: StateReleased},
		{name: "done -> released", current: StateRunning, event: EventIssueDone, want: StateReleased},
		{name: "unknown event keeps state", current: StateClaimed, event: Event("no-op"), want: StateClaimed},
		{name: "event normalization", current: StateClaimed, event: Event(" ISSUE.DISPATCHED "), want: StateRunning},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := Transition(tc.current, tc.event)
			if got != tc.want {
				t.Fatalf("Transition(%q, %q)=%q want %q", tc.current, tc.event, got, tc.want)
			}
		})
	}
}

func TestApplyTransitions(t *testing.T) {
	t.Parallel()

	got := ApplyTransitions(StateUnclaimed, []Event{
		EventIssueClaimed,
		EventIssueDispatched,
		EventIssueRetryQueue,
		EventIssueDispatched,
		EventIssueDone,
	})
	if got != StateReleased {
		t.Fatalf("ApplyTransitions(...)=%q want %q", got, StateReleased)
	}
}

func TestApplyTransitionsPropertyLastRecognizedEventWinsInOrder(t *testing.T) {
	t.Parallel()

	property := func(initialSeed uint8, eventSeeds []uint8) bool {
		initial := generatedInitialState(initialSeed)
		events := make([]Event, 0, len(eventSeeds))
		want := initial
		for _, seed := range eventSeeds {
			event, state, recognized := generatedTransitionEvent(seed)
			events = append(events, event)
			if recognized {
				want = state
			}
		}

		got := ApplyTransitions(initial, events)
		if got != want {
			t.Logf("ApplyTransitions(%q, %#v)=%q want %q", initial, events, got, want)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func generatedInitialState(seed uint8) State {
	states := []State{
		StateUnclaimed,
		StateClaimed,
		StateRunning,
		StateRetryQueue,
		StateReleased,
		State("custom-projection-state"),
	}
	return states[int(seed)%len(states)]
}

func generatedTransitionEvent(seed uint8) (Event, State, bool) {
	switch seed % 12 {
	case 0:
		return EventIssueDiscovered, StateUnclaimed, true
	case 1:
		return Event(" ISSUE.DISCOVERED "), StateUnclaimed, true
	case 2:
		return EventIssueClaimed, StateClaimed, true
	case 3:
		return Event(" ISSUE.CLAIMED "), StateClaimed, true
	case 4:
		return EventIssueDispatched, StateRunning, true
	case 5:
		return Event(" issue.dispatched "), StateRunning, true
	case 6:
		return EventIssueRetryQueue, StateRetryQueue, true
	case 7:
		return Event(" ISSUE.RETRY_QUEUED "), StateRetryQueue, true
	case 8:
		return EventIssueReleased, StateReleased, true
	case 9:
		return EventIssueBlocked, StateReleased, true
	case 10:
		return EventIssueDone, StateReleased, true
	default:
		return Event("issue.unknown"), "", false
	}
}
