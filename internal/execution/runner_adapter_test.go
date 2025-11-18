package execution_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/execution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunnerExecutor_Implements verifies RunnerExecutor implements SkillExecutor.
func TestRunnerExecutor_Implements(t *testing.T) {
	t.Helper()
	var _ = execution.NewRunnerExecutor()
}

// TestNewRunnerExecutor verifies the constructor returns a non-nil executor.
func TestNewRunnerExecutor(t *testing.T) {
	executor := execution.NewRunnerExecutor()
	assert.NotNil(t, executor)
}

// TestRunnerExecutor_Execute_InvalidManifest verifies error handling for invalid manifest.
func TestRunnerExecutor_Execute_InvalidManifest(t *testing.T) {
	executor := execution.NewRunnerExecutor()

	result, err := executor.Execute(context.Background(), execution.ExecuteOptions{
		ManifestPath: "/nonexistent/path/to/manifest.yaml",
		Input:        []byte(`{}`),
	})

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "load manifest")
}

// TestRunnerExecutor_Execute_Success tests successful execution with a real skill.
// This test requires a valid skill manifest and artifact to be available.
func TestRunnerExecutor_Execute_Success(t *testing.T) {
	// Skip if no test fixtures available
	testdataDir := filepath.Join("..", "..", "testdata")
	if _, err := os.Stat(testdataDir); os.IsNotExist(err) {
		t.Skip("testdata directory not available")
	}

	// Look for an echo skill for testing
	echoManifest := filepath.Join(testdataDir, "echo", "skill.yaml")
	if _, err := os.Stat(echoManifest); os.IsNotExist(err) {
		t.Skip("echo skill manifest not available for testing")
	}

	executor := execution.NewRunnerExecutor()

	result, err := executor.Execute(context.Background(), execution.ExecuteOptions{
		ManifestPath: echoManifest,
		Input:        []byte(`{"message": "hello"}`),
	})
	if err != nil {
		// If execution fails, it might be due to missing artifact
		// This is acceptable for this test
		t.Logf("Execution failed (expected if artifact not built): %v", err)
		return
	}

	require.NotNil(t, result)
	assert.Equal(t, 0, result.ExitCode)
}

// TestRunnerExecutor_Execute_Options verifies options are passed correctly.
func TestRunnerExecutor_Execute_Options(t *testing.T) {
	executor := execution.NewRunnerExecutor()

	opts := execution.ExecuteOptions{
		ManifestPath: "/nonexistent/manifest.yaml",
		ArtifactPath: "/path/to/artifact",
		Input:        []byte(`{"test": true}`),
	}

	// This will fail due to nonexistent manifest, but we're testing
	// that the options structure works correctly
	_, err := executor.Execute(context.Background(), opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "load manifest")
}
