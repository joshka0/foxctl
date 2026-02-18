package contextbuilder

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	// ErrInvalidRef indicates an unsupported or malformed context reference.
	ErrInvalidRef = errors.New("v2 contextbuilder: invalid reference")
	// ErrInvalidSlice indicates an invalid message slice window.
	ErrInvalidSlice = errors.New("v2 contextbuilder: invalid slice window")
)

var (
	reWhole = regexp.MustCompile(`^turn/([^/#]+)$`)
	reIter  = regexp.MustCompile(`^turn/([^/#]+)/iter/([0-9]+)$`)
	reTool  = regexp.MustCompile(`^turn/([^/#]+)/iter/([0-9]+)/tool/([^/#]+)$`)
	reSlice = regexp.MustCompile(`^turn/([^#]+)#msg:([^:]+):([0-9]+)-([0-9]+)$`)
)

// RefKind is the parsed reference type.
type RefKind string

const (
	RefWholeTurn RefKind = "turn"
	RefIteration RefKind = "iteration"
	RefToolCall  RefKind = "tool_call"
	RefSlice     RefKind = "slice"
)

// Ref is a parsed turn reference.
type Ref struct {
	Raw string

	Kind RefKind

	TurnID string

	IterationIndex int
	ToolCallID     string

	MessageID string
	Start     int
	End       int
}

// ParseRef parses canonical turn reference formats.
func ParseRef(raw string) (Ref, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Ref{}, ErrInvalidRef
	}

	if m := reSlice.FindStringSubmatch(raw); len(m) == 5 {
		start, err := strconv.Atoi(m[3])
		if err != nil {
			return Ref{}, fmt.Errorf("%w: start", ErrInvalidRef)
		}
		end, err := strconv.Atoi(m[4])
		if err != nil {
			return Ref{}, fmt.Errorf("%w: end", ErrInvalidRef)
		}
		if start < 0 || end < start {
			return Ref{}, ErrInvalidSlice
		}
		return Ref{
			Raw:       raw,
			Kind:      RefSlice,
			TurnID:    strings.TrimSpace(m[1]),
			MessageID: strings.TrimSpace(m[2]),
			Start:     start,
			End:       end,
		}, nil
	}

	if m := reTool.FindStringSubmatch(raw); len(m) == 4 {
		iter, err := strconv.Atoi(m[2])
		if err != nil || iter < 0 {
			return Ref{}, ErrInvalidRef
		}
		return Ref{
			Raw:            raw,
			Kind:           RefToolCall,
			TurnID:         strings.TrimSpace(m[1]),
			IterationIndex: iter,
			ToolCallID:     strings.TrimSpace(m[3]),
		}, nil
	}

	if m := reIter.FindStringSubmatch(raw); len(m) == 3 {
		iter, err := strconv.Atoi(m[2])
		if err != nil || iter < 0 {
			return Ref{}, ErrInvalidRef
		}
		return Ref{
			Raw:            raw,
			Kind:           RefIteration,
			TurnID:         strings.TrimSpace(m[1]),
			IterationIndex: iter,
		}, nil
	}

	if m := reWhole.FindStringSubmatch(raw); len(m) == 2 {
		return Ref{
			Raw:    raw,
			Kind:   RefWholeTurn,
			TurnID: strings.TrimSpace(m[1]),
		}, nil
	}

	return Ref{}, ErrInvalidRef
}
