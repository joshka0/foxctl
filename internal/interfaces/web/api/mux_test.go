package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/runtime/terminal/agentpane"
	"github.com/joshka0/foxctl/internal/runtime/terminal/tmuxbridge"
)

func TestMuxPanesHandlerTMUXReturnsViewerMetadata(t *testing.T) {
	socketPath := agentpane.DefaultSocketPath("work", "claude-a")
	readyPath := agentpane.DefaultReadyPath("work", "claude-a")
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen(unix): %v", err)
	}
	defer ln.Close()
	if err := os.WriteFile(readyPath, []byte("ready\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(ready): %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(socketPath)
		_ = os.Remove(readyPath)
	})

	orig := newMuxTMUXClient
	defer func() { newMuxTMUXClient = orig }()
	newMuxTMUXClient = func() *tmuxbridge.Client {
		return tmuxbridge.NewWithRunner(fakeMuxRunner{
			responses: map[string]fakeMuxResponse{
				"tmux list-sessions": {stdout: "ok\n"},
				"tmux list-panes -a -F " + tmuxMuxListFormat: {
					stdout: "%1" + tmuxMuxFieldSep + "work" + tmuxMuxFieldSep + "0" + tmuxMuxFieldSep + "0" + tmuxMuxFieldSep + "main" + tmuxMuxFieldSep + "111" + tmuxMuxFieldSep + "80" + tmuxMuxFieldSep + "24" + tmuxMuxFieldSep + "claude-a" + tmuxMuxFieldSep + "/repo" + tmuxMuxFieldSep + "foxctl" + tmuxMuxFieldSep + "1" + tmuxMuxFieldSep + "claude-a" + tmuxMuxFieldSep + "claude" + tmuxMuxFieldSep + "room-alpha" + tmuxMuxFieldSep + "1\n",
				},
			},
		}, map[string]string{})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/mux/panes?backend=tmux", nil)
	rr := httptest.NewRecorder()
	MuxPanesHandler(config.Config{}, zerolog.Nop()).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"participant_id":"claude-a"`) {
		t.Fatalf("body missing participant_id: %s", body)
	}
	if !strings.Contains(body, `"provider":"claude"`) {
		t.Fatalf("body missing provider: %s", body)
	}
	if !strings.Contains(body, `"display_command":"claude"`) {
		t.Fatalf("body missing display_command: %s", body)
	}
	if !strings.Contains(body, `"socket_path":"`+socketPath+`"`) {
		t.Fatalf("body missing socket_path: %s", body)
	}
	if !strings.Contains(body, `"ready_path":"`+readyPath+`"`) {
		t.Fatalf("body missing ready_path: %s", body)
	}
	if !strings.Contains(body, `"state":"running"`) {
		t.Fatalf("body missing running state: %s", body)
	}
}

func TestMuxPanesHandlerZellijScansSocketRegistry(t *testing.T) {
	shortTmp, err := os.MkdirTemp("/tmp", "agt-zellij-api-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(shortTmp) })
	t.Setenv("TMPDIR", shortTmp)

	session := "api-zellij-scan"
	participantID := "gemini-participant-with-a-long-suffix"
	socketPath := agentpane.DefaultSocketPath(session, participantID)
	readyPath := agentpane.DefaultReadyPath(session, participantID)
	metaPath := agentpane.MetadataPathForSocket(socketPath)
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
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
		RoomID:        "room-gamma",
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

	req := httptest.NewRequest(http.MethodGet, "/api/mux/panes?backend=zellij&session="+session, nil).WithContext(context.Background())
	rr := httptest.NewRecorder()
	MuxPanesHandler(config.Config{}, zerolog.Nop()).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"participant_id":"`+participantID+`"`) {
		t.Fatalf("body missing participant_id: %s", body)
	}
	if !strings.Contains(body, `"state":"running"`) {
		t.Fatalf("body missing running state: %s", body)
	}
	if !strings.Contains(body, `"room_id":"room-gamma"`) {
		t.Fatalf("body missing room_id: %s", body)
	}
}

func TestMuxReadHandlerTMUXReturnsCapture(t *testing.T) {
	orig := newMuxTMUXClient
	defer func() { newMuxTMUXClient = orig }()
	newMuxTMUXClient = func() *tmuxbridge.Client {
		return tmuxbridge.NewWithRunner(fakeMuxRunner{
			responses: map[string]fakeMuxResponse{
				"tmux list-sessions":                       {stdout: "ok\n"},
				"tmux display-message -t %1 -p #{pane_id}": {stdout: "%1\n"},
				"tmux display-message -t %1 -p " + tmuxMuxListFormat: {
					stdout: "%1" + tmuxMuxFieldSep + "work" + tmuxMuxFieldSep + "0" + tmuxMuxFieldSep + "0" + tmuxMuxFieldSep + "main" + tmuxMuxFieldSep + "111" + tmuxMuxFieldSep + "80" + tmuxMuxFieldSep + "24" + tmuxMuxFieldSep + "claude-a" + tmuxMuxFieldSep + "/repo" + tmuxMuxFieldSep + "foxctl" + tmuxMuxFieldSep + "1" + tmuxMuxFieldSep + "claude-a" + tmuxMuxFieldSep + "claude" + tmuxMuxFieldSep + "room-alpha" + tmuxMuxFieldSep + "1\n",
				},
				"tmux capture-pane -t %1 -p -J -S -40": {stdout: "hello\nworld\n"},
			},
		}, map[string]string{})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/mux/read?backend=tmux&target=%251&lines=40", nil)
	rr := httptest.NewRecorder()
	MuxReadHandler(config.Config{}, zerolog.Nop()).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"resolved_target":"%1"`) {
		t.Fatalf("body missing resolved_target: %s", body)
	}
	if !strings.Contains(body, `"content":"hello\nworld"`) {
		t.Fatalf("body missing capture content: %s", body)
	}
}

const (
	tmuxMuxFieldSep   = "\x1f"
	tmuxMuxListFormat = "#{pane_id}" + tmuxMuxFieldSep + "#{session_name}" + tmuxMuxFieldSep + "#{window_index}" + tmuxMuxFieldSep + "#{pane_index}" + tmuxMuxFieldSep + "#{window_name}" + tmuxMuxFieldSep + "#{pane_pid}" + tmuxMuxFieldSep + "#{pane_width}" + tmuxMuxFieldSep + "#{pane_height}" + tmuxMuxFieldSep + "#{@name}" + tmuxMuxFieldSep + "#{pane_current_path}" + tmuxMuxFieldSep + "#{pane_current_command}" + tmuxMuxFieldSep + "#{pane_active}" + tmuxMuxFieldSep + "#{@foxctl_participant}" + tmuxMuxFieldSep + "#{@foxctl_provider}" + tmuxMuxFieldSep + "#{@foxctl_room_id}" + tmuxMuxFieldSep + "#{@foxctl_wrapped}"
)

type fakeMuxRunner struct {
	responses map[string]fakeMuxResponse
}

type fakeMuxResponse struct {
	stdout string
	stderr string
	err    error
}

func (f fakeMuxRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	key := strings.TrimSpace(name + " " + strings.Join(args, " "))
	resp, ok := f.responses[key]
	if !ok {
		return "", "", os.ErrNotExist
	}
	return resp.stdout, resp.stderr, resp.err
}
