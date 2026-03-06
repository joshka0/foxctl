package orchestration

import "strings"

// Event is the canonical orchestration event marker used to derive internal state.
type Event string

const (
	EventIssueDiscovered Event = "issue.discovered"
	EventIssueClaimed    Event = "issue.claimed"
	EventIssueDispatched Event = "issue.dispatched"
	EventIssueRetryQueue Event = "issue.retry_queued"
	EventIssueReleased   Event = "issue.released"
	EventIssueBlocked    Event = "issue.blocked"
	EventIssueHandoff    Event = "issue.handoff"
	EventIssueDone       Event = "issue.done"
)

// Transition applies one canonical event transition.
//
// Blocked/review/done remain projection concerns. Internal state collapses to
// released when execution is no longer active.
func Transition(current State, event Event) State {
	switch normalizeEvent(event) {
	case strings.ToLower(string(EventIssueDiscovered)):
		return StateUnclaimed
	case strings.ToLower(string(EventIssueClaimed)):
		return StateClaimed
	case strings.ToLower(string(EventIssueDispatched)):
		return StateRunning
	case strings.ToLower(string(EventIssueRetryQueue)):
		return StateRetryQueue
	case strings.ToLower(string(EventIssueReleased)),
		strings.ToLower(string(EventIssueBlocked)),
		strings.ToLower(string(EventIssueHandoff)),
		strings.ToLower(string(EventIssueDone)):
		return StateReleased
	default:
		return current
	}
}

// ApplyTransitions deterministically applies events in order.
func ApplyTransitions(initial State, events []Event) State {
	state := initial
	for _, evt := range events {
		state = Transition(state, evt)
	}
	return state
}

func normalizeEvent(event Event) string {
	return strings.ToLower(strings.TrimSpace(string(event)))
}
