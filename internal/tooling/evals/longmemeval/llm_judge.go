package longmemeval

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/engine"
)

// LLMJudge scores a model answer against an expected answer using an LLM as
// a binary semantic-equivalence judge. It is intentionally narrow: it returns
// 1.0 for YES, 0.0 for NO, and an error if the judge could not produce a
// parseable verdict.
type LLMJudge interface {
	// Judge returns 1.0 when the model answer is semantically equivalent to
	// the expected answer, 0.0 when it is not, and an error on failure.
	Judge(ctx context.Context, question, answer, expected string) (float64, string, error)
}

// LLMJudgeConfig configures the judge engine. Provider/model/base URL follow
// the same conventions as engine.LLMChatConfig.
type LLMJudgeConfig struct {
	Provider  string
	APIKey    string
	BaseURL   string
	AuthMode  string
	Model     string
	Timeout   time.Duration
	MaxTokens int
}

// NewLLMJudge creates a judge backed by engine.NewLLMChatEngine.
func NewLLMJudge(cfg LLMJudgeConfig) LLMJudge {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 256
	}
	return &engineLLMJudge{cfg: cfg}
}

type engineLLMJudge struct {
	cfg LLMJudgeConfig
}

// Judge implements LLMJudge.
func (j *engineLLMJudge) Judge(ctx context.Context, question, answer, expected string) (float64, string, error) {
	llmCfg := engine.DefaultLLMChatConfig()
	llmCfg.Provider = strings.TrimSpace(j.cfg.Provider)
	llmCfg.APIKey = strings.TrimSpace(j.cfg.APIKey)
	llmCfg.BaseURL = strings.TrimSpace(j.cfg.BaseURL)
	llmCfg.AuthMode = strings.TrimSpace(j.cfg.AuthMode)
	llmCfg.Model = strings.TrimSpace(j.cfg.Model)
	llmCfg.Timeout = j.cfg.Timeout
	llmCfg.MaxTokens = j.cfg.MaxTokens
	// Temperature 0 for reproducible binary verdicts.
	llmCfg.Temperature = 0.0

	llm, err := engine.NewLLMChatEngine(llmCfg)
	if err != nil {
		return 0, "", fmt.Errorf("create judge engine: %w", err)
	}

	out, err := llm.Run(ctx, engine.EngineInput{
		SystemPrompt: llmJudgeSystemPrompt,
		Messages:     []engine.Message{engine.NewUserMessage(llmJudgeUserPrompt(question, answer, expected))},
	})
	if err != nil {
		return 0, "", fmt.Errorf("judge run: %w", err)
	}

	text := strings.TrimSpace(out.AssistantText)
	if text == "" {
		return 0, "", fmt.Errorf("judge returned empty response")
	}

	score, reason, err := parseLLMJudgeVerdict(text)
	if err != nil {
		return 0, text, err
	}
	return score, reason, nil
}

const llmJudgeSystemPrompt = `You are a strict semantic-equivalence judge for question-answering evaluations.

Your job is to compare a MODEL ANSWER to an EXPECTED ANSWER for a given QUESTION and decide whether they are semantically equivalent.

Rules:
- YES if the model answer contains the same factual content as the expected answer, even if phrased differently.
- YES if the model answer is a paraphrase that conveys the same core fact(s).
- YES if the model answer correctly refuses when the expected answer indicates the question is unanswerable.
- NO if the model answer states a wrong number, date, name, or other key fact.
- NO if the model answer refuses to answer when the expected answer is available.
- NO if the model answer adds contradictory information.
- When the expected answer is a number, the model answer must state the same number (or an exact equivalent) to be YES.

Respond in exactly this format and no other:

VERDICT: YES or NO
REASON: one concise sentence explaining your decision`

func llmJudgeUserPrompt(question, answer, expected string) string {
	var b strings.Builder
	b.WriteString("QUESTION: ")
	b.WriteString(strings.TrimSpace(question))
	b.WriteString("\n\nEXPECTED ANSWER: ")
	b.WriteString(strings.TrimSpace(expected))
	b.WriteString("\n\nMODEL ANSWER: ")
	b.WriteString(strings.TrimSpace(answer))
	b.WriteString("\n\nRespond with VERDICT and REASON only.")
	return b.String()
}

var judgeVerdictPattern = regexp.MustCompile(`(?i)^\s*VERDICT:\s*(YES|NO)\b`)

func parseLLMJudgeVerdict(text string) (float64, string, error) {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := judgeVerdictPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		verdict := strings.ToUpper(strings.TrimSpace(m[1]))
		var score float64
		switch verdict {
		case "YES":
			score = 1.0
		case "NO":
			score = 0.0
		default:
			return 0, text, fmt.Errorf("unrecognized judge verdict: %q", verdict)
		}
		// Extract reason from remaining lines for context.
		reason := extractJudgeReason(lines)
		return score, reason, nil
	}
	return 0, text, fmt.Errorf("no VERDICT line found in judge output")
}

func extractJudgeReason(lines []string) string {
	for _, line := range lines {
		line = strings.TrimSpace(line)
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "REASON:") {
			return strings.TrimSpace(line[len("REASON:"):])
		}
	}
	return ""
}

// JudgeAnswerFunc adapts an LLMJudge to the Deps.JudgeAnswer signature.
func JudgeAnswerFunc(judge LLMJudge) func(ctx context.Context, question, answer, expected string) (float64, string, error) {
	if judge == nil {
		return nil
	}
	return func(ctx context.Context, question, answer, expected string) (float64, string, error) {
		return judge.Judge(ctx, question, answer, expected)
	}
}

// RescoreReportWithLLMJudge re-scores the answers in an existing RunResult
// using the supplied LLM judge. It mutates the result in place: each case
// with a non-empty answer gets AnswerJudgeScore/AnswerJudgeReason, and if
// the deterministic scorer previously marked it as not matched, a YES judge
// verdict upgrades AnswerScore, AnswerMatched, and AnswerMethod. Metrics are
// recomputed after all cases are judged.
func RescoreReportWithLLMJudge(ctx context.Context, result *RunResult, judge LLMJudge) error {
	if judge == nil {
		return fmt.Errorf("judge is required")
	}
	if result == nil {
		return fmt.Errorf("result is required")
	}
	judgeFn := JudgeAnswerFunc(judge)
	for i := range result.Cases {
		c := &result.Cases[i]
		if strings.TrimSpace(c.Answer) == "" || strings.TrimSpace(c.ExpectedAnswer) == "" {
			continue
		}
		score, reason, err := judgeFn(ctx, c.Question, c.Answer, c.ExpectedAnswer)
		if err != nil {
			c.AnswerJudgeReason = fmt.Sprintf("judge error: %v", err)
			continue
		}
		c.AnswerJudgeScore = score
		c.AnswerJudgeReason = reason
		if !c.AnswerMatched && score > 0 {
			c.AnswerScore = score
			c.AnswerMatched = true
			if c.AnswerMethod == "" {
				c.AnswerMethod = "judge"
			} else if !strings.HasSuffix(c.AnswerMethod, "-judge") {
				c.AnswerMethod = c.AnswerMethod + "-judge"
			}
		}
	}
	if result.Metrics != nil {
		m := Summarize(result.Cases)
		mergeAnswerMetrics(&m, result.Cases)
		// Preserve retrieval-only fields that Summarize recomputes.
		result.Metrics = &m
	}
	return nil
}
