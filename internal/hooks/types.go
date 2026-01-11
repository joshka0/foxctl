// Package hooks defines the v1 hook contract for agentctl's actor runtime.
//
// Hooks are skills invoked at canonical events during actor execution.
// They can observe, block, mutate, or enqueue actions.
//
// The types in this file are CANONICAL and STABLE for v1.
// Changes here affect all hook skills and the dispatcher.
package hooks

import "encoding/json"

// Event is the canonical event name.
// v1 supports ONLY these events.
type Event string

const (
	EventSessionStart          Event = "SessionStart"
	EventMessageReceived       Event = "MessageReceived"
	EventUserPromptSubmit      Event = "UserPromptSubmit"
	EventLLMRequest            Event = "LLMRequest"
	EventLLMResponse           Event = "LLMResponse"
	EventPreToolUse            Event = "PreToolUse"
	EventPostToolUse           Event = "PostToolUse"
	EventStopRequested         Event = "StopRequested"
	EventPostAgentTurn         Event = "PostAgentTurn"
	EventContextBudgetExceeded Event = "ContextBudgetExceeded"
	EventSessionEnd            Event = "SessionEnd"
	EventSubagentStart         Event = "SubagentStart" // When a subagent is spawned
	EventSubagentStop          Event = "SubagentStop"  // When a subagent completes
)

// AllEvents returns all canonical events for v1.
func AllEvents() []Event {
	return []Event{
		EventSessionStart,
		EventMessageReceived,
		EventUserPromptSubmit,
		EventLLMRequest,
		EventLLMResponse,
		EventPreToolUse,
		EventPostToolUse,
		EventStopRequested,
		EventPostAgentTurn,
		EventContextBudgetExceeded,
		EventSessionEnd,
		EventSubagentStart,
		EventSubagentStop,
	}
}

// IsValid returns true if the event is a canonical v1 event.
func (e Event) IsValid() bool {
	switch e {
	case EventSessionStart, EventMessageReceived, EventUserPromptSubmit,
		EventLLMRequest, EventLLMResponse, EventPreToolUse, EventPostToolUse,
		EventStopRequested, EventPostAgentTurn, EventContextBudgetExceeded,
		EventSessionEnd, EventSubagentStart, EventSubagentStop:
		return true
	default:
		return false
	}
}

// ToolKind categorizes tools for hook matching.
// Allows hooks to match by tool category rather than specific tool names.
type ToolKind string

const (
	ToolKindRead   ToolKind = "read"   // File/content reading operations
	ToolKindWrite  ToolKind = "write"  // File/content modification operations
	ToolKindExec   ToolKind = "exec"   // Command/process execution
	ToolKindSearch ToolKind = "search" // Search/query operations
	ToolKindAny    ToolKind = "any"    // Matches any tool kind
)

// IsValid returns true if the tool kind is a valid v1 kind.
func (k ToolKind) IsValid() bool {
	switch k {
	case ToolKindRead, ToolKindWrite, ToolKindExec, ToolKindSearch, ToolKindAny, "":
		return true
	default:
		return false
	}
}

// ClassifyToolKind determines the ToolKind for a given tool name.
// This is used to compute tool_kind when not explicitly provided.
func ClassifyToolKind(toolName, toolCanonical string) ToolKind {
	// Check canonical name first (more precise)
	if toolCanonical != "" {
		switch {
		case hasPrefix(toolCanonical, "edit."):
			return ToolKindWrite
		case hasPrefix(toolCanonical, "fs.write"), hasPrefix(toolCanonical, "fs.create"):
			return ToolKindWrite
		case hasPrefix(toolCanonical, "fs.read"), hasPrefix(toolCanonical, "fs."):
			return ToolKindRead
		case hasPrefix(toolCanonical, "code.search"), hasPrefix(toolCanonical, "code.semantic"):
			return ToolKindSearch
		case hasPrefix(toolCanonical, "text.grep"), hasPrefix(toolCanonical, "text."):
			return ToolKindSearch
		case hasPrefix(toolCanonical, "tests."), hasPrefix(toolCanonical, "bash."):
			return ToolKindExec
		case hasPrefix(toolCanonical, "todo."):
			return ToolKindWrite // Treat task management as write
		}
	}

	// Fall back to platform tool names (CC/OC)
	switch toolName {
	// CC write tools
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return ToolKindWrite
	// CC read tools
	case "Read":
		return ToolKindRead
	// CC search tools
	case "Grep", "Glob":
		return ToolKindSearch
	// CC exec tools
	case "Bash", "Task":
		return ToolKindExec
	// CC plan tools (treat as write)
	case "TodoWrite":
		return ToolKindWrite
	}

	return ToolKindAny
}

// hasPrefix is a helper for string prefix matching.
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// Decision is the hook verdict.
type Decision string

const (
	DecisionNone    Decision = "none"    // Advisory/no-op; does not block.
	DecisionApprove Decision = "approve" // Allow the operation (default allow).
	DecisionBlock   Decision = "block"   // Block the operation (hard gate).
)

// IsBlocking returns true if the decision blocks the operation.
func (d Decision) IsBlocking() bool {
	return d == DecisionBlock
}

// ProviderCapabilities describes what the calling provider can do.
// This helps hooks decide whether to inject immediately or enqueue for later.
//
// Context injection by event:
//   - PreToolUse: CANNOT inject (use enqueue_context → buffer)
//   - PostToolUse: CAN inject
//   - UserPromptSubmit: CAN inject
//   - SessionStart: CAN inject
//
// The Context Buffer is drained on inject-capable events.
type ProviderCapabilities struct {
	Name             string `json:"name"`               // "claude-code", "opencode", "agentctl"
	Event            string `json:"event"`              // Current event name
	CanInjectContext bool   `json:"can_inject_context"` // Can this event inject context?
	CanBlock         bool   `json:"can_block"`          // Can this event block the operation?
}

// Input is passed to hook skills on stdin (JSON).
// Fields are populated based on Event; unused fields may be omitted.
type Input struct {
	// Core routing
	Event         Event  `json:"event"`
	ActorID       string `json:"actor_id,omitempty"`       // e.g. actor:agent:coder-1
	WorkspaceID   string `json:"workspace_id,omitempty"`   // stable workspace key (e.g. hashed)
	WorkspaceRoot string `json:"workspace_root,omitempty"` // absolute path (when available)

	// Provider capabilities - helps hooks decide inject vs enqueue
	Provider *ProviderCapabilities `json:"provider,omitempty"`

	// Session/turn identity
	SessionID      string `json:"session_id,omitempty"`      // durable session identity
	TurnID         string `json:"turn_id,omitempty"`         // current turn correlation (engine-generated)
	TraceID        string `json:"trace_id,omitempty"`        // request trace
	CorrelationID  string `json:"correlation_id,omitempty"`  // e.g. ask_id/cmd_id/tool_call_id
	TokenEstimate  int    `json:"token_estimate,omitempty"`  // prompt token estimate (len/4 MVP)
	TokenBudget    int    `json:"token_budget,omitempty"`    // budget (when known)
	BudgetPressure string `json:"budget_pressure,omitempty"` // optional: "ok"|"warning"|"exceeded"

	// Message context (for MessageReceived)
	MailboxMessage *MailboxMessage `json:"mailbox_message,omitempty"`

	// Prompt context (for LLMRequest/LLMResponse/PostAgentTurn/StopRequested)
	Prompt        string `json:"prompt,omitempty"`         // full prompt text (bounded/assembled)
	AssistantText string `json:"assistant_text,omitempty"` // latest assistant text (if any)

	// Tool context (for PreToolUse/PostToolUse)
	ToolName      string          `json:"tool_name,omitempty"`      // platform tool name (e.g. Edit, Write for CC; edit for OC)
	ToolCanonical string          `json:"tool_canonical,omitempty"` // agentctl canonical tool name (e.g. edit.apply_patch)
	ToolKind      ToolKind        `json:"tool_kind,omitempty"`      // tool category: read|write|exec|search|any
	ToolInput     json.RawMessage `json:"tool_input,omitempty"`     // JSON args being executed

	// PostToolUse observation (what would be appended back to the LLM loop).
	// This should already be "safe": large outputs pointerized to artifact refs.
	ToolObservation json.RawMessage `json:"tool_observation,omitempty"`
	ToolError       string          `json:"tool_error,omitempty"`
	ToolDurationMS  int64           `json:"tool_duration_ms,omitempty"`

	// Hook-specific config from hooks.yaml
	HookConfig map[string]any `json:"hook_config,omitempty"`
}

// MailboxMessage is the durable queue message (leased mailbox store).
// This is the actor scheduler queue message, not the board/inbox message.
type MailboxMessage struct {
	ID      string            `json:"id"`
	FromNS  string            `json:"from_ns"`
	ToNS    string            `json:"to_ns"`
	Type    string            `json:"type"` // ask|cmd|event|reply|console.ask|...
	Headers map[string]string `json:"headers,omitempty"`
	Payload json.RawMessage   `json:"payload,omitempty"`

	TTLMS     int64 `json:"ttl_ms,omitempty"`
	VisibleAt int64 `json:"visible_at,omitempty"`
	Timestamp int64 `json:"timestamp,omitempty"`
}

// Output is returned by hook skills inside envelope.data.hook_output.
type Output struct {
	Decision Decision `json:"decision"`

	// Human-readable explanation. Used for block reasons and debugging.
	Reason string `json:"reason,omitempty"`

	// Optional context to inject immediately (dispatcher treats this as an inject_context action).
	// Keep small (<4KB). For larger, use Actions with CAS-backed artifacts.
	Context string `json:"context,omitempty"`

	// PreToolUse: if set, replaces the tool input args passed to the tool.
	// Dispatcher uses "last-wins" across hooks.
	UpdatedToolInput json.RawMessage `json:"updated_tool_input,omitempty"`

	// PostAgentTurn: if set, replaces the assistant final response text.
	// Dispatcher uses "last-wins" across hooks.
	UpdatedAssistantText string `json:"updated_assistant_text,omitempty"`

	// Ordered action list emitted by this hook.
	// Dispatcher concatenates actions in hook execution order.
	Actions []Action `json:"actions,omitempty"`

	// Free-form metadata for debugging/observability.
	Meta map[string]any `json:"meta,omitempty"`
}

// ActionType enumerates allowed action kinds in v1.
type ActionType string

const (
	ActionRunSkill       ActionType = "run_skill"
	ActionInjectContext  ActionType = "inject_context"
	ActionEnqueueContext ActionType = "enqueue_context" // Enqueue for later injection via buffer
	ActionSendMailbox    ActionType = "send_mailbox"
	ActionBBPost         ActionType = "bb_post"
	ActionBBClaim        ActionType = "bb_claim"
)

// AllActionTypes returns all valid action types for v1.
func AllActionTypes() []ActionType {
	return []ActionType{
		ActionRunSkill,
		ActionInjectContext,
		ActionEnqueueContext,
		ActionSendMailbox,
		ActionBBPost,
		ActionBBClaim,
	}
}

// IsValid returns true if the action type is a canonical v1 action.
func (a ActionType) IsValid() bool {
	switch a {
	case ActionRunSkill, ActionInjectContext, ActionEnqueueContext, ActionSendMailbox, ActionBBPost, ActionBBClaim:
		return true
	default:
		return false
	}
}

// Action is a tagged union.
// v1 uses a single struct with a superset of fields; fields must match Type.
type Action struct {
	Type ActionType `json:"type"`

	// run_skill
	Skill string          `json:"skill,omitempty"` // e.g. "todo/manage"
	Args  json.RawMessage `json:"args,omitempty"`  // skill input

	// inject_context / enqueue_context (shared fields)
	Text     string `json:"text,omitempty"`
	Priority int    `json:"priority,omitempty"` // higher = earlier in ContextInbox; default 0

	// enqueue_context (additional fields)
	Source     string `json:"source,omitempty"`      // Source identifier for deduplication/filtering
	TTLSeconds int    `json:"ttl_seconds,omitempty"` // Time-to-live for buffer entry (default: 60)
	Dedupe     bool   `json:"dedupe,omitempty"`      // Skip if same source+text exists unconsumed

	// send_mailbox
	ToNS         string            `json:"to_ns,omitempty"`
	MessageType  string            `json:"message_type,omitempty"` // ask|cmd|event|reply|console.ask|...
	Payload      json.RawMessage   `json:"payload,omitempty"`      // agentctl envelope
	Headers      map[string]string `json:"headers,omitempty"`
	TTLMS        int64             `json:"ttl_ms,omitempty"`
	DeliveryHint string            `json:"delivery_hint,omitempty"` // optional: "interrupt"|"next_turn"

	// bb_post / bb_claim
	Topic     string          `json:"topic,omitempty"`
	RecordID  string          `json:"record_id,omitempty"`
	BBPayload json.RawMessage `json:"payload_bb,omitempty"`
}

// Helper constructors for hook skills

// NewNone returns a no-op output (advisory, does not block).
func NewNone() Output {
	return Output{Decision: DecisionNone}
}

// NewNoneWithContext returns a no-op output with context to inject.
func NewNoneWithContext(context string) Output {
	return Output{Decision: DecisionNone, Context: context}
}

// NewApprove returns an approve output with optional reason and metadata.
func NewApprove(reason string, meta map[string]any) Output {
	return Output{Decision: DecisionApprove, Reason: reason, Meta: meta}
}

// NewBlock returns a block output with a reason.
func NewBlock(reason string) Output {
	return Output{Decision: DecisionBlock, Reason: reason}
}

// NewBlockWithContext returns a block output with reason and context to inject.
func NewBlockWithContext(reason, context string) Output {
	return Output{Decision: DecisionBlock, Reason: reason, Context: context}
}

// WithActions adds actions to an output (builder pattern).
func (o Output) WithActions(actions ...Action) Output {
	o.Actions = append(o.Actions, actions...)
	return o
}

// WithContext sets the context field (builder pattern).
func (o Output) WithContext(context string) Output {
	o.Context = context
	return o
}

// WithMeta sets metadata (builder pattern).
func (o Output) WithMeta(meta map[string]any) Output {
	o.Meta = meta
	return o
}

// Action constructors

// RunSkillAction creates a run_skill action.
func RunSkillAction(skill string, args json.RawMessage) Action {
	return Action{Type: ActionRunSkill, Skill: skill, Args: args}
}

// InjectContextAction creates an inject_context action.
func InjectContextAction(text string, priority int) Action {
	return Action{Type: ActionInjectContext, Text: text, Priority: priority}
}

// EnqueueContextAction creates an enqueue_context action for buffer-based injection.
// Source identifies the hook origin for filtering/deduplication.
// Priority: 1=high, 2=normal (default), 3=low.
// TTLSeconds defaults to 60 if <= 0.
func EnqueueContextAction(source, text string, priority, ttlSeconds int, dedupe bool) Action {
	return Action{
		Type:       ActionEnqueueContext,
		Source:     source,
		Text:       text,
		Priority:   priority,
		TTLSeconds: ttlSeconds,
		Dedupe:     dedupe,
	}
}

// SendMailboxAction creates a send_mailbox action.
func SendMailboxAction(toNS, msgType string, payload json.RawMessage, headers map[string]string) Action {
	return Action{Type: ActionSendMailbox, ToNS: toNS, MessageType: msgType, Payload: payload, Headers: headers}
}

// BBPostAction creates a bb_post action.
func BBPostAction(topic string, payload json.RawMessage) Action {
	return Action{Type: ActionBBPost, Topic: topic, BBPayload: payload}
}

// BBClaimAction creates a bb_claim action.
func BBClaimAction(topic, recordID string) Action {
	return Action{Type: ActionBBClaim, Topic: topic, RecordID: recordID}
}
