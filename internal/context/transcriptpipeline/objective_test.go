package transcriptpipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/v2/adapters/sourceimport"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

func TestBuildSessionObjective_Deterministic(t *testing.T) {
	got, err := BuildSessionObjective(context.Background(), nil, LocalModelRuntime{
		Mode: "deterministic",
	}, sourceimport.ParsedSession{
		Provider:  sourceimport.ProviderCodex,
		SessionID: "sess-1",
		Turns: []run.TurnRecord{
			{
				Prompt: "Design a robust transcript-driven memory consolidation pipeline for this repo.",
				FinalOutput: run.MessageRef{
					Text: "We should use a staged pipeline with classification and consolidation.",
				},
			},
			{
				Prompt: "Let's make the small models handle bounded transforms.",
				FinalOutput: run.MessageRef{
					Text: "We can extract a session objective first and thread it downstream.",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildSessionObjective() error = %v", err)
	}
	if got.Objective == "" {
		t.Fatal("expected objective text")
	}
	if got.Label == "" {
		t.Fatal("expected objective label")
	}
	if got.Status != "active" {
		t.Fatalf("status=%q want active", got.Status)
	}
	if got.Artifact == nil {
		t.Fatal("expected artifact report")
	}
}

func TestBuildSessionObjective_DeterministicCompactsLongPathPrefixedPrompt(t *testing.T) {
	got, err := BuildSessionObjective(context.Background(), nil, LocalModelRuntime{
		Mode: "deterministic",
	}, sourceimport.ParsedSession{
		Provider:  sourceimport.ProviderCodex,
		SessionID: "sess-2",
		Turns: []run.TurnRecord{
			{
				Prompt: "In /Users/joshka/repos/personal/praze-v2-compare, map the current rebuilt frontend architecture to these Paper target surfaces: Pulse Feed, Pulse Map, Prayer Arc, Reader Workspace, Circle Home, Me/Burdens.",
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildSessionObjective() error = %v", err)
	}
	if got.Label == "" {
		t.Fatal("expected compact label")
	}
	if len(strings.Fields(got.Label)) > 10 {
		t.Fatalf("label=%q want <= 10 words", got.Label)
	}
	if strings.Contains(got.Label, "/Users/joshka/repos/personal/praze-v2-compare") {
		t.Fatalf("label=%q still contains workspace path", got.Label)
	}
}

func TestBuildSessionObjective_DeterministicSkipsImageOnlyPromptAndPrefersSubstantiveAsk(t *testing.T) {
	got, err := BuildSessionObjective(context.Background(), nil, LocalModelRuntime{
		Mode: "deterministic",
	}, sourceimport.ParsedSession{
		Provider:  sourceimport.ProviderCodex,
		SessionID: "sess-3",
		Turns: []run.TurnRecord{
			{
				Prompt: "<image name=[Image #1]>\n</image>\n[Image #1] An interesting bug in our app, I see 5 reflections but \"No reflections...\"",
			},
			{
				Prompt: "<turn_aborted>\nThe user interrupted the previous turn on purpose.\n</turn_aborted>\nIts possible it could also have to do with how we set up our seeding (another place to check)",
			},
			{
				Prompt: "Can we go over our app as well and try to look at all the possible error states and error boundaries and make our app production ready for that",
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildSessionObjective() error = %v", err)
	}
	if strings.Contains(got.Objective, "<image") || strings.Contains(got.Objective, "[Image #1]") {
		t.Fatalf("objective=%q still contains image placeholder", got.Objective)
	}
	if strings.Contains(got.Objective, "turn_aborted") {
		t.Fatalf("objective=%q still contains aborted marker", got.Objective)
	}
	if !strings.Contains(strings.ToLower(got.Objective), "production ready") {
		t.Fatalf("objective=%q want substantive later ask", got.Objective)
	}
	if len(got.Evidence) == 0 || strings.Contains(got.Evidence[0], "<image") {
		t.Fatalf("evidence=%v should be cleaned and non-empty", got.Evidence)
	}
	if got.Label != "make our app production ready" {
		t.Fatalf("label=%q want compact goal label", got.Label)
	}
}
