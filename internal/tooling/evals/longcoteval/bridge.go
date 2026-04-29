package longcoteval

import (
	"context"
	"encoding/json"
)

type LoadRequest struct {
	Dataset    string   `json:"dataset,omitempty"`
	Split      string   `json:"split,omitempty"`
	Domains    []string `json:"domains,omitempty"`
	Difficulty string   `json:"difficulty,omitempty"`
	Limit      int      `json:"limit,omitempty"`
	Seed       int64    `json:"seed,omitempty"`
}

type VerifyRequest struct {
	ResponsesPath string `json:"responses_path"`
	OutputPath    string `json:"output_path,omitempty"`
}

type VerifyResult struct {
	VerifierName    string         `json:"verifier_name,omitempty"`
	VerifierVersion string         `json:"verifier_version,omitempty"`
	Counts          map[string]int `json:"counts,omitempty"`
	Rows            []VerifyRow    `json:"rows,omitempty"`
	Raw             map[string]any `json:"raw,omitempty"`
}

type VerifyRow struct {
	QuestionID        string `json:"question_id"`
	Status            string `json:"status"`
	Correct           bool   `json:"correct,omitempty"`
	WrongFormatting   bool   `json:"wrong_formatting,omitempty"`
	VerificationError string `json:"verification_error,omitempty"`
	NormalizedAnswer  string `json:"normalized_answer,omitempty"`
}

type OfficialResponse struct {
	QuestionID   string         `json:"question_id"`
	Domain       string         `json:"domain,omitempty"`
	Difficulty   string         `json:"difficulty,omitempty"`
	Successful   bool           `json:"successful"`
	ResponseText string         `json:"response_text"`
	Model        string         `json:"model,omitempty"`
	Usage        map[string]any `json:"usage,omitempty"`
	Reasoning    string         `json:"reasoning,omitempty"`
}

type QuestionLoader interface {
	LoadQuestions(ctx context.Context, request LoadRequest) ([]Question, error)
}

type Verifier interface {
	Verify(ctx context.Context, request VerifyRequest) (VerifyResult, error)
}

// OfficialResponseForAttempt converts an attempt into the JSONL shape expected
// by the official LongCoT verifier harness.
func OfficialResponseForAttempt(attempt Attempt) OfficialResponse {
	return OfficialResponseForAttemptQuestion(attempt, Question{})
}

func OfficialResponseForAttemptQuestion(attempt Attempt, question Question) OfficialResponse {
	usage := map[string]any{}
	if attempt.Usage.InputTokens != 0 {
		usage["prompt_tokens"] = attempt.Usage.InputTokens
	}
	if attempt.Usage.OutputTokens != 0 {
		usage["completion_tokens"] = attempt.Usage.OutputTokens
	}
	if attempt.Usage.TotalTokens != 0 {
		usage["total_tokens"] = attempt.Usage.TotalTokens
	}
	if len(attempt.Usage.RawProviderUsage) > 0 {
		var raw map[string]any
		if err := json.Unmarshal(attempt.Usage.RawProviderUsage, &raw); err == nil {
			usage["raw_provider_usage"] = raw
		}
	}
	return OfficialResponse{
		QuestionID:   attempt.QuestionID,
		Domain:       question.Domain,
		Difficulty:   question.Difficulty,
		Successful:   attempt.Status == AttemptStatusOK && !attempt.LeakageFlags.Leaked(),
		ResponseText: attempt.ResponseText,
		Model:        attempt.Model,
		Usage:        usage,
		Reasoning:    attempt.ReasoningText,
	}
}
