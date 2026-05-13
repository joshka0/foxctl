package rlm

import "testing"

func BenchmarkResolveRunSpecStagedCodeRetrieval(b *testing.B) {
	tools := benchmarkAllTools()
	input := ResolveRunSpecInput{
		Prompt:               "Trace internal/runtime/engine.ToolRunner.Execute and verify hook dispatch behavior.",
		RequestedRoute:       RouteProfileCodeRetrieval,
		RequestedPlanMode:    PlanModeStaged,
		RequestedToolProfile: string(ToolProfileCodeIntel),
		AvailableTools:       tools,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		spec, err := ResolveRunSpec(input)
		if err != nil {
			b.Fatalf("ResolveRunSpec() error = %v", err)
		}
		if spec.PlanMode != PlanModeStaged || len(spec.Plan.Phases) == 0 {
			b.Fatalf("unexpected spec: mode=%s phases=%d", spec.PlanMode, len(spec.Plan.Phases))
		}
	}
}

func BenchmarkResolveRunSpecAutoMixedDefaultPolicy(b *testing.B) {
	tools := benchmarkAllTools()
	input := ResolveRunSpecInput{
		Prompt:               "Compare the benchmark plan with the docs and memory surfaces.",
		RequestedRoute:       RouteProfileAuto,
		RequestedPlanMode:    PlanModeFree,
		RequestedToolProfile: string(ToolProfileDefault),
		AvailableTools:       tools,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		spec, err := ResolveRunSpec(input)
		if err != nil {
			b.Fatalf("ResolveRunSpec() error = %v", err)
		}
		if len(spec.ToolPolicy.Tools) == 0 {
			b.Fatal("tool policy is empty")
		}
	}
}

func benchmarkAllTools() []Tool {
	return []Tool{
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
	}
}
