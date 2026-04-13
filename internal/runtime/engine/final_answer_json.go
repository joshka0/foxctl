package engine

import (
	"encoding/json"
	"fmt"
	"strings"
)

const FinalAnswerJSONToolName = "final_answer_json"

// FinalAnswerJSONToolDef returns the terminal tool definition used to emit a
// final structured JSON answer. The engine intercepts this tool and ends the
// turn immediately instead of routing it through the normal tool executor.
func FinalAnswerJSONToolDef() ToolDef {
	return ToolDef{
		Name:        FinalAnswerJSONToolName,
		Description: "Emit the final user-facing answer as exact compact JSON. Use this when the final answer must be JSON. This ends the turn immediately.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"additionalProperties": false,
			"properties": {
				"payload": {
					"description": "The exact JSON value to return to the user as the final answer."
				},
				"json": {
					"description": "Alias for payload. The exact JSON value to return to the user as the final answer."
				}
			}
		}`),
	}
}

func maybeHandleFinalAnswerJSONTool(call ToolCall) (ToolResult, string, bool) {
	if strings.TrimSpace(call.Name) != FinalAnswerJSONToolName {
		return ToolResult{}, "", false
	}

	payload, err := extractFinalAnswerJSONPayload(call.Arguments)
	if err != nil {
		return ToolResult{
			ToolCallID: call.ID,
			Content:    fmt.Sprintf("invalid %s payload: %v", FinalAnswerJSONToolName, err),
			IsError:    true,
		}, "", true
	}

	return ToolResult{
		ToolCallID: call.ID,
		Content:    payload,
		IsError:    false,
	}, payload, true
}

func extractFinalAnswerJSONPayload(args json.RawMessage) (string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(args, &raw); err != nil {
		return "", fmt.Errorf("decode arguments: %w", err)
	}

	payload := raw["payload"]
	if len(payload) == 0 || string(payload) == "null" {
		payload = raw["json"]
	}
	if len(payload) == 0 || string(payload) == "null" {
		return "", fmt.Errorf("missing payload")
	}

	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return "", fmt.Errorf("decode payload: %w", err)
	}

	compact, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode payload: %w", err)
	}
	return string(compact), nil
}
