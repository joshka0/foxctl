package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/context/contextplane/taskhistory"
	"github.com/joshka0/foxctl/internal/context/sessionkit"
	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/daemon"
	"github.com/joshka0/foxctl/internal/runtime/runservice"
	"github.com/joshka0/foxctl/internal/storage/cache"
	"github.com/joshka0/foxctl/internal/storage/sessions"
	"github.com/joshka0/foxctl/internal/storage/tasks"
)

type StartRequest struct {
	Workspace string
	Source    string
}

type StartResponse struct {
	Context     string `json:"context,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	Provider    string `json:"provider,omitempty"`
	AgentID     string `json:"agent_id,omitempty"`
	Workspace   string `json:"workspace"`
	Source      string `json:"source"`
	DaemonReady bool   `json:"daemon_ready,omitempty"`
}

type EndPayload struct {
	AssistantText string `json:"assistant_text,omitempty"`
}

type EndRequest struct {
	Workspace string
	Payload   EndPayload
}

type EndResponse struct {
	Workspace         string `json:"workspace"`
	SessionID         string `json:"session_id,omitempty"`
	CapturedSessionID string `json:"captured_session_id,omitempty"`
	CaptureStatus     string `json:"capture_status,omitempty"`
	HandoffPath       string `json:"handoff_path,omitempty"`
	SummaryWritten    bool   `json:"summary_written,omitempty"`
	PromotionDraft    string `json:"promotion_draft,omitempty"`
}

type SubagentStopRequest struct {
	Workspace string
	Payload   EndPayload
}

type SubagentStopResponse struct {
	Workspace      string `json:"workspace"`
	TaskID         string `json:"task_id,omitempty"`
	HandoffPath    string `json:"handoff_path,omitempty"`
	PromotionDraft string `json:"promotion_draft,omitempty"`
}

type PostcompactRestoreRequest struct {
	Workspace string
}

type PostcompactRestoreResponse struct {
	Decision  string `json:"decision"`
	Context   string `json:"context,omitempty"`
	Workspace string `json:"workspace"`
	SessionID string `json:"session_id,omitempty"`
}

type (
	SkillRunner      func(ctx context.Context, skill string, input any, workspace string, out any) error
	IdentityDetector func(workspace string) (sessionID, provider, agentID string)
	SummaryAppender  func(workspace string, prefs, gotchas, timeSinks []string) bool
	DaemonEnsurer    func(ctx context.Context, workspace string) bool
	WarmupFunc       func(ctx context.Context, workspace string)
	TaskContinuity   func(ctx context.Context, workspace string) (string, error)
)

type Dependencies struct {
	StorageRoot    string
	EnsureDaemon   DaemonEnsurer
	WarmWorkspace  WarmupFunc
	DetectIdentity IdentityDetector
	RunSkill       SkillRunner
	AppendSummary  SummaryAppender
	TaskContinuity TaskContinuity
}

type sessionRestoreEnvelope struct {
	Data struct {
		HookOutput struct {
			Context string `json:"context"`
		} `json:"hook_output"`
	} `json:"data"`
}

type sessionAnchorEnvelope struct {
	Data struct {
		Found  bool `json:"found"`
		Anchor struct {
			MainPrompt      string `json:"main_prompt"`
			PendingQuestion string `json:"pending_question"`
		} `json:"anchor"`
	} `json:"data"`
}

type sessionCaptureEnvelope struct {
	Data struct {
		SessionID string `json:"session_id"`
		Status    string `json:"status"`
	} `json:"data"`
}

type sessionSummarizeEnvelope struct {
	Data struct {
		UserPreferences []string `json:"user_preferences"`
		Gotchas         []string `json:"gotchas"`
		TimeSinks       []string `json:"time_sinks"`
	} `json:"data"`
}

func NewDependencies(cfg config.Config) Dependencies {
	client := daemon.NewClient()
	return Dependencies{
		StorageRoot: cfg.Storage.Root,
		EnsureDaemon: func(ctx context.Context, workspace string) bool {
			if client.IsRunning() {
				return true
			}
			ensureCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			return client.EnsureRunningContext(ensureCtx) == nil
		},
		WarmWorkspace: func(ctx context.Context, workspace string) {
			_ = client.Warm(workspace)
		},
		DetectIdentity: DetectIdentity,
		RunSkill: func(ctx context.Context, skillName string, input any, workspace string, out any) error {
			return RunSkill(ctx, cfg, client, skillName, input, workspace, out)
		},
		AppendSummary: AppendSummaryNotes,
		TaskContinuity: func(ctx context.Context, workspace string) (string, error) {
			collector, cleanup, err := taskhistory.OpenCollector(ctx, cfg.Storage.Root, workspace, "")
			if err != nil {
				return "", err
			}
			defer cleanup()
			pack, err := collector.Collect(ctx, taskhistory.Options{
				WorkspacePath:          workspace,
				WorkspaceID:            workspaceID(workspace),
				TranscriptHistoryScope: taskhistory.DefaultTranscriptHistoryScope(),
			})
			if err != nil {
				return "", err
			}
			artifact, err := taskhistory.PersistPack(ctx, cfg.Paths.CAS, pack)
			if err != nil {
				return "", err
			}
			return taskhistory.RenderHookContextWithArtifact(pack, artifact), nil
		},
	}
}

func RunSkill(ctx context.Context, cfg config.Config, client *daemon.Client, skillName string, input any, workspace string, out any) error {
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return err
	}
	if client != nil {
		if client.IsRunning() || client.EnsureRunningContext(ctx) == nil {
			result, err := client.Run(skillName, inputJSON, workspace, true)
			if err == nil {
				return json.Unmarshal(result.Output, out)
			}
		}
	}
	handle, err := resolveSkillHandle(cfg, skillName)
	if err != nil {
		return err
	}
	var stdout bytes.Buffer
	executor := runservice.NewExecutor(ctx, cfg, handle, &stdout, io.Discard, runservice.RunOptions{
		SkillName: skillName,
		Workspace: workspace,
		Ephemeral: true,
		CacheMode: cache.ModeOff,
	})
	defer executor.Close()
	if err := executor.ExecuteEphemeral(inputJSON); err != nil {
		return err
	}
	return json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), out)
}

func resolveSkillHandle(cfg config.Config, requested string) (runservice.SkillHandle, error) {
	searchPaths := append([]string{}, skill.EnvSearchPaths()...)
	if cfg.Paths.Skills != "" {
		searchPaths = append(searchPaths, cfg.Paths.Skills)
	}
	searchPaths = append(searchPaths, skill.UserSearchPaths()...)
	searchPaths = append(searchPaths, skill.BuiltinSearchPaths()...)
	searchPaths = append(searchPaths, skill.DevSearchPaths()...)
	resolver := skill.NewResolver(skill.WithSearchPaths(skill.NormalizeSearchPaths(searchPaths)...))
	handle, err := resolver.Resolve(requested)
	if err != nil {
		return runservice.SkillHandle{}, err
	}
	manifest, artifactPath, err := skill.LoadManifestAndArtifact(handle.ManifestPath, skill.ArtifactOptions{})
	if err != nil {
		return runservice.SkillHandle{}, err
	}
	return runservice.SkillHandle{
		Manifest:     manifest,
		ManifestPath: handle.ManifestPath,
		ArtifactPath: artifactPath,
	}, nil
}

func DetectIdentity(workspacePath string) (sessionID, provider, agentID string) {
	if sid := strings.TrimSpace(os.Getenv("FOXCTL_SESSION_ID")); sid != "" {
		return sid, nonEmptyOr("foxctl", os.Getenv("FOXCTL_PROVIDER")), defaultAgentID("foxctl")
	}
	if sid := strings.TrimSpace(os.Getenv("CLAUDE_SESSION_ID")); sid != "" {
		return sid, "claude", defaultAgentID("claude")
	}
	if recent := loadRecentIdentity(workspacePath, time.Hour); recent.SessionID != "" {
		return recent.SessionID, nonEmptyOr("claude", recent.Provider), nonEmptyOr(defaultAgentID("claude"), recent.AgentID)
	}
	sid, detectedProvider, agentID, err := detectIdentityAuto(workspacePath)
	if err != nil || sid == "" {
		return "", "", ""
	}
	persistIdentity(workspacePath, sid, detectedProvider, agentID)
	return sid, detectedProvider, agentID
}

func detectIdentityAuto(workspacePath string) (sessionID, provider, agentID string, err error) {
	if sid := strings.TrimSpace(os.Getenv("CURSOR_SESSION_ID")); sid != "" {
		return sid, "cursor", defaultAgentID("cursor"), nil
	}
	if sid := strings.TrimSpace(os.Getenv("OPENCODE_SESSION_ID")); sid != "" {
		return sid, "opencode", defaultAgentID("opencode"), nil
	}
	sid, err := detectClaudeSession(workspacePath)
	if err != nil || sid == "" {
		return "", "", "", err
	}
	return sid, "claude", defaultAgentID("claude"), nil
}

func detectClaudeSession(workspacePath string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	projectDir := filepath.Join(home, ".claude", "projects", strings.ReplaceAll(strings.TrimSuffix(workspacePath, "/"), "/", "-"))
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return "", err
	}
	latestID := ""
	var latestMod time.Time
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latestMod) {
			latestMod = info.ModTime()
			latestID = sessionID
		}
	}
	return latestID, nil
}

func defaultAgentID(provider string) string {
	if env := strings.TrimSpace(os.Getenv("FOXCTL_AGENT_ID")); env != "" {
		return env
	}
	return provider
}

func nonEmptyOr(primary, fallback string) string {
	if strings.TrimSpace(fallback) != "" {
		return strings.TrimSpace(fallback)
	}
	return strings.TrimSpace(primary)
}

type identityFile struct {
	SessionID     string `json:"session_id"`
	AgentID       string `json:"agent_id,omitempty"`
	Provider      string `json:"provider,omitempty"`
	Workspace     string `json:"workspace,omitempty"`
	WorkspaceHash string `json:"workspace_hash,omitempty"`
	StartedAt     string `json:"started_at,omitempty"`
	LastActivity  string `json:"last_activity,omitempty"`
	DetectedFrom  string `json:"detected_from,omitempty"`
}

func loadRecentIdentity(workspacePath string, maxAge time.Duration) identityFile {
	path := identityPath(workspacePath)
	info, err := os.Stat(path)
	if err != nil {
		return identityFile{}
	}
	if time.Since(info.ModTime()) > maxAge {
		return identityFile{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return identityFile{}
	}
	var identity identityFile
	if err := json.Unmarshal(data, &identity); err != nil {
		return identityFile{}
	}
	return identity
}

func persistIdentity(workspacePath, sessionID, provider, agentID string) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	path := identityPath(workspacePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	body, err := json.MarshalIndent(identityFile{
		SessionID:     sessionID,
		AgentID:       agentID,
		Provider:      provider,
		Workspace:     workspacePath,
		WorkspaceHash: sessionkit.ComputeWorkspaceHash(workspacePath),
		StartedAt:     now,
		LastActivity:  now,
		DetectedFrom:  "go-hook",
	}, "", "  ")
	if err != nil {
		return
	}
	body = append(body, '\n')
	_ = os.WriteFile(path, body, 0o644)
}

func identityPath(workspacePath string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".foxctl", "sessions", "active", sessionkit.ComputeWorkspaceHash(workspacePath)+"-claude.json")
	}
	return filepath.Join(home, ".foxctl", "sessions", "active", sessionkit.ComputeWorkspaceHash(workspacePath)+"-claude.json")
}

func pendingRestoreMarkerPath(workspacePath string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".foxctl", "sessions", "pending-restore", sessionkit.ComputeWorkspaceHash(workspacePath)+".json")
	}
	return filepath.Join(home, ".foxctl", "sessions", "pending-restore", sessionkit.ComputeWorkspaceHash(workspacePath)+".json")
}

func AppendSummaryNotes(workspacePath string, prefs, gotchas, timeSinks []string) bool {
	changed := false
	if len(prefs) > 0 {
		path := filepath.Join(workspacePath, "configs", "USER_PREFS.md")
		header := "# User Preferences\n#\n# Append-only log of explicit user preferences discovered from session summaries.\n# Format: - YYYY-MM-DD: preference\n"
		lines := make([]string, 0, len(prefs))
		today := time.Now().Format("2006-01-02")
		for _, item := range prefs {
			item = strings.TrimSpace(item)
			if item != "" {
				lines = append(lines, "- "+today+": "+item)
			}
		}
		if len(lines) > 0 {
			_ = appendLines(path, header, lines)
			changed = true
		}
	}
	if len(gotchas) > 0 || len(timeSinks) > 0 {
		path := filepath.Join(workspacePath, "configs", "RECENT_GOTCHAS.md")
		header := "# Recent Errors, Gotchas, and Time Sinks\n#\n# Append-only log of recent errors/gotchas and slow-to-resolve issues.\n# Format: - YYYY-MM-DD [gotcha|time]: note\n"
		lines := make([]string, 0, len(gotchas)+len(timeSinks))
		today := time.Now().Format("2006-01-02")
		for _, item := range gotchas {
			item = strings.TrimSpace(item)
			if item != "" {
				lines = append(lines, "- "+today+" [gotcha]: "+item)
			}
		}
		for _, item := range timeSinks {
			item = strings.TrimSpace(item)
			if item != "" {
				lines = append(lines, "- "+today+" [time]: "+item)
			}
		}
		if len(lines) > 0 {
			_ = appendLines(path, header, lines)
			changed = true
		}
	}
	return changed
}

func appendLines(path, header string, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte(header), 0o644); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	for _, line := range lines {
		if _, err := io.WriteString(f, line+"\n"); err != nil {
			return err
		}
	}
	return nil
}

func Start(ctx context.Context, deps Dependencies, req StartRequest) (StartResponse, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(req.Workspace)))
	if target == "" {
		return StartResponse{}, fmt.Errorf("detect workspace")
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "startup"
	}

	response := StartResponse{
		Workspace: target,
		Source:    source,
	}
	orientationContext, err := orientWorkspace(ctx, deps.StorageRoot, target)
	if err != nil {
		return StartResponse{}, err
	}
	daemonReady := false
	if deps.EnsureDaemon != nil && !envEnabled("FOXCTL_DAEMON_DISABLED") {
		daemonReady = deps.EnsureDaemon(ctx, target)
	}
	response.DaemonReady = daemonReady
	if deps.WarmWorkspace != nil && !envEnabled("FOXCTL_WARMUP_DISABLED") && daemonReady {
		deps.WarmWorkspace(ctx, target)
	}
	if deps.DetectIdentity != nil {
		response.SessionID, response.Provider, response.AgentID = deps.DetectIdentity(target)
	}
	restoreContext := ""
	if daemonReady && deps.RunSkill != nil && (source == "resume" || source == "compact") {
		restoreContext, _ = restoreContextForStart(ctx, deps, target, source, response.SessionID)
	}
	response.Context = joinContext(orientationContext, restoreContext)
	if response.Context == "" && response.SessionID != "" {
		response.Context = "Session: " + response.SessionID
	}
	return response, nil
}

func End(ctx context.Context, deps Dependencies, req EndRequest) (EndResponse, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(req.Workspace)))
	if target == "" {
		return EndResponse{}, fmt.Errorf("detect workspace")
	}
	response := EndResponse{Workspace: target}
	if envEnabled("FOXCTL_SESSION_CAPTURE_DISABLED") || deps.RunSkill == nil {
		return response, nil
	}
	sessionID := sessionkit.ResolveSessionID(target, "")
	response.SessionID = sessionID

	var capture sessionCaptureEnvelope
	if err := deps.RunSkill(ctx, "session/capture", map[string]any{
		"workspace":  target,
		"session_id": nullableString(sessionID),
	}, target, &capture); err != nil {
		return response, nil
	}
	response.CaptureStatus = strings.TrimSpace(capture.Data.Status)
	response.CapturedSessionID = firstNonEmpty(strings.TrimSpace(capture.Data.SessionID), sessionID)
	if response.CaptureStatus == "" || (response.CaptureStatus != "captured" && response.CaptureStatus != "exists" && response.CaptureStatus != "scanned") {
		return response, nil
	}
	if envEnabledAny("FOXCTL_CONTEXTWIKI_DISABLED", "FOXCTL_ACA_DISABLED") {
		return response, nil
	}

	store := contextplane.NewWorkspaceStore(target)
	taskID, objective := acaContext(store, response.CapturedSessionID)
	summary := firstSummaryLine(req.Payload.AssistantText)
	if summary == "" {
		summary = firstNonEmpty(objective, "Session ended; see captured session artifacts.")
	}
	handoffPath, err := captureHandoff(store, taskID, "session_end", summary, "session:"+firstNonEmpty(response.CapturedSessionID, sessionID, "unknown"))
	if err == nil {
		response.HandoffPath = handoffPath
		recordInsights(store, target, summary, []string{"session:" + firstNonEmpty(response.CapturedSessionID, sessionID, "unknown")})
		if envEnabledAny("FOXCTL_CONTEXTWIKI_AUTO_PROMOTE", "FOXCTL_ACA_AUTO_PROMOTE") {
			response.PromotionDraft = autoPromote(store, handoffPath)
		}
	}
	if deps.AppendSummary != nil && deps.RunSkill != nil && strings.TrimSpace(os.Getenv("CEREBRAS_API_KEY")) != "" && response.CapturedSessionID != "" {
		var summarized sessionSummarizeEnvelope
		if err := deps.RunSkill(ctx, "session/summarize", map[string]any{
			"session_id": response.CapturedSessionID,
		}, target, &summarized); err == nil {
			response.SummaryWritten = deps.AppendSummary(target, summarized.Data.UserPreferences, summarized.Data.Gotchas, summarized.Data.TimeSinks)
		}
	}
	return response, nil
}

func SubagentStop(_ context.Context, deps Dependencies, req SubagentStopRequest) (SubagentStopResponse, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(req.Workspace)))
	if target == "" {
		return SubagentStopResponse{}, fmt.Errorf("detect workspace")
	}
	response := SubagentStopResponse{Workspace: target}
	if envEnabledAny("FOXCTL_CONTEXTWIKI_DISABLED", "FOXCTL_ACA_DISABLED") {
		return response, nil
	}
	store := contextplane.NewWorkspaceStore(target)
	sessionID := firstNonEmpty(os.Getenv("CLAUDE_SESSION_ID"), os.Getenv("FOXCTL_SESSION_ID"))
	agentID := firstNonEmpty(os.Getenv("CLAUDE_AGENT_ID"), os.Getenv("FOXCTL_AGENT_ID"), "subagent")
	taskID, objective := acaContext(store, agentID)
	summary := firstSummaryLine(req.Payload.AssistantText)
	if summary == "" {
		summary = firstNonEmpty(objective, "Subagent completed bounded work.")
	}
	response.TaskID = taskID
	handoffPath, err := captureHandoff(store, taskID, "subagent_stop", summary, "session:"+firstNonEmpty(sessionID, "unknown"))
	if err == nil {
		response.HandoffPath = handoffPath
		recordInsights(store, target, summary, []string{"agent:" + agentID})
		if envEnabledAny("FOXCTL_CONTEXTWIKI_AUTO_PROMOTE", "FOXCTL_ACA_AUTO_PROMOTE") {
			response.PromotionDraft = autoPromote(store, handoffPath)
		}
	}
	return response, nil
}

func RestorePostcompact(ctx context.Context, deps Dependencies, req PostcompactRestoreRequest) (PostcompactRestoreResponse, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(req.Workspace)))
	if target == "" {
		return PostcompactRestoreResponse{}, fmt.Errorf("detect workspace")
	}
	response := PostcompactRestoreResponse{
		Decision:  "approve",
		Workspace: target,
	}
	if deps.RunSkill == nil {
		return response, nil
	}
	markerPath := pendingRestoreMarkerPath(target)
	info, err := os.Stat(markerPath)
	if err != nil {
		return response, nil
	}
	if time.Since(info.ModTime()) > 10*time.Minute {
		_ = os.Remove(markerPath)
		return response, nil
	}
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return response, nil
	}
	var marker struct {
		SessionID string `json:"session_id"`
	}
	_ = json.Unmarshal(data, &marker)
	response.SessionID = strings.TrimSpace(marker.SessionID)
	_ = os.Remove(markerPath)

	var restore sessionRestoreEnvelope
	if err := deps.RunSkill(ctx, "session/restore", map[string]any{
		"workspace":  target,
		"session_id": nullableString(response.SessionID),
	}, target, &restore); err != nil {
		return response, nil
	}
	response.Context = strings.TrimSpace(restore.Data.HookOutput.Context)
	if deps.TaskContinuity != nil {
		if continuity, err := deps.TaskContinuity(ctx, target); err == nil && strings.TrimSpace(continuity) != "" {
			response.Context = joinContext(response.Context, continuity)
		}
	}
	return response, nil
}

func orientWorkspace(ctx context.Context, storageRoot, workspacePath string) (string, error) {
	taskStore, err := tasks.Open(ctx, storageRoot)
	if err != nil {
		return "", fmt.Errorf("open task store: %w", err)
	}
	defer func() { _ = taskStore.Close() }()
	sessionStore, err := sessions.Open(ctx, storageRoot)
	if err != nil {
		return "", fmt.Errorf("open session store: %w", err)
	}
	defer func() { _ = sessionStore.Close() }()
	orienter := contextplane.NewOrienter(taskStore, sessionStore)
	top, err := orienter.Build(ctx, workspacePath)
	if err != nil {
		return "", fmt.Errorf("build orientation: %w", err)
	}
	workspaceStore := contextplane.NewWorkspaceStore(workspacePath)
	layout, err := workspaceStore.SaveTopOfMind(top)
	if err != nil {
		return "", fmt.Errorf("persist orientation: %w", err)
	}
	body, err := os.ReadFile(layout.OrientationExportPath)
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(body)), nil
}

func restoreContextForStart(ctx context.Context, deps Dependencies, workspacePath, source, sessionID string) (string, error) {
	var restore sessionRestoreEnvelope
	if err := deps.RunSkill(ctx, "session/restore", map[string]any{
		"trigger":   source,
		"workspace": workspacePath,
	}, workspacePath, &restore); err != nil {
		return "", err
	}
	restoreContext := strings.TrimSpace(restore.Data.HookOutput.Context)
	var anchor sessionAnchorEnvelope
	if err := deps.RunSkill(ctx, "session/anchor", map[string]any{
		"operation":  "get",
		"workspace":  workspacePath,
		"session_id": sessionID,
	}, workspacePath, &anchor); err == nil && anchor.Data.Found && strings.TrimSpace(anchor.Data.Anchor.MainPrompt) != "" {
		var b strings.Builder
		b.WriteString("## Session Anchor\n\n**Goal:** ")
		b.WriteString(strings.TrimSpace(anchor.Data.Anchor.MainPrompt))
		if pending := strings.TrimSpace(anchor.Data.Anchor.PendingQuestion); pending != "" {
			b.WriteString("\n\n**Pending:** ")
			b.WriteString(pending)
		}
		return joinContext(b.String(), restoreContext), nil
	}
	return restoreContext, nil
}

func acaContext(store *contextplane.WorkspaceStore, fallbackTaskID string) (taskID, objective string) {
	top, err := store.LoadTopOfMind()
	if err != nil {
		return firstNonEmpty(fallbackTaskID, "hook-task"), ""
	}
	if len(top.ActiveTaskIDs) > 0 {
		taskID = top.ActiveTaskIDs[0]
	}
	return firstNonEmpty(taskID, fallbackTaskID, "hook-task"), strings.TrimSpace(top.Objective)
}

func captureHandoff(store *contextplane.WorkspaceStore, taskID, phase, summary, evidenceRef string) (string, error) {
	record := contextplane.Handoff{
		TaskID:       strings.TrimSpace(taskID),
		Phase:        phase,
		Outcome:      "partial",
		Summary:      strings.TrimSpace(summary),
		EvidenceRefs: contextplane.StringsToEvidenceRefs([]string{evidenceRef}),
	}
	return store.SaveHandoff(record)
}

func recordInsights(store *contextplane.WorkspaceStore, workspacePath, summary string, evidenceRefs []string) {
	inference := contextplane.InferInsights(summary, filepath.Base(workspacePath), "aca", evidenceRefs)
	for _, obs := range inference.Observations {
		_, _ = store.AppendObservation(obs)
	}
	for _, tension := range inference.Tensions {
		_, _ = store.AppendTension(tension)
	}
}

func autoPromote(store *contextplane.WorkspaceStore, handoffPath string) string {
	observations, err := store.ListObservations(10)
	if err == nil {
		for _, obs := range observations {
			if obs.Count < 2 {
				continue
			}
			sourceRef := "observation:" + obs.ID
			if promotionExists(store, sourceRef) {
				continue
			}
			draft, err := store.DraftPromotionFromObservation(obs.ID, "pattern", "")
			if err == nil {
				return draft.DraftPath
			}
		}
	}
	if handoffPath == "" {
		return ""
	}
	sourceRef := "handoff:" + filepath.Base(handoffPath)
	if promotionExists(store, sourceRef) {
		return ""
	}
	draft, err := store.DraftPromotionFromHandoff(handoffPath, "investigation", "")
	if err != nil {
		return ""
	}
	return draft.DraftPath
}

func promotionExists(store *contextplane.WorkspaceStore, sourceRef string) bool {
	jobs, err := store.ListPromotionJobs(50)
	if err != nil {
		return false
	}
	for _, job := range jobs {
		if strings.TrimSpace(job.SourceRef) == strings.TrimSpace(sourceRef) {
			return true
		}
	}
	return false
}

func joinContext(parts ...string) string {
	trimmed := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			trimmed = append(trimmed, part)
		}
	}
	return strings.Join(trimmed, "\n\n")
}

func firstSummaryLine(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	line, _, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(line)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func workspaceID(path string) string {
	return workspace.CanonicalID(path)
}

func nullableString(value string) any {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return nil
}

func envEnabledAny(names ...string) bool {
	for _, name := range names {
		if envEnabled(name) {
			return true
		}
	}
	return false
}

func envEnabled(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}
