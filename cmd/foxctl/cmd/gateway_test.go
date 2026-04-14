package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayCommand_Registered(t *testing.T) {
	// Verify gateway command is registered with root
	cmd, _, err := rootCmd.Find([]string{"gateway"})
	assert.NoError(t, err)
	assert.NotNil(t, cmd)
	assert.Equal(t, "gateway", cmd.Name())
}

func TestGatewayCommand_Flags(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"gateway"})
	assert.NoError(t, err)
	require.NotNil(t, cmd)

	// Check all expected flags exist
	flags := cmd.Flags()

	assert.NotNil(t, flags.Lookup("dev"))
	assert.NotNil(t, flags.Lookup("port"))
	assert.NotNil(t, flags.Lookup("state-dir"))
	assert.NotNil(t, flags.Lookup("ts-authkey"))
	assert.NotNil(t, flags.Lookup("hostname"))
}

func TestGatewayCommand_FlagDefaults(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"gateway"})
	assert.NoError(t, err)
	require.NotNil(t, cmd)

	flags := cmd.Flags()

	// Dev should default to false
	dev, err := flags.GetBool("dev")
	assert.NoError(t, err)
	assert.False(t, dev)

	// Port should default to 8765
	port, err := flags.GetInt("port")
	assert.NoError(t, err)
	assert.Equal(t, 8765, port)

	// Hostname should default to foxctl-gateway
	hostname, err := flags.GetString("hostname")
	assert.NoError(t, err)
	assert.Equal(t, "foxctl-gateway", hostname)

	// Auth key should default to empty
	authKey, err := flags.GetString("ts-authkey")
	assert.NoError(t, err)
	assert.Equal(t, "", authKey)
}
