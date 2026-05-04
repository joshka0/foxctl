// Package flow defines the data model and interfaces for the foxctl flow engine.
//
// A flow is a named directed graph of envelope-producing nodes connected by
// typed edges. Nodes are execution strategies (skill, pty, http, playwright,
// image, transform) and edges route envelope data between nodes with optional
// transforms, triggers, and conditions.
//
// The envelope contract (version/status/command/data/meta/error) is the
// universal typed pipe: every node takes envelope data in and produces an
// envelope out.
package flow

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
)

// ---------------------------------------------------------------------------
// Enums
// ---------------------------------------------------------------------------

// FlowState represents the lifecycle state of a flow.
type FlowState string

const (
	FlowDraft   FlowState = "draft"
	FlowRunning FlowState = "running"
	FlowPaused  FlowState = "paused"
	FlowStopped FlowState = "stopped"
	FlowErrored FlowState = "errored"
)

// ValidFlowStates is the set of all valid FlowState values.
var ValidFlowStates = []FlowState{FlowDraft, FlowRunning, FlowPaused, FlowStopped, FlowErrored}

// IsValid reports whether s is a recognised FlowState.
func (s FlowState) IsValid() bool {
	switch s {
	case FlowDraft, FlowRunning, FlowPaused, FlowStopped, FlowErrored:
		return true
	default:
		return false
	}
}

// NodeKind identifies the execution strategy for a flow node.
type NodeKind string

const (
	// NodeSkill executes a foxctl skill via `foxctl run <skill> --input <json>`.
	NodeSkill NodeKind = "skill"
	// NodePTY runs a foxprox PTY session.
	NodePTY NodeKind = "pty"
	// NodeHTTP makes an HTTP request (webhooks, APIs).
	NodeHTTP NodeKind = "http"
	// NodePlaywright runs browser automation via foxctl playwright skills.
	NodePlaywright NodeKind = "playwright"
	// NodeImage invokes an image generation or capture tool.
	NodeImage NodeKind = "image"
	// NodeTransform is a pure data transform (no external execution).
	NodeTransform NodeKind = "transform"
	// NodeAgent spawns a foxctl agent and captures its output.
	NodeAgent NodeKind = "agent"
)

// ValidNodeKinds is the set of all valid NodeKind values.
var ValidNodeKinds = []NodeKind{NodeSkill, NodePTY, NodeHTTP, NodePlaywright, NodeImage, NodeTransform, NodeAgent}

// IsValid reports whether k is a recognised NodeKind.
func (k NodeKind) IsValid() bool {
	switch k {
	case NodeSkill, NodePTY, NodeHTTP, NodePlaywright, NodeImage, NodeTransform, NodeAgent:
		return true
	default:
		return false
	}
}

// TransformKind identifies the transform applied on an edge.
type TransformKind string

const (
	TransformPassthrough TransformKind = "passthrough"
	TransformRegex       TransformKind = "regex_extract"
	TransformTemplate    TransformKind = "template"
	TransformJQ          TransformKind = "jq_filter"
	TransformSplitLines  TransformKind = "split_lines"
	TransformMapFields   TransformKind = "map_fields"
	TransformFileWrite   TransformKind = "file_write"
)

// ValidTransformKinds is the set of all valid TransformKind values.
var ValidTransformKinds = []TransformKind{
	TransformPassthrough, TransformRegex, TransformTemplate,
	TransformJQ, TransformSplitLines, TransformMapFields,
	TransformFileWrite,
}

// IsValid reports whether t is a recognised TransformKind.
func (t TransformKind) IsValid() bool {
	switch t {
	case TransformPassthrough, TransformRegex, TransformTemplate,
		TransformJQ, TransformSplitLines, TransformMapFields, TransformFileWrite:
		return true
	default:
		return false
	}
}

// TriggerKind identifies when an edge fires.
type TriggerKind string

const (
	// TriggerOutputReady fires when the source node produces an envelope.
	TriggerOutputReady TriggerKind = "output_ready"
	// TriggerScreenMatch fires when a PTY node's vtscreen matches a regex.
	TriggerScreenMatch TriggerKind = "screen_match"
	// TriggerExit fires when a PTY node's session exits.
	TriggerExit TriggerKind = "exit"
	// TriggerManual only fires on explicit `foxctl flow trigger <edge-id>`.
	TriggerManual TriggerKind = "manual"
)

// ValidTriggerKinds is the set of all valid TriggerKind values.
var ValidTriggerKinds = []TriggerKind{TriggerOutputReady, TriggerScreenMatch, TriggerExit, TriggerManual}

// IsValid reports whether t is a recognised TriggerKind.
func (t TriggerKind) IsValid() bool {
	switch t {
	case TriggerOutputReady, TriggerScreenMatch, TriggerExit, TriggerManual:
		return true
	default:
		return false
	}
}

// RunState represents the lifecycle state of a flow run.
type RunState string

const (
	RunRunning   RunState = "running"
	RunCompleted RunState = "completed"
	RunFailed    RunState = "failed"
)

// ValidRunStates is the set of all valid RunState values.
var ValidRunStates = []RunState{RunRunning, RunCompleted, RunFailed}

// IsValid reports whether s is a recognised RunState.
func (s RunState) IsValid() bool {
	switch s {
	case RunRunning, RunCompleted, RunFailed:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Core types
// ---------------------------------------------------------------------------

// Flow is a named directed graph of envelope-producing nodes.
type Flow struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Workspace   string    `json:"workspace"`
	State       FlowState `json:"state"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Validate checks that the flow has valid field values. Returns nil if valid.
func (f Flow) Validate() error {
	if len(f.Name) > MaxFlowNameLen {
		return ErrNameTooLong
	}
	return nil
}

// FlowNode is a single step in a flow that consumes an envelope and produces one.
type FlowNode struct {
	ID       string          `json:"id"`
	FlowID   string          `json:"flow_id"`
	Kind     NodeKind        `json:"kind"`
	Label    string          `json:"label"`
	Config   json.RawMessage `json:"config"`
	Position *Position       `json:"position,omitempty"`
}

// FlowEdge is a typed pipe between two nodes.
type FlowEdge struct {
	ID              string        `json:"id"`
	FlowID          string        `json:"flow_id"`
	FromNodeID      string        `json:"from_node_id"`
	ToNodeID        string        `json:"to_node_id"`
	Transform       TransformKind `json:"transform"`
	TransformConfig string        `json:"transform_config,omitempty"`
	Trigger         TriggerKind   `json:"trigger"`
	TriggerConfig   string        `json:"trigger_config,omitempty"`
	Condition       string        `json:"condition,omitempty"`
	RetryPolicy     *RetryPolicy  `json:"retry_policy,omitempty"`
}

// FlowRun tracks a single execution of a flow.
type FlowRun struct {
	ID          string     `json:"id"`
	FlowID      string     `json:"flow_id"`
	State       RunState   `json:"state"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// NodeOutput is the structured output from any flow node.
type NodeOutput struct {
	Envelope envelope.Envelope `json:"envelope"`
	Duration time.Duration     `json:"duration"`
	NodeID   string            `json:"node_id"`
}

// ---------------------------------------------------------------------------
// Helper types
// ---------------------------------------------------------------------------

// Position captures node placement for the visual canvas.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// RetryPolicy controls edge delivery retries.
type RetryPolicy struct {
	MaxAttempts int   `json:"max_attempts"` // default 0 (no retry)
	DelayMS     int64 `json:"delay_ms,omitempty"`
}

// ---------------------------------------------------------------------------
// Typed config structs
// ---------------------------------------------------------------------------

// RunLog captures a single log entry produced by a flow node during execution.
// Each entry stores the full envelope JSON produced by the node.
type RunLog struct {
	ID        string            `json:"id"`
	RunID     string            `json:"run_id"`
	NodeID    string            `json:"node_id"`
	Seq       int               `json:"seq"`
	Envelope  json.RawMessage   `json:"envelope"`
	CreatedAt time.Time         `json:"created_at"`
}

// RunLogFilter holds optional parameters for querying run logs.
type RunLogFilter struct {
	NodeID string
	Limit  int
	Offset int
}

// RunLogOption is a functional option for run log queries.
type RunLogOption func(*RunLogFilter)

// WithNodeID filters log entries by the given node ID.
func WithNodeID(nodeID string) RunLogOption {
	return func(f *RunLogFilter) {
		f.NodeID = nodeID
	}
}

// WithLimit limits the number of log entries returned.
func WithLimit(limit int) RunLogOption {
	return func(f *RunLogFilter) {
		f.Limit = limit
	}
}

// WithOffset skips the first N log entries.
func WithOffset(offset int) RunLogOption {
	return func(f *RunLogFilter) {
		f.Offset = offset
	}
}

// ApplyRunLogOptions builds a RunLogFilter from the given options.
func ApplyRunLogOptions(opts ...RunLogOption) RunLogFilter {
	f := RunLogFilter{}
	for _, o := range opts {
		o(&f)
	}
	return f
}

// SkillConfig is the typed config for NodeSkill.
type SkillConfig struct {
	Skill     string   `json:"skill"`
	ExtraArgs []string `json:"extra_args,omitempty"`
	Workspace string   `json:"workspace,omitempty"`
	InputMode string   `json:"input_mode,omitempty"` // "data" (default) | "envelope"
}

// PTYConfig is the typed config for NodePTY.
type PTYConfig struct {
	Cmd            []string `json:"cmd"`
	Cwd            string   `json:"cwd,omitempty"`
	Adapter        string   `json:"adapter,omitempty"`
	SubmitKey      string   `json:"submit_key,omitempty"`
	Env            []string `json:"env,omitempty"`
	Rows           uint16   `json:"rows,omitempty"`
	Cols           uint16   `json:"cols,omitempty"`
	WaitForReadyMS int64    `json:"wait_for_ready_ms,omitempty"`
}

// HTTPConfig is the typed config for NodeHTTP.
type HTTPConfig struct {
	URL       string            `json:"url"`
	Method    string            `json:"method,omitempty"` // GET (default) | POST | PUT | DELETE
	Headers   map[string]string `json:"headers,omitempty"`
	TimeoutMS int64             `json:"timeout_ms,omitempty"`
	BodyPath  string            `json:"body_path,omitempty"` // jq-style path into envelope data for request body
}

// FileWriteConfig is the typed config for the file_write transform.
// It writes upstream envelope data to a specified file path.
type FileWriteConfig struct {
	// Path is the file path to write to. Required. Supports basic templating
	// from envelope data (e.g., {{.data.topic}} in the filename).
	Path string `json:"path"`

	// Format controls the output format. One of "raw", "json", "markdown".
	// Default: "raw".
	Format string `json:"format,omitempty"`
}

// Validate checks that the FileWriteConfig has valid field values.
// Path is required. Format must be one of "", "raw", "json", "markdown".
func (c FileWriteConfig) Validate() error {
	if c.Path == "" {
		return fmt.Errorf("flow: file_write config: path is required")
	}
	switch c.Format {
	case "", "raw", "json", "markdown":
	default:
		return fmt.Errorf("flow: file_write config: invalid format %q (must be raw, json, or markdown)", c.Format)
	}
	return nil
}

// AgentConfig is the typed config for NodeAgent.
// It defines how to spawn and interact with a foxctl agent.
type AgentConfig struct {
	// Role is the agent role (e.g., "researcher", "coder", "reviewer"). Required.
	Role string `json:"role"`

	// Prompt is the initial prompt for the agent. Required.
	Prompt string `json:"prompt"`

	// ExecMode controls agent execution: "reactive", "autonomous", "proactive".
	// Default: "autonomous".
	ExecMode string `json:"exec_mode,omitempty"`

	// MaxIterations limits the number of tool calls per engine run. Default: 50.
	MaxIterations int `json:"max_iterations,omitempty"`

	// MaxAutoTurns limits autonomous continuation turns. Default: 1.
	MaxAutoTurns int `json:"max_auto_turns,omitempty"`

	// Timeout is the maximum duration for the agent to run (e.g., "5m").
	// Default: agent runtime default.
	Timeout string `json:"timeout,omitempty"`

	// LLMProvider overrides the default LLM provider.
	LLMProvider string `json:"llm_provider,omitempty"`

	// LLMModel overrides the default LLM model.
	LLMModel string `json:"llm_model,omitempty"`

	// SkillsAllow restricts which skills the agent can use.
	SkillsAllow []string `json:"skills_allow,omitempty"`

	// InputMode controls how upstream data is passed to the agent.
	// "prompt" — inject upstream data into the spawn prompt (default).
	// "ask" — spawn first, then send upstream data as an agent.ask message.
	InputMode string `json:"input_mode,omitempty"`

	// OutputMode controls how the agent's output is captured.
	// "session_summary" — poll until completion and capture session summary (default).
	// "ask" — use the ask reply as the output (requires InputMode "ask").
	OutputMode string `json:"output_mode,omitempty"`

	// AskTimeoutMS is the timeout in milliseconds for waiting on an ask reply.
	// Default: 30000 (30 seconds). Only used when InputMode is "ask".
	AskTimeoutMS int `json:"ask_timeout_ms,omitempty"`

	// Workspace overrides the workspace for the spawned agent.
	Workspace string `json:"workspace,omitempty"`

	// CLICmd specifies which CLI agent command to launch when using the
	// foxprox spawner (e.g., "droid", "claude"). Default: "droid".
	// This field is ignored by the daemon's internal agent runtime.
	CLICmd string `json:"cli_cmd,omitempty"`
}

// Validate checks that the AgentConfig has valid field values.
// Role is required. Returns nil if valid.
func (c AgentConfig) Validate() error {
	if c.Role == "" {
		return fmt.Errorf("flow: agent config: role is required")
	}
	if c.Prompt == "" && c.InputMode != "ask" {
		return fmt.Errorf("flow: agent config: prompt is required")
	}
	if c.InputMode != "" && c.InputMode != "prompt" && c.InputMode != "ask" {
		return fmt.Errorf("flow: agent config: invalid input_mode %q (must be prompt or ask)", c.InputMode)
	}
	if c.OutputMode != "" && c.OutputMode != "session_summary" && c.OutputMode != "ask" && c.OutputMode != "push" {
		return fmt.Errorf("flow: agent config: invalid output_mode %q (must be session_summary, ask, or push)", c.OutputMode)
	}
	return nil
}
