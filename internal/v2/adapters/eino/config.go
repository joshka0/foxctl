package eino

import (
	"os"
	"strings"
)

const (
	// EnvEngineBackend is the environment variable that opts into the Eino engine path.
	// Set to "eino" to activate the EinoEngineAdapter; any other value (including unset)
	// keeps the default LLMChatEngine path from Milestone 1.
	EnvEngineBackend = "AGENTCTL_ENGINE_BACKEND"

	backendEino = "eino"
)

// IsEinoEnabled reports whether the Eino engine adapter is explicitly requested.
// The default is false — the LLMChatEngine path is used unless the caller has
// set AGENTCTL_ENGINE_BACKEND=eino.
func IsEinoEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(EnvEngineBackend)), backendEino)
}
