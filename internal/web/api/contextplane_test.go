package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
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
			"draft_path":                 "inbox/drafted-from-agentctl/external-evidence/aca-inspect/aca-vocabulary-review.md",
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
	targetNotePath := filepath.Join(vaultPath, "notes", "repo", "agentctl", "platform-and-web.md")
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
			"draft_path":                 "inbox/drafted-from-agentctl/external-evidence/agentctl/aca-gui-pending.md",
			"suggested_target_note_path": "notes/repo/agentctl/platform-and-web.md",
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
