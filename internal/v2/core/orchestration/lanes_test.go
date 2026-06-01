package orchestration

import (
	"strings"
	"testing"
	"testing/quick"
)

func TestDeriveLane_Precedence(t *testing.T) {
	t.Parallel()

	opts := LaneOptions{
		TerminalTrackerStates: []string{"done", "closed"},
		ReviewTrackerStates:   []string{"human review"},
	}

	tests := []struct {
		name string
		card Card
		want Lane
	}{
		{
			name: "terminal state wins",
			card: Card{
				State:        StateRunning,
				TrackerState: "Done",
				PolicyStatus: PolicyStatusDenied,
			},
			want: LaneDone,
		},
		{
			name: "review state wins before policy block",
			card: Card{
				State:        StateRunning,
				TrackerState: "Human Review",
				PolicyStatus: PolicyStatusDenied,
			},
			want: LaneReview,
		},
		{
			name: "policy denied is blocked",
			card: Card{
				State:        StateRunning,
				TrackerState: "In Progress",
				PolicyStatus: PolicyStatusDenied,
			},
			want: LaneBlocked,
		},
		{
			name: "outcome denied is blocked",
			card: Card{
				State:       StateClaimed,
				LastOutcome: OutcomeSpawnDenied,
			},
			want: LaneBlocked,
		},
		{
			name: "running maps running",
			card: Card{
				State:        StateRunning,
				TrackerState: "In Progress",
				PolicyStatus: PolicyStatusOK,
				Eligibility:  EligibilityEligible,
			},
			want: LaneRunning,
		},
		{
			name: "running remains running when ineligible",
			card: Card{
				State:        StateRunning,
				TrackerState: "In Progress",
				PolicyStatus: PolicyStatusOK,
				Eligibility:  EligibilityIneligible,
			},
			want: LaneRunning,
		},
		{
			name: "retry maps retry queued",
			card: Card{
				State:        StateRetryQueue,
				PolicyStatus: PolicyStatusOK,
				Eligibility:  EligibilityEligible,
			},
			want: LaneRetryQueue,
		},
		{
			name: "retry remains retry queued when ineligible",
			card: Card{
				State:        StateRetryQueue,
				PolicyStatus: PolicyStatusOK,
				Eligibility:  EligibilityIneligible,
			},
			want: LaneRetryQueue,
		},
		{
			name: "claimed maps claimed",
			card: Card{
				State:        StateClaimed,
				PolicyStatus: PolicyStatusOK,
				Eligibility:  EligibilityEligible,
			},
			want: LaneClaimed,
		},
		{
			name: "claimed remains claimed when ineligible",
			card: Card{
				State:        StateClaimed,
				PolicyStatus: PolicyStatusOK,
				Eligibility:  EligibilityIneligible,
			},
			want: LaneClaimed,
		},
		{
			name: "released eligible maps todo",
			card: Card{
				State:        StateReleased,
				PolicyStatus: PolicyStatusOK,
				Eligibility:  EligibilityEligible,
			},
			want: LaneTodo,
		},
		{
			name: "released ineligible maps blocked",
			card: Card{
				State:        StateReleased,
				PolicyStatus: PolicyStatusOK,
				Eligibility:  EligibilityIneligible,
			},
			want: LaneBlocked,
		},
		{
			name: "unknown state still total",
			card: Card{
				State:        State("mystery"),
				PolicyStatus: PolicyStatusOK,
				Eligibility:  EligibilityEligible,
			},
			want: LaneTodo,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := DeriveLane(tc.card, opts)
			if got != tc.want {
				t.Fatalf("DeriveLane(%+v)=%q want %q", tc.card, got, tc.want)
			}
		})
	}
}

func TestEnsureLaneCounts_FillsMissingLanes(t *testing.T) {
	t.Parallel()

	counts := EnsureLaneCounts(map[Lane]int{
		LaneRunning: 3,
		LaneDone:    2,
	})

	if got := counts[LaneRunning]; got != 3 {
		t.Fatalf("running count=%d want 3", got)
	}
	if got := counts[LaneDone]; got != 2 {
		t.Fatalf("done count=%d want 2", got)
	}
	for _, lane := range LaneOrder() {
		if _, ok := counts[lane]; !ok {
			t.Fatalf("missing lane count for %q", lane)
		}
	}
}

func TestDeriveLanePropertyTerminalTrackerWins(t *testing.T) {
	t.Parallel()

	property := func(seed laneProjectionSeed) bool {
		card := seed.card()
		card.TrackerState = seed.spacedCase("terminal")
		got := DeriveLane(card, LaneOptions{
			TerminalTrackerStates: []string{" terminal "},
			ReviewTrackerStates:   []string{"review"},
		})
		if got != LaneDone {
			t.Logf("terminal tracker DeriveLane(%+v)=%q want %q", card, got, LaneDone)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 256}); err != nil {
		t.Fatalf("terminal tracker precedence property failed: %v", err)
	}
}

func TestDeriveLanePropertyReviewTrackerWinsBeforePolicy(t *testing.T) {
	t.Parallel()

	property := func(seed laneProjectionSeed) bool {
		card := seed.card()
		card.TrackerState = seed.spacedCase("review")
		card.PolicyStatus = generatedBlockedPolicyStatus(seed.Policy)
		card.LastOutcome = generatedBlockedOutcome(seed.Outcome)
		got := DeriveLane(card, LaneOptions{
			TerminalTrackerStates: []string{"done"},
			ReviewTrackerStates:   []string{" review "},
		})
		if got != LaneReview {
			t.Logf("review tracker DeriveLane(%+v)=%q want %q", card, got, LaneReview)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 256}); err != nil {
		t.Fatalf("review tracker precedence property failed: %v", err)
	}
}

func TestDeriveLanePropertyPolicyStatusBlockWinsBeforeRuntimeState(t *testing.T) {
	t.Parallel()

	property := func(seed laneProjectionSeed) bool {
		card := seed.card()
		card.TrackerState = "in progress"
		card.PolicyStatus = generatedBlockedPolicyStatus(seed.Policy)
		card.LastOutcome = OutcomeExecFailed
		got := DeriveLane(card, LaneOptions{
			TerminalTrackerStates: []string{"done"},
			ReviewTrackerStates:   []string{"review"},
		})
		if got != LaneBlocked {
			t.Logf("policy status block DeriveLane(%+v)=%q want %q", card, got, LaneBlocked)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 256}); err != nil {
		t.Fatalf("policy status block precedence property failed: %v", err)
	}
}

func TestDeriveLanePropertyOutcomeBlockWinsBeforeRuntimeState(t *testing.T) {
	t.Parallel()

	property := func(seed laneProjectionSeed) bool {
		card := seed.card()
		card.TrackerState = "in progress"
		card.PolicyStatus = PolicyStatusOK
		card.LastOutcome = generatedBlockedOutcome(seed.Outcome)
		got := DeriveLane(card, LaneOptions{
			TerminalTrackerStates: []string{"done"},
			ReviewTrackerStates:   []string{"review"},
		})
		if got != LaneBlocked {
			t.Logf("outcome block DeriveLane(%+v)=%q want %q", card, got, LaneBlocked)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 256}); err != nil {
		t.Fatalf("outcome block precedence property failed: %v", err)
	}
}

func TestDeriveLanePropertyRuntimeStatesIgnoreEligibilityWhenUnblocked(t *testing.T) {
	t.Parallel()

	property := func(seed laneProjectionSeed) bool {
		state, want := generatedRuntimeLane(seed.State)
		card := Card{
			State:        state,
			PolicyStatus: PolicyStatusOK,
			Eligibility:  generatedEligibility(seed.Eligibility),
		}
		got := DeriveLane(card, DefaultLaneOptions())
		if got != want {
			t.Logf("runtime state DeriveLane(%+v)=%q want %q", card, got, want)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 256}); err != nil {
		t.Fatalf("runtime state lane property failed: %v", err)
	}
}

func TestDeriveLanePropertyInactiveStatesFollowEligibilityWhenUnblocked(t *testing.T) {
	t.Parallel()

	property := func(seed laneProjectionSeed) bool {
		card := Card{
			State:        generatedInactiveState(seed.State),
			PolicyStatus: PolicyStatusOK,
			Eligibility:  generatedEligibility(seed.Eligibility),
		}
		want := LaneBlocked
		if card.Eligibility == EligibilityEligible {
			want = LaneTodo
		}
		got := DeriveLane(card, DefaultLaneOptions())
		if got != want {
			t.Logf("inactive state DeriveLane(%+v)=%q want %q", card, got, want)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 256}); err != nil {
		t.Fatalf("inactive state lane property failed: %v", err)
	}
}

type laneProjectionSeed struct {
	State       uint8
	Policy      uint8
	Outcome     uint8
	Eligibility uint8
	Case        uint8
}

func (s laneProjectionSeed) card() Card {
	return Card{
		State:        generatedLaneState(s.State),
		PolicyStatus: generatedPolicyStatus(s.Policy),
		LastOutcome:  generatedOutcome(s.Outcome),
		Eligibility:  generatedEligibility(s.Eligibility),
	}
}

func (s laneProjectionSeed) spacedCase(value string) string {
	switch s.Case % 4 {
	case 0:
		return value
	case 1:
		return " " + value + " "
	case 2:
		return strings.ToUpper(value)
	default:
		return " " + strings.ToUpper(value) + " "
	}
}

func generatedLaneState(seed uint8) State {
	states := []State{
		StateUnclaimed,
		StateClaimed,
		StateRunning,
		StateRetryQueue,
		StateReleased,
		State("mystery"),
	}
	return states[int(seed)%len(states)]
}

func generatedRuntimeLane(seed uint8) (State, Lane) {
	pairs := []struct {
		state State
		lane  Lane
	}{
		{state: StateRunning, lane: LaneRunning},
		{state: StateRetryQueue, lane: LaneRetryQueue},
		{state: StateClaimed, lane: LaneClaimed},
	}
	pair := pairs[int(seed)%len(pairs)]
	return pair.state, pair.lane
}

func generatedInactiveState(seed uint8) State {
	states := []State{
		StateUnclaimed,
		StateReleased,
		State("mystery"),
	}
	return states[int(seed)%len(states)]
}

func generatedPolicyStatus(seed uint8) PolicyStatus {
	statuses := []PolicyStatus{
		PolicyStatusOK,
		PolicyStatusDenied,
		PolicyStatusBlocked,
		PolicyStatusValidationError,
		PolicyStatus("unknown"),
	}
	return statuses[int(seed)%len(statuses)]
}

func generatedBlockedPolicyStatus(seed uint8) PolicyStatus {
	statuses := []PolicyStatus{
		PolicyStatusDenied,
		PolicyStatusBlocked,
		PolicyStatusValidationError,
	}
	return statuses[int(seed)%len(statuses)]
}

func generatedOutcome(seed uint8) Outcome {
	outcomes := []Outcome{
		"",
		OutcomeSpawnDenied,
		OutcomePolicyDenied,
		OutcomePreflightErr,
		OutcomeExecFailed,
		Outcome("unknown"),
	}
	return outcomes[int(seed)%len(outcomes)]
}

func generatedBlockedOutcome(seed uint8) Outcome {
	outcomes := []Outcome{
		OutcomeSpawnDenied,
		OutcomePolicyDenied,
		OutcomePreflightErr,
	}
	return outcomes[int(seed)%len(outcomes)]
}

func generatedEligibility(seed uint8) Eligibility {
	values := []Eligibility{
		EligibilityEligible,
		EligibilityIneligible,
		Eligibility("unknown"),
		"",
	}
	return values[int(seed)%len(values)]
}
