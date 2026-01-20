package memory

import (
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{
			name:     "empty string",
			text:     "",
			expected: 0,
		},
		{
			name:     "short text",
			text:     "hello",
			expected: 1, // 5 chars / 4 = 1
		},
		{
			name:     "medium text",
			text:     "hello world",
			expected: 2, // 11 chars / 4 = 2
		},
		{
			name:     "longer text",
			text:     "The quick brown fox jumps over the lazy dog",
			expected: 10, // 43 chars / 4 = 10
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTokens(tt.text)
			if got != tt.expected {
				t.Errorf("EstimateTokens(%q) = %d, want %d", tt.text, got, tt.expected)
			}
		})
	}
}

func TestEstimateTokensWithOverhead(t *testing.T) {
	text := "hello world" // 11 chars = 2 tokens
	overhead := 5

	got := EstimateTokensWithOverhead(text, overhead)
	expected := 7 // 2 + 5

	if got != expected {
		t.Errorf("EstimateTokensWithOverhead() = %d, want %d", got, expected)
	}
}

func TestFitsInBudget(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		budget   int
		expected bool
	}{
		{
			name:     "fits within budget",
			text:     "hello", // 1 token
			budget:   10,      // effective = 8
			expected: true,
		},
		{
			name:     "exactly at effective budget",
			text:     "12345678901234567890123456789012", // 32 chars = 8 tokens
			budget:   10,                                 // effective = 8
			expected: true,
		},
		{
			name:     "exceeds effective budget",
			text:     "123456789012345678901234567890123456", // 36 chars = 9 tokens
			budget:   10,                                     // effective = 8
			expected: false,
		},
		{
			name:     "empty text always fits",
			text:     "",
			budget:   1,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FitsInBudget(tt.text, tt.budget)
			if got != tt.expected {
				t.Errorf("FitsInBudget(%q, %d) = %v, want %v", tt.text, tt.budget, got, tt.expected)
			}
		})
	}
}

func TestRemainingBudget(t *testing.T) {
	tests := []struct {
		name     string
		used     int
		budget   int
		expected int
	}{
		{
			name:     "nothing used",
			used:     0,
			budget:   100, // effective = 80
			expected: 80,
		},
		{
			name:     "some used",
			used:     30,
			budget:   100, // effective = 80
			expected: 50,
		},
		{
			name:     "all used",
			used:     80,
			budget:   100,
			expected: 0,
		},
		{
			name:     "over budget returns zero",
			used:     100,
			budget:   100,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RemainingBudget(tt.used, tt.budget)
			if got != tt.expected {
				t.Errorf("RemainingBudget(%d, %d) = %d, want %d", tt.used, tt.budget, got, tt.expected)
			}
		})
	}
}

func TestTruncateToFit(t *testing.T) {
	tests := []struct {
		name              string
		text              string
		budget            int
		expectedText      string
		expectedTruncated bool
	}{
		{
			name:              "no truncation needed",
			text:              "hello",
			budget:            100,
			expectedText:      "hello",
			expectedTruncated: false,
		},
		{
			name:              "truncation required",
			text:              "The quick brown fox jumps over the lazy dog",
			budget:            5, // effective = 4 tokens = 16 chars
			expectedText:      "The quick bro...",
			expectedTruncated: true,
		},
		{
			name:              "empty text",
			text:              "",
			budget:            1,
			expectedText:      "",
			expectedTruncated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotText, gotTruncated := TruncateToFit(tt.text, tt.budget)
			if gotText != tt.expectedText {
				t.Errorf("TruncateToFit() text = %q, want %q", gotText, tt.expectedText)
			}
			if gotTruncated != tt.expectedTruncated {
				t.Errorf("TruncateToFit() truncated = %v, want %v", gotTruncated, tt.expectedTruncated)
			}
		})
	}
}

func TestTruncateToFitWithMarginTail(t *testing.T) {
	text := "abcdefghijkl"
	gotText, gotTruncated := TruncateToFitWithMargin(text, 2, 1.0, true)

	if gotText != "...hijkl" {
		t.Errorf("TruncateToFitWithMargin tail = %q, want %q", gotText, "...hijkl")
	}
	if !gotTruncated {
		t.Error("expected truncation")
	}
}

func TestNewTokenBudgetWithMargin(t *testing.T) {
	b := NewTokenBudgetWithMargin(100, 1.0)

	if b.Total != 100 {
		t.Errorf("Total = %d, want 100", b.Total)
	}
	if b.Remaining != 100 {
		t.Errorf("Remaining = %d, want 100", b.Remaining)
	}
}

func TestTokenBudget(t *testing.T) {
	t.Run("new budget", func(t *testing.T) {
		b := NewTokenBudget(100)

		if b.Total != 100 {
			t.Errorf("Total = %d, want 100", b.Total)
		}
		if b.Used != 0 {
			t.Errorf("Used = %d, want 0", b.Used)
		}
		if b.Remaining != 80 { // 80% of 100
			t.Errorf("Remaining = %d, want 80", b.Remaining)
		}
	})

	t.Run("add tokens", func(t *testing.T) {
		b := NewTokenBudget(100)
		b.Add(30)

		if b.Used != 30 {
			t.Errorf("Used = %d, want 30", b.Used)
		}
		if b.Remaining != 50 { // 80 - 30
			t.Errorf("Remaining = %d, want 50", b.Remaining)
		}
	})

	t.Run("add text", func(t *testing.T) {
		b := NewTokenBudget(100)
		tokens := b.AddText("hello world") // 11 chars = 2 tokens

		if tokens != 2 {
			t.Errorf("AddText returned %d, want 2", tokens)
		}
		if b.Used != 2 {
			t.Errorf("Used = %d, want 2", b.Used)
		}
	})

	t.Run("can fit", func(t *testing.T) {
		b := NewTokenBudget(100)
		b.Add(70)

		if !b.CanFit(10) {
			t.Error("CanFit(10) should be true")
		}
		if b.CanFit(20) {
			t.Error("CanFit(20) should be false")
		}
	})

	t.Run("can fit text", func(t *testing.T) {
		b := NewTokenBudget(100)
		b.Add(75)

		if !b.CanFitText("hi") { // 0 tokens
			t.Error("CanFitText('hi') should be true")
		}
		if b.CanFitText("hello world this is a longer text") {
			t.Error("CanFitText(longer) should be false")
		}
	})

	t.Run("reset", func(t *testing.T) {
		b := NewTokenBudget(100)
		b.Add(50)
		b.Reset()

		if b.Used != 0 {
			t.Errorf("Used after reset = %d, want 0", b.Used)
		}
		if b.Remaining != 80 {
			t.Errorf("Remaining after reset = %d, want 80", b.Remaining)
		}
	})

	t.Run("usage percent", func(t *testing.T) {
		b := NewTokenBudget(100)
		b.Add(50)

		got := b.UsagePercent()
		if got != 50.0 {
			t.Errorf("UsagePercent = %f, want 50.0", got)
		}
	})

	t.Run("usage percent zero budget", func(t *testing.T) {
		b := NewTokenBudget(0)
		got := b.UsagePercent()
		if got != 0 {
			t.Errorf("UsagePercent with zero budget = %f, want 0", got)
		}
	})
}
