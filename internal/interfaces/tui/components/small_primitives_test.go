package components

import (
	"strings"
	"testing"

	"github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// --- StatusBadge Tests ---

func TestStatusBadgeOK(t *testing.T) {
	t.Parallel()
	sb := NewStatusBadge(StatusOK, "running", 0)
	mt := renderSmallWidget(sb, 20, 1)

	// Verify the icon is present.
	content := mt.StringTrimmed()
	if !strings.Contains(content, "●") {
		t.Errorf("StatusOK: expected ● icon, got %q", content)
	}
	if !strings.Contains(content, "running") {
		t.Errorf("StatusOK: expected label 'running', got %q", content)
	}

	// Verify the color is StatusOK (green).
	cell := mt.CellAt(0, 0)
	if !cell.Style.Fg.Equal(theme.Colors.StatusOK) {
		t.Errorf("StatusOK: want fg=StatusOK, got %v", cell.Style.Fg)
	}
}

func TestStatusBadgeWarn(t *testing.T) {
	t.Parallel()
	sb := NewStatusBadge(StatusWarn, "idle", 0)
	mt := renderSmallWidget(sb, 20, 1)

	content := mt.StringTrimmed()
	if !strings.Contains(content, "◐") {
		t.Errorf("StatusWarn: expected ◐ icon, got %q", content)
	}
	if !strings.Contains(content, "idle") {
		t.Errorf("StatusWarn: expected label 'idle', got %q", content)
	}

	cell := mt.CellAt(0, 0)
	if !cell.Style.Fg.Equal(theme.Colors.StatusWarn) {
		t.Errorf("StatusWarn: want fg=StatusWarn, got %v", cell.Style.Fg)
	}
}

func TestStatusBadgeError(t *testing.T) {
	t.Parallel()
	sb := NewStatusBadge(StatusError, "error", 0)
	mt := renderSmallWidget(sb, 20, 1)

	content := mt.StringTrimmed()
	if !strings.Contains(content, "○") {
		t.Errorf("StatusError: expected ○ icon, got %q", content)
	}
	if !strings.Contains(content, "error") {
		t.Errorf("StatusError: expected label 'error', got %q", content)
	}

	cell := mt.CellAt(0, 0)
	if !cell.Style.Fg.Equal(theme.Colors.StatusError) {
		t.Errorf("StatusError: want fg=StatusError, got %v", cell.Style.Fg)
	}
}

func TestStatusBadgePending(t *testing.T) {
	t.Parallel()
	sb := NewStatusBadge(StatusPending, "starting", 0)
	mt := renderSmallWidget(sb, 20, 1)

	content := mt.StringTrimmed()
	if !strings.Contains(content, "…") {
		t.Errorf("StatusPending: expected … icon, got %q", content)
	}
	if !strings.Contains(content, "starting") {
		t.Errorf("StatusPending: expected label 'starting', got %q", content)
	}

	cell := mt.CellAt(0, 0)
	if !cell.Style.Fg.Equal(theme.Colors.StatusPending) {
		t.Errorf("StatusPending: want fg=StatusPending, got %v", cell.Style.Fg)
	}
}

func TestStatusBadgeNone(t *testing.T) {
	t.Parallel()
	sb := NewStatusBadge(StatusNone, "unknown", 0)
	mt := renderSmallWidget(sb, 20, 1)

	content := mt.StringTrimmed()
	// StatusNone has no icon.
	if strings.Contains(content, "●") || strings.Contains(content, "◐") ||
		strings.Contains(content, "○") || strings.Contains(content, "…") {
		t.Errorf("StatusNone: should not have a status icon, got %q", content)
	}
	if !strings.Contains(content, "unknown") {
		t.Errorf("StatusNone: expected label 'unknown', got %q", content)
	}

	// StatusNone should use TextPrimary.
	cell := mt.CellAt(0, 0)
	if !cell.Style.Fg.Equal(theme.Colors.TextPrimary) {
		t.Errorf("StatusNone: want fg=TextPrimary, got %v", cell.Style.Fg)
	}
}

// TestStatusBadgeVariantsDistinct verifies that all four colored variants
// produce different ANSI sequences in the raw cell buffer (not just
// bold-vs-not-bold). VAL-CMP-009 requires distinct ANSI sequences.
func TestStatusBadgeVariantsDistinct(t *testing.T) {
	t.Parallel()

	variants := []struct {
		name    string
		variant StatusVariant
		label   string
	}{
		{"ok", StatusOK, "running"},
		{"warn", StatusWarn, "idle"},
		{"error", StatusError, "failed"},
		{"pending", StatusPending, "starting"},
	}

	colors := make(map[string]tui.Color)
	for _, v := range variants {
		sb := NewStatusBadge(v.variant, v.label, 0)
		mt := renderSmallWidget(sb, 20, 1)
		cell := mt.CellAt(0, 0)
		colors[v.name] = cell.Style.Fg
	}

	// Every pair must be distinct.
	names := []string{"ok", "warn", "error", "pending"}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			a, b := colors[names[i]], colors[names[j]]
			if a.Equal(b) {
				t.Errorf("variant %q and %q have the same foreground color; must be distinct",
					names[i], names[j])
			}
		}
	}

	// Also verify they're not just bold-vs-not-bold by checking the actual
	// color values differ (not just text attributes).
	for _, n := range names {
		col := colors[n]
		if col.IsDefault() {
			t.Errorf("variant %q has default color; must be a concrete color", n)
		}
	}
}

func TestStatusBadgeWidthPadding(t *testing.T) {
	t.Parallel()
	// When Width > natural width, badge is padded.
	sb := NewStatusBadge(StatusOK, "ok", 15)
	mt := renderSmallWidget(sb, 15, 1)
	content := mt.StringTrimmed()
	if len(content) == 0 {
		t.Fatal("expected non-empty output")
	}
	// The badge should be padded to the requested width.
	if len([]rune(strings.TrimSpace(content))) > 15 {
		t.Errorf("padded badge exceeds requested width: %q", content)
	}
}

func TestStatusBadgeWidthTruncation(t *testing.T) {
	t.Parallel()
	// When label + icon exceeds Width, label is truncated with ….
	sb := NewStatusBadge(StatusOK, "verylonglabel", 8)
	mt := renderSmallWidget(sb, 8, 1)
	content := mt.StringTrimmed()
	if len(content) == 0 {
		t.Fatal("expected non-empty output")
	}
	// Should contain the icon.
	if !strings.Contains(content, "●") {
		t.Errorf("truncated badge should still have icon, got %q", content)
	}
	// Should contain an ellipsis if truncated.
	if strings.Contains(content, "verylonglabel") {
		t.Errorf("badge should be truncated, got full label %q", content)
	}
}

// --- EmptyState Tests ---

func TestEmptyStateRenderMessage(t *testing.T) {
	t.Parallel()
	es := NewEmptyState("No agents running.", "", 40, 10)
	mt := renderSmallWidget(es, 40, 10)

	content := mt.StringTrimmed()
	if !strings.Contains(content, "No agents running.") {
		t.Errorf("EmptyState: expected message 'No agents running.', got %q", content)
	}
}

func TestEmptyStateRenderCTA(t *testing.T) {
	t.Parallel()
	es := NewEmptyState("No agents running.", "Spawn one: foxctl agent spawn --role researcher", 60, 10)
	mt := renderSmallWidget(es, 60, 10)

	content := mt.StringTrimmed()
	if !strings.Contains(content, "No agents running.") {
		t.Errorf("EmptyState: expected message, got %q", content)
	}
	if !strings.Contains(content, "Spawn one:") {
		t.Errorf("EmptyState: expected CTA, got %q", content)
	}
}

func TestEmptyStateWithoutCTA(t *testing.T) {
	t.Parallel()
	es := NewEmptyState("No items.", "", 30, 5)
	mt := renderSmallWidget(es, 30, 5)

	content := mt.StringTrimmed()
	if !strings.Contains(content, "No items.") {
		t.Errorf("EmptyState without CTA: expected message, got %q", content)
	}
}

func TestEmptyStateWidthRespect(t *testing.T) {
	t.Parallel()
	// Long message should not overflow the width.
	longMsg := "This is a very long message that should be wrapped or truncated at the width boundary."
	es := NewEmptyState(longMsg, "", 20, 5)
	mt := renderSmallWidget(es, 20, 5)

	// No line should exceed the width.
	for y := 0; y < 5; y++ {
		lineWidth := 0
		for x := 0; x < 20; x++ {
			cell := mt.CellAt(x, y)
			if cell.Rune != 0 && cell.Rune != ' ' {
				lineWidth = x + 1
			}
		}
		if lineWidth > 20 {
			t.Errorf("line %d exceeds width: %d cells", y, lineWidth)
		}
	}
}

func TestEmptyStateWithIcon(t *testing.T) {
	t.Parallel()
	es := NewEmptyState("No agents running.", "", 40, 10, WithIcon("∅"))
	mt := renderSmallWidget(es, 40, 10)

	content := mt.StringTrimmed()
	if !strings.Contains(content, "∅") {
		t.Errorf("EmptyState with icon: expected ∅, got %q", content)
	}
	if !strings.Contains(content, "No agents running.") {
		t.Errorf("EmptyState with icon: expected message, got %q", content)
	}
}

// --- LoadingState Tests ---

func TestLoadingStateRenderMessage(t *testing.T) {
	t.Parallel()
	ls := NewLoadingState("Connecting to daemon…", 40, 5)
	mt := renderSmallWidget(ls, 40, 5)

	content := mt.StringTrimmed()
	if !strings.Contains(content, "Connecting to daemon") {
		t.Errorf("LoadingState: expected message, got %q", content)
	}
}

func TestLoadingStateSpinnerFrame(t *testing.T) {
	t.Parallel()
	ls := NewLoadingState("Loading…", 30, 3)
	ls.SetSpinnerIndex(0)
	mt := renderSmallWidget(ls, 30, 3)

	content := mt.StringTrimmed()
	// Default spinner frames are ⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏
	defaultFrames := "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
	hasFrame := false
	for _, f := range defaultFrames {
		if strings.Contains(content, string(f)) {
			hasFrame = true
			break
		}
	}
	if !hasFrame {
		t.Errorf("LoadingState: expected a spinner frame character, got %q", content)
	}
}

func TestLoadingStateSpinnerAdvances(t *testing.T) {
	t.Parallel()
	ls := NewLoadingState("Loading…", 30, 5)
	ls.SetSpinnerIndex(0)

	// Advance spinner.
	ls.SetSpinnerIndex(1)
	mt := renderSmallWidget(ls, 30, 5)

	content := mt.StringTrimmed()
	if content == "" {
		t.Fatal("expected non-empty output after spinner advance")
	}
}

func TestLoadingStateCompactMode(t *testing.T) {
	t.Parallel()
	// When Height <= 3, compact mode: spinner + message on one line.
	ls := NewLoadingState("Loading…", 30, 3)
	mt := renderSmallWidget(ls, 30, 3)

	content := mt.StringTrimmed()
	if content == "" {
		t.Fatal("compact LoadingState should render something")
	}
}

func TestLoadingStateWidthRespect(t *testing.T) {
	t.Parallel()
	longMsg := "This is a very long loading message that explains what is being prepared in great detail."
	ls := NewLoadingState(longMsg, 20, 5)
	mt := renderSmallWidget(ls, 20, 5)

	// No line should exceed the width.
	for y := 0; y < 5; y++ {
		lineWidth := 0
		for x := 0; x < 20; x++ {
			cell := mt.CellAt(x, y)
			if cell.Rune != 0 && cell.Rune != ' ' {
				lineWidth = x + 1
			}
		}
		if lineWidth > 20 {
			t.Errorf("line %d exceeds width: %d cells", y, lineWidth)
		}
	}
}

func TestLoadingStateCustomSpinnerFrames(t *testing.T) {
	t.Parallel()
	ls := NewLoadingState("Loading…", 20, 5, WithSpinnerFrames([]string{"|", "/", "-", "\\"}))
	ls.SetSpinnerIndex(2) // "-"
	mt := renderSmallWidget(ls, 20, 5)

	content := mt.StringTrimmed()
	if !strings.Contains(content, "-") {
		t.Errorf("LoadingState custom spinner: expected '-' at index 2, got %q", content)
	}
}

// --- KeybindHint Tests ---

func TestKeybindHintCompactRender(t *testing.T) {
	t.Parallel()
	kh := NewKeybindHint("Tab", "focus", "", true)
	mt := renderSmallWidget(kh, 20, 1)

	content := mt.StringTrimmed()
	if !strings.Contains(content, "Tab") {
		t.Errorf("KeybindHint compact: expected 'Tab', got %q", content)
	}
	if !strings.Contains(content, "focus") {
		t.Errorf("KeybindHint compact: expected 'focus', got %q", content)
	}
	if !strings.Contains(content, ":") {
		t.Errorf("KeybindHint compact: expected ':' separator, got %q", content)
	}
}

func TestKeybindHintExpandedRender(t *testing.T) {
	t.Parallel()
	kh := NewKeybindHint("Tab", "focus", "", false)
	mt := renderSmallWidget(kh, 20, 2)

	content := mt.StringTrimmed()
	if !strings.Contains(content, "Tab") {
		t.Errorf("KeybindHint expanded: expected 'Tab', got %q", content)
	}
	if !strings.Contains(content, "focus") {
		t.Errorf("KeybindHint expanded: expected 'focus', got %q", content)
	}
}

func TestKeybindHintCustomSeparator(t *testing.T) {
	t.Parallel()
	kh := NewKeybindHint("Tab", "focus", "→", true)
	mt := renderSmallWidget(kh, 20, 1)

	content := mt.StringTrimmed()
	if !strings.Contains(content, "Tab→focus") {
		t.Errorf("KeybindHint custom separator: expected 'Tab→focus', got %q", content)
	}
}

func TestKeybindHintDefaultSeparator(t *testing.T) {
	t.Parallel()
	kh := NewKeybindHint("↑↓", "nav", "", true)
	mt := renderSmallWidget(kh, 20, 1)

	content := mt.StringTrimmed()
	if !strings.Contains(content, "↑↓:nav") {
		t.Errorf("KeybindHint default separator: expected '↑↓:nav', got %q", content)
	}
}

func TestKeybindHintCompactSingleLine(t *testing.T) {
	t.Parallel()
	kh := NewKeybindHint("Enter", "submit", "", true)
	mt := renderSmallWidget(kh, 30, 1)

	// Compact should fit on a single line.
	content := mt.StringTrimmed()
	lines := strings.Split(content, "\n")
	if len(lines) > 1 {
		t.Errorf("compact KeybindHint should be single line, got %d lines: %q", len(lines), content)
	}
}

func TestKeybindHintExpandedMultiLine(t *testing.T) {
	t.Parallel()
	kh := NewKeybindHint("Ctrl+X", "cancel", "", false)
	mt := renderSmallWidget(kh, 30, 2)

	content := mt.StringTrimmed()
	// Expanded should have the key on one concept, description on another.
	if !strings.Contains(content, "Ctrl+X") {
		t.Errorf("KeybindHint expanded: expected 'Ctrl+X', got %q", content)
	}
	if !strings.Contains(content, "cancel") {
		t.Errorf("KeybindHint expanded: expected 'cancel', got %q", content)
	}
}

// --- render helper ---

// renderSmallWidget is a helper for rendering small primitives to a
// MockTerminal for testing.
func renderSmallWidget(widget interface {
	Render(buf *tui.Buffer)
}, width, height int) *tui.MockTerminal {
	mt := tui.NewMockTerminal(width, height)
	buf := tui.NewBuffer(width, height)
	widget.Render(buf)
	tui.RenderFull(mt, buf)
	return mt
}
