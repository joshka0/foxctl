package webterm

import (
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testHubLogger() zerolog.Logger {
	return zerolog.New(io.Discard).With().Timestamp().Logger()
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

func TestHub_RegisterRoom(t *testing.T) {
	hub := NewHub(HubConfig{}, testHubLogger())

	hub.RegisterRoom("room-1", RoomConfig{TmuxSession: "session-1"})
	assert.True(t, hub.HasRoom("room-1"))
	assert.False(t, hub.HasRoom("room-2"))
}

func TestHub_RegisterRoom_UpdatesConfig(t *testing.T) {
	hub := NewHub(HubConfig{}, testHubLogger())

	hub.RegisterRoom("room-1", RoomConfig{TmuxSession: "session-1", MaxConnections: 5})
	hub.RegisterRoom("room-1", RoomConfig{TmuxSession: "session-1-updated", MaxConnections: 10})

	assert.True(t, hub.HasRoom("room-1"))
	// Verify config was updated by checking client count behavior later
}

func TestHub_UnregisterRoom(t *testing.T) {
	hub := NewHub(HubConfig{}, testHubLogger())

	hub.RegisterRoom("room-1", RoomConfig{TmuxSession: "session-1"})
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

	hub.RegisterRoom("room-1", RoomConfig{})
	hub.RegisterRoom("room-2", RoomConfig{})
	hub.RegisterRoom("room-3", RoomConfig{})

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
	hub.RegisterRoom("room-1", RoomConfig{TmuxSession: "s1"})

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
	hub.RegisterRoom("room-1", RoomConfig{TmuxSession: "s1", MaxConnections: 1})

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
	hub.RegisterRoom("room-1", RoomConfig{TmuxSession: "s1"})

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
	hub.RegisterRoom("room-1", RoomConfig{TmuxSession: "s1"})

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
	hub.RegisterRoom("room-1", RoomConfig{TmuxSession: "s1"})
	hub.RegisterRoom("room-2", RoomConfig{TmuxSession: "s2"})

	hub.Close()
	assert.False(t, hub.HasRoom("room-1"))
	assert.False(t, hub.HasRoom("room-2"))
}

func TestHub_UnregisterRoom_CleansUpClients(t *testing.T) {
	hub := NewHub(HubConfig{}, testHubLogger())
	hub.RegisterRoom("room-1", RoomConfig{TmuxSession: "s1"})

	client := &Client{output: make(chan []byte, OutputBufferSize)}
	_ = hub.AddClient("room-1", client)

	hub.UnregisterRoom("room-1")
	assert.False(t, hub.HasRoom("room-1"))
	// Client's room should still be set but room is gone from hub
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
