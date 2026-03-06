package orchestration

import "testing"

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
