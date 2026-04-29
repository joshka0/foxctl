package runner

import (
	"context"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/rlm"
	"github.com/joshka0/foxctl/internal/rlm/repl"
	rlmruntime "github.com/joshka0/foxctl/internal/rlm/runtime"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

// RLMRunner is the minimal execution contract used by the v2 rlm_repl adapter.
type RLMRunner interface {
	Run(ctx context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error)
}

// RLMREPLRunnerFactory builds per-turn RLM REPL runners.
type RLMREPLRunnerFactory interface {
	New(cfg run.RLMREPLConfig) (RLMRunner, error)
}

type defaultRLMREPLRunnerFactory struct {
	runnerBuilder func(cfg rlmruntime.REPLRunnerConfig) RLMRunner
}

func (f defaultRLMREPLRunnerFactory) New(cfg run.RLMREPLConfig) (RLMRunner, error) {
	runnerCfg := mapRLMREPLConfig(cfg)
	runnerCfg.RLMQueryFactory = recursiveRLMQueryFactory(&runnerCfg, f.newRunner)
	applyRLMRecursiveMode(&runnerCfg)
	return f.newRunner(runnerCfg), nil
}

func (f defaultRLMREPLRunnerFactory) newRunner(cfg rlmruntime.REPLRunnerConfig) RLMRunner {
	if f.runnerBuilder != nil {
		return f.runnerBuilder(cfg)
	}
	return &rlmruntime.REPLRunner{Config: cfg}
}

func recursiveRLMQueryFactory(
	cfg *rlmruntime.REPLRunnerConfig,
	newRunner func(cfg rlmruntime.REPLRunnerConfig) RLMRunner,
) func(parentTask rlm.Task, env rlm.Environment) rlmruntime.RLMQueryRunFunc {
	return func(_ rlm.Task, _ rlm.Environment) rlmruntime.RLMQueryRunFunc {
		return func(ctx context.Context, childTask rlm.Task, childEnv rlm.Environment) (rlm.Result, error) {
			childCfg := childREPLRunnerConfig(*cfg, childTask)
			childCfg.RLMQueryFactory = recursiveRLMQueryFactory(&childCfg, newRunner)
			applyRLMRecursiveMode(&childCfg)
			return newRunner(childCfg).Run(ctx, childTask, childEnv)
		}
	}
}

func childREPLRunnerConfig(parent rlmruntime.REPLRunnerConfig, childTask rlm.Task) rlmruntime.REPLRunnerConfig {
	child := parent
	child.LLM.RequireToolUse = false
	child.RequiredSubcallRules = nil
	if childTask.MaxDepth > 0 {
		child.Budget.MaxDepth = childTask.MaxDepth
	}
	if childTask.MaxSubcalls >= 0 {
		child.Budget.MaxSubcalls = childTask.MaxSubcalls
	}
	if childTask.MaxIterations > 0 {
		child.Budget.MaxIterations = childTask.MaxIterations
		child.LLM.MaxIterations = childTask.MaxIterations
	}
	return child
}

func recursiveSubcallsEnabled(budget rlmruntime.BudgetConfig) bool {
	return budget.MaxDepth > 0 && budget.MaxSubcalls > 0
}

func applyRLMRecursiveMode(cfg *rlmruntime.REPLRunnerConfig) {
	if cfg == nil {
		return
	}
	cfg.AsyncRecursion = recursiveSubcallsEnabled(cfg.Budget)
}

func mapRLMREPLConfig(cfg run.RLMREPLConfig) rlmruntime.REPLRunnerConfig {
	return rlmruntime.REPLRunnerConfig{
		LLM: rlm.LLMConfig{
			Provider:       strings.TrimSpace(cfg.LLM.Provider),
			APIKey:         strings.TrimSpace(cfg.LLM.APIKey),
			BaseURL:        strings.TrimSpace(cfg.LLM.BaseURL),
			AuthMode:       strings.TrimSpace(cfg.LLM.AuthMode),
			AuthHeader:     strings.TrimSpace(cfg.LLM.AuthHeader),
			AuthPrefix:     strings.TrimSpace(cfg.LLM.AuthPrefix),
			Model:          strings.TrimSpace(cfg.LLM.Model),
			Timeout:        millisecondsToDuration(cfg.LLM.TimeoutMS),
			MaxTokens:      cfg.LLM.MaxTokens,
			Temperature:    cfg.LLM.Temperature,
			MaxIterations:  cfg.LLM.MaxIterations,
			RequireToolUse: cfg.LLM.RequireToolUse,
		},
		Budget: rlmruntime.BudgetConfig{
			MaxDepth:        cfg.Budget.MaxDepth,
			MaxSubcalls:     cfg.Budget.MaxSubcalls,
			MaxREPLCalls:    cfg.Budget.MaxREPLCalls,
			MaxIterations:   cfg.Budget.MaxIterations,
			MaxParentTokens: cfg.Budget.MaxParentTokens,
			MaxChildTokens:  cfg.Budget.MaxChildTokens,
			MaxDuration:     millisecondsToDuration(cfg.Budget.MaxDurationMS),
		},
		Sandbox: rlmruntime.SandboxConfig{
			Kind: rlmruntime.SandboxKind(strings.TrimSpace(cfg.Sandbox.Kind)),
			Python: repl.Options{
				PythonPath:     firstNonEmptyString(strings.TrimSpace(cfg.Sandbox.Python.PythonPath), strings.TrimSpace(cfg.Python.PythonPath)),
				MaxOutputBytes: firstPositiveInt(cfg.Sandbox.Python.MaxOutputBytes, cfg.Python.MaxOutputBytes),
			},
			Yaegi: repl.YaegiOptions{
				MaxOutputBytes: cfg.Sandbox.Yaegi.MaxOutputBytes,
			},
		},
		Python: repl.Options{
			PythonPath:     strings.TrimSpace(cfg.Python.PythonPath),
			MaxOutputBytes: cfg.Python.MaxOutputBytes,
		},
		SystemPrompt: strings.TrimSpace(cfg.SystemPrompt),
		Telemetry: rlmruntime.ObservabilityTelemetrySink{
			WorkspaceID: strings.TrimSpace(cfg.WorkspaceID),
			Command:     "v2.run.rlm_repl",
		},
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func taskFromTurnInput(in run.TurnInput) rlm.Task {
	maxIterations := in.RLM.Budget.MaxIterations
	if maxIterations <= 0 {
		maxIterations = in.MaxIterations
	}
	return rlm.Task{
		Prompt:        in.Prompt,
		RunID:         strings.TrimSpace(in.RunID),
		AgentID:       strings.TrimSpace(in.ActorID),
		OutputRoot:    strings.TrimSpace(in.RLM.OutputRoot),
		WorkspaceID:   strings.TrimSpace(in.RLM.WorkspaceID),
		WorkspaceRoot: strings.TrimSpace(in.RLM.WorkspaceRoot),
		MaxDepth:      in.RLM.Budget.MaxDepth,
		MaxIterations: maxIterations,
		MaxSubcalls:   in.RLM.Budget.MaxSubcalls,
	}
}

func millisecondsToDuration(raw int) time.Duration {
	if raw <= 0 {
		return 0
	}
	return time.Duration(raw) * time.Millisecond
}

func rlmResultToolCalls(result rlm.Result) int {
	if count, ok := readInt(result.Metadata["tool_calls"]); ok && count >= 0 {
		return count
	}
	if result.Subcalls > 0 {
		return result.Subcalls
	}
	return 0
}

func cloneMetadata(raw map[string]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(raw))
	for key, value := range raw {
		cloned[key] = value
	}
	return cloned
}

func readInt(raw any) (int, bool) {
	switch v := raw.(type) {
	case int:
		return v, true
	case int8:
		return int(v), true
	case int16:
		return int(v), true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case uint:
		return int(v), true
	case uint8:
		return int(v), true
	case uint16:
		return int(v), true
	case uint32:
		return int(v), true
	case uint64:
		return int(v), true
	case float32:
		return int(v), true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}
