package transcriptpipeline

import (
	"context"
	"testing"

	"github.com/jkatigb/agentctl/internal/storage/transcriptcache"
	"github.com/jkatigb/agentctl/internal/v2/adapters/sourceimport"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
)

func TestPreprocessParsedSession_DeterministicCachesReferenceBlob(t *testing.T) {
	ctx := context.Background()
	cacheStore, err := transcriptcache.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("transcriptcache.Open() error = %v", err)
	}
	defer cacheStore.Close()

	parsed := sourceimport.ParsedSession{
		Provider:  sourceimport.ProviderCodex,
		SessionID: "sess-1",
		Turns: []run.TurnRecord{{
			ID:     "t1",
			Prompt: "here's the article: <article>\nClaude Code Dreams\nAuto Dream consolidates memory files automatically.\nPhase 1: Orientation\nPhase 2: Gather Signal\nPhase 3: Consolidation\n</article>",
		}},
	}

	got, err := PreprocessParsedSession(ctx, parsed, cacheStore, PreprocessOptions{
		Mode:                    "deterministic",
		ReferencePromptVersion:  "reference_blob_summary_v1",
		ToolOutputPromptVersion: "tool_output_summary_v1",
	})
	if err != nil {
		t.Fatalf("PreprocessParsedSession() error = %v", err)
	}
	if len(got.Artifacts) != 1 {
		t.Fatalf("artifacts=%d want 1", len(got.Artifacts))
	}
	if got.Artifacts[0].ArtifactKind != "reference_blob" {
		t.Fatalf("artifact_kind=%q want reference_blob", got.Artifacts[0].ArtifactKind)
	}
	if got.Parsed.Turns[0].Prompt == parsed.Turns[0].Prompt {
		t.Fatal("expected prompt to be summarized")
	}
}
