package webterm

import (
	"testing"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTerminalTarget struct {
	inputs  [][]byte
	resizes [][2]uint16
}

func (f *fakeTerminalTarget) WriteInput(data []byte) error {
	cp := append([]byte(nil), data...)
	f.inputs = append(f.inputs, cp)
	return nil
}

func (f *fakeTerminalTarget) Resize(cols, rows uint16) error {
	f.resizes = append(f.resizes, [2]uint16{cols, rows})
	return nil
}

func TestHandleClientMessage_BinaryInput(t *testing.T) {
	pty := &fakeTerminalTarget{}

	result, err := handleClientMessage(websocket.MessageBinary, []byte("echo hello\n"), pty)

	require.NoError(t, err)
	assert.Nil(t, result.controlError)
	require.Len(t, pty.inputs, 1)
	assert.Equal(t, []byte("echo hello\n"), pty.inputs[0])
	assert.Empty(t, pty.resizes)
}

func TestHandleClientMessage_JSONLookingShellInputIsBinary(t *testing.T) {
	pty := &fakeTerminalTarget{}
	input := []byte(`{"type":"resize","cols":999,"rows":999}`)

	result, err := handleClientMessage(websocket.MessageBinary, input, pty)

	require.NoError(t, err)
	assert.Nil(t, result.controlError)
	require.Len(t, pty.inputs, 1)
	assert.Equal(t, input, pty.inputs[0])
	assert.Empty(t, pty.resizes)
}

func TestHandleClientMessage_TextResizeControl(t *testing.T) {
	pty := &fakeTerminalTarget{}

	result, err := handleClientMessage(websocket.MessageText, []byte(`{"type":"resize","cols":120,"rows":40}`), pty)

	require.NoError(t, err)
	assert.Nil(t, result.controlError)
	assert.True(t, result.resized)
	require.Len(t, pty.resizes, 1)
	assert.Equal(t, [2]uint16{120, 40}, pty.resizes[0])
	assert.Empty(t, pty.inputs)
}

func TestHandleClientMessage_TextControlErrorsDoNotWriteToPTY(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		wantCode string
	}{
		{"plain terminal text", "echo hello\n", "EINVAL"},
		{"unknown control", `{"type":"lease_requested","mode":"write"}`, "EUNKNOWN"},
		{"missing type", `{"cols":80,"rows":24}`, "EINVAL"},
		{"zero cols", `{"type":"resize","cols":0,"rows":24}`, "EINVAL"},
		{"zero rows", `{"type":"resize","cols":80,"rows":0}`, "EINVAL"},
		{"negative cols", `{"type":"resize","cols":-1,"rows":24}`, "EINVAL"},
		{"negative rows", `{"type":"resize","cols":80,"rows":-1}`, "EINVAL"},
		{"excessive cols", `{"type":"resize","cols":1001,"rows":24}`, "EINVAL"},
		{"excessive rows", `{"type":"resize","cols":80,"rows":1001}`, "EINVAL"},
		{"malformed cols", `{"type":"resize","cols":"80","rows":24}`, "EINVAL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pty := &fakeTerminalTarget{}

			result, err := handleClientMessage(websocket.MessageText, []byte(tt.payload), pty)

			require.NoError(t, err)
			require.NotNil(t, result.controlError)
			assert.Equal(t, "error", result.controlError.Type)
			assert.Equal(t, tt.wantCode, result.controlError.Code)
			assert.Empty(t, pty.inputs)
			assert.Empty(t, pty.resizes)
		})
	}
}
