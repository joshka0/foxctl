package entities

import (
	"testing"
)

// ---------------------------------------------------------------------------
// EntryKind constants — compile-time existence checks
// ---------------------------------------------------------------------------

func TestEntryKindConstants_Exist(t *testing.T) {
	// Every one of the 18 current kinds must exist as a named EntryKind constant.
	kinds := map[string]EntryKind{
		"pending":   EntryKindPending,
		"ask":       EntryKindAsk,
		"reply":     EntryKindReply,
		"event":     EntryKindEvent,
		"cmd":       EntryKindCmd,
		"draft":     EntryKindDraft,
		"status":    EntryKindStatus,
		"error":     EntryKindError,
		"tool":      EntryKindTool,
		"counts":    EntryKindCounts,
		"next":      EntryKindNext,
		"brief":     EntryKindBrief,
		"epic":      EntryKindEpic,
		"inflight":  EntryKindInflight,
		"agent":     EntryKindAgent,
		"console":   EntryKindConsole,
		"connected": EntryKindConnected,
		"heartbeat": EntryKindHeartbeat,
	}
	for name, kind := range kinds {
		if kind == EntryKindUnknown {
			t.Errorf("EntryKind constant for %q is EntryKindUnknown — check the constant value", name)
		}
	}
}

func TestEntryKindConstants_Distinct(t *testing.T) {
	// All 18 constants must be distinct (no accidental duplication).
	seen := map[EntryKind]string{}
	all := []struct {
		name string
		kind EntryKind
	}{
		{"EntryKindPending", EntryKindPending},
		{"EntryKindAsk", EntryKindAsk},
		{"EntryKindReply", EntryKindReply},
		{"EntryKindEvent", EntryKindEvent},
		{"EntryKindCmd", EntryKindCmd},
		{"EntryKindDraft", EntryKindDraft},
		{"EntryKindStatus", EntryKindStatus},
		{"EntryKindError", EntryKindError},
		{"EntryKindTool", EntryKindTool},
		{"EntryKindCounts", EntryKindCounts},
		{"EntryKindNext", EntryKindNext},
		{"EntryKindBrief", EntryKindBrief},
		{"EntryKindEpic", EntryKindEpic},
		{"EntryKindInflight", EntryKindInflight},
		{"EntryKindAgent", EntryKindAgent},
		{"EntryKindConsole", EntryKindConsole},
		{"EntryKindConnected", EntryKindConnected},
		{"EntryKindHeartbeat", EntryKindHeartbeat},
	}
	for _, item := range all {
		if prev, ok := seen[item.kind]; ok {
			t.Errorf("%s == %s (both map to %d)", item.name, prev, item.kind)
		}
		seen[item.kind] = item.name
	}
}

// ---------------------------------------------------------------------------
// EntryKind.String() — human-readable representation
// ---------------------------------------------------------------------------

func TestEntryKind_String(t *testing.T) {
	cases := map[EntryKind]string{
		EntryKindPending:   "pending",
		EntryKindAsk:       "ask",
		EntryKindReply:     "reply",
		EntryKindEvent:     "event",
		EntryKindCmd:       "cmd",
		EntryKindDraft:     "draft",
		EntryKindStatus:    "status",
		EntryKindError:     "error",
		EntryKindTool:      "tool",
		EntryKindCounts:    "counts",
		EntryKindNext:      "next",
		EntryKindBrief:     "brief",
		EntryKindEpic:      "epic",
		EntryKindInflight:  "inflight",
		EntryKindAgent:     "agent",
		EntryKindConsole:   "console",
		EntryKindConnected: "connected",
		EntryKindHeartbeat: "heartbeat",
	}
	for kind, want := range cases {
		got := kind.String()
		if got != want {
			t.Errorf("EntryKind(%d).String() = %q, want %q", kind, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// ParseEntryKind — legacy string → typed enum mapper
// ---------------------------------------------------------------------------

func TestParseEntryKind_AllCurrentValues(t *testing.T) {
	// Every current legacy string value must map to the correct EntryKind.
	cases := map[string]EntryKind{
		"pending":   EntryKindPending,
		"ask":       EntryKindAsk,
		"reply":     EntryKindReply,
		"event":     EntryKindEvent,
		"cmd":       EntryKindCmd,
		"draft":     EntryKindDraft,
		"status":    EntryKindStatus,
		"error":     EntryKindError,
		"tool":      EntryKindTool,
		"counts":    EntryKindCounts,
		"next":      EntryKindNext,
		"brief":     EntryKindBrief,
		"epic":      EntryKindEpic,
		"inflight":  EntryKindInflight,
		"agent":     EntryKindAgent,
		"console":   EntryKindConsole,
		"connected": EntryKindConnected,
		"heartbeat": EntryKindHeartbeat,
	}
	for input, want := range cases {
		got := ParseEntryKind(input)
		if got != want {
			t.Errorf("ParseEntryKind(%q) = %d (%s), want %d (%s)",
				input, got, got, want, want)
		}
	}
}

func TestParseEntryKind_CaseInsensitive(t *testing.T) {
	// The mapper should handle case-insensitive input since existing code
	// normalizes kinds via strings.ToLower.
	cases := map[string]EntryKind{
		"Reply":     EntryKindReply,
		"ASK":       EntryKindAsk,
		"Event":     EntryKindEvent,
		"STATUS":    EntryKindStatus,
		"Heartbeat": EntryKindHeartbeat,
	}
	for input, want := range cases {
		got := ParseEntryKind(input)
		if got != want {
			t.Errorf("ParseEntryKind(%q) = %d (%s), want %d (%s)",
				input, got, got, want, want)
		}
	}
}

func TestParseEntryKind_Unknown(t *testing.T) {
	// Unknown strings should return EntryKindUnknown.
	cases := []string{"", "unknown", "foo", "bar", "PLAN", "ToolCall"}
	for _, input := range cases {
		got := ParseEntryKind(input)
		if got != EntryKindUnknown {
			t.Errorf("ParseEntryKind(%q) = %d (%s), want EntryKindUnknown",
				input, got, got)
		}
	}
}

// ---------------------------------------------------------------------------
// Round-trip: ParseEntryKind → String
// ---------------------------------------------------------------------------

func TestParseEntryKind_RoundTrip(t *testing.T) {
	// ParseEntryKind(s).String() should yield the original lower-case string.
	strings := []string{
		"pending", "ask", "reply", "event", "cmd", "draft", "status",
		"error", "tool", "counts", "next", "brief", "epic", "inflight",
		"agent", "console", "connected", "heartbeat",
	}
	for _, s := range strings {
		got := ParseEntryKind(s).String()
		if got != s {
			t.Errorf("ParseEntryKind(%q).String() = %q, want %q", s, got, s)
		}
	}
}

// ---------------------------------------------------------------------------
// AgentNode — Parent and Children fields
// ---------------------------------------------------------------------------

func TestAgentNode_ParentChildrenFields(t *testing.T) {
	parent := &AgentNode{
		Agent: Agent{ID: "parent-1", Role: "overseer"},
	}
	child := &AgentNode{
		Agent:  Agent{ID: "child-1", Role: "coder"},
		Parent: parent,
		Children: []*AgentNode{
			{Agent: Agent{ID: "grandchild-1", Role: "scout"}},
		},
	}
	if child.Parent == nil {
		t.Fatal("child.Parent is nil, expected non-nil")
	}
	if child.Parent.Agent.ID != "parent-1" {
		t.Errorf("child.Parent.Agent.ID = %q, want %q", child.Parent.Agent.ID, "parent-1")
	}
	if len(child.Children) != 1 {
		t.Fatalf("len(child.Children) = %d, want 1", len(child.Children))
	}
	if child.Children[0].Agent.ID != "grandchild-1" {
		t.Errorf("child.Children[0].Agent.ID = %q, want %q", child.Children[0].Agent.ID, "grandchild-1")
	}
}

// ---------------------------------------------------------------------------
// Room, RoomMessage, EventRow — struct existence and basic fields
// ---------------------------------------------------------------------------

func TestRoom_BasicFields(t *testing.T) {
	room := Room{
		ID:          "room-1",
		Title:       "Test Room",
		Description: "A test room",
		Members:     []string{"agent-1", "agent-2"},
	}
	if room.ID != "room-1" {
		t.Errorf("Room.ID = %q, want %q", room.ID, "room-1")
	}
	if len(room.Members) != 2 {
		t.Errorf("len(Room.Members) = %d, want 2", len(room.Members))
	}
}

func TestRoomMessage_BasicFields(t *testing.T) {
	msg := RoomMessage{
		ID:     "msg-1",
		RoomID: "room-1",
		Sender: "agent-1",
		Kind:   EntryKindReply,
		Text:   "hello",
	}
	if msg.Kind != EntryKindReply {
		t.Errorf("RoomMessage.Kind = %d, want EntryKindReply (%d)", msg.Kind, EntryKindReply)
	}
	if msg.Sender != "agent-1" {
		t.Errorf("RoomMessage.Sender = %q, want %q", msg.Sender, "agent-1")
	}
}

func TestEventRow_BasicFields(t *testing.T) {
	row := EventRow{
		ID:        "evt-1",
		Kind:      EntryKindAgent,
		Text:      "agent spawned",
		Timestamp: 1712000000,
	}
	if row.Kind != EntryKindAgent {
		t.Errorf("EventRow.Kind = %d, want EntryKindAgent (%d)", row.Kind, EntryKindAgent)
	}
	if row.Timestamp != 1712000000 {
		t.Errorf("EventRow.Timestamp = %d, want %d", row.Timestamp, 1712000000)
	}
}
