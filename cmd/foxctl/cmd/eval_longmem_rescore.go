package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/tooling/evals/longmemeval"
	"github.com/spf13/cobra"
)

// newEvalLongmemRescoreCommand returns a subcommand that re-scores an
// existing longmem answer-mode artifact with an LLM judge, without
// re-running retrieval or answer generation.
func newEvalLongmemRescoreCommand() *cobra.Command {
	var (
		artifactDir   string
		judgeProvider string
		judgeModel    string
		judgeBaseURL  string
		judgeAPIKey   string
		judgeTimeout  time.Duration
	)

	cmd := &cobra.Command{
		Use:   "rescore",
		Short: "Re-score existing longmem answer-mode artifacts with an LLM judge",
		Long: strings.TrimSpace(`Load a report.json produced by 'foxctl eval longmem --mode answer' and
re-score every answer with an LLM semantic-equivalence judge. The original
answer text is preserved; only AnswerJudgeScore, AnswerJudgeReason, and (on
judge YES for previously-failed cases) AnswerScore/AnswerMatched/AnswerMethod
are updated. Updated report.json and per-case files are written back to the
same artifact directory.`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			artifactDir = strings.TrimSpace(artifactDir)
			if artifactDir == "" {
				return fmt.Errorf("--artifact-dir is required")
			}
			reportPath := filepath.Join(artifactDir, "report.json")
			body, err := os.ReadFile(reportPath)
			if err != nil {
				return fmt.Errorf("read report: %w", err)
			}
			var result longmemeval.RunResult
			if err := json.Unmarshal(body, &result); err != nil {
				return fmt.Errorf("decode report: %w", err)
			}
			if judgeTimeout <= 0 {
				judgeTimeout = 30 * time.Second
			}
			judge := longmemeval.NewLLMJudge(longmemeval.LLMJudgeConfig{
				Provider:  judgeProvider,
				Model:     judgeModel,
				BaseURL:   judgeBaseURL,
				APIKey:    judgeAPIKey,
				Timeout:   judgeTimeout,
				MaxTokens: 256,
			})
			if err := longmemeval.RescoreReportWithLLMJudge(ctx, &result, judge); err != nil {
				return fmt.Errorf("rescore: %w", err)
			}
			if err := longmemeval.WriteArtifacts(artifactDir, result); err != nil {
				return fmt.Errorf("write artifacts: %w", err)
			}
			data := map[string]any{
				"artifact_dir": artifactDir,
				"report_path":  reportPath,
				"result":       result,
			}
			if result.Metrics != nil {
				data["answer_accuracy"] = result.Metrics.AnswerAccuracy
				data["answer_judge_accuracy"] = result.Metrics.AnswerJudgeAccuracy
				data["answer_judge_case_count"] = result.Metrics.AnswerJudgeCaseCount
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "eval/longmem/rescore", data, protocol.WithSource("cli"))
		},
	}

	cmd.Flags().StringVar(&artifactDir, "artifact-dir", "", "Artifact directory containing report.json (required)")
	cmd.Flags().StringVar(&judgeProvider, "judge-provider", "", "Judge LLM provider, e.g. lmstudio, openai_compat, or anthropic_compat")
	cmd.Flags().StringVar(&judgeModel, "judge-model", "", "Judge LLM model")
	cmd.Flags().StringVar(&judgeBaseURL, "judge-base-url", "", "Judge LLM base URL")
	cmd.Flags().StringVar(&judgeAPIKey, "judge-api-key", "", "Judge LLM API key")
	cmd.Flags().DurationVar(&judgeTimeout, "judge-timeout", 30*time.Second, "Judge timeout per case")
	_ = cmd.MarkFlagRequired("artifact-dir")
	return cmd
}
