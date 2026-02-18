package run

import "encoding/json"

// ArtifactRef points to derived content associated with a tool call.
type ArtifactRef struct {
	ID   string `json:"id,omitempty"`
	Kind string `json:"kind,omitempty"`
	Text string `json:"text,omitempty"`
}

// ToolCallRecord captures one tool invocation and result within an iteration.
type ToolCallRecord struct {
	CallID         string          `json:"call_id"`
	IterationIndex int             `json:"iteration_index"`
	TraceID        string          `json:"trace_id,omitempty"`
	SpanID         string          `json:"span_id,omitempty"`
	ParentSpanID   string          `json:"parent_span_id,omitempty"`
	Name           string          `json:"name"`
	ArgsJSON       json.RawMessage `json:"args_json,omitempty"`
	Status         string          `json:"status,omitempty"`
	ResultRef      ArtifactRef     `json:"result_ref,omitempty"`
}

// Clone returns a deep copy.
func (r ToolCallRecord) Clone() ToolCallRecord {
	out := r
	if len(r.ArgsJSON) > 0 {
		out.ArgsJSON = append(json.RawMessage(nil), r.ArgsJSON...)
	}
	return out
}
