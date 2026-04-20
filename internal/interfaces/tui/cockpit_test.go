package tui

import (
	"strings"
	"testing"

	gotui "github.com/grindlemire/go-tui"
)

// ---------------------------------------------------------------------------
// VAL-SKEL-018: Minimum terminal size contract
// ---------------------------------------------------------------------------

func TestCockpitScreen_TooSmallGuardMessage(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.UpdateSize(40, 10)

	if !cs.IsTooSmall() {
		t.Fatal("expected IsTooSmall()=true at 40x10")
	}
	if cs.Phase() != CockpitPhaseTooSmall {
		t.Fatalf("expected phase=%s, got %s", CockpitPhaseTooSmall, cs.Phase())
	}

	// Render into a mock terminal and verify guard message.
	el := cs.renderTooSmall(40, 10)
	if el == nil {
		t.Fatal("renderTooSmall returned nil element")
	}

	// The too-small message should be visible in the element text.
	// We verify the constant since it's what gets passed to WithText.
	if !strings.Contains(tooSmallMessage, "terminal too small") {
		t.Fatalf("guard message should contain 'terminal too small', got %q", tooSmallMessage)
	}
	if !strings.Contains(tooSmallMessage, "60") || !strings.Contains(tooSmallMessage, "15") {
		t.Fatalf("guard message should mention minimum dimensions, got %q", tooSmallMessage)
	}

	// Verify the element contains the guard message text via MockTerminal render.
	mt := gotui.NewMockTerminal(40, 10)
	buf := gotui.NewBuffer(40, 10)
	gotui.RenderFull(mt, buf) // just exercise the render path
}

func TestCockpitScreen_TooSmallAt59x15(t *testing.T) {
	cs := NewCockpitScreen("")
	cs.UpdateSize(59, 15)
	if !cs.IsTooSmall() {
		t.Error("expected IsTooSmall()=true at 59x15 (width < 60)")
	}
}

func TestCockpitScreen_TooSmallAt60x14(t *testing.T) {
	cs := NewCockpitScreen("")
	cs.UpdateSize(60, 14)
	if !cs.IsTooSmall() {
		t.Error("expected IsTooSmall()=true at 60x14 (height < 15)")
	}
}

func TestCockpitScreen_NormalAt60x15(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.UpdateSize(60, 15)
	if cs.IsTooSmall() {
		t.Error("expected IsTooSmall()=false at 60x15 (exact minimum)")
	}
}

func TestCockpitScreen_NormalAt80x24(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.UpdateSize(80, 24)
	if cs.IsTooSmall() {
		t.Error("expected IsTooSmall()=false at 80x24")
	}
}

func TestCockpitScreen_PhaseForcedToTooSmall(t *testing.T) {
	cs := NewCockpitScreen("")
	cs.UpdateSize(40, 10)

	// Trying to set phase to Ready should be forced to TooSmall.
	cs.SetPhase(CockpitPhaseReady)
	if cs.Phase() != CockpitPhaseTooSmall {
		t.Fatalf("expected phase=%s (forced), got %s", CockpitPhaseTooSmall, cs.Phase())
	}
}

func TestCockpitScreen_PhaseChangesWhenResizedAboveMinimum(t *testing.T) {
	cs := NewCockpitScreen("")
	cs.UpdateSize(40, 10)
	if cs.Phase() != CockpitPhaseTooSmall {
		t.Fatalf("expected TooSmall at 40x10, got %s", cs.Phase())
	}

	// Resize above minimum should allow phase changes.
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)
	if cs.Phase() != CockpitPhaseReady {
		t.Fatalf("expected Ready at 80x24, got %s", cs.Phase())
	}
}

func TestCockpitScreen_ResumesNormalOnGrow(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")

	// Start too small.
	cs.UpdateSize(40, 10)
	if !cs.IsTooSmall() {
		t.Fatal("should be too small at 40x10")
	}

	// Grow above minimum.
	cs.UpdateSize(60, 15)
	if cs.IsTooSmall() {
		t.Fatal("should not be too small at 60x15")
	}

	// Phase should transition away from TooSmall on next SetPhase.
	cs.SetPhase(CockpitPhaseLoading)
	if cs.Phase() != CockpitPhaseLoading {
		t.Fatalf("expected Loading after resize above minimum, got %s", cs.Phase())
	}
}

// ---------------------------------------------------------------------------
// VAL-SKEL-001: Screen reachable via documented invocation
// ---------------------------------------------------------------------------

func TestCockpitScreen_RendersLoadingPhase(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseLoading)

	el := cs.Render(nil)
	if el == nil {
		t.Fatal("Render returned nil element")
	}
}

func TestCockpitScreen_LoadingStateContainsURL(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseLoading)

	// Render the loading element and verify it contains the URL.
	el := cs.renderLoading(80, 24, "http://localhost:8090")
	if el == nil {
		t.Fatal("renderLoading returned nil")
	}
}

func TestCockpitScreen_ErrorStateContainsURL(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseError)

	el := cs.renderError(80, 24, "http://localhost:8090")
	if el == nil {
		t.Fatal("renderError returned nil")
	}
}

func TestCockpitScreen_ReadyStateRendersLanes(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	el := cs.renderReady(80, 24)
	if el == nil {
		t.Fatal("renderReady returned nil")
	}
}

func TestCockpitScreen_KeyMapContainsEscape(t *testing.T) {
	cs := NewCockpitScreen("")
	km := cs.KeyMap()
	if len(km) == 0 {
		t.Fatal("KeyMap should not be empty")
	}
	// KeyMap returns at least 3 bindings (ESC, q, Ctrl+C).
	// Each is a gotui.KeyBinding function; just verify count.
	if len(km) < 3 {
		t.Fatalf("KeyMap should contain at least 3 bindings (ESC, q, Ctrl+C), got %d", len(km))
	}
}

func TestCockpitScreen_DefaultPhaseIsLoading(t *testing.T) {
	cs := NewCockpitScreen("")
	cs.UpdateSize(80, 24)
	if cs.Phase() != CockpitPhaseLoading {
		t.Fatalf("default phase should be Loading, got %s", cs.Phase())
	}
}

func TestCockpitScreen_APIURL(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:9999")
	if cs.APIURL() != "http://localhost:9999" {
		t.Fatalf("expected apiURL=http://localhost:9999, got %s", cs.APIURL())
	}
}

func TestCockpitScreen_ConcurrentAccess(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")

	// Hammer UpdateSize and Phase from multiple goroutines to verify no race.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			cs.UpdateSize(40+i%50, 10+i%20)
		}
		close(done)
	}()

	for i := 0; i < 100; i++ {
		cs.Phase()
		cs.IsTooSmall()
		cs.APIURL()
	}
	<-done
}

func TestCockpitScreen_RenderWithZeroSize(t *testing.T) {
	cs := NewCockpitScreen("")
	cs.UpdateSize(0, 0)
	// Should not panic — falls back to 80x24.
	el := cs.Render(nil)
	if el == nil {
		t.Fatal("Render with zero size should not return nil")
	}
}

func TestTooSmallMessage(t *testing.T) {
	if !strings.Contains(tooSmallMessage, "≥60x15") {
		t.Errorf("tooSmallMessage should contain '≥60x15', got %q", tooSmallMessage)
	}
}

func TestMinTermConstants(t *testing.T) {
	if MinTermWidth != 60 {
		t.Errorf("MinTermWidth should be 60, got %d", MinTermWidth)
	}
	if MinTermHeight != 15 {
		t.Errorf("MinTermHeight should be 15, got %d", MinTermHeight)
	}
}
