package orchestration

import "testing"

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
