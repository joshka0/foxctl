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
			{Name: "search_repo", ReadOnly: true},
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
			{Name: "search_repo", ReadOnly: true},
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

	spec, err := ResolveRunSpec(ResolveRunSpecInput{
		Prompt:               "trace auth handler",
		RequestedRoute:       RouteProfileCodeRetrieval,
		RequestedPlanMode:    PlanModeStaged,
		RequestedToolProfile: string(ToolProfileCodeIntel),
		AvailableTools: []Tool{
			{Name: "semantic_search_code", ReadOnly: true},
			{Name: "search_repo", ReadOnly: true},
			{Name: "load_file", ReadOnly: true},
			{Name: "subcall", ReadOnly: true},
		},
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
	if spec.ToolPolicy.Profile != ToolProfileCodeIntel {
		t.Fatalf("policy.profile=%s", spec.ToolPolicy.Profile)
	}
	if got, want := names(spec.ToolPolicy.Tools), []string{"semantic_search_code", "load_file", "subcall"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("policy.tools=%v want %v", got, want)
	}
}

func names(tools []Tool) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Name)
	}
	return out
}
