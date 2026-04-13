package transcriptpipeline

import (
	"testing"

	"github.com/jkatigb/agentctl/internal/context/companion"
	"github.com/jkatigb/agentctl/internal/v2/adapters/sourceimport"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
)

func TestBuildPacketSet_SeparatesMainlineAndSidecars(t *testing.T) {
	group := SourceGroup{
		GroupID: "codex:root",
		Bundles: []SourceBundle{
			{
				Meta: SourceMeta{
					SessionID: "root",
				},
			},
			{
				Meta: SourceMeta{
					SessionID:  "child",
					IsSubagent: true,
					AgentRole:  "explorer",
					SourcePath: "/tmp/child.jsonl",
				},
				Parsed: sourceimport.ParsedSession{
					SessionID: "child",
					Turns: []run.TurnRecord{
						{
							ID: "t1",
							FinalOutput: run.MessageRef{
								Text: "Explorer found a durable memory primitive.",
							},
						},
					},
				},
			},
		},
	}

	frame := companion.AnchoredInteractionFrame{
		ConversationID: "conv-1",
		UserEvent:      companion.ConversationEvent{Content: "user"},
		AssistantEvent: companion.ConversationEvent{Content: "assistant"},
	}
	set := BuildPacketSet(group, []companion.AnchoredInteractionFrame{frame})
	if len(set.Mainline) != 1 {
		t.Fatalf("mainline packets=%d want 1", len(set.Mainline))
	}
	if len(set.Sidecars) != 1 {
		t.Fatalf("sidecar packets=%d want 1", len(set.Sidecars))
	}
}
