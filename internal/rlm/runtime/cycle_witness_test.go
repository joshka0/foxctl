package runtime

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/quick"
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
	if err == nil || !strings.Contains(err.Error(), "domain exceeds max") || !strings.Contains(err.Error(), `variable "y"`) || !strings.Contains(err.Error(), "current_product=") {
		t.Fatalf("err=%v want detailed domain cap error", err)
	}
}

func TestParseCycleWitnessRejectsNonCanonicalModelOutput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
		want string
	}{
		{
			name: "markdown_fence",
			text: "```json\n{\"version\":1}\n```",
			want: "must be raw JSON",
		},
		{
			name: "unknown_field",
			text: `{"version":1,"checker_kind":"bounded_search","variables":[],"constraints":[],"extra":true}`,
			want: "unknown field",
		},
		{
			name: "multiple_json_values",
			text: `{"version":1,"checker_kind":"bounded_search","variables":[],"constraints":[]} {}`,
			want: "multiple JSON values",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseCycleWitnessText(tc.text)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ParseCycleWitnessText() error=%v want %q", err, tc.want)
			}
		})
	}
}

func TestValidateCycleWitnessRejectsInvalidBoundedSearchSpecs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*CycleWitness)
		want   string
	}{
		{
			name: "duplicate_variable",
			mutate: func(w *CycleWitness) {
				w.Variables = append(w.Variables, CycleWitnessVariable{Name: "x", Min: 0, Max: 1})
			},
			want: `duplicate variable "x"`,
		},
		{
			name: "min_exceeds_max",
			mutate: func(w *CycleWitness) {
				w.Variables[0].Min = 2
				w.Variables[0].Max = 1
			},
			want: "min 2 exceeds max 1",
		},
		{
			name: "unsupported_constraint_op",
			mutate: func(w *CycleWitness) {
				w.Constraints[0].Op = "approx"
			},
			want: `unsupported op "approx"`,
		},
		{
			name: "unknown_known_value",
			mutate: func(w *CycleWitness) {
				w.Constraints[0].Right = CycleExpr{Known: "missing"}
			},
			want: `unknown known value "missing"`,
		},
		{
			name: "expression_multiple_kinds",
			mutate: func(w *CycleWitness) {
				one := 1.0
				w.Constraints[0].Left = CycleExpr{Const: &one, Var: "x"}
			},
			want: "expression must set exactly one",
		},
		{
			name: "op_without_args",
			mutate: func(w *CycleWitness) {
				w.Constraints[0].Left = CycleExpr{Op: "add"}
			},
			want: `op "add" requires args`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			witness := minimalCycleWitness()
			tc.mutate(&witness)
			err := ValidateCycleWitness(witness)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateCycleWitness() error=%v want %q", err, tc.want)
			}
		})
	}
}

func TestValidateCycleWitnessRejectsGeneratedUnknownVariables(t *testing.T) {
	t.Parallel()

	unknownVarsFailClosed := func(raw string) bool {
		witness := minimalCycleWitness()
		witness.Constraints[0].Left = CycleExpr{Var: "unknown:" + raw}
		err := ValidateCycleWitness(witness)
		return err != nil && strings.Contains(err.Error(), "unknown variable")
	}
	if err := quick.Check(unknownVarsFailClosed, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("generated unknown variable was accepted: %v", err)
	}
}

func TestCheckCycleWitnessEmitsDeterministicCandidatesAndStats(t *testing.T) {
	t.Parallel()

	witness := CycleWitness{
		Version:     cycleWitnessVersionV1,
		CheckerKind: cycleWitnessCheckerBounded,
		Variables: []CycleWitnessVariable{
			{Name: "x", Type: "int", Min: 0, Max: 2},
			{Name: "y", Type: "int", Min: 0, Max: 3},
		},
		Constraints: []CycleWitnessConstraint{{
			Name: "sum",
			Op:   "eq",
			Left: CycleExpr{Op: "add", Args: []CycleExpr{
				{Var: "x"},
				{Var: "y"},
			}},
			Right: CycleExpr{Const: floatPtr(3)},
		}},
		Claims: map[string]CycleExpr{
			"answer": {Op: "add", Args: []CycleExpr{{Var: "x"}, {Var: "y"}}},
		},
	}
	result, err := CheckCycleWitness(witness)
	if err != nil {
		t.Fatalf("CheckCycleWitness() error = %v", err)
	}
	if !result.Pass {
		t.Fatalf("pass=false result=%+v", result)
	}
	if got, want := result.Candidates["x"], int64(0); got != want {
		t.Fatalf("candidate x=%v want %v", got, want)
	}
	if got, want := result.Candidates["y"], int64(3); got != want {
		t.Fatalf("candidate y=%v want %v", got, want)
	}
	if got, want := result.Candidates["answer"], int64(3); got != want {
		t.Fatalf("candidate answer=%v want %v", got, want)
	}
	if got, want := result.Stats["attempts"], 4; got != want {
		t.Fatalf("attempts=%v want %v", got, want)
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

func minimalCycleWitness() CycleWitness {
	return CycleWitness{
		Version:     cycleWitnessVersionV1,
		CheckerKind: cycleWitnessCheckerBounded,
		Variables: []CycleWitnessVariable{
			{Name: "x", Type: "int", Min: 0, Max: 2},
		},
		KnownValues: map[string]float64{"target": 1},
		Constraints: []CycleWitnessConstraint{{
			Name:  "target",
			Op:    "eq",
			Left:  CycleExpr{Var: "x"},
			Right: CycleExpr{Known: "target"},
		}},
	}
}

func floatPtr(value float64) *float64 {
	return &value
}
