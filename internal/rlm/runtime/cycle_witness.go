package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
)

const (
	cycleWitnessVersionV1        = 1
	cycleWitnessCheckerBounded   = "bounded_search"
	maxCycleWitnessVariables     = 6
	maxCycleWitnessDomainProduct = 100000
	maxCycleWitnessChecks        = 32
)

// CycleWitness is a compact, model-authored bounded-search specification. The
// runtime checks it deterministically and emits the cycle_json consumed by the
// BRAID verifier.
type CycleWitness struct {
	Version         int                      `json:"version"`
	CheckerKind     string                   `json:"checker_kind"`
	Variables       []CycleWitnessVariable   `json:"variables"`
	KnownValues     map[string]float64       `json:"known_values,omitempty"`
	Constraints     []CycleWitnessConstraint `json:"constraints"`
	Claims          map[string]CycleExpr     `json:"claims,omitempty"`
	RequestedOutput []string                 `json:"requested_outputs,omitempty"`
}

type CycleWitnessVariable struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
	Min  int    `json:"min"`
	Max  int    `json:"max"`
}

type CycleWitnessConstraint struct {
	Name  string    `json:"name"`
	Op    string    `json:"op"`
	Left  CycleExpr `json:"left"`
	Right CycleExpr `json:"right"`
}

type CycleExpr struct {
	Const *float64    `json:"const,omitempty"`
	Var   string      `json:"var,omitempty"`
	Known string      `json:"known,omitempty"`
	Op    string      `json:"op,omitempty"`
	Func  string      `json:"func,omitempty"`
	Args  []CycleExpr `json:"args,omitempty"`
}

type CycleWitnessResult struct {
	Pass       bool                `json:"pass"`
	Candidates map[string]any      `json:"candidates"`
	Checks     []CycleWitnessCheck `json:"checks"`
	Stats      map[string]any      `json:"stats,omitempty"`
}

type CycleWitnessCheck struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Observed any    `json:"observed,omitempty"`
	Expected any    `json:"expected,omitempty"`
	Error    string `json:"error,omitempty"`
}

func ParseCycleWitnessText(text string) (CycleWitness, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return CycleWitness{}, fmt.Errorf("cycle witness: empty output")
	}
	if strings.HasPrefix(trimmed, "```") || strings.Contains(trimmed, "```") {
		return CycleWitness{}, fmt.Errorf("cycle witness: must be raw JSON, not markdown")
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(trimmed)))
	decoder.DisallowUnknownFields()
	var witness CycleWitness
	if err := decoder.Decode(&witness); err != nil {
		return CycleWitness{}, fmt.Errorf("cycle witness: parse JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return CycleWitness{}, fmt.Errorf("cycle witness: parse JSON: %w", err)
	}
	return witness, nil
}

func ValidateCycleWitness(w CycleWitness) error {
	if w.Version != cycleWitnessVersionV1 {
		return fmt.Errorf("cycle witness: unsupported version %d", w.Version)
	}
	if strings.TrimSpace(w.CheckerKind) != cycleWitnessCheckerBounded {
		return fmt.Errorf("cycle witness: unsupported checker_kind %q", w.CheckerKind)
	}
	if len(w.Variables) == 0 {
		return fmt.Errorf("cycle witness: variables is required")
	}
	if len(w.Variables) > maxCycleWitnessVariables {
		return fmt.Errorf("cycle witness: variable count %d exceeds max %d", len(w.Variables), maxCycleWitnessVariables)
	}
	if len(w.Constraints) == 0 {
		return fmt.Errorf("cycle witness: constraints is required")
	}
	if len(w.Constraints) > maxCycleWitnessChecks {
		return fmt.Errorf("cycle witness: constraint count %d exceeds max %d", len(w.Constraints), maxCycleWitnessChecks)
	}
	seen := map[string]struct{}{}
	product := 1
	for idx, variable := range w.Variables {
		name := strings.TrimSpace(variable.Name)
		if name == "" {
			return fmt.Errorf("cycle witness: variable %d name is required", idx+1)
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("cycle witness: duplicate variable %q", name)
		}
		seen[name] = struct{}{}
		typ := strings.TrimSpace(variable.Type)
		if typ != "" && typ != "int" {
			return fmt.Errorf("cycle witness: variable %q has unsupported type %q", name, variable.Type)
		}
		if variable.Min > variable.Max {
			return fmt.Errorf("cycle witness: variable %q min %d exceeds max %d", name, variable.Min, variable.Max)
		}
		width := variable.Max - variable.Min + 1
		if width <= 0 {
			return fmt.Errorf("cycle witness: variable %q has invalid domain width", name)
		}
		if product > maxCycleWitnessDomainProduct/width {
			return fmt.Errorf("cycle witness: bounded search domain exceeds max %d", maxCycleWitnessDomainProduct)
		}
		product *= width
	}
	varNames := make(map[string]struct{}, len(w.Variables))
	for _, variable := range w.Variables {
		varNames[variable.Name] = struct{}{}
	}
	for idx, constraint := range w.Constraints {
		if strings.TrimSpace(constraint.Name) == "" {
			return fmt.Errorf("cycle witness: constraint %d name is required", idx+1)
		}
		if !validCycleConstraintOp(constraint.Op) {
			return fmt.Errorf("cycle witness: constraint %q has unsupported op %q", constraint.Name, constraint.Op)
		}
		if err := validateCycleExpr(constraint.Left, varNames, w.KnownValues); err != nil {
			return fmt.Errorf("cycle witness: constraint %q left: %w", constraint.Name, err)
		}
		if err := validateCycleExpr(constraint.Right, varNames, w.KnownValues); err != nil {
			return fmt.Errorf("cycle witness: constraint %q right: %w", constraint.Name, err)
		}
	}
	for name, expr := range w.Claims {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("cycle witness: claim name is required")
		}
		if err := validateCycleExpr(expr, varNames, w.KnownValues); err != nil {
			return fmt.Errorf("cycle witness: claim %q: %w", name, err)
		}
	}
	return nil
}

func CheckCycleWitnessText(text string) (CycleWitnessResult, error) {
	witness, err := ParseCycleWitnessText(text)
	if err != nil {
		return CycleWitnessResult{}, err
	}
	return CheckCycleWitness(witness)
}

func CheckCycleWitness(w CycleWitness) (CycleWitnessResult, error) {
	if err := ValidateCycleWitness(w); err != nil {
		return CycleWitnessResult{}, err
	}
	stats := map[string]any{"checker_kind": cycleWitnessCheckerBounded}
	var attempts int
	var lastChecks []CycleWitnessCheck
	var found map[string]float64
	var search func(int, map[string]float64) bool
	search = func(idx int, assignment map[string]float64) bool {
		if idx == len(w.Variables) {
			attempts++
			checks, ok := evaluateCycleConstraints(w, assignment)
			lastChecks = checks
			if ok {
				found = copyFloatMap(assignment)
				return true
			}
			return false
		}
		variable := w.Variables[idx]
		for value := variable.Min; value <= variable.Max; value++ {
			assignment[variable.Name] = float64(value)
			if search(idx+1, assignment) {
				return true
			}
		}
		delete(assignment, variable.Name)
		return false
	}
	pass := search(0, map[string]float64{})
	stats["attempts"] = attempts
	if !pass {
		checks := lastChecks
		if len(checks) == 0 {
			checks = []CycleWitnessCheck{{
				Name:     "exhausted_bounds",
				OK:       false,
				Observed: "no_candidate",
				Expected: "candidate",
			}}
		}
		return CycleWitnessResult{Pass: false, Candidates: map[string]any{}, Checks: checks, Stats: stats}, nil
	}
	candidates := map[string]any{}
	for _, name := range sortedFloatMapKeys(found) {
		candidates[name] = normalizeCycleNumber(found[name])
	}
	for _, name := range sortedCycleClaimKeys(w.Claims) {
		value, err := evalCycleExpr(w.Claims[name], found, w.KnownValues)
		if err != nil {
			return CycleWitnessResult{}, fmt.Errorf("cycle witness: claim %q: %w", name, err)
		}
		candidates[name] = normalizeCycleNumber(value)
	}
	return CycleWitnessResult{Pass: true, Candidates: candidates, Checks: lastChecks, Stats: stats}, nil
}

func CycleWitnessResultJSONLine(result CycleWitnessResult) (string, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return "cycle_json: " + string(raw), nil
}

func evaluateCycleConstraints(w CycleWitness, assignment map[string]float64) ([]CycleWitnessCheck, bool) {
	checks := make([]CycleWitnessCheck, 0, len(w.Constraints))
	allOK := true
	for _, constraint := range w.Constraints {
		left, leftErr := evalCycleExpr(constraint.Left, assignment, w.KnownValues)
		right, rightErr := evalCycleExpr(constraint.Right, assignment, w.KnownValues)
		check := CycleWitnessCheck{Name: constraint.Name, Expected: nil}
		if leftErr != nil || rightErr != nil {
			check.OK = false
			check.Error = strings.TrimSpace(strings.Trim(strings.Join([]string{errString(leftErr), errString(rightErr)}, "; "), "; "))
			allOK = false
			checks = append(checks, check)
			continue
		}
		ok := compareCycleNumbers(left, right, constraint.Op)
		check.OK = ok
		check.Observed = normalizeCycleNumber(left)
		check.Expected = normalizeCycleNumber(right)
		if !ok {
			allOK = false
		}
		checks = append(checks, check)
	}
	return checks, allOK
}

func validateCycleExpr(expr CycleExpr, variables map[string]struct{}, known map[string]float64) error {
	kinds := 0
	if expr.Const != nil {
		kinds++
	}
	if strings.TrimSpace(expr.Var) != "" {
		kinds++
		if _, ok := variables[expr.Var]; !ok {
			return fmt.Errorf("unknown variable %q", expr.Var)
		}
	}
	if strings.TrimSpace(expr.Known) != "" {
		kinds++
		if _, ok := known[expr.Known]; !ok {
			return fmt.Errorf("unknown known value %q", expr.Known)
		}
	}
	if strings.TrimSpace(expr.Op) != "" {
		kinds++
		if !validCycleExprOp(expr.Op) {
			return fmt.Errorf("unsupported op %q", expr.Op)
		}
	}
	if strings.TrimSpace(expr.Func) != "" {
		kinds++
		if !validCycleExprFunc(expr.Func) {
			return fmt.Errorf("unsupported func %q", expr.Func)
		}
	}
	if kinds != 1 {
		return fmt.Errorf("expression must set exactly one of const, var, known, op, func")
	}
	for idx, arg := range expr.Args {
		if err := validateCycleExpr(arg, variables, known); err != nil {
			return fmt.Errorf("arg %d: %w", idx+1, err)
		}
	}
	if expr.Op != "" && len(expr.Args) == 0 {
		return fmt.Errorf("op %q requires args", expr.Op)
	}
	if expr.Func != "" && len(expr.Args) == 0 {
		return fmt.Errorf("func %q requires args", expr.Func)
	}
	return nil
}

func evalCycleExpr(expr CycleExpr, variables map[string]float64, known map[string]float64) (float64, error) {
	if expr.Const != nil {
		return *expr.Const, nil
	}
	if expr.Var != "" {
		value, ok := variables[expr.Var]
		if !ok {
			return 0, fmt.Errorf("unknown variable %q", expr.Var)
		}
		return value, nil
	}
	if expr.Known != "" {
		value, ok := known[expr.Known]
		if !ok {
			return 0, fmt.Errorf("unknown known value %q", expr.Known)
		}
		return value, nil
	}
	if expr.Op != "" {
		values, err := evalCycleArgs(expr.Args, variables, known)
		if err != nil {
			return 0, err
		}
		return applyCycleOp(expr.Op, values)
	}
	if expr.Func != "" {
		values, err := evalCycleArgs(expr.Args, variables, known)
		if err != nil {
			return 0, err
		}
		return applyCycleFunc(expr.Func, values)
	}
	return 0, fmt.Errorf("empty expression")
}

func evalCycleArgs(args []CycleExpr, variables map[string]float64, known map[string]float64) ([]float64, error) {
	values := make([]float64, 0, len(args))
	for _, arg := range args {
		value, err := evalCycleExpr(arg, variables, known)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func applyCycleOp(op string, values []float64) (float64, error) {
	switch op {
	case "add":
		total := 0.0
		for _, value := range values {
			total += value
		}
		return total, nil
	case "sub":
		if len(values) == 0 {
			return 0, fmt.Errorf("sub requires args")
		}
		total := values[0]
		for _, value := range values[1:] {
			total -= value
		}
		return total, nil
	case "mul":
		total := 1.0
		for _, value := range values {
			total *= value
		}
		return total, nil
	case "div":
		if len(values) != 2 {
			return 0, fmt.Errorf("div requires two args")
		}
		if values[1] == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		return values[0] / values[1], nil
	case "mod":
		if len(values) != 2 {
			return 0, fmt.Errorf("mod requires two args")
		}
		left, right := int(math.Round(values[0])), int(math.Round(values[1]))
		if right == 0 {
			return 0, fmt.Errorf("mod by zero")
		}
		return float64(left % right), nil
	case "neg":
		if len(values) != 1 {
			return 0, fmt.Errorf("neg requires one arg")
		}
		return -values[0], nil
	case "min":
		if len(values) == 0 {
			return 0, fmt.Errorf("min requires args")
		}
		best := values[0]
		for _, value := range values[1:] {
			if value < best {
				best = value
			}
		}
		return best, nil
	case "max":
		if len(values) == 0 {
			return 0, fmt.Errorf("max requires args")
		}
		best := values[0]
		for _, value := range values[1:] {
			if value > best {
				best = value
			}
		}
		return best, nil
	default:
		return 0, fmt.Errorf("unsupported op %q", op)
	}
}

func applyCycleFunc(fn string, values []float64) (float64, error) {
	switch fn {
	case "sum_prime_factors", "prime_factor_sum":
		total := 0
		for _, value := range values {
			n := int(math.Round(value))
			if n < 0 {
				n = -n
			}
			total += sumPrimeFactors(n)
		}
		return float64(total), nil
	case "gcd":
		if len(values) != 2 {
			return 0, fmt.Errorf("gcd requires two args")
		}
		return float64(gcdInt(int(math.Round(values[0])), int(math.Round(values[1])))), nil
	case "abs":
		if len(values) != 1 {
			return 0, fmt.Errorf("abs requires one arg")
		}
		return math.Abs(values[0]), nil
	default:
		return 0, fmt.Errorf("unsupported func %q", fn)
	}
}

func compareCycleNumbers(left, right float64, op string) bool {
	const eps = 1e-9
	switch op {
	case "eq":
		return math.Abs(left-right) <= eps
	case "ne":
		return math.Abs(left-right) > eps
	case "lt":
		return left < right && math.Abs(left-right) > eps
	case "lte":
		return left < right || math.Abs(left-right) <= eps
	case "gt":
		return left > right && math.Abs(left-right) > eps
	case "gte":
		return left > right || math.Abs(left-right) <= eps
	default:
		return false
	}
}

func validCycleConstraintOp(op string) bool {
	switch strings.TrimSpace(op) {
	case "eq", "ne", "lt", "lte", "gt", "gte":
		return true
	default:
		return false
	}
}

func validCycleExprOp(op string) bool {
	switch strings.TrimSpace(op) {
	case "add", "sub", "mul", "div", "mod", "neg", "min", "max":
		return true
	default:
		return false
	}
}

func validCycleExprFunc(fn string) bool {
	switch strings.TrimSpace(fn) {
	case "sum_prime_factors", "prime_factor_sum", "gcd", "abs":
		return true
	default:
		return false
	}
}

func sumPrimeFactors(n int) int {
	if n <= 1 {
		return 0
	}
	sum := 0
	for d := 2; d*d <= n; d++ {
		for n%d == 0 {
			sum += d
			n /= d
		}
	}
	if n > 1 {
		sum += n
	}
	return sum
}

func gcdInt(a, b int) int {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func copyFloatMap(in map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func sortedFloatMapKeys(in map[string]float64) []string {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedCycleClaimKeys(in map[string]CycleExpr) []string {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func normalizeCycleNumber(value float64) any {
	rounded := math.Round(value)
	if math.Abs(value-rounded) <= 1e-9 {
		return int64(rounded)
	}
	return value
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
