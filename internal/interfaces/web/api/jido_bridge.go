package api

import (
	"errors"
	"os"
	"strings"

	v2jido "github.com/joshka0/foxctl/internal/v2/adapters/jido"
)

const defaultJidoSocketPath = "/tmp/foxctl-jido.sock"

// loadOptionalJidoClient returns a configured client when the bridge socket is
// present. When the socket file is missing, it reports "not available" without
// treating that as an application error.
func loadOptionalJidoClient() (v2jido.Client, bool, error) {
	socketPath := strings.TrimSpace(os.Getenv(v2jido.EnvJidoSocketPath))
	if socketPath == "" {
		socketPath = defaultJidoSocketPath
	}
	if _, err := os.Stat(socketPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}

	client, err := v2jido.NewEnvJSONRPCClient()
	if err != nil {
		return nil, false, err
	}
	return client, true, nil
}
