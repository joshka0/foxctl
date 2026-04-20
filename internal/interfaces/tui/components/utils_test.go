package components

import (
	"testing"
	"unicode/utf8"

	"github.com/grindlemire/go-tui"
)

// ---------------------------------------------------------------------------
// TestTruncate
// ---------------------------------------------------------------------------

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxCells int
		want     string
	}{
		{
			name:     "ASCII_short_no_truncate",
			input:    "hello",
			maxCells: 10,
			want:     "hello",
		},
		{
			name:     "ASCII_exact_width",
			input:    "hello",
			maxCells: 5,
			want:     "hello",
		},
		{
			name:     "ASCII_truncate_with_ellipsis",
			input:    "hello world",
			maxCells: 5,
			want:     "hell…",
		},
		{
			name:     "ASCII_truncate_to_ellipsis_only",
			input:    "hello",
			maxCells: 1,
			want:     "…",
		},
		{
			name:     "zero_max_cells",
			input:    "hello",
			maxCells: 0,
			want:     "",
		},
		{
			name:     "CJK_exact_width_no_truncate",
			input:    "研究",
			maxCells: 4,
			want:     "研究",
		},
		{
			name:     "CJK_truncate_with_ellipsis",
			input:    "研究員",
			maxCells: 4,
			want:     "研…",
		},
		{
			name:     "CJK_truncate_mid_char",
			input:    "研究員エージェント",
			maxCells: 5,
			want:     "研究…",
		},
		{
			name:     "mixed_ASCII_CJK",
			input:    "a研究b",
			maxCells: 4,
			want:     "a研…",
		},
		{
			name:     "empty_string",
			input:    "",
			maxCells: 5,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.input, tt.maxCells)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q; want %q", tt.input, tt.maxCells, got, tt.want)
			}
			// Verify display width never exceeds maxCells.
			if tt.maxCells > 0 {
				dw := runeWidth(got)
				if dw > tt.maxCells {
					t.Errorf("truncate(%q, %d) result %q has display width %d > %d", tt.input, tt.maxCells, got, dw, tt.maxCells)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestRuneWidth
// ---------------------------------------------------------------------------

func TestRuneWidth(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{
			name:  "ASCII",
			input: "hello",
			want:  5,
		},
		{
			name:  "CJK",
			input: "研究員",
			want:  6,
		},
		{
			name:  "mixed_ASCII_CJK",
			input: "a研究b",
			want:  6,
		},
		{
			name:  "empty",
			input: "",
			want:  0,
		},
		{
			name:  "combining_marks",
			input: "Ag\u0301nt",
			want:  5,
		},
		{
			name:  "ZWJ_emoji",
			input: "\U0001F468\u200D\U0001F4BB",
			want:  5, // go-tui RuneWidth returns 5 for this ZWJ sequence (1+0+1+0+2)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runeWidth(tt.input)
			if got != tt.want {
				t.Errorf("runeWidth(%q) = %d; want %d", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestCenter
// ---------------------------------------------------------------------------

func TestCenter(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		width  int
		wantDw int // expected display width
	}{
		{
			name:   "ASCII_centered",
			input:  "hello",
			width:  11,
			wantDw: 11,
		},
		{
			name:   "ASCII_exact_width",
			input:  "hello",
			width:  5,
			wantDw: 5,
		},
		{
			name:   "ASCII_truncate",
			input:  "hello world",
			width:  5,
			wantDw: 5,
		},
		{
			name:   "CJK_centered",
			input:  "研究",
			width:  10,
			wantDw: 10,
		},
		{
			name:   "CJK_exact_width",
			input:  "研究",
			width:  4,
			wantDw: 4,
		},
		{
			name:   "CJK_truncate",
			input:  "研究員",
			width:  4,
			wantDw: 3,
		},
		{
			name:   "mixed_ASCII_CJK_centered",
			input:  "a研究b",
			width:  10,
			wantDw: 10,
		},
		{
			name:   "zero_width_returns_input",
			input:  "hello",
			width:  0,
			wantDw: 5,
		},
		{
			name:   "empty_string",
			input:  "",
			width:  5,
			wantDw: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := center(tt.input, tt.width)
			dw := runeWidth(got)
			if dw != tt.wantDw {
				t.Errorf("center(%q, %d) display width = %d; want %d (got: %q)", tt.input, tt.width, dw, tt.wantDw, got)
			}
			// Verify the original string is present (unless truncated).
			if tt.width > 0 && runeWidth(tt.input) <= tt.width {
				if !containsRunes(got, tt.input) {
					t.Errorf("center(%q, %d) = %q; does not contain input", tt.input, tt.width, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestPadOrTruncate
// ---------------------------------------------------------------------------

func TestPadOrTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		width  int
		wantDw int
		want   string
	}{
		{
			name:   "ASCII_pad",
			input:  "hi",
			width:  5,
			wantDw: 5,
			want:   "hi   ",
		},
		{
			name:   "ASCII_exact",
			input:  "hello",
			width:  5,
			wantDw: 5,
			want:   "hello",
		},
		{
			name:   "ASCII_truncate",
			input:  "hello world",
			width:  5,
			wantDw: 5,
			want:   "hell…",
		},
		{
			name:   "CJK_pad",
			input:  "研",
			width:  4,
			wantDw: 4,
			want:   "研  ",
		},
		{
			name:   "CJK_exact",
			input:  "研究",
			width:  4,
			wantDw: 4,
			want:   "研究",
		},
		{
			name:   "CJK_truncate",
			input:  "研究員",
			width:  4,
			wantDw: 3,
			want:   "研…",
		},
		{
			name:   "mixed_ASCII_CJK_pad",
			input:  "a研",
			width:  6,
			wantDw: 6,
			want:   "a研   ",
		},
		{
			name:   "zero_width",
			input:  "hello",
			width:  0,
			wantDw: 0,
			want:   "",
		},
		{
			name:   "width_one",
			input:  "hello",
			width:  1,
			wantDw: 1,
			want:   "…",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := padOrTruncate(tt.input, tt.width)
			if got != tt.want {
				t.Errorf("padOrTruncate(%q, %d) = %q; want %q", tt.input, tt.width, got, tt.want)
			}
			dw := runeWidth(got)
			if dw != tt.wantDw {
				t.Errorf("padOrTruncate(%q, %d) display width = %d; want %d", tt.input, tt.width, dw, tt.wantDw)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// containsRunes reports whether s contains all runes of substr in order.
func containsRunes(s, substr string) bool {
	idx := 0
	for _, r := range substr {
		found := false
		for ; idx < len(s); {
			sr, size := utf8.DecodeRuneInString(s[idx:])
			if sr == r {
				idx += size
				found = true
				break
			}
			idx += size
		}
		if !found {
			return false
		}
	}
	return true
}

// TestCenterRuneArrayDeadCodeRemoved verifies that center() no longer
// contains the dead rune-array allocation path that was replaced by direct
// string concatenation.
func TestCenterRuneArrayDeadCodeRemoved(t *testing.T) {
	// This test is a behavioral check: center must still produce correct
	// results after the dead-code removal. The dead code was a phantom
	// allocation of make([]rune, width) that was discarded in favor of
	// strings.Repeat + concatenation.
	result := center("研究", 10)
	if runeWidth(result) != 10 {
		t.Errorf("center('研究', 10) width = %d; want 10", runeWidth(result))
	}
	// Ensure the CJK chars are present.
	if !containsRunes(result, "研究") {
		t.Errorf("center('研究', 10) = %q; missing input content", result)
	}
}

// TestTruncateEllipsisWidth verifies that the ellipsis character itself
// has the expected width of 1 cell.
func TestTruncateEllipsisWidth(t *testing.T) {
	w := int(tui.RuneWidth('…'))
	if w != 1 {
		t.Fatalf("ellipsis rune width = %d; expected 1 (truncate logic depends on this)", w)
	}
}
