package flow

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// okEnv builds a NodeOutput with the given status and data.
func okEnv(status string, data any) NodeOutput {
	return NodeOutput{
		Envelope: envelope.Envelope{
			Version: 1,
			Status:  status,
			Command: "test/node",
			Data:    data,
			Meta:    envelope.Meta{TS: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)},
		},
		Duration: 100 * time.Millisecond,
		NodeID:   "node-1",
	}
}

// envWithData creates a NodeOutput with status "ok" and the given JSON data.
func envWithData(t *testing.T, jsonData string) NodeOutput {
	t.Helper()
	var data any
	if jsonData != "" {
		if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
			t.Fatalf("invalid test json data: %v", err)
		}
	}
	return okEnv("ok", data)
}

// ---------------------------------------------------------------------------
// ParseCondition tests (VAL-M1-036)
// ---------------------------------------------------------------------------

func TestParseCondition(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Empty condition is valid and matches everything.
		{name: "empty string", input: "", wantErr: false},
		{name: "whitespace only", input: "   ", wantErr: false},

		// Valid == expressions.
		{name: "status == ok", input: "status == ok", wantErr: false},
		{name: "exit_code == 0", input: "exit_code == 0", wantErr: false},
		{name: "data.field == value", input: "data.status == ready", wantErr: false},
		{name: "data.nested.field == value", input: "data.a.b.c == deep", wantErr: false},

		// Valid != expressions.
		{name: "status != error", input: "status != error", wantErr: false},

		// Valid > and < expressions.
		{name: "output_len > 0", input: "output_len > 0", wantErr: false},
		{name: "output_len < 100", input: "output_len < 100", wantErr: false},
		{name: "exit_code > 1", input: "exit_code > 1", wantErr: false},

		// Valid contains: expressions.
		{name: "output_contains:error", input: "output_contains:error", wantErr: false},
		{name: "output_contains:hello world", input: "output_contains:hello world", wantErr: false},

		// Malformed conditions.
		{name: "double bang", input: "!!invalid", wantErr: true},
		{name: "only operator", input: "==", wantErr: true},
		{name: "missing value", input: "status ==", wantErr: true},
		{name: "unknown operator", input: "status ~= ok", wantErr: true},
		{name: "only field", input: "status", wantErr: true},
		{name: "random text", input: "foo bar baz", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseCondition(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseCondition(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Eval tests: == operator (VAL-M2-025)
// ---------------------------------------------------------------------------

func TestConditionEquals(t *testing.T) {
	cond, err := ParseCondition("status == ok")
	if err != nil {
		t.Fatalf("ParseCondition: %v", err)
	}

	if !cond.Eval(okEnv("ok", nil)) {
		t.Error("status == ok should pass for status=ok")
	}
	if cond.Eval(okEnv("error", nil)) {
		t.Error("status == ok should not pass for status=error")
	}
}

// ---------------------------------------------------------------------------
// Eval tests: != operator (VAL-M2-026)
// ---------------------------------------------------------------------------

func TestConditionNotEquals(t *testing.T) {
	cond, err := ParseCondition("status != error")
	if err != nil {
		t.Fatalf("ParseCondition: %v", err)
	}

	if !cond.Eval(okEnv("ok", nil)) {
		t.Error("status != error should pass for status=ok")
	}
	if !cond.Eval(okEnv("progress", nil)) {
		t.Error("status != error should pass for status=progress")
	}
	if cond.Eval(okEnv("error", nil)) {
		t.Error("status != error should not pass for status=error")
	}
}

// ---------------------------------------------------------------------------
// Eval tests: > and < operators (VAL-M2-027)
// ---------------------------------------------------------------------------

func TestConditionNumericComparison(t *testing.T) {
	tests := []struct {
		name string
		cond string
		data any // envelope data (nil for empty)
		want bool
	}{
		// output_len tests: output_len is computed from serialized data length.
		{"output_len > 0 with non-empty data", "output_len > 0", map[string]any{"key": "value"}, true},
		{"output_len > 0 with nil data", "output_len > 0", nil, false},
		{"output_len < 100 with small data", "output_len < 100", map[string]any{"a": "b"}, true},
		{"output_len > 100 with small data", "output_len > 100", map[string]any{"a": "b"}, false},

		// exit_code tests: exit_code comes from envelope.Data.exit_code field.
		{"exit_code == 0 with code 0", "exit_code == 0", map[string]any{"exit_code": float64(0)}, true},
		{"exit_code == 0 with code 1", "exit_code == 0", map[string]any{"exit_code": float64(1)}, false},
		{"exit_code > 0 with code 1", "exit_code > 0", map[string]any{"exit_code": float64(1)}, true},
		{"exit_code > 0 with code 0", "exit_code > 0", map[string]any{"exit_code": float64(0)}, false},
		{"exit_code == 0 with missing field", "exit_code == 0", map[string]any{"other": "value"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond, err := ParseCondition(tt.cond)
			if err != nil {
				t.Fatalf("ParseCondition(%q): %v", tt.cond, err)
			}
			out := NodeOutput{
				Envelope: envelope.Envelope{
					Version: 1,
					Status:  "ok",
					Command: "test/node",
					Data:    tt.data,
					Meta:    envelope.Meta{TS: "2026-01-01T00:00:00Z"},
				},
				Duration: 100 * time.Millisecond,
				NodeID:   "node-1",
			}
			got := cond.Eval(out)
			if got != tt.want {
				t.Errorf("Eval(%q) = %v, want %v", tt.cond, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Eval tests: contains: operator (VAL-M2-028)
// ---------------------------------------------------------------------------

func TestConditionContains(t *testing.T) {
	cond, err := ParseCondition("output_contains:error")
	if err != nil {
		t.Fatalf("ParseCondition: %v", err)
	}

	// Output data contains "error" as a value.
	outWithErr := okEnv("ok", map[string]any{"message": "an error occurred"})
	if !cond.Eval(outWithErr) {
		t.Error("output_contains:error should pass when data contains 'error'")
	}

	// Output data does not contain "error".
	outNoErr := okEnv("ok", map[string]any{"message": "all good"})
	if cond.Eval(outNoErr) {
		t.Error("output_contains:error should not pass when data does not contain 'error'")
	}

	// Case-sensitive: "ERROR" does not match "error".
	outUpper := okEnv("ok", map[string]any{"message": "ERROR found"})
	if cond.Eval(outUpper) {
		t.Error("output_contains:error should be case-sensitive, 'ERROR' != 'error'")
	}

	// nil data does not contain anything.
	outNil := okEnv("ok", nil)
	if cond.Eval(outNil) {
		t.Error("output_contains:error should not pass with nil data")
	}
}

// ---------------------------------------------------------------------------
// Eval tests: data.field == value (VAL-M2-029)
// ---------------------------------------------------------------------------

func TestConditionDataField(t *testing.T) {
	tests := []struct {
		name     string
		cond     string
		dataJSON string
		want     bool
	}{
		{
			name:     "data.status == ready with matching",
			cond:     "data.status == ready",
			dataJSON: `{"status":"ready","other":1}`,
			want:     true,
		},
		{
			name:     "data.status == ready with different",
			cond:     "data.status == ready",
			dataJSON: `{"status":"pending"}`,
			want:     false,
		},
		{
			name:     "data.status == ready with missing field",
			cond:     "data.status == ready",
			dataJSON: `{"other":1}`,
			want:     false,
		},
		{
			name:     "data.items.0 == first",
			cond:     "data.items.0 == first",
			dataJSON: `{"items":["first","second"]}`,
			want:     true,
		},
		{
			name:     "data.nested.deep == value with matching",
			cond:     "data.nested.deep == value",
			dataJSON: `{"nested":{"deep":"value"}}`,
			want:     true,
		},
		{
			name:     "data.nested.deep == value with non-matching",
			cond:     "data.nested.deep == value",
			dataJSON: `{"nested":{"deep":"other"}}`,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond, err := ParseCondition(tt.cond)
			if err != nil {
				t.Fatalf("ParseCondition(%q): %v", tt.cond, err)
			}
			out := envWithData(t, tt.dataJSON)
			got := cond.Eval(out)
			if got != tt.want {
				t.Errorf("Eval(%q) with data %s = %v, want %v", tt.cond, tt.dataJSON, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Eval tests: empty condition (VAL-M2-030)
// ---------------------------------------------------------------------------

func TestConditionEmptyPassesEverything(t *testing.T) {
	cond, err := ParseCondition("")
	if err != nil {
		t.Fatalf("ParseCondition empty: %v", err)
	}

	if !cond.Eval(okEnv("ok", nil)) {
		t.Error("empty condition should pass status=ok")
	}
	if !cond.Eval(okEnv("error", nil)) {
		t.Error("empty condition should pass status=error")
	}
	if !cond.Eval(okEnv("progress", nil)) {
		t.Error("empty condition should pass status=progress")
	}
}

// ---------------------------------------------------------------------------
// Eval tests: whitespace-only condition
// ---------------------------------------------------------------------------

func TestConditionWhitespacePassesEverything(t *testing.T) {
	cond, err := ParseCondition("   ")
	if err != nil {
		t.Fatalf("ParseCondition whitespace: %v", err)
	}

	if !cond.Eval(okEnv("ok", nil)) {
		t.Error("whitespace condition should pass everything")
	}
}

// ---------------------------------------------------------------------------
// Eval tests: output_len computed from serialized data
// ---------------------------------------------------------------------------

func TestConditionOutputLen(t *testing.T) {
	cond, err := ParseCondition("output_len > 0")
	if err != nil {
		t.Fatalf("ParseCondition: %v", err)
	}

	// Non-empty data: output_len > 0 should be true.
	out := okEnv("ok", map[string]any{"key": "value"})
	if !cond.Eval(out) {
		t.Error("output_len > 0 should pass with non-empty data")
	}

	// nil data: output_len should be 0.
	outNil := okEnv("ok", nil)
	if cond.Eval(outNil) {
		t.Error("output_len > 0 should not pass with nil data")
	}
}

// ---------------------------------------------------------------------------
// Eval tests: exit_code from envelope data
// ---------------------------------------------------------------------------

func TestConditionExitCode(t *testing.T) {
	cond, err := ParseCondition("exit_code == 0")
	if err != nil {
		t.Fatalf("ParseCondition: %v", err)
	}

	// exit_code 0 in data.
	out := okEnv("ok", map[string]any{"exit_code": float64(0)})
	if !cond.Eval(out) {
		t.Error("exit_code == 0 should pass with exit_code=0")
	}

	// exit_code 1 in data.
	out1 := okEnv("ok", map[string]any{"exit_code": float64(1)})
	if cond.Eval(out1) {
		t.Error("exit_code == 0 should not pass with exit_code=1")
	}

	// No exit_code field.
	outNoField := okEnv("ok", map[string]any{"status": "done"})
	if cond.Eval(outNoField) {
		t.Error("exit_code == 0 should not pass when exit_code field is missing")
	}
}

// ---------------------------------------------------------------------------
// Comprehensive table-driven Eval tests
// ---------------------------------------------------------------------------

func TestConditionEval(t *testing.T) {
	tests := []struct {
		name    string
		cond    string
		output  NodeOutput
		want    bool
		wantErr bool // parse error expected
	}{
		// Empty conditions.
		{
			name:   "empty passes ok",
			cond:   "",
			output: okEnv("ok", nil),
			want:   true,
		},
		{
			name:   "empty passes error",
			cond:   "",
			output: okEnv("error", nil),
			want:   true,
		},

		// status ==
		{
			name:   "status == ok matches ok",
			cond:   "status == ok",
			output: okEnv("ok", nil),
			want:   true,
		},
		{
			name:   "status == ok rejects error",
			cond:   "status == ok",
			output: okEnv("error", nil),
			want:   false,
		},

		// status !=
		{
			name:   "status != error passes ok",
			cond:   "status != error",
			output: okEnv("ok", nil),
			want:   true,
		},
		{
			name:   "status != error rejects error",
			cond:   "status != error",
			output: okEnv("error", nil),
			want:   false,
		},

		// output_len >
		{
			name:   "output_len > 0 passes with data",
			cond:   "output_len > 0",
			output: okEnv("ok", map[string]any{"key": "value"}),
			want:   true,
		},
		{
			name:   "output_len > 0 rejects nil data",
			cond:   "output_len > 0",
			output: okEnv("ok", nil),
			want:   false,
		},

		// exit_code ==
		{
			name:   "exit_code == 0 passes zero",
			cond:   "exit_code == 0",
			output: okEnv("ok", map[string]any{"exit_code": float64(0)}),
			want:   true,
		},
		{
			name:   "exit_code == 0 rejects non-zero",
			cond:   "exit_code == 0",
			output: okEnv("ok", map[string]any{"exit_code": float64(1)}),
			want:   false,
		},

		// output_contains:
		{
			name:   "output_contains:error matches data with error",
			cond:   "output_contains:error",
			output: okEnv("ok", map[string]any{"msg": "an error occurred"}),
			want:   true,
		},
		{
			name:   "output_contains:error rejects clean output",
			cond:   "output_contains:error",
			output: okEnv("ok", map[string]any{"msg": "success"}),
			want:   false,
		},

		// data.field ==
		{
			name:   "data.status == ready matches",
			cond:   "data.status == ready",
			output: envWithData(t, `{"status":"ready"}`),
			want:   true,
		},
		{
			name:   "data.status == ready rejects mismatch",
			cond:   "data.status == ready",
			output: envWithData(t, `{"status":"pending"}`),
			want:   false,
		},

		// Malformed.
		{
			name:    "malformed condition",
			cond:    "!!invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond, err := ParseCondition(tt.cond)
			if tt.wantErr {
				if err == nil {
					t.Error("expected parse error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCondition(%q): %v", tt.cond, err)
			}
			got := cond.Eval(tt.output)
			if got != tt.want {
				t.Errorf("Eval() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Condition.String() round-trip
// ---------------------------------------------------------------------------

func TestConditionString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"status == ok", "status == ok"},
		{"status != error", "status != error"},
		{"output_len > 0", "output_len > 0"},
		{"exit_code == 0", "exit_code == 0"},
		{"output_contains:error", "output_contains:error"},
		{"data.status == ready", "data.status == ready"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			cond, err := ParseCondition(tt.input)
			if err != nil {
				t.Fatalf("ParseCondition(%q): %v", tt.input, err)
			}
			got := cond.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AlwaysCondition and NeverCondition
// ---------------------------------------------------------------------------

func TestAlwaysCondition(t *testing.T) {
	c := AlwaysCondition
	if !c.Eval(okEnv("ok", nil)) {
		t.Error("AlwaysCondition should always pass")
	}
	if !c.Eval(okEnv("error", nil)) {
		t.Error("AlwaysCondition should always pass")
	}
	if c.String() != "" {
		t.Errorf("AlwaysCondition.String() = %q, want empty", c.String())
	}
}

func TestNeverCondition(t *testing.T) {
	c := NeverCondition
	if c.Eval(okEnv("ok", nil)) {
		t.Error("NeverCondition should never pass")
	}
}

// ---------------------------------------------------------------------------
// Duration field access
// ---------------------------------------------------------------------------

func TestConditionDuration(t *testing.T) {
	tests := []struct {
		name string
		cond string
		dur  time.Duration
		want bool
	}{
		{"duration > 0 with positive", "duration > 0", 100 * time.Millisecond, true},
		{"duration > 0 with zero", "duration > 0", 0, false},
		{"duration < 1000 with small", "duration < 1000", 500 * time.Millisecond, true},
		{"duration < 1000 with large", "duration < 1000", 2 * time.Second, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond, err := ParseCondition(tt.cond)
			if err != nil {
				t.Fatalf("ParseCondition(%q): %v", tt.cond, err)
			}
			out := NodeOutput{
				Envelope: envelope.Envelope{
					Version: 1,
					Status:  "ok",
					Command: "test/node",
					Data:    nil,
					Meta:    envelope.Meta{TS: "2026-01-01T00:00:00Z"},
				},
				Duration: tt.dur,
				NodeID:   "node-1",
			}
			got := cond.Eval(out)
			if got != tt.want {
				t.Errorf("Eval(%q) = %v, want %v", tt.cond, got, tt.want)
			}
		})
	}
}
