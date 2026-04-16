package tui

import "testing"

func TestCompactFooterTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "empty after trim",
			input:  "   ",
			expect: "",
		},
		{
			name:   "short target unchanged",
			input:  "corr-123",
			expect: "corr-123",
		},
		{
			name:   "long target truncated deterministically",
			input:  "12345678-aaaa-bbbb-cccc-1234567890ab",
			expect: "12345678...7890ab",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := compactFooterTarget(tc.input)
			if got != tc.expect {
				t.Fatalf("compactFooterTarget(%q) = %q, want %q", tc.input, got, tc.expect)
			}
		})
	}
}

func TestFooterTargetText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		cancelEnabled bool
		inFlight      string
		expect        string
	}{
		{
			name:          "cancel disabled",
			cancelEnabled: false,
			inFlight:      "corr-123",
			expect:        "",
		},
		{
			name:          "cancel enabled broad",
			cancelEnabled: true,
			inFlight:      "",
			expect:        "cancel target: broad",
		},
		{
			name:          "cancel enabled short target",
			cancelEnabled: true,
			inFlight:      "corr-123",
			expect:        "cancel target: corr-123",
		},
		{
			name:          "cancel enabled long target",
			cancelEnabled: true,
			inFlight:      "12345678-aaaa-bbbb-cccc-1234567890ab",
			expect:        "cancel target: 12345678...7890ab",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := footerTargetText(tc.cancelEnabled, tc.inFlight)
			if got != tc.expect {
				t.Fatalf("footerTargetText(%t, %q) = %q, want %q", tc.cancelEnabled, tc.inFlight, got, tc.expect)
			}
		})
	}
}
