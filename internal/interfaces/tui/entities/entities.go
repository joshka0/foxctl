package entities

// Agent represents a foxctl agent as rendered in the TUI cockpit.
type Agent struct {
	ID            string
	Name          string
	Slug          string
	Role          string
	State         string
	ParentID      string
	Namespace     string
	ExecMode      string
	LLMProvider   string
	LLMModel      string
	WorkspaceRoot string
}

// AgentNode wraps an Agent in a tree structure, encoding the parent-child
// hierarchy used for multi-agent coordination displays. Parent and Children
// are nil/empty for standalone agents.
type AgentNode struct {
	Agent    Agent
	Parent   *AgentNode
	Children []*AgentNode
}

// Room represents a coordination room in the foxctl multi-agent system.
type Room struct {
	ID          string
	Title       string
	Description string
	Members     []string
}

// RoomMessage is a single message within a coordination room.
type RoomMessage struct {
	ID        string
	RoomID    string
	Sender    string
	Kind      EntryKind
	Text      string
	Timestamp int64
}

// EventRow represents a single row in the event/evidence stream displayed
// in the TUI's evidence lane.
type EventRow struct {
	ID        string
	Kind      EntryKind
	Text      string
	Timestamp int64
	Raw       string // optional raw payload for evidence inspection
}
