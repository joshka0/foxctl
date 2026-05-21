package contextplane

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/memorycore"
)

func TestPlanAutonomousMemoryDraftsFromCorrectedFeedback(t *testing.T) {
	now := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	feedback := contextengine.RetrievalFeedback{
		ID:             "fb-1",
		WorkspaceID:    "ws-foxctl",
		EpisodeID:      "ep-1",
		Kind:           contextengine.RetrievalFeedbackKindAnswerCorrected,
		Query:          "how should curator memory drafts work",
		CorrectionStmt: "Autonomous memory drafts must stay evidence-only until reviewed.",
		UsedRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypePath, Ref: "internal/runtime/curator/worker.go"},
		},
		CreatedAt: now,
	}

	plan := PlanAutonomousMemoryDrafts(AutonomousMemoryDraftInput{
		WorkspaceID:   "ws-foxctl",
		WorkspacePath: "/home/dev/repos/foxctl",
		Now:           now,
		Feedback:      []contextengine.RetrievalFeedback{feedback},
		Episodes: []contextengine.RetrievalEpisode{
			{ID: "ep-1", WorkspaceID: "ws-foxctl", Query: feedback.Query, Lane: contextengine.LaneMixed, CreatedAt: now},
		},
	})

	if len(plan.Drafts) != 1 {
		t.Fatalf("expected one draft, got %d", len(plan.Drafts))
	}
	draft := plan.Drafts[0]
	if draft.MemoryKind != string(memorycore.KindSemanticFact) {
		t.Fatalf("MemoryKind=%q want %q", draft.MemoryKind, memorycore.KindSemanticFact)
	}
	if draft.DraftPath == "" || !strings.HasPrefix(draft.DraftPath, "inbox/drafted-from-foxctl/memory/foxctl/2026-05-21/") {
		t.Fatalf("unexpected draft path %q", draft.DraftPath)
	}
	if draft.TargetPath != "notes/memory/foxctl.md" {
		t.Fatalf("TargetPath=%q", draft.TargetPath)
	}
	if !strings.Contains(draft.Content, "status: \"draft\"") {
		t.Fatalf("draft content missing draft status:\n%s", draft.Content)
	}
	if !strings.Contains(draft.Content, "Autonomous memory drafts must stay evidence-only until reviewed.") {
		t.Fatalf("draft content missing correction:\n%s", draft.Content)
	}

	proposal := draft.MemoryProposal()
	if proposal.Kind != PolicyKindMemoryDraft {
		t.Fatalf("proposal.Kind=%q want %q", proposal.Kind, PolicyKindMemoryDraft)
	}
	if proposal.Status != "prepared" || proposal.ApplyStatus != "review_prepared" || !proposal.ReviewRequired {
		t.Fatalf("unexpected proposal state: status=%q apply=%q review=%v", proposal.Status, proposal.ApplyStatus, proposal.ReviewRequired)
	}
	packet, ok := buildStoredPreparedProposalWorkPacket(&proposal)
	if !ok {
		t.Fatal("expected memory draft proposal to produce prepared work packet")
	}
	if packet.Action != "merge_promotion" || packet.DraftPath != draft.DraftPath || packet.TargetPath != draft.TargetPath {
		t.Fatalf("unexpected packet: %#v", packet)
	}
}

func TestRunAutonomousMemoryDraftsCanBlurPlannedDrafts(t *testing.T) {
	now := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	feedback := contextengine.RetrievalFeedback{
		ID:          "fb-blur",
		WorkspaceID: "ws-foxctl",
		EpisodeID:   "ep-blur",
		Kind:        contextengine.RetrievalFeedbackKindGapCreated,
		Query:       "what should durable memory preserve",
		GapStmt:     "Durable memory should preserve review-gated structural lessons.",
		CreatedAt:   now,
	}
	plan := PlanAutonomousMemoryDrafts(AutonomousMemoryDraftInput{
		WorkspaceID: "ws-foxctl",
		Now:         now,
		Feedback:    []contextengine.RetrievalFeedback{feedback},
		Episodes:    []contextengine.RetrievalEpisode{{ID: "ep-blur", WorkspaceID: "ws-foxctl", Query: feedback.Query, CreatedAt: now}},
	})
	report := blurAutonomousMemoryDraftPlan(context.Background(), recordingBlurAgent{}, "recording", &plan)
	if report.Errors != 0 || report.Rejected != 0 || report.DraftsBlurred != 1 {
		t.Fatalf("unexpected blur report: %#v", report)
	}
	draft := plan.Drafts[0]
	if draft.Blur == nil {
		t.Fatal("expected draft blur")
	}
	if !strings.Contains(draft.Content, "## Blurred Mechanism") {
		t.Fatalf("draft content missing blurred mechanism:\n%s", draft.Content)
	}
	if !strings.Contains(draft.Content, "agent_blurred") {
		t.Fatalf("draft frontmatter missing agent_blurred:\n%s", draft.Content)
	}
	proposal := draft.MemoryProposal()
	if proposal.ProposedChange["agent_blurred"] != true {
		t.Fatalf("proposal missing agent_blurred: %#v", proposal.ProposedChange)
	}
}

type recordingBlurAgent struct{}

func (recordingBlurAgent) BlurMemory(context.Context, MemoryBlurAgentPromptInput) (MemoryBlurAgentOutput, string, error) {
	output := MemoryBlurAgentOutput{
		AbstractSchema: "typed feedback signal prepares a review-gated structural memory",
		MechanismTags:  []string{"review_gated_memory"},
		Confidence:     0.84,
		LeakageRisk:    0.02,
	}
	return output, `{"abstract_schema":"typed feedback signal prepares a review-gated structural memory","mechanism_tags":["review_gated_memory"],"confidence":0.84,"leakage_risk":0.02}`, nil
}

func TestPlanAutonomousMemoryDraftsSkipsNonMemoryFeedbackAndDedupes(t *testing.T) {
	now := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	correction := contextengine.RetrievalFeedback{
		ID:             "fb-1",
		WorkspaceID:    "ws",
		EpisodeID:      "ep-1",
		Kind:           contextengine.RetrievalFeedbackKindAnswerCorrected,
		Query:          "query",
		CorrectionStmt: "Corrected fact.",
		CreatedAt:      now,
	}
	duplicate := correction
	duplicate.ID = "fb-duplicate"
	accepted := contextengine.RetrievalFeedback{
		ID:          "fb-accepted",
		WorkspaceID: "ws",
		EpisodeID:   "ep-1",
		Kind:        contextengine.RetrievalFeedbackKindAnswerAccepted,
		Query:       "query",
		CreatedAt:   now,
	}

	plan := PlanAutonomousMemoryDrafts(AutonomousMemoryDraftInput{
		WorkspaceID: "ws",
		Now:         now,
		Feedback:    []contextengine.RetrievalFeedback{correction, duplicate, accepted},
	})

	if len(plan.Drafts) != 1 {
		t.Fatalf("expected one deduped draft, got %d", len(plan.Drafts))
	}
	if plan.Skipped != 2 {
		t.Fatalf("Skipped=%d want 2", plan.Skipped)
	}
}
