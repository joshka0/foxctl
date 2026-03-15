package rlm

import (
	"reflect"
	"testing"
)

func TestClassifyRouteProfileDefaultsCodeRetrieval(t *testing.T) {
	t.Parallel()

	if got := ClassifyRouteProfile("storage memory package"); got != RouteProfileCodeRetrieval {
		t.Fatalf("ClassifyRouteProfile()=%s want %s", got, RouteProfileCodeRetrieval)
	}
}

func TestBuildPlanStagedCodeRetrieval(t *testing.T) {
	t.Parallel()

	plan := BuildPlan("auth runtime wiring", RouteProfileCodeRetrieval, PlanModeStaged)
	if plan.RouteProfile != RouteProfileCodeRetrieval {
		t.Fatalf("route=%s", plan.RouteProfile)
	}
	if plan.Mode != PlanModeStaged {
		t.Fatalf("mode=%s", plan.Mode)
	}
	if len(plan.Phases) != 3 {
		t.Fatalf("phases=%d", len(plan.Phases))
	}
	if plan.Phases[0].Name != "discovery" {
		t.Fatalf("phase0=%s", plan.Phases[0].Name)
	}
	if !reflect.DeepEqual(plan.Phases[1].RequireOneOf, []string{"load_file", "read_note"}) {
		t.Fatalf("inspection require_one_of=%v", plan.Phases[1].RequireOneOf)
	}
}

func TestFilterToolsByNames(t *testing.T) {
	t.Parallel()

	in := []Tool{
		{Name: "semantic_search_code"},
		{Name: "search_repo"},
		{Name: "load_file"},
	}
	got := filterToolsByNames(in, []string{"search_repo", "load_file"})
	if len(got) != 2 || got[0].Name != "search_repo" || got[1].Name != "load_file" {
		t.Fatalf("filterToolsByNames()=%v", got)
	}
}
