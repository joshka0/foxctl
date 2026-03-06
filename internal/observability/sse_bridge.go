package observability

import (
	"strings"
	"sync"
)

// SSEPublisher defines the interface for publishing events to SSE.
// This avoids importing the sse package directly (dependency direction: observability <- web).
type SSEPublisher interface {
	Publish(eventType string, data any)
}

var (
	ssePublisher   SSEPublisher
	ssePublisherMu sync.RWMutex
)

// SetSSEPublisher sets the global SSE publisher used to publish activity events.
// It is safe to call multiple times; the most recently provided publisher replaces any previous one.
func SetSSEPublisher(pub SSEPublisher) {
	ssePublisherMu.Lock()
	defer ssePublisherMu.Unlock()
	ssePublisher = pub
}

// getSSEPublisher returns the current SSE publisher (may be nil).
func getSSEPublisher() SSEPublisher {
	ssePublisherMu.RLock()
	defer ssePublisherMu.RUnlock()
	return ssePublisher
}

// ActivityEvent is the event format sent to SSE clients for agent activity.
type ActivityEvent struct {
	// Operation is the operation type (e.g., "agent.spawn", "hook.execute").
	Operation string `json:"operation"`

	// Command is the skill/hook name (e.g., "code/semantic_search").
	Command string `json:"command,omitempty"`

	// Status is the outcome (ok, error, canceled).
	Status string `json:"status"`

	// Component is the originating system (cli, web, hook, skill).
	Component string `json:"component,omitempty"`

	// TraceID is the trace correlation ID.
	TraceID string `json:"trace_id,omitempty"`

	// SpanID is the span ID for this event.
	SpanID string `json:"span_id,omitempty"`

	// ParentID is the parent span ID.
	ParentID string `json:"parent_id,omitempty"`

	// Service is the emitting service name.
	Service string `json:"service,omitempty"`

	// Version is the build version.
	Version string `json:"version,omitempty"`

	// Subtype is an additional classification.
	Subtype string `json:"subtype,omitempty"`

	// SessionID is the agent session ID (if applicable).
	SessionID string `json:"session_id,omitempty"`

	// AgentID is the agent config ID (if applicable).
	AgentID string `json:"agent_id,omitempty"`

	// WorkspaceID is the workspace context.
	WorkspaceID string `json:"workspace_id,omitempty"`

	// JobID is the job ID for background tasks.
	JobID string `json:"job_id,omitempty"`

	// DurationMS is the operation duration in milliseconds.
	DurationMS int64 `json:"duration_ms,omitempty"`

	// ErrorType is the error category.
	ErrorType string `json:"error_type,omitempty"`

	// ErrorCode is the machine-readable error code.
	ErrorCode string `json:"error_code,omitempty"`

	// ErrorMessage is the human-readable error message.
	ErrorMessage string `json:"error_message,omitempty"`

	// Retriable indicates if the error is retriable.
	Retriable *bool `json:"retriable,omitempty"`

	// Timestamp is when the event occurred.
	Timestamp string `json:"ts"`

	// Data contains operation-specific details.
	Data map[string]any `json:"data,omitempty"`
}

// sseActivityPrefixes are the operation prefixes to forward to SSE.
// We forward user-facing activity events to the GUI.
var sseActivityPrefixes = []string{
	"agent.",
	"hook.",
	"skill.",
	"session.",
	"job.",
	"embedding.",
	"index.",
	"rerank.",
	"codemap.",
	"memory.",
	"orchestration.",
	"v2.runtime.enricher.",
	"v2.runtime.maintenance.",
	"web.",
}

var sseActivityOperations = map[string]struct{}{
	OpContextSemanticArtifactSearch: {},
	OpContextLayeredBundle:          {},
}

// shouldPublishToSSE reports whether a WideEvent should be forwarded to SSE clients.
func shouldPublishToSSE(event *WideEvent) bool {
	if event == nil {
		return false
	}

	op := event.Operation
	if _, ok := sseActivityOperations[op]; ok {
		return true
	}
	for _, prefix := range sseActivityPrefixes {
		if strings.HasPrefix(op, prefix) {
			return true
		}
	}
	return false
}

// publishToSSE publishes a WideEvent to the configured SSE publisher as an ActivityEvent.
//
// Index:
// - Purpose: Stream eligible WideEvents to SSE clients as activity updates
// - Flow: resolve publisher → filter by operation prefix → map to ActivityEvent → publish
// - SideEffects: publishes SSE activity events
// - FailureModes: no-op when publisher is nil or event is ineligible
// - Related: shouldPublishToSSE, extractActivityData
// - Keywords: sse, activity, wide_event, publish, operation_prefix
func publishToSSE(event *WideEvent) {
	pub := getSSEPublisher()
	if pub == nil {
		return
	}

	if !shouldPublishToSSE(event) {
		return
	}

	// Convert WideEvent to ActivityEvent (smaller, focused payload)
	activity := ActivityEvent{
		Operation:    event.Operation,
		Command:      event.Command,
		Status:       string(event.Status),
		Component:    event.Component,
		TraceID:      event.TraceID,
		SpanID:       event.SpanID,
		ParentID:     event.ParentID,
		Service:      event.Service,
		Version:      event.Version,
		Subtype:      event.Subtype,
		SessionID:    event.SessionID,
		AgentID:      event.AgentID,
		WorkspaceID:  event.WorkspaceID,
		JobID:        event.JobID,
		DurationMS:   event.DurationMS,
		ErrorType:    event.ErrorType,
		ErrorCode:    event.ErrorCode,
		ErrorMessage: event.ErrorMessage,
		Retriable:    event.Retriable,
		Timestamp:    event.Ts.Format("2006-01-02T15:04:05.000Z07:00"),
		Data:         extractActivityData(event),
	}

	pub.Publish("activity", activity)
}

// extractActivityData builds a filtered map of user-facing fields from a WideEvent's Data.
// It selects a curated set of keys and returns nil if no relevant fields are present.
// If event.ErrorMessage is non-empty it sets or overrides the "error" key with that message.
func extractActivityData(event *WideEvent) map[string]any {
	if event.Data == nil {
		return nil
	}

	// Copy selected fields that are useful for the UI
	result := make(map[string]any)

	// Common useful fields - include LLM stats, tool info, and operational data
	copyIfPresent := []string{
		// Agent/LLM fields
		"role",
		"model",
		"provider",
		"prompt_tokens",
		"completion_tokens",
		"total_tokens",
		"iteration",
		"iterations",
		"message_count",
		"finish_reason",
		"tool_calls",
		"tool_name",
		// Hook fields
		"hook_name",
		"hook_type",
		"hook_names",
		"hooks_run",
		"blocked",
		"blocked_by",
		"event",
		"tool_kind",
		// Skill fields
		"skill_name",
		"skill_command",
		"skill_version",
		// Index/embedding fields
		"scope",
		"count",
		"batch_size",
		"files_processed",
		"symbols_processed",
		"dimensions",
		"texts_count",
		"tokens_actual",
		"tokens_estimated",
		"cost_usd",
		// Context/semantic retrieval fields
		"search_path",
		"vector_capability",
		"hit_count",
		"hit_bucket",
		"latency_bucket",
		"artifact_types",
		"min_similarity",
		"limit",
		"query_dims",
		"working_context_applied",
		"working_context_fallback_level",
		"working_context_eligible_count",
		"artifact_search_path",
		"artifact_vector_capability",
		"artifact_hit_count",
		"artifact_search_error",
		// Context bundle refs
		"refs",
		"turn_refs",
		"slice_refs",
		"episode_refs",
		"narrative_refs",
		"artifact_refs",
		"expandable_refs",
		"ref_count",
		"turn_ref_count",
		"slice_ref_count",
		"episode_ref_count",
		"narrative_ref_count",
		"artifact_ref_count",
		// Orchestration fields
		"request_id",
		"issue_id",
		"issue_identifier",
		"lane",
		"last_outcome",
		"policy_status",
		"eligibility",
		"coalesced",
		"queued",
		"idempotent",
		"card_count",
		"lane_filter",
		// Skill input fields (from enrichSpanWithInput)
		"input_query",
		"input_scope",
		"input_scopes",
		"input_limit",
		"input_path",
		"input_paths",
		"input_paths_count",
		"input_pattern",
		"input_format",
		"input_action",
		"input_name",
		"input_type",
		"input_pr",
		"input_branch",
		"input_file",
		"input_files_count",
		"input_symbol",
		"input_symbols_count",
		"input_prompt",
		"input_model",
		"input_errors_only",
		"input_since",
		// Caller context
		"caller",
		"caller_file",
		"caller_path",
		"caller_line",
		"caller_func",
		// Error/result fields
		"error_message",
		"error",
		"result",
		"prompt",
	}

	for _, key := range copyIfPresent {
		if v, ok := event.Data[key]; ok {
			result[key] = v
		}
	}

	// Include error info if present
	if event.ErrorMessage != "" {
		result["error"] = event.ErrorMessage
	}

	if len(result) == 0 {
		return nil
	}

	return result
}
