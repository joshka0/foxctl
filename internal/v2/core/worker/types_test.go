package worker

import (
	"strings"
	"testing"
	"testing/quick"
)

func TestNormalizeStatusMapsBackendVocabulary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want Status
	}{
		{name: "blank is unknown", raw: "   ", want: StatusUnknown},
		{name: "pending", raw: "pending", want: StatusPending},
		{name: "starting", raw: "starting", want: StatusStarting},
		{name: "running", raw: "running", want: StatusRunning},
		{name: "active synonym", raw: "active", want: StatusRunning},
		{name: "processing synonym", raw: "processing", want: StatusRunning},
		{name: "stopping", raw: "stopping", want: StatusStopping},
		{name: "completed", raw: "completed", want: StatusCompleted},
		{name: "ok synonym", raw: "ok", want: StatusCompleted},
		{name: "success synonym", raw: "success", want: StatusCompleted},
		{name: "done synonym", raw: "done", want: StatusCompleted},
		{name: "failed", raw: "failed", want: StatusFailed},
		{name: "error synonym", raw: "error", want: StatusFailed},
		{name: "cancelled", raw: "cancelled", want: StatusCancelled},
		{name: "canceled synonym", raw: "canceled", want: StatusCancelled},
		{name: "unknown backend value", raw: "paused", want: StatusUnknown},
		{name: "case and space normalized", raw: "  SuCcEsS  ", want: StatusCompleted},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := NormalizeStatus(tt.raw); got != tt.want {
				t.Fatalf("NormalizeStatus(%q)=%q want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestNormalizeStatusCanonicalStatusProperty(t *testing.T) {
	t.Parallel()

	canonical := map[string]Status{
		string(StatusUnknown):   StatusUnknown,
		string(StatusPending):   StatusPending,
		string(StatusStarting):  StatusStarting,
		string(StatusRunning):   StatusRunning,
		string(StatusStopping):  StatusStopping,
		string(StatusCompleted): StatusCompleted,
		string(StatusFailed):    StatusFailed,
		string(StatusCancelled): StatusCancelled,
	}
	statuses := []Status{
		StatusUnknown,
		StatusPending,
		StatusStarting,
		StatusRunning,
		StatusStopping,
		StatusCompleted,
		StatusFailed,
		StatusCancelled,
	}

	property := func(seed uint8, prefixSpace bool, suffixSpace bool) bool {
		want := statuses[int(seed)%len(statuses)]
		raw := strings.ToUpper(string(want))
		if prefixSpace {
			raw = "\t " + raw
		}
		if suffixSpace {
			raw += " \n"
		}

		got := NormalizeStatus(raw)
		return got == canonical[string(want)] && NormalizeStatus(string(got)) == got
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 128}); err != nil {
		t.Fatalf("canonical status normalization property failed: %v", err)
	}
}

func TestIsTerminalOnlyForCompletedFailedOrCancelled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status Status
		want   bool
	}{
		{status: StatusUnknown, want: false},
		{status: StatusPending, want: false},
		{status: StatusStarting, want: false},
		{status: StatusRunning, want: false},
		{status: StatusStopping, want: false},
		{status: StatusCompleted, want: true},
		{status: StatusFailed, want: true},
		{status: StatusCancelled, want: true},
		{status: Status("paused"), want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.status), func(t *testing.T) {
			t.Parallel()

			if got := IsTerminal(tt.status); got != tt.want {
				t.Fatalf("IsTerminal(%q)=%v want %v", tt.status, got, tt.want)
			}
		})
	}
}
