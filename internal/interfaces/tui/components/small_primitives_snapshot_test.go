package components

import (
	"testing"

	"github.com/grindlemire/go-tui"
)

// --- StatusBadge snapshot tests ---
// VAL-CMP-009 requires 4 tuistory snapshots for StatusBadge variants.

func TestSnapshotStatusBadgeOK(t *testing.T) {
	sb := NewStatusBadge(StatusOK, "running", 0)
	mt := renderSmallWidget(sb, 20, 1)
	writeSnapshot(t, "statusbadge-ok.txt", mt.StringTrimmed())
}

func TestSnapshotStatusBadgeWarn(t *testing.T) {
	sb := NewStatusBadge(StatusWarn, "idle", 0)
	mt := renderSmallWidget(sb, 20, 1)
	writeSnapshot(t, "statusbadge-warn.txt", mt.StringTrimmed())
}

func TestSnapshotStatusBadgeError(t *testing.T) {
	sb := NewStatusBadge(StatusError, "error", 0)
	mt := renderSmallWidget(sb, 20, 1)
	writeSnapshot(t, "statusbadge-error.txt", mt.StringTrimmed())
}

func TestSnapshotStatusBadgePending(t *testing.T) {
	sb := NewStatusBadge(StatusPending, "starting", 0)
	mt := renderSmallWidget(sb, 20, 1)
	writeSnapshot(t, "statusbadge-pending.txt", mt.StringTrimmed())
}

// --- EmptyState snapshot test ---

func TestSnapshotEmptyStateWithCTA(t *testing.T) {
	es := NewEmptyState(
		"No agents running.",
		"Spawn one: foxctl agent spawn --role researcher",
		60, 5,
	)
	mt := renderSmallWidget(es, 60, 5)
	writeSnapshot(t, "emptystate-with-cta.txt", mt.StringTrimmed())
}

// --- LoadingState snapshot test ---

func TestSnapshotLoadingStateDefault(t *testing.T) {
	ls := NewLoadingState("Connecting to daemon at http://localhost:8090…", 60, 5)
	mt := renderSmallWidget(ls, 60, 5)
	writeSnapshot(t, "loadingstate-default.txt", mt.StringTrimmed())
}

// --- KeybindHint snapshot test ---
// The component-spec.md requires a snapshot showing a compact hint strip
// with 3+ hints: Tab:focus  ↑↓:nav  Enter:submit.
// Since each hint is a separate widget, we render them into separate
// buffers and compose the output manually.

func TestSnapshotKeybindHintCompact(t *testing.T) {
	hints := []*KeybindHint{
		NewKeybindHint("Tab", "focus", "", true),
		NewKeybindHint("↑↓", "nav", "", true),
		NewKeybindHint("Enter", "submit", "", true),
	}

	// Render each hint individually and collect trimmed output.
	var parts []string
	for _, h := range hints {
		mt := renderSmallWidget(h, 20, 1)
		parts = append(parts, mt.StringTrimmed())
	}

	// Compose as a strip with spacing.
	composed := ""
	for i, p := range parts {
		if i > 0 {
			composed += "  "
		}
		composed += p
	}

	// Also render to a single-width buffer for the snapshot.
	buf := tui.NewBuffer(60, 1)
	buf.SetString(0, 0, composed, tui.NewStyle())
	mt := tui.NewMockTerminal(60, 1)
	tui.RenderFull(mt, buf)

	writeSnapshot(t, "keybindhint-compact.txt", composed)
}
