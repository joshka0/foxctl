package components

import (
	"strings"
	"testing"

	"github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// ---------------------------------------------------------------------------
// VAL-SKEL-012: Status footer — connection + active entity + keybindings
//
// Tests verify:
//   (i)   Footer renders connection status indicator (ok / degraded / error)
//   (ii)  Footer renders active entity label when present
//   (iii) Footer renders compact keybinding hint strip with ≥3 bindings
//   (iv)  Footer fits within a single row
//   (v)   Each status variant uses distinct ANSI color
// ---------------------------------------------------------------------------

func TestStatusFooter_OKStatus(t *testing.T) {
	t.Parallel()
	sf := NewStatusFooter(StatusFooterConfig{
		ConnectionStatus: StatusOK,
		ConnectionLabel:  "connected",
		ActiveEntity:     "agent: abc12345 (researcher)",
		Keybinds: []KeybindHintConfig{
			{Key: "Tab", Desc: "focus"},
			{Key: "↑↓", Desc: "nav"},
			{Key: "Enter", Desc: "submit"},
			{Key: "e", Desc: "evidence"},
			{Key: "Ctrl+X", Desc: "cancel"},
		},
		Width: 80,
	})
	mt := renderStatusFooter(sf, 80, 1)

	content := mt.StringTrimmed()
	if !strings.Contains(content, "connected") {
		t.Errorf("expected 'connected' label, got: %q", content)
	}
	if !strings.Contains(content, "agent: abc12345 (researcher)") {
		t.Errorf("expected active entity, got: %q", content)
	}
	// Verify at least 3 keybindings are present.
	binds := []string{"Tab", "↑↓", "Enter", "e", "Ctrl+X"}
	found := 0
	for _, b := range binds {
		if strings.Contains(content, b) {
			found++
		}
	}
	if found < 3 {
		t.Errorf("expected ≥3 keybindings in footer, found %d: %q", found, content)
	}

	// Verify the status badge uses StatusOK color.
	cell := mt.CellAt(0, 0)
	if !cell.Style.Fg.Equal(theme.Colors.StatusOK) {
		t.Errorf("StatusOK footer: want fg=StatusOK, got %v", cell.Style.Fg)
	}
}

func TestStatusFooter_WarnStatus(t *testing.T) {
	t.Parallel()
	sf := NewStatusFooter(StatusFooterConfig{
		ConnectionStatus: StatusWarn,
		ConnectionLabel:  "degraded",
		ActiveEntity:     "agent: abc12345 (researcher)",
		Keybinds: []KeybindHintConfig{
			{Key: "Tab", Desc: "focus"},
			{Key: "↑↓", Desc: "nav"},
			{Key: "Enter", Desc: "submit"},
		},
		Width: 80,
	})
	mt := renderStatusFooter(sf, 80, 1)

	content := mt.StringTrimmed()
	if !strings.Contains(content, "degraded") {
		t.Errorf("expected 'degraded' label, got: %q", content)
	}

	cell := mt.CellAt(0, 0)
	if !cell.Style.Fg.Equal(theme.Colors.StatusWarn) {
		t.Errorf("StatusWarn footer: want fg=StatusWarn, got %v", cell.Style.Fg)
	}
}

func TestStatusFooter_ErrorStatus(t *testing.T) {
	t.Parallel()
	sf := NewStatusFooter(StatusFooterConfig{
		ConnectionStatus: StatusError,
		ConnectionLabel:  "error",
		ActiveEntity:     "",
		Keybinds: []KeybindHintConfig{
			{Key: "ESC", Desc: "quit"},
			{Key: "r", Desc: "retry"},
			{Key: "↑↓", Desc: "nav"},
		},
		Width: 80,
	})
	mt := renderStatusFooter(sf, 80, 1)

	content := mt.StringTrimmed()
	if !strings.Contains(content, "error") {
		t.Errorf("expected 'error' label, got: %q", content)
	}

	cell := mt.CellAt(0, 0)
	if !cell.Style.Fg.Equal(theme.Colors.StatusError) {
		t.Errorf("StatusError footer: want fg=StatusError, got %v", cell.Style.Fg)
	}
}

func TestStatusFooter_PendingStatus(t *testing.T) {
	t.Parallel()
	sf := NewStatusFooter(StatusFooterConfig{
		ConnectionStatus: StatusPending,
		ConnectionLabel:  "connecting…",
		ActiveEntity:     "",
		Keybinds: []KeybindHintConfig{
			{Key: "ESC", Desc: "quit"},
			{Key: "↑↓", Desc: "nav"},
			{Key: "Enter", Desc: "submit"},
		},
		Width: 80,
	})
	mt := renderStatusFooter(sf, 80, 1)

	content := mt.StringTrimmed()
	if !strings.Contains(content, "connecting") {
		t.Errorf("expected 'connecting' label, got: %q", content)
	}

	cell := mt.CellAt(0, 0)
	if !cell.Style.Fg.Equal(theme.Colors.StatusPending) {
		t.Errorf("StatusPending footer: want fg=StatusPending, got %v", cell.Style.Fg)
	}
}

func TestStatusFooter_NoActiveEntity(t *testing.T) {
	t.Parallel()
	sf := NewStatusFooter(StatusFooterConfig{
		ConnectionStatus: StatusOK,
		ConnectionLabel:  "connected",
		ActiveEntity:     "",
		Keybinds: []KeybindHintConfig{
			{Key: "ESC", Desc: "quit"},
			{Key: "↑↓", Desc: "nav"},
			{Key: "Enter", Desc: "submit"},
		},
		Width: 80,
	})
	mt := renderStatusFooter(sf, 80, 1)

	content := mt.StringTrimmed()
	// Should not contain an empty entity label or stray separators.
	if strings.Contains(content, "agent:") {
		t.Errorf("expected no active entity when empty, got: %q", content)
	}
}

func TestStatusFooter_SingleRow(t *testing.T) {
	t.Parallel()
	sf := NewStatusFooter(StatusFooterConfig{
		ConnectionStatus: StatusOK,
		ConnectionLabel:  "connected",
		ActiveEntity:     "agent: abc12345 (researcher)",
		Keybinds: []KeybindHintConfig{
			{Key: "Tab", Desc: "focus"},
			{Key: "↑↓", Desc: "nav"},
			{Key: "Enter", Desc: "submit"},
			{Key: "e", Desc: "evidence"},
			{Key: "Ctrl+X", Desc: "cancel"},
		},
		Width: 80,
	})
	mt := renderStatusFooter(sf, 80, 1)

	// Verify no newlines — everything fits on one row.
	content := mt.StringTrimmed()
	if strings.Contains(content, "\n") {
		t.Errorf("footer should be single row, got newlines: %q", content)
	}
}

func TestStatusFooter_TruncatesWhenTooNarrow(t *testing.T) {
	t.Parallel()
	sf := NewStatusFooter(StatusFooterConfig{
		ConnectionStatus: StatusOK,
		ConnectionLabel:  "connected",
		ActiveEntity:     "agent: abc12345 (researcher)",
		Keybinds: []KeybindHintConfig{
			{Key: "Tab", Desc: "focus"},
			{Key: "↑↓", Desc: "nav"},
			{Key: "Enter", Desc: "submit"},
		},
		Width: 30,
	})
	mt := renderStatusFooter(sf, 30, 1)

	content := mt.StringTrimmed()
	// Should still render without panic and fit within width (display cells).
	if runeWidth(content) > 30 {
		t.Errorf("footer content %d display cells exceeds width 30: %q", runeWidth(content), content)
	}
	// Status indicator should still be visible.
	if !strings.Contains(content, "connected") {
		t.Errorf("expected 'connected' even when narrow, got: %q", content)
	}
}

func TestStatusFooter_VariantsDistinctColors(t *testing.T) {
	t.Parallel()
	variants := []StatusVariant{StatusOK, StatusWarn, StatusError, StatusPending}
	seen := map[tui.Color]bool{}
	for _, sv := range variants {
		sf := NewStatusFooter(StatusFooterConfig{
			ConnectionStatus: sv,
			ConnectionLabel:  "test",
			Keybinds:         []KeybindHintConfig{{Key: "A", Desc: "b"}},
			Width:            20,
		})
		mt := renderStatusFooter(sf, 20, 1)
		cell := mt.CellAt(0, 0)
		if seen[cell.Style.Fg] {
			t.Errorf("variant %v has duplicate color %v with another variant", sv, cell.Style.Fg)
		}
		seen[cell.Style.Fg] = true
	}
}

func TestStatusFooter_ThreeBindingsMinimum(t *testing.T) {
	t.Parallel()
	sf := NewStatusFooter(StatusFooterConfig{
		ConnectionStatus: StatusOK,
		ConnectionLabel:  "connected",
		ActiveEntity:     "agent: abc12345 (researcher)",
		Keybinds: []KeybindHintConfig{
			{Key: "Tab", Desc: "focus"},
			{Key: "↑↓", Desc: "nav"},
		},
		Width: 80,
	})
	mt := renderStatusFooter(sf, 80, 1)

	content := mt.StringTrimmed()
	// With only 2 bindings, we should still see both.
	if !strings.Contains(content, "Tab") {
		t.Errorf("expected 'Tab' binding, got: %q", content)
	}
	if !strings.Contains(content, "↑↓") {
		t.Errorf("expected '↑↓' binding, got: %q", content)
	}
}

// renderStatusFooter renders a StatusFooter into a MockTerminal.
func renderStatusFooter(sf *StatusFooter, width, height int) *tui.MockTerminal {
	mt := tui.NewMockTerminal(width, height)
	buf := tui.NewBuffer(width, height)
	sf.Render(buf)
	tui.RenderFull(mt, buf)
	return mt
}
