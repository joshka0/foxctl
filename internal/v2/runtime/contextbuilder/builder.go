package contextbuilder

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/v2/core/run"
)

var (
	// ErrMissingReader indicates the builder was configured without turn source.
	ErrMissingReader = errors.New("v2 contextbuilder: missing turn reader")
	// ErrMessageNotFound indicates a requested message ID is not present in the turn.
	ErrMessageNotFound = errors.New("v2 contextbuilder: message not found")
)

// Request is one context assembly request.
type Request struct {
	Ref string
}

// Bundle is assembled context from one reference.
type Bundle struct {
	Ref     string         `json:"ref"`
	TurnID  string         `json:"turn_id"`
	Kind    RefKind        `json:"kind"`
	Content string         `json:"content"`
	Meta    map[string]any `json:"meta,omitempty"`
}

// Builder resolves turn references to context bundles.
type Builder struct {
	reader run.TurnReader
}

// New creates a context builder.
func New(reader run.TurnReader) *Builder {
	return &Builder{reader: reader}
}

// Build resolves one reference into deterministic context content.
func (b *Builder) Build(ctx context.Context, req Request) (Bundle, error) {
	if b == nil || b.reader == nil {
		return Bundle{}, ErrMissingReader
	}
	parsed, err := ParseRef(req.Ref)
	if err != nil {
		return Bundle{}, err
	}

	turn, err := b.reader.GetTurn(ctx, parsed.TurnID)
	if err != nil {
		return Bundle{}, err
	}
	turn = turn.Clone()

	content, meta, err := b.resolve(parsed, turn)
	if err != nil {
		return Bundle{}, err
	}

	return Bundle{
		Ref:     parsed.Raw,
		TurnID:  parsed.TurnID,
		Kind:    parsed.Kind,
		Content: content,
		Meta:    meta,
	}, nil
}

func (b *Builder) resolve(parsed Ref, turn run.TurnRecord) (string, map[string]any, error) {
	switch parsed.Kind {
	case RefWholeTurn:
		return renderWholeTurn(turn), map[string]any{
			"iterations": len(turn.Iterations),
			"tool_calls": countToolCalls(turn),
		}, nil
	case RefIteration:
		iter, ok := findIteration(turn, parsed.IterationIndex)
		if !ok {
			return "", nil, run.ErrTurnNotFound
		}
		return renderIteration(iter), map[string]any{
			"iteration_index": iter.IterationIndex,
			"tool_calls":      len(iter.ToolCalls),
		}, nil
	case RefToolCall:
		iter, ok := findIteration(turn, parsed.IterationIndex)
		if !ok {
			return "", nil, run.ErrTurnNotFound
		}
		call, ok := findToolCall(iter, parsed.ToolCallID)
		if !ok {
			return "", nil, run.ErrTurnNotFound
		}
		return renderToolCall(call), map[string]any{
			"iteration_index": iter.IterationIndex,
			"tool_call_id":    call.CallID,
		}, nil
	case RefSlice:
		msg, ok := findMessage(turn, parsed.MessageID)
		if !ok {
			return "", nil, ErrMessageNotFound
		}
		slice, err := sliceMessage(msg.Text, parsed.Start, parsed.End)
		if err != nil {
			return "", nil, err
		}
		return slice, map[string]any{
			"message_id": parsed.MessageID,
			"start":      parsed.Start,
			"end":        parsed.End,
		}, nil
	default:
		return "", nil, ErrInvalidRef
	}
}

func renderWholeTurn(turn run.TurnRecord) string {
	var sb strings.Builder
	if strings.TrimSpace(turn.Prompt) != "" {
		sb.WriteString("Prompt: ")
		sb.WriteString(strings.TrimSpace(turn.Prompt))
		sb.WriteString("\n")
	}
	for _, iter := range turn.Iterations {
		sb.WriteString(renderIteration(iter))
		sb.WriteString("\n")
	}
	if strings.TrimSpace(turn.FinalOutput.Text) != "" {
		sb.WriteString("Final: ")
		sb.WriteString(strings.TrimSpace(turn.FinalOutput.Text))
	}
	return strings.TrimSpace(sb.String())
}

func renderIteration(iter run.IterationRecord) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Iteration %d", iter.IterationIndex))
	if strings.TrimSpace(iter.Message.Text) != "" {
		sb.WriteString(": ")
		sb.WriteString(strings.TrimSpace(iter.Message.Text))
	}
	for _, call := range iter.ToolCalls {
		sb.WriteString("\n")
		sb.WriteString(renderToolCall(call))
	}
	return sb.String()
}

func renderToolCall(call run.ToolCallRecord) string {
	text := strings.TrimSpace(call.ResultRef.Text)
	if text == "" {
		text = "<empty>"
	}
	return fmt.Sprintf("Tool %s (%s): %s", call.Name, call.CallID, text)
}

func countToolCalls(turn run.TurnRecord) int {
	var total int
	for _, iter := range turn.Iterations {
		total += len(iter.ToolCalls)
	}
	return total
}

func findIteration(turn run.TurnRecord, index int) (run.IterationRecord, bool) {
	for _, iter := range turn.Iterations {
		if iter.IterationIndex == index {
			return iter, true
		}
	}
	return run.IterationRecord{}, false
}

func findToolCall(iter run.IterationRecord, callID string) (run.ToolCallRecord, bool) {
	for _, call := range iter.ToolCalls {
		if call.CallID == callID {
			return call, true
		}
	}
	return run.ToolCallRecord{}, false
}

func findMessage(turn run.TurnRecord, msgID string) (run.MessageRef, bool) {
	msgID = strings.TrimSpace(msgID)
	if msgID == "" {
		return run.MessageRef{}, false
	}

	if turn.FinalOutput.ID == msgID {
		return turn.FinalOutput, true
	}
	for _, iter := range turn.Iterations {
		if iter.Message.ID == msgID {
			return iter.Message, true
		}
		for _, call := range iter.ToolCalls {
			if call.ResultRef.ID == msgID {
				return run.MessageRef{
					ID:   call.ResultRef.ID,
					Role: "tool",
					Text: call.ResultRef.Text,
				}, true
			}
		}
	}
	return run.MessageRef{}, false
}

func sliceMessage(text string, start, end int) (string, error) {
	if start < 0 || end < start {
		return "", ErrInvalidSlice
	}
	runes := []rune(text)
	if start > len(runes) {
		return "", ErrInvalidSlice
	}
	if end > len(runes) {
		end = len(runes)
	}
	return string(runes[start:end]), nil
}
