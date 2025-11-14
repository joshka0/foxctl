package execution_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jkatigb/agentctl/internal/execution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecutorFunc_Implements verifies ExecutorFunc implements SkillExecutor.
func TestExecutorFunc_Implements(t *testing.T) {
	var _ execution.SkillExecutor = execution.ExecutorFunc(nil)
}

// TestExecutorFunc_Execute verifies the function adapter works correctly.
func TestExecutorFunc_Execute(t *testing.T) {
	called := false
	expectedResult := &execution.Result{
		Stdout:   []byte("output"),
		Stderr:   []byte(""),
		ExitCode: 0,
	}

	fn := execution.ExecutorFunc(func(ctx context.Context, opts execution.ExecuteOptions) (*execution.Result, error) {
		called = true
		assert.Equal(t, "test.yaml", opts.ManifestPath)
		assert.Equal(t, []byte("input"), opts.Input)
		return expectedResult, nil
	})

	result, err := fn.Execute(context.Background(), execution.ExecuteOptions{
		ManifestPath: "test.yaml",
		Input:        []byte("input"),
	})

	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, expectedResult, result)
}

// TestExecutorFunc_Error verifies error propagation.
func TestExecutorFunc_Error(t *testing.T) {
	expectedErr := errors.New("execution failed")

	fn := execution.ExecutorFunc(func(ctx context.Context, opts execution.ExecuteOptions) (*execution.Result, error) {
		return nil, expectedErr
	})

	result, err := fn.Execute(context.Background(), execution.ExecuteOptions{})

	assert.ErrorIs(t, err, expectedErr)
	assert.Nil(t, result)
}

// TestExecuteOptions_Fields verifies all fields are accessible.
func TestExecuteOptions_Fields(t *testing.T) {
	opts := execution.ExecuteOptions{
		ManifestPath:    "/path/to/manifest.yaml",
		ArtifactPath:    "/path/to/artifact",
		Input:           []byte(`{"key": "value"}`),
		MaxMemoryBytes:  1024 * 1024,
		MaxCPUSeconds:   30,
		AllowNetwork:    true,
		AllowFilesystem: false,
	}

	assert.Equal(t, "/path/to/manifest.yaml", opts.ManifestPath)
	assert.Equal(t, "/path/to/artifact", opts.ArtifactPath)
	assert.Equal(t, []byte(`{"key": "value"}`), opts.Input)
	assert.Equal(t, uint64(1024*1024), opts.MaxMemoryBytes)
	assert.Equal(t, uint64(30), opts.MaxCPUSeconds)
	assert.True(t, opts.AllowNetwork)
	assert.False(t, opts.AllowFilesystem)
}

// TestResult_Fields verifies all fields are accessible.
func TestResult_Fields(t *testing.T) {
	testErr := errors.New("test error")
	result := execution.Result{
		Stdout:   []byte("standard output"),
		Stderr:   []byte("standard error"),
		ExitCode: 1,
		Error:    testErr,
	}

	assert.Equal(t, []byte("standard output"), result.Stdout)
	assert.Equal(t, []byte("standard error"), result.Stderr)
	assert.Equal(t, 1, result.ExitCode)
	assert.ErrorIs(t, result.Error, testErr)
}
