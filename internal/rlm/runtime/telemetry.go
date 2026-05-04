package runtime

import (
	"context"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/observability"
)

const (
	OpRLMParentLLMCall     = "rlm.parent_llm_call"
	OpRLMREPLCall          = "rlm.repl_call"
	OpRLMREPLResult        = "rlm.repl_result"
	OpRLMSubcallStart      = "rlm.subcall_start"
	OpRLMSubcallEnd        = "rlm.subcall_end"
	OpRLMNodeQueued        = "rlm.node_queued"
	OpRLMNodeStarted       = "rlm.node_started"
	OpRLMNodeWaitStarted   = "rlm.node_wait_started"
	OpRLMNodeWaitCompleted = "rlm.node_wait_completed"
	OpRLMNodeCompleted     = "rlm.node_completed"
	OpRLMNodeFailed        = "rlm.node_failed"
	OpRLMNodeCanceled      = "rlm.node_canceled"
	OpRLMBudget            = "rlm.budget"
	OpRLMBraid             = "rlm.braid"
	OpRLMContract          = "rlm.contract"
	OpRLMFinalAnswer       = "rlm.final_answer"
	OpRLMError             = "rlm.error"
)

// ObservabilityTelemetrySink emits RLM events into foxctl wide-event
// observability. It intentionally avoids raw prompt/answer/code payloads.
type ObservabilityTelemetrySink struct {
	SessionID   string
	AgentID     string
	WorkspaceID string
	Command     string
}

func (s ObservabilityTelemetrySink) EmitRLMEvent(ctx context.Context, event Event) {
	builder := observability.NewEvent(operationForRLMEvent(event.Type)).
		WithComponent(observability.ComponentAgent).
		WithCommand(s.Command).
		WithSession(s.SessionID, s.AgentID).
		WithWorkspace(s.WorkspaceID).
		WithData("always_sample", true).
		WithData("seq", event.Seq).
		WithData("event_type", string(event.Type))

	switch event.Type {
	case EventTypeParentLLMCall:
		if payload := event.ParentLLMCall; payload != nil {
			builder.WithData("call_id", payload.CallID).
				WithData("model", payload.Model).
				WithData("prompt_tokens", payload.PromptTokens).
				WithData("completion_tokens", payload.CompletionTokens).
				WithData("finish_reason", payload.FinishReason).
				WithData("tool_calls", payload.ToolCalls).
				WithData("tool_names", append([]string(nil), payload.ToolNames...))
		}
	case EventTypeREPLCall:
		if payload := event.REPLCall; payload != nil {
			builder.WithData("call_id", payload.CallID).
				WithData("input_chars", len(payload.Input))
		}
	case EventTypeREPLResult:
		if payload := event.REPLResult; payload != nil {
			builder.WithData("call_id", payload.CallID).
				WithData("success", payload.Success).
				WithData("duration_ms", payload.DurationMS).
				WithData("output_chars", len(payload.Output))
		}
	case EventTypeSubcallStart, EventTypeSubcallEnd:
		if payload := event.Subcall; payload != nil {
			builder.WithData("call_id", payload.CallID).
				WithData("name", payload.Name).
				WithData("depth", payload.Depth).
				WithData("agent_id", payload.AgentID).
				WithData("parent_agent_id", payload.ParentAgentID).
				WithData("output_namespace", payload.OutputNamespace)
		}
	case EventTypeNodeQueued, EventTypeNodeStarted, EventTypeNodeCompleted, EventTypeNodeFailed, EventTypeNodeCanceled:
		if payload := event.Node; payload != nil {
			builder.WithData("run_id", payload.RunID).
				WithData("node_id", payload.NodeID).
				WithData("parent_node_id", payload.ParentNodeID).
				WithData("depth", payload.Depth).
				WithData("status", string(payload.Status)).
				WithData("output_namespace", payload.OutputNamespace).
				WithData("message", payload.Message).
				WithData("required_subcalls", payload.RequiredSubcalls).
				WithData("required_subcall_attempts", payload.RequiredSubcallAttempts).
				WithData("recursive_subcalls_used", payload.RecursiveSubcallsUsed)
		}
	case EventTypeNodeWaitStarted, EventTypeNodeWaitCompleted:
		if payload := event.Wait; payload != nil {
			builder.WithData("run_id", payload.RunID).
				WithData("parent_node_id", payload.ParentNodeID).
				WithData("child_ids", append([]string(nil), payload.ChildIDs...)).
				WithData("completed", payload.Completed).
				WithData("failed", payload.Failed).
				WithData("pending", payload.Pending).
				WithData("min_complete", payload.MinComplete).
				WithData("timeout_ms", payload.TimeoutMS)
		}
	case EventTypeBudget:
		if payload := event.Budget; payload != nil {
			builder.WithData("limit", string(payload.Limit)).
				WithData("used", payload.Used).
				WithData("max", payload.Max).
				WithData("message", payload.Message)
		}
	case EventTypeBraid:
		if payload := event.Braid; payload != nil {
			builder.WithData("phase", payload.Phase).
				WithData("status", payload.Status).
				WithData("wave", payload.Wave).
				WithData("node_id", payload.NodeID).
				WithData("kind", payload.Kind).
				WithData("final_node", payload.FinalNode).
				WithData("node_count", payload.NodeCount).
				WithData("message", payload.Message)
		}
	case EventTypeContract:
		if payload := event.Contract; payload != nil {
			builder.WithData("boundary", payload.Boundary).
				WithData("phase", payload.Phase).
				WithData("tool", payload.Tool).
				WithData("status", payload.Status).
				WithData("issue_kind", payload.IssueKind).
				WithData("issue_path", payload.IssuePath).
				WithData("repair_rule", payload.RepairRule).
				WithData("revalidate_ok", payload.RevalidateOK).
				WithData("message", payload.Message).
				WithData("candidate_solved", payload.CandidateSolved).
				WithData("candidate_blocked", payload.CandidateBlocked).
				WithData("candidate_partial", payload.CandidatePartial).
				WithData("candidate_placeholder", payload.CandidatePlaceholder).
				WithData("candidate_failed", payload.CandidateFailed).
				WithData("candidate_pending", payload.CandidatePending).
				WithData("candidate_registered", payload.CandidateRegistered).
				WithData("candidate_rejected", payload.CandidateRejected).
				WithData("assistant_chars", payload.AssistantChars).
				WithData("executed_output_chars", payload.ExecutedOutputChars).
				WithData("tool_input_bytes", payload.ToolInputBytes).
				WithData("repaired_input_bytes", payload.RepairedInputBytes)
		}
	case EventTypeFinalAnswer:
		if payload := event.FinalAnswer; payload != nil {
			builder.WithData("answer_chars", len(payload.Text)).
				WithData("tokens", payload.Tokens)
		}
	case EventTypeError:
		if payload := event.RuntimeError; payload != nil {
			builder.WithData("code", payload.Code).
				WithData("message", payload.Message)
			if payload.RawChars > 0 {
				builder.WithData("raw_chars", payload.RawChars)
			}
			if payload.SanitizedChars > 0 {
				builder.WithData("sanitized_chars", payload.SanitizedChars)
			}
			if payload.RawExcerpt != "" {
				builder.WithData("raw_excerpt", payload.RawExcerpt)
			}
			if payload.SanitizedExcerpt != "" {
				builder.WithData("sanitized_excerpt", payload.SanitizedExcerpt)
			}
			if len(payload.Artifacts) > 0 {
				builder.WithData("artifacts", append([]string(nil), payload.Artifacts...))
			}
		}
		observability.Emit(ctx, builder.Error(nil, 0))
		return
	}
	observability.Emit(ctx, builder.Success(0*time.Millisecond))
}

func operationForRLMEvent(eventType EventType) string {
	switch eventType {
	case EventTypeParentLLMCall:
		return OpRLMParentLLMCall
	case EventTypeREPLCall:
		return OpRLMREPLCall
	case EventTypeREPLResult:
		return OpRLMREPLResult
	case EventTypeSubcallStart:
		return OpRLMSubcallStart
	case EventTypeSubcallEnd:
		return OpRLMSubcallEnd
	case EventTypeNodeQueued:
		return OpRLMNodeQueued
	case EventTypeNodeStarted:
		return OpRLMNodeStarted
	case EventTypeNodeWaitStarted:
		return OpRLMNodeWaitStarted
	case EventTypeNodeWaitCompleted:
		return OpRLMNodeWaitCompleted
	case EventTypeNodeCompleted:
		return OpRLMNodeCompleted
	case EventTypeNodeFailed:
		return OpRLMNodeFailed
	case EventTypeNodeCanceled:
		return OpRLMNodeCanceled
	case EventTypeBudget:
		return OpRLMBudget
	case EventTypeBraid:
		return OpRLMBraid
	case EventTypeContract:
		return OpRLMContract
	case EventTypeFinalAnswer:
		return OpRLMFinalAnswer
	case EventTypeError:
		return OpRLMError
	default:
		return "rlm.event"
	}
}
