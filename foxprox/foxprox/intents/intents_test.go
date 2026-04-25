package intents

import "testing"

func TestParseBracketed(t *testing.T) {
	cases := map[string]BracketedPolicy{
		"":        BracketedAuto,
		"auto":    BracketedAuto,
		"AUTO":    BracketedAuto, // unknown case-sensitive is auto fallback
		"force":   BracketedForce,
		"off":     BracketedOff,
		"unknown": BracketedAuto,
	}
	for in, want := range cases {
		if got := ParseBracketed(in); got != want {
			t.Errorf("ParseBracketed(%q) = %v, want %v", in, got, want)
		}
	}
}
