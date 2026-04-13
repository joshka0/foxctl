package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompanionSkillRunnerAdapter_NilRunner(t *testing.T) {
	var adapter *companionSkillRunnerAdapter
	result, err := adapter.Run(context.Background(), "presence/orchestrate", map[string]any{"text": "hi"})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "not configured")
}

func TestToCompanionSkillRunResult(t *testing.T) {
	out := json.RawMessage(`{"ok":true}`)
	got, err := toCompanionSkillRunResult(&RunResult{
		Success: true,
		Output:  out,
		Error:   "",
	}, "presence/orchestrate")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, got.Success)
	assert.Equal(t, out, got.Output)
	assert.Empty(t, got.Error)
}

func TestToCompanionSkillRunResult_Nil(t *testing.T) {
	got, err := toCompanionSkillRunResult(nil, "presence/orchestrate")
	require.Error(t, err)
	assert.Nil(t, got)
	assert.True(t, strings.Contains(err.Error(), "nil result") || strings.Contains(err.Error(), "returned nil result"))
}
