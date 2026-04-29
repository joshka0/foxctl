package rlm

import (
	"reflect"
	"testing"
)

func TestClassifyQueryReturnsTypedPlan(t *testing.T) {
	t.Parallel()

	qp := ClassifyQuery("inspect internal/rlm/plan.go ClassifyQuery()")
	if qp.Route != QueryRouteCode {
		t.Fatalf("ClassifyQuery() route=%s want %s", qp.Route, QueryRouteCode)
	}
	if qp.Confidence <= 0 {
		t.Fatalf("ClassifyQuery() confidence=%.2f want positive", qp.Confidence)
	}
}

func TestClassifyQueryFallsBackToMixedForUncertainPrompt(t *testing.T) {
	t.Parallel()

	qp := ClassifyQuery("what should we consider before planning the next review")
	if qp.Route != QueryRouteMixed {
		t.Fatalf("ClassifyQuery() route=%s want %s", qp.Route, QueryRouteMixed)
	}
}

func TestBuildPlanAutoFallsBackToMixedForUncertainPrompt(t *testing.T) {
	t.Parallel()

	plan := BuildPlan("what should we consider before planning the next review", RouteProfileAuto, PlanModeStaged)
	if plan.RouteProfile != RouteProfileMixed {
		t.Fatalf("route=%s want %s", plan.RouteProfile, RouteProfileMixed)
	}
	if plan.QueryPlan.Route != QueryRouteMixed {
		t.Fatalf("query_plan.route=%s want %s", plan.QueryPlan.Route, QueryRouteMixed)
	}
	if len(plan.Phases) == 0 || len(plan.Phases[0].RequireOneOf) == 0 || plan.Phases[0].RequireOneOf[0] != "retrieve_mixed" {
		t.Fatalf("mixed plan phases=%+v", plan.Phases)
	}
}

func TestBuildPlanStagedCodeRetrievalUsesCompositeTools(t *testing.T) {
	t.Parallel()

	plan := BuildPlan("auth runtime wiring", RouteProfileCodeRetrieval, PlanModeStaged)
	if plan.RouteProfile != RouteProfileCodeRetrieval {
		t.Fatalf("route=%s", plan.RouteProfile)
	}
	if plan.Mode != PlanModeStaged {
		t.Fatalf("mode=%s", plan.Mode)
	}
	if plan.QueryPlan.Route != QueryRouteCode {
		t.Fatalf("query_plan.route=%s want %s", plan.QueryPlan.Route, QueryRouteCode)
	}
	if len(plan.Phases) != 3 {
		t.Fatalf("phases=%d", len(plan.Phases))
	}
	if plan.Phases[0].Name != "discovery" {
		t.Fatalf("phase0=%s", plan.Phases[0].Name)
	}
	// Staged phases must only reference composite tools.
	composites := map[string]struct{}{
		"retrieve_code": {}, "retrieve_memory": {}, "retrieve_context": {},
		"retrieve_task": {}, "retrieve_mixed": {}, "load_evidence_ref": {},
	}
	for _, phase := range plan.Phases {
		for _, tool := range phase.AllowedTools {
			if _, ok := composites[tool]; !ok {
				t.Errorf("phase %q allowed_tool %q is not a composite", phase.Name, tool)
			}
		}
		for _, tool := range phase.RequireOneOf {
			if _, ok := composites[tool]; !ok {
				t.Errorf("phase %q require_one_of %q is not a composite", phase.Name, tool)
			}
		}
	}
	if !reflect.DeepEqual(plan.Phases[0].RequireOneOf, []string{"retrieve_code"}) {
		t.Fatalf("discovery require_one_of=%v", plan.Phases[0].RequireOneOf)
	}
	if !reflect.DeepEqual(plan.Phases[1].RequireOneOf, []string{"load_evidence_ref"}) {
		t.Fatalf("inspection require_one_of=%v", plan.Phases[1].RequireOneOf)
	}
}

func TestBuildPlanStagedMemoryRecallUsesCompositeTools(t *testing.T) {
	t.Parallel()

	plan := BuildPlan("recent decisions about auth", RouteProfileMemoryRecall, PlanModeStaged)
	if plan.RouteProfile != RouteProfileMemoryRecall {
		t.Fatalf("route=%s", plan.RouteProfile)
	}
	if len(plan.Phases) != 2 {
		t.Fatalf("phases=%d want 2", len(plan.Phases))
	}
	composites := map[string]struct{}{
		"retrieve_code": {}, "retrieve_memory": {}, "retrieve_context": {},
		"retrieve_task": {}, "retrieve_mixed": {}, "load_evidence_ref": {},
	}
	for _, phase := range plan.Phases {
		for _, tool := range phase.AllowedTools {
			if _, ok := composites[tool]; !ok {
				t.Errorf("phase %q allowed_tool %q is not a composite", phase.Name, tool)
			}
		}
	}
}

func TestFilterToolsByNames(t *testing.T) {
	t.Parallel()

	in := []Tool{
		{Name: "retrieve_code"},
		{Name: "retrieve_mixed"},
		{Name: "load_evidence_ref"},
	}
	got := filterToolsByNames(in, []string{"retrieve_mixed", "load_evidence_ref"})
	if len(got) != 2 || got[0].Name != "retrieve_mixed" || got[1].Name != "load_evidence_ref" {
		t.Fatalf("filterToolsByNames()=%v", got)
	}
}

func TestNormalizeRouteProfileNoLegacyAliases(t *testing.T) {
	t.Parallel()

	if got := NormalizeRouteProfile("code"); got != RouteProfileAuto {
		t.Fatalf("NormalizeRouteProfile(code)=%s want %s", got, RouteProfileAuto)
	}
	if got := NormalizeRouteProfile("memory-recall"); got != RouteProfileAuto {
		t.Fatalf("NormalizeRouteProfile(memory-recall)=%s want %s", got, RouteProfileAuto)
	}
}

func TestValidateQueryPlanAcceptsValidRoutes(t *testing.T) {
	t.Parallel()

	for _, route := range []QueryRoute{QueryRouteCode, QueryRouteMemory, QueryRouteMixed, QueryRouteEvidenceAudit} {
		if err := ValidateQueryPlan(QueryPlan{Route: route}); err != nil {
			t.Errorf("ValidateQueryPlan(%s)=%v", route, err)
		}
	}
}

func TestValidateQueryPlanRejectsUnknownRoute(t *testing.T) {
	t.Parallel()

	if err := ValidateQueryPlan(QueryPlan{Route: "unknown"}); err == nil {
		t.Fatal("expected error for unknown route")
	}
}
