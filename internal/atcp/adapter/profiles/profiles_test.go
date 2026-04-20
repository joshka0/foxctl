package profiles

import (
	"regexp"
	"testing"
)

func TestDefaultReadinessKnownProfilesMatchPromptSamples(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{name: "codex", line: "›   Type your message"},
		{name: "droid", line: "│ > "},
		{name: "gemini", line: " >   Type your message or @path/to/file"},
		{name: "claude", line: "❯\u00a0"},
		{name: "claude-code", line: "❯\u00a0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DefaultReadiness(tt.name)
			if got.ScreenRegex == "" {
				t.Fatalf("DefaultReadiness(%q) missing screen regex", tt.name)
			}
			if got.ThresholdBPS <= 0 {
				t.Fatalf("DefaultReadiness(%q) threshold = %f", tt.name, got.ThresholdBPS)
			}
			if got.Debounce <= 0 {
				t.Fatalf("DefaultReadiness(%q) debounce = %v", tt.name, got.Debounce)
			}
			if matched, err := regexp.MatchString(got.ScreenRegex, tt.line); err != nil || !matched {
				t.Fatalf("DefaultReadiness(%q) regex %q did not match %q (err=%v)",
					tt.name, got.ScreenRegex, tt.line, err)
			}
		})
	}
}

func TestDefaultReadinessUnknownProfileKeepsByteIdleDefaults(t *testing.T) {
	got := DefaultReadiness("unknown")
	if got.ScreenRegex != "" {
		t.Fatalf("ScreenRegex = %q, want empty", got.ScreenRegex)
	}
	if got.ThresholdBPS <= 0 {
		t.Fatalf("ThresholdBPS = %f, want positive default", got.ThresholdBPS)
	}
	if got.Debounce <= 0 {
		t.Fatalf("Debounce = %v, want positive default", got.Debounce)
	}
}

func TestNamesReturnsCanonicalProfilesOnly(t *testing.T) {
	got := Names()
	want := []string{"codex", "droid", "gemini", "claude"}
	if len(got) != len(want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names() = %v, want %v", got, want)
		}
	}
}
