package contextplane

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/memorycore"
	contextstore "github.com/joshka0/foxctl/internal/storage/contextengine"
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

func TestPlanAutonomousMemoryDraftsDedupeIsScopeAware(t *testing.T) {
	now := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	feedback := func(id, taskID, sessionID string) contextengine.RetrievalFeedback {
		return contextengine.RetrievalFeedback{
			ID:             id,
			WorkspaceID:    "ws-foxctl",
			EpisodeID:      "ep-scope",
			Kind:           contextengine.RetrievalFeedbackKindAnswerCorrected,
			Query:          "how should scoped memory drafts work",
			CorrectionStmt: "Scoped memory drafts must remain visible to their task.",
			UsedRefs: []contextengine.EvidenceRef{
				{Type: contextengine.RefTypePath, Ref: "internal/context/contextplane/autonomous_memory_drafts.go"},
				{Type: contextengine.RefTypeTask, Ref: taskID},
				{Type: contextengine.RefTypeSession, Ref: sessionID},
			},
			CreatedAt: now,
		}
	}

	plan := PlanAutonomousMemoryDrafts(AutonomousMemoryDraftInput{
		WorkspaceID:   "ws-foxctl",
		WorkspacePath: "/home/dev/repos/foxctl",
		Now:           now,
		Feedback: []contextengine.RetrievalFeedback{
			feedback("fb-scope-1", "task-1", "session-1"),
			feedback("fb-scope-2", "task-2", "session-2"),
			feedback("fb-scope-1-duplicate", "task-1", "session-1"),
		},
		Episodes: []contextengine.RetrievalEpisode{
			{ID: "ep-scope", WorkspaceID: "ws-foxctl", Query: "how should scoped memory drafts work", Lane: contextengine.LaneMixed, CreatedAt: now},
		},
	})

	if len(plan.Drafts) != 2 || plan.Skipped != 1 {
		t.Fatalf("drafts=%d skipped=%d want 2 drafts and 1 same-scope duplicate", len(plan.Drafts), plan.Skipped)
	}
	if plan.Drafts[0].DedupeKey == plan.Drafts[1].DedupeKey {
		t.Fatalf("different task/session scopes shared dedupe key %q", plan.Drafts[0].DedupeKey)
	}
	firstClaim := plan.Drafts[0].MemoryClaim()
	secondClaim := plan.Drafts[1].MemoryClaim()
	if firstClaim.ID == secondClaim.ID {
		t.Fatalf("different task/session scopes shared claim id %q", firstClaim.ID)
	}
	if firstClaim.Scope.TaskID != "task-1" || firstClaim.Scope.SessionID != "session-1" {
		t.Fatalf("first claim scope=%#v", firstClaim.Scope)
	}
	if secondClaim.Scope.TaskID != "task-2" || secondClaim.Scope.SessionID != "session-2" {
		t.Fatalf("second claim scope=%#v", secondClaim.Scope)
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

func TestRunAutonomousMemoryDraftsRecordsContextEngineClaim(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC)
	storageRoot := t.TempDir()
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	vaultPath := filepath.Join(t.TempDir(), "vault")

	ctxStore, err := contextstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("open contextengine store: %v", err)
	}
	episode := contextengine.RetrievalEpisode{
		ID:          "ep-apply",
		WorkspaceID: "ws-foxctl",
		Query:       "how should retrieval feedback become durable memory",
		Lane:        contextengine.LaneMixed,
		PackID:      "pack-apply",
		CreatedAt:   now,
	}
	if _, err := ctxStore.RecordRetrievalEpisode(ctx, episode); err != nil {
		t.Fatalf("record episode: %v", err)
	}
	feedback := contextengine.RetrievalFeedback{
		ID:             "fb-apply",
		WorkspaceID:    "ws-foxctl",
		EpisodeID:      episode.ID,
		Kind:           contextengine.RetrievalFeedbackKindAnswerCorrected,
		Query:          episode.Query,
		CorrectionStmt: "Applied memory drafts should create candidate context claims.",
		UsedRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypePath, Ref: "internal/context/contextplane/autonomous_memory_drafts.go"},
			{Type: contextengine.RefTypeTask, Ref: "task-apply"},
			{Type: contextengine.RefTypeSession, Ref: "session-apply"},
		},
		CreatedAt: now,
	}
	if _, err := ctxStore.RecordRetrievalFeedback(ctx, feedback); err != nil {
		t.Fatalf("record feedback: %v", err)
	}
	if err := ctxStore.Close(); err != nil {
		t.Fatalf("close seeded contextengine store: %v", err)
	}

	report, err := RunAutonomousMemoryDrafts(ctx, AutonomousMemoryDraftRunOptions{
		StorageRoot:   storageRoot,
		WorkspaceID:   "ws-foxctl",
		WorkspacePath: workspacePath,
		VaultPath:     vaultPath,
		Now:           now.Add(time.Minute),
		Lookback:      time.Hour,
		Limit:         5,
		ApplyDrafts:   true,
		DryRun:        false,
	})
	if err != nil {
		t.Fatalf("RunAutonomousMemoryDrafts: %v", err)
	}
	if report.Errors != 0 || report.DraftsWritten != 1 || report.ProposalsRecorded != 1 || report.ClaimsRecorded != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if _, err := os.Stat(filepath.Join(vaultPath, filepath.FromSlash(report.DraftPaths[0]))); err != nil {
		t.Fatalf("expected draft note to be written: %v", err)
	}

	proposals, err := NewWorkspaceStore(workspacePath).ListMemoryProposals(ctx, 10)
	if err != nil {
		t.Fatalf("ListMemoryProposals: %v", err)
	}
	if len(proposals) != 1 {
		t.Fatalf("proposals=%d want 1", len(proposals))
	}

	ctxStore, err = contextstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("reopen contextengine store: %v", err)
	}
	claims, err := ctxStore.ListClaims(ctx, contextstore.ClaimFilter{
		WorkspaceID: "ws-foxctl",
		Status:      contextengine.ClaimStatusCandidate,
	})
	if err != nil {
		t.Fatalf("ListClaims: %v", err)
	}
	if len(claims) != 1 {
		t.Fatalf("claims=%d want 1", len(claims))
	}
	claim := claims[0]
	if claim.ClaimType != string(memorycore.KindSemanticFact) {
		t.Fatalf("claim type=%q want %q", claim.ClaimType, memorycore.KindSemanticFact)
	}
	if claim.Summary != "Retrieval correction: Applied memory drafts should create candidate context claims." {
		t.Fatalf("claim summary=%q", claim.Summary)
	}
	if claim.SourceEventID != "retrieval_feedback:fb-apply" {
		t.Fatalf("source event=%q", claim.SourceEventID)
	}
	if claim.Scope.Path != "internal/context/contextplane/autonomous_memory_drafts.go" ||
		claim.Scope.TaskID != "task-apply" ||
		claim.Scope.SessionID != "session-apply" {
		t.Fatalf("claim scope=%#v", claim.Scope)
	}
	if len(claim.SourceRefs) != 5 {
		t.Fatalf("source refs=%#v want feedback, episode, and used evidence", claim.SourceRefs)
	}

	promoted := claim
	promoted.Status = contextengine.ClaimStatusCurrent
	promoted.Reason = "reviewed by memory curator"
	if _, err := ctxStore.UpsertClaim(ctx, promoted); err != nil {
		t.Fatalf("promote claim: %v", err)
	}
	if err := ctxStore.Close(); err != nil {
		t.Fatalf("close contextengine store before rerun: %v", err)
	}

	secondReport, err := RunAutonomousMemoryDrafts(ctx, AutonomousMemoryDraftRunOptions{
		StorageRoot:   storageRoot,
		WorkspaceID:   "ws-foxctl",
		WorkspacePath: workspacePath,
		VaultPath:     vaultPath,
		Now:           now.Add(time.Minute),
		Lookback:      time.Hour,
		Limit:         5,
		ApplyDrafts:   true,
		DryRun:        false,
	})
	if err != nil {
		t.Fatalf("second RunAutonomousMemoryDrafts: %v", err)
	}
	if secondReport.Errors != 0 || secondReport.ClaimsRecorded != 0 {
		t.Fatalf("unexpected second report: %#v", secondReport)
	}

	ctxStore, err = contextstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("reopen contextengine store after rerun: %v", err)
	}
	defer ctxStore.Close()
	got, err := ctxStore.GetClaim(ctx, claim.ID)
	if err != nil {
		t.Fatalf("GetClaim after rerun: %v", err)
	}
	if got.Status != contextengine.ClaimStatusCurrent || got.Reason != "reviewed by memory curator" {
		t.Fatalf("rerun changed reviewed claim: status=%q reason=%q", got.Status, got.Reason)
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
