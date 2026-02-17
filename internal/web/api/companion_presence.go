package api

import (
	"context"
	"fmt"

	"github.com/jkatigb/agentctl/internal/companion"
)

// companionSkillRunnerAdapter bridges api.SkillRunner to companion.SkillRunner.
type companionSkillRunnerAdapter struct {
	inner *SkillRunner
}

func (a *companionSkillRunnerAdapter) Run(ctx context.Context, skillName string, input map[string]any) (*companion.SkillRunResult, error) {
	if a == nil || a.inner == nil {
		return nil, fmt.Errorf("presence skill runner is not configured")
	}

	result, err := a.inner.Run(ctx, skillName, input)
	if err != nil {
		return nil, err
	}
	return toCompanionSkillRunResult(result, skillName)
}

func toCompanionSkillRunResult(result *RunResult, skillName string) (*companion.SkillRunResult, error) {
	if result == nil {
		return nil, fmt.Errorf("presence skill runner returned nil result for %s", skillName)
	}
	return &companion.SkillRunResult{
		Success: result.Success,
		Output:  result.Output,
		Error:   result.Error,
	}, nil
}
