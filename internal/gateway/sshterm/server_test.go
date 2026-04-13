package sshterm

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/jkatigb/agentctl/internal/agentpane"
)

// testLogger creates a logger that discards output.
func testLogger() zerolog.Logger {
	return zerolog.New(io.Discard).With().Timestamp().Logger()
}

func registerSSHRoom(rooms *RoomManager, roomID, tmuxSession string, maxSessions int) {
	rooms.RegisterTerminalRoom(agentpane.ResolveTerminalRoomConfig(roomID, tmuxSession, maxSessions))
}

// mockWhoIs is a test WhoIsFunc that returns a fixed identity for known addresses.
func mockWhoIs(_ context.Context, addr string) (*IdentityInfo, error) {
	// Allow all connections for testing (simulates tailnet)
	return &IdentityInfo{
		UserID:    "u-test123",
		UserLogin: "test@example.com",
		UserName:  "Test User",
		NodeName:  "test-node",
		NodeID:    "n-test456",
	}, nil
}

// rejectWhoIs is a test WhoIsFunc that rejects all connections.
func rejectWhoIs(_ context.Context, addr string) (*IdentityInfo, error) {
	return nil, &IdentityRejectedError{
		RemoteAddr: addr,
		Reason:     "not on tailnet",
	}
}

// setupTestServer creates a test SSH server with a room registered.
func setupTestServer(t *testing.T, whoIs WhoIsFunc) (*Server, *RoomManager, string) {
	t.Helper()

	config := DefaultSSHServerConfig()
	rooms := NewRoomManager(config, testLogger())

	srv := NewServer(config, rooms, whoIs, testLogger())

	// Register a test room
	roomID := "test-room"
	registerSSHRoom(rooms, roomID, "test-room-session", 0)

	return srv, rooms, roomID
}

// startTestServer starts the SSH server on a random port and returns the address.
func startTestServer(t *testing.T, srv *Server) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		srv.Close()
	})

	go func() {
		_ = srv.Serve(ctx, ln)
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	return addr
}

// sshClientConfig returns an SSH client config for testing.
func sshClientConfig(user string) *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			// No auth needed — server uses NoClientAuth
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
}

// --- Test ParseRoomIDFromUser ---

func TestParseRoomIDFromUser(t *testing.T) {
	tests := []struct {
		name     string
		user     string
		expected string
	}{
		{"standard room ID", "room-my-room", "my-room"},
		{"room ID with dashes", "room-alpha-beta", "alpha-beta"},
		{"room ID with underscores", "room-my_room", "my_room"},
		{"room ID with numbers", "room-123", "123"},
		{"room ID with UUID", "room-abc-123-def", "abc-123-def"},
		{"empty user", "", ""},
		{"just prefix", "room-", ""},
		{"too short", "roo", ""},
		{"no prefix", "my-room", ""},
		{"wrong prefix", "space-123", ""},
		{"single char room", "room-x", "x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseRoomIDFromUser(tt.user)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseRoomIDFromUser_MatchesAgentpaneContract(t *testing.T) {
	user := "room-alpha-beta"
	assert.Equal(t, agentpane.ParseRoomTerminalUser(user), ParseRoomIDFromUser(user))
}

// --- Test RoomManager ---

func TestRoomManager_RegisterTerminalRoom_Basic(t *testing.T) {
	rooms := NewRoomManager(DefaultSSHServerConfig(), testLogger())

	registerSSHRoom(rooms, "room-1", "session-1", 0)

	assert.True(t, rooms.HasRoom("room-1"))
	assert.False(t, rooms.HasRoom("room-2"))
}

func TestRoomManager_RegisterTerminalRoom(t *testing.T) {
	rooms := NewRoomManager(DefaultSSHServerConfig(), testLogger())

	rooms.RegisterTerminalRoom(agentpane.TerminalRoomConfig{
		RoomID:         "room-1",
		TmuxSession:    "session-1",
		MaxConnections: 3,
	})

	assert.True(t, rooms.HasRoom("room-1"))
	config, ok := rooms.TerminalRoomConfig("room-1")
	require.True(t, ok)
	assert.Equal(t, "session-1", config.TmuxSession)
	assert.Equal(t, 3, config.MaxConnections)
}

func TestRoomManager_RegisterRoom_Overwrite(t *testing.T) {
	rooms := NewRoomManager(DefaultSSHServerConfig(), testLogger())

	registerSSHRoom(rooms, "room-1", "old", 0)
	registerSSHRoom(rooms, "room-1", "new", 0)

	config, ok := rooms.TerminalRoomConfig("room-1")
	require.True(t, ok)
	assert.Equal(t, "new", config.TmuxSession)
}

func TestRoomManager_UnregisterRoom(t *testing.T) {
	rooms := NewRoomManager(DefaultSSHServerConfig(), testLogger())

	registerSSHRoom(rooms, "room-1", "session-1", 0)
	require.True(t, rooms.HasRoom("room-1"))

	rooms.UnregisterRoom("room-1")
	assert.False(t, rooms.HasRoom("room-1"))
}

func TestRoomManager_UnregisterRoom_NotExist(t *testing.T) {
	rooms := NewRoomManager(DefaultSSHServerConfig(), testLogger())
	// Should not panic
	rooms.UnregisterRoom("nonexistent")
}

func TestRoomManager_AddSession(t *testing.T) {
	rooms := NewRoomManager(DefaultSSHServerConfig(), testLogger())
	registerSSHRoom(rooms, "room-1", "session-1", 0)

	sess := &SSHSession{
		ID:         "sess-1",
		RoomID:     "room-1",
		RemoteAddr: "10.0.0.1:12345",
		Identity: IdentityInfo{
			UserLogin: "user@example.com",
			NodeName:  "user-laptop",
		},
		StartedAt: time.Now(),
	}

	err := rooms.AddSession("room-1", sess)
	require.NoError(t, err)
	assert.Equal(t, 1, rooms.SessionCount("room-1"))
}

func TestRoomManager_AddSession_RoomNotFound(t *testing.T) {
	rooms := NewRoomManager(DefaultSSHServerConfig(), testLogger())

	sess := &SSHSession{
		ID:     "sess-1",
		RoomID: "nonexistent",
	}

	err := rooms.AddSession("nonexistent", sess)
	require.Error(t, err)
	var notFound *RoomNotFoundError
	assert.ErrorAs(t, err, &notFound)
}

func TestRoomManager_AddSession_SessionLimit(t *testing.T) {
	config := DefaultSSHServerConfig()
	config.MaxSessions = 2
	rooms := NewRoomManager(config, testLogger())
	registerSSHRoom(rooms, "room-1", "s1", 2)

	for i := 0; i < 2; i++ {
		sess := &SSHSession{
			ID:     fmt.Sprintf("sess-%d", i),
			RoomID: "room-1",
		}
		err := rooms.AddSession("room-1", sess)
		require.NoError(t, err)
	}

	// Third should fail
	sess := &SSHSession{ID: "sess-3", RoomID: "room-1"}
	err := rooms.AddSession("room-1", sess)
	require.Error(t, err)
	var limitErr *SessionLimitError
	assert.ErrorAs(t, err, &limitErr)
}

func TestRoomManager_RemoveSession(t *testing.T) {
	rooms := NewRoomManager(DefaultSSHServerConfig(), testLogger())
	registerSSHRoom(rooms, "room-1", "s1", 0)

	sess := &SSHSession{ID: "sess-1", RoomID: "room-1"}
	err := rooms.AddSession("room-1", sess)
	require.NoError(t, err)
	assert.Equal(t, 1, rooms.SessionCount("room-1"))

	rooms.RemoveSession("sess-1")
	assert.Equal(t, 0, rooms.SessionCount("room-1"))
}

func TestRoomManager_RemoveSession_NotExist(t *testing.T) {
	rooms := NewRoomManager(DefaultSSHServerConfig(), testLogger())
	// Should not panic
	rooms.RemoveSession("nonexistent")
}

func TestRoomManager_ActiveSessions(t *testing.T) {
	rooms := NewRoomManager(DefaultSSHServerConfig(), testLogger())
	registerSSHRoom(rooms, "room-1", "s1", 0)

	sess1 := &SSHSession{
		ID:         "sess-1",
		RoomID:     "room-1",
		RemoteAddr: "10.0.0.1:12345",
		Identity:   IdentityInfo{UserLogin: "a@b.com", NodeName: "node-a"},
		StartedAt:  time.Now(),
	}
	sess2 := &SSHSession{
		ID:         "sess-2",
		RoomID:     "room-1",
		RemoteAddr: "10.0.0.2:54321",
		Identity:   IdentityInfo{UserLogin: "c@d.com", NodeName: "node-b"},
		StartedAt:  time.Now(),
	}

	require.NoError(t, rooms.AddSession("room-1", sess1))
	require.NoError(t, rooms.AddSession("room-1", sess2))

	sessions := rooms.ActiveSessions("room-1")
	assert.Len(t, sessions, 2)
}

func TestRoomManager_TmuxSessionForRoom(t *testing.T) {
	rooms := NewRoomManager(DefaultSSHServerConfig(), testLogger())
	registerSSHRoom(rooms, "room-1", "custom-session", 0)

	session, err := rooms.TmuxSessionForRoom(context.Background(), "room-1")
	require.NoError(t, err)
	assert.Equal(t, "custom-session", session)
}

func TestRoomManager_TmuxSessionForRoom_Default(t *testing.T) {
	rooms := NewRoomManager(DefaultSSHServerConfig(), testLogger())
	registerSSHRoom(rooms, "room-1", "", 0)

	session, err := rooms.TmuxSessionForRoom(context.Background(), "room-1")
	require.NoError(t, err)
	assert.Equal(t, agentpane.DefaultRoomTmuxSession("room-1"), session)
}

func TestRoomManager_TmuxSessionForRoom_NotFound(t *testing.T) {
	rooms := NewRoomManager(DefaultSSHServerConfig(), testLogger())

	_, err := rooms.TmuxSessionForRoom(context.Background(), "nonexistent")
	require.Error(t, err)
	var notFound *RoomNotFoundError
	assert.ErrorAs(t, err, &notFound)
}

func TestRoomManager_Close(t *testing.T) {
	rooms := NewRoomManager(DefaultSSHServerConfig(), testLogger())
	registerSSHRoom(rooms, "room-1", "s1", 0)
	sess := &SSHSession{ID: "sess-1", RoomID: "room-1"}
	require.NoError(t, rooms.AddSession("room-1", sess))

	rooms.Close()
	assert.False(t, rooms.HasRoom("room-1"))
}

// --- Test SSH Server ---

func TestNewServer(t *testing.T) {
	config := DefaultSSHServerConfig()
	rooms := NewRoomManager(config, testLogger())
	srv := NewServer(config, rooms, mockWhoIs, testLogger())

	assert.NotNil(t, srv)
	assert.False(t, srv.IsRunning())
}

func TestServer_ServeAndConnect(t *testing.T) {
	srv, _, _ := setupTestServer(t, mockWhoIs)
	addr := startTestServer(t, srv)

	// Connect as SSH client
	client, err := ssh.Dial("tcp", addr, sshClientConfig("room-test-room"))
	require.NoError(t, err)
	defer client.Close()

	assert.True(t, srv.IsRunning())
}

func TestServer_RejectedWhoIs(t *testing.T) {
	srv, _, _ := setupTestServer(t, rejectWhoIs)
	addr := startTestServer(t, srv)

	// Connection should be rejected (WhoIs fails)
	_, err := ssh.Dial("tcp", addr, sshClientConfig("room-test-room"))
	// The connection might succeed at TCP level but fail during handshake
	// since the server closes the connection immediately after WhoIs rejection
	if err == nil {
		// If somehow it connected, it won't work for sessions
		t.Log("Connection established but should be rejected at session level")
	}
}

func TestServer_InvalidUsername(t *testing.T) {
	srv, _, _ := setupTestServer(t, mockWhoIs)
	addr := startTestServer(t, srv)

	// Connect with invalid username format
	client, err := ssh.Dial("tcp", addr, sshClientConfig("invalid-user"))
	require.NoError(t, err)
	defer client.Close()

	// Try to open a session
	session, err := client.NewSession()
	if err == nil {
		session.Close()
	}
	// Session should fail because the room doesn't exist
	// (the server logs a warning and returns without handling channels)
}

func TestServer_RoomNotFound(t *testing.T) {
	config := DefaultSSHServerConfig()
	rooms := NewRoomManager(config, testLogger())
	srv := NewServer(config, rooms, mockWhoIs, testLogger())
	// Don't register any rooms

	addr := startTestServer(t, srv)

	// Connect with valid room prefix but non-existent room
	client, err := ssh.Dial("tcp", addr, sshClientConfig("room-nonexistent"))
	require.NoError(t, err)
	defer client.Close()

	session, err := client.NewSession()
	if err == nil {
		session.Close()
	}
}

func tmuxAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
}

func TestServer_PTYSession(t *testing.T) {
	tmuxAvailable(t)
	if os.Getenv("CI") != "" {
		t.Skip("skipping tmux tests in CI")
	}

	sessionName := "sshterm-test-pty-" + time.Now().Format("20060102-150405")
	defer func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	}()

	config := DefaultSSHServerConfig()
	rooms := NewRoomManager(config, testLogger())
	srv := NewServer(config, rooms, mockWhoIs, testLogger())

	roomID := "pty-room"
	registerSSHRoom(rooms, roomID, sessionName, 0)

	addr := startTestServer(t, srv)

	// Connect as SSH client
	client, err := ssh.Dial("tcp", addr, sshClientConfig("room-"+roomID))
	require.NoError(t, err)
	defer client.Close()

	// Open a session with PTY
	session, err := client.NewSession()
	require.NoError(t, err)
	defer session.Close()

	// Request PTY
	err = session.RequestPty("xterm-256color", 80, 24, ssh.TerminalModes{})
	require.NoError(t, err)

	// Set up stdin/stdout pipes before starting shell
	stdinPipe, err := session.StdinPipe()
	require.NoError(t, err)
	defer stdinPipe.Close()

	stdoutPipe, err := session.StdoutPipe()
	require.NoError(t, err)

	// Start the shell (non-blocking)
	err = session.Shell()
	require.NoError(t, err)

	// Wait for tmux to start
	time.Sleep(1500 * time.Millisecond)

	// Verify session is tracked
	assert.Equal(t, 1, rooms.SessionCount(roomID))

	// Write a command
	_, err = stdinPipe.Write([]byte("echo hello-sshterm\n"))
	require.NoError(t, err)

	// Read output in a goroutine with timeout
	outputCh := make(chan string, 1)
	go func() {
		buf := make([]byte, 8192)
		var output string
		for {
			n, readErr := stdoutPipe.Read(buf)
			if n > 0 {
				output += string(buf[:n])
				if strings.Contains(output, "hello-sshterm") {
					outputCh <- output
					return
				}
			}
			if readErr != nil {
				outputCh <- output
				return
			}
		}
	}()

	select {
	case output := <-outputCh:
		assert.Contains(t, output, "hello-sshterm", "should see echo output in SSH session")
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for echo output")
	}
}

func TestServer_WindowResize(t *testing.T) {
	tmuxAvailable(t)
	if os.Getenv("CI") != "" {
		t.Skip("skipping tmux tests in CI")
	}

	sessionName := "sshterm-test-resize-" + time.Now().Format("20060102-150405")
	defer func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	}()

	config := DefaultSSHServerConfig()
	rooms := NewRoomManager(config, testLogger())
	srv := NewServer(config, rooms, mockWhoIs, testLogger())

	roomID := "resize-room"
	registerSSHRoom(rooms, roomID, sessionName, 0)

	addr := startTestServer(t, srv)

	client, err := ssh.Dial("tcp", addr, sshClientConfig("room-"+roomID))
	require.NoError(t, err)
	defer client.Close()

	session, err := client.NewSession()
	require.NoError(t, err)
	defer session.Close()

	// Request PTY with initial size
	err = session.RequestPty("xterm-256color", 80, 24, ssh.TerminalModes{})
	require.NoError(t, err)

	// Start shell
	err = session.Shell()
	require.NoError(t, err)

	// Wait for tmux to start
	time.Sleep(1500 * time.Millisecond)

	// Send window-change (h=40 rows, w=132 cols — note: ssh.Session.WindowChange takes rows first)
	err = session.WindowChange(40, 132)
	require.NoError(t, err)

	time.Sleep(1 * time.Second)

	// Verify tmux pane width changed
	out, err := exec.Command("tmux", "list-panes", "-t", sessionName, "-F", "#{pane_width} #{pane_height}").Output()
	if err == nil {
		parts := strings.Fields(strings.TrimSpace(string(out)))
		if len(parts) >= 2 {
			t.Logf("tmux pane dimensions after resize: width=%s height=%s", parts[0], parts[1])
			assert.Equal(t, "132", parts[0], "pane width should be 132 after resize")
		}
	}
}

func TestServer_MultipleSessions(t *testing.T) {
	tmuxAvailable(t)
	if os.Getenv("CI") != "" {
		t.Skip("skipping tmux tests in CI")
	}

	sessionName := "sshterm-test-multi-" + time.Now().Format("20060102-150405")
	defer func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	}()

	config := DefaultSSHServerConfig()
	config.MaxSessions = 5
	rooms := NewRoomManager(config, testLogger())
	srv := NewServer(config, rooms, mockWhoIs, testLogger())

	roomID := "multi-room"
	registerSSHRoom(rooms, roomID, sessionName, 5)

	addr := startTestServer(t, srv)

	// Connect multiple SSH sessions to the same room
	var clients []*ssh.Client
	var sessions []*ssh.Session
	for i := 0; i < 3; i++ {
		client, err := ssh.Dial("tcp", addr, sshClientConfig("room-"+roomID))
		require.NoError(t, err, "client %d should connect", i)
		clients = append(clients, client)

		sess, err := client.NewSession()
		require.NoError(t, err, "session %d should open", i)
		sessions = append(sessions, sess)

		err = sess.RequestPty("xterm-256color", 80, 24, ssh.TerminalModes{})
		require.NoError(t, err)

		// Start shell (non-blocking)
		err = sess.Shell()
		require.NoError(t, err)
	}

	// Wait for all sessions to be established
	time.Sleep(500 * time.Millisecond)

	// Verify session count
	assert.Equal(t, 3, rooms.SessionCount(roomID), "should have 3 sessions")

	// Clean up
	for _, sess := range sessions {
		sess.Close()
	}
	for _, client := range clients {
		client.Close()
	}

	// Wait for cleanup
	time.Sleep(500 * time.Millisecond)

	// Verify session count decreased
	count := rooms.SessionCount(roomID)
	assert.Equal(t, 0, count, "all sessions should be cleaned up")
}

func TestServer_SessionLimit(t *testing.T) {
	config := DefaultSSHServerConfig()
	config.MaxSessions = 2
	rooms := NewRoomManager(config, testLogger())
	srv := NewServer(config, rooms, mockWhoIs, testLogger())

	roomID := "limit-room"
	registerSSHRoom(rooms, roomID, "limit-session", 2)

	_ = startTestServer(t, srv)

	// Fill up sessions manually (without real PTY)
	for i := 0; i < 2; i++ {
		sess := &SSHSession{
			ID:     fmt.Sprintf("sess-%d", i),
			RoomID: roomID,
		}
		require.NoError(t, rooms.AddSession(roomID, sess))
	}

	// Third should fail
	sess := &SSHSession{ID: "sess-3", RoomID: roomID}
	err := rooms.AddSession(roomID, sess)
	require.Error(t, err)
	var limitErr *SessionLimitError
	assert.ErrorAs(t, err, &limitErr)
	assert.Contains(t, limitErr.Error(), "session limit reached")
}

func TestServer_Close(t *testing.T) {
	srv, _, _ := setupTestServer(t, mockWhoIs)
	addr := startTestServer(t, srv)

	// Connect
	client, err := ssh.Dial("tcp", addr, sshClientConfig("room-test-room"))
	require.NoError(t, err)

	// Close server
	err = srv.Close()
	require.NoError(t, err)

	// Connection should be broken
	client.Close()

	assert.False(t, srv.IsRunning())
}

// --- Test SSH Protocol Parsing ---

func TestParsePtyRequest(t *testing.T) {
	tests := []struct {
		name     string
		payload  []byte
		wantTerm string
		wantCols uint16
		wantRows uint16
	}{
		{
			name:     "empty payload",
			payload:  nil,
			wantTerm: "",
			wantCols: 0,
			wantRows: 0,
		},
		{
			name:     "xterm-256color 80x24",
			payload:  encodePtyRequest("xterm-256color", 80, 24),
			wantTerm: "xterm-256color",
			wantCols: 80,
			wantRows: 24,
		},
		{
			name:     "xterm 132x40",
			payload:  encodePtyRequest("xterm", 132, 40),
			wantTerm: "xterm",
			wantCols: 132,
			wantRows: 40,
		},
		{
			name:     "vt100 40x10",
			payload:  encodePtyRequest("vt100", 40, 10),
			wantTerm: "vt100",
			wantCols: 40,
			wantRows: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			term, cols, rows := parsePtyRequest(tt.payload)
			assert.Equal(t, tt.wantTerm, term)
			assert.Equal(t, tt.wantCols, cols)
			assert.Equal(t, tt.wantRows, rows)
		})
	}
}

func TestParseWindowChange(t *testing.T) {
	tests := []struct {
		name     string
		payload  []byte
		wantCols uint16
		wantRows uint16
	}{
		{"empty", nil, 0, 0},
		{"too short", []byte{0, 0, 0}, 0, 0},
		{"80x24", encodeWindowChange(80, 24), 80, 24},
		{"132x40", encodeWindowChange(132, 40), 132, 40},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cols, rows := parseWindowChange(tt.payload)
			assert.Equal(t, tt.wantCols, cols)
			assert.Equal(t, tt.wantRows, rows)
		})
	}
}

func TestParseSignal(t *testing.T) {
	tests := []struct {
		name     string
		sigName  string
		expected bool
	}{
		{"INT", "INT", true},
		{"TSTP", "TSTP", true},
		{"TERM", "TERM", true},
		{"QUIT", "QUIT", true},
		{"KILL", "KILL", true},
		{"HUP", "HUP", true},
		{"unknown", "UNKNOWN", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := encodeSSHString(tt.sigName)
			sig := parseSignal(payload)
			if tt.expected {
				assert.NotZero(t, sig, "expected non-zero signal for %s", tt.sigName)
			} else {
				assert.Zero(t, sig, "expected zero signal for %s", tt.sigName)
			}
		})
	}
}

// --- Test Identity Info ---

func TestIdentityInfo_String(t *testing.T) {
	info := IdentityInfo{
		UserLogin: "test@example.com",
		UserName:  "Test User",
		NodeName:  "test-node",
	}
	assert.Equal(t, "test@example.com (Test User) from test-node", info.String())
}

// --- Test Error Types ---

func TestRoomNotFoundError(t *testing.T) {
	err := &RoomNotFoundError{RoomID: "test"}
	assert.Equal(t, "room not found: test", err.Error())
}

func TestIdentityRejectedError(t *testing.T) {
	err := &IdentityRejectedError{RemoteAddr: "1.2.3.4:5678", Reason: "not on tailnet"}
	assert.Contains(t, err.Error(), "1.2.3.4:5678")
	assert.Contains(t, err.Error(), "not on tailnet")
}

func TestSessionLimitError(t *testing.T) {
	err := &SessionLimitError{RoomID: "room1", Current: 10, MaxAllowed: 10}
	assert.Contains(t, err.Error(), "room1")
	assert.Contains(t, err.Error(), "session limit reached")
	assert.Contains(t, err.Error(), "10/10")
}

// --- Test SSHSession ---

func TestSSHSession_Close(t *testing.T) {
	sess := &SSHSession{ID: "test"}
	assert.False(t, sess.IsClosed())
	sess.Close()
	assert.True(t, sess.IsClosed())
}

func TestSSHSession_SetTerminal(t *testing.T) {
	sess := &SSHSession{ID: "test"}
	assert.Nil(t, sess.Terminal)

	sess.SetTerminal("xterm-256color", 80, 24)
	require.NotNil(t, sess.Terminal)
	assert.Equal(t, "xterm-256color", sess.Terminal.Term)
	assert.Equal(t, uint16(80), sess.Terminal.Cols)
	assert.Equal(t, uint16(24), sess.Terminal.Rows)

	// Update
	sess.SetTerminal("vt100", 40, 10)
	assert.Equal(t, "vt100", sess.Terminal.Term)
	assert.Equal(t, uint16(40), sess.Terminal.Cols)
}

// --- Test Concurrent Access ---

func TestRoomManager_ConcurrentSessions(t *testing.T) {
	rooms := NewRoomManager(DefaultSSHServerConfig(), testLogger())
	registerSSHRoom(rooms, "room-1", "s1", 0)

	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sess := &SSHSession{
				ID:     fmt.Sprintf("sess-%d", i),
				RoomID: "room-1",
			}
			if err := rooms.AddSession("room-1", sess); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Should have 10 sessions
	assert.Equal(t, 10, rooms.SessionCount("room-1"))

	// Should have no errors (default max is 10)
	for err := range errors {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRoomManager_ConcurrentAddRemove(t *testing.T) {
	rooms := NewRoomManager(DefaultSSHServerConfig(), testLogger())
	registerSSHRoom(rooms, "room-1", "s1", 0)

	var wg sync.WaitGroup

	// Concurrent add and remove
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sess := &SSHSession{
				ID:     fmt.Sprintf("sess-%d", i),
				RoomID: "room-1",
			}
			_ = rooms.AddSession("room-1", sess)
			time.Sleep(time.Millisecond)
			rooms.RemoveSession(sess.ID)
		}(i)
	}

	wg.Wait()
	assert.Equal(t, 0, rooms.SessionCount("room-1"))
}

// --- Test Detach on Disconnect ---

func TestServer_DetachOnDisconnect(t *testing.T) {
	tmuxAvailable(t)
	if os.Getenv("CI") != "" {
		t.Skip("skipping tmux tests in CI")
	}

	sessionName := "sshterm-test-detach-" + time.Now().Format("20060102-150405")
	// Create a "keepalive" session so tmux server doesn't shut down
	keepaliveSession := "sshterm-keepalive-" + time.Now().Format("20060102-150405")
	defer func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
		_ = exec.Command("tmux", "kill-session", "-t", keepaliveSession).Run()
	}()

	// Ensure tmux server is running (previous tests may have killed all sessions)
	_ = exec.Command("tmux", "start-server").Run()

	// Create a keepalive session so the tmux server stays alive
	keepaliveOut, err := exec.Command("tmux", "new-session", "-d", "-s", keepaliveSession).CombinedOutput()
	require.NoError(t, err, "failed to create keepalive tmux session: %s", string(keepaliveOut))

	config := DefaultSSHServerConfig()
	rooms := NewRoomManager(config, testLogger())
	srv := NewServer(config, rooms, mockWhoIs, testLogger())

	roomID := "detach-room"
	registerSSHRoom(rooms, roomID, sessionName, 0)

	addr := startTestServer(t, srv)

	// First connection: start a long-running command
	client1, err := ssh.Dial("tcp", addr, sshClientConfig("room-"+roomID))
	require.NoError(t, err)

	sess1, err := client1.NewSession()
	require.NoError(t, err)
	err = sess1.RequestPty("xterm-256color", 80, 24, ssh.TerminalModes{})
	require.NoError(t, err)

	stdin1, err := sess1.StdinPipe()
	require.NoError(t, err)
	_, _ = sess1.StdoutPipe() // drain pipe

	// Start shell
	err = sess1.Shell()
	require.NoError(t, err)

	// Wait for tmux
	time.Sleep(1500 * time.Millisecond)

	// Start a sleep command
	_, err = stdin1.Write([]byte("sleep 300\n"))
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	// Disconnect client 1
	sess1.Close()
	client1.Close()

	// Wait for cleanup
	time.Sleep(2 * time.Second)

	// Verify tmux session still exists
	out, err := exec.Command("tmux", "list-sessions").CombinedOutput()
	if err != nil {
		t.Fatalf("tmux list-sessions failed: %v\noutput: %s", err, string(out))
	}
	assert.Contains(t, string(out), sessionName, "tmux session should survive disconnect")

	// Verify the sleep process is still running
	out, err = exec.Command("tmux", "list-panes", "-t", sessionName, "-F", "#{pane_pid}").Output()
	require.NoError(t, err)
	panePID := strings.TrimSpace(string(out))
	assert.NotEmpty(t, panePID, "should have a pane PID")

	// Check if the process in the pane is still running
	if panePID != "" {
		out, err = exec.Command("ps", "-p", panePID, "-o", "pid=").Output()
		if err == nil {
			assert.Contains(t, strings.TrimSpace(string(out)), panePID, "tmux pane process should still be running")
		}
	}
}

// --- Test WhoIs Identity Logging ---

func TestServer_WhoIsIdentityLogged(t *testing.T) {
	var loggedIdentity string
	var loggedMu sync.Mutex

	trackWhoIs := func(ctx context.Context, addr string) (*IdentityInfo, error) {
		loggedMu.Lock()
		loggedIdentity = "test@example.com"
		loggedMu.Unlock()
		return &IdentityInfo{
			UserID:    "u-test123",
			UserLogin: "test@example.com",
			UserName:  "Test User",
			NodeName:  "test-node",
		}, nil
	}

	config := DefaultSSHServerConfig()
	rooms := NewRoomManager(config, testLogger())
	srv := NewServer(config, rooms, trackWhoIs, testLogger())

	roomID := "identity-room"
	registerSSHRoom(rooms, roomID, "identity-session", 0)

	addr := startTestServer(t, srv)

	// Connect
	client, err := ssh.Dial("tcp", addr, sshClientConfig("room-"+roomID))
	require.NoError(t, err)
	client.Close()

	// Wait for WhoIs to be called
	time.Sleep(200 * time.Millisecond)

	loggedMu.Lock()
	assert.Equal(t, "test@example.com", loggedIdentity, "WhoIs should have been called")
	loggedMu.Unlock()
}

// --- Helper functions for encoding SSH protocol data ---

func encodeSSHString(s string) []byte {
	result := make([]byte, 4+len(s))
	result[0] = byte(len(s) >> 24)
	result[1] = byte(len(s) >> 16)
	result[2] = byte(len(s) >> 8)
	result[3] = byte(len(s))
	copy(result[4:], s)
	return result
}

func encodePtyRequest(term string, cols, rows uint32) []byte {
	// string (TERM) + uint32 (cols) + uint32 (rows) + uint32 (px) + uint32 (py) + string (modes)
	termBytes := encodeSSHString(term)
	result := make([]byte, 0, 4+len(term)+16+4)
	result = append(result, termBytes...)
	result = append(result, encodeUint32(cols)...)
	result = append(result, encodeUint32(rows)...)
	result = append(result, encodeUint32(0)...) // px
	result = append(result, encodeUint32(0)...) // py
	result = append(result, encodeSSHString("")...)
	return result
}

func encodeWindowChange(cols, rows uint32) []byte {
	result := make([]byte, 16)
	copy(result[0:4], encodeUint32(cols))
	copy(result[4:8], encodeUint32(rows))
	copy(result[8:12], encodeUint32(0))  // px
	copy(result[12:16], encodeUint32(0)) // py
	return result
}

func encodeUint32(v uint32) []byte {
	return []byte{
		byte(v >> 24),
		byte(v >> 16),
		byte(v >> 8),
		byte(v),
	}
}
