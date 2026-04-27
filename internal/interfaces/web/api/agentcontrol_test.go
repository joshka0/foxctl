package api

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/joshka0/foxctl/internal/runtime/daemon"
)

// mockAgentControl is a test double that records calls and returns canned responses.
type mockAgentControl struct {
	ensureRunningCalled bool
	ensureRunningErr    error

	isRunningResult bool

	spawnCalled bool
	spawnParams daemon.AgentSpawnParams
	spawnResult *daemon.AgentSpawnResult
	spawnErr    error

	listCalled bool
	listResult *daemon.AgentListResult
	listErr    error

	killCalled  bool
	killSession string
	killResult  *daemon.AgentKillResult
	killErr     error
}

func (m *mockAgentControl) EnsureRunning() error {
	m.ensureRunningCalled = true
	return m.ensureRunningErr
}

func (m *mockAgentControl) IsRunning() bool {
	return m.isRunningResult
}

func (m *mockAgentControl) Spawn(params daemon.AgentSpawnParams) (*daemon.AgentSpawnResult, error) {
	m.spawnCalled = true
	m.spawnParams = params
	return m.spawnResult, m.spawnErr
}

func (m *mockAgentControl) List() (*daemon.AgentListResult, error) {
	m.listCalled = true
	return m.listResult, m.listErr
}

func (m *mockAgentControl) Kill(sessionID string) (*daemon.AgentKillResult, error) {
	m.killCalled = true
	m.killSession = sessionID
	return m.killResult, m.killErr
}

func TestNewLocalDaemonControl(t *testing.T) {
	ctrl := NewLocalDaemonControl()
	assert.NotNil(t, ctrl, "NewLocalDaemonControl should return a non-nil AgentControl")
}

func TestAgentControl(t *testing.T) {
	// Save and restore the default
	original := defaultAgentControl
	defer func() { defaultAgentControl = original }()

	mock := &mockAgentControl{}
	SetAgentControl(mock)

	got := agentControl()
	assert.Equal(t, mock, got, "agentControl should return the mock after SetAgentControl")
}

func TestSetAgentControl_NilIgnored(t *testing.T) {
	original := defaultAgentControl
	defer func() { defaultAgentControl = original }()

	SetAgentControl(nil)
	assert.Equal(t, original, defaultAgentControl, "SetAgentControl(nil) should not change the default")
}

func TestMockAgentControl_ImplementsInterface(t *testing.T) {
	// Verify the mock satisfies the interface at compile time
	var _ AgentControl = (*mockAgentControl)(nil)
}

func TestMockAgentControl_EnsureRunning(t *testing.T) {
	mock := &mockAgentControl{ensureRunningErr: nil}
	err := mock.EnsureRunning()
	assert.NoError(t, err)
	assert.True(t, mock.ensureRunningCalled)
}

func TestMockAgentControl_IsRunning(t *testing.T) {
	mock := &mockAgentControl{isRunningResult: true}
	assert.True(t, mock.IsRunning())
}

func TestMockAgentControl_Spawn(t *testing.T) {
	want := &daemon.AgentSpawnResult{SessionID: "sess-1", AgentID: "agent-1"}
	mock := &mockAgentControl{spawnResult: want}

	params := daemon.AgentSpawnParams{Role: "coder"}
	result, err := mock.Spawn(params)

	assert.NoError(t, err)
	assert.Equal(t, want, result)
	assert.Equal(t, params, mock.spawnParams)
	assert.True(t, mock.spawnCalled)
}

func TestMockAgentControl_List(t *testing.T) {
	want := &daemon.AgentListResult{Sessions: []daemon.AgentSessionInfo{{SessionID: "s1"}}}
	mock := &mockAgentControl{listResult: want}

	result, err := mock.List()
	assert.NoError(t, err)
	assert.Equal(t, want, result)
	assert.True(t, mock.listCalled)
}

func TestMockAgentControl_Kill(t *testing.T) {
	want := &daemon.AgentKillResult{Status: "killed"}
	mock := &mockAgentControl{killResult: want}

	result, err := mock.Kill("sess-1")
	assert.NoError(t, err)
	assert.Equal(t, want, result)
	assert.Equal(t, "sess-1", mock.killSession)
	assert.True(t, mock.killCalled)
}
