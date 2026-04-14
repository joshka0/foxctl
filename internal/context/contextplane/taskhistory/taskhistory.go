package taskhistory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/jkatigb/agentctl/internal/context/contextplane"
	"github.com/jkatigb/agentctl/internal/context/transcriptpipeline"
	tphistory "github.com/jkatigb/agentctl/internal/context/transcriptpipeline/history"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/intelligence/repoquery"
	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
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

type TranscriptHistoryScope string

const (
	TranscriptHistoryScopeAuto      TranscriptHistoryScope = "auto"
	TranscriptHistoryScopeWorkspace TranscriptHistoryScope = "workspace"
	TranscriptHistoryScopeFamily    TranscriptHistoryScope = "family"
)

func ParseTranscriptHistoryScope(raw string) (TranscriptHistoryScope, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(TranscriptHistoryScopeAuto):
		return TranscriptHistoryScopeAuto, nil
	case string(TranscriptHistoryScopeWorkspace):
		return TranscriptHistoryScopeWorkspace, nil
	case string(TranscriptHistoryScopeFamily):
		return TranscriptHistoryScopeFamily, nil
	default:
		return "", fmt.Errorf("unsupported transcript history scope %q", raw)
	}
}

type Options struct {
	WorkspacePath          string
	WorkspaceID            string
	TaskID                 string
	SessionLimit           int
	HandoffLimit           int
	FileLimit              int
	GitCommitLimit         int
	AnchorLimit            int
	NoteLimit              int
	DAGDepth               int
	DAGBudget              int
	PerNodeCap             int
	TranscriptHistoryScope TranscriptHistoryScope
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
	Transcript    *TranscriptHistory           `json:"transcript,omitempty"`
	Summary       string                       `json:"summary,omitempty"`
}

type TranscriptHistory struct {
	Overview            string                 `json:"overview,omitempty"`
	ObjectiveLabel      string                 `json:"objective_label,omitempty"`
	RequestedScope      TranscriptHistoryScope `json:"requested_scope,omitempty"`
	AppliedScope        TranscriptHistoryScope `json:"applied_scope,omitempty"`
	WorkspacePath       string                 `json:"workspace_path,omitempty"`
	FamilyPath          string                 `json:"family_path,omitempty"`
	AgentBrief          string                 `json:"agent_brief,omitempty"`
	HumanBrief          []string               `json:"human_brief,omitempty"`
	ContinueWith        []string               `json:"continue_with,omitempty"`
	WatchOutFor         []string               `json:"watch_out_for,omitempty"`
	Regressions         []string               `json:"regressions,omitempty"`
	RecurringMistakes   []string               `json:"recurring_mistakes,omitempty"`
	RecentLearnings     []string               `json:"recent_learnings,omitempty"`
	RecentSurprises     []string               `json:"recent_surprises,omitempty"`
	RetrievedBrief      string                 `json:"retrieved_brief,omitempty"`
	RetrievedHighlights []string               `json:"retrieved_highlights,omitempty"`
	EvidenceRefs        []string               `json:"evidence_refs,omitempty"`
	SourceNames         []string               `json:"source_names,omitempty"`
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
	MemoryStore      storage.MemoryStore
	RepoStore        *repoindex.Store
	VaultIndex       obsidianindex.Store
	SemanticProvider semantic.EmbeddingProvider
	TranscriptWorker *TranscriptSummaryWorker
	TranscriptRun    TranscriptSummaryRunFunc
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
	pack.Transcript, _ = c.collectTranscriptHistory(ctx, opts.WorkspacePath, buildTaskACAQuery(packet), opts.TranscriptHistoryScope)
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
	if opts.TranscriptHistoryScope == "" {
		opts.TranscriptHistoryScope = TranscriptHistoryScopeAuto
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

func (c Collector) collectTranscriptHistory(ctx context.Context, workspacePath, query string, scope TranscriptHistoryScope) (*TranscriptHistory, error) {
	if c.MemoryStore == nil {
		return nil, nil
	}
	workspacePath = ws.Normalize(strings.TrimSpace(workspacePath))
	familyPath := ws.FamilyPath(workspacePath)
	query = strings.TrimSpace(query)
	if workspacePath == "" || query == "" {
		return nil, nil
	}
	scope = normalizeTranscriptHistoryScope(scope)
	searchScopes := []TranscriptHistoryScope{scope}
	if scope == TranscriptHistoryScopeAuto {
		searchScopes = []TranscriptHistoryScope{TranscriptHistoryScopeWorkspace}
		if familyPath != "" && ws.Normalize(familyPath) != ws.Normalize(workspacePath) {
			searchScopes = append(searchScopes, TranscriptHistoryScopeFamily)
		}
	}
	var selectedScope TranscriptHistoryScope
	var entries []storage.NamedEntry
	for _, candidateScope := range searchScopes {
		entries = c.searchTranscriptHistoryAnswers(ctx, workspacePath, familyPath, query, candidateScope, 8)
		if len(entries) == 0 {
			continue
		}
		selectedScope = candidateScope
		break
	}
	if len(entries) == 0 {
		for _, candidateScope := range searchScopes {
			entries = c.latestTranscriptHistoryAnswers(ctx, workspacePath, familyPath, candidateScope, 8)
			if len(entries) == 0 {
				continue
			}
			selectedScope = candidateScope
			break
		}
	}
	if len(entries) == 0 {
		return nil, nil
	}
	answers := make([]tphistory.HistoryAnswer, 0, len(entries))
	sourceNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		answer, ok := historyAnswerFromMemoryEntry(entry)
		if !ok {
			continue
		}
		answers = append(answers, answer)
		sourceNames = append(sourceNames, entry.Name)
	}
	pack := tphistory.BuildHistoryPack(answers)
	if pack == nil {
		return nil, nil
	}
	ownerPrefix := transcriptHistoryOwnerPrefix(entries[0].Name)
	if selectedScope == "" {
		selectedScope = scope
	}
	retrieved := c.collectTranscriptRetrievedBundle(ctx, workspacePath, familyPath, query, ownerPrefix, selectedScope, 10)
	support := c.collectTranscriptSupport(ctx, workspacePath, familyPath, query, ownerPrefix, selectedScope, 6)
	recurring := c.collectTranscriptRecurringMistakes(ctx, workspacePath, familyPath, selectedScope, 3, TranscriptHistoryDateRange{})
	continueWith := appendUniqueLocalStrings(append([]string(nil), pack.ContinueWith...), retrieved.continueWith, 4)
	watchOutFor := appendUniqueLocalStrings(support.watchOutFor, retrieved.watchOutFor, 4)
	recentLearnings := appendUniqueLocalStrings(support.recentLearnings, retrieved.recentLearnings, 3)
	recentSurprises := appendUniqueLocalStrings(support.recentSurprises, retrieved.recentSurprises, 3)
	if sameNormalizedStringSet(recentLearnings, pack.AcceptedLearnings) {
		recentLearnings = nil
	}
	evidenceRefs := appendUniqueLocalStrings(support.evidenceRefs, retrieved.evidenceRefs, 8)
	sourceNames = appendUniqueLocalStrings(sourceNames, retrieved.sourceNames, 8)
	agentBrief := strings.TrimSpace(pack.AgentBrief)
	if strings.TrimSpace(retrieved.brief) != "" {
		agentBrief = mergeTranscriptBrief(agentBrief, retrieved.brief)
	}
	if len(watchOutFor) > 0 {
		agentBrief = ensureTranscriptBriefLine(agentBrief, "Watch out for: "+strings.Join(watchOutFor, " | "))
	}
	if len(pack.Regressions) > 0 {
		agentBrief = ensureTranscriptBriefLine(agentBrief, "Regressions: "+strings.Join(pack.Regressions, " | "))
	}
	if len(recurring) > 0 {
		agentBrief = ensureTranscriptBriefLine(agentBrief, "Recurring mistakes: "+strings.Join(recurring, " | "))
	} else if len(pack.RecurringMistakes) > 0 {
		agentBrief = ensureTranscriptBriefLine(agentBrief, "Recurring mistakes: "+strings.Join(pack.RecurringMistakes, " | "))
	}
	if len(recentLearnings) > 0 && !transcriptBriefHasOverlappingLearning(agentBrief, recentLearnings) {
		agentBrief = ensureTranscriptBriefLine(agentBrief, "Recent learnings: "+strings.Join(recentLearnings, " | "))
	}
	if len(recentSurprises) > 0 {
		agentBrief = ensureTranscriptBriefLine(agentBrief, "Recent surprises: "+strings.Join(recentSurprises, " | "))
	}
	humanBrief := append([]string(nil), pack.HumanBrief...)
	if len(continueWith) > 0 {
		humanBrief = appendUniqueLocalStrings(humanBrief, []string{"Continue with: " + strings.Join(continueWith, " | ")}, 10)
	}
	if len(watchOutFor) > 0 {
		humanBrief = append(humanBrief, "Transcript watch-outs: "+strings.Join(watchOutFor, " | "))
	}
	if len(pack.Regressions) > 0 {
		humanBrief = append(humanBrief, "Transcript regressions: "+strings.Join(pack.Regressions, " | "))
	}
	if len(recurring) > 0 {
		humanBrief = append(humanBrief, "Recurring mistakes: "+strings.Join(recurring, " | "))
	} else if len(pack.RecurringMistakes) > 0 {
		humanBrief = append(humanBrief, "Recurring mistakes: "+strings.Join(pack.RecurringMistakes, " | "))
	}
	if len(recentLearnings) > 0 {
		humanBrief = append(humanBrief, "Transcript learnings: "+strings.Join(recentLearnings, " | "))
	}
	if len(recentSurprises) > 0 {
		humanBrief = append(humanBrief, "Transcript surprises: "+strings.Join(recentSurprises, " | "))
	}
	if len(retrieved.highlights) > 0 {
		humanBrief = append(humanBrief, "Transcript highlights: "+strings.Join(retrieved.highlights, " | "))
	}
	return &TranscriptHistory{
		RequestedScope:      scope,
		AppliedScope:        selectedScope,
		WorkspacePath:       workspacePath,
		FamilyPath:          familyPath,
		Overview:            strings.TrimSpace(pack.Overview),
		ObjectiveLabel:      strings.TrimSpace(pack.ObjectiveLabel),
		AgentBrief:          agentBrief,
		HumanBrief:          humanBrief,
		ContinueWith:        continueWith,
		WatchOutFor:         watchOutFor,
		Regressions:         pack.Regressions,
		RecurringMistakes:   recurringOrPack(recurring, pack.RecurringMistakes),
		RecentLearnings:     recentLearnings,
		RecentSurprises:     recentSurprises,
		RetrievedBrief:      strings.TrimSpace(retrieved.brief),
		RetrievedHighlights: retrieved.highlights,
		EvidenceRefs:        evidenceRefs,
		SourceNames:         appendUniqueLocalStrings(sourceNames, support.sourceNames, 8),
	}, nil
}

type transcriptSupportBundle struct {
	watchOutFor     []string
	recentLearnings []string
	recentSurprises []string
	evidenceRefs    []string
	sourceNames     []string
}

type transcriptRetrievedBundle struct {
	brief           string
	continueWith    []string
	watchOutFor     []string
	recentLearnings []string
	recentSurprises []string
	highlights      []string
	evidenceRefs    []string
	sourceNames     []string
}

type transcriptRetrievedSummaryPayload struct {
	ContinueWith    []string `json:"continue_with"`
	RecentLearnings []string `json:"recent_learnings"`
	WatchOutFor     []string `json:"watch_out_for"`
	RecentSurprises []string `json:"recent_surprises"`
	Highlights      []string `json:"highlights"`
	Brief           string   `json:"brief"`
}

type transcriptRecordCandidate struct {
	entry storage.NamedEntry
	score float64
}

const transcriptRetrievedSummaryPromptV1 = `Summarize retrieved transcript-history records for continuity.
Return only valid JSON:
{"continue_with":["..."],"recent_learnings":["..."],"watch_out_for":["..."],"recent_surprises":["..."],"highlights":["..."],"brief":"..."}

Rules:
- Use only the retrieved records provided; do not invent context.
- Prefer concrete continuation work, durable learnings, real blockers, and surprising findings.
- Drop duplicate phrasings, commit chatter, and low-signal path inventory.
- Keep each list item short and human-readable.
- If the same learning appears in both brief and recent_learnings, use exactly the same canonical wording in both places instead of paraphrasing it twice.
- brief should be a compact multiline continuity brief using only the selected items.`

type recurringTranscriptCluster struct {
	summary   string
	owners    map[string]struct{}
	updatedAt time.Time
}

func (c Collector) searchTranscriptHistoryAnswers(ctx context.Context, workspacePath, familyPath, query string, scope TranscriptHistoryScope, limit int) []storage.NamedEntry {
	if c.MemoryStore == nil {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	type ownerBucket struct {
		score   int
		entries []storage.NamedEntry
		seen    map[string]struct{}
	}
	owners := map[string]*ownerBucket{}
	appendEntry := func(entry storage.NamedEntry, rank int) {
		if entry.Type != "history_answer" {
			return
		}
		owner := transcriptHistoryOwnerPrefix(entry.Name)
		if owner == "" {
			return
		}
		bucket, ok := owners[owner]
		if !ok {
			bucket = &ownerBucket{seen: map[string]struct{}{}}
			owners[owner] = bucket
		}
		key := strings.TrimSpace(entry.Name) + "|" + strings.TrimSpace(entry.Workspace)
		if _, ok := bucket.seen[key]; ok {
			return
		}
		bucket.seen[key] = struct{}{}
		bucket.entries = append(bucket.entries, entry)
		weight := (limit * 3) - rank
		if weight < 1 {
			weight = 1
		}
		bucket.score += weight
	}

	addScoredEntries := func(scored []storage.ScoredEntry) {
		for idx, item := range scored {
			appendEntry(item.Entry, idx)
		}
	}
	if c.SemanticProvider != nil {
		if vec, err := c.SemanticProvider.Embed(ctx, query); err == nil && len(vec) > 0 {
			if scored, err := c.MemoryStore.SearchSimilarByType(ctx, workspacePath, "history_answer", vec, limit); err == nil {
				addScoredEntries(filterTranscriptEntriesByScope(scored, workspacePath, familyPath, scope))
			}
		}
	}
	addScoredEntries(c.lexicalHistoryEntries(ctx, workspacePath, familyPath, []string{"history_answer"}, query, "", scope, limit*3))
	if len(owners) == 0 {
		return nil
	}
	type rankedOwner struct {
		prefix string
		score  int
		count  int
	}
	ranked := make([]rankedOwner, 0, len(owners))
	for prefix, bucket := range owners {
		ranked = append(ranked, rankedOwner{prefix: prefix, score: bucket.score, count: len(bucket.entries)})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		if ranked[i].count != ranked[j].count {
			return ranked[i].count > ranked[j].count
		}
		return ranked[i].prefix < ranked[j].prefix
	})
	ownerLimit := limit
	if profileLimit := len(tphistory.DefaultHistoryProfile().Questions) + 4; profileLimit > ownerLimit {
		ownerLimit = profileLimit
	}
	return c.ownerHistoryEntries(ctx, workspacePath, familyPath, []string{"history_answer"}, ranked[0].prefix, scope, ownerLimit, TranscriptHistoryDateRange{})
}

func (c Collector) latestTranscriptHistoryAnswers(ctx context.Context, workspacePath, familyPath string, scope TranscriptHistoryScope, limit int) []storage.NamedEntry {
	if c.MemoryStore == nil {
		return nil
	}
	if limit <= 0 {
		limit = 8
	}
	entries, _, err := c.MemoryStore.ListFiltered(ctx, workspacePath, storage.MemoryListFilter{Types: []string{"history_answer"}}, 200, 0)
	if err != nil || len(entries) == 0 {
		return nil
	}
	for _, entry := range entries {
		if !matchesTranscriptHistoryScope(entry, workspacePath, familyPath, scope) {
			continue
		}
		ownerPrefix := transcriptHistoryOwnerPrefix(entry.Name)
		if ownerPrefix == "" {
			continue
		}
		ownerLimit := limit
		if profileLimit := len(tphistory.DefaultHistoryProfile().Questions) + 4; profileLimit > ownerLimit {
			ownerLimit = profileLimit
		}
		return c.ownerHistoryEntries(ctx, workspacePath, familyPath, []string{"history_answer"}, ownerPrefix, scope, ownerLimit, TranscriptHistoryDateRange{})
	}
	return nil
}

func (c Collector) collectTranscriptSupport(ctx context.Context, workspacePath, familyPath, query, ownerPrefix string, scope TranscriptHistoryScope, limit int) transcriptSupportBundle {
	var bundle transcriptSupportBundle
	if c.MemoryStore == nil || strings.TrimSpace(ownerPrefix) == "" {
		return bundle
	}
	for _, entry := range c.searchTranscriptHistorySupportingEntries(ctx, workspacePath, familyPath, query, ownerPrefix, scope, limit) {
		sourceName := strings.TrimSpace(entry.Name)
		switch entry.Type {
		case "history_notable":
			notable, ok := notableHistoryFromMemoryEntry(entry)
			if !ok {
				continue
			}
			switch notable.Kind {
			case transcriptpipeline.NotableInsightMisunderstanding, transcriptpipeline.NotableInsightGotcha:
				bundle.watchOutFor = appendUniqueLocalStrings(bundle.watchOutFor, []string{notable.Summary}, 4)
			case transcriptpipeline.NotableInsightSurprise:
				bundle.recentSurprises = appendUniqueLocalStrings(bundle.recentSurprises, []string{notable.Summary}, 3)
			case transcriptpipeline.NotableInsightProceduralLearning:
				bundle.recentLearnings = appendUniqueLocalStrings(bundle.recentLearnings, []string{notable.Summary}, 3)
			}
			bundle.evidenceRefs = appendUniqueLocalStrings(bundle.evidenceRefs, notable.EvidenceRefs, 6)
			bundle.sourceNames = appendUniqueLocalStrings(bundle.sourceNames, []string{sourceName}, 6)
		case "history_insight":
			insight, ok := insightHistoryFromMemoryEntry(entry)
			if !ok {
				continue
			}
			if (insight.Status == transcriptpipeline.InsightStatusAccepted || insight.Status == transcriptpipeline.InsightStatusSupported) &&
				(insight.Kind == transcriptpipeline.InsightKindDecision || insight.Kind == transcriptpipeline.InsightKindPreference || insight.Kind == transcriptpipeline.InsightKindDirection) {
				bundle.recentLearnings = appendUniqueLocalStrings(bundle.recentLearnings, []string{insight.Summary}, 3)
			}
			bundle.evidenceRefs = appendUniqueLocalStrings(bundle.evidenceRefs, insight.EvidenceRefs, 6)
			bundle.sourceNames = appendUniqueLocalStrings(bundle.sourceNames, []string{sourceName}, 6)
		}
	}
	return bundle
}

func (c Collector) collectTranscriptRetrievedBundle(ctx context.Context, workspacePath, familyPath, query, ownerPrefix string, scope TranscriptHistoryScope, limit int) transcriptRetrievedBundle {
	var bundle transcriptRetrievedBundle
	if c.MemoryStore == nil || strings.TrimSpace(ownerPrefix) == "" {
		return bundle
	}
	entries := c.searchTranscriptHistoryBundleEntries(ctx, workspacePath, familyPath, query, ownerPrefix, scope, limit)
	if len(entries) == 0 {
		return bundle
	}
	for _, entry := range entries {
		sourceName := strings.TrimSpace(entry.Name)
		switch entry.Type {
		case "history_answer":
			answer, ok := historyAnswerFromMemoryEntry(entry)
			if !ok {
				continue
			}
			switch answer.QuestionID {
			case tphistory.HistoryQuestionActiveDirections, tphistory.HistoryQuestionNextStep:
				bundle.continueWith = appendUniqueLocalStrings(bundle.continueWith, splitTranscriptAnswerItems(answer.Answer), 4)
			case tphistory.HistoryQuestionAcceptedLearnings:
				bundle.recentLearnings = appendUniqueLocalStrings(bundle.recentLearnings, splitTranscriptAnswerItems(answer.Answer), 3)
			case tphistory.HistoryQuestionGotchas, tphistory.HistoryQuestionRegressions, tphistory.HistoryQuestionRecurringMistakes, tphistory.HistoryQuestionMisunderstandings:
				bundle.watchOutFor = appendUniqueLocalStrings(bundle.watchOutFor, splitTranscriptAnswerItems(answer.Answer), 4)
			case tphistory.HistoryQuestionSurprises:
				bundle.recentSurprises = appendUniqueLocalStrings(bundle.recentSurprises, splitTranscriptAnswerItems(answer.Answer), 3)
			case tphistory.HistoryQuestionEpisodicHistory:
				bundle.highlights = appendUniqueLocalStrings(bundle.highlights, splitTranscriptAnswerItems(answer.Answer), 3)
			}
			bundle.evidenceRefs = appendUniqueLocalStrings(bundle.evidenceRefs, answer.Evidence, 8)
			bundle.sourceNames = appendUniqueLocalStrings(bundle.sourceNames, []string{sourceName}, 8)
		case "history_notable":
			notable, ok := notableHistoryFromMemoryEntry(entry)
			if !ok {
				continue
			}
			switch notable.Kind {
			case transcriptpipeline.NotableInsightMisunderstanding, transcriptpipeline.NotableInsightGotcha:
				bundle.watchOutFor = appendUniqueLocalStrings(bundle.watchOutFor, []string{summarizeTranscriptSummary(notable.Summary)}, 4)
			case transcriptpipeline.NotableInsightSurprise:
				bundle.recentSurprises = appendUniqueLocalStrings(bundle.recentSurprises, []string{summarizeTranscriptSummary(notable.Summary)}, 3)
			case transcriptpipeline.NotableInsightProceduralLearning:
				bundle.recentLearnings = appendUniqueLocalStrings(bundle.recentLearnings, []string{summarizeTranscriptSummary(notable.Summary)}, 3)
			case transcriptpipeline.NotableInsightEpisodic:
				bundle.highlights = appendUniqueLocalStrings(bundle.highlights, []string{summarizeTranscriptSummary(notable.Summary)}, 3)
			}
			bundle.evidenceRefs = appendUniqueLocalStrings(bundle.evidenceRefs, notable.EvidenceRefs, 8)
			bundle.sourceNames = appendUniqueLocalStrings(bundle.sourceNames, []string{sourceName}, 8)
		case "history_insight":
			insight, ok := insightHistoryFromMemoryEntry(entry)
			if !ok {
				continue
			}
			switch {
			case insight.Kind == transcriptpipeline.InsightKindRisk:
				bundle.watchOutFor = appendUniqueLocalStrings(bundle.watchOutFor, []string{summarizeTranscriptSummary(insight.Summary)}, 4)
			case (insight.Status == transcriptpipeline.InsightStatusAccepted || insight.Status == transcriptpipeline.InsightStatusSupported) &&
				(insight.Kind == transcriptpipeline.InsightKindDecision || insight.Kind == transcriptpipeline.InsightKindPreference || insight.Kind == transcriptpipeline.InsightKindDirection):
				bundle.recentLearnings = appendUniqueLocalStrings(bundle.recentLearnings, []string{summarizeTranscriptSummary(insight.Summary)}, 3)
			case insight.Kind == transcriptpipeline.InsightKindDirection &&
				(insight.Status == transcriptpipeline.InsightStatusActive || insight.Status == transcriptpipeline.InsightStatusOpen) &&
				transcriptInsightSuitableForContinuation(insight):
				bundle.continueWith = appendUniqueLocalStrings(bundle.continueWith, []string{summarizeTranscriptSummary(insight.Summary)}, 4)
			}
			bundle.evidenceRefs = appendUniqueLocalStrings(bundle.evidenceRefs, insight.EvidenceRefs, 8)
			bundle.sourceNames = appendUniqueLocalStrings(bundle.sourceNames, []string{sourceName}, 8)
		}
	}
	bundle = c.refineTranscriptRetrievedBundle(ctx, query, entries, bundle)
	bundle.brief = buildTranscriptRetrievedBrief(bundle)
	return bundle
}

func (c Collector) searchTranscriptHistoryBundleEntries(ctx context.Context, workspacePath, familyPath, query, ownerPrefix string, scope TranscriptHistoryScope, limit int) []storage.NamedEntry {
	if c.MemoryStore == nil || strings.TrimSpace(ownerPrefix) == "" {
		return nil
	}
	if limit <= 0 {
		limit = 10
	}
	candidates := make(map[string]transcriptRecordCandidate, limit*3)
	addEntry := func(entry storage.NamedEntry, score float64) {
		if !strings.HasPrefix(strings.TrimSpace(entry.Name), ownerPrefix) {
			return
		}
		key := strings.TrimSpace(entry.Name) + "|" + strings.TrimSpace(entry.Workspace)
		score += transcriptRecordEntryWeight(entry)
		if existing, ok := candidates[key]; ok {
			if score > existing.score {
				existing.score = score
				existing.entry = entry
				candidates[key] = existing
			}
			return
		}
		candidates[key] = transcriptRecordCandidate{entry: entry, score: score}
	}
	addScoredEntries := func(scored []storage.ScoredEntry) {
		for _, item := range scored {
			addEntry(item.Entry, item.Score)
		}
	}
	entryTypes := []string{"history_answer", "history_notable", "history_insight"}
	if c.SemanticProvider != nil {
		if vec, err := c.SemanticProvider.Embed(ctx, query); err == nil && len(vec) > 0 {
			for _, entryType := range entryTypes {
				if scored, err := c.MemoryStore.SearchSimilarByType(ctx, workspacePath, entryType, vec, limit); err == nil {
					addScoredEntries(filterTranscriptEntriesByScope(scored, workspacePath, familyPath, scope))
				}
			}
		}
	}
	addScoredEntries(c.lexicalHistoryEntries(ctx, workspacePath, familyPath, entryTypes, query, ownerPrefix, scope, limit*4))
	for _, entry := range c.ownerHistoryEntries(ctx, workspacePath, familyPath, entryTypes, ownerPrefix, scope, limit*2, TranscriptHistoryDateRange{}) {
		addEntry(entry, 1)
	}
	if len(candidates) == 0 {
		return nil
	}
	ranked := make([]transcriptRecordCandidate, 0, len(candidates))
	for _, item := range candidates {
		ranked = append(ranked, item)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		if !ranked[i].entry.UpdatedAt.Equal(ranked[j].entry.UpdatedAt) {
			return ranked[i].entry.UpdatedAt.After(ranked[j].entry.UpdatedAt)
		}
		return ranked[i].entry.Name < ranked[j].entry.Name
	})
	out := make([]storage.NamedEntry, 0, minInt(limit, len(ranked)))
	for _, item := range ranked {
		out = append(out, item.entry)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func buildTranscriptRetrievedBrief(bundle transcriptRetrievedBundle) string {
	lines := make([]string, 0, 5)
	if len(bundle.continueWith) > 0 {
		lines = append(lines, "Continue with: "+strings.Join(shortenStrings(bundle.continueWith, 3), " | "))
	}
	if len(bundle.recentLearnings) > 0 {
		lines = append(lines, "Learned: "+strings.Join(shortenStrings(bundle.recentLearnings, 3), " | "))
	}
	if len(bundle.watchOutFor) > 0 {
		lines = append(lines, "Watch out for: "+strings.Join(shortenStrings(bundle.watchOutFor, 3), " | "))
	}
	if len(bundle.recentSurprises) > 0 {
		lines = append(lines, "Recent surprises: "+strings.Join(shortenStrings(bundle.recentSurprises, 2), " | "))
	}
	return strings.Join(lines, "\n")
}

func (c Collector) refineTranscriptRetrievedBundle(ctx context.Context, query string, entries []storage.NamedEntry, bundle transcriptRetrievedBundle) transcriptRetrievedBundle {
	if c.TranscriptWorker == nil || len(entries) == 0 {
		return bundle
	}
	artifact := buildTranscriptRetrievedSummaryArtifact(query, entries)
	if strings.TrimSpace(artifact) == "" {
		return bundle
	}
	run := c.TranscriptRun
	if run == nil {
		run = defaultTranscriptSummaryRun
	}
	result, err := run(ctx, *c.TranscriptWorker, TranscriptSummaryRequest{
		InputKind:     "transcript_history_retrieved_bundle",
		PromptVersion: "transcript_history_retrieved_summary_v1",
		SystemPrompt:  transcriptRetrievedSummaryPromptV1,
		ArtifactText:  artifact,
		MaxTokens:     220,
	})
	if err != nil {
		return bundle
	}
	payload, ok := parseTranscriptRetrievedSummaryPayload(result.OutputText)
	if !ok {
		return bundle
	}
	payload = canonicalizeRetrievedSummaryPayload(bundle, payload)
	if len(payload.ContinueWith) > 0 {
		bundle.continueWith = appendUniqueLocalStrings(nil, payload.ContinueWith, 4)
	}
	if len(payload.RecentLearnings) > 0 {
		bundle.recentLearnings = appendUniqueLocalStrings(nil, payload.RecentLearnings, 3)
	}
	if len(payload.WatchOutFor) > 0 {
		bundle.watchOutFor = appendUniqueLocalStrings(nil, payload.WatchOutFor, 4)
	}
	if len(payload.RecentSurprises) > 0 {
		bundle.recentSurprises = appendUniqueLocalStrings(nil, payload.RecentSurprises, 3)
	}
	if len(payload.Highlights) > 0 {
		bundle.highlights = appendUniqueLocalStrings(nil, payload.Highlights, 3)
	}
	if strings.TrimSpace(payload.Brief) != "" {
		bundle.brief = strings.TrimSpace(payload.Brief)
	}
	return bundle
}

func canonicalizeRetrievedSummaryPayload(existing transcriptRetrievedBundle, payload transcriptRetrievedSummaryPayload) transcriptRetrievedSummaryPayload {
	if len(existing.recentLearnings) > 0 {
		payload.RecentLearnings = append([]string(nil), existing.recentLearnings...)
	}
	payload.Brief = canonicalizeTranscriptBriefLearningLine(payload.Brief, payload.RecentLearnings)
	return payload
}

func buildTranscriptRetrievedSummaryArtifact(query string, entries []storage.NamedEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	if strings.TrimSpace(query) != "" {
		b.WriteString("task_query: ")
		b.WriteString(strings.TrimSpace(query))
		b.WriteString("\n")
	}
	b.WriteString("retrieved_records:\n")
	for idx, entry := range entries {
		b.WriteString("- rank: ")
		b.WriteString(fmt.Sprintf("%d", idx+1))
		b.WriteString("\n  type: ")
		b.WriteString(strings.TrimSpace(entry.Type))
		switch entry.Type {
		case "history_answer":
			if answer, ok := historyAnswerFromMemoryEntry(entry); ok {
				b.WriteString("\n  question: ")
				b.WriteString(string(answer.QuestionID))
				b.WriteString("\n  summary: ")
				b.WriteString(summarizeTranscriptSummary(answer.Answer))
			}
		case "history_notable":
			if notable, ok := notableHistoryFromMemoryEntry(entry); ok {
				b.WriteString("\n  notable_kind: ")
				b.WriteString(string(notable.Kind))
				b.WriteString("\n  summary: ")
				b.WriteString(summarizeTranscriptSummary(notable.Summary))
			}
		case "history_insight":
			if insight, ok := insightHistoryFromMemoryEntry(entry); ok {
				b.WriteString("\n  insight_kind: ")
				b.WriteString(string(insight.Kind))
				b.WriteString("\n  insight_status: ")
				b.WriteString(string(insight.Status))
				if strings.TrimSpace(insight.SourceBasis) != "" {
					b.WriteString("\n  source_basis: ")
					b.WriteString(strings.TrimSpace(insight.SourceBasis))
				}
				b.WriteString("\n  summary: ")
				b.WriteString(summarizeTranscriptSummary(insight.Summary))
			}
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func parseTranscriptRetrievedSummaryPayload(raw string) (transcriptRetrievedSummaryPayload, bool) {
	var payload transcriptRetrievedSummaryPayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &payload); err != nil {
		return transcriptRetrievedSummaryPayload{}, false
	}
	payload.ContinueWith = normalizeTranscriptSummaryList(payload.ContinueWith, 4)
	payload.RecentLearnings = normalizeTranscriptSummaryList(payload.RecentLearnings, 3)
	payload.WatchOutFor = normalizeTranscriptSummaryList(payload.WatchOutFor, 4)
	payload.RecentSurprises = normalizeTranscriptSummaryList(payload.RecentSurprises, 3)
	payload.Highlights = normalizeTranscriptSummaryList(payload.Highlights, 3)
	payload.Brief = strings.TrimSpace(payload.Brief)
	return payload, true
}

func normalizeTranscriptSummaryList(in []string, limit int) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, minInt(limit, len(in)))
	for _, item := range in {
		item = summarizeTranscriptSummary(item)
		if item == "" {
			continue
		}
		out = appendUniqueLocalStrings(out, []string{item}, limit)
	}
	return out
}

func canonicalizeTranscriptBriefLearningLine(brief string, learnings []string) string {
	brief = strings.TrimSpace(brief)
	if brief == "" || len(learnings) == 0 {
		return brief
	}
	lines := splitTranscriptBriefLines(brief)
	canonical := "Learned: " + strings.Join(learnings, " | ")
	found := false
	for idx, line := range lines {
		if transcriptBriefLineFamily(line) != "learning" {
			continue
		}
		lines[idx] = canonical
		found = true
	}
	if !found {
		lines = append(lines, canonical)
	}
	out := ""
	for _, line := range lines {
		out = ensureTranscriptBriefLine(out, line)
	}
	return out
}

func (c Collector) searchTranscriptHistorySupportingEntries(ctx context.Context, workspacePath, familyPath, query, ownerPrefix string, scope TranscriptHistoryScope, limit int) []storage.NamedEntry {
	if c.MemoryStore == nil {
		return nil
	}
	if limit <= 0 {
		limit = 6
	}
	seen := make(map[string]struct{}, limit)
	out := make([]storage.NamedEntry, 0, limit)
	appendEntry := func(entry storage.NamedEntry) {
		if entry.Type != "history_notable" && entry.Type != "history_insight" {
			return
		}
		if !strings.HasPrefix(strings.TrimSpace(entry.Name), ownerPrefix) {
			return
		}
		key := strings.TrimSpace(entry.Name) + "|" + strings.TrimSpace(entry.Workspace)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, entry)
	}
	addScoredEntries := func(scored []storage.ScoredEntry) {
		for _, item := range scored {
			appendEntry(item.Entry)
			if len(out) >= limit {
				return
			}
		}
	}
	if c.SemanticProvider != nil {
		if vec, err := c.SemanticProvider.Embed(ctx, query); err == nil && len(vec) > 0 {
			for _, entryType := range []string{"history_notable", "history_insight"} {
				if scored, err := c.MemoryStore.SearchSimilarByType(ctx, workspacePath, entryType, vec, limit); err == nil {
					addScoredEntries(filterTranscriptEntriesByScope(scored, workspacePath, familyPath, scope))
				}
			}
		}
	}
	addScoredEntries(c.lexicalHistoryEntries(ctx, workspacePath, familyPath, []string{"history_notable", "history_insight"}, query, ownerPrefix, scope, limit*4))
	if len(out) < limit {
		for _, entry := range c.ownerHistoryEntries(ctx, workspacePath, familyPath, []string{"history_notable", "history_insight"}, ownerPrefix, scope, limit, TranscriptHistoryDateRange{}) {
			appendEntry(entry)
			if len(out) >= limit {
				break
			}
		}
	}
	if len(out) >= limit {
		return out[:limit]
	}
	return out
}

func lexicalHistoryQueries(query string) []string {
	parts := strings.Fields(strings.TrimSpace(query))
	if len(parts) == 0 {
		return nil
	}
	out := make([]string, 0, 8)
	if len(parts) > 1 {
		window := parts
		if len(window) > 6 {
			window = window[:6]
		}
		out = append(out, strings.Join(window, " "))
	}
	for _, part := range parts {
		part = strings.Trim(strings.ToLower(part), ".,:;!?()[]{}<>\"'")
		if len(part) < 4 {
			continue
		}
		if containsString(out, part) {
			continue
		}
		out = append(out, part)
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func transcriptHistoryOwnerPrefix(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	idx := strings.LastIndex(name, ":answer:")
	if idx < 0 {
		return ""
	}
	return name[:idx+1]
}

func (c Collector) lexicalHistoryEntries(ctx context.Context, workspacePath, familyPath string, entryTypes []string, query, ownerPrefix string, scope TranscriptHistoryScope, limit int) []storage.ScoredEntry {
	if c.MemoryStore == nil || strings.TrimSpace(workspacePath) == "" || strings.TrimSpace(query) == "" || len(entryTypes) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 12
	}
	entries, _, err := c.MemoryStore.ListFiltered(ctx, workspacePath, storage.MemoryListFilter{Types: entryTypes}, 200, 0)
	if err != nil || len(entries) == 0 {
		return nil
	}
	type candidate struct {
		entry storage.NamedEntry
		score float64
	}
	out := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(ownerPrefix) != "" && !strings.HasPrefix(strings.TrimSpace(entry.Name), ownerPrefix) {
			continue
		}
		if !matchesTranscriptHistoryScope(entry, workspacePath, familyPath, scope) {
			continue
		}
		score := lexicalHistoryEntryScore(query, entry)
		if score <= 0 {
			continue
		}
		out = append(out, candidate{entry: entry, score: score})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].entry.UpdatedAt.After(out[j].entry.UpdatedAt)
	})
	scored := make([]storage.ScoredEntry, 0, minInt(limit, len(out)))
	for _, item := range out {
		scored = append(scored, storage.ScoredEntry{Entry: item.entry, Score: item.score})
		if len(scored) >= limit {
			break
		}
	}
	return scored
}

func (c Collector) collectTranscriptRecurringMistakes(ctx context.Context, workspacePath, familyPath string, scope TranscriptHistoryScope, limit int, dateRange TranscriptHistoryDateRange) []string {
	return c.collectTranscriptRecurringSummaries(ctx, workspacePath, familyPath, scope, limit, recurringMistakeSummariesFromEntry, dateRange)
}

func (c Collector) collectTranscriptRecurringLearnings(ctx context.Context, workspacePath, familyPath string, scope TranscriptHistoryScope, limit int, dateRange TranscriptHistoryDateRange) []string {
	return c.collectTranscriptRecurringSummaries(ctx, workspacePath, familyPath, scope, limit, recurringLearningSummariesFromEntry, dateRange)
}

func (c Collector) collectTranscriptRecurringSummaries(ctx context.Context, workspacePath, familyPath string, scope TranscriptHistoryScope, limit int, extract func(storage.NamedEntry) []string, dateRange TranscriptHistoryDateRange) []string {
	if c.MemoryStore == nil || limit <= 0 {
		return nil
	}
	entries, _, err := c.MemoryStore.ListFiltered(ctx, workspacePath, storage.MemoryListFilter{Types: []string{"history_answer", "history_notable", "history_insight"}}, 400, 0)
	if err != nil || len(entries) == 0 {
		return nil
	}
	clusters := make([]*recurringTranscriptCluster, 0, 12)
	for _, entry := range entries {
		if !matchesTranscriptHistoryScope(entry, workspacePath, familyPath, scope) {
			continue
		}
		entryTime := transcriptHistoryEntryTime(entry)
		if !dateRange.Contains(entryTime) {
			continue
		}
		owner := transcriptHistoryOwnerPrefix(entry.Name)
		if owner == "" {
			continue
		}
		summaries := extract(entry)
		if len(summaries) == 0 {
			continue
		}
		for _, summary := range summaries {
			summary = summarizeTranscriptSummary(summary)
			if normalizeRecurringText(summary) == "" {
				continue
			}
			cluster := matchRecurringTranscriptCluster(clusters, summary)
			if cluster == nil {
				cluster = &recurringTranscriptCluster{
					summary: summary,
					owners:  map[string]struct{}{},
				}
				clusters = append(clusters, cluster)
			}
			cluster.owners[owner] = struct{}{}
			if recurringTranscriptSummaryScore(summary) > recurringTranscriptSummaryScore(cluster.summary) {
				cluster.summary = summary
			}
			if entryTime.After(cluster.updatedAt) {
				cluster.updatedAt = entryTime
			}
		}
	}
	type ranked struct {
		summary   string
		count     int
		updatedAt time.Time
	}
	rankedClusters := make([]ranked, 0, len(clusters))
	for _, cluster := range clusters {
		if len(cluster.owners) < 2 {
			continue
		}
		rankedClusters = append(rankedClusters, ranked{
			summary:   cluster.summary,
			count:     len(cluster.owners),
			updatedAt: cluster.updatedAt,
		})
	}
	sort.SliceStable(rankedClusters, func(i, j int) bool {
		if rankedClusters[i].count != rankedClusters[j].count {
			return rankedClusters[i].count > rankedClusters[j].count
		}
		if !rankedClusters[i].updatedAt.Equal(rankedClusters[j].updatedAt) {
			return rankedClusters[i].updatedAt.After(rankedClusters[j].updatedAt)
		}
		return rankedClusters[i].summary < rankedClusters[j].summary
	})
	out := make([]string, 0, minInt(limit, len(rankedClusters)))
	for _, item := range rankedClusters {
		out = appendUniqueLocalStrings(out, []string{item.summary}, limit)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func matchRecurringTranscriptCluster(clusters []*recurringTranscriptCluster, summary string) *recurringTranscriptCluster {
	bestScore := 0.0
	var best *recurringTranscriptCluster
	for _, cluster := range clusters {
		score := recurringTranscriptSimilarity(summary, cluster.summary)
		if score < 0.5 {
			continue
		}
		if score > bestScore {
			bestScore = score
			best = cluster
		}
	}
	return best
}

func recurringTranscriptSimilarity(a, b string) float64 {
	at := recurringTranscriptTokens(a)
	bt := recurringTranscriptTokens(b)
	if len(at) == 0 || len(bt) == 0 {
		return 0
	}
	shared := 0
	for token := range at {
		if _, ok := bt[token]; ok {
			shared++
		}
	}
	if shared == 0 {
		return 0
	}
	return float64(shared*2) / float64(len(at)+len(bt))
}

func recurringTranscriptTokens(text string) map[string]struct{} {
	normalized := normalizeRecurringText(text)
	if normalized == "" {
		return nil
	}
	tokens := make(map[string]struct{})
	for _, token := range strings.Fields(normalized) {
		token = strings.Trim(token, ".,:;!?()[]{}<>\"'`")
		if len(token) < 4 {
			continue
		}
		tokens[token] = struct{}{}
	}
	return tokens
}

func recurringTranscriptSummaryScore(text string) int {
	tokens := recurringTranscriptTokens(text)
	return len(tokens)*100 - len(strings.Fields(text))
}

func lexicalHistoryEntryScore(query string, entry storage.NamedEntry) float64 {
	text := normalizeHistoryRetrievalText(historyRetrievalTextFromMemoryEntry(entry))
	if text == "" {
		return 0
	}
	score := 0.0
	for idx, phrase := range lexicalHistoryQueries(query) {
		needle := normalizeHistoryRetrievalText(phrase)
		if needle == "" {
			continue
		}
		if strings.Contains(text, needle) {
			score += float64(10 - idx)
		}
	}
	return score
}

func (c Collector) ownerHistoryEntries(ctx context.Context, workspacePath, familyPath string, entryTypes []string, ownerPrefix string, scope TranscriptHistoryScope, limit int, dateRange TranscriptHistoryDateRange) []storage.NamedEntry {
	if c.MemoryStore == nil || strings.TrimSpace(ownerPrefix) == "" || len(entryTypes) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = 6
	}
	entries, _, err := c.MemoryStore.ListFiltered(ctx, workspacePath, storage.MemoryListFilter{Types: entryTypes}, 200, 0)
	if err != nil {
		return nil
	}
	out := make([]storage.NamedEntry, 0, limit)
	for _, entry := range entries {
		if !strings.HasPrefix(strings.TrimSpace(entry.Name), ownerPrefix) {
			continue
		}
		if !matchesTranscriptHistoryScope(entry, workspacePath, familyPath, scope) {
			continue
		}
		if !dateRange.Contains(entry.UpdatedAt) {
			continue
		}
		out = append(out, entry)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func normalizeTranscriptHistoryScope(scope TranscriptHistoryScope) TranscriptHistoryScope {
	switch scope {
	case TranscriptHistoryScopeWorkspace, TranscriptHistoryScopeFamily:
		return scope
	default:
		return TranscriptHistoryScopeAuto
	}
}

func filterTranscriptEntriesByScope(in []storage.ScoredEntry, workspacePath, familyPath string, scope TranscriptHistoryScope) []storage.ScoredEntry {
	if len(in) == 0 {
		return nil
	}
	out := make([]storage.ScoredEntry, 0, len(in))
	for _, item := range in {
		if !matchesTranscriptHistoryScope(item.Entry, workspacePath, familyPath, scope) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func matchesTranscriptHistoryScope(entry storage.NamedEntry, workspacePath, familyPath string, scope TranscriptHistoryScope) bool {
	scope = normalizeTranscriptHistoryScope(scope)
	metaPath, metaFamily := historyWorkspacePathsFromMemoryEntry(entry)
	switch scope {
	case TranscriptHistoryScopeWorkspace:
		if strings.TrimSpace(metaPath) == "" {
			return false
		}
		return ws.Normalize(metaPath) == ws.Normalize(workspacePath)
	case TranscriptHistoryScopeFamily:
		if strings.TrimSpace(metaFamily) == "" {
			return true
		}
		return ws.Normalize(metaFamily) == ws.Normalize(familyPath)
	default:
		return true
	}
}

func historyWorkspacePathsFromMemoryEntry(entry storage.NamedEntry) (string, string) {
	var payload struct {
		WorkspacePath       string `json:"workspace_path"`
		WorkspaceFamilyPath string `json:"workspace_family_path"`
	}
	if err := json.Unmarshal(entry.Result, &payload); err != nil {
		return "", ""
	}
	return strings.TrimSpace(payload.WorkspacePath), strings.TrimSpace(payload.WorkspaceFamilyPath)
}

func transcriptHistoryEntryTime(entry storage.NamedEntry) time.Time {
	var payload struct {
		SourceStartedAt string `json:"source_started_at"`
	}
	if err := json.Unmarshal(entry.Result, &payload); err == nil {
		if ts, err := time.Parse(time.RFC3339, strings.TrimSpace(payload.SourceStartedAt)); err == nil && !ts.IsZero() {
			return ts.UTC()
		}
	}
	return entry.UpdatedAt
}

func historyRetrievalTextFromMemoryEntry(entry storage.NamedEntry) string {
	var payload struct {
		RetrievalText string `json:"retrieval_text"`
		Summary       string `json:"summary"`
	}
	if err := json.Unmarshal(entry.Result, &payload); err == nil {
		if text := strings.TrimSpace(payload.RetrievalText); text != "" {
			return text
		}
		if text := strings.TrimSpace(payload.Summary); text != "" {
			return text
		}
	}
	return strings.TrimSpace(entry.Summary)
}

func recurringMistakeSummariesFromEntry(entry storage.NamedEntry) []string {
	switch entry.Type {
	case "history_answer":
		answer, ok := historyAnswerFromMemoryEntry(entry)
		if !ok {
			return nil
		}
		switch answer.QuestionID {
		case tphistory.HistoryQuestionGotchas, tphistory.HistoryQuestionRegressions, tphistory.HistoryQuestionRecurringMistakes, tphistory.HistoryQuestionMisunderstandings:
			return splitTranscriptAnswerItems(answer.Answer)
		default:
			return nil
		}
	case "history_notable":
		notable, ok := notableHistoryFromMemoryEntry(entry)
		if !ok {
			return nil
		}
		switch notable.Kind {
		case transcriptpipeline.NotableInsightGotcha, transcriptpipeline.NotableInsightMisunderstanding:
			return []string{summarizeTranscriptSummary(notable.Summary)}
		default:
			return nil
		}
	case "history_insight":
		insight, ok := insightHistoryFromMemoryEntry(entry)
		if !ok {
			return nil
		}
		if insight.Kind == transcriptpipeline.InsightKindRisk {
			return []string{summarizeTranscriptSummary(insight.Summary)}
		}
	}
	return nil
}

func recurringLearningSummariesFromEntry(entry storage.NamedEntry) []string {
	switch entry.Type {
	case "history_answer":
		answer, ok := historyAnswerFromMemoryEntry(entry)
		if !ok {
			return nil
		}
		if answer.QuestionID == tphistory.HistoryQuestionAcceptedLearnings {
			return splitTranscriptAnswerItems(answer.Answer)
		}
	case "history_notable":
		notable, ok := notableHistoryFromMemoryEntry(entry)
		if !ok {
			return nil
		}
		if notable.Kind == transcriptpipeline.NotableInsightProceduralLearning {
			return []string{summarizeTranscriptSummary(notable.Summary)}
		}
	case "history_insight":
		insight, ok := insightHistoryFromMemoryEntry(entry)
		if !ok {
			return nil
		}
		if (insight.Status == transcriptpipeline.InsightStatusAccepted || insight.Status == transcriptpipeline.InsightStatusSupported) &&
			(insight.Kind == transcriptpipeline.InsightKindDecision || insight.Kind == transcriptpipeline.InsightKindPreference || insight.Kind == transcriptpipeline.InsightKindDirection) {
			return []string{summarizeTranscriptSummary(insight.Summary)}
		}
	}
	return nil
}

func recurringOrPack(recurring, fallback []string) []string {
	if len(recurring) > 0 {
		return recurring
	}
	return fallback
}

func normalizeRecurringText(text string) string {
	text = strings.TrimSpace(strings.ToLower(text))
	text = strings.TrimRight(text, ".!?")
	return strings.Join(strings.Fields(text), " ")
}

func normalizeHistoryRetrievalText(text string) string {
	text = strings.TrimSpace(strings.ToLower(text))
	text = strings.TrimRight(text, ".!?")
	text = strings.Join(strings.Fields(text), " ")
	return text
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
	if pack.Transcript != nil && strings.TrimSpace(pack.Transcript.Overview) != "" {
		parts = append(parts, "transcript: "+strings.TrimSpace(pack.Transcript.Overview))
	}
	return strings.Join(parts, " | ")
}

func historyAnswerFromMemoryEntry(entry storage.NamedEntry) (tphistory.HistoryAnswer, bool) {
	var payload struct {
		QuestionID tphistory.HistoryQuestionID `json:"history_question_id"`
		Summary    string                      `json:"summary"`
		Label      string                      `json:"answer_label"`
		Confidence float64                     `json:"confidence"`
		Evidence   []string                    `json:"evidence_refs"`
	}
	if err := json.Unmarshal(entry.Result, &payload); err != nil {
		return tphistory.HistoryAnswer{}, false
	}
	if strings.TrimSpace(string(payload.QuestionID)) == "" {
		return tphistory.HistoryAnswer{}, false
	}
	answer := strings.TrimSpace(payload.Summary)
	if answer == "" {
		answer = strings.TrimSpace(entry.Summary)
	}
	if answer == "" {
		return tphistory.HistoryAnswer{}, false
	}
	return tphistory.HistoryAnswer{
		QuestionID: payload.QuestionID,
		Answer:     answer,
		Label:      strings.TrimSpace(payload.Label),
		Confidence: payload.Confidence,
		Evidence:   append([]string(nil), payload.Evidence...),
	}, true
}

type transcriptNotableMemory struct {
	Kind         transcriptpipeline.NotableInsightKind
	Summary      string
	EvidenceRefs []string
}

func notableHistoryFromMemoryEntry(entry storage.NamedEntry) (transcriptNotableMemory, bool) {
	var payload struct {
		Kind        transcriptpipeline.NotableInsightKind `json:"notable_kind"`
		Summary     string                                `json:"summary"`
		EvidenceRef []string                              `json:"evidence_refs"`
	}
	if err := json.Unmarshal(entry.Result, &payload); err != nil {
		return transcriptNotableMemory{}, false
	}
	summary := strings.TrimSpace(payload.Summary)
	if summary == "" {
		summary = strings.TrimSpace(entry.Summary)
	}
	if payload.Kind == "" || summary == "" {
		return transcriptNotableMemory{}, false
	}
	return transcriptNotableMemory{
		Kind:         payload.Kind,
		Summary:      summary,
		EvidenceRefs: append([]string(nil), payload.EvidenceRef...),
	}, true
}

type transcriptInsightMemory struct {
	Kind         transcriptpipeline.InsightKind
	Status       transcriptpipeline.InsightStatus
	SourceBasis  string
	Tags         []string
	Summary      string
	EvidenceRefs []string
}

func insightHistoryFromMemoryEntry(entry storage.NamedEntry) (transcriptInsightMemory, bool) {
	var payload struct {
		Kind        transcriptpipeline.InsightKind   `json:"insight_kind"`
		Status      transcriptpipeline.InsightStatus `json:"insight_status"`
		SourceBasis string                           `json:"source_basis"`
		Tags        []string                         `json:"tags"`
		Summary     string                           `json:"summary"`
		EvidenceRef []string                         `json:"evidence_refs"`
	}
	if err := json.Unmarshal(entry.Result, &payload); err != nil {
		return transcriptInsightMemory{}, false
	}
	summary := strings.TrimSpace(payload.Summary)
	if summary == "" {
		summary = strings.TrimSpace(entry.Summary)
	}
	if payload.Kind == "" || summary == "" {
		return transcriptInsightMemory{}, false
	}
	return transcriptInsightMemory{
		Kind:         payload.Kind,
		Status:       payload.Status,
		SourceBasis:  strings.TrimSpace(payload.SourceBasis),
		Tags:         append([]string(nil), payload.Tags...),
		Summary:      summary,
		EvidenceRefs: append([]string(nil), payload.EvidenceRef...),
	}, true
}

func ensureTranscriptBriefLine(base, line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return strings.TrimSpace(base)
	}
	lines := splitTranscriptBriefLines(base)
	for idx, existing := range lines {
		if strings.EqualFold(strings.TrimSpace(existing), line) {
			return strings.Join(lines, "\n")
		}
		if transcriptBriefLinesOverlap(existing, line) {
			lines[idx] = line
			return strings.Join(lines, "\n")
		}
	}
	lines = append(lines, line)
	return strings.Join(lines, "\n")
}

func mergeTranscriptBrief(base, extra string) string {
	base = strings.TrimSpace(base)
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return base
	}
	for _, line := range strings.Split(extra, "\n") {
		base = ensureTranscriptBriefLine(base, line)
	}
	return base
}

func splitTranscriptBriefLines(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	parts := strings.Split(text, "\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func transcriptBriefLinesOverlap(existing, incoming string) bool {
	existingFamily := transcriptBriefLineFamily(existing)
	incomingFamily := transcriptBriefLineFamily(incoming)
	if existingFamily == "" || incomingFamily == "" || existingFamily != incomingFamily {
		return false
	}
	return sameNormalizedStringSet(
		splitTranscriptAnswerItems(transcriptBriefLineContent(existing)),
		splitTranscriptAnswerItems(transcriptBriefLineContent(incoming)),
	)
}

func transcriptBriefLineFamily(line string) string {
	switch {
	case strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "learned:"),
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "recent learnings:"):
		return "learning"
	case strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "watch out for:"):
		return "watch"
	case strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "recent surprises:"):
		return "surprise"
	case strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "continue with:"):
		return "continue"
	default:
		return ""
	}
}

func transcriptBriefLineContent(line string) string {
	line = strings.TrimSpace(line)
	if idx := strings.Index(line, ":"); idx >= 0 {
		return strings.TrimSpace(line[idx+1:])
	}
	return line
}

func transcriptBriefHasOverlappingLearning(brief string, learnings []string) bool {
	if len(learnings) == 0 {
		return false
	}
	target := splitTranscriptAnswerItems(strings.Join(learnings, " | "))
	if len(target) == 0 {
		return false
	}
	for _, line := range splitTranscriptBriefLines(brief) {
		if transcriptBriefLineFamily(line) != "learning" {
			continue
		}
		if sameNormalizedStringSet(splitTranscriptAnswerItems(transcriptBriefLineContent(line)), target) {
			return true
		}
	}
	return false
}

func splitTranscriptAnswerItems(answer string) []string {
	parts := strings.Split(strings.TrimSpace(answer), "|")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = summarizeTranscriptSummary(part)
		if part == "" {
			continue
		}
		out = appendUniqueLocalStrings(out, []string{part}, 8)
	}
	return out
}

func summarizeTranscriptSummary(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = markdownLinkPattern.ReplaceAllString(text, "$1")
	return strings.Join(strings.Fields(text), " ")
}

var markdownLinkPattern = regexp.MustCompile(`\[(.*?)\]\((.*?)\)`)

func transcriptRecordEntryWeight(entry storage.NamedEntry) float64 {
	switch entry.Type {
	case "history_answer":
		answer, ok := historyAnswerFromMemoryEntry(entry)
		if !ok {
			return 40
		}
		return transcriptAnswerWeight(answer)
	case "history_notable":
		notable, ok := notableHistoryFromMemoryEntry(entry)
		if !ok {
			return 28
		}
		switch notable.Kind {
		case transcriptpipeline.NotableInsightMisunderstanding:
			return 34
		case transcriptpipeline.NotableInsightGotcha:
			return 33
		case transcriptpipeline.NotableInsightSurprise:
			return 31
		case transcriptpipeline.NotableInsightProceduralLearning:
			return 29
		case transcriptpipeline.NotableInsightEpisodic:
			return 20
		default:
			return 24
		}
	case "history_insight":
		insight, ok := insightHistoryFromMemoryEntry(entry)
		if !ok {
			return 22
		}
		return transcriptInsightWeight(insight)
	default:
		return 0
	}
}

func transcriptAnswerWeight(answer tphistory.HistoryAnswer) float64 {
	switch answer.QuestionID {
	case tphistory.HistoryQuestionObjective:
		return 42
	case tphistory.HistoryQuestionActiveDirections:
		return 40
	case tphistory.HistoryQuestionAcceptedLearnings:
		return 38
	case tphistory.HistoryQuestionNextStep:
		return 36
	case tphistory.HistoryQuestionGotchas, tphistory.HistoryQuestionRegressions, tphistory.HistoryQuestionRecurringMistakes, tphistory.HistoryQuestionMisunderstandings:
		return 34
	case tphistory.HistoryQuestionSurprises:
		return 32
	case tphistory.HistoryQuestionEpisodicHistory:
		return 24
	default:
		return 26
	}
}

func transcriptInsightWeight(insight transcriptInsightMemory) float64 {
	switch {
	case insight.Kind == transcriptpipeline.InsightKindRisk:
		return 30
	case insight.Kind == transcriptpipeline.InsightKindDirection && (insight.Status == transcriptpipeline.InsightStatusAccepted || insight.Status == transcriptpipeline.InsightStatusSupported):
		return 29
	case insight.Kind == transcriptpipeline.InsightKindDirection:
		return 27
	case insight.Kind == transcriptpipeline.InsightKindDecision || insight.Kind == transcriptpipeline.InsightKindPreference:
		return 28
	case insight.Kind == transcriptpipeline.InsightKindContext:
		return 18
	default:
		return 16
	}
}

func transcriptInsightSuitableForContinuation(insight transcriptInsightMemory) bool {
	if strings.EqualFold(strings.TrimSpace(insight.SourceBasis), "user") || strings.EqualFold(strings.TrimSpace(insight.SourceBasis), "objective") {
		return true
	}
	for _, tag := range insight.Tags {
		switch strings.ToLower(strings.TrimSpace(tag)) {
		case "assistant-guidance", "technical-context":
			return false
		}
	}
	return insight.Kind == transcriptpipeline.InsightKindDirection && insight.Status == transcriptpipeline.InsightStatusActive
}

func sameNormalizedStringSet(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, item := range a {
		seen[normalizeHistoryRetrievalText(item)]++
	}
	for _, item := range b {
		key := normalizeHistoryRetrievalText(item)
		if seen[key] == 0 {
			return false
		}
		seen[key]--
	}
	for _, count := range seen {
		if count != 0 {
			return false
		}
	}
	return true
}

func appendUniqueLocalStrings(dst, items []string, limit int) []string {
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || containsString(dst, item) {
			continue
		}
		dst = append(dst, item)
		if limit > 0 && len(dst) >= limit {
			return dst[:limit]
		}
	}
	return dst
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
