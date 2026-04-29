package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
)

type sequenceSimulationSpec struct {
	Initial       any
	Events        []sequenceSimulationEvent
	Invariants    []sequenceSimulationConstraint
	GoalState     any
	HasGoalState  bool
	GoalChecks    []sequenceSimulationConstraint
	AnswerKey     string
	OriginalInput map[string]any
}

type sequenceSimulationEvent struct {
	Op       string
	Path     []sequencePathSegment
	Value    any
	Delta    float64
	HasValue bool
	HasDelta bool
}

type sequenceSimulationConstraint struct {
	Path       []sequencePathSegment
	Equals     any
	Min        float64
	Max        float64
	Present    bool
	HasEquals  bool
	HasMin     bool
	HasMax     bool
	HasPresent bool
}

type sequencePathSegment struct {
	Key     string
	Index   int
	IsIndex bool
}

func sequenceSimulationSpecFromInput(input map[string]any) (sequenceSimulationSpec, bool) {
	if len(input) == 0 {
		return sequenceSimulationSpec{}, false
	}
	if class, ok := input["scaffold_class"].(string); ok && strings.TrimSpace(class) != "" && strings.TrimSpace(class) != BraidScaffoldClassSequenceSimulation {
		return sequenceSimulationSpec{}, false
	}
	model := strings.TrimSpace(stringAny(input["sequence_model"]))
	id := strings.TrimSpace(stringAny(input["scaffold_id"]))
	if model == "" && id == BraidScaffoldIDJSONPatchSequenceV1 {
		model = id
	}
	if class, ok := input["scaffold_class"].(string); model == "" && ok && strings.TrimSpace(class) == BraidScaffoldClassSequenceSimulation {
		model = BraidScaffoldIDJSONPatchSequenceV1
	}
	if model != BraidScaffoldIDJSONPatchSequenceV1 {
		return sequenceSimulationSpec{}, false
	}
	initial, ok := input["initial_state"]
	if !ok {
		return sequenceSimulationSpec{}, false
	}
	rawEvents, answerKey, ok := sequenceSimulationRawEvents(input)
	if !ok {
		return sequenceSimulationSpec{}, false
	}
	events, ok := sequenceSimulationEventsFromAny(rawEvents)
	if !ok {
		return sequenceSimulationSpec{}, false
	}
	invariants, ok := sequenceSimulationConstraintsFromAny(input["invariants"])
	if !ok {
		return sequenceSimulationSpec{}, false
	}
	goalChecks, ok := sequenceSimulationConstraintsFromAny(input["goal_conditions"])
	if !ok {
		return sequenceSimulationSpec{}, false
	}
	goalState, hasGoalState := input["goal_state"]
	return sequenceSimulationSpec{
		Initial:       cloneAny(initial),
		Events:        events,
		Invariants:    invariants,
		GoalState:     cloneAny(goalState),
		HasGoalState:  hasGoalState,
		GoalChecks:    goalChecks,
		AnswerKey:     answerKey,
		OriginalInput: cloneMapAny(input),
	}, true
}

func sequenceSimulationRawEvents(input map[string]any) (any, string, bool) {
	if raw, ok := input["events"]; ok {
		return raw, "events", true
	}
	if raw, ok := input["actions"]; ok {
		return raw, "actions", true
	}
	return nil, "", false
}

func sequenceSimulationEventsFromAny(value any) ([]sequenceSimulationEvent, bool) {
	rawEvents, ok := value.([]any)
	if !ok {
		return nil, false
	}
	events := make([]sequenceSimulationEvent, 0, len(rawEvents))
	for _, rawEvent := range rawEvents {
		eventMap, ok := rawEvent.(map[string]any)
		if !ok {
			return nil, false
		}
		op := strings.TrimSpace(stringAny(eventMap["op"]))
		path, ok := sequencePathFromAny(eventMap["path"])
		if op == "" || !ok {
			return nil, false
		}
		event := sequenceSimulationEvent{Op: op, Path: path}
		if value, ok := eventMap["value"]; ok {
			event.Value = cloneAny(value)
			event.HasValue = true
		}
		if delta, ok := floatFromJSONNumberLike(eventMap["delta"]); ok {
			event.Delta = delta
			event.HasDelta = true
		}
		events = append(events, event)
	}
	return events, true
}

func sequenceSimulationConstraintsFromAny(value any) ([]sequenceSimulationConstraint, bool) {
	if value == nil {
		return nil, true
	}
	rawItems, ok := value.([]any)
	if !ok {
		return nil, false
	}
	constraints := make([]sequenceSimulationConstraint, 0, len(rawItems))
	for _, rawItem := range rawItems {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, false
		}
		path, ok := sequencePathFromAny(item["path"])
		if !ok {
			return nil, false
		}
		constraint := sequenceSimulationConstraint{Path: path}
		if value, ok := item["equals"]; ok {
			constraint.Equals = cloneAny(value)
			constraint.HasEquals = true
		}
		if value, ok := floatFromJSONNumberLike(item["min"]); ok {
			constraint.Min = value
			constraint.HasMin = true
		}
		if value, ok := floatFromJSONNumberLike(item["max"]); ok {
			constraint.Max = value
			constraint.HasMax = true
		}
		if value, ok := item["present"].(bool); ok {
			constraint.Present = value
			constraint.HasPresent = true
		}
		constraints = append(constraints, constraint)
	}
	return constraints, true
}

func sequencePathFromAny(value any) ([]sequencePathSegment, bool) {
	rawPath, ok := value.([]any)
	if !ok || len(rawPath) == 0 {
		return nil, false
	}
	path := make([]sequencePathSegment, 0, len(rawPath))
	for _, raw := range rawPath {
		if key, ok := raw.(string); ok && strings.TrimSpace(key) != "" {
			path = append(path, sequencePathSegment{Key: strings.TrimSpace(key)})
			continue
		}
		if idx, ok := intFromJSONNumberLike(raw); ok && idx >= 0 {
			path = append(path, sequencePathSegment{Index: idx, IsIndex: true})
			continue
		}
		return nil, false
	}
	return path, true
}

func simulateSequence(spec sequenceSimulationSpec) (any, HelperVerifierDiagnostic, bool) {
	state := cloneAny(spec.Initial)
	if diag, ok := sequenceCheckConstraints(state, spec.Invariants, -1, "invariant"); !ok {
		return state, diag, false
	}
	for idx, event := range spec.Events {
		before := cloneAny(state)
		next, err := applySequenceEvent(state, event)
		if err != nil {
			return state, HelperVerifierDiagnostic{
				Pass:         false,
				Score:        sequenceSimulationStepScore(idx, len(spec.Events)),
				FailureKind:  "event",
				FailedAtStep: idx,
				StateBefore:  before,
				Message:      err.Error(),
				RepairHint:   "fix the typed event op, path, value, or numeric delta",
			}, false
		}
		state = next
		if diag, ok := sequenceCheckConstraints(state, spec.Invariants, idx, "invariant"); !ok {
			diag.Score = sequenceSimulationStepScore(idx, len(spec.Events))
			return state, diag, false
		}
	}
	if spec.HasGoalState && !jsonCanonicalEqual(state, spec.GoalState) {
		return state, HelperVerifierDiagnostic{
			Pass:          false,
			Score:         0.9,
			FailureKind:   "goal_state_mismatch",
			FailedAtStep:  len(spec.Events),
			ObservedFinal: state,
			ExpectedFinal: spec.GoalState,
			Message:       "final state does not equal goal_state",
			RepairHint:    "simulate every event in order and compare the complete final JSON state",
		}, false
	}
	if diag, ok := sequenceCheckConstraints(state, spec.GoalChecks, len(spec.Events), "goal_condition"); !ok {
		diag.Score = 0.9
		return state, diag, false
	}
	return state, HelperVerifierDiagnostic{Pass: true, Score: 1, FailureKind: "sequence_simulation", FailedAtStep: -1}, true
}

func applySequenceEvent(state any, event sequenceSimulationEvent) (any, error) {
	switch event.Op {
	case "set":
		if !event.HasValue {
			return state, fmt.Errorf("set event missing value")
		}
		next, ok, detail := sequenceSetAtPath(state, event.Path, event.Value)
		if !ok {
			return state, fmt.Errorf("set failed: %s", detail)
		}
		return next, nil
	case "inc":
		delta := event.Delta
		if !event.HasDelta {
			if !event.HasValue {
				return state, fmt.Errorf("inc event missing delta")
			}
			value, ok := floatFromJSONNumberLike(event.Value)
			if !ok {
				return state, fmt.Errorf("inc event value is not numeric")
			}
			delta = value
		}
		current, ok := sequenceValueAtPath(state, event.Path)
		if !ok {
			return state, fmt.Errorf("inc path does not exist")
		}
		currentNumber, ok := floatFromJSONNumberLike(current)
		if !ok {
			return state, fmt.Errorf("inc target is not numeric")
		}
		next, ok, detail := sequenceSetAtPath(state, event.Path, currentNumber+delta)
		if !ok {
			return state, fmt.Errorf("inc failed: %s", detail)
		}
		return next, nil
	case "append":
		if !event.HasValue {
			return state, fmt.Errorf("append event missing value")
		}
		current, ok := sequenceValueAtPath(state, event.Path)
		if !ok {
			return state, fmt.Errorf("append path does not exist")
		}
		list, ok := current.([]any)
		if !ok {
			return state, fmt.Errorf("append target is not an array")
		}
		nextList := append(append([]any(nil), list...), cloneAny(event.Value))
		next, ok, detail := sequenceSetAtPath(state, event.Path, nextList)
		if !ok {
			return state, fmt.Errorf("append failed: %s", detail)
		}
		return next, nil
	case "delete":
		next, ok, detail := sequenceDeleteAtPath(state, event.Path)
		if !ok {
			return state, fmt.Errorf("delete failed: %s", detail)
		}
		return next, nil
	default:
		return state, fmt.Errorf("unsupported op %q", event.Op)
	}
}

func sequenceValueAtPath(value any, path []sequencePathSegment) (any, bool) {
	current := value
	for _, segment := range path {
		if segment.IsIndex {
			list, ok := current.([]any)
			if !ok || segment.Index < 0 || segment.Index >= len(list) {
				return nil, false
			}
			current = list[segment.Index]
			continue
		}
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := object[segment.Key]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func sequenceSetAtPath(current any, path []sequencePathSegment, value any) (any, bool, string) {
	if len(path) == 0 {
		return cloneAny(value), true, ""
	}
	head := path[0]
	if head.IsIndex {
		list, ok := current.([]any)
		if !ok || head.Index < 0 || head.Index >= len(list) {
			return current, false, "array index out of range"
		}
		nextList := append([]any(nil), list...)
		if len(path) == 1 {
			nextList[head.Index] = cloneAny(value)
			return nextList, true, ""
		}
		child, ok, detail := sequenceSetAtPath(nextList[head.Index], path[1:], value)
		if !ok {
			return current, false, detail
		}
		nextList[head.Index] = child
		return nextList, true, ""
	}
	object, ok := current.(map[string]any)
	if !ok {
		return current, false, "object key applied to non-object"
	}
	nextObject := cloneMapAny(object)
	if len(path) == 1 {
		nextObject[head.Key] = cloneAny(value)
		return nextObject, true, ""
	}
	child, exists := nextObject[head.Key]
	if !exists {
		return current, false, "intermediate object key does not exist"
	}
	nextChild, ok, detail := sequenceSetAtPath(child, path[1:], value)
	if !ok {
		return current, false, detail
	}
	nextObject[head.Key] = nextChild
	return nextObject, true, ""
}

func sequenceDeleteAtPath(current any, path []sequencePathSegment) (any, bool, string) {
	if len(path) == 0 {
		return current, false, "delete path is empty"
	}
	head := path[0]
	if head.IsIndex {
		list, ok := current.([]any)
		if !ok || head.Index < 0 || head.Index >= len(list) {
			return current, false, "array index out of range"
		}
		nextList := append([]any(nil), list...)
		if len(path) == 1 {
			nextList = append(nextList[:head.Index], nextList[head.Index+1:]...)
			return nextList, true, ""
		}
		child, ok, detail := sequenceDeleteAtPath(nextList[head.Index], path[1:])
		if !ok {
			return current, false, detail
		}
		nextList[head.Index] = child
		return nextList, true, ""
	}
	object, ok := current.(map[string]any)
	if !ok {
		return current, false, "object key applied to non-object"
	}
	nextObject := cloneMapAny(object)
	if len(path) == 1 {
		if _, exists := nextObject[head.Key]; !exists {
			return current, false, "object key does not exist"
		}
		delete(nextObject, head.Key)
		return nextObject, true, ""
	}
	child, exists := nextObject[head.Key]
	if !exists {
		return current, false, "intermediate object key does not exist"
	}
	nextChild, ok, detail := sequenceDeleteAtPath(child, path[1:])
	if !ok {
		return current, false, detail
	}
	nextObject[head.Key] = nextChild
	return nextObject, true, ""
}

func sequenceCheckConstraints(state any, constraints []sequenceSimulationConstraint, step int, kind string) (HelperVerifierDiagnostic, bool) {
	for _, constraint := range constraints {
		observed, exists := sequenceValueAtPath(state, constraint.Path)
		if constraint.HasPresent && constraint.Present != exists {
			return sequenceConstraintDiagnostic(kind, step, observed, constraint, fmt.Sprintf("presence check failed: present=%v", exists)), false
		}
		if !exists {
			return sequenceConstraintDiagnostic(kind, step, nil, constraint, "path does not exist"), false
		}
		if constraint.HasEquals && !jsonCanonicalEqual(observed, constraint.Equals) {
			return sequenceConstraintDiagnostic(kind, step, observed, constraint, "equals check failed"), false
		}
		if constraint.HasMin || constraint.HasMax {
			number, ok := floatFromJSONNumberLike(observed)
			if !ok {
				return sequenceConstraintDiagnostic(kind, step, observed, constraint, "numeric bound target is not numeric"), false
			}
			if constraint.HasMin && number < constraint.Min {
				return sequenceConstraintDiagnostic(kind, step, observed, constraint, "minimum bound check failed"), false
			}
			if constraint.HasMax && number > constraint.Max {
				return sequenceConstraintDiagnostic(kind, step, observed, constraint, "maximum bound check failed"), false
			}
		}
	}
	return HelperVerifierDiagnostic{Pass: true, FailedAtStep: -1}, true
}

func sequenceConstraintDiagnostic(kind string, step int, observed any, constraint sequenceSimulationConstraint, message string) HelperVerifierDiagnostic {
	return HelperVerifierDiagnostic{
		Pass:         false,
		FailureKind:  kind,
		FailedAtStep: step,
		ObservedFinal: map[string]any{
			"path":  sequencePathForDiagnostic(constraint.Path),
			"value": observed,
		},
		Message:    message,
		RepairHint: "simulate the sequence prefix and repair the event or expected constraint at the first failing step",
	}
}

func sequencePathForDiagnostic(path []sequencePathSegment) []any {
	out := make([]any, 0, len(path))
	for _, segment := range path {
		if segment.IsIndex {
			out = append(out, segment.Index)
		} else {
			out = append(out, segment.Key)
		}
	}
	return out
}

func sequenceSimulationStepScore(step, total int) float64 {
	if total <= 0 || step < 0 {
		return 0
	}
	score := float64(step) / float64(total)
	if score > 0.85 {
		return 0.85
	}
	return score
}

func sequenceSimulationAnswerVerifier(answer string, input map[string]any) (HelperVerifierDiagnostic, bool) {
	spec, ok := sequenceSimulationSpecFromInput(input)
	if !ok {
		return HelperVerifierDiagnostic{}, false
	}
	want, diag, pass := simulateSequence(spec)
	if !pass {
		return diag, true
	}
	got, ok := sequenceSimulationFinalStateFromAnswer(answer)
	if !ok {
		return HelperVerifierDiagnostic{
			Pass:         false,
			Score:        0,
			FailureKind:  "parse",
			FailedAtStep: -1,
			Message:      "candidate does not contain a parseable JSON final state",
			RepairHint:   "return answer exactly as solution = <JSON final_state>",
		}, true
	}
	if !jsonCanonicalEqual(got, want) {
		return HelperVerifierDiagnostic{
			Pass:          false,
			Score:         0.95,
			FailureKind:   "final_state_mismatch",
			FailedAtStep:  len(spec.Events),
			ObservedFinal: got,
			ExpectedFinal: want,
			Message:       "candidate final state does not match deterministic simulation",
			RepairHint:    "return the simulated final JSON state, not an intermediate state or prose",
		}, true
	}
	return HelperVerifierDiagnostic{Pass: true, Score: 1, FailureKind: "sequence_simulation", FailedAtStep: -1}, true
}

func sequenceSimulationFinalStateFromAnswer(answer string) (any, bool) {
	raw := strings.TrimSpace(answer)
	if idx := strings.Index(raw, "="); idx >= 0 && strings.Contains(strings.ToLower(raw[:idx]), "solution") {
		raw = strings.TrimSpace(raw[idx+1:])
	}
	raw = strings.Trim(raw, "` \t\r\n")
	var value any
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, false
	}
	return value, true
}

func jsonCanonicalEqual(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(ab) == string(bb)
}

func stringAny(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func floatFromJSONNumberLike(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		n, err := typed.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}

func buildSequenceSimulationHelperContract() string {
	return strings.TrimSpace(`
Generic sequence-simulation helper contract:
- Treat the typed input as a forward simulation over JSON-like state, not prose.
- Implement parse_initial_state(input), parse_events(input), apply_event(state,event), check_invariants(state,step), and check_goal(final_state).
- For json_patch_v1, events are objects with op and path. Supported ops are set, inc, append, and delete.
- Paths are arrays of object keys or array indexes. Reject invalid paths instead of guessing.
- Check invariants before the first event and after every event. Check goal_state and goal_conditions after all events.
- Return ok:true only with answer exactly 'solution = <JSON final_state>' after the full trace verifies.
- If simulation or verification fails, return ok:false with first_failure, failed_step, state_before, observed, expected, and repair_hint.
`)
}

func jsonPatchSequenceSimulationPresetSource() string {
	return strings.TrimSpace(`
func Solve(input map[string]any) map[string]any {
	state := clone(input["initial_state"])
	rawEvents, ok := input["events"].([]any)
	if !ok {
		rawEvents, ok = input["actions"].([]any)
	}
	if !ok {
		return map[string]any{"ok": false, "first_failure": "expected events or actions array", "repair_hint": "provide typed json_patch_v1 events"}
	}
	checks, ok := constraints(input["invariants"])
	if !ok {
		return map[string]any{"ok": false, "first_failure": "invalid invariants", "repair_hint": "use constraints with path plus equals, min, max, or present"}
	}
	if fail := checkAll(state, checks); fail != "" {
		return map[string]any{"ok": false, "first_failure": fail, "failed_step": -1, "state_before": state, "repair_hint": "initial state violates invariant"}
	}
	for i, raw := range rawEvents {
		before := clone(state)
		ev, ok := raw.(map[string]any)
		if !ok {
			return map[string]any{"ok": false, "first_failure": "event is not an object", "failed_step": i, "state_before": before}
		}
		next, fail := apply(state, ev)
		if fail != "" {
			return map[string]any{"ok": false, "first_failure": fail, "failed_step": i, "state_before": before, "repair_hint": "fix event op, path, value, or delta"}
		}
		state = next
		if fail := checkAll(state, checks); fail != "" {
			return map[string]any{"ok": false, "first_failure": fail, "failed_step": i, "state_before": before, "observed": state, "repair_hint": "event violates invariant"}
		}
	}
	if goal, ok := input["goal_state"]; ok && !equal(state, goal) {
		return map[string]any{"ok": false, "first_failure": "final state does not equal goal_state", "observed": state, "expected": goal, "repair_hint": "simulate all events in order"}
	}
	goalChecks, ok := constraints(input["goal_conditions"])
	if !ok {
		return map[string]any{"ok": false, "first_failure": "invalid goal_conditions", "repair_hint": "use constraints with path plus equals, min, max, or present"}
	}
	if fail := checkAll(state, goalChecks); fail != "" {
		return map[string]any{"ok": false, "first_failure": fail, "observed": state, "repair_hint": "final state violates goal condition"}
	}
	body, err := json.Marshal(state)
	if err != nil {
		return map[string]any{"ok": false, "first_failure": err.Error()}
	}
	return map[string]any{"ok": true, "answer": "solution = " + string(body)}
}

func clone(v any) any {
	body, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(body, &out)
	return out
}

func equal(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

func apply(state any, ev map[string]any) (any, string) {
	op, _ := ev["op"].(string)
	path, ok := path(ev["path"])
	if !ok {
		return state, "event path must be a non-empty array"
	}
	switch op {
	case "set":
		value, ok := ev["value"]
		if !ok {
			return state, "set missing value"
		}
		next, ok := setAt(state, path, value)
		if !ok {
			return state, "set path invalid"
		}
		return next, ""
	case "inc":
		delta, ok := number(ev["delta"])
		if !ok {
			delta, ok = number(ev["value"])
		}
		if !ok {
			return state, "inc missing numeric delta"
		}
		current, ok := getAt(state, path)
		if !ok {
			return state, "inc path missing"
		}
		n, ok := number(current)
		if !ok {
			return state, "inc target not numeric"
		}
		next, ok := setAt(state, path, n+delta)
		if !ok {
			return state, "inc path invalid"
		}
		return next, ""
	case "append":
		value, ok := ev["value"]
		if !ok {
			return state, "append missing value"
		}
		current, ok := getAt(state, path)
		if !ok {
			return state, "append path missing"
		}
		list, ok := current.([]any)
		if !ok {
			return state, "append target not array"
		}
		nextList := append(append([]any(nil), list...), clone(value))
		next, ok := setAt(state, path, nextList)
		if !ok {
			return state, "append path invalid"
		}
		return next, ""
	case "delete":
		next, ok := deleteAt(state, path)
		if !ok {
			return state, "delete path invalid"
		}
		return next, ""
	default:
		return state, "unsupported op"
	}
}

func path(v any) ([]any, bool) {
	raw, ok := v.([]any)
	if !ok || len(raw) == 0 {
		return nil, false
	}
	for _, item := range raw {
		switch x := item.(type) {
		case string:
			if x == "" {
				return nil, false
			}
		case float64:
			if x < 0 || float64(int(x)) != x {
				return nil, false
			}
		default:
			return nil, false
		}
	}
	return raw, true
}

func getAt(v any, p []any) (any, bool) {
	cur := v
	for _, part := range p {
		switch x := part.(type) {
		case string:
			obj, ok := cur.(map[string]any)
			if !ok {
				return nil, false
			}
			cur, ok = obj[x]
			if !ok {
				return nil, false
			}
		case float64:
			list, ok := cur.([]any)
			i := int(x)
			if !ok || i < 0 || i >= len(list) {
				return nil, false
			}
			cur = list[i]
		}
	}
	return cur, true
}

func setAt(cur any, p []any, val any) (any, bool) {
	if len(p) == 0 {
		return clone(val), true
	}
	switch x := p[0].(type) {
	case string:
		obj, ok := cur.(map[string]any)
		if !ok {
			return cur, false
		}
		next := map[string]any{}
		for k, v := range obj {
			next[k] = v
		}
		if len(p) == 1 {
			next[x] = clone(val)
			return next, true
		}
		child, ok := next[x]
		if !ok {
			return cur, false
		}
		newChild, ok := setAt(child, p[1:], val)
		if !ok {
			return cur, false
		}
		next[x] = newChild
		return next, true
	case float64:
		list, ok := cur.([]any)
		i := int(x)
		if !ok || i < 0 || i >= len(list) {
			return cur, false
		}
		next := append([]any(nil), list...)
		if len(p) == 1 {
			next[i] = clone(val)
			return next, true
		}
		child, ok := setAt(next[i], p[1:], val)
		if !ok {
			return cur, false
		}
		next[i] = child
		return next, true
	}
	return cur, false
}

func deleteAt(cur any, p []any) (any, bool) {
	if len(p) == 0 {
		return cur, false
	}
	switch x := p[0].(type) {
	case string:
		obj, ok := cur.(map[string]any)
		if !ok {
			return cur, false
		}
		next := map[string]any{}
		for k, v := range obj {
			next[k] = v
		}
		if len(p) == 1 {
			if _, ok := next[x]; !ok {
				return cur, false
			}
			delete(next, x)
			return next, true
		}
		child, ok := next[x]
		if !ok {
			return cur, false
		}
		newChild, ok := deleteAt(child, p[1:])
		if !ok {
			return cur, false
		}
		next[x] = newChild
		return next, true
	case float64:
		list, ok := cur.([]any)
		i := int(x)
		if !ok || i < 0 || i >= len(list) {
			return cur, false
		}
		next := append([]any(nil), list...)
		if len(p) == 1 {
			next = append(next[:i], next[i+1:]...)
			return next, true
		}
		child, ok := deleteAt(next[i], p[1:])
		if !ok {
			return cur, false
		}
		next[i] = child
		return next, true
	}
	return cur, false
}

func constraints(v any) ([]map[string]any, bool) {
	if v == nil {
		return nil, true
	}
	raw, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := []map[string]any{}
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		if _, ok := path(m["path"]); !ok {
			return nil, false
		}
		out = append(out, m)
	}
	return out, true
}

func checkAll(state any, checks []map[string]any) string {
	for _, c := range checks {
		p, _ := path(c["path"])
		got, exists := getAt(state, p)
		if wantPresent, ok := c["present"].(bool); ok && wantPresent != exists {
			return "presence check failed"
		}
		if !exists {
			return "constraint path missing"
		}
		if want, ok := c["equals"]; ok && !equal(got, want) {
			return "equals check failed"
		}
		if min, ok := number(c["min"]); ok {
			n, ok := number(got)
			if !ok || n < min {
				return "minimum bound check failed"
			}
		}
		if max, ok := number(c["max"]); ok {
			n, ok := number(got)
			if !ok || n > max {
				return "maximum bound check failed"
			}
		}
	}
	return ""
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	default:
		return 0, false
	}
}
`)
}
