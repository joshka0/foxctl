package cmd

import "testing"

func TestParseTranscriptMemoryLane(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want transcriptMemoryLane
		ok   bool
	}{
		{name: "empty defaults insight", in: "", want: transcriptMemoryLaneInsight, ok: true},
		{name: "doctrine", in: "doctrine", want: transcriptMemoryLaneDoctrine, ok: true},
		{name: "insight", in: "insight", want: transcriptMemoryLaneInsight, ok: true},
		{name: "mixed", in: "mixed", want: transcriptMemoryLaneMixed, ok: true},
		{name: "trimmed case insensitive", in: " Insight ", want: transcriptMemoryLaneInsight, ok: true},
		{name: "invalid", in: "auto", ok: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseTranscriptMemoryLane(tc.in)
			if tc.ok {
				if err != nil {
					t.Fatalf("parse error = %v", err)
				}
				if got != tc.want {
					t.Fatalf("lane=%q want %q", got, tc.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error for %q", tc.in)
			}
		})
	}
}
