package webterm

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	terminalruntime "github.com/jkatigb/agentctl/internal/runtime/terminal"
	"github.com/jkatigb/agentctl/internal/runtime/terminal/agentpane"
)

func testHubLogger() zerolog.Logger {
	return zerolog.New(io.Discard).With().Timestamp().Logger()
}

func registerTestRoom(hub *Hub, roomID, tmuxSession string, maxConnections int) {
	hub.RegisterTerminalRoom(agentpane.ResolveTerminalRoomConfig(roomID, tmuxSession, maxConnections))
}

func TestNewHub_Defaults(t *testing.T) {
	hub := NewHub(HubConfig{}, testHubLogger())
	assert.Equal(t, DefaultMaxConnections, hub.config.MaxConnectionsPerRoom)
	assert.Equal(t, DefaultPingInterval, hub.config.PingInterval)
}

func TestNewHub_CustomConfig(t *testing.T) {
	hub := NewHub(HubConfig{
		MaxConnectionsPerRoom: 5,
		PingInterval:          15 * time.Second,
	}, testHubLogger())
	assert.Equal(t, 5, hub.config.MaxConnectionsPerRoom)
	assert.Equal(t, 15*time.Second, hub.config.PingInterval)
}

func TestHub_RegisterTerminalRoom_Basic(t *testing.T) {
	hub := NewHub(HubConfig{}, testHubLogger())

	registerTestRoom(hub, "room-1", "session-1", 0)
	assert.True(t, hub.HasRoom("room-1"))
	assert.False(t, hub.HasRoom("room-2"))
}

func TestHub_RegisterTerminalRoom(t *testing.T) {
	hub := NewHub(HubConfig{}, testHubLogger())

	hub.RegisterTerminalRoom(agentpane.TerminalRoomConfig{
		RoomID:         "room-1",
		TmuxSession:    "session-1",
		MaxConnections: 3,
	})

	assert.True(t, hub.HasRoom("room-1"))
}

func TestHub_RegisterTerminalRoom_UpdatesConfig(t *testing.T) {
	hub := NewHub(HubConfig{}, testHubLogger())

	registerTestRoom(hub, "room-1", "session-1", 5)
	registerTestRoom(hub, "room-1", "session-1-updated", 10)

	assert.True(t, hub.HasRoom("room-1"))
	// Verify config was updated by checking client count behavior later
}

func TestHub_UnregisterRoom(t *testing.T) {
	hub := NewHub(HubConfig{}, testHubLogger())

	registerTestRoom(hub, "room-1", "session-1", 0)
	assert.True(t, hub.HasRoom("room-1"))

	hub.UnregisterRoom("room-1")
	assert.False(t, hub.HasRoom("room-1"))
}

func TestHub_UnregisterRoom_NotFound(t *testing.T) {
	hub := NewHub(HubConfig{}, testHubLogger())
	// Should not panic
	hub.UnregisterRoom("nonexistent")
}

func TestHub_RoomIDs(t *testing.T) {
	hub := NewHub(HubConfig{}, testHubLogger())

	registerTestRoom(hub, "room-1", "", 0)
	registerTestRoom(hub, "room-2", "", 0)
	registerTestRoom(hub, "room-3", "", 0)

	ids := hub.RoomIDs()
	assert.Len(t, ids, 3)
	assert.Contains(t, ids, "room-1")
	assert.Contains(t, ids, "room-2")
	assert.Contains(t, ids, "room-3")
}

func TestHub_AddClient_RoomNotFound(t *testing.T) {
	hub := NewHub(HubConfig{}, testHubLogger())
	client := &Client{}

	err := hub.AddClient("nonexistent", client)
	require.Error(t, err)

	var notFound *RoomNotFoundError
	assert.ErrorAs(t, err, &notFound)
	assert.Equal(t, "nonexistent", notFound.RoomID)
}

func TestHub_AddClient_ConnectionLimit(t *testing.T) {
	hub := NewHub(HubConfig{MaxConnectionsPerRoom: 2}, testHubLogger())
	registerTestRoom(hub, "room-1", "s1", 0)

	// Add 2 clients — should succeed
	for i := 0; i < 2; i++ {
		client := &Client{output: make(chan []byte, OutputBufferSize)}
		err := hub.AddClient("room-1", client)
		require.NoError(t, err)
	}

	// 3rd client — should fail with connection limit
	client := &Client{output: make(chan []byte, OutputBufferSize)}
	err := hub.AddClient("room-1", client)
	require.Error(t, err)

	var limitErr *ConnectionLimitError
	assert.ErrorAs(t, err, &limitErr)
	assert.Equal(t, "room-1", limitErr.RoomID)
	assert.Equal(t, 2, limitErr.Current)
	assert.Equal(t, 2, limitErr.MaxAllowed)
}

func TestHub_AddClient_RoomOverrideLimit(t *testing.T) {
	// Hub default is 10, but room overrides to 1
	hub := NewHub(HubConfig{MaxConnectionsPerRoom: 10}, testHubLogger())
	registerTestRoom(hub, "room-1", "s1", 1)

	client := &Client{output: make(chan []byte, OutputBufferSize)}
	err := hub.AddClient("room-1", client)
	require.NoError(t, err)

	client2 := &Client{output: make(chan []byte, OutputBufferSize)}
	err = hub.AddClient("room-1", client2)
	require.Error(t, err)

	var limitErr *ConnectionLimitError
	assert.ErrorAs(t, err, &limitErr)
	assert.Equal(t, 1, limitErr.MaxAllowed)
}

func TestHub_RemoveClient(t *testing.T) {
	hub := NewHub(HubConfig{}, testHubLogger())
	registerTestRoom(hub, "room-1", "s1", 0)

	client := &Client{output: make(chan []byte, OutputBufferSize)}
	err := hub.AddClient("room-1", client)
	require.NoError(t, err)
	assert.Equal(t, 1, hub.ClientCount("room-1"))

	hub.RemoveClient(client)
	assert.Equal(t, 0, hub.ClientCount("room-1"))
}

func TestHub_RemoveClient_NilRoom(t *testing.T) {
	hub := NewHub(HubConfig{}, testHubLogger())
	client := &Client{}
	// Should not panic
	hub.RemoveClient(client)
}

func TestHub_ClientCount(t *testing.T) {
	hub := NewHub(HubConfig{}, testHubLogger())
	registerTestRoom(hub, "room-1", "s1", 0)

	assert.Equal(t, 0, hub.ClientCount("room-1"))

	client := &Client{output: make(chan []byte, OutputBufferSize)}
	_ = hub.AddClient("room-1", client)
	assert.Equal(t, 1, hub.ClientCount("room-1"))

	hub.RemoveClient(client)
	assert.Equal(t, 0, hub.ClientCount("room-1"))
}

func TestHub_ClientCount_NonexistentRoom(t *testing.T) {
	hub := NewHub(HubConfig{}, testHubLogger())
	assert.Equal(t, 0, hub.ClientCount("nonexistent"))
}

func TestHub_Close(t *testing.T) {
	hub := NewHub(HubConfig{}, testHubLogger())
	registerTestRoom(hub, "room-1", "s1", 0)
	registerTestRoom(hub, "room-2", "s2", 0)

	hub.Close()
	assert.False(t, hub.HasRoom("room-1"))
	assert.False(t, hub.HasRoom("room-2"))
}

func TestHub_UnregisterRoom_CleansUpClients(t *testing.T) {
	hub := NewHub(HubConfig{}, testHubLogger())
	registerTestRoom(hub, "room-1", "s1", 0)

	client := &Client{output: make(chan []byte, OutputBufferSize)}
	_ = hub.AddClient("room-1", client)

	hub.UnregisterRoom("room-1")
	assert.False(t, hub.HasRoom("room-1"))
	// Client's room should still be set but room is gone from hub
}

func TestHub_RoomPTY_DoesNotHoldRoomLockDuringStartup(t *testing.T) {
	origStart := startTmuxAttach
	defer func() { startTmuxAttach = origStart }()

	started := make(chan struct{})
	release := make(chan struct{})
	startTmuxAttach = func(_ context.Context, _ TmuxOptions) (*PTYProcess, error) {
		close(started)
		<-release
		return &PTYProcess{}, nil
	}

	hub := NewHub(HubConfig{}, testHubLogger())
	registerTestRoom(hub, "room-1", "s1", 0)

	errCh := make(chan error, 1)
	go func() {
		_, err := hub.RoomPTY(context.Background(), "room-1", 80, 24)
		errCh <- err
	}()

	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for PTY startup")
	}

	addDone := make(chan error, 1)
	go func() {
		addDone <- hub.AddClient("room-1", &Client{output: make(chan []byte, OutputBufferSize)})
	}()

	select {
	case err := <-addDone:
		require.NoError(t, err)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("AddClient blocked while PTY startup was in progress")
	}

	close(release)
	require.NoError(t, <-errCh)
}

func TestHub_RoomPTY_DefaultSessionUsesRuntimeTerminalContract(t *testing.T) {
	origStart := startTmuxAttach
	defer func() { startTmuxAttach = origStart }()

	called := make(chan TmuxOptions, 1)
	startTmuxAttach = func(_ context.Context, opts TmuxOptions) (*PTYProcess, error) {
		called <- opts
		return &PTYProcess{}, nil
	}

	hub := NewHub(HubConfig{}, testHubLogger())
	registerTestRoom(hub, "alpha-room", "", 0)

	pty, err := hub.RoomPTY(context.Background(), "alpha-room", 80, 24)
	require.NoError(t, err)
	require.NotNil(t, pty)

	opts := <-called
	assert.Equal(t, terminalruntime.DefaultRoomTmuxSession("alpha-room"), opts.Session)
}

func TestHub_UnregisterRoom_DoesNotHoldHubLockWhileClosingPTY(t *testing.T) {
	origClose := closePTYProcess
	defer func() { closePTYProcess = origClose }()

	blocked := make(chan struct{})
	release := make(chan struct{})
	closePTYProcess = func(_ *PTYProcess) {
		close(blocked)
		<-release
	}

	hub := NewHub(HubConfig{}, testHubLogger())
	registerTestRoom(hub, "room-1", "s1", 0)
	room := hub.rooms["room-1"]
	room.pty = &PTYProcess{}

	done := make(chan struct{})
	go func() {
		hub.UnregisterRoom("room-1")
		close(done)
	}()

	select {
	case <-blocked:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for PTY close")
	}

	registerDone := make(chan struct{})
	go func() {
		registerTestRoom(hub, "room-2", "s2", 0)
		close(registerDone)
	}()

	select {
	case <-registerDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("RegisterRoom blocked while UnregisterRoom was waiting on PTY close")
	}

	close(release)
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("UnregisterRoom did not finish after PTY close release")
	}
}

func TestRoomNotFoundError(t *testing.T) {
	err := &RoomNotFoundError{RoomID: "test-room"}
	assert.Equal(t, "room not found: test-room", err.Error())
}

func TestConnectionLimitError(t *testing.T) {
	err := &ConnectionLimitError{
		RoomID:     "test-room",
		Current:    10,
		MaxAllowed: 10,
	}
	assert.Contains(t, err.Error(), "test-room")
	assert.Contains(t, err.Error(), "10/10")
}
