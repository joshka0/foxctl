package hooks

import (
	"encoding/json"
	"strings"
)

// Merge combines multiple hook outputs into a single output using deterministic rules.
//
// Rules (v1, stable):
//  1. Decision: block wins; else approve if any approve; else none
//  2. Reason: first non-empty reason for the final decision
//  3. UpdatedToolInput: last-wins (hook execution order)
//  4. UpdatedAssistantText: last-wins (hook execution order)
//  5. Context: join non-empty contexts with "\n\n" (hook execution order)
//  6. Actions: concatenated in hook execution order
//  7. Meta: shallow merge, last-wins per key
func Merge(outputs []Output) Output {
	if len(outputs) == 0 {
		return NewNone()
	}

	result := Output{
		Decision: DecisionNone,
		Meta:     make(map[string]any),
	}

	var (
		anyBlock    bool
		anyApprove  bool
		reasonBlock string
		reasonOK    string
		contexts    []string
		allActions  []Action
	)

	for _, out := range outputs {
		// Decision tracking
		switch out.Decision {
		case DecisionBlock:
			anyBlock = true
			if reasonBlock == "" && out.Reason != "" {
				reasonBlock = out.Reason
			}
		case DecisionApprove:
			anyApprove = true
			if reasonOK == "" && out.Reason != "" {
				reasonOK = out.Reason
			}
		}

		// UpdatedToolInput - last-wins
		if len(out.UpdatedToolInput) > 0 {
			result.UpdatedToolInput = out.UpdatedToolInput
		}

		// UpdatedAssistantText - last-wins
		if out.UpdatedAssistantText != "" {
			result.UpdatedAssistantText = out.UpdatedAssistantText
		}

		// Context join
		if out.Context != "" {
			contexts = append(contexts, out.Context)
		}

		// Actions concatenated
		allActions = append(allActions, out.Actions...)

		// Meta shallow merge
		for k, v := range out.Meta {
			result.Meta[k] = v
		}
	}

	result.Actions = allActions
	if len(contexts) > 0 {
		result.Context = strings.Join(contexts, "\n\n")
	}

	if anyBlock {
		result.Decision = DecisionBlock
		result.Reason = reasonBlock
	} else if anyApprove {
		result.Decision = DecisionApprove
		result.Reason = reasonOK
	}

	// Clean up empty meta
	if len(result.Meta) == 0 {
		result.Meta = nil
	}

	return result
}

// MergeResult is the detailed result of a merge operation.
// Used for debugging and observability.
type MergeResult struct {
	Output        Output
	BlockedBy     int   // Index of first blocking output (-1 if none)
	ReasonSources []int // Indices of outputs that contributed reasons
	ActionCounts  []int // Number of actions from each output
	HasToolInput  bool  // True if any output had UpdatedToolInput
	HasAssistant  bool  // True if any output had UpdatedAssistantText
}

// MergeWithDetails combines outputs and returns detailed merge information.
func MergeWithDetails(outputs []Output) MergeResult {
	result := MergeResult{
		BlockedBy:    -1,
		ActionCounts: make([]int, len(outputs)),
	}

	if len(outputs) == 0 {
		result.Output = NewNone()
		return result
	}

	merged := Output{
		Decision: DecisionNone,
		Meta:     make(map[string]any),
	}

	var (
		anyBlock       bool
		anyApprove     bool
		reasonBlock    string
		reasonOK       string
		reasonBlockIdx = -1
		reasonOKIdx    = -1
		contexts       []string
		allActions     []Action
	)

	for i, out := range outputs {
		switch out.Decision {
		case DecisionBlock:
			anyBlock = true
			if result.BlockedBy == -1 {
				result.BlockedBy = i
			}
			if reasonBlock == "" && out.Reason != "" {
				reasonBlock = out.Reason
				reasonBlockIdx = i
			}
		case DecisionApprove:
			anyApprove = true
			if reasonOK == "" && out.Reason != "" {
				reasonOK = out.Reason
				reasonOKIdx = i
			}
		}

		// UpdatedToolInput
		if len(out.UpdatedToolInput) > 0 {
			merged.UpdatedToolInput = out.UpdatedToolInput
			result.HasToolInput = true
		}

		// UpdatedAssistantText
		if out.UpdatedAssistantText != "" {
			merged.UpdatedAssistantText = out.UpdatedAssistantText
			result.HasAssistant = true
		}

		// Context
		if out.Context != "" {
			contexts = append(contexts, out.Context)
		}

		// Actions
		result.ActionCounts[i] = len(out.Actions)
		allActions = append(allActions, out.Actions...)

		// Meta
		for k, v := range out.Meta {
			merged.Meta[k] = v
		}
	}

	merged.Actions = allActions
	if len(contexts) > 0 {
		merged.Context = strings.Join(contexts, "\n\n")
	}

	if anyBlock {
		merged.Decision = DecisionBlock
		merged.Reason = reasonBlock
		if reasonBlockIdx >= 0 {
			result.ReasonSources = []int{reasonBlockIdx}
		}
	} else if anyApprove {
		merged.Decision = DecisionApprove
		merged.Reason = reasonOK
		if reasonOKIdx >= 0 {
			result.ReasonSources = []int{reasonOKIdx}
		}
	}

	if len(merged.Meta) == 0 {
		merged.Meta = nil
	}

	result.Output = merged
	return result
}

// DeduplicateActions removes duplicate actions while preserving order.
// Two actions are considered duplicates if they have the same type and key fields.
func DeduplicateActions(actions []Action) []Action {
	if len(actions) <= 1 {
		return actions
	}

	seen := make(map[string]bool)
	result := make([]Action, 0, len(actions))

	for _, a := range actions {
		key := actionKey(a)
		if !seen[key] {
			seen[key] = true
			result = append(result, a)
		}
	}

	return result
}

// actionKey generates a unique key for deduplication.
func actionKey(a Action) string {
	switch a.Type {
	case ActionRunSkill:
		return string(a.Type) + ":" + a.Skill + ":" + string(a.Args)
	case ActionInjectContext:
		return string(a.Type) + ":" + a.Text
	case ActionSendMailbox:
		return string(a.Type) + ":" + a.ToNS + ":" + a.MessageType + ":" + string(a.Payload)
	case ActionBBPost:
		return string(a.Type) + ":" + a.Topic + ":" + string(a.BBPayload)
	case ActionBBClaim:
		return string(a.Type) + ":" + a.Topic + ":" + a.RecordID
	default:
		// Fallback: use JSON serialization
		data, _ := json.Marshal(a)
		return string(data)
	}
}

// SortActionsByPriority sorts inject_context actions by priority (descending).
// Other action types are left in their original order.
func SortActionsByPriority(actions []Action) []Action {
	if len(actions) <= 1 {
		return actions
	}

	// Separate inject_context actions from others
	var injectActions []Action
	var otherActions []Action

	for _, a := range actions {
		if a.Type == ActionInjectContext {
			injectActions = append(injectActions, a)
		} else {
			otherActions = append(otherActions, a)
		}
	}

	// Sort inject_context by priority (descending)
	for i := 0; i < len(injectActions)-1; i++ {
		for j := i + 1; j < len(injectActions); j++ {
			if injectActions[j].Priority > injectActions[i].Priority {
				injectActions[i], injectActions[j] = injectActions[j], injectActions[i]
			}
		}
	}

	// Return inject_context first (they should be surfaced first), then others
	return append(injectActions, otherActions...)
}
