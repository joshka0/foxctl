package longcoteval

import (
	"encoding/json"
	"testing"
)

func TestOfficialResponseForAttempt(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"completion_tokens_details":{"reasoning_tokens":42}}`)
	resp := OfficialResponseForAttempt(Attempt{
		QuestionID:    "q1",
		Status:        AttemptStatusOK,
		ResponseText:  "answer",
		ReasoningText: "reasoning",
		Model:         "model-a",
		Usage: Usage{
			InputTokens:      10,
			OutputTokens:     20,
			TotalTokens:      30,
			RawProviderUsage: raw,
		},
	})
	if resp.QuestionID != "q1" || !resp.Successful || resp.ResponseText != "answer" || resp.Model != "model-a" || resp.Reasoning != "reasoning" {
		t.Fatalf("response=%+v", resp)
	}
	if resp.Usage["prompt_tokens"] != 10 || resp.Usage["completion_tokens"] != 20 || resp.Usage["total_tokens"] != 30 {
		t.Fatalf("usage=%v", resp.Usage)
	}
	if _, ok := resp.Usage["raw_provider_usage"]; !ok {
		t.Fatalf("missing raw provider usage: %v", resp.Usage)
	}
}

func TestOfficialResponseForLeakedAttemptIsUnsuccessful(t *testing.T) {
	t.Parallel()

	resp := OfficialResponseForAttempt(Attempt{
		QuestionID: "q1",
		Status:     AttemptStatusOK,
		LeakageFlags: LeakageFlags{
			DatasetAccessibleDuringSolve: true,
		},
	})
	if resp.Successful {
		t.Fatalf("expected unsuccessful leaked response: %+v", resp)
	}
}
