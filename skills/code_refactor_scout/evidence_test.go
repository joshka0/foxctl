package main

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skilltest"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	refevidence "github.com/jkatigb/agentctl/internal/refactor/evidence"
	refhot "github.com/jkatigb/agentctl/internal/refactor/hot"
	refstatus "github.com/jkatigb/agentctl/internal/refactor/status"
	"github.com/jkatigb/agentctl/internal/repoquery"
)

func TestFindingSeedQueriesNormalizesMethodNames(t *testing.T) {
	got := findingSeedQueries("*AgentActor.handleAsk")
	if len(got) < 3 {
		t.Fatalf("queries=%v want at least 3 candidates", got)
	}
	if got[0] != "*AgentActor.handleAsk" {
		t.Fatalf("first query=%q want raw symbol", got[0])
	}
	if got[1] != "AgentActor.handleAsk" {
		t.Fatalf("second query=%q want AgentActor.handleAsk", got[1])
	}
	if got[2] != "handleAsk" {
		t.Fatalf("third query=%q want handleAsk", got[2])
	}
}

func TestPickFindingSeedNodeMatchesExactFileAndNormalizedName(t *testing.T) {
	item := finding{
		File:   "internal/actor/agent_actor.go",
		Symbol: "*AgentActor.handleAsk",
	}
	node, ok := pickFindingSeedNode([]repoindex.Node{
		{ID: "wrong-file", Kind: repoindex.NodeSymbol, File: "internal/other.go", Name: "handleAsk"},
		{ID: "target", Kind: repoindex.NodeSymbol, File: "internal/actor/agent_actor.go", Name: "AgentActor.handleAsk"},
	}, item)
	if !ok {
		t.Fatal("expected match")
	}
	if node.ID != "target" {
		t.Fatalf("id=%q want target", node.ID)
	}
}

func TestSuggestedReadPathsSkipsCurrentFileAndDedupes(t *testing.T) {
	got := suggestedReadPaths("a.go",
		[]repoquery.Anchor{
			{Path: "b.go"},
			{Path: "a.go"},
		},
		[]repoquery.Anchor{
			{Path: "c.go"},
			{Path: "b.go"},
		},
	)
	if len(got) != 2 {
		t.Fatalf("got=%v want 2 paths", got)
	}
	if got[0] != "b.go" || got[1] != "c.go" {
		t.Fatalf("got=%v want [b.go c.go]", got)
	}
}

func TestAttachEvidenceToHotspotsAddsSnapshotAndHotFieldsWithoutRepoGraph(t *testing.T) {
	findings := []finding{
		{
			RuleID: "function_hotspot",
			File:   "internal/actor/agent_actor.go",
			Symbol: "*AgentActor.handleAsk",
		},
		{
			RuleID: "duplicate_recovery_block",
			File:   "internal/actor/agent_actor.go",
			Symbol: "*AgentActor.handleAsk",
		},
	}
	pack := refevidence.HotspotPack{}
	symbolHotIndex := buildFindingSymbolHotIndex([]refhot.SymbolHotspot{
		{
			Path:             "internal/actor/agent_actor.go",
			Name:             "AgentActor.handleAsk",
			Score:            5.5,
			TouchCount:       3,
			ChangedLineCount: 12,
			LineStart:        302,
			LineEnd:          464,
		},
	})
	got := attachEvidenceToHotspots(context.Background(), findings, nil, map[string]refhot.FileHotspot{
		"internal/actor/agent_actor.go": {
			Path:       "internal/actor/agent_actor.go",
			TouchCount: 7,
			Score:      42.5,
		},
	}, symbolHotIndex, map[string][]refhot.CochangeNeighbor{
		"internal/actor/agent_actor.go": {
			{Path: "internal/actor/base_actor.go", Count: 2, Score: 1.75},
			{Path: "internal/actor/actor.go", Count: 1, Score: 0.9},
		},
	}, "refsnap-1", "sha256:test", &pack)

	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	evidence := got[0].Evidence
	if evidence["scope_snapshot_id"] != "refsnap-1" {
		t.Fatalf("scope_snapshot_id=%v want refsnap-1", evidence["scope_snapshot_id"])
	}
	if evidence["scope_snapshot_artifact"] != "sha256:test" {
		t.Fatalf("scope_snapshot_artifact=%v want sha256:test", evidence["scope_snapshot_artifact"])
	}
	if evidence["recent_change_count"] != 7 {
		t.Fatalf("recent_change_count=%v want 7", evidence["recent_change_count"])
	}
	if evidence["hot_score"] != 42.5 {
		t.Fatalf("hot_score=%v want 42.5", evidence["hot_score"])
	}
	if evidence["symbol_recent_change_count"] != 3 {
		t.Fatalf("symbol_recent_change_count=%v want 3", evidence["symbol_recent_change_count"])
	}
	if evidence["symbol_hot_score"] != 5.5 {
		t.Fatalf("symbol_hot_score=%v want 5.5", evidence["symbol_hot_score"])
	}
	if evidence["symbol_changed_line_count"] != 12 {
		t.Fatalf("symbol_changed_line_count=%v want 12", evidence["symbol_changed_line_count"])
	}
	if evidence["cochange_count"] != 2 {
		t.Fatalf("cochange_count=%v want 2", evidence["cochange_count"])
	}
	if evidence["cochange_strength"] != 1.75 {
		t.Fatalf("cochange_strength=%v want 1.75", evidence["cochange_strength"])
	}
	if len(pack.Hotspots) != 1 {
		t.Fatalf("hotspots=%d want 1", len(pack.Hotspots))
	}
	if pack.Hotspots[0].RecentChangeCount != 7 {
		t.Fatalf("pack recent_change_count=%d want 7", pack.Hotspots[0].RecentChangeCount)
	}
	if pack.Hotspots[0].HotScore != 42.5 {
		t.Fatalf("pack hot_score=%v want 42.5", pack.Hotspots[0].HotScore)
	}
	if pack.Hotspots[0].SymbolTouchCount != 3 {
		t.Fatalf("pack symbol_touch_count=%d want 3", pack.Hotspots[0].SymbolTouchCount)
	}
	if pack.Hotspots[0].SymbolHotScore != 5.5 {
		t.Fatalf("pack symbol_hot_score=%v want 5.5", pack.Hotspots[0].SymbolHotScore)
	}
	if pack.Hotspots[0].SymbolChangedLine != 12 {
		t.Fatalf("pack symbol_changed_line_count=%d want 12", pack.Hotspots[0].SymbolChangedLine)
	}
	if pack.Hotspots[0].CochangeCount != 2 {
		t.Fatalf("pack cochange_count=%d want 2", pack.Hotspots[0].CochangeCount)
	}
	if pack.Hotspots[0].CochangeStrength != 1.75 {
		t.Fatalf("pack cochange_strength=%v want 1.75", pack.Hotspots[0].CochangeStrength)
	}
	if len(pack.Hotspots[0].CochangePaths) != 2 {
		t.Fatalf("pack cochange_paths=%v want 2 paths", pack.Hotspots[0].CochangePaths)
	}
	if pack.Hotspots[0].SeedNodeID != "" {
		t.Fatalf("seed_node_id=%q want empty", pack.Hotspots[0].SeedNodeID)
	}
}

func TestLookupFindingSymbolHotspotNormalizesReceiverNames(t *testing.T) {
	index := buildFindingSymbolHotIndex([]refhot.SymbolHotspot{
		{
			Path:             "internal/actor/agent_actor.go",
			Name:             "AgentActor.handleAsk",
			Score:            4.2,
			ChangedLineCount: 9,
			LineStart:        302,
			LineEnd:          464,
		},
	})
	got, ok := lookupFindingSymbolHotspot(index, finding{
		File:   "internal/actor/agent_actor.go",
		Symbol: "*AgentActor.handleAsk",
		Line:   320,
	})
	if !ok {
		t.Fatal("expected symbol hotspot match")
	}
	if got.Name != "AgentActor.handleAsk" {
		t.Fatalf("name=%q want AgentActor.handleAsk", got.Name)
	}
}

func TestClassifySuggestedBoundary(t *testing.T) {
	cases := []struct {
		name string
		item finding
		want string
	}{
		{
			name: "error normalizer",
			item: finding{
				RuleID: "function_hotspot",
				Evidence: map[string]any{
					"rules": []string{"duplicate_recovery_block", "duplicated_error_remap"},
				},
			},
			want: "extract_error_normalizer",
		},
		{
			name: "boolean surface",
			item: finding{
				RuleID: "function_hotspot",
				Evidence: map[string]any{
					"rules": []string{"semantic_simplification_candidate", "high_cyclomatic_complexity"},
				},
			},
			want: "simplify_boolean_surface",
		},
		{
			name: "repo loader",
			item: finding{
				RuleID: "function_hotspot",
				Evidence: map[string]any{
					"rules": []string{"preload_after_get_chain", "same_file_extraction_candidate"},
				},
			},
			want: "extract_repo_loader",
		},
		{
			name: "post transaction loader",
			item: finding{
				RuleID: "function_hotspot",
				Evidence: map[string]any{
					"rules": []string{"post_transaction_preload", "transaction_script_hotspot"},
				},
			},
			want: "split_transaction_loader",
		},
		{
			name: "transaction script",
			item: finding{
				RuleID: "function_hotspot",
				Evidence: map[string]any{
					"rules": []string{"transaction_script_hotspot", "fan_out_dependency_spread"},
				},
			},
			want: "extract_transaction_script",
		},
		{
			name: "workflow step",
			item: finding{
				RuleID: "function_hotspot",
				Evidence: map[string]any{
					"rules": []string{"duplicate_orchestration_fingerprint", "fan_out_dependency_spread"},
				},
			},
			want: "extract_workflow_step",
		},
		{
			name: "branch core",
			item: finding{
				RuleID: "function_hotspot",
				Evidence: map[string]any{
					"rules": []string{"same_file_extraction_candidate", "high_cyclomatic_complexity"},
				},
			},
			want: "extract_branch_core",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifySuggestedBoundary(tt.item); got != tt.want {
				t.Fatalf("boundary=%q want %q", got, tt.want)
			}
		})
	}
}

func TestPersistScoutEvidencePackAddsArtifactToHotspots(t *testing.T) {
	stdout := &bytes.Buffer{}
	rc, cleanup := skilltest.NewTestRunContext(t, stdout, nil)
	defer cleanup()
	rc.Now = func() time.Time { return time.Unix(1, 0).UTC() }

	current := scoutEvidenceResult{
		Findings: []finding{
			{
				RuleID:   "function_hotspot",
				File:     "internal/actor/agent_actor.go",
				Symbol:   "*AgentActor.handleAsk",
				Evidence: map[string]any{"scope_snapshot_id": "refsnap-1"},
			},
			{
				RuleID:   "duplicate_recovery_block",
				File:     "internal/actor/agent_actor.go",
				Symbol:   "*AgentActor.handleAsk",
				Evidence: map[string]any{"duplicate_count": 2},
			},
		},
	}
	got, err := persistScoutEvidencePack(context.Background(), rc, current, refevidence.HotspotPack{
		SnapshotID: "refsnap-1",
		Hotspots: []refevidence.HotspotRow{
			{
				File:              "internal/actor/agent_actor.go",
				Symbol:            "*AgentActor.handleAsk",
				RuleID:            "function_hotspot",
				RecentChangeCount: 3,
				HotScore:          17.25,
			},
		},
	})
	if err != nil {
		t.Fatalf("persist pack: %v", err)
	}
	if got.EvidenceArtifact == "" {
		t.Fatal("expected evidence artifact digest")
	}
	if artifact, _ := got.Findings[0].Evidence["evidence_artifact"].(string); artifact == "" {
		t.Fatalf("function_hotspot evidence_artifact=%v want digest", got.Findings[0].Evidence["evidence_artifact"])
	}
	if _, exists := got.Findings[1].Evidence["evidence_artifact"]; exists {
		t.Fatalf("non-hotspot unexpectedly received evidence_artifact: %#v", got.Findings[1].Evidence)
	}
}

func TestRerankScoutFindingsBoostsIndexBackedHotspots(t *testing.T) {
	findings := []finding{
		{
			RuleID:   "function_hotspot",
			File:     "a.go",
			Symbol:   "Alpha",
			Score:    94,
			Severity: "high",
			Evidence: map[string]any{},
		},
		{
			RuleID:   "function_hotspot",
			File:     "b.go",
			Symbol:   "Beta",
			Score:    90,
			Severity: "high",
			Evidence: map[string]any{
				"reverse_dep_count": 13,
				"forward_dep_count": 11,
				"hot_score":         3.2,
			},
		},
	}

	got := rerankScoutFindings(findings, refstatus.ModeIndexBacked)
	sortFindings(got)
	if got[0].Symbol != "Beta" {
		t.Fatalf("top symbol=%q want Beta after rerank", got[0].Symbol)
	}
	if got[0].Score <= 90 {
		t.Fatalf("reranked score=%d want above base 90", got[0].Score)
	}
	if got[0].Evidence["base_score"] != 90 {
		t.Fatalf("base_score=%v want 90", got[0].Evidence["base_score"])
	}
	if got[0].Evidence["index_rerank_bonus"] != 15 {
		t.Fatalf("index_rerank_bonus=%v want 15", got[0].Evidence["index_rerank_bonus"])
	}
}

func TestRerankScoutFindingsSkipsParserOnlyMode(t *testing.T) {
	findings := []finding{
		{
			RuleID:   "function_hotspot",
			File:     "a.go",
			Symbol:   "Alpha",
			Score:    90,
			Severity: "high",
			Evidence: map[string]any{
				"reverse_dep_count": 99,
				"forward_dep_count": 99,
			},
		},
	}

	got := rerankScoutFindings(findings, refstatus.ModeParserOnly)
	if got[0].Score != 90 {
		t.Fatalf("score=%d want unchanged 90", got[0].Score)
	}
	if _, ok := got[0].Evidence["index_rerank_bonus"]; ok {
		t.Fatalf("unexpected rerank evidence in parser_only mode: %#v", got[0].Evidence)
	}
}

func TestRerankScoutFindingsUsesParserOnlyOpportunityScoreFromSymbolHotness(t *testing.T) {
	findings := []finding{
		{
			RuleID:   "function_hotspot",
			File:     "a.go",
			Symbol:   "Alpha",
			Score:    90,
			Severity: "high",
			Evidence: map[string]any{
				"symbol_hot_score":           5.2,
				"symbol_recent_change_count": 6,
				"symbol_changed_line_count":  11,
			},
		},
	}

	got := rerankScoutFindings(findings, refstatus.ModeParserOnly)
	if got[0].Score <= 90 {
		t.Fatalf("score=%d want parser_only opportunity boost", got[0].Score)
	}
	if got[0].Evidence["opportunity_score"] != got[0].Score {
		t.Fatalf("opportunity_score=%v want %d", got[0].Evidence["opportunity_score"], got[0].Score)
	}
	if _, ok := got[0].Evidence["index_rerank_bonus"]; ok {
		t.Fatalf("unexpected index_rerank_bonus in parser_only mode: %#v", got[0].Evidence)
	}
}

func TestRerankScoutFindingsUsesCochangePressure(t *testing.T) {
	findings := []finding{
		{
			RuleID:   "function_hotspot",
			File:     "a.go",
			Symbol:   "Alpha",
			Score:    90,
			Severity: "high",
			Evidence: map[string]any{
				"cochange_strength": 1.8,
				"cochange_count":    3,
			},
		},
	}

	got := rerankScoutFindings(findings, refstatus.ModeParserOnly)
	if got[0].Score <= 90 {
		t.Fatalf("score=%d want cochange boost", got[0].Score)
	}
	factors, ok := got[0].Evidence["opportunity_factors"].(map[string]int)
	if !ok {
		t.Fatalf("opportunity_factors=%T want map[string]int", got[0].Evidence["opportunity_factors"])
	}
	if factors["cochange"] == 0 {
		t.Fatalf("cochange factor=%d want positive", factors["cochange"])
	}
}
