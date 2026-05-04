package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
)

func TestExtractGatherContextPathsUsesRefsAndMetadata(t *testing.T) {
	t.Parallel()

	bundle := contextengine.ContextBundle{
		Evidence: []contextengine.EvidenceNode{
			{
				ID:          "node-path",
				WorkspaceID: "ws",
				NodeType:    contextengine.EvidenceNodeTypeCode,
				Ref:         contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "./internal/context/contextengine/context_gather.go"},
			},
			{
				ID:          "node-symbol",
				WorkspaceID: "ws",
				NodeType:    contextengine.EvidenceNodeTypeCode,
				Ref:         contextengine.EvidenceRef{Type: contextengine.RefTypeSymbol, Ref: "GatherContext"},
				Metadata:    map[string]any{"path": "internal/context/contextengine/context_gather.go"},
			},
			{
				ID:          "node-second",
				WorkspaceID: "ws",
				NodeType:    contextengine.EvidenceNodeTypeCode,
				Ref:         contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: "internal/rlm/env/tool_exec.go"},
			},
		},
	}

	got := extractGatherContextPaths(bundle)
	want := []string{"internal/context/contextengine/context_gather.go", "internal/rlm/env/tool_exec.go"}
	if len(got) != len(want) {
		t.Fatalf("paths=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paths=%v want %v", got, want)
		}
	}
}

func TestSummarizeGatherContextEvalResults(t *testing.T) {
	t.Parallel()

	summary := summarizeGatherContextEvalResults([]gatherContextEvalResult{
		{Status: "ok", Passed: true, PathRecall: 1, RoleRecall: 1, FactRecall: 1, DurationMS: 10, RawContextChars: 100, EmittedContextChars: 40, FactCount: 2, EvidenceCount: 2},
		{Status: "error", Passed: false, PathRecall: 0.5, RoleRecall: 0.5, FactRecall: 0.25, DurationMS: 30, RawContextChars: 60, EmittedContextChars: 20, OmittedContextItems: 1, FactCount: 1, EvidenceCount: 1},
		{Status: "ok", StaleEval: true, PathRecall: 0, FactRecall: 0, DurationMS: 1000},
	})
	if summary.Count != 3 {
		t.Fatalf("count=%d want 3", summary.Count)
	}
	if summary.ScoredCount != 2 {
		t.Fatalf("scored_count=%d want 2", summary.ScoredCount)
	}
	if summary.StaleEvalCount != 1 {
		t.Fatalf("stale_eval_count=%d want 1", summary.StaleEvalCount)
	}
	if summary.PassRate != 0.5 {
		t.Fatalf("pass_rate=%f want 0.5", summary.PassRate)
	}
	if summary.MeanPathRecall != 0.75 {
		t.Fatalf("mean_path_recall=%f want 0.75", summary.MeanPathRecall)
	}
	if summary.MeanRoleRecall != 0.75 {
		t.Fatalf("mean_role_recall=%f want 0.75", summary.MeanRoleRecall)
	}
	if len(summary.RoleRecallByRole) != 0 {
		t.Fatalf("role_recall_by_role=%v want empty when no expected roles", summary.RoleRecallByRole)
	}
	if summary.MeanFactRecall != 0.625 {
		t.Fatalf("mean_fact_recall=%f want 0.625", summary.MeanFactRecall)
	}
	if summary.MeanDurationMS != 20 {
		t.Fatalf("mean_duration_ms=%f want 20", summary.MeanDurationMS)
	}
	if summary.MeanEmittedContextChars != 30 {
		t.Fatalf("mean_emitted_context_chars=%f want 30", summary.MeanEmittedContextChars)
	}
	if summary.ErrorCount != 1 {
		t.Fatalf("error_count=%d want 1", summary.ErrorCount)
	}
}

func TestSummarizeGatherContextEvalRoleDiagnostics(t *testing.T) {
	t.Parallel()

	summary := summarizeGatherContextEvalResults([]gatherContextEvalResult{
		{
			Status:        "ok",
			Passed:        true,
			ExpectedRoles: []string{"router", "docs"},
			MatchedRoles:  []string{"router"},
			RoleRecall:    0.5,
			RoleCoverage:  map[string]int{"router": 1, "tooling": 2, "test": 1},
		},
		{
			Status:        "ok",
			Passed:        true,
			ExpectedRoles: []string{"router", "test"},
			MatchedRoles:  []string{"router", "test"},
			RoleRecall:    1,
			RoleCoverage:  map[string]int{"router": 1, "test": 2, "generated": 1},
		},
		{
			Status:        "ok",
			StaleEval:     true,
			ExpectedRoles: []string{"docs"},
			MatchedRoles:  []string{"docs"},
			RoleCoverage:  map[string]int{"vendor": 3},
		},
	})

	if got := len(summary.RoleRecallByRole); got != 3 {
		t.Fatalf("role scores=%d want 3: %+v", got, summary.RoleRecallByRole)
	}
	wantScores := map[string]gatherContextRoleRecallScore{
		"docs":   {Role: "docs", ExpectedCases: 1, MatchedCases: 0, MissingCases: 1, Recall: 0},
		"router": {Role: "router", ExpectedCases: 2, MatchedCases: 2, MissingCases: 0, Recall: 1},
		"test":   {Role: "test", ExpectedCases: 1, MatchedCases: 1, MissingCases: 0, Recall: 1},
	}
	for _, score := range summary.RoleRecallByRole {
		want, ok := wantScores[score.Role]
		if !ok {
			t.Fatalf("unexpected role score %+v", score)
		}
		if score != want {
			t.Fatalf("score[%s]=%+v want %+v", score.Role, score, want)
		}
	}
	if summary.PeripheralRoleCoverage["tooling"] != 2 || summary.WrongRolePeripheralCaseCount["tooling"] != 1 {
		t.Fatalf("tooling diagnostics coverage=%v cases=%v", summary.PeripheralRoleCoverage, summary.WrongRolePeripheralCaseCount)
	}
	if summary.PeripheralRoleCoverage["test"] != 1 || summary.WrongRolePeripheralCaseCount["test"] != 1 {
		t.Fatalf("test diagnostics coverage=%v cases=%v", summary.PeripheralRoleCoverage, summary.WrongRolePeripheralCaseCount)
	}
	if summary.PeripheralRoleCoverage["generated"] != 1 || summary.WrongRolePeripheralCaseCount["generated"] != 1 {
		t.Fatalf("generated diagnostics coverage=%v cases=%v", summary.PeripheralRoleCoverage, summary.WrongRolePeripheralCaseCount)
	}
	if _, ok := summary.PeripheralRoleCoverage["vendor"]; ok {
		t.Fatalf("stale eval peripheral role should be excluded: %v", summary.PeripheralRoleCoverage)
	}
}

func TestRenderGatherContextRepoIndexFreshnessMarkdown(t *testing.T) {
	t.Parallel()

	markdown := renderGatherContextRepoIndexFreshnessMarkdown(map[string]any{
		"index_head_sha":          "111111111111aaaaaaaa",
		"current_head_sha":        "222222222222bbbbbbbb",
		"index_worktree_dirty":    false,
		"current_worktree_dirty":  true,
		"stale_or_dirty_mismatch": true,
		"freshness": repoindex.IndexFreshnessStatus{
			Level: repoindex.FreshnessStale,
		},
	})
	for _, want := range []string{
		"Repoindex freshness: `stale`",
		"index head `111111111111`",
		"current head `222222222222`",
		"index dirty `false`",
		"current dirty `true`",
		"stale/dirty mismatch `true`",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q: %s", want, markdown)
		}
	}
}

func TestRenderGatherContextEvalMarkdownIncludesRoleDiagnostics(t *testing.T) {
	t.Parallel()

	markdown := renderGatherContextEvalMarkdown("/workspace", gatherContextEvalSummary{
		Count:          1,
		ScoredCount:    1,
		MeanRoleRecall: 0.5,
		RoleRecallByRole: []gatherContextRoleRecallScore{{
			Role:          "router",
			ExpectedCases: 2,
			MatchedCases:  1,
			MissingCases:  1,
			Recall:        0.5,
		}},
		PeripheralRoleCoverage:       map[string]int{"tooling": 3},
		WrongRolePeripheralCaseCount: map[string]int{"tooling": 2},
	}, []gatherContextEvalResult{{
		CaseID:     "case-1",
		Status:     "ok",
		RoleRecall: 0.5,
	}}, nil, nil)
	for _, want := range []string{
		"## Role Diagnostics",
		"| router | 0.50 | 1 | 2 | 1 |",
		"Peripheral roles returned when not expected",
		"| tooling | 3 | 2 |",
		"| case-1 | no |",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown missing %q:\n%s", want, markdown)
		}
	}
}

func TestGatherContextRoleScoring(t *testing.T) {
	t.Parallel()

	evalCase := promptEvalCase{Metadata: map[string]any{
		"expected_roles":      []any{"route_file", "router", "docs"},
		"expected_file_roles": "deploy_config,test",
	}}
	expected := gatherContextEvalExpectedRoles(evalCase)
	if got, want := strings.Join(expected, ","), "route,router,docs,deploy_config,test"; got != want {
		t.Fatalf("expected roles=%q want %q", got, want)
	}

	coverage := gatherContextPathRoleCoverage([]string{
		"apps/mobile/app/connection/[connectionId].tsx",
		"apps/api/lib/praze_web/router.ex",
		"docs/architecture/presence.md",
		"charts/presence/values.yaml",
		"apps/mobile/app/connection/[connectionId].test.tsx",
	})
	matched, recall := scoreGatherContextExpectedRoles(expected, coverage)
	if recall != 1 {
		t.Fatalf("recall=%f matched=%v coverage=%v", recall, matched, coverage)
	}
}

func TestInferGatherContextPathRoles(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		"apps/mobile/app/onboarding/_layout.tsx":        {"route"},
		"lib/my_app_web/router.ex":                      {"router"},
		"lib/my_app_web/controllers/page_controller.ex": {"controller"},
		"docs/architecture/runtime.md":                  {"docs"},
		"deploy/kubernetes/app.yaml":                    {"deploy_config"},
		"test/support/fixtures/users.json":              {"test", "data"},
		".factory/templates/vote-test-script.ts":        {"tooling", "test"},
		"internal/generated/client.pb.go":               {"generated"},
	}
	for path, wants := range cases {
		roles := inferGatherContextPathRoles(path)
		for _, want := range wants {
			if !containsString(roles, want) {
				t.Fatalf("path %s roles=%v missing %s", path, roles, want)
			}
		}
	}
}

func TestBuildGatherContextFactBlobIncludesFactsEvidenceAndRefs(t *testing.T) {
	t.Parallel()

	bundle := contextengine.ContextBundle{
		Summary: "Bundle summary mentions runtime certification.",
		Facts: []contextengine.ContextFact{{
			Fact: "Facts must reference evidence IDs.",
			Refs: []contextengine.EvidenceRef{{
				Type:    contextengine.RefTypePath,
				Ref:     "internal/context/contextengine/context_bundle.go",
				Excerpt: "EvidenceIDs []string",
			}},
		}},
		Evidence: []contextengine.EvidenceNode{{
			Statement: "gather_context reduces EvidencePacks into ContextBundle.",
			Ref: contextengine.EvidenceRef{
				Type:  contextengine.RefTypeSymbol,
				Ref:   "GatherContext",
				Title: "GatherContext",
			},
			Metadata: map[string]any{"path": "internal/context/contextengine/context_gather.go"},
		}},
	}

	blob := buildGatherContextFactBlob(bundle)
	matched, recall := scoreRequiredFacts([]string{
		"runtime certification",
		"Facts must reference evidence IDs",
		"internal/context/contextengine/context_gather.go",
	}, blob)
	if recall != 1 {
		t.Fatalf("recall=%f matched=%v blob=%q", recall, matched, blob)
	}
}

func TestGatherContextEvalPassedRequiresFactRecall(t *testing.T) {
	t.Parallel()

	evalCase := promptEvalCase{
		ExpectedPaths: []string{"internal/rlm/env/tool_exec.go"},
		RequiredFacts: []string{"gather_context"},
	}
	result := gatherContextEvalResult{
		Status:        "ok",
		Answerable:    true,
		EvidenceCount: 1,
		PathRecall:    1,
		FactRecall:    0,
	}
	if gatherContextEvalPassed(result, evalCase, 0.8) {
		t.Fatalf("expected fail when required facts are missing")
	}
	result.FactRecall = 1
	if !gatherContextEvalPassed(result, evalCase, 0.8) {
		t.Fatalf("expected pass when path and fact recall pass")
	}
	result.StaleEval = true
	if gatherContextEvalPassed(result, evalCase, 0.8) {
		t.Fatalf("expected fail when eval expected paths are stale")
	}
}

func TestGatherContextExpectedPathsMissingOnDisk(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "internal", "rlm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "internal", "rlm", "lambda.go"), []byte("package rlm\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := gatherContextExpectedPathsMissingOnDisk(workspace, []string{
		"internal/rlm/lambda.go",
		"internal/rlm/missing.go",
	})
	if got := strings.Join(missing, ","); got != "internal/rlm/missing.go" {
		t.Fatalf("missing=%v", missing)
	}
}

func TestGatherContextEvalCaseMetadataOverrides(t *testing.T) {
	t.Parallel()

	evalCase := promptEvalCase{Metadata: map[string]any{
		"lanes":             []any{"code", "memory", "code", ""},
		"memory_statuses":   "current,candidate",
		"goal":              "mixed_context_eval",
		"max_context_chars": float64(2048),
	}}

	lanes := gatherContextEvalCaseStringSlice(evalCase, "lanes", []string{"code"})
	if got, want := strings.Join(lanes, ","), "code,memory"; got != want {
		t.Fatalf("lanes=%q want %q", got, want)
	}
	statuses := gatherContextEvalCaseStringSlice(evalCase, "memory_statuses", nil)
	if got, want := strings.Join(statuses, ","), "current,candidate"; got != want {
		t.Fatalf("statuses=%q want %q", got, want)
	}
	if got := gatherContextEvalCaseString(evalCase, "goal", "fallback"); got != "mixed_context_eval" {
		t.Fatalf("goal=%q", got)
	}
	if got := gatherContextEvalCaseInt(evalCase, "max_context_chars", 6000); got != 2048 {
		t.Fatalf("max_context_chars=%d", got)
	}
}

func TestPolyglotGatherContextFixtureDatasetPathsExist(t *testing.T) {
	t.Parallel()

	workspace := filepath.Join("..", "..", "..", "testdata", "fixtures", "gather-context", "polyglot-repo")
	dataset := filepath.Join("..", "..", "..", "testdata", "evals", "gather-context", "polyglot-fixture.jsonl")
	cases, err := loadPromptEvalCases(dataset)
	if err != nil {
		t.Fatalf("load polyglot dataset: %v", err)
	}
	if len(cases) != 5 {
		t.Fatalf("cases=%d want 5", len(cases))
	}
	categories := map[string]struct{}{}
	for _, evalCase := range cases {
		categories[evalCase.Category] = struct{}{}
		if strings.TrimSpace(evalCase.TaskType) == "" {
			t.Fatalf("case %s missing task_type", evalCase.ID)
		}
		if len(evalCase.ExpectedPaths) == 0 {
			t.Fatalf("case %s missing expected_paths", evalCase.ID)
		}
		for _, path := range evalCase.ExpectedPaths {
			if _, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(path))); err != nil {
				t.Fatalf("case %s expected path %s: %v", evalCase.ID, path, err)
			}
		}
	}
	for _, category := range []string{"polyglot-go", "polyglot-typescript", "polyglot-python", "polyglot-elixir", "polyglot-docs"} {
		if _, ok := categories[category]; !ok {
			t.Fatalf("missing category %s in %v", category, categories)
		}
	}
}

func TestBuildRLMSearchAgentGatherPayloadUsesEvalConstraints(t *testing.T) {
	t.Parallel()

	evalCase := promptEvalCase{
		Question:      "Where is gather_context executed?",
		Context:       "Focus on the RLM tool path.",
		TaskType:      "execution_trace",
		RequiredFacts: []string{"adapter executes gather_context"},
		Metadata: map[string]any{
			"lanes":             []any{"code", "context"},
			"memory_statuses":   "current,candidate",
			"goal":              "repo_grounded_eval",
			"max_context_chars": float64(2048),
		},
	}

	payload := buildRLMSearchAgentGatherPayload(evalCase, 7, 6000, []string{"code"})
	if payload["response_mode"] != "answer_surface" {
		t.Fatalf("response_mode=%v", payload["response_mode"])
	}
	if payload["task_type"] != "execution_trace" {
		t.Fatalf("task_type=%v", payload["task_type"])
	}
	if payload["limit"] != 7 || payload["max_context_chars"] != 2048 {
		t.Fatalf("budget fields=%v", payload)
	}
	if got := strings.Join(payload["lanes"].([]string), ","); got != "code,context" {
		t.Fatalf("lanes=%q", got)
	}
	if got := strings.Join(payload["memory_statuses"].([]string), ","); got != "current,candidate" {
		t.Fatalf("memory_statuses=%q", got)
	}
	if got := strings.Join(payload["required_evidence"].([]string), ","); got != "adapter executes gather_context" {
		t.Fatalf("required_evidence=%q", got)
	}
	prompt := buildRLMSearchAgentEvalPrompt(evalCase, payload)
	if !strings.Contains(prompt, "Your first tool call must be gather_context") || !strings.Contains(prompt, `"response_mode": "answer_surface"`) {
		t.Fatalf("prompt does not include forced gather payload:\n%s", prompt)
	}
	if !strings.Contains(prompt, "answer_seed.paths") {
		t.Fatalf("prompt does not direct model to copy answer_seed.paths:\n%s", prompt)
	}
}

func TestGatherContextEvalClassifiesReductionAndFinalAnswerLoss(t *testing.T) {
	t.Parallel()

	expected := []string{
		"cmd/foxctl/cmd/eval_gather_context.go",
		"cmd/foxctl/cmd/eval_agents.go",
	}
	raw := strings.Join([]string{
		"cmd/foxctl/cmd/eval_gather_context.go",
		"cmd/foxctl/cmd/eval_agents.go",
	}, "\n")
	selected := "cmd/foxctl/cmd/eval_gather_context.go"
	final := "cmd/foxctl/cmd/eval_gather_context.go"

	reductionMisses := expectedPathsPresentThenMissing(expected, raw, selected)
	if got := strings.Join(reductionMisses, ","); got != "cmd/foxctl/cmd/eval_agents.go" {
		t.Fatalf("reductionMisses=%v", reductionMisses)
	}
	finalLosses := expectedPathsPresentThenMissing(expected, raw, final)
	if got := strings.Join(finalLosses, ","); got != "cmd/foxctl/cmd/eval_agents.go" {
		t.Fatalf("finalLosses=%v", finalLosses)
	}
	retrievalMisses := missingExpectedPaths(expected, raw)
	if len(retrievalMisses) != 0 {
		t.Fatalf("retrievalMisses=%v", retrievalMisses)
	}
}

func TestCompareGatherContextToAgentBaselines(t *testing.T) {
	t.Parallel()

	comparisons := compareGatherContextToAgentBaselines(gatherContextEvalSummary{
		PassRate:                1,
		MeanPathRecall:          0.9,
		MeanFactRecall:          0.8,
		MeanDurationMS:          100,
		MeanEmittedContextChars: 1200,
	}, []agentEvalSummary{{
		Label:          "mini-subagent",
		Role:           "researcher",
		Provider:       "openai",
		Model:          "mini",
		Runner:         "external",
		Count:          2,
		PassRate:       0.5,
		MeanPathRecall: 0.4,
		MeanFactRecall: 0.3,
		MeanDurationMS: 1000,
		MeanTokens:     6000,
		MeanCostUSD:    0.01,
	}})
	if len(comparisons) != 1 {
		t.Fatalf("comparisons=%d want 1", len(comparisons))
	}
	got := comparisons[0]
	if got.Label != "mini-subagent" || got.PassRateDelta != 0.5 || got.PathRecallDelta != 0.5 || got.FactRecallDelta != 0.5 {
		t.Fatalf("comparison=%+v", got)
	}
	if got.DurationSpeedup != 10 {
		t.Fatalf("speedup=%f want 10", got.DurationSpeedup)
	}
	if got.EmittedCharsPerTokenMean != 0.2 {
		t.Fatalf("chars/token=%f want 0.2", got.EmittedCharsPerTokenMean)
	}
}
