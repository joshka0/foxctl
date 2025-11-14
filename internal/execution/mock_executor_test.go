package execution_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jkatigb/agentctl/internal/execution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMockExecutor_Implements verifies MockExecutor implements SkillExecutor.
func TestMockExecutor_Implements(t *testing.T) {
	t.Helper()
	var _ execution.SkillExecutor = &execution.MockExecutor{}
}

// TestNewMockExecutor verifies the constructor.
func TestNewMockExecutor(t *testing.T) {
	mock := execution.NewMockExecutor()
	assert.NotNil(t, mock)
	assert.Equal(t, 0, mock.CallCount())
}

// TestMockExecutor_DefaultBehavior tests default success response.
func TestMockExecutor_DefaultBehavior(t *testing.T) {
	mock := execution.NewMockExecutor()

	result, err := mock.Execute(context.Background(), execution.ExecuteOptions{
		ManifestPath: "test.yaml",
		Input:        []byte(`{"test": true}`),
	})

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, []byte(`{"ok": true}`), result.Stdout)
	assert.Equal(t, []byte{}, result.Stderr)
	assert.Equal(t, 0, result.ExitCode)
	assert.Nil(t, result.Error)
}

// TestMockExecutor_CustomBehavior tests injected ExecuteFunc.
func TestMockExecutor_CustomBehavior(t *testing.T) {
	mock := execution.NewMockExecutor()
	customResult := &execution.Result{
		Stdout:   []byte("custom output"),
		Stderr:   []byte("custom error"),
		ExitCode: 42,
	}

	mock.ExecuteFunc = func(_ context.Context, opts execution.ExecuteOptions) (*execution.Result, error) {
		assert.Equal(t, "manifest.yaml", opts.ManifestPath)
		return customResult, nil
	}

	result, err := mock.Execute(context.Background(), execution.ExecuteOptions{
		ManifestPath: "manifest.yaml",
	})

	require.NoError(t, err)
	assert.Equal(t, customResult, result)
}

// TestMockExecutor_ErrorBehavior tests error injection.
func TestMockExecutor_ErrorBehavior(t *testing.T) {
	mock := execution.NewMockExecutor()
	expectedErr := errors.New("execution failed")

	mock.ExecuteFunc = func(_ context.Context, _ execution.ExecuteOptions) (*execution.Result, error) {
		return &execution.Result{
			Stderr:   []byte("error message"),
			ExitCode: 1,
			Error:    expectedErr,
		}, expectedErr
	}

	result, err := mock.Execute(context.Background(), execution.ExecuteOptions{})

	assert.ErrorIs(t, err, expectedErr)
	assert.Equal(t, 1, result.ExitCode)
	assert.Equal(t, []byte("error message"), result.Stderr)
}

// TestMockExecutor_CallTracking tests call recording.
func TestMockExecutor_CallTracking(t *testing.T) {
	mock := execution.NewMockExecutor()

	// First call
	_, _ = mock.Execute(context.Background(), execution.ExecuteOptions{
		ManifestPath: "first.yaml",
		Input:        []byte("first"),
	})

	assert.Equal(t, 1, mock.CallCount())

	// Second call
	_, _ = mock.Execute(context.Background(), execution.ExecuteOptions{
		ManifestPath: "second.yaml",
		Input:        []byte("second"),
	})

	assert.Equal(t, 2, mock.CallCount())

	// Verify all calls recorded
	require.Len(t, mock.Calls, 2)
	assert.Equal(t, "first.yaml", mock.Calls[0].ManifestPath)
	assert.Equal(t, []byte("first"), mock.Calls[0].Input)
	assert.Equal(t, "second.yaml", mock.Calls[1].ManifestPath)
	assert.Equal(t, []byte("second"), mock.Calls[1].Input)
}

// TestMockExecutor_LastCall tests LastCall helper.
func TestMockExecutor_LastCall(t *testing.T) {
	mock := execution.NewMockExecutor()

	// No calls yet
	assert.Nil(t, mock.LastCall())

	// Make a call
	_, _ = mock.Execute(context.Background(), execution.ExecuteOptions{
		ManifestPath: "test.yaml",
		ArtifactPath: "/path/to/artifact",
	})

	lastCall := mock.LastCall()
	require.NotNil(t, lastCall)
	assert.Equal(t, "test.yaml", lastCall.ManifestPath)
	assert.Equal(t, "/path/to/artifact", lastCall.ArtifactPath)
}

// TestMockExecutor_Reset tests Reset functionality.
func TestMockExecutor_Reset(t *testing.T) {
	mock := execution.NewMockExecutor()

	// Make some calls
	_, _ = mock.Execute(context.Background(), execution.ExecuteOptions{})
	_, _ = mock.Execute(context.Background(), execution.ExecuteOptions{})

	assert.Equal(t, 2, mock.CallCount())

	// Reset
	mock.Reset()

	assert.Equal(t, 0, mock.CallCount())
	assert.Len(t, mock.Calls, 0)
	assert.Nil(t, mock.LastCall())
}

// TestMockExecutor_ConcurrentCalls tests thread safety.
func TestMockExecutor_ConcurrentCalls(t *testing.T) {
	mock := execution.NewMockExecutor()
	const numGoroutines = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			_, _ = mock.Execute(context.Background(), execution.ExecuteOptions{})
		}()
	}

	wg.Wait()

	assert.Equal(t, numGoroutines, mock.CallCount())
	assert.Len(t, mock.Calls, numGoroutines)
}
