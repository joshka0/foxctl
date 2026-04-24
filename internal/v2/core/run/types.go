package run

import (
	"encoding/json"
	"strings"
)

// ToolCall is a model-requested tool invocation inside an iteration.
type ToolCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

// TurnBackend identifies the canonical v2 run backend.
type TurnBackend string

const (
	// TurnBackendLLMChat uses the default model+tool iteration stage.
	TurnBackendLLMChat TurnBackend = "llm_chat"
	// TurnBackendRLMREPL uses the RLM REPL-backed runner adapter.
	TurnBackendRLMREPL TurnBackend = "rlm_repl"
)

// NormalizeTurnBackend canonicalizes backend labels and applies the default.
func NormalizeTurnBackend(raw TurnBackend) TurnBackend {
	switch strings.ToLower(strings.TrimSpace(string(raw))) {
	case "", string(TurnBackendLLMChat):
		return TurnBackendLLMChat
	case string(TurnBackendRLMREPL):
		return TurnBackendRLMREPL
	default:
		return TurnBackend(strings.ToLower(strings.TrimSpace(string(raw))))
	}
}

// IsSupportedTurnBackend reports whether the backend is known by the runner.
func IsSupportedTurnBackend(raw TurnBackend) bool {
	switch NormalizeTurnBackend(raw) {
	case TurnBackendLLMChat, TurnBackendRLMREPL:
		return true
	default:
		return false
	}
}

// RLMREPLLLMConfig configures the parent LLM for the RLM REPL adapter.
type RLMREPLLLMConfig struct {
	Provider       string  `json:"provider,omitempty"`
	APIKey         string  `json:"api_key,omitempty"`
	BaseURL        string  `json:"base_url,omitempty"`
	AuthMode       string  `json:"auth_mode,omitempty"`
	AuthHeader     string  `json:"auth_header,omitempty"`
	AuthPrefix     string  `json:"auth_prefix,omitempty"`
	Model          string  `json:"model,omitempty"`
	TimeoutMS      int     `json:"timeout_ms,omitempty"`
	MaxTokens      int     `json:"max_tokens,omitempty"`
	Temperature    float64 `json:"temperature,omitempty"`
	MaxIterations  int     `json:"max_iterations,omitempty"`
	RequireToolUse bool    `json:"require_tool_use,omitempty"`
}

// RLMREPLBudgetConfig configures runtime budgets for the RLM REPL adapter.
type RLMREPLBudgetConfig struct {
	MaxDepth        int `json:"max_depth,omitempty"`
	MaxSubcalls     int `json:"max_subcalls,omitempty"`
	MaxREPLCalls    int `json:"max_repl_calls,omitempty"`
	MaxIterations   int `json:"max_iterations,omitempty"`
	MaxParentTokens int `json:"max_parent_tokens,omitempty"`
	MaxChildTokens  int `json:"max_child_tokens,omitempty"`
	MaxDurationMS   int `json:"max_duration_ms,omitempty"`
}

// RLMREPLPythonConfig configures the python sandbox for the RLM REPL adapter.
type RLMREPLPythonConfig struct {
	PythonPath     string `json:"python_path,omitempty"`
	MaxOutputBytes int    `json:"max_output_bytes,omitempty"`
}

// RLMREPLYaegiConfig configures the in-process Go/Yaegi sandbox.
type RLMREPLYaegiConfig struct {
	MaxOutputBytes int `json:"max_output_bytes,omitempty"`
}

// RLMREPLSandboxConfig selects the scratch REPL sandbox backend.
type RLMREPLSandboxConfig struct {
	Kind   string              `json:"kind,omitempty"`
	Python RLMREPLPythonConfig `json:"python,omitempty"`
	Yaegi  RLMREPLYaegiConfig  `json:"yaegi,omitempty"`
}

// RLMREPLConfig configures the v2 `rlm_repl` backend adapter.
type RLMREPLConfig struct {
	LLM           RLMREPLLLMConfig     `json:"llm,omitempty"`
	Budget        RLMREPLBudgetConfig  `json:"budget,omitempty"`
	Sandbox       RLMREPLSandboxConfig `json:"sandbox,omitempty"`
	Python        RLMREPLPythonConfig  `json:"python,omitempty"`
	SystemPrompt  string               `json:"system_prompt,omitempty"`
	WorkspaceID   string               `json:"workspace_id,omitempty"`
	WorkspaceRoot string               `json:"workspace_root,omitempty"`
	OutputRoot    string               `json:"output_root,omitempty"`
}

// TurnInput is the canonical request-scoped input passed into the v2 runner.
type TurnInput struct {
	RunID         string        `json:"run_id"`
	TurnID        string        `json:"turn_id,omitempty"`
	Command       string        `json:"command,omitempty"`
	Mode          string        `json:"mode,omitempty"`
	Backend       TurnBackend   `json:"backend,omitempty"`
	Prompt        string        `json:"prompt,omitempty"`
	ActorID       string        `json:"actor_id,omitempty"`
	CorrelationID string        `json:"correlation_id,omitempty"`
	CausationID   string        `json:"causation_id,omitempty"`
	RequestID     string        `json:"request_id,omitempty"`
	MaxIterations int           `json:"max_iterations,omitempty"`
	RLM           RLMREPLConfig `json:"rlm,omitempty"`
}

// StageFailure records non-fatal stage degradation details.
type StageFailure struct {
	Stage     string `json:"stage"`
	Kind      string `json:"kind,omitempty"`
	Message   string `json:"message"`
	Fatal     bool   `json:"fatal,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`
}

// TurnOutput captures deterministic runner results, including degraded state.
type TurnOutput struct {
	TurnID        string         `json:"turn_id"`
	Summary       string         `json:"summary,omitempty"`
	Iterations    int            `json:"iterations"`
	ToolCalls     int            `json:"tool_calls"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	Degraded      bool           `json:"degraded,omitempty"`
	StageFailures []StageFailure `json:"stage_failures,omitempty"`
}
