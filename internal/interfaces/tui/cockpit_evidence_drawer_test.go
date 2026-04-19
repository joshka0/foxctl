package tui

import (
	"strings"
	"testing"
	"time"

	gotui "github.com/grindlemire/go-tui"
)

// ---------------------------------------------------------------------------
// VAL-SKEL-008: Evidence drawer — selecting a transcript row and pressing 'e'
// opens the drawer showing the raw payload. Must exercise all three row types:
//   (a) text reply ("assistant: ...")
//   (b) tool call with args + result ("tool: ...")
//   (c) error row with error details ("⚠ ...")
// Drawer opens with ≤200ms latency; closes on ESC.
// ---------------------------------------------------------------------------

// --- Test: Drawer opens on 'e' key when agent selected ---

func TestEvidenceDrawer_OpensOnEKey(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running", Workspace: "ws1", ParentID: "—", LastActive: "2m"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	// Simulate pressing 'e' via the KeyMap binding.
	km := cs.KeyMap()
	for _, kb := range km {
		if kb.Pattern.Rune == 'e' {
			kb.Handler(gotui.KeyEvent{Key: gotui.KeyRune, Rune: 'e'})
			break
		}
	}

	if !cs.EvidenceDrawerOpen() {
		t.Fatal("expected drawer to be open after pressing 'e'")
	}
	if cs.EvidenceDrawerTitle() == "" {
		t.Error("expected drawer title to be non-empty")
	}
}

// --- Test: Drawer shows correct content for text reply row ---

func TestEvidenceDrawer_TextReplyContent(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	// Seed a text reply stream line.
	cs.streamLines = []string{"assistant: Hello, this is a text reply."}
	cs.selectedStreamLine = 0

	cs.openEvidenceDrawer()

	content := cs.EvidenceDrawerContent()
	if len(content) == 0 {
		t.Fatal("expected drawer content to be non-empty")
	}
	joined := strings.Join(content, "\n")
	if !strings.Contains(joined, "text reply") {
		t.Errorf("expected content to contain 'text reply', got:\n%s", joined)
	}
	if !strings.Contains(joined, "Hello, this is a text reply.") {
		t.Errorf("expected content to contain the reply text, got:\n%s", joined)
	}
}

// --- Test: Drawer shows correct content for tool call row ---

func TestEvidenceDrawer_ToolCallContent(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	// Seed a tool call stream line.
	cs.streamLines = []string{"tool: search_web"}
	cs.selectedStreamLine = 0

	cs.openEvidenceDrawer()

	content := cs.EvidenceDrawerContent()
	if len(content) == 0 {
		t.Fatal("expected drawer content to be non-empty")
	}
	joined := strings.Join(content, "\n")
	if !strings.Contains(joined, "tool call") {
		t.Errorf("expected content to contain 'tool call', got:\n%s", joined)
	}
	if !strings.Contains(joined, "search_web") {
		t.Errorf("expected content to contain tool name 'search_web', got:\n%s", joined)
	}
}

// --- Test: Drawer shows correct content for error row ---

func TestEvidenceDrawer_ErrorRowContent(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	// Seed an error stream line.
	cs.streamLines = []string{"⚠ Runtime error: connection timeout"}
	cs.selectedStreamLine = 0

	cs.openEvidenceDrawer()

	content := cs.EvidenceDrawerContent()
	if len(content) == 0 {
		t.Fatal("expected drawer content to be non-empty")
	}
	joined := strings.Join(content, "\n")
	if !strings.Contains(joined, "error") {
		t.Errorf("expected content to contain 'error', got:\n%s", joined)
	}
	if !strings.Contains(joined, "Runtime error: connection timeout") {
		t.Errorf("expected content to contain error text, got:\n%s", joined)
	}
	if !strings.Contains(joined, "ERUNTIME") {
		t.Errorf("expected content to contain error code 'ERUNTIME', got:\n%s", joined)
	}
}

// --- Test: Drawer closes on ESC key ---

func TestEvidenceDrawer_ClosesOnESC(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	cs.openEvidenceDrawer()
	if !cs.EvidenceDrawerOpen() {
		t.Fatal("expected drawer to be open before ESC")
	}

	// Simulate pressing ESC via the KeyMap binding.
	km := cs.KeyMap()
	for _, kb := range km {
		if kb.Pattern.Key == gotui.KeyEscape {
			kb.Handler(gotui.KeyEvent{Key: gotui.KeyEscape})
			break
		}
	}

	if cs.EvidenceDrawerOpen() {
		t.Error("expected drawer to be closed after ESC")
	}
}

// --- Test: Drawer open latency ≤200ms ---

func TestEvidenceDrawer_OpenLatency(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	cs.streamLines = []string{"assistant: latency test"}
	cs.selectedStreamLine = 0

	start := time.Now()
	cs.openEvidenceDrawer()
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Errorf("drawer open latency %v exceeds 200ms", elapsed)
	}
	if !cs.EvidenceDrawerOpen() {
		t.Error("expected drawer to be open after latency test")
	}
}

// --- Test: Drawer renders as overlay in ready state ---

func TestEvidenceDrawer_RendersOverlay(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	cs.streamLines = []string{"assistant: overlay test"}
	cs.selectedStreamLine = 0
	cs.openEvidenceDrawer()

	_, mt := renderCockpitToMT(cs, 80, 24)

	// The drawer should render some content in the right portion of the screen.
	// Look for the drawer title "Evidence" in the rendered output.
	found := false
	for y := 0; y < 24; y++ {
		row := rowTextFromMT(mt, y, 80)
		if strings.Contains(row, "Evidence") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected rendered output to contain drawer title 'Evidence'")
	}
}

// --- Test: Drawer shows agent details when no stream lines ---

func TestEvidenceDrawer_AgentDetailsWhenNoStream(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running", Workspace: "ws1", ParentID: "parent-1", LastActive: "2m"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	// No stream lines — drawer should show agent raw details.
	cs.openEvidenceDrawer()

	content := cs.EvidenceDrawerContent()
	if len(content) == 0 {
		t.Fatal("expected drawer content to be non-empty")
	}
	joined := strings.Join(content, "\n")
	if !strings.Contains(joined, "agent-abc") {
		t.Errorf("expected content to contain agent ID, got:\n%s", joined)
	}
	if !strings.Contains(joined, "researcher") {
		t.Errorf("expected content to contain role, got:\n%s", joined)
	}
}

// --- Test: Drawer content for user message row ---

func TestEvidenceDrawer_UserMessageContent(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	cs.streamLines = []string{"you: hello agent"}
	cs.selectedStreamLine = 0

	cs.openEvidenceDrawer()

	content := cs.EvidenceDrawerContent()
	joined := strings.Join(content, "\n")
	if !strings.Contains(joined, "user message") {
		t.Errorf("expected content to contain 'user message', got:\n%s", joined)
	}
	if !strings.Contains(joined, "hello agent") {
		t.Errorf("expected content to contain message text, got:\n%s", joined)
	}
}

// --- Test: Drawer content for tool result row ---

func TestEvidenceDrawer_ToolResultContent(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	cs.streamLines = []string{"result: {\"count\": 42}"}
	cs.selectedStreamLine = 0

	cs.openEvidenceDrawer()

	content := cs.EvidenceDrawerContent()
	joined := strings.Join(content, "\n")
	if !strings.Contains(joined, "tool result") {
		t.Errorf("expected content to contain 'tool result', got:\n%s", joined)
	}
	if !strings.Contains(joined, "{\"count\": 42}") {
		t.Errorf("expected content to contain result JSON, got:\n%s", joined)
	}
}

// --- Test: Drawer does not open when no agent selected ---

func TestEvidenceDrawer_NoOpenWhenNoSelection(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.ClearSelection()
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	cs.openEvidenceDrawer()

	if cs.EvidenceDrawerOpen() {
		t.Error("expected drawer to remain closed when no agent is selected")
	}
}

// --- Test: Drawer scroll offset resets on open ---

func TestEvidenceDrawer_ScrollOffsetResetsOnOpen(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	// Open drawer.
	cs.streamLines = []string{"assistant: first open"}
	cs.selectedStreamLine = 0
	cs.openEvidenceDrawer()

	// Manually set scroll offset (simulating user scrolled down).
	cs.mu.Lock()
	cs.drawerScrollOffset = 5
	cs.mu.Unlock()

	// Close and reopen — scroll should reset to 0.
	cs.CloseEvidenceDrawer()
	cs.streamLines = []string{"assistant: second open"}
	cs.openEvidenceDrawer()

	cs.mu.Lock()
	offset := cs.drawerScrollOffset
	cs.mu.Unlock()

	if offset != 0 {
		t.Errorf("expected scroll offset to reset to 0 on reopen, got %d", offset)
	}
}

// --- Test: KeyMap contains 'e' binding ---

func TestCockpitScreen_KeyMapContainsE(t *testing.T) {
	cs := NewCockpitScreen("")
	km := cs.KeyMap()
	if len(km) == 0 {
		t.Fatal("KeyMap should not be empty")
	}

	found := false
	for _, kb := range km {
		if kb.Pattern.Rune == 'e' {
			found = true
			break
		}
	}
	if !found {
		t.Error("KeyMap should contain 'e' binding for evidence drawer")
	}
}

// --- Test: ESC closes drawer before quitting app ---

func TestEvidenceDrawer_ESClosesDrawerBeforeQuit(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	cs.openEvidenceDrawer()
	if !cs.EvidenceDrawerOpen() {
		t.Fatal("expected drawer to be open")
	}

	// Simulate ESC — should close drawer, not quit app.
	quitCalled := false
	app := &gotui.App{}
	// We can't easily mock App.Stop() here, but we can verify the drawer closes.
	km := cs.KeyMap()
	for _, kb := range km {
		if kb.Pattern.Key == gotui.KeyEscape {
			// Create a KeyEvent without app reference.
			kb.Handler(gotui.KeyEvent{Key: gotui.KeyEscape})
			break
		}
	}

	if cs.EvidenceDrawerOpen() {
		t.Error("expected drawer to be closed after ESC")
	}
	_ = quitCalled
	_ = app
}
