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
	if len(got) < 2 {
		t.Fatalf("queries=%v want at least 2 candidates", got)
	}
	if got[0] != "*AgentActor.handleAsk" {
		t.Fatalf("first query=%q want raw symbol", got[0])
	}
	if got[1] != "handleAsk" {
		t.Fatalf("second query=%q want handleAsk", got[1])
	}
}

func TestPickFindingSeedNodeMatchesExactFileAndNormalizedName(t *testing.T) {
	item := finding{
		File:   "internal/actor/agent_actor.go",
		Symbol: "*AgentActor.handleAsk",
	}
	node, ok := pickFindingSeedNode([]repoindex.Node{
		{ID: "wrong-file", Kind: repoindex.NodeSymbol, File: "internal/other.go", Name: "handleAsk"},
		{ID: "target", Kind: repoindex.NodeSymbol, File: "internal/actor/agent_actor.go", Name: "handleAsk"},
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
	got := attachEvidenceToHotspots(context.Background(), findings, nil, map[string]refhot.FileHotspot{
		"internal/actor/agent_actor.go": {
			Path:       "internal/actor/agent_actor.go",
			TouchCount: 7,
			Score:      42.5,
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
	if len(pack.Hotspots) != 1 {
		t.Fatalf("hotspots=%d want 1", len(pack.Hotspots))
	}
	if pack.Hotspots[0].RecentChangeCount != 7 {
		t.Fatalf("pack recent_change_count=%d want 7", pack.Hotspots[0].RecentChangeCount)
	}
	if pack.Hotspots[0].HotScore != 42.5 {
		t.Fatalf("pack hot_score=%v want 42.5", pack.Hotspots[0].HotScore)
	}
	if pack.Hotspots[0].SeedNodeID != "" {
		t.Fatalf("seed_node_id=%q want empty", pack.Hotspots[0].SeedNodeID)
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
				"hot_score":         10.0,
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
