package smolvm

import (
	"context"
	"errors"
	"fmt"
)

var ErrCommandSequenceFailed = errors.New("smolvm: command sequence failed")

// CommandStepResult captures one executed or skipped step in a command
// sequence.
type CommandStepResult struct {
	Name     string        `json:"name"`
	Optional bool          `json:"optional,omitempty"`
	Command  CommandPlan   `json:"command"`
	Result   CommandResult `json:"result,omitempty"`
	Error    string        `json:"error,omitempty"`
	Skipped  bool          `json:"skipped,omitempty"`
}

// CommandSequenceResult captures the complete execution trace for an ordered
// command sequence.
type CommandSequenceResult struct {
	Steps   []CommandStepResult `json:"steps"`
	Success bool                `json:"success"`
}

// RunCommandSequence executes planned steps in order. A non-optional step with
// a runner error or non-zero exit marks the sequence failed. Remaining
// non-optional steps are skipped, but later optional steps still run so cleanup
// commands can be appended to a plan.
func RunCommandSequence(ctx context.Context, runner CommandRunner, steps []CommandStepPlan) (CommandSequenceResult, error) {
	if runner == nil {
		return CommandSequenceResult{}, errors.New("smolvm: command runner is required")
	}

	result := CommandSequenceResult{
		Steps:   make([]CommandStepResult, 0, len(steps)),
		Success: true,
	}

	failed := false
	var failure error
	for _, step := range steps {
		stepResult := CommandStepResult{
			Name:     step.Name,
			Optional: step.Optional,
			Command:  step.Command,
		}

		if failed && !step.Optional {
			stepResult.Skipped = true
			result.Steps = append(result.Steps, stepResult)
			continue
		}

		commandResult, err := runner.Run(ctx, step.Command)
		stepResult.Result = commandResult
		if err != nil {
			stepResult.Error = err.Error()
		} else if commandResult.ExitCode != 0 {
			stepResult.Error = fmt.Sprintf("exit code %d", commandResult.ExitCode)
		}

		if stepResult.Error != "" && !step.Optional {
			result.Success = false
			failed = true
			if failure == nil {
				failure = fmt.Errorf("%w: step %q: %s", ErrCommandSequenceFailed, step.Name, stepResult.Error)
			}
		}
		result.Steps = append(result.Steps, stepResult)
	}

	if failure != nil {
		return result, failure
	}
	return result, nil
}
