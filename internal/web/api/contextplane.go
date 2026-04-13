package api

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/context/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
)

type contextOverviewStats struct {
	ProposalCount        int `json:"proposal_count"`
	ActiveProposalCount  int `json:"active_proposal_count"`
	PreparedMergeCount   int `json:"prepared_merge_count"`
	ClaimedMergeCount    int `json:"claimed_merge_count"`
	EvidenceImportCount  int `json:"evidence_import_count"`
	PromotionDraftCount  int `json:"promotion_draft_count"`
	PromotionMergedCount int `json:"promotion_merged_count"`
}

type contextOverviewResponse struct {
	WorkspacePath        string                           `json:"workspace_path"`
	VaultPath            string                           `json:"vault_path,omitempty"`
	Stats                contextOverviewStats             `json:"stats"`
	NextProposalMerge    *contextplane.MaintenanceTask    `json:"next_proposal_merge,omitempty"`
	ClaimedProposalMerge *contextplane.MaintenanceTask    `json:"claimed_proposal_merge,omitempty"`
	Proposals            []contextplane.MemoryProposal    `json:"proposals,omitempty"`
	EvidenceImports      []contextplane.EvidenceImportRun `json:"evidence_imports,omitempty"`
	PromotionJobs        []contextplane.PromotionJob      `json:"promotion_jobs,omitempty"`
}

type contextProposalMergeRequest struct {
	Workspace  string `json:"workspace,omitempty"`
	VaultName  string `json:"vault_name,omitempty"`
	VaultPath  string `json:"vault_path,omitempty"`
	DraftPath  string `json:"draft_path,omitempty"`
	TargetPath string `json:"target_path,omitempty"`
	Heading    string `json:"heading,omitempty"`
}

func ContextOverviewHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		workspacePath := strings.TrimSpace(r.URL.Query().Get("workspace"))
		if workspacePath == "" {
			workspacePath = strings.TrimSpace(GetCurrentWorkspace())
		}
		if workspacePath == "" {
			httpError(w, http.StatusBadRequest, "workspace is required")
			return
		}
		listLimit := 6
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if parsed, err := parsePositiveInt(raw); err == nil {
				listLimit = parsed
			}
		}
		maintenanceLimit := 50
		if raw := strings.TrimSpace(r.URL.Query().Get("maintenance_limit")); raw != "" {
			if parsed, err := parsePositiveInt(raw); err == nil {
				maintenanceLimit = parsed
			}
		}
		vaultPath := resolveContextVaultPath(strings.TrimSpace(r.URL.Query().Get("vault_path")))
		store := contextplane.NewWorkspaceStore(workspacePath)
		if vaultPath != "" {
			index, err := obsidianindex.Open(r.Context(), cfg.Storage.Root, vaultPath)
			if err != nil {
				log.Error().Err(err).Msg("failed to open obsidian index")
				httpError(w, http.StatusInternalServerError, "failed to open obsidian index")
				return
			}
			defer func() { _ = index.Close() }()
			health, err := index.Health(r.Context())
			if err != nil {
				httpError(w, http.StatusInternalServerError, "health report failed")
				return
			}
			if _, err := store.GenerateMaintenanceTasksWithHealth(r.Context(), maintenanceLimit, &health); err != nil {
				httpError(w, http.StatusInternalServerError, "generate maintenance tasks failed: "+err.Error())
				return
			}
		} else {
			if _, err := store.GenerateMaintenanceTasks(r.Context(), maintenanceLimit); err != nil {
				httpError(w, http.StatusInternalServerError, "generate maintenance tasks failed: "+err.Error())
				return
			}
		}

		proposals, err := store.ListMemoryProposals(r.Context(), 0)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "list proposals failed: "+err.Error())
			return
		}
		evidenceImports, err := store.ListEvidenceImportRuns(0)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "list evidence imports failed: "+err.Error())
			return
		}
		promotionJobs, err := store.ListPromotionJobs(0)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "list promotion jobs failed: "+err.Error())
			return
		}
		nextMerge, err := store.NextProposalMergeTask(r.Context(), maintenanceLimit)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "select proposal merge task failed: "+err.Error())
			return
		}
		claimedMerge, err := store.ClaimedProposalMergeTask(r.Context())
		if err != nil {
			httpError(w, http.StatusInternalServerError, "load claimed proposal merge failed: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, envelope.OK("context.overview", contextOverviewResponse{
			WorkspacePath:        workspacePath,
			VaultPath:            vaultPath,
			Stats:                summarizeContextOverviewStats(proposals, evidenceImports, promotionJobs),
			NextProposalMerge:    nextMerge,
			ClaimedProposalMerge: claimedMerge,
			Proposals:            limitMemoryProposals(proposals, listLimit),
			EvidenceImports:      limitEvidenceImportRuns(evidenceImports, listLimit),
			PromotionJobs:        limitPromotionJobs(promotionJobs, listLimit),
		}))
	}
}

func ContextNextProposalMergeHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodPost:
		default:
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		claim := r.Method == http.MethodPost
		workspacePath := strings.TrimSpace(r.URL.Query().Get("workspace"))
		started := time.Now()
		if claim {
			var body struct {
				Workspace string `json:"workspace,omitempty"`
				VaultPath string `json:"vault_path,omitempty"`
				Limit     int    `json:"limit,omitempty"`
			}
			if err := readJSON(w, r, &body); err != nil {
				httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
				return
			}
			if workspacePath == "" {
				workspacePath = strings.TrimSpace(body.Workspace)
			}
			if strings.TrimSpace(r.URL.Query().Get("vault_path")) == "" && strings.TrimSpace(body.VaultPath) != "" {
				q := r.URL.Query()
				q.Set("vault_path", strings.TrimSpace(body.VaultPath))
				r.URL.RawQuery = q.Encode()
			}
			if body.Limit > 0 && strings.TrimSpace(r.URL.Query().Get("limit")) == "" {
				q := r.URL.Query()
				q.Set("limit", strconv.Itoa(body.Limit))
				r.URL.RawQuery = q.Encode()
			}
		}
		if workspacePath == "" {
			httpError(w, http.StatusBadRequest, "workspace is required")
			return
		}
		limit := 50
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if parsed, err := parsePositiveInt(raw); err == nil {
				limit = parsed
			}
		}
		vaultPath := resolveContextVaultPath(strings.TrimSpace(r.URL.Query().Get("vault_path")))
		store := contextplane.NewWorkspaceStore(workspacePath)
		if vaultPath != "" {
			index, err := obsidianindex.Open(r.Context(), cfg.Storage.Root, vaultPath)
			if err != nil {
				log.Error().Err(err).Msg("failed to open obsidian index")
				httpError(w, http.StatusInternalServerError, "failed to open obsidian index")
				return
			}
			defer func() { _ = index.Close() }()
			health, err := index.Health(r.Context())
			if err != nil {
				httpError(w, http.StatusInternalServerError, "health report failed")
				return
			}
			if _, err := store.GenerateMaintenanceTasksWithHealth(r.Context(), limit, &health); err != nil {
				httpError(w, http.StatusInternalServerError, "generate maintenance tasks failed: "+err.Error())
				return
			}
		} else {
			if _, err := store.GenerateMaintenanceTasks(r.Context(), limit); err != nil {
				httpError(w, http.StatusInternalServerError, "generate maintenance tasks failed: "+err.Error())
				return
			}
		}
		var (
			task *contextplane.MaintenanceTask
			err  error
		)
		if claim {
			task, err = store.ClaimNextProposalMergeTask(r.Context(), limit)
		} else {
			task, err = store.NextProposalMergeTask(r.Context(), limit)
		}
		if err != nil {
			httpError(w, http.StatusInternalServerError, "select proposal merge task failed: "+err.Error())
			return
		}
		found := task != nil
		var packet *contextplane.ProposalWorkPacket
		if task != nil {
			packet = task.WorkPacket
		}
		if claim {
			observability.Emit(r.Context(), observability.NewEvent("web.context.next_proposal_merge").
				WithComponent("web").
				WithCommand("context.next_proposal_merge").
				WithWorkspace(workspacePath).
				WithData("claim", true).
				WithData("found", found).
				Success(time.Since(started)))
		}
		writeJSON(w, http.StatusOK, envelope.OK("context.next_proposal_merge", map[string]any{
			"workspace_path": workspacePath,
			"vault_path":     vaultPath,
			"found":          found,
			"task":           task,
			"work_packet":    packet,
			"claimed":        claim && found,
		}))
	}
}

func ContextProposalReleaseMergeHandler(_ config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		proposalID := strings.TrimPrefix(r.URL.Path, "/api/context/proposals/")
		proposalID = strings.TrimSuffix(proposalID, "/release-merge")
		proposalID = strings.Trim(proposalID, "/")
		if proposalID == "" {
			httpError(w, http.StatusBadRequest, "proposal id is required")
			return
		}
		var body struct {
			Workspace string `json:"workspace,omitempty"`
		}
		if err := readJSON(w, r, &body); err != nil {
			httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
		workspacePath := strings.TrimSpace(body.Workspace)
		if workspacePath == "" {
			httpError(w, http.StatusBadRequest, "workspace is required")
			return
		}
		store := contextplane.NewWorkspaceStore(workspacePath)
		proposal, err := store.ReleaseProposalMergeClaim(r.Context(), proposalID)
		if err != nil {
			httpError(w, classifyContextProposalStatus(err), "release proposal merge failed: "+err.Error())
			return
		}
		observability.Emit(r.Context(), observability.NewEvent("web.context.proposal_release_merge").
			WithComponent("web").
			WithCommand("context.proposal_release_merge").
			WithWorkspace(workspacePath).
			WithData("proposal_id", proposalID).
			Success(0))
		writeJSON(w, http.StatusOK, envelope.OK("context.proposal_release_merge", map[string]any{
			"workspace_path": workspacePath,
			"proposal":       proposal,
		}))
	}
}

func ContextProposalMergeHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		proposalID := strings.TrimPrefix(r.URL.Path, "/api/context/proposals/")
		proposalID = strings.TrimSuffix(proposalID, "/merge")
		proposalID = strings.Trim(proposalID, "/")
		if proposalID == "" {
			httpError(w, http.StatusBadRequest, "proposal id is required")
			return
		}
		var req contextProposalMergeRequest
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
		workspacePath := strings.TrimSpace(req.Workspace)
		if workspacePath == "" {
			httpError(w, http.StatusBadRequest, "workspace is required")
			return
		}
		vaultPath := resolveContextVaultPath(strings.TrimSpace(req.VaultPath))
		if vaultPath == "" {
			httpError(w, http.StatusBadRequest, "vault_path is required")
			return
		}
		store := contextplane.NewWorkspaceStore(workspacePath)
		started := time.Now()
		proposal, merge, packet, err := store.MergeMemoryProposal(r.Context(), strings.TrimSpace(req.VaultName), vaultPath, proposalID, strings.TrimSpace(req.DraftPath), strings.TrimSpace(req.TargetPath), strings.TrimSpace(req.Heading))
		if err != nil {
			log.Error().Err(err).Msg("merge proposal failed")
			httpError(w, classifyContextProposalStatus(err), "merge proposal failed: "+err.Error())
			return
		}
		observability.Emit(r.Context(), observability.NewEvent("web.context.proposal_merge").
			WithComponent("web").
			WithCommand("context.proposal_merge").
			WithWorkspace(workspacePath).
			WithData("proposal_id", proposalID).
			WithData("target_path", packet.TargetPath).
			Success(time.Since(started)))
		writeJSON(w, http.StatusOK, envelope.OK("context.proposal_merge", map[string]any{
			"workspace_path": workspacePath,
			"vault_path":     vaultPath,
			"proposal":       proposal,
			"merge":          merge,
			"work_packet":    packet,
		}))
	}
}

func resolveContextVaultPath(explicit string) string {
	if value := strings.TrimSpace(explicit); value != "" {
		return value
	}
	for _, key := range []string{"AGENTCTL_ACA_VAULT_PATH", "AGENTCTL_OBSIDIAN_VAULT_PATH"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func parsePositiveInt(raw string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("invalid positive integer")
	}
	return value, nil
}

func summarizeContextOverviewStats(
	proposals []contextplane.MemoryProposal,
	evidenceImports []contextplane.EvidenceImportRun,
	promotionJobs []contextplane.PromotionJob,
) contextOverviewStats {
	stats := contextOverviewStats{
		ProposalCount:       len(proposals),
		EvidenceImportCount: len(evidenceImports),
	}
	for _, proposal := range proposals {
		status := strings.ToLower(strings.TrimSpace(proposal.Status))
		applyStatus := strings.ToLower(strings.TrimSpace(proposal.ApplyStatus))
		if status != "rejected" && status != "merged" {
			stats.ActiveProposalCount++
		}
		switch applyStatus {
		case "review_prepared":
			stats.PreparedMergeCount++
		case "review_claimed":
			stats.ClaimedMergeCount++
		}
	}
	for _, job := range promotionJobs {
		switch strings.ToLower(strings.TrimSpace(job.Status)) {
		case "reviewed_merged":
			stats.PromotionMergedCount++
		default:
			stats.PromotionDraftCount++
		}
	}
	return stats
}

func limitMemoryProposals(items []contextplane.MemoryProposal, limit int) []contextplane.MemoryProposal {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func limitEvidenceImportRuns(items []contextplane.EvidenceImportRun, limit int) []contextplane.EvidenceImportRun {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func limitPromotionJobs(items []contextplane.PromotionJob, limit int) []contextplane.PromotionJob {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func classifyContextProposalStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(text, "no proposal found"), strings.Contains(text, "not found"):
		return http.StatusNotFound
	case strings.Contains(text, "not claimed"),
		strings.Contains(text, "not prepared"),
		strings.Contains(text, "does not support direct merge"),
		strings.Contains(text, "has no draft_path"),
		strings.Contains(text, "has no target path"),
		strings.Contains(text, "vault_path is required"):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
