package rlm

import (
	"reflect"
	"strings"
	"testing"
	"testing/quick"
)

func TestResolveRunSpecRejectsUnknownToolProfile(t *testing.T) {
	t.Parallel()

	_, err := ResolveRunSpec(ResolveRunSpecInput{
		Prompt:               "trace auth handler",
		RequestedRoute:       RouteProfileCodeRetrieval,
		RequestedPlanMode:    PlanModeStaged,
		RequestedToolProfile: "longcot-repl",
		AvailableTools: []Tool{
			{Name: "retrieve_code", ReadOnly: true},
		},
	})
	if err == nil {
		t.Fatal("ResolveRunSpec() error = nil, want unsupported tool profile")
	}
	if !strings.Contains(err.Error(), "unsupported tool profile") {
		t.Fatalf("ResolveRunSpec() error = %v", err)
	}
}

func TestResolveRunSpecRejectsLegacyRouteAliases(t *testing.T) {
	t.Parallel()

	_, err := ResolveRunSpec(ResolveRunSpecInput{
		Prompt:               "trace auth handler",
		RequestedRoute:       RouteProfile("code"),
		RequestedPlanMode:    PlanModeFree,
		RequestedToolProfile: string(ToolProfileDefault),
		AvailableTools: []Tool{
			{Name: "retrieve_code", ReadOnly: true},
		},
	})
	if err == nil {
		t.Fatal("ResolveRunSpec() error = nil, want unsupported route profile")
	}
	if !strings.Contains(err.Error(), "unsupported route profile") {
		t.Fatalf("ResolveRunSpec() error = %v", err)
	}
}

func TestResolveRunSpecBuildsCanonicalPlanAndPolicy(t *testing.T) {
	t.Parallel()

	allTools := []Tool{
		{Name: "gather_context", ReadOnly: true},
		{Name: "gather_test_context", ReadOnly: true},
		{Name: "gather_docs_context", ReadOnly: true},
		{Name: "expand_context_graph", ReadOnly: true},
		{Name: "load_evidence_ref", ReadOnly: true},
		{Name: "code_search_ensemble", ReadOnly: true},
		{Name: "retrieve_code", ReadOnly: true},
		{Name: "retrieve_memory", ReadOnly: true},
		{Name: "retrieve_context", ReadOnly: true},
		{Name: "retrieve_task", ReadOnly: true},
		{Name: "retrieve_mixed", ReadOnly: true},
	}
	spec, err := ResolveRunSpec(ResolveRunSpecInput{
		Prompt:               "trace auth handler",
		RequestedRoute:       RouteProfileCodeRetrieval,
		RequestedPlanMode:    PlanModeStaged,
		RequestedToolProfile: string(ToolProfileDefault),
		AvailableTools:       allTools,
	})
	if err != nil {
		t.Fatalf("ResolveRunSpec() error = %v", err)
	}
	if spec.RouteProfile != RouteProfileCodeRetrieval {
		t.Fatalf("route=%s", spec.RouteProfile)
	}
	if spec.PlanMode != PlanModeStaged {
		t.Fatalf("mode=%s", spec.PlanMode)
	}
	if spec.Plan.RouteProfile != RouteProfileCodeRetrieval {
		t.Fatalf("plan.route=%s", spec.Plan.RouteProfile)
	}
	if spec.Plan.Mode != PlanModeStaged {
		t.Fatalf("plan.mode=%s", spec.Plan.Mode)
	}
	if len(spec.Plan.Phases) != 3 {
		t.Fatalf("plan.phases=%d want 3", len(spec.Plan.Phases))
	}
	if spec.ToolPolicy.Profile != ToolProfileDefault {
		t.Fatalf("policy.profile=%s", spec.ToolPolicy.Profile)
	}
	if len(spec.ToolPolicy.Tools) != 5 {
		t.Fatalf("policy.tools=%d want 5", len(spec.ToolPolicy.Tools))
	}
}

func TestResolveToolPolicyDefaultReturnsMiniSurface(t *testing.T) {
	t.Parallel()

	allTools := []Tool{
		{Name: "gather_context", ReadOnly: true},
		{Name: "gather_test_context", ReadOnly: true},
		{Name: "gather_docs_context", ReadOnly: true},
		{Name: "expand_context_graph", ReadOnly: true},
		{Name: "load_evidence_ref", ReadOnly: true},
		{Name: "code_search_ensemble", ReadOnly: true},
		{Name: "retrieve_code", ReadOnly: true},
		{Name: "retrieve_memory", ReadOnly: true},
		{Name: "retrieve_context", ReadOnly: true},
		{Name: "retrieve_task", ReadOnly: true},
		{Name: "retrieve_mixed", ReadOnly: true},
	}
	policy, err := ResolveToolPolicy(allTools, string(ToolProfileDefault))
	if err != nil {
		t.Fatalf("ResolveToolPolicy() error = %v", err)
	}
	if len(policy.Tools) != 5 {
		t.Fatalf("default tools=%d want 5", len(policy.Tools))
	}
	wantNames := []string{"gather_context", "gather_test_context", "gather_docs_context", "expand_context_graph", "load_evidence_ref"}
	if got := names(policy.Tools); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("default tool names=%v want %v", got, wantNames)
	}
}

func TestResolveToolPolicyNativeExplorerReturnsGatherFirstSurface(t *testing.T) {
	t.Parallel()

	allTools := []Tool{
		{Name: "gather_context", ReadOnly: true},
		{Name: "gather_test_context", ReadOnly: true},
		{Name: "gather_docs_context", ReadOnly: true},
		{Name: "expand_context_graph", ReadOnly: true},
		{Name: "load_evidence_ref", ReadOnly: true},
		{Name: "code_search_ensemble", ReadOnly: true},
		{Name: "retrieve_code", ReadOnly: true},
		{Name: "retrieve_memory", ReadOnly: true},
		{Name: "retrieve_context", ReadOnly: true},
		{Name: "retrieve_task", ReadOnly: true},
		{Name: "retrieve_mixed", ReadOnly: true},
	}
	policy, err := ResolveToolPolicy(allTools, string(ToolProfileNativeExplorer))
	if err != nil {
		t.Fatalf("ResolveToolPolicy() error = %v", err)
	}
	wantNames := []string{"gather_context", "gather_test_context", "gather_docs_context", "expand_context_graph", "load_evidence_ref"}
	if got := names(policy.Tools); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("native-explorer tool names=%v want %v", got, wantNames)
	}
}

func TestResolveToolPolicyCodeIntelReturnsCodeTools(t *testing.T) {
	t.Parallel()

	allTools := []Tool{
		{Name: "gather_context", ReadOnly: true},
		{Name: "gather_test_context", ReadOnly: true},
		{Name: "gather_docs_context", ReadOnly: true},
		{Name: "expand_context_graph", ReadOnly: true},
		{Name: "load_evidence_ref", ReadOnly: true},
		{Name: "code_search_ensemble", ReadOnly: true},
		{Name: "retrieve_code", ReadOnly: true},
		{Name: "retrieve_memory", ReadOnly: true},
		{Name: "retrieve_context", ReadOnly: true},
		{Name: "retrieve_task", ReadOnly: true},
		{Name: "retrieve_mixed", ReadOnly: true},
	}
	policy, err := ResolveToolPolicy(allTools, string(ToolProfileCodeIntel))
	if err != nil {
		t.Fatalf("ResolveToolPolicy() error = %v", err)
	}
	wantNames := []string{"gather_context", "gather_test_context", "gather_docs_context", "expand_context_graph", "load_evidence_ref", "code_search_ensemble", "retrieve_code"}
	if got := names(policy.Tools); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("code-intel tool names=%v want %v", got, wantNames)
	}
}

func TestResolveToolPolicyGatherContextReturnsBundleTools(t *testing.T) {
	t.Parallel()

	allTools := []Tool{
		{Name: "gather_context", ReadOnly: true},
		{Name: "gather_test_context", ReadOnly: true},
		{Name: "gather_docs_context", ReadOnly: true},
		{Name: "expand_context_graph", ReadOnly: true},
		{Name: "load_evidence_ref", ReadOnly: true},
		{Name: "retrieve_code", ReadOnly: true},
		{Name: "retrieve_memory", ReadOnly: true},
		{Name: "retrieve_context", ReadOnly: true},
		{Name: "retrieve_task", ReadOnly: true},
		{Name: "retrieve_mixed", ReadOnly: true},
	}
	policy, err := ResolveToolPolicy(allTools, string(ToolProfileGatherContext))
	if err != nil {
		t.Fatalf("ResolveToolPolicy() error = %v", err)
	}
	wantNames := []string{"gather_context", "gather_test_context", "gather_docs_context", "expand_context_graph", "load_evidence_ref"}
	if got := names(policy.Tools); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("gather-context tool names=%v want %v", got, wantNames)
	}
}

func TestResolveToolPolicyMemoryRecallReturnsMemoryTools(t *testing.T) {
	t.Parallel()

	allTools := []Tool{
		{Name: "gather_context", ReadOnly: true},
		{Name: "gather_test_context", ReadOnly: true},
		{Name: "gather_docs_context", ReadOnly: true},
		{Name: "expand_context_graph", ReadOnly: true},
		{Name: "load_evidence_ref", ReadOnly: true},
		{Name: "retrieve_code", ReadOnly: true},
		{Name: "retrieve_memory", ReadOnly: true},
		{Name: "retrieve_context", ReadOnly: true},
		{Name: "retrieve_task", ReadOnly: true},
		{Name: "retrieve_mixed", ReadOnly: true},
	}
	policy, err := ResolveToolPolicy(allTools, string(ToolProfileMemoryRecall))
	if err != nil {
		t.Fatalf("ResolveToolPolicy() error = %v", err)
	}
	wantNames := []string{"gather_context", "gather_test_context", "gather_docs_context", "expand_context_graph", "load_evidence_ref", "retrieve_memory", "retrieve_context"}
	if got := names(policy.Tools); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("memory-recall tool names=%v want %v", got, wantNames)
	}
}

func TestResolveToolPolicyProfilesExposeOnlyExpectedSurfaces(t *testing.T) {
	t.Parallel()

	allTools := []Tool{
		{Name: "gather_context", ReadOnly: true},
		{Name: "gather_test_context", ReadOnly: true},
		{Name: "gather_docs_context", ReadOnly: true},
		{Name: "expand_context_graph", ReadOnly: true},
		{Name: "load_evidence_ref", ReadOnly: true},
		{Name: "code_search_ensemble", ReadOnly: true},
		{Name: "retrieve_code", ReadOnly: true},
		{Name: "retrieve_memory", ReadOnly: true},
		{Name: "retrieve_context", ReadOnly: true},
		{Name: "retrieve_task", ReadOnly: true},
		{Name: "retrieve_mixed", ReadOnly: true},
		{Name: "memory_ensemble_retrieve", ReadOnly: true},
		{Name: "raw_shell", ReadOnly: false},
	}

	for _, tc := range []struct {
		profile ToolProfile
		want    []string
	}{
		{profile: ToolProfileDefault, want: []string{"gather_context", "gather_test_context", "gather_docs_context", "expand_context_graph", "load_evidence_ref"}},
		{profile: ToolProfileGatherContext, want: []string{"gather_context", "gather_test_context", "gather_docs_context", "expand_context_graph", "load_evidence_ref"}},
		{profile: ToolProfileLambdaRepo, want: []string{"gather_context", "gather_test_context", "gather_docs_context", "expand_context_graph", "load_evidence_ref"}},
		{profile: ToolProfileNativeExplorer, want: []string{"gather_context", "gather_test_context", "gather_docs_context", "expand_context_graph", "load_evidence_ref"}},
		{profile: ToolProfileCodeDebug, want: []string{"gather_context", "gather_test_context", "gather_docs_context", "expand_context_graph", "load_evidence_ref", "code_search_ensemble", "retrieve_code"}},
		{profile: ToolProfileCodeIntel, want: []string{"gather_context", "gather_test_context", "gather_docs_context", "expand_context_graph", "load_evidence_ref", "code_search_ensemble", "retrieve_code"}},
		{profile: ToolProfileMemoryContext, want: []string{"gather_context", "gather_test_context", "gather_docs_context", "expand_context_graph", "load_evidence_ref", "retrieve_memory", "retrieve_context"}},
		{profile: ToolProfileMemoryRecall, want: []string{"gather_context", "gather_test_context", "gather_docs_context", "expand_context_graph", "load_evidence_ref", "retrieve_memory", "retrieve_context"}},
		{profile: ToolProfileFullDebug, want: []string{"gather_context", "gather_test_context", "gather_docs_context", "expand_context_graph", "load_evidence_ref", "code_search_ensemble", "retrieve_code", "retrieve_memory", "retrieve_context", "retrieve_task", "retrieve_mixed", "memory_ensemble_retrieve"}},
		{profile: ToolProfileLongCoTNoModelTools, want: nil},
	} {
		policy, err := ResolveToolPolicy(allTools, string(tc.profile))
		if err != nil {
			t.Fatalf("ResolveToolPolicy(%s) error = %v", tc.profile, err)
		}
		if policy.Profile != tc.profile {
			t.Fatalf("ResolveToolPolicy(%s) profile=%s", tc.profile, policy.Profile)
		}
		if got := names(policy.Tools); !sameStrings(got, tc.want) {
			t.Fatalf("ResolveToolPolicy(%s) tools=%v want %v", tc.profile, got, tc.want)
		}
		if !sameStrings(policy.AllowedTools, tc.want) {
			t.Fatalf("ResolveToolPolicy(%s) allowed_tools=%v want %v", tc.profile, policy.AllowedTools, tc.want)
		}
	}
}

func TestResolveToolPolicyNeverExposesWritableTools(t *testing.T) {
	t.Parallel()

	writableAllowedTools := []Tool{
		{Name: "gather_context", ReadOnly: false},
		{Name: "gather_test_context", ReadOnly: false},
		{Name: "gather_docs_context", ReadOnly: false},
		{Name: "expand_context_graph", ReadOnly: false},
		{Name: "load_evidence_ref", ReadOnly: false},
		{Name: "code_search_ensemble", ReadOnly: false},
		{Name: "retrieve_code", ReadOnly: false},
		{Name: "retrieve_memory", ReadOnly: false},
		{Name: "retrieve_context", ReadOnly: false},
		{Name: "retrieve_task", ReadOnly: false},
		{Name: "retrieve_mixed", ReadOnly: false},
		{Name: "memory_ensemble_retrieve", ReadOnly: false},
		{Name: "safe_but_unknown", ReadOnly: true},
	}

	for _, profile := range toolPolicyProfiles() {
		policy, err := ResolveToolPolicy(writableAllowedTools, string(profile))
		if err != nil {
			t.Fatalf("ResolveToolPolicy(%s) error = %v", profile, err)
		}
		if len(policy.Tools) != 0 {
			t.Fatalf("ResolveToolPolicy(%s) exposed writable tools: %v", profile, names(policy.Tools))
		}
		if len(policy.AllowedTools) != 0 {
			t.Fatalf("ResolveToolPolicy(%s) allowed_tools=%v want empty", profile, policy.AllowedTools)
		}
	}
}

func TestResolveToolPolicyPropertyExposedToolsAreReadOnly(t *testing.T) {
	t.Parallel()

	property := func(profileSeed uint8, readOnlyMask uint16) bool {
		profile := toolPolicyProfileFixture(profileSeed)
		available := make([]Tool, 0, len(toolPolicyToolNames()))
		for i, name := range toolPolicyToolNames() {
			available = append(available, Tool{
				Name:     name,
				ReadOnly: readOnlyMask&(1<<uint(i)) != 0,
			})
		}

		policy, err := ResolveToolPolicy(available, string(profile))
		if err != nil {
			t.Logf("ResolveToolPolicy(%s) error = %v", profile, err)
			return false
		}
		if !sameStrings(policy.AllowedTools, names(policy.Tools)) {
			t.Logf("profile %s allowed_tools=%v tools=%v", profile, policy.AllowedTools, names(policy.Tools))
			return false
		}
		for _, tool := range policy.Tools {
			if !tool.ReadOnly {
				t.Logf("profile %s exposed writable tool %q", profile, tool.Name)
				return false
			}
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("tool policy read-only property failed: %v", err)
	}
}

func TestResolveToolPolicyTrimsAvailableToolNamesForAllowlist(t *testing.T) {
	t.Parallel()

	policy, err := ResolveToolPolicy([]Tool{
		{Name: " gather_context ", ReadOnly: true},
		{Name: "\tload_evidence_ref\n", ReadOnly: true},
		{Name: " retrieve_code ", ReadOnly: true},
	}, string(ToolProfileDefault))
	if err != nil {
		t.Fatalf("ResolveToolPolicy() error = %v", err)
	}
	if got, want := policy.AllowedTools, []string{"gather_context", "load_evidence_ref"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed_tools=%v want %v", got, want)
	}
	if got, want := names(policy.Tools), []string{"gather_context", "load_evidence_ref"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tool names=%v want %v", got, want)
	}
}

func TestNormalizeToolProfileRejectsGeneratedUnknowns(t *testing.T) {
	t.Parallel()

	unknownsFailClosed := func(raw string) bool {
		profile, err := NormalizeToolProfile("unknown:" + raw)
		return err != nil && profile == ""
	}
	if err := quick.Check(unknownsFailClosed, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("generated unknown tool profile did not fail closed: %v", err)
	}
}

func TestResolveToolPolicyUnknownFailsClosed(t *testing.T) {
	t.Parallel()

	allTools := []Tool{
		{Name: "retrieve_code", ReadOnly: true},
	}
	policy, err := ResolveToolPolicy(allTools, "longcot-repl")
	if err == nil {
		t.Fatal("expected error for unknown profile")
	}
	if !strings.Contains(err.Error(), "unsupported tool profile") {
		t.Fatalf("error=%v", err)
	}
	if len(policy.Tools) != 0 {
		t.Fatalf("tools=%v want empty", policy.Tools)
	}
}

func names(tools []Tool) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Name)
	}
	return out
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func toolPolicyProfileFixture(seed uint8) ToolProfile {
	profiles := toolPolicyProfiles()
	return profiles[int(seed)%len(profiles)]
}

func toolPolicyProfiles() []ToolProfile {
	return []ToolProfile{
		ToolProfileDefault,
		ToolProfileGatherContext,
		ToolProfileLambdaRepo,
		ToolProfileNativeExplorer,
		ToolProfileCodeDebug,
		ToolProfileCodeIntel,
		ToolProfileMemoryContext,
		ToolProfileMemoryRecall,
		ToolProfileFullDebug,
		ToolProfileLongCoTNoModelTools,
	}
}

func toolPolicyToolNames() []string {
	return []string{
		"gather_context",
		"gather_test_context",
		"gather_docs_context",
		"expand_context_graph",
		"load_evidence_ref",
		"code_search_ensemble",
		"retrieve_code",
		"retrieve_memory",
		"retrieve_context",
		"retrieve_task",
		"retrieve_mixed",
		"memory_ensemble_retrieve",
	}
}
