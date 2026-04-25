package runtime

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCheckCycleWitnessFindsBoundedCandidate(t *testing.T) {
	t.Parallel()

	text := `{
		"version":1,
		"checker_kind":"bounded_search",
		"variables":[{"name":"x","type":"int","min":1,"max":10}],
		"known_values":{"target":6},
		"constraints":[{"name":"prime_sum","op":"eq","left":{"func":"sum_prime_factors","args":[{"var":"x"}]},"right":{"known":"target"}}],
		"claims":{"answer":{"var":"x"}},
		"requested_outputs":["answer"]
	}`
	result, err := CheckCycleWitnessText(text)
	if err != nil {
		t.Fatalf("CheckCycleWitnessText() error = %v", err)
	}
	if !result.Pass {
		t.Fatalf("pass=false result=%+v", result)
	}
	if got := result.Candidates["answer"]; got != int64(8) {
		t.Fatalf("answer=%v want 8", got)
	}
	if len(result.Checks) != 1 || !result.Checks[0].OK {
		t.Fatalf("checks=%+v want one ok check", result.Checks)
	}
	line, err := CycleWitnessResultJSONLine(result)
	if err != nil {
		t.Fatalf("CycleWitnessResultJSONLine() error = %v", err)
	}
	if !strings.HasPrefix(line, "cycle_json: ") {
		t.Fatalf("line=%q want cycle_json prefix", line)
	}
	var decoded struct {
		Pass bool `json:"pass"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "cycle_json: ")), &decoded); err != nil {
		t.Fatalf("cycle_json is invalid JSON: %v", err)
	}
	if !decoded.Pass {
		t.Fatal("cycle_json pass=false want true")
	}
}

func TestCheckCycleWitnessRejectsExcessiveDomain(t *testing.T) {
	t.Parallel()

	text := `{"version":1,"checker_kind":"bounded_search","variables":[{"name":"x","min":0,"max":1000},{"name":"y","min":0,"max":1000}],"constraints":[{"name":"sum","op":"eq","left":{"op":"add","args":[{"var":"x"},{"var":"y"}]},"right":{"const":1}}]}`
	_, err := CheckCycleWitnessText(text)
	if err == nil || !strings.Contains(err.Error(), "domain exceeds max") {
		t.Fatalf("err=%v want domain cap error", err)
	}
}

func TestCheckCycleWitnessRejectsUnsupportedFunction(t *testing.T) {
	t.Parallel()

	text := `{"version":1,"checker_kind":"bounded_search","variables":[{"name":"x","min":0,"max":1}],"constraints":[{"name":"bad","op":"eq","left":{"func":"eval","args":[{"var":"x"}]},"right":{"const":1}}]}`
	_, err := CheckCycleWitnessText(text)
	if err == nil || !strings.Contains(err.Error(), `unsupported func "eval"`) {
		t.Fatalf("err=%v want unsupported func error", err)
	}
}

func TestCheckCycleWitnessReturnsPassFalseWhenNoCandidate(t *testing.T) {
	t.Parallel()

	text := `{"version":1,"checker_kind":"bounded_search","variables":[{"name":"x","min":0,"max":2}],"constraints":[{"name":"target","op":"eq","left":{"var":"x"},"right":{"const":9}}]}`
	result, err := CheckCycleWitnessText(text)
	if err != nil {
		t.Fatalf("CheckCycleWitnessText() error = %v", err)
	}
	if result.Pass {
		t.Fatalf("pass=true result=%+v want false", result)
	}
	if len(result.Candidates) != 0 {
		t.Fatalf("candidates=%v want empty", result.Candidates)
	}
	if len(result.Checks) == 0 || result.Checks[0].OK {
		t.Fatalf("checks=%+v want failing check", result.Checks)
	}
}
