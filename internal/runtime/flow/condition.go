package flow

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Condition types
// ---------------------------------------------------------------------------

// Condition evaluates whether a NodeOutput should pass an edge filter.
type Condition interface {
	// Eval returns true if the output satisfies the condition.
	Eval(out NodeOutput) bool
	// String returns the canonical text form of the condition.
	String() string
}

// AlwaysCondition passes every output. Used for empty condition strings.
var AlwaysCondition Condition = &alwaysCond{}

// NeverCondition passes no output. Useful as a sentinel.
var NeverCondition Condition = &neverCond{}

type alwaysCond struct{}

func (c *alwaysCond) Eval(_ NodeOutput) bool { return true }
func (c *alwaysCond) String() string         { return "" }

type neverCond struct{}

func (c *neverCond) Eval(_ NodeOutput) bool { return false }
func (c *neverCond) String() string         { return "never" }

// ---------------------------------------------------------------------------
// Comparison condition
// ---------------------------------------------------------------------------

// cmpOp represents a comparison operator.
type cmpOp int

const (
	cmpEq cmpOp = iota // ==
	cmpNe              // !=
	cmpGt              // >
	cmpLt              // <
)

func (op cmpOp) String() string {
	switch op {
	case cmpEq:
		return "=="
	case cmpNe:
		return "!="
	case cmpGt:
		return ">"
	case cmpLt:
		return "<"
	default:
		return "?"
	}
}

// cmpCond is a field comparison: field op value.
type cmpCond struct {
	field string
	op    cmpOp
	value string // raw string value
}

func (c *cmpCond) Eval(out NodeOutput) bool {
	actual := resolveField(c.field, out)
	switch c.op {
	case cmpEq:
		return actual == c.value
	case cmpNe:
		return actual != c.value
	case cmpGt:
		return numericCompare(actual, c.value, func(a, b float64) bool { return a > b })
	case cmpLt:
		return numericCompare(actual, c.value, func(a, b float64) bool { return a < b })
	default:
		return false
	}
}

func (c *cmpCond) String() string {
	return fmt.Sprintf("%s %s %s", c.field, c.op, c.value)
}

// ---------------------------------------------------------------------------
// Contains condition
// ---------------------------------------------------------------------------

// containsCond checks whether the serialized output contains a substring.
type containsCond struct {
	substr string
}

func (c *containsCond) Eval(out NodeOutput) bool {
	serialized := serializeOutput(out)
	return strings.Contains(serialized, c.substr)
}

func (c *containsCond) String() string {
	return fmt.Sprintf("output_contains:%s", c.substr)
}

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

// ParseCondition parses a condition expression string into a Condition.
// An empty string (or whitespace-only) returns AlwaysCondition.
// Returns an error for malformed expressions.
//
// Supported syntax:
//
//	field == value    string equality
//	field != value    string inequality
//	field > value     numeric comparison
//	field < value     numeric comparison
//	output_contains:substring  substring check
//	data.x.y == value nested data field access
//
// Known fields: status, output_len, exit_code, duration, data.*
func ParseCondition(expr string) (Condition, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return AlwaysCondition, nil
	}

	// Check for contains: operator first (no spaces around the colon).
	if strings.HasPrefix(expr, "output_contains:") {
		substr := strings.TrimPrefix(expr, "output_contains:")
		if substr == "" {
			return nil, fmt.Errorf("condition: output_contains requires a substring after ':'")
		}
		return &containsCond{substr: substr}, nil
	}

	// Try to split on comparison operators (order matters: != before ==, longest first).
	for _, op := range []cmpOp{cmpNe, cmpEq, cmpGt, cmpLt} {
		opStr := " " + op.String() + " "
		idx := strings.Index(expr, opStr)
		if idx >= 0 {
			field := strings.TrimSpace(expr[:idx])
			value := strings.TrimSpace(expr[idx+len(opStr):])
			if field == "" {
				return nil, fmt.Errorf("condition: missing field before %s", op)
			}
			if value == "" {
				return nil, fmt.Errorf("condition: missing value after %s", op)
			}
			return &cmpCond{field: field, op: op, value: value}, nil
		}
	}

	return nil, fmt.Errorf("condition: cannot parse %q", expr)
}

// ---------------------------------------------------------------------------
// Field resolution
// ---------------------------------------------------------------------------

// resolveField extracts the string value of a named field from a NodeOutput.
// Known top-level fields: status, output_len, exit_code, duration.
// Fields prefixed with "data." navigate into envelope.Data as a map.
func resolveField(field string, out NodeOutput) string {
	switch {
	case field == "status":
		return out.Envelope.Status
	case field == "output_len":
		return strconv.Itoa(computeOutputLen(out))
	case field == "exit_code":
		return resolveExitCode(out)
	case field == "duration":
		// Duration in milliseconds for numeric comparison.
		return strconv.FormatInt(out.Duration.Milliseconds(), 10)
	case strings.HasPrefix(field, "data."):
		path := strings.TrimPrefix(field, "data.")
		return resolveDataPath(path, out.Envelope.Data)
	default:
		return ""
	}
}

// computeOutputLen returns the byte length of the serialized envelope data.
func computeOutputLen(out NodeOutput) int {
	if out.Envelope.Data == nil {
		return 0
	}
	b, err := json.Marshal(out.Envelope.Data)
	if err != nil {
		return 0
	}
	return len(b)
}

// resolveExitCode looks for an "exit_code" field in envelope data.
func resolveExitCode(out NodeOutput) string {
	m, ok := toMap(out.Envelope.Data)
	if !ok {
		return ""
	}
	v, ok := m["exit_code"]
	if !ok {
		return ""
	}
	// JSON numbers unmarshal as float64.
	switch n := v.(type) {
	case float64:
		if n == float64(int(n)) {
			return strconv.Itoa(int(n))
		}
		return strconv.FormatFloat(n, 'f', -1, 64)
	case json.Number:
		return n.String()
	case string:
		return n
	default:
		return fmt.Sprintf("%v", v)
	}
}

// resolveDataPath navigates into a nested map using dot-separated keys
// and numeric indices.
func resolveDataPath(path string, data any) string {
	parts := splitDataPath(path)
	current := data
	for _, part := range parts {
		if current == nil {
			return ""
		}
		// Try numeric index into array/slice.
		if idx, err := strconv.Atoi(part); err == nil {
			arr, ok := toSlice(current)
			if !ok || idx < 0 || idx >= len(arr) {
				return ""
			}
			current = arr[idx]
			continue
		}
		// Map key navigation.
		m, ok := toMap(current)
		if !ok {
			return ""
		}
		v, exists := m[part]
		if !exists {
			return ""
		}
		current = v
	}
	return valueToString(current)
}

// splitDataPath splits "a.b.0.c" into ["a", "b", "0", "c"].
func splitDataPath(path string) []string {
	return strings.Split(path, ".")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// numericCompare compares two values as floats. Returns false if either
// value cannot be parsed as a number.
func numericCompare(actual, expected string, cmp func(a, b float64) bool) bool {
	a, err := strconv.ParseFloat(actual, 64)
	if err != nil {
		return false
	}
	b, err := strconv.ParseFloat(expected, 64)
	if err != nil {
		return false
	}
	return cmp(a, b)
}

// serializeOutput serializes the envelope data to a string for contains checks.
// This serializes the data payload (not the envelope wrapper) so that
// envelope structural keys like "error" don't interfere with content matching.
func serializeOutput(out NodeOutput) string {
	if out.Envelope.Data == nil {
		return ""
	}
	switch v := out.Envelope.Data.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

// toMap converts an any to map[string]any. Handles both map[string]any
// and map[string]interface{} from JSON unmarshaling.
func toMap(v any) (map[string]any, bool) {
	if v == nil {
		return nil, false
	}
	m, ok := v.(map[string]any)
	return m, ok
}

// toSlice converts an any to []any.
func toSlice(v any) ([]any, bool) {
	if v == nil {
		return nil, false
	}
	s, ok := v.([]any)
	return s, ok
}

// valueToString converts a resolved value to its string representation.
func valueToString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == float64(int(val)) {
			return strconv.Itoa(int(val))
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	case nil:
		return ""
	case time.Duration:
		return strconv.FormatInt(val.Milliseconds(), 10)
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(b)
	}
}
