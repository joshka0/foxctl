package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	obs "github.com/joshka0/foxctl/internal/adapters/skillslib/obs"
	"github.com/joshka0/foxctl/internal/agent/optimization"
	agentruntime "github.com/joshka0/foxctl/internal/agent/runtime"
	agenttypes "github.com/joshka0/foxctl/internal/agent/types"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
)

type agentEvalResult struct {
	CaseID            string                `json:"case_id,omitempty"`
	Category          string                `json:"category,omitempty"`
	Role              string                `json:"role,omitempty"`
	Label             string                `json:"label,omitempty"`
	Provider          string                `json:"provider,omitempty"`
	Model             string                `json:"model,omitempty"`
	Runner            string                `json:"runner,omitempty"`
	Status            string                `json:"status,omitempty"`
	Output            string                `json:"output,omitempty"`
	DurationMS        int64                 `json:"duration_ms,omitempty"`
	ToolCallCount     int                   `json:"tool_call_count,omitempty"`
	ToolNames         []string              `json:"tool_names,omitempty"`
	InputTokens       int                   `json:"input_tokens,omitempty"`
	OutputTokens      int                   `json:"output_tokens,omitempty"`
	TotalTokens       int                   `json:"total_tokens,omitempty"`
	TotalCostUSD      float64               `json:"total_cost_usd,omitempty"`
	QualityScore      float64               `json:"quality_score,omitempty"`
	AccuracyScore     float64               `json:"accuracy_score,omitempty"`
	ThoroughnessScore float64               `json:"thoroughness_score,omitempty"`
	PathRecall        float64               `json:"path_recall,omitempty"`
	SymbolRecall      float64               `json:"symbol_recall,omitempty"`
	SnippetRecall     float64               `json:"snippet_recall,omitempty"`
	FactRecall        float64               `json:"fact_recall,omitempty"`
	CorrectnessScore  float64               `json:"correctness_score,omitempty"`
	PathMatchCount    int                   `json:"path_match_count,omitempty"`
	MatchedPaths      []string              `json:"matched_paths,omitempty"`
	MatchedSymbols    []string              `json:"matched_symbols,omitempty"`
	MatchedSnippets   []string              `json:"matched_snippets,omitempty"`
	MatchedFacts      []string              `json:"matched_facts,omitempty"`
	Symbols           []string              `json:"symbols,omitempty"`
	Snippets          []evalObservedSnippet `json:"snippets,omitempty"`
	InvalidPaths      []string              `json:"invalid_paths,omitempty"`
	ExcludedPathHits  []string              `json:"excluded_path_hits,omitempty"`
	WrongScopePenalty float64               `json:"wrong_scope_penalty,omitempty"`
	LengthQuality     float64               `json:"length_quality,omitempty"`
	GenericPenalty    float64               `json:"generic_penalty,omitempty"`
	Passed            bool                  `json:"passed,omitempty"`
	Error             string                `json:"error,omitempty"`
}

type agentEvalSummary struct {
	Label             string  `json:"label"`
	Role              string  `json:"role,omitempty"`
	Provider          string  `json:"provider,omitempty"`
	Model             string  `json:"model,omitempty"`
	Runner            string  `json:"runner,omitempty"`
	Count             int     `json:"count"`
	PassRate          float64 `json:"pass_rate"`
	MeanQuality       float64 `json:"mean_quality"`
	MeanCorrectness   float64 `json:"mean_correctness"`
	MeanAccuracy      float64 `json:"mean_accuracy"`
	MeanThoroughness  float64 `json:"mean_thoroughness"`
	MeanPathRecall    float64 `json:"mean_path_recall"`
	MeanSymbolRecall  float64 `json:"mean_symbol_recall"`
	MeanSnippetRecall float64 `json:"mean_snippet_recall"`
	MeanFactRecall    float64 `json:"mean_fact_recall"`
	MeanWrongScope    float64 `json:"mean_wrong_scope_penalty"`
	MeanDurationMS    float64 `json:"mean_duration_ms"`
	MeanTokens        float64 `json:"mean_tokens"`
	MeanCostUSD       float64 `json:"mean_cost_usd"`
	ErrorCount        int     `json:"error_count"`
}

type agentEvalTarget struct {
	Label                   string `json:"label"`
	Provider                string `json:"provider"`
	BaseURL                 string `json:"base_url,omitempty"`
	APIKey                  string `json:"api_key,omitempty"`
	Model                   string `json:"model"`
	Runner                  string `json:"runner"`
	SupportsRequiredToolUse bool   `json:"supports_required_tool_use,omitempty"`
}

type externalAgentEvalRecord struct {
	CaseID       string  `json:"case_id,omitempty"`
	Category     string  `json:"category,omitempty"`
	Role         string  `json:"role,omitempty"`
	Label        string  `json:"label,omitempty"`
	Provider     string  `json:"provider,omitempty"`
	Model        string  `json:"model,omitempty"`
	Runner       string  `json:"runner,omitempty"`
	Output       string  `json:"output"`
	DurationMS   int64   `json:"duration_ms,omitempty"`
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	TotalTokens  int     `json:"total_tokens,omitempty"`
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
	Error        string  `json:"error,omitempty"`
}

type structuredAgentEvalOutput struct {
	Summary  string   `json:"summary"`
	Paths    []string `json:"paths"`
	Symbols  []string `json:"symbols,omitempty"`
	Snippets []struct {
		Path      string `json:"path,omitempty"`
		StartLine int    `json:"start_line,omitempty"`
		EndLine   int    `json:"end_line,omitempty"`
		Reason    string `json:"reason,omitempty"`
	} `json:"snippets,omitempty"`
	Facts     []string `json:"facts,omitempty"`
	Rationale string   `json:"rationale,omitempty"`
}

type structuredScoutJudgeOutput struct {
	Summary         string `json:"summary"`
	CurrentBestView string `json:"current_best_view,omitempty"`
	Claims          []struct {
		Key   string `json:"key,omitempty"`
		Value string `json:"value,omitempty"`
	} `json:"claims,omitempty"`
	Timeline []struct {
		Value string `json:"value,omitempty"`
	} `json:"timeline,omitempty"`
	ContextBlocks []struct {
		Summary string `json:"summary,omitempty"`
	} `json:"context_blocks,omitempty"`
	Gaps []string `json:"gaps,omitempty"`
}

func newEvalAgentsCommand() *cobra.Command {
	var (
		workspace       string
		evalDatasetFile string
		roles           []string
		defaultProvider string
		models          []string
		targets         []string
		externalResults []string
		agentRef        string
		conversationID  string
		vaultPath       string
		timeout         time.Duration
		maxIterations   int
		passThreshold   float64
		reportFile      string
	)

	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Compare agent roles across model targets using a prompt-eval dataset",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, "eval.agents", err.Error())
			}
			absWorkspace, err := absWorkspaceOrWriteError(out, "eval.agents", workspace)
			if err != nil {
				return err
			}

			evalCases, err := loadPromptEvalCases(evalDatasetFile)
			if err != nil {
				return writeOptimizeError(out, "eval.agents", fmt.Sprintf("load eval dataset: %v", err))
			}
			if len(evalCases) == 0 {
				return writeOptimizeError(out, "eval.agents", "eval-dataset-file must contain at least one case")
			}

			resolvedRoles := normalizeAgentEvalRoles(roles)
			if len(resolvedRoles) == 0 {
				resolvedRoles = []string{
					string(agenttypes.RoleMemoryFactScout),
					string(agenttypes.RoleMemoryTimelineScout),
					string(agenttypes.RoleACAContextScout),
					string(agenttypes.RoleSubcallWorker),
					string(agenttypes.RoleResearcher),
				}
			}
			if requiresMemoryAgentRef(resolvedRoles) && strings.TrimSpace(agentRef) == "" {
				return writeOptimizeError(out, "eval.agents", "memory scout evals require --agent-ref so agent_memory_* tools know which memory lineage to inspect")
			}

			resolvedTargets, err := resolveAgentEvalTargets(cfg, defaultProvider, models, targets)
			if err != nil {
				return writeOptimizeError(out, "eval.agents", err.Error())
			}

			results := make([]agentEvalResult, 0, len(evalCases)*len(resolvedRoles)*len(resolvedTargets))
			withTemporaryVaultEnv(vaultPath, func() {
				for _, role := range resolvedRoles {
					for _, target := range resolvedTargets {
						for _, evalCase := range evalCases {
							result := runSingleAgentEval(ctx, cfg, absWorkspace, target, role, evalCase, agentRef, conversationID, timeout, maxIterations)
							result.Passed = shouldPassAgentEval(result, evalCase, passThreshold)
							results = append(results, result)
						}
					}
				}
			})

			imported, err := loadExternalAgentEvalResults(externalResults, evalCases, passThreshold)
			if err != nil {
				return writeOptimizeError(out, "eval.agents", fmt.Sprintf("load external results: %v", err))
			}
			results = append(results, imported...)

			summaries := summarizeAgentEvalResults(results)
			report := map[string]any{
				"operation":             "eval.agents",
				"workspace_id":          absWorkspace,
				"role_count":            len(resolvedRoles),
				"roles":                 resolvedRoles,
				"target_count":          len(resolvedTargets),
				"targets":               resolvedTargets,
				"eval_case_count":       len(evalCases),
				"eval_cases":            evalCases,
				"memory_agent_ref":      strings.TrimSpace(agentRef),
				"conversation_id":       strings.TrimSpace(conversationID),
				"vault_path":            strings.TrimSpace(vaultPath),
				"pass_threshold":        passThreshold,
				"results":               results,
				"summaries":             summaries,
				"external_result_files": append([]string(nil), externalResults...),
				"cli_command":           cmd.CommandPath(),
			}

			if strings.TrimSpace(reportFile) != "" {
				payload, err := json.MarshalIndent(report, "", "  ")
				if err != nil {
					return writeOptimizeError(out, "eval.agents", fmt.Sprintf("marshal report: %v", err))
				}
				if err := os.WriteFile(reportFile, append(payload, '\n'), 0o644); err != nil {
					return writeOptimizeError(out, "eval.agents", fmt.Sprintf("write report: %v", err))
				}
			}

			return protocol.WriteOK(out, "eval.agents", map[string]any{
				"markdown": renderAgentEvalMarkdown(absWorkspace, evalCases, summaries, results),
				"report":   report,
			}, protocol.WithSource("run"), protocol.WithWorkspace(absWorkspace))
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringVar(&evalDatasetFile, "eval-dataset-file", "", "JSONL eval dataset file with question/context/target_response rows")
	cmd.Flags().StringSliceVar(&roles, "role", nil, "Agent role to evaluate (repeatable)")
	cmd.Flags().StringVar(&defaultProvider, "provider", "lmstudio", "Default provider for --model entries")
	cmd.Flags().StringSliceVar(&models, "model", nil, "Model ID using the default provider (repeatable)")
	cmd.Flags().StringSliceVar(&targets, "target", nil, "Explicit benchmark target in provider:model format (repeatable)")
	cmd.Flags().StringSliceVar(&externalResults, "external-results", nil, "Optional JSONL file of external baseline results to merge into the report (repeatable)")
	cmd.Flags().StringVar(&agentRef, "agent-ref", "", "Existing agent ID/slug/name whose memory lineage the memory scouts should inspect")
	cmd.Flags().StringVar(&conversationID, "conversation-id", "", "Optional conversation lineage override for agent_memory_* tool calls")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Optional vault path to expose to ACA/Obsidian tools via environment")
	cmd.Flags().DurationVar(&timeout, "timeout", 90*time.Second, "Per-run timeout")
	cmd.Flags().IntVar(&maxIterations, "max-iterations", 6, "Per-run max iterations")
	cmd.Flags().Float64Var(&passThreshold, "pass-threshold", 0.8, "Quality threshold for pass/fail")
	cmd.Flags().StringVar(&reportFile, "report-file", "", "Optional path to write the JSON report")
	_ = cmd.MarkFlagRequired("eval-dataset-file")
	return cmd
}

func normalizeAgentEvalRoles(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, role := range in {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		if _, ok := seen[role]; ok {
			continue
		}
		seen[role] = struct{}{}
		out = append(out, role)
	}
	return out
}

func requiresMemoryAgentRef(roles []string) bool {
	for _, role := range roles {
		switch strings.TrimSpace(role) {
		case string(agenttypes.RoleMemoryFactScout), string(agenttypes.RoleMemoryTimelineScout):
			return true
		}
	}
	return false
}

func resolveAgentEvalTargets(cfg config.Config, defaultProvider string, models, targets []string) ([]agentEvalTarget, error) {
	resolvedSpecs, err := resolveClaudeLeaderboardTargets(defaultProvider, models, targets)
	if err != nil {
		return nil, err
	}
	if len(resolvedSpecs) == 0 {
		resolvedSpecs = []claudeLeaderboardTarget{
			{Provider: "openrouter", Model: "openai/gpt-5.4-nano"},
			{Provider: "openrouter", Model: "minimax/minimax-m2.7"},
			{Provider: "lmstudio", Model: "liquid/lfm2.5-1.2b"},
		}
	}

	out := make([]agentEvalTarget, 0, len(resolvedSpecs))
	for _, spec := range resolvedSpecs {
		resolved, err := resolvePromptComparisonTarget(cfg, spec.Provider, "", "", []string{spec.Model})
		if err != nil {
			return nil, err
		}
		model := ""
		if len(resolved.Models) > 0 {
			model = resolved.Models[0]
		}
		label := strings.TrimSpace(spec.Provider) + ":" + strings.TrimSpace(spec.Model)
		out = append(out, agentEvalTarget{
			Label:                   label,
			Provider:                resolved.Provider,
			BaseURL:                 resolved.BaseURL,
			APIKey:                  resolved.APIKey,
			Model:                   model,
			Runner:                  "native",
			SupportsRequiredToolUse: agentEvalSupportsRequiredToolUse(resolved.Provider, model),
		})
	}
	return out, nil
}

func agentEvalSupportsRequiredToolUse(provider, model string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.ToLower(strings.TrimSpace(model))
	if provider == "openrouter" && strings.Contains(model, "minimax/") {
		return false
	}
	return true
}

func withTemporaryVaultEnv(vaultPath string, fn func()) {
	vaultPath = strings.TrimSpace(vaultPath)
	if vaultPath == "" {
		fn()
		return
	}
	prevACA, hasACA := os.LookupEnv("AGENTCTL_ACA_VAULT_PATH")
	prevObs, hasObs := os.LookupEnv("AGENTCTL_OBSIDIAN_VAULT_PATH")
	_ = os.Setenv("AGENTCTL_ACA_VAULT_PATH", vaultPath)
	_ = os.Setenv("AGENTCTL_OBSIDIAN_VAULT_PATH", vaultPath)
	defer func() {
		if hasACA {
			_ = os.Setenv("AGENTCTL_ACA_VAULT_PATH", prevACA)
		} else {
			_ = os.Unsetenv("AGENTCTL_ACA_VAULT_PATH")
		}
		if hasObs {
			_ = os.Setenv("AGENTCTL_OBSIDIAN_VAULT_PATH", prevObs)
		} else {
			_ = os.Unsetenv("AGENTCTL_OBSIDIAN_VAULT_PATH")
		}
	}()
	fn()
}

func buildAgentEvalPrompt(role string, evalCase promptEvalCase, agentRef, conversationID string) string {
	var b strings.Builder
	b.WriteString("Use your available tools when needed to answer this question as accurately as possible.\n")
	b.WriteString("Return a concise answer with no markdown unless required by the question.\n")
	if strings.TrimSpace(agentRef) != "" {
		b.WriteString("Target memory agent ref: " + strings.TrimSpace(agentRef) + "\n")
	}
	if strings.TrimSpace(conversationID) != "" {
		b.WriteString("Target conversation lineage: " + strings.TrimSpace(conversationID) + "\n")
	}
	isCodingLocate := hasCodeCorrectnessExpectations(evalCase)
	isRefactorLocate := isRefactorEntryCase(evalCase)
	if isCodingLocate {
		b.WriteString("This is a repo-grounded file-location task.\n")
		if strings.TrimSpace(evalCase.TaskType) != "" {
			b.WriteString("Task type: " + strings.TrimSpace(evalCase.TaskType) + ".\n")
		}
		b.WriteString("Return the minimal exact repo-relative file set that is directly supported by tool evidence.\n")
		b.WriteString("Copy paths verbatim from tool output or file reads. If you cannot verify a path, omit it.\n")
		b.WriteString("Stop once you have a small verified set of real files; do not broaden into unrelated exploration.\n")
		b.WriteString("Avoid multiline regex in code_search or context_grep (for example \\n or [\\s\\S]). Use simple literal or symbol-name probes only.\n")
		b.WriteString("Avoid slash-heavy repoindex queries. Use short natural-language phrases or exact symbol names.\n")
		if isRefactorLocate {
			b.WriteString("This is a refactor-entrypoint task. You MUST call refactor_scout first, with an explicit single language, before any other repo tool.\n")
			b.WriteString("Use refactor_scout output to choose the 1-3 files or symbols worth verifying. Do not broaden into generic grep exploration first.\n")
		}
		if len(evalCase.ExcludedPaths) > 0 {
			b.WriteString("Treat these paths or prefixes as out of scope unless the question explicitly asks for them: " + strings.Join(evalCase.ExcludedPaths, ", ") + "\n")
		}
	}
	switch strings.TrimSpace(role) {
	case string(agenttypes.RoleMemoryFactScout):
		b.WriteString("Focus on explicit current facts, preferences, decisions, and technical context.\n")
		b.WriteString("Prefer semantic_search_memories before broader memory or session exploration.\n")
	case string(agenttypes.RoleMemoryTimelineScout):
		b.WriteString("Focus on changes over time, updates, retractions, and the current best view.\n")
		b.WriteString("Prefer semantic_search_sessions before broader timeline or memory exploration.\n")
	case string(agenttypes.RoleACAContextScout):
		b.WriteString("Focus on ACA top-of-mind, task continuity, and vault-backed durable context.\n")
		b.WriteString("Prefer semantic_search_context before broader ACA or vault exploration.\n")
	case string(agenttypes.RoleResearcher):
		if isCodingLocate {
			b.WriteString("Use only the shortest path to verified repo files: search, inspect the top evidence, and stop.\n")
			if isRefactorLocate {
				b.WriteString("Prefer refactor_scout, then semantic_search_code or smart_search, then fs_read_file for verification.\n")
			} else {
				b.WriteString("Prefer semantic_search_code, repo_index_search, smart_search, code_search, code_symbols, and fs_read_file over broad grep sweeps.\n")
			}
		} else {
			b.WriteString("Use your role's normal tool strategy.\n")
			b.WriteString("This is a memory-focused evaluation. Prefer the most direct memory or session tools available before broader repo, ACA, or vault exploration.\n")
			b.WriteString("If one direct memory lane already answers the question, stop there instead of broadening the search.\n")
		}
	case string(agenttypes.RoleSemanticScout):
		b.WriteString("Prefer semantic_search_code as your first discovery lane, then verify with smart_search.\n")
	case string(agenttypes.RoleSymbolScout):
		if isCodingLocate {
			if isRefactorLocate {
				b.WriteString("Use refactor_scout first with an explicit single language. Then verify the strongest 1-3 returned files with code_symbols or fs_read_file.\n")
			} else {
				b.WriteString("First locate candidate files with semantic_search_code or code_search. Only then run code_symbols on files you actually found.\n")
			}
			b.WriteString("Use context_grep only to verify the strongest 1-3 candidate files or caller sites, with simple single-line patterns only.\n")
		}
	default:
		if !isCodingLocate {
			b.WriteString("Use your role's normal tool strategy.\n")
			b.WriteString("This is a memory-focused evaluation. Prefer the most direct memory or session tools available before broader repo, ACA, or vault exploration.\n")
			b.WriteString("If one direct memory lane already answers the question, stop there instead of broadening the search.\n")
		}
	}
	if strings.TrimSpace(evalCase.Context) != "" {
		b.WriteString("\nContext:\n" + strings.TrimSpace(evalCase.Context) + "\n")
	}
	if hasCodeCorrectnessExpectations(evalCase) {
		b.WriteString("\nReturn JSON only with this exact shape:\n")
		b.WriteString("{\"summary\":\"...\",\"paths\":[\"repo/relative/path.go\"],\"symbols\":[\"path::SymbolName\"],\"snippets\":[{\"path\":\"repo/relative/path.go\",\"start_line\":1,\"end_line\":20,\"reason\":\"...\"}],\"facts\":[\"...\"],\"rationale\":\"...\"}\n")
		b.WriteString("The `paths` field must contain the exact repo-relative file paths that matter.\n")
		b.WriteString("Use `symbols` for exact symbol names when you can verify them. Prefer `path::SymbolName` when the file is known.\n")
		b.WriteString("Use `snippets` for the smallest verified code ranges that support your answer.\n")
		b.WriteString("Use `facts` for short concrete findings, not long prose.\n")
		b.WriteString("Every path must be a real path in this repository. Do not invent placeholder paths.\n")
		b.WriteString("Do not use session/, repo/, note:, or workspace/ placeholder prefixes unless they are real repo-relative paths.\n")
		b.WriteString("Do not wrap the JSON in markdown fences.\n")
	}
	b.WriteString("\nQuestion:\n" + strings.TrimSpace(evalCase.Question))
	return b.String()
}

func isRefactorEntryCase(evalCase promptEvalCase) bool {
	blob := strings.ToLower(strings.Join([]string{
		evalCase.Question,
		evalCase.Context,
		evalCase.TaskType,
		evalCase.Category,
	}, " "))
	return strings.Contains(blob, "refactor")
}

func runSingleAgentEval(ctx context.Context, cfg config.Config, workspace string, target agentEvalTarget, role string, evalCase promptEvalCase, agentRef, conversationID string, timeout time.Duration, maxIterations int) agentEvalResult {
	return runSingleAgentEvalAttempt(ctx, cfg, workspace, target, role, evalCase, agentRef, conversationID, timeout, maxIterations, false)
}

func runSingleAgentEvalAttempt(ctx context.Context, cfg config.Config, workspace string, target agentEvalTarget, role string, evalCase promptEvalCase, agentRef, conversationID string, timeout time.Duration, maxIterations int, forcedRetry bool) agentEvalResult {
	res := agentEvalResult{
		CaseID:   strings.TrimSpace(evalCase.ID),
		Category: strings.TrimSpace(evalCase.Category),
		Role:     strings.TrimSpace(role),
		Label:    strings.TrimSpace(role) + "@" + target.Label,
		Provider: target.Provider,
		Model:    target.Model,
		Runner:   target.Runner,
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rt := agentruntime.NewRuntime(agentruntime.Config{
		DefaultMaxIterations: maxIterations,
		DefaultTimeout:       timeout,
		LLMProvider:          target.Provider,
		LLMModel:             target.Model,
		LLMAPIKey:            target.APIKey,
		LLMBaseURL:           target.BaseURL,
		WorkspaceRoot:        workspace,
	})
	session, err := rt.Spawn(runCtx, agenttypes.AgentConfig{
		Role:          agenttypes.AgentRole(role),
		ActorID:       "actor:eval:" + ulid.Make().String(),
		WorkspaceID:   workspace,
		WorkspaceRoot: workspace,
		Prompt:        buildAgentEvalPromptForAttempt(role, evalCase, agentRef, conversationID, forcedRetry),
		SkillsAllow:   evalSkillsAllow(role, evalCase),
		ForceToolUse:  len(evalCase.ExpectedPaths) > 0 && target.SupportsRequiredToolUse,
		MaxIterations: maxIterations,
		Timeout:       timeout,
	})
	if err != nil {
		res.Status = "error"
		res.Error = err.Error()
		return res
	}

	<-session.Done()
	res.Status = string(session.Status)
	res.Output = strings.TrimSpace(session.Summary)
	res.DurationMS = 0
	if session.EndedAt != nil {
		res.DurationMS = session.EndedAt.Sub(session.StartedAt).Milliseconds()
	}
	res.ToolCallCount = len(session.ToolCalls)
	res.ToolNames = collectRuntimeToolNames(session.ToolCalls)
	res.InputTokens = session.InputTokens
	res.OutputTokens = session.OutputTokens
	res.TotalTokens = session.TotalTokens
	cost := obs.CalculateTokenCost(target.Model, res.InputTokens, res.OutputTokens)
	res.TotalCostUSD = cost.TotalCostUSD
	if strings.TrimSpace(session.Error) != "" {
		res.Error = session.Error
	}
	if hasCodeCorrectnessExpectations(evalCase) && !forcedRetry && res.ToolCallCount == 0 {
		return runSingleAgentEvalAttempt(ctx, cfg, workspace, target, role, evalCase, agentRef, conversationID, timeout, maxIterations, true)
	}

	if strings.TrimSpace(res.Output) == "" {
		return res
	}
	judgeOutput := res.Output
	if structuredJudgeOutput, ok := judgeTextFromStructuredScoutOutput(res.Output); ok {
		judgeOutput = structuredJudgeOutput
	}
	structured, ok := parseStructuredAgentEvalOutput(res.Output)
	if hasCodeCorrectnessExpectations(evalCase) && !ok {
		res.Error = firstNonEmpty(res.Error, "expected structured JSON output with summary, paths, symbols, snippets, facts, and rationale")
	}
	if hasCodeCorrectnessExpectations(evalCase) && ok {
		validPaths, invalidPaths := validateRepoRelativePaths(workspace, structured.Paths)
		res.InvalidPaths = invalidPaths
		if len(invalidPaths) > 0 {
			res.Error = firstNonEmpty(res.Error, "invalid repo-relative paths in structured output")
		}
		judgeOutput = buildStructuredAgentJudgeText(structured)
		res.MatchedPaths, res.PathRecall = scoreExpectedPaths(evalCase.ExpectedPaths, strings.Join(validPaths, "\n"))
		res.PathMatchCount = len(res.MatchedPaths)
		res.Symbols = normalizeExpectedSymbols(structured.Symbols)
		res.Snippets = extractStructuredAgentSnippets(structured)
		res.MatchedSymbols, res.SymbolRecall = scoreExpectedSymbols(evalCase.ExpectedSymbols, res.Symbols)
		res.MatchedSnippets, res.SnippetRecall = scoreExpectedSnippets(workspace, evalCase.ExpectedSnippets, res.Snippets)
		res.MatchedFacts, res.FactRecall = scoreRequiredFacts(evalCase.RequiredFacts, judgeOutput)
		res.CorrectnessScore = blendedCorrectnessScore(
			res.PathRecall,
			res.SymbolRecall,
			res.SnippetRecall,
			res.FactRecall,
			len(evalCase.ExpectedPaths) > 0,
			len(evalCase.ExpectedSymbols) > 0,
			len(evalCase.ExpectedSnippets) > 0,
			len(evalCase.RequiredFacts) > 0,
		)
	}
	res.ExcludedPathHits, res.WrongScopePenalty = scoreExcludedPaths(evalCase.ExcludedPaths, res.Output)

	judgeResult := optimization.DefaultPromptJudge().Evaluate(optimization.PromptJudgeInput{
		Question:       evalCase.Question,
		Context:        evalCase.Context,
		TargetResponse: evalCase.TargetResponse,
		Output:         judgeOutput,
	})
	res.QualityScore = judgeResult.Score
	res.AccuracyScore = judgeResult.TargetSimilarity
	res.ThoroughnessScore = judgeResult.QuerySimilarity
	res.LengthQuality = judgeResult.LengthQuality
	res.GenericPenalty = judgeResult.GenericPenalty
	if hasCodeCorrectnessExpectations(evalCase) && !ok {
		res.MatchedPaths, res.PathRecall = scoreExpectedPaths(evalCase.ExpectedPaths, res.Output)
		res.PathMatchCount = len(res.MatchedPaths)
	}
	if hasCodeCorrectnessExpectations(evalCase) {
		res.QualityScore = blendedCodingQuality(res.QualityScore, res.PathRecall)
	}
	if res.WrongScopePenalty > 0 {
		res.QualityScore = clampEvalScore(res.QualityScore - res.WrongScopePenalty)
		res.CorrectnessScore = clampEvalScore(res.CorrectnessScore - res.WrongScopePenalty)
	}
	return res
}

func buildAgentEvalPromptForAttempt(role string, evalCase promptEvalCase, agentRef, conversationID string, forcedRetry bool) string {
	prompt := buildAgentEvalPrompt(role, evalCase, agentRef, conversationID)
	if forcedRetry && len(evalCase.ExpectedPaths) > 0 {
		prompt += "\n\nYou previously failed to use tools. You MUST call at least one allowed retrieval tool before answering this question."
	}
	return prompt
}

func collectRuntimeToolNames(calls []agenttypes.ToolCall) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.ToolName)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func loadExternalAgentEvalResults(paths []string, evalCases []promptEvalCase, passThreshold float64) ([]agentEvalResult, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	byCase := map[string]promptEvalCase{}
	for _, c := range evalCases {
		if strings.TrimSpace(c.ID) != "" {
			byCase[strings.TrimSpace(c.ID)] = c
		}
	}
	results := make([]agentEvalResult, 0)
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var rec externalAgentEvalRecord
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				_ = f.Close()
				return nil, fmt.Errorf("decode external result %s: %w", path, err)
			}
			result := agentEvalResult{
				CaseID:       strings.TrimSpace(rec.CaseID),
				Category:     strings.TrimSpace(rec.Category),
				Role:         strings.TrimSpace(rec.Role),
				Label:        firstNonEmpty(rec.Label, strings.TrimSpace(rec.Role)+"@"+strings.TrimSpace(rec.Provider)+":"+strings.TrimSpace(rec.Model)),
				Provider:     strings.TrimSpace(rec.Provider),
				Model:        strings.TrimSpace(rec.Model),
				Runner:       firstNonEmpty(rec.Runner, "external"),
				Status:       "ok",
				Output:       strings.TrimSpace(rec.Output),
				DurationMS:   rec.DurationMS,
				InputTokens:  rec.InputTokens,
				OutputTokens: rec.OutputTokens,
				TotalTokens:  rec.TotalTokens,
				TotalCostUSD: rec.TotalCostUSD,
				Error:        strings.TrimSpace(rec.Error),
			}
			if result.Error != "" {
				result.Status = "error"
			}
			if result.TotalTokens == 0 {
				result.TotalTokens = result.InputTokens + result.OutputTokens
			}
			if evalCase, ok := byCase[result.CaseID]; ok && strings.TrimSpace(result.Output) != "" {
				judgeOutput := result.Output
				if structuredJudgeOutput, parsed := judgeTextFromStructuredScoutOutput(result.Output); parsed {
					judgeOutput = structuredJudgeOutput
				}
				structured, parsed := parseStructuredAgentEvalOutput(result.Output)
				if hasCodeCorrectnessExpectations(evalCase) && !parsed {
					result.Error = firstNonEmpty(result.Error, "expected structured JSON output with summary, paths, symbols, snippets, facts, and rationale")
				}
				if hasCodeCorrectnessExpectations(evalCase) && parsed {
					validPaths, invalidPaths := validateRepoRelativePaths(".", structured.Paths)
					result.InvalidPaths = invalidPaths
					if len(invalidPaths) > 0 {
						result.Error = firstNonEmpty(result.Error, "invalid repo-relative paths in structured output")
					}
					judgeOutput = buildStructuredAgentJudgeText(structured)
					result.MatchedPaths, result.PathRecall = scoreExpectedPaths(evalCase.ExpectedPaths, strings.Join(validPaths, "\n"))
					result.PathMatchCount = len(result.MatchedPaths)
					result.Symbols = normalizeExpectedSymbols(structured.Symbols)
					result.Snippets = extractStructuredAgentSnippets(structured)
					result.MatchedSymbols, result.SymbolRecall = scoreExpectedSymbols(evalCase.ExpectedSymbols, result.Symbols)
					result.MatchedSnippets, result.SnippetRecall = scoreExpectedSnippets(".", evalCase.ExpectedSnippets, result.Snippets)
					result.MatchedFacts, result.FactRecall = scoreRequiredFacts(evalCase.RequiredFacts, judgeOutput)
					result.CorrectnessScore = blendedCorrectnessScore(
						result.PathRecall,
						result.SymbolRecall,
						result.SnippetRecall,
						result.FactRecall,
						len(evalCase.ExpectedPaths) > 0,
						len(evalCase.ExpectedSymbols) > 0,
						len(evalCase.ExpectedSnippets) > 0,
						len(evalCase.RequiredFacts) > 0,
					)
				}
				result.ExcludedPathHits, result.WrongScopePenalty = scoreExcludedPaths(evalCase.ExcludedPaths, result.Output)
				judgeResult := optimization.DefaultPromptJudge().Evaluate(optimization.PromptJudgeInput{
					Question:       evalCase.Question,
					Context:        evalCase.Context,
					TargetResponse: evalCase.TargetResponse,
					Output:         judgeOutput,
				})
				result.QualityScore = judgeResult.Score
				result.AccuracyScore = judgeResult.TargetSimilarity
				result.ThoroughnessScore = judgeResult.QuerySimilarity
				result.LengthQuality = judgeResult.LengthQuality
				result.GenericPenalty = judgeResult.GenericPenalty
				if hasCodeCorrectnessExpectations(evalCase) && !parsed {
					result.MatchedPaths, result.PathRecall = scoreExpectedPaths(evalCase.ExpectedPaths, result.Output)
					result.PathMatchCount = len(result.MatchedPaths)
				}
				if hasCodeCorrectnessExpectations(evalCase) {
					result.QualityScore = blendedCodingQuality(result.QualityScore, result.PathRecall)
				}
				if result.WrongScopePenalty > 0 {
					result.QualityScore = clampEvalScore(result.QualityScore - result.WrongScopePenalty)
					result.CorrectnessScore = clampEvalScore(result.CorrectnessScore - result.WrongScopePenalty)
				}
				result.Passed = shouldPassAgentEval(result, evalCase, passThreshold)
			}
			results = append(results, result)
		}
		if err := scanner.Err(); err != nil {
			_ = f.Close()
			return nil, err
		}
		_ = f.Close()
	}
	return results, nil
}

func summarizeAgentEvalResults(results []agentEvalResult) []agentEvalSummary {
	type agg struct {
		agentEvalSummary
	}
	byLabel := map[string]*agg{}
	for _, result := range results {
		label := strings.TrimSpace(result.Label)
		if label == "" {
			label = strings.TrimSpace(result.Role) + "@" + strings.TrimSpace(result.Provider) + ":" + strings.TrimSpace(result.Model)
		}
		item, ok := byLabel[label]
		if !ok {
			item = &agg{agentEvalSummary: agentEvalSummary{
				Label:    label,
				Role:     result.Role,
				Provider: result.Provider,
				Model:    result.Model,
				Runner:   result.Runner,
			}}
			byLabel[label] = item
		}
		item.Count++
		if result.Passed {
			item.PassRate++
		}
		item.MeanQuality += result.QualityScore
		item.MeanCorrectness += result.CorrectnessScore
		item.MeanAccuracy += result.AccuracyScore
		item.MeanThoroughness += result.ThoroughnessScore
		item.MeanPathRecall += result.PathRecall
		item.MeanSymbolRecall += result.SymbolRecall
		item.MeanSnippetRecall += result.SnippetRecall
		item.MeanFactRecall += result.FactRecall
		item.MeanWrongScope += result.WrongScopePenalty
		item.MeanDurationMS += float64(result.DurationMS)
		item.MeanTokens += float64(result.TotalTokens)
		item.MeanCostUSD += result.TotalCostUSD
		if strings.TrimSpace(result.Error) != "" {
			item.ErrorCount++
		}
	}

	out := make([]agentEvalSummary, 0, len(byLabel))
	for _, item := range byLabel {
		if item.Count > 0 {
			scale := float64(item.Count)
			item.PassRate /= scale
			item.MeanQuality /= scale
			item.MeanCorrectness /= scale
			item.MeanAccuracy /= scale
			item.MeanThoroughness /= scale
			item.MeanPathRecall /= scale
			item.MeanSymbolRecall /= scale
			item.MeanSnippetRecall /= scale
			item.MeanFactRecall /= scale
			item.MeanWrongScope /= scale
			item.MeanDurationMS /= scale
			item.MeanTokens /= scale
			item.MeanCostUSD /= scale
		}
		out = append(out, item.agentEvalSummary)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].MeanQuality == out[j].MeanQuality {
			return out[i].Label < out[j].Label
		}
		return out[i].MeanQuality > out[j].MeanQuality
	})
	return out
}

func renderAgentEvalMarkdown(workspace string, evalCases []promptEvalCase, summaries []agentEvalSummary, results []agentEvalResult) string {
	var b strings.Builder
	b.WriteString("# Agent Eval\n\n")
	b.WriteString("- Workspace: `" + workspace + "`\n")
	b.WriteString(fmt.Sprintf("- Eval cases: `%d`\n\n", len(evalCases)))
	b.WriteString("## Summary\n\n")
	b.WriteString("| Label | Runner | pass | quality | correctness | path | symbol | snippet | fact | wrong-scope | ms | tokens | cost |\n")
	b.WriteString("| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, s := range summaries {
		b.WriteString(fmt.Sprintf("| %s | %s | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f | %.0f | %.0f | %.4f |\n",
			s.Label, s.Runner, s.PassRate, s.MeanQuality, s.MeanCorrectness, s.MeanPathRecall, s.MeanSymbolRecall, s.MeanSnippetRecall, s.MeanFactRecall, s.MeanWrongScope, s.MeanDurationMS, s.MeanTokens, s.MeanCostUSD))
	}
	b.WriteString("\n## Results\n\n")
	for _, r := range results {
		b.WriteString("### " + firstNonEmpty(r.CaseID, r.Label) + "\n\n")
		b.WriteString(fmt.Sprintf("- Label: `%s`\n", r.Label))
		b.WriteString(fmt.Sprintf("- Role: `%s`\n", r.Role))
		b.WriteString(fmt.Sprintf("- Target: `%s:%s`\n", r.Provider, r.Model))
		b.WriteString(fmt.Sprintf("- Runner: `%s`\n", r.Runner))
		b.WriteString(fmt.Sprintf("- Quality: `%.2f`\n", r.QualityScore))
		b.WriteString(fmt.Sprintf("- Correctness: `%.2f`\n", r.CorrectnessScore))
		b.WriteString(fmt.Sprintf("- Path recall: `%.2f`\n", r.PathRecall))
		b.WriteString(fmt.Sprintf("- Symbol recall: `%.2f`\n", r.SymbolRecall))
		b.WriteString(fmt.Sprintf("- Snippet recall: `%.2f`\n", r.SnippetRecall))
		b.WriteString(fmt.Sprintf("- Fact recall: `%.2f`\n", r.FactRecall))
		b.WriteString(fmt.Sprintf("- Accuracy: `%.2f`\n", r.AccuracyScore))
		b.WriteString(fmt.Sprintf("- Thoroughness: `%.2f`\n", r.ThoroughnessScore))
		b.WriteString(fmt.Sprintf("- Duration: `%dms`\n", r.DurationMS))
		b.WriteString(fmt.Sprintf("- Tokens: `%d`\n", r.TotalTokens))
		if len(r.ToolNames) > 0 {
			b.WriteString("- Tools: `" + strings.Join(r.ToolNames, ", ") + "`\n")
		}
		if len(r.MatchedPaths) > 0 {
			b.WriteString("- Matched paths: `" + strings.Join(r.MatchedPaths, ", ") + "`\n")
		}
		if len(r.MatchedSymbols) > 0 {
			b.WriteString("- Matched symbols: `" + strings.Join(r.MatchedSymbols, ", ") + "`\n")
		}
		if len(r.MatchedSnippets) > 0 {
			b.WriteString("- Matched snippets: `" + strings.Join(r.MatchedSnippets, ", ") + "`\n")
		}
		if len(r.MatchedFacts) > 0 {
			b.WriteString("- Matched facts: `" + strings.Join(r.MatchedFacts, ", ") + "`\n")
		}
		if len(r.ExcludedPathHits) > 0 {
			b.WriteString("- Excluded path hits: `" + strings.Join(r.ExcludedPathHits, ", ") + "`\n")
		}
		if r.Error != "" {
			b.WriteString("- Error: " + r.Error + "\n")
		}
		if r.Output != "" {
			b.WriteString("\n```\n" + r.Output + "\n```\n\n")
		}
	}
	return b.String()
}

func evalSkillsAllow(role string, evalCase promptEvalCase) []string {
	if !hasCodeCorrectnessExpectations(evalCase) {
		return nil
	}
	switch strings.TrimSpace(role) {
	case string(agenttypes.RoleResearcher):
		if isRefactorEntryCase(evalCase) {
			return []string{
				"refactor_scout", "code_symbols", "fs_read_file", "semantic_search_code",
				"smart_search", "repo_index_search", "context_search",
			}
		}
		return []string{
			"context_search", "semantic_search_code", "smart_search", "code_search", "code_symbols", "refactor_scout",
			"repo_index_search", "repo_index_open",
			"fs_read_file",
		}
	case string(agenttypes.RoleDAGScout):
		return []string{
			"repo_index_search", "repo_index_expand", "repo_index_open", "repo_index_dag_grep",
		}
	case string(agenttypes.RoleSymbolScout):
		if isRefactorEntryCase(evalCase) {
			return []string{
				"refactor_scout", "code_symbols", "context_grep",
			}
		}
		return []string{
			"code_symbols", "context_grep", "code_search", "refactor_scout",
		}
	case string(agenttypes.RoleSemanticScout):
		return []string{
			"context_search", "semantic_search_code", "smart_search",
		}
	case string(agenttypes.RoleSubcallWorker):
		if isRefactorEntryCase(evalCase) {
			return []string{
				"refactor_scout", "code_symbols", "fs_read_file", "semantic_search_code",
				"smart_search", "repo_index_search", "context_search",
			}
		}
		return []string{
			"context_search", "smart_search", "context_grep", "code_search", "code_symbols", "refactor_scout",
			"repo_index_search", "repo_index_expand", "repo_index_open", "repo_index_dag_grep", "fs_read_file",
		}
	default:
		return nil
	}
}

func shouldPassAgentEval(result agentEvalResult, evalCase promptEvalCase, passThreshold float64) bool {
	if strings.TrimSpace(result.Error) != "" {
		return false
	}
	if hasCodeCorrectnessExpectations(evalCase) {
		if len(result.ExcludedPathHits) > 0 {
			return false
		}
		hasPaths := len(evalCase.ExpectedPaths) > 0
		hasSymbols := len(evalCase.ExpectedSymbols) > 0
		hasSnippets := len(evalCase.ExpectedSnippets) > 0
		hasFacts := len(evalCase.RequiredFacts) > 0
		if hasPaths && result.PathRecall == 0 {
			return false
		}
		if hasPaths && !hasSymbols && !hasSnippets && !hasFacts && result.PathRecall >= 1 {
			return true
		}
		return result.CorrectnessScore >= passThreshold
	}
	return result.QualityScore >= passThreshold
}

func blendedCodingQuality(proseScore, pathRecall float64) float64 {
	if proseScore < 0 {
		proseScore = 0
	}
	if proseScore > 1 {
		proseScore = 1
	}
	if pathRecall < 0 {
		pathRecall = 0
	}
	if pathRecall > 1 {
		pathRecall = 1
	}
	return proseScore*0.4 + pathRecall*0.6
}

func clampEvalScore(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func scoreExpectedPaths(expected []string, output string) ([]string, float64) {
	normalized := normalizeExpectedPaths(expected)
	if len(normalized) == 0 {
		return nil, 0
	}
	output = filepath.ToSlash(strings.ToLower(strings.TrimSpace(output)))
	matched := make([]string, 0, len(normalized))
	for _, want := range normalized {
		if strings.Contains(output, strings.ToLower(want)) {
			matched = append(matched, want)
		}
	}
	return matched, float64(len(matched)) / float64(len(normalized))
}

func scoreExcludedPaths(excluded []string, output string) ([]string, float64) {
	normalized := normalizeExpectedPaths(excluded)
	if len(normalized) == 0 {
		return nil, 0
	}
	output = filepath.ToSlash(strings.ToLower(strings.TrimSpace(output)))
	hits := make([]string, 0, len(normalized))
	for _, want := range normalized {
		if strings.Contains(output, strings.ToLower(want)) {
			hits = append(hits, want)
		}
	}
	if len(hits) == 0 {
		return nil, 0
	}
	return hits, float64(len(hits)) / float64(len(normalized))
}

func normalizeExpectedPaths(paths []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func validateRepoRelativePaths(workspace string, paths []string) (valid []string, invalid []string) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		workspace = "."
	}
	fileIndex := buildWorkspaceFileIndex(workspace)
	for _, path := range paths {
		normalized, ok := normalizeRepoRelativePath(workspace, path, fileIndex)
		if !ok {
			path = filepath.ToSlash(strings.TrimSpace(path))
			if path == "" {
				continue
			}
			invalid = append(invalid, path)
			continue
		}
		valid = append(valid, normalized)
	}
	return normalizeExpectedPaths(valid), normalizeExpectedPaths(invalid)
}

func normalizeRepoRelativePath(workspace, path string, fileIndex map[string][]string) (string, bool) {
	path = filepath.ToSlash(strings.Trim(strings.TrimSpace(path), "`\"'"))
	if path == "" {
		return "", false
	}
	if workspace == "" {
		workspace = "."
	}
	if filepath.IsAbs(path) {
		if rel, err := filepath.Rel(workspace, path); err == nil && !strings.HasPrefix(rel, "..") {
			path = filepath.ToSlash(rel)
		}
	}
	for _, prefix := range []string{"./", "repo/", "workspace/", filepath.Base(workspace) + "/"} {
		if strings.HasPrefix(path, prefix) {
			candidate := strings.TrimPrefix(path, prefix)
			if repoRelativePathExists(workspace, candidate) {
				return filepath.ToSlash(candidate), true
			}
			path = candidate
		}
	}
	if repoRelativePathExists(workspace, path) {
		return filepath.ToSlash(path), true
	}
	if base := filepath.Base(path); base != "" && strings.Contains(base, ".") {
		if matches := fileIndex[base]; len(matches) == 1 {
			return matches[0], true
		}
	}
	return "", false
}

func repoRelativePathExists(workspace, path string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" || strings.HasPrefix(path, "/") {
		return false
	}
	full := filepath.Join(workspace, filepath.FromSlash(path))
	if info, err := os.Stat(full); err == nil && !info.IsDir() {
		return true
	}
	return false
}

func buildWorkspaceFileIndex(workspace string) map[string][]string {
	index := map[string][]string{}
	_ = filepath.WalkDir(workspace, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".idea", ".vscode":
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(workspace, path)
		if err != nil {
			return nil
		}
		base := filepath.Base(rel)
		index[base] = append(index[base], filepath.ToSlash(rel))
		return nil
	})
	for base, matches := range index {
		index[base] = normalizeExpectedPaths(matches)
	}
	return index
}

func parseStructuredAgentEvalOutput(raw string) (structuredAgentEvalOutput, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return structuredAgentEvalOutput{}, false
	}
	var out structuredAgentEvalOutput
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return out, true
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start == -1 || end <= start {
		return structuredAgentEvalOutput{}, false
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &out); err == nil {
		return out, true
	}
	return structuredAgentEvalOutput{}, false
}

func buildStructuredAgentJudgeText(out structuredAgentEvalOutput) string {
	lines := make([]string, 0, 4+len(out.Symbols)+len(out.Facts))
	if strings.TrimSpace(out.Summary) != "" {
		lines = append(lines, strings.TrimSpace(out.Summary))
	}
	if strings.TrimSpace(out.Rationale) != "" {
		lines = append(lines, strings.TrimSpace(out.Rationale))
	}
	for _, symbol := range out.Symbols {
		if strings.TrimSpace(symbol) != "" {
			lines = append(lines, strings.TrimSpace(symbol))
		}
	}
	for _, fact := range out.Facts {
		if strings.TrimSpace(fact) != "" {
			lines = append(lines, strings.TrimSpace(fact))
		}
	}
	return strings.Join(lines, "\n")
}

func extractStructuredAgentSnippets(out structuredAgentEvalOutput) []evalObservedSnippet {
	snippets := make([]evalObservedSnippet, 0, len(out.Snippets))
	for _, snippet := range out.Snippets {
		path := normalizeCodeSearchPath(snippet.Path)
		if path == "" {
			continue
		}
		snippets = append(snippets, evalObservedSnippet{
			Path:      path,
			StartLine: snippet.StartLine,
			EndLine:   snippet.EndLine,
		})
	}
	return normalizeObservedSnippets(snippets)
}

func judgeTextFromStructuredScoutOutput(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	var out structuredScoutJudgeOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		start := strings.Index(raw, "{")
		end := strings.LastIndex(raw, "}")
		if start == -1 || end <= start {
			return "", false
		}
		if err := json.Unmarshal([]byte(raw[start:end+1]), &out); err != nil {
			return "", false
		}
	}
	lines := make([]string, 0, 8)
	if strings.TrimSpace(out.CurrentBestView) != "" {
		lines = append(lines, strings.TrimSpace(out.CurrentBestView))
	}
	if strings.TrimSpace(out.Summary) != "" {
		lines = append(lines, strings.TrimSpace(out.Summary))
	}
	for _, claim := range out.Claims {
		if strings.TrimSpace(claim.Key) == "" && strings.TrimSpace(claim.Value) == "" {
			continue
		}
		lines = append(lines, strings.TrimSpace(claim.Key+": "+claim.Value))
	}
	for _, item := range out.Timeline {
		if strings.TrimSpace(item.Value) != "" {
			lines = append(lines, strings.TrimSpace(item.Value))
		}
	}
	for _, block := range out.ContextBlocks {
		if strings.TrimSpace(block.Summary) != "" {
			lines = append(lines, strings.TrimSpace(block.Summary))
		}
	}
	for _, gap := range out.Gaps {
		if strings.TrimSpace(gap) != "" {
			lines = append(lines, "Gap: "+strings.TrimSpace(gap))
		}
	}
	if len(lines) == 0 {
		return "", false
	}
	return strings.Join(lines, "\n"), true
}
