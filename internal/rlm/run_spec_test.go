package rlm

import (
	"reflect"
	"strings"
	"testing"
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
	if len(spec.ToolPolicy.Tools) != 3 {
		t.Fatalf("policy.tools=%d want 3", len(spec.ToolPolicy.Tools))
	}
}

func TestResolveToolPolicyDefaultReturnsMiniSurface(t *testing.T) {
	t.Parallel()

	allTools := []Tool{
		{Name: "gather_context", ReadOnly: true},
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
	if len(policy.Tools) != 3 {
		t.Fatalf("default tools=%d want 3", len(policy.Tools))
	}
	wantNames := []string{"gather_context", "expand_context_graph", "load_evidence_ref"}
	if got := names(policy.Tools); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("default tool names=%v want %v", got, wantNames)
	}
}

func TestResolveToolPolicyCodeIntelReturnsCodeTools(t *testing.T) {
	t.Parallel()

	allTools := []Tool{
		{Name: "gather_context", ReadOnly: true},
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
	wantNames := []string{"gather_context", "expand_context_graph", "load_evidence_ref", "code_search_ensemble", "retrieve_code"}
	if got := names(policy.Tools); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("code-intel tool names=%v want %v", got, wantNames)
	}
}

func TestResolveToolPolicyGatherContextReturnsBundleTools(t *testing.T) {
	t.Parallel()

	allTools := []Tool{
		{Name: "gather_context", ReadOnly: true},
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
	wantNames := []string{"gather_context", "expand_context_graph", "load_evidence_ref"}
	if got := names(policy.Tools); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("gather-context tool names=%v want %v", got, wantNames)
	}
}

func TestResolveToolPolicyMemoryRecallReturnsMemoryTools(t *testing.T) {
	t.Parallel()

	allTools := []Tool{
		{Name: "gather_context", ReadOnly: true},
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
	wantNames := []string{"gather_context", "expand_context_graph", "load_evidence_ref", "retrieve_memory", "retrieve_context"}
	if got := names(policy.Tools); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("memory-recall tool names=%v want %v", got, wantNames)
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
