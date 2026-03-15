package taskhistory

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/repoquery"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
	taskstore "github.com/jkatigb/agentctl/internal/storage/tasks"
)

type SessionSource interface {
	Get(ctx context.Context, id string) (storage.Session, error)
	List(ctx context.Context, opts storage.SessionListOptions) ([]storage.Session, error)
}

type sessionTimelineSource interface {
	GetContextWindows(ctx context.Context, sessionID string) ([]storage.ContextWindow, error)
	GetChunkSummaries(ctx context.Context, sessionID string, windowIndex int) ([]storage.SessionChunkSummary, error)
	GetTurns(ctx context.Context, sessionID string, opts storage.SessionTurnListOptions) ([]storage.SessionTurn, error)
}

type GitRunner interface {
	FileHistory(ctx context.Context, workspacePath, filePath string, limit int) ([]GitCommit, error)
}

type Options struct {
	WorkspacePath  string
	WorkspaceID    string
	TaskID         string
	SessionLimit   int
	HandoffLimit   int
	FileLimit      int
	GitCommitLimit int
	AnchorLimit    int
	NoteLimit      int
	DAGDepth       int
	DAGBudget      int
	PerNodeCap     int
}

type Pack struct {
	WorkspacePath string                       `json:"workspace_path"`
	WorkspaceID   string                       `json:"workspace_id"`
	GeneratedAt   time.Time                    `json:"generated_at"`
	Task          contextplane.TaskCandidate   `json:"task"`
	TaskPacket    contextplane.TaskPacket      `json:"task_packet"`
	Handoffs      []contextplane.HandoffRecord `json:"handoffs,omitempty"`
	FilesTouched  []string                     `json:"files_touched,omitempty"`
	ExternalRefs  []string                     `json:"external_refs,omitempty"`
	Sessions      []SessionSummary             `json:"sessions,omitempty"`
	GitHistory    []GitFileHistory             `json:"git_history,omitempty"`
	RepoAnchors   []repoquery.Anchor           `json:"repo_anchors,omitempty"`
	DAGAnchors    []repoquery.Anchor           `json:"dag_anchors,omitempty"`
	ACANotes      []contextplane.RetrievalHit  `json:"aca_notes,omitempty"`
	Summary       string                       `json:"summary,omitempty"`
}

type SessionSummary struct {
	ID                 string    `json:"id"`
	Reason             string    `json:"reason,omitempty"`
	ProjectName        string    `json:"project_name,omitempty"`
	Summary            string    `json:"summary,omitempty"`
	Accomplished       []string  `json:"accomplished,omitempty"`
	Decisions          []string  `json:"decisions,omitempty"`
	Gotchas            []string  `json:"gotchas,omitempty"`
	KeyFiles           []string  `json:"key_files,omitempty"`
	TimelineSummaries  []string  `json:"timeline_summaries,omitempty"`
	TimelineTools      []string  `json:"timeline_tools,omitempty"`
	TimelineFiles      []string  `json:"timeline_files,omitempty"`
	RecentFilesTouched []string  `json:"recent_files_touched,omitempty"`
	StartedAt          time.Time `json:"started_at,omitempty"`
	EndedAt            time.Time `json:"ended_at,omitempty"`
}

type GitCommit struct {
	Hash    string `json:"hash"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
}

type GitFileHistory struct {
	Path    string      `json:"path"`
	Commits []GitCommit `json:"commits,omitempty"`
}

type Collector struct {
	WorkspaceStore   *contextplane.WorkspaceStore
	TaskStore        taskstore.Store
	SessionStore     SessionSource
	RepoStore        *repoindex.Store
	VaultIndex       obsidianindex.Store
	SemanticProvider semantic.EmbeddingProvider
	GitRunner        GitRunner
}

func (c Collector) Collect(ctx context.Context, opts Options) (Pack, error) {
	if c.WorkspaceStore == nil {
		return Pack{}, fmt.Errorf("workspace store is required")
	}
	if c.TaskStore == nil {
		return Pack{}, fmt.Errorf("task store is required")
	}
	applyDefaults(&opts)

	taskID, err := c.selectTaskID(ctx, opts)
	if err != nil {
		return Pack{}, err
	}
	packet, err := c.WorkspaceStore.BuildTaskPacket(ctx, c.TaskStore, opts.WorkspaceID, taskID)
	if err != nil {
		return Pack{}, err
	}
	query := buildTaskQuery(packet)
	acaQuery := buildTaskACAQuery(packet)
	handoffs, err := c.relevantHandoffs(opts.HandoffLimit, packet.Task.ID)
	if err != nil {
		return Pack{}, err
	}
	filesTouched, externalRefs := collectFiles(opts.WorkspacePath, packet, handoffs)
	if len(filesTouched) > opts.FileLimit {
		filesTouched = filesTouched[:opts.FileLimit]
	}
	sessions, err := c.relevantSessions(ctx, packet, handoffs, filesTouched, opts.SessionLimit)
	if err != nil {
		return Pack{}, err
	}
	gitHistory := c.collectGitHistory(ctx, opts.WorkspacePath, filesTouched, opts.GitCommitLimit)
	repoAnchors, dagAnchors, err := c.collectRepoContext(ctx, query, opts)
	if err != nil {
		return Pack{}, err
	}
	notes, err := c.collectACANotes(ctx, acaQuery, opts.NoteLimit)
	if err != nil {
		return Pack{}, err
	}

	pack := Pack{
		WorkspacePath: opts.WorkspacePath,
		WorkspaceID:   opts.WorkspaceID,
		GeneratedAt:   time.Now().UTC(),
		Task:          packet.Task,
		TaskPacket:    packet,
		Handoffs:      handoffs,
		FilesTouched:  filesTouched,
		ExternalRefs:  externalRefs,
		Sessions:      sessions,
		GitHistory:    gitHistory,
		RepoAnchors:   repoAnchors,
		DAGAnchors:    dagAnchors,
		ACANotes:      notes,
	}
	pack.Summary = summarizePack(pack)
	return pack, nil
}

func applyDefaults(opts *Options) {
	if opts.SessionLimit <= 0 {
		opts.SessionLimit = 5
	}
	if opts.HandoffLimit <= 0 {
		opts.HandoffLimit = 10
	}
	if opts.FileLimit <= 0 {
		opts.FileLimit = 12
	}
	if opts.GitCommitLimit <= 0 {
		opts.GitCommitLimit = 3
	}
	if opts.AnchorLimit <= 0 {
		opts.AnchorLimit = 8
	}
	if opts.NoteLimit <= 0 {
		opts.NoteLimit = 5
	}
	if opts.DAGDepth <= 0 {
		opts.DAGDepth = 2
	}
	if opts.DAGBudget <= 0 {
		opts.DAGBudget = 80
	}
	if opts.PerNodeCap <= 0 {
		opts.PerNodeCap = 20
	}
}

func (c Collector) selectTaskID(ctx context.Context, opts Options) (string, error) {
	if taskID := strings.TrimSpace(opts.TaskID); taskID != "" {
		return taskID, nil
	}
	active, ok, err := c.TaskStore.GetActive(ctx, opts.WorkspaceID)
	if err == nil && ok {
		if continuityTaskLocalityScore(opts.WorkspacePath, active) >= 0 {
			return active.ID, nil
		}
	}
	items, err := c.TaskStore.ListWithOptions(ctx, opts.WorkspaceID, taskstore.ListOptions{
		Statuses: []string{
			taskstore.StatusInProgress,
			taskstore.StatusReadyForReview,
			taskstore.StatusPending,
			taskstore.StatusBlocked,
		},
		Limit: 50,
	})
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", fmt.Errorf("no eligible task found")
	}
	sort.SliceStable(items, func(i, j int) bool {
		si := continuityTaskLocalityScore(opts.WorkspacePath, items[i])
		sj := continuityTaskLocalityScore(opts.WorkspacePath, items[j])
		if si != sj {
			return si > sj
		}
		ri := continuityTaskRank(items[i].Status)
		rj := continuityTaskRank(items[j].Status)
		if ri != rj {
			return ri < rj
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items[0].ID, nil
}

func buildTaskQuery(packet contextplane.TaskPacket) string {
	title := strings.TrimSpace(packet.Task.Title)
	parts := []string{}
	if title != "" && !looksPathLike(title) {
		parts = append(parts, title)
	}
	if scope := strings.TrimSpace(packet.Task.ScopePath); scope != "" {
		parts = append(parts, filepath.Base(strings.TrimSuffix(scope, filepath.Ext(scope))))
	}
	if packet.LatestHandoff != nil && strings.TrimSpace(packet.LatestHandoff.Handoff.Summary) != "" {
		parts = append(parts, packet.LatestHandoff.Handoff.Summary)
	}
	if title == "" {
		if objective := strings.TrimSpace(packet.Objective); objective != "" {
			parts = append(parts, objective)
		}
	}
	if title == "" && strings.TrimSpace(packet.Task.Description) != "" {
		parts = append(parts, packet.Task.Description)
	}
	return normalizeQueryText(strings.Join(uniqueStrings(parts), " "))
}

func buildTaskACAQuery(packet contextplane.TaskPacket) string {
	if title := strings.TrimSpace(packet.Task.Title); title != "" {
		return normalizeQueryText(title)
	}
	return buildTaskQuery(packet)
}

func (c Collector) relevantHandoffs(limit int, taskID string) ([]contextplane.HandoffRecord, error) {
	records, err := c.WorkspaceStore.ListHandoffs(limit * 4)
	if err != nil {
		return nil, err
	}
	out := make([]contextplane.HandoffRecord, 0, limit)
	for _, record := range records {
		if strings.TrimSpace(record.Handoff.TaskID) != strings.TrimSpace(taskID) {
			continue
		}
		out = append(out, record)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func collectFiles(workspacePath string, packet contextplane.TaskPacket, handoffs []contextplane.HandoffRecord) ([]string, []string) {
	paths := make([]string, 0, 16)
	external := make([]string, 0, 8)
	appendPath := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if rel, ok := relWithinWorkspace(workspacePath, path); ok {
			paths = append(paths, rel)
			return
		}
		external = append(external, path)
	}
	if scope := strings.TrimSpace(packet.Task.ScopePath); scope != "" {
		appendPath(scope)
	}
	if plan := strings.TrimSpace(packet.Task.PlanFile); plan != "" {
		appendPath(plan)
	}
	for _, ref := range packet.RelevantRefs {
		if trimmed, ok := trimPathRef(ref); ok {
			appendPath(trimmed)
		}
	}
	for _, handoff := range handoffs {
		for _, path := range handoff.Handoff.FilesTouched {
			appendPath(path)
		}
		for _, ref := range handoff.Handoff.EvidenceRefs {
			if trimmed, ok := trimPathRef(ref); ok {
				appendPath(trimmed)
			}
		}
	}
	return uniqueStrings(paths), uniqueStrings(external)
}

func (c Collector) relevantSessions(ctx context.Context, packet contextplane.TaskPacket, handoffs []contextplane.HandoffRecord, filesTouched []string, limit int) ([]SessionSummary, error) {
	if c.SessionStore == nil {
		return nil, nil
	}
	seen := map[string]struct{}{}
	var sessions []SessionSummary
	appendSession := func(session storage.Session, reason string) {
		if strings.TrimSpace(session.ID) == "" {
			return
		}
		if _, ok := seen[session.ID]; ok {
			return
		}
		seen[session.ID] = struct{}{}
		summary := SessionSummary{
			ID:           session.ID,
			Reason:       reason,
			ProjectName:  session.ProjectName,
			Summary:      session.Summary,
			Accomplished: append([]string(nil), session.Accomplished...),
			Decisions:    append([]string(nil), session.Decisions...),
			Gotchas:      append([]string(nil), session.Gotchas...),
			KeyFiles:     append([]string(nil), session.KeyFiles...),
			StartedAt:    session.StartedAt,
			EndedAt:      session.EndedAt,
		}
		c.enrichSessionSummary(ctx, &summary)
		sessions = append(sessions, summary)
	}

	if sid := strings.TrimSpace(packet.Task.SessionID); sid != "" {
		session, err := c.SessionStore.Get(ctx, sid)
		if err == nil {
			appendSession(session, "task_session")
		}
	}
	for _, sid := range collectSessionRefs(packet, handoffs) {
		session, err := c.SessionStore.Get(ctx, sid)
		if err == nil {
			appendSession(session, "handoff_session")
		}
	}

	workspaceSessions, err := c.SessionStore.List(ctx, storage.SessionListOptions{
		WorkspaceID:   packet.WorkspaceID,
		WorkspacePath: "",
		Limit:         limit * 4,
	})
	if err != nil {
		return sessions, nil
	}
	type scored struct {
		session storage.Session
		score   int
		reason  string
	}
	candidates := make([]scored, 0, len(workspaceSessions))
	for _, session := range workspaceSessions {
		if _, ok := seen[session.ID]; ok {
			continue
		}
		score := 0
		reason := ""
		for _, keyFile := range session.KeyFiles {
			if containsString(filesTouched, keyFile) {
				score += 5
				reason = "key_file_overlap"
			}
		}
		if strings.Contains(strings.ToLower(session.Summary), strings.ToLower(packet.Task.Title)) {
			score += 3
			if reason == "" {
				reason = "summary_match"
			}
		}
		if score <= 0 {
			continue
		}
		candidates = append(candidates, scored{session: session, score: score, reason: reason})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].session.StartedAt.After(candidates[j].session.StartedAt)
	})
	for _, candidate := range candidates {
		appendSession(candidate.session, candidate.reason)
		if len(sessions) >= limit {
			break
		}
	}
	return sessions, nil
}

func (c Collector) enrichSessionSummary(ctx context.Context, summary *SessionSummary) {
	if summary == nil || c.SessionStore == nil {
		return
	}
	source, ok := c.SessionStore.(sessionTimelineSource)
	if !ok {
		return
	}
	windows, err := source.GetContextWindows(ctx, summary.ID)
	if err == nil && len(windows) > 0 {
		sort.SliceStable(windows, func(i, j int) bool {
			return windows[i].WindowIndex > windows[j].WindowIndex
		})
		for _, window := range windows[:minInt(len(windows), 2)] {
			if strings.TrimSpace(window.Summary) != "" {
				summary.TimelineSummaries = append(summary.TimelineSummaries, window.Summary)
			}
			chunkSummaries, err := source.GetChunkSummaries(ctx, summary.ID, window.WindowIndex)
			if err == nil {
				for _, chunk := range chunkSummaries {
					if strings.TrimSpace(chunk.Summary) != "" {
						summary.TimelineSummaries = append(summary.TimelineSummaries, chunk.Summary)
					}
					summary.TimelineTools = append(summary.TimelineTools, chunk.Tools...)
					summary.TimelineFiles = append(summary.TimelineFiles, chunk.Files...)
				}
			}
		}
	}
	turns, err := source.GetTurns(ctx, summary.ID, storage.SessionTurnListOptions{Limit: 12})
	if err == nil {
		for _, turn := range turns {
			summary.RecentFilesTouched = append(summary.RecentFilesTouched, turn.FilesTouched...)
		}
	}
	summary.TimelineSummaries = uniqueStrings(summary.TimelineSummaries)
	summary.TimelineTools = uniqueStrings(summary.TimelineTools)
	summary.TimelineFiles = uniqueStrings(summary.TimelineFiles)
	summary.RecentFilesTouched = uniqueStrings(summary.RecentFilesTouched)
}

func (c Collector) collectGitHistory(ctx context.Context, workspacePath string, files []string, commitLimit int) []GitFileHistory {
	if c.GitRunner == nil || strings.TrimSpace(workspacePath) == "" {
		return nil
	}
	out := make([]GitFileHistory, 0, len(files))
	for _, file := range files {
		commits, err := c.GitRunner.FileHistory(ctx, workspacePath, file, commitLimit)
		if err != nil || len(commits) == 0 {
			continue
		}
		out = append(out, GitFileHistory{
			Path:    file,
			Commits: commits,
		})
	}
	return out
}

func (c Collector) collectRepoContext(ctx context.Context, query string, opts Options) ([]repoquery.Anchor, []repoquery.Anchor, error) {
	if c.RepoStore == nil || strings.TrimSpace(query) == "" {
		return nil, nil, nil
	}
	service := repoquery.NewQueryService(repoindex.NewQueryEngine(c.RepoStore))
	searchOut, err := service.SearchWithProjection(ctx, repoquery.SearchRequest{
		Query: query,
		Limit: opts.AnchorLimit,
	})
	if err != nil {
		return nil, nil, err
	}
	dagOut, err := service.DAGGrepWithProjection(ctx, repoquery.DAGGrepRequest{
		Query:          query,
		K:              3,
		EdgeTypes:      repoindex.EdgeSetStructural,
		Direction:      repoindex.DirOut,
		Depth:          opts.DAGDepth,
		Budget:         opts.DAGBudget,
		PerNodeCap:     opts.PerNodeCap,
		IncludeAnchors: true,
	})
	if err != nil {
		return searchOut.Anchors, nil, nil
	}
	return searchOut.Anchors, dagOut.Anchors, nil
}

func (c Collector) collectACANotes(ctx context.Context, query string, noteLimit int) ([]contextplane.RetrievalHit, error) {
	if c.VaultIndex == nil || strings.TrimSpace(query) == "" {
		return nil, nil
	}
	result, err := c.WorkspaceStore.Retrieve(ctx, c.VaultIndex, c.RepoStore, c.SemanticProvider, query, noteLimit)
	if err != nil {
		return nil, err
	}
	return result.VaultHits, nil
}

func summarizePack(pack Pack) string {
	parts := []string{
		fmt.Sprintf("Task %q (%s)", pack.Task.Title, pack.Task.Status),
		fmt.Sprintf("%d handoff(s)", len(pack.Handoffs)),
		fmt.Sprintf("%d file(s)", len(pack.FilesTouched)),
		fmt.Sprintf("%d session(s)", len(pack.Sessions)),
		fmt.Sprintf("%d repo anchor(s)", len(pack.RepoAnchors)),
		fmt.Sprintf("%d dag anchor(s)", len(pack.DAGAnchors)),
		fmt.Sprintf("%d ACA note(s)", len(pack.ACANotes)),
	}
	if len(pack.FilesTouched) > 0 {
		parts = append(parts, "top files: "+strings.Join(shortenStrings(pack.FilesTouched, 3), ", "))
	}
	if len(pack.ExternalRefs) > 0 {
		parts = append(parts, "external refs: "+strings.Join(shortenStrings(pack.ExternalRefs, 2), ", "))
	}
	return strings.Join(parts, " | ")
}

func shortenStrings(items []string, limit int) []string {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func uniqueStrings(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = filepath.ToSlash(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func normalizeQueryText(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(strings.ToLower(part))
		if len(part) < 2 {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return strings.Join(out, " ")
}

func continuityTaskLocalityScore(workspacePath string, task taskstore.Task) int {
	score := 0
	if rel, ok := relWithinWorkspace(workspacePath, strings.TrimSpace(task.ScopePath)); ok && rel != "" {
		score += 10
	}
	if rel, ok := relWithinWorkspace(workspacePath, strings.TrimSpace(task.PlanFile)); ok && rel != "" {
		score += 8
	}
	if score == 0 {
		score = -5
	}
	return score
}

func continuityTaskRank(status string) int {
	switch strings.TrimSpace(status) {
	case taskstore.StatusInProgress:
		return 0
	case taskstore.StatusReadyForReview:
		return 1
	case taskstore.StatusPending:
		return 2
	case taskstore.StatusBlocked:
		return 3
	default:
		return 99
	}
}

func containsString(items []string, want string) bool {
	want = filepath.ToSlash(strings.TrimSpace(want))
	for _, item := range items {
		if filepath.ToSlash(strings.TrimSpace(item)) == want {
			return true
		}
	}
	return false
}

func looksPathLike(value string) bool {
	value = strings.TrimSpace(value)
	return strings.Contains(value, "/") || strings.Contains(value, "\\") || strings.Contains(value, ".md")
}

func relWithinWorkspace(workspacePath, path string) (string, bool) {
	workspacePath = strings.TrimSpace(workspacePath)
	path = strings.TrimSpace(path)
	if workspacePath == "" || path == "" {
		return "", false
	}
	if !filepath.IsAbs(path) {
		return filepath.ToSlash(path), true
	}
	rel, err := filepath.Rel(workspacePath, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func collectSessionRefs(packet contextplane.TaskPacket, handoffs []contextplane.HandoffRecord) []string {
	var refs []string
	appendRef := func(ref string) {
		ref = strings.TrimSpace(ref)
		if strings.HasPrefix(ref, "session:") {
			refs = append(refs, strings.TrimPrefix(ref, "session:"))
		}
	}
	for _, ref := range packet.RelevantRefs {
		appendRef(ref)
	}
	if packet.LatestHandoff != nil {
		for _, ref := range packet.LatestHandoff.Handoff.EvidenceRefs {
			appendRef(ref)
		}
	}
	for _, handoff := range handoffs {
		for _, ref := range handoff.Handoff.EvidenceRefs {
			appendRef(ref)
		}
	}
	return uniqueStrings(refs)
}

func trimPathRef(ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, "path:") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(ref, "path:")), true
}

type DefaultGitRunner struct{}

func (DefaultGitRunner) FileHistory(ctx context.Context, workspacePath, filePath string, limit int) ([]GitCommit, error) {
	if limit <= 0 {
		limit = 3
	}
	cmd := exec.CommandContext(ctx, "git", "-C", workspacePath, "log", fmt.Sprintf("-n%d", limit), "--date=short", "--format=%H%x1f%ad%x1f%s", "--", filePath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git log %s: %w (%s)", filePath, err, strings.TrimSpace(stderr.String()))
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	commits := make([]GitCommit, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x1f", 3)
		if len(parts) != 3 {
			continue
		}
		commits = append(commits, GitCommit{
			Hash:    parts[0],
			Date:    parts[1],
			Subject: parts[2],
		})
	}
	return commits, nil
}
