package run

// IterationRecord captures one model iteration within a turn.
type IterationRecord struct {
	TurnID         string           `json:"turn_id"`
	IterationIndex int              `json:"iteration_index"`
	TraceID        string           `json:"trace_id,omitempty"`
	SpanID         string           `json:"span_id,omitempty"`
	ParentSpanID   string           `json:"parent_span_id,omitempty"`
	Message        MessageRef       `json:"message,omitempty"`
	ToolCalls      []ToolCallRecord `json:"tool_calls,omitempty"`
}

// Clone returns a deep copy.
func (r IterationRecord) Clone() IterationRecord {
	out := r
	if len(r.ToolCalls) > 0 {
		out.ToolCalls = make([]ToolCallRecord, len(r.ToolCalls))
		for i := range r.ToolCalls {
			out.ToolCalls[i] = r.ToolCalls[i].Clone()
		}
	}
	return out
}
