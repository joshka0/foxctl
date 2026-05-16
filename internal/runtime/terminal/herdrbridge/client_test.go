package herdrbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSocketPathOrder(t *testing.T) {
	env := map[string]string{
		"HOME":              "/home/test",
		"XDG_CONFIG_HOME":   "/xdg",
		"HERDR_SOCKET_PATH": "/tmp/inherited.sock",
		"HERDR_SESSION":     "envsession",
	}

	if got := ResolveSocketPath(Options{Session: "work", SocketPath: "/tmp/explicit.sock", Env: env}); got != "/tmp/explicit.sock" {
		t.Fatalf("explicit socket override path = %q", got)
	}
	if got := ResolveSocketPath(Options{Session: "work", Env: env}); got != "/xdg/herdr/sessions/work/herdr.sock" {
		t.Fatalf("explicit session path = %q", got)
	}
	if got := ResolveSocketPath(Options{Session: "default", Env: env}); got != "/xdg/herdr/herdr.sock" {
		t.Fatalf("explicit default session path = %q", got)
	}
	if got := ResolveSocketPath(Options{SocketPath: "/tmp/explicit.sock", Env: env}); got != "/tmp/explicit.sock" {
		t.Fatalf("explicit socket path = %q", got)
	}
	if got := ResolveSocketPath(Options{Env: env}); got != "/tmp/inherited.sock" {
		t.Fatalf("env socket path = %q", got)
	}
	delete(env, "HERDR_SOCKET_PATH")
	if got := ResolveSocketPath(Options{Env: env}); got != "/xdg/herdr/sessions/envsession/herdr.sock" {
		t.Fatalf("env session path = %q", got)
	}
	delete(env, "HERDR_SESSION")
	if got := ResolveSocketPath(Options{Env: env}); got != "/xdg/herdr/herdr.sock" {
		t.Fatalf("default socket path = %q", got)
	}
}

func TestListReadsPaneList(t *testing.T) {
	server := startFakeHerdrServer(t, func(req rawRequest) any {
		if req.Method != "pane.list" {
			t.Fatalf("method=%q want pane.list", req.Method)
		}
		return map[string]any{
			"id": req.ID,
			"result": map[string]any{
				"type": "pane_list",
				"panes": []map[string]any{
					{
						"pane_id":       "w1-1",
						"workspace_id":  "w1",
						"tab_id":        "w1:1",
						"focused":       true,
						"cwd":           "/repo",
						"label":         "codex-a",
						"agent":         "codex",
						"agent_status":  "working",
						"custom_status": "indexing",
						"revision":      12,
					},
				},
			},
		}
	})

	client := NewWithOptions(Options{SocketPath: server.socketPath})
	panes, err := client.List(context.Background(), ListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(panes) != 1 {
		t.Fatalf("len(panes)=%d want 1", len(panes))
	}
	if panes[0].PaneID != "w1-1" || panes[0].Label != "codex-a" || panes[0].AgentStatus != "working" {
		t.Fatalf("pane=%+v", panes[0])
	}
}

func TestReadSendsRecentRequest(t *testing.T) {
	server := startFakeHerdrServer(t, func(req rawRequest) any {
		if req.Method != "pane.read" {
			t.Fatalf("method=%q want pane.read", req.Method)
		}
		var params struct {
			PaneID    string `json:"pane_id"`
			Source    string `json:"source"`
			Lines     int    `json:"lines"`
			StripANSI bool   `json:"strip_ansi"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			t.Fatalf("params decode: %v", err)
		}
		if params.PaneID != "w1-1" || params.Source != "recent" || params.Lines != 20 || !params.StripANSI {
			t.Fatalf("params=%+v", params)
		}
		return map[string]any{
			"id": req.ID,
			"result": map[string]any{
				"type": "pane_read",
				"read": map[string]any{
					"pane_id":      "w1-1",
					"workspace_id": "w1",
					"tab_id":       "w1:1",
					"source":       "recent",
					"format":       "text",
					"text":         "line one\nline two",
					"revision":     13,
					"truncated":    false,
				},
			},
		}
	})

	client := NewWithOptions(Options{SocketPath: server.socketPath})
	got, err := client.Read(context.Background(), "w1-1", ReadOptions{Lines: 20})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got.Text != "line one\nline two" || got.Revision != 13 {
		t.Fatalf("read=%+v", got)
	}
}

func TestSendInputReturnsHerdrError(t *testing.T) {
	server := startFakeHerdrServer(t, func(req rawRequest) any {
		return map[string]any{
			"id": req.ID,
			"error": map[string]any{
				"code":    "pane_not_found",
				"message": "pane nope not found",
			},
		}
	})

	client := NewWithOptions(Options{SocketPath: server.socketPath})
	_, err := client.SendInput(context.Background(), "nope", "hello", []string{"Enter"})
	if err == nil {
		t.Fatal("SendInput() error = nil, want error")
	}
	var herdrErr *HerdrError
	if !errors.As(err, &herdrErr) {
		t.Fatalf("error type=%T want *HerdrError", err)
	}
	if herdrErr.Code != "pane_not_found" {
		t.Fatalf("code=%q want pane_not_found", herdrErr.Code)
	}
}

func TestSubmitKeys(t *testing.T) {
	tests := []struct {
		name string
		mode string
		want []string
	}{
		{name: "default", mode: "", want: []string{"Esc", "Enter"}},
		{name: "escape enter", mode: SubmitModeEscapeEnter, want: []string{"Esc", "Enter"}},
		{name: "enter only", mode: SubmitModeEnterOnly, want: []string{"Enter"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := submitKeys(tt.mode)
			if err != nil {
				t.Fatalf("submitKeys() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("keys=%v want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("keys=%v want %v", got, tt.want)
				}
			}
		})
	}
}

func TestParseParticipantID(t *testing.T) {
	ref, ok := ParseParticipantID("herdr:work:w1-2")
	if !ok {
		t.Fatal("ParseParticipantID() ok=false")
	}
	if ref.Session != "work" || ref.PaneID != "w1-2" {
		t.Fatalf("ref=%+v", ref)
	}
}

type fakeHerdrServer struct {
	socketPath string
}

func startFakeHerdrServer(t *testing.T, handler func(rawRequest) any) fakeHerdrServer {
	t.Helper()
	socketDir, err := os.MkdirTemp("/tmp", "hd")
	if err != nil {
		t.Fatalf("mkdir temp socket dir: %v", err)
	}
	socketPath := filepath.Join(socketDir, "h.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
		_ = os.RemoveAll(socketDir)
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		line, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
			return
		}
		var req rawRequest
		if err := json.Unmarshal(line, &req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		_ = json.NewEncoder(conn).Encode(handler(req))
	}()
	t.Cleanup(func() {
		<-done
	})
	return fakeHerdrServer{socketPath: socketPath}
}
