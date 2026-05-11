package operationalflow

import (
	"bufio"
	"context"
	"fmt"
	"os"
	execpkg "os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/hooks/lifecycle"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type Dependencies struct {
	Config   config.Config
	RunSkill lifecycle.SkillRunner
	ExecCmd  func(ctx context.Context, dir, name string, args ...string) (string, error)
	LookPath func(string) (string, error)
}

func NewDependencies(cfg config.Config, life lifecycle.Dependencies) Dependencies {
	return Dependencies{
		Config:   cfg,
		RunSkill: life.RunSkill,
		ExecCmd:  defaultExecCmd,
		LookPath: execpkg.LookPath,
	}
}

type LiveIndexPayload struct {
	ToolName  string `json:"tool_name,omitempty"`
	ToolInput struct {
		FilePath string `json:"file_path,omitempty"`
		Path     string `json:"path,omitempty"`
	} `json:"tool_input"`
}

type LiveIndexRequest struct {
	Workspace string
	Payload   LiveIndexPayload
}

type LiveIndexResponse struct {
	Decision        string   `json:"decision"`
	Context         string   `json:"context,omitempty"`
	FilePath        string   `json:"file_path,omitempty"`
	SymbolsUpdated  int      `json:"symbols_updated,omitempty"`
	SymbolsDeleted  int      `json:"symbols_deleted,omitempty"`
	EmbeddingQueued int      `json:"embedding_queued,omitempty"`
	DurationMS      int64    `json:"duration_ms,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
}

type LSPDiagnosticsPayload struct {
	ToolInput struct {
		FilePath string `json:"file_path,omitempty"`
		Path     string `json:"path,omitempty"`
	} `json:"tool_input"`
}

type LSPDiagnosticsRequest struct {
	Workspace string
	Payload   LSPDiagnosticsPayload
}

type LSPDiagnosticsResponse struct {
	Decision    string   `json:"decision"`
	Context     string   `json:"context,omitempty"`
	FilePath    string   `json:"file_path,omitempty"`
	Diagnostics []string `json:"diagnostics,omitempty"`
}

type GraphMaintenanceRequest struct {
	Workspace string
}

type GraphMaintenanceResponse struct {
	Workspace         string   `json:"workspace"`
	Mode              string   `json:"mode,omitempty"`
	LogFile           string   `json:"log_file,omitempty"`
	CleanupRan        bool     `json:"cleanup_ran,omitempty"`
	DegreeRepairRan   bool     `json:"degree_repair_ran,omitempty"`
	PageRankRan       bool     `json:"pagerank_ran,omitempty"`
	ExpiredRemoved    int      `json:"expired_removed,omitempty"`
	DanglingRemoved   int      `json:"dangling_removed,omitempty"`
	NodesUpdated      int      `json:"nodes_updated,omitempty"`
	EdgesProcessed    int      `json:"edges_processed,omitempty"`
	Warnings          []string `json:"warnings,omitempty"`
	DegreesRecomputed bool     `json:"degrees_recomputed,omitempty"`
}

type EmbeddingFlushRequest struct {
	Workspace string
}

type EmbeddingFlushResponse struct {
	Workspace string   `json:"workspace"`
	Queued    int      `json:"queued,omitempty"`
	Processed int      `json:"processed,omitempty"`
	Remaining int      `json:"remaining,omitempty"`
	Status    string   `json:"status,omitempty"`
	Message   string   `json:"message,omitempty"`
	Warnings  []string `json:"warnings,omitempty"`
	Skipped   bool     `json:"skipped,omitempty"`
	Reason    string   `json:"reason,omitempty"`
}

type PlanSyncPayload struct {
	SessionCwd string `json:"session_cwd,omitempty"`
}

type PlanSyncRequest struct {
	Workspace string
	Payload   PlanSyncPayload
}

type PlanSyncResponse struct {
	Workspace      string   `json:"workspace"`
	Mode           string   `json:"mode,omitempty"`
	LogFile        string   `json:"log_file,omitempty"`
	PlansProcessed int      `json:"plans_processed,omitempty"`
	PlansChanged   int      `json:"plans_changed,omitempty"`
	TasksCreated   int      `json:"tasks_created,omitempty"`
	Provider       string   `json:"provider,omitempty"`
	Message        string   `json:"message,omitempty"`
	Warnings       []string `json:"warnings,omitempty"`
}

type incrementalIndexEnvelope struct {
	Data struct {
		SymbolsUpdated  int   `json:"symbols_updated"`
		SymbolsDeleted  int   `json:"symbols_deleted"`
		EmbeddingQueued int   `json:"embedding_queued"`
		DurationMS      int64 `json:"duration_ms"`
		Skipped         bool  `json:"skipped"`
	} `json:"data"`
}

type graphCleanupEnvelope struct {
	Data struct {
		Result struct {
			ExpiredEdgesRemoved  int  `json:"expired_edges_removed"`
			DanglingEdgesRemoved int  `json:"dangling_edges_removed"`
			DegreesRecalculated  bool `json:"degrees_recalculated"`
		} `json:"result"`
	} `json:"data"`
}

type graphPageRankEnvelope struct {
	Data struct {
		NodesUpdated   int `json:"nodes_updated"`
		EdgesProcessed int `json:"edges_processed"`
	} `json:"data"`
}

type embeddingQueueStatsEnvelope struct {
	Data struct {
		Stats struct {
			QueuedCount int `json:"queued_count"`
		} `json:"stats"`
	} `json:"data"`
}

type embeddingWorkerEnvelope struct {
	Data struct {
		Processed int    `json:"processed"`
		Remaining int    `json:"remaining"`
		Status    string `json:"status"`
		Message   string `json:"message"`
	} `json:"data"`
}

type planSyncEnvelope struct {
	Data struct {
		PlansProcessed int    `json:"plans_processed"`
		PlansChanged   int    `json:"plans_changed"`
		TasksCreated   int    `json:"tasks_created"`
		Provider       string `json:"provider"`
		Message        string `json:"message"`
	} `json:"data"`
}

var (
	reGoDiag          = regexp.MustCompile(`:([0-9]+):([0-9]+)(-[0-9]+)?:\s+(.+)$`)
	reQuickLintDiag   = regexp.MustCompile(`:([0-9]+):([0-9]+):\s+(error|warning):\s+(.+)$`)
	reRuffDiag        = regexp.MustCompile(`:([0-9]+):([0-9]+):\s+([A-Z]+[0-9]+)\s+(.+)$`)
	rePyrightDiag     = regexp.MustCompile(`:([0-9]+):([0-9]+):\s+(error|warning):\s+(.+)$`)
	reBiomeLineNumber = regexp.MustCompile(`^\s*([0-9]+)\s*│`)
	titleCaser        = cases.Title(language.Und)
)

func IndexEditedFile(ctx context.Context, deps Dependencies, req LiveIndexRequest) (LiveIndexResponse, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(req.Workspace)))
	if target == "" {
		return LiveIndexResponse{}, fmt.Errorf("detect workspace")
	}
	response := LiveIndexResponse{Decision: "approve"}
	filePath := firstNonEmpty(
		strings.TrimSpace(req.Payload.ToolInput.FilePath),
		strings.TrimSpace(req.Payload.ToolInput.Path),
	)
	if filePath == "" || !isSupportedLiveIndexFile(filePath) || shouldSkipLiveIndexPath(filePath) {
		return response, nil
	}
	response.FilePath = filePath
	if deps.RunSkill == nil {
		return response, nil
	}

	var env incrementalIndexEnvelope
	if err := deps.RunSkill(ctx, "code/incremental_index", map[string]any{
		"file":        filePath,
		"symbols":     true,
		"embed":       false,
		"embed_queue": liveIndexEmbedQueueEnabled(),
	}, target, &env); err != nil {
		if envEnabled("FOXCTL_LIVE_INDEX_DEBUG") {
			response.Warnings = append(response.Warnings, fmt.Sprintf("index failed for %s: %v", filePath, err))
		}
		return response, nil
	}
	response.SymbolsUpdated = env.Data.SymbolsUpdated
	response.SymbolsDeleted = env.Data.SymbolsDeleted
	response.EmbeddingQueued = env.Data.EmbeddingQueued
	response.DurationMS = env.Data.DurationMS
	if env.Data.Skipped || (response.SymbolsUpdated == 0 && response.SymbolsDeleted == 0) {
		return response, nil
	}

	filename := filepath.Base(filePath)
	if response.SymbolsDeleted > 0 {
		response.Context = fmt.Sprintf(
			"Indexed **%d** symbols (+%d removed) from `%s` (%dms)",
			response.SymbolsUpdated,
			response.SymbolsDeleted,
			filename,
			response.DurationMS,
		)
	} else {
		response.Context = fmt.Sprintf(
			"Indexed **%d** symbols from `%s` (%dms)",
			response.SymbolsUpdated,
			filename,
			response.DurationMS,
		)
	}
	if response.EmbeddingQueued > 0 {
		response.Context += fmt.Sprintf(" | Queued **%d** for embedding", response.EmbeddingQueued)
	}
	return response, nil
}

func DiagnoseEditedFile(ctx context.Context, deps Dependencies, req LSPDiagnosticsRequest) (LSPDiagnosticsResponse, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(req.Workspace)))
	if target == "" {
		return LSPDiagnosticsResponse{}, fmt.Errorf("detect workspace")
	}
	response := LSPDiagnosticsResponse{Decision: "approve"}
	filePath := firstNonEmpty(
		strings.TrimSpace(req.Payload.ToolInput.FilePath),
		strings.TrimSpace(req.Payload.ToolInput.Path),
	)
	if filePath == "" {
		return response, nil
	}
	absPath := filePath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(target, filePath)
	}
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		return response, nil
	}
	response.FilePath = filePath

	diagnostics, err := collectDiagnostics(ctx, deps, target, absPath)
	if err != nil {
		return response, err
	}
	if len(diagnostics) == 0 {
		return response, nil
	}

	response.Diagnostics = diagnostics
	response.Context = fmt.Sprintf("**LSP Diagnostics** for `%s`:\n```\n%s\n```", filepath.Base(filePath), strings.Join(diagnostics, "\n"))
	return response, nil
}

func MaintainGraphSync(ctx context.Context, deps Dependencies, req GraphMaintenanceRequest) (GraphMaintenanceResponse, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(req.Workspace)))
	if target == "" {
		return GraphMaintenanceResponse{}, fmt.Errorf("detect workspace")
	}
	response := GraphMaintenanceResponse{
		Workspace: target,
		Mode:      "sync",
	}
	if envEnabled("FOXCTL_GRAPH_MAINTENANCE_DISABLED") || deps.RunSkill == nil {
		return response, nil
	}

	doCleanup := !envEnabled("FOXCTL_GRAPH_CLEANUP_DISABLED")
	doPagerank := !envEnabled("FOXCTL_GRAPH_PAGERANK_DISABLED")
	if !doCleanup && !doPagerank {
		return response, nil
	}

	if doCleanup {
		var cleanup graphCleanupEnvelope
		if err := deps.RunSkill(ctx, "graph/manage", map[string]any{
			"workspace": target,
			"operation": "cleanup",
			"cleanup": map[string]any{
				"expired_edges":  true,
				"dangling_edges": true,
				"recalc_degrees": false,
			},
		}, target, &cleanup); err != nil {
			response.Warnings = append(response.Warnings, fmt.Sprintf("cleanup failed: %v", err))
		} else {
			response.CleanupRan = true
			response.ExpiredRemoved = cleanup.Data.Result.ExpiredEdgesRemoved
			response.DanglingRemoved = cleanup.Data.Result.DanglingEdgesRemoved
		}
	}

	if doPagerank {
		var degreeRepair graphCleanupEnvelope
		if err := deps.RunSkill(ctx, "graph/manage", map[string]any{
			"workspace": target,
			"operation": "cleanup",
			"cleanup": map[string]any{
				"expired_edges":  false,
				"dangling_edges": false,
				"recalc_degrees": true,
			},
		}, target, &degreeRepair); err != nil {
			response.Warnings = append(response.Warnings, fmt.Sprintf("degree repair failed: %v", err))
		} else {
			response.DegreeRepairRan = true
			response.DegreesRecomputed = degreeRepair.Data.Result.DegreesRecalculated
		}

		var pagerank graphPageRankEnvelope
		if err := deps.RunSkill(ctx, "graph/pagerank", map[string]any{
			"workspace": target,
		}, target, &pagerank); err != nil {
			response.Warnings = append(response.Warnings, fmt.Sprintf("pagerank failed: %v", err))
		} else {
			response.PageRankRan = true
			response.NodesUpdated = pagerank.Data.NodesUpdated
			response.EdgesProcessed = pagerank.Data.EdgesProcessed
		}
	}

	return response, nil
}

func FlushEmbeddings(ctx context.Context, deps Dependencies, req EmbeddingFlushRequest) (EmbeddingFlushResponse, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(req.Workspace)))
	if target == "" {
		return EmbeddingFlushResponse{}, fmt.Errorf("detect workspace")
	}
	response := EmbeddingFlushResponse{Workspace: target}
	if !liveIndexEmbedQueueEnabled() {
		response.Skipped = true
		response.Reason = "embedding queue disabled"
		return response, nil
	}
	if deps.RunSkill == nil {
		response.Skipped = true
		response.Reason = "skill runner unavailable"
		return response, nil
	}
	if !embeddingConfigured(deps.Config) {
		response.Skipped = true
		response.Reason = "embedding provider not configured"
		return response, nil
	}

	var stats embeddingQueueStatsEnvelope
	if err := deps.RunSkill(ctx, "embedding/queue", map[string]any{
		"operation": "stats",
	}, target, &stats); err != nil {
		response.Warnings = append(response.Warnings, fmt.Sprintf("queue stats failed: %v", err))
		return response, nil
	}
	response.Queued = stats.Data.Stats.QueuedCount
	if response.Queued <= 0 {
		response.Skipped = true
		response.Reason = "no queued embeddings"
		return response, nil
	}

	var worker embeddingWorkerEnvelope
	if err := deps.RunSkill(ctx, "embedding/worker", map[string]any{
		"batch_size":   50,
		"max_duration": 60,
	}, target, &worker); err != nil {
		response.Warnings = append(response.Warnings, fmt.Sprintf("embedding worker failed: %v", err))
		return response, nil
	}
	response.Processed = worker.Data.Processed
	response.Remaining = worker.Data.Remaining
	response.Status = worker.Data.Status
	response.Message = worker.Data.Message
	return response, nil
}

func SyncPlans(ctx context.Context, deps Dependencies, req PlanSyncRequest) (PlanSyncResponse, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(firstNonEmpty(req.Payload.SessionCwd, req.Workspace))))
	if target == "" {
		return PlanSyncResponse{}, fmt.Errorf("detect workspace")
	}
	response := PlanSyncResponse{
		Workspace: target,
		Mode:      "sync",
	}
	if envEnabled("FOXCTL_PLAN_SYNC_DISABLED") || deps.RunSkill == nil {
		return response, nil
	}

	var env planSyncEnvelope
	if err := deps.RunSkill(ctx, "plan/sync", map[string]any{
		"workspace":    target,
		"import_tasks": false,
	}, target, &env); err != nil {
		response.Warnings = append(response.Warnings, fmt.Sprintf("plan sync failed: %v", err))
		return response, nil
	}
	response.PlansProcessed = env.Data.PlansProcessed
	response.PlansChanged = env.Data.PlansChanged
	response.TasksCreated = env.Data.TasksCreated
	response.Provider = env.Data.Provider
	response.Message = env.Data.Message
	return response, nil
}

func GraphMaintenanceSyncEnabled() bool {
	return envEnabled("FOXCTL_GRAPH_MAINTENANCE_SYNC")
}

func PlanSyncSyncEnabled() bool {
	return envEnabled("FOXCTL_PLAN_SYNC_SYNC")
}

func liveIndexEmbedQueueEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(firstNonEmpty(
		getenv("FOXCTL_EMBED_QUEUE"),
		"1",
	)))
	return value != "0" && value != "false" && value != "no" && value != "off"
}

func isSupportedLiveIndexFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".py", ".ts", ".tsx", ".js", ".jsx", ".gd":
		return true
	default:
		return false
	}
}

func shouldSkipLiveIndexPath(path string) bool {
	slashPath := filepath.ToSlash(strings.ToLower(path))
	if strings.Contains(slashPath, "/vendor/") ||
		strings.Contains(slashPath, "/node_modules/") ||
		strings.Contains(slashPath, "/.git/") ||
		strings.Contains(slashPath, "/dist/") ||
		strings.Contains(slashPath, "/build/") ||
		strings.Contains(slashPath, "/__pycache__/") {
		return true
	}
	if strings.Contains(slashPath, "/testdata/") || strings.Contains(slashPath, "/fixtures/") {
		return true
	}
	return strings.HasSuffix(slashPath, "_test.go")
}

func collectDiagnostics(ctx context.Context, deps Dependencies, workspaceRoot, absPath string) ([]string, error) {
	ext := strings.ToLower(filepath.Ext(absPath))
	switch ext {
	case ".go":
		return runGoDiagnostics(ctx, deps, workspaceRoot, absPath)
	case ".ts", ".tsx", ".js", ".jsx":
		return runJSDiagnostics(ctx, deps, workspaceRoot, absPath)
	case ".py":
		return runPythonDiagnostics(ctx, deps, workspaceRoot, absPath)
	default:
		return nil, nil
	}
}

func runGoDiagnostics(ctx context.Context, deps Dependencies, workspaceRoot, absPath string) ([]string, error) {
	if !commandAvailable(deps, "gopls") {
		return nil, nil
	}
	output, _ := deps.ExecCmd(ctx, workspaceRoot, "gopls", "check", absPath)
	if strings.TrimSpace(output) == "" {
		return nil, nil
	}
	return scanMatches(output, 10, func(line string) string {
		matches := reGoDiag.FindStringSubmatch(line)
		if len(matches) == 0 {
			return ""
		}
		return fmt.Sprintf("Error [%s:%s] %s", matches[1], matches[2], matches[4])
	}), nil
}

func runJSDiagnostics(ctx context.Context, deps Dependencies, workspaceRoot, absPath string) ([]string, error) {
	type candidate struct {
		name  string
		args  []string
		parse func(string) []string
	}
	candidates := []candidate{
		{
			name: "quick-lint-js",
			args: []string{"--output-format=gnu-like", absPath},
			parse: func(out string) []string {
				return scanMatches(out, 10, func(line string) string {
					matches := reQuickLintDiag.FindStringSubmatch(line)
					if len(matches) == 0 {
						return ""
					}
					return fmt.Sprintf("%s [%s:%s] %s", titleCaser.String(matches[3]), matches[1], matches[2], matches[4])
				})
			},
		},
		{
			name: "biome",
			args: []string{"check", "--max-diagnostics=10", absPath},
			parse: func(out string) []string {
				if strings.Contains(out, "No diagnostics") {
					return nil
				}
				return scanMatches(out, 10, func(line string) string {
					matches := reBiomeLineNumber.FindStringSubmatch(line)
					if len(matches) == 0 {
						return ""
					}
					return fmt.Sprintf("Error [%s] %s", matches[1], strings.TrimSpace(line))
				})
			},
		},
		{
			name: "eslint",
			args: []string{"--format", "compact", absPath},
			parse: func(out string) []string {
				if strings.Contains(out, "0 problems") {
					return nil
				}
				return scanMatches(out, 10, func(line string) string {
					matches := reQuickLintDiag.FindStringSubmatch(line)
					if len(matches) == 0 {
						return ""
					}
					return fmt.Sprintf("%s [%s:%s] %s", titleCaser.String(matches[3]), matches[1], matches[2], matches[4])
				})
			},
		},
	}

	for _, candidate := range candidates {
		if !commandAvailable(deps, candidate.name) {
			continue
		}
		output, _ := deps.ExecCmd(ctx, workspaceRoot, candidate.name, candidate.args...)
		if strings.TrimSpace(output) == "" {
			continue
		}
		diagnostics := candidate.parse(output)
		if len(diagnostics) > 0 {
			return diagnostics, nil
		}
	}
	return nil, nil
}

func runPythonDiagnostics(ctx context.Context, deps Dependencies, workspaceRoot, absPath string) ([]string, error) {
	type candidate struct {
		name  string
		args  []string
		parse func(string) []string
	}
	candidates := []candidate{
		{
			name: "ruff",
			args: []string{"check", "--output-format=concise", absPath},
			parse: func(out string) []string {
				if strings.Contains(out, "All checks passed") {
					return nil
				}
				return scanMatches(out, 10, func(line string) string {
					matches := reRuffDiag.FindStringSubmatch(line)
					if len(matches) == 0 {
						return ""
					}
					return fmt.Sprintf("Error [%s:%s] %s %s", matches[1], matches[2], matches[3], matches[4])
				})
			},
		},
		{
			name: "pyright",
			args: []string{absPath},
			parse: func(out string) []string {
				return scanMatches(out, 10, func(line string) string {
					matches := rePyrightDiag.FindStringSubmatch(line)
					if len(matches) == 0 {
						return ""
					}
					return fmt.Sprintf("%s [%s:%s] %s", titleCaser.String(matches[3]), matches[1], matches[2], matches[4])
				})
			},
		},
	}
	for _, candidate := range candidates {
		if !commandAvailable(deps, candidate.name) {
			continue
		}
		output, _ := deps.ExecCmd(ctx, workspaceRoot, candidate.name, candidate.args...)
		if strings.TrimSpace(output) == "" {
			continue
		}
		diagnostics := candidate.parse(output)
		if len(diagnostics) > 0 {
			return diagnostics, nil
		}
	}
	return nil, nil
}

func embeddingConfigured(cfg config.Config) bool {
	if strings.TrimSpace(cfg.Embedding.APIKey) != "" {
		return true
	}
	if strings.TrimSpace(cfg.Embedding.Provider) != "" {
		return true
	}
	return strings.TrimSpace(getenv("GEMINI_API_KEY")) != ""
}

func commandAvailable(deps Dependencies, name string) bool {
	if deps.LookPath == nil {
		return false
	}
	_, err := deps.LookPath(name)
	return err == nil
}

func scanMatches(output string, limit int, fn func(string) string) []string {
	results := make([]string, 0, limit)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		if limit > 0 && len(results) >= limit {
			break
		}
		if formatted := strings.TrimSpace(fn(scanner.Text())); formatted != "" {
			results = append(results, formatted)
		}
	}
	return results
}

func defaultExecCmd(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := execpkg.CommandContext(ctx, name, args...)
	if strings.TrimSpace(dir) != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func envEnabled(name string) bool {
	value := strings.ToLower(strings.TrimSpace(getenv(name)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

var getenv = os.Getenv
