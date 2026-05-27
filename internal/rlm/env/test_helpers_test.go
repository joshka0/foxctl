package env

import "testing"

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func skipShortRLMIntegration(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("full RLM adapter pipeline coverage runs in test-integration-rlm")
	}
}
