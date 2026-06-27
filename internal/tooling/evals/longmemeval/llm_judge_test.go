package longmemeval

import (
	"context"
	"testing"
)

func TestParseLLMJudgeVerdict(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		input      string
		wantScore  float64
		wantReason string
		wantErr    bool
	}{
		{
			name:       "yes with reason",
			input:      "VERDICT: YES\nREASON: Both answers state the same fact.",
			wantScore:  1,
			wantReason: "Both answers state the same fact.",
		},
		{
			name:       "no lowercase",
			input:      "verdict: no\nreason: wrong number",
			wantScore:  0,
			wantReason: "wrong number",
		},
		{
			name:    "missing verdict",
			input:   "REASON: no verdict line",
			wantErr: true,
		},
		{
			name:       "reason before verdict",
			input:      "REASON: paraphrase\nVERDICT: YES",
			wantScore:  1,
			wantReason: "paraphrase",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, reason, err := parseLLMJudgeVerdict(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseLLMJudgeVerdict(%q) error=%v wantErr=%v", tt.input, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if score != tt.wantScore {
				t.Fatalf("score=%v want %v", score, tt.wantScore)
			}
			if reason != tt.wantReason {
				t.Fatalf("reason=%q want %q", reason, tt.wantReason)
			}
		})
	}
}

type fakeLLMJudge struct {
	verdicts map[string]float64
	reasons  map[string]string
}

func (f *fakeLLMJudge) Judge(_ context.Context, question, answer, expected string) (float64, string, error) {
	key := question + "|" + answer + "|" + expected
	if score, ok := f.verdicts[key]; ok {
		return score, f.reasons[key], nil
	}
	return 0, "default no", nil
}

func TestRescoreReportWithLLMJudge(t *testing.T) {
	t.Parallel()
	result := RunResult{
		Cases: []CaseResult{
			{
				CaseID:         "case-1",
				Question:       "q1",
				Answer:         "the answer is five",
				ExpectedAnswer: "5",
				AnswerMatched:  false,
				AnswerScore:    0,
				AnswerMethod:   "answer",
			},
			{
				CaseID:         "case-2",
				Question:       "q2",
				Answer:         "the answer is six",
				ExpectedAnswer: "6",
				AnswerMatched:  true,
				AnswerScore:    1,
				AnswerMethod:   "answer",
			},
		},
		Metrics: &Metrics{},
	}
	judge := &fakeLLMJudge{
		verdicts: map[string]float64{
			"q1|the answer is five|5": 1,
			"q2|the answer is six|6":  0,
		},
		reasons: map[string]string{
			"q1|the answer is five|5": "paraphrase",
			"q2|the answer is six|6":  "wrong number",
		},
	}
	if err := RescoreReportWithLLMJudge(context.Background(), &result, judge); err != nil {
		t.Fatalf("RescoreReportWithLLMJudge: %v", err)
	}
	if !result.Cases[0].AnswerMatched {
		t.Fatalf("case-1 should be upgraded to matched by judge")
	}
	if result.Cases[0].AnswerMethod != "answer-judge" {
		t.Fatalf("case-1 AnswerMethod=%q want answer-judge", result.Cases[0].AnswerMethod)
	}
	if result.Cases[0].AnswerJudgeReason != "paraphrase" {
		t.Fatalf("case-1 judge reason=%q want paraphrase", result.Cases[0].AnswerJudgeReason)
	}
	if !result.Cases[1].AnswerMatched {
		t.Fatalf("case-2 should stay matched (judge never downgrades)")
	}
	if result.Metrics == nil || result.Metrics.AnswerAccuracy != 1 {
		t.Fatalf("metrics accuracy=%v want 1", result.Metrics.AnswerAccuracy)
	}
	if result.Metrics.AnswerJudgeAccuracy != 0.5 {
		t.Fatalf("judge accuracy=%v want 0.5", result.Metrics.AnswerJudgeAccuracy)
	}
}
