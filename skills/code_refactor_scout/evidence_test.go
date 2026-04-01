package main

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skilltest"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	refhot "github.com/jkatigb/agentctl/internal/refactor/hot"
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
	pack := scoutHotspotEvidencePack{}
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
	got, err := persistScoutEvidencePack(context.Background(), rc, current, scoutHotspotEvidencePack{
		SnapshotID: "refsnap-1",
		Hotspots: []scoutHotspotEvidenceRow{
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
