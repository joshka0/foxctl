package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	configpkg "github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/rlm"
	rlmenv "github.com/joshka0/foxctl/internal/rlm/env"
	"github.com/joshka0/foxctl/internal/rlm/repl"
	rlmruntime "github.com/joshka0/foxctl/internal/rlm/runtime"
	"github.com/joshka0/foxctl/internal/tooling/evals/longcoteval"
)

func TestEvalCommandRegistersLongCoTOnce(t *testing.T) {
	t.Parallel()

	cmd := newEvalCommand()
	count := 0
	for _, child := range cmd.Commands() {
		if child.Name() == "longcot" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("longcot subcommand count=%d want 1", count)
	}
}

func TestLoadLongCoTQuestionsLoadsOfficialShape(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", "testdata", "evals", "longcot", "fixture.jsonl")
	questions, err := loadLongCoTQuestions(path, longCoTQuestionFilter{
		Split:      "eval",
		Domains:    []string{"math"},
		Difficulty: "medium",
	})
	if err != nil {
		t.Fatalf("loadLongCoTQuestions() error = %v", err)
	}
	if len(questions) != 1 {
		t.Fatalf("len(questions)=%d want 1", len(questions))
	}
	if questions[0].ID != "Arithmetic_medium_1" {
		t.Fatalf("question id=%q want Arithmetic_medium_1", questions[0].ID)
	}
	if questions[0].Template != "ArithmeticChain" {
		t.Fatalf("template=%q", questions[0].Template)
	}
	if !strings.Contains(questions[0].PromptText, "solution = <value>") {
		t.Fatalf("prompt did not preserve official answer format: %q", questions[0].PromptText)
	}
	if !strings.Contains(questions[0].Answer, `"solution":42`) {
		t.Fatalf("answer=%q", questions[0].Answer)
	}
	if questions[0].Canary != "fixture-canary-math" {
		t.Fatalf("canary=%q", questions[0].Canary)
	}
	if strings.TrimSpace(questions[0].QuestionHash) == "" {
		t.Fatal("expected generated question hash")
	}
}

func TestResolveLongCoTConditionsRequiresExactIDs(t *testing.T) {
	t.Parallel()

	conditions, err := resolveLongCoTConditions(nil, longCoTConditionRuntime{})
	if err != nil {
		t.Fatalf("resolveLongCoTConditions(default) error = %v", err)
	}
	if len(conditions) != 2 {
		t.Fatalf("len(conditions)=%d want 2", len(conditions))
	}
	if conditions[0].ID != longcoteval.ConditionBaselineNoToolsOfficial || conditions[1].ID != longcoteval.ConditionRLMNoToolsSingle {
		t.Fatalf("default conditions=%v", []longcoteval.ConditionID{conditions[0].ID, conditions[1].ID})
	}

	_, err = resolveLongCoTConditions([]string{"RLM_NO_TOOLS_SINGLE"}, longCoTConditionRuntime{})
	if err == nil || !strings.Contains(err.Error(), "unknown --condition") {
		t.Fatalf("expected exact-id validation error, got %v", err)
	}
}

func TestLongCoTRetryableRLMError(t *testing.T) {
	t.Parallel()

	retryable := []error{
		fmt.Errorf("rlm repl runner: API error (status 429): provider returned error"),
		fmt.Errorf("provider is temporarily rate-limited upstream"),
		fmt.Errorf("Upstream error from Alibaba: Request rate increased too quickly"),
		fmt.Errorf("API error (status 503): service unavailable"),
	}
	for _, err := range retryable {
		if !longCoTRetryableRLMError(err) {
			t.Fatalf("longCoTRetryableRLMError(%v)=false, want true", err)
		}
	}
	nonRetryable := []error{
		nil,
		fmt.Errorf("braid node did not complete: status blocked"),
		fmt.Errorf("API error (status 400): invalid request"),
	}
	for _, err := range nonRetryable {
		if longCoTRetryableRLMError(err) {
			t.Fatalf("longCoTRetryableRLMError(%v)=true, want false", err)
		}
	}
}

func TestResolveLongCoTLiveTargetOpenRouterUsesBearerWithGlobalAuthNone(t *testing.T) {
	t.Parallel()

	cfg := configpkg.Config{}
	cfg.LLM.AuthMode = "none"
	target, err := resolveLongCoTLiveTarget(cfg, "openrouter", "tencent/hy3-preview:free", "", "sk-test")
	if err != nil {
		t.Fatalf("resolveLongCoTLiveTarget() error = %v", err)
	}
	if target.AuthMode != "bearer" {
		t.Fatalf("auth mode=%q want bearer", target.AuthMode)
	}
	if target.AuthHeader != "Authorization" {
		t.Fatalf("auth header=%q", target.AuthHeader)
	}
	if target.AuthPrefix != "Bearer " {
		t.Fatalf("auth prefix=%q", target.AuthPrefix)
	}
	if target.BaseURL != "https://openrouter.ai/api/v1" {
		t.Fatalf("base url=%q", target.BaseURL)
	}
}

func TestResolveLongCoTConditionsREPLRecursiveIncludesRecursiveTools(t *testing.T) {
	t.Parallel()

	conditions, err := resolveLongCoTConditions([]string{string(longcoteval.ConditionRLMReplRecursive)}, longCoTConditionRuntime{})
	if err != nil {
		t.Fatalf("resolveLongCoTConditions(recursive) error = %v", err)
	}
	if len(conditions) != 1 {
		t.Fatalf("len(conditions)=%d want 1", len(conditions))
	}
	got := conditions[0]
	if got.ID != longcoteval.ConditionRLMReplRecursive {
		t.Fatalf("condition id=%q want %q", got.ID, longcoteval.ConditionRLMReplRecursive)
	}
	if got.MaxDepth <= 0 || got.MaxSubcalls <= 0 {
		t.Fatalf("recursive condition must have depth/subcalls > 0, got depth=%d subcalls=%d", got.MaxDepth, got.MaxSubcalls)
	}
	wantTools := []string{"python_repl", "rlm_query", "rlm_wait", "rlm_result"}
	if !stringSlicesEqual(got.AllowedTools, wantTools) {
		t.Fatalf("allowed tools=%v want %v", got.AllowedTools, wantTools)
	}
}

func TestResolveLongCoTConditionsNoToolsMeansRecursiveRLMWithoutEphemeralTools(t *testing.T) {
	t.Parallel()

	conditions, err := resolveLongCoTConditions([]string{string(longcoteval.ConditionRLMNoToolsSingle)}, longCoTConditionRuntime{})
	if err != nil {
		t.Fatalf("resolveLongCoTConditions(no tools) error = %v", err)
	}
	got := conditions[0]
	if got.MaxDepth != 1 || got.MaxSubcalls <= 0 {
		t.Fatalf("no-tools condition must allow root-breadth RLM only, got depth=%d subcalls=%d", got.MaxDepth, got.MaxSubcalls)
	}
	wantTools := []string{rlmruntime.PythonREPLToolName, rlmruntime.RLMQueryToolName, rlmruntime.RLMWaitToolName, rlmruntime.RLMResultToolName}
	if !stringSlicesEqual(got.AllowedTools, wantTools) {
		t.Fatalf("allowed tools=%v want %v", got.AllowedTools, wantTools)
	}
}

func TestResolveLongCoTConditionsBraidSingle(t *testing.T) {
	t.Parallel()

	conditions, err := resolveLongCoTConditions([]string{string(longcoteval.ConditionRLMBraidSingle)}, longCoTConditionRuntime{})
	if err != nil {
		t.Fatalf("resolveLongCoTConditions(braid) error = %v", err)
	}
	if len(conditions) != 1 {
		t.Fatalf("len(conditions)=%d want 1", len(conditions))
	}
	got := conditions[0]
	if got.ID != longcoteval.ConditionRLMBraidSingle {
		t.Fatalf("condition id=%q want %q", got.ID, longcoteval.ConditionRLMBraidSingle)
	}
	if got.RLMRouteProfile != "longcot_repl_braid" {
		t.Fatalf("route profile=%q want longcot_repl_braid", got.RLMRouteProfile)
	}
	if got.RLMPlanMode != "repl_braid" {
		t.Fatalf("plan mode=%q want repl_braid", got.RLMPlanMode)
	}
	if got.RLMToolProfile != "longcot-repl-recursive" {
		t.Fatalf("tool profile=%q want longcot-repl-recursive", got.RLMToolProfile)
	}
	if got.MaxDepth != 1 || got.MaxIterations != 32 || got.MaxSubcalls != 16 {
		t.Fatalf("limits depth=%d iterations=%d subcalls=%d want 1/32/16", got.MaxDepth, got.MaxIterations, got.MaxSubcalls)
	}
	wantTools := []string{rlmruntime.PythonREPLToolName, rlmruntime.RLMQueryToolName, rlmruntime.RLMWaitToolName, rlmruntime.RLMResultToolName}
	if !stringSlicesEqual(got.AllowedTools, wantTools) {
		t.Fatalf("allowed tools=%v want %v", got.AllowedTools, wantTools)
	}
}

func TestResolveLongCoTConditionsLambdaReplKeepsScratchAndHelperTools(t *testing.T) {
	t.Parallel()

	conditions, err := resolveLongCoTConditions([]string{string(longcoteval.ConditionRLMLambdaReplSingle)}, longCoTConditionRuntime{
		EphemeralSkills: true,
		GeneralHelper:   true,
	})
	if err != nil {
		t.Fatalf("resolveLongCoTConditions(lambda repl) error = %v", err)
	}
	got := conditions[0]
	if got.ID != longcoteval.ConditionRLMLambdaReplSingle {
		t.Fatalf("condition id=%q want %q", got.ID, longcoteval.ConditionRLMLambdaReplSingle)
	}
	if got.RLMPlanMode != "repl_lambda" {
		t.Fatalf("plan mode=%q want repl_lambda", got.RLMPlanMode)
	}
	if got.MaxDepth != 2 || got.MaxSubcalls != 4 {
		t.Fatalf("limits depth=%d subcalls=%d want 2/4", got.MaxDepth, got.MaxSubcalls)
	}
	wantTools := []string{rlmruntime.PythonREPLToolName, rlmruntime.RLMQueryToolName, rlmruntime.RLMWaitToolName, rlmruntime.RLMResultToolName, rlmruntime.EphemeralHelperSolveToolName}
	if !stringSlicesEqual(got.AllowedTools, wantTools) {
		t.Fatalf("allowed tools=%v want %v", got.AllowedTools, wantTools)
	}
}

func TestResolveLongCoTConditionsLambdaAdaptiveKeepsScratchAndOptionalRecursion(t *testing.T) {
	t.Parallel()

	conditions, err := resolveLongCoTConditions([]string{string(longcoteval.ConditionRLMLambdaAdaptiveSingle)}, longCoTConditionRuntime{})
	if err != nil {
		t.Fatalf("resolveLongCoTConditions(lambda adaptive) error = %v", err)
	}
	got := conditions[0]
	if got.ID != longcoteval.ConditionRLMLambdaAdaptiveSingle {
		t.Fatalf("condition id=%q want %q", got.ID, longcoteval.ConditionRLMLambdaAdaptiveSingle)
	}
	if got.RLMPlanMode != "repl_lambda_adaptive" {
		t.Fatalf("plan mode=%q want repl_lambda_adaptive", got.RLMPlanMode)
	}
	if got.MaxDepth != 2 || got.MaxSubcalls != 4 {
		t.Fatalf("limits depth=%d subcalls=%d want 2/4", got.MaxDepth, got.MaxSubcalls)
	}
	wantTools := []string{rlmruntime.PythonREPLToolName, rlmruntime.RLMQueryToolName, rlmruntime.RLMWaitToolName, rlmruntime.RLMResultToolName}
	if !stringSlicesEqual(got.AllowedTools, wantTools) {
		t.Fatalf("allowed tools=%v want %v", got.AllowedTools, wantTools)
	}
}

func TestResolveLongCoTConditionsLambdaThenBraidUsesHybridContract(t *testing.T) {
	t.Parallel()

	conditions, err := resolveLongCoTConditions([]string{string(longcoteval.ConditionRLMLambdaThenBraidSingle)}, longCoTConditionRuntime{
		Timeout: 360 * time.Second,
	})
	if err != nil {
		t.Fatalf("resolveLongCoTConditions(lambda then braid) error = %v", err)
	}
	got := conditions[0]
	if got.ID != longcoteval.ConditionRLMLambdaThenBraidSingle {
		t.Fatalf("condition id=%q want %q", got.ID, longcoteval.ConditionRLMLambdaThenBraidSingle)
	}
	if got.RLMPlanMode != "repl_lambda_then_braid" {
		t.Fatalf("plan mode=%q want repl_lambda_then_braid", got.RLMPlanMode)
	}
	if got.MaxDepth != 2 || got.MaxSubcalls != 16 {
		t.Fatalf("limits depth=%d subcalls=%d want 2/16", got.MaxDepth, got.MaxSubcalls)
	}
	wantTools := []string{rlmruntime.PythonREPLToolName, rlmruntime.RLMQueryToolName, rlmruntime.RLMWaitToolName, rlmruntime.RLMResultToolName}
	if !stringSlicesEqual(got.AllowedTools, wantTools) {
		t.Fatalf("allowed tools=%v want %v", got.AllowedTools, wantTools)
	}
	if fallback := longCoTBraidFallbackTimeoutMS(got.TimeoutMS); fallback != (720 * time.Second).Milliseconds() {
		t.Fatalf("fallback timeout=%d want 720s", fallback)
	}
}

func TestResolveLongCoTConditionsNoModelToolsMeansREPLOnly(t *testing.T) {
	t.Parallel()

	conditions, err := resolveLongCoTConditions([]string{string(longcoteval.ConditionRLMNoModelToolsSingle)}, longCoTConditionRuntime{
		EphemeralSkills: true,
		GeneralHelper:   true,
	})
	if err != nil {
		t.Fatalf("resolveLongCoTConditions(no model tools) error = %v", err)
	}
	got := conditions[0]
	if got.MaxSubcalls != 0 {
		t.Fatalf("no-model-tools MaxSubcalls=%d want 0", got.MaxSubcalls)
	}
	wantTools := []string{rlmruntime.PythonREPLToolName}
	if !stringSlicesEqual(got.AllowedTools, wantTools) {
		t.Fatalf("allowed tools=%v want %v", got.AllowedTools, wantTools)
	}
}

func TestResolveLongCoTConditionsREPLRecursiveUsesGoToolForYaegi(t *testing.T) {
	t.Parallel()

	conditions, err := resolveLongCoTConditions([]string{string(longcoteval.ConditionRLMReplRecursive)}, longCoTConditionRuntime{
		SandboxKind: rlmruntime.SandboxKindYaegi,
	})
	if err != nil {
		t.Fatalf("resolveLongCoTConditions(recursive) error = %v", err)
	}
	got := conditions[0]
	wantTools := []string{"go_repl", "rlm_query", "rlm_wait", "rlm_result"}
	if !stringSlicesEqual(got.AllowedTools, wantTools) {
		t.Fatalf("allowed tools=%v want %v", got.AllowedTools, wantTools)
	}
}

func TestResolveLongCoTConditionsGeneralHelperReportsHelperOnlyTool(t *testing.T) {
	t.Parallel()

	conditions, err := resolveLongCoTConditions([]string{string(longcoteval.ConditionRLMReplNoSubcalls)}, longCoTConditionRuntime{
		EphemeralSkills: true,
		GeneralHelper:   true,
	})
	if err != nil {
		t.Fatalf("resolveLongCoTConditions(general helper) error = %v", err)
	}
	got := conditions[0]
	wantTools := []string{rlmruntime.EphemeralHelperSolveToolName}
	if !stringSlicesEqual(got.AllowedTools, wantTools) {
		t.Fatalf("allowed tools=%v want %v", got.AllowedTools, wantTools)
	}
}

func TestEvalLongCoTDryRunWithoutDatasetUsesPlaceholder(t *testing.T) {
	t.Parallel()

	env, err := runEvalLongCoTForTest(t, "--dry-run")
	if err != nil {
		t.Fatalf("runEvalLongCoTForTest() error = %v", err)
	}
	if env.Status != envelope.StatusOK {
		t.Fatalf("status=%q want ok", env.Status)
	}
	if env.Command != evalLongCoTCommand {
		t.Fatalf("command=%q want %q", env.Command, evalLongCoTCommand)
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data=%T want map[string]any", env.Data)
	}
	result := decodeLongCoTRunResult(t, data["result"])
	if len(result.Questions) != 1 || result.Questions[0].ID != "fixture_math_easy_1" {
		t.Fatalf("questions=%+v", result.Questions)
	}
	if len(result.Attempts) != 2 {
		t.Fatalf("attempts=%d want 2", len(result.Attempts))
	}

	var sawBaseline, sawRLM bool
	for _, attempt := range result.Attempts {
		if attempt.Status != longcoteval.AttemptStatusUnverified {
			t.Fatalf("attempt status=%q want %q", attempt.Status, longcoteval.AttemptStatusUnverified)
		}
		if attempt.LeakageFlags.Leaked() {
			t.Fatalf("attempt leaked unexpectedly: %+v", attempt.LeakageFlags)
		}
		switch attempt.ConditionID {
		case longcoteval.ConditionBaselineNoToolsOfficial:
			sawBaseline = true
			if attempt.ConditionKind != longcoteval.ConditionKindBaseline {
				t.Fatalf("baseline kind=%q", attempt.ConditionKind)
			}
			if attempt.RLM != nil {
				t.Fatalf("baseline should not have rlm metadata: %+v", attempt.RLM)
			}
		case longcoteval.ConditionRLMNoToolsSingle:
			sawRLM = true
			if attempt.ConditionKind != longcoteval.ConditionKindRLM {
				t.Fatalf("rlm kind=%q", attempt.ConditionKind)
			}
			if attempt.RLM == nil || attempt.RLM.ToolProfile == "" {
				t.Fatalf("expected RLM metadata, got %+v", attempt.RLM)
			}
			if attempt.RLM.Metadata == nil || attempt.RLM.Metadata["effective_contract"] == nil {
				t.Fatalf("missing effective_contract metadata: %+v", attempt.RLM)
			}
		}
	}
	if !sawBaseline || !sawRLM {
		t.Fatalf("missing default attempts: baseline=%v rlm=%v", sawBaseline, sawRLM)
	}

	config := decodeStringAnyMap(t, data["config"])
	if got := config["dataset"]; got != "official-fixture://longcot-mini-dry-run" {
		t.Fatalf("config.dataset=%v want official fixture label", got)
	}
	if got := config["dataset_source"]; got != "offline_fixture" {
		t.Fatalf("config.dataset_source=%v want offline_fixture", got)
	}
}

func TestEvalLongCoTDryRunSaveWritesArtifacts(t *testing.T) {
	t.Parallel()

	outputDir := t.TempDir()
	dataset := filepath.Join("..", "..", "..", "testdata", "evals", "longcot", "fixture.jsonl")
	env, err := runEvalLongCoTForTest(t,
		"--dry-run",
		"--dataset", dataset,
		"--condition", string(longcoteval.ConditionBaselineNoToolsOfficial),
		"--condition", string(longcoteval.ConditionRLMNoModelToolsSingle),
		"--output-dir", outputDir,
		"--save",
	)
	if err != nil {
		t.Fatalf("runEvalLongCoTForTest() error = %v", err)
	}
	if env.Status != envelope.StatusOK {
		t.Fatalf("status=%q want ok", env.Status)
	}

	data := decodeStringAnyMap(t, env.Data)
	result := decodeLongCoTRunResult(t, data["result"])
	if len(result.Artifacts) != 3 {
		t.Fatalf("artifacts=%+v", result.Artifacts)
	}
	for _, artifact := range result.Artifacts {
		if _, err := os.Stat(artifact.Path); err != nil {
			t.Fatalf("artifact %q missing: %v", artifact.Path, err)
		}
	}

	var jsonPath, markdownPath, responsesPath string
	for _, artifact := range result.Artifacts {
		switch artifact.Kind {
		case "result_json":
			jsonPath = artifact.Path
		case "report_markdown":
			markdownPath = artifact.Path
		case "responses_official_jsonl":
			responsesPath = artifact.Path
		}
	}
	if jsonPath == "" || markdownPath == "" || responsesPath == "" {
		t.Fatalf("unexpected artifacts=%+v", result.Artifacts)
	}

	body, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", jsonPath, err)
	}
	var saved longcoteval.RunResult
	if err := json.Unmarshal(body, &saved); err != nil {
		t.Fatalf("Unmarshal(result_json) error = %v", err)
	}
	if saved.RunID != result.RunID {
		t.Fatalf("saved run id=%q want %q", saved.RunID, result.RunID)
	}
	if len(saved.Artifacts) != 3 {
		t.Fatalf("saved artifacts=%+v want persisted artifact metadata", saved.Artifacts)
	}
	for _, attempt := range saved.Attempts {
		if attempt.Status != longcoteval.AttemptStatusUnverified {
			t.Fatalf("saved attempt status=%q want %q", attempt.Status, longcoteval.AttemptStatusUnverified)
		}
	}

	report, err := os.ReadFile(markdownPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", markdownPath, err)
	}
	if !strings.Contains(string(report), "# LongCoT × RLM Eval") {
		t.Fatalf("report missing heading: %s", string(report))
	}

	responses, err := os.ReadFile(responsesPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", responsesPath, err)
	}
	lines := strings.Split(strings.TrimSpace(string(responses)), "\n")
	if len(lines) != len(result.Attempts) {
		t.Fatalf("official responses lines=%d want attempts=%d", len(lines), len(result.Attempts))
	}
	var official longcoteval.OfficialResponse
	if err := json.Unmarshal([]byte(lines[0]), &official); err != nil {
		t.Fatalf("Unmarshal(official response) error = %v", err)
	}
	if official.QuestionID == "" || official.ResponseText != "" || official.Successful {
		t.Fatalf("unexpected official response record: %+v", official)
	}
}

func TestEvalLongCoTLiveRequiresDataset(t *testing.T) {
	t.Parallel()

	env, err := runEvalLongCoTForTest(t)
	if err == nil {
		t.Fatal("expected command error when live mode has no dataset")
	}
	if env.Status != envelope.StatusError {
		t.Fatalf("status=%q want error", env.Status)
	}
	if !strings.Contains(err.Error(), "--dataset/--longcot-dataset or --longcot-repo is required when --dry-run=false") {
		t.Fatalf("error=%v", err)
	}
}

func TestEvalLongCoTVerifyUsesDatasetAnswersForDatasetDryRun(t *testing.T) {
	t.Setenv("FOXCTL_LONGCOT_REPO", "")
	t.Setenv("LONGCOT_REPO", "")

	dataset := filepath.Join("..", "..", "..", "testdata", "evals", "longcot", "fixture.jsonl")
	env, err := runEvalLongCoTForTest(t, "--dry-run", "--dataset", dataset, "--verify")
	if err != nil {
		t.Fatalf("runEvalLongCoTForTest() error = %v", err)
	}
	if env.Status != envelope.StatusOK {
		t.Fatalf("status=%q want ok", env.Status)
	}
	data := decodeStringAnyMap(t, env.Data)
	result := decodeLongCoTRunResult(t, data["result"])
	verification := decodeStringAnyMap(t, result.Config["verification"])
	if got := verification["verifier_name"]; got != "foxctl.dataset_answer" {
		t.Fatalf("verifier_name=%v want foxctl.dataset_answer", got)
	}
	if len(result.Attempts) == 0 {
		t.Fatal("expected dry-run attempts")
	}
	for _, attempt := range result.Attempts {
		if attempt.VerifierStatus == "" {
			t.Fatalf("attempt missing verifier status: %+v", attempt)
		}
	}
}

func TestLongCoTRunnerSelection(t *testing.T) {
	t.Parallel()

	baseline := longCoTRunnerForCondition(longcoteval.Condition{
		ID:   longcoteval.ConditionBaselineNoToolsOfficial,
		Kind: longcoteval.ConditionKindBaseline,
	})
	if baseline != longCoTLiveRunnerAgent {
		t.Fatalf("baseline runner=%q want %q", baseline, longCoTLiveRunnerAgent)
	}

	rlmRunner := longCoTRunnerForCondition(longcoteval.Condition{
		ID:   longcoteval.ConditionRLMNoToolsSingle,
		Kind: longcoteval.ConditionKindRLM,
	})
	if rlmRunner != longCoTLiveRunnerREPL {
		t.Fatalf("rlm runner=%q want %q", rlmRunner, longCoTLiveRunnerREPL)
	}

	noModelToolsRunner := longCoTRunnerForCondition(longcoteval.Condition{
		ID:   longcoteval.ConditionRLMNoModelToolsSingle,
		Kind: longcoteval.ConditionKindRLM,
	})
	if noModelToolsRunner != longCoTLiveRunnerREPL {
		t.Fatalf("no-model-tools runner=%q want %q", noModelToolsRunner, longCoTLiveRunnerREPL)
	}

	replRunner := longCoTRunnerForCondition(longcoteval.Condition{
		ID:   longcoteval.ConditionRLMReplNoSubcalls,
		Kind: longcoteval.ConditionKindRLM,
	})
	if replRunner != longCoTLiveRunnerREPL {
		t.Fatalf("repl runner=%q want %q", replRunner, longCoTLiveRunnerREPL)
	}

	replRecursiveRunner := longCoTRunnerForCondition(longcoteval.Condition{
		ID:   longcoteval.ConditionRLMReplRecursive,
		Kind: longcoteval.ConditionKindRLM,
	})
	if replRecursiveRunner != longCoTLiveRunnerREPL {
		t.Fatalf("recursive repl runner=%q want %q", replRecursiveRunner, longCoTLiveRunnerREPL)
	}
}

func TestLongCoTEffectiveConditionDisablesOptionalSubcallsByDefault(t *testing.T) {
	t.Parallel()

	condition := longcoteval.Condition{
		ID:            longcoteval.ConditionRLMReplRecursive,
		Kind:          longcoteval.ConditionKindRLM,
		MaxDepth:      2,
		MaxIterations: 8,
		MaxSubcalls:   2,
		AllowedTools:  []string{rlmruntime.PythonREPLToolName, rlmruntime.RLMQueryToolName, rlmruntime.RLMWaitToolName, rlmruntime.RLMResultToolName},
	}
	effective := longCoTEffectiveConditionForQuestion(longcoteval.Question{}, condition)
	if effective.MaxSubcalls != 0 {
		t.Fatalf("effective MaxSubcalls=%d want 0", effective.MaxSubcalls)
	}
	if effective.MaxDepth != 1 {
		t.Fatalf("effective MaxDepth=%d want 1", effective.MaxDepth)
	}
	if len(effective.AllowedTools) != 1 || effective.AllowedTools[0] != rlmruntime.PythonREPLToolName {
		t.Fatalf("effective allowed tools=%v", effective.AllowedTools)
	}
	cfg := longCoTREPLRunnerConfig(
		longcoteval.Question{PromptText: "Move A onto B."},
		effective,
		longCoTLiveTarget{Provider: "openai", Model: "gpt-5"},
		longCoTHelperRuntime{Target: longCoTLiveTarget{Provider: "openai", Model: "gpt-5"}},
		30*time.Second,
		8,
		t.TempDir(),
		rlmruntime.SandboxKindPython,
		false,
		false,
		false,
		true,
		false,
	)
	if cfg.AsyncRecursion {
		t.Fatal("expected no required/optional subcalls to disable async recursion")
	}
	if cfg.InitialState["official_prompt"] != "Move A onto B." {
		t.Fatalf("official_prompt initial state = %#v", cfg.InitialState["official_prompt"])
	}
}

func TestLongCoTREPLRunnerConfigRecursiveEnablesAsyncRecursionWhenQuestionAllowsSubcalls(t *testing.T) {
	t.Parallel()

	condition := longcoteval.Condition{
		ID:            longcoteval.ConditionRLMReplRecursive,
		Kind:          longcoteval.ConditionKindRLM,
		MaxDepth:      2,
		MaxIterations: 8,
		MaxSubcalls:   2,
	}
	cfg := longCoTREPLRunnerConfig(
		longcoteval.Question{AllowOptionalSubcalls: true},
		longCoTEffectiveConditionForQuestion(longcoteval.Question{AllowOptionalSubcalls: true}, condition),
		longCoTLiveTarget{Provider: "openai", Model: "gpt-5"},
		longCoTHelperRuntime{Target: longCoTLiveTarget{Provider: "openai", Model: "gpt-5"}},
		30*time.Second,
		8,
		t.TempDir(),
		rlmruntime.SandboxKindPython,
		false,
		false,
		false,
		true,
		false,
	)
	if !cfg.AsyncRecursion {
		t.Fatal("expected recursive condition to enable async recursion")
	}
	if cfg.RLMQueryFactory == nil {
		t.Fatal("expected recursive query factory for async child backend")
	}
	if cfg.AsyncScheduler.MaxWorkers != 2 {
		t.Fatalf("max workers=%d want 2", cfg.AsyncScheduler.MaxWorkers)
	}
	if cfg.RecursionPolicy != rlmruntime.RecursionPolicyRequired {
		t.Fatalf("recursion policy=%q want required", cfg.RecursionPolicy)
	}
	if cfg.ChildSummaryMaxChars != 700 {
		t.Fatalf("child summary max chars=%d want 700", cfg.ChildSummaryMaxChars)
	}
	if cfg.REPLToolResultMaxChars != 1600 {
		t.Fatalf("repl tool result max chars=%d want 1600", cfg.REPLToolResultMaxChars)
	}
	if !cfg.ChildSummaryNormalizeBeforeSubmit {
		t.Fatal("expected child summary pre-submit normalization")
	}
	if !cfg.ChildSummaryRewriteOverLimit {
		t.Fatal("expected child summary rewrite over limit")
	}
	if cfg.ChildSummaryRewriteMaxIterations != 1 {
		t.Fatalf("child summary rewrite iterations=%d want 1", cfg.ChildSummaryRewriteMaxIterations)
	}
	if got := phaseNames(cfg.Phases); fmt.Sprint(got) != "[context fanout-1 wait-1 integrate fanout-2 wait-2 final]" {
		t.Fatalf("phases=%v", got)
	}
	for _, idx := range []int{0, 1, 2, 3, 4, 5} {
		if !cfg.Phases[idx].AutoExecuteRequiredTool {
			t.Fatalf("phase %q should auto-execute its required tool", cfg.Phases[idx].Name)
		}
	}
	if len(cfg.Phases[1].AutoExecuteToolCalls) != 3 {
		t.Fatalf("fanout-1 calls=%d want 3", len(cfg.Phases[1].AutoExecuteToolCalls))
	}
	if len(cfg.Phases[4].AutoExecuteToolCalls) != 1 {
		t.Fatalf("fanout-2 calls=%d want 1", len(cfg.Phases[4].AutoExecuteToolCalls))
	}
	if cfg.DefaultREPLCode == "" {
		t.Fatal("expected default context REPL code")
	}
	if !strings.Contains(cfg.DefaultRLMQueryPrompt, "bounded branch") {
		t.Fatalf("default query prompt=%q", cfg.DefaultRLMQueryPrompt)
	}
}

func TestLongCoTREPLRunnerConfigLambdaReplUsesBoundedPhases(t *testing.T) {
	t.Parallel()

	condition := longcoteval.Condition{
		ID:            longcoteval.ConditionRLMLambdaReplSingle,
		Kind:          longcoteval.ConditionKindRLM,
		RLMPlanMode:   "repl_lambda",
		MaxDepth:      2,
		MaxIterations: 8,
		MaxSubcalls:   2,
	}
	cfg := longCoTREPLRunnerConfig(
		longcoteval.Question{PromptText: "Return solution = 42."},
		condition,
		longCoTLiveTarget{Provider: "openai", Model: "gpt-5"},
		longCoTHelperRuntime{Target: longCoTLiveTarget{Provider: "openai", Model: "gpt-5"}},
		30*time.Second,
		8,
		t.TempDir(),
		rlmruntime.SandboxKindPython,
		true,
		true,
		false,
		false,
		false,
	)
	if !cfg.AsyncRecursion {
		t.Fatal("expected lambda repl condition to keep async recursion available")
	}
	if cfg.RecursionPolicy != rlmruntime.RecursionPolicyRequired {
		t.Fatalf("recursion policy=%q want required", cfg.RecursionPolicy)
	}
	if got, want := phaseNames(cfg.Phases), []string{"lambda_fanout", "lambda_wait", "lambda_verify", "lambda_final"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("lambda repl phases=%v want %v", got, want)
	}
	if got := cfg.Phases[0].RequiredTools; !stringSlicesEqual(got, []string{rlmruntime.RLMQueryToolName}) {
		t.Fatalf("lambda_fanout required tools=%v want [%s]", got, rlmruntime.RLMQueryToolName)
	}
	if got := len(cfg.Phases[0].AutoExecuteToolCalls); got != 2 {
		t.Fatalf("lambda_fanout auto calls=%d want 2", got)
	}
	if got := cfg.Phases[2].OutputKind; got != rlmruntime.REPLPhaseOutputKindREPLCode {
		t.Fatalf("lambda_verify OutputKind=%q want %q", got, rlmruntime.REPLPhaseOutputKindREPLCode)
	}
	if !cfg.Phases[2].RequireVerifierArtifact {
		t.Fatal("lambda_verify should require verifier artifact")
	}
	if !cfg.Phases[2].InjectVerifierPrelude || !cfg.Phases[2].RequireStructuredToolOutputOnly {
		t.Fatalf("lambda_verify should use runtime-assisted verifier artifact helpers: inject=%v structured_only=%v", cfg.Phases[2].InjectVerifierPrelude, cfg.Phases[2].RequireStructuredToolOutputOnly)
	}
	if cfg.ToolErrorRepairMaxAttempts != 1 {
		t.Fatalf("lambda repl should use one code repair attempt, got %d", cfg.ToolErrorRepairMaxAttempts)
	}
	if !cfg.Phases[2].DisableREPLCodeRepair || cfg.Phases[2].MaxTokens != 900 {
		t.Fatalf("lambda_verify should be single-shot compact code: disable_repair=%v max_tokens=%d", cfg.Phases[2].DisableREPLCodeRepair, cfg.Phases[2].MaxTokens)
	}
	if !strings.Contains(cfg.Phases[2].Prompt, "accept_candidate") || !strings.Contains(cfg.Phases[2].Prompt, "rlm_candidates") {
		t.Fatalf("lambda_verify prompt should expose candidate registry helpers:\n%s", cfg.Phases[2].Prompt)
	}
	if len(cfg.Phases[2].RequiredREPLCodeSubstrings) != 0 {
		t.Fatalf("lambda_verify should rely on artifact validation, not brittle code substring gates: %v", cfg.Phases[2].RequiredREPLCodeSubstrings)
	}
	if cfg.Phases[2].VerifierRepairSubcalls != 0 {
		t.Fatalf("lambda_verify repair subcalls=%d want 0", cfg.Phases[2].VerifierRepairSubcalls)
	}
	if !cfg.Phases[3].Final {
		t.Fatal("lambda_final should be final")
	}
	if !cfg.Phases[3].ForwardVerifierArtifactAnswer {
		t.Fatal("lambda_final should forward verified artifact answer")
	}
	if cfg.HelperFactory == nil {
		t.Fatal("expected helper factory to remain available")
	}
}

func TestLongCoTREPLRunnerConfigLambdaAdaptiveUsesSimpleOptionalPhases(t *testing.T) {
	t.Parallel()

	condition := longcoteval.Condition{
		ID:            longcoteval.ConditionRLMLambdaAdaptiveSingle,
		Kind:          longcoteval.ConditionKindRLM,
		RLMPlanMode:   "repl_lambda_adaptive",
		MaxDepth:      2,
		MaxIterations: 8,
		MaxSubcalls:   2,
	}
	cfg := longCoTREPLRunnerConfig(
		longcoteval.Question{PromptText: "Return solution = 42."},
		condition,
		longCoTLiveTarget{Provider: "openai", Model: "gpt-5"},
		longCoTHelperRuntime{Target: longCoTLiveTarget{Provider: "openai", Model: "gpt-5"}},
		30*time.Second,
		8,
		t.TempDir(),
		rlmruntime.SandboxKindPython,
		false,
		false,
		false,
		false,
		false,
	)
	if !cfg.AsyncRecursion {
		t.Fatal("expected adaptive lambda to keep async recursion available")
	}
	if cfg.RecursionPolicy != rlmruntime.RecursionPolicyOptional {
		t.Fatalf("recursion policy=%q want optional", cfg.RecursionPolicy)
	}
	if cfg.LLM.RequireToolUse {
		t.Fatal("adaptive lambda should be model-first and not require tool use")
	}
	if got, want := phaseNames(cfg.Phases), []string{"solve_direct", "tool_assist", "final"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("adaptive phases=%v want %v", got, want)
	}
	if cfg.Phases[0].Final {
		t.Fatal("solve_direct should not be the final phase")
	}
	if cfg.Phases[0].MaxTokens != 0 {
		t.Fatalf("solve_direct should inherit the condition token budget, got max_tokens=%d", cfg.Phases[0].MaxTokens)
	}
	if !cfg.Phases[2].Final {
		t.Fatal("final should be the no-tool formatting phase")
	}
	if cfg.Phases[0].RequireVerifierArtifact {
		t.Fatal("adaptive solve should not require verifier artifact")
	}
	if cfg.Phases[0].AutoExecuteRequiredTool {
		t.Fatal("adaptive solve should not force tool execution")
	}
	if len(cfg.Phases[0].Tools) != 0 {
		t.Fatalf("direct solve phase should not expose tools, got %v", cfg.Phases[0].Tools)
	}
	if !stringSlicesEqual(cfg.Phases[1].Tools, []string{rlmruntime.PythonREPLToolName, rlmruntime.RLMQueryToolName, rlmruntime.RLMWaitToolName, rlmruntime.RLMResultToolName}) {
		t.Fatalf("adaptive tool-assist tools=%v", cfg.Phases[1].Tools)
	}
	if !stringSlicesEqual(cfg.Phases[1].RequiredTools, []string{rlmruntime.PythonREPLToolName}) {
		t.Fatalf("tool-assist phase should require a REPL check, got %v", cfg.Phases[1].RequiredTools)
	}
	if cfg.Sandbox.Python.AllowPackageInstall {
		t.Fatal("host Python sandbox should not install task packages")
	}
	if !cfg.Sandbox.SmolVMPython.AllowPackageInstall {
		t.Fatal("adaptive lambda smolvm sandbox should allow policy-controlled package installs")
	}
	if cfg.Sandbox.SmolVMPython.Network {
		t.Fatal("adaptive lambda smolvm sandbox should run without outbound network")
	}
	if cfg.Sandbox.SmolVMPython.CreateOnInit {
		t.Fatal("adaptive lambda smolvm sandbox should require the prepared offline machine")
	}
	if cfg.Sandbox.SmolVMPython.MachineName != "foxctl-rlm-longcot-glibc-offline" {
		t.Fatalf("smolvm machine=%q want foxctl-rlm-longcot-glibc-offline", cfg.Sandbox.SmolVMPython.MachineName)
	}
	if !strings.Contains(cfg.Sandbox.SmolVMPython.GuestWorkDir, "/runs/") {
		t.Fatalf("smolvm guest workdir should be run-scoped, got %q", cfg.Sandbox.SmolVMPython.GuestWorkDir)
	}
	if cfg.Sandbox.SmolVMPython.SitePackagesDir != "/workspace/foxctl-rlm-python/site-packages" {
		t.Fatalf("smolvm site packages dir=%q", cfg.Sandbox.SmolVMPython.SitePackagesDir)
	}
	if !stringSlicesContain(cfg.Sandbox.SmolVMPython.AllowedPackages, "python-chess") {
		t.Fatalf("allowed packages should include python-chess, got %v", cfg.Sandbox.SmolVMPython.AllowedPackages)
	}
	if got := cfg.Sandbox.SmolVMPython.PackageAliases["chess"]; got != "python-chess" {
		t.Fatalf("smolvm python package alias chess=%q want python-chess", got)
	}
	if got := cfg.Sandbox.SmolVMPython.PackageAliases["rdkit"]; got != "rdkit" {
		t.Fatalf("smolvm python package alias rdkit=%q want rdkit", got)
	}
	if cfg.Sandbox.MachineMode != "serialized_shared" {
		t.Fatalf("sandbox machine mode=%q want serialized_shared", cfg.Sandbox.MachineMode)
	}
	if cfg.Sandbox.EvalImageID != "foxctl-python312-longcot-glibc" {
		t.Fatalf("sandbox eval image id=%q want foxctl-python312-longcot-glibc", cfg.Sandbox.EvalImageID)
	}
	if len(cfg.Sandbox.SmolVMPython.ForwardEnv) != 0 {
		t.Fatalf("smolvm python sandbox should not forward API key env by default, got %v", cfg.Sandbox.SmolVMPython.ForwardEnv)
	}
	if !strings.Contains(cfg.Phases[1].Prompt, "packages") || !strings.Contains(cfg.Phases[1].Prompt, "python-chess") {
		t.Fatalf("tool-assist prompt should teach package requests, got: %s", cfg.Phases[1].Prompt)
	}
	if !strings.Contains(cfg.Phases[1].Prompt, "official_prompt") {
		t.Fatalf("tool-assist prompt should tell models to parse exact prompt variables, got: %s", cfg.Phases[1].Prompt)
	}
	if !cfg.Phases[1].RequireToolResultOK {
		t.Fatal("tool-assist phase should require successful REPL execution")
	}
	if !cfg.Phases[1].AutoVerifyPriorSolutionLine {
		t.Fatal("tool-assist phase should auto-verify a prior direct solution line")
	}
	if !cfg.Phases[1].IncludePriorAssistantText {
		t.Fatal("tool-assist phase should include the prior candidate for repair")
	}
	if len(cfg.Phases[2].Tools) != 0 {
		t.Fatalf("final phase should not expose tools, got %v", cfg.Phases[2].Tools)
	}
	if !cfg.Phases[2].BlockFinalOnFailedToolEvidence {
		t.Fatal("adaptive final should block when prior structured tool evidence failed")
	}
	if !cfg.Phases[2].ForwardStructuredToolAnswer {
		t.Fatal("adaptive final should forward the structured RLM_ANSWER_JSON sentinel when present")
	}
	if !cfg.Phases[2].ForwardExecutedStructuredToolAnswer {
		t.Fatal("adaptive final should trust executed REPL structured answers after failed evidence is ruled out")
	}
	if !cfg.Phases[2].RequireStructuredToolAnswer {
		t.Fatal("adaptive final should require a structured sentinel after the required tool-assist phase")
	}
	if cfg.Phases[2].ForwardPriorSolutionLine {
		t.Fatal("adaptive final should not forward stale free-form prior solution lines after tool-assist runs")
	}
	solvePrompt := cfg.Phases[0].Prompt
	if !strings.Contains(solvePrompt, "without calling tools") {
		t.Fatalf("adaptive solve prompt should allow direct answers, got %q", solvePrompt)
	}
	toolPrompt := cfg.Phases[1].Prompt
	if !strings.Contains(toolPrompt, "RLM_ANSWER_JSON=") {
		t.Fatalf("adaptive solve prompt should document optional structured answer sentinel, got %q", solvePrompt)
	}
	if !strings.Contains(toolPrompt, "RLM_CHECK_JSON={\"pass\":false") {
		t.Fatalf("adaptive solve prompt should document failed deterministic checks, got %q", solvePrompt)
	}
}

func TestLongCoTREPLRunnerConfigSmolVMMachineEnvOverride(t *testing.T) {
	t.Setenv("FOXCTL_LONGCOT_SMOLVM_MACHINE", "foxctl-rlm-longcot-glibc-builder")
	t.Setenv("FOXCTL_LONGCOT_SMOLVM_IMAGE", "python:3.12-slim")
	t.Setenv("FOXCTL_LONGCOT_SMOLVM_IMAGE_ID", "foxctl-longcot-python-2026-05-04")
	t.Setenv("FOXCTL_LONGCOT_SMOLVM_MACHINE_MODE", "per_attempt")

	condition := longcoteval.Condition{
		ID:            longcoteval.ConditionRLMLambdaAdaptiveSingle,
		Kind:          longcoteval.ConditionKindRLM,
		RLMPlanMode:   "repl_lambda_adaptive",
		MaxDepth:      2,
		MaxIterations: 8,
		MaxSubcalls:   2,
	}
	cfg := longCoTREPLRunnerConfig(
		longcoteval.Question{PromptText: "Return solution = 42."},
		condition,
		longCoTLiveTarget{Provider: "openai", Model: "gpt-5"},
		longCoTHelperRuntime{Target: longCoTLiveTarget{Provider: "openai", Model: "gpt-5"}},
		30*time.Second,
		8,
		t.TempDir(),
		rlmruntime.SandboxKindPython,
		false,
		false,
		false,
		false,
		false,
	)
	if cfg.Sandbox.SmolVMPython.MachineName != "foxctl-rlm-longcot-glibc-builder" {
		t.Fatalf("smolvm machine=%q", cfg.Sandbox.SmolVMPython.MachineName)
	}
	if cfg.Sandbox.SmolVMPython.Image != "python:3.12-slim" {
		t.Fatalf("smolvm image=%q", cfg.Sandbox.SmolVMPython.Image)
	}
	if cfg.Sandbox.EvalImageID != "foxctl-longcot-python-2026-05-04" {
		t.Fatalf("sandbox eval image id=%q", cfg.Sandbox.EvalImageID)
	}
	if cfg.Sandbox.MachineMode != "per_attempt" {
		t.Fatalf("sandbox machine mode=%q", cfg.Sandbox.MachineMode)
	}
}

func TestLongCoTSmolVMImageIDFollowsImageOverride(t *testing.T) {
	t.Setenv("FOXCTL_LONGCOT_SMOLVM_IMAGE", "python:3.12-bookworm")
	t.Setenv("FOXCTL_LONGCOT_SMOLVM_IMAGE_ID", "")

	if got := longCoTSmolVMImageID(); got != "python:3.12-bookworm" {
		t.Fatalf("image id=%q want python:3.12-bookworm", got)
	}
}

func TestLongCoTREPLRunnerConfigSmolVMRecordsCapabilityProbe(t *testing.T) {
	t.Parallel()

	condition := longcoteval.Condition{
		ID:            longcoteval.ConditionRLMLambdaAdaptiveSingle,
		Kind:          longcoteval.ConditionKindRLM,
		RLMPlanMode:   "repl_lambda_adaptive",
		MaxDepth:      2,
		MaxIterations: 8,
		MaxSubcalls:   2,
	}
	cfg := longCoTREPLRunnerConfig(
		longcoteval.Question{PromptText: "Return solution = 42."},
		condition,
		longCoTLiveTarget{Provider: "openai", Model: "gpt-5"},
		longCoTHelperRuntime{Target: longCoTLiveTarget{Provider: "openai", Model: "gpt-5"}},
		30*time.Second,
		8,
		t.TempDir(),
		rlmruntime.SandboxKindSmolVMPython,
		false,
		false,
		false,
		false,
		false,
	)
	if !stringSlicesContain(cfg.Sandbox.CapabilityProbe, "chess") || !stringSlicesContain(cfg.Sandbox.CapabilityProbe, "rdkit.Chem") {
		t.Fatalf("sandbox capability probe missing expected modules: %v", cfg.Sandbox.CapabilityProbe)
	}
	if cfg.Sandbox.MachineMode != "serialized_shared" {
		t.Fatalf("sandbox machine mode=%q want serialized_shared", cfg.Sandbox.MachineMode)
	}
}

func TestLongCoTREPLRunnerConfigLambdaAdaptiveLongInputForcesFanoutAndOfficialPromptVerifier(t *testing.T) {
	t.Parallel()

	condition := longcoteval.Condition{
		ID:            longcoteval.ConditionRLMLambdaAdaptiveSingle,
		Kind:          longcoteval.ConditionKindRLM,
		RLMPlanMode:   "repl_lambda_adaptive",
		MaxDepth:      2,
		MaxIterations: 8,
		MaxSubcalls:   3,
	}
	longPrompt := "Return solution = <value>.\n" + strings.Repeat("node_1 depends on node_2.\n", 90)
	cfg := longCoTREPLRunnerConfig(
		longcoteval.Question{PromptText: longPrompt},
		condition,
		longCoTLiveTarget{Provider: "openai", Model: "gpt-5"},
		longCoTHelperRuntime{Target: longCoTLiveTarget{Provider: "openai", Model: "gpt-5"}},
		30*time.Second,
		8,
		t.TempDir(),
		rlmruntime.SandboxKindPython,
		false,
		false,
		false,
		false,
		false,
	)
	if cfg.RecursionPolicy != rlmruntime.RecursionPolicyOptional {
		t.Fatalf("recursion policy=%q want optional", cfg.RecursionPolicy)
	}
	if got, want := phaseNames(cfg.Phases), []string{"prompt_packet", "long_fanout", "long_wait", "long_tool_verify", "final"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("adaptive long phases=%v want %v", got, want)
	}
	if !cfg.Phases[0].AutoExecuteRequiredTool || !stringSlicesEqual(cfg.Phases[0].RequiredTools, []string{rlmruntime.PythonREPLToolName}) {
		t.Fatalf("prompt_packet should auto-execute python_repl, required=%v auto=%v", cfg.Phases[0].RequiredTools, cfg.Phases[0].AutoExecuteRequiredTool)
	}
	if !stringSlicesEqual(cfg.Phases[0].RequiredToolOutputSubstrings, []string{"PROMPT_PACKET_JSON="}) {
		t.Fatalf("prompt_packet required output substrings=%v", cfg.Phases[0].RequiredToolOutputSubstrings)
	}
	if !cfg.Phases[1].AutoExecuteRequiredTool || !stringSlicesEqual(cfg.Phases[1].RequiredTools, []string{rlmruntime.RLMQueryToolName}) {
		t.Fatalf("long_fanout should auto-execute rlm_query, required=%v auto=%v", cfg.Phases[1].RequiredTools, cfg.Phases[1].AutoExecuteRequiredTool)
	}
	if len(cfg.Phases[1].AutoExecuteToolCalls) != 3 {
		t.Fatalf("long_fanout calls=%d want 3", len(cfg.Phases[1].AutoExecuteToolCalls))
	}
	if !strings.Contains(string(cfg.Phases[1].AutoExecuteToolCalls[0].Args), "Parser child") {
		t.Fatalf("long_fanout first child should be parser role, args=%s", string(cfg.Phases[1].AutoExecuteToolCalls[0].Args))
	}
	verify := cfg.Phases[3]
	if verify.OutputKind != rlmruntime.REPLPhaseOutputKindREPLCode {
		t.Fatalf("long_tool_verify output kind=%q want repl_code", verify.OutputKind)
	}
	if len(verify.RequiredREPLCodeSubstrings) != 0 {
		t.Fatalf("long_tool_verify should not require brittle code substrings=%v", verify.RequiredREPLCodeSubstrings)
	}
	if len(verify.RequiredToolOutputSubstrings) != 0 || !verify.RequireStructuredToolOutputOnly {
		t.Fatalf("long_tool_verify should rely on strict structured output, substrings=%v strict=%v", verify.RequiredToolOutputSubstrings, verify.RequireStructuredToolOutputOnly)
	}
	if !verify.InjectVerifierPrelude {
		t.Fatalf("long_tool_verify should inject verifier prelude")
	}
	if !verify.AllowPartialPseudoToolCallCode {
		t.Fatalf("long_tool_verify should allow phase-scoped pseudo tool-call code salvage")
	}
	if len(verify.AllowedREPLImports) != 0 {
		t.Fatalf("python long_tool_verify should not allow third-party imports by default: %v", verify.AllowedREPLImports)
	}
	if !strings.Contains(verify.Prompt, "accept(answer") || !strings.Contains(verify.Prompt, "reject(reason)") {
		t.Fatalf("long_tool_verify prompt should expose accept/reject helpers:\n%s", verify.Prompt)
	}
	if !strings.Contains(verify.Prompt, "exact_labeled_section") || !strings.Contains(verify.Prompt, "regex_tokens_from_labeled_section") {
		t.Fatalf("long_tool_verify prompt should expose generic exact-data helpers:\n%s", verify.Prompt)
	}
	if !cfg.Phases[4].ForwardExecutedStructuredToolAnswer || !cfg.Phases[4].RequireStructuredToolAnswer {
		t.Fatalf("final should forward executed structured tool answer: forward=%v require=%v", cfg.Phases[4].ForwardExecutedStructuredToolAnswer, cfg.Phases[4].RequireStructuredToolAnswer)
	}
}

func TestLongCoTREPLRunnerConfigLambdaAdaptiveLongInputBudgetsVerifierRepair(t *testing.T) {
	t.Parallel()

	condition := longcoteval.Condition{
		ID:            longcoteval.ConditionRLMLambdaAdaptiveSingle,
		Kind:          longcoteval.ConditionKindRLM,
		RLMPlanMode:   "repl_lambda_adaptive",
		MaxDepth:      2,
		MaxIterations: 3,
		MaxSubcalls:   3,
	}
	longPrompt := "Return solution = <value>.\n" + strings.Repeat("node_1 depends on node_2.\n", 90)
	cfg := longCoTREPLRunnerConfig(
		longcoteval.Question{PromptText: longPrompt},
		condition,
		longCoTLiveTarget{Provider: "openai", Model: "gpt-5"},
		longCoTHelperRuntime{Target: longCoTLiveTarget{Provider: "openai", Model: "gpt-5"}},
		30*time.Second,
		3,
		t.TempDir(),
		rlmruntime.SandboxKindPython,
		false,
		false,
		false,
		false,
		false,
	)
	if got, want := cfg.Budget.MaxREPLCalls, 5; got < want {
		t.Fatalf("MaxREPLCalls=%d want at least %d for prompt packet + verifier repair attempts", got, want)
	}
	if got, want := cfg.Budget.MaxIterations, 5; got < want {
		t.Fatalf("MaxIterations=%d want at least %d for verifier filtering/repair attempts", got, want)
	}
}

func TestLongCoTEnsurePhaseREPLBudgetCoversChildSolvePhases(t *testing.T) {
	t.Parallel()

	cfg := rlmruntime.REPLRunnerConfig{
		Budget: rlmruntime.BudgetConfig{
			MaxIterations: 2,
			MaxREPLCalls:  2,
		},
		ToolErrorRepairMaxAttempts: 2,
		Phases:                     longCoTChildSolvePhases(rlmruntime.SandboxKindPython, false),
	}

	longCoTEnsurePhaseREPLBudget(&cfg)

	if got, want := cfg.Budget.MaxIterations, 4; got < want {
		t.Fatalf("MaxIterations=%d want at least %d for child scratch + repair + final", got, want)
	}
	if got, want := cfg.Budget.MaxREPLCalls, 4; got < want {
		t.Fatalf("MaxREPLCalls=%d want at least %d for child context + scratch repair", got, want)
	}
}

func TestLongCoTFanoutQueryCallsGiveChildrenEnoughModelTurns(t *testing.T) {
	t.Parallel()

	calls := longCoTFanoutQueryCalls([]string{"child"})
	if got, want := len(calls), 1; got != want {
		t.Fatalf("calls=%d want %d", got, want)
	}
	var payload struct {
		MaxIterations int `json:"max_iterations"`
	}
	if err := json.Unmarshal(calls[0].Args, &payload); err != nil {
		t.Fatalf("decode args: %v", err)
	}
	if got, want := payload.MaxIterations, 3; got != want {
		t.Fatalf("max_iterations=%d want %d", got, want)
	}
}

func TestLongCoTRetryableRLMErrorIncludesEmptyChildResponse(t *testing.T) {
	t.Parallel()

	if !longCoTRetryableRLMError(errors.New("rlm repl runner: empty assistant response")) {
		t.Fatal("empty assistant response should be retryable for child runs")
	}
	if longCoTRetryableRLMError(errors.New("rlm runtime: iterations budget exceeded")) {
		t.Fatal("budget errors should not be retried blindly")
	}
}

func TestLongCoTVerifierAllowedImportsForSmolVM(t *testing.T) {
	t.Parallel()

	got := longCoTVerifierAllowedImports(rlmruntime.SandboxKindSmolVMPython)
	want := []string{"chess", "rdkit", "sympy", "networkx", "numpy"}
	if !stringSlicesEqual(got, want) {
		t.Fatalf("smolvm verifier allowed imports=%v want %v", got, want)
	}
	if got := longCoTVerifierAllowedImports(rlmruntime.SandboxKindPython); len(got) != 0 {
		t.Fatalf("plain python verifier allowed imports=%v want empty", got)
	}
}

func TestLongCoTChildSandboxConfigIsolatesScratchWorkdir(t *testing.T) {
	t.Parallel()

	cfg := rlmruntime.SandboxConfig{
		Kind: rlmruntime.SandboxKindSmolVMPython,
		Python: repl.Options{
			WorkDir: "/tmp/foxctl-longcot-sandboxes/backtracking",
		},
		SmolVMPython: repl.SmolVMPythonOptions{
			GuestWorkDir:    "/workspace/foxctl-rlm-python/runs/backtracking",
			SitePackagesDir: "/workspace/foxctl-rlm-python/site-packages",
		},
	}
	childTask := rlm.Task{
		AgentID:         "eval/longcot/rlm_lambda_adaptive_single/root-2",
		OutputNamespace: "runs/attempt/nodes/root-2",
	}

	got := longCoTChildSandboxConfig(cfg, childTask)
	if got.Python.WorkDir == cfg.Python.WorkDir || !strings.HasSuffix(got.Python.WorkDir, filepath.Join("children", "root-2")) {
		t.Fatalf("child python workdir=%q parent=%q", got.Python.WorkDir, cfg.Python.WorkDir)
	}
	if got.SmolVMPython.GuestWorkDir == cfg.SmolVMPython.GuestWorkDir || !strings.HasSuffix(got.SmolVMPython.GuestWorkDir, "/children/root-2") {
		t.Fatalf("child smolvm guest workdir=%q parent=%q", got.SmolVMPython.GuestWorkDir, cfg.SmolVMPython.GuestWorkDir)
	}
	if got.SmolVMPython.SitePackagesDir != cfg.SmolVMPython.SitePackagesDir {
		t.Fatalf("child should share package cache, got site-packages=%q", got.SmolVMPython.SitePackagesDir)
	}
}

func TestLongCoTDefaultContextREPLCodeBuildsCompactPacket(t *testing.T) {
	t.Parallel()

	code := longCoTDefaultContextREPLCode(rlmruntime.SandboxKindPython)
	for _, want := range []string{"official_prompt", "PROMPT_PACKET_JSON=", "answer_format", "counts", "runtime_capabilities", "exact_data_sections"} {
		if !strings.Contains(code, want) {
			t.Fatalf("default context code missing %q:\n%s", want, code)
		}
	}
	if strings.Contains(code, "print(official_prompt)") {
		t.Fatalf("default context code should not dump the full official prompt:\n%s", code)
	}
}

func TestLongCoTPromptPacketIncludesSmolVMCapabilities(t *testing.T) {
	t.Parallel()

	code := longCoTPromptPacketREPLCode(rlmruntime.SandboxKindSmolVMPython)
	for _, want := range []string{"runtime_capabilities", "official_prompt_rule", "importlib.util.find_spec", "'available'", "chess", "rdkit.Chem", "sympy", "networkx", "numpy"} {
		if !strings.Contains(code, want) {
			t.Fatalf("smolvm prompt packet code missing %q:\n%s", want, code)
		}
	}

	line := longCoTCapabilityPromptLine(rlmruntime.SandboxKindSmolVMPython)
	for _, want := range []string{"capability packet", "chess", "rdkit.Chem", "sympy"} {
		if !strings.Contains(line, want) {
			t.Fatalf("capability prompt line missing %q: %s", want, line)
		}
	}
}

func TestLongCoTApplyAttemptSandboxWorkDirSeparatesAttempts(t *testing.T) {
	t.Parallel()

	cfg := rlmruntime.REPLRunnerConfig{
		Sandbox: rlmruntime.SandboxConfig{
			Python:       repl.Options{WorkDir: "/tmp/foxctl-longcot-sandboxes/q-cond"},
			SmolVMPython: repl.SmolVMPythonOptions{GuestWorkDir: "/workspace/foxctl-rlm-python/runs/q-cond"},
		},
	}
	longCoTApplyAttemptSandboxWorkDir(&cfg, "attempt-abc/with spaces")
	if !strings.HasSuffix(cfg.Sandbox.Python.WorkDir, filepath.Join("q-cond", "attempt-abc-with-spaces")) {
		t.Fatalf("python workdir=%q", cfg.Sandbox.Python.WorkDir)
	}
	if !strings.HasSuffix(cfg.Sandbox.SmolVMPython.GuestWorkDir, "/q-cond/attempt-abc-with-spaces") {
		t.Fatalf("smolvm guest workdir=%q", cfg.Sandbox.SmolVMPython.GuestWorkDir)
	}
}

func TestLongCoTDatasetAnswerVerifier(t *testing.T) {
	t.Parallel()

	attempts := []longcoteval.Attempt{
		{QuestionID: "math", Status: longcoteval.AttemptStatusOK, ResponseText: "solution = 42"},
		{QuestionID: "logic", Status: longcoteval.AttemptStatusOK, ResponseText: "solution = move A to B"},
		{QuestionID: "bad", Status: longcoteval.AttemptStatusOK, ResponseText: "solution = 41"},
		{QuestionID: "format", Status: longcoteval.AttemptStatusOK, ResponseText: "the answer is 42"},
	}
	questions := []longcoteval.Question{
		{ID: "math", Answer: `{"solution":42}`},
		{ID: "logic", Answer: `{"solution":"move A to B"}`},
		{ID: "bad", Answer: `{"solution":42}`},
		{ID: "format", Answer: `{"solution":42}`},
	}

	verified, result := verifyLongCoTAttemptsAgainstDatasetAnswers(attempts, questions)
	if result.VerifierName != "foxctl.dataset_answer" {
		t.Fatalf("verifier name=%q", result.VerifierName)
	}
	if got := result.Counts["correct"]; got != 2 {
		t.Fatalf("correct count=%d rows=%+v", got, result.Rows)
	}
	if got := result.Counts["incorrect"]; got != 1 {
		t.Fatalf("incorrect count=%d rows=%+v", got, result.Rows)
	}
	if got := result.Counts["wrong_formatting"]; got != 1 {
		t.Fatalf("wrong formatting count=%d rows=%+v", got, result.Rows)
	}
	if !verified[0].Correct || verified[0].VerifierStatus != longcoteval.VerifierStatusCorrect {
		t.Fatalf("math attempt not correct: %+v", verified[0])
	}
	if !verified[1].Correct || verified[1].NormalizedAnswer != "move A to B" {
		t.Fatalf("logic attempt not correct: %+v", verified[1])
	}
	if verified[2].Correct || verified[2].VerifierStatus != longcoteval.VerifierStatusIncorrect {
		t.Fatalf("bad attempt should be incorrect: %+v", verified[2])
	}
	if !verified[3].WrongFormatting || verified[3].VerifierStatus != longcoteval.VerifierStatusWrongFormatting {
		t.Fatalf("format attempt should be wrong_formatting: %+v", verified[3])
	}
}

func TestLongCoTREPLRunnerConfigUsesQuestionRequiredSubcallRules(t *testing.T) {
	t.Parallel()

	cfg := longCoTREPLRunnerConfig(
		longcoteval.Question{
			RequiredSubcallRules: []longcoteval.RequiredSubcallRule{
				{Child: 1, RequiredSubcalls: 1},
			},
		},
		longcoteval.Condition{
			ID:            longcoteval.ConditionRLMReplRecursive,
			Kind:          longcoteval.ConditionKindRLM,
			MaxDepth:      2,
			MaxIterations: 8,
			MaxSubcalls:   2,
		},
		longCoTLiveTarget{Provider: "openai", Model: "gpt-5"},
		longCoTHelperRuntime{Target: longCoTLiveTarget{Provider: "openai", Model: "gpt-5"}},
		30*time.Second,
		8,
		t.TempDir(),
		rlmruntime.SandboxKindPython,
		false,
		false,
		false,
		true,
		false,
	)
	if len(cfg.RequiredSubcallRules) != 1 {
		t.Fatalf("required rules = %+v, want one rule", cfg.RequiredSubcallRules)
	}
	if cfg.RequiredSubcallRules[0].Child != 1 || cfg.RequiredSubcallRules[0].RequiredSubcalls != 1 {
		t.Fatalf("required rule = %+v", cfg.RequiredSubcallRules[0])
	}
}

func TestLongCoTBraidSolvePhases(t *testing.T) {
	t.Parallel()

	phases := longCoTBraidSolvePhases(rlmruntime.SandboxKindPython)
	if got := fmt.Sprint(phaseNames(phases)); got != "[context graph_plan graph_fanout final]" {
		t.Fatalf("phases=%s", got)
	}
	planIdx := -1
	fanoutIdx := -1
	for i, phase := range phases {
		switch phase.Name {
		case "graph_plan":
			planIdx = i
		case "graph_fanout":
			fanoutIdx = i
		}
	}
	if planIdx < 0 || fanoutIdx < 0 {
		t.Fatalf("missing graph phases in %v", phaseNames(phases))
	}
	if got := phases[planIdx].OutputKind; got != rlmruntime.REPLPhaseOutputKindBraidGraph {
		t.Fatalf("graph_plan OutputKind=%q want %q", got, rlmruntime.REPLPhaseOutputKindBraidGraph)
	}
	if got := strings.TrimSpace(string(phases[planIdx].ResponseFormat)); got != `{"type":"json_object"}` {
		t.Fatalf("graph_plan ResponseFormat=%q want json_object", got)
	}
	if got := phases[planIdx].MaxGraphNodes; got != 12 {
		t.Fatalf("graph_plan MaxGraphNodes=%d want 12", got)
	}
	if got := phases[planIdx].MaxTokens; got != longCoTBraidGraphPlanMaxTokens {
		t.Fatalf("graph_plan MaxTokens=%d want %d", got, longCoTBraidGraphPlanMaxTokens)
	}
	if got := phases[planIdx].BraidGraphPolicy; got != rlmruntime.BraidGraphPolicyLongCoTController {
		t.Fatalf("graph_plan BraidGraphPolicy=%q want %q", got, rlmruntime.BraidGraphPolicyLongCoTController)
	}
	for _, want := range []string{"valid json object", "one primary solve wave", "Allowed kind values are extract, solve, cycle_solve, verify, reduce", "cycle_solve is optional", "BlocksWorld-style stack puzzles", "do not segment the plan by vague phases", "Use one primary solve node to build an executable candidate", "Keep the runtime graph acyclic", "Every solve, cycle_solve, and verify node must additionally include archetype, scaffold_class, scaffold_id, and input_schema", "Allowed scaffold pairs are strict", "Do not use generic_v1", "prefer finite_state_transition/stack_relocation_v1", "Use state_transition/state_replay_v1 only for replaying an explicit action sequence", "Do not copy large literals", "\"source_ref\":\"official_prompt\""} {
		if !strings.Contains(phases[planIdx].Prompt, want) {
			t.Fatalf("graph_plan prompt missing %q:\n%s", want, phases[planIdx].Prompt)
		}
	}
	if !phases[planIdx].RequireScaffoldContract {
		t.Fatal("graph_plan RequireScaffoldContract=false want true")
	}
	if !phases[fanoutIdx].AutoExecuteGraphNodes {
		t.Fatal("graph_fanout AutoExecuteGraphNodes=false want true")
	}
	if got := phases[fanoutIdx].BraidGraphPolicy; got != rlmruntime.BraidGraphPolicyLongCoTController {
		t.Fatalf("graph_fanout BraidGraphPolicy=%q want %q", got, rlmruntime.BraidGraphPolicyLongCoTController)
	}
	if got := phases[fanoutIdx].MaxTokens; got != longCoTBraidGraphFanoutMaxTokens {
		t.Fatalf("graph_fanout MaxTokens=%d want %d", got, longCoTBraidGraphFanoutMaxTokens)
	}
	if !phases[fanoutIdx].RequireScaffoldContract {
		t.Fatal("graph_fanout RequireScaffoldContract=false want true")
	}
	if phases[fanoutIdx].DisableHelperFirstFallback {
		t.Fatal("graph_fanout DisableHelperFirstFallback=true want false")
	}
	if !stringSlicesEqual(phases[fanoutIdx].Tools, []string{rlmruntime.RLMQueryToolName}) {
		t.Fatalf("graph_fanout tools=%v want [%s]", phases[fanoutIdx].Tools, rlmruntime.RLMQueryToolName)
	}
	if phases[fanoutIdx].BraidRepairAttempts != 2 {
		t.Fatalf("graph_fanout BraidRepairAttempts=%d want 2", phases[fanoutIdx].BraidRepairAttempts)
	}
	if got := phases[len(phases)-1].MaxTokens; got != longCoTBraidFinalMaxTokens {
		t.Fatalf("final MaxTokens=%d want %d", got, longCoTBraidFinalMaxTokens)
	}
}

func TestLongCoTBraidGeneralHelperDoesNotReplaceBraidPhases(t *testing.T) {
	t.Parallel()

	condition := longcoteval.Condition{
		ID:             longcoteval.ConditionRLMBraidSingle,
		Kind:           longcoteval.ConditionKindRLM,
		RLMPlanMode:    "repl_braid",
		RLMToolProfile: "longcot-repl-recursive",
		MaxDepth:       1,
		MaxIterations:  8,
		MaxSubcalls:    4,
	}
	cfg := longCoTREPLRunnerConfig(
		longcoteval.Question{PromptText: "Solve a generic task."},
		condition,
		longCoTLiveTarget{Provider: "openai", Model: "gpt-5"},
		longCoTHelperRuntime{Target: longCoTLiveTarget{Provider: "openai", Model: "gpt-5"}, Attempts: 2},
		30*time.Second,
		8,
		t.TempDir(),
		rlmruntime.SandboxKindPython,
		false,
		true,
		false,
		false,
		false,
	)
	if got := fmt.Sprint(phaseNames(cfg.Phases)); got != "[context graph_plan graph_fanout final]" {
		t.Fatalf("phases=%s, want braid phases", got)
	}
	if cfg.HelperFactory == nil {
		t.Fatal("HelperFactory nil with general helper enabled")
	}
}

func TestLongCoTChildSolvePhasesAutoInspectContext(t *testing.T) {
	t.Parallel()

	phases := longCoTChildSolvePhases(rlmruntime.SandboxKindPython, false)
	if got := fmt.Sprint(phaseNames(phases)); got != "[child_context child_scratch child_final]" {
		t.Fatalf("phases=%s", got)
	}
	if !phases[0].AutoExecuteRequiredTool {
		t.Fatal("child context phase should auto-execute required REPL tool")
	}
	if !stringSlicesEqual(phases[0].RequiredTools, []string{rlmruntime.PythonREPLToolName}) {
		t.Fatalf("required tools=%v want [%s]", phases[0].RequiredTools, rlmruntime.PythonREPLToolName)
	}
	if !stringSlicesEqual(phases[1].Tools, []string{rlmruntime.PythonREPLToolName}) {
		t.Fatalf("scratch tools=%v want [%s]", phases[1].Tools, rlmruntime.PythonREPLToolName)
	}
	if phases[1].OutputKind != rlmruntime.REPLPhaseOutputKindREPLCode {
		t.Fatalf("scratch output kind=%q want repl_code", phases[1].OutputKind)
	}
	if phases[1].MaxTokens != 1280 {
		t.Fatalf("scratch max tokens=%d want 1280", phases[1].MaxTokens)
	}
	if phases[1].MaxIterations != 1 {
		t.Fatalf("scratch max iterations=%d want 1", phases[1].MaxIterations)
	}
	if !phases[1].RequireToolOutput || !phases[1].RequireToolResultOK {
		t.Fatalf("scratch should require output and successful execution: output=%v ok=%v", phases[1].RequireToolOutput, phases[1].RequireToolResultOK)
	}
	if !phases[1].ContinueOnREPLCodeError {
		t.Fatalf("scratch should continue to final summary after repl_code failure")
	}
	if !strings.Contains(phases[1].Prompt, "standard library") {
		t.Fatalf("scratch prompt missing standard library constraint:\n%s", phases[1].Prompt)
	}
	if !phases[2].Final {
		t.Fatal("child final phase should be final")
	}
	if phases[2].FinalOutputKind != "child_summary" {
		t.Fatalf("child final output kind=%q want child_summary", phases[2].FinalOutputKind)
	}
}

func TestLongCoTChildPhasesForReduceSkipsScratch(t *testing.T) {
	t.Parallel()

	phases := longCoTChildPhasesForTask(rlm.Task{
		Prompt: "BRAID node n_reduce (reduce)\nDependency summaries:\n- n_verify: status: solved",
	}, rlmruntime.SandboxKindPython, false)
	if got := fmt.Sprint(phaseNames(phases)); got != "[child_reduce_final]" {
		t.Fatalf("phases=%s", got)
	}
	if !phases[0].Final {
		t.Fatal("reduce phase should be final")
	}
	if phases[0].FinalOutputKind != "child_summary" {
		t.Fatalf("reduce final output kind=%q want child_summary", phases[0].FinalOutputKind)
	}
	if len(phases[0].Tools) != 0 || len(phases[0].RequiredTools) != 0 {
		t.Fatalf("reduce phase should not expose scratch tools: tools=%v required=%v", phases[0].Tools, phases[0].RequiredTools)
	}
}

func TestLongCoTChildPhasesForExtractSkipsScratch(t *testing.T) {
	t.Parallel()

	phases := longCoTChildPhasesForTask(rlm.Task{
		Prompt: "BRAID node n_extract (extract)\nOfficial root task:\nProblem...",
	}, rlmruntime.SandboxKindPython, false)
	if got := fmt.Sprint(phaseNames(phases)); got != "[child_extract_final]" {
		t.Fatalf("phases=%s", got)
	}
	if !phases[0].Final {
		t.Fatal("extract phase should be final")
	}
	if phases[0].FinalOutputKind != "child_summary" {
		t.Fatalf("extract final output kind=%q want child_summary", phases[0].FinalOutputKind)
	}
	if len(phases[0].Tools) != 0 || len(phases[0].RequiredTools) != 0 {
		t.Fatalf("extract phase should not expose scratch tools: tools=%v required=%v", phases[0].Tools, phases[0].RequiredTools)
	}
	for _, want := range []string{"Facts-only extraction", "Do not solve or verify", `"status":"solved"`} {
		if !strings.Contains(phases[0].Prompt, want) {
			t.Fatalf("extract prompt missing %q:\n%s", want, phases[0].Prompt)
		}
	}
}

func TestLongCoTChildPhasesForVerifyUsesComputationalScratch(t *testing.T) {
	t.Parallel()

	phases := longCoTChildPhasesForTask(rlm.Task{
		Prompt: "BRAID node n_verify (verify)\nDependency summaries:\n- n_solve: status: solved",
	}, rlmruntime.SandboxKindPython, false)
	if got := fmt.Sprint(phaseNames(phases)); got != "[child_verify_scratch child_verify_final]" {
		t.Fatalf("phases=%s", got)
	}
	if phases[0].OutputKind != rlmruntime.REPLPhaseOutputKindREPLCode {
		t.Fatalf("verify scratch output kind=%q want repl_code", phases[0].OutputKind)
	}
	if phases[0].MaxTokens != 512 {
		t.Fatalf("verify scratch max tokens=%d want 512", phases[0].MaxTokens)
	}
	if !phases[0].RequireToolOutput || !phases[0].RequireToolResultOK {
		t.Fatalf("verify scratch should require output and successful execution: output=%v ok=%v", phases[0].RequireToolOutput, phases[0].RequireToolResultOK)
	}
	if !phases[0].ContinueOnREPLCodeError {
		t.Fatalf("verify scratch should continue to final summary after repl_code failure")
	}
	if !stringSlicesEqual(phases[0].Tools, []string{rlmruntime.PythonREPLToolName}) {
		t.Fatalf("verify scratch tools=%v want [%s]", phases[0].Tools, rlmruntime.PythonREPLToolName)
	}
	if !strings.Contains(phases[0].Prompt, "pass=false") {
		t.Fatalf("verify scratch prompt missing pass=false contract:\n%s", phases[0].Prompt)
	}
	if !strings.Contains(phases[0].Prompt, "Do not import sympy") {
		t.Fatalf("verify scratch prompt missing third-party import ban:\n%s", phases[0].Prompt)
	}
	if !phases[1].Final {
		t.Fatal("verify phase should be final")
	}
	if phases[1].FinalOutputKind != "child_summary" {
		t.Fatalf("verify final output kind=%q want child_summary", phases[1].FinalOutputKind)
	}
	if len(phases[1].Tools) != 0 || len(phases[1].RequiredTools) != 0 {
		t.Fatalf("verify final phase should not expose scratch tools: tools=%v required=%v", phases[1].Tools, phases[1].RequiredTools)
	}
}

func TestLongCoTChildPhasesForCycleSolveUsesWitnessContract(t *testing.T) {
	t.Parallel()

	phases := longCoTChildPhasesForTask(rlm.Task{
		Prompt: "BRAID node n_cycle (cycle_solve)\nDependency summaries:\n- n_extract: status: solved",
	}, rlmruntime.SandboxKindPython, true)
	if got := fmt.Sprint(phaseNames(phases)); got != "[child_cycle_packet child_cycle_witness child_cycle_final]" {
		t.Fatalf("phases=%s", got)
	}
	if phases[0].AutoExecuteRequiredTool {
		t.Fatal("cycle packet phase should be model-generated, not auto-executed")
	}
	if phases[0].OutputKind != rlmruntime.REPLPhaseOutputKindCyclePacket {
		t.Fatalf("cycle packet output kind=%q want %q", phases[0].OutputKind, rlmruntime.REPLPhaseOutputKindCyclePacket)
	}
	if len(phases[0].Tools) != 0 || len(phases[0].RequiredTools) != 0 {
		t.Fatalf("cycle packet should not expose tools: tools=%v required=%v", phases[0].Tools, phases[0].RequiredTools)
	}
	if !phases[0].FilterOverlongOutput {
		t.Fatal("cycle packet should filter overlong JSON output")
	}
	if phases[0].FilterOutputMaxTokens != 512 {
		t.Fatalf("cycle packet filter max tokens=%d want 512", phases[0].FilterOutputMaxTokens)
	}
	for _, want := range []string{"Cycle packet phase", "unknowns", "constraints", "candidate_bounds", "under 1400 characters"} {
		if !strings.Contains(phases[0].Prompt, want) {
			t.Fatalf("cycle packet prompt missing %q:\n%s", want, phases[0].Prompt)
		}
	}
	for _, phase := range phases {
		if len(phase.AutoExecuteToolCalls) > 0 && phase.AutoExecuteToolCalls[0].Tool == rlmruntime.EphemeralHelperSolveToolName {
			t.Fatalf("cycle_solve should not auto-run helper phase: %+v", phase)
		}
	}
	if phases[1].OutputKind != rlmruntime.REPLPhaseOutputKindCycleWitness {
		t.Fatalf("cycle witness output kind=%q want cycle_witness", phases[1].OutputKind)
	}
	if phases[1].MaxTokens != 1024 {
		t.Fatalf("cycle witness max tokens=%d want 1024", phases[1].MaxTokens)
	}
	if !phases[1].IncludePriorAssistantText {
		t.Fatal("cycle witness should include prior cycle_packet output")
	}
	if len(phases[1].Tools) != 0 || len(phases[1].RequiredTools) != 0 {
		t.Fatalf("cycle witness should not expose scratch tools: tools=%v required=%v", phases[1].Tools, phases[1].RequiredTools)
	}
	for _, want := range []string{"bounded_search", "variables", "known_values", "constraints", "claims", "sum_prime_factors", "The runtime will check this witness and emit cycle_json"} {
		if !strings.Contains(phases[1].Prompt, want) {
			t.Fatalf("cycle witness prompt missing %q:\n%s", want, phases[1].Prompt)
		}
	}
	if !phases[2].Final {
		t.Fatal("cycle final phase should be final")
	}
	if phases[2].FinalOutputKind != "child_summary" {
		t.Fatalf("cycle final output kind=%q want child_summary", phases[2].FinalOutputKind)
	}
	if !strings.Contains(phases[2].Prompt, "finite candidate bounds were not derivable") {
		t.Fatalf("cycle final prompt missing blocker contract:\n%s", phases[2].Prompt)
	}
	if !strings.Contains(phases[2].Prompt, "Copy the cycle_json object emitted by cycle_witness_check exactly") {
		t.Fatalf("cycle final prompt missing witness check copy contract:\n%s", phases[2].Prompt)
	}
	if strings.Contains(phases[2].Prompt, "helper output") {
		t.Fatalf("cycle final prompt should not reference helper output:\n%s", phases[2].Prompt)
	}
	if !strings.Contains(phases[2].Prompt, "under 600 characters") {
		t.Fatalf("cycle final prompt missing compact output cap:\n%s", phases[2].Prompt)
	}
	if !strings.Contains(phases[2].Prompt, "pass=true|pass=false") {
		t.Fatalf("cycle final prompt missing explicit pass label contract:\n%s", phases[2].Prompt)
	}
	if !strings.Contains(phases[2].Prompt, "cycle_json") {
		t.Fatalf("cycle final prompt missing cycle_json contract:\n%s", phases[2].Prompt)
	}
}

func TestLongCoTChildSolvePhasesSkipGeneralHelperForOrdinarySolve(t *testing.T) {
	t.Parallel()

	phases := longCoTChildSolvePhases(rlmruntime.SandboxKindPython, true)
	if got := fmt.Sprint(phaseNames(phases)); got != "[child_context child_scratch child_final]" {
		t.Fatalf("phases=%s", got)
	}
	for _, phase := range phases {
		if len(phase.AutoExecuteToolCalls) > 0 && phase.AutoExecuteToolCalls[0].Tool == rlmruntime.EphemeralHelperSolveToolName {
			t.Fatalf("ordinary solve should not auto-run helper phase: %+v", phase)
		}
	}
}

func TestLongCoTChildVerifyPhasesSkipGeneralHelper(t *testing.T) {
	t.Parallel()

	phases := longCoTChildPhasesForTask(rlm.Task{
		Prompt: "BRAID node n_verify (verify)\nDependency summaries:\n- n_solve: status: solved",
	}, rlmruntime.SandboxKindPython, true)
	if got := fmt.Sprint(phaseNames(phases)); got != "[child_verify_scratch child_verify_final]" {
		t.Fatalf("phases=%s", got)
	}
	for _, phase := range phases {
		if len(phase.AutoExecuteToolCalls) > 0 && phase.AutoExecuteToolCalls[0].Tool == rlmruntime.EphemeralHelperSolveToolName {
			t.Fatalf("verify should not auto-run helper phase: %+v", phase)
		}
	}
}

func TestInflateLongCoTBraidChildPhaseBudgets(t *testing.T) {
	t.Parallel()

	phases := inflateLongCoTBraidChildPhaseBudgets(longCoTChildPhasesForTask(rlm.Task{
		Prompt: "BRAID node n_verify (verify)\nDependency summaries:\n- n_solve: status: solved",
	}, rlmruntime.SandboxKindPython, false))
	if got := phases[0].MaxTokens; got != longCoTBraidChildVerifyTokens {
		t.Fatalf("verify scratch max tokens=%d want %d", got, longCoTBraidChildVerifyTokens)
	}
	if got := phases[0].FilterREPLCodeMaxTokens; got != longCoTBraidChildFilterTokens {
		t.Fatalf("verify scratch filter max tokens=%d want %d", got, longCoTBraidChildFilterTokens)
	}
	if got := phases[1].MaxTokens; got != longCoTBraidChildMaxTokens {
		t.Fatalf("verify final max tokens=%d want %d", got, longCoTBraidChildMaxTokens)
	}
}

func TestLongCoTREPLRunnerConfigNoSubcallsDisablesAsyncRecursion(t *testing.T) {
	t.Parallel()

	condition := longcoteval.Condition{
		ID:            longcoteval.ConditionRLMReplNoSubcalls,
		Kind:          longcoteval.ConditionKindRLM,
		MaxIterations: 8,
	}
	cfg := longCoTREPLRunnerConfig(
		longcoteval.Question{},
		condition,
		longCoTLiveTarget{Provider: "openai", Model: "gpt-5"},
		longCoTHelperRuntime{Target: longCoTLiveTarget{Provider: "openai", Model: "gpt-5"}},
		30*time.Second,
		8,
		t.TempDir(),
		rlmruntime.SandboxKindPython,
		false,
		false,
		false,
		true,
		false,
	)
	if cfg.AsyncRecursion {
		t.Fatal("expected no-subcalls condition to disable async recursion")
	}
}

func TestLongCoTREPLRunnerConfigGeneralHelperUsesSingleHelperPhase(t *testing.T) {
	t.Parallel()

	cfg := longCoTREPLRunnerConfig(
		longcoteval.Question{PromptText: "Return solution = helper."},
		longcoteval.Condition{
			ID:            longcoteval.ConditionRLMReplNoSubcalls,
			Kind:          longcoteval.ConditionKindRLM,
			MaxIterations: 8,
		},
		longCoTLiveTarget{Provider: "openai", Model: "gpt-5"},
		longCoTHelperRuntime{Target: longCoTLiveTarget{Provider: "openai", Model: "gpt-5-helper"}, Attempts: 2, Timeout: 5 * time.Second, MaxTokens: 777},
		30*time.Second,
		8,
		t.TempDir(),
		rlmruntime.SandboxKindPython,
		true,
		true,
		true,
		false,
		false,
	)
	if cfg.HelperFactory == nil {
		t.Fatal("expected helper factory config")
	}
	if cfg.HelperFactory.LLM.Model != "gpt-5-helper" {
		t.Fatalf("helper model=%q", cfg.HelperFactory.LLM.Model)
	}
	if cfg.HelperFactory.Attempts != 2 {
		t.Fatalf("helper attempts=%d", cfg.HelperFactory.Attempts)
	}
	if cfg.HelperFactory.LLM.Timeout != 5*time.Second {
		t.Fatalf("helper timeout=%s", cfg.HelperFactory.LLM.Timeout)
	}
	if cfg.HelperFactory.LLM.MaxTokens != 777 {
		t.Fatalf("helper max tokens=%d", cfg.HelperFactory.LLM.MaxTokens)
	}
	if cfg.HelperFactory.PresetName != "" {
		t.Fatalf("unexpected helper preset=%q", cfg.HelperFactory.PresetName)
	}
	if cfg.HelperFactory.MaxSourceLines != 120 {
		t.Fatalf("helper max source lines=%d want 120", cfg.HelperFactory.MaxSourceLines)
	}
	if cfg.HelperFactory.MaxSourceChars != 3200 {
		t.Fatalf("helper max source chars=%d want 3200", cfg.HelperFactory.MaxSourceChars)
	}
	if len(cfg.Phases) != 1 {
		t.Fatalf("phases=%d want 1", len(cfg.Phases))
	}
	if cfg.Phases[0].Name != "helper-solve" {
		t.Fatalf("phase0=%q", cfg.Phases[0].Name)
	}
	if got := cfg.Phases[0].RequiredTools; len(got) != 1 || got[0] != rlmruntime.EphemeralHelperSolveToolName {
		t.Fatalf("required tools=%v", got)
	}
	if !cfg.Phases[0].AutoExecuteRequiredTool || !cfg.Phases[0].RequireToolResultOK {
		t.Fatalf("helper phase enforcement = auto:%v ok:%v", cfg.Phases[0].AutoExecuteRequiredTool, cfg.Phases[0].RequireToolResultOK)
	}
}

func TestLongCoTREPLRunnerConfigGeneralHelperDoesNotUseBlocksWorldPreset(t *testing.T) {
	t.Parallel()

	question := longcoteval.Question{
		Template:   "BlocksWorld",
		PromptText: "Initial state: [[1],[2],[]]\nGoal state: [[1,2],[],[]]",
	}
	cfg := longCoTREPLRunnerConfig(
		question,
		longcoteval.Condition{
			ID:            longcoteval.ConditionRLMReplNoSubcalls,
			Kind:          longcoteval.ConditionKindRLM,
			MaxIterations: 8,
		},
		longCoTLiveTarget{Provider: "openai", Model: "gpt-5"},
		longCoTHelperRuntime{Target: longCoTLiveTarget{Provider: "openai", Model: "gpt-5-helper"}, Attempts: 1},
		30*time.Second,
		8,
		t.TempDir(),
		rlmruntime.SandboxKindPython,
		true,
		true,
		true,
		false,
		false,
	)
	if cfg.HelperFactory == nil {
		t.Fatal("expected helper factory config")
	}
	if cfg.HelperFactory.PresetName != "" || strings.TrimSpace(cfg.HelperFactory.PresetSource) != "" || len(cfg.HelperFactory.PresetInput) != 0 {
		t.Fatalf("unexpected preset name=%q source=%q input=%#v", cfg.HelperFactory.PresetName, cfg.HelperFactory.PresetSource, cfg.HelperFactory.PresetInput)
	}
}

func TestLongCoTREPLRunnerConfigGeneralHelperDoesNotUseDungeonPreset(t *testing.T) {
	t.Parallel()

	const prompt = `You are given a dungeon.
Grid layout: [[-2,-3,3],[-5,-10,1],[10,30,-5]]
Return solution = <integer> for the minimum initial health needed to move only right or down while health stays > 0.`

	question := longcoteval.Question{
		ID:         "logic_medium_1",
		Template:   "Dungeon",
		PromptText: prompt,
	}
	cfg := longCoTREPLRunnerConfig(
		question,
		longcoteval.Condition{
			ID:            longcoteval.ConditionRLMReplNoSubcalls,
			Kind:          longcoteval.ConditionKindRLM,
			MaxIterations: 8,
		},
		longCoTLiveTarget{Provider: "openai", Model: "gpt-5"},
		longCoTHelperRuntime{Target: longCoTLiveTarget{Provider: "openai", Model: "gpt-5-helper"}, Attempts: 1},
		30*time.Second,
		8,
		t.TempDir(),
		rlmruntime.SandboxKindPython,
		true,
		true,
		true,
		false,
		false,
	)
	if cfg.HelperFactory == nil {
		t.Fatal("expected helper factory config")
	}
	if cfg.HelperFactory.PresetName != "" || strings.TrimSpace(cfg.HelperFactory.PresetSource) != "" || len(cfg.HelperFactory.PresetInput) != 0 {
		t.Fatalf("unexpected preset name=%q source=%q input=%#v", cfg.HelperFactory.PresetName, cfg.HelperFactory.PresetSource, cfg.HelperFactory.PresetInput)
	}
}

func TestLongCoTIsSummaryRewriteTask(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		task rlm.Task
		want bool
	}{
		{name: "run id suffix", task: rlm.Task{RunID: "attempt-1-summary"}, want: true},
		{name: "agent suffix", task: rlm.Task{AgentID: "eval/root-1/summary"}, want: true},
		{name: "normal child", task: rlm.Task{RunID: "attempt-1", AgentID: "eval/root-1"}, want: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := longCoTIsSummaryRewriteTask(tt.task); got != tt.want {
				t.Fatalf("longCoTIsSummaryRewriteTask()=%v want %v", got, tt.want)
			}
		})
	}
}

func TestLongCoTQwenNoThinkConfig(t *testing.T) {
	t.Parallel()

	cfg := longCoTLLMConfigFromTarget(
		longCoTLiveTarget{Provider: "openrouter", Model: "qwen/qwen3.6-plus"},
		longcoteval.Condition{MaxTokens: 123, Temperature: 0.2},
		10*time.Second,
		3,
	)
	if !cfg.QwenNoThink {
		t.Fatal("expected qwen no-think profile")
	}
	reasoning, ok := cfg.ExtraBody["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("reasoning missing/wrong type: %#v", cfg.ExtraBody["reasoning"])
	}
	if reasoning["effort"] != "none" || reasoning["exclude"] != true {
		t.Fatalf("reasoning=%#v", reasoning)
	}
	if cfg.ExtraBody["parallel_tool_calls"] != false {
		t.Fatalf("parallel_tool_calls=%#v want false", cfg.ExtraBody["parallel_tool_calls"])
	}

	local := longCoTLLMConfigFromTarget(
		longCoTLiveTarget{Provider: "lmstudio", Model: "qwen3.6-27b"},
		longcoteval.Condition{},
		10*time.Second,
		3,
	)
	if local.ExtraBody["enable_thinking"] != false {
		t.Fatalf("enable_thinking=%#v want false", local.ExtraBody["enable_thinking"])
	}
	kwargs, ok := local.ExtraBody["chat_template_kwargs"].(map[string]any)
	if !ok {
		t.Fatalf("chat_template_kwargs missing/wrong type: %#v", local.ExtraBody["chat_template_kwargs"])
	}
	if kwargs["enable_thinking"] != false {
		t.Fatalf("chat_template_kwargs.enable_thinking=%#v want false", kwargs["enable_thinking"])
	}
}

func TestLongCoTDeepSeekDisablesThinking(t *testing.T) {
	t.Parallel()

	cfg := longCoTLLMConfigFromTarget(
		longCoTLiveTarget{Provider: "openrouter", Model: "deepseek/deepseek-v4-flash"},
		longcoteval.Condition{MaxTokens: 123, Temperature: 0.2},
		10*time.Second,
		3,
	)
	thinking, ok := cfg.ExtraBody["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking missing/wrong type: %#v", cfg.ExtraBody["thinking"])
	}
	if thinking["type"] != "disabled" {
		t.Fatalf("thinking=%#v want disabled", thinking)
	}
}

func TestLongCoTChildMaxTokensCapsQwen(t *testing.T) {
	t.Parallel()

	if got := longCoTChildMaxTokens(rlm.LLMConfig{Model: "qwen/qwen3.6-plus", MaxTokens: 4096}); got != 2048 {
		t.Fatalf("qwen child max tokens=%d want 2048", got)
	}
	if got := longCoTChildMaxTokens(rlm.LLMConfig{Model: "qwen/qwen3.6-plus", MaxTokens: 512}); got != 512 {
		t.Fatalf("qwen child max tokens=%d want 512", got)
	}
	if got := longCoTChildMaxTokens(rlm.LLMConfig{Model: "anthropic/claude", MaxTokens: 4096}); got != 4096 {
		t.Fatalf("non-qwen child max tokens=%d want 4096", got)
	}
}

func TestBuildLongCoTOfficialBaselinePrompt(t *testing.T) {
	t.Parallel()

	official := "  You are being tested. Return the answer as solution = <value>.  "
	prompt := buildLongCoTOfficialBaselinePrompt(official)
	if prompt != strings.TrimSpace(official) {
		t.Fatalf("baseline prompt=%q want official prompt preserved", prompt)
	}
}

func TestShouldReviewLongCoTAttempt(t *testing.T) {
	t.Parallel()

	condition := longcoteval.Condition{Kind: longcoteval.ConditionKindRLM}
	if !shouldReviewLongCoTAttempt(longcoteval.Question{Difficulty: "hard"}, condition, longcoteval.AttemptStatusOK, "auto") {
		t.Fatal("expected auto review for hard RLM question")
	}
	if !shouldReviewLongCoTAttempt(longcoteval.Question{RLMReview: true}, condition, longcoteval.AttemptStatusOK, "auto") {
		t.Fatal("expected auto review for marked RLM question")
	}
	if !shouldReviewLongCoTAttempt(longcoteval.Question{RLMReviewRecursive: true}, condition, longcoteval.AttemptStatusOK, "auto") {
		t.Fatal("expected auto review for recursive-review question")
	}
	if shouldReviewLongCoTAttempt(longcoteval.Question{Difficulty: "easy"}, condition, longcoteval.AttemptStatusOK, "auto") {
		t.Fatal("did not expect auto review for unmarked easy question")
	}
	if shouldReviewLongCoTAttempt(longcoteval.Question{Difficulty: "hard"}, condition, longcoteval.AttemptStatusError, "always") {
		t.Fatal("did not expect review for failed attempt")
	}
	if shouldReviewLongCoTAttempt(longcoteval.Question{Difficulty: "hard"}, longcoteval.Condition{Kind: longcoteval.ConditionKindBaseline}, longcoteval.AttemptStatusOK, "always") {
		t.Fatal("did not expect review for baseline attempt")
	}
}

func phaseNames(phases []rlmruntime.REPLRunnerPhase) []string {
	out := make([]string, 0, len(phases))
	for _, phase := range phases {
		out = append(out, phase.Name)
	}
	return out
}

func TestNormalizeLongCoTReviewMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "default", raw: "", want: "off"},
		{name: "off", raw: "never", want: "off"},
		{name: "auto", raw: " AUTO ", want: "auto"},
		{name: "always", raw: "on", want: "always"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeLongCoTReviewMode(tt.raw)
			if err != nil {
				t.Fatalf("normalize returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("mode=%q want %q", got, tt.want)
			}
		})
	}

	if _, err := normalizeLongCoTReviewMode("sometimes"); err == nil {
		t.Fatal("expected invalid mode to fail")
	}
}

func TestEvalLongCoTRejectsUnsupportedHelperLanguage(t *testing.T) {
	t.Parallel()

	env, err := runEvalLongCoTForTest(t, "--dry-run", "--helper-language", "ruby")
	if err == nil {
		t.Fatal("expected unsupported helper language error")
	}
	if env.Status != envelope.StatusError {
		t.Fatalf("status=%q want error", env.Status)
	}
	if !strings.Contains(err.Error(), `unsupported --helper-language "ruby" (allowed: go, python)`) {
		t.Fatalf("error=%v", err)
	}
}

func TestEvalLongCoTRejectsUnsupportedSandbox(t *testing.T) {
	t.Parallel()

	env, err := runEvalLongCoTForTest(t, "--dry-run", "--sandbox", "lua")
	if err == nil {
		t.Fatal("expected unsupported sandbox error")
	}
	if env.Status != envelope.StatusError {
		t.Fatalf("status=%q want error", env.Status)
	}
	if !strings.Contains(err.Error(), `unsupported --sandbox "lua" (allowed: python, smolvm, yaegi)`) {
		t.Fatalf("error=%v", err)
	}
}

func TestNormalizeLongCoTHelperFlags(t *testing.T) {
	t.Parallel()

	ephemeral, helper := normalizeLongCoTHelperFlags(false, false, true)
	if !ephemeral || !helper {
		t.Fatalf("require-ephemeral expected true/true, got ephemeral=%v helper=%v", ephemeral, helper)
	}

	ephemeral, helper = normalizeLongCoTHelperFlags(false, true, false)
	if !ephemeral || !helper {
		t.Fatalf("general-helper expected true/true, got ephemeral=%v helper=%v", ephemeral, helper)
	}
}

func TestNormalizeLongCoTReviewInputsRejectsInvalidIterations(t *testing.T) {
	t.Parallel()

	_, _, _, err := normalizeLongCoTReviewInputs("markdown", "auto", 0, false, 2, 2, 2000, 900, true, 2)
	if err == nil {
		t.Fatal("expected invalid review iterations to fail")
	}
	if !strings.Contains(err.Error(), "--rlm-review-iterations must be >= 1") {
		t.Fatalf("error=%v", err)
	}
}

func TestValidateLongCoTRunNumericFlags(t *testing.T) {
	t.Parallel()

	if err := validateLongCoTRunNumericFlags(0, 0, 0, 0, 0, 0, 0); err != nil {
		t.Fatalf("expected zero values to pass, got %v", err)
	}
	if err := validateLongCoTRunNumericFlags(-1, 0, 0, 0, 0, 0, 0); err == nil || !strings.Contains(err.Error(), "--limit must be >= 0") {
		t.Fatalf("expected --limit validation error, got %v", err)
	}
	if err := validateLongCoTRunNumericFlags(0, 0, -1*time.Second, 0, 0, 0, 0); err == nil || !strings.Contains(err.Error(), "--timeout must be >= 0") {
		t.Fatalf("expected --timeout validation error, got %v", err)
	}
}

func TestNormalizeLongCoTQuestionFilterRejectsUnsupportedDifficulty(t *testing.T) {
	t.Parallel()

	_, err := normalizeLongCoTQuestionFilter("eval", []string{"math"}, "expert", 5, 7)
	if err == nil {
		t.Fatal("expected unsupported difficulty to fail")
	}
	if !strings.Contains(err.Error(), `unsupported --difficulty "expert"`) {
		t.Fatalf("error=%v", err)
	}
}

func TestLongCoTReviewConfigForQuestion(t *testing.T) {
	t.Parallel()

	cfg, err := newLongCoTReviewConfig("auto", 3, false, 2, 2, 2000, 900, true, 2)
	if err != nil {
		t.Fatalf("new review config: %v", err)
	}
	if cfg.Recursive {
		t.Fatal("expected base config to be non-recursive")
	}
	got := longCoTReviewConfigForQuestion(longcoteval.Question{RLMReviewRecursive: true}, cfg)
	if !got.Recursive || got.MaxDepth != 2 || got.MaxSubcalls != 2 {
		t.Fatalf("question recursive config = %+v", got)
	}
	if _, err := newLongCoTReviewConfig("auto", 3, true, 1, 2, 2000, 900, true, 2); err == nil {
		t.Fatal("expected recursive config with depth < 2 to fail")
	}
	if _, err := newLongCoTReviewConfig("auto", 3, true, 2, 0, 2000, 900, true, 2); err == nil {
		t.Fatal("expected recursive config with subcalls < 1 to fail")
	}
	if _, err := newLongCoTReviewConfig("auto", 3, false, 2, 2, -1, 900, true, 2); err == nil {
		t.Fatal("expected negative candidate cap to fail")
	}
	if _, err := newLongCoTReviewConfig("auto", 3, false, 2, 2, 2000, -1, true, 2); err == nil {
		t.Fatal("expected negative child summary cap to fail")
	}
	if _, err := newLongCoTReviewConfig("auto", 3, false, 2, 2, 2000, 900, true, 0); err == nil {
		t.Fatal("expected rewrite iterations < 1 to fail")
	}
}

func TestBuildLongCoTReviewPrompt(t *testing.T) {
	t.Parallel()

	prompt := buildLongCoTReviewPrompt("Return solution = 4.", "solution = <value>", rlmruntime.SandboxKindPython, longCoTReviewConfig{})
	for _, want := range []string{
		"LongCoT RLM review pass",
		"official_prompt",
		"candidate_answer",
		"Do not use answer keys",
		"output only a corrected final answer",
		"Return solution = 4.",
		"solution = <value>",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildLongCoTReviewPromptRecursive(t *testing.T) {
	t.Parallel()

	prompt := buildLongCoTReviewPrompt(
		"Return solution = 4.",
		"solution = <value>",
		rlmruntime.SandboxKindPython,
		longCoTReviewConfig{Recursive: true, MaxDepth: 2, MaxSubcalls: 2},
	)
	for _, want := range []string{
		"Recursive review contract",
		"rlm_query",
		"rlm_wait({})",
		"max_depth=2",
		"max_subcalls=2",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("recursive prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "or recursive child calls") {
		t.Fatalf("recursive prompt still forbids child calls:\n%s", prompt)
	}
}

func TestCleanLongCoTReviewResponse(t *testing.T) {
	t.Parallel()

	got := cleanLongCoTReviewResponse("Some review text.\n\nFinal answer: solution = move A to B")
	if got != "solution = move A to B" {
		t.Fatalf("cleaned=%q", got)
	}
	got = cleanLongCoTReviewResponse("Reasoning\nsolution = 42")
	if got != "solution = 42" {
		t.Fatalf("cleaned solution line=%q", got)
	}
}

func TestSanitizeLongCoTResponseTextRemovesChannelArtifacts(t *testing.T) {
	t.Parallel()

	got, info := sanitizeLongCoTResponseText("<|channel>thought\n<channel|>solution = block A is on block B")
	if got != "solution = block A is on block B" {
		t.Fatalf("sanitized=%q", got)
	}
	if !info.Changed {
		t.Fatal("expected sanitization to report changed output")
	}
	want := []string{"reasoning_channel_open_thought", "reasoning_channel_close"}
	if fmt.Sprint(info.Artifacts) != fmt.Sprint(want) {
		t.Fatalf("artifacts=%v want %v", info.Artifacts, want)
	}

	got, info = sanitizeLongCoTResponseText("<|channel>}\n<channel|>solution = move A to B")
	if got != "solution = move A to B" {
		t.Fatalf("malformed channel sanitized=%q", got)
	}
	if !info.Changed {
		t.Fatal("expected malformed channel marker to be detected")
	}

	got, info = sanitizeLongCoTResponseText(`<|tool_call>call:python_repl:python_repl(code="x")<tool_call|>`)
	if got != "" {
		t.Fatalf("tool-call-only sanitized=%q want empty", got)
	}
	if !info.Changed || fmt.Sprint(info.Artifacts) != "[tool_call_markup_open tool_call_markup_close]" {
		t.Fatalf("tool-call artifact info=%+v", info)
	}
}

func TestEnforceLongCoTOutputSanitizationStoresMetadata(t *testing.T) {
	t.Parallel()

	attempt := longcoteval.Attempt{
		ResponseText: "<|channel>thought\n<channel|>solution = block A is on block B",
		RLM:          &longcoteval.RLMAttemptMeta{Metadata: map[string]any{}},
	}
	info := enforceLongCoTOutputSanitization(&attempt)
	if !info.Changed {
		t.Fatal("expected sanitization")
	}
	if attempt.ResponseText != "solution = block A is on block B" {
		t.Fatalf("response=%q", attempt.ResponseText)
	}
	meta, ok := attempt.RLM.Metadata["output_sanitization"].(map[string]any)
	if !ok {
		t.Fatalf("missing output_sanitization metadata: %#v", attempt.RLM.Metadata)
	}
	if meta["raw_text"] == "" {
		t.Fatalf("raw_text not preserved: %#v", meta)
	}
}

func TestApplyLongCoTReviewOutcomePreservesPreReviewSanitization(t *testing.T) {
	t.Parallel()

	attempt := longcoteval.Attempt{
		ResponseText: "candidate",
		RLM: &longcoteval.RLMAttemptMeta{Metadata: map[string]any{
			"output_sanitization": map[string]any{"changed": true},
		}},
	}
	applyLongCoTReviewOutcome(&attempt, longCoTLiveAttemptOutcome{
		ResponseText: "reviewed",
		RLM:          &longcoteval.RLMAttemptMeta{Metadata: map[string]any{"review_raw_response_text": "reviewed"}},
	})
	if attempt.ResponseText != "reviewed" {
		t.Fatalf("response=%q", attempt.ResponseText)
	}
	if _, ok := attempt.RLM.Metadata["output_sanitization"]; ok {
		t.Fatalf("stale output_sanitization remained: %#v", attempt.RLM.Metadata)
	}
	if _, ok := attempt.RLM.Metadata["pre_review_output_sanitization"]; !ok {
		t.Fatalf("missing pre_review_output_sanitization: %#v", attempt.RLM.Metadata)
	}
}

func TestCompactLongCoTReviewCandidatePreservesHeadAndTail(t *testing.T) {
	t.Parallel()

	raw := strings.Repeat("a", 80) + " solution = 42"
	got, info := compactLongCoTReviewCandidate(raw, 64)
	if !info.Changed {
		t.Fatal("expected candidate compaction")
	}
	if info.RawChars != len([]rune(raw)) || info.CompactChars > 64 || info.MaxChars != 64 {
		t.Fatalf("compaction info = %+v", info)
	}
	if !strings.Contains(got, "[candidate truncated]") {
		t.Fatalf("missing truncation marker: %q", got)
	}
	if !strings.Contains(got, "solution = 42") {
		t.Fatalf("tail final answer not preserved: %q", got)
	}
}

func TestBuildLongCoTRLMTaskPrompt(t *testing.T) {
	t.Parallel()

	prompt := buildLongCoTRLMTaskPrompt("What is 2 + 2?", longcoteval.Condition{ID: longcoteval.ConditionRLMNoToolsSingle})
	for _, want := range []string{
		"LongCoT internal eval condition: rlm_no_tools_single",
		"No external tools are available in this condition.",
		"Follow the prompt exactly, including its required answer format.",
		"What is 2 + 2?",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildLongCoTREPLTaskPrompt(t *testing.T) {
	t.Parallel()

	prompt := buildLongCoTREPLTaskPrompt("Return solution = 4.", longcoteval.Condition{ID: longcoteval.ConditionRLMReplNoSubcalls}, rlmruntime.SandboxKindPython)
	for _, want := range []string{
		"LongCoT internal eval condition: rlm_repl_no_subcalls",
		"first call python_repl",
		"official_prompt",
		"persistent Python REPL",
		"No recursive child-query tool is available",
		"expected, not an environment failure",
		"Never return the placeholder itself",
		"Official task text begins:",
		"Return solution = 4.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildLongCoTREPLTaskPromptRecursive(t *testing.T) {
	t.Parallel()

	prompt := buildLongCoTREPLTaskPrompt("Return solution = 4.", longcoteval.Condition{
		ID:          longcoteval.ConditionRLMReplRecursive,
		MaxSubcalls: 2,
	}, rlmruntime.SandboxKindPython)
	for _, want := range []string{
		"LongCoT internal eval condition: rlm_repl_recursive",
		"first call python_repl",
		"official_prompt",
		"persistent Python REPL",
		"rlm_query submits child solves",
		"runtime enforces that shape",
		"Runtime-enforced recursive solve order",
		"Use rlm_wait({}) after submitted child work",
		"Do not invent or pass child IDs",
		"Official task text begins:",
		"Return solution = 4.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildLongCoTREPLTaskPromptYaegi(t *testing.T) {
	t.Parallel()

	prompt := buildLongCoTREPLTaskPrompt("Return solution = 4.", longcoteval.Condition{
		ID:          longcoteval.ConditionRLMReplRecursive,
		MaxSubcalls: 2,
	}, rlmruntime.SandboxKindYaegi)
	for _, want := range []string{
		"first call go_repl",
		"persistent Go REPL",
		"Use rlm_wait({}) after submitted child work",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildLongCoTREPLTaskPromptBlocksWorldMentionsHelper(t *testing.T) {
	t.Parallel()

	prompt := buildLongCoTREPLTaskPromptForQuestion(longcoteval.Question{
		Template:   "BlocksWorld",
		PromptText: "Return solution = <value>. Move block A onto block B.",
	}, longcoteval.Condition{ID: longcoteval.ConditionRLMReplNoSubcalls}, rlmruntime.SandboxKindPython, false, false, false, true)
	for _, want := range []string{
		"blocksworld_solve",
		"canonical action answer format",
		"use its answer_format exactly",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildLongCoTREPLTaskPromptCanDisableBlocksWorldHelper(t *testing.T) {
	t.Parallel()

	prompt := buildLongCoTREPLTaskPromptForQuestion(longcoteval.Question{
		Template:   "BlocksWorld",
		PromptText: "Return solution = <value>. Move block A onto block B.",
	}, longcoteval.Condition{ID: longcoteval.ConditionRLMReplNoSubcalls}, rlmruntime.SandboxKindPython, true, false, false, false)
	if strings.Contains(prompt, "blocksworld_solve") {
		t.Fatalf("prompt unexpectedly mentions blocksworld helper:\n%s", prompt)
	}
	if !strings.Contains(prompt, rlmruntime.EphemeralHelperSolveToolName) {
		t.Fatalf("prompt missing helper solve instructions:\n%s", prompt)
	}
	for _, forbidden := range []string{"ephemeral_skill_draft", "ephemeral_skill_run"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("prompt unexpectedly contains legacy helper tool %q:\n%s", forbidden, prompt)
		}
	}
}

func TestBuildLongCoTREPLTaskPromptMentionsGeneralHelper(t *testing.T) {
	t.Parallel()

	prompt := buildLongCoTREPLTaskPromptForQuestion(longcoteval.Question{
		PromptText: "Return solution = <value>.",
	}, longcoteval.Condition{ID: longcoteval.ConditionRLMReplNoSubcalls}, rlmruntime.SandboxKindPython, true, true, true, false)
	for _, want := range []string{
		rlmruntime.EphemeralHelperSolveToolName,
		"runtime owns helper synthesis, validation, repair, execution",
		"first call ephemeral_helper_solve",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, forbidden := range []string{
		"first call python_repl",
		"persistent Python REPL",
	} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("prompt unexpectedly contains %q:\n%s", forbidden, prompt)
		}
	}
}

func TestLongCoTQuestionIsBlocksWorldOfficialShape(t *testing.T) {
	t.Parallel()

	if !longCoTQuestionIsBlocksWorld(longcoteval.Question{ID: "BlocksWorld_easy_1"}) {
		t.Fatal("question id prefix should identify BlocksWorld")
	}
	if !longCoTQuestionIsBlocksWorld(longcoteval.Question{
		PromptText: "Initial state: [[0], []]\nGoal state: [[], [0]]\nNumber of blocks: 1\nNumber of stacks: 2",
	}) {
		t.Fatal("official prompt shape should identify BlocksWorld")
	}
}

func TestLongCoTBlocksWorldSolveTool(t *testing.T) {
	t.Parallel()

	executor := longCoTBlocksWorldToolExecutor{
		Prompt: "/no_think\nReturn solution = <value>. Move block A onto block B.",
	}
	raw, err := executor.Execute(t.Context(), longCoTBlocksWorldSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if got["ok"] != true {
		t.Fatalf("ok=%v want true; raw=%s", got["ok"], raw)
	}
	if got["solution"] != "move A to B" {
		t.Fatalf("solution=%v want move A to B", got["solution"])
	}
	if got["answer_format"] != "solution = move A to B" {
		t.Fatalf("answer_format=%v", got["answer_format"])
	}
	output, ok := got["output"].(string)
	if !ok {
		t.Fatalf("output missing or not string: %#v", got["output"])
	}
	for _, want := range []string{"RLM_CHECK_JSON=", "RLM_ANSWER_JSON=", "solution = move A to B"} {
		if !strings.Contains(output, want) {
			t.Fatalf("structured output missing %q:\n%s", want, output)
		}
	}
}

func TestLongCoTBlocksWorldSolveToolPlansStateGoalProblem(t *testing.T) {
	t.Parallel()

	executor := longCoTBlocksWorldToolExecutor{
		Prompt: "Initial state: block A is on block B. block B is on the table. block C is on the table. Goal: block B is on block C.",
	}
	raw, err := executor.Execute(t.Context(), longCoTBlocksWorldSolveToolName, json.RawMessage(`{"max_depth":4}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var got struct {
		OK             bool              `json:"ok"`
		Solution       string            `json:"solution"`
		Plan           []string          `json:"plan"`
		InitialState   map[string]string `json:"initial_state"`
		GoalState      map[string]string `json:"goal_state"`
		FinalState     map[string]string `json:"final_state"`
		AnswerFormat   string            `json:"answer_format"`
		ToolError      string            `json:"error"`
		ToolConfidence string            `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if !got.OK {
		t.Fatalf("ok=false error=%q raw=%s", got.ToolError, raw)
	}
	want := "move A to table; move B to C"
	if got.Solution != want {
		t.Fatalf("solution=%q want %q; raw=%s", got.Solution, want, raw)
	}
	if got.AnswerFormat != "solution = "+want {
		t.Fatalf("answer_format=%q", got.AnswerFormat)
	}
	if got.FinalState["B"] != "C" {
		t.Fatalf("final_state=%v want B on C", got.FinalState)
	}
	if got.InitialState["A"] != "B" || got.GoalState["B"] != "C" {
		t.Fatalf("parsed states unexpected: initial=%v goal=%v", got.InitialState, got.GoalState)
	}
}

func TestLongCoTBlocksWorldSolveToolPlansStackProblem(t *testing.T) {
	t.Parallel()

	executor := longCoTBlocksWorldToolExecutor{
		Prompt: `Initial state: [[0], [1, 2], []]
Goal state: [[], [1], [2, 0]]
Number of blocks: 3
Number of stacks: 3`,
	}
	raw, err := executor.Execute(t.Context(), longCoTBlocksWorldSolveToolName, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var got struct {
		OK           bool    `json:"ok"`
		Solution     string  `json:"solution"`
		Moves        [][]int `json:"moves"`
		AnswerFormat string  `json:"answer_format"`
		ToolError    string  `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if !got.OK {
		t.Fatalf("ok=false error=%q raw=%s", got.ToolError, raw)
	}
	wantMoves := [][]int{{2, 1, 2}, {0, 0, 2}}
	if !equalIntStacks(got.Moves, wantMoves) {
		t.Fatalf("moves=%v want %v; raw=%s", got.Moves, wantMoves, raw)
	}
	if got.Solution != "[[2,1,2],[0,0,2]]" {
		t.Fatalf("solution=%q", got.Solution)
	}
	if got.AnswerFormat != "solution = [[2,1,2],[0,0,2]]" {
		t.Fatalf("answer_format=%q", got.AnswerFormat)
	}
}

func TestLongCoTBlocksWorldStackParserSkipsTemplateExample(t *testing.T) {
	t.Parallel()

	prompt := `You will be provided with a problem instance, given in the form:
Initial state: [stack0, stack1, ..., stackk]
Goal state: [stack0, stack1, ..., stackk]

Example:
Initial state: [[9], [], []]
Goal state: [[], [9], []]

Puzzle instance:

Initial state: [[0], [1, 2], []]
Goal state: [[], [1], [2, 0]]
Number of blocks: 3
Number of stacks: 3`
	result := solveLongCoTBlocksWorldPrompt(prompt, 0)
	if !result.OK {
		t.Fatalf("solve failed: %#v", result)
	}
	if result.Solution != "[[2,1,2],[0,0,2]]" {
		t.Fatalf("solution=%q", result.Solution)
	}
}

func TestLongCoTBlocksWorldFinalResponse(t *testing.T) {
	t.Parallel()

	got, ok := longCoTBlocksWorldFinalResponse(longcoteval.Question{
		Template: "BlocksWorld",
		PromptText: `Initial state: [[0], [1, 2], []]
Goal state: [[], [1], [2, 0]]`,
	})
	if !ok {
		t.Fatal("final response unavailable")
	}
	if got != "solution = [[2,1,2],[0,0,2]]" {
		t.Fatalf("response=%q", got)
	}
}

func TestLongCoTBlocksWorldSolveUnsupported(t *testing.T) {
	t.Parallel()

	result := solveLongCoTBlocksWorldPrompt("Return solution = <value>. Describe the color of block A.", 0)
	if result.OK {
		t.Fatalf("OK=true want unsupported: %#v", result)
	}
	if !strings.Contains(result.Error, "unsupported") {
		t.Fatalf("error=%q", result.Error)
	}
}

func TestLongCoTRLMExecutionSettingsRejectsStagedUntilRouteExists(t *testing.T) {
	t.Parallel()

	condition := longcoteval.Condition{
		ID:              longcoteval.ConditionRLMNoToolsStaged,
		Kind:            longcoteval.ConditionKindRLM,
		RLMRouteProfile: "longcot_no_tools_staged",
		RLMPlanMode:     "staged",
		RLMToolProfile:  rlmenv.ToolProfileLongCoTNoModelTools,
		MaxIterations:   2,
		MaxSubcalls:     0,
	}
	_, _, err := longCoTRLMExecutionSettings(condition)
	if err == nil || !strings.Contains(err.Error(), "currently skipped in live mode") {
		t.Fatalf("expected staged route error, got %v", err)
	}
}

func TestRunLongCoTRLMAttemptTreatsStagedAsUnsupported(t *testing.T) {
	t.Parallel()

	outcome, err := runLongCoTRLMAttempt(
		t.Context(),
		configpkg.Config{},
		t.TempDir(),
		nil,
		longcoteval.Question{PromptText: "Return solution = 4."},
		longcoteval.Condition{
			ID:              longcoteval.ConditionRLMNoToolsStaged,
			Kind:            longcoteval.ConditionKindRLM,
			RLMRouteProfile: "longcot_no_tools_staged",
			RLMPlanMode:     "staged",
			RLMToolProfile:  rlmenv.ToolProfileLongCoTNoModelTools,
			MaxIterations:   2,
			MaxSubcalls:     0,
		},
		longCoTLiveTarget{Provider: "lmstudio", Model: "test-model"},
	)
	if err != nil {
		t.Fatalf("runLongCoTRLMAttempt() error = %v", err)
	}
	if outcome.Status != longCoTAttemptStatusUnsupported {
		t.Fatalf("status=%q want %q", outcome.Status, longCoTAttemptStatusUnsupported)
	}
	if !strings.Contains(strings.ToLower(outcome.Error), "skipped") {
		t.Fatalf("error=%q want skipped wording", outcome.Error)
	}
	if outcome.RLM == nil || outcome.RLM.Metadata == nil {
		t.Fatalf("missing rlm metadata: %+v", outcome.RLM)
	}
	if got, ok := outcome.RLM.Metadata["unsupported_live_condition"].(bool); !ok || !got {
		t.Fatalf("unsupported_live_condition=%v want true", outcome.RLM.Metadata["unsupported_live_condition"])
	}
}

func TestLongCoTRLMExecutionSettingsAndMapping(t *testing.T) {
	t.Parallel()

	condition := longcoteval.Condition{
		ID:              longcoteval.ConditionRLMNoToolsSingle,
		Kind:            longcoteval.ConditionKindRLM,
		RLMRouteProfile: "longcot_no_tools_single",
		RLMPlanMode:     "single",
		RLMToolProfile:  rlmenv.ToolProfileLongCoTNoModelTools,
		MaxIterations:   1,
		MaxSubcalls:     0,
	}
	route, plan, err := longCoTRLMExecutionSettings(condition)
	if err != nil {
		t.Fatalf("longCoTRLMExecutionSettings() error = %v", err)
	}
	if route != rlm.RouteProfileMixed {
		t.Fatalf("route=%q want %q", route, rlm.RouteProfileMixed)
	}
	if plan != rlm.PlanModeFree {
		t.Fatalf("plan=%q want %q", plan, rlm.PlanModeFree)
	}

	result := rlm.Result{
		Iterations:     3,
		Subcalls:       1,
		EvidenceRefs:   []string{"artifact:abc"},
		RetrievedPaths: []string{"internal/foo.go"},
		Metadata: map[string]any{
			"parent_input_tokens_total":  120,
			"parent_output_tokens_total": 48,
			"parent_total_tokens_total":  168,
			"parent_iteration_count":     4,
			"tool_names":                 []any{"search_repo", "load_file"},
			"parent_tool_usage": map[string]any{
				"target_tool_invocations": 2,
			},
			"phases": []any{
				map[string]any{
					"name":                 "discovery",
					"tool_names":           []any{"search_repo"},
					"parent_input_tokens":  10,
					"parent_output_tokens": 4,
					"parent_total_tokens":  14,
					"answer":               "found candidate files",
				},
			},
		},
	}

	usage := longCoTUsageFromRLMResult(result)
	if usage.InputTokens != 120 || usage.OutputTokens != 48 || usage.TotalTokens != 168 {
		t.Fatalf("usage=%+v", usage)
	}

	meta := longCoTRLMMetaFromResult(condition, result)
	if meta == nil {
		t.Fatal("expected RLM metadata")
	}
	if meta.ParentInputTokens != 120 || meta.ParentOutputTokens != 48 || meta.ParentTotalTokens != 168 {
		t.Fatalf("meta parent tokens=%+v", meta)
	}
	if len(meta.Phases) != 1 || meta.Phases[0].Name != "discovery" {
		t.Fatalf("meta phases=%+v", meta.Phases)
	}
}

func TestLongCoTSafeRLMEnvironmentDoesNotExposeExternalHandles(t *testing.T) {
	t.Parallel()

	env := longCoTSafeRLMEnvironment(longcoteval.Condition{
		ID:             longcoteval.ConditionRLMNoToolsSingle,
		Kind:           longcoteval.ConditionKindRLM,
		RLMToolProfile: rlmenv.ToolProfileLongCoTNoModelTools,
	})
	if len(env.Tools) != 0 {
		t.Fatalf("tools=%+v want none", env.Tools)
	}
	if len(env.RepoHandles) != 0 || len(env.VaultHandles) != 0 || len(env.ArtifactHandles) != 0 || len(env.SceneHandles) != 0 || len(env.ActiveThreadIDs) != 0 {
		t.Fatalf("environment exposes external handles: %+v", env)
	}
	if len(env.TopOfMind) != 0 || len(env.LatestHandoff) != 0 {
		t.Fatalf("environment exposes memory maps: %+v", env)
	}
}

func runEvalLongCoTForTest(t *testing.T, args ...string) (envelope.Envelope, error) {
	t.Helper()
	cmd := newEvalLongCoTCommand()
	var out bytes.Buffer
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()

	env, decodeErr := protocol.DecodeEnvelope(bytes.TrimSpace(out.Bytes()))
	if decodeErr != nil {
		t.Fatalf("DecodeEnvelope() error = %v; output=%q", decodeErr, out.String())
	}
	return env, err
}

func decodeStringAnyMap(t *testing.T, value any) map[string]any {
	t.Helper()
	out, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value type=%T want map[string]any", value)
	}
	return out
}

func decodeLongCoTRunResult(t *testing.T, value any) longcoteval.RunResult {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal(value) error = %v", err)
	}
	var result longcoteval.RunResult
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Unmarshal(RunResult) error = %v", err)
	}
	return result
}

func stringSlicesEqual(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func stringSlicesContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
