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
)

// ValidNodeKinds is the set of all valid NodeKind values.
var ValidNodeKinds = []NodeKind{NodeSkill, NodePTY, NodeHTTP, NodePlaywright, NodeImage, NodeTransform}

// IsValid reports whether k is a recognised NodeKind.
func (k NodeKind) IsValid() bool {
	switch k {
	case NodeSkill, NodePTY, NodeHTTP, NodePlaywright, NodeImage, NodeTransform:
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
)

// ValidTransformKinds is the set of all valid TransformKind values.
var ValidTransformKinds = []TransformKind{
	TransformPassthrough, TransformRegex, TransformTemplate,
	TransformJQ, TransformSplitLines, TransformMapFields,
}

// IsValid reports whether t is a recognised TransformKind.
func (t TransformKind) IsValid() bool {
	switch t {
	case TransformPassthrough, TransformRegex, TransformTemplate,
		TransformJQ, TransformSplitLines, TransformMapFields:
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
