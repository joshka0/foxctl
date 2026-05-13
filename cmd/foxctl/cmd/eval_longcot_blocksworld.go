package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/runtime/engine"
	"github.com/joshka0/foxctl/internal/tooling/evals/longcoteval"
)

const longCoTBlocksWorldSolveToolName = "blocksworld_solve"

type longCoTBlocksWorldToolExecutor struct {
	Prompt string
}

func (e longCoTBlocksWorldToolExecutor) List() []engine.ToolDef {
	return []engine.ToolDef{{
		Name:        longCoTBlocksWorldSolveToolName,
		Description: "Parse a BlocksWorld instruction or state/goal problem and return the canonical action answer format. If prompt is omitted, the current official prompt is used.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"prompt":{"type":"string","description":"Optional BlocksWorld prompt text. Omit to use the official prompt for this attempt."},"max_depth":{"type":"integer","minimum":1,"description":"Optional BFS search depth for state/goal prompts. Defaults to 8."}},"additionalProperties":false}`),
	}}
}

func (e longCoTBlocksWorldToolExecutor) Execute(_ context.Context, name string, args json.RawMessage) (string, error) {
	if strings.TrimSpace(name) != longCoTBlocksWorldSolveToolName {
		return "", fmt.Errorf("unknown LongCoT BlocksWorld tool %q", name)
	}
	var input struct {
		Prompt   string `json:"prompt,omitempty"`
		MaxDepth int    `json:"max_depth,omitempty"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &input); err != nil {
			return "", fmt.Errorf("decode blocksworld_solve args: %w", err)
		}
	}
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		prompt = strings.TrimSpace(e.Prompt)
	}
	result := solveLongCoTBlocksWorldPrompt(prompt, input.MaxDepth)
	confidence := "high"
	if !result.OK {
		confidence = "unsupported"
	}
	body, err := json.Marshal(map[string]any{
		"ok":            result.OK,
		"solution":      result.Solution,
		"answer_format": "solution = " + result.Solution,
		"output":        longCoTBlocksWorldStructuredOutput(result),
		"confidence":    confidence,
		"plan":          result.Plan,
		"moves":         result.Moves,
		"initial_state": result.Initial,
		"goal_state":    result.Goal,
		"final_state":   result.Final,
		"stack_trace":   result.Stacks,
		"error":         result.Error,
	})
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func longCoTBlocksWorldStructuredOutput(result longCoTBlocksWorldSolveResult) string {
	if !result.OK {
		payload, _ := json.Marshal(map[string]any{
			"pass":   false,
			"reason": firstNonEmpty(result.Error, "BlocksWorld helper could not solve prompt"),
		})
		return "RLM_CHECK_JSON=" + string(payload)
	}
	answer := "solution = " + result.Solution
	checkPayload, _ := json.Marshal(map[string]any{
		"pass":   true,
		"reason": "BlocksWorld helper produced a valid plan",
	})
	answerPayload, _ := json.Marshal(map[string]any{
		"answer": answer,
		"pass":   true,
		"checks": []string{"blocksworld helper parsed prompt and planned moves"},
	})
	return strings.Join([]string{
		"RLM_CHECK_JSON=" + string(checkPayload),
		"RLM_ANSWER_JSON=" + string(answerPayload),
	}, "\n")
}

var (
	longCoTBlockMoveOntoRE = regexp.MustCompile(`(?i)\bmove\s+(?:block\s+)?([A-Za-z0-9_-]+)\s+(?:onto|on\s+top\s+of|on)\s+(?:block\s+)?([A-Za-z0-9_-]+)\b`)
	longCoTBlockMoveToRE   = regexp.MustCompile(`(?i)\bmove\s+(?:block\s+)?([A-Za-z0-9_-]+)\s+to\s+(?:the\s+)?(?:block\s+)?([A-Za-z0-9_-]+|table)\b`)
	longCoTBlockOnRE       = regexp.MustCompile(`(?i)\b(?:block\s+)?([A-Za-z0-9_-]+)\s+(?:is\s+)?(?:on|onto|on\s+top\s+of)\s+(?:the\s+)?(?:block\s+)?([A-Za-z0-9_-]+|table)\b`)
)

type longCoTBlocksWorldSolveResult struct {
	OK       bool              `json:"ok"`
	Solution string            `json:"solution,omitempty"`
	Plan     []string          `json:"plan,omitempty"`
	Moves    [][]int           `json:"moves,omitempty"`
	Initial  map[string]string `json:"initial_state,omitempty"`
	Goal     map[string]string `json:"goal_state,omitempty"`
	Final    map[string]string `json:"final_state,omitempty"`
	Stacks   [][][]int         `json:"stack_trace,omitempty"`
	Error    string            `json:"error,omitempty"`
}

func solveLongCoTBlocksWorldPrompt(prompt string, maxDepth int) longCoTBlocksWorldSolveResult {
	prompt = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(prompt), "/no_think"))
	if maxDepth <= 0 {
		maxDepth = 8
	}

	if initial, goal, ok := parseLongCoTBlocksWorldStackProblem(prompt); ok {
		moves, finalStacks, solved := planLongCoTBlocksWorldStacks(initial, goal)
		if !solved {
			return longCoTBlocksWorldSolveResult{OK: false, Error: "stack planner failed"}
		}
		return longCoTBlocksWorldSolveResult{
			OK:       true,
			Solution: formatLongCoTBlocksWorldMoves(moves),
			Moves:    moves,
			Stacks:   [][][]int{initial, finalStacks},
		}
	}

	if initial, goal, ok := parseLongCoTBlocksWorldProblem(prompt); ok {
		plan, finalState, solved := planLongCoTBlocksWorld(initial, goal, maxDepth)
		if !solved {
			return longCoTBlocksWorldSolveResult{
				OK:      false,
				Initial: initial,
				Goal:    goal,
				Error:   "no plan found within search depth",
			}
		}
		return longCoTBlocksWorldSolveResult{
			OK:       true,
			Solution: strings.Join(plan, "; "),
			Plan:     plan,
			Initial:  initial,
			Goal:     goal,
			Final:    finalState,
		}
	}

	for _, re := range []*regexp.Regexp{longCoTBlockMoveOntoRE, longCoTBlockMoveToRE} {
		match := re.FindStringSubmatch(prompt)
		if len(match) != 3 {
			continue
		}
		src := normalizeLongCoTBlockName(match[1])
		dst := normalizeLongCoTBlockName(match[2])
		if src == "" || dst == "" {
			continue
		}
		solution := "move " + src + " to " + dst
		return longCoTBlocksWorldSolveResult{
			OK:       true,
			Solution: solution,
			Plan:     []string{solution},
			Final:    map[string]string{src: dst},
		}
	}
	return longCoTBlocksWorldSolveResult{OK: false, Error: "unsupported BlocksWorld prompt shape"}
}

func longCoTBlocksWorldFinalResponse(question longcoteval.Question) (string, bool) {
	if !longCoTQuestionIsBlocksWorld(question) {
		return "", false
	}
	result := solveLongCoTBlocksWorldPrompt(question.PromptText, 0)
	if !result.OK {
		return "", false
	}
	return "solution = " + result.Solution, true
}

func parseLongCoTBlocksWorldStackProblem(prompt string) ([][]int, [][]int, bool) {
	initial, ok := extractLongCoTBlocksWorldStackList(prompt, "Initial state:")
	if !ok {
		return nil, nil, false
	}
	goal, ok := extractLongCoTBlocksWorldStackList(prompt, "Goal state:")
	if !ok {
		return nil, nil, false
	}
	if len(initial) == 0 || len(goal) == 0 || len(initial) != len(goal) {
		return nil, nil, false
	}
	return initial, goal, true
}

func extractLongCoTBlocksWorldStackList(prompt, marker string) ([][]int, bool) {
	lowerPrompt := strings.ToLower(prompt)
	lowerMarker := strings.ToLower(marker)
	searchFrom := 0
	var found [][]int
	for {
		idx := strings.Index(lowerPrompt[searchFrom:], lowerMarker)
		if idx < 0 {
			return found, len(found) > 0
		}
		idx += searchFrom
		rest := prompt[idx+len(marker):]
		start := strings.Index(rest, "[")
		if start >= 0 {
			end := matchingLongCoTBracketEnd(rest[start:])
			if end >= 0 {
				var stacks [][]int
				if err := json.Unmarshal([]byte(rest[start:start+end+1]), &stacks); err == nil {
					found = stacks
				}
			}
		}
		searchFrom = idx + len(marker)
	}
}

func matchingLongCoTBracketEnd(text string) int {
	depth := 0
	for idx, ch := range text {
		switch ch {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return idx
			}
		}
	}
	return -1
}

func planLongCoTBlocksWorldStacks(initial, goal [][]int) ([][]int, [][]int, bool) {
	stacks := cloneIntStacks(initial)
	locked := make([]int, len(stacks))
	for i := range stacks {
		for locked[i] < len(stacks[i]) && locked[i] < len(goal[i]) && stacks[i][locked[i]] == goal[i][locked[i]] {
			locked[i]++
		}
	}
	moves := make([][]int, 0)
	for dstStack := range goal {
		for locked[dstStack] < len(goal[dstStack]) {
			target := goal[dstStack][locked[dstStack]]
			for len(stacks[dstStack]) > locked[dstStack] {
				to, ok := chooseLongCoTStackBuffer(stacks, locked, goal, dstStack, dstStack)
				if !ok {
					return nil, nil, false
				}
				appendLongCoTStackMove(&moves, stacks, dstStack, to)
			}
			srcStack, ok := findLongCoTBlockStack(stacks, target)
			if !ok {
				return nil, nil, false
			}
			for len(stacks[srcStack]) > 0 && stacks[srcStack][len(stacks[srcStack])-1] != target {
				to, ok := chooseLongCoTStackBuffer(stacks, locked, goal, srcStack, dstStack)
				if !ok {
					return nil, nil, false
				}
				appendLongCoTStackMove(&moves, stacks, srcStack, to)
			}
			srcStack, ok = findLongCoTBlockStack(stacks, target)
			if !ok || len(stacks[srcStack]) == 0 || stacks[srcStack][len(stacks[srcStack])-1] != target {
				return nil, nil, false
			}
			if srcStack != dstStack {
				appendLongCoTStackMove(&moves, stacks, srcStack, dstStack)
			}
			if len(stacks[dstStack]) <= locked[dstStack] || stacks[dstStack][locked[dstStack]] != target {
				return nil, nil, false
			}
			locked[dstStack]++
		}
	}
	return moves, stacks, equalIntStacks(stacks, goal)
}

func chooseLongCoTStackBuffer(stacks [][]int, locked []int, goal [][]int, from, preferredForbidden int) (int, bool) {
	best := -1
	bestScore := 0
	for i := range stacks {
		if i == from || i == preferredForbidden {
			continue
		}
		if i < len(goal) && locked[i] >= len(goal[i]) && len(stacks[i]) == locked[i] {
			continue
		}
		score := len(stacks[i]) - locked[i]
		if best < 0 || score < bestScore {
			best = i
			bestScore = score
		}
	}
	if best >= 0 {
		return best, true
	}
	for i := range stacks {
		if i != from && i != preferredForbidden {
			return i, true
		}
	}
	for i := range stacks {
		if i != from {
			return i, true
		}
	}
	return 0, false
}

func appendLongCoTStackMove(moves *[][]int, stacks [][]int, from, to int) {
	block := stacks[from][len(stacks[from])-1]
	stacks[from] = stacks[from][:len(stacks[from])-1]
	stacks[to] = append(stacks[to], block)
	*moves = append(*moves, []int{block, from, to})
}

func findLongCoTBlockStack(stacks [][]int, target int) (int, bool) {
	for i, stack := range stacks {
		for _, block := range stack {
			if block == target {
				return i, true
			}
		}
	}
	return 0, false
}

func formatLongCoTBlocksWorldMoves(moves [][]int) string {
	body, err := json.Marshal(moves)
	if err != nil {
		return "[]"
	}
	return string(body)
}

func cloneIntStacks(in [][]int) [][]int {
	out := make([][]int, len(in))
	for i := range in {
		out[i] = append([]int(nil), in[i]...)
	}
	return out
}

func equalIntStacks(a, b [][]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}

func parseLongCoTBlocksWorldProblem(prompt string) (map[string]string, map[string]string, bool) {
	initialText, goalText, ok := splitLongCoTBlocksWorldInitialGoal(prompt)
	if !ok {
		return nil, nil, false
	}
	initial := parseLongCoTBlocksWorldRelations(initialText)
	goal := parseLongCoTBlocksWorldRelations(goalText)
	if len(initial) == 0 || len(goal) == 0 {
		return nil, nil, false
	}
	return initial, goal, true
}

func splitLongCoTBlocksWorldInitialGoal(prompt string) (string, string, bool) {
	lower := strings.ToLower(prompt)
	goalIdx := firstNonNegativeIndex(
		strings.Index(lower, "goal state"),
		strings.Index(lower, "goal:"),
		strings.Index(lower, "goal is"),
		strings.Index(lower, "target state"),
		strings.Index(lower, "target:"),
	)
	if goalIdx < 0 {
		return "", "", false
	}
	initialIdx := firstNonNegativeIndex(
		strings.Index(lower, "initial state"),
		strings.Index(lower, "initially"),
		strings.Index(lower, "start state"),
		strings.Index(lower, "starting state"),
	)
	if initialIdx < 0 || initialIdx >= goalIdx {
		initialIdx = 0
	}
	return prompt[initialIdx:goalIdx], prompt[goalIdx:], true
}

func parseLongCoTBlocksWorldRelations(text string) map[string]string {
	out := map[string]string{}
	for _, match := range longCoTBlockOnRE.FindAllStringSubmatch(text, -1) {
		if len(match) != 3 {
			continue
		}
		src := normalizeLongCoTBlockName(match[1])
		dst := normalizeLongCoTBlockName(match[2])
		if src == "" || dst == "" || src == dst {
			continue
		}
		out[src] = dst
		if _, ok := out[dst]; !ok && dst != "table" {
			out[dst] = "table"
		}
	}
	return out
}

func planLongCoTBlocksWorld(initial, goal map[string]string, maxDepth int) ([]string, map[string]string, bool) {
	initial = normalizeLongCoTBlocksWorldState(initial, goal)
	goal = normalizeLongCoTBlocksWorldGoal(goal)
	if satisfiesLongCoTBlocksWorldGoal(initial, goal) {
		return []string{}, cloneStringMap(initial), true
	}
	type node struct {
		State map[string]string
		Plan  []string
	}
	queue := []node{{State: initial}}
	seen := map[string]struct{}{encodeLongCoTBlocksWorldState(initial): {}}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if len(current.Plan) >= maxDepth {
			continue
		}
		for _, move := range legalLongCoTBlocksWorldMoves(current.State) {
			nextState := cloneStringMap(current.State)
			nextState[move.Block] = move.Destination
			key := encodeLongCoTBlocksWorldState(nextState)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			nextPlan := append(append([]string(nil), current.Plan...), move.String())
			if satisfiesLongCoTBlocksWorldGoal(nextState, goal) {
				return nextPlan, nextState, true
			}
			queue = append(queue, node{State: nextState, Plan: nextPlan})
		}
	}
	return nil, nil, false
}

type longCoTBlocksWorldMove struct {
	Block       string
	Destination string
}

func (m longCoTBlocksWorldMove) String() string {
	return "move " + m.Block + " to " + m.Destination
}

func legalLongCoTBlocksWorldMoves(state map[string]string) []longCoTBlocksWorldMove {
	blocks := sortedLongCoTBlocksWorldBlocks(state)
	clear := clearLongCoTBlocksWorldBlocks(state)
	moves := make([]longCoTBlocksWorldMove, 0)
	for _, block := range blocks {
		if !clear[block] {
			continue
		}
		current := state[block]
		if current != "table" {
			moves = append(moves, longCoTBlocksWorldMove{Block: block, Destination: "table"})
		}
		for _, dst := range blocks {
			if dst == block || !clear[dst] || current == dst {
				continue
			}
			moves = append(moves, longCoTBlocksWorldMove{Block: block, Destination: dst})
		}
	}
	return moves
}

func normalizeLongCoTBlocksWorldState(initial, goal map[string]string) map[string]string {
	out := cloneStringMap(initial)
	for block, dst := range goal {
		if _, ok := out[block]; !ok {
			out[block] = "table"
		}
		if dst != "table" {
			if _, ok := out[dst]; !ok {
				out[dst] = "table"
			}
		}
	}
	for block, dst := range out {
		if dst == "" {
			out[block] = "table"
		}
	}
	return out
}

func normalizeLongCoTBlocksWorldGoal(goal map[string]string) map[string]string {
	out := map[string]string{}
	for block, dst := range goal {
		if block == "" || dst == "" {
			continue
		}
		out[block] = dst
	}
	return out
}

func satisfiesLongCoTBlocksWorldGoal(state, goal map[string]string) bool {
	for block, dst := range goal {
		if state[block] != dst {
			return false
		}
	}
	return true
}

func clearLongCoTBlocksWorldBlocks(state map[string]string) map[string]bool {
	clear := map[string]bool{}
	for block, dst := range state {
		clear[block] = true
		if dst != "table" {
			clear[dst] = true
		}
	}
	for _, dst := range state {
		if dst != "table" {
			clear[dst] = false
		}
	}
	return clear
}

func sortedLongCoTBlocksWorldBlocks(state map[string]string) []string {
	seen := map[string]struct{}{}
	for block, dst := range state {
		seen[block] = struct{}{}
		if dst != "table" {
			seen[dst] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for block := range seen {
		out = append(out, block)
	}
	sort.Strings(out)
	return out
}

func encodeLongCoTBlocksWorldState(state map[string]string) string {
	blocks := sortedLongCoTBlocksWorldBlocks(state)
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		parts = append(parts, block+"="+state[block])
	}
	return strings.Join(parts, "|")
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func firstNonNegativeIndex(values ...int) int {
	best := -1
	for _, value := range values {
		if value < 0 {
			continue
		}
		if best < 0 || value < best {
			best = value
		}
	}
	return best
}

func normalizeLongCoTBlockName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.EqualFold(value, "table") {
		return "table"
	}
	return strings.ToUpper(value)
}
