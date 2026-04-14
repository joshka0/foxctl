package transcriptpipeline

import (
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/v2/adapters/sourceimport"
)

func TestGroupSourceBundlesSeparatesSubagents(t *testing.T) {
	now := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)
	bundles := []SourceBundle{
		{
			Meta: SourceMeta{
				Provider:      sourceimport.ProviderCodex,
				SessionID:     "root",
				SourcePath:    "/tmp/root.jsonl",
				WorkspacePath: "/tmp/ws",
				StartedAt:     now,
			},
			Parsed: sourceimport.ParsedSession{Provider: sourceimport.ProviderCodex, SessionID: "root"},
		},
		{
			Meta: SourceMeta{
				Provider:        sourceimport.ProviderCodex,
				SessionID:       "child-subagent",
				ParentSessionID: "root",
				SourcePath:      "/tmp/subagent.jsonl",
				WorkspacePath:   "/tmp/ws",
				IsSubagent:      true,
				AgentRole:       "explorer",
				StartedAt:       now.Add(time.Minute),
			},
			Parsed: sourceimport.ParsedSession{Provider: sourceimport.ProviderCodex, SessionID: "child-subagent"},
		},
	}

	groups := GroupSourceBundles(bundles)
	if len(groups) != 1 {
		t.Fatalf("groups=%d want 1", len(groups))
	}
	group := groups[0]
	if got := SessionIDsForBundles(group.MainlineBundles()); len(got) != 1 || got[0] != "root" {
		t.Fatalf("mainline=%v want [root]", got)
	}
	if got := SessionIDsForBundles(group.SidecarBundles()); len(got) != 1 || got[0] != "child-subagent" {
		t.Fatalf("sidecars=%v want [child-subagent]", got)
	}
}
