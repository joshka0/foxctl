package cmd

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/agentpane"
	domainagent "github.com/jkatigb/agentctl/internal/domain/agent"
)

func TestListZellijBoundPanesFiltersAndDedupesByLatestPane(t *testing.T) {
	base := time.Date(2026, 4, 4, 15, 0, 0, 0, time.UTC)
	all := []domainagent.Agent{
		{
			ID:        "old-parent",
			Role:      "overseer",
			State:     domainagent.StateError,
			CreatedAt: base,
			TerminalBinding: domainagent.TerminalBinding{
				Backend:       "zellij",
				Session:       "alpha",
				PaneID:        "overseer",
				ParticipantID: "overseer",
				RoomAccess:    "direct",
			},
		},
		{
			ID:        "new-parent",
			Role:      "overseer",
			State:     domainagent.StateRunning,
			CreatedAt: base.Add(time.Minute),
			TerminalBinding: domainagent.TerminalBinding{
				Backend:       "zellij",
				Session:       "alpha",
				PaneID:        "overseer",
				ParticipantID: "overseer",
				RoomAccess:    "direct",
			},
		},
		{
			ID:        "child",
			ParentID:  "new-parent",
			Role:      "coder",
			State:     domainagent.StateStarting,
			CreatedAt: base.Add(2 * time.Minute),
			TerminalBinding: domainagent.TerminalBinding{
				Backend:             "zellij",
				Session:             "alpha",
				PaneID:              "coder-abc123",
				ParticipantID:       "coder-abc123",
				ParentParticipantID: "overseer",
				ParentAgentID:       "new-parent",
				RoomAccess:          "none",
			},
		},
		{
			ID:        "other-session",
			Role:      "reviewer",
			State:     domainagent.StateRunning,
			CreatedAt: base.Add(3 * time.Minute),
			TerminalBinding: domainagent.TerminalBinding{
				Backend:       "zellij",
				Session:       "beta",
				PaneID:        "reviewer-1",
				ParticipantID: "reviewer-1",
				RoomAccess:    "direct",
			},
		},
		{
			ID:        "tmux-agent",
			Role:      "coder",
			State:     domainagent.StateRunning,
			CreatedAt: base.Add(4 * time.Minute),
			TerminalBinding: domainagent.TerminalBinding{
				Backend:       "tmux",
				Session:       "14",
				PaneID:        "%1",
				ParticipantID: "tmux:14:%1",
			},
		},
	}

	panes := listZellijBoundPanes(all, "alpha")
	if len(panes) != 2 {
		t.Fatalf("len(panes) = %d, want 2", len(panes))
	}
	if panes[0].PaneName != "coder-abc123" || panes[0].AgentID != "child" {
		t.Fatalf("first pane = %+v, want child coder pane", panes[0])
	}
	if panes[1].PaneName != "overseer" || panes[1].AgentID != "new-parent" {
		t.Fatalf("second pane = %+v, want newest overseer pane", panes[1])
	}
}

func TestListZellijBoundPanesIncludesSocketOnlyWrappedPanes(t *testing.T) {
	shortTmp, err := os.MkdirTemp("/tmp", "agt-zellij-cmd-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(shortTmp) })
	t.Setenv("TMPDIR", shortTmp)

	session := "socket-only-zellij"
	participantID := "claude-participant-with-a-long-suffix"
	socketPath := agentpane.DefaultSocketPath(session, participantID)
	readyPath := agentpane.DefaultReadyPath(session, participantID)
	metaPath := agentpane.MetadataPathForSocket(socketPath)
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(socket dir): %v", err)
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen(unix): %v", err)
	}
	defer ln.Close()
	if err := os.WriteFile(readyPath, []byte("ready\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(ready): %v", err)
	}
	meta, err := json.Marshal(agentpane.PaneMetadata{
		ParticipantID: participantID,
		RoomID:        "room-delta",
		SocketPath:    socketPath,
		ReadyPath:     readyPath,
	})
	if err != nil {
		t.Fatalf("Marshal(metadata): %v", err)
	}
	if err := os.WriteFile(metaPath, meta, 0o600); err != nil {
		t.Fatalf("WriteFile(metadata): %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(socketPath)
		_ = os.Remove(readyPath)
		_ = os.Remove(metaPath)
	})

	panes := listZellijBoundPanes(nil, session)
	if len(panes) != 1 {
		t.Fatalf("len(panes) = %d, want 1", len(panes))
	}
	if panes[0].ParticipantID != participantID {
		t.Fatalf("ParticipantID = %q, want %q", panes[0].ParticipantID, participantID)
	}
	if panes[0].PaneName != participantID {
		t.Fatalf("PaneName = %q, want %q", panes[0].PaneName, participantID)
	}
	if panes[0].Source != "pane_socket" {
		t.Fatalf("Source = %q, want pane_socket", panes[0].Source)
	}
	if panes[0].State != domainagent.StateRunning {
		t.Fatalf("State = %q, want running", panes[0].State)
	}
	if panes[0].SocketPath != socketPath {
		t.Fatalf("SocketPath = %q, want %q", panes[0].SocketPath, socketPath)
	}
	if panes[0].RoomID != "room-delta" {
		t.Fatalf("RoomID = %q, want room-delta", panes[0].RoomID)
	}
}

func TestListZellijBoundPanesMarksStaleSocketAsStopped(t *testing.T) {
	shortTmp, err := os.MkdirTemp("/tmp", "agt-zellij-cmd-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(shortTmp) })
	t.Setenv("TMPDIR", shortTmp)

	session := "socket-stale-zellij"
	socketPath := agentpane.DefaultSocketPath(session, "stale-a")
	readyPath := agentpane.DefaultReadyPath(session, "stale-a")
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(socket dir): %v", err)
	}
	if err := os.WriteFile(socketPath, []byte{}, 0o600); err != nil {
		t.Fatalf("WriteFile(socket placeholder): %v", err)
	}
	if err := os.WriteFile(readyPath, []byte("ready\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(ready): %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(socketPath)
		_ = os.Remove(readyPath)
	})

	panes := listZellijBoundPanes(nil, session)
	if len(panes) != 1 {
		t.Fatalf("len(panes) = %d, want 1", len(panes))
	}
	if panes[0].State != domainagent.StateStopped {
		t.Fatalf("State = %q, want stopped for unreachable socket", panes[0].State)
	}
}

func TestZellijSingletonSubmitKind(t *testing.T) {
	t.Parallel()
	room := domainagent.RoomSummary{
		Members: []domainagent.RoomMember{
			{ActorID: "droid-a"},
			{ActorID: "gemini-a"},
			{ActorID: "claude-a"},
		},
	}
	if got := zellijSingletonSubmitKind(room, "droid-a"); got != "composer" {
		t.Fatalf("droid-a: %q want composer", got)
	}
	if got := zellijSingletonSubmitKind(room, "gemini-a"); got != "enter" {
		t.Fatalf("gemini-a: %q want enter", got)
	}
	if got := zellijSingletonSubmitKind(room, "claude-a"); got != "enter" {
		t.Fatalf("claude-a: %q want enter", got)
	}
	if got := zellijSingletonSubmitKind(room, domainagent.BroadcastRecipient); got != "enter" {
		t.Fatalf("broadcast: %q want enter", got)
	}
}
