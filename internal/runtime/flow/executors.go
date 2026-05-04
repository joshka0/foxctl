package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
	"github.com/joshka0/foxctl/internal/domain/envelope"
)

// ---------------------------------------------------------------------------
// NodeExecutor interface
// ---------------------------------------------------------------------------

// NodeExecutor executes a flow node, producing output from input.
// Implementations must be safe for concurrent use.
type NodeExecutor interface {
	// Execute runs the node with the given input and returns the output.
	// Source nodes receive nil input.
	// Implementations should respect context cancellation.
	Execute(ctx context.Context, node FlowNode, input any) (NodeOutput, error)
}

// ---------------------------------------------------------------------------
// SkillExecutor
// ---------------------------------------------------------------------------

// SkillExecutor executes a foxctl skill subprocess.
type SkillExecutor struct {
	// Workspace is the working directory for skill execution.
	Workspace string
}

// Execute runs the configured skill via executil.RunFoxctlSkill.
// For source nodes (nil input), it passes no input.
// For non-source nodes, it serializes the input data and passes it as --input.
func (e *SkillExecutor) Execute(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
	start := time.Now()

	// Parse skill config.
	var cfg SkillConfig
	if err := json.Unmarshal(node.Config, &cfg); err != nil {
		return NodeOutput{}, fmt.Errorf("flow: skill executor: parse config: %w", err)
	}

	workspace := cfg.Workspace
	if workspace == "" {
		workspace = e.Workspace
	}

	// Serialize input.
	var inputBytes []byte
	if input != nil {
		var err error
		inputBytes, err = json.Marshal(input)
		if err != nil {
			return NodeOutput{}, fmt.Errorf("flow: skill executor: marshal input: %w", err)
		}
	}

	// Run skill.
	result, err := executil.RunFoxctlSkillWithArgs(ctx, workspace, cfg.Skill, inputBytes, cfg.ExtraArgs)
	duration := time.Since(start)

	if err != nil {
		// If we have a partial result with an envelope, use it.
		if result.Envelope.Version > 0 {
			return NodeOutput{
				Envelope: result.Envelope,
				Duration: duration,
				NodeID:   node.ID,
			}, nil
		}
		// Otherwise, produce an error envelope.
		return NodeOutput{
			Envelope: envelope.Error("flow/skill", "ERUNTIME",
				fmt.Sprintf("skill %q execution failed: %v", cfg.Skill, err), nil),
			Duration: duration,
			NodeID:   node.ID,
		}, nil
	}

	return NodeOutput{
		Envelope: result.Envelope,
		Duration: duration,
		NodeID:   node.ID,
	}, nil
}

// ---------------------------------------------------------------------------
// TransformExecutor
// ---------------------------------------------------------------------------

// TransformExecutor applies a configured transform to input data.
// It uses the transform registry to look up and execute transforms.
type TransformExecutor struct{}

// Execute runs the configured transform on the input data.
func (e *TransformExecutor) Execute(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
	start := time.Now()

	// Parse transform config.
	var cfg struct {
		Transform string `json:"transform"`
		Config    string `json:"config"`
	}
	if err := json.Unmarshal(node.Config, &cfg); err != nil {
		return NodeOutput{}, fmt.Errorf("flow: transform executor: parse config: %w", err)
	}

	kind := TransformKind(cfg.Transform)
	if !kind.IsValid() {
		return NodeOutput{}, fmt.Errorf("flow: transform executor: invalid transform kind %q", kind)
	}

	// Apply the transform.
	result, err := ApplyTransform(ctx, kind, cfg.Config, input)
	duration := time.Since(start)

	if err != nil {
		return NodeOutput{
			Envelope: envelope.Error("flow/transform", "EPARSE", err.Error(), nil),
			Duration: duration,
			NodeID:   node.ID,
		}, nil
	}

	return NodeOutput{
		Envelope: envelope.OK("flow/transform", result),
		Duration: duration,
		NodeID:   node.ID,
	}, nil
}

// ---------------------------------------------------------------------------
// parseOutputEnvelope helper
// ---------------------------------------------------------------------------

// parseOutputEnvelope parses raw bytes into an envelope.
// Returns an error if the bytes are not valid JSON or not a valid envelope.
func parseOutputEnvelope(data []byte) (envelope.Envelope, error) {
	var env envelope.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return envelope.Envelope{}, fmt.Errorf("flow: parse output: %w", err)
	}
	return env, nil
}

// ---------------------------------------------------------------------------
// AgentExecutor
// ---------------------------------------------------------------------------

// AgentSpawnResult holds the essential fields returned after spawning an agent.
type AgentSpawnResult struct {
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
}

// AgentAskResult holds the result of asking an agent a question.
type AgentAskResult struct {
	Reply  string         `json:"reply"`
	Status string         `json:"status"`
}

// AgentInfoResult holds the status info for a spawned agent.
type AgentInfoResult struct {
	Status  string    `json:"status"`
	Summary string    `json:"summary,omitempty"`
	Error   string    `json:"error,omitempty"`
}

// AgentSpawner abstracts the daemon client operations needed to spawn and
// interact with agents. This allows the executor to work with real daemon
// clients or test mocks.
type AgentSpawner interface {
	// Spawn creates a new agent and returns its ID and session ID.
	Spawn(ctx context.Context, role, prompt string, opts AgentSpawnOptions) (*AgentSpawnResult, error)
	// Ask sends a message to an agent and waits for the reply.
	Ask(ctx context.Context, agentID string, message string, timeoutMS int) (*AgentAskResult, error)
	// Info retrieves the current status of an agent.
	Info(ctx context.Context, agentID string) (*AgentInfoResult, error)
	// Kill terminates an agent session.
	Kill(ctx context.Context, sessionID string) error
}

// AgentSpawnOptions holds optional parameters for spawning an agent.
type AgentSpawnOptions struct {
	ExecMode      string
	MaxIterations int
	MaxAutoTurns  int
	Timeout       string
	LLMProvider   string
	LLMModel      string
	SkillsAllow   []string
	Workspace     string
	// CLICmd specifies which CLI agent command to launch when using the
	// foxprox spawner (e.g., "droid", "claude"). Default: "droid".
	// This field is ignored by the daemon's internal agent runtime.
	CLICmd string
}

// AgentExecutor spawns a foxctl agent, waits for completion (or ask reply),
// and returns the captured output as a NodeOutput envelope.
type AgentExecutor struct {
	// Spawner is the interface used to interact with agents. Required.
	Spawner AgentSpawner
	// Workspace is the default workspace for the agent. Overridden by config.
	Workspace string
	// SubscribeOutput is an optional function that subscribes to the OutputBus
	// for a specific node in a specific flow. When set, the "push" output mode
	// uses this to wait for externally pushed output instead of polling Info().
	// Returns a channel that receives the pushed output, or nil if not available.
	SubscribeOutput func(flowID, nodeID string) <-chan NodeOutput
	// GetRunID is an optional function that returns the active run ID for a
	// given flow. When set, push mode uses this to inject the actual run_id
	// into the agent's prompt so it knows where to push output via
	// `foxctl flow output`. Returns empty string if the flow is not running.
	GetRunID func(flowID string) string
}

// Execute runs the agent node: parses config, spawns agent, waits for output.
// For source nodes (nil input), the prompt is used as-is.
// For non-source nodes, upstream data is injected based on InputMode.
func (e *AgentExecutor) Execute(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
	start := time.Now()

	// Parse agent config.
	var cfg AgentConfig
	if err := json.Unmarshal(node.Config, &cfg); err != nil {
		return NodeOutput{}, fmt.Errorf("flow: agent executor: parse config: %w", err)
	}

	// Validate config.
	if err := cfg.Validate(); err != nil {
		return NodeOutput{
			Envelope: envelope.Error("flow/agent", "EARG", err.Error(), nil),
			Duration: time.Since(start),
			NodeID:   node.ID,
		}, nil
	}

	// Determine defaults.
	inputMode := cfg.InputMode
	if inputMode == "" {
		inputMode = "prompt"
	}
	outputMode := cfg.OutputMode
	if outputMode == "" {
		outputMode = "session_summary"
	}
	askTimeout := cfg.AskTimeoutMS
	if askTimeout <= 0 {
		askTimeout = 30000
	}

	workspace := cfg.Workspace
	if workspace == "" {
		workspace = e.Workspace
	}

	// Build the spawn prompt based on input mode.
	prompt := cfg.Prompt
	if inputMode == "prompt" && input != nil {
		inputData, err := json.Marshal(input)
		if err != nil {
			return NodeOutput{
				Envelope: envelope.Error("flow/agent", "EPARSE",
					fmt.Sprintf("marshal upstream input: %v", err), nil),
				Duration: time.Since(start),
				NodeID:   node.ID,
			}, nil
		}
		prompt = cfg.Prompt + "\n\n--- Upstream Data ---\n" + string(inputData)
	}

	// For push output mode, inject flow output push instructions into the prompt
	// so the agent knows where to send its output.
	if outputMode == "push" {
		runID := ""
		if e.GetRunID != nil {
			runID = e.GetRunID(node.FlowID)
		}
		if runID == "" {
			runID = "unknown"
		}
		pushInstructions := fmt.Sprintf(
			"\n\n--- Flow Output Push Configuration ---\n"+
				"You are running as a node (%s/%s) in flow (%s), run (%s).\n"+
				"When you have completed your task, push your structured output "+
				"back to the flow engine by running:\n\n"+
				"  foxctl flow output %s --node %s --data '<your-json-output>'\n\n"+
				"Replace <your-json-output> with your actual result as a JSON object.\n"+
				"This is REQUIRED as your final step before completing.\n",
			node.Label, node.ID, node.FlowID, runID,
			runID, node.ID,
		)
		prompt = prompt + pushInstructions
	}

	// Set up timeout context if configured.
	spawnCtx := ctx
	var cancel context.CancelFunc
	if cfg.Timeout != "" {
		d, err := time.ParseDuration(cfg.Timeout)
		if err != nil {
			return NodeOutput{
				Envelope: envelope.Error("flow/agent", "EARG",
					fmt.Sprintf("invalid timeout %q: %v", cfg.Timeout, err), nil),
				Duration: time.Since(start),
				NodeID:   node.ID,
			}, nil
		}
		spawnCtx, cancel = context.WithTimeout(ctx, d)
	}
	defer func() {
		if cancel != nil {
			cancel()
		}
	}()

	// Spawn the agent.
	spawnOpts := AgentSpawnOptions{
		ExecMode:      cfg.ExecMode,
		MaxIterations: cfg.MaxIterations,
		MaxAutoTurns:  cfg.MaxAutoTurns,
		Timeout:       cfg.Timeout,
		LLMProvider:   cfg.LLMProvider,
		LLMModel:      cfg.LLMModel,
		SkillsAllow:   cfg.SkillsAllow,
		Workspace:     workspace,
		CLICmd:        cfg.CLICmd,
	}
	spawnResult, err := e.Spawner.Spawn(spawnCtx, cfg.Role, prompt, spawnOpts)
	duration := time.Since(start)

	if err != nil {
		errMsg := err.Error()
		if spawnCtx.Err() == context.DeadlineExceeded {
			errMsg = fmt.Sprintf("agent spawn timed out: %v", err)
		}
		return NodeOutput{
			Envelope: envelope.Error("flow/agent", "ESPAWN", errMsg, nil),
			Duration: duration,
			NodeID:   node.ID,
		}, nil
	}

	// Handle output capture based on output mode.
	switch outputMode {
	case "ask":
		return e.executeAskMode(spawnCtx, cfg, spawnResult, input, start, node, askTimeout)
	case "session_summary":
		return e.executeSessionSummaryMode(spawnCtx, spawnResult, start, node)
	case "push":
		return e.executePushMode(spawnCtx, cfg, spawnResult, input, start, node)
	default:
		return NodeOutput{
			Envelope: envelope.Error("flow/agent", "EARG",
				fmt.Sprintf("invalid output_mode %q", outputMode), nil),
			Duration: time.Since(start),
			NodeID:   node.ID,
		}, nil
	}
}

// executeAskMode handles the ask output mode: spawn agent, send ask, wait for reply.
func (e *AgentExecutor) executeAskMode(
	ctx context.Context,
	cfg AgentConfig,
	spawnResult *AgentSpawnResult,
	input any,
	start time.Time,
	node FlowNode,
	askTimeout int,
) (NodeOutput, error) {
	// Send ask message with upstream data.
	message := ""
	if input != nil {
		inputData, err := json.Marshal(input)
		if err != nil {
			return NodeOutput{
				Envelope: envelope.Error("flow/agent", "EPARSE",
					fmt.Sprintf("marshal upstream input for ask: %v", err), nil),
				Duration: time.Since(start),
				NodeID:   node.ID,
			}, nil
		}
		message = string(inputData)
	}

	askResult, err := e.Spawner.Ask(ctx, spawnResult.AgentID, message, askTimeout)
	duration := time.Since(start)

	if err != nil {
		// Timeout: kill the agent and return error.
		_ = e.Spawner.Kill(context.Background(), spawnResult.SessionID)
		errMsg := err.Error()
		if ctx.Err() == context.DeadlineExceeded {
			errMsg = "agent ask timed out"
		}
		return NodeOutput{
			Envelope: envelope.Error("flow/agent", "ETIMEOUT", errMsg, nil),
			Duration: duration,
			NodeID:   node.ID,
		}, nil
	}

	// Build output envelope from ask reply.
	data := map[string]any{
		"agent_id":  spawnResult.AgentID,
		"reply":     askResult.Reply,
		"status":    askResult.Status,
		"role":      cfg.Role,
		"output_mode": "ask",
	}
	return NodeOutput{
		Envelope: envelope.OK("flow/agent", data),
		Duration: time.Since(start),
		NodeID:   node.ID,
	}, nil
}

// executeSessionSummaryMode handles the session_summary output mode: poll until
// the agent completes, then capture the summary.
func (e *AgentExecutor) executeSessionSummaryMode(
	ctx context.Context,
	spawnResult *AgentSpawnResult,
	start time.Time,
	node FlowNode,
) (NodeOutput, error) {
	// Poll agent status until completion or timeout.
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Context cancelled or timed out. Kill the agent.
			_ = e.Spawner.Kill(context.Background(), spawnResult.SessionID)
			return NodeOutput{
				Envelope: envelope.Error("flow/agent", "ETIMEOUT",
					"agent execution timed out", nil),
				Duration: time.Since(start),
				NodeID:   node.ID,
			}, nil
		case <-ticker.C:
			info, err := e.Spawner.Info(ctx, spawnResult.AgentID)
			if err != nil {
				// Info error: could be transient. Continue polling.
				continue
			}
			switch info.Status {
			case "completed", "stopped", "exited":
				// Agent finished. Return summary.
				data := map[string]any{
					"agent_id":  spawnResult.AgentID,
					"summary":   info.Summary,
					"status":    info.Status,
					"output_mode": "session_summary",
				}
				if info.Error != "" {
					data["error"] = info.Error
				}
				env := envelope.OK("flow/agent", data)
				if info.Error != "" {
					env.Status = envelope.StatusError
					env.Error = envelope.ErrorFields{Code: "EAGENT", Message: info.Error}
				}
				return NodeOutput{
					Envelope: env,
					Duration: time.Since(start),
					NodeID:   node.ID,
				}, nil
			case "error":
				// Agent errored. Kill and return error.
				_ = e.Spawner.Kill(context.Background(), spawnResult.SessionID)
				errMsg := info.Error
				if errMsg == "" {
					errMsg = "agent execution failed"
				}
				return NodeOutput{
					Envelope: envelope.Error("flow/agent", "EAGENT", errMsg, nil),
					Duration: time.Since(start),
					NodeID:   node.ID,
				}, nil
			}
			// Agent still running (status "running", "active", etc.). Continue polling.
		}
	}
}

// executePushMode handles the push output mode: spawn agent, then subscribe
// to the OutputBus and wait for the agent to push its output via
// `foxctl flow output`. This avoids screen-scraping for output capture.
func (e *AgentExecutor) executePushMode(
	ctx context.Context,
	cfg AgentConfig,
	spawnResult *AgentSpawnResult,
	input any,
	start time.Time,
	node FlowNode,
) (NodeOutput, error) {
	// The agent has been spawned. In push mode, we wait for the agent to
	// push its output back via `foxctl flow output` instead of polling Info().
	// This requires SubscribeOutput to be set on the executor.
	if e.SubscribeOutput == nil {
		// No subscription available — fall back to session_summary mode.
		return e.executeSessionSummaryMode(ctx, spawnResult, start, node)
	}

	// Subscribe to this node's output channel on the engine's OutputBus.
	// The flowID is embedded in the node's FlowID field.
	sub := e.SubscribeOutput(node.FlowID, node.ID)
	if sub == nil {
		// Flow not running or bus not available — fall back.
		return e.executeSessionSummaryMode(ctx, spawnResult, start, node)
	}

	// Wait for the pushed output or context cancellation.
	select {
	case out, ok := <-sub:
		if !ok {
			// Channel closed — flow stopped or context cancelled.
			_ = e.Spawner.Kill(context.Background(), spawnResult.SessionID)
			return NodeOutput{
				Envelope: envelope.Error("flow/agent", "ECANCELED",
					"output bus closed while waiting for pushed output", nil),
				Duration: time.Since(start),
				NodeID:   node.ID,
			}, nil
		}
		// Agent pushed output successfully.
		return out, nil
	case <-ctx.Done():
		// Context cancelled or timed out. Kill the agent.
		_ = e.Spawner.Kill(context.Background(), spawnResult.SessionID)
		return NodeOutput{
			Envelope: envelope.Error("flow/agent", "ETIMEOUT",
				"timed out waiting for pushed output", nil),
			Duration: time.Since(start),
			NodeID:   node.ID,
		}, nil
	}
}
