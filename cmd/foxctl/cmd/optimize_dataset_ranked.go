package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/agent/optimization"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/storage/cas"
	"github.com/spf13/cobra"
)

func newOptimizeDatasetExportRankedCommand() *cobra.Command {
	var (
		workspace   string
		limit       int
		minScoreGap float64
		granularity string
		outputFile  string
		toCAS       bool
		dryRun      bool
	)

	cmd := &cobra.Command{
		Use:   "export-ranked",
		Short: "Export ranked preference examples from saved prompt comparison runs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, optimizeDatasetCommand, err.Error())
			}

			absWorkspace, err := absWorkspaceOrWriteError(out, optimizeDatasetCommand, workspace)
			if err != nil {
				return err
			}

			runStore, err := optimization.OpenPromptComparisonRunStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("open prompt comparison run store: %v", err))
			}
			defer runStore.Close() //nolint:errcheck

			variantStore, err := optimization.OpenPromptVariantStore(ctx, cfg.Storage.Root)
			if err != nil {
				return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("open prompt variant store: %v", err))
			}
			defer variantStore.Close() //nolint:errcheck

			runs, err := runStore.List(ctx, absWorkspace, limit)
			if err != nil {
				return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("list comparison runs: %v", err))
			}

			examples, err := buildPromptPreferenceExamples(ctx, runs, variantStore, cfg.Paths.CAS, minScoreGap, granularity)
			if err != nil {
				return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("build ranked dataset: %v", err))
			}

			plan, err := planPromptPreferenceExport(absWorkspace, runs, examples, minScoreGap, granularity, outputFile, toCAS)
			if err != nil {
				return writeOptimizeError(out, optimizeDatasetCommand, fmt.Sprintf("plan ranked dataset export: %v", err))
			}
			data, artifact, err := applyPromptPreferenceExport(ctx, cfg.Paths.CAS, plan, dryRun)
			if err != nil {
				return writeOptimizeError(out, optimizeDatasetCommand, err.Error())
			}
			return protocol.WriteOK(
				out, optimizeDatasetCommand, data,
				protocol.WithSource("run"),
				protocol.WithWorkspace(absWorkspace),
				protocol.WithCASDigest(artifact),
			)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum comparison runs to inspect")
	cmd.Flags().Float64Var(&minScoreGap, "min-score-gap", 0.05, "Minimum mean-score gap required to emit a preference pair")
	cmd.Flags().StringVar(&granularity, "granularity", "run", "Preference granularity: run or case")
	cmd.Flags().StringVar(&outputFile, "output-file", "", "Write ranked dataset JSONL to a file")
	cmd.Flags().BoolVar(&toCAS, "to-cas", false, "Write ranked dataset JSONL to CAS and return a digest")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview ranked dataset export without writing output")
	return cmd
}

type promptPreferenceExportPlan struct {
	Data       map[string]any
	Examples   []optimization.PromptPreferenceExample
	OutputFile string
	ToCAS      bool
	Candidate  string
}

func planPromptPreferenceExport(
	workspace string,
	runs []optimization.PromptComparisonRun,
	examples []optimization.PromptPreferenceExample,
	minScoreGap float64,
	granularity string,
	outputFile string,
	toCAS bool,
) (promptPreferenceExportPlan, error) {
	body, err := optimization.BuildPromptPreferenceDatasetJSONL(examples)
	if err != nil {
		return promptPreferenceExportPlan{}, err
	}
	sum := sha256.Sum256(body)
	candidate := "sha256:" + hex.EncodeToString(sum[:])
	data := map[string]any{
		"operation":                 "dataset.export-ranked",
		"workspace_id":              workspace,
		"run_count":                 len(runs),
		"example_count":             len(examples),
		"min_score_gap":             minScoreGap,
		"granularity":               normalizePromptPreferenceGranularity(granularity),
		"artifact_digest_candidate": candidate,
		"cli_command":               "foxctl optimize dataset export-ranked",
	}
	if strings.TrimSpace(outputFile) != "" {
		data["output_file"] = outputFile
	}
	if toCAS {
		data["to_cas"] = true
	}
	if strings.TrimSpace(outputFile) == "" && !toCAS {
		data["examples"] = examples
	}
	return promptPreferenceExportPlan{
		Data:       data,
		Examples:   examples,
		OutputFile: strings.TrimSpace(outputFile),
		ToCAS:      toCAS,
		Candidate:  candidate,
	}, nil
}

func applyPromptPreferenceExport(ctx context.Context, casRoot string, plan promptPreferenceExportPlan, dryRun bool) (map[string]any, string, error) {
	data := cloneMap(plan.Data)
	data["dry_run"] = dryRun
	if dryRun {
		if plan.OutputFile != "" {
			data["would_write_file"] = true
		}
		if plan.ToCAS {
			data["would_write_cas"] = true
		}
		return data, "", nil
	}

	if plan.OutputFile != "" {
		if err := savePromptPreferenceDatasetFile(plan.OutputFile, plan.Examples); err != nil {
			return nil, "", fmt.Errorf("write ranked dataset file: %v", err)
		}
	}
	if plan.ToCAS {
		artifact, err := persistPromptPreferenceDatasetArtifact(ctx, casRoot, plan.Examples)
		if err != nil {
			return nil, "", fmt.Errorf("persist ranked dataset artifact: %v", err)
		}
		data["artifact"] = artifact
		return data, artifact, nil
	}
	if plan.OutputFile == "" {
		data["examples"] = plan.Examples
	}

	return data, "", nil
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func buildPromptPreferenceExamples(
	ctx context.Context,
	runs []optimization.PromptComparisonRun,
	variantStore optimization.PromptVariantStore,
	casRoot string,
	minScoreGap float64,
	granularity string,
) ([]optimization.PromptPreferenceExample, error) {
	if minScoreGap < 0 {
		minScoreGap = 0
	}
	granularity = normalizePromptPreferenceGranularity(granularity)

	examples := make([]optimization.PromptPreferenceExample, 0, len(runs))
	for _, run := range runs {
		report, err := readPromptComparisonArtifact(ctx, casRoot, run.ArtifactDigest)
		if err != nil {
			return nil, err
		}

		var ranking []promptVariantAggregate
		if err := decodeReportField(report, "ranking", &ranking); err != nil || len(ranking) < 2 {
			continue
		}
		top := ranking[0]
		bottom := ranking[len(ranking)-1]
		if (top.MeanScore - bottom.MeanScore) < minScoreGap {
			continue
		}

		chosenVariant, err := variantStore.Get(ctx, run.WorkspaceID, top.VariantID)
		if err != nil {
			return nil, err
		}
		rejectedVariant, err := variantStore.Get(ctx, run.WorkspaceID, bottom.VariantID)
		if err != nil {
			return nil, err
		}

		resultsByVariant := map[string][]optimization.PromptPreferenceModel{}
		resultsByVariantCase := map[string]map[string][]optimization.PromptPreferenceModel{}
		var rawResults []promptVariantComparison
		if err := decodeReportField(report, "results", &rawResults); err == nil {
			for _, result := range rawResults {
				modelResult := optimization.PromptPreferenceModel{
					Model:  result.Model,
					Output: result.Output,
					Error:  result.Error,
					Score:  result.Score,
					Passed: result.Passed,
				}
				resultsByVariant[result.VariantID] = append(resultsByVariant[result.VariantID], modelResult)
				evalCaseID := strings.TrimSpace(result.EvalCaseID)
				if evalCaseID != "" {
					if _, ok := resultsByVariantCase[result.VariantID]; !ok {
						resultsByVariantCase[result.VariantID] = map[string][]optimization.PromptPreferenceModel{}
					}
					resultsByVariantCase[result.VariantID][evalCaseID] = append(resultsByVariantCase[result.VariantID][evalCaseID], modelResult)
				}
			}
		}
		for _, item := range resultsByVariant {
			sort.Slice(item, func(i, j int) bool { return item[i].Model < item[j].Model })
		}

		scoring, _ := report["scoring"].(map[string]any)
		question, _ := report["question"].(string)
		contextText, _ := report["context"].(string)

		if granularity == "case" {
			var evalCases []promptEvalCase
			if err := decodeReportField(report, "eval_cases", &evalCases); err == nil && len(evalCases) > 0 {
				for _, evalCase := range evalCases {
					evalCaseID := strings.TrimSpace(evalCase.ID)
					type caseVariantAggregate struct {
						Variant optimization.PromptVariant
						Models  []optimization.PromptPreferenceModel
						Mean    float64
						Worst   float64
						Passes  int
					}
					caseVariants := make([]caseVariantAggregate, 0, len(resultsByVariantCase))
					for variantID, byCase := range resultsByVariantCase {
						models := byCase[evalCaseID]
						if len(models) == 0 {
							continue
						}
						variant, err := variantStore.Get(ctx, run.WorkspaceID, variantID)
						if err != nil {
							return nil, err
						}
						mean, worst, passCount := promptPreferenceAggregateModels(models)
						caseVariants = append(caseVariants, caseVariantAggregate{
							Variant: variant,
							Models:  models,
							Mean:    mean,
							Worst:   worst,
							Passes:  passCount,
						})
					}
					if len(caseVariants) < 2 {
						continue
					}
					sort.Slice(caseVariants, func(i, j int) bool {
						if caseVariants[i].Mean != caseVariants[j].Mean {
							return caseVariants[i].Mean > caseVariants[j].Mean
						}
						if caseVariants[i].Worst != caseVariants[j].Worst {
							return caseVariants[i].Worst > caseVariants[j].Worst
						}
						if caseVariants[i].Passes != caseVariants[j].Passes {
							return caseVariants[i].Passes > caseVariants[j].Passes
						}
						return caseVariants[i].Variant.ID < caseVariants[j].Variant.ID
					})
					chosenCase := caseVariants[0]
					rejectedCase := caseVariants[len(caseVariants)-1]
					if (chosenCase.Mean - rejectedCase.Mean) < minScoreGap {
						continue
					}
					examples = append(examples, optimization.PromptPreferenceExample{
						RecordType: "prompt_preference_case",
						Input: optimization.PromptPreferenceInput{
							Question:       strings.TrimSpace(evalCase.Question),
							Context:        strings.TrimSpace(evalCase.Context),
							TargetResponse: strings.TrimSpace(evalCase.TargetResponse),
							EvalCaseID:     evalCaseID,
							Category:       strings.TrimSpace(evalCase.Category),
						},
						Chosen: optimization.PromptPreferenceCandidate{
							VariantID:      chosenCase.Variant.ID,
							AgentRole:      chosenCase.Variant.AgentRole,
							Mode:           chosenCase.Variant.Mode,
							Prompt:         chosenCase.Variant.Prompt,
							MeanScore:      chosenCase.Mean,
							WorstScore:     chosenCase.Worst,
							PassCount:      chosenCase.Passes,
							OutputsByModel: chosenCase.Models,
						},
						Rejected: optimization.PromptPreferenceCandidate{
							VariantID:      rejectedCase.Variant.ID,
							AgentRole:      rejectedCase.Variant.AgentRole,
							Mode:           rejectedCase.Variant.Mode,
							Prompt:         rejectedCase.Variant.Prompt,
							MeanScore:      rejectedCase.Mean,
							WorstScore:     rejectedCase.Worst,
							PassCount:      rejectedCase.Passes,
							OutputsByModel: rejectedCase.Models,
						},
						Metadata: optimization.PromptPreferenceExampleMeta{
							RunID:          run.ID,
							ArtifactDigest: run.ArtifactDigest,
							Provider:       run.Provider,
							BaseURL:        run.BaseURL,
							Granularity:    "case",
							EvalCaseID:     evalCaseID,
							Category:       strings.TrimSpace(evalCase.Category),
							Scoring:        scoring,
						},
					})
				}
				continue
			}
		}

		examples = append(examples, optimization.PromptPreferenceExample{
			RecordType: "prompt_preference",
			Input: optimization.PromptPreferenceInput{
				Question: strings.TrimSpace(question),
				Context:  strings.TrimSpace(contextText),
			},
			Chosen: optimization.PromptPreferenceCandidate{
				VariantID:      chosenVariant.ID,
				AgentRole:      chosenVariant.AgentRole,
				Mode:           chosenVariant.Mode,
				Prompt:         chosenVariant.Prompt,
				MeanScore:      top.MeanScore,
				WorstScore:     top.WorstScore,
				PassCount:      top.PassCount,
				OutputsByModel: resultsByVariant[chosenVariant.ID],
			},
			Rejected: optimization.PromptPreferenceCandidate{
				VariantID:      rejectedVariant.ID,
				AgentRole:      rejectedVariant.AgentRole,
				Mode:           rejectedVariant.Mode,
				Prompt:         rejectedVariant.Prompt,
				MeanScore:      bottom.MeanScore,
				WorstScore:     bottom.WorstScore,
				PassCount:      bottom.PassCount,
				OutputsByModel: resultsByVariant[rejectedVariant.ID],
			},
			Metadata: optimization.PromptPreferenceExampleMeta{
				RunID:          run.ID,
				ArtifactDigest: run.ArtifactDigest,
				Provider:       run.Provider,
				BaseURL:        run.BaseURL,
				Granularity:    "run",
				Scoring:        scoring,
			},
		})
	}
	return examples, nil
}

func normalizePromptPreferenceGranularity(granularity string) string {
	switch strings.ToLower(strings.TrimSpace(granularity)) {
	case "case":
		return "case"
	default:
		return "run"
	}
}

func promptPreferenceAggregateModels(models []optimization.PromptPreferenceModel) (mean, worst float64, passCount int) {
	if len(models) == 0 {
		return 0, 0, 0
	}
	total := 0.0
	worst = models[0].Score
	for _, item := range models {
		total += item.Score
		if item.Score < worst {
			worst = item.Score
		}
		if item.Passed {
			passCount++
		}
	}
	mean = total / float64(len(models))
	return mean, worst, passCount
}

func decodeReportField(report map[string]any, key string, dest any) error {
	raw, ok := report[key]
	if !ok || raw == nil {
		return fmt.Errorf("report field %q missing", key)
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dest)
}

func savePromptPreferenceDatasetFile(path string, examples []optimization.PromptPreferenceExample) error {
	return optimization.SavePromptPreferenceDatasetFile(path, examples)
}

func persistPromptPreferenceDatasetArtifact(ctx context.Context, casRoot string, examples []optimization.PromptPreferenceExample) (string, error) {
	store, err := cas.NewStore(casRoot)
	if err != nil {
		return "", err
	}
	defer store.Close()
	body, err := optimization.BuildPromptPreferenceDatasetJSONL(examples)
	if err != nil {
		return "", err
	}
	obj, err := store.Put(ctx, strings.NewReader(string(body)), "application/jsonl", []string{"gepa", "prompt-preference-dataset"})
	if err != nil {
		return "", err
	}
	return obj.Digest, nil
}
