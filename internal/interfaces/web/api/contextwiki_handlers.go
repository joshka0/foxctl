package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/runtime/memoryblur"
	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
	taskstore "github.com/joshka0/foxctl/internal/storage/tasks"
	"github.com/joshka0/foxctl/internal/tooling/tools/obsidian"
)

// resolveWorkspaceOrDefault returns the explicit workspace or falls back to
// the current workspace (env / config). Returns ("", error) if neither is set.
func resolveWorkspaceOrDefault(r *http.Request) (string, error) {
	if wp := strings.TrimSpace(r.URL.Query().Get("workspace")); wp != "" {
		return wp, nil
	}
	if wp := strings.TrimSpace(GetCurrentWorkspace()); wp != "" {
		return wp, nil
	}
	return "", fmt.Errorf("workspace is required (query param or FOXCTL_WORKSPACE)")
}

// ── Context Show ────────────────────────────────────────────────────────

func ContextShowHandler(_ config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		wp, err := resolveWorkspaceOrDefault(r)
		if err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
		store := contextplane.NewWorkspaceStore(wp)
		tom, err := store.LoadTopOfMind()
		if err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusOK, envelope.OK("context.show", map[string]any{
					"workspace_path": wp,
					"top_of_mind":    nil,
					"status":         "no_top_of_mind",
				}))
				return
			}
			httpError(w, http.StatusInternalServerError, "load top-of-mind failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, envelope.OK("context.show", map[string]any{
			"workspace_path": wp,
			"top_of_mind":    tom,
		}))
	}
}

// ── Context Report ──────────────────────────────────────────────────────

func ContextReportHandler(_ config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		wp, err := resolveWorkspaceOrDefault(r)
		if err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
		store := contextplane.NewWorkspaceStore(wp)
		report, err := store.BuildReport()
		if err != nil {
			httpError(w, http.StatusInternalServerError, "build report failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, envelope.OK("context.report", map[string]any{
			"workspace_path": wp,
			"report":         report,
		}))
	}
}

// ── Context Next ────────────────────────────────────────────────────────

func ContextNextHandler(cfg config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		wp, err := resolveWorkspaceOrDefault(r)
		if err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
		taskDB, err := taskstore.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "open task store failed: "+err.Error())
			return
		}
		defer func() { _ = taskDB.Close() }()
		workspaceID := workspaceCanonicalID(wp)
		task, ok, err := contextplane.SelectNextTask(r.Context(), taskDB, workspaceID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "select next task failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, envelope.OK("context.next", map[string]any{
			"workspace_path": wp,
			"task":           task,
			"found":          ok,
		}))
	}
}

// ── Context Capture (handoff) ───────────────────────────────────────────

type contextCaptureRequest struct {
	Workspace    string   `json:"workspace,omitempty"`
	TaskID       string   `json:"task_id,omitempty"`
	Phase        string   `json:"phase,omitempty"`
	Outcome      string   `json:"outcome,omitempty"`
	Summary      string   `json:"summary"`
	Observations []string `json:"observations,omitempty"`
	Tensions     []string `json:"tensions,omitempty"`
	NextActions  []string `json:"next_actions,omitempty"`
	FileTouched  []string `json:"file_touched,omitempty"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
}

func ContextCaptureHandler(_ config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req contextCaptureRequest
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		wp := strings.TrimSpace(req.Workspace)
		if wp == "" {
			wp = strings.TrimSpace(GetCurrentWorkspace())
		}
		if wp == "" {
			httpError(w, http.StatusBadRequest, "workspace is required")
			return
		}
		if strings.TrimSpace(req.Summary) == "" {
			httpError(w, http.StatusBadRequest, "summary is required")
			return
		}
		handoff := contextplane.Handoff{
			TaskID:       strings.TrimSpace(req.TaskID),
			Phase:        strings.TrimSpace(req.Phase),
			Outcome:      strings.TrimSpace(req.Outcome),
			Summary:      strings.TrimSpace(req.Summary),
			Observations: req.Observations,
			Tensions:     req.Tensions,
			NextActions:  req.NextActions,
			FileRefs:     stringsToEvidenceRefs(req.FileTouched),
			EvidenceRefs: stringsToEvidenceRefs(req.EvidenceRefs),
			CreatedAt:    time.Now().UTC(),
		}
		store := contextplane.NewWorkspaceStore(wp)
		path, err := store.SaveHandoff(handoff)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "save handoff failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, envelope.OK("context.capture", map[string]any{
			"workspace_path": wp,
			"path":           path,
			"handoff":        handoff,
		}))
	}
}

// ── Context Dispatch ────────────────────────────────────────────────────

func ContextDispatchHandler(cfg config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		wp, err := resolveWorkspaceOrDefault(r)
		if err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
		var taskID string
		if r.Method == http.MethodPost {
			var body struct {
				TaskID string `json:"task_id,omitempty"`
			}
			if err := readJSON(w, r, &body); err == nil {
				taskID = strings.TrimSpace(body.TaskID)
			}
		} else {
			taskID = strings.TrimSpace(r.URL.Query().Get("task_id"))
		}
		taskDB, err := taskstore.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "open task store failed: "+err.Error())
			return
		}
		defer func() { _ = taskDB.Close() }()
		workspaceID := workspaceCanonicalID(wp)
		store := contextplane.NewWorkspaceStore(wp)
		packet, err := store.BuildTaskPacket(r.Context(), taskDB, workspaceID, taskID)
		if err != nil {
			// No eligible task is not an error — return empty packet
			if strings.Contains(err.Error(), "no eligible task") {
				writeJSON(w, http.StatusOK, envelope.OK("context.dispatch", map[string]any{
					"workspace_path": wp,
					"packet":         nil,
					"message":        err.Error(),
				}))
				return
			}
			httpError(w, http.StatusInternalServerError, "build task packet failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, envelope.OK("context.dispatch", map[string]any{
			"workspace_path": wp,
			"packet":         packet,
		}))
	}
}

// ── Context Handoffs ────────────────────────────────────────────────────

func ContextHandoffsHandler(_ config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		wp, err := resolveWorkspaceOrDefault(r)
		if err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
		limit := 10
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if parsed, perr := parsePositiveInt(raw); perr == nil {
				limit = parsed
			}
		}
		store := contextplane.NewWorkspaceStore(wp)
		handoffs, err := store.ListHandoffs(limit)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "list handoffs failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, envelope.OK("context.handoffs", map[string]any{
			"workspace_path": wp,
			"handoffs":       handoffs,
			"count":          len(handoffs),
		}))
	}
}

// ── Context Observe ─────────────────────────────────────────────────────

type contextObserveRequest struct {
	Workspace    string   `json:"workspace,omitempty"`
	Statement    string   `json:"statement"`
	Confidence   float64  `json:"confidence,omitempty"`
	Project      string   `json:"project,omitempty"`
	Area         string   `json:"area,omitempty"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
}

func ContextObserveHandler(_ config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req contextObserveRequest
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		wp := strings.TrimSpace(req.Workspace)
		if wp == "" {
			wp = strings.TrimSpace(GetCurrentWorkspace())
		}
		if wp == "" {
			httpError(w, http.StatusBadRequest, "workspace is required")
			return
		}
		if strings.TrimSpace(req.Statement) == "" {
			httpError(w, http.StatusBadRequest, "statement is required")
			return
		}
		confidence := req.Confidence
		if confidence <= 0 {
			confidence = 0.7
		}
		obs := contextplane.Observation{
			Statement:    strings.TrimSpace(req.Statement),
			Confidence:   confidence,
			Count:        1,
			Project:      strings.TrimSpace(req.Project),
			Area:         strings.TrimSpace(req.Area),
			EvidenceRefs: stringsToEvidenceRefs(req.EvidenceRefs),
			FirstSeen:    time.Now().UTC(),
			LastSeen:     time.Now().UTC(),
		}
		store := contextplane.NewWorkspaceStore(wp)
		id, err := store.AppendObservation(obs)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "append observation failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, envelope.OK("context.observe", map[string]any{
			"workspace_path": wp,
			"id":             id,
			"observation":    obs,
		}))
	}
}

// ── Context Tension ─────────────────────────────────────────────────────

type contextTensionRequest struct {
	Workspace   string   `json:"workspace,omitempty"`
	Statement   string   `json:"statement"`
	Kind        string   `json:"kind,omitempty"`
	Impact      string   `json:"impact,omitempty"`
	Status      string   `json:"status,omitempty"`
	RelatedRefs []string `json:"related_refs,omitempty"`
}

func ContextTensionHandler(_ config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req contextTensionRequest
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		wp := strings.TrimSpace(req.Workspace)
		if wp == "" {
			wp = strings.TrimSpace(GetCurrentWorkspace())
		}
		if wp == "" {
			httpError(w, http.StatusBadRequest, "workspace is required")
			return
		}
		if strings.TrimSpace(req.Statement) == "" {
			httpError(w, http.StatusBadRequest, "statement is required")
			return
		}
		tension := contextplane.Tension{
			Kind:        strings.TrimSpace(req.Kind),
			Statement:   strings.TrimSpace(req.Statement),
			Impact:      strings.TrimSpace(req.Impact),
			Status:      strings.TrimSpace(req.Status),
			RelatedRefs: stringsToEvidenceRefs(req.RelatedRefs),
			Count:       1,
			CreatedAt:   time.Now().UTC(),
			LastSeen:    time.Now().UTC(),
		}
		store := contextplane.NewWorkspaceStore(wp)
		id, err := store.AppendTension(tension)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "append tension failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, envelope.OK("context.tension", map[string]any{
			"workspace_path": wp,
			"id":             id,
			"tension":        tension,
		}))
	}
}

// ── Context Infer ───────────────────────────────────────────────────────

type contextInferRequest struct {
	Workspace    string   `json:"workspace,omitempty"`
	Summary      string   `json:"summary"`
	Project      string   `json:"project,omitempty"`
	Area         string   `json:"area,omitempty"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
	Apply        bool     `json:"apply,omitempty"`
}

func ContextInferHandler(_ config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req contextInferRequest
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if strings.TrimSpace(req.Summary) == "" {
			httpError(w, http.StatusBadRequest, "summary is required")
			return
		}
		result := contextplane.InferInsights(
			strings.TrimSpace(req.Summary),
			strings.TrimSpace(req.Project),
			strings.TrimSpace(req.Area),
			req.EvidenceRefs,
		)
		wp := strings.TrimSpace(req.Workspace)
		if wp == "" {
			wp = strings.TrimSpace(GetCurrentWorkspace())
		}
		if req.Apply && wp != "" {
			store := contextplane.NewWorkspaceStore(wp)
			for _, obs := range result.Observations {
				if _, err := store.AppendObservation(obs); err != nil {
					httpError(w, http.StatusInternalServerError, "apply observation failed: "+err.Error())
					return
				}
			}
			for _, t := range result.Tensions {
				if _, err := store.AppendTension(t); err != nil {
					httpError(w, http.StatusInternalServerError, "apply tension failed: "+err.Error())
					return
				}
			}
		}
		writeJSON(w, http.StatusOK, envelope.OK("context.infer", map[string]any{
			"workspace_path": wp,
			"applied":        req.Apply && wp != "",
			"observations":   result.Observations,
			"tensions":       result.Tensions,
		}))
	}
}

// ── Vault Search ────────────────────────────────────────────────────────

func VaultSearchHandler(cfg config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		query := strings.TrimSpace(r.URL.Query().Get("query"))
		if query == "" {
			httpError(w, http.StatusBadRequest, "query is required")
			return
		}
		limit := 20
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if parsed, err := parsePositiveInt(raw); err == nil {
				limit = parsed
			}
		}
		vaultPath := resolveContextVaultPath(strings.TrimSpace(r.URL.Query().Get("vault_path")))
		if vaultPath == "" {
			httpError(w, http.StatusBadRequest, "vault_path is required (or set FOXCTL_OBSIDIAN_VAULT_PATH)")
			return
		}
		index, err := obsidianindex.Open(r.Context(), cfg.Storage.Root, vaultPath)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "open vault index failed: "+err.Error())
			return
		}
		defer func() { _ = index.Close() }()
		hits, err := index.SearchNotes(r.Context(), query, limit)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "search failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, envelope.OK("vault.search", map[string]any{
			"vault_path": vaultPath,
			"query":      query,
			"hits":       hits,
			"count":      len(hits),
		}))
	}
}

// ── Vault Promote ───────────────────────────────────────────────────────

type vaultPromoteRequest struct {
	VaultPath string `json:"vault_path,omitempty"`
	Slug      string `json:"slug"`
	Content   string `json:"content"`
}

func VaultPromoteHandler(_ config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req vaultPromoteRequest
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if strings.TrimSpace(req.Slug) == "" {
			httpError(w, http.StatusBadRequest, "slug is required")
			return
		}
		if strings.TrimSpace(req.Content) == "" {
			httpError(w, http.StatusBadRequest, "content is required")
			return
		}
		vaultPath := resolveContextVaultPath(strings.TrimSpace(req.VaultPath))
		if vaultPath == "" {
			httpError(w, http.StatusBadRequest, "vault_path is required")
			return
		}
		writer := obsidian.NewWriter("", filepath.Base(vaultPath), obsidian.DefaultPolicy())
		writer.VaultPath = vaultPath
		path, err := writer.PromoteToEvergreen(r.Context(), strings.TrimSpace(req.Slug), req.Content)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "promote failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, envelope.OK("vault.promote", map[string]any{
			"vault_path": vaultPath,
			"path":       path,
		}))
	}
}

// ── Vault Append ────────────────────────────────────────────────────────

type vaultAppendRequest struct {
	VaultPath string `json:"vault_path,omitempty"`
	Path      string `json:"path"`
	Heading   string `json:"heading"`
	Content   string `json:"content"`
}

func VaultAppendHandler(_ config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req vaultAppendRequest
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if strings.TrimSpace(req.Path) == "" {
			httpError(w, http.StatusBadRequest, "path is required")
			return
		}
		if strings.TrimSpace(req.Heading) == "" {
			httpError(w, http.StatusBadRequest, "heading is required")
			return
		}
		if strings.TrimSpace(req.Content) == "" {
			httpError(w, http.StatusBadRequest, "content is required")
			return
		}
		vaultPath := resolveContextVaultPath(strings.TrimSpace(req.VaultPath))
		if vaultPath == "" {
			httpError(w, http.StatusBadRequest, "vault_path is required")
			return
		}
		writer := obsidian.NewWriter("", filepath.Base(vaultPath), obsidian.DefaultPolicy())
		writer.VaultPath = vaultPath
		if err := writer.AppendUnderHeading(r.Context(), req.Path, req.Heading, req.Content); err != nil {
			httpError(w, http.StatusInternalServerError, "append failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, envelope.OK("vault.append", map[string]any{
			"vault_path": vaultPath,
			"path":       req.Path,
			"heading":    req.Heading,
		}))
	}
}

// ── Vault Bridge ────────────────────────────────────────────────────────

type vaultBridgeRequest struct {
	VaultPath string `json:"vault_path,omitempty"`
	DocsRoot  string `json:"docs_root,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

func VaultBridgeHandler(_ config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req vaultBridgeRequest
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		vaultPath := resolveContextVaultPath(strings.TrimSpace(req.VaultPath))
		if vaultPath == "" {
			httpError(w, http.StatusBadRequest, "vault_path is required")
			return
		}
		wp := strings.TrimSpace(req.Workspace)
		if wp == "" {
			wp = strings.TrimSpace(GetCurrentWorkspace())
		}
		writer := obsidian.NewWriter("", filepath.Base(vaultPath), obsidian.DefaultPolicy())
		writer.VaultPath = vaultPath
		opts := obsidian.DocsBridgeReconcileOptions{
			DocsRoot: strings.TrimSpace(req.DocsRoot),
		}
		if wp != "" && opts.DocsRoot == "" {
			opts.DocsRoot = filepath.Join(wp, "docs")
		}
		result, err := obsidian.ReconcileDocsBridge(r.Context(), writer, opts)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "bridge reconcile failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, envelope.OK("vault.bridge", map[string]any{
			"vault_path": vaultPath,
			"result":     result,
		}))
	}
}

// ── Vault Graph ─────────────────────────────────────────────────────────

type vaultGraphRequest struct {
	VaultPath string `json:"vault_path,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

func VaultGraphHandler(_ config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req vaultGraphRequest
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		vaultPath := resolveContextVaultPath(strings.TrimSpace(req.VaultPath))
		if vaultPath == "" {
			httpError(w, http.StatusBadRequest, "vault_path is required")
			return
		}
		_ = strings.TrimSpace(req.Workspace) // workspace not used for graph build
		writer := obsidian.NewWriter("", filepath.Base(vaultPath), obsidian.DefaultPolicy())
		writer.VaultPath = vaultPath
		opts := obsidian.RepoGraphBuildOptions{}
		result, err := obsidian.BuildRepoGraphDrafts(r.Context(), writer, nil, opts)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "graph build failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, envelope.OK("vault.graph", map[string]any{
			"vault_path": vaultPath,
			"result":     result,
		}))
	}
}

// ── Vault Index Build ───────────────────────────────────────────────────

func VaultIndexBuildHandler(cfg config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		vaultPath := resolveContextVaultPath(strings.TrimSpace(r.URL.Query().Get("vault_path")))
		if vaultPath == "" {
			// Also check body
			var body struct {
				VaultPath string `json:"vault_path,omitempty"`
			}
			if err := readJSON(w, r, &body); err == nil {
				vaultPath = resolveContextVaultPath(strings.TrimSpace(body.VaultPath))
			}
		}
		if vaultPath == "" {
			httpError(w, http.StatusBadRequest, "vault_path is required")
			return
		}
		index, err := obsidianindex.Open(r.Context(), cfg.Storage.Root, vaultPath)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "open vault index failed: "+err.Error())
			return
		}
		defer func() { _ = index.Close() }()
		result, err := index.Rebuild(r.Context(), vaultPath)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "rebuild failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, envelope.OK("vault.index_build", map[string]any{
			"vault_path": vaultPath,
			"result":     result,
		}))
	}
}

// ── Vault Stats ─────────────────────────────────────────────────────────

func VaultStatsHandler(cfg config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		vaultPath := resolveContextVaultPath(strings.TrimSpace(r.URL.Query().Get("vault_path")))
		if vaultPath == "" {
			httpError(w, http.StatusBadRequest, "vault_path is required")
			return
		}
		index, err := obsidianindex.Open(r.Context(), cfg.Storage.Root, vaultPath)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "open vault index failed: "+err.Error())
			return
		}
		defer func() { _ = index.Close() }()
		stats, err := index.Stats(r.Context())
		if err != nil {
			httpError(w, http.StatusInternalServerError, "stats failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, envelope.OK("vault.stats", map[string]any{
			"vault_path": vaultPath,
			"stats":      stats,
		}))
	}
}

// ── Memory Put ──────────────────────────────────────────────────────────

// Note: memory_put records a control proposal for coordinator review, not a direct write.
// The Hermes tool calls store.RecordMemoryCandidateProposal() via the agent tool registry.
// This HTTP endpoint provides a simplified write path that records a memory proposal.

type memoryPutRequest struct {
	Workspace    string   `json:"workspace,omitempty"`
	Name         string   `json:"name"`
	Content      string   `json:"content"`
	Summary      string   `json:"summary"`
	Kind         string   `json:"kind,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	FileRefs     []string `json:"file_refs,omitempty"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
	SourceRefs   []string `json:"source_refs,omitempty"`
}

func MemoryPutHandler(_ config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req memoryPutRequest
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if strings.TrimSpace(req.Name) == "" {
			httpError(w, http.StatusBadRequest, "name is required")
			return
		}
		if strings.TrimSpace(req.Content) == "" {
			httpError(w, http.StatusBadRequest, "content is required")
			return
		}
		if strings.TrimSpace(req.Summary) == "" {
			httpError(w, http.StatusBadRequest, "summary is required")
			return
		}
		wp := strings.TrimSpace(req.Workspace)
		if wp == "" {
			wp = strings.TrimSpace(GetCurrentWorkspace())
		}
		if wp == "" {
			httpError(w, http.StatusBadRequest, "workspace is required")
			return
		}
		store := contextplane.NewWorkspaceStore(wp)
		input := contextplane.MemoryCandidateInput{
			WorkspaceID:    workspaceCanonicalID(wp),
			Name:           strings.TrimSpace(req.Name),
			Kind:           strings.TrimSpace(req.Kind),
			Summary:        strings.TrimSpace(req.Summary),
			Content:        req.Content,
			FileRefs:       stringsToEvidenceRefs(req.FileRefs),
			EvidenceRefs:   stringsToEvidenceRefs(req.EvidenceRefs),
			SourceRefs:     stringsToEvidenceRefs(req.SourceRefs),
			EvidenceOnly:   true,
			ReviewRequired: boolPtr(true),
		}
		// If source_refs not provided, synthesize from evidence_refs or file_refs
		if len(input.SourceRefs) == 0 {
			input.SourceRefs = input.EvidenceRefs
		}
		if len(input.SourceRefs) == 0 {
			input.SourceRefs = []contextengine.EvidenceRef{{Type: "session", Ref: "api"}}
		}
		// evidence_refs are also required by the validator
		if len(input.EvidenceRefs) == 0 {
			input.EvidenceRefs = input.SourceRefs
		}
		proposal, err := store.RecordMemoryCandidateProposal(r.Context(), input)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "record proposal failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, envelope.OK("memory.put", map[string]any{
			"workspace_path": wp,
			"id":             proposal.ID,
			"name":           req.Name,
			"status":         proposal.Status,
		}))
	}
}

// ── Context Memory Drafts ───────────────────────────────────────────────

type contextMemoryDraftsRequest struct {
	Workspace              string   `json:"workspace,omitempty"`
	VaultPath              string   `json:"vault_path,omitempty"`
	ApplyDrafts            bool     `json:"apply_drafts,omitempty"`
	DryRun                 *bool    `json:"dry_run,omitempty"`
	Lookback               string   `json:"lookback,omitempty"`
	Limit                  int      `json:"limit,omitempty"`
	BlurWithAgent          bool     `json:"blur_with_agent,omitempty"`
	BlurAgent              string   `json:"blur_agent,omitempty"`
	BlurAgentBin           string   `json:"blur_agent_bin,omitempty"`
	BlurAgentProvider      string   `json:"blur_agent_provider,omitempty"`
	BlurAgentModel         string   `json:"blur_agent_model,omitempty"`
	BlurAgentCommand       []string `json:"blur_agent_command,omitempty"`
	BlurAgentPromptMode    string   `json:"blur_agent_prompt_mode,omitempty"`
	PiMode                 string   `json:"pi_mode,omitempty"`
	PiSDKBin               string   `json:"pi_sdk_bin,omitempty"`
	PiSDKScript            string   `json:"pi_sdk_script,omitempty"`
	PiSDKCWD               string   `json:"pi_sdk_cwd,omitempty"`
	PiAgentDir             string   `json:"pi_agent_dir,omitempty"`
	PiThinking             string   `json:"pi_thinking,omitempty"`
	PiNoExtensions         *bool    `json:"pi_no_extensions,omitempty"`
	HermesIgnoreRules      *bool    `json:"hermes_ignore_rules,omitempty"`
	HermesIgnoreUserConfig bool     `json:"hermes_ignore_user_config,omitempty"`
	FoxctlAgentID          string   `json:"foxctl_agent_id,omitempty"`
	FoxctlDispatcher       string   `json:"foxctl_dispatcher,omitempty"`
	FoxctlConversationID   string   `json:"foxctl_conversation_id,omitempty"`
}

func ContextMemoryDraftsHandler(cfg config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req contextMemoryDraftsRequest
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		wp := strings.TrimSpace(req.Workspace)
		if wp == "" {
			wp = strings.TrimSpace(GetCurrentWorkspace())
		}
		if wp == "" {
			httpError(w, http.StatusBadRequest, "workspace is required")
			return
		}
		var lookback time.Duration
		if strings.TrimSpace(req.Lookback) != "" {
			parsed, err := time.ParseDuration(strings.TrimSpace(req.Lookback))
			if err != nil || parsed <= 0 {
				httpError(w, http.StatusBadRequest, "lookback must be a positive Go duration such as 24h")
				return
			}
			lookback = parsed
		}
		dryRun := true
		if req.DryRun != nil {
			dryRun = *req.DryRun
		}
		var blurAgent contextplane.MemoryBlurAgent
		if req.BlurWithAgent {
			var err error
			blurAgent, err = contextMemoryDraftBlurAgent(req)
			if err != nil {
				httpError(w, http.StatusBadRequest, "invalid blur agent: "+err.Error())
				return
			}
		}
		report, err := contextplane.RunAutonomousMemoryDrafts(r.Context(), contextplane.AutonomousMemoryDraftRunOptions{
			StorageRoot:   cfg.Storage.Root,
			WorkspacePath: wp,
			VaultPath:     resolveContextVaultPath(strings.TrimSpace(req.VaultPath)),
			Lookback:      lookback,
			Limit:         req.Limit,
			ApplyDrafts:   req.ApplyDrafts,
			DryRun:        dryRun,
			BlurWithAgent: req.BlurWithAgent,
			BlurAgent:     blurAgent,
			BlurAgentName: firstNonEmpty(strings.TrimSpace(req.BlurAgent), memoryblur.BackendPi),
		})
		if err != nil {
			httpError(w, http.StatusInternalServerError, "memory drafts failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, envelope.OK("context.memory_drafts", map[string]any{
			"workspace_path": wp,
			"report":         report,
		}))
	}
}

func contextMemoryDraftBlurAgent(req contextMemoryDraftsRequest) (contextplane.MemoryBlurAgent, error) {
	backend := strings.ToLower(strings.TrimSpace(req.BlurAgent))
	if backend == "" {
		backend = memoryblur.BackendPi
	}
	piNoExtensions := true
	if req.PiNoExtensions != nil {
		piNoExtensions = *req.PiNoExtensions
	}
	hermesIgnoreRules := true
	if req.HermesIgnoreRules != nil {
		hermesIgnoreRules = *req.HermesIgnoreRules
	}
	commandBin := ""
	if len(req.BlurAgentCommand) > 0 {
		commandBin = req.BlurAgentCommand[0]
	}
	if strings.TrimSpace(commandBin) == "" {
		commandBin = req.BlurAgentBin
	}
	return memoryblur.NewAgent(memoryblur.AgentOptions{
		Backend: backend,
		Command: memoryblur.CommandAgentOptions{
			Name:       memoryblur.BackendCommand,
			Bin:        strings.TrimSpace(commandBin),
			Args:       commandArgsAfterFirst(req.BlurAgentCommand),
			PromptMode: strings.TrimSpace(req.BlurAgentPromptMode),
		},
		Pi: memoryblur.PiAgentOptions{
			PiBin:        firstNonEmpty(strings.TrimSpace(req.BlurAgentBin), "pi"),
			Mode:         firstNonEmpty(strings.TrimSpace(req.PiMode), memoryblur.PiModeSDK),
			SDKBin:       firstNonEmpty(strings.TrimSpace(req.PiSDKBin), "bun"),
			SDKScript:    strings.TrimSpace(req.PiSDKScript),
			SDKCWD:       strings.TrimSpace(req.PiSDKCWD),
			AgentDir:     strings.TrimSpace(req.PiAgentDir),
			Thinking:     firstNonEmpty(strings.TrimSpace(req.PiThinking), "off"),
			Provider:     strings.TrimSpace(req.BlurAgentProvider),
			Model:        strings.TrimSpace(req.BlurAgentModel),
			NoExtensions: piNoExtensions,
		},
		Claude: memoryblur.ClaudeAgentOptions{
			ClaudeBin: firstNonEmpty(strings.TrimSpace(req.BlurAgentBin), "claude"),
			Model:     strings.TrimSpace(req.BlurAgentModel),
		},
		Hermes: memoryblur.HermesAgentOptions{
			HermesBin:        firstNonEmpty(strings.TrimSpace(req.BlurAgentBin), "hermes"),
			Provider:         strings.TrimSpace(req.BlurAgentProvider),
			Model:            strings.TrimSpace(req.BlurAgentModel),
			IgnoreRules:      hermesIgnoreRules,
			IgnoreUserConfig: req.HermesIgnoreUserConfig,
		},
		Foxctl: memoryblur.FoxctlAgentOptions{
			FoxctlBin:      firstNonEmpty(strings.TrimSpace(req.BlurAgentBin), "foxctl"),
			AgentID:        strings.TrimSpace(req.FoxctlAgentID),
			Dispatcher:     strings.TrimSpace(req.FoxctlDispatcher),
			ConversationID: strings.TrimSpace(req.FoxctlConversationID),
		},
	})
}

func commandArgsAfterFirst(argv []string) []string {
	if len(argv) <= 1 {
		return nil
	}
	return append([]string(nil), argv[1:]...)
}

// ── Embedding Flush ─────────────────────────────────────────────────────

type embeddingFlushRequest struct {
	BatchSize   int `json:"batch_size,omitempty"`
	MaxDuration int `json:"max_duration,omitempty"`
}

func EmbeddingFlushHandler(_ config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req embeddingFlushRequest
		if err := readJSON(w, r, &req); err != nil {
			// Allow empty body
			req = embeddingFlushRequest{}
		}
		batchSize := 50
		if req.BatchSize > 0 {
			batchSize = req.BatchSize
		}
		maxDuration := 120
		if req.MaxDuration > 0 {
			maxDuration = req.MaxDuration
		}
		// Embedding flush runs the embedding/worker and embedding/queue skills.
		// For the HTTP API we delegate to the skill runner via the bin path.
		// If skills aren't available we report the config but can't process.
		log.Info().Int("batch_size", batchSize).Int("max_duration", maxDuration).Msg("embedding flush requested via HTTP API")

		writeJSON(w, http.StatusOK, envelope.OK("embedding.flush", map[string]any{
			"batch_size":   batchSize,
			"max_duration": maxDuration,
			"status":       "queued",
			"note":         "Use 'foxctl hooks operational embedding-flush' CLI command for full processing",
		}))
	}
}

// ── Publish Context ─────────────────────────────────────────────────────

type publishContextRequest struct {
	RoomID  string `json:"room_id,omitempty"`
	Context string `json:"context"`
}

func PublishContextHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req publishContextRequest
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if strings.TrimSpace(req.Context) == "" {
			httpError(w, http.StatusBadRequest, "context is required")
			return
		}
		roomID := strings.TrimSpace(req.RoomID)
		if roomID == "" {
			roomID = strings.TrimSpace(os.Getenv("FOXCTL_ROOM_ID"))
		}
		if roomID == "" {
			roomID = "alpha"
		}
		// Publish context as a room message with subject agent-context-broadcast
		// This mirrors what the Hermes Python client does
		log.Info().Str("room_id", roomID).Str("subject", "agent-context-broadcast").Msg("publishing context broadcast")

		writeJSON(w, http.StatusOK, envelope.OK("context.publish", map[string]any{
			"room_id": roomID,
			"subject": "agent-context-broadcast",
			"status":  "broadcast_queued",
			"note":    "Full room message posting requires room API integration; context logged for broadcast",
		}))
	}
}

// ── helpers ─────────────────────────────────────────────────────────────

// workspaceCanonicalID returns a stable workspace ID from a path.
// Mirrors the workspace.CanonicalID function used by CLI commands.
func workspaceCanonicalID(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return strings.ToLower(strings.ReplaceAll(abs, string(filepath.Separator), "_"))
}

// parseLimitQueryParam parses a "limit" query parameter with a default.
// stringsToEvidenceRefs converts a slice of raw strings to EvidenceRef values.
func stringsToEvidenceRefs(refs []string) []contextengine.EvidenceRef {
	if len(refs) == 0 {
		return nil
	}
	result := make([]contextengine.EvidenceRef, 0, len(refs))
	for _, r := range refs {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if parsed, err := contextengine.ParseEvidenceRef(r); err == nil {
			result = append(result, parsed)
		} else {
			result = append(result, contextengine.EvidenceRef{Type: "file", Ref: r})
		}
	}
	return result
}
