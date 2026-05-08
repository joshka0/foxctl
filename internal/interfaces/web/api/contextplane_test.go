package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestContextNextProposalMergeHandlerAndRelease(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Config{Storage: config.StorageSettings{Root: t.TempDir()}}
	store := contextplane.NewWorkspaceStore(workspace)
	proposal, err := store.RecordMemoryProposal(context.Background(), contextplane.MemoryProposal{
		DedupeKey:      "external_evidence_import|aca-vocabulary-api",
		Kind:           "external_evidence_import",
		Classification: "external_evidence",
		Status:         "prepared",
		ReviewRequired: true,
		Confidence:     0.72,
		BlastRadius:    "medium",
		Summary:        "Review imported evidence draft for merge consideration: ACA Vocabulary Review. Suggested target: notes/repo/aca-inspect/semantic-and-memory.md.",
		ProposedChange: map[string]any{
			"draft_path":                 "inbox/drafted-from-foxctl/external-evidence/aca-inspect/aca-vocabulary-review.md",
			"suggested_target_note_path": "notes/repo/aca-inspect/semantic-and-memory.md",
			"suggested_target_heading":   "Review",
		},
		EvaluationStatus: "accepted",
		ApplyStatus:      "review_prepared",
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("RecordMemoryProposal: %v", err)
	}

	nextHandler := ContextNextProposalMergeHandler(cfg, zerolog.Nop())
	getReq := httptest.NewRequest(http.MethodGet, "/api/context/next-proposal-merge?workspace="+workspace, nil)
	getRR := httptest.NewRecorder()
	nextHandler(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", getRR.Code, getRR.Body.String())
	}
	var getEnv envelope.Envelope
	if err := json.Unmarshal(getRR.Body.Bytes(), &getEnv); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	getData, ok := getEnv.Data.(map[string]any)
	if !ok || getData["found"] != true {
		t.Fatalf("unexpected data=%v", getEnv.Data)
	}

	claimReq := httptest.NewRequest(http.MethodPost, "/api/context/next-proposal-merge/claim?workspace="+workspace, strings.NewReader(`{}`))
	claimRR := httptest.NewRecorder()
	nextHandler(claimRR, claimReq)
	if claimRR.Code != http.StatusOK {
		t.Fatalf("claim status=%d body=%s", claimRR.Code, claimRR.Body.String())
	}
	var claimEnv envelope.Envelope
	if err := json.Unmarshal(claimRR.Body.Bytes(), &claimEnv); err != nil {
		t.Fatalf("decode claim envelope: %v", err)
	}
	claimData, ok := claimEnv.Data.(map[string]any)
	if !ok || claimData["claimed"] != true {
		t.Fatalf("unexpected claim data=%v", claimEnv.Data)
	}

	releaseHandler := ContextProposalReleaseMergeHandler(cfg, zerolog.Nop())
	releaseReq := httptest.NewRequest(http.MethodPost, "/api/context/proposals/"+proposal.ID+"/release-merge", strings.NewReader(`{"workspace":"`+workspace+`"}`))
	releaseRR := httptest.NewRecorder()
	releaseHandler(releaseRR, releaseReq)
	if releaseRR.Code != http.StatusOK {
		t.Fatalf("release status=%d body=%s", releaseRR.Code, releaseRR.Body.String())
	}
	var releaseEnv envelope.Envelope
	if err := json.Unmarshal(releaseRR.Body.Bytes(), &releaseEnv); err != nil {
		t.Fatalf("decode release envelope: %v", err)
	}
	releaseData, ok := releaseEnv.Data.(map[string]any)
	if !ok {
		t.Fatalf("release data=%T", releaseEnv.Data)
	}
	releasedProposal, ok := releaseData["proposal"].(map[string]any)
	if !ok || releasedProposal["apply_status"] != "review_prepared" {
		t.Fatalf("released proposal=%v", releaseData["proposal"])
	}
}

func TestContextOverviewHandler(t *testing.T) {
	workspace := t.TempDir()
	vaultPath := t.TempDir()
	cfg := config.Config{Storage: config.StorageSettings{Root: t.TempDir()}}
	store := contextplane.NewWorkspaceStore(workspace)
	targetNotePath := filepath.Join(vaultPath, "notes", "repo", "foxctl", "platform-and-web.md")
	if err := os.MkdirAll(filepath.Dir(targetNotePath), 0o755); err != nil {
		t.Fatalf("mkdir target note: %v", err)
	}
	if err := os.WriteFile(targetNotePath, []byte(`---
title: ACA operator rail for platform and web
trust: canonical
type: map
---

This note covers the ACA operator surface, workspace-aware proposal routing,
reviewed merge actions, and GUI control-plane integration.
`), 0o644); err != nil {
		t.Fatalf("write target note: %v", err)
	}

	imported, err := store.ImportEvidence(context.Background(), cfg.Storage.Root, vaultPath, contextplane.EvidenceImportInput{
		Title:      "ACA GUI operator notes",
		SourceKind: "transcript",
		SourceRef:  "team-retro",
		Content:    "ACA needs an explicit operator surface with workspace-aware proposal routing and reviewed merge actions.",
	})
	if err != nil {
		t.Fatalf("ImportEvidence: %v", err)
	}
	if _, _, _, err := store.ApplyMemoryProposal(context.Background(), imported.Proposal.ID); err != nil {
		t.Fatalf("ApplyMemoryProposal(imported): %v", err)
	}
	if _, err := store.GenerateMaintenanceTasks(context.Background(), 50); err != nil {
		t.Fatalf("GenerateMaintenanceTasks: %v", err)
	}
	if _, err := store.ClaimNextProposalMergeTask(context.Background(), 50); err != nil {
		t.Fatalf("ClaimNextProposalMergeTask: %v", err)
	}
	if _, err := store.RecordMemoryProposal(context.Background(), contextplane.MemoryProposal{
		DedupeKey:      "external_evidence_import|aca-gui-pending",
		Kind:           "external_evidence_import",
		Classification: "external_evidence",
		Status:         "prepared",
		ReviewRequired: true,
		Confidence:     0.66,
		BlastRadius:    "medium",
		Summary:        "Review imported ACA GUI evidence for merge consideration.",
		ProposedChange: map[string]any{
			"draft_path":                 "inbox/drafted-from-foxctl/external-evidence/foxctl/aca-gui-pending.md",
			"suggested_target_note_path": "notes/repo/foxctl/platform-and-web.md",
			"suggested_target_heading":   "ACA GUI",
		},
		EvaluationStatus: "accepted",
		ApplyStatus:      "review_prepared",
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RecordMemoryProposal: %v", err)
	}

	handler := ContextOverviewHandler(cfg, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/context/overview?workspace="+workspace, nil)
	rr := httptest.NewRecorder()
	handler(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var env envelope.Envelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected data=%T", env.Data)
	}
	stats, ok := data["stats"].(map[string]any)
	if !ok {
		t.Fatalf("missing stats=%v", data["stats"])
	}
	if stats["evidence_import_count"] != float64(1) {
		t.Fatalf("unexpected evidence_import_count=%v", stats["evidence_import_count"])
	}
	if stats["promotion_draft_count"] != float64(1) {
		t.Fatalf("unexpected promotion_draft_count=%v", stats["promotion_draft_count"])
	}
	if stats["claimed_merge_count"] != float64(1) {
		t.Fatalf("unexpected claimed_merge_count=%v", stats["claimed_merge_count"])
	}
	if stats["prepared_merge_count"] != float64(1) {
		t.Fatalf("unexpected prepared_merge_count=%v", stats["prepared_merge_count"])
	}
	if data["claimed_proposal_merge"] == nil {
		t.Fatalf("expected claimed_proposal_merge in overview")
	}
	if data["next_proposal_merge"] == nil {
		t.Fatalf("expected next_proposal_merge in overview")
	}
	proposals, ok := data["proposals"].([]any)
	if !ok || len(proposals) < 2 {
		t.Fatalf("unexpected proposals=%v", data["proposals"])
	}
	imports, ok := data["evidence_imports"].([]any)
	if !ok || len(imports) != 1 {
		t.Fatalf("unexpected evidence_imports=%v", data["evidence_imports"])
	}
	jobs, ok := data["promotion_jobs"].([]any)
	if !ok || len(jobs) != 1 {
		t.Fatalf("unexpected promotion_jobs=%v", data["promotion_jobs"])
	}
}

func TestContextControlProposalsListFiltersDerivedStatus(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Config{Storage: config.StorageSettings{Root: t.TempDir()}}
	store := contextplane.NewWorkspaceStore(workspace)
	ctx := context.Background()

	openProposal, err := store.RecordControlProposal(ctx, contextplane.ControlProposal{
		DedupeKey:   "task:open",
		Kind:        contextplane.ProposalKindTaskProposal,
		Status:      contextplane.ProposalStatusOpen,
		WorkspaceID: workspace,
		Summary:     "Open proposal",
		EvidenceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypeEvent, Ref: "hook:open"},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("RecordControlProposal(open): %v", err)
	}

	approvedProposal, err := store.RecordControlProposal(ctx, contextplane.ControlProposal{
		DedupeKey:   "task:approved",
		Kind:        contextplane.ProposalKindTaskProposal,
		Status:      contextplane.ProposalStatusOpen,
		WorkspaceID: workspace,
		Summary:     "Approved proposal",
		EvidenceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypeEvent, Ref: "hook:approved"},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("RecordControlProposal(approved): %v", err)
	}
	if _, err := store.RecordCoordinatorDecision(ctx, contextplane.CoordinatorDecision{
		ProposalID:    approvedProposal.ID,
		WorkspaceID:   workspace,
		Decision:      contextplane.DecisionKindApprove,
		AuthorityMode: contextplane.AuthorityModeHumanApproval,
		ApprovalActor: "human:test",
		EvidenceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypeEvent, Ref: "decision:approved"},
		},
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RecordCoordinatorDecision: %v", err)
	}

	handler := ContextControlProposalsHandler(cfg, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/context/control-proposals?workspace="+url.QueryEscape(workspace), nil)
	rr := httptest.NewRecorder()
	handler(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	allStates := decodeControlProposalStateList(t, rr.Body.Bytes())
	if len(allStates) != 2 {
		t.Fatalf("state count=%d want 2", len(allStates))
	}

	filterReq := httptest.NewRequest(http.MethodGet, "/api/context/control-proposals?workspace="+url.QueryEscape(workspace)+"&status=approved", nil)
	filterRR := httptest.NewRecorder()
	handler(filterRR, filterReq)
	if filterRR.Code != http.StatusOK {
		t.Fatalf("filter status=%d body=%s", filterRR.Code, filterRR.Body.String())
	}
	filtered := decodeControlProposalStateList(t, filterRR.Body.Bytes())
	if len(filtered) != 1 {
		t.Fatalf("filtered count=%d want 1", len(filtered))
	}
	if filtered[0].Proposal.ID != approvedProposal.ID {
		t.Fatalf("filtered proposal id=%q want %q", filtered[0].Proposal.ID, approvedProposal.ID)
	}
	if filtered[0].DerivedStatus != contextplane.ProposalStatusApproved {
		t.Fatalf("derived status=%q want approved", filtered[0].DerivedStatus)
	}
	if filtered[0].Proposal.ID == openProposal.ID && filtered[0].DerivedStatus == contextplane.ProposalStatusOpen {
		t.Fatalf("expected approved proposal, got open proposal")
	}
}

func TestContextControlProposalInspectReturnsNotFound(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Config{Storage: config.StorageSettings{Root: t.TempDir()}}
	handler := ContextControlProposalsHandler(cfg, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/context/control-proposals/missing-id?workspace="+url.QueryEscape(workspace), nil)
	rr := httptest.NewRecorder()
	handler(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestContextControlProposalsRequireExplicitWorkspace(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Config{Storage: config.StorageSettings{Root: t.TempDir()}}
	store := contextplane.NewWorkspaceStore(workspace)
	proposal, err := store.RecordControlProposal(context.Background(), contextplane.ControlProposal{
		DedupeKey:   "task:workspace-required",
		Kind:        contextplane.ProposalKindTaskProposal,
		Status:      contextplane.ProposalStatusOpen,
		WorkspaceID: workspace,
		Summary:     "Workspace required proposal",
		EvidenceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypeEvent, Ref: "hook:workspace"},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("RecordControlProposal: %v", err)
	}

	handler := ContextControlProposalsHandler(cfg, zerolog.Nop())
	cases := []struct {
		name   string
		method string
		target string
		body   string
	}{
		{name: "list", method: http.MethodGet, target: "/api/context/control-proposals"},
		{name: "inspect", method: http.MethodGet, target: "/api/context/control-proposals/" + proposal.ID},
		{
			name:   "decision",
			method: http.MethodPost,
			target: "/api/context/control-proposals/" + proposal.ID + "/decisions",
			body:   `{"decision":"approve","evidence_refs":[{"type":"event","ref":"decision:workspace"}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			rr := httptest.NewRecorder()
			handler(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
			}
		})
	}
}

func TestContextControlProposalDecisionAppendsAndUpdatesLatestState(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Config{Storage: config.StorageSettings{Root: t.TempDir()}}
	store := contextplane.NewWorkspaceStore(workspace)
	ctx := context.Background()

	proposal, err := store.RecordControlProposal(ctx, contextplane.ControlProposal{
		DedupeKey:   "task:append-decision",
		Kind:        contextplane.ProposalKindTaskProposal,
		Status:      contextplane.ProposalStatusOpen,
		WorkspaceID: workspace,
		Summary:     "Append decision proposal",
		EvidenceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypeEvent, Ref: "hook:proposal"},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("RecordControlProposal: %v", err)
	}

	handler := ContextControlProposalsHandler(cfg, zerolog.Nop())
	firstBody := `{"workspace":"` + workspace + `","decision":"approve","approval_actor":"pi:coordinator","reason":"safe to proceed","evidence_refs":[{"type":"event","ref":"decision:1"}]}`
	firstReq := httptest.NewRequest(http.MethodPost, "/api/context/control-proposals/"+proposal.ID+"/decisions", strings.NewReader(firstBody))
	firstRR := httptest.NewRecorder()
	handler(firstRR, firstReq)
	if firstRR.Code != http.StatusOK {
		t.Fatalf("first post status=%d body=%s", firstRR.Code, firstRR.Body.String())
	}
	firstResp := decodeControlProposalDecisionResponse(t, firstRR.Body.Bytes())
	if firstResp.Decision.AuthorityMode != contextplane.AuthorityModeHumanApproval {
		t.Fatalf("authority mode=%q want human_approval", firstResp.Decision.AuthorityMode)
	}
	if firstResp.State.DerivedStatus != contextplane.ProposalStatusApproved {
		t.Fatalf("derived status after first=%q want approved", firstResp.State.DerivedStatus)
	}

	secondBody := `{"workspace":"` + workspace + `","decision":"defer","approval_actor":"human:reviewer","reason":"need more context","evidence_refs":[{"type":"event","ref":"decision:2"}]}`
	secondReq := httptest.NewRequest(http.MethodPost, "/api/context/control-proposals/"+proposal.ID+"/decisions", strings.NewReader(secondBody))
	secondRR := httptest.NewRecorder()
	handler(secondRR, secondReq)
	if secondRR.Code != http.StatusOK {
		t.Fatalf("second post status=%d body=%s", secondRR.Code, secondRR.Body.String())
	}
	secondResp := decodeControlProposalDecisionResponse(t, secondRR.Body.Bytes())
	if secondResp.State.DerivedStatus != contextplane.ProposalStatusEvaluating {
		t.Fatalf("derived status after second=%q want evaluating", secondResp.State.DerivedStatus)
	}
	if secondResp.State.LatestDecision == nil || secondResp.State.LatestDecision.Decision != contextplane.DecisionKindDefer {
		t.Fatalf("latest decision=%+v want defer", secondResp.State.LatestDecision)
	}

	decisions, err := store.ListCoordinatorDecisions(ctx, proposal.ID, 0)
	if err != nil {
		t.Fatalf("ListCoordinatorDecisions: %v", err)
	}
	if len(decisions) != 2 {
		t.Fatalf("decision count=%d want 2", len(decisions))
	}
	if decisions[0].Decision != contextplane.DecisionKindDefer || decisions[1].Decision != contextplane.DecisionKindApprove {
		t.Fatalf("decision order=%+v", decisions)
	}
}

func TestContextControlProposalDecisionRequiresEvidenceRefs(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Config{Storage: config.StorageSettings{Root: t.TempDir()}}
	store := contextplane.NewWorkspaceStore(workspace)
	ctx := context.Background()

	proposal, err := store.RecordControlProposal(ctx, contextplane.ControlProposal{
		DedupeKey:   "task:missing-evidence",
		Kind:        contextplane.ProposalKindTaskProposal,
		Status:      contextplane.ProposalStatusOpen,
		WorkspaceID: workspace,
		Summary:     "Evidence required proposal",
		EvidenceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypeEvent, Ref: "hook:proposal"},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("RecordControlProposal: %v", err)
	}

	handler := ContextControlProposalsHandler(cfg, zerolog.Nop())
	req := httptest.NewRequest(http.MethodPost, "/api/context/control-proposals/"+proposal.ID+"/decisions", strings.NewReader(`{"workspace":"`+workspace+`","decision":"approve"}`))
	rr := httptest.NewRecorder()
	handler(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestContextControlProposalDecisionRejectsInvalidEvidenceRefs(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Config{Storage: config.StorageSettings{Root: t.TempDir()}}
	store := contextplane.NewWorkspaceStore(workspace)
	ctx := context.Background()

	proposal, err := store.RecordControlProposal(ctx, contextplane.ControlProposal{
		DedupeKey:   "task:invalid-evidence",
		Kind:        contextplane.ProposalKindTaskProposal,
		Status:      contextplane.ProposalStatusOpen,
		WorkspaceID: workspace,
		Summary:     "Invalid evidence proposal",
		EvidenceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypeEvent, Ref: "hook:proposal"},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("RecordControlProposal: %v", err)
	}

	handler := ContextControlProposalsHandler(cfg, zerolog.Nop())
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/context/control-proposals/"+proposal.ID+"/decisions",
		strings.NewReader(`{"workspace":"`+workspace+`","decision":"approve","evidence_refs":[{"type":"","ref":""}]}`),
	)
	rr := httptest.NewRecorder()
	handler(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestContextControlProposalDecisionDoesNotCreateApplyResults(t *testing.T) {
	workspace := t.TempDir()
	cfg := config.Config{Storage: config.StorageSettings{Root: t.TempDir()}}
	store := contextplane.NewWorkspaceStore(workspace)
	ctx := context.Background()

	proposal, err := store.RecordControlProposal(ctx, contextplane.ControlProposal{
		DedupeKey:   "task:no-apply",
		Kind:        contextplane.ProposalKindTaskProposal,
		Status:      contextplane.ProposalStatusOpen,
		WorkspaceID: workspace,
		Summary:     "No apply side effect proposal",
		EvidenceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypeEvent, Ref: "hook:proposal"},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("RecordControlProposal: %v", err)
	}

	handler := ContextControlProposalsHandler(cfg, zerolog.Nop())
	body := `{"workspace":"` + workspace + `","decision":"approve","approval_actor":"pi:coordinator","reason":"approved","evidence_refs":[{"type":"event","ref":"decision:no-apply"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/context/control-proposals/"+proposal.ID+"/decisions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handler(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	state, err := store.GetControlProposalState(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("GetControlProposalState: %v", err)
	}
	if state == nil {
		t.Fatalf("state is nil")
	}
	if state.LatestApplyResult != nil {
		t.Fatalf("expected no apply result, got %+v", state.LatestApplyResult)
	}
}

func decodeControlProposalStateList(t *testing.T, body []byte) []contextplane.ControlProposalState {
	t.Helper()
	var env envelope.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	raw, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatalf("marshal envelope data: %v", err)
	}
	var states []contextplane.ControlProposalState
	if err := json.Unmarshal(raw, &states); err != nil {
		t.Fatalf("decode states: %v", err)
	}
	return states
}

type contextControlProposalDecisionResponse struct {
	Decision contextplane.CoordinatorDecision  `json:"decision"`
	State    contextplane.ControlProposalState `json:"state"`
}

func decodeControlProposalDecisionResponse(t *testing.T, body []byte) contextControlProposalDecisionResponse {
	t.Helper()
	var env envelope.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	raw, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatalf("marshal envelope data: %v", err)
	}
	var data contextControlProposalDecisionResponse
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("decode decision response: %v", err)
	}
	return data
}
